//go:build darwin || linux

package access

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	bctx "github.com/ncmink/biebie-protocol/context"
	"github.com/ncmink/biebie-protocol/handoff"
	"github.com/ncmink/biebie-protocol/ipc"
)

// fakeAccess is a stand-in for Biebie Access speaking the real protocol on a
// throwaway endpoint.
type fakeAccess struct {
	endpoint ipc.Endpoint
	tickets  *handoff.Store
}

func startFakeAccess(t *testing.T, connected bool) *fakeAccess {
	t.Helper()

	// A socket beside the package keeps the path under the 104-byte limit that
	// applies on macOS, which t.TempDir would exceed.
	dir, err := os.MkdirTemp(".", "sock")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	endpoint := ipc.Endpoint{Dir: dir, Address: filepath.Join(dir, "a.sock")}
	fake := &fakeAccess{endpoint: endpoint, tickets: handoff.NewStore()}

	srv := ipc.NewServer(endpoint)
	srv.Handle(ipc.MethodAccessStatus, func(_ context.Context, params json.RawMessage) (any, error) {
		var req struct {
			ProfileID string `json:"profileId"`
		}
		_ = json.Unmarshal(params, &req)
		state := bctx.AccessDisconnected
		if connected {
			state = bctx.AccessConnected
		}
		return bctx.AccessStatus{ProfileID: req.ProfileID, State: state, Connected: connected}, nil
	})
	srv.Handle(ipc.MethodConsumeHandoff, func(_ context.Context, params json.RawMessage) (any, error) {
		var req struct {
			HandoffID string `json:"handoffId"`
			TargetApp string `json:"targetApp"`
		}
		_ = json.Unmarshal(params, &req)
		return fake.tickets.ConsumeFor(req.HandoffID, bctx.App(req.TargetApp))
	})

	if err := srv.Start(); err != nil {
		t.Fatalf("start fake Biebie Access: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	return fake
}

func TestStatusReportsConnectedProfile(t *testing.T) {
	fake := startFakeAccess(t, true)
	client := NewClientFor(fake.endpoint)

	status, err := client.Status(context.Background(), "smoi-vpn")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Connected || status.State != bctx.AccessConnected {
		t.Fatalf("status = %+v", status)
	}
}

func TestMissingAccessIsReportedAsUnknownNotAsFailure(t *testing.T) {
	dir, err := os.MkdirTemp(".", "sock")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	client := NewClientFor(ipc.Endpoint{Dir: dir, Address: filepath.Join(dir, "gone.sock")})

	if client.Installed(context.Background()) {
		t.Fatal("nothing is listening")
	}
	status, err := client.Status(context.Background(), "smoi-vpn")
	if err != nil {
		t.Fatalf("a missing sibling application must not be an error: %v", err)
	}
	if status.State != bctx.AccessUnknown {
		t.Fatalf("state = %q, want unknown", status.State)
	}
}

func TestConsumeHandoffReturnsContext(t *testing.T) {
	fake := startFakeAccess(t, true)

	id, err := fake.tickets.CreateHandoff(context.Background(), handoff.ContextHandoff{
		SourceApp: bctx.AppAccess,
		TargetApp: bctx.AppKube,
		Context: bctx.BiebieContext{
			ContextID:       "ctx_1",
			CustomerID:      "smoi",
			CustomerName:    "SMOI",
			EnvironmentID:   "prod",
			EnvironmentName: "Production",
			AccessProfileID: "smoi-vpn",
			ClusterID:       "rke2-prod",
			ClusterName:     "RKE2 Production",
			Server:          "https://172.16.20.65:6443",
		},
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	client := NewClientFor(fake.endpoint)
	received, err := client.ConsumeHandoff(context.Background(), id)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if received.ClusterName != "RKE2 Production" || received.CustomerName != "SMOI" {
		t.Fatalf("context = %+v", received)
	}

	if _, err := client.ConsumeHandoff(context.Background(), id); err == nil {
		t.Fatal("a handoff must not be redeemable twice")
	}
}

func TestHandoffAddressedElsewhereIsRefused(t *testing.T) {
	fake := startFakeAccess(t, true)

	id, err := fake.tickets.CreateHandoff(context.Background(), handoff.ContextHandoff{
		SourceApp: bctx.AppKube,
		TargetApp: bctx.AppAccess,
		Context: bctx.BiebieContext{
			ContextID:  "ctx_1",
			CustomerID: "smoi",
			ClusterID:  "rke2-prod",
		},
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	if _, err := NewClientFor(fake.endpoint).ConsumeHandoff(context.Background(), id); err == nil {
		t.Fatal("Biebie Kube must refuse a handoff issued for another application")
	}
}
