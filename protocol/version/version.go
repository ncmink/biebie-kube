// Package version pins the Biebie Context Protocol wire version.
//
// Biebie Access and Biebie Kube ship independently, so every message states
// which protocol it speaks. A peer that speaks a different major version is
// refused with an explanation instead of being partially understood.
package version

import "fmt"

// Name identifies the protocol in every envelope. It is deliberately product
// neutral: future Biebie applications consume the same contract.
const Name = "biebie-context"

// Current is the protocol version this build speaks.
const Current = 1

// Minimum is the oldest version this build still accepts.
const Minimum = 1

// Envelope is embedded in every request and response.
type Envelope struct {
	Protocol string `json:"protocol"`
	Version  int    `json:"version"`
}

// NewEnvelope stamps a message with the current protocol version.
func NewEnvelope() Envelope {
	return Envelope{Protocol: Name, Version: Current}
}

// Check reports whether a received envelope can be understood.
func Check(e Envelope) error {
	if e.Protocol != Name {
		return fmt.Errorf("unknown protocol %q, expected %q", e.Protocol, Name)
	}
	if e.Version < Minimum {
		return fmt.Errorf("protocol version %d is no longer supported, minimum is %d", e.Version, Minimum)
	}
	if e.Version > Current {
		return fmt.Errorf("protocol version %d is newer than this application understands (%d); update Biebie", e.Version, Current)
	}
	return nil
}
