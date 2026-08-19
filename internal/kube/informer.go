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

// WatchHub owns every informer belonging to one cluster session.
//
// Informers are keyed by resource type and namespace, so switching namespaces
// does not tear down and rebuild the ones already warm, and closing a cluster
// stops all of its watches at once.
type WatchHub struct {
	client dynamic.Interface

	mu      sync.Mutex
	watches map[watchKey]*Watch
	closed  bool

	// notify is called, debounced, when a watched resource type changes.
	notify func(gvr schema.GroupVersionResource, namespace string)

	pending map[watchKey]*time.Timer
}

type watchKey struct {
	gvr       schema.GroupVersionResource
	namespace string
}

// NewWatchHub creates a hub for one cluster.
func NewWatchHub(client dynamic.Interface, notify func(schema.GroupVersionResource, string)) *WatchHub {
	return &WatchHub{
		client:  client,
		watches: make(map[watchKey]*Watch),
		pending: make(map[watchKey]*time.Timer),
		notify:  notify,
	}
}

// Ensure starts a watch if one is not already running, and returns it.
//
// The caller passes the namespace it is displaying; an empty namespace watches
// the whole cluster. There is deliberately no context parameter: a watch
// outlives the request that asked for it, and ends only through Stop,
// StopNamespace or Close.
func (h *WatchHub) Ensure(gvr schema.GroupVersionResource, namespace string) *Watch {
	key := watchKey{gvr: gvr, namespace: namespace}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	if existing, ok := h.watches[key]; ok {
		h.mu.Unlock()
		return existing
	}

	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(h.client, resyncPeriod, namespace, nil)
	informer := factory.ForResource(gvr)

	watch := &Watch{
		informer: informer.Informer(),
		lister:   informer.Lister(),
		stop:     make(chan struct{}),
	}
	h.watches[key] = watch
	h.mu.Unlock()

	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { h.schedule(key) },
		UpdateFunc: func(any, any) { h.schedule(key) },
		DeleteFunc: func(any) { h.schedule(key) },
	}
	// A handler can only fail to register on an already-stopped informer,
	// which the hub's own locking rules out here.
	_, _ = watch.informer.AddEventHandler(handler)

	go watch.informer.Run(watch.stop)

	// Waiting for the initial sync happens off the caller's path: the first
	// list is served directly by the API while the cache warms, so opening a
	// resource page never blocks on it.
	go func() {
		if cache.WaitForCacheSync(watch.stop, watch.informer.HasSynced) {
			h.schedule(key)
		}
	}()

	return watch
}

// Existing returns a running watch, or nil. It lets a reader use the cache
// when it is warm without starting a watch as a side effect.
func (h *WatchHub) Existing(gvr schema.GroupVersionResource, namespace string) *Watch {
	h.mu.Lock()
	defer h.mu.Unlock()
	if watch, ok := h.watches[watchKey{gvr: gvr, namespace: namespace}]; ok && watch.Synced() {
		return watch
	}
	return nil
}

// StopNamespace ends watches for a namespace the user has navigated away from.
func (h *WatchHub) StopNamespace(namespace string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for key, watch := range h.watches {
		if key.namespace == namespace {
			watch.Stop()
			delete(h.watches, key)
		}
	}
}

// Close stops every watch. Called when a cluster session ends.
func (h *WatchHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for key, watch := range h.watches {
		watch.Stop()
		delete(h.watches, key)
	}
	for key, timer := range h.pending {
		timer.Stop()
		delete(h.pending, key)
	}
}

// schedule coalesces a burst of events into one notification.
func (h *WatchHub) schedule(key watchKey) {
	if h.notify == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	if _, waiting := h.pending[key]; waiting {
		return
	}

	h.pending[key] = time.AfterFunc(notifyDebounce, func() {
		h.mu.Lock()
		delete(h.pending, key)
		closed := h.closed
		h.mu.Unlock()

		if !closed {
			h.notify(key.gvr, key.namespace)
		}
	})
}
