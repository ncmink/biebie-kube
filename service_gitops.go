package main

import (
	"context"
	"errors"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/git"
)

// GitOpsService answers questions about the desired state behind a live
// object, as opposed to the object itself.
//
// It is separate from ArgoCDService because the two ask different things of
// different places. Ownership is read from Argo CD's own objects in the
// cluster and costs a list; locating a manifest leaves the cluster entirely
// and reads a repository.
type GitOpsService struct{ core *Core }

func (s *GitOpsService) ServiceName() string { return "GitOpsService" }

// CompareWithSource finds the manifest in Git that declares one live object and
// holds the two against each other.
//
// Source rather than desired state: what this reads is a file, and a rendering
// step or a pipeline may sit between that file and what Argo CD applied.
//
// This is the expensive half of the GitOps panel and is never called when a
// drawer opens: the first read of a repository clones it. It runs when
// somebody asks for it, and it answers with what it found rather than with
// what it assumes — a directory whose manifests are rendered has no file to
// name, and two files declaring one name is a state of the repository rather
// than a choice this code should make.
//
// Both answers come back together because they must come from the same commit.
// Two calls would be two reads of a branch that moves, and the panel could end
// up showing a file from one commit beside a difference computed against
// another with nothing on screen saying so.
func (s *GitOpsService) CompareWithSource(ctx context.Context, clusterID string, ref domain.ResourceRef) (domain.SourceState, error) {
	if s.core.gitops == nil {
		// A machine that would not name a cache directory has no repository
		// mirror to read. Everything else in the panel still works.
		return domain.SourceState{Ref: ref}, errors.New("this machine has no cache directory for repository mirrors")
	}
	state, err := s.core.gitops.Compare(ctx, clusterID, ref)
	return state, describe(err)
}

// DiagnoseGitAccess works out which part of the path to the repository is
// refusing.
//
// Behind its own press rather than run when a comparison fails. It asks a
// server questions, and doing that automatically would mean every failed
// comparison in a list of resources quietly opened connections nobody asked
// for. Nothing it does writes anything, anywhere.
func (s *GitOpsService) DiagnoseGitAccess(ctx context.Context, clusterID string, ref domain.ResourceRef) (domain.GitAccess, error) {
	if s.core.gitops == nil {
		return domain.GitAccess{}, errors.New("this machine has no cache directory for repository mirrors")
	}
	access, err := s.core.gitops.Diagnose(ctx, clusterID, ref)
	return access, describe(err)
}

// TestGitIdentity asks the git host which account it authenticates as.
//
// Its own call because it is its own question, and one worth being explicit
// about: it opens an ssh connection to somebody's git host. The answer is
// about authentication only — a host that greets you by name has said nothing
// about which repositories are yours.
func (s *GitOpsService) TestGitIdentity(ctx context.Context, clusterID string, ref domain.ResourceRef) (domain.GitIdentity, error) {
	if s.core.gitops == nil {
		return domain.GitIdentity{}, errors.New("this machine has no cache directory for repository mirrors")
	}
	identity, err := s.core.gitops.Identify(ctx, clusterID, ref)
	return identity, describe(err)
}

// RevealSSHConfig shows the ssh configuration file in the file manager.
//
// Shows rather than opens, and shows rather than edits. Which key reaches
// which host is the engineer's decision, and an application that rewrote a
// Host block because a comparison failed would be making it for them.
func (s *GitOpsService) RevealSSHConfig(ctx context.Context, path string) error {
	if s.core.reveal == nil {
		return errors.New("this platform has no file manager to show the file in")
	}

	// The path is resolved here rather than trusted from the window. The
	// frontend was given one by the diagnosis and has no business substituting
	// another, and this is the only ssh path there is to show.
	resolved, err := git.SSHConfig()
	if err != nil {
		return describe(err)
	}
	if path != "" && path != resolved {
		return errors.New("that is not this machine's ssh configuration file")
	}
	return describe(s.core.reveal.Reveal(ctx, resolved))
}
