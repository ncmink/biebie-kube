// Package reveal shows a file to somebody in their own file manager.
//
// It exists for one thing this application deliberately will not do, which is
// edit somebody's ssh configuration for them. Diagnosing a repository that
// cannot be read often ends at a file in ~/.ssh, and the useful thing to do
// with that conclusion is put the engineer in front of the file — not open it
// in an editor this application chose, and certainly not write to it.
//
// Revealing rather than opening is the deliberate choice. `~/.ssh/config` has
// no extension, so handing it to the platform's open command asks the desktop
// to guess which application should edit it, and the guess is wrong often
// enough to be worse than useless. Selecting it in a file manager is
// unambiguous on every platform and cannot launch the wrong thing.
package reveal

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// grace is how long the file manager is given to reject the path.
//
// The helpers hand the path over and exit immediately, so a non-zero status
// inside this window means nothing claimed it. One still running has taken the
// path and is not waited on.
const grace = 2 * time.Second

// Revealer shows paths to a person.
type Revealer struct {
	// show is indirected so tests do not open windows on the machine running
	// them.
	show func(ctx context.Context, path string) error
}

// New creates a revealer for this platform.
func New() *Revealer { return &Revealer{show: showPath} }

// Reveal selects a file in the platform's file manager.
//
// The path must be absolute and is never passed through a shell. It is not
// read, not written and not created: if it is not there, the directory holding
// it is shown instead, because "your ssh config does not exist yet" is a thing
// somebody needs to see rather than a thing to fix on their behalf.
func (r *Revealer) Reveal(ctx context.Context, path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("only an absolute path can be revealed")
	}
	if err := r.show(ctx, filepath.Clean(path)); err != nil {
		return fmt.Errorf("show %s: %w", filepath.Base(path), err)
	}
	return nil
}

func showPath(ctx context.Context, path string) error {
	switch runtime.GOOS {
	case "darwin":
		// -R selects the file in Finder rather than opening it, which is the
		// whole distinction this package is drawn around.
		return start(exec.CommandContext(ctx, "/usr/bin/open", "-R", path))
	case "windows":
		// The comma is Explorer's own syntax and not a shell separator; the
		// argument reaches it as one string.
		return start(exec.CommandContext(ctx, "explorer", "/select,"+path))
	default:
		// No portable "select this file" on Linux, and the desktops that do
		// support it disagree about how to ask. The directory is the reliable
		// answer everywhere.
		return start(exec.CommandContext(ctx, "xdg-open", filepath.Dir(path)))
	}
}

// start launches a file manager and waits only long enough to catch it
// failing, so a button that did nothing is reported rather than silently
// counted as success.
func start(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}

	// Buffered, so this goroutine finishes whether or not anyone is listening.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	select {
	case err := <-exited:
		return err
	case <-time.After(grace):
		// Still running, which means it took the path. Explorer in particular
		// stays alive for the window it opened.
		return nil
	}
}
