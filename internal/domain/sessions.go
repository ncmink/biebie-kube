package domain

import (
	"errors"
	"fmt"
	"time"
)

// LogOptions is what the log viewer asks for.
type LogOptions struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container"`

	Follow     bool `json:"follow"`
	Timestamps bool `json:"timestamps"`

	// TailLines bounds the initial read. A pod that has been running for a
	// month must not stream a month of history into the window.
	TailLines int64 `json:"tailLines"`

	// Previous reads the terminated instance of a container, which is where
	// the reason for a CrashLoopBackOff actually is.
	Previous bool `json:"previous"`
}

// DefaultTailLines is the initial history a log view requests.
const DefaultTailLines = 500

// MaxTailLines caps what the UI may ask for in one read.
const MaxTailLines = 10_000

// Normalise fills defaults and clamps a request into a range the frontend can
// hold in a bounded buffer.
func (o LogOptions) Normalise() (LogOptions, error) {
	if o.Pod == "" {
		return o, errors.New("a pod is required")
	}
	if o.TailLines <= 0 {
		o.TailLines = DefaultTailLines
	}
	if o.TailLines > MaxTailLines {
		o.TailLines = MaxTailLines
	}
	if o.Previous {
		// A terminated container produces no new lines, so following it would
		// leave a stream open forever with nothing to deliver.
		o.Follow = false
	}
	return o, nil
}

// LogChunk is a batch of log lines delivered to the frontend.
//
// Lines are batched rather than emitted one per event: a chatty pod produces
// thousands of lines a second, and one Wails event per line would stall the
// renderer.
type LogChunk struct {
	StreamID string   `json:"streamId"`
	Lines    []string `json:"lines"`

	// Done marks the final chunk, after which no more arrive.
	Done bool `json:"done,omitempty"`

	// Error explains an interrupted stream.
	Error string `json:"error,omitempty"`
}

// TerminalRequest opens an exec session inside a container.
type TerminalRequest struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container"`

	// Command is optional. When empty the service probes the shells a
	// container might have rather than assuming bash exists — distroless and
	// Alpine images routinely have neither bash nor sh at the usual path.
	Command []string `json:"command,omitempty"`
}

// TerminalChunk carries container output to xterm.js.
type TerminalChunk struct {
	SessionID string `json:"sessionId"`
	Data      string `json:"data"`

	Done  bool   `json:"done,omitempty"`
	Error string `json:"error,omitempty"`
}

// TerminalSession is an open exec session.
type TerminalSession struct {
	ID        string `json:"id"`
	ClusterID string `json:"clusterId"`

	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container"`

	// Shell is the command that actually started, which is not always what was
	// requested.
	Shell string `json:"shell"`

	StartedAt time.Time `json:"startedAt"`
}

// PortForwardState is the lifecycle of a forward.
type PortForwardState string

// Port-forward states.
const (
	PortForwardStarting PortForwardState = "starting"
	PortForwardRunning  PortForwardState = "running"
	PortForwardStopped  PortForwardState = "stopped"
	PortForwardFailed   PortForwardState = "failed"
)

// PortForwardRequest asks for a local port to reach a pod or service port.
type PortForwardRequest struct {
	Namespace    string `json:"namespace"`
	ResourceType string `json:"resourceType"`
	ResourceName string `json:"resourceName"`

	RemotePort int `json:"remotePort"`

	// LocalPort may be 0, in which case the operating system picks a free one.
	LocalPort int `json:"localPort"`
}

// Validate rejects a request that could not be honoured.
func (r PortForwardRequest) Validate() error {
	if r.ResourceName == "" {
		return errors.New("a pod or service is required")
	}
	switch r.ResourceType {
	case "pod", "service":
	default:
		return fmt.Errorf("cannot forward to %q", r.ResourceType)
	}
	if r.RemotePort < 1 || r.RemotePort > 65535 {
		return fmt.Errorf("remote port %d is out of range", r.RemotePort)
	}
	if r.LocalPort < 0 || r.LocalPort > 65535 {
		return fmt.Errorf("local port %d is out of range", r.LocalPort)
	}
	return nil
}

// PortForwardSession is one running forward.
type PortForwardSession struct {
	ID string `json:"id"`

	ClusterID   string `json:"clusterId"`
	ClusterName string `json:"clusterName"`
	Namespace   string `json:"namespace"`

	ResourceType string `json:"resourceType"`
	ResourceName string `json:"resourceName"`

	LocalPort  int `json:"localPort"`
	RemotePort int `json:"remotePort"`

	State PortForwardState `json:"state"`
	Error string           `json:"error,omitempty"`

	StartedAt time.Time `json:"startedAt"`
}

// URL is the address to open in a browser.
//
// It is always loopback: a forward exists to reach a customer's service from
// this machine, not to republish it on the network.
func (s PortForwardSession) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", s.LocalPort)
}

// ApplyResult reports what an edited manifest changed.
type ApplyResult struct {
	Ref ResourceRef `json:"ref"`

	// Changed is false when the submitted YAML matched the live object, which
	// is worth saying rather than implying success.
	Changed bool `json:"changed"`

	Message string `json:"message,omitempty"`
}
