package kube

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const testKubeconfig = `apiVersion: v1
kind: Config
current-context: test
clusters:
  - name: test
    cluster:
      server: https://127.0.0.1:6443
      insecure-skip-tls-verify: true
contexts:
  - name: test
    context:
      cluster: test
      user: test
users:
  - name: test
    user:
      token: not-a-real-token
`

func writeKubeconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(testKubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

func TestRESTConfigBoundsOrdinaryRequests(t *testing.T) {
	config, err := NewFactory("test").RESTConfig(writeKubeconfig(t), "test")
	if err != nil {
		t.Fatalf("RESTConfig: %v", err)
	}
	if config.Timeout != RequestTimeout {
		t.Errorf("ordinary request timeout = %v, want %v", config.Timeout, RequestTimeout)
	}
}

// A timeout on the streaming config severs a log follow the moment it elapses,
// which the user sees as "request canceled ... while reading body" instead of
// the logs they asked to watch.
func TestStreamConfigHasNoRequestTimeout(t *testing.T) {
	config, err := NewFactory("test").RESTConfig(writeKubeconfig(t), "test")
	if err != nil {
		t.Fatalf("RESTConfig: %v", err)
	}

	stream := StreamConfig(config)
	if stream.Timeout != 0 {
		t.Errorf("streaming request timeout = %v, want none", stream.Timeout)
	}
	if config.Timeout != RequestTimeout {
		t.Errorf("StreamConfig mutated its input: timeout = %v, want %v", config.Timeout, RequestTimeout)
	}
	if stream.Host != config.Host || stream.BearerToken != config.BearerToken {
		t.Error("streaming config lost the connection settings it was copied from")
	}
}

// This pins the behaviour StreamConfig exists for: an http.Client timeout ends
// the reading of the body, not only the wait for response headers, so a healthy
// log follow dies the moment it elapses. The durations are shortened to keep
// the test quick; RequestTimeout makes the same stream fail after 30 seconds.
func TestARequestTimeoutSeversAFollowedLogStream(t *testing.T) {
	const (
		clientTimeout = 100 * time.Millisecond
		writeGap      = 400 * time.Millisecond
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("test server cannot stream a response")
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "first")
		flusher.Flush()

		// The gap stands in for a container that is quiet for a while, which
		// is when the timeout has something to interrupt.
		time.Sleep(writeGap)
		fmt.Fprintln(w, "second")
		flusher.Flush()
	}))
	defer server.Close()

	bounded := &rest.Config{Host: server.URL, Timeout: clientTimeout}

	if lines, err := followLogs(t, bounded); err == nil {
		t.Errorf("bounded client read %v and reported no error, so this test no longer proves anything", lines)
	}

	lines, err := followLogs(t, StreamConfig(bounded))
	if err != nil {
		t.Fatalf("streaming client: %v", err)
	}
	if len(lines) != 2 {
		t.Errorf("streaming client read %v, want both lines", lines)
	}
}

// followLogs reads a followed log stream to its end, the way the log service
// does, and reports how far it got.
func followLogs(t *testing.T, config *rest.Config) ([]string, error) {
	t.Helper()

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	body, err := clientset.CoreV1().Pods("default").
		GetLogs("pod", &corev1.PodLogOptions{Follow: true}).
		Stream(context.Background())
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()

	var lines []string
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func TestBuildProvidesStreamingClients(t *testing.T) {
	client, err := NewFactory("test").Build(writeKubeconfig(t), "test")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if client.StreamClientset == nil {
		t.Error("StreamClientset is nil, so log follow would use the bounded client")
	}
	if client.StreamDynamic == nil {
		t.Error("StreamDynamic is nil, so watches would use the bounded client")
	}
}
