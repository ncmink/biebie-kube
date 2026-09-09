package cluster

import (
	"testing"

	bctx "github.com/ncmink/biebie-protocol/context"

	"biebie-kube/internal/domain"
)

func TestSuspendForAccessDownLeavesTunnelledClustersWaiting(t *testing.T) {
	repo := newRepo(t)
	cluster := addCluster(t, repo, domain.ClusterInput{
		Name: "RKE2 Production", CustomerID: "smoi",
		KubeconfigRef: "kubeconfig_1", ContextName: "default",
		RequiresAccess: true, AccessProfileID: "tunnel-1",
	}, unreachable)

	manager := NewManager(repo, stubResolver{}, nil, nil, &recorder{})

	manager.mu.Lock()
	manager.sessions[cluster.ID] = &session{
		cluster: cluster,
		state:   domain.ClusterConnected,
		apiForward: &bctx.Forward{
			LocalPort: 16443, RemoteHost: "localhost", RemotePort: 6443,
		},
	}
	manager.mu.Unlock()

	manager.SuspendForAccessDown(cluster.ID)

	got := manager.Session(cluster.ID)
	if got.State != domain.ClusterWaitingAccess {
		t.Fatalf("state = %q, want waiting_access", got.State)
	}
	if got.APIForward != nil {
		t.Fatalf("apiForward = %+v, want cleared", got.APIForward)
	}
}
