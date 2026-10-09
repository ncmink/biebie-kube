package cluster

import (
	"testing"

	"biebie-kube/internal/domain"
)

func TestSuspendForAuthFailureLeavesClusterUnauthorized(t *testing.T) {
	repo := newRepo(t)
	cluster := addCluster(t, repo, domain.ClusterInput{
		Name: "Test", KubeconfigRef: "kubeconfig_1", ContextName: "default",
	}, unreachable)

	var cleaned bool
	manager := NewManager(repo, stubResolver{}, nil, nil, &recorder{})
	manager.OnAuthFailure(func(string) { cleaned = true })

	manager.mu.Lock()
	manager.sessions[cluster.ID] = &session{
		cluster: cluster,
		state:   domain.ClusterConnected,
	}
	manager.mu.Unlock()

	manager.SuspendForAuthFailure(cluster.ID)

	got := manager.Session(cluster.ID)
	if got.State != domain.ClusterUnauthorized {
		t.Fatalf("state = %q, want unauthorized", got.State)
	}
	if got.Diagnosis == nil || got.Diagnosis.Summary != "Session expired — reconnect" {
		t.Fatalf("diagnosis = %+v", got.Diagnosis)
	}
	if !cleaned {
		t.Fatal("auth failure hook was not called")
	}
}
