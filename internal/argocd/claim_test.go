package argocd

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The gate on direct creation is this function. Every test here is about a
// wrong answer being invisible: a resource created into a namespace Argo CD
// owns looks fine until the next reconcile deletes or overwrites it.

func TestNothingClaimingATargetLeavesItUnclaimed(t *testing.T) {
	apps := []*unstructured.Unstructured{
		claimApp("argocd", "payment", "payment", nil),
	}

	claim := resolveClaim(Target{Namespace: "reporting"}, apps)

	if claim.Kind != ClaimNone {
		t.Fatalf("kind = %q (%s)", claim.Kind, claim.Reason)
	}
	if claim.App != nil {
		t.Fatalf("an unclaimed target was given an application: %+v", claim.App)
	}
}

func TestTheUnclaimedReasonDoesNotEndorseWritingDirectly(t *testing.T) {
	// Not finding an owner is not proof there is none. The wording has to stay
	// on what was checked, because a person reading "this is the correct thing
	// to do" will stop thinking about the operator nobody asked about.
	claim := resolveClaim(Target{Namespace: "reporting"}, nil)

	if strings.Contains(strings.ToLower(claim.Reason), "correct thing to do") {
		t.Fatalf("the reason endorses a direct write: %q", claim.Reason)
	}
	if !strings.Contains(claim.Reason, "not be recorded in Git") {
		t.Fatalf("the reason does not say the change is unrecorded: %q", claim.Reason)
	}
}

func TestATargetWithNothingToMatchOnDoesNotClaimToHaveChecked(t *testing.T) {
	// A cluster-scoped target arrives with neither a namespace nor a name, so
	// neither rule had anything to match on. Reporting "no Application claims
	// this resource" would be describing a check that did not happen, on a
	// screen whose entire job is knowing whether one did.
	claim := resolveClaim(Target{Kind: "Namespace"}, []*unstructured.Unstructured{
		claimApp("argocd", "super-report", "reporting", nil),
	})

	if claim.Kind != ClaimNone {
		t.Fatalf("kind = %q", claim.Kind)
	}
	if strings.Contains(claim.Reason, "No Argo CD Application claims") {
		t.Fatalf("the reason reports a check that did not happen: %q", claim.Reason)
	}
	if !strings.Contains(claim.Reason, "before anything is created") {
		t.Fatalf("the reason does not say when ownership will actually be checked: %q", claim.Reason)
	}
}

func TestAnApplicationDeployingIntoTheNamespaceClaimsIt(t *testing.T) {
	apps := []*unstructured.Unstructured{
		claimApp("argocd", "super-report", "reporting", nil),
	}

	claim := resolveClaim(Target{Namespace: "reporting"}, apps)

	if claim.Kind != ClaimNamespace || !claim.Kind.Managed() {
		t.Fatalf("kind = %q", claim.Kind)
	}
	if claim.App == nil || claim.App.Name != "super-report" {
		t.Fatalf("app = %+v", claim.App)
	}
}

func TestAnApplicationListingTheObjectOutranksTheNamespace(t *testing.T) {
	// An Application lists an object in status.resources even while the object
	// is Missing, which is exactly the state a create is aiming at. That is a
	// stronger claim than merely deploying into the namespace, and the
	// stronger one has to be the one reported.
	apps := []*unstructured.Unstructured{
		claimApp("argocd", "namespace-owner", "reporting", nil),
		claimApp("argocd", "object-owner", "other", []any{
			map[string]any{"group": "apps", "kind": "Deployment", "namespace": "reporting", "name": "report-api"},
		}),
	}

	claim := resolveClaim(Target{
		Group: "apps", Kind: "Deployment", Namespace: "reporting", Name: "report-api",
	}, apps)

	if claim.Kind != ClaimObject {
		t.Fatalf("kind = %q", claim.Kind)
	}
	if claim.App == nil || claim.App.Name != "object-owner" {
		t.Fatalf("app = %+v", claim.App)
	}
}

func TestACoreObjectIsClaimedOnTheEmptyGroup(t *testing.T) {
	// A ConfigMap's apiVersion is a bare "v1", which is the empty group and
	// not a group called "v1". Getting this wrong makes every ConfigMap in the
	// cluster look unclaimed, which is the direction that creates resources
	// into somebody's GitOps namespace.
	apps := []*unstructured.Unstructured{
		claimApp("argocd", "super-report", "other", []any{
			map[string]any{"kind": "ConfigMap", "namespace": "reporting", "name": "report-config"},
		}),
	}

	claim := resolveClaim(Target{
		Kind: "ConfigMap", Namespace: "reporting", Name: "report-config",
	}, apps)

	if claim.Kind != ClaimObject {
		t.Fatalf("kind = %q", claim.Kind)
	}
}

func TestAnApplicationListingAnotherObjectDoesNotClaimThisOne(t *testing.T) {
	apps := []*unstructured.Unstructured{
		claimApp("argocd", "super-report", "other", []any{
			map[string]any{"group": "apps", "kind": "Deployment", "namespace": "reporting", "name": "something-else"},
		}),
	}

	claim := resolveClaim(Target{
		Group: "apps", Kind: "Deployment", Namespace: "reporting", Name: "report-api",
	}, apps)

	if claim.Kind != ClaimNone {
		t.Fatalf("another object's entry claimed this one: %q", claim.Kind)
	}
}

func TestTwoEqualClaimsResolveTheSameWayEveryTime(t *testing.T) {
	// A screen that names a different Application each time it opens is worse
	// than one that names none.
	first := []*unstructured.Unstructured{
		claimApp("argocd", "beta", "reporting", nil),
		claimApp("argocd", "alpha", "reporting", nil),
	}
	second := []*unstructured.Unstructured{
		claimApp("argocd", "alpha", "reporting", nil),
		claimApp("argocd", "beta", "reporting", nil),
	}

	one := resolveClaim(Target{Namespace: "reporting"}, first)
	two := resolveClaim(Target{Namespace: "reporting"}, second)

	if one.App == nil || two.App == nil || one.App.Name != two.App.Name {
		t.Fatalf("order changed the answer: %+v then %+v", one.App, two.App)
	}
}

func TestUnknownIsNotTreatedAsUnmanaged(t *testing.T) {
	// "We could not look" must not read as "we looked and found nothing".
	if ClaimUnknown.Managed() {
		t.Fatal("an unknown claim reports as managed, which would block every create")
	}
	if rank(ClaimUnknown) <= rank(ClaimNone) {
		t.Fatal("unknown ranks no higher than a completed check that found nothing")
	}
}

// application builds an Argo CD Application with a destination and, optionally,
// the resources it says it manages.
func claimApp(namespace, name, destination string, resources []any) *unstructured.Unstructured {
	spec := map[string]any{
		"destination": map[string]any{"namespace": destination, "server": "https://kubernetes.default.svc"},
		"source": map[string]any{
			"repoURL":        "git@github.com:acme/platform-infra.git",
			"targetRevision": "main",
			"path":           "apps/" + name,
		},
	}
	var status map[string]any
	if resources != nil {
		status = map[string]any{"resources": resources}
	}
	return argoApp(namespace, name, spec, status)
}
