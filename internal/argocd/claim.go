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
type ClaimKind string

// The claims, weakest first.
const (
	// ClaimNone is nothing found. It is not proof that nothing exists: a
	// cluster this account cannot list Applications in produces ClaimUnknown,
	// and even a full listing says nothing about controllers that are not
	// Argo CD.
	ClaimNone ClaimKind = "none"

	// ClaimUnknown is Argo CD not having been consulted — not installed, or
	// not listable by this account. It is separated from ClaimNone because
	// "we looked and found nothing" and "we could not look" lead to different
	// sentences, and only one of them is an answer.
	ClaimUnknown ClaimKind = "unknown"

	// ClaimNamespace is an Application whose destination is this namespace.
	ClaimNamespace ClaimKind = "namespace"

	// ClaimObject is an Application that lists this exact object among its
	// resources, which it does even while the object is Missing.
	ClaimObject ClaimKind = "object"
)

// Managed reports whether a claim should stop a direct write.
func (k ClaimKind) Managed() bool { return k == ClaimNamespace || k == ClaimObject }

// Claim is what Argo CD says about a target before it exists.
type Claim struct {
	Kind   ClaimKind
	App    *domain.ArgoApp
	Reason string
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
func (s *Service) ClaimFor(ctx context.Context, clusterID string, target Target) Claim {
	if !s.Installed(clusterID) {
		return Claim{
			Kind:   ClaimNone,
			Reason: "Argo CD is not installed in this cluster, so no Application can be claiming this resource.",
		}
	}

	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return Claim{Kind: ClaimUnknown, Reason: "This cluster is not connected, so Argo CD ownership could not be checked."}
	}

	apps, err := listAll(ctx, client, applicationGVR)
	if err != nil {
		return Claim{
			Kind:   ClaimUnknown,
			Reason: "This account cannot list Argo CD Applications, so whether one claims this resource is unknown.",
		}
	}
	return resolveClaim(target, apps)
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
