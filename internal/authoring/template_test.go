package authoring

import (
	"strings"
	"testing"
)

// The bug these exist for: pressing Create on the Namespaces list opened an
// editor containing a ConfigMap in namespace `default`, on a screen whose own
// header said the namespace was none. A dialog whose whole purpose is to be
// certain what is being made cannot disagree with itself about what is being
// made.

func TestTheStarterIsForTheKindTheListWasShowing(t *testing.T) {
	starter := yamlStarter(target{apiVersion: "v1", kind: "Namespace"})

	objects, problems := parseAll(starter)
	if len(problems) != 0 {
		t.Fatalf("the starter does not parse: %+v", problems)
	}
	if objects[0].GetKind() != "Namespace" {
		t.Fatalf("kind = %q", objects[0].GetKind())
	}
}

func TestAClusterScopedStarterCarriesNoNamespace(t *testing.T) {
	// A Namespace with a `namespace:` on it is a manifest the API server
	// rejects and a person has to work out why. Worse, the value would have
	// been invented here: the screen said the namespace was none.
	yamlForm := yamlStarter(target{apiVersion: "v1", kind: "Namespace"})
	if strings.Contains(yamlForm, "namespace:") {
		t.Fatalf("a cluster-scoped starter declares a namespace:\n%s", yamlForm)
	}

	typescriptForm := typescriptStarter(target{apiVersion: "v1", kind: "Namespace"})
	if strings.Contains(typescriptForm, "namespace:") {
		t.Fatalf("a cluster-scoped TypeScript starter declares a namespace:\n%s", typescriptForm)
	}
}

func TestANamespacedStarterNamesTheChosenNamespace(t *testing.T) {
	// An editor that opens on `default` when the sidebar says `reporting` is
	// an editor whose first Validate fails on a namespace mismatch.
	spec := target{apiVersion: "v1", kind: "ConfigMap", namespace: "reporting", namespaced: true}

	if !strings.Contains(yamlStarter(spec), "namespace: reporting") {
		t.Fatalf("YAML starter = %q", yamlStarter(spec))
	}
	if !strings.Contains(typescriptStarter(spec), "namespace: 'reporting'") {
		t.Fatalf("TypeScript starter = %q", typescriptStarter(spec))
	}
}

func TestAGroupedKindKeepsItsGroupInTheApiVersion(t *testing.T) {
	starter := yamlStarter(target{
		apiVersion: "networking.k8s.io/v1", kind: "NetworkPolicy",
		namespace: "reporting", namespaced: true,
	})

	objects, problems := parseAll(starter)
	if len(problems) != 0 {
		t.Fatalf("problems = %+v", problems)
	}
	if objects[0].GetAPIVersion() != "networking.k8s.io/v1" {
		t.Fatalf("apiVersion = %q", objects[0].GetAPIVersion())
	}
}

func TestAKindWithNoBodyStillProducesACompleteObject(t *testing.T) {
	// Most kinds get a skeleton, and for most kinds a skeleton is a complete
	// and valid object. The three identity checks are what "complete" means
	// here, so they are the ones asserted.
	starter := yamlStarter(target{apiVersion: "v1", kind: "ServiceAccount", namespace: "reporting", namespaced: true})

	objects, problems := parseAll(starter)
	if len(problems) != 0 {
		t.Fatalf("problems = %+v", problems)
	}
	if issues := identityProblems(0, objects[0]); len(issues) != 0 {
		t.Fatalf("the skeleton is missing identity fields: %+v", issues)
	}
}

func TestEveryHandWrittenBodyParsesAsPartOfItsObject(t *testing.T) {
	// The bodies are written out by hand in two spellings. The YAML one can at
	// least be held to parsing, so a stray indent is caught here rather than
	// by somebody's first Validate.
	for kind := range bodies {
		starter := yamlStarter(target{apiVersion: "v1", kind: kind, namespace: "reporting", namespaced: true})

		objects, problems := parseAll(starter)
		if len(problems) != 0 {
			t.Fatalf("%s: %+v", kind, problems)
		}
		if objects[0].GetKind() != kind {
			t.Fatalf("%s: kind = %q", kind, objects[0].GetKind())
		}
	}
}

func TestTheDeploymentStarterIsSomethingTheClusterWouldAccept(t *testing.T) {
	// A Deployment skeleton is not merely sparse, it is rejected: the selector
	// and the pod template are required. Handing somebody one to fix is
	// handing them a puzzle rather than a start.
	starter := yamlStarter(target{apiVersion: "apps/v1", kind: "Deployment", namespace: "reporting", namespaced: true})

	for _, required := range []string{"selector:", "matchLabels:", "template:", "containers:", "image:"} {
		if !strings.Contains(starter, required) {
			t.Fatalf("the Deployment starter has no %s:\n%s", required, starter)
		}
	}
}

func TestTheSecretStarterDoesNotAskForHandWrittenBase64(t *testing.T) {
	// base64 typed by hand is base64 nobody can review, including the person
	// who typed it.
	starter := yamlStarter(target{apiVersion: "v1", kind: "Secret", namespace: "reporting", namespaced: true})

	if strings.Contains(starter, "\ndata:") {
		t.Fatalf("the Secret starter uses data rather than stringData:\n%s", starter)
	}
	if !strings.Contains(starter, "stringData:") {
		t.Fatalf("the Secret starter = %q", starter)
	}
}

func TestBothSpellingsOfABodyDeclareTheSameTopLevelFields(t *testing.T) {
	// The two forms are written out separately, so nothing but a test stops
	// them drifting into two different starters for the same kind.
	for kind, form := range bodies {
		for _, line := range strings.Split(form.yaml, "\n") {
			if line == "" || strings.HasPrefix(line, " ") {
				continue
			}
			field, _, found := strings.Cut(line, ":")
			if !found {
				continue
			}
			if !strings.Contains(form.typescript, field+":") {
				t.Fatalf("%s declares %q in YAML but not in TypeScript", kind, field)
			}
		}
	}
}
