package gitops

import (
	"errors"
	"strings"
	"testing"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/git"
)

func TestEachWayGitRefusesGetsItsOwnConclusion(t *testing.T) {
	// The whole point of the panel is that these have different people to go
	// and ask. Collapsing them into "git failed" is the thing being fixed.
	for name, test := range map[string]struct {
		kind git.ErrorKind
		want domain.GitFault
	}{
		"no git":        {git.ErrorMissing, domain.FaultGitMissing},
		"bad url":       {git.ErrorUnsupported, domain.FaultBadRemote},
		"host key":      {git.ErrorHostKey, domain.FaultHostKey},
		"credentials":   {git.ErrorAuth, domain.FaultAuth},
		"slow":          {git.ErrorTimeout, domain.FaultTimeout},
		"absent":        {git.ErrorUnreachable, domain.FaultUnreachable},
		"no repo":       {git.ErrorNoRepository, domain.FaultNoRepository},
		"something new": {git.ErrorFailed, domain.FaultUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			got := faultOf(&git.Error{Kind: test.kind, Message: "…"})
			if got != test.want {
				t.Fatalf("fault = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAnErrorFromSomewhereElseIsNotGuessedAt(t *testing.T) {
	if got := faultOf(errors.New("the cluster went away")); got != domain.FaultUnknown {
		t.Fatalf("fault = %q", got)
	}
}

func TestARefusedRepositoryKeepsBothPossibilitiesOpen(t *testing.T) {
	// GitLab and several others answer "no such repository" and "not yours"
	// with the same sentence on purpose, so that a stranger cannot map a
	// private namespace by reading error messages. A panel that picked one of
	// them would be inventing the half the server withheld.
	summary, causes := explain(domain.FaultNoRepository, nil)

	if strings.Contains(strings.ToLower(summary), "does not exist") {
		t.Fatalf("the summary asserts the repository is missing: %q", summary)
	}
	if len(causes) < 2 {
		t.Fatalf("causes = %v", causes)
	}

	joined := strings.ToLower(strings.Join(causes, " | "))
	for _, want := range []string{"access", "moved, renamed or deleted"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the causes do not mention %q: %v", want, causes)
		}
	}
}

func TestAWorkingRepositoryDoesNotOfferThingsToFix(t *testing.T) {
	summary, causes := explain(domain.FaultNone, nil)
	if summary == "" {
		t.Fatal("a passing diagnosis says nothing")
	}
	if causes != nil {
		t.Fatalf("causes = %v", causes)
	}
}

func TestTheStepsComeBackInTheOrderTheyWereTried(t *testing.T) {
	// A reader works down the list until they hit the first ✕, so the order is
	// the diagnosis. Skipped steps stay in it: a gap would read as a check
	// that was forgotten rather than one there was no point running.
	steps := &ladder{}
	steps.pass(domain.StepGit, "found", 0)
	steps.fail(domain.StepRemote, "no", 0)
	steps.skip(domain.StepHost, domain.StepAgent, domain.StepRepository)

	want := []domain.GitStep{
		domain.StepGit, domain.StepRemote, domain.StepHost, domain.StepAgent, domain.StepRepository,
	}
	if len(steps.checks) != len(want) {
		t.Fatalf("checks = %+v", steps.checks)
	}
	for i, step := range want {
		if steps.checks[i].Step != step {
			t.Fatalf("check %d = %q, want %q", i, steps.checks[i].Step, step)
		}
	}
	if steps.checks[2].Result != domain.StepSkipped {
		t.Fatalf("result = %q", steps.checks[2].Result)
	}
}

func TestAMissingAgentIsNotCalledAFailure(t *testing.T) {
	// A key with no passphrase never touches an agent. Marking its absence red
	// sends people to fix something that is not broken, and this application
	// has no way to know whether their keys need one.
	steps := &ladder{}
	steps.unknown(domain.StepAgent, "No ssh-agent is visible to this application.")

	if steps.checks[0].Result != domain.StepUnknown {
		t.Fatalf("result = %q, want %q", steps.checks[0].Result, domain.StepUnknown)
	}
}

func TestWhatGitSaidIsCarriedIntoTheDiagnosis(t *testing.T) {
	// The summary is written by this package. The reader who does not believe
	// it needs the output the conclusion was drawn from.
	failure := &git.Error{
		Kind:    git.ErrorNoRepository,
		Message: "The project you were looking for could not be found",
		Output:  "remote: The project you were looking for could not be found\nfatal: Could not read from remote repository.",
	}

	got := settle(domain.GitAccess{}, &ladder{}, domain.FaultNoRepository, failure)
	if !strings.Contains(got.Output, "Could not read from remote repository") {
		t.Fatalf("output = %q", got.Output)
	}
	if got.Fault != domain.FaultNoRepository {
		t.Fatalf("fault = %q", got.Fault)
	}
}

func TestTheSSHConfigIsReportedRatherThanCreated(t *testing.T) {
	// A missing config is normal — ssh works without one — and writing into
	// somebody's .ssh directory because a comparison failed would be this
	// application deciding something about their authentication that is not
	// its to decide.
	found := sshConfig()
	if found.Path == "" {
		t.Skip("this machine has no home directory")
	}
	if strings.Contains(found.Path, "~") {
		t.Fatalf("path = %q, which a file manager cannot open", found.Path)
	}
	// Exists is whatever it is on this machine. What matters is that asking
	// did not bring the file into being.
	if _, err := git.SSHConfig(); err != nil {
		t.Fatalf("SSHConfig: %v", err)
	}
}
