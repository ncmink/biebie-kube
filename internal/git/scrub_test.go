package git

import (
	"strings"
	"testing"
)

func TestGitsOwnErrorsDoNotCarryCredentialsOut(t *testing.T) {
	// git quotes back the URL it was handed, and this text becomes an error
	// message, a line in a window and eventually a screenshot in a ticket.
	for name, test := range map[string]struct{ raw, want string }{
		"https token": {
			"fatal: Authentication failed for 'https://oauth2:ghp_supersecret@github.com/acme/infra.git/'",
			"fatal: Authentication failed for 'https://github.com/acme/infra.git/'",
		},
		"https bare username is treated as a token": {
			"remote: repository https://ghp_supersecret@github.com/acme/infra not found",
			"remote: repository https://github.com/acme/infra not found",
		},
		"ssh password": {
			"fatal: unable to access 'ssh://git:hunter2@git.acme.internal/infra'",
			"fatal: unable to access 'ssh://git@git.acme.internal/infra'",
		},
		"ssh account name is kept": {
			"Permission denied (publickey). ssh://git@github.com/acme/infra.git",
			"Permission denied (publickey). ssh://git@github.com/acme/infra.git",
		},
		"nothing to remove": {
			"fatal: could not resolve host: git.acme.internal",
			"fatal: could not resolve host: git.acme.internal",
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := Scrub(test.raw)
			if got != test.want {
				t.Fatalf("got  %q\nwant %q", got, test.want)
			}
			for _, secret := range []string{"ghp_supersecret", "hunter2"} {
				if strings.Contains(got, secret) {
					t.Fatalf("a credential survived: %q", got)
				}
			}
		})
	}
}

func TestScrubbingKeepsTheHostBecauseThatIsTheUsefulHalf(t *testing.T) {
	// "Could not reach the host" is worth nothing without the host's name.
	got := Scrub("fatal: unable to access 'https://user:token@git.acme.internal/infra'")
	if !strings.Contains(got, "git.acme.internal") {
		t.Fatalf("the host was scrubbed away too: %q", got)
	}
}

func TestAFailureIsClassifiedByWhatGitSaidRatherThanItsExitCode(t *testing.T) {
	// Every one of these exits non-zero. The difference between them is the
	// whole of what the reader needs, and only the text carries it.
	for name, test := range map[string]struct {
		stderr string
		want   ErrorKind
	}{
		"auth":        {"fatal: Authentication failed for 'https://github.com/acme/infra'", ErrorAuth},
		"key":         {"git@github.com: Permission denied (publickey).", ErrorAuth},
		"dns":         {"fatal: could not resolve host: git.acme.internal", ErrorUnreachable},
		"refused":     {"fatal: unable to access: Connection refused", ErrorUnreachable},
		"no repo":     {"remote: Repository not found.", ErrorNoRepository},
		"no revision": {"fatal: couldn't find remote ref refs/heads/nope", ErrorNoRevision},
		"anything":    {"fatal: the remote end hung up unexpectedly", ErrorFailed},
	} {
		t.Run(name, func(t *testing.T) {
			err := classify(t.Context(), errStub{}, test.stderr)
			if got := kind(t, err); got != test.want {
				t.Fatalf("kind = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAClassifiedFailureIsAlreadyScrubbed(t *testing.T) {
	// The scrubbing has to happen inside the classification rather than at the
	// point a message is displayed, because every caller of run gets this
	// error and not all of them end up on a screen.
	err := classify(t.Context(), errStub{},
		"fatal: Authentication failed for 'https://oauth2:ghp_supersecret@github.com/acme/infra.git/'")
	if strings.Contains(err.Error(), "ghp_supersecret") {
		t.Fatalf("a credential reached the error: %q", err)
	}
}

func TestOnlyTheLineThatSaysWhatHappenedIsKept(t *testing.T) {
	// git follows the failure with hints, a blank line and a suggestion to
	// read a manual page.
	err := classify(t.Context(), errStub{}, strings.Join([]string{
		"",
		"fatal: Repository not found.",
		"",
		"hint: see 'git help credential' for a list of helpers",
	}, "\n"))
	if err.Error() != "Repository not found." {
		t.Fatalf("message = %q", err.Error())
	}
}

type errStub struct{}

func (errStub) Error() string { return "exit status 128" }
