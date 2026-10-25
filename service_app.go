package main

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"

	"biebie-kube/internal/store"
	"biebie-kube/internal/update"
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

// ReleaseNotesState tells the frontend whether to show What's new on launch.
type ReleaseNotesState struct {
	Current      string `json:"current"`
	LastSeen     string `json:"lastSeen"`
	ShowSince    string `json:"showSince"`
	ShouldPrompt bool   `json:"shouldPrompt"`
}

// ReleaseNotesState reports the running version and whether What's new should
// open for versions after ShowSince and up to Current. An empty ShowSince
// includes every bundled note up to Current.
func (s *AppService) ReleaseNotesState() ReleaseNotesState {
	current := appVersion
	data := s.core.store.Read()
	lastSeen := data.LastSeenVersion

	state := ReleaseNotesState{
		Current:  current,
		LastSeen: lastSeen,
	}

	if lastSeen != "" && update.CompareVersions(current, lastSeen) <= 0 {
		return state
	}

	if lastSeen == "" && !data.HasPriorUse() {
		_ = s.markReleaseNotesSeen()
		return state
	}

	state.ShouldPrompt = true
	state.ShowSince = lastSeen
	return state
}

// MarkReleaseNotesSeen records that the user dismissed What's new for this version.
func (s *AppService) MarkReleaseNotesSeen() error {
	return s.markReleaseNotesSeen()
}

func (s *AppService) markReleaseNotesSeen() error {
	return s.core.store.Update(func(data *store.Data) error {
		data.LastSeenVersion = appVersion
		return nil
	})
}

// StatePath reports where Biebie Kube keeps its own data.
func (s *AppService) StatePath() string { return s.core.store.Path() }

// CheckForUpdate opens the Wails update window and, if a newer GitHub
// Release exists, downloads and stages it. Once the update is ready the
// application restarts itself into the new version; the call returns as
// soon as the check has started so the Settings button does not wait on
// the download. Manual checks are not throttled.
func (s *AppService) CheckForUpdate() error {
	if s.updater == nil || s.updater.State() == updater.StateUnconfigured {
		return errors.New("checking for updates is not available in this build")
	}
	go s.runUpdateCheck(true)
	return nil
}

// Ready is called by the frontend once its event listeners are attached.
//
// A deep link can arrive before the window exists, and an event emitted then
// reaches nobody — the handoff would be consumed and silently lost. The
// frontend saying when it is listening is a call rather than an event, so
// there is no window in which the announcement itself can be missed.
//
// When autoCheckUpdates is true, a background check runs at most once per
// update.AutoCheckMinInterval unless the user checks manually from Settings
// or the application menu.
func (s *AppService) Ready(autoCheckUpdates bool) {
	s.links.release()
	if !autoCheckUpdates {
		return
	}
	if !s.dueForAutoUpdateCheck() {
		return
	}
	go s.runUpdateCheck(false)
}

func (s *AppService) dueForAutoUpdateCheck() bool {
	data := s.core.store.Read()
	return update.DueForAutoCheck(data.LastUpdateCheckAt, time.Now())
}

func (s *AppService) recordUpdateCheck(now time.Time) {
	if err := s.core.store.Update(func(data *store.Data) error {
		data.LastUpdateCheckAt = now.UTC().Format(time.RFC3339)
		return nil
	}); err != nil {
		log.Printf("update: could not record last check: %v", err)
	}
}

// runUpdateCheck contacts GitHub. interactive opens the update window even
// when no newer release exists; background stays silent unless an update is
// found.
func (s *AppService) runUpdateCheck(interactive bool) {
	if s.updater == nil || s.updater.State() == updater.StateUnconfigured {
		return
	}

	s.recordUpdateCheck(time.Now())

	if interactive {
		if err := s.updater.CheckAndInstall(context.Background()); err != nil {
			log.Printf("update: %v", err)
		}
		return
	}

	rel, err := s.updater.Check(context.Background())
	if err != nil || rel == nil {
		return
	}
	if err := s.updater.CheckAndInstall(context.Background()); err != nil {
		log.Printf("update: %v", err)
	}
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
