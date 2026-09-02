package main

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

// AppService answers questions about the application itself.
type AppService struct {
	core    *Core
	links   *linkQueue
	updater *updater.Updater
}

func (s *AppService) ServiceName() string { return "AppService" }

// Version reports the application version, shown in Settings.
func (s *AppService) Version() string { return appVersion }

// StatePath reports where Biebie Kube keeps its own data.
func (s *AppService) StatePath() string { return s.core.store.Path() }

// CheckForUpdate opens the Wails update window and, if a newer GitHub
// Release exists, downloads and stages it. Once the update is ready the
// application restarts itself into the new version; the call returns as
// soon as the check has started so the Settings button does not wait on
// the download.
func (s *AppService) CheckForUpdate() error {
	if s.updater == nil || s.updater.State() == updater.StateUnconfigured {
		return errors.New("checking for updates is not available in this build")
	}
	go func() {
		if err := s.updater.CheckAndInstall(context.Background()); err != nil {
			log.Printf("update: %v", err)
		}
	}()
	return nil
}

// Ready is called by the frontend once its event listeners are attached.
//
// A deep link can arrive before the window exists, and an event emitted then
// reaches nobody — the handoff would be consumed and silently lost. The
// frontend saying when it is listening is a call rather than an event, so
// there is no window in which the announcement itself can be missed.
func (s *AppService) Ready() {
	s.links.release()
	s.promptIfUpdateAvailable()
}

// promptIfUpdateAvailable checks GitHub quietly. The update window opens only
// when a newer release exists, so a launch that is already current stays silent.
func (s *AppService) promptIfUpdateAvailable() {
	if s.updater == nil || s.updater.State() == updater.StateUnconfigured {
		return
	}
	go func() {
		rel, err := s.updater.Check(context.Background())
		if err != nil || rel == nil {
			return
		}
		if err := s.updater.CheckAndInstall(context.Background()); err != nil {
			log.Printf("update: %v", err)
		}
	}()
}

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
