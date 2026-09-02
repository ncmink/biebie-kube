// Package manifest reads and writes resources as YAML for the editor.
//
// The rule this package enforces: a live resource is never silently
// overwritten. The editor shows what the cluster has, the user edits it, and
// Biebie Kube shows the difference before anything is sent.
package manifest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"biebie-kube/internal/argocd"
	"biebie-kube/internal/cluster"
	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
	"biebie-kube/internal/resources"
)

// Service converts between live objects and editable YAML.
type Service struct {
	clusters  *cluster.Manager
	resources *resources.Service

	// argo answers whether a persistent change to an object belongs here or in
	// a repository. It is a dependency rather than an optional extra for the
	// same reason it is one on the authoring service: the gate is the feature,
	// and a service that could not ask would be one that always said yes.
	argo *argocd.Service
}

// NewService wires the service.
func NewService(clusters *cluster.Manager, res *resources.Service, argo *argocd.Service) *Service {
	return &Service{clusters: clusters, resources: res, argo: argo}
}

// Read returns a resource as YAML, cleaned of the fields Kubernetes manages.
//
// Server-side noise — managedFields above all — makes a manifest unreadable
// and is rejected or ignored on the way back, so it is stripped for the
// editor rather than shown and then quietly dropped.
func (s *Service) Read(ctx context.Context, clusterID string, ref domain.ResourceRef) (string, error) {
	obj, err := s.resources.Get(ctx, clusterID, ref)
	if err != nil {
		return "", err
	}
	return render(obj)
}

// OpenSession captures the object an editor will be compared against.
//
// This is the moment "Original" is decided, and it is decided once. Everything
// the editor shows afterwards is held against this text: a watch event, a
// controller writing to the object, another engineer applying something — none
// of them may replace it, because the question the editor answers is "what
// have I changed since I opened this", and an Original that moves makes that
// question unanswerable without saying so.
//
// The live change is not ignored. It is a different fact, reported through
// Freshness before a write, where it can be shown as the conflict it is.
func (s *Service) OpenSession(ctx context.Context, clusterID string, ref domain.ResourceRef) (domain.EditSession, error) {
	obj, err := s.resources.Get(ctx, clusterID, ref)
	if err != nil {
		return domain.EditSession{}, err
	}
	original, err := render(obj)
	if err != nil {
		return domain.EditSession{}, err
	}

	return domain.EditSession{
		Ref:      ref,
		Original: original,
		// Taken from the object the snapshot was rendered from, not from a
		// second read. Two reads are two moments, and the version would then
		// guard something other than what is on screen.
		ResourceVersion: obj.GetResourceVersion(),
		OpenedAt:        time.Now(),
		Gate:            s.gate(ctx, clusterID, ref),
	}, nil
}

// gate asks whether a persistent change to this object may be written here.
//
// A failed ownership check produces a refusal, never a permission. It also
// never produces an error: the editor still opens, the YAML is still readable,
// and only the write is withheld — an Argo CD permission this account lacks is
// not a reason to take the inspector away.
func (s *Service) gate(ctx context.Context, clusterID string, ref domain.ResourceRef) domain.MutationGate {
	if s.argo == nil {
		return argocd.UnknownGate("Biebie Kube could not check whether Argo CD manages this object, so persistent changes are not offered.")
	}
	found, err := s.argo.Ownership(ctx, clusterID, ref)
	if err != nil {
		return argocd.UnknownGate("Biebie Kube could not check whether Argo CD manages this object: " + oneLine(err.Error()))
	}
	return argocd.GateForOwnership(found)
}

// Freshness reports whether the live object moved since the editor opened.
//
// Asked rather than watched, and asked before a write rather than during one.
// The answer is a value rather than an error because the caller has a decision
// to offer — take a fresh snapshot, or leave the edits alone — and an error
// would make that decision by collapsing the screen.
func (s *Service) Freshness(
	ctx context.Context,
	clusterID string,
	ref domain.ResourceRef,
	opened string,
) (domain.EditFreshness, error) {
	if opened == "" {
		return domain.EditFreshness{
			Unchecked: "This editor did not record the version it opened at, so whether the object has changed since is unknown.",
		}, nil
	}

	current, err := s.resources.Get(ctx, clusterID, ref)
	if apierrors.IsNotFound(err) {
		return domain.EditFreshness{
			Gone:   true,
			Reason: "This object no longer exists in the cluster. Applying would not recreate it: the editor updates objects and does not create them.",
		}, nil
	}
	if err != nil {
		return domain.EditFreshness{Unchecked: oneLine(err.Error())}, nil
	}

	version := current.GetResourceVersion()
	if version == opened {
		return domain.EditFreshness{ResourceVersion: version}, nil
	}
	return domain.EditFreshness{
		Stale:           true,
		ResourceVersion: version,
		Reason: "This object changed in the cluster after the editor was opened. " +
			"Your edits are still here; applying them now would be a write against a version that no longer exists.",
	}, nil
}

// Apply writes edited YAML back to the cluster.
//
// Three guards, in order, and all three are here rather than in the frontend.
//
// Ownership first: an object whose desired state lives in a repository is not
// edited through a cluster, and an object whose ownership could not be
// established is not either. A screen can be wrong about that; this cannot be
// bypassed by one that is.
//
// Identity second: a manifest whose name or namespace was changed in the editor
// would create a second object rather than update the one on screen.
//
// Concurrency last, and it is the version the editor opened at rather than the
// one the cluster holds now. Stamping the current version would make every
// write succeed by definition, which is the same as having no protection while
// appearing to have some.
func (s *Service) Apply(
	ctx context.Context,
	clusterID string,
	ref domain.ResourceRef,
	edited string,
	opened string,
) (domain.ApplyResult, error) {
	if gate := s.gate(ctx, clusterID, ref); !gate.Allowed {
		return domain.ApplyResult{}, errors.New("nothing was applied. " + gate.Reason)
	}

	info, ok := s.clusters.LookupKind(clusterID, ref.Kind)
	if !ok {
		return domain.ApplyResult{}, fmt.Errorf("unknown resource type %q", ref.Kind)
	}
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return domain.ApplyResult{}, err
	}

	parsed, err := parse(edited)
	if err != nil {
		return domain.ApplyResult{}, err
	}
	if name := parsed.GetName(); name != ref.Name {
		return domain.ApplyResult{}, fmt.Errorf(
			"this manifest is named %q but you are editing %q; renaming creates a new object rather than updating this one",
			name, ref.Name)
	}
	if info.Namespaced && parsed.GetNamespace() != "" && parsed.GetNamespace() != ref.Namespace {
		return domain.ApplyResult{}, fmt.Errorf(
			"this manifest is in namespace %q but you are editing %q",
			parsed.GetNamespace(), ref.Namespace)
	}

	current, err := s.resources.Get(ctx, clusterID, ref)
	if err != nil {
		return domain.ApplyResult{}, err
	}

	if err := guard(opened, current.GetResourceVersion()); err != nil {
		return domain.ApplyResult{}, err
	}
	parsed.SetResourceVersion(opened)
	if info.Namespaced {
		parsed.SetNamespace(ref.Namespace)
	}

	gvr := kube.GVRFor(info.Group, info.Version, info.Resource)

	var updated *unstructured.Unstructured
	if info.Namespaced {
		updated, err = client.Dynamic.Resource(gvr).Namespace(ref.Namespace).
			Update(ctx, parsed, metav1.UpdateOptions{})
	} else {
		updated, err = client.Dynamic.Resource(gvr).Update(ctx, parsed, metav1.UpdateOptions{})
	}
	if err != nil {
		return domain.ApplyResult{}, describeApplyError(err)
	}

	return domain.ApplyResult{
		Ref:     ref,
		Changed: updated.GetResourceVersion() != current.GetResourceVersion(),
		Message: "Applied to " + ref.Name,
	}, nil
}

// render turns a live object into the text the editor shows.
//
// Everything removed here is either written by the API server or rejected on
// the way back. resourceVersion goes with the rest: the editor's concurrency
// token is carried on the session rather than in the text, so it cannot be
// edited away by somebody tidying up a manifest.
func render(obj *unstructured.Unstructured) (string, error) {
	clean := obj.DeepCopy()
	unstructured.RemoveNestedField(clean.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(clean.Object, "metadata", "generation")
	unstructured.RemoveNestedField(clean.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(clean.Object, "metadata", "uid")
	unstructured.RemoveNestedField(clean.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(clean.Object, "metadata", "annotations",
		"kubectl.kubernetes.io/last-applied-configuration")
	unstructured.RemoveNestedField(clean.Object, "status")

	encoded, err := yaml.Marshal(clean.Object)
	if err != nil {
		return "", fmt.Errorf("render YAML: %w", err)
	}
	return string(encoded), nil
}

func oneLine(message string) string {
	return strings.TrimSpace(strings.SplitN(message, "\n", 2)[0])
}

func parse(raw string) (*unstructured.Unstructured, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("the manifest is empty")
	}
	var object map[string]any
	if err := yaml.Unmarshal([]byte(raw), &object); err != nil {
		return nil, fmt.Errorf("this is not valid YAML: %w", err)
	}
	parsed := &unstructured.Unstructured{Object: object}
	if parsed.GetKind() == "" {
		return nil, errors.New("the manifest has no kind")
	}
	if parsed.GetName() == "" {
		return nil, errors.New("the manifest has no metadata.name")
	}
	return parsed, nil
}

// guard decides whether a write may go ahead, given the version the editor
// opened at and the version the cluster holds now.
//
// A missing version is refused rather than tolerated. Falling back to the live
// version is what the code did before this, and it is worse than having no
// protection: every write succeeds by definition while the interface implies
// something is being checked.
func guard(opened, live string) error {
	if opened == "" {
		return errors.New(
			"this edit carries no version to write against, so nothing was applied. " +
				"Reopen the editor and reapply your changes")
	}
	if live != opened {
		return errors.New(
			"this object changed in the cluster after the editor was opened, so nothing was written. " +
				"Refresh the original to see what it looks like now, then reapply your changes")
	}
	return nil
}

func describeApplyError(err error) error {
	message := err.Error()
	switch {
	case strings.Contains(message, "the object has been modified"):
		return errors.New("this resource changed in the cluster while you were editing it; reload and reapply")
	case strings.Contains(message, "is invalid"):
		return fmt.Errorf("the cluster rejected this manifest: %w", err)
	default:
		return fmt.Errorf("apply: %w", err)
	}
}
