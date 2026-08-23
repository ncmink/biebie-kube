package resources

import (
	"context"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"biebie-kube/internal/cluster"
	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
)

// maxRows bounds one table render.
//
// A cluster with fifty thousand pods must not stall the renderer or the IPC
// bridge. The table says it was truncated rather than silently showing part of
// the truth.
const maxRows = 2000

// Service reads and mutates Kubernetes objects for the UI.
type Service struct {
	clusters *cluster.Manager
}

// NewService wires the service to live cluster sessions.
func NewService(clusters *cluster.Manager) *Service {
	return &Service{clusters: clusters}
}

// List renders a resource table.
//
// It prefers a warm informer cache and falls back to a direct API list, so the
// first view of a resource type appears immediately while the watch is still
// syncing, and later views cost nothing.
func (s *Service) List(ctx context.Context, clusterID string, kind domain.Kind, namespace string) (domain.ResourcePage, error) {
	info, ok := s.clusters.LookupKind(clusterID, kind)
	if !ok {
		return domain.ResourcePage{}, fmt.Errorf("unknown resource type %q", kind)
	}
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return domain.ResourcePage{}, err
	}
	if !info.Namespaced {
		namespace = domain.AllNamespaces
	}

	gvr := kube.GVRFor(info.Group, info.Version, info.Resource)

	objects, err := s.read(ctx, clusterID, client, gvr, namespace)
	if err != nil {
		return domain.ResourcePage{}, err
	}

	page := domain.ResourcePage{
		Kind:       kind,
		Columns:    info.Columns,
		Namespaced: info.Namespaced,
		Rows:       make([]domain.ResourceRow, 0, len(objects)),
	}
	for _, obj := range objects {
		page.Rows = append(page.Rows, Row(info, obj))
	}

	// Newest first: an engineer opening a pod list is almost always looking
	// for what just changed.
	sort.SliceStable(page.Rows, func(i, j int) bool {
		if page.Rows[i].CreatedAt.Equal(page.Rows[j].CreatedAt) {
			return page.Rows[i].Name < page.Rows[j].Name
		}
		return page.Rows[i].CreatedAt.After(page.Rows[j].CreatedAt)
	})

	if len(page.Rows) > maxRows {
		page.Rows = page.Rows[:maxRows]
		page.Truncated = true
	}
	return page, nil
}

// read returns objects from the informer cache when it is warm, and from the
// API server otherwise. Either way a watch is started, so the next change
// arrives as an event instead of a poll.
func (s *Service) read(
	ctx context.Context,
	clusterID string,
	client *kube.ClusterClient,
	gvr schema.GroupVersionResource,
	namespace string,
) ([]*unstructured.Unstructured, error) {
	hub, hubErr := s.clusters.Hub(clusterID)

	if hubErr == nil {
		if watch := hub.Existing(gvr, namespace); watch != nil {
			cached, err := watch.List(namespace)
			if err == nil {
				return toUnstructured(cached), nil
			}
		}
	}

	var list *unstructured.UnstructuredList
	var err error
	if namespace == domain.AllNamespaces {
		list, err = client.Dynamic.Resource(gvr).List(ctx, metav1.ListOptions{})
	} else {
		list, err = client.Dynamic.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", gvr.Resource, err)
	}

	if hubErr == nil {
		hub.Ensure(gvr, namespace)
	}

	out := make([]*unstructured.Unstructured, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, &list.Items[i])
	}
	return out, nil
}

// Get returns one object in full, for the detail and YAML views.
func (s *Service) Get(ctx context.Context, clusterID string, ref domain.ResourceRef) (*unstructured.Unstructured, error) {
	info, ok := s.clusters.LookupKind(clusterID, ref.Kind)
	if !ok {
		return nil, fmt.Errorf("unknown resource type %q", ref.Kind)
	}
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return nil, err
	}

	gvr := kube.GVRFor(info.Group, info.Version, info.Resource)
	if info.Namespaced {
		return client.Dynamic.Resource(gvr).Namespace(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	}
	return client.Dynamic.Resource(gvr).Get(ctx, ref.Name, metav1.GetOptions{})
}

// Delete removes an object.
//
// Confirmation is the UI's responsibility, and it shows customer, environment
// and cluster before it calls this: deleting the right deployment in the wrong
// customer's production cluster is the failure this product exists to prevent.
func (s *Service) Delete(ctx context.Context, clusterID string, ref domain.ResourceRef) error {
	info, ok := s.clusters.LookupKind(clusterID, ref.Kind)
	if !ok {
		return fmt.Errorf("unknown resource type %q", ref.Kind)
	}
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return err
	}

	gvr := kube.GVRFor(info.Group, info.Version, info.Resource)
	if info.Namespaced {
		return client.Dynamic.Resource(gvr).Namespace(ref.Namespace).Delete(ctx, ref.Name, metav1.DeleteOptions{})
	}
	return client.Dynamic.Resource(gvr).Delete(ctx, ref.Name, metav1.DeleteOptions{})
}

// Search looks for a name fragment across the kinds engineers actually search.
//
// Each kind is read from the cache when warm; a cluster-wide search is
// deliberately limited to a handful of kinds so it stays instant.
func (s *Service) Search(ctx context.Context, clusterID, query, namespace string) ([]domain.SearchHit, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if len(query) < 2 {
		return nil, nil
	}

	var hits []domain.SearchHit
	for _, kind := range domain.SearchableKinds() {
		info, ok := s.clusters.LookupKind(clusterID, kind)
		if !ok {
			continue
		}
		page, err := s.List(ctx, clusterID, kind, namespace)
		if err != nil {
			// A kind this account cannot list is skipped rather than failing
			// the whole search.
			continue
		}
		for _, row := range page.Rows {
			if !strings.Contains(strings.ToLower(row.Name), query) {
				continue
			}
			hits = append(hits, domain.SearchHit{
				Kind:      kind,
				KindTitle: info.Title,
				Name:      row.Name,
				Namespace: row.Namespace,
				Health:    row.Health,
			})
			if len(hits) >= 50 {
				return hits, nil
			}
		}
	}
	return hits, nil
}

// Watch starts a watch for a table the user is looking at, so changes arrive
// as events rather than by polling.
func (s *Service) Watch(clusterID string, kind domain.Kind, namespace string) error {
	info, ok := s.clusters.LookupKind(clusterID, kind)
	if !ok {
		return fmt.Errorf("unknown resource type %q", kind)
	}
	hub, err := s.clusters.Hub(clusterID)
	if err != nil {
		return err
	}
	if !info.Namespaced {
		namespace = domain.AllNamespaces
	}
	hub.Ensure(kube.GVRFor(info.Group, info.Version, info.Resource), namespace)
	return nil
}

func toUnstructured(objects []runtime.Object) []*unstructured.Unstructured {
	out := make([]*unstructured.Unstructured, 0, len(objects))
	for _, obj := range objects {
		if typed, ok := obj.(*unstructured.Unstructured); ok {
			out = append(out, typed)
		}
	}
	return out
}
