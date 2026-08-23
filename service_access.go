package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"

	bctx "biebie.net/protocol/context"
	"biebie.net/protocol/deeplink"
	"biebie.net/protocol/ipc"

	"biebie-kube/internal/access"
)

// AccessService is the seam between Biebie Kube and Biebie Access.
//
// Everything it does is expressed in identifiers. It resolves no credential,
// speaks no VPN protocol, and treats a missing sibling application as a state
// rather than an error.
type AccessService struct{ core *Core }

func (s *AccessService) ServiceName() string { return "AccessService" }

// AccessState is what the cluster card shows about customer connectivity.
type AccessState struct {
	// Installed reports whether Biebie Access answered. When false the UI
	// offers installation guidance instead of a button that does nothing.
	Installed bool `json:"installed"`

	Status bctx.AccessStatus `json:"status"`
}

// GetAccessStatus asks Biebie Access whether a customer network is up.
func (s *AccessService) GetAccessStatus(ctx context.Context, profileID string) AccessState {
	if profileID == "" {
		return AccessState{Installed: access.Installed()}
	}
	installed := s.core.access.Installed(ctx)
	status, err := s.core.access.Status(ctx, profileID)
	if err != nil {
		return AccessState{
			Installed: installed,
			Status:    bctx.Unknown(profileID, err.Error()),
		}
	}
	return AccessState{Installed: installed, Status: status}
}

// ConnectWithAccess opens Biebie Access at a customer profile.
//
// Two things are tried: a direct request over IPC, which raises the window of
// a running Biebie Access, and failing that a deep link, which launches it.
// Neither carries anything but a profile identifier.
//
// The identifier returned is the one now in effect for the cluster. It differs
// from profileID when Biebie Access resolved a connection name to its own
// identifier and the cluster record was rewritten to match.
func (s *AccessService) ConnectWithAccess(ctx context.Context, profileID, customerID string) (string, error) {
	if profileID == "" {
		return "", fmt.Errorf("this cluster has no Biebie Access profile configured")
	}

	resolved, err := s.core.access.Connect(ctx, profileID)
	if err == nil {
		return s.adopt(profileID, resolved), nil
	}
	// Biebie Access answered, and its answer was no — most often because this
	// cluster holds an identifier that no longer names one of its connections.
	// The deep link is not a second chance at that: it would carry the same
	// identifier to the same application, which would drop it just as flatly,
	// leaving a window that appears to have ignored the click. Only a peer that
	// never answered is worth launching.
	if !errors.Is(err, ipc.ErrPeerUnavailable) {
		return "", describe(err)
	}

	launch := s.core.launcher.ConnectProfile(ctx, profileID, customerID)
	if launch == nil {
		// Nothing answered, so nothing resolved anything: the reference stands
		// as it was configured.
		return profileID, nil
	}
	// Neither route worked. Not being installed is the likeliest reason and the
	// only one with an obvious remedy, so it is named rather than left inside a
	// message about a URL scheme the engineer never typed.
	if !access.Installed() {
		return "", fmt.Errorf("Biebie Access is not running, and does not appear to be installed on this machine")
	}
	return "", describe(launch)
}

// adopt records the identifier Biebie Access resolved a request to, and returns
// the reference now in effect.
//
// A cluster configured with a connection name has to learn the identifier once,
// because every notification Biebie Access sends afterwards names the tunnel by
// identifier: left as a name, the cluster would not recognise its own customer
// network coming up and would sit waiting for a click that already happened.
//
// A failure to record it is not worth failing the connection over. The request
// was accepted, the tunnel is on its way up, and the worst case is being asked
// to click again later.
func (s *AccessService) adopt(configured, resolved string) string {
	if resolved == "" || resolved == configured {
		return configured
	}
	if _, err := s.core.clusters.Repository().AdoptAccessProfileID(configured, resolved); err != nil {
		return configured
	}
	return resolved
}

// AccessInstalled reports whether Biebie Access appears to be on this machine.
func (s *AccessService) AccessInstalled() bool { return access.Installed() }

// ListAccessProfiles returns the connections Biebie Access holds, for the
// cluster dialog to offer instead of a free-text identifier.
//
// An empty list means Biebie Access is not running or holds no connections. The
// dialog treats those the same way: it falls back to letting the identifier be
// typed, so a cluster can still be configured with Biebie Access closed.
func (s *AccessService) ListAccessProfiles(ctx context.Context) ([]bctx.AccessProfile, error) {
	profiles, err := s.core.access.Profiles(ctx)
	if err != nil {
		return nil, describe(err)
	}
	return profiles, nil
}

// HandoffResult is what the UI is told after a handoff is redeemed.
type HandoffResult struct {
	// ClusterID is set when the context matched a cluster this application
	// already knows about.
	ClusterID string `json:"clusterId,omitempty"`

	Context bctx.BiebieContext `json:"context"`

	// Unmatched is true when the handoff was valid but names a cluster that is
	// not configured here, so the UI can offer to add it.
	Unmatched bool `json:"unmatched,omitempty"`
}

// OpenDeepLink handles biebie-kube://open?handoff=...
//
// This is the whole point of the protocol: Biebie Access has already chosen
// the customer and the cluster, so the engineer must not be asked to choose
// them a second time.
func (s *AccessService) OpenDeepLink(ctx context.Context, link string) {
	request, err := deeplink.ParseOpen(link)
	if err != nil {
		emit(EventHandoffFailed, err.Error())
		return
	}
	result, err := s.ConsumeHandoff(ctx, request.HandoffID)
	if err != nil {
		emit(EventHandoffFailed, err.Error())
		return
	}
	emit(EventOpenCluster, result)
}

// ConsumeHandoff redeems a ticket and opens the cluster it names.
func (s *AccessService) ConsumeHandoff(ctx context.Context, handoffID string) (HandoffResult, error) {
	received, err := s.core.access.ConsumeHandoff(ctx, handoffID)
	if err != nil {
		return HandoffResult{}, describe(err)
	}

	present()

	result := HandoffResult{Context: *received}

	cluster, ok := s.core.clusters.Repository().FindByContext(*received)
	if !ok {
		result.Unmatched = true
		return result, nil
	}
	result.ClusterID = cluster.ID

	// Connecting happens in the background so the window can already show the
	// cluster with its progress, rather than staying blank until the API
	// server answers. The call's own context ends when this returns, so the
	// connection sequence gets one that does not.
	go func() {
		if _, err := s.core.clusters.Connect(context.Background(), cluster.ID); err != nil {
			emit(EventHandoffFailed, err.Error())
		}
	}()

	return result, nil
}

// present brings the window forward.
//
// The user clicked a button in another application and expects this one to
// appear, which will not happen on its own when Biebie Kube was already
// running behind other windows. The window is looked up by name rather than
// asking for the current one, which is nil when nothing has focus — precisely
// the case this exists to handle.
func present() {
	app := application.Get()
	if app == nil {
		return
	}
	window, ok := app.Window.GetByName(mainWindow)
	if !ok {
		return
	}
	window.Show()
	window.UnMinimise()
	window.Focus()
}
