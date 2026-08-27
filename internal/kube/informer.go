package kube

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

// resyncPeriod is how often informers reconcile against a full list.
//
// Watches are the mechanism; a slow resync only repairs the rare case of a
// missed event, so it is deliberately long. Polling every few seconds is
// exactly what this architecture exists to avoid.
const resyncPeriod = 10 * time.Minute

// notifyDebounce batches change notifications.
//
// A deployment rollout in a large namespace produces hundreds of events in a
// second. Forwarding each one would repaint the table continuously and starve
// the renderer, so changes are coalesced into one notification per window.
const notifyDebounce = 300 * time.Millisecond

// maxPendingKeys bounds how many changed objects one notification names.
//
// Naming them lets a reader re-render only what moved. Past this many it is
// cheaper to re-read the cache than to carry the list, and a resync — which
// touches every object — would otherwise build a set the size of the cluster.
const maxPendingKeys = 256

// Watch budget for one cluster session.
//
// Every informer holds a full copy of its resource type, so browsing thirty
// kinds must not leave thirty caches resident for the rest of the session.
// Ones nobody has looked at for a while are stopped, and the cap keeps a
// deliberate tour of the sidebar from outgrowing it either way.
const (
	watchIdleTimeout = 5 * time.Minute
	watchSweepPeriod = time.Minute
	maxWatches       = 16
)

// Change is what a watch saw, coalesced over the debounce window.
type Change struct {
	GVR       schema.GroupVersionResource
	Namespace string

	// Keys name the objects that changed, as client-go's cache keys them:
	// "namespace/name", or the bare name for a cluster-scoped kind.
	Keys []string

	// Full is set when the changes were too many to name — a resync, or a
	// burst past the budget — and the reader must re-read the cache instead.
	Full bool
}

// Watch is one running informer over a resource type in a namespace.
type Watch struct {
	informer cache.SharedIndexInformer
	lister   cache.GenericLister

	stop chan struct{}
	once sync.Once
}

// Stop terminates the informer.
func (w *Watch) Stop() {
	w.once.Do(func() { close(w.stop) })
}

// Synced reports whether the initial list has completed, so callers know
// whether an empty result means "nothing" or "not yet".
func (w *Watch) Synced() bool { return w.informer.HasSynced() }

// List returns everything the informer currently holds, without a network
// round trip.
func (w *Watch) List(namespace string) ([]runtime.Object, error) {
	if namespace == "" {
		return w.lister.List(labels.Everything())
	}
	return w.lister.ByNamespace(namespace).List(labels.Everything())
}

// Get returns one object from the cache, reporting whether it is still there.
//
// An absent object is the ordinary answer for a delete notification, not an
// error, so the caller can tell "changed" from "gone" without a second call.
func (w *Watch) Get(key string) (runtime.Object, bool) {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return nil, false
	}
	if namespace == "" {
		object, err := w.lister.Get(name)
		return object, err == nil
	}
	object, err := w.lister.ByNamespace(namespace).Get(name)
	return object, err == nil
}

// WatchHub owns every informer belonging to one cluster session.
//
// Informers are keyed by resource type and namespace, so switching namespaces
// does not tear down and rebuild the ones already warm, and closing a cluster
// stops all of its watches at once.
type WatchHub struct {
	client dynamic.Interface

	mu      sync.Mutex
	watches map[watchKey]*entry
	closed  bool

	// notify is called, debounced, when a watched resource type changes.
	notify func(Change)

	pending map[watchKey]*pendingChange

	sweep chan struct{}
}

type watchKey struct {
	gvr       schema.GroupVersionResource
	namespace string
}

// entry is a watch and when it was last read from, which is what the budget
// decides on.
type entry struct {
	watch    *Watch
	lastUsed time.Time
}

// pendingChange accumulates one debounce window's worth of events.
type pendingChange struct {
	timer *time.Timer
	keys  map[string]struct{}
	full  bool
}

// NewWatchHub creates a hub for one cluster.
func NewWatchHub(client dynamic.Interface, notify func(Change)) *WatchHub {
	hub := &WatchHub{
		client:  client,
		watches: make(map[watchKey]*entry),
		pending: make(map[watchKey]*pendingChange),
		notify:  notify,
		sweep:   make(chan struct{}),
	}
	go hub.sweepIdle()
	return hub
}

// Ensure starts a watch if one is not already running, and returns it.
//
// The caller passes the namespace it is displaying; an empty namespace watches
// the whole cluster. There is deliberately no context parameter: a watch
// outlives the request that asked for it, and ends only through Stop,
// StopNamespace or Close.
func (h *WatchHub) Ensure(gvr schema.GroupVersionResource, namespace string) *Watch {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}

	// A watch over every namespace already answers for one namespace, so a
	// view that narrows down reuses it rather than opening a second informer
	// holding a subset of the same objects.
	if namespace != "" {
		if wide, ok := h.watches[watchKey{gvr: gvr}]; ok {
			wide.lastUsed = time.Now()
			h.mu.Unlock()
			return wide.watch
		}
	}

	key := watchKey{gvr: gvr, namespace: namespace}
	if existing, ok := h.watches[key]; ok {
		existing.lastUsed = time.Now()
		h.mu.Unlock()
		return existing.watch
	}

	// The reverse case: a cluster-wide view makes any per-namespace watch of
	// the same type redundant.
	if namespace == "" {
		for candidate, narrow := range h.watches {
			if candidate.gvr == gvr && candidate.namespace != "" {
				narrow.watch.Stop()
				delete(h.watches, candidate)
			}
		}
	}
	h.evictLocked(maxWatches - 1)

	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(h.client, resyncPeriod, namespace, nil)
	informer := factory.ForResource(gvr)

	watch := &Watch{
		informer: informer.Informer(),
		lister:   informer.Lister(),
		stop:     make(chan struct{}),
	}
	h.watches[key] = &entry{watch: watch, lastUsed: time.Now()}
	h.mu.Unlock()

	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { h.schedule(key, obj) },
		UpdateFunc: func(_, obj any) { h.schedule(key, obj) },
		DeleteFunc: func(obj any) { h.schedule(key, obj) },
	}
	// A handler can only fail to register on an already-stopped informer,
	// which the hub's own locking rules out here.
	_, _ = watch.informer.AddEventHandler(handler)

	go watch.informer.Run(watch.stop)

	// Waiting for the initial sync happens off the caller's path: the first
	// list is served directly by the API while the cache warms, so opening a
	// resource page never blocks on it. The reader is told to re-read rather
	// than given a key per object, because that first sync is the whole
	// resource type arriving at once.
	go func() {
		if cache.WaitForCacheSync(watch.stop, watch.informer.HasSynced) {
			h.scheduleFull(key)
		}
	}()

	return watch
}

// Existing returns a running watch, or nil. It lets a reader use the cache
// when it is warm without starting a watch as a side effect.
func (h *WatchHub) Existing(gvr schema.GroupVersionResource, namespace string) *Watch {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, key := range []watchKey{{gvr: gvr, namespace: namespace}, {gvr: gvr}} {
		found, ok := h.watches[key]
		if !ok || !found.watch.Synced() {
			continue
		}
		found.lastUsed = time.Now()
		return found.watch
	}
	return nil
}

// StopNamespace ends watches for a namespace the user has navigated away from.
func (h *WatchHub) StopNamespace(namespace string) {
	if namespace == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for key, found := range h.watches {
		if key.namespace == namespace {
			found.watch.Stop()
			delete(h.watches, key)
		}
	}
}

// Close stops every watch. Called when a cluster session ends.
func (h *WatchHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	close(h.sweep)
	for key, found := range h.watches {
		found.watch.Stop()
		delete(h.watches, key)
	}
	for key, waiting := range h.pending {
		waiting.timer.Stop()
		delete(h.pending, key)
	}
}

// sweepIdle stops watches nobody has read for a while.
func (h *WatchHub) sweepIdle() {
	ticker := time.NewTicker(watchSweepPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-h.sweep:
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-watchIdleTimeout)
			h.mu.Lock()
			for key, found := range h.watches {
				if found.lastUsed.Before(cutoff) {
					found.watch.Stop()
					delete(h.watches, key)
				}
			}
			h.mu.Unlock()
		}
	}
}

// evictLocked stops the least recently read watches until at most keep remain.
// The caller holds the lock.
func (h *WatchHub) evictLocked(keep int) {
	for len(h.watches) > keep {
		var oldest watchKey
		var oldestAt time.Time
		found := false
		for key, candidate := range h.watches {
			if !found || candidate.lastUsed.Before(oldestAt) {
				oldest, oldestAt, found = key, candidate.lastUsed, true
			}
		}
		if !found {
			return
		}
		h.watches[oldest].watch.Stop()
		delete(h.watches, oldest)
	}
}

// schedule coalesces a burst of events into one notification, naming the
// objects that moved.
func (h *WatchHub) schedule(key watchKey, obj any) {
	objectKey, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		// The object's identity could not be read, so the reader is told to
		// re-read rather than shown a table missing whatever this was.
		h.scheduleFull(key)
		return
	}
	h.accumulate(key, objectKey, false)
}

// scheduleFull asks the reader to re-read the whole cache.
func (h *WatchHub) scheduleFull(key watchKey) { h.accumulate(key, "", true) }

func (h *WatchHub) accumulate(key watchKey, objectKey string, full bool) {
	if h.notify == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}

	waiting, ok := h.pending[key]
	if !ok {
		waiting = &pendingChange{keys: make(map[string]struct{}, 8)}
		h.pending[key] = waiting
		waiting.timer = time.AfterFunc(notifyDebounce, func() { h.flush(key) })
	}

	if full {
		waiting.full = true
		waiting.keys = nil
		return
	}
	if waiting.full {
		return
	}
	waiting.keys[objectKey] = struct{}{}
	if len(waiting.keys) > maxPendingKeys {
		waiting.full = true
		waiting.keys = nil
	}
}

func (h *WatchHub) flush(key watchKey) {
	h.mu.Lock()
	waiting, ok := h.pending[key]
	delete(h.pending, key)
	closed := h.closed
	h.mu.Unlock()

	if closed || !ok {
		return
	}

	change := Change{GVR: key.gvr, Namespace: key.namespace, Full: waiting.full}
	if !waiting.full {
		change.Keys = make([]string, 0, len(waiting.keys))
		for objectKey := range waiting.keys {
			change.Keys = append(change.Keys, objectKey)
		}
	}
	h.notify(change)
}
