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

// ProbeTimeout bounds a question asked only to find out whether a repository
// can be reached at all.
//
// Shorter than Timeout on purpose. A clone is allowed to be slow because it is
// moving a repository; a diagnostic is a person waiting in front of a panel to
// be told what is wrong, and one that takes a minute and a half to say
// "unreachable" has answered a question they stopped asking.
const ProbeTimeout = 20 * time.Second

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

	// ErrorAuth is the server declining to say who we are. It is separate from
	// ErrorNoRepository because a key the server does not know and a key it
	// knows but has not been given this repository are different problems with
	// different people to go and ask.
	ErrorAuth ErrorKind = "auth"

	// ErrorHostKey is ssh refusing the host rather than the host refusing us.
	// It sits on its own because the fix is never a credential, and because
	// the convenient fix — turning host key checking off — is the one thing
	// this application will not do on somebody's behalf.
	ErrorHostKey ErrorKind = "hostKey"

	// ErrorUnreachable is nothing answering at the other end.
	ErrorUnreachable ErrorKind = "unreachable"

	// ErrorTimeout is something answering too slowly to wait for. Separate
	// from unreachable: a host that is merely slow is worth pressing again,
	// and one that does not exist is not.
	ErrorTimeout ErrorKind = "timeout"

	// ErrorNoRepository is the server saying no to this repository. The name
	// is older than what is now known about it — several servers answer
	// "missing" and "not yours" with the same sentence on purpose, so that a
	// stranger cannot map a private namespace by watching which error comes
	// back. Anything worded for a person must keep both possibilities open.
	ErrorNoRepository ErrorKind = "noRepository"

	ErrorNoRevision ErrorKind = "noRevision"
	ErrorFailed     ErrorKind = "failed"
)

// Error is a git failure with the reason separated from the wording.
//
// The message has already been through Scrub, so it is safe to show and safe
// to log. The kind is what decides whether a retry could ever help.
type Error struct {
	Kind    ErrorKind
	Message string

	// Output is everything git wrote, scrubbed but otherwise untouched.
	//
	// Message is one line chosen out of this by a rule that cannot be right
	// about output nobody has seen yet — ssh alone grows new advisories
	// between releases. Carrying the whole of it means a person reading an
	// unfamiliar failure can see what git actually said, rather than trusting
	// that the line this package picked was the one that mattered.
	Output string
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
	return runWithin(ctx, Timeout, dir, args...)
}

// runWithin is run with a bound of its own, for callers whose question is
// worth less waiting than a clone is.
func runWithin(ctx context.Context, limit time.Duration, dir string, args ...string) ([]byte, error) {
	binary, err := Look()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, limit)
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
//
// Everything git said is searched, not only the line that gets shown. `git
// clone` announces itself on stderr before it does anything — "Cloning into
// bare repository '…'" — so on a clone the explanation is never the first
// line, and looking only there classified every failed clone as "something
// went wrong" and showed the announcement as the reason.
func classify(ctx context.Context, err error, stderr string) error {
	if ctx.Err() != nil {
		return &Error{Kind: ErrorTimeout, Message: "Git took too long to answer and was stopped."}
	}

	said := strings.TrimSpace(Scrub(stderr))
	text := reason(said)

	switch lower := strings.ToLower(said); {
	// Host key first. ssh reports it as a permission problem as well, and
	// reading it as one would send somebody to look at a key of their own
	// when what changed was the server's.
	case mentions(lower, "host key verification failed", "remote host identification has changed",
		"no matching host key type"):
		return &Error{Kind: ErrorHostKey, Message: text, Output: said}

	// Then the repository, before authentication. A server that says "no such
	// project, or not yours" has already let us in — it is answering about
	// authorisation — and ssh follows that answer with its own "could not read
	// from remote repository", which reads like a credential problem and is
	// not one.
	case mentions(lower, "repository not found", "does not appear to be a git repository",
		"remote error: upload-pack", "access denied",
		// GitLab, deliberately ambiguous between missing and forbidden.
		"could not be found or you don't have permission",
		// Bitbucket and several enterprise servers.
		"you may not have access to this repository"):
		return &Error{Kind: ErrorNoRepository, Message: text, Output: said}

	case mentions(lower, "authentication failed", "could not read username", "could not read password",
		"permission denied (publickey", "invalid username or password", "terminal prompts disabled",
		"authentication is not supported"):
		return &Error{Kind: ErrorAuth, Message: text, Output: said}

	case mentions(lower, "connection timed out", "operation timed out"):
		return &Error{Kind: ErrorTimeout, Message: text, Output: said}

	case mentions(lower, "could not resolve host", "could not resolve hostname", "connection refused",
		"network is unreachable", "no route to host", "failed to connect"):
		return &Error{Kind: ErrorUnreachable, Message: text, Output: said}

	case mentions(lower, "couldn't find remote ref", "unknown revision", "not a valid object name",
		"did not match any file"):
		return &Error{Kind: ErrorNoRevision, Message: text, Output: said}
	}

	if text == "" {
		return &Error{Kind: ErrorFailed, Message: fmt.Sprintf("Git failed: %v.", err), Output: said}
	}
	return &Error{Kind: ErrorFailed, Message: text, Output: said}
}

func mentions(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

// chatter is what git and ssh say while they are working rather than about
// what went wrong.
//
// Progress goes to stderr alongside errors, so a failed clone's output begins
// with the announcement of the clone and only later says why it stopped.
// Showing the first line meant showing a cache path and the word "Cloning" to
// somebody whose key was not loaded.
//
// The `**` entry is OpenSSH's advisory prefix. Version 10 warns on every
// connection to a server that has no post-quantum key exchange, which is most
// git hosts today: it is a true thing to say about the connection and no part
// of why a command failed.
var chatter = []string{
	"**",
	"cloning into",
	"enumerating objects",
	"counting objects",
	"compressing objects",
	"receiving objects",
	"resolving deltas",
	"updating files",
	"total ",
	"warning:",
	"hint:",
}

// reason takes the sentence worth showing out of git's output.
//
// git surrounds the line that matters with progress before it and hints, a
// blank line and a suggestion to read a manual page after it. What is wanted is
// the first line that is neither, and the cap is there because a server may
// answer with a page of HTML.
//
// `remote:` is dropped along with `fatal:`. Both say where a sentence came
// from rather than what it says, and a panel that leads with "remote: The
// project you were looking for…" is quoting a protocol at somebody.
func reason(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimPrefix(line, "remote:"))
		line = strings.TrimSpace(strings.TrimPrefix(line, "fatal:"))
		if line == "" || isChatter(line) || isRule(line) {
			continue
		}
		if len(line) > 300 {
			line = line[:300] + "…"
		}
		return line
	}
	return ""
}

func isChatter(line string) bool {
	lower := strings.ToLower(line)
	for _, noise := range chatter {
		if strings.HasPrefix(lower, noise) {
			return true
		}
	}
	return false
}

// isRule spots the row of punctuation servers draw around a message.
//
// GitLab boxes its refusal in `=====` lines forty characters wide. They are
// not empty, so skipping blanks does not skip them, and the first of them
// arrives before the sentence it is decorating — which made a row of equals
// signs the reason the comparison could not run.
func isRule(line string) bool {
	for _, r := range line {
		if r != '=' && r != '-' && r != '_' && r != '*' && r != '~' {
			return false
		}
	}
	return true
}
