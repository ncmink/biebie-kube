package access

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

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

// openURL hands a URL to the platform's registered handler.
func openURL(ctx context.Context, url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "/usr/bin/open", url)
	case "windows":
		// rundll32 is used rather than `cmd /c start`, which would treat the
		// URL as a shell token and mangle its query string.
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// The handler outlives this call; waiting on it would block until the user
	// closes the other application.
	go func() { _ = cmd.Wait() }()
	return nil
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
