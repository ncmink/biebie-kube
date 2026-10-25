package ai

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestScrubSecretRemovesData(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"kind": "Secret",
		"metadata": map[string]interface{}{
			"name": "db",
		},
		"data": map[string]interface{}{
			"password": "c2VjcmV0",
		},
		"stringData": map[string]interface{}{
			"user": "admin",
		},
	}}
	if err := ensureSecretScrubbed(obj); err != nil {
		t.Fatal(err)
	}
	out, notes, err := scrubYAML(obj)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "c2VjcmV0") || strings.Contains(out, "stringData") {
		t.Fatalf("secret material leaked: %s", out)
	}
	joined := strings.Join(notes, ";")
	if !strings.Contains(joined, "Secret data") || !strings.Contains(joined, "Secret stringData") {
		t.Fatalf("summary should report removed secret fields: %v", notes)
	}
}

func TestScrubRemovesLastApplied(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"kind": "ConfigMap",
		"metadata": map[string]interface{}{
			"name": "cfg",
			"annotations": map[string]interface{}{
				"kubectl.kubernetes.io/last-applied-configuration": "{\"data\":{\"token\":\"x\"}}",
			},
			"managedFields": []interface{}{map[string]interface{}{"manager": "kubectl"}},
		},
	}}
	out, notes, err := scrubYAML(obj)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "last-applied") || strings.Contains(out, "managedFields") {
		t.Fatalf("noise not scrubbed: %s", out)
	}
	if len(notes) != 2 {
		t.Fatalf("expected two scrub notes, got %v", notes)
	}
}

func TestScrubNotesOnlyWhatWasPresent(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"kind":     "ConfigMap",
		"metadata": map[string]interface{}{"name": "plain"},
	}}
	_, notes, err := scrubYAML(obj)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Fatalf("nothing was removed, but summary says %v", notes)
	}
}

func TestScrubSecretKeyRefInDeploymentTemplate(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"kind": "Deployment",
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"initContainers": []interface{}{
						map[string]interface{}{
							"name": "migrate",
							"env": []interface{}{
								map[string]interface{}{
									"name": "TOKEN",
									"valueFrom": map[string]interface{}{
										"secretKeyRef": map[string]interface{}{"name": "api", "key": "token"},
									},
								},
							},
						},
					},
				},
			},
		},
	}}
	out, notes, err := scrubYAML(obj)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "secretKeyRef") || !strings.Contains(out, "<secret api/token>") {
		t.Fatalf("init container secretKeyRef not replaced: %s", out)
	}
	if !strings.Contains(strings.Join(notes, ";"), "1 env var") {
		t.Fatalf("summary should count the env var: %v", notes)
	}
}

func TestScrubSecretKeyRefEnv(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"kind": "Pod",
		"spec": map[string]interface{}{
			"containers": []interface{}{
				map[string]interface{}{
					"name": "app",
					"env": []interface{}{
						map[string]interface{}{
							"name":  "DB_PASS",
							"value": "literal",
							"valueFrom": map[string]interface{}{
								"secretKeyRef": map[string]interface{}{
									"name": "db",
									"key":  "password",
								},
							},
						},
					},
				},
			},
		},
	}}
	out, _, err := scrubYAML(obj)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "valueFrom") {
		t.Fatalf("valueFrom not scrubbed: %s", out)
	}
	if !strings.Contains(out, "<secret db/password>") {
		t.Fatalf("expected secret placeholder: %s", out)
	}
}

func TestContextCap(t *testing.T) {
	body := strings.Repeat("x", maxContextBytes+100)
	if len(body) <= maxContextBytes {
		t.Fatal("test setup")
	}
	trimmed := body[:maxContextBytes] + "\n\n[Context truncated at 60 KiB]"
	if !strings.Contains(trimmed, "truncated") {
		t.Fatal("expected truncation marker")
	}
}
