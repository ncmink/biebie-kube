package git

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// This file holds the questions asked when a read has already failed and
// somebody wants to know which part of the path is broken.
//
// Every one of them is cheaper than the read that failed, and none of them
// changes anything: a repository is asked for its list of refs rather than
// cloned, a host is asked whether it is listening rather than talked to, and
// ssh is asked who it thinks we are rather than asked for a repository. The
// point is to separate "the server does not know you" from "the server knows
// you and this repository is not yours", which is a distinction the one error
// message from a failed clone does not make.

// dialTimeout bounds the test of whether a host is listening at all.
//
// Short, because it is answering a yes-or-no question about a socket. A host
// that has not accepted a connection in five seconds is not going to make the
// rest of the diagnosis any better by being waited for.
const dialTimeout = 5 * time.Second

// Listening reports whether anything accepts connections at the remote's host
// and port.
//
// It proves less than it looks like it proves. A host that answers may still
// refuse the key, and a host that does not answer here may be reachable
// through a ProxyCommand or a jump host that ssh knows about and this dial
// does not. So a failure is worth reporting and a success is worth reporting,
// and neither is worth concluding much from on its own.
func Listening(ctx context.Context, remote Remote) error {
	address := remote.Address()
	if address == "" {
		return &Error{Kind: ErrorUnsupported, Message: "This repository URL does not name a host to test."}
	}

	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	var dialer net.Dialer
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		if ctx.Err() != nil {
			return &Error{Kind: ErrorTimeout, Message: remote.Host() + " did not answer in time."}
		}
		return &Error{Kind: ErrorUnreachable, Message: Scrub(err.Error())}
	}
	return connection.Close()
}

// Readable asks the server for the repository's refs.
//
// `ls-remote` is the cheapest thing that proves read access: it is one
// round trip, it transfers no objects, and it is what the server would refuse
// first if the repository were missing or not ours. Diagnosing an access
// problem by cloning again would be repeating the expensive failure in order
// to learn what the cheap one already knows.
func Readable(ctx context.Context, remote Remote) error {
	// HEAD rather than nothing: a bare ls-remote lists every ref in the
	// repository, and a repository with a hundred thousand tags answers a
	// yes-or-no question with a megabyte.
	_, err := runWithin(ctx, ProbeTimeout, "", "ls-remote", "--quiet", "--", remote.String(), "HEAD")
	return err
}

// Identification is what a git host said when asked who is calling.
type Identification struct {
	// Account is the name the server used, set only when the server said one
	// in a form this code recognises. An empty account with no error means
	// authentication worked and the server did not introduce us to ourselves.
	Account string

	// Greeting is the scrubbed line the server sent, kept so a person can read
	// what a server this code does not recognise actually replied.
	Greeting string
}

// greetings are the forms a git host uses to name the account it authenticated.
//
// Deliberately short. Every entry here is a promise that a string captured
// from a server is a username, and a pattern that is nearly right turns some
// other word into somebody's account name in the UI. A server whose reply is
// not on this list is reported as unrecognised, which is true, rather than
// guessed at, which would not be.
var greetings = []*regexp.Regexp{
	// GitHub: "Hi octocat! You've successfully authenticated, but GitHub does
	// not provide shell access."
	regexp.MustCompile(`^Hi ([A-Za-z0-9][A-Za-z0-9._-]*)! You've successfully authenticated`),

	// GitLab: "Welcome to GitLab, @octocat!"
	regexp.MustCompile(`^Welcome to GitLab, @([A-Za-z0-9][A-Za-z0-9._-]*)!`),

	// Bitbucket Cloud: "authenticated via ssh key."  — preceded by the account
	// on its own line, which this does not try to reconstruct.
	regexp.MustCompile(`^logged in as ([A-Za-z0-9][A-Za-z0-9._-]*)\.`),
}

// Identify asks an ssh host to authenticate without asking it for anything.
//
// Most git hosts answer `ssh -T git@host` with a sentence naming the account
// they authenticated, then close the connection with a non-zero status because
// they do not offer a shell. That non-zero status is the normal outcome and is
// not treated as failure here — what is read is what the server said.
//
// The account is only reported when the reply matches a form this code knows.
// Two engineers with two GitLab accounts and one agent is the situation this
// exists for, and telling one of them the wrong account name would be worse
// than telling them nothing.
func Identify(ctx context.Context, remote Remote) (Identification, error) {
	target := remote.SSHTarget()
	if target == "" {
		return Identification{}, &Error{
			Kind:    ErrorUnsupported,
			Message: "This repository is not reached over ssh, so there is no ssh identity to test.",
		}
	}

	binary, err := exec.LookPath("ssh")
	if err != nil {
		return Identification{}, &Error{
			Kind:    ErrorMissing,
			Message: "The ssh command is not on the PATH this application can see.",
		}
	}

	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()

	// BatchMode rather than StrictHostKeyChecking: this must never wait for a
	// passphrase or a yes/no about a host key, and it must never answer either
	// of them on somebody's behalf. A host this machine has not accepted is a
	// finding, not an obstacle to work around.
	command := exec.CommandContext(ctx, binary, "-T", "-o", "BatchMode=yes", "--", target)
	command.Env = environment()

	// ssh says all of this on stderr, including the greeting.
	output, _ := command.CombinedOutput()
	said := strings.TrimSpace(Scrub(string(output)))

	if ctx.Err() != nil {
		return Identification{}, &Error{Kind: ErrorTimeout, Message: target + " did not answer in time.", Output: said}
	}

	lower := strings.ToLower(said)
	switch {
	case mentions(lower, "host key verification failed", "remote host identification has changed",
		"no matching host key type"):
		return Identification{}, &Error{Kind: ErrorHostKey, Message: reason(said), Output: said}
	case mentions(lower, "permission denied", "no supported authentication methods"):
		return Identification{}, &Error{Kind: ErrorAuth, Message: reason(said), Output: said}
	case mentions(lower, "could not resolve host", "could not resolve hostname", "connection refused",
		"network is unreachable", "no route to host"):
		return Identification{}, &Error{Kind: ErrorUnreachable, Message: reason(said), Output: said}
	}

	found := Identification{Greeting: reason(said)}
	for _, line := range strings.Split(said, "\n") {
		line = strings.TrimSpace(line)
		for _, pattern := range greetings {
			if match := pattern.FindStringSubmatch(line); match != nil {
				found.Account = match[1]
				found.Greeting = line
				return found, nil
			}
		}
	}
	if found.Greeting == "" {
		// Nothing recognisable and nothing that reads as a failure. Saying so
		// is the honest answer; inventing a success is not.
		return Identification{}, &Error{
			Kind:    ErrorFailed,
			Message: "The host answered without saying anything this application could read.",
			Output:  said,
		}
	}
	return found, nil
}

// AgentRunning reports whether an ssh-agent is visible to this process.
//
// It is worth asking because this is a window rather than a terminal. A key
// added with `ssh-add` reaches an agent, the agent is found through an
// environment variable, and a desktop application started by launchd or a
// session manager does not always inherit the same environment a shell has.
//
// A false here is not a diagnosis. Authentication with an IdentityFile and no
// passphrase never touches an agent, and works perfectly well without one.
func AgentRunning() bool {
	if os.Getenv("SSH_AUTH_SOCK") != "" {
		return true
	}
	if runtime.GOOS == "windows" {
		// Win32-OpenSSH uses a named pipe at a fixed address rather than a
		// variable, so there is nothing in the environment to look at.
		_, err := os.Stat(`\\.\pipe\openssh-ssh-agent`)
		return err == nil
	}
	return false
}

// SSHConfig is where this platform keeps the per-user ssh configuration.
//
// Returned rather than assumed. `~/.ssh/config` is right on macOS and Linux
// and reads as a mistake on Windows, and a path is exactly the sort of thing
// that gets hardcoded once in a Vue file and then is wrong for a third of the
// people using it.
func SSHConfig() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "config"), nil
}

// Command spells the equivalent of Readable for somebody to run themselves.
//
// The value of this is not the command; it is the comparison. A person who
// runs it in their own terminal learns whether their shell can read the
// repository when this application cannot, and that single fact separates a
// credential problem from an environment one.
//
// The URL is scrubbed, and there is nothing else in it to leak: this
// application holds no credential to put in a command in the first place.
func Command(remote Remote) string {
	return "git ls-remote " + quote(remote.Display()) + " HEAD"
}

// quote wraps a URL in single quotes when a shell would otherwise take it
// apart. It is for legibility in something a person pastes, and is not what
// keeps this application safe — nothing here is ever run through a shell.
func quote(value string) string {
	if !strings.ContainsAny(value, " \t\"'\\$`|&;<>()*?[]{}!#~") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
