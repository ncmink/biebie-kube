package main

import (
	"context"

	"biebie-kube/internal/domain"
)

// ArgoCDService reads the Argo CD installation in a cluster and runs its two
// actions.
type ArgoCDService struct{ core *Core }

func (s *ArgoCDService) ServiceName() string { return "ArgoCDService" }

// ArgoCDInstalled reports whether the cluster serves the Application
// definition, which is what decides whether the sidebar offers Argo CD at all.
func (s *ArgoCDService) ArgoCDInstalled(clusterID string) bool {
	return s.core.argocd.Installed(clusterID)
}

// GetArgoDashboard builds the Argo CD dashboard.
func (s *ArgoCDService) GetArgoDashboard(ctx context.Context, clusterID string) (domain.ArgoDashboard, error) {
	dashboard, err := s.core.argocd.Dashboard(ctx, clusterID)
	return dashboard, describe(err)
}

// ListArgoApplications returns every Application, for the sync and refresh
// selection dialogs.
func (s *ArgoCDService) ListArgoApplications(ctx context.Context, clusterID string) ([]domain.ArgoApp, error) {
	apps, err := s.core.argocd.Applications(ctx, clusterID)
	return apps, describe(err)
}

// ResolveGitOwnership answers where one live object's desired state lives.
//
// It is the bridge between the two halves of the product: an object a
// repository declares is changed in that repository, and an object nothing
// declares is operated on directly. The answer carries how it was reached, so
// the UI can show a guess as a guess.
func (s *ArgoCDService) ResolveGitOwnership(ctx context.Context, clusterID string, ref domain.ResourceRef) (domain.ResourceOwnership, error) {
	ownership, err := s.core.argocd.Ownership(ctx, clusterID, ref)
	return ownership, describe(err)
}

// SyncArgoApplications records a sync request on each Application.
//
// Prune is carried on the request rather than defaulted here, because it
// deletes live resources and the dialog that sets it is where the engineer was
// told so.
func (s *ArgoCDService) SyncArgoApplications(ctx context.Context, clusterID string, req domain.ArgoSyncRequest) (domain.ArgoActionResult, error) {
	result, err := s.core.argocd.Sync(ctx, clusterID, req)
	return result, describe(err)
}

// RefreshArgoApplications asks Argo CD to compare each Application against Git
// again.
func (s *ArgoCDService) RefreshArgoApplications(ctx context.Context, clusterID string, req domain.ArgoRefreshRequest) (domain.ArgoActionResult, error) {
	result, err := s.core.argocd.Refresh(ctx, clusterID, req)
	return result, describe(err)
}

// OpenArgoUI returns a loopback URL that reaches the Argo CD web UI, starting
// or reusing a port forward to the server Service.
func (s *ArgoCDService) OpenArgoUI(ctx context.Context, clusterID string) (domain.ArgoEndpoint, error) {
	endpoint, err := s.core.argocd.OpenUI(ctx, clusterID)
	return endpoint, describe(err)
}
