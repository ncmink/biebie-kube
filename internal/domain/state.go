package domain

import "time"

// ClusterState is where a cluster session is in its lifecycle.
//
// A boolean cannot express the difference between "the VPN is not up yet",
// "the API server refused the credentials" and "the TCP connection timed out",
// and those three send an engineer to completely different places.
type ClusterState string

// The connection lifecycle.
const (
	ClusterDisconnected  ClusterState = "disconnected"
	ClusterWaitingAccess ClusterState = "waiting_access"
	ClusterConnecting    ClusterState = "connecting"
	ClusterConnected     ClusterState = "connected"
	ClusterUnauthorized  ClusterState = "unauthorized"
	ClusterUnreachable   ClusterState = "unreachable"
	ClusterFailed        ClusterState = "failed"
)

// Active reports whether work is in flight, so the UI can show progress
// without enumerating states.
func (s ClusterState) Active() bool {
	return s == ClusterWaitingAccess || s == ClusterConnecting
}

// Usable reports whether Kubernetes calls can be made.
func (s ClusterState) Usable() bool { return s == ClusterConnected }

// Layer names the part of the path between the laptop and the API server that
// a diagnosis is talking about.
type Layer string

// The layers probed, in the order they are attempted.
const (
	LayerAccess     Layer = "access"
	LayerNetwork    Layer = "network"
	LayerTCP        Layer = "tcp"
	LayerTLS        Layer = "tls"
	LayerKubernetes Layer = "kubernetes"
)

// ProbeResult is the outcome of testing one layer.
type ProbeResult string

// Probe outcomes. Skipped matters: after TCP times out there is nothing to
// learn from asking Kubernetes, and reporting a second failure would imply two
// problems where there is one.
const (
	ProbePassed  ProbeResult = "passed"
	ProbeFailed  ProbeResult = "failed"
	ProbeSkipped ProbeResult = "skipped"
)

// Probe is one line of the connection diagnosis shown to the user.
type Probe struct {
	Layer   Layer       `json:"layer"`
	Result  ProbeResult `json:"result"`
	Detail  string      `json:"detail,omitempty"`
	Elapsed int64       `json:"elapsedMs,omitempty"`
}

// FailureKind classifies why a connection attempt did not succeed, so the UI
// can offer the one action that helps.
type FailureKind string

// The failures worth distinguishing.
const (
	FailureNone           FailureKind = ""
	FailureAccessDown     FailureKind = "access_disconnected"
	FailureNoRoute        FailureKind = "route_unavailable"
	FailureDNS            FailureKind = "dns_failure"
	FailureTCPTimeout     FailureKind = "tcp_timeout"
	FailureTLS            FailureKind = "tls_failure"
	FailureUnauthorized   FailureKind = "authentication_failure"
	FailureForbidden      FailureKind = "authorization_failure"
	FailureAPIUnavailable FailureKind = "kubernetes_unavailable"
	FailureConfig         FailureKind = "configuration_error"
)

// Diagnosis explains a failed connection layer by layer.
//
// It contains no credential material and may be shared with Biebie Access as
// non-secret diagnostic context.
type Diagnosis struct {
	Kind    FailureKind `json:"kind"`
	Summary string      `json:"summary"`
	Detail  string      `json:"detail,omitempty"`
	Probes  []Probe     `json:"probes"`

	// AccessProfileID is set when the fix is to connect a Biebie Access
	// profile, so the UI can offer that button directly.
	AccessProfileID string `json:"accessProfileId,omitempty"`
}

// Session is the live state of one cluster connection.
type Session struct {
	ClusterID string       `json:"clusterId"`
	State     ClusterState `json:"state"`

	Namespace string `json:"namespace"`

	ServerVersion string `json:"serverVersion,omitempty"`

	ConnectedAt *time.Time `json:"connectedAt,omitempty"`

	Diagnosis *Diagnosis `json:"diagnosis,omitempty"`

	// Error is a short message for the header. Diagnosis carries the detail.
	Error string `json:"error,omitempty"`
}
