package authoring

import (
	"testing"

	"biebie-kube/internal/argocd"
	"biebie-kube/internal/domain"
)

// The gate has two halves that fail for different reasons and lead to
// different places. These tests exist so neither can quietly start standing in
// for the other.

func TestUnmanagedWithAReadyRuntimeOffersBothSurfaces(t *testing.T) {
	out := decide(unclaimed(), readyRuntime())

	if !out.Allowed {
		t.Fatalf("creation was refused: %q", out.Reason)
	}
	if len(out.Modes) != 2 || out.Modes[0] != domain.AuthoringTypeScript {
		t.Fatalf("modes = %v, TypeScript should lead when it is available", out.Modes)
	}
}

func TestUnmanagedWithoutARuntimeStillOffersYAML(t *testing.T) {
	// The product rule the runtime work exists to protect: somebody with no
	// Node installed can still create a ConfigMap.
	out := decide(unclaimed(), domain.AuthoringRuntime{YAML: true, Reason: "cdk8s was not found."})

	if !out.Allowed {
		t.Fatalf("creation was refused for want of Node: %q", out.Reason)
	}
	if len(out.Modes) != 1 || out.Modes[0] != domain.AuthoringYAML {
		t.Fatalf("modes = %v", out.Modes)
	}
	if out.Runtime.Reason == "" {
		t.Fatal("the runtime's reason was dropped, so the screen cannot say why TypeScript is locked")
	}
}

func TestAFoundButUninstalledRuntimeDoesNotOfferTypeScript(t *testing.T) {
	// Node, npm and cdk8s all present is not the same as being able to synth.
	// Offering TypeScript here would put the npm install behind the first
	// Preview, which is the thing the dependency policy forbids.
	runtime := readyRuntime()
	runtime.Prepared = false

	out := decide(unclaimed(), runtime)

	if len(out.Modes) != 1 || out.Modes[0] != domain.AuthoringYAML {
		t.Fatalf("modes = %v", out.Modes)
	}
}

func TestOwnershipAndTheRuntimeAreIndependentGates(t *testing.T) {
	// A missing TypeScript runtime must not open the GitOps gate, and a GitOps
	// refusal must not be reported as a runtime problem. They are two answers
	// with two different fixes, and the screen sends people to two different
	// places.
	out := decide(managedGate(), domain.AuthoringRuntime{YAML: true, Reason: "cdk8s was not found."})

	if out.Allowed {
		t.Fatal("an unready runtime was allowed to decide the GitOps question")
	}
	if out.Ownership.Status != domain.OwnershipStatusManaged {
		t.Fatalf("status = %q; the refusal no longer reads as a GitOps one", out.Ownership.Status)
	}
	if out.Runtime.Reason == "" {
		t.Fatal("the runtime's own answer was dropped by the ownership refusal")
	}
}

func TestAnArgoManagedNamespaceRefusesDirectCreation(t *testing.T) {
	out := decide(managedGate(), readyRuntime())

	if out.Allowed {
		t.Fatal("direct creation was offered in a GitOps-managed namespace")
	}
	if !out.Ownership.Managed {
		t.Fatal("the refusal does not report itself as a GitOps one, so the screen cannot explain it")
	}
	if out.Ownership.App == nil || out.Ownership.App.Name != "super-report" {
		t.Fatalf("app = %+v; the screen cannot name where the change belongs", out.Ownership.App)
	}
	if len(out.Modes) != 0 {
		t.Fatalf("authoring surfaces were offered anyway: %v", out.Modes)
	}
}

func TestAnObjectClaimOutranksANamespaceWarning(t *testing.T) {
	// Both refuse, and they must not read the same. A namespace claim is
	// context about where the object would land; an object claim is Argo CD
	// saying it already has a desired state for this exact name.
	object := argocd.GateForClaim(argocd.Claim{
		Kind: argocd.ClaimObject, Complete: true, Reason: "already listed",
	})

	out := decide(object, readyRuntime())

	if out.Allowed {
		t.Fatal("direct creation was offered for an object Argo CD already lists")
	}
	if out.Ownership.Claim != domain.ClaimObject {
		t.Fatalf("claim = %q; the strength of the evidence was lost", out.Ownership.Claim)
	}
}

func TestAnUnansweredOwnershipCheckRefusesRatherThanAssumes(t *testing.T) {
	// An account that may create resources but may not list Applications is an
	// ordinary configuration. Reading its silence as "unmanaged" is how a
	// resource lands in a namespace a repository owns.
	gate := argocd.GateForClaim(argocd.Claim{
		Kind:        argocd.ClaimUnknown,
		Uncertainty: domain.UncertaintyForbidden,
		Reason:      "This account may not list Argo CD Applications, so whether one claims this target could not be checked.",
	})

	out := decide(gate, readyRuntime())

	if out.Allowed {
		t.Fatal("creation was offered on the strength of a check that failed")
	}
	if out.Ownership.Managed {
		t.Fatal("an unanswered check was reported as a GitOps refusal, which names a repository that may not exist")
	}
	if out.Ownership.Status != domain.OwnershipStatusUnknown {
		t.Fatalf("status = %q, want unknown", out.Ownership.Status)
	}
	if out.Ownership.Uncertainty != domain.UncertaintyForbidden {
		t.Fatalf("uncertainty = %q; the screen cannot say which permission is missing", out.Ownership.Uncertainty)
	}
}

func TestAnIncompleteSearchRefusesEvenThoughItFoundNothing(t *testing.T) {
	// The partial-visibility case. Some Applications were listed and some
	// namespace refused, so "no claim was found" describes what was read
	// rather than what exists.
	gate := argocd.GateForClaim(argocd.Claim{
		Kind:        argocd.ClaimUnknown,
		Uncertainty: domain.UncertaintyIncomplete,
		Reason:      "Not every Argo CD Application could be listed.",
	})

	if decide(gate, readyRuntime()).Allowed {
		t.Fatal("a partial search that found nothing was treated as proof there is nothing")
	}
}

func TestAnUnansweredGateRefusesRatherThanDefaultsOpen(t *testing.T) {
	// The loading window, expressed as the thing that makes it safe: the gate
	// is closed unless something explicitly opened it. A person who presses
	// Create in the second before ownership comes back gets a refusal, not a
	// write, whatever the screen happened to be rendering.
	if decide(domain.MutationGate{}, readyRuntime()).Allowed {
		t.Fatal("creation was offered on a gate nothing had answered")
	}
	if decide(domain.MutationGate{Status: domain.OwnershipStatusLoading}, readyRuntime()).Allowed {
		t.Fatal("creation was offered while ownership was still being checked")
	}
}

func unclaimed() domain.MutationGate {
	return argocd.GateForClaim(argocd.Claim{
		Kind:     argocd.ClaimNone,
		Complete: true,
		Reason: "No Argo CD Application claims this resource. " +
			"Direct cluster changes are available; they will not be recorded in Git by Biebie Kube.",
	})
}

func managedGate() domain.MutationGate {
	return argocd.GateForClaim(argocd.Claim{
		Kind:     argocd.ClaimNamespace,
		Complete: true,
		App:      &domain.ArgoApp{Namespace: "argocd", Name: "super-report"},
		Reason:   "Argo CD Application super-report deploys into this namespace.",
	})
}

func readyRuntime() domain.AuthoringRuntime {
	return domain.AuthoringRuntime{YAML: true, TypeScript: true, Prepared: true}
}
