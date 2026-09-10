package domain

// AccessMode is the persisted or effective write policy for a cluster.
type AccessMode string

const (
	AccessModeReadWrite AccessMode = "read_write"
	AccessModeReadOnly  AccessMode = "read_only"
)

// EffectiveReadOnly reports whether mutations must be blocked.
func (m AccessMode) EffectiveReadOnly() bool { return m == AccessModeReadOnly }

// Capability names one side-effect path the application can dispatch.
type Capability string

const (
	CapResourceApply   Capability = "resource_apply"
	CapResourceDelete  Capability = "resource_delete"
	CapResourceAction  Capability = "resource_action"
	CapResourceCreate  Capability = "resource_create"
	CapArgoSync        Capability = "argo_sync"
	CapArgoRefresh     Capability = "argo_refresh"
	CapArgoOpenUI      Capability = "argo_open_ui"
	CapTerminalOpen    Capability = "terminal_open"
	CapTerminalInput   Capability = "terminal_input"
	CapPortForward     Capability = "port_forward"
	CapAuthoringCreate Capability = "authoring_create"
	CapAuthoringSynth  Capability = "authoring_synthesize"
)

// OperationPolicy is the effective access mode for one cluster session.
type OperationPolicy struct {
	ClusterID       string     `json:"clusterId"`
	SessionEpoch    string     `json:"sessionEpoch"`
	PersistedMode   AccessMode `json:"persistedMode"`
	SessionReadOnly bool       `json:"sessionReadOnly"`
	EffectiveMode   AccessMode `json:"effectiveMode"`
	Revision        int64      `json:"revision"`
}

// PolicyDecision is the outcome of one capability check.
type PolicyDecision struct {
	Allowed  bool   `json:"allowed"`
	Code     string `json:"code,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Revision int64  `json:"revision"`
}
