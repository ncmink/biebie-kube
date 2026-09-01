package authoring

import (
	"context"
	"errors"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/argocd"
	"biebie-kube/internal/cluster"
	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
)

// Service is the authoring surface the Wails layer calls.
type Service struct {
	clusters  *cluster.Manager
	argo      *argocd.Service
	workspace *Workspace
	detector  *Detector
	runner    runner
}

// NewService wires authoring against a cluster manager and an Argo CD service.
//
// Argo CD is a dependency rather than an optional extra because the gate is
// the feature. Direct creation is offered on the strength of nothing being
// found that claims the target, and a service that could not ask would be one
// that always answered "unmanaged".
func NewService(clusters *cluster.Manager, argo *argocd.Service, root string) *Service {
	return &Service{
		clusters:  clusters,
		argo:      argo,
		workspace: NewWorkspace(root),
		detector:  NewDetector(),
		runner:    systemRunner(),
	}
}

// Runtime reports what this machine can author with, freshly each time.
func (s *Service) Runtime(ctx context.Context) domain.AuthoringRuntime {
	out := s.detector.Detect(ctx)
	out.Prepared = s.workspace.Prepared()
	if out.TypeScript && !out.Prepared {
		out.Reason = "Node.js, npm and cdk8s were all found. The cdk8s dependencies still need installing once before TypeScript authoring can run."
	}
	return out
}

// Availability answers whether creation is offered for one namespace.
//
// The two halves of the answer are computed separately and both are returned.
// A GitOps-managed namespace and a missing TypeScript runtime are different
// refusals with different fixes, and collapsing them into one boolean would
// leave the screen unable to say which one it is looking at.
func (s *Service) Availability(ctx context.Context, clusterID, namespace, kind string) domain.CreateAvailability {
	runtime := s.Runtime(ctx)

	resolved, known := s.resolveTarget(clusterID, namespace, kind)
	if !known {
		return domain.CreateAvailability{
			ClusterID: clusterID, Namespace: namespace, Kind: kind, Runtime: runtime,
			Reason: "This cluster does not serve that resource type, so there is nothing to create.",
		}
	}

	// The namespace question comes before the ownership one because it decides
	// whether the ownership one can be asked at all. With no namespace chosen,
	// the check below has nothing to match an Application's destination
	// against, and would answer "unmanaged" without having looked — which is
	// the wrong answer to be confident about in a production cluster.
	if resolved.namespaced && namespace == "" {
		return domain.CreateAvailability{
			ClusterID: clusterID, Namespace: namespace, Kind: kind, Runtime: runtime,
			TargetKind:     resolved.kind,
			Namespaced:     true,
			NeedsNamespace: true,
			Reason: resolved.kind + " belongs to a namespace, and no namespace is selected. " +
				"Choose one first: it is where the object goes, and it is what Biebie Kube checks for an Argo CD owner.",
		}
	}

	claim := s.argo.ClaimFor(ctx, clusterID, argocd.Target{
		Kind: resolved.kind, Namespace: namespace,
	})
	out := decide(claim, runtime)
	out.ClusterID, out.Namespace, out.Kind = clusterID, namespace, kind
	out.TargetKind, out.Namespaced = resolved.kind, resolved.namespaced
	return out
}

// decide combines the two halves of the gate.
//
// Separated from the call that fetches them because this is the part that can
// be quietly wrong: a rule that let a create through on an unanswered
// ownership check would look exactly like one that worked, right up until a
// resource appeared in a namespace a repository owns.
func decide(claim argocd.Claim, runtime domain.AuthoringRuntime) domain.CreateAvailability {
	out := domain.CreateAvailability{
		Runtime: runtime,
		App:     claim.App,
		Managed: claim.Kind.Managed(),
		Reason:  claim.Reason,
	}

	// Only a completed check that found nothing opens the gate. ClaimUnknown
	// is not an answer, and treating "we could not look" as "nothing is there"
	// is the one mistake in this file nobody would notice until afterwards.
	if claim.Kind != argocd.ClaimNone {
		return out
	}

	out.Allowed = true
	out.Modes = []domain.AuthoringMode{domain.AuthoringYAML}
	if runtime.TypeScript && runtime.Prepared {
		out.Modes = append([]domain.AuthoringMode{domain.AuthoringTypeScript}, out.Modes...)
	}
	return out
}

// NewSession opens an authoring workspace and returns its starter text.
//
// The session directory is created for both modes even though YAML does not
// need one, so the frontend holds a single identifier and the two surfaces are
// not two different lifecycles to reason about.
func (s *Service) NewSession(ctx context.Context, clusterID, namespace, kind string, mode domain.AuthoringMode) (string, string, error) {
	if available := s.Availability(ctx, clusterID, namespace, kind); !available.Allowed {
		return "", "", errors.New(available.Reason)
	}
	resolved, known := s.resolveTarget(clusterID, namespace, kind)
	if !known {
		return "", "", errors.New("this cluster does not serve that resource type")
	}

	session, err := s.workspace.NewSession()
	if err != nil {
		return "", "", err
	}
	if mode == domain.AuthoringTypeScript {
		return session.ID, typescriptStarter(resolved), nil
	}
	return session.ID, yamlStarter(resolved), nil
}

// Discard removes a session's directory.
func (s *Service) Discard(sessionID string) { s.workspace.Discard(sessionID) }

// Preview parses a manifest and checks everything that can be checked without
// changing anything.
//
// Nothing here mutates. Existence is read with a Get, discovery is read from
// the connected session's cache, and no dry-run is sent — the cluster is asked
// what it has, never asked what it would do.
func (s *Service) Preview(ctx context.Context, clusterID, namespace, manifest string) (domain.ManifestPreview, error) {
	out := domain.ManifestPreview{YAML: manifest}

	objects, problems := parseAll(manifest)
	out.Problems = problems

	live := make([]*unstructured.Unstructured, 0, len(objects))
	for _, obj := range objects {
		if obj != nil {
			live = append(live, obj)
		}
	}
	if len(live) == 0 {
		if len(out.Problems) == 0 {
			out.Problems = []domain.ManifestProblem{{Resource: -1, Message: "The manifest declares no object."}}
		}
		return out, nil
	}

	out.Problems = append(out.Problems, duplicateProblems(objects)...)

	for index, obj := range objects {
		if obj == nil {
			continue
		}
		out.Problems = append(out.Problems, identityProblems(index, obj)...)

		described, checks := s.describe(ctx, clusterID, namespace, index, obj)
		out.Resources = append(out.Resources, described)
		out.Problems = append(out.Problems, checks...)
	}

	// Re-encoding is what makes the preview and the write the same text. It is
	// skipped when a document failed to parse, because there is nothing to
	// encode and the person needs the text they typed, not a partial copy.
	if len(problems) == 0 {
		if encoded, err := render(objects); err == nil {
			out.YAML = encoded
		}
	}

	out.Ready = len(out.Problems) == 0
	return out, nil
}

// describe works out what one document is and whether it can be created.
func (s *Service) describe(
	ctx context.Context,
	clusterID, namespace string,
	index int,
	obj *unstructured.Unstructured,
) (domain.ManifestResource, []domain.ManifestProblem) {
	out := domain.ManifestResource{
		APIVersion: obj.GetAPIVersion(),
		Kind:       obj.GetKind(),
		Name:       obj.GetName(),
		Namespace:  obj.GetNamespace(),
	}

	resource, found := s.resolve(clusterID, obj)
	out.Known = found
	out.Namespaced = found && resource.Namespaced

	var problems []domain.ManifestProblem
	if !found {
		if out.Kind != "" && out.APIVersion != "" {
			problems = append(problems, domain.ManifestProblem{
				Resource: index,
				Message:  fmt.Sprintf("This cluster does not serve %s %s.", out.APIVersion, out.Kind),
			})
		}
		return out, problems
	}

	if problem := namespaceProblem(index, obj, namespace, resource.Namespaced, true); problem != nil {
		problems = append(problems, *problem)
	}
	if out.Name == "" {
		return out, problems
	}

	exists, err := s.exists(ctx, clusterID, resource, obj)
	switch {
	case err != nil:
		problems = append(problems, domain.ManifestProblem{
			Resource: index,
			Message:  "Whether this object already exists could not be checked: " + oneLine(err.Error()),
		})
	case exists:
		out.Exists = true
		problems = append(problems, domain.ManifestProblem{
			Resource: index,
			Message: fmt.Sprintf("%s/%s already exists. Biebie Kube creates objects; editing an existing one is the YAML editor on that resource.",
				out.Kind, out.Name),
		})
	}
	return out, problems
}

// resolve maps a manifest's apiVersion and kind onto what the cluster serves.
//
// Discovery rather than the compiled-in catalogue, because a manifest may name
// a custom resource and because the catalogue is keyed by plural name while a
// manifest is keyed by Kind.
func (s *Service) resolve(clusterID string, obj *unstructured.Unstructured) (kube.APIResource, bool) {
	group, version := groupVersion(obj.GetAPIVersion())
	kind := obj.GetKind()
	if kind == "" || version == "" {
		return kube.APIResource{}, false
	}
	for _, candidate := range s.clusters.APIResources(clusterID) {
		if candidate.Group == group && candidate.Version == version && candidate.Kind == kind {
			return candidate, true
		}
	}
	return kube.APIResource{}, false
}

func groupVersion(apiVersion string) (string, string) {
	if before, after, found := strings.Cut(apiVersion, "/"); found {
		return before, after
	}
	return "", apiVersion
}

// exists asks the cluster whether the name is already taken.
//
// NotFound is the answer this is looking for and is not an error. Anything
// else — a forbidden namespace, an unreachable API server — is, because "we
// could not tell" must not be reported as "it is free".
func (s *Service) exists(
	ctx context.Context,
	clusterID string,
	resource kube.APIResource,
	obj *unstructured.Unstructured,
) (bool, error) {
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return false, err
	}

	gvr := kube.GVRFor(resource.Group, resource.Version, resource.Resource)
	if resource.Namespaced {
		_, err = client.Dynamic.Resource(gvr).Namespace(obj.GetNamespace()).
			Get(ctx, obj.GetName(), metav1.GetOptions{})
	} else {
		_, err = client.Dynamic.Resource(gvr).Get(ctx, obj.GetName(), metav1.GetOptions{})
	}
	switch {
	case err == nil:
		return true, nil
	case apierrors.IsNotFound(err):
		return false, nil
	default:
		return false, err
	}
}

// Create sends a checked manifest to the cluster.
//
// The order is preflight, then gate, then write, and it is that way round on
// purpose. The preview a person read may be minutes old: an object may have
// appeared under the same name, and an Argo CD Application may have started
// claiming the namespace while the editor was open. Both are re-checked
// against the cluster immediately before anything is sent.
func (s *Service) Create(ctx context.Context, clusterID, namespace, manifest string) (domain.CreateOutcome, error) {
	preview, err := s.Preview(ctx, clusterID, namespace, manifest)
	if err != nil {
		return domain.CreateOutcome{}, err
	}
	if !preview.Ready {
		return domain.CreateOutcome{}, fmt.Errorf(
			"nothing was created: %s", firstProblem(preview.Problems))
	}

	objects, problems := parseAll(preview.YAML)
	if len(problems) > 0 {
		return domain.CreateOutcome{}, errors.New("nothing was created: " + problems[0].Message)
	}

	// Ownership is re-checked per object rather than for the namespace alone.
	// An Application that started listing this exact name is the case worth
	// catching, and it is invisible to a namespace-level question.
	for _, obj := range objects {
		group, _ := groupVersion(obj.GetAPIVersion())
		claim := s.argo.ClaimFor(ctx, clusterID, argocd.Target{
			Group:     group,
			Kind:      obj.GetKind(),
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		})
		if claim.Kind.Managed() || claim.Kind == argocd.ClaimUnknown {
			return domain.CreateOutcome{}, errors.New("nothing was created. " + claim.Reason)
		}
	}

	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return domain.CreateOutcome{}, err
	}

	var out domain.CreateOutcome
	for _, obj := range objects {
		resource, found := s.resolve(clusterID, obj)
		if !found {
			out.Failed = append(out.Failed, failure(obj, "this cluster does not serve that kind"))
			break
		}

		gvr := kube.GVRFor(resource.Group, resource.Version, resource.Resource)
		var created *unstructured.Unstructured
		if resource.Namespaced {
			created, err = client.Dynamic.Resource(gvr).Namespace(obj.GetNamespace()).
				Create(ctx, obj, metav1.CreateOptions{})
		} else {
			created, err = client.Dynamic.Resource(gvr).Create(ctx, obj, metav1.CreateOptions{})
		}
		if err != nil {
			out.Failed = append(out.Failed, failure(obj, createError(err)))
			// Stopping rather than carrying on. A manifest is one intention,
			// and creating the objects after a failure would leave a state
			// nobody asked for and nobody can name.
			break
		}

		out.Created = append(out.Created, domain.CreatedResource{
			Kind: created.GetKind(),
			Ref: domain.ResourceRef{
				Kind:      s.kindOf(clusterID, resource),
				Namespace: created.GetNamespace(),
				Name:      created.GetName(),
			},
		})
	}

	out.Message = outcomeMessage(out)
	return out, nil
}

// kindOf finds the navigation kind for a resource the cluster served, so the
// UI can open what was created. An unmapped resource returns empty, which the
// frontend reads as "created, but there is no list to open".
func (s *Service) kindOf(clusterID string, resource kube.APIResource) domain.Kind {
	for _, info := range s.clusters.Catalogue(clusterID) {
		if info.Group == resource.Group && info.Resource == resource.Resource {
			return info.Kind
		}
	}
	return ""
}

func failure(obj *unstructured.Unstructured, reason string) domain.CreateFailure {
	return domain.CreateFailure{
		Kind:      obj.GetKind(),
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		Error:     reason,
	}
}

// createError words the refusals worth distinguishing.
func createError(err error) string {
	switch {
	case apierrors.IsAlreadyExists(err):
		return "an object of that name already exists"
	case apierrors.IsForbidden(err):
		return "this account may not create that object"
	case apierrors.IsInvalid(err):
		return "the cluster rejected this manifest: " + oneLine(err.Error())
	default:
		return oneLine(err.Error())
	}
}

// outcomeMessage reports exactly what happened, including a partial result.
//
// Kubernetes has no transaction across objects. A manifest of three that fails
// on the third leaves two behind, and there is no rollback here — so the
// sentence says so rather than implying the cluster is as it was.
func outcomeMessage(out domain.CreateOutcome) string {
	switch {
	case len(out.Failed) == 0 && len(out.Created) == 1:
		return "Created " + out.Created[0].Ref.Name + "."
	case len(out.Failed) == 0:
		return fmt.Sprintf("Created %d resources.", len(out.Created))
	case len(out.Created) == 0:
		return "Nothing was created: " + out.Failed[0].Error + "."
	default:
		return fmt.Sprintf(
			"%d of %d created, then %s failed: %s. What was already created is still in the cluster; Biebie Kube did not remove it.",
			len(out.Created), len(out.Created)+len(out.Failed),
			out.Failed[0].Name, out.Failed[0].Error)
	}
}

func firstProblem(problems []domain.ManifestProblem) string {
	if len(problems) == 0 {
		return "the manifest is not ready"
	}
	if len(problems) == 1 {
		return problems[0].Message
	}
	return fmt.Sprintf("%s (and %d more)", problems[0].Message, len(problems)-1)
}
