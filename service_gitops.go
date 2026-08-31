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

// LocateManifest looks for the document in Git that declares one live object.
//
// This is the expensive half of the GitOps panel and is never called when a
// drawer opens: the first read of a repository clones it. It runs when
// somebody asks for it, and it answers with what it found rather than with
// what it assumes — a directory whose manifests are rendered has no file to
// name, and two files declaring one name is a state of the repository rather
// than a choice this code should make.
func (s *GitOpsService) LocateManifest(ctx context.Context, clusterID string, ref domain.ResourceRef) (domain.ManifestSearch, error) {
	if s.core.gitops == nil {
		// A machine that would not name a cache directory has no repository
		// mirror to read. Everything else in the panel still works.
		return domain.ManifestSearch{Ref: ref}, errors.New("this machine has no cache directory for repository mirrors")
	}
	search, err := s.core.gitops.Locate(ctx, clusterID, ref)
	return search, describe(err)
}
