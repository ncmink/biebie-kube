package cluster

import (
	"testing"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
)

func find(catalogue []domain.KindInfo, kind domain.Kind) (domain.KindInfo, bool) {
	for _, info := range catalogue {
		if info.Kind == kind {
			return info, true
		}
	}
	return domain.KindInfo{}, false
}

func TestCatalogueHidesBuiltInKindsTheClusterDoesNotServe(t *testing.T) {
	served := []kube.APIResource{
		{Group: "", Resource: "pods"},
		{Group: "apps", Resource: "deployments"},
	}

	catalogue := catalogueFor(served, nil)

	if _, ok := find(catalogue, domain.KindPod); !ok {
		t.Fatal("a served kind must be navigable")
	}
	if _, ok := find(catalogue, domain.KindIngress); ok {
		t.Fatal("a kind the cluster does not serve must not be offered")
	}
}

func TestCatalogueKeepsBuiltInsWhenDiscoveryFailed(t *testing.T) {
	catalogue := catalogueFor(nil, nil)

	if len(catalogue) != len(domain.Catalogue()) {
		t.Fatalf("catalogue has %d entries; failed discovery is not evidence of an empty cluster",
			len(catalogue))
	}
}

func TestCatalogueAddsCustomKinds(t *testing.T) {
	served := []kube.APIResource{{Group: "", Resource: "pods"}}
	customs := []kube.CustomResource{{
		Group: "argoproj.io", Version: "v1alpha1", Plural: "applications",
		Kind: "Application", Namespaced: true,
		Columns: []kube.PrinterColumn{
			{Name: "Sync Status", JSONPath: ".status.sync.status"},
			{Name: "Health Status", JSONPath: ".status.health.status"},
		},
	}}

	catalogue := catalogueFor(served, customs)

	info, ok := find(catalogue, domain.Kind("applications.argoproj.io"))
	if !ok {
		t.Fatal("a custom resource must be navigable under plural.group")
	}
	if !info.Custom {
		t.Fatal("a discovered kind must be marked custom, so the sidebar can group it")
	}
	if info.Category != domain.CategoryCustom {
		t.Fatalf("category = %q", info.Category)
	}
	if info.Title != "Applications" {
		t.Fatalf("title = %q; the kind name is what the engineer wrote in their manifests", info.Title)
	}
	if info.Group != "argoproj.io" || info.Version != "v1alpha1" || info.Resource != "applications" {
		t.Fatalf("info = %+v; the GVR must address the stored version", info)
	}
	if len(info.Columns) != 2 {
		t.Fatalf("columns = %+v", info.Columns)
	}
	if info.Columns[0].Key != "syncStatus" || info.Columns[0].Path != ".status.sync.status" {
		t.Fatalf("first column = %+v", info.Columns[0])
	}
}

func TestCustomKindsSurviveFailedDiscovery(t *testing.T) {
	customs := []kube.CustomResource{{
		Group: "example.com", Version: "v1", Plural: "widgets", Kind: "Widget",
	}}

	catalogue := catalogueFor(nil, customs)

	if _, ok := find(catalogue, domain.Kind("widgets.example.com")); !ok {
		t.Fatal("a definition that was read must be navigable even when discovery was not")
	}
}

func TestColumnKeysDoNotCollide(t *testing.T) {
	custom := kube.CustomResource{
		Group: "example.com", Version: "v1", Plural: "widgets", Kind: "Widget",
		Columns: []kube.PrinterColumn{
			{Name: "Sync Status", JSONPath: ".a"},
			{Name: "sync status", JSONPath: ".b"},
			{Name: "!!!", JSONPath: ".c"},
		},
	}

	info := customKindInfo(custom)

	seen := make(map[string]struct{}, len(info.Columns))
	for _, column := range info.Columns {
		if _, clash := seen[column.Key]; clash {
			t.Fatalf("key %q is used twice; one column's value would overwrite the other", column.Key)
		}
		seen[column.Key] = struct{}{}
	}
	if info.Columns[1].Key != "syncStatus2" {
		t.Fatalf("second key = %q", info.Columns[1].Key)
	}
	if info.Columns[2].Key != "value" {
		t.Fatalf("third key = %q; a column named in punctuation still needs a field", info.Columns[2].Key)
	}
}

func TestKindNamesArePluralisedForTheSidebar(t *testing.T) {
	cases := map[string]string{
		"Application":    "Applications",
		"Ingress":        "Ingresses",
		"NetworkPolicy":  "NetworkPolicies",
		"Gateway":        "Gateways",
		"PodMonitor":     "PodMonitors",
		"SecurityPolicy": "SecurityPolicies",
	}
	for kind, want := range cases {
		if got := plural(kind); got != want {
			t.Fatalf("plural(%q) = %q, want %q", kind, got, want)
		}
	}
}
