// Package terminal opens interactive shells inside containers.
//
// It bridges xterm.js to client-go's remotecommand executor. The one piece of
// judgement here is shell selection: a container image may have bash, only sh,
// or — in a distroless image — no shell at all, and assuming bash produces a
// confusing "executable file not found" instead of a usable session.
package terminal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/google/uuid"

	"biebie-kube/internal/cluster"
	"biebie-kube/internal/domain"
)

// candidateShells are tried in order of how pleasant they are to work in.
//
// The busybox paths matter: an Alpine-based image often has /bin/sh, while a
// scratch image built around busybox may only expose it under /busybox.
var candidateShells = [][]string{
	{"/bin/bash"},
	{"/bin/sh"},
	{"/usr/bin/bash"},
	{"/usr/bin/sh"},
	{"/busybox/sh"},
}

// EventChunk carries container output to the frontend.
const EventChunk = "terminal:chunk"

// probeTimeout bounds each shell existence check, so a container that hangs on
// exec does not stall the whole probe sequence.
const probeTimeout = 4 * time.Second

// Emitter delivers output to the frontend.
type Emitter interface {
	Emit(event string, data any)
}

// Service owns open exec sessions.
type Service struct {
	clusters *cluster.Manager
	emitter  Emitter

	mu       sync.Mutex
	sessions map[string]*execSession
}

type execSession struct {
	info   domain.TerminalSession
	cancel context.CancelFunc

	// stdin is the write end the frontend's keystrokes go to.
	stdin *io.PipeWriter

	// resize carries terminal size changes to the executor.
	resize chan remotecommand.TerminalSize
}

// NewService wires the service.
func NewService(clusters *cluster.Manager, emitter Emitter) *Service {
	return &Service{
		clusters: clusters,
		emitter:  emitter,
		sessions: make(map[string]*execSession),
	}
}

// Open starts a shell in a container.
func (s *Service) Open(ctx context.Context, clusterID string, req domain.TerminalRequest) (domain.TerminalSession, error) {
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return domain.TerminalSession{}, err
	}
	if req.Pod == "" {
		return domain.TerminalSession{}, errors.New("a pod is required")
	}

	command := req.Command
	if len(command) == 0 {
		command, err = s.detectShell(ctx, clusterID, req)
		if err != nil {
			return domain.TerminalSession{}, err
		}
	}

	stdinReader, stdinWriter := io.Pipe()
	sessionCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

	session := &execSession{
		info: domain.TerminalSession{
			ID:        "term_" + uuid.NewString(),
			ClusterID: clusterID,
			Namespace: req.Namespace,
			Pod:       req.Pod,
			Container: req.Container,
			Shell:     command[0],
			StartedAt: time.Now(),
		},
		cancel: cancel,
		stdin:  stdinWriter,
		resize: make(chan remotecommand.TerminalSize, 4),
	}

	executor, err := s.executor(clusterID, req, command, true)
	if err != nil {
		cancel()
		_ = stdinWriter.Close()
		return domain.TerminalSession{}, err
	}

	s.mu.Lock()
	s.sessions[session.info.ID] = session
	s.mu.Unlock()

	go s.run(sessionCtx, session, executor, stdinReader)

	_ = client
	return session.info, nil
}

// Write forwards keystrokes into the container.
func (s *Service) Write(sessionID, data string) error {
	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("this terminal session has ended")
	}
	_, err := session.stdin.Write([]byte(data))
	return err
}

// Resize tells the container's TTY the window changed, so full-screen tools
// such as top and vim redraw correctly.
func (s *Service) Resize(sessionID string, cols, rows uint16) {
	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return
	}
	select {
	case session.resize <- remotecommand.TerminalSize{Width: cols, Height: rows}:
	default:
		// A dropped resize is harmless: the next one carries the final size,
		// and blocking here would stall the UI thread during a window drag.
	}
}

// Close ends a session.
func (s *Service) Close(sessionID string) {
	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	delete(s.sessions, sessionID)
	s.mu.Unlock()

	if ok {
		_ = session.stdin.Close()
		session.cancel()
	}
}

// CloseAll ends every session, on disconnect or shutdown.
func (s *Service) CloseAll() {
	s.mu.Lock()
	sessions := make([]*execSession, 0, len(s.sessions))
	for id, session := range s.sessions {
		sessions = append(sessions, session)
		delete(s.sessions, id)
	}
	s.mu.Unlock()

	for _, session := range sessions {
		_ = session.stdin.Close()
		session.cancel()
	}
}

// Sessions lists open terminals, so the UI can restore them after a reload.
func (s *Service) Sessions() []domain.TerminalSession {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]domain.TerminalSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		out = append(out, session.info)
	}
	return out
}

func (s *Service) run(ctx context.Context, session *execSession, executor remotecommand.Executor, stdin io.Reader) {
	defer func() {
		s.mu.Lock()
		delete(s.sessions, session.info.ID)
		s.mu.Unlock()
	}()

	out := &chunkWriter{emit: func(data string) {
		s.emitter.Emit(EventChunk, domain.TerminalChunk{SessionID: session.info.ID, Data: data})
	}}

	err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            out,
		Stderr:            out,
		Tty:               true,
		TerminalSizeQueue: sizeQueue{ch: session.resize, done: ctx.Done()},
	})

	chunk := domain.TerminalChunk{SessionID: session.info.ID, Done: true}
	// A cancelled context means the user closed the tab, which is not a
	// failure worth showing them.
	if err != nil && ctx.Err() == nil {
		chunk.Error = err.Error()
	}
	s.emitter.Emit(EventChunk, chunk)
}

// detectShell finds a shell that actually exists in the container.
//
// Each candidate is tested with a trivial non-interactive exec; the first one
// that exits cleanly is used. If none do, the user is told plainly rather than
// being dropped into a session that immediately dies.
func (s *Service) detectShell(ctx context.Context, clusterID string, req domain.TerminalRequest) ([]string, error) {
	for _, candidate := range candidateShells {
		probe := append(append([]string{}, candidate...), "-c", "exit 0")

		executor, err := s.executor(clusterID, req, probe, false)
		if err != nil {
			return nil, err
		}

		probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		err = executor.StreamWithContext(probeCtx, remotecommand.StreamOptions{
			Stdout: io.Discard,
			Stderr: io.Discard,
		})
		cancel()

		if err == nil {
			return candidate, nil
		}
	}
	return nil, fmt.Errorf(
		"no shell found in this container; images built without a shell cannot be opened interactively")
}

func (s *Service) executor(
	clusterID string,
	req domain.TerminalRequest,
	command []string,
	tty bool,
) (remotecommand.Executor, error) {
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return nil, err
	}

	request := client.Clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Namespace(req.Namespace).
		Name(req.Pod).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: req.Container,
			Command:   command,
			Stdin:     tty,
			Stdout:    true,
			Stderr:    true,
			TTY:       tty,
		}, scheme.ParameterCodec)

	// The WebSocket executor is preferred and falls back to SPDY, because
	// SPDY is deprecated but still the only transport older clusters offer.
	executor, err := remotecommand.NewWebSocketExecutor(client.RESTConfig, "GET", request.URL().String())
	if err == nil {
		fallback, spdyErr := remotecommand.NewSPDYExecutor(client.RESTConfig, "POST", request.URL())
		if spdyErr == nil {
			return remotecommand.NewFallbackExecutor(executor, fallback, func(err error) bool {
				return httpstreamFallback(err)
			})
		}
		return executor, nil
	}

	executor, err = remotecommand.NewSPDYExecutor(client.RESTConfig, "POST", request.URL())
	if err != nil {
		return nil, fmt.Errorf("open exec session: %w", err)
	}
	return executor, nil
}

// httpstreamFallback reports whether an error means the cluster does not speak
// the WebSocket exec protocol.
func httpstreamFallback(err error) bool {
	if err == nil {
		return false
	}
	if meta.IsNoMatchError(err) {
		return true
	}
	message := err.Error()
	return bytes.Contains([]byte(message), []byte("Upgrade request required")) ||
		bytes.Contains([]byte(message), []byte("400 Bad Request"))
}

// chunkWriter turns container output into UI events.
type chunkWriter struct {
	emit func(string)
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.emit(string(p))
	}
	return len(p), nil
}

// sizeQueue feeds resize events to the executor and blocks until the session
// ends, which is the contract remotecommand expects.
type sizeQueue struct {
	ch   chan remotecommand.TerminalSize
	done <-chan struct{}
}

func (q sizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case size := <-q.ch:
		return &size
	case <-q.done:
		return nil
	}
}
