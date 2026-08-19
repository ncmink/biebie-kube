package handoff

import (
	stdctx "context"
	"fmt"
	"sync"
	"time"

	bctx "biebie-kube/protocol/context"
)

// Broker is the capability an application exposes when it can hand its context
// to another Biebie application.
//
// Biebie Access implements it. Biebie Kube consumes it over IPC.
type Broker interface {
	CreateHandoff(ctx stdctx.Context, handoff ContextHandoff) (string, error)
	ConsumeHandoff(ctx stdctx.Context, handoffID string) (*bctx.BiebieContext, error)
}

// Store is an in-memory broker. Handoffs are deliberately not persisted: a
// ticket that outlives the process that issued it has no reason to be honoured.
type Store struct {
	mu      sync.Mutex
	byID    map[string]ContextHandoff
	now     func() time.Time
	osUser  func() (string, error)
	ttl     time.Duration
	maxOpen int
}

// StoreOption customises a Store. Defaults are the safe choice; the options
// exist for tests and for applications with unusual launch latency.
type StoreOption func(*Store)

// WithTTL sets how long new handoffs live.
func WithTTL(ttl time.Duration) StoreOption {
	return func(s *Store) { s.ttl = ttl }
}

// WithClock replaces the time source, for tests.
func WithClock(now func() time.Time) StoreOption {
	return func(s *Store) { s.now = now }
}

// WithOSUser replaces OS-user resolution, for tests.
func WithOSUser(fn func() (string, error)) StoreOption {
	return func(s *Store) { s.osUser = fn }
}

// NewStore creates an empty broker.
func NewStore(opts ...StoreOption) *Store {
	s := &Store{
		byID:    make(map[string]ContextHandoff),
		now:     time.Now,
		osUser:  currentUser,
		ttl:     DefaultTTL,
		maxOpen: 32,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// CreateHandoff registers a pending transfer and returns its identifier.
//
// The caller supplies the context and the target application; the store owns
// identity, timing and the OS-user binding so those cannot be forgotten.
func (s *Store) CreateHandoff(_ stdctx.Context, h ContextHandoff) (string, error) {
	if err := h.Context.Validate(); err != nil {
		return "", err
	}
	if !h.TargetApp.Known() {
		return "", fmt.Errorf("unknown target application %q", h.TargetApp)
	}
	if !h.SourceApp.Known() {
		return "", fmt.Errorf("unknown source application %q", h.SourceApp)
	}

	ttl := s.ttl
	if !h.ExpiresAt.IsZero() && !h.CreatedAt.IsZero() {
		ttl = h.ExpiresAt.Sub(h.CreatedAt)
	}
	if ttl < MinTTL || ttl > MaxTTL {
		return "", ErrTTLRange
	}

	osUser, err := s.osUser()
	if err != nil {
		return "", fmt.Errorf("resolve current user: %w", err)
	}

	id, err := NewID()
	if err != nil {
		return "", err
	}

	now := s.now()
	h.ID = id
	h.OSUser = osUser
	h.CreatedAt = now
	h.ExpiresAt = now.Add(ttl)
	h.Consumed = false
	if h.Context.CreatedAt.IsZero() {
		h.Context.CreatedAt = now
	}
	if h.Context.ExpiresAt.IsZero() {
		h.Context.ExpiresAt = h.ExpiresAt
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	if len(s.byID) >= s.maxOpen {
		return "", fmt.Errorf("too many pending handoffs (%d)", len(s.byID))
	}
	s.byID[id] = h
	return id, nil
}

// ConsumeHandoff redeems a ticket exactly once.
//
// Every failure mode is distinct so the user is told what actually happened:
// an expired ticket is a different problem from one addressed elsewhere.
func (s *Store) ConsumeHandoff(_ stdctx.Context, id string) (*bctx.BiebieContext, error) {
	return s.consume(id, "")
}

// ConsumeFor redeems a ticket on behalf of a named application, rejecting one
// addressed to a different Biebie product.
func (s *Store) ConsumeFor(id string, target bctx.App) (*bctx.BiebieContext, error) {
	return s.consume(id, target)
}

func (s *Store) consume(id string, target bctx.App) (*bctx.BiebieContext, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}

	osUser, err := s.osUser()
	if err != nil {
		return nil, fmt.Errorf("resolve current user: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.sweepLocked(now)

	h, ok := s.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	if h.Consumed {
		return nil, ErrConsumed
	}
	if h.Expired(now) {
		delete(s.byID, id)
		return nil, ErrExpired
	}
	if h.OSUser != osUser {
		return nil, ErrWrongUser
	}
	if target != "" && h.TargetApp != target {
		return nil, ErrWrongApp
	}

	delete(s.byID, id)

	out := h.Context
	return &out, nil
}

// Pending reports how many tickets are outstanding, for diagnostics.
func (s *Store) Pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(s.now())
	return len(s.byID)
}

func (s *Store) sweepLocked(now time.Time) {
	for id, h := range s.byID {
		if h.Expired(now) {
			delete(s.byID, id)
		}
	}
}
