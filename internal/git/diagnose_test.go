package git

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestARemoteKnowsHowGitWillReachIt(t *testing.T) {
	// The transport is what decides which half of the diagnosis is worth
	// running: an ssh repository has an agent and an identity to ask about, and
	// an https one has neither.
	for name, test := range map[string]struct {
		raw       string
		transport Transport
		host      string
		address   string
		target    string
	}{
		"scp style": {
			raw:       "git@github.com:acme/infra.git",
			transport: TransportSSH,
			host:      "github.com",
			address:   "github.com:22",
			target:    "git@github.com",
		},
		"scp style without a user": {
			raw:       "gitlab.example.com:acme/infra.git",
			transport: TransportSSH,
			host:      "gitlab.example.com",
			address:   "gitlab.example.com:22",
			// git defaults the account to `git`, and so does this.
			target: "git@gitlab.example.com",
		},
		"ssh url with a port": {
			raw:       "ssh://git@git.acme.internal:2222/acme/infra.git",
			transport: TransportSSH,
			host:      "git.acme.internal",
			address:   "git.acme.internal:2222",
			target:    "git@git.acme.internal",
		},
		"https": {
			raw:       "https://github.com/acme/infra.git",
			transport: TransportHTTPS,
			host:      "github.com",
			address:   "github.com:443",
			// Nothing to ask ssh about.
			target: "",
		},
		"http": {
			raw:       "http://git.acme.internal/acme/infra.git",
			transport: TransportHTTP,
			host:      "git.acme.internal",
			address:   "git.acme.internal:80",
			target:    "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			remote, err := ParseRemote(test.raw)
			if err != nil {
				t.Fatalf("ParseRemote(%q) = %v", test.raw, err)
			}
			if remote.Transport() != test.transport {
				t.Errorf("transport = %q, want %q", remote.Transport(), test.transport)
			}
			if remote.Host() != test.host {
				t.Errorf("host = %q, want %q", remote.Host(), test.host)
			}
			if remote.Address() != test.address {
				t.Errorf("address = %q, want %q", remote.Address(), test.address)
			}
			if remote.SSHTarget() != test.target {
				t.Errorf("ssh target = %q, want %q", remote.SSHTarget(), test.target)
			}
		})
	}
}

func TestAnHTTPSTokenIsNotKeptOnTheRemote(t *testing.T) {
	// The userinfo on an https URL is frequently the token itself. Nothing
	// downstream needs it, so it is not carried, and it is taken back out on
	// the way to a screen even though it should never have arrived.
	remote, err := ParseRemote("https://oauth2:ghp_supersecret@github.com/acme/infra.git")
	if err != nil {
		t.Fatalf("ParseRemote: %v", err)
	}
	if strings.Contains(remote.Display(), "ghp_supersecret") {
		t.Fatalf("display = %q", remote.Display())
	}
	if strings.Contains(remote.SSHTarget(), "oauth2") {
		t.Fatalf("ssh target = %q", remote.SSHTarget())
	}
}

func TestAnUnsupportedRemoteIsRefusedRatherThanDiagnosed(t *testing.T) {
	// There is nothing to diagnose about a URL that will never be run. The
	// refusal is the answer.
	for name, raw := range map[string]string{
		"transport helper": "ext::sh -c whoami",
		"local path":       "/srv/git/infra.git",
		"git protocol":     "git://github.com/acme/infra.git",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseRemote(raw); kind(t, err) != ErrorUnsupported {
				t.Fatalf("kind = %q", kind(t, err))
			}
		})
	}
}

func TestTheCopiedCommandCarriesNothingSecret(t *testing.T) {
	// It is meant to be pasted into a terminal, and a command a person pastes
	// is a command that ends up in their shell history.
	remote, err := ParseRemote("https://oauth2:ghp_supersecret@github.com/acme/infra.git")
	if err != nil {
		t.Fatalf("ParseRemote: %v", err)
	}

	command := Command(remote)
	if strings.Contains(command, "ghp_supersecret") || strings.Contains(command, "oauth2") {
		t.Fatalf("command = %q", command)
	}
	if !strings.HasPrefix(command, "git ls-remote ") {
		t.Fatalf("command = %q", command)
	}
	// ls-remote rather than clone: the person running it is answering "can my
	// terminal read this", not fetching a repository.
	if strings.Contains(command, "clone") {
		t.Fatalf("the diagnostic command clones: %q", command)
	}
}

func TestAServerGreetingIsOnlyReadWhenItIsRecognised(t *testing.T) {
	// Every pattern here is a promise that what it captured is a username. A
	// pattern that is nearly right puts somebody else's word on screen as an
	// account name, so an unfamiliar greeting stays unread.
	for name, test := range map[string]struct {
		line string
		want string
	}{
		"github": {"Hi octocat! You've successfully authenticated, but GitHub does not provide shell access.", "octocat"},
		"gitlab": {"Welcome to GitLab, @octocat!", "octocat"},
		"gitlab with dots": {
			"Welcome to GitLab, @first.last!", "first.last",
		},
		"an unfamiliar server": {"Interactive shell disabled for this account.", ""},
		"a near miss":          {"Hi there, welcome to the git server", ""},
	} {
		t.Run(name, func(t *testing.T) {
			var got string
			for _, pattern := range greetings {
				if match := pattern.FindStringSubmatch(test.line); match != nil {
					got = match[1]
					break
				}
			}
			if got != test.want {
				t.Fatalf("account = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNothingIsAskedOfAHostThatIsNotReachedOverSSH(t *testing.T) {
	// An https repository has no ssh identity, and pretending otherwise would
	// mean running ssh against a web server.
	remote, err := ParseRemote("https://github.com/acme/infra.git")
	if err != nil {
		t.Fatalf("ParseRemote: %v", err)
	}
	if _, err := Identify(t.Context(), remote); kind(t, err) != ErrorUnsupported {
		t.Fatalf("kind = %q", kind(t, err))
	}
}

func TestAHostThatIsNotListeningIsSaidSoQuickly(t *testing.T) {
	// Port 1 on the loopback interface: nothing has ever listened there, and
	// the kernel refuses immediately rather than making the test wait.
	remote, err := ParseRemote("ssh://git@127.0.0.1:1/acme/infra.git")
	if err != nil {
		t.Fatalf("ParseRemote: %v", err)
	}

	started := time.Now()
	err = Listening(t.Context(), remote)
	if err == nil {
		t.Fatal("something answered on port 1")
	}
	if got := kind(t, err); got != ErrorUnreachable && got != ErrorTimeout {
		t.Fatalf("kind = %q", got)
	}
	if time.Since(started) > dialTimeout+time.Second {
		t.Fatalf("the dial outlived its own bound: %s", time.Since(started))
	}
}

func TestACancelledDiagnosticStopsRatherThanWaitingOutItsTimeout(t *testing.T) {
	// The drawer that asked may already be closed. A probe that ignores that
	// leaves a connection open to somebody's git host for no reason.
	remote, err := ParseRemote("ssh://git@10.255.255.1:22/acme/infra.git")
	if err != nil {
		t.Fatalf("ParseRemote: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	started := time.Now()
	if err := Listening(ctx, remote); err == nil {
		t.Fatal("a cancelled dial succeeded")
	}
	if time.Since(started) > time.Second {
		t.Fatalf("cancellation took %s", time.Since(started))
	}
}

func TestTheSSHConfigPathIsResolvedRatherThanSpelled(t *testing.T) {
	// `~/.ssh/config` is right on macOS and Linux and wrong on Windows, which
	// is exactly the sort of thing that gets written into a Vue file once and
	// is then wrong for a third of the people using it.
	path, err := SSHConfig()
	if err != nil {
		t.Skip("this machine has no home directory")
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("path = %q, which is not absolute", path)
	}
	if filepath.Base(path) != "config" || filepath.Base(filepath.Dir(path)) != ".ssh" {
		t.Fatalf("path = %q", path)
	}
	if strings.Contains(path, "~") {
		t.Fatalf("path = %q, which a file manager cannot open", path)
	}
}
