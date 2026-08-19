// Package handoff carries a Biebie Context from one application to another
// through a short-lived, single-use ticket.
//
// The ticket identifier is not a secret by itself: it is random, it expires in
// under two minutes, it can be redeemed once, and it is only redeemable by the
// application it was addressed to, running as the OS user who created it. That
// is what makes it safe to put in a deep-link URL.
package handoff

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"os/user"
	"strings"
	"time"

	bctx "biebie-kube/protocol/context"
)

// IDPrefix marks a handoff identifier in logs and URLs.
const IDPrefix = "hnd_"

// Lifetime bounds. A handoff exists only long enough for the target
// application to launch and ask for it.
const (
	MinTTL     = 15 * time.Second
	DefaultTTL = 60 * time.Second
	MaxTTL     = 120 * time.Second
)

// ContextHandoff is one pending transfer.
type ContextHandoff struct {
	ID string `json:"id"`

	SourceApp bctx.App `json:"sourceApp"`
	TargetApp bctx.App `json:"targetApp"`

	Context bctx.BiebieContext `json:"context"`

	// OSUser is the account that created the handoff. A different user on the
	// same machine may not redeem it.
	OSUser string `json:"osUser"`

	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Consumed  bool      `json:"consumed"`
}

// Expired reports whether the ticket is past its lifetime.
func (h ContextHandoff) Expired(now time.Time) bool { return now.After(h.ExpiresAt) }

// Redeemable reports whether the ticket can still be exchanged for a context.
func (h ContextHandoff) Redeemable(now time.Time) bool { return !h.Consumed && !h.Expired(now) }

// Errors a caller is expected to distinguish and explain to the user.
var (
	ErrNotFound  = errors.New("handoff not found")
	ErrExpired   = errors.New("handoff expired")
	ErrConsumed  = errors.New("handoff already used")
	ErrWrongApp  = errors.New("handoff was issued for a different application")
	ErrWrongUser = errors.New("handoff belongs to a different operating-system user")
	ErrTTLRange  = fmt.Errorf("handoff lifetime must be between %s and %s", MinTTL, MaxTTL)
)

// NewID returns a random, URL-safe handoff identifier.
//
// 80 bits of entropy is far more than a ticket living under two minutes needs,
// while staying short enough to appear in a log line.
func NewID() (string, error) {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate handoff id: %w", err)
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	return IDPrefix + strings.ToLower(encoded), nil
}

// ValidID reports whether a string is shaped like a handoff identifier. It is
// a cheap screen before a lookup, not an authorisation check.
func ValidID(id string) bool {
	if !strings.HasPrefix(id, IDPrefix) {
		return false
	}
	body := strings.TrimPrefix(id, IDPrefix)
	if len(body) != 16 {
		return false
	}
	for _, r := range body {
		isLower := r >= 'a' && r <= 'z'
		isDigit := r >= '2' && r <= '7'
		if !isLower && !isDigit {
			return false
		}
	}
	return true
}

// currentUser is indirected so tests can exercise the cross-user rejection
// without creating operating-system accounts.
var currentUser = func() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	if u.Uid != "" {
		return u.Uid, nil
	}
	return u.Username, nil
}

// CurrentOSUser identifies the account this process runs as.
func CurrentOSUser() (string, error) { return currentUser() }
