// Package access is Biebie Kube's only relationship with Biebie Access.
//
// It asks two questions — "is this customer's network up?" and "what context
// does this handoff carry?" — and can ask Biebie Access to open its own window
// for a profile. It contains no VPN logic of any kind: no Forcepoint, no
// Ivanti, no FortiClient, no login, no credential. Biebie Kube must never
// become a VPN client, and this package is where that boundary is kept.
package access

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	bctx "biebie.net/protocol/context"
	"biebie.net/protocol/ipc"
)

// statusCacheTTL smooths repeated status questions.
//
// A cluster page asks about access on every connect and retry. Without a short
// cache, opening several clusters at once would hammer Biebie Access with
// identical questions.
const statusCacheTTL = 2 * time.Second

// Client talks to Biebie Access over local IPC.
//
// Every call tolerates Biebie Access being absent: it is a sibling product,
// not a dependency, and Biebie Kube must work without it for clusters that do
// not need customer network access.
type Client struct {
	rpc *ipc.Client

	mu     sync.Mutex
	cache  map[string]cachedStatus
	nowFn  func() time.Time
	absent bool
}

type cachedStatus struct {
	status bctx.AccessStatus
	at     time.Time
}

// NewClient targets the Biebie Access endpoint for the current user.
func NewClient() (*Client, error) {
	endpoint, err := ipc.AccessEndpoint()
	if err != nil {
		return nil, err
	}
	return NewClientFor(endpoint), nil
}

// NewClientFor targets a specific endpoint, used by tests and by the bundled
// development stand-in.
func NewClientFor(endpoint ipc.Endpoint) *Client {
	return &Client{
		rpc:   ipc.NewClient(endpoint),
		cache: make(map[string]cachedStatus),
		nowFn: time.Now,
	}
}

// Installed reports whether Biebie Access is running and answering.
func (c *Client) Installed(ctx context.Context) bool {
	available := c.rpc.Available(ctx)

	c.mu.Lock()
	c.absent = !available
	c.mu.Unlock()

	return available
}

// Status asks whether a customer profile is currently connected.
func (c *Client) Status(ctx context.Context, profileID string) (bctx.AccessStatus, error) {
	if profileID == "" {
		return bctx.AccessStatus{}, errors.New("an access profile is required")
	}

	c.mu.Lock()
	if entry, ok := c.cache[profileID]; ok && c.nowFn().Sub(entry.at) < statusCacheTTL {
		c.mu.Unlock()
		return entry.status, nil
	}
	c.mu.Unlock()

	var status bctx.AccessStatus
	err := c.rpc.Call(ctx, ipc.MethodAccessStatus, map[string]string{"profileId": profileID}, &status)
	if errors.Is(err, ipc.ErrPeerUnavailable) {
		return bctx.Unknown(profileID, "Biebie Access is not running."), nil
	}
	if err != nil {
		return bctx.AccessStatus{}, fmt.Errorf("ask Biebie Access about %s: %w", profileID, err)
	}
	status.ProfileID = profileID

	c.mu.Lock()
	c.cache[profileID] = cachedStatus{status: status, at: c.nowFn()}
	c.mu.Unlock()

	return status, nil
}

// Connect asks Biebie Access to bring a profile up.
//
// Biebie Access decides what that means — it may need a password, an OTP, or a
// vendor window — and it always shows its own UI. Biebie Kube never performs
// an unattended login on the user's behalf.
//
// The returned identifier is the connection Biebie Access resolved the request
// to. It differs from profileID when a cluster refers to its connection by
// name, and it is the identifier every later notification will use.
func (c *Client) Connect(ctx context.Context, profileID string) (string, error) {
	var result bctx.AccessConnectResult
	if err := c.rpc.Call(ctx, ipc.MethodAccessConnect, map[string]string{"profileId": profileID}, &result); err != nil {
		return "", err
	}
	c.Forget(profileID)
	if result.ProfileID == "" {
		// An older Biebie Access acknowledged without naming what it resolved,
		// so the reference stands as the only identifier either side knows.
		return profileID, nil
	}
	c.Forget(result.ProfileID)
	return result.ProfileID, nil
}

// Profiles lists the connections Biebie Access holds.
//
// It returns an empty list, not an error, when Biebie Access is not running.
// The caller is populating a picker, and "no choices yet" is the honest answer
// to show beside an offer to install or start the other application.
func (c *Client) Profiles(ctx context.Context) ([]bctx.AccessProfile, error) {
	var profiles []bctx.AccessProfile
	err := c.rpc.Call(ctx, ipc.MethodAccessProfiles, struct{}{}, &profiles)
	if errors.Is(err, ipc.ErrPeerUnavailable) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ask Biebie Access for its connections: %w", err)
	}
	return profiles, nil
}

// ConsumeHandoff redeems a handoff ticket with Biebie Access.
//
// The reply is a context: identifiers and display names. If Biebie Access ever
// sent credential material, the protocol's own validation would refuse it.
func (c *Client) ConsumeHandoff(ctx context.Context, handoffID string) (*bctx.BiebieContext, error) {
	var received bctx.BiebieContext
	err := c.rpc.Call(ctx, ipc.MethodConsumeHandoff, map[string]string{
		"handoffId": handoffID,
		"targetApp": string(bctx.AppKube),
	}, &received)
	if err != nil {
		return nil, err
	}
	if err := received.Validate(); err != nil {
		return nil, fmt.Errorf("Biebie Access sent an unusable context: %w", err)
	}
	return &received, nil
}

// Forget drops a cached status, so the next question hits Biebie Access. It is
// called after anything that could have changed the answer.
func (c *Client) Forget(profileID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, profileID)
}
