package resources

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
)

func TestAServiceWithoutASelectorMatchesNothing(t *testing.T) {
	// The trap this guards: an absent or empty selector converted the obvious
	// way matches every pod in the namespace, so a headless service would
	// answer "what am I routing to?" with the whole namespace.
	for name, object := range map[string]map[string]any{
		"absent": {"spec": map[string]any{}},
		"empty":  {"spec": map[string]any{"selector": map[string]any{}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := serviceSelector(&unstructured.Unstructured{Object: object}); ok {
				t.Fatal("a service with no selector was given one")
			}
		})
	}
}

func TestAServiceSelectorMatchesOnEveryLabel(t *testing.T) {
	service := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"selector": map[string]any{"app": "argocd-server", "tier": "web"},
		},
	}}

	selector, ok := serviceSelector(service)
	if !ok {
		t.Fatal("selector not read")
	}
	if !selector.Matches(labels.Set{"app": "argocd-server", "tier": "web", "extra": "ignored"}) {
		t.Fatal("a pod carrying both labels was not matched")
	}
	// A service selector is an AND: half the labels is not a backend.
	if selector.Matches(labels.Set{"app": "argocd-server"}) {
		t.Fatal("a pod missing a selector label was matched")
	}
}

func TestOnlyTheControllerOwnerCounts(t *testing.T) {
	pod := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"ownerReferences": []any{
				// A plain reference — a scaler, a mesh, anything that wants to
				// be cleaned up with the pod — is not what created it.
				map[string]any{"kind": "ConfigMap", "name": "settings", "uid": "cm-1"},
				map[string]any{"kind": "ReplicaSet", "name": "web-abc", "uid": "rs-1", "controller": true},
			},
		},
	}}

	reference, ok := controllerOf(pod)
	if !ok {
		t.Fatal("controller not found")
	}
	if reference.Name != "web-abc" || string(reference.UID) != "rs-1" {
		t.Fatalf("controller = %s/%s", reference.Kind, reference.Name)
	}
}

func TestAnObjectWithNoControllerHasNoOwner(t *testing.T) {
	pod := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"ownerReferences": []any{
				map[string]any{"kind": "ReplicaSet", "name": "web-abc", "uid": "rs-1"},
			},
		},
	}}

	if _, ok := controllerOf(pod); ok {
		t.Fatal("a reference that is not the controller was read as the owner")
	}
}

func TestRevisionsAreReadFromTheDeploymentsAnnotation(t *testing.T) {
	if got := revisionOf(replicaSetAt("11")); got != 11 {
		t.Fatalf("revision = %d", got)
	}
	// A replica set the deployment never stamped sorts last rather than
	// failing the group it belongs to.
	if got := revisionOf(replicaSetAt("")); got != 0 {
		t.Fatalf("unstamped revision = %d", got)
	}
}

func replicaSetAt(revision string) *unstructured.Unstructured {
	annotations := map[string]any{}
	if revision != "" {
		annotations[revisionAnnotation] = revision
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"annotations": annotations},
	}}
}
