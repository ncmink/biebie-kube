package kube

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sjson "k8s.io/apimachinery/pkg/util/json"
)

// definition decodes a fixture the way client-go decodes a live object.
func definition(t *testing.T, raw string) *unstructured.Unstructured {
	t.Helper()
	var content map[string]any
	if err := k8sjson.Unmarshal([]byte(raw), &content); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return &unstructured.Unstructured{Object: content}
}

func TestDescribeCRDTakesTheStorageVersion(t *testing.T) {
	crd := definition(t, `{
		"spec": {
			"group": "argoproj.io",
			"scope": "Namespaced",
			"names": {"plural": "applications", "kind": "Application"},
			"versions": [
				{"name": "v1alpha1", "served": true, "storage": false},
				{"name": "v1beta1", "served": true, "storage": true}
			]
		}
	}`)

	described, ok := describeCRD(crd)
	if !ok {
		t.Fatal("a served definition must be described")
	}
	if described.Version != "v1beta1" {
		t.Fatalf("version = %q; the stored version is what the cluster holds", described.Version)
	}
	if !described.Namespaced {
		t.Fatal("a Namespaced definition must be namespaced")
	}
	if described.Kind != "Application" || described.Plural != "applications" {
		t.Fatalf("described = %+v", described)
	}
}

func TestDescribeCRDIgnoresVersionsThatAreNotServed(t *testing.T) {
	crd := definition(t, `{
		"spec": {
			"group": "example.com",
			"scope": "Cluster",
			"names": {"plural": "widgets", "kind": "Widget"},
			"versions": [
				{"name": "v1", "served": false, "storage": true},
				{"name": "v2", "served": true, "storage": false}
			]
		}
	}`)

	described, ok := describeCRD(crd)
	if !ok {
		t.Fatal("a definition with one served version must be described")
	}
	if described.Version != "v2" {
		t.Fatalf("version = %q; an unserved version cannot be read", described.Version)
	}
	if described.Namespaced {
		t.Fatal("a Cluster definition must not be namespaced")
	}
}

func TestDescribeCRDSkipsWhatCannotBeAddressed(t *testing.T) {
	cases := map[string]string{
		"no group":           `{"spec": {"names": {"plural": "widgets"}, "versions": [{"name": "v1", "served": true, "storage": true}]}}`,
		"no plural":          `{"spec": {"group": "example.com", "versions": [{"name": "v1", "served": true, "storage": true}]}}`,
		"no served version":  `{"spec": {"group": "example.com", "names": {"plural": "widgets"}, "versions": [{"name": "v1", "served": false}]}}`,
		"no versions at all": `{"spec": {"group": "example.com", "names": {"plural": "widgets"}}}`,
	}

	for name, raw := range cases {
		if _, ok := describeCRD(definition(t, raw)); ok {
			t.Fatalf("%s: an unaddressable definition must be skipped rather than listed", name)
		}
	}
}

func TestPrinterColumnsDropWideAndAgeColumns(t *testing.T) {
	crd := definition(t, `{
		"spec": {
			"group": "argoproj.io",
			"scope": "Namespaced",
			"names": {"plural": "applications", "kind": "Application"},
			"versions": [{
				"name": "v1alpha1", "served": true, "storage": true,
				"additionalPrinterColumns": [
					{"name": "Sync Status", "type": "string", "jsonPath": ".status.sync.status"},
					{"name": "Health Status", "type": "string", "jsonPath": ".status.health.status"},
					{"name": "Revision", "type": "string", "jsonPath": ".status.sync.revision", "priority": 10},
					{"name": "Age", "type": "date", "jsonPath": ".metadata.creationTimestamp"},
					{"name": "Broken", "type": "string"}
				]
			}]
		}
	}`)

	described, ok := describeCRD(crd)
	if !ok {
		t.Fatal("definition must be described")
	}
	if len(described.Columns) != 2 {
		t.Fatalf("columns = %+v; wide, age and pathless columns do not belong in the table", described.Columns)
	}
	if described.Columns[0].Name != "Sync Status" || described.Columns[0].JSONPath != ".status.sync.status" {
		t.Fatalf("first column = %+v", described.Columns[0])
	}
}
