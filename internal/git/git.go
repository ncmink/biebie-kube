// Package git reads repositories through the git command already installed on
// the machine.
//
// Nothing here holds a credential, and that is the point of the design rather
// than a limitation of it. A repository is cloned with the URL as a person's
// own git would spell it, so authentication is whatever their credential
// helper, ssh-agent or keychain already does — the same as typing `git clone`
// in a terminal. An Argo CD Application sometimes stores a token inside its
// repository URL; that token is stripped before it reaches this package,
// because a process's arguments are readable by every other process on the
// machine and `ps` is not a place to put one.
//
// The consequence is worth stating plainly: a repository the person cannot
// reach from their own terminal is a repository this application cannot reach
// either, and it says so rather than finding another way in.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Timeout bounds one git invocation.
//
// The first clone of a large repository over a slow link is the long one;
// everything after it is local and immediate. The bound exists so a git
// waiting on a host that will never answer does not leave a button spinning
// for the rest of the session.
const Timeout = 90 * time.Second

// ErrorKind says why git refused, in terms the UI can turn into a sentence
// that names the thing to go and fix.
type ErrorKind string

// The reasons a read does not happen.
const (
	// ErrorMissing is git not being installed, which is a fact about the
	// machine rather than about the repository.
	ErrorMissing ErrorKind = "missing"

	// ErrorUnsupported is a URL, revision or path this application declines to
	// hand to git. It is a refusal rather than a failure.
	ErrorUnsupported ErrorKind = "unsupported"

	ErrorAuth         ErrorKind = "auth"
	ErrorUnreachable  ErrorKind = "unreachable"
	ErrorNoRepository ErrorKind = "noRepository"
	ErrorNoRevision   ErrorKind = "noRevision"
	ErrorFailed       ErrorKind = "failed"
)

// Error is a git failure with the reason separated from the wording.
//
// The message has already been through Scrub, so it is safe to show and safe
// to log. The kind is what decides whether a retry could ever help.
type Error struct {
	Kind    ErrorKind
	Message string
}

func (e *Error) Error() string { return e.Message }

// Look finds the git command.
//
// PATH has already been widened by internal/shellenv, which asks the login
// shell what it exports: a GUI application on macOS is started by launchd and
// would otherwise never see the git in /opt/homebrew/bin.
func Look() (string, error) {
	binary, err := exec.LookPath("git")
	if err != nil {
		return "", &Error{
			Kind:    ErrorMissing,
			Message: "Git is not installed on this machine, or is not on the PATH this application can see.",
		}
	}
	return binary, nil
}

// run invokes git in a directory and returns its standard output.
//
// The arguments are passed as a vector and never through a shell, so nothing
// in them is expanded, quoted or split — which is the other half of why the
// values in remote.go are types. Stdin is left nil: a git that decides to ask
// for a password reads end-of-file and gives up rather than waiting for
// somebody who is not there.
func run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	binary, err := Look()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = dir
	command.Env = environment()

	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		return nil, classify(ctx, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// classify turns git's exit into the reason a person can act on.
//
// Matching on the text of a message is fragile and is done anyway, because the
// exit status says only that git failed and the difference between "your key
// is not loaded" and "that host does not exist" is the whole of what the
// reader needs. LC_ALL is pinned in the environment so the strings matched
// here are the ones git actually prints.
func classify(ctx context.Context, err error, stderr string) error {
	if ctx.Err() != nil {
		return &Error{Kind: ErrorUnreachable, Message: "Git took too long to answer and was stopped."}
	}

	text := firstLine(Scrub(stderr))
	switch lower := strings.ToLower(text); {
	case mentions(lower, "authentication failed", "could not read username", "could not read password",
		"permission denied (publickey", "invalid username or password", "terminal prompts disabled",
		"authentication is not supported"):
		return &Error{Kind: ErrorAuth, Message: text}
	case mentions(lower, "could not resolve host", "connection refused", "connection timed out",
		"network is unreachable", "operation timed out", "no route to host", "failed to connect"):
		return &Error{Kind: ErrorUnreachable, Message: text}
	case mentions(lower, "repository not found", "does not appear to be a git repository",
		"remote error: upload-pack", "access denied"):
		return &Error{Kind: ErrorNoRepository, Message: text}
	case mentions(lower, "couldn't find remote ref", "unknown revision", "not a valid object name",
		"did not match any file"):
		return &Error{Kind: ErrorNoRevision, Message: text}
	}

	if text == "" {
		return &Error{Kind: ErrorFailed, Message: fmt.Sprintf("Git failed: %v.", err)}
	}
	return &Error{Kind: ErrorFailed, Message: text}
}

func mentions(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

// firstLine takes the sentence worth showing out of git's output.
//
// git prints hints, a blank line and a suggestion to read a manual page after
// the line that says what went wrong. The first non-empty line is the one a
// person needs, and the cap is there because a server may answer with a page
// of HTML.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "fatal: "))
		if line == "" {
			continue
		}
		if len(line) > 300 {
			line = line[:300] + "…"
		}
		return line
	}
	return ""
}
