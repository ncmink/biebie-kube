package argocd

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
)

// resolveOwnership decides what the evidence says; settle decides what may be
// concluded from it given how much of the cluster was visible. These are the
// tests for the second half, which is the half that can be wrong in a way
// nobody notices until a reconcile deletes something.

func TestAnApplicationInAnotherNamespaceStillManagesTheObject(t *testing.T) {
	// Argo CD in `argocd`, the Deployment in `reporting`, and the navigator
	// showing `reporting` only. Ownership never sees the navigator, so the
	// only thing this can depend on is the Application being in the search —
	// and it is, because the search is cluster-wide.
	deployment := object("apps/v1", "Deployment", "reporting", "super-report", nil, map[string]string{
		TrackingAnnotation: "super-report:apps/Deployment:reporting/super-report",
	})
	apps := []*unstructured.Unstructured{
		argoApp("argocd", "super-report", gitSpec("apps/reporting"), nil),
	}

	out := resolveOwnership(reportingRef(), deployment, apps)
	settle(&out, search{complete: true, installed: installYes})

	if out.Status != domain.OwnershipStatusManaged {
		t.Fatalf("status = %q (%s)", out.Status, out.Reason)
	}
	if out.App == nil || out.App.Namespace != "argocd" {
		t.Fatalf("app = %+v", out.App)
	}
}

func TestAnApplicationSetGeneratedApplicationOwnsLikeAnyOther(t *testing.T) {
	// The effective owner is the concrete Application. Where its generator
	// lives, and whether that namespace is selected anywhere, changes nothing
	// about who manages the Deployment.
	app := argoApp("argocd", "super-report", gitSpec("apps/reporting"), nil)
	app.SetOwnerReferences(ownerReference("ApplicationSet", "reporting-per-env"))

	deployment := object("apps/v1", "Deployment", "reporting", "super-report", nil, map[string]string{
		TrackingAnnotation: "super-report:apps/Deployment:reporting/super-report",
	})

	out := resolveOwnership(reportingRef(), deployment, []*unstructured.Unstructured{app})
	settle(&out, search{complete: true, installed: installYes})

	if out.Status != domain.OwnershipStatusManaged {
		t.Fatalf("status = %q", out.Status)
	}
	if out.GeneratedBy != "reporting-per-env" {
		t.Fatalf("generatedBy = %q; editing a generated Application is overwritten on the next reconcile", out.GeneratedBy)
	}
}

func TestACompletedSearchThatFoundNothingIsUnmanaged(t *testing.T) {
	deployment := object("apps/v1", "Deployment", "reporting", "super-report", nil, nil)
	apps := []*unstructured.Unstructured{argoApp("argocd", "payment", gitSpec("apps/payment"), nil)}

	out := resolveOwnership(reportingRef(), deployment, apps)
	settle(&out, search{complete: true, installed: installYes})

	if out.Status != domain.OwnershipStatusUnmanaged {
		t.Fatalf("status = %q (%s)", out.Status, out.Reason)
	}
}

func TestAnIncompleteSearchThatFoundNothingIsUnknown(t *testing.T) {
	// The whole point of the type. The evidence is identical to the test above
	// — no Application claims this object — and the conclusion is different,
	// because one search saw the cluster and the other did not.
	deployment := object("apps/v1", "Deployment", "reporting", "super-report", nil, nil)

	out := resolveOwnership(reportingRef(), deployment, nil)
	settle(&out, search{
		installed:   installYes,
		uncertainty: domain.UncertaintyForbidden,
		reason:      "This account may not list Argo CD Applications.",
	})

	if out.Status != domain.OwnershipStatusUnknown {
		t.Fatalf("status = %q; an unreadable cluster was reported as an unowned object", out.Status)
	}
	if out.Uncertainty != domain.UncertaintyForbidden {
		t.Fatalf("uncertainty = %q", out.Uncertainty)
	}
	if out.Confidence != domain.OwnershipUnmanaged {
		t.Fatalf("confidence = %q; the evidence found is a separate statement from what may be concluded", out.Confidence)
	}
}

func TestAPartialSearchStillReportsWhatItPositivelyFound(t *testing.T) {
	// Positive evidence survives an incomplete search. An Application that
	// lists this object lists it whatever else could not be read; only the
	// absence depends on having seen everything.
	deployment := object("apps/v1", "Deployment", "reporting", "super-report", nil, nil)
	apps := []*unstructured.Unstructured{
		argoApp("reporting", "super-report", gitSpec("apps/reporting"), map[string]any{
			"resources": []any{
				map[string]any{"group": "apps", "kind": "Deployment", "namespace": "reporting", "name": "super-report"},
			},
		}),
	}

	out := resolveOwnership(reportingRef(), deployment, apps)
	settle(&out, search{installed: installYes, uncertainty: domain.UncertaintyForbidden})

	if out.Status != domain.OwnershipStatusManaged {
		t.Fatalf("status = %q; a claim found in a readable namespace was discarded because another was not", out.Status)
	}
}

func TestAnAmbiguousLabelIsUnknownRatherThanEitherAnswer(t *testing.T) {
	// `app.kubernetes.io/instance` naming an Application that does not list
	// the object. Helm writes that label on everything it installs, so this is
	// a question and not an answer — and a question must not open a gate.
	deployment := object("apps/v1", "Deployment", "reporting", "super-report",
		map[string]string{InstanceLabel: "super-report"}, nil)
	apps := []*unstructured.Unstructured{argoApp("argocd", "super-report", gitSpec("apps/reporting"), nil)}

	out := resolveOwnership(reportingRef(), deployment, apps)
	settle(&out, search{complete: true, installed: installYes})

	if out.Status != domain.OwnershipStatusUnknown {
		t.Fatalf("status = %q", out.Status)
	}
	if out.Uncertainty != domain.UncertaintyAmbiguous {
		t.Fatalf("uncertainty = %q", out.Uncertainty)
	}
	// The displayed confidence keeps saying "possibly", because that is what
	// the panel has always said and it is accurate. Only the gate is closed.
	if out.Confidence != domain.OwnershipCandidate {
		t.Fatalf("confidence = %q", out.Confidence)
	}
}

// The gate is the one place the three states become a yes or a no. These check
// that no state grew a second meaning on the way through it.

func TestOnlyAnUnmanagedAnswerOpensTheGate(t *testing.T) {
	for name, test := range map[string]struct {
		status domain.OwnershipStatus
		allow  bool
	}{
		"managed":   {domain.OwnershipStatusManaged, false},
		"unmanaged": {domain.OwnershipStatusUnmanaged, true},
		"unknown":   {domain.OwnershipStatusUnknown, false},
		"loading":   {domain.OwnershipStatusLoading, false},
		"unset":     {"", false},
	} {
		t.Run(name, func(t *testing.T) {
			gate := GateForOwnership(domain.ResourceOwnership{Status: test.status})
			if gate.Allowed != test.allow {
				t.Fatalf("allowed = %v for %q", gate.Allowed, test.status)
			}
		})
	}
}

func TestAManagedGateSaysWhereTheChangeBelongs(t *testing.T) {
	// A read-only editor with no explanation is a bug report. The evidence
	// sentence says how ownership was established; the banner has to answer
	// the other question, which is what to do instead.
	gate := GateForOwnership(domain.ResourceOwnership{
		Status: domain.OwnershipStatusManaged,
		Reason: "Argo CD's tracking annotation on this object names this Application.",
		App:    &domain.ArgoApp{Namespace: "argocd", Name: "super-report"},
	})

	if !strings.Contains(gate.Reason, "super-report") {
		t.Fatalf("the refusal does not name the Application: %q", gate.Reason)
	}
	if !strings.Contains(gate.Reason, "Git source") {
		t.Fatalf("the refusal does not say where the change belongs: %q", gate.Reason)
	}
}

func TestAnUnsettledOwnershipIsNeverCalledManaged(t *testing.T) {
	// Unknown must not be reported as managed either. "Managed" sends somebody
	// to a repository, and naming one that may not exist wastes their
	// afternoon as surely as offering a write they should not have.
	gate := GateForOwnership(domain.ResourceOwnership{
		Status:      domain.OwnershipStatusUnknown,
		Uncertainty: domain.UncertaintyForbidden,
		Reason:      "This account may not list Argo CD Applications.",
	})

	if gate.Managed {
		t.Fatal("an unknown answer reports as managed")
	}
	if gate.Claim != domain.ClaimUnknown {
		t.Fatalf("claim = %q", gate.Claim)
	}
}

func TestAGateCarriesStructuredStateRatherThanOnlyASentence(t *testing.T) {
	// The frontend renders the result; it does not decide it, and it must
	// never have to read an error message to find out what happened.
	gate := GateForClaim(Claim{
		Kind:        ClaimUnknown,
		Uncertainty: domain.UncertaintyForbidden,
		Reason:      "This account may not list Argo CD Applications.",
		Probes: []domain.OwnershipProbe{{
			Resource: "applications.argoproj.io", Verb: "list",
			Scope: "cluster", Result: domain.OwnershipProbeForbidden,
		}},
	})

	if gate.Status == "" || gate.Uncertainty == "" || gate.Claim == "" {
		t.Fatalf("the gate leaves the frontend to infer its state: %+v", gate)
	}
	if len(gate.Probes) == 0 {
		t.Fatal("the gate names no permission, so the screen can only say that something failed")
	}
	if strings.TrimSpace(gate.Reason) == "" {
		t.Fatal("the gate carries no sentence to show")
	}
}

func TestAnObjectGateNeverRestsOnANamespaceClaim(t *testing.T) {
	// A namespace claim is a reason to be careful about creating a new name.
	// It is not a reason to refuse an edit to an object that Application has
	// never mentioned, and treating it as one would lock the YAML editor for
	// every object in any namespace Argo CD touches.
	gate := GateForOwnership(domain.ResourceOwnership{Status: domain.OwnershipStatusManaged})

	if gate.Claim != domain.ClaimObject {
		t.Fatalf("claim = %q; ownership of an existing object is object-level evidence only", gate.Claim)
	}
}

func reportingRef() domain.ResourceRef {
	return domain.ResourceRef{Kind: domain.KindDeployment, Namespace: "reporting", Name: "super-report"}
}
