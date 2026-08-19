//go:build darwin || linux

package ipc

import (
	stdctx "context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"biebie-kube/protocol/version"
)

// testEndpoint keeps sockets short and local.
//
// t.TempDir embeds the test name, which pushes the path past the 104-byte
// sun_path limit on macOS, and the system temp directory is not always
// bindable in a sandboxed build environment. A directory beside the package
// avoids both problems.
func testEndpoint(t *testing.T) Endpoint {
	t.Helper()
	dir, err := os.MkdirTemp(".", "sock")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return Endpoint{Dir: dir, Address: filepath.Join(dir, "t.sock")}
}

func startServer(t *testing.T, endpoint Endpoint) *Server {
	t.Helper()
	srv := NewServer(endpoint)
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

func TestCallRoundTrip(t *testing.T) {
	endpoint := testEndpoint(t)
	srv := startServer(t, endpoint)
	srv.Handle(MethodAccessStatus, func(_ stdctx.Context, params json.RawMessage) (any, error) {
		var req struct {
			ProfileID string `json:"profileId"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		return map[string]any{"profileId": req.ProfileID, "connected": true}, nil
	})

	var got struct {
		ProfileID string `json:"profileId"`
		Connected bool   `json:"connected"`
	}
	client := NewClient(endpoint)
	if err := client.Call(stdctx.Background(), MethodAccessStatus,
		map[string]string{"profileId": "smoi-vpn"}, &got); err != nil {
		t.Fatalf("call: %v", err)
	}
	if got.ProfileID != "smoi-vpn" || !got.Connected {
		t.Fatalf("result = %+v", got)
	}
}

func TestPingIsRegisteredByDefault(t *testing.T) {
	endpoint := testEndpoint(t)
	startServer(t, endpoint)

	if !NewClient(endpoint).Available(stdctx.Background()) {
		t.Fatal("a started server must answer ping")
	}
}

func TestMissingPeerIsNotAnError(t *testing.T) {
	endpoint := testEndpoint(t)
	client := NewClient(endpoint)

	if client.Available(stdctx.Background()) {
		t.Fatal("nothing is listening")
	}
	err := client.Call(stdctx.Background(), MethodPing, nil, nil)
	if !errors.Is(err, ErrPeerUnavailable) {
		t.Fatalf("error = %v, want ErrPeerUnavailable", err)
	}
}

func TestHandlerErrorReachesCaller(t *testing.T) {
	endpoint := testEndpoint(t)
	srv := startServer(t, endpoint)
	srv.Handle(MethodConsumeHandoff, func(stdctx.Context, json.RawMessage) (any, error) {
		return nil, errors.New("handoff expired")
	})

	err := NewClient(endpoint).Call(stdctx.Background(), MethodConsumeHandoff, nil, nil)
	if err == nil || err.Error() != "handoff expired" {
		t.Fatalf("error = %v, want the handler's message", err)
	}
}

func TestUnsupportedMethodIsRefused(t *testing.T) {
	endpoint := testEndpoint(t)
	startServer(t, endpoint)

	err := NewClient(endpoint).Call(stdctx.Background(), Method("nonsense"), nil, nil)
	if err == nil {
		t.Fatal("an unknown method must be refused")
	}
}

func TestFutureProtocolVersionIsRefused(t *testing.T) {
	endpoint := testEndpoint(t)
	srv := startServer(t, endpoint)

	req := Request{Envelope: version.Envelope{Protocol: version.Name, Version: version.Current + 1}, Method: MethodPing}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp := srv.dispatch(payload)
	if resp.OK {
		t.Fatal("a newer protocol version must not be silently accepted")
	}
}

func TestSocketIsNotReadableByOtherUsers(t *testing.T) {
	endpoint := testEndpoint(t)
	startServer(t, endpoint)

	info, err := os.Stat(endpoint.Address)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("socket mode = %v, want no group or world access", perm)
	}

	dirInfo, err := os.Stat(endpoint.Dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("directory mode = %v, want 0700", perm)
	}
}

func TestStaleSocketIsReplaced(t *testing.T) {
	endpoint := testEndpoint(t)
	if err := os.WriteFile(endpoint.Address, []byte("stale"), fs.FileMode(0o600)); err != nil {
		t.Fatalf("seed stale socket: %v", err)
	}
	startServer(t, endpoint)

	if !NewClient(endpoint).Available(stdctx.Background()) {
		t.Fatal("a stale socket from a crashed process must not block startup")
	}
}

func TestSecondInstanceIsRefused(t *testing.T) {
	endpoint := testEndpoint(t)
	startServer(t, endpoint)

	second := NewServer(endpoint)
	if err := second.Start(); err == nil {
		_ = second.Close()
		t.Fatal("a second listener on the same endpoint must be refused")
	}
}
