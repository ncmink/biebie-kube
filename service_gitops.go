package main

import (
	"context"
	"errors"

	"biebie-kube/internal/domain"
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
