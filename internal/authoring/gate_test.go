package authoring

import (
	"strings"
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

func TestAnArgoManagedNamespaceRefusesDirectCreation(t *testing.T) {
	claim := argocd.Claim{
		Kind:   argocd.ClaimNamespace,
		App:    &domain.ArgoApp{Namespace: "argocd", Name: "super-report"},
		Reason: "Argo CD Application super-report deploys into this namespace.",
	}

	out := decide(claim, readyRuntime())

	if out.Allowed {
		t.Fatal("direct creation was offered in a GitOps-managed namespace")
	}
	if !out.Managed {
		t.Fatal("the refusal does not report itself as a GitOps one, so the screen cannot explain it")
	}
	if out.App == nil || out.App.Name != "super-report" {
		t.Fatalf("app = %+v; the screen cannot name where the change belongs", out.App)
	}
	if len(out.Modes) != 0 {
		t.Fatalf("authoring surfaces were offered anyway: %v", out.Modes)
	}
}

func TestAnObjectAlreadyClaimedRefusesDirectCreation(t *testing.T) {
	claim := argocd.Claim{Kind: argocd.ClaimObject, Reason: "already listed"}

	if decide(claim, readyRuntime()).Allowed {
		t.Fatal("direct creation was offered for an object Argo CD already lists")
	}
}

func TestAnUnansweredOwnershipCheckRefusesRatherThanAssumes(t *testing.T) {
	// An account that may create resources but may not list Applications is an
	// ordinary configuration. Reading its silence as "unmanaged" is how a
	// resource lands in a namespace a repository owns.
	claim := argocd.Claim{
		Kind:   argocd.ClaimUnknown,
		Reason: "This account cannot list Argo CD Applications, so whether one claims this resource is unknown.",
	}

	out := decide(claim, readyRuntime())

	if out.Allowed {
		t.Fatal("creation was offered on the strength of a check that failed")
	}
	if out.Managed {
		t.Fatal("an unanswered check was reported as a GitOps refusal, which names a repository that may not exist")
	}
	if !strings.Contains(out.Reason, "unknown") {
		t.Fatalf("the reason does not say the check failed: %q", out.Reason)
	}
}

func unclaimed() argocd.Claim {
	return argocd.Claim{
		Kind: argocd.ClaimNone,
		Reason: "No Argo CD Application claims this resource. " +
			"Direct cluster changes are available; they will not be recorded in Git by Biebie Kube.",
	}
}

func readyRuntime() domain.AuthoringRuntime {
	return domain.AuthoringRuntime{YAML: true, TypeScript: true, Prepared: true}
}
