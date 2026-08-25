package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

// stubProvider stands in for the GitHub provider, returning a release whose
// asset URL points at a test server.
type stubProvider struct {
	assetURL string
	payload  []byte
}

func (s stubProvider) Name() string { return "github" }

func (s stubProvider) Check(context.Context, updater.CheckRequest) (*updater.Release, error) {
	digest := sha256.Sum256(s.payload)
	return &updater.Release{
		Version:  "1.2.3",
		Artifact: updater.Artifact{Filename: "app-darwin-universal.zip"},
		Verification: &updater.Verification{
			DigestAlgo: "sha256",
			Digest:     digest[:],
		},
		Metadata: map[string]any{"github.asset.url": s.assetURL},
	}, nil
}

func (s stubProvider) Download(_ context.Context, _ *updater.Release, dst io.Writer, _ func(int64, int64)) error {
	_, err := dst.Write(s.payload)
	return err
}

// TestCheckAttachesVerifiableSignature is the test that matters: a signature
// produced by cmd/sign-release must satisfy ed25519.Verify against the SHA-256
// digest the Updater computes while streaming the download. If the signer and
// the framework disagree on what is signed, every release fails to install.
func TestCheckAttachesVerifiableSignature(t *testing.T) {
	payload := []byte("pretend this is a release zip")
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	digest := sha256.Sum256(payload)
	signature := ed25519.Sign(private, digest[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, signatureSuffix) {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, base64.StdEncoding.EncodeToString(signature)+"\n")
	}))
	defer server.Close()

	provider := WithSignatures(stubProvider{assetURL: server.URL + "/app.zip", payload: payload})

	release, err := provider.Check(context.Background(), updater.CheckRequest{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if release.Verification.SignatureAlgo != "ed25519" {
		t.Errorf("SignatureAlgo = %q, want ed25519", release.Verification.SignatureAlgo)
	}
	if !bytes.Equal(release.Verification.Signature, signature) {
		t.Error("attached signature does not match the one published")
	}
	if !ed25519.Verify(public, digest[:], release.Verification.Signature) {
		t.Error("attached signature does not verify against the artifact digest")
	}
}

func TestCheckRejectsMissingSignature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()

	provider := WithSignatures(stubProvider{assetURL: server.URL + "/app.zip", payload: []byte("x")})

	release, err := provider.Check(context.Background(), updater.CheckRequest{})
	if err == nil {
		t.Fatalf("expected an error for an unsigned release, got release %+v", release)
	}
}

func TestDecodeSignatureRejectsMalformedInput(t *testing.T) {
	for name, body := range map[string]string{
		"empty":      "   \n",
		"not base64": "!!!not base64!!!\n",
		"too short":  base64.StdEncoding.EncodeToString([]byte("short")) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeSignature([]byte(body)); err == nil {
				t.Error("expected an error, got none")
			}
		})
	}
}

// TestSignReleaseMatchesFrameworkScheme runs the real signing tool against the
// repository's own key pair and checks the result verifies under the public key
// the application embeds, so the committed PEM and the signer stay in step.
func TestSignReleaseMatchesFrameworkScheme(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	if _, err := os.Stat(filepath.Join(repoRoot, "updater-key")); err != nil {
		t.Skip("no local updater-key; signing is exercised in CI")
	}

	artifact := filepath.Join(t.TempDir(), "app-darwin-universal.zip")
	payload := []byte("pretend this is a release zip")
	if err := os.WriteFile(artifact, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "run", "./cmd/sign-release", "-key", "updater-key", artifact)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sign-release: %v\n%s", err, out)
	}

	signature, err := decodeSignature(mustRead(t, artifact+signatureSuffix))
	if err != nil {
		t.Fatalf("decoding the tool's output: %v", err)
	}

	public := embeddedPublicKey(t, filepath.Join(repoRoot, "updater-key.pem"))

	digest := sha256.Sum256(payload)
	if !ed25519.Verify(public, digest[:], signature) {
		t.Error("signature from cmd/sign-release does not verify under updater-key.pem")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// embeddedPublicKey parses the PKIX PEM key the application pins, the same way
// the framework's verifier does.
func embeddedPublicKey(t *testing.T, path string) ed25519.PublicKey {
	t.Helper()
	block, _ := pem.Decode(mustRead(t, path))
	if block == nil {
		t.Fatalf("%s is not PEM", path)
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	public, ok := parsed.(ed25519.PublicKey)
	if !ok {
		t.Fatalf("%s holds a %T, want Ed25519", path, parsed)
	}
	return public
}
