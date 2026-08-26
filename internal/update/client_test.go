package update

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// trickleServer serves a body in chunks spread over roughly total, the way a
// real asset arrives on a slow link.
func trickleServer(t *testing.T, chunks int, gap time.Duration) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("test server cannot flush, so it cannot trickle a body")
			return
		}
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		for i := 0; i < chunks; i++ {
			time.Sleep(gap)
			_, _ = w.Write([]byte("x"))
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// TestATransferOutlastingAnyOverallTimeoutStillCompletes is the test that
// matters: a download that takes longer than a whole-exchange timeout must
// still finish. A client with such a timeout severs the body mid-read and
// reports "context deadline exceeded (Client.Timeout or context cancellation
// while reading body)", which is the failure users saw on large releases.
func TestATransferOutlastingAnyOverallTimeoutStillCompletes(t *testing.T) {
	const (
		chunks = 10
		gap    = 30 * time.Millisecond
	)
	server := trickleServer(t, chunks, gap)

	// A whole-exchange timeout shorter than the transfer, as the provider's
	// default is for a multi-megabyte asset.
	bounded := &http.Client{Timeout: chunks * gap / 2}
	if _, err := readAll(bounded, server.URL); err == nil {
		t.Fatal("a client with an overall timeout read the whole body, so this test proves nothing")
	}

	body, err := readAll(NewHTTPClient(), server.URL)
	if err != nil {
		t.Fatalf("reading a slow body: %v", err)
	}
	if len(body) != chunks {
		t.Errorf("read %d bytes, want %d", len(body), chunks)
	}
}

// TestAStalledConnectionIsStillBounded guards the other half of the trade: no
// overall timeout must not mean no timeout at all.
func TestAStalledConnectionIsStillBounded(t *testing.T) {
	client := NewHTTPClient()
	if client.Timeout != 0 {
		t.Errorf("client timeout = %v, want none so a large download can finish", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T, want *http.Transport", client.Transport)
	}
	if transport.ResponseHeaderTimeout != responseHeaderTimeout {
		t.Errorf("response header timeout = %v, want %v",
			transport.ResponseHeaderTimeout, responseHeaderTimeout)
	}
	if transport.TLSHandshakeTimeout != tlsHandshakeTimeout {
		t.Errorf("TLS handshake timeout = %v, want %v",
			transport.TLSHandshakeTimeout, tlsHandshakeTimeout)
	}
}

func readAll(client *http.Client, url string) ([]byte, error) {
	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return io.ReadAll(response.Body)
}
