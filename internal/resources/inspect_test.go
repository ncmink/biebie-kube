package resources

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
)

func TestADeploymentReportsEveryReplicaCount(t *testing.T) {
	properties := propertyMap(inspectDeployment(deploymentFixture()))

	// The five numbers together are what says whether a rollout finished:
	// "2 available" alone cannot distinguish a healthy deployment from one
	// serving the old version while the new one crash-loops.
	if got := properties["Replicas"]; got != "3 desired, 2 updated, 3 total, 2 available, 1 unavailable" {
		t.Fatalf("Replicas = %q", got)
	}
	if got := properties["Strategy Type"]; got != "RollingUpdate" {
		t.Fatalf("Strategy Type = %q", got)
	}
}

func TestOnlyTheConditionsThatHoldAreNamed(t *testing.T) {
	properties := propertyMap(inspectDeployment(deploymentFixture()))

	// ReplicaFailure is False, which is the deployment saying nothing is
	// wrong. Listing it would read as though something is.
	if got := properties["Conditions"]; got != "Progressing, Available" {
		t.Fatalf("Conditions = %q", got)
	}
}

func TestASelectorKeepsItsExpressions(t *testing.T) {
	properties := propertyMap(inspectDeployment(deploymentFixture()))

	// A selector shown as its matchLabels alone is a different selector, and
	// the reader has no way to tell it was abridged.
	want := "app=argocd-server\ntier In (web, api)"
	if got := properties["Selector"]; got != want {
		t.Fatalf("Selector = %q", got)
	}
}

func TestImagesAreListedOnceWithInitContainersFirst(t *testing.T) {
	properties := propertyMap(inspectDeployment(deploymentFixture()))

	want := "busybox:1.36\nargocd:v3.4.4\nenvoy:v1.31"
	if got := properties["Images"]; got != want {
		t.Fatalf("Images = %q", got)
	}
}

func TestSchedulingRulesAreCountedAndEmptyOnesLeftOut(t *testing.T) {
	properties := propertyMap(inspectDeployment(deploymentFixture()))

	if got := properties["Tolerations"]; got != "2 Tolerations" {
		t.Fatalf("Tolerations = %q", got)
	}
	if got := properties["Pod Anti Affinities"]; got != "1 Rule" {
		t.Fatalf("Pod Anti Affinities = %q", got)
	}
	// A deployment with no pod affinity gets no row at all: a drawer of
	// "None" costs the reader the same attention as a real value.
	if _, found := properties["Pod Affinities"]; found {
		t.Fatal("an absent affinity was given a row")
	}
}

func TestAKindWithNoInspectorOfItsOwnKeepsItsMetadata(t *testing.T) {
	// Inspect must still answer for a kind it has no properties for, or the
	// drawer loses labels and annotations along with the rows it never had.
	inspect := Inspect(domain.KindLease, &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"name":   "kube-scheduler",
			"labels": map[string]any{"app": "scheduler"},
		},
	}})

	if len(inspect.Properties) != 0 {
		t.Fatalf("properties = %v", inspect.Properties)
	}
	if inspect.Labels["app"] != "scheduler" {
		t.Fatalf("labels = %v", inspect.Labels)
	}
}

func propertyMap(properties []domain.InspectProperty) map[string]string {
	out := make(map[string]string, len(properties))
	for _, property := range properties {
		out[property.Label] = property.Value
	}
	return out
}

func deploymentFixture() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"replicas": int64(3),
			"strategy": map[string]any{"type": "RollingUpdate"},
			"selector": map[string]any{
				"matchLabels": map[string]any{"app": "argocd-server"},
				"matchExpressions": []any{
					map[string]any{"key": "tier", "operator": "In", "values": []any{"web", "api"}},
				},
			},
			"template": map[string]any{
				"spec": map[string]any{
					"nodeSelector": map[string]any{"kubernetes.io/os": "linux"},
					"tolerations": []any{
						map[string]any{"key": "one"},
						map[string]any{"key": "two"},
					},
					"affinity": map[string]any{
						"podAntiAffinity": map[string]any{
							"preferredDuringSchedulingIgnoredDuringExecution": []any{
								map[string]any{"weight": int64(100)},
							},
						},
					},
					"initContainers": []any{
						map[string]any{"name": "wait", "image": "busybox:1.36"},
					},
					"containers": []any{
						map[string]any{"name": "server", "image": "argocd:v3.4.4"},
						map[string]any{"name": "proxy", "image": "envoy:v1.31"},
						map[string]any{"name": "sidecar", "image": "envoy:v1.31"},
					},
				},
			},
		},
		"status": map[string]any{
			"replicas":            int64(3),
			"updatedReplicas":     int64(2),
			"availableReplicas":   int64(2),
			"unavailableReplicas": int64(1),
			"conditions": []any{
				map[string]any{"type": "Progressing", "status": "True"},
				map[string]any{"type": "Available", "status": "True"},
				map[string]any{"type": "ReplicaFailure", "status": "False"},
			},
		},
	}}
}
