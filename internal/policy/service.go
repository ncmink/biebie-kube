package policy

import (
	"fmt"
	"sync"

	"biebie-kube/internal/cluster"
	"biebie-kube/internal/domain"
)

// EventPolicyChanged is emitted when a cluster's effective policy changes.
const EventPolicyChanged = "cluster:policy"

// Service enforces read-only policy for cluster mutations.
type Service struct {
	repo     *cluster.Repository
	clusters *cluster.Manager

	mu       sync.Mutex
	revision map[string]int64

	onReadOnly func(clusterID string)
	emit       func(domain.OperationPolicy)
}

// NewService wires policy against cluster storage and live sessions.
func NewService(
	repo *cluster.Repository,
	clusters *cluster.Manager,
	onReadOnly func(string),
	emit func(domain.OperationPolicy),
) *Service {
	return &Service{
		repo:       repo,
		clusters:   clusters,
		revision:   make(map[string]int64),
		onReadOnly: onReadOnly,
		emit:       emit,
	}
}

// Check reports whether one capability may run for a cluster.
func (s *Service) Check(clusterID string, cap domain.Capability) domain.PolicyDecision {
	policy := s.Snapshot(clusterID)
	if policy.EffectiveMode.EffectiveReadOnly() && !allowedInReadOnly(cap) {
		return domain.PolicyDecision{
			Allowed:  false,
			Code:     "read_only",
			Reason:   readOnlyReason(policy),
			Revision: policy.Revision,
		}
	}
	if !knownCapability(cap) {
		return domain.PolicyDecision{
			Allowed:  false,
			Code:     "unsupported_capability",
			Reason:   fmt.Sprintf("operation %q is not recognised", cap),
			Revision: policy.Revision,
		}
	}
	return domain.PolicyDecision{Allowed: true, Revision: policy.Revision}
}

// Snapshot returns the current policy for one cluster.
func (s *Service) Snapshot(clusterID string) domain.OperationPolicy {
	persisted := s.repo.AccessMode(clusterID)
	sessionReadOnly := s.clusters.SessionReadOnly(clusterID)
	return domain.OperationPolicy{
		ClusterID:       clusterID,
		SessionEpoch:    s.clusters.SessionEpoch(clusterID),
		PersistedMode:   persisted,
		SessionReadOnly: sessionReadOnly,
		EffectiveMode:   effectiveMode(persisted, sessionReadOnly),
		Revision:        s.revisionFor(clusterID),
	}
}

// SetPersistedMode stores the cluster default access mode.
func (s *Service) SetPersistedMode(clusterID string, mode domain.AccessMode) (domain.OperationPolicy, error) {
	if mode != domain.AccessModeReadWrite && mode != domain.AccessModeReadOnly {
		return domain.OperationPolicy{}, fmt.Errorf("unknown access mode %q", mode)
	}
	if err := s.repo.SetAccessMode(clusterID, mode); err != nil {
		return domain.OperationPolicy{}, err
	}
	return s.applyChange(clusterID), nil
}

// SetSessionReadOnly toggles the session-only restriction.
//
// Session override can only add restriction when the persisted mode is
// read-write. Clearing session read-only is always allowed.
func (s *Service) SetSessionReadOnly(clusterID string, readOnly bool) (domain.OperationPolicy, error) {
	persisted := s.repo.AccessMode(clusterID)
	if readOnly && persisted.EffectiveReadOnly() {
		return domain.OperationPolicy{}, fmt.Errorf("this cluster is read-only by default; change the cluster access mode in settings")
	}
	s.clusters.SetSessionReadOnly(clusterID, readOnly)
	return s.applyChange(clusterID), nil
}

func (s *Service) applyChange(clusterID string) domain.OperationPolicy {
	s.mu.Lock()
	s.revision[clusterID]++
	revision := s.revision[clusterID]
	s.mu.Unlock()

	policy := s.Snapshot(clusterID)
	policy.Revision = revision

	if policy.EffectiveMode.EffectiveReadOnly() && s.onReadOnly != nil {
		s.onReadOnly(clusterID)
	}
	if s.emit != nil {
		s.emit(policy)
	}
	return policy
}

func (s *Service) revisionFor(clusterID string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revision[clusterID]
}

func effectiveMode(persisted domain.AccessMode, sessionReadOnly bool) domain.AccessMode {
	if persisted.EffectiveReadOnly() || sessionReadOnly {
		return domain.AccessModeReadOnly
	}
	return domain.AccessModeReadWrite
}

func readOnlyReason(policy domain.OperationPolicy) string {
	switch {
	case policy.PersistedMode.EffectiveReadOnly() && policy.SessionReadOnly:
		return "This cluster is read-only by default and this session is also read-only."
	case policy.PersistedMode.EffectiveReadOnly():
		return "This cluster is read-only by default."
	default:
		return "This session is read-only."
	}
}

func knownCapability(cap domain.Capability) bool {
	switch cap {
	case domain.CapResourceApply, domain.CapResourceDelete, domain.CapResourceAction,
		domain.CapResourceCreate, domain.CapArgoSync, domain.CapArgoRefresh,
		domain.CapArgoOpenUI, domain.CapTerminalOpen, domain.CapTerminalInput,
		domain.CapPortForward, domain.CapAuthoringCreate, domain.CapAuthoringSynth:
		return true
	default:
		return false
	}
}

func allowedInReadOnly(cap domain.Capability) bool {
	return false
}
