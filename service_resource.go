package main

import (
	"context"

	"biebie-kube/internal/domain"
)

// ResourceService reads and edits Kubernetes objects.
type ResourceService struct{ core *Core }

func (s *ResourceService) ServiceName() string { return "ResourceService" }

// ListResources renders a resource table.
//
// kind crosses the binding as a plain string rather than domain.Kind, because
// the generated TypeScript cannot express a Go string alias and would emit a
// reference to a type it never declares.
func (s *ResourceService) ListResources(ctx context.Context, clusterID, kind, namespace string) (domain.ResourcePage, error) {
	page, err := s.core.resources.List(ctx, clusterID, domain.Kind(kind), namespace)
	return page, describe(err)
}

// WatchResources starts a watch for a table the user is looking at, so updates
// arrive as events instead of the frontend polling.
func (s *ResourceService) WatchResources(clusterID, kind, namespace string) error {
	return describe(s.core.resources.Watch(clusterID, domain.Kind(kind), namespace))
}

// CountResources reports which kinds have objects in the current namespace,
// so the sidebar can fade empty entries.
func (s *ResourceService) CountResources(ctx context.Context, clusterID, namespace string) ([]domain.KindPresence, error) {
	counts, err := s.core.resources.Counts(ctx, clusterID, namespace)
	return counts, describe(err)
}

// InspectResource returns the right-hand inspector payload for one object.
//
// ConfigMap and Secret data values are the stored encoding. Secret `data` is
// base64 and is never decoded here.
func (s *ResourceService) InspectResource(ctx context.Context, clusterID string, ref domain.ResourceRef) (domain.ResourceInspect, error) {
	inspect, err := s.core.resources.InspectResource(ctx, clusterID, ref)
	return inspect, describe(err)
}

// GetResourceYAML returns a resource as editable YAML.
func (s *ResourceService) GetResourceYAML(ctx context.Context, clusterID string, ref domain.ResourceRef) (string, error) {
	yaml, err := s.core.manifests.Read(ctx, clusterID, ref)
	return yaml, describe(err)
}

// YAMLDiff is the pair the editor compares before anything is applied.
type YAMLDiff struct {
	Current string `json:"current"`
	Edited  string `json:"edited"`
}

// DiffResourceYAML compares an edit with what the cluster currently holds.
func (s *ResourceService) DiffResourceYAML(ctx context.Context, clusterID string, ref domain.ResourceRef, edited string) (YAMLDiff, error) {
	current, normalised, err := s.core.manifests.Diff(ctx, clusterID, ref, edited)
	if err != nil {
		return YAMLDiff{}, describe(err)
	}
	return YAMLDiff{Current: current, Edited: normalised}, nil
}

// ApplyResourceYAML writes an edited manifest back to the cluster.
func (s *ResourceService) ApplyResourceYAML(ctx context.Context, clusterID string, ref domain.ResourceRef, edited string) (domain.ApplyResult, error) {
	result, err := s.core.manifests.Apply(ctx, clusterID, ref, edited)
	return result, describe(err)
}

// DeleteResource removes an object.
//
// The confirmation dialog that precedes this shows customer, environment and
// cluster, because the dangerous mistake is not deleting the wrong object — it
// is deleting the right object in the wrong customer's cluster.
func (s *ResourceService) DeleteResource(ctx context.Context, clusterID string, ref domain.ResourceRef) error {
	return describe(s.core.resources.Delete(ctx, clusterID, ref))
}

// GetPodDetail reads the overview tab of a pod.
func (s *ResourceService) GetPodDetail(ctx context.Context, clusterID, namespace, name string) (domain.PodDetail, error) {
	detail, err := s.core.resources.PodDetail(ctx, clusterID, namespace, name)
	return detail, describe(err)
}

// ListContainers returns a pod's containers for the log and terminal pickers.
func (s *ResourceService) ListContainers(ctx context.Context, clusterID, namespace, pod string) ([]domain.ContainerInfo, error) {
	containers, err := s.core.resources.Containers(ctx, clusterID, namespace, pod)
	return containers, describe(err)
}

// ListEvents returns events, optionally for one object.
func (s *ResourceService) ListEvents(ctx context.Context, clusterID, namespace, involving string) ([]domain.EventRow, error) {
	events, err := s.core.resources.Events(ctx, clusterID, namespace, involving)
	return events, describe(err)
}

// GetClusterOverview builds the cluster dashboard.
func (s *ResourceService) GetClusterOverview(ctx context.Context, clusterID string) (domain.ClusterOverview, error) {
	overview, err := s.core.resources.Overview(ctx, clusterID)
	return overview, describe(err)
}

// SearchResources looks for a name across the kinds engineers search by name.
func (s *ResourceService) SearchResources(ctx context.Context, clusterID, query, namespace string) ([]domain.SearchHit, error) {
	hits, err := s.core.resources.Search(ctx, clusterID, query, namespace)
	return hits, describe(err)
}
