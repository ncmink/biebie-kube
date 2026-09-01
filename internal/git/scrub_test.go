package git

import (
	"errors"
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

func TestACloneIsNotDescribedByItsOwnAnnouncement(t *testing.T) {
	// `git clone` writes its progress to stderr, so the first line of a failed
	// clone is always the clone announcing itself. Reading only that line meant
	// every failed clone was classified as "something went wrong" and shown to
	// the reader as a cache path.
	//
	// This is the real output of a clone against a host that will not take the
	// key that is loaded.
	err := classify(t.Context(), errStub{}, strings.Join([]string{
		"Cloning into bare repository '/Users/someone/Library/Caches/biebie-kube/git/4b30a29db9ee'...",
		"git@github.com: Permission denied (publickey).",
		"fatal: Could not read from remote repository.",
		"",
		"Please make sure you have the correct access rights",
		"and the repository exists.",
	}, "\n"))

	if got := kind(t, err); got != ErrorAuth {
		t.Fatalf("kind = %q, want %q — the reason was below the progress line", got, ErrorAuth)
	}
	if err.Error() != "git@github.com: Permission denied (publickey)." {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestProgressNeverBecomesTheMessage(t *testing.T) {
	// The counting lines arrive before the failure on a clone that gets far
	// enough to start transferring.
	err := classify(t.Context(), errStub{}, strings.Join([]string{
		"Cloning into bare repository '/tmp/mirror'...",
		"remote: Enumerating objects: 4213, done.",
		"remote: Counting objects: 100% (4213/4213), done.",
		"warning: redirecting to https://git.acme.internal/infra.git/",
		"fatal: could not resolve host: git.acme.internal",
	}, "\n"))

	if got := kind(t, err); got != ErrorUnreachable {
		t.Fatalf("kind = %q", got)
	}
	if err.Error() != "could not resolve host: git.acme.internal" {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestTheFailureAPersonActuallyHit(t *testing.T) {
	// Verbatim from a GitLab clone that failed, including the advisory ssh 10
	// prints on every connection and the rule GitLab draws around its refusal.
	// Every line above the sentence had at one point or another been shown to
	// the reader as the reason.
	err := classify(t.Context(), errStub{}, strings.Join([]string{
		"Cloning into bare repository '/Users/someone/Library/Caches/biebie-kube/git/4b30a29'...",
		"** WARNING: connection is not using a post-quantum key exchange algorithm.",
		"** This session may be vulnerable to \"store now, decrypt later\" attacks.",
		"remote: ",
		"remote: ========================================================================",
		"remote: ",
		"remote: The project you were looking for could not be found or you don't have permission to view it.",
		"remote: ",
		"remote: ========================================================================",
		"remote: ",
		"fatal: Could not read from remote repository.",
		"",
		"Please make sure you have the correct access rights",
		"and the repository exists.",
	}, "\n"))

	// Not auth. The server let the connection in and then declined to hand
	// over a repository, which is a different person to go and ask.
	if got := kind(t, err); got != ErrorNoRepository {
		t.Fatalf("kind = %q, want %q", got, ErrorNoRepository)
	}
	if err.Error() != "The project you were looking for could not be found or you don't have permission to view it." {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestARowOfPunctuationIsNotAnExplanation(t *testing.T) {
	// GitLab boxes its message in `=====`. Those lines are not empty, so
	// skipping blanks does not skip them, and the first one arrives before the
	// sentence it is decorating.
	if got := reason("remote: ==========\nremote: Repository not found.\n"); got != "Repository not found." {
		t.Fatalf("reason = %q", got)
	}
	if got := reason("--------\n________\nfatal: nothing worked"); got != "nothing worked" {
		t.Fatalf("reason = %q", got)
	}
}

func TestHostKeyTroubleIsNotReadAsACredentialProblem(t *testing.T) {
	// ssh words this as a permission failure too. Reading it as one sends
	// somebody to look at a key of their own when what changed was the
	// server's, and the convenient fix for it is the one thing this
	// application will not do.
	for name, stderr := range map[string]string{
		"unknown host": "Host key verification failed.\nfatal: Could not read from remote repository.",
		"changed key": strings.Join([]string{
			"@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@",
			"WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!",
			"fatal: Could not read from remote repository.",
		}, "\n"),
		"no shared algorithm": "Unable to negotiate with 10.0.0.1 port 22: no matching host key type found.",
	} {
		t.Run(name, func(t *testing.T) {
			err := classify(t.Context(), errStub{}, stderr)
			if got := kind(t, err); got != ErrorHostKey {
				t.Fatalf("kind = %q, want %q", got, ErrorHostKey)
			}
		})
	}
}

func TestSlowIsNotTheSameAnswerAsAbsent(t *testing.T) {
	// A host that is merely slow is worth pressing again. One that does not
	// exist is not, and offering the same button for both wastes the press.
	timedOut := classify(t.Context(), errStub{}, "ssh: connect to host git.acme.internal port 22: Operation timed out")
	if got := kind(t, timedOut); got != ErrorTimeout {
		t.Fatalf("kind = %q, want %q", got, ErrorTimeout)
	}

	absent := classify(t.Context(), errStub{}, "ssh: Could not resolve hostname git.acme.internal")
	if got := kind(t, absent); got != ErrorUnreachable {
		t.Fatalf("kind = %q, want %q", got, ErrorUnreachable)
	}
}

func TestAnSSHAdvisoryIsNotWhyAnythingFailed(t *testing.T) {
	// OpenSSH 10 warns on every connection to a host without post-quantum key
	// exchange, which today is most git hosts. It is true and it is not the
	// answer to "why did this not work".
	err := classify(t.Context(), errStub{}, strings.Join([]string{
		"Cloning into bare repository '/tmp/mirror'...",
		"** WARNING: connection is not using a post-quantum key exchange algorithm.",
		"** This session may be vulnerable to \"store now, decrypt later\" attacks.",
		"** The server may need to be upgraded. See https://openssh.com/pq.html",
		"fatal: Remote branch develop not found in upstream origin",
	}, "\n"))

	if err.Error() != "Remote branch develop not found in upstream origin" {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestTheWholeOfWhatGitSaidIsKeptBesideTheSentence(t *testing.T) {
	// One line is chosen by a rule that only knows the failures git had grown
	// by the time it was written. The reader who does not believe the sentence
	// needs the rest of it to hand.
	err := classify(t.Context(), errStub{}, strings.Join([]string{
		"Cloning into bare repository '/tmp/mirror'...",
		"git@github.com: Permission denied (publickey).",
		"fatal: Could not read from remote repository.",
	}, "\n"))

	var failure *Error
	if !errors.As(err, &failure) {
		t.Fatalf("error = %T", err)
	}
	for _, want := range []string{"Cloning into", "Permission denied", "Could not read from remote"} {
		if !strings.Contains(failure.Output, want) {
			t.Fatalf("output is missing %q: %q", want, failure.Output)
		}
	}
}

func TestTheKeptOutputIsScrubbedToo(t *testing.T) {
	// It goes to the same window the message does, so it is subject to the
	// same rule about what may appear there.
	err := classify(t.Context(), errStub{},
		"fatal: Authentication failed for 'https://oauth2:ghp_supersecret@github.com/acme/infra.git/'")

	var failure *Error
	if !errors.As(err, &failure) {
		t.Fatalf("error = %T", err)
	}
	if strings.Contains(failure.Output, "ghp_supersecret") {
		t.Fatalf("a credential reached the kept output: %q", failure.Output)
	}
}

func TestAFailureThatSaidNothingButProgressStillReads(t *testing.T) {
	// Skipping the chatter must not leave the reader with an empty sentence.
	err := classify(t.Context(), errStub{}, "Cloning into bare repository '/tmp/mirror'...\n")
	if err.Error() == "" || strings.Contains(err.Error(), "Cloning into") {
		t.Fatalf("message = %q", err.Error())
	}
}

type errStub struct{}

func (errStub) Error() string { return "exit status 128" }
