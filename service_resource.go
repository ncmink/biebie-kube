package main

import (
	"context"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/manifest"
)

// ResourceService reads and edits Kubernetes objects.
type ResourceService struct{ core *Core }

func (s *ResourceService) ServiceName() string { return "ResourceService" }

// ListResources answers one table query: a filtered, sorted window of a kind.
//
// Listing starts the watch behind it, so the frontend does not ask for updates
// separately — the first read of a table is also the subscription to it.
//
// kind crosses the binding as a plain string rather than domain.Kind, because
// the generated TypeScript cannot express a Go string alias and would emit a
// reference to a type it never declares.
func (s *ResourceService) ListResources(ctx context.Context, clusterID, kind string, query domain.ListQuery) (domain.ResourcePage, error) {
	page, err := s.core.resources.List(ctx, clusterID, domain.Kind(kind), query)
	return page, describe(err)
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

// ListRelatedResources returns the objects that belong to one object: the pods
// a deployment runs, the revisions behind it, the workload a pod came from.
//
// Separate from InspectResource rather than folded into it, because it costs a
// list where the inspector costs a get: the properties should paint as soon as
// the object is read instead of waiting on a namespace of pods.
func (s *ResourceService) ListRelatedResources(ctx context.Context, clusterID string, ref domain.ResourceRef) ([]domain.RelatedGroup, error) {
	groups, err := s.core.resources.Related(ctx, clusterID, ref)
	return groups, describe(err)
}

// GetResourceYAML returns a resource as editable YAML.
//
// A read on its own, for the places that want to look at a manifest without
// opening an editing session against it.
func (s *ResourceService) GetResourceYAML(ctx context.Context, clusterID string, ref domain.ResourceRef) (string, error) {
	yaml, err := s.core.manifests.Read(ctx, clusterID, ref)
	return yaml, describe(err)
}

// OpenResourceEditor captures the object an editor will be compared against.
//
// The snapshot it returns is Original for the whole life of that editor. The
// frontend holds it and never asks for it again: a second read would be a
// second moment, and the editor would then be showing a difference against
// something other than what it opened with.
func (s *ResourceService) OpenResourceEditor(ctx context.Context, clusterID string, ref domain.ResourceRef) (domain.EditSession, error) {
	session, err := s.core.manifests.OpenSession(ctx, clusterID, ref)
	return session, describe(err)
}

// CompareResourceEdit holds an editor's original snapshot against its current
// text.
//
// It touches no cluster and is the same function the tests exercise. Whether a
// session is dirty, and whether its edits would actually change the object, are
// Kubernetes-adjacent judgements, and judgements in a Vue component are
// judgements nobody can test.
func (s *ResourceService) CompareResourceEdit(original, edited string) domain.EditComparison {
	return manifest.CompareEdit(original, edited)
}

// CheckResourceFreshness reports whether the live object moved since an editor
// opened at the given version.
//
// Asked before a write rather than watched during one, so the answer arrives
// as a choice the person makes rather than as an editor that rewrote itself
// while they were typing.
func (s *ResourceService) CheckResourceFreshness(
	ctx context.Context,
	clusterID string,
	ref domain.ResourceRef,
	resourceVersion string,
) (domain.EditFreshness, error) {
	freshness, err := s.core.manifests.Freshness(ctx, clusterID, ref, resourceVersion)
	return freshness, describe(err)
}

// ApplyResourceYAML writes an edited manifest back to the cluster.
//
// resourceVersion is the one the editor opened at, not the one the cluster
// holds now. It is what the API server checks the write against, and sending
// the current version instead would be a concurrency guard that can never fire.
func (s *ResourceService) ApplyResourceYAML(
	ctx context.Context,
	clusterID string,
	ref domain.ResourceRef,
	edited string,
	resourceVersion string,
) (domain.ApplyResult, error) {
	result, err := s.core.manifests.Apply(ctx, clusterID, ref, edited, resourceVersion)
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

// PerformResourceAction scales, restarts, cordons, suspends or triggers one
// object.
//
// These are guarded the same way a delete is, and for the same reason: the
// mistake worth preventing is not scaling the wrong deployment, it is scaling
// the right one in the wrong customer's cluster.
//
// They are deliberately not gated on GitOps ownership, which the YAML editor
// and Create are. These are operational verbs: they belong to the moment, and
// an object being managed from a repository is not a reason to be unable to
// restart it during an incident. The uncomfortable member of the list is
// scale, which writes a field a repository may declare and which Argo CD will
// take back on the next reconcile — that is a fix for the next ten minutes,
// and it is treated as one rather than blocked.

func (s *ResourceService) PerformResourceAction(
	ctx context.Context,
	clusterID string,
	request domain.ActionRequest,
) (domain.ActionResult, error) {
	result, err := s.core.resources.Perform(ctx, clusterID, request)
	return result, describe(err)
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
