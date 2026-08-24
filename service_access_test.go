//go:build darwin || linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ncmink/biebie-protocol/ipc"

	"biebie-kube/internal/access"
)

// startRefusingAccess stands in for a running Biebie Access that does not hold
// the connection being asked for, which is what a cluster carrying a profile
// identifier from somewhere else meets.
func startRefusingAccess(t *testing.T) ipc.Endpoint {
	t.Helper()

	// A socket beside the package keeps the path under the 104-byte limit that
	// applies on macOS, which t.TempDir would exceed.
	dir, err := os.MkdirTemp(".", "sock")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	endpoint := ipc.Endpoint{Dir: dir, Address: filepath.Join(dir, "a.sock")}
	srv := ipc.NewServer(endpoint)
	srv.Handle(ipc.MethodAccessConnect, func(context.Context, json.RawMessage) (any, error) {
		return nil, errors.New("no connection with id ghost exists in Biebie Access")
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("start the stand-in: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	return endpoint
}

// A refusal is an answer.
//
// Following it with a deep link hands the same identifier to the same
// application, which drops it just as flatly — turning a message the engineer
// could act on into a window that appears to have ignored their click. The
// launcher is left nil deliberately: consulting it at all is the bug.
func TestARefusalFromAccessIsReportedRatherThanLaunched(t *testing.T) {
	service := &AccessService{core: &Core{
		access:   access.NewClientFor(startRefusingAccess(t)),
		launcher: nil,
	}}

	_, err := service.ConnectWithAccess(context.Background(), "ghost", "customer")
	if err == nil {
		t.Fatal("a refusal from a reachable Biebie Access must reach the caller")
	}
	if !strings.Contains(err.Error(), "no connection with id ghost") {
		t.Fatalf("error = %v, want the refusal Biebie Access gave", err)
	}
}

// A cluster with no profile configured is a configuration mistake, not a reason
// to launch anything.
func TestAMissingProfileIsRefusedBeforeAnythingIsContacted(t *testing.T) {
	service := &AccessService{core: &Core{access: nil, launcher: nil}}

	if _, err := service.ConnectWithAccess(context.Background(), "", "customer"); err == nil {
		t.Fatal("an empty profile must be refused")
	}
}
