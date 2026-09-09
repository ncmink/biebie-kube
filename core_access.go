package main

import (
	"context"
	"sync"

	"biebie-kube/internal/domain"
)

// accessCoordinator tracks Biebie Access launches and retries that must not
// race or duplicate one another.
type accessCoordinator struct {
	mu sync.Mutex

	// launched records profile identifiers handed to a deep link because direct
	// IPC was unavailable. The name may still be in cluster records when the
	// first session event arrives under the resolved UUID.
	launched map[string]struct{}

	// retrying guards against duplicate RetryWaiting calls for one profile.
	retrying map[string]struct{}
}

func newAccessCoordinator() *accessCoordinator {
	return &accessCoordinator{
		launched: make(map[string]struct{}),
		retrying: make(map[string]struct{}),
	}
}

func (a *accessCoordinator) noteLaunched(profileID string) {
	if profileID == "" {
		return
	}
	a.mu.Lock()
	a.launched[profileID] = struct{}{}
	a.mu.Unlock()
}

func (a *accessCoordinator) clearLaunched(profileID string) {
	if profileID == "" {
		return
	}
	a.mu.Lock()
	delete(a.launched, profileID)
	a.mu.Unlock()
}

func (a *accessCoordinator) wasLaunched(profileID string) bool {
	a.mu.Lock()
	_, ok := a.launched[profileID]
	a.mu.Unlock()
	return ok
}

func (a *accessCoordinator) beginRetry(profileID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, busy := a.retrying[profileID]; busy {
		return false
	}
	a.retrying[profileID] = struct{}{}
	return true
}

func (a *accessCoordinator) endRetry(profileID string) {
	a.mu.Lock()
	delete(a.retrying, profileID)
	a.mu.Unlock()
}

// noteAccessLaunched records that Biebie Access was cold-launched for a profile
// identifier that may still be a connection name rather than a UUID.
func (c *Core) noteAccessLaunched(profileID string) {
	c.accessCoord.noteLaunched(profileID)
}

// reconcileAccessProfile rewrites connection names to the UUID Biebie Access
// reports, including when the notification arrives before direct IPC could
// resolve the reference.
func (c *Core) reconcileAccessProfile(ctx context.Context, profileUUID string) {
	if profileUUID == "" {
		return
	}

	profiles, err := c.access.Profiles(ctx)
	if err != nil || len(profiles) == 0 {
		return
	}

	var profileName string
	for _, profile := range profiles {
		if profile.ID == profileUUID {
			profileName = profile.Name
			break
		}
	}

	for _, cluster := range c.clusters.Repository().All() {
		if !cluster.Access.Required {
			continue
		}
		configured := cluster.Access.ProfileID
		if configured == "" || configured == profileUUID {
			continue
		}
		if configured == profileName || c.accessCoord.wasLaunched(configured) {
			if _, err := c.clusters.Repository().AdoptAccessProfileID(configured, profileUUID); err == nil {
				c.accessCoord.clearLaunched(configured)
				c.access.Forget(configured)
			}
		}
	}
}

func (c *Core) retryAccessOnce(ctx context.Context, profileID string) {
	if !c.accessCoord.beginRetry(profileID) {
		return
	}
	go func() {
		c.clusters.RetryWaiting(ctx, profileID)
		c.accessCoord.endRetry(profileID)
	}()
}

// handleAccessDown reflects a customer network going away for clusters that
// were actually using its SSH forwards. Clusters that only needed a route, or
// that can still reach the API server another way, are left connected.
func (c *Core) handleAccessDown(profileID string) {
	for _, clusterID := range c.clusters.ClustersForAccessProfile(profileID) {
		session := c.clusters.Session(clusterID)
		if session.State != domain.ClusterConnected || session.APIForward == nil {
			continue
		}
		c.forwards.StopCluster(clusterID)
		c.terminals.CloseCluster(clusterID)
		c.clusters.SuspendForAccessDown(clusterID)
	}
}
