// Package logs streams container logs from Kubernetes to the log viewer.
//
// The shape of this package is dictated by one fact: a busy container emits
// far more lines per second than a UI can render. Lines are therefore read on
// a goroutine, batched on a short timer, and delivered as chunks. Nothing
// keeps an unbounded history in memory — not here, and not in the frontend.
package logs

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/google/uuid"

	"biebie-kube/internal/cluster"
	"biebie-kube/internal/domain"
)

// Delivery pacing. A chunk goes out when either bound is hit, so a chatty
// container is batched and a quiet one still feels live.
const (
	flushInterval = 120 * time.Millisecond
	flushLines    = 200

	// maxLineBytes truncates a single pathological line — a stack trace
	// printed as one JSON string, say — instead of letting it grow the buffer
	// without limit.
	maxLineBytes = 64 * 1024
)

// Emitter delivers chunks to the frontend.
type Emitter interface {
	Emit(event string, data any)
}

// EventChunk is the Wails event carrying log output.
const EventChunk = "logs:chunk"

// Service owns running log streams.
type Service struct {
	clusters *cluster.Manager
	emitter  Emitter

	mu      sync.Mutex
	streams map[string]context.CancelFunc
}

// NewService wires the service.
func NewService(clusters *cluster.Manager, emitter Emitter) *Service {
	return &Service{
		clusters: clusters,
		emitter:  emitter,
		streams:  make(map[string]context.CancelFunc),
	}
}

// Start opens a stream and returns its identifier.
//
// The identifier is how the frontend routes chunks to the right viewer and how
// it stops the stream when the tab closes.
func (s *Service) Start(ctx context.Context, clusterID string, opts domain.LogOptions) (string, error) {
	opts, err := opts.Normalise()
	if err != nil {
		return "", err
	}
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return "", err
	}

	// The streaming client is required here rather than preferred: a followed
	// stream stays open for as long as the user watches it, which is longer
	// than any bound an ordinary request should carry.
	request := client.StreamClientset.CoreV1().Pods(opts.Namespace).GetLogs(opts.Pod, &corev1.PodLogOptions{
		Container:  opts.Container,
		Follow:     opts.Follow,
		Timestamps: opts.Timestamps,
		TailLines:  &opts.TailLines,
		Previous:   opts.Previous,
	})

	// The stream lives past this call, so it gets its own cancellable context
	// rather than the caller's, which Wails ends when the binding returns.
	streamCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

	reader, err := request.Stream(streamCtx)
	if err != nil {
		cancel()
		return "", describeLogError(err, opts)
	}

	streamID := "log_" + uuid.NewString()

	s.mu.Lock()
	s.streams[streamID] = cancel
	s.mu.Unlock()

	go s.pump(streamCtx, streamID, reader)

	return streamID, nil
}

// Stop ends a stream. Stopping an already-finished stream is not an error: the
// container may have exited a moment before the user pressed the button.
func (s *Service) Stop(streamID string) {
	s.mu.Lock()
	cancel, ok := s.streams[streamID]
	delete(s.streams, streamID)
	s.mu.Unlock()

	if ok {
		cancel()
	}
}

// StopAll ends every stream, on cluster disconnect or shutdown.
func (s *Service) StopAll() {
	s.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.streams))
	for id, cancel := range s.streams {
		cancels = append(cancels, cancel)
		delete(s.streams, id)
	}
	s.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}

// Snapshot reads a bounded slice of logs without following, for a download or
// a one-off look.
func (s *Service) Snapshot(ctx context.Context, clusterID string, opts domain.LogOptions) (string, error) {
	opts.Follow = false
	opts, err := opts.Normalise()
	if err != nil {
		return "", err
	}
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return "", err
	}

	raw, err := client.Clientset.CoreV1().Pods(opts.Namespace).GetLogs(opts.Pod, &corev1.PodLogOptions{
		Container:  opts.Container,
		Timestamps: opts.Timestamps,
		TailLines:  &opts.TailLines,
		Previous:   opts.Previous,
	}).DoRaw(ctx)
	if err != nil {
		return "", describeLogError(err, opts)
	}
	return string(raw), nil
}

// pump reads the stream and emits batched chunks until it ends or is stopped.
func (s *Service) pump(ctx context.Context, streamID string, reader io.ReadCloser) {
	defer func() {
		_ = reader.Close()
		s.mu.Lock()
		delete(s.streams, streamID)
		s.mu.Unlock()
	}()

	lines := make(chan string, 512)
	readErr := make(chan error, 1)

	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 0, 8*1024), maxLineBytes)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			readErr <- err
		}
	}()

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	batch := make([]string, 0, flushLines)
	flush := func(done bool, failure string) {
		if len(batch) == 0 && !done && failure == "" {
			return
		}
		chunk := domain.LogChunk{StreamID: streamID, Done: done, Error: failure}
		if len(batch) > 0 {
			chunk.Lines = append([]string(nil), batch...)
			batch = batch[:0]
		}
		s.emitter.Emit(EventChunk, chunk)
	}

	for {
		select {
		case <-ctx.Done():
			flush(true, "")
			return

		case err := <-readErr:
			flush(true, err.Error())
			return

		case line, ok := <-lines:
			if !ok {
				flush(true, "")
				return
			}
			batch = append(batch, line)
			if len(batch) >= flushLines {
				flush(false, "")
			}

		case <-ticker.C:
			flush(false, "")
		}
	}
}

// describeLogError turns a Kubernetes error into something the log pane can
// show without the user opening a terminal to find out what happened.
func describeLogError(err error, opts domain.LogOptions) error {
	switch {
	case errors.IsNotFound(err):
		return fmt.Errorf("pod %s no longer exists", opts.Pod)
	case errors.IsBadRequest(err) && opts.Previous:
		return fmt.Errorf("container %s has no previous instance to read", opts.Container)
	case errors.IsBadRequest(err):
		return fmt.Errorf("container %s is not ready to serve logs yet", opts.Container)
	case errors.IsForbidden(err):
		return fmt.Errorf("these credentials may not read logs in %s", opts.Namespace)
	default:
		return fmt.Errorf("read logs: %w", err)
	}
}
