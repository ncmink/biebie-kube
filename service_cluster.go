package main

import (
	"context"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"biebie-kube/internal/autoimport"
	"biebie-kube/internal/domain"
	"biebie-kube/internal/kubeconfig"
)

// ClusterService is the frontend's door to cluster configuration and the
// connection lifecycle.
type ClusterService struct{ core *Core }

// ServiceName is what appears in Wails logs and in the generated bindings
// directory, so it is the product's own name rather than the Go type's.
func (s *ClusterService) ServiceName() string { return "ClusterService" }

// ClusterView is a cluster plus its live session, which is what every screen
// needs together.
type ClusterView struct {
	Cluster domain.Cluster `json:"cluster"`
	Session domain.Session `json:"session"`
}

// ListClusters returns every cluster with its current state.
func (s *ClusterService) ListClusters() []ClusterView {
	clusters := s.core.clusters.Clusters()
	out := make([]ClusterView, 0, len(clusters))
	for _, cluster := range clusters {
		out = append(out, ClusterView{Cluster: cluster, Session: s.core.clusters.Session(cluster.ID)})
	}
	return out
}

// GetCluster returns one cluster with its state.
func (s *ClusterService) GetCluster(clusterID string) (ClusterView, error) {
	cluster, err := s.core.clusters.Cluster(clusterID)
	if err != nil {
		return ClusterView{}, describe(err)
	}
	return ClusterView{Cluster: cluster, Session: s.core.clusters.Session(clusterID)}, nil
}

// CreateCluster adds a cluster from a kubeconfig context.
//
// The API endpoint is read from the kubeconfig rather than typed by the user:
// it has to match what client-go will actually dial, or the reachability probe
// would test a different address from the one that fails.
func (s *ClusterService) CreateCluster(input domain.ClusterInput) (ClusterView, error) {
	server, err := s.serverFor(input)
	if err != nil {
		return ClusterView{}, describe(err)
	}
	cluster, err := s.core.clusters.Repository().Create(input, server)
	if err != nil {
		return ClusterView{}, describe(err)
	}
	return ClusterView{Cluster: cluster, Session: s.core.clusters.Session(cluster.ID)}, nil
}

// UpdateCluster edits a cluster.
func (s *ClusterService) UpdateCluster(clusterID string, input domain.ClusterInput) (ClusterView, error) {
	server, err := s.serverFor(input)
	if err != nil {
		return ClusterView{}, describe(err)
	}
	cluster, err := s.core.clusters.Repository().Update(clusterID, input, server)
	if err != nil {
		return ClusterView{}, describe(err)
	}
	return ClusterView{Cluster: cluster, Session: s.core.clusters.Session(cluster.ID)}, nil
}

// DeleteCluster forgets a cluster. The kubeconfig it referenced is untouched.
func (s *ClusterService) DeleteCluster(clusterID string) error {
	s.core.forwards.StopCluster(clusterID)
	s.core.clusters.Disconnect(clusterID)
	return describe(s.core.clusters.Repository().Delete(clusterID))
}

// ListCustomerGroups reports the customer sections of the cluster list, in the
// order they are shown, and which of them are hidden.
func (s *ClusterService) ListCustomerGroups() []domain.CustomerGroup {
	return s.core.clusters.Repository().Groups()
}

// SetCustomerGroupHidden hides or reveals one customer's clusters and returns
// the refreshed grouping.
//
// This is a change to the list, not to the clusters: nothing is deleted, an
// open session survives, and a cluster in a hidden group still connects when a
// handoff or the command palette asks for it.
func (s *ClusterService) SetCustomerGroupHidden(key string, hidden bool) ([]domain.CustomerGroup, error) {
	repo := s.core.clusters.Repository()
	if err := repo.SetGroupHidden(key, hidden); err != nil {
		return nil, describe(err)
	}
	return repo.Groups(), nil
}

// SetClusterArchived puts one cluster in the archive or takes it back out.
//
// The archive is the section of the list that is hidden unless asked for, so
// this is how a cluster is put out of the way without deleting it or losing
// which customer it belongs to. Like hiding a customer it changes the list
// only: an archived cluster still connects, and one already connected keeps its
// session and its tab.
func (s *ClusterService) SetClusterArchived(clusterID string, archived bool) (ClusterView, error) {
	cluster, err := s.core.clusters.Repository().SetArchived(clusterID, archived)
	if err != nil {
		return ClusterView{}, describe(err)
	}
	return ClusterView{Cluster: cluster, Session: s.core.clusters.Session(cluster.ID)}, nil
}

// ConnectCluster runs the connection sequence and returns the resulting state.
func (s *ClusterService) ConnectCluster(ctx context.Context, clusterID string) (domain.Session, error) {
	session, err := s.core.clusters.Connect(ctx, clusterID)
	return session, describe(err)
}

// DisconnectCluster ends a session and everything hanging off it.
func (s *ClusterService) DisconnectCluster(clusterID string) domain.Session {
	s.core.forwards.StopCluster(clusterID)
	return s.core.clusters.Disconnect(clusterID)
}

// GetSession reports one cluster's state.
func (s *ClusterService) GetSession(clusterID string) domain.Session {
	return s.core.clusters.Session(clusterID)
}

// ListSessions reports every cluster's state.
func (s *ClusterService) ListSessions() []domain.Session { return s.core.clusters.Sessions() }

// ListNamespaces returns the namespaces read when the cluster connected.
func (s *ClusterService) ListNamespaces(clusterID string) []string {
	return s.core.clusters.Namespaces(clusterID)
}

// SetNamespace changes the namespace in view and remembers it for next time.
func (s *ClusterService) SetNamespace(clusterID, namespace string) error {
	return describe(s.core.clusters.SetNamespace(clusterID, namespace))
}

// ListKubeconfigs returns the imported kubeconfigs and their contexts.
func (s *ClusterService) ListKubeconfigs() []kubeconfig.File { return s.core.configs.List() }

// ImportKubeconfig indexes a kubeconfig file.
func (s *ClusterService) ImportKubeconfig(opts kubeconfig.ImportOptions) (kubeconfig.File, error) {
	file, err := s.core.configs.Import(opts)
	return file, describe(err)
}

// ImportDefaultKubeconfig indexes ~/.kube/config, the file most engineers
// already have, so the common case needs no file picker.
func (s *ClusterService) ImportDefaultKubeconfig() (kubeconfig.File, error) {
	file, err := s.core.configs.ImportDefault()
	return file, describe(err)
}

// ChooseKubeconfig opens the native file picker and returns the chosen path.
// The frontend cannot read the filesystem, so the path must come from here.
func (s *ClusterService) ChooseKubeconfig() (string, error) {
	dialog := application.Get().Dialog.OpenFile()
	dialog.SetTitle("Select a kubeconfig")
	dialog.CanChooseFiles(true)
	dialog.AllowsOtherFileTypes(true)
	// A kubeconfig is often named plain "config" with no extension, so the
	// filter cannot be extension-only without hiding the usual file.
	dialog.AddFilter("Kubeconfig", "*.yaml;*.yml;*.conf;config")

	path, err := dialog.PromptForSingleSelection()
	return path, describe(err)
}

// ForgetKubeconfig removes a reference to a kubeconfig.
func (s *ClusterService) ForgetKubeconfig(ref string) error {
	return describe(s.core.configs.Forget(ref))
}

// ScanContexts lists kubeconfig contexts that are not clusters yet, with the
// name and environment an import would give them.
func (s *ClusterService) ScanContexts() []autoimport.Candidate {
	return s.core.imports.Scan()
}

// ImportAllContexts adds a cluster for every context that does not have one.
func (s *ClusterService) ImportAllContexts() autoimport.Result {
	return s.core.imports.ImportAll()
}

// AutoImportEnabled reports whether new contexts are added on their own.
func (s *ClusterService) AutoImportEnabled() bool { return s.core.imports.Enabled() }

// SetAutoImportEnabled turns automatic import on or off. Turning it off leaves
// the clusters already imported exactly where they are.
func (s *ClusterService) SetAutoImportEnabled(enabled bool) error {
	return describe(s.core.imports.SetEnabled(enabled))
}

// ResourceCatalogue returns the navigation tree, filtered to what this cluster
// actually serves so the sidebar does not offer kinds that will 404.
func (s *ClusterService) ResourceCatalogue(clusterID string) []domain.KindInfo {
	full := domain.Catalogue()
	served := s.core.clusters.APIResources(clusterID)
	if len(served) == 0 {
		return full
	}

	available := make(map[string]struct{}, len(served))
	for _, resource := range served {
		available[resource.Group+"/"+resource.Resource] = struct{}{}
	}

	out := make([]domain.KindInfo, 0, len(full))
	for _, info := range full {
		if _, ok := available[info.Group+"/"+info.Resource]; ok {
			out = append(out, info)
		}
	}
	return out
}

func (s *ClusterService) serverFor(input domain.ClusterInput) (string, error) {
	path, err := s.core.configs.PathFor(strings.TrimSpace(input.KubeconfigRef))
	if err != nil {
		return "", err
	}
	return kubeconfig.ServerFor(path, input.ContextName)
}
