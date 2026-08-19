package main

import (
	"context"

	"biebie-kube/internal/domain"
)

// LogService streams container output.
//
// Logs, terminals and port forwards are separate services rather than one
// "streams" service: they have separate lifetimes, and the frontend imports
// only what a given view needs.
type LogService struct{ core *Core }

func (s *LogService) ServiceName() string { return "LogService" }

// StartLogStream opens a log stream and returns its identifier.
//
// Output arrives as batched "logs:chunk" events rather than as a return value,
// because a followed log has no end.
func (s *LogService) StartLogStream(ctx context.Context, clusterID string, opts domain.LogOptions) (string, error) {
	streamID, err := s.core.logs.Start(ctx, clusterID, opts)
	return streamID, describe(err)
}

// StopLogStream ends a stream.
func (s *LogService) StopLogStream(streamID string) { s.core.logs.Stop(streamID) }

// DownloadLogs returns a bounded snapshot for saving to a file.
func (s *LogService) DownloadLogs(ctx context.Context, clusterID string, opts domain.LogOptions) (string, error) {
	text, err := s.core.logs.Snapshot(ctx, clusterID, opts)
	return text, describe(err)
}

// TerminalService runs interactive shells inside containers.
type TerminalService struct{ core *Core }

func (s *TerminalService) ServiceName() string { return "TerminalService" }

// OpenTerminal starts a shell in a container.
func (s *TerminalService) OpenTerminal(ctx context.Context, clusterID string, req domain.TerminalRequest) (domain.TerminalSession, error) {
	session, err := s.core.terminals.Open(ctx, clusterID, req)
	return session, describe(err)
}

// SendTerminalInput forwards keystrokes to a container.
func (s *TerminalService) SendTerminalInput(sessionID, data string) error {
	return describe(s.core.terminals.Write(sessionID, data))
}

// ResizeTerminal tells the container's TTY the window changed.
func (s *TerminalService) ResizeTerminal(sessionID string, cols, rows int) {
	s.core.terminals.Resize(sessionID, uint16(cols), uint16(rows))
}

// CloseTerminal ends a shell session.
func (s *TerminalService) CloseTerminal(sessionID string) { s.core.terminals.Close(sessionID) }

// ListTerminals reports open sessions, so the UI can restore them.
func (s *TerminalService) ListTerminals() []domain.TerminalSession {
	return s.core.terminals.Sessions()
}

// PortForwardService owns loopback ports that reach into clusters.
type PortForwardService struct{ core *Core }

func (s *PortForwardService) ServiceName() string { return "PortForwardService" }

// StartPortForward opens a loopback port that reaches into the cluster.
func (s *PortForwardService) StartPortForward(ctx context.Context, clusterID string, req domain.PortForwardRequest) (domain.PortForwardSession, error) {
	session, err := s.core.forwards.Start(ctx, clusterID, req)
	return session, describe(err)
}

// StopPortForward closes one forward.
func (s *PortForwardService) StopPortForward(id string) { s.core.forwards.Stop(id) }

// ListPortForwards reports running forwards across every cluster.
func (s *PortForwardService) ListPortForwards() []domain.PortForwardSession {
	return s.core.forwards.Sessions()
}
