package autoimport

import (
	"path/filepath"
	"testing"

	bctx "github.com/ncmink/biebie-protocol/context"

	"biebie-kube/internal/cluster"
	"biebie-kube/internal/kubeconfig"
	"biebie-kube/internal/store"
)

type fakeConfigs struct{ files []kubeconfig.File }

func (f fakeConfigs) List() []kubeconfig.File { return f.files }

func newService(t *testing.T, files ...kubeconfig.File) (*Service, *cluster.Repository) {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	clusters := cluster.NewRepository(st)
	return NewService(fakeConfigs{files: files}, clusters, st), clusters
}

func kubeconfigFile(ref string, contexts ...string) kubeconfig.File {
	file := kubeconfig.File{Ref: ref, Name: ref}
	for _, name := range contexts {
		file.Contexts = append(file.Contexts, kubeconfig.ContextEntry{
			Name:    name,
			Cluster: name,
			Server:  "https://" + name + ".example:6443",
		})
	}
	return file
}

func TestSyncAddsAClusterPerContext(t *testing.T) {
	service, clusters := newService(t, kubeconfigFile("default", "acme-prod", "docker-desktop"))

	result := service.Sync()
	if len(result.Added) != 2 {
		t.Fatalf("added %d clusters, want 2 (failures: %v)", len(result.Added), result.Failed)
	}

	stored := clusters.All()
	if len(stored) != 2 {
		t.Fatalf("stored %d clusters, want 2", len(stored))
	}
	for _, cluster := range stored {
		if cluster.ContextName != cluster.Name {
			t.Errorf("cluster %q was not named after its context %q", cluster.Name, cluster.ContextName)
		}
		if cluster.Server == "" {
			t.Errorf("cluster %q has no API endpoint, so a failed connection could not be diagnosed", cluster.Name)
		}
		if cluster.Access.Required {
			t.Errorf("cluster %q was imported as needing a VPN, which a kubeconfig cannot know", cluster.Name)
		}
	}
}

// The whole point of recording seen contexts: an engineer who removes an
// auto-imported cluster must not find it back after the next launch.
func TestSyncDoesNotRecreateADeletedCluster(t *testing.T) {
	service, clusters := newService(t, kubeconfigFile("default", "acme-prod", "acme-dev"))

	if result := service.Sync(); len(result.Added) != 2 {
		t.Fatalf("first sync added %d clusters, want 2", len(result.Added))
	}

	stored := clusters.All()
	if err := clusters.Delete(stored[0].ID); err != nil {
		t.Fatalf("delete cluster: %v", err)
	}

	if result := service.Sync(); len(result.Added) != 0 {
		t.Errorf("second sync added %d clusters, want none", len(result.Added))
	}
	if remaining := clusters.All(); len(remaining) != 1 {
		t.Errorf("cluster count is %d, want the one that was not deleted", len(remaining))
	}
}

func TestSyncAddsOnlyContextsThatAreNewSinceLastTime(t *testing.T) {
	service, clusters := newService(t, kubeconfigFile("default", "acme-dev"))
	if result := service.Sync(); len(result.Added) != 1 {
		t.Fatalf("first sync added %d clusters, want 1", len(result.Added))
	}

	// A context added with kubectl between launches is the case this feature
	// exists to catch.
	service.configs = fakeConfigs{files: []kubeconfig.File{
		kubeconfigFile("default", "acme-dev", "acme-staging"),
	}}

	result := service.Sync()
	if len(result.Added) != 1 {
		t.Fatalf("second sync added %d clusters, want 1", len(result.Added))
	}
	if result.Added[0].ContextName != "acme-staging" {
		t.Errorf("second sync added %q, want acme-staging", result.Added[0].ContextName)
	}
	if len(clusters.All()) != 2 {
		t.Errorf("stored %d clusters, want 2", len(clusters.All()))
	}
}

func TestSyncDoesNothingWhenTurnedOff(t *testing.T) {
	service, clusters := newService(t, kubeconfigFile("default", "acme-dev"))

	if err := service.SetEnabled(false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if service.Enabled() {
		t.Fatal("Enabled reports true after being switched off")
	}

	if result := service.Sync(); len(result.Added) != 0 {
		t.Errorf("sync added %d clusters while switched off", len(result.Added))
	}
	if len(clusters.All()) != 0 {
		t.Error("clusters were created while automatic import was switched off")
	}

	// The contexts must remain available, so switching the setting back on
	// still has something to find.
	if candidates := service.Scan(); len(candidates) != 1 {
		t.Errorf("scan found %d candidates, want 1", len(candidates))
	}
}

func TestEnabledByDefault(t *testing.T) {
	service, _ := newService(t)
	if !service.Enabled() {
		t.Error("a fresh install has automatic import switched off")
	}
}

// Asking for an import by hand is an explicit instruction, so it overrides the
// record that says a context was already offered once.
func TestImportAllReturnsToContextsAlreadyOffered(t *testing.T) {
	service, clusters := newService(t, kubeconfigFile("default", "acme-dev"))

	service.Sync()
	stored := clusters.All()
	if err := clusters.Delete(stored[0].ID); err != nil {
		t.Fatalf("delete cluster: %v", err)
	}

	result := service.ImportAll()
	if len(result.Added) != 1 {
		t.Fatalf("ImportAll added %d clusters, want 1 (failures: %v)", len(result.Added), result.Failed)
	}
}

func TestScanShowsWhatAnImportWouldCreate(t *testing.T) {
	service, _ := newService(t, kubeconfigFile("default", "acme-prod"))

	candidates := service.Scan()
	if len(candidates) != 1 {
		t.Fatalf("scan found %d candidates, want 1", len(candidates))
	}
	candidate := candidates[0]
	if candidate.Name != "acme-prod" {
		t.Errorf("candidate name is %q, want acme-prod", candidate.Name)
	}
	if candidate.EnvironmentKind != bctx.EnvironmentProduction {
		t.Errorf("candidate environment is %q, want production", candidate.EnvironmentKind)
	}
	if candidate.Seen {
		t.Error("a context that has never been imported is marked as seen")
	}

	service.Sync()
	if remaining := service.Scan(); len(remaining) != 0 {
		t.Errorf("scan still offers %d candidates after importing them", len(remaining))
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name  string
		want  bctx.Environment
		input []string
	}{
		{"plain production", bctx.EnvironmentProduction, []string{"acme-prod"}},
		{"spelled out", bctx.EnvironmentProduction, []string{"acme-production-eu"}},
		{"abbreviated", bctx.EnvironmentProduction, []string{"acme_prd"}},
		{"staging", bctx.EnvironmentStaging, []string{"acme-staging"}},
		{"uat", bctx.EnvironmentStaging, []string{"acme-uat-1"}},
		{"development", bctx.EnvironmentDevelopment, []string{"acme-dev"}},
		{"minikube", bctx.EnvironmentDevelopment, []string{"minikube"}},
		{"kind", bctx.EnvironmentDevelopment, []string{"kind-playground"}},
		{"docker desktop", bctx.EnvironmentDevelopment, []string{"docker-desktop"}},

		// Pre-production contains the production marker but means the opposite,
		// and non-production says only what a cluster is not.
		{"pre-production", bctx.EnvironmentStaging, []string{"acme-pre-prod"}},
		{"preprod squashed", bctx.EnvironmentStaging, []string{"acmepreprod"}},
		{"non-production", bctx.EnvironmentUnknown, []string{"acme-non-prod"}},

		// Whole words only, or a customer's name would classify their cluster.
		{"customer named Devon", bctx.EnvironmentUnknown, []string{"devon-cluster"}},
		{"word containing prod", bctx.EnvironmentUnknown, []string{"reproduction-eu"}},

		{"nothing to go on", bctx.EnvironmentUnknown, []string{"cluster-17"}},
		{"empty", bctx.EnvironmentUnknown, []string{""}},

		// The context name is asked first, then the cluster name, then the
		// endpoint, which is often the only place the environment appears.
		{"endpoint decides", bctx.EnvironmentProduction, []string{"cluster-17", "c17", "https://api.prod.acme.com"}},
		{"context wins over endpoint", bctx.EnvironmentDevelopment, []string{"acme-dev", "c17", "https://api.prod.acme.com"}},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := Classify(test.input...); got != test.want {
				t.Errorf("Classify(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}
