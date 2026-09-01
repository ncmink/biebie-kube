package main

import (
	"context"
	"errors"

	"biebie-kube/internal/domain"
)

// AuthoringService creates Kubernetes objects that do not exist yet.
//
// It is a separate service from ResourceService rather than three more methods
// on it, because the two do opposite things. ResourceService reads and updates
// what is in the cluster; this one writes what is not, and the only protection
// against writing over somebody else's object is that it never updates. Two
// services means no method can drift from Create to Apply because it happened
// to be next to one that does.
//
// Everything here is a domain operation. There is deliberately no method that
// takes an executable and arguments: the frontend can ask for a manifest to be
// synthesised and cannot ask for a program to be run.
type AuthoringService struct{ core *Core }

func (s *AuthoringService) ServiceName() string { return "AuthoringService" }

// errNoAuthoring is returned when the workspace could not be placed on disk.
// The application still runs; this one feature does not.
var errNoAuthoring = errors.New("resource authoring is unavailable: Biebie Kube could not find a cache directory to work in")

// AuthoringRuntime reports whether this machine can synthesise cdk8s
// TypeScript, and what was found while looking.
//
// Detection runs on every call. Settings has a Recheck button precisely
// because somebody installs cdk8s while the window is open, and a cached
// failure would keep telling them it is missing.
func (s *AuthoringService) AuthoringRuntime(ctx context.Context) (domain.AuthoringRuntime, error) {
	if s.core.authoring == nil {
		return domain.AuthoringRuntime{YAML: true, Reason: errNoAuthoring.Error()}, nil
	}
	return s.core.authoring.Runtime(ctx), nil
}

// PrepareAuthoringRuntime installs the fixed cdk8s dependency set, once.
//
// This is the only path in the application that runs npm install, and it is
// behind a button rather than behind opening an editor. Pressing Preview must
// never be the thing that starts a network install.
func (s *AuthoringService) PrepareAuthoringRuntime(ctx context.Context) (domain.AuthoringRuntime, error) {
	if s.core.authoring == nil {
		return domain.AuthoringRuntime{YAML: true}, describe(errNoAuthoring)
	}
	runtime, err := s.core.authoring.Prepare(ctx)
	return runtime, describe(err)
}

// CreateAvailability answers whether direct creation is offered for one kind
// in one namespace, and says why when it is not.
//
// The kind is part of the question rather than only part of the answer: it is
// what decides whether a namespace is required at all, and a Namespace and a
// Deployment are not gated the same way.
func (s *AuthoringService) CreateAvailability(ctx context.Context, clusterID, namespace, kind string) (domain.CreateAvailability, error) {
	if s.core.authoring == nil {
		return domain.CreateAvailability{ClusterID: clusterID, Namespace: namespace, Kind: kind, Reason: errNoAuthoring.Error()}, nil
	}
	return s.core.authoring.Availability(ctx, clusterID, namespace, kind), nil
}

// AuthoringSession is a new editor: an identifier and the text it opens with.
type AuthoringSession struct {
	ID     string `json:"id"`
	Source string `json:"source"`
}

// NewAuthoringSession opens an authoring workspace for one kind in one
// namespace, and returns the starter text for that kind.
//
// kind and mode both cross the binding as plain strings, because the generated
// TypeScript cannot express a Go string alias and would emit a reference to a
// type it never declares.
func (s *AuthoringService) NewAuthoringSession(ctx context.Context, clusterID, namespace, kind, mode string) (AuthoringSession, error) {
	if s.core.authoring == nil {
		return AuthoringSession{}, describe(errNoAuthoring)
	}
	id, source, err := s.core.authoring.NewSession(ctx, clusterID, namespace, kind, domain.AuthoringMode(mode))
	if err != nil {
		return AuthoringSession{}, describe(err)
	}
	return AuthoringSession{ID: id, Source: source}, nil
}

// DiscardAuthoringSession removes a session's working directory.
func (s *AuthoringService) DiscardAuthoringSession(sessionID string) {
	if s.core.authoring == nil {
		return
	}
	s.core.authoring.Discard(sessionID)
}

// Synthesize runs cdk8s over one session's TypeScript and previews the result.
//
// The TypeScript never reaches the cluster. What comes back is the manifest
// cdk8s wrote, already parsed and checked, and that manifest is the only thing
// the create path will accept afterwards.
func (s *AuthoringService) Synthesize(
	ctx context.Context,
	clusterID, namespace, sessionID, source string,
) (domain.ManifestPreview, error) {
	if s.core.authoring == nil {
		return domain.ManifestPreview{}, describe(errNoAuthoring)
	}

	manifest, output, err := s.core.authoring.Synthesize(ctx, sessionID, source)
	if err != nil {
		return domain.ManifestPreview{Output: output}, describe(err)
	}

	preview, err := s.core.authoring.Preview(ctx, clusterID, namespace, manifest)
	preview.Output = output
	return preview, describe(err)
}

// Validate parses a manifest and reports what would stop it being created.
//
// Nothing is sent to the cluster except reads. There is no dry-run here: the
// cluster is asked what it has, never asked what it would do.
func (s *AuthoringService) Validate(ctx context.Context, clusterID, namespace, manifest string) (domain.ManifestPreview, error) {
	if s.core.authoring == nil {
		return domain.ManifestPreview{}, describe(errNoAuthoring)
	}
	preview, err := s.core.authoring.Preview(ctx, clusterID, namespace, manifest)
	return preview, describe(err)
}

// CreateResources creates every object in a manifest.
//
// Ownership and existence are checked again immediately before the write. The
// preview a person read may be minutes old, and both of the things it was
// checking can change while an editor is open.
func (s *AuthoringService) CreateResources(ctx context.Context, clusterID, namespace, manifest string) (domain.CreateOutcome, error) {
	if s.core.authoring == nil {
		return domain.CreateOutcome{}, describe(errNoAuthoring)
	}
	outcome, err := s.core.authoring.Create(ctx, clusterID, namespace, manifest)
	return outcome, describe(err)
}
