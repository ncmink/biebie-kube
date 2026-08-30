package resources

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"biebie-kube/internal/cluster"
	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
)

// coldBudget bounds the first read of a resource type, before its watch has
// filled a cache.
//
// One request rather than a paginated crawl, because this is the read the user
// is waiting on. A cluster with more objects than this of one kind gets a page
// marked as still loading, and the watch corrects the counts within moments.
const coldBudget = 5000

// maxTables bounds how many rendered tables are kept per application.
//
// A table holds a rendered row for every object of its kind, which is worth
// keeping for the tab in front of the user and the few behind it, and not
// worth keeping for every kind they have ever clicked.
const maxTables = 6

// EventRows carries a patch to the table the frontend is holding.
const EventRows = "cluster:rows"

// Emitter publishes events to the UI. The service depends on this rather than
// on the Wails runtime, so rendering can be tested headlessly.
type Emitter interface {
	Emit(event string, data any)
}

// RowsChanged is what changed about the window the frontend holds.
//
// It is a patch rather than a table: a rollout in a large namespace changes a
// handful of rows, and re-sending every row three times a second is what makes
// a big cluster feel broken.
type RowsChanged struct {
	ClusterID string      `json:"clusterId"`
	Kind      domain.Kind `json:"kind"`
	Namespace string      `json:"namespace"`

	Upserts []domain.ResourceRow `json:"upserts,omitempty"`
	Removed []string             `json:"removed,omitempty"`
	Order   []string             `json:"order,omitempty"`

	Total   int  `json:"total"`
	Matched int  `json:"matched"`
	Loading bool `json:"loading"`

	// Token is the query this patch was computed against, so a table that has
	// since been rebuilt with a different filter or order can ignore it.
	Token string `json:"token"`
}

// Service reads and mutates Kubernetes objects for the UI.
type Service struct {
	clusters *cluster.Manager
	emitter  Emitter

	mu     sync.Mutex
	tables map[view]*table

	// touched is when each view was last asked for, and belongs to the
	// registry rather than to the table: eviction is a decision about the set
	// of tables, taken under the lock that owns that set.
	touched map[view]time.Time

	usage map[string]*usageState
}

// NewService wires the service to live cluster sessions.
func NewService(clusters *cluster.Manager, emitter Emitter) *Service {
	return &Service{
		clusters: clusters,
		emitter:  emitter,
		tables:   make(map[view]*table),
		touched:  make(map[view]time.Time),
		usage:    make(map[string]*usageState),
	}
}

// List answers one table query.
//
// The filter, the order and the window are all applied here, where the whole
// set is, rather than in the renderer. A filter applied to a window would only
// ever search the rows that happened to be sent, and would report a pod that
// exists as missing — which is the difference between a slow table and a table
// that cannot be trusted.
func (s *Service) List(ctx context.Context, clusterID string, kind domain.Kind, query domain.ListQuery) (domain.ResourcePage, error) {
	info, ok := s.clusters.LookupKind(clusterID, kind)
	if !ok {
		return domain.ResourcePage{}, fmt.Errorf("unknown resource type %q", kind)
	}
	if !info.Namespaced {
		query.Namespace = domain.AllNamespaces
	}

	key := view{clusterID: clusterID, kind: kind, namespace: query.Namespace}

	// An append is answered from the rows already rendered. Re-reading the
	// cluster to hand out the next five hundred of an order that has not
	// changed would undo the point of holding them.
	if query.Offset > 0 {
		if existing := s.table(key); existing != nil {
			return existing.page(query), nil
		}
	}

	objects, loading, err := s.read(ctx, clusterID, info, query.Namespace, true)
	if err != nil {
		return domain.ResourcePage{}, err
	}

	rendered := s.ensureTable(key, info)
	if kind == domain.KindPod {
		// Nothing to patch: the rows are about to be rendered again anyway,
		// and the page that follows carries the usage with them.
		rendered.setUsage(s.usageFor(ctx, clusterID, true))
	}
	rendered.replace(objects, loading)
	return rendered.page(query), nil
}

// Forget drops what was rendered for a cluster, when its session ends or is
// replaced. Rows rendered from a cache that no longer exists would be patched
// against nothing.
func (s *Service) Forget(clusterID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key := range s.tables {
		if key.clusterID == clusterID {
			delete(s.tables, key)
			delete(s.touched, key)
		}
	}
	delete(s.usage, clusterID)
}

// OnResourceChange turns a watch notification into a patch for every table it
// affects.
//
// This is the hot path of a busy cluster: it re-renders the objects that
// changed, compares the result with the window the frontend was given, and
// stays silent when nothing the user can see is different.
func (s *Service) OnResourceChange(clusterID string, change kube.Change) {
	affected := s.affected(clusterID, change)

	for key := range affected {
		// Usage ages out on metrics-server's schedule rather than the
		// cluster's, so a busy pod table takes the chance to check whether it
		// is due. The read happens in the background and patches the cells it
		// changes when it lands.
		if key.kind == domain.KindPod {
			s.usageFor(context.Background(), clusterID, false)
			break
		}
	}

	for key, rendered := range affected {
		touched, reordered, err := s.refresh(clusterID, key, rendered, change)
		if err != nil {
			continue
		}
		delta, worth := rendered.patch(touched, reordered)
		if !worth {
			continue
		}
		s.emit(EventRows, RowsChanged{
			ClusterID: clusterID,
			Kind:      key.kind,
			Namespace: key.namespace,
			Upserts:   delta.Upserts,
			Removed:   delta.Removed,
			Order:     delta.Order,
			Total:     delta.Total,
			Matched:   delta.Matched,
			Loading:   delta.Loading,
			Token:     delta.Token,
		})
	}
}

// affected finds the tables a watch notification speaks for.
//
// A watch over every namespace feeds any view of its kind; a watch over one
// feeds only the views showing it.
func (s *Service) affected(clusterID string, change kube.Change) map[view]*table {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make(map[view]*table)
	for key, rendered := range s.tables {
		if key.clusterID != clusterID {
			continue
		}
		info := rendered.info
		if kube.GVRFor(info.Group, info.Version, info.Resource) != change.GVR {
			continue
		}
		if change.Namespace != domain.AllNamespaces && change.Namespace != key.namespace {
			continue
		}
		out[key] = rendered
	}
	return out
}

// refresh brings one table up to date with the cache behind it, reporting the
// keys it touched and whether the order can still stand.
func (s *Service) refresh(clusterID string, key view, rendered *table, change kube.Change) ([]string, bool, error) {
	hub, err := s.clusters.Hub(clusterID)
	if err != nil {
		return nil, false, err
	}
	info := rendered.info
	gvr := kube.GVRFor(info.Group, info.Version, info.Resource)

	watch := hub.Existing(gvr, key.namespace)
	if watch == nil {
		return nil, false, fmt.Errorf("no warm cache for %s", info.Resource)
	}

	// A resync, or a burst too large to track object by object, is cheaper to
	// answer by re-rendering the cache than by naming every key in it.
	if change.Full {
		cached, err := watch.List(key.namespace)
		if err != nil {
			return nil, false, err
		}
		objects := toUnstructured(cached)
		rendered.replace(objects, false)
		return keysIn(objects), true, nil
	}

	changed := make([]*unstructured.Unstructured, 0, len(change.Keys))
	removed := make([]string, 0, len(change.Keys))
	for _, objectKey := range change.Keys {
		object, found := watch.Get(objectKey)
		if !found {
			removed = append(removed, objectKey)
			continue
		}
		if typed, ok := object.(*unstructured.Unstructured); ok {
			changed = append(changed, typed)
		}
	}
	touched, reordered := rendered.apply(changed, removed)
	return touched, reordered, nil
}

// read returns objects from the informer cache when it is warm, and from one
// bounded API request otherwise.
//
// subscribe starts a watch on the way past, which is what a table wants: its
// next update should arrive as an event rather than a poll. A search does not
// want it — scanning eleven kinds would start eleven informers and evict the
// cache belonging to the table the engineer is actually looking at.
//
// The second return value reports that more objects exist than were read, so
// the page can say the counts are still settling instead of presenting a
// prefix as the whole list.
func (s *Service) read(
	ctx context.Context,
	clusterID string,
	info domain.KindInfo,
	namespace string,
	subscribe bool,
) ([]*unstructured.Unstructured, bool, error) {
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return nil, false, err
	}
	gvr := kube.GVRFor(info.Group, info.Version, info.Resource)

	hub, hubErr := s.clusters.Hub(clusterID)
	if hubErr == nil {
		if watch := hub.Existing(gvr, namespace); watch != nil {
			cached, err := watch.List(namespace)
			if err == nil {
				return toUnstructured(cached), false, nil
			}
		}
	}

	options := metav1.ListOptions{Limit: coldBudget}
	var list *unstructured.UnstructuredList
	if namespace == domain.AllNamespaces {
		list, err = client.Dynamic.Resource(gvr).List(ctx, options)
	} else {
		list, err = client.Dynamic.Resource(gvr).Namespace(namespace).List(ctx, options)
	}
	if err != nil {
		return nil, false, fmt.Errorf("list %s: %w", gvr.Resource, err)
	}

	if subscribe && hubErr == nil {
		hub.Ensure(gvr, namespace)
	}

	out := make([]*unstructured.Unstructured, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, &list.Items[i])
	}
	return out, list.GetContinue() != "", nil
}

// readMatching returns the objects of one kind in a namespace whose labels a
// selector accepts.
//
// It exists for the related lists, where what an object owns is settled by UID
// after the read. The selector is only what lets the API server hand over the
// handful of pods a deployment could own rather than the five thousand in its
// namespace, so narrowing it wrongly — or not at all — costs a larger read and
// never a wrong answer. A selector that matches everything is the same request
// as no selector, and is sent as one.
//
// The limit stays the cold budget rather than the group's: a selector that
// turned out not to narrow would otherwise return the first two hundred pods
// in the namespace, which the ownership test would reject to nothing.
func (s *Service) readMatching(
	ctx context.Context,
	clusterID string,
	info domain.KindInfo,
	namespace string,
	selector labels.Selector,
) ([]*unstructured.Unstructured, bool, error) {
	if selector == nil || selector.Empty() {
		return s.read(ctx, clusterID, info, namespace, false)
	}

	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return nil, false, err
	}
	gvr := kube.GVRFor(info.Group, info.Version, info.Resource)

	// A warm cache already holds the namespace, so it is filtered here rather
	// than asked for a second time with the selector attached.
	if hub, hubErr := s.clusters.Hub(clusterID); hubErr == nil {
		if watch := hub.Existing(gvr, namespace); watch != nil {
			if cached, err := watch.List(namespace); err == nil {
				return matching(toUnstructured(cached), selector), false, nil
			}
		}
	}

	list, err := client.Dynamic.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
		Limit:         coldBudget,
	})
	if err != nil {
		return nil, false, fmt.Errorf("list %s: %w", gvr.Resource, err)
	}

	out := make([]*unstructured.Unstructured, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, &list.Items[i])
	}
	return out, list.GetContinue() != "", nil
}

func matching(objects []*unstructured.Unstructured, selector labels.Selector) []*unstructured.Unstructured {
	out := make([]*unstructured.Unstructured, 0, len(objects))
	for _, object := range objects {
		if selector.Matches(labels.Set(object.GetLabels())) {
			out = append(out, object)
		}
	}
	return out
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
// Names are matched on the objects themselves and only the hits are rendered,
// so searching a cluster with fifty thousand pods costs a scan of their
// metadata rather than a rendered table per kind.
func (s *Service) Search(ctx context.Context, clusterID, query, namespace string) ([]domain.SearchHit, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	if len(needle) < 2 {
		return nil, nil
	}

	var hits []domain.SearchHit
	for _, kind := range domain.SearchableKinds() {
		info, ok := s.clusters.LookupKind(clusterID, kind)
		if !ok {
			continue
		}
		scope := namespace
		if !info.Namespaced {
			scope = domain.AllNamespaces
		}
		objects, _, err := s.read(ctx, clusterID, info, scope, false)
		if err != nil {
			// A kind this account cannot list is skipped rather than failing
			// the whole search.
			continue
		}
		for _, object := range objects {
			if !strings.Contains(strings.ToLower(object.GetName()), needle) {
				continue
			}
			row := Row(info, object)
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

// table returns a rendered table, or nil.
func (s *Service) table(key view) *table {
	s.mu.Lock()
	defer s.mu.Unlock()

	rendered, ok := s.tables[key]
	if !ok {
		return nil
	}
	s.touched[key] = time.Now()
	return rendered
}

// ensureTable returns the table for a view, creating it and evicting the least
// recently used one when the budget is full.
func (s *Service) ensureTable(key view, info domain.KindInfo) *table {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.touched[key] = time.Now()

	if existing, ok := s.tables[key]; ok {
		// A kind's columns come from the cluster for a custom resource, so a
		// reconnect can change them under a view that is still open.
		existing.info = info
		return existing
	}

	for len(s.tables) >= maxTables {
		oldest, found := view{}, false
		for candidate := range s.tables {
			if !found || s.touched[candidate].Before(s.touched[oldest]) {
				oldest, found = candidate, true
			}
		}
		if !found {
			break
		}
		delete(s.tables, oldest)
		delete(s.touched, oldest)
	}

	created := newTable(info)
	s.tables[key] = created
	return created
}

func (s *Service) emit(event string, data any) {
	if s.emitter != nil {
		s.emitter.Emit(event, data)
	}
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

func keysIn(objects []*unstructured.Unstructured) []string {
	keys := make([]string, len(objects))
	for i, object := range objects {
		keys[i] = RowKey(object.GetNamespace(), object.GetName())
	}
	return keys
}
