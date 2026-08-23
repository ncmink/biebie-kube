package kube

import (
	"context"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// crdGVR addresses the definitions themselves. They are read through the
// dynamic client like everything else, so no apiextensions Go types are
// compiled in for what is only ever read as data.
var crdGVR = schema.GroupVersionResource{
	Group:    "apiextensions.k8s.io",
	Version:  "v1",
	Resource: "customresourcedefinitions",
}

// CustomResource is one custom type a cluster serves, resolved to the single
// version this application should address.
//
// A CRD may serve several versions at once. Only the storage version is
// described here: it is the one the API server persists and converts the
// others to, so reading it is reading what the cluster actually holds.
type CustomResource struct {
	Group    string `json:"group"`
	Version  string `json:"version"`
	Plural   string `json:"plural"`
	Kind     string `json:"kind"`
	ListKind string `json:"listKind"`

	Namespaced bool `json:"namespaced"`

	// Columns are the table columns the definition declares for itself. They
	// are what `kubectl get` prints, which makes them the cluster owner's own
	// answer to "what matters about this resource".
	Columns []PrinterColumn `json:"columns"`
}

// PrinterColumn is one column a CustomResourceDefinition declares.
type PrinterColumn struct {
	Name     string `json:"name"`
	JSONPath string `json:"jsonPath"`
}

// CustomResources lists the custom types the cluster serves.
//
// Being unable to read definitions is an ordinary outcome, not a failure: an
// account scoped to one namespace has no business listing cluster-wide CRDs,
// and the navigation simply has no custom section for it. The caller passes
// the error on rather than treating it as a broken cluster.
func (c *ClusterClient) CustomResources(ctx context.Context) ([]CustomResource, error) {
	list, err := c.Dynamic.Resource(crdGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list custom resource definitions: %w", err)
	}

	out := make([]CustomResource, 0, len(list.Items))
	for i := range list.Items {
		if described, ok := describeCRD(&list.Items[i]); ok {
			out = append(out, described)
		}
	}

	// Group first, then the name shown in the sidebar, so the tree is stable
	// between connections rather than following API server ordering.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Plural < out[j].Plural
	})
	return out, nil
}

// describeCRD reduces one definition to the version this application reads.
//
// A definition missing a group, a plural or a servable version is skipped
// rather than guessed at: an entry that cannot be addressed would appear in
// the sidebar and fail on every click.
func describeCRD(obj *unstructured.Unstructured) (CustomResource, bool) {
	group, _, _ := unstructured.NestedString(obj.Object, "spec", "group")
	plural, _, _ := unstructured.NestedString(obj.Object, "spec", "names", "plural")
	if group == "" || plural == "" {
		return CustomResource{}, false
	}

	scope, _, _ := unstructured.NestedString(obj.Object, "spec", "scope")
	described := CustomResource{
		Group:      group,
		Plural:     plural,
		Namespaced: scope == "Namespaced",
	}
	described.Kind, _, _ = unstructured.NestedString(obj.Object, "spec", "names", "kind")
	described.ListKind, _, _ = unstructured.NestedString(obj.Object, "spec", "names", "listKind")

	version, ok := storageVersion(obj)
	if !ok {
		return CustomResource{}, false
	}
	described.Version, _, _ = unstructured.NestedString(version, "name")
	if described.Version == "" {
		return CustomResource{}, false
	}
	described.Columns = printerColumns(version)

	return described, true
}

// storageVersion picks the version to address.
//
// The storage version is preferred because it is what the cluster persists.
// The first served version is the fallback for a definition that names no
// storage version at all, which is invalid but must not cost the whole entry.
func storageVersion(obj *unstructured.Unstructured) (map[string]any, bool) {
	versions, _, _ := unstructured.NestedSlice(obj.Object, "spec", "versions")

	var fallback map[string]any
	for _, raw := range versions {
		version, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if served, _, _ := unstructured.NestedBool(version, "served"); !served {
			continue
		}
		if storage, _, _ := unstructured.NestedBool(version, "storage"); storage {
			return version, true
		}
		if fallback == nil {
			fallback = version
		}
	}
	return fallback, fallback != nil
}

// printerColumns reads the columns a version declares.
//
// Two kinds are dropped. A column with a priority is what `kubectl get -o wide`
// shows and is deliberately not in the default table. An age column duplicates
// the one every table already renders from the creation timestamp.
func printerColumns(version map[string]any) []PrinterColumn {
	declared, _, _ := unstructured.NestedSlice(version, "additionalPrinterColumns")

	out := make([]PrinterColumn, 0, len(declared))
	for _, raw := range declared {
		column, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if priority, found, _ := unstructured.NestedInt64(column, "priority"); found && priority > 0 {
			continue
		}
		name, _, _ := unstructured.NestedString(column, "name")
		path, _, _ := unstructured.NestedString(column, "jsonPath")
		if name == "" || path == "" || name == "Age" {
			continue
		}
		out = append(out, PrinterColumn{Name: name, JSONPath: path})
	}
	return out
}
