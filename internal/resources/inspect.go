package resources

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
)

// Inspect builds the right-hand inspector for one object.
//
// ConfigMap and Secret `data` values are copied as stored. Secret data is
// base64 in the API JSON; this function never decodes it.
func Inspect(kind domain.Kind, obj *unstructured.Unstructured) domain.ResourceInspect {
	out := domain.ResourceInspect{
		Ref: domain.ResourceRef{
			Kind:      kind,
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		},
		CreatedAt: obj.GetCreationTimestamp().Time,
	}

	switch kind {
	case domain.KindSecret:
		out.Type = nestedString(obj, "type")
		out.Data = dataEntries(obj, "data", false)
	case domain.KindConfigMap:
		out.Data = append(dataEntries(obj, "data", false), dataEntries(obj, "binaryData", true)...)
		sort.Slice(out.Data, func(i, j int) bool { return out.Data[i].Key < out.Data[j].Key })
	}

	return out
}

// InspectResource reads one object and returns its inspector payload.
func (s *Service) InspectResource(ctx context.Context, clusterID string, ref domain.ResourceRef) (domain.ResourceInspect, error) {
	obj, err := s.Get(ctx, clusterID, ref)
	if err != nil {
		return domain.ResourceInspect{}, err
	}
	return Inspect(ref.Kind, obj), nil
}

func dataEntries(obj *unstructured.Unstructured, field string, binary bool) []domain.DataEntry {
	raw, ok, _ := unstructured.NestedFieldNoCopy(obj.Object, field)
	data, okMap := raw.(map[string]any)
	if !ok || !okMap || len(data) == 0 {
		return nil
	}
	entries := make([]domain.DataEntry, 0, len(data))
	for key, value := range data {
		entries = append(entries, domain.DataEntry{
			Key:    key,
			Value:  storedString(value),
			Binary: binary,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries
}

// storedString keeps Kubernetes' on-the-wire form.
//
// A string is already what the API JSON carries (base64 for Secret data).
// []byte would be the decoded secret from a typed object — re-encode it so
// the UI never receives plaintext by accident.
func storedString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return base64.StdEncoding.EncodeToString(v)
	default:
		return fmt.Sprint(v)
	}
}
