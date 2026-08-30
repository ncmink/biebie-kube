package argocd

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"biebie-kube/internal/domain"
)

// refreshAnnotation is the request Argo CD watches for. The controller clears
// it once the refresh completes, which is also how a stuck request shows up.
const refreshAnnotation = "argocd.argoproj.io/refresh"

// initiator is recorded on every sync this application requests, so an
// engineer reading an Application's history afterwards can tell a sync from
// Biebie Kube apart from one somebody ran with the Argo CD CLI.
const initiator = "biebie-kube"

// Sync asks Argo CD to bring Applications in line with their target revision.
//
// The request is recorded on the Application rather than sent to Argo CD's own
// API server: the application controller is what performs a sync, it is
// already watching these objects, and going through it means this works
// against a cluster whose Argo CD server is not reachable from this machine.
func (s *Service) Sync(ctx context.Context, clusterID string, req domain.ArgoSyncRequest) (domain.ArgoActionResult, error) {
	sync := map[string]any{"prune": req.Prune, "syncStrategy": map[string]any{"hook": map[string]any{}}}
	patch, err := json.Marshal(map[string]any{
		"operation": map[string]any{
			"initiatedBy": map[string]any{"username": initiator},
			"info":        []any{map[string]any{"name": "Reason", "value": "Requested from Biebie Kube"}},
			"sync":        sync,
		},
	})
	if err != nil {
		return domain.ArgoActionResult{}, err
	}
	return s.patchEach(ctx, clusterID, req.Apps, patch)
}

// Refresh asks Argo CD to compare Applications against Git again, without
// waiting for the next polling interval.
func (s *Service) Refresh(ctx context.Context, clusterID string, req domain.ArgoRefreshRequest) (domain.ArgoActionResult, error) {
	kind := "normal"
	if req.Hard {
		kind = "hard"
	}
	patch, err := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]any{refreshAnnotation: kind},
		},
	})
	if err != nil {
		return domain.ArgoActionResult{}, err
	}
	return s.patchEach(ctx, clusterID, req.Apps, patch)
}

// patchEach applies one patch to every named Application and reports both
// halves of the outcome.
//
// A batch is not abandoned at the first failure. An engineer syncing forty
// Applications where two are governed by a project they may not touch wants
// the thirty-eight, and wants to be told about the two.
func (s *Service) patchEach(
	ctx context.Context,
	clusterID string,
	apps []domain.ArgoAppRef,
	patch []byte,
) (domain.ArgoActionResult, error) {
	if len(apps) == 0 {
		return domain.ArgoActionResult{}, fmt.Errorf("no applications were selected")
	}
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return domain.ArgoActionResult{}, err
	}

	var result domain.ArgoActionResult
	for _, app := range apps {
		name := app.Namespace + "/" + app.Name
		_, err := client.Dynamic.
			Resource(applicationGVR).
			Namespace(app.Namespace).
			Patch(ctx, app.Name, types.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil {
			result.Failed = append(result.Failed, domain.ArgoActionFailed{App: name, Error: err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, name)
	}
	return result, nil
}
