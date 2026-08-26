// Package shellenv recovers the PATH a person has in their terminal.
//
// A GUI application on macOS is started by launchd, not by a shell, so it
// inherits PATH=/usr/bin:/bin:/usr/sbin:/sbin and never sees ~/.zshrc. A
// kubeconfig exec plugin is almost always written as a bare command name — the
// EKS entries kubectl generates say `command: aws` — which client-go resolves
// through PATH. The result is that a cluster reachable from the terminal
// cannot be opened from the Dock, because the credential helper lives in
// /opt/homebrew/bin or ~/.local/bin and this process cannot see it.
//
// Asking the login shell what PATH it would have is the same approach Lens
// takes, and it is the only one that finds helpers installed by a version
// manager such as asdf or mise, whose directories are not knowable in advance.
package shellenv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Timeout bounds the shell invocation. A login shell that initialises a
// version manager can take a moment, but this runs before the window appears,
// so a shell wedged on a prompt or a slow network mount must not hold up the
// whole application.
const Timeout = 3 * time.Second

// Markers delimit the value inside whatever a login shell prints on startup.
// Instructions to run `brew upgrade`, motd banners and version manager notices
// all arrive on the same stream.
const (
	markerBegin = "__biebie_kube_path_begin__"
	markerEnd   = "__biebie_kube_path_end__"
)

// Apply merges the login shell's PATH into this process's PATH and reports the
// value it settled on.
//
// The current PATH is kept rather than replaced: the shell's answer goes first
// so a user's chosen version of a tool wins, but nothing already reachable
// becomes unreachable, which keeps a misconfigured rc file from making things
// worse than they were.
func Apply(ctx context.Context) (string, error) {
	current := os.Getenv("PATH")

	// Windows has no login shell and its GUI processes already inherit the
	// user's environment from Explorer, so there is nothing to recover.
	if runtime.GOOS == "windows" {
		return current, nil
	}

	fromShell, err := loginShellPath(ctx)
	if err != nil {
		return current, err
	}

	merged := merge(fromShell, current)
	if merged == current {
		return current, nil
	}
	if err := os.Setenv("PATH", merged); err != nil {
		return current, err
	}
	return merged, nil
}

// loginShellPath asks the user's shell for the PATH it exports.
func loginShellPath(ctx context.Context) (string, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return "", errors.New("SHELL is not set")
	}

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	// The PATH is read by a child /bin/sh rather than expanded by the login
	// shell itself, because $PATH is a list in fish and a colon-joined string
	// in sh, and only the exported form is the same everywhere. Sourcing the
	// rc files is the login shell's job; formatting the answer is not.
	inner := fmt.Sprintf("printf %s%%s%s \"$PATH\"", markerBegin, markerEnd)
	script := "/bin/sh -c '" + inner + "'"

	// -i sources the interactive file (~/.zshrc) and -l the login file
	// (~/.zprofile); tools put themselves in either one, so both are asked
	// for. Shells that reject the combination are retried with fewer flags
	// rather than reported as unavailable.
	var lastErr error
	for _, flags := range [][]string{{"-ilc"}, {"-lc"}, {"-ic"}, {"-c"}} {
		out, err := runShell(ctx, shell, append(flags, script))
		if err != nil {
			lastErr = err
			continue
		}
		if value, ok := extract(out); ok {
			return value, nil
		}
		lastErr = fmt.Errorf("%s %s printed no PATH", shell, strings.Join(flags, " "))
	}
	return "", lastErr
}

// runShell invokes the shell and returns its stdout.
//
// Stdin is left nil so the child reads /dev/null: an rc file that prompts
// would otherwise sit there until the timeout.
func runShell(ctx context.Context, shell string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, shell, args...)
	out, err := cmd.Output()
	if err != nil {
		// A non-zero exit is still worth reading: an rc file that fails on
		// its last line has usually already set PATH.
		if len(out) > 0 {
			if _, ok := extract(string(out)); ok {
				return string(out), nil
			}
		}
		return "", fmt.Errorf("run %s %s: %w", shell, strings.Join(args[:len(args)-1], " "), err)
	}
	return string(out), nil
}

// extract pulls the delimited value out of a shell's startup chatter.
func extract(out string) (string, bool) {
	start := strings.Index(out, markerBegin)
	if start < 0 {
		return "", false
	}
	rest := out[start+len(markerBegin):]
	stop := strings.Index(rest, markerEnd)
	if stop < 0 {
		return "", false
	}
	value := rest[:stop]
	if strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

// merge joins PATH lists in order, keeping the first occurrence of each
// directory.
//
// Relative and tilde entries are dropped: exec.LookPath does not expand `~`,
// and a relative entry would resolve against the working directory of a
// double-clicked application, which is the filesystem root.
func merge(lists ...string) string {
	var ordered []string
	seen := make(map[string]bool)
	for _, list := range lists {
		for _, dir := range filepath.SplitList(list) {
			if dir == "" || !filepath.IsAbs(dir) {
				continue
			}
			dir = filepath.Clean(dir)
			if seen[dir] {
				continue
			}
			seen[dir] = true
			ordered = append(ordered, dir)
		}
	}
	return strings.Join(ordered, string(filepath.ListSeparator))
}
