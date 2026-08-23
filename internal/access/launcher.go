package access

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"biebie.net/protocol/deeplink"
)

// Launcher opens Biebie Access from Biebie Kube.
//
// The only thing that travels is a profile identifier. A deep link is visible
// to the shell and to anything that logs process arguments, so it can never
// carry a password, a token or an OTP — and the protocol's own parser refuses
// a link that tries.
type Launcher struct {
	// open is indirected so tests do not launch applications.
	open func(ctx context.Context, url string) error
}

// NewLauncher creates a launcher for this platform.
func NewLauncher() *Launcher { return &Launcher{open: openURL} }

// ConnectProfile asks the desktop to open Biebie Access at a profile.
func (l *Launcher) ConnectProfile(ctx context.Context, profileID, customerID string) error {
	link, err := deeplink.ConnectAccess(profileID, customerID)
	if err != nil {
		return err
	}
	if err := l.open(ctx, link); err != nil {
		return fmt.Errorf("open Biebie Access: %w", err)
	}
	return nil
}

// launchGrace is how long the desktop is given to reject a URL.
//
// The helpers below hand the URL over and exit within milliseconds, so this is
// generous for the only question being asked: did anything claim the scheme?
const launchGrace = 2 * time.Second

// openURL hands a URL to the platform's registered handler.
//
// The handler is given a moment to fail. A non-zero exit inside that window
// means no application claimed the scheme, which is a launch failure the caller
// must hear about — reporting success would leave the engineer looking at a
// button that did nothing. A handler still running afterwards has accepted the
// URL and is deliberately not waited on, because it may outlive this call.
func openURL(ctx context.Context, url string) error {
	switch runtime.GOOS {
	case "darwin":
		return runHandler(ctx, exec.CommandContext(ctx, "/usr/bin/open", url))
	case "windows":
		// rundll32 is used rather than `cmd /c start`, which would treat the
		// URL as a shell token and mangle its query string.
		return runHandler(ctx, exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url))
	default:
		return runHandler(ctx, exec.CommandContext(ctx, "xdg-open", url))
	}
}

// runHandler starts a URL handler and waits only long enough to catch it
// failing. It is separate from openURL so the waiting rule can be tested
// without a registered URL scheme.
func runHandler(_ context.Context, cmd *exec.Cmd) error {
	// The handler explains itself on stderr — "No application knows how to open
	// URL biebie-access://..." — which is more use to the engineer than an exit
	// status.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	// Buffered, so this goroutine finishes whether or not anyone is listening.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	select {
	case err := <-exited:
		if err == nil {
			return nil
		}
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return errors.New(detail)
		}
		return err
	case <-time.After(launchGrace):
		return nil
	}
}

// Installed reports whether Biebie Access appears to be installed, so the UI
// can offer installation instructions instead of a button that does nothing.
func Installed() bool {
	switch runtime.GOOS {
	case "darwin":
		for _, path := range []string{
			"/Applications/Biebie Access.app",
			"/Applications/biebie-access.app",
		} {
			if pathExists(path) {
				return true
			}
		}
		return false
	case "windows":
		return pathExists(`C:\Program Files\Biebie Access\Biebie Access.exe`)
	default:
		path, err := exec.LookPath("biebie-access")
		return err == nil && strings.TrimSpace(path) != ""
	}
}
