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
type CustomResource struct {
	Group    string `json:"group"`
	Version  string `json:"version"`
	Plural   string `json:"plural"`
	Kind     string `json:"kind"`
	ListKind string `json:"listKind"`

	Namespaced bool `json:"namespaced"`

	// Columns are the table columns for the resolved version.
	Columns []PrinterColumn `json:"columns"`

	// ServedVersions lists every served version name in deterministic order.
	ServedVersions []string `json:"servedVersions,omitempty"`
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

	version, columns, served, ok := resolveCRDVersion(obj, "")
	if !ok {
		return CustomResource{}, false
	}
	described.Version = version
	described.Columns = columns
	described.ServedVersions = served

	return described, true
}

// ResolveCRDVersion picks the version and columns for one definition.
func ResolveCRDVersion(obj *unstructured.Unstructured, preferred string) (version string, columns []PrinterColumn, ok bool) {
	version, columns, _, ok = resolveCRDVersion(obj, preferred)
	return version, columns, ok
}

func resolveCRDVersion(obj *unstructured.Unstructured, preferred string) (string, []PrinterColumn, []string, bool) {
	versions, _, _ := unstructured.NestedSlice(obj.Object, "spec", "versions")
	served := make([]string, 0, len(versions))
	var storage map[string]any
	var fallback map[string]any
	for _, raw := range versions {
		version, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(version, "name")
		if servedFlag, _, _ := unstructured.NestedBool(version, "served"); !servedFlag {
			continue
		}
		if name != "" {
			served = append(served, name)
		}
		if preferred != "" && name == preferred {
			return name, printerColumns(version), served, true
		}
		if storageFlag, _, _ := unstructured.NestedBool(version, "storage"); storageFlag {
			storage = version
		}
		if fallback == nil {
			fallback = version
		}
	}
	sort.Strings(served)
	chosen := storage
	if chosen == nil {
		chosen = fallback
	}
	if chosen == nil {
		return "", nil, served, false
	}
	name, _, _ := unstructured.NestedString(chosen, "name")
	if name == "" {
		return "", nil, served, false
	}
	return name, printerColumns(chosen), served, true
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
