package argocd

import (
	"context"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
)

// This file answers the ownership question for an object that does not exist.
//
// Ownership in ownership.go reads a live object and asks what claims it. There
// is nothing to read before a resource is created, so the same question has to
// be asked of Argo CD alone: does any Application already speak for this name,
// or for the namespace it would land in.
//
// Both answers matter and they are not the same strength. An Application that
// lists the object has a desired state for it already — creating it by hand
// puts the cluster and the repository into a race. An Application that merely
// deploys into the namespace does not necessarily claim the name, but it does
// mean this namespace is somebody's GitOps destination, and a resource dropped
// into it by hand is a resource nobody's repository knows about.

// ClaimKind is how strongly Argo CD speaks for a target that does not exist.
//
// It is the domain's vocabulary rather than a second one. An alias rather than
// a distinct type because the frontend has to render the value, and a package
// type that had to be translated at the binding is a translation that can be
// wrong in one direction only.
type ClaimKind = domain.OwnershipClaim

// The claims, weakest first. Their meanings are documented on the domain type.
const (
	ClaimNone      = domain.ClaimNone
	ClaimUnknown   = domain.ClaimUnknown
	ClaimNamespace = domain.ClaimNamespace
	ClaimObject    = domain.ClaimObject
)

// Claim is what Argo CD says about a target before it exists.
type Claim struct {
	Kind   ClaimKind
	App    *domain.ArgoApp
	Reason string

	// Complete reports that the search behind this claim enumerated every
	// Application in the cluster. A ClaimNone from an incomplete search is not
	// an answer, which is why it never leaves ClaimFor as one.
	Complete bool

	Uncertainty domain.OwnershipUncertainty
	Probes      []domain.OwnershipProbe
}

// Target is the object identity a claim is checked against.
//
// Group rather than apiVersion, because that is the form Argo CD records in
// `status.resources` and the form the tracking annotation uses. A core object
// carries the empty group, which is not the same as a group called "v1".
type Target struct {
	Group     string
	Kind      string
	Namespace string
	Name      string
}

// ClaimFor asks whether an Application already speaks for a target.
//
// A failure to list Applications is reported as ClaimUnknown rather than
// returned as an error. The caller is deciding whether to offer a direct
// write, and "Argo CD could not be checked" is an answer that changes the
// wording rather than one that should collapse the screen.
//
// The search is cluster-wide. The target's namespace is where the object would
// land, not where its owner has to live: Argo CD is usually installed in a
// namespace of its own, and scoping the Application listing to the target's
// namespace — or worse, to the one the navigator happens to be showing — would
// hide every owner in every ordinary installation.
func (s *Service) ClaimFor(ctx context.Context, clusterID string, target Target) Claim {
	found := s.discover(ctx, clusterID, target.Namespace)

	if found.installed == installNo {
		return Claim{
			Kind:     ClaimNone,
			Complete: true,
			Probes:   found.probes,
			Reason: "Argo CD is not installed in this cluster, so no Application can be claiming this resource. " +
				"Direct cluster changes are available; they will not be recorded in Git by Biebie Kube.",
		}
	}

	claim := resolveClaim(target, found.apps)
	claim.Probes = found.probes

	if found.complete {
		claim.Complete = true
		return claim
	}

	// The search did not see the whole cluster. What it did see still counts
	// for a positive — an Application that lists this name lists it — but an
	// empty result from a partial search is not an absence, and reporting it
	// as one is how a resource lands in a namespace a repository owns.
	if claim.Kind.Managed() {
		return claim
	}
	return Claim{
		Kind:        ClaimUnknown,
		Uncertainty: found.uncertainty,
		Reason:      found.reason,
		Probes:      found.probes,
	}
}

// resolveClaim decides which Application speaks for a target.
//
// Split from ClaimFor for the same reason resolveOwnership is: this is the
// part that can be quietly wrong, and it is worth testing against Applications
// written by hand rather than against a cluster.
func resolveClaim(target Target, apps []*unstructured.Unstructured) Claim {
	// Ordered so two Applications with equal claims resolve the same way on
	// every call. A screen that names a different Application each time it
	// opens is worse than one that names none.
	ordered := make([]*unstructured.Unstructured, len(apps))
	copy(ordered, apps)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].GetNamespace() != ordered[j].GetNamespace() {
			return ordered[i].GetNamespace() < ordered[j].GetNamespace()
		}
		return ordered[i].GetName() < ordered[j].GetName()
	})

	id := resourceID{
		group:     target.Group,
		kind:      target.Kind,
		namespace: target.Namespace,
		name:      target.Name,
	}

	var best *unstructured.Unstructured
	bestKind := ClaimNone
	for _, app := range ordered {
		kind := ClaimNone
		switch {
		case id.kind != "" && id.name != "" && listsResource(app, id):
			kind = ClaimObject
		case target.Namespace != "" && destinationNamespace(app) == target.Namespace:
			kind = ClaimNamespace
		}
		if rank(kind) > rank(bestKind) {
			best, bestKind = app, kind
		}
	}

	if best == nil {
		// A cluster-scoped target arrives here with neither a namespace nor a
		// name, so there was nothing for either rule to match on. Saying "no
		// Application claims this" would be reporting a check that did not
		// happen; the object-level check before the write is the one that will
		// actually answer, and the sentence says so.
		if target.Namespace == "" && target.Name == "" {
			return Claim{
				Kind: ClaimNone,
				Reason: "This resource is not in a namespace, so there is no Argo CD destination to check yet. " +
					"Ownership is checked against the object's own name before anything is created. " +
					"A change made here is not recorded in Git by Biebie Kube.",
			}
		}
		return Claim{
			Kind: ClaimNone,
			Reason: "No Argo CD Application claims this resource. " +
				"Direct cluster changes are available; they will not be recorded in Git by Biebie Kube.",
		}
	}

	app := describeApp(best)
	out := Claim{Kind: bestKind, App: &app}
	switch bestKind {
	case ClaimObject:
		out.Reason = "Argo CD Application " + app.Name + " already lists this object among the resources it manages. " +
			"Its desired state belongs in that Application's source."
	case ClaimNamespace:
		out.Reason = "Argo CD Application " + app.Name + " deploys into this namespace. " +
			"A resource created here by hand is one no repository declares."
	}
	return out
}

func rank(kind ClaimKind) int {
	switch kind {
	case ClaimObject:
		return 3
	case ClaimNamespace:
		return 2
	case ClaimUnknown:
		return 1
	default:
		return 0
	}
}

// destinationNamespace reads where an Application deploys.
//
// Only the literal namespace is read. An Application whose destination is a
// server rather than a name, or one whose namespace comes from a Helm release
// setting, is not resolved here — which is why an absent match is reported as
// "none found" and never as "nothing manages this namespace".
func destinationNamespace(app *unstructured.Unstructured) string {
	value, _, _ := unstructured.NestedString(app.Object, "spec", "destination", "namespace")
	return strings.TrimSpace(value)
}
