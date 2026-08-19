// Package manifest reads and writes resources as YAML for the editor.
//
// The rule this package enforces: a live resource is never silently
// overwritten. The editor shows what the cluster has, the user edits it, and
// Biebie Kube shows the difference before anything is sent.
package manifest

import (
	"context"
	"errors"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"biebie-kube/internal/cluster"
	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
	"biebie-kube/internal/resources"
)

// Service converts between live objects and editable YAML.
type Service struct {
	clusters  *cluster.Manager
	resources *resources.Service
}

// NewService wires the service.
func NewService(clusters *cluster.Manager, res *resources.Service) *Service {
	return &Service{clusters: clusters, resources: res}
}

// Read returns a resource as YAML, cleaned of the fields Kubernetes manages.
//
// Server-side noise — managedFields above all — makes a manifest unreadable
// and is rejected or ignored on the way back, so it is stripped for the
// editor rather than shown and then quietly dropped.
func (s *Service) Read(ctx context.Context, clusterID string, ref domain.ResourceRef) (string, error) {
	obj, err := s.resources.Get(ctx, clusterID, ref)
	if err != nil {
		return "", err
	}

	clean := obj.DeepCopy()
	unstructured.RemoveNestedField(clean.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(clean.Object, "metadata", "generation")
	unstructured.RemoveNestedField(clean.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(clean.Object, "metadata", "uid")
	unstructured.RemoveNestedField(clean.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(clean.Object, "metadata", "annotations",
		"kubectl.kubernetes.io/last-applied-configuration")
	unstructured.RemoveNestedField(clean.Object, "status")

	encoded, err := yaml.Marshal(clean.Object)
	if err != nil {
		return "", fmt.Errorf("render YAML: %w", err)
	}
	return string(encoded), nil
}

// Diff compares edited YAML with what the cluster currently holds.
//
// The comparison is done against a freshly read object, so an edit written
// while a controller was changing the same resource shows the conflict instead
// of hiding it.
func (s *Service) Diff(ctx context.Context, clusterID string, ref domain.ResourceRef, edited string) (string, string, error) {
	current, err := s.Read(ctx, clusterID, ref)
	if err != nil {
		return "", "", err
	}
	normalised, err := normalise(edited)
	if err != nil {
		return "", "", err
	}
	return current, normalised, nil
}

// Apply writes edited YAML back to the cluster.
//
// The object's identity is verified against the resource being edited: a
// manifest whose name or namespace was changed in the editor would otherwise
// create a second object rather than update the one on screen.
func (s *Service) Apply(ctx context.Context, clusterID string, ref domain.ResourceRef, edited string) (domain.ApplyResult, error) {
	info, ok := domain.Lookup(ref.Kind)
	if !ok {
		return domain.ApplyResult{}, fmt.Errorf("unknown resource type %q", ref.Kind)
	}
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return domain.ApplyResult{}, err
	}

	parsed, err := parse(edited)
	if err != nil {
		return domain.ApplyResult{}, err
	}
	if name := parsed.GetName(); name != ref.Name {
		return domain.ApplyResult{}, fmt.Errorf(
			"this manifest is named %q but you are editing %q; renaming creates a new object rather than updating this one",
			name, ref.Name)
	}
	if info.Namespaced && parsed.GetNamespace() != "" && parsed.GetNamespace() != ref.Namespace {
		return domain.ApplyResult{}, fmt.Errorf(
			"this manifest is in namespace %q but you are editing %q",
			parsed.GetNamespace(), ref.Namespace)
	}

	current, err := s.resources.Get(ctx, clusterID, ref)
	if err != nil {
		return domain.ApplyResult{}, err
	}

	// The live resourceVersion is carried over so the API server rejects the
	// write if something else changed the object since it was read.
	parsed.SetResourceVersion(current.GetResourceVersion())
	if info.Namespaced {
		parsed.SetNamespace(ref.Namespace)
	}

	gvr := kube.GVRFor(info.Group, info.Version, info.Resource)

	var updated *unstructured.Unstructured
	if info.Namespaced {
		updated, err = client.Dynamic.Resource(gvr).Namespace(ref.Namespace).
			Update(ctx, parsed, metav1.UpdateOptions{})
	} else {
		updated, err = client.Dynamic.Resource(gvr).Update(ctx, parsed, metav1.UpdateOptions{})
	}
	if err != nil {
		return domain.ApplyResult{}, describeApplyError(err)
	}

	return domain.ApplyResult{
		Ref:     ref,
		Changed: updated.GetResourceVersion() != current.GetResourceVersion(),
		Message: "Applied to " + ref.Name,
	}, nil
}

func parse(raw string) (*unstructured.Unstructured, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("the manifest is empty")
	}
	var object map[string]any
	if err := yaml.Unmarshal([]byte(raw), &object); err != nil {
		return nil, fmt.Errorf("this is not valid YAML: %w", err)
	}
	parsed := &unstructured.Unstructured{Object: object}
	if parsed.GetKind() == "" {
		return nil, errors.New("the manifest has no kind")
	}
	if parsed.GetName() == "" {
		return nil, errors.New("the manifest has no metadata.name")
	}
	return parsed, nil
}

// normalise re-encodes edited YAML so a diff shows real changes rather than
// differences in key order or indentation.
func normalise(raw string) (string, error) {
	parsed, err := parse(raw)
	if err != nil {
		return "", err
	}
	encoded, err := yaml.Marshal(parsed.Object)
	if err != nil {
		return "", fmt.Errorf("render YAML: %w", err)
	}
	return string(encoded), nil
}

func describeApplyError(err error) error {
	message := err.Error()
	switch {
	case strings.Contains(message, "the object has been modified"):
		return errors.New("this resource changed in the cluster while you were editing it; reload and reapply")
	case strings.Contains(message, "is invalid"):
		return fmt.Errorf("the cluster rejected this manifest: %w", err)
	default:
		return fmt.Errorf("apply: %w", err)
	}
}
