package context

import "time"

// AccessState is the connectivity of a Biebie Access profile, as reported to
// another application. It never carries credentials.
type AccessState string

// Access states, mirroring the Biebie Access connection lifecycle at the
// coarseness other applications need.
const (
	AccessUnknown      AccessState = "unknown"
	AccessDisconnected AccessState = "disconnected"
	AccessConnecting   AccessState = "connecting"
	AccessConnected    AccessState = "connected"
	AccessFailed       AccessState = "failed"
)

// AccessStatus tells Biebie Kube whether customer network access is currently
// available for a profile.
type AccessStatus struct {
	ProfileID string      `json:"profileId"`
	State     AccessState `json:"state"`
	Connected bool        `json:"connected"`

	// AssignedIP is the tunnel address, shown for diagnosis. Empty when the
	// profile is down or the provider cannot report it.
	AssignedIP string `json:"assignedIp,omitempty"`

	ConnectedAt *time.Time `json:"connectedAt,omitempty"`

	// Detail explains a failed or unknown state in words a person can act on.
	Detail string `json:"detail,omitempty"`
}

// Unknown builds the status used when Biebie Access is not installed, not
// running, or too old to answer.
func Unknown(profileID, detail string) AccessStatus {
	return AccessStatus{ProfileID: profileID, State: AccessUnknown, Detail: detail}
}

// AccessSessionChanged is broadcast by Biebie Access when a profile changes
// state, so a waiting application can retry without a restart.
//
// It is a notification, not a credential channel.
type AccessSessionChanged struct {
	ProfileID string      `json:"profileId"`
	State     AccessState `json:"state"`
}
