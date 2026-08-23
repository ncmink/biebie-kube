package access

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// A launch failure used to be discarded, so Biebie Kube reported success for a
// button that did nothing. These tests pin the failure to the caller.
func TestLaunchFailureReachesTheCaller(t *testing.T) {
	launcher := &Launcher{
		open: func(context.Context, string) error {
			return errors.New("No application knows how to open URL biebie-access://connect")
		},
	}

	err := launcher.ConnectProfile(context.Background(), "smoi-vpn", "smoi")
	if err == nil {
		t.Fatal("a handler that refused the URL must not be reported as success")
	}
	if !strings.Contains(err.Error(), "No application knows how to open URL") {
		t.Fatalf("the handler's own explanation should survive, got %q", err)
	}
}

func TestSuccessfulLaunchReportsNoError(t *testing.T) {
	var opened string
	launcher := &Launcher{
		open: func(_ context.Context, url string) error {
			opened = url
			return nil
		},
	}

	if err := launcher.ConnectProfile(context.Background(), "smoi-vpn", "smoi"); err != nil {
		t.Fatalf("ConnectProfile: %v", err)
	}
	if !strings.HasPrefix(opened, "biebie-access://connect?") {
		t.Fatalf("expected a biebie-access connect link, got %q", opened)
	}
	// The profile travels; nothing else is allowed to.
	if !strings.Contains(opened, "profile=smoi-vpn") {
		t.Fatalf("link is missing the profile: %q", opened)
	}
}

func TestProfileIsRequiredBeforeAnythingIsLaunched(t *testing.T) {
	called := false
	launcher := &Launcher{
		open: func(context.Context, string) error {
			called = true
			return nil
		},
	}

	if err := launcher.ConnectProfile(context.Background(), "   ", "smoi"); err == nil {
		t.Fatal("a blank profile must be refused")
	}
	if called {
		t.Fatal("nothing should be launched when there is no profile to open")
	}
}

// runHandler is the part that used to swallow the exit status. A handler that
// fails immediately must surface, and its stderr is what explains why.
func TestHandlerFailureIsReported(t *testing.T) {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "sh", "-c", "echo nothing claims that scheme >&2; exit 1")

	err := runHandler(ctx, cmd)
	if err == nil {
		t.Fatal("a handler that exited non-zero must be reported as a failure")
	}
	if !strings.Contains(err.Error(), "nothing claims that scheme") {
		t.Fatalf("stderr should explain the failure, got %q", err)
	}
}

func TestHandlerStillRunningCountsAsAccepted(t *testing.T) {
	// A handler still running has taken the URL. Waiting for it would block
	// until the user closed the other application.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // kills the sleep rather than leaving it behind

	if err := runHandler(ctx, exec.CommandContext(ctx, "sleep", "30")); err != nil {
		t.Fatalf("a handler still running must be treated as success, got %v", err)
	}
}
