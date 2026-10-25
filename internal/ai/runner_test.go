package ai

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeEmitter struct {
	mu     sync.Mutex
	events []string
	chunks strings.Builder
}

func (f *fakeEmitter) Emit(event string, data any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
	if event == EventChunk {
		if m, ok := data.(map[string]string); ok {
			f.chunks.WriteString(m["text"])
		}
	}
}

func TestRunnerStreamsAndCompletes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script helper")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-ai")
	content := "#!/bin/sh\nprintf 'line1\\nline2'\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	emit := &fakeEmitter{}
	r := NewRunner(emit)
	err := r.Run(context.Background(), "sess1", []string{script}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(emit.chunks.String(), "line1") {
		t.Fatalf("chunks: %q events %v", emit.chunks.String(), emit.events)
	}
	if emit.events[len(emit.events)-1] != EventDone {
		t.Fatalf("expected done, got %v", emit.events)
	}
}

func TestRunnerCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script helper")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "slow-ai")
	content := "#!/bin/sh\nsleep 30\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	emit := &fakeEmitter{}
	r := NewRunner(emit)
	done := make(chan error, 1)
	go func() {
		done <- r.Run(context.Background(), "sess2", []string{script}, "")
	}()
	time.Sleep(200 * time.Millisecond)
	r.Cancel("sess2")
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancel error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop after cancel")
	}
}
