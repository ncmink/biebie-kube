package ai

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
)

const (
	EventChunk = "ai:chunk"
	EventDone  = "ai:done"
	EventError = "ai:error"

	runTimeout = 5 * time.Minute
	chunkSize  = 4096
)

// Emitter delivers streaming output to the UI.
type Emitter interface {
	Emit(event string, data any)
}

// Runner executes one CLI invocation.
type Runner struct {
	emitter Emitter
	mu      sync.Mutex
	active  map[string]context.CancelFunc
}

// NewRunner wires a runner.
func NewRunner(emitter Emitter) *Runner {
	return &Runner{emitter: emitter, active: make(map[string]context.CancelFunc)}
}

// Cancel stops a session.
func (r *Runner) Cancel(sessionID string) {
	r.mu.Lock()
	cancel, ok := r.active[sessionID]
	r.mu.Unlock()
	if ok {
		cancel()
	}
}

// CloseAll cancels every in-flight run.
func (r *Runner) CloseAll() {
	r.mu.Lock()
	cancels := r.active
	r.active = make(map[string]context.CancelFunc)
	r.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// Run starts argv with optional stdin body; emits chunk/done/error events.
func (r *Runner) Run(parent context.Context, sessionID string, argv []string, stdinBody string) error {
	if len(argv) == 0 {
		return errors.New("no command to run")
	}

	workDir, err := os.MkdirTemp("", "biebie-kube-ai-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	ctx, cancel := context.WithTimeout(parent, runTimeout)
	r.mu.Lock()
	r.active[sessionID] = cancel
	r.mu.Unlock()
	defer func() {
		cancel()
		r.mu.Lock()
		delete(r.active, sessionID)
		r.mu.Unlock()
	}()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = workDir
	cmd.Env = filteredEnvironment()
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	if stdinBody != "" {
		cmd.Stdin = bytes.NewReader([]byte(stdinBody))
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = killProcess(cmd)
	}()

	reader := bufio.NewReaderSize(stdout, chunkSize)
	for {
		chunk := make([]byte, chunkSize)
		n, readErr := reader.Read(chunk)
		if n > 0 {
			r.emitter.Emit(EventChunk, map[string]string{
				"sessionId": sessionID,
				"text":      string(chunk[:n]),
			})
		}
		if readErr != nil {
			if readErr != io.EOF {
				waitErr := cmd.Wait()
				msg := readErr.Error()
				if waitErr != nil {
					msg = waitErr.Error()
				}
				r.emitter.Emit(EventError, map[string]string{
					"sessionId": sessionID,
					"error":     msg,
				})
				return fmt.Errorf("ai run: %w", readErr)
			}
			break
		}
	}

	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		r.emitter.Emit(EventError, map[string]string{
			"sessionId": sessionID,
			"error":     "cancelled",
		})
		return ctx.Err()
	}
	if waitErr != nil {
		r.emitter.Emit(EventError, map[string]string{
			"sessionId": sessionID,
			"error":     waitErr.Error(),
		})
		return waitErr
	}
	r.emitter.Emit(EventDone, map[string]string{"sessionId": sessionID})
	return nil
}

// KillProcessGroup stops the child process group on Unix.
func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		return cmd.Process.Kill()
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// ExecutablePath validates a custom command path.
func ExecutablePath(path string) (string, error) {
	path = filepath.Clean(path)
	if path == "" {
		return "", errors.New("path is empty")
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("custom command path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", errors.New("path is a directory")
	}
	return path, nil
}
