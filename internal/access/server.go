package access

import (
	"context"
	"encoding/json"
	"fmt"

	bctx "biebie-kube/protocol/context"
	"biebie-kube/protocol/deeplink"
	"biebie-kube/protocol/ipc"
)

// Server is Biebie Kube's own local endpoint.
//
// It exists for two things: Biebie Access pushing a session change so a waiting
// cluster can retry without a restart, and Biebie Access asking this window to
// open a cluster without going through the desktop's URL handler.
type Server struct {
	rpc *ipc.Server

	onSessionChanged func(bctx.AccessSessionChanged)
	onOpenLink       func(string)
}

// ServerOptions wires the callbacks the application supplies.
type ServerOptions struct {
	// OnSessionChanged is called when Biebie Access reports a profile moved.
	OnSessionChanged func(bctx.AccessSessionChanged)

	// OnOpenLink is called when Biebie Access sends a deep link over IPC
	// instead of launching one through the desktop.
	OnOpenLink func(link string)

	// OnError observes serving failures. It must never be given a payload.
	OnError func(error)
}

// NewServer prepares the endpoint. Nothing is bound until Start.
func NewServer(opts ServerOptions) (*Server, error) {
	endpoint, err := ipc.KubeEndpoint()
	if err != nil {
		return nil, err
	}

	rpc := ipc.NewServer(endpoint)
	rpc.OnError = opts.OnError

	s := &Server{
		rpc:              rpc,
		onSessionChanged: opts.OnSessionChanged,
		onOpenLink:       opts.OnOpenLink,
	}

	rpc.Handle(ipc.MethodAccessSessionChanged, s.handleSessionChanged)
	rpc.Handle(ipc.Method("app.open"), s.handleOpenLink)

	return s, nil
}

// Start binds the endpoint.
//
// Failing to bind is not fatal: it usually means another instance is already
// running, and the caller forwards its deep link to that one instead.
func (s *Server) Start() error { return s.rpc.Start() }

// Close releases the endpoint.
func (s *Server) Close() error { return s.rpc.Close() }

func (s *Server) handleSessionChanged(_ context.Context, params json.RawMessage) (any, error) {
	var event bctx.AccessSessionChanged
	if err := json.Unmarshal(params, &event); err != nil {
		return nil, fmt.Errorf("malformed session event")
	}
	if event.ProfileID == "" {
		return nil, fmt.Errorf("session event is missing a profile")
	}
	if s.onSessionChanged != nil {
		// The notification is acknowledged immediately; retrying a cluster can
		// take seconds and must not hold Biebie Access's call open.
		go s.onSessionChanged(event)
	}
	return map[string]bool{"accepted": true}, nil
}

func (s *Server) handleOpenLink(_ context.Context, params json.RawMessage) (any, error) {
	var req struct {
		Link string `json:"link"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("malformed open request")
	}
	// Re-parsing here rejects anything that is not one of our own links before
	// it reaches the application, including a link carrying query parameters
	// that look like credentials.
	if _, err := deeplink.ParseOpen(req.Link); err != nil {
		return nil, err
	}
	if s.onOpenLink != nil {
		go s.onOpenLink(req.Link)
	}
	return map[string]bool{"accepted": true}, nil
}
