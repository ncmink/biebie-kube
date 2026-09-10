package policy

import (
	"path/filepath"
	"testing"

	bctx "github.com/ncmink/biebie-protocol/context"

	"biebie-kube/internal/cluster"
	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
	"biebie-kube/internal/store"
)

func TestMissingPersistedModeDefaultsReadWrite(t *testing.T) {
	repo := openRepo(t)
	svc := newTestService(t, repo)

	cluster, err := repo.Create(domain.ClusterInput{
		Name:            "dev",
		EnvironmentKind: bctx.EnvironmentDevelopment,
		KubeconfigRef:   "kubeconfig_1",
		ContextName:     "default",
	}, "https://127.0.0.1:1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	policy := svc.Snapshot(cluster.ID)
	if policy.PersistedMode != domain.AccessModeReadWrite {
		t.Fatalf("persisted = %q", policy.PersistedMode)
	}
	if policy.EffectiveMode != domain.AccessModeReadWrite {
		t.Fatalf("effective = %q", policy.EffectiveMode)
	}
}

func TestProductionClusterDefaultsReadOnly(t *testing.T) {
	repo := openRepo(t)

	cluster, err := repo.Create(domain.ClusterInput{
		Name:            "prod",
		EnvironmentKind: bctx.EnvironmentProduction,
		KubeconfigRef:   "kubeconfig_1",
		ContextName:     "default",
	}, "https://127.0.0.1:1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if repo.AccessMode(cluster.ID) != domain.AccessModeReadOnly {
		t.Fatalf("access mode = %q", repo.AccessMode(cluster.ID))
	}
}

func TestSessionToggleAddsRestriction(t *testing.T) {
	repo := openRepo(t)
	manager := cluster.NewManager(repo, stubResolver{}, kube.NewFactory("test"), nil, nil)
	svc := NewService(repo, manager, nil, nil)

	cluster, err := repo.Create(domain.ClusterInput{
		Name:            "dev",
		EnvironmentKind: bctx.EnvironmentDevelopment,
		KubeconfigRef:   "kubeconfig_1",
		ContextName:     "default",
	}, "https://127.0.0.1:1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	manager.SetSessionReadOnly(cluster.ID, true)
	policy := svc.Snapshot(cluster.ID)
	if !policy.EffectiveMode.EffectiveReadOnly() {
		t.Fatal("session read-only must make the cluster read-only")
	}

	decision := svc.Check(cluster.ID, domain.CapResourceDelete)
	if decision.Allowed || decision.Code != "read_only" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestPersistedReadOnlyCannotBeOverriddenBySession(t *testing.T) {
	repo := openRepo(t)
	manager := cluster.NewManager(repo, stubResolver{}, kube.NewFactory("test"), nil, nil)
	svc := NewService(repo, manager, nil, nil)

	cluster, err := repo.Create(domain.ClusterInput{
		Name:            "prod",
		EnvironmentKind: bctx.EnvironmentProduction,
		KubeconfigRef:   "kubeconfig_1",
		ContextName:     "default",
	}, "https://127.0.0.1:1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := svc.SetSessionReadOnly(cluster.ID, false); err != nil {
		t.Fatalf("clear session read-only: %v", err)
	}
	decision := svc.Check(cluster.ID, domain.CapResourceDelete)
	if decision.Allowed {
		t.Fatal("persisted read-only must stay enforced when session override clears")
	}
}

func TestUnknownCapabilityIsDenied(t *testing.T) {
	repo := openRepo(t)
	svc := newTestService(t, repo)

	cluster, err := repo.Create(domain.ClusterInput{
		Name:            "dev",
		EnvironmentKind: bctx.EnvironmentDevelopment,
		KubeconfigRef:   "kubeconfig_1",
		ContextName:     "default",
	}, "https://127.0.0.1:1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	decision := svc.Check(cluster.ID, domain.Capability("mystery"))
	if decision.Allowed || decision.Code != "unsupported_capability" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestReadOnlyLifecycleCallback(t *testing.T) {
	repo := openRepo(t)
	manager := cluster.NewManager(repo, stubResolver{}, kube.NewFactory("test"), nil, nil)
	called := false
	svc := NewService(repo, manager, func(string) { called = true }, nil)

	cluster, err := repo.Create(domain.ClusterInput{
		Name:            "dev",
		EnvironmentKind: bctx.EnvironmentDevelopment,
		KubeconfigRef:   "kubeconfig_1",
		ContextName:     "default",
	}, "https://127.0.0.1:1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := svc.SetPersistedMode(cluster.ID, domain.AccessModeReadOnly); err != nil {
		t.Fatalf("set mode: %v", err)
	}
	if !called {
		t.Fatal("read-only transition must stop active sessions")
	}
}

type stubResolver struct{}

func (stubResolver) PathFor(string) (string, error) { return "", nil }

func openRepo(t *testing.T) *cluster.Repository {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return cluster.NewRepository(st)
}

func newTestService(t *testing.T, repo *cluster.Repository) *Service {
	t.Helper()
	manager := cluster.NewManager(repo, stubResolver{}, kube.NewFactory("test"), nil, nil)
	return NewService(repo, manager, nil, nil)
}
