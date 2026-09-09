//go:build darwin || linux

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	bctx "github.com/ncmink/biebie-protocol/context"
	"github.com/ncmink/biebie-protocol/ipc"

	"biebie-kube/internal/access"
	"biebie-kube/internal/cluster"
	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
	"biebie-kube/internal/store"
)

type fakeAccessServer struct {
	endpoint ipc.Endpoint
	profiles []bctx.AccessProfile
	events   chan bctx.AccessSessionChanged
}

func startFakeAccessServer(t *testing.T, profiles []bctx.AccessProfile) *fakeAccessServer {
	t.Helper()
	dir, err := os.MkdirTemp(".", "sock")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	endpoint := ipc.Endpoint{Dir: dir, Address: filepath.Join(dir, "access.sock")}
	fake := &fakeAccessServer{
		endpoint: endpoint,
		profiles: profiles,
		events:   make(chan bctx.AccessSessionChanged, 4),
	}

	srv := ipc.NewServer(endpoint)
	srv.Handle(ipc.MethodAccessProfiles, func(context.Context, json.RawMessage) (any, error) {
		return fake.profiles, nil
	})
	srv.Handle(ipc.MethodAccessConnect, func(_ context.Context, params json.RawMessage) (any, error) {
		var req struct {
			ProfileID string `json:"profileId"`
		}
		_ = json.Unmarshal(params, &req)
		for _, profile := range fake.profiles {
			if profile.ID == req.ProfileID || profile.Name == req.ProfileID {
				return bctx.AccessConnectResult{Accepted: true, ProfileID: profile.ID}, nil
			}
		}
		return nil, errProfileMissing
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("start access: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return fake
}

var errProfileMissing = &accessRefusedError{}

type accessRefusedError struct{}

func (accessRefusedError) Error() string { return "no such profile" }

func testCoreWithAccess(t *testing.T, profiles []bctx.AccessProfile) (*Core, *fakeAccessServer) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "data.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	fake := startFakeAccessServer(t, profiles)
	client := access.NewClientFor(fake.endpoint)

	core := &Core{
		store:       st,
		access:      client,
		accessCoord: newAccessCoordinator(),
	}
	repo := cluster.NewRepository(st)
	core.clusters = cluster.NewManager(repo, testResolver{path: filepath.Join(dir, "kubeconfig")}, kube.NewFactory("test"), client, &recorder{})

	return core, fake
}

type testResolver struct{ path string }

func (r testResolver) PathFor(string) (string, error) { return r.path, nil }

type recorder struct {
	mu sync.Mutex
}

func (r *recorder) Emit(string, any) {
	r.mu.Lock()
	defer r.mu.Unlock()
}

func TestColdLaunchAdoptsNameBeforeRetry(t *testing.T) {
	const (
		name = "vpn-cat"
		uuid = "812c795a-1111-2222-3333-444444444444"
	)

	core, _ := testCoreWithAccess(t, []bctx.AccessProfile{{
		ID: uuid, Name: name, State: bctx.AccessDisconnected,
	}})

	repo := core.clusters.Repository()
	clusterRecord, err := repo.Create(domain.ClusterInput{
		Name: "RKE2 Production", CustomerID: "smoi",
		KubeconfigRef: "kubeconfig_1", ContextName: "default",
		RequiresAccess: true, AccessProfileID: name,
	}, "https://127.0.0.1:1")
	if err != nil {
		t.Fatalf("create cluster: %v", err)
	}

	core.noteAccessLaunched(name)
	core.onAccessSessionChanged(bctx.AccessSessionChanged{
		ProfileID: uuid,
		State:     bctx.AccessConnected,
	})

	updated, err := repo.Get(clusterRecord.ID)
	if err != nil {
		t.Fatalf("get cluster: %v", err)
	}
	if updated.Access.ProfileID != uuid {
		t.Fatalf("profile = %q, want adopted UUID %q", updated.Access.ProfileID, uuid)
	}
}

func TestRejectedConnectIsNotTreatedAsSuccess(t *testing.T) {
	dir, err := os.MkdirTemp(".", "sock")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	endpoint := ipc.Endpoint{Dir: dir, Address: filepath.Join(dir, "decline.sock")}
	srv := ipc.NewServer(endpoint)
	srv.Handle(ipc.MethodAccessConnect, func(context.Context, json.RawMessage) (any, error) {
		return bctx.AccessConnectResult{Accepted: false, ProfileID: "ghost"}, nil
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	_, err = access.NewClientFor(endpoint).Connect(context.Background(), "ghost")
	if err == nil {
		t.Fatal("accepted:false must not be treated as success")
	}
}
