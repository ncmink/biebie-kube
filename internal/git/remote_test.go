package git

import (
	"errors"
	"strings"
	"testing"
)

func TestATransportThatRunsACommandIsRefused(t *testing.T) {
	// `ext::` is git's escape hatch: the rest of the string is a command it
	// runs. The URL comes off an Argo CD Application, so anybody who may edit
	// one in the cluster would otherwise choose what this machine executes.
	// This is the single most important line in the package.
	for _, raw := range []string{
		"ext::sh -c 'id > /tmp/pwned'",
		"ext::git-upload-pack %S /repo",
		"EXT::sh -c id",
	} {
		if _, err := ParseRemote(raw); err == nil {
			t.Fatalf("%q was accepted", raw)
		} else if kind(t, err) != ErrorUnsupported {
			t.Fatalf("%q was refused for the wrong reason: %v", raw, err)
		}
	}
}

func TestARemoteThatWouldBecomeAnOptionIsRefused(t *testing.T) {
	// git reads a leading dash as an option wherever the argument sits, so
	// `--upload-pack=…` in the place of a URL is another way to choose a
	// command.
	for _, raw := range []string{
		"--upload-pack=/bin/sh",
		"-u/bin/sh",
		"--config=core.pager=id",
	} {
		if _, err := ParseRemote(raw); err == nil {
			t.Fatalf("%q was accepted", raw)
		}
	}
}

func TestALocalPathIsNotARepositoryThisWillRead(t *testing.T) {
	// Reading the filesystem of the machine running the application is not
	// what "the desired state lives in a repository" is supposed to mean.
	for _, raw := range []string{
		"file:///etc",
		"/etc/passwd",
		"./relative/repo",
		`C:\Users\someone\repo`,
		"git://github.com/acme/infra.git",
	} {
		if _, err := ParseRemote(raw); err == nil {
			t.Fatalf("%q was accepted", raw)
		}
	}
}

func TestTheRemotesPeopleActuallyUseAreAccepted(t *testing.T) {
	for _, raw := range []string{
		"https://github.com/acme/infra.git",
		"http://git.internal/acme/infra.git",
		"ssh://git@github.com/acme/infra.git",
		"git@github.com:acme/infra.git",
		"git@git.acme.internal:platform/infra/cluster-config.git",
	} {
		remote, err := ParseRemote(raw)
		if err != nil {
			t.Fatalf("%q was refused: %v", raw, err)
		}
		if remote.String() != raw {
			t.Fatalf("%q became %q", raw, remote.String())
		}
	}
}

func TestAControlCharacterInARemoteIsRefused(t *testing.T) {
	if _, err := ParseRemote("https://github.com/acme/infra.git\nrm -rf /"); err == nil {
		t.Fatal("a newline was accepted")
	}
}

func TestARevisionMayNotCarryRevisionSyntax(t *testing.T) {
	// Everything here is legal git revision syntax and none of it is something
	// Argo CD writes. Accepting it would let a string from a cluster address
	// arbitrary objects in the repository.
	for _, raw := range []string{
		"--output=/tmp/x",
		"main..other",
		"HEAD^{tree}",
		"main:../../etc/passwd",
		"main~3",
		"refs/heads/main with a space",
	} {
		if _, err := ParseRevision(raw); err == nil {
			t.Fatalf("%q was accepted", raw)
		}
	}
}

func TestTheRevisionsPeopleActuallyWriteAreAccepted(t *testing.T) {
	for _, raw := range []string{"main", "HEAD", "release/1.4", "v2.0.1", "8f3c1a2", "feature_x"} {
		if _, err := ParseRevision(raw); err != nil {
			t.Fatalf("%q was refused: %v", raw, err)
		}
	}
}

func TestAnEmptyRevisionBecomesTheDefaultRatherThanAnError(t *testing.T) {
	// Argo CD leaves targetRevision out to mean HEAD, and an Application that
	// does so is not misconfigured.
	revision, err := ParseRevision("")
	if err != nil {
		t.Fatalf("an empty revision was refused: %v", err)
	}
	if revision.String() != "HEAD" {
		t.Fatalf("revision = %q", revision)
	}
}

func TestAPathMayNotLeaveTheRepository(t *testing.T) {
	for _, raw := range []string{
		"../../etc",
		"/etc/passwd",
		"apps/../../../etc",
		"--exec=id",
	} {
		if _, err := ParsePath(raw); err == nil {
			t.Fatalf("%q was accepted", raw)
		}
	}
}

func TestAPathIsCleanedRatherThanRefusedWhenItStaysInside(t *testing.T) {
	got, err := ParsePath("apps/payment/../payment/prod/")
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if got.String() != "apps/payment/prod" {
		t.Fatalf("path = %q", got)
	}
}

func TestAnEmptyPathIsTheRepositoryRoot(t *testing.T) {
	// An Application with no path renders the whole repository, which is a
	// real configuration rather than a missing one.
	for _, raw := range []string{"", ".", "  "} {
		got, err := ParsePath(raw)
		if err != nil {
			t.Fatalf("%q was refused: %v", raw, err)
		}
		if !got.IsRoot() {
			t.Fatalf("%q became %q", raw, got)
		}
	}
}

func kind(t *testing.T, err error) ErrorKind {
	t.Helper()
	var failure *Error
	if !errors.As(err, &failure) {
		t.Fatalf("not a git error: %v", err)
	}
	return failure.Kind
}

func TestARefusalNamesTheTransportItRefused(t *testing.T) {
	// A message that says only "unsupported" leaves the reader guessing at
	// which of the several strings on the Application was the problem.
	_, err := ParseRemote("ext::sh -c id")
	if err == nil || !strings.Contains(err.Error(), "ext") {
		t.Fatalf("the message does not name the transport: %v", err)
	}
}
