package kube

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestRedirectKeepsTheSchemeAndNamesTheOriginalHost(t *testing.T) {
	config, err := NewFactory("test").RESTConfigVia(writeKubeconfig(t), "test", &Endpoint{
		Address:    "127.0.0.1:16443",
		ServerName: "172.16.20.65",
	})
	if err != nil {
		t.Fatalf("RESTConfigVia: %v", err)
	}

	if config.Host != "https://127.0.0.1:16443" {
		t.Errorf("host = %q, want the local stand-in", config.Host)
	}
	if config.TLSClientConfig.ServerName != "172.16.20.65" {
		t.Errorf("server name = %q, want the cluster's own address", config.TLSClientConfig.ServerName)
	}
	if config.TLSClientConfig.Insecure != true {
		t.Error("redirecting changed what the kubeconfig said about verification")
	}
}

func TestNoEndpointLeavesTheConfigAlone(t *testing.T) {
	path := writeKubeconfig(t)
	factory := NewFactory("test")

	direct, err := factory.RESTConfig(path, "test")
	if err != nil {
		t.Fatalf("RESTConfig: %v", err)
	}
	empty, err := factory.RESTConfigVia(path, "test", &Endpoint{})
	if err != nil {
		t.Fatalf("RESTConfigVia: %v", err)
	}

	for _, config := range []*rest.Config{direct, empty} {
		if config.Host != "https://127.0.0.1:6443" {
			t.Errorf("host = %q, want the kubeconfig's own server", config.Host)
		}
		if config.TLSClientConfig.ServerName != "" {
			t.Errorf("server name = %q, want none", config.TLSClientConfig.ServerName)
		}
	}
}

// A kubeconfig that sets tls-server-name was written by someone who knew what
// the certificate says, and guessing over them would break the connection they
// had already made work.
func TestAnExplicitServerNameWins(t *testing.T) {
	const kubeconfig = `apiVersion: v1
kind: Config
current-context: test
clusters:
  - name: test
    cluster:
      server: https://172.16.20.65:6443
      tls-server-name: kubernetes.default.svc
      insecure-skip-tls-verify: true
contexts:
  - name: test
    context: {cluster: test, user: test}
users:
  - name: test
    user: {token: not-a-real-token}
`
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(kubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	config, err := NewFactory("test").RESTConfigVia(path, "test", &Endpoint{
		Address:    "127.0.0.1:16443",
		ServerName: "172.16.20.65",
	})
	if err != nil {
		t.Fatalf("RESTConfigVia: %v", err)
	}
	if config.TLSClientConfig.ServerName != "kubernetes.default.svc" {
		t.Errorf("server name = %q, want the kubeconfig's own", config.TLSClientConfig.ServerName)
	}
}

// The reason Endpoint.ServerName exists, proven against a real handshake: the
// certificate is issued for the cluster's name and nothing else, and the client
// is dialling a loopback port. Verification stays on in both halves — the only
// difference is whether the original name is carried across the substitution.
func TestServerNameIsWhatKeepsVerificationOnThroughATunnel(t *testing.T) {
	const clusterName = "kube.smoi.example"

	server, caPEM := serveTLS(t, clusterName)
	local := server.Listener.Addr().String()

	path := filepath.Join(t.TempDir(), "config")
	kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: test
clusters:
  - name: test
    cluster:
      server: https://%s:6443
      certificate-authority: %s
contexts:
  - name: test
    context: {cluster: test, user: test}
users:
  - name: test
    user: {token: not-a-real-token}
`, clusterName, caPEM)
	if err := os.WriteFile(path, []byte(kubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	factory := NewFactory("test")

	config, err := factory.RESTConfigVia(path, "test", &Endpoint{
		Address:    local,
		ServerName: clusterName,
	})
	if err != nil {
		t.Fatalf("RESTConfigVia: %v", err)
	}
	if err := getNamespace(t, config); err != nil {
		t.Fatalf("a tunnelled request failed with the original name carried over: %v", err)
	}

	// Same connection, same certificate, only the name dropped.
	blind, err := factory.RESTConfigVia(path, "test", &Endpoint{Address: local})
	if err != nil {
		t.Fatalf("RESTConfigVia: %v", err)
	}
	err = getNamespace(t, blind)
	if err == nil {
		t.Fatal("the handshake succeeded without the original name, so this test no longer proves anything")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("expected a certificate failure, got: %v", err)
	}
}

// getNamespace makes one ordinary API call, so a TLS failure surfaces the way
// it would during a connection attempt. A 404 from the stub server means the
// handshake and the request both went through.
func getNamespace(t *testing.T, config *rest.Config) error {
	t.Helper()

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = clientset.CoreV1().Namespaces().Get(ctx, "default", metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	return err
}

// serveTLS starts an HTTPS listener on loopback whose certificate is issued for
// name alone, with no address among its SANs, and returns the CA file to trust
// it with. That is the shape of a real API server certificate.
func serveTLS(t *testing.T, name string) (*httptest.Server, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		DNSNames:              []string{name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("load certificate: %v", err)
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`))
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
	server.StartTLS()
	t.Cleanup(server.Close)

	caPath := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(caPath, certPEM, 0o600); err != nil {
		t.Fatalf("write CA: %v", err)
	}
	return server, caPath
}
