package gitops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/git"
)

// Diagnose works out which part of the path to a repository is refusing.
//
// The comparison already failed by the time anybody presses this, and it
// failed with one sentence from git that has to stand for every way a
// repository can be out of reach: a PATH without git in it, a URL pointing
// somewhere that no longer exists, a host that does not answer, a key the
// server does not know, a key it knows perfectly well and an account that has
// never been given this repository. Those have different people to go and ask,
// and the sentence does not distinguish them.
//
// So each is asked separately, cheaply, in the order they sit in. Nothing here
// clones, writes, or changes a credential. The most it does is ask a server
// for a list of refs and ask ssh who it thinks we are.
func (s *Service) Diagnose(ctx context.Context, clusterID string, ref domain.ResourceRef) (domain.GitAccess, error) {
	source, err := s.repositoryFor(ctx, clusterID, ref)
	if err != nil {
		return domain.GitAccess{}, err
	}

	out := domain.GitAccess{SSHConfig: sshConfig()}
	steps := &ladder{}

	if _, err := git.Look(); err != nil {
		steps.fail(domain.StepGit, err.Error(), 0)
		steps.skip(domain.StepRemote, domain.StepHost, domain.StepAgent, domain.StepRepository)
		return settle(out, steps, domain.FaultGitMissing, err), nil
	}
	steps.pass(domain.StepGit, "Found on the PATH this application can see.", 0)

	remote, err := git.ParseRemote(source.RepoURL)
	if err != nil {
		steps.fail(domain.StepRemote, err.Error(), 0)
		steps.skip(domain.StepHost, domain.StepAgent, domain.StepRepository)
		return settle(out, steps, domain.FaultBadRemote, err), nil
	}
	out.Repository = remote.Display()
	out.Transport = string(remote.Transport())
	out.Command = git.Command(remote)
	steps.pass(domain.StepRemote, fmt.Sprintf("Read as %s.", transportName(remote.Transport())), 0)

	// A host that does not answer is worth knowing about before anything is
	// concluded from a refusal, but a host that does answer proves very
	// little: ssh may reach the same repository through a ProxyCommand this
	// dial knows nothing about. So a failure here does not stop the ladder.
	started := time.Now()
	if err := git.Listening(ctx, remote); err != nil {
		steps.fail(domain.StepHost, err.Error(), time.Since(started))
	} else {
		steps.pass(domain.StepHost, remote.Host()+" accepted a connection.", time.Since(started))
	}

	if remote.Transport() == git.TransportSSH {
		if git.AgentRunning() {
			steps.pass(domain.StepAgent, "An ssh-agent is visible to this application.", 0)
		} else {
			// Not a failure. Plenty of working setups authenticate with an
			// IdentityFile and never start an agent, and marking this red
			// would send those people to fix something that is not broken.
			steps.unknown(domain.StepAgent,
				"No ssh-agent is visible to this application. That is only a problem if your keys need one — "+
					"a key with no passphrase does not.")
		}
	} else {
		steps.skip(domain.StepAgent)
	}

	started = time.Now()
	if err := git.Readable(ctx, remote); err != nil {
		steps.fail(domain.StepRepository, err.Error(), time.Since(started))
		return settle(out, steps, faultOf(err), err), nil
	}
	steps.pass(domain.StepRepository, "The server listed this repository's refs.", time.Since(started))
	return settle(out, steps, domain.FaultNone, nil), nil
}

// Identify asks the git host which account it authenticates this machine as.
//
// Separate from Diagnose and behind its own press, because it is a separate
// question and because the answer to it is not the answer to the one people
// think they are asking. A server greeting somebody by name has established
// who they are and nothing whatever about what they may read.
func (s *Service) Identify(ctx context.Context, clusterID string, ref domain.ResourceRef) (domain.GitIdentity, error) {
	source, err := s.repositoryFor(ctx, clusterID, ref)
	if err != nil {
		return domain.GitIdentity{}, err
	}
	remote, err := git.ParseRemote(source.RepoURL)
	if err != nil {
		return domain.GitIdentity{}, err
	}

	found, err := git.Identify(ctx, remote)
	if err != nil {
		return domain.GitIdentity{}, err
	}
	out := domain.GitIdentity{Account: found.Account, Greeting: found.Greeting}
	if found.Account == "" {
		// The honest answer, and the one that matters most to somebody with
		// two accounts on one host: this code does not know which of them
		// answered, so it says that rather than picking.
		out.Summary = "Authentication succeeded. This host did not name the account in a form Biebie Kube reads."
		return out, nil
	}
	out.Summary = fmt.Sprintf("Authenticated as %s. This says who you are, not what you may read.", found.Account)
	return out, nil
}

// repositoryFor finds the one repository behind an object.
//
// Taking the URL from ownership rather than from the caller is deliberate.
// A repository URL that arrived over the binding would be a URL this
// application ran git against because a window asked it to, and the whole of
// remote.go exists because strings that reach a git command line are
// dangerous. This way the only reachable repositories are the ones an
// Application in the cluster already names.
func (s *Service) repositoryFor(ctx context.Context, clusterID string, ref domain.ResourceRef) (domain.GitSource, error) {
	ownership, err := s.argo.Ownership(ctx, clusterID, ref)
	if err != nil {
		return domain.GitSource{}, err
	}
	if !ownership.Confidence.Managed() || len(ownership.Sources) == 0 {
		return domain.GitSource{}, errors.New("no Argo CD Application names a repository for this object")
	}
	// The tree sources first, since those are the ones a comparison would have
	// tried to read. A generated source still has a repository worth testing.
	if trees := treeSources(ownership.Sources); len(trees) > 0 {
		return trees[0], nil
	}
	return ownership.Sources[0], nil
}

// ladder collects the checks in the order they were attempted.
type ladder struct{ checks []domain.GitCheck }

func (l *ladder) pass(step domain.GitStep, detail string, elapsed time.Duration) {
	l.add(step, domain.StepPassed, detail, elapsed)
}

func (l *ladder) fail(step domain.GitStep, detail string, elapsed time.Duration) {
	l.add(step, domain.StepFailed, detail, elapsed)
}

func (l *ladder) unknown(step domain.GitStep, detail string) {
	l.add(step, domain.StepUnknown, detail, 0)
}

func (l *ladder) skip(steps ...domain.GitStep) {
	for _, step := range steps {
		l.add(step, domain.StepSkipped, "", 0)
	}
}

func (l *ladder) add(step domain.GitStep, result domain.GitStepResult, detail string, elapsed time.Duration) {
	check := domain.GitCheck{Step: step, Result: result, Detail: detail}
	if elapsed > 0 {
		check.Elapsed = elapsed.Milliseconds()
	}
	l.checks = append(l.checks, check)
}

// settle turns the ladder and the failure into the sentence and the list of
// things that could have produced it.
func settle(out domain.GitAccess, steps *ladder, fault domain.GitFault, err error) domain.GitAccess {
	out.Checks = steps.checks
	out.Fault = fault
	out.Summary, out.Causes = explain(fault, err)

	var failure *git.Error
	if errors.As(err, &failure) {
		out.Output = failure.Output
	}
	return out
}

// explain says what was concluded and what could account for it.
//
// The causes are a list rather than a sentence because for the common failure
// there is genuinely more than one, and the server has declined to say which.
// A panel that guessed would be right about half the time and would send the
// other half to argue with the wrong person.
func explain(fault domain.GitFault, err error) (string, []string) {
	switch fault {
	case domain.FaultNone:
		return "This repository is readable with your current git credentials.", nil
	case domain.FaultGitMissing:
		return "Biebie Kube cannot find a git to run.", []string{
			"Git is not installed on this machine",
			"Git is installed somewhere this application's PATH does not reach",
		}
	case domain.FaultBadRemote:
		return "This Application's repository URL is not one Biebie Kube will run git against.", nil
	case domain.FaultUnreachable:
		return "Nothing answered at the address this repository points at.", []string{
			"The host name is wrong or no longer resolves",
			"A VPN or network route this machine needs is not up",
			"A firewall is refusing the connection",
		}
	case domain.FaultTimeout:
		return "The host did not answer in time.", []string{
			"The network is slow or the host is under load",
			"A firewall is dropping the connection rather than refusing it",
		}
	case domain.FaultHostKey:
		return "SSH would not accept this host's key.", []string{
			"This machine has not accepted this host before",
			"The host's key changed and known_hosts still has the old one",
		}
	case domain.FaultAuth:
		return "The server did not accept your ssh credentials.", []string{
			"The key this connection offered is not registered with your account",
			"The key needs a passphrase and no agent holding it is visible to this application",
			"Your terminal and this application are not using the same key",
		}
	case domain.FaultNoRepository:
		// Deliberately both. GitLab and several others answer "missing" and
		// "forbidden" identically so that a stranger cannot map a private
		// namespace by reading errors, and this code has no way to see past
		// that — nor should it want one.
		return "The server would not hand over this repository. It did not say whether the repository is missing or your account may not read it.", []string{
			"Your account has not been given access to this repository",
			"Authentication succeeded as a different account from the one with access",
			"The repository was moved, renamed or deleted",
			"The path in the Argo CD Application is wrong",
		}
	default:
		summary := "Git refused, in a way Biebie Kube does not recognise."
		if err != nil {
			summary = err.Error()
		}
		return summary, nil
	}
}

// faultOf maps a git failure onto what the panel should offer to do about it.
func faultOf(err error) domain.GitFault {
	var failure *git.Error
	if !errors.As(err, &failure) {
		return domain.FaultUnknown
	}
	switch failure.Kind {
	case git.ErrorMissing:
		return domain.FaultGitMissing
	case git.ErrorUnsupported:
		return domain.FaultBadRemote
	case git.ErrorHostKey:
		return domain.FaultHostKey
	case git.ErrorAuth:
		return domain.FaultAuth
	case git.ErrorTimeout:
		return domain.FaultTimeout
	case git.ErrorUnreachable:
		return domain.FaultUnreachable
	case git.ErrorNoRepository:
		return domain.FaultNoRepository
	default:
		return domain.FaultUnknown
	}
}

func transportName(transport git.Transport) string {
	switch transport {
	case git.TransportSSH:
		return "an ssh repository"
	case git.TransportHTTPS, git.TransportHTTP:
		return "an https repository"
	default:
		return "a repository"
	}
}

// sshConfig reports where this platform keeps the per-user ssh configuration
// and whether there is one there.
//
// Absence is reported and nothing is done about it. A missing config is
// perfectly normal — ssh works without one — and creating a file in somebody's
// .ssh directory because a comparison failed would be this application making
// a decision about their authentication that is not its to make.
func sshConfig() domain.SSHConfigFile {
	path, err := git.SSHConfig()
	if err != nil {
		return domain.SSHConfigFile{}
	}
	info, err := os.Stat(path)
	return domain.SSHConfigFile{Path: path, Exists: err == nil && !info.IsDir()}
}
