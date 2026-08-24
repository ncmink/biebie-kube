package cluster

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	bctx "github.com/ncmink/biebie-protocol/context"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
	"biebie-kube/internal/store"
)

type stubAccess struct {
	installed bool
	status    bctx.AccessStatus
	err       error

	mu    sync.Mutex
	asked []string
}

func (s *stubAccess) Installed(context.Context) bool { return s.installed }

func (s *stubAccess) Status(_ context.Context, profileID string) (bctx.AccessStatus, error) {
	s.mu.Lock()
	s.asked = append(s.asked, profileID)
	s.mu.Unlock()
	if s.err != nil {
		return bctx.AccessStatus{}, s.err
	}
	return s.status, nil
}

type stubResolver struct{ path string }

func (s stubResolver) PathFor(string) (string, error) { return s.path, nil }

type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) Emit(event string, _ any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recorder) saw(event string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e == event {
			return true
		}
	}
	return false
}

func newRepo(t *testing.T) *Repository {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "data.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return NewRepository(st)
}

func addCluster(t *testing.T, repo *Repository, in domain.ClusterInput, server string) domain.Cluster {
	t.Helper()
	cluster, err := repo.Create(in, server)
	if err != nil {
		t.Fatalf("create cluster: %v", err)
	}
	return cluster
}

func TestClusterWaitsForAccessInsteadOfFailing(t *testing.T) {
	repo := newRepo(t)
	cluster := addCluster(t, repo, domain.ClusterInput{
		Name:            "RKE2 Production",
		CustomerID:      "smoi",
		CustomerName:    "SMOI",
		EnvironmentKind: bctx.EnvironmentProduction,
		KubeconfigRef:   "kubeconfig_1",
		ContextName:     "default",
		RequiresAccess:  true,
		AccessProfileID: "smoi-vpn",
	}, "https://172.16.20.65:6443")

	access := &stubAccess{installed: true, status: bctx.AccessStatus{
		ProfileID: "smoi-vpn", State: bctx.AccessDisconnected,
	}}
	events := &recorder{}
	manager := NewManager(repo, stubResolver{}, kube.NewFactory("test"), access, events)

	session, err := manager.Connect(context.Background(), cluster.ID)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if session.State != domain.ClusterWaitingAccess {
		t.Fatalf("state = %q, want waiting_access", session.State)
	}
	if session.Diagnosis == nil || session.Diagnosis.AccessProfileID != "smoi-vpn" {
		t.Fatalf("the diagnosis must name the profile the UI should offer: %+v", session.Diagnosis)
	}
	if !events.saw(EventSessionChanged) {
		t.Fatal("the UI must be told the session changed")
	}
}

func TestMissingAccessApplicationIsExplainedNotCrashed(t *testing.T) {
	repo := newRepo(t)
	cluster := addCluster(t, repo, domain.ClusterInput{
		Name: "RKE2 Production", CustomerID: "smoi",
		KubeconfigRef: "kubeconfig_1", ContextName: "default",
		RequiresAccess: true, AccessProfileID: "smoi-vpn",
	}, "https://172.16.20.65:6443")

	manager := NewManager(repo, stubResolver{}, kube.NewFactory("test"), &stubAccess{installed: false}, &recorder{})

	session, err := manager.Connect(context.Background(), cluster.ID)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if session.State != domain.ClusterWaitingAccess {
		t.Fatalf("state = %q", session.State)
	}
	if session.Diagnosis.Kind != domain.FailureAccessDown {
		t.Fatalf("kind = %q", session.Diagnosis.Kind)
	}
}

func TestClusterWithoutAccessRequirementSkipsBiebieAccess(t *testing.T) {
	repo := newRepo(t)
	cluster := addCluster(t, repo, domain.ClusterInput{
		Name: "docker-desktop", KubeconfigRef: "kubeconfig_1", ContextName: "docker-desktop",
	}, "https://127.0.0.1:1")

	access := &stubAccess{installed: true}
	manager := NewManager(repo, stubResolver{path: "/nonexistent"}, kube.NewFactory("test"), access, &recorder{})

	session, err := manager.Connect(context.Background(), cluster.ID)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	access.mu.Lock()
	asked := len(access.asked)
	access.mu.Unlock()
	if asked != 0 {
		t.Fatal("a local cluster must not consult Biebie Access")
	}
	// Nothing is listening on that port, so the attempt stops at the TCP probe
	// rather than reporting a Kubernetes problem.
	if session.State != domain.ClusterUnreachable {
		t.Fatalf("state = %q, want unreachable", session.State)
	}
	if session.Diagnosis == nil || len(session.Diagnosis.Probes) == 0 {
		t.Fatal("a failed connection must explain which layer failed")
	}
	if session.Diagnosis.Probes[0].Layer != domain.LayerTCP {
		t.Fatalf("first probe = %q, want tcp", session.Diagnosis.Probes[0].Layer)
	}
}

func TestUnreachableClusterMarksLaterLayersSkipped(t *testing.T) {
	repo := newRepo(t)
	cluster := addCluster(t, repo, domain.ClusterInput{
		Name: "unreachable", KubeconfigRef: "kubeconfig_1", ContextName: "default",
	}, "https://127.0.0.1:1")

	manager := NewManager(repo, stubResolver{path: "/nonexistent"}, kube.NewFactory("test"), nil, &recorder{})
	session, _ := manager.Connect(context.Background(), cluster.ID)

	var kubernetesProbe *domain.Probe
	for i, probe := range session.Diagnosis.Probes {
		if probe.Layer == domain.LayerKubernetes {
			kubernetesProbe = &session.Diagnosis.Probes[i]
		}
	}
	if kubernetesProbe == nil {
		t.Fatal("the Kubernetes layer must appear in the diagnosis")
	}
	if kubernetesProbe.Result != domain.ProbeSkipped {
		t.Fatalf("Kubernetes probe = %q; an untested layer must not be reported as failed", kubernetesProbe.Result)
	}
}

func TestMissingKubeconfigIsAConfigurationFailure(t *testing.T) {
	repo := newRepo(t)
	cluster := addCluster(t, repo, domain.ClusterInput{
		Name: "orphan", KubeconfigRef: "kubeconfig_gone", ContextName: "default",
	}, "https://127.0.0.1:1")

	resolver := failingResolver{}
	manager := NewManager(repo, resolver, kube.NewFactory("test"), nil, &recorder{})

	session, _ := manager.Connect(context.Background(), cluster.ID)
	if session.State != domain.ClusterFailed {
		t.Fatalf("state = %q, want failed", session.State)
	}
	if session.Diagnosis.Kind != domain.FailureConfig {
		t.Fatalf("kind = %q", session.Diagnosis.Kind)
	}
}

type failingResolver struct{}

func (failingResolver) PathFor(string) (string, error) {
	return "", errors.New("kubeconfig_gone is not imported")
}

func TestNamespaceIsRememberedPerCluster(t *testing.T) {
	repo := newRepo(t)
	first := addCluster(t, repo, domain.ClusterInput{
		Name: "a", KubeconfigRef: "k", ContextName: "a",
	}, "https://127.0.0.1:1")
	second := addCluster(t, repo, domain.ClusterInput{
		Name: "b", KubeconfigRef: "k", ContextName: "b",
	}, "https://127.0.0.1:2")

	if err := repo.RememberNamespace(first.ID, "argocd"); err != nil {
		t.Fatalf("remember: %v", err)
	}
	if err := repo.RememberNamespace(second.ID, "monitoring"); err != nil {
		t.Fatalf("remember: %v", err)
	}

	if got := repo.Namespace(first.ID); got != "argocd" {
		t.Fatalf("namespace = %q", got)
	}
	if got := repo.Namespace(second.ID); got != "monitoring" {
		t.Fatalf("namespace = %q; each cluster keeps its own", got)
	}
}

func TestFindByContextMatchesCustomerAndName(t *testing.T) {
	repo := newRepo(t)
	addCluster(t, repo, domain.ClusterInput{
		Name: "RKE2 Production", CustomerID: "smoi", EnvironmentID: "prod",
		KubeconfigRef: "k", ContextName: "smoi-prod",
	}, "https://172.16.20.65:6443")
	addCluster(t, repo, domain.ClusterInput{
		Name: "RKE2 Development", CustomerID: "smoi", EnvironmentID: "dev",
		KubeconfigRef: "k", ContextName: "smoi-dev",
	}, "https://172.16.20.66:6443")

	found, ok := repo.FindByContext(bctx.BiebieContext{
		CustomerID: "smoi", ClusterName: "RKE2 Production", ClusterID: "rke2-prod",
	})
	if !ok {
		t.Fatal("a handoff naming a customer and cluster must resolve")
	}
	if found.Name != "RKE2 Production" {
		t.Fatalf("matched %q", found.Name)
	}
}

func TestFindByContextFallsBackToSoleClusterForEnvironment(t *testing.T) {
	repo := newRepo(t)
	addCluster(t, repo, domain.ClusterInput{
		Name: "Production cluster", CustomerID: "smoi", EnvironmentID: "prod",
		KubeconfigRef: "k", ContextName: "smoi-prod",
	}, "https://172.16.20.65:6443")

	found, ok := repo.FindByContext(bctx.BiebieContext{
		CustomerID: "smoi", EnvironmentID: "prod",
		ClusterID: "named-differently-in-access", ClusterName: "RKE2 Production",
	})
	if !ok {
		t.Fatal("the two applications may name a cluster differently and still mean the same one")
	}
	if found.EnvironmentID != "prod" {
		t.Fatalf("matched %+v", found)
	}
}

func TestFindByContextRefusesToGuessBetweenCandidates(t *testing.T) {
	repo := newRepo(t)
	addCluster(t, repo, domain.ClusterInput{
		Name: "one", CustomerID: "smoi", EnvironmentID: "prod", KubeconfigRef: "k", ContextName: "a",
	}, "https://10.0.0.1:6443")
	addCluster(t, repo, domain.ClusterInput{
		Name: "two", CustomerID: "smoi", EnvironmentID: "prod", KubeconfigRef: "k", ContextName: "b",
	}, "https://10.0.0.2:6443")

	if _, ok := repo.FindByContext(bctx.BiebieContext{
		CustomerID: "smoi", EnvironmentID: "prod", ClusterName: "something else",
	}); ok {
		t.Fatal("opening the wrong production cluster is worse than asking the user")
	}
}

func TestDeletingAClusterLeavesTheKubeconfigAlone(t *testing.T) {
	dir := t.TempDir()
	kubeconfigPath := filepath.Join(dir, "config")
	if err := os.WriteFile(kubeconfigPath, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("seed kubeconfig: %v", err)
	}

	repo := newRepo(t)
	cluster := addCluster(t, repo, domain.ClusterInput{
		Name: "a", KubeconfigRef: "k", ContextName: "a",
	}, "https://127.0.0.1:1")

	if err := repo.Delete(cluster.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(kubeconfigPath); err != nil {
		t.Fatal("the user's kubeconfig must survive deleting a Biebie cluster record")
	}
}

func TestProductionSortsAfterOtherEnvironments(t *testing.T) {
	repo := newRepo(t)
	addCluster(t, repo, domain.ClusterInput{
		Name: "prod", CustomerID: "smoi", CustomerName: "SMOI",
		EnvironmentKind: bctx.EnvironmentProduction, KubeconfigRef: "k", ContextName: "p",
	}, "https://10.0.0.1:6443")
	addCluster(t, repo, domain.ClusterInput{
		Name: "dev", CustomerID: "smoi", CustomerName: "SMOI",
		EnvironmentKind: bctx.EnvironmentDevelopment, KubeconfigRef: "k", ContextName: "d",
	}, "https://10.0.0.2:6443")

	all := repo.All()
	if all[0].Name != "dev" {
		t.Fatalf("order = %q first; production must not sit where a mis-click lands", all[0].Name)
	}
}

// A connection may be configured by the name Biebie Access shows, which is
// readable but not what Biebie Access reports state changes under. Adopting the
// identifier is what lets the two sides recognise the same tunnel.
func TestAdoptingAnIdentifierRewritesEveryClusterThatUsedTheName(t *testing.T) {
	repo := newRepo(t)
	base := domain.ClusterInput{
		CustomerID: "smoi", KubeconfigRef: "kubeconfig_1",
		RequiresAccess: true, AccessProfileID: "vpn-cat",
	}

	base.Name, base.ContextName = "Staging", "staging"
	first := addCluster(t, repo, base, "https://10.0.0.1:6443")
	base.Name, base.ContextName = "Production", "production"
	second := addCluster(t, repo, base, "https://10.0.0.2:6443")

	// A cluster on a different connection must be left alone.
	base.Name, base.ContextName = "Other", "other"
	base.AccessProfileID = "govbkk"
	other := addCluster(t, repo, base, "https://10.0.0.3:6443")

	changed, err := repo.AdoptAccessProfileID("vpn-cat", "812c795a")
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if changed != 2 {
		t.Fatalf("changed = %d, want the two clusters on that connection", changed)
	}

	for _, id := range []string{first.ID, second.ID} {
		cluster, err := repo.Get(id)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if cluster.Access.ProfileID != "812c795a" {
			t.Fatalf("profile = %q, want the adopted identifier", cluster.Access.ProfileID)
		}
	}
	untouched, err := repo.Get(other.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if untouched.Access.ProfileID != "govbkk" {
		t.Fatalf("an unrelated cluster was rewritten to %q", untouched.Access.ProfileID)
	}
}

func TestAdoptingIgnoresNothingToDo(t *testing.T) {
	repo := newRepo(t)
	addCluster(t, repo, domain.ClusterInput{
		Name: "Staging", CustomerID: "smoi", KubeconfigRef: "kubeconfig_1",
		ContextName: "default", RequiresAccess: true, AccessProfileID: "vpn-cat",
	}, "https://10.0.0.1:6443")

	for _, pair := range [][2]string{{"", "x"}, {"x", ""}, {"vpn-cat", "vpn-cat"}, {"absent", "x"}} {
		changed, err := repo.AdoptAccessProfileID(pair[0], pair[1])
		if err != nil {
			t.Fatalf("adopt %v: %v", pair, err)
		}
		if changed != 0 {
			t.Fatalf("adopt %v changed %d clusters, want none", pair, changed)
		}
	}
}

// A cluster that started waiting under its old reference must still be retried
// when the tunnel comes up under the adopted one, or the engineer is left
// clicking connect on a customer network that is already up.
func TestWaitingClustersAreRetriedUnderTheAdoptedIdentifier(t *testing.T) {
	repo := newRepo(t)
	cluster := addCluster(t, repo, domain.ClusterInput{
		Name: "RKE2 Production", CustomerID: "smoi",
		KubeconfigRef: "kubeconfig_1", ContextName: "default",
		RequiresAccess: true, AccessProfileID: "vpn-cat",
	}, "https://172.16.20.65:6443")

	access := &stubAccess{installed: true, status: bctx.AccessStatus{
		ProfileID: "vpn-cat", State: bctx.AccessDisconnected,
	}}
	manager := NewManager(repo, stubResolver{}, kube.NewFactory("test"), access, &recorder{})

	if _, err := manager.Connect(context.Background(), cluster.ID); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if got := manager.Session(cluster.ID).State; got != domain.ClusterWaitingAccess {
		t.Fatalf("state = %q, want waiting_access", got)
	}

	if _, err := repo.AdoptAccessProfileID("vpn-cat", "812c795a"); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	// The notification names the identifier, which the waiting session has
	// never seen: it began waiting while the cluster still held the name.
	manager.RetryWaiting(context.Background(), "812c795a")

	asked := access.taken()
	if len(asked) < 2 {
		t.Fatalf("Biebie Access was asked %v, want a second question from the retry", asked)
	}
}

func (s *stubAccess) taken() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}
