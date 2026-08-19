package main

import "sync"

// AppService answers questions about the application itself.
type AppService struct {
	core  *Core
	links *linkQueue
}

func (s *AppService) ServiceName() string { return "AppService" }

// Version reports the application version, shown in Settings.
func (s *AppService) Version() string { return appVersion }

// StatePath reports where Biebie Kube keeps its own data.
func (s *AppService) StatePath() string { return s.core.store.Path() }

// Ready is called by the frontend once its event listeners are attached.
//
// A deep link can arrive before the window exists, and an event emitted then
// reaches nobody — the handoff would be consumed and silently lost. The
// frontend saying when it is listening is a call rather than an event, so
// there is no window in which the announcement itself can be missed.
func (s *AppService) Ready() { s.links.release() }

// linkQueue defers deep links until the frontend can receive the result.
type linkQueue struct {
	open func(string)

	mu      sync.Mutex
	ready   bool
	pending []string
}

func newLinkQueue(open func(string)) *linkQueue {
	return &linkQueue{open: open}
}

// deliver acts on a link now, or holds it until the frontend is ready.
func (q *linkQueue) deliver(link string) {
	q.mu.Lock()
	if !q.ready {
		q.pending = append(q.pending, link)
		q.mu.Unlock()
		return
	}
	q.mu.Unlock()

	go q.open(link)
}

// release marks the frontend ready and acts on anything held.
func (q *linkQueue) release() {
	q.mu.Lock()
	q.ready = true
	held := q.pending
	q.pending = nil
	q.mu.Unlock()

	for _, link := range held {
		go q.open(link)
	}
}
