package gitops

import (
	"encoding/json"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
)

// fieldOwners names the managers that metadata.managedFields records as
// owning this dotted path.
//
// A manager name appearing anywhere on the object is not ownership. The
// FieldsV1 tree has to contain the path, because an HPA that owns replicas
// also leaves other managers on the same object and those must not explain
// this field.
func fieldOwners(live *unstructured.Unstructured, path string) []domain.DifferenceEvidence {
	if live == nil || path == "" {
		return nil
	}
	entries := live.GetManagedFields()
	if len(entries) == 0 {
		return nil
	}

	want := fieldKeys(path)
	var out []domain.DifferenceEvidence
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.Subresource == "status" {
			continue
		}
		if entry.FieldsV1 == nil || len(entry.FieldsV1.Raw) == 0 {
			continue
		}
		if !owns(entry.FieldsV1.Raw, want) {
			continue
		}
		manager := entry.Manager
		if manager == "" || seen[manager] {
			continue
		}
		seen[manager] = true
		out = append(out, domain.DifferenceEvidence{
			Kind:       domain.EvidenceFieldOwner,
			Confidence: domain.ConfidenceSupporting,
			Subject:    manager,
			Summary:    "managedFields records " + manager + " as owning " + path + ".",
			Facts: []domain.EvidenceFact{
				{Name: "Manager", Value: manager},
				{Name: "Path", Value: path},
			},
		})
	}
	return out
}

func managersOf(evidence []domain.DifferenceEvidence) []string {
	var out []string
	for _, item := range evidence {
		if item.Kind == domain.EvidenceFieldOwner && item.Subject != "" {
			out = append(out, item.Subject)
		}
	}
	return out
}

func fieldKeys(path string) []string {
	parts := strings.Split(path, ".")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, "f:"+part)
	}
	return out
}

func owns(raw []byte, keys []string) bool {
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return false
	}
	current := tree
	for _, key := range keys {
		object, ok := current.(map[string]any)
		if !ok {
			return false
		}
		next, found := object[key]
		if !found {
			return false
		}
		current = next
	}
	return true
}
