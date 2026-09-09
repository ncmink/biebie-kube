package cluster

import (
	"testing"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
	"biebie-kube/internal/testfixture"
)

func TestDiscoveryFallbackShowsWidgetWithoutCRDList(t *testing.T) {
	catalogue, snapshot := BuildCatalogue(CatalogueInput{
		ClusterID:    "cluster-a",
		SessionEpoch: "epoch-1",
		Discovery: kube.DiscoveryResult{
			Resources: testfixture.DiscoveryBaseline.WidgetOnly,
			Complete:  true,
		},
		CRDErr: testfixture.DiscoveryBaseline.ForbiddenCRDError,
	})

	info, ok := find(catalogue, domain.Kind("widgets.widgets.example.io"))
	if !ok {
		t.Fatal("widget must be navigable from API discovery alone")
	}
	if info.MetadataSource != domain.MetadataDiscovery {
		t.Fatalf("metadata source = %q", info.MetadataSource)
	}
	if len(info.Columns) != 0 {
		t.Fatalf("discovery-only kind must not invent CRD columns: %+v", info.Columns)
	}
	if snapshot.Complete != true {
		t.Fatal("discovery with resources must be complete")
	}
}

func TestPartialDiscoveryKeepsCorePodsAndReportsIssue(t *testing.T) {
	catalogue, snapshot := BuildCatalogue(CatalogueInput{
		Discovery: testfixture.DiscoveryBaseline.PartialApps,
	})

	if _, ok := find(catalogue, domain.KindPod); !ok {
		t.Fatal("pods from successful groups must remain navigable")
	}
	if _, ok := find(catalogue, domain.KindDeployment); ok {
		t.Fatal("apps group failure must not silently offer deployments")
	}
	if snapshot.Complete {
		t.Fatal("partial discovery must not claim completeness")
	}
	if len(snapshot.Issues) == 0 {
		t.Fatal("partial discovery must carry issues")
	}
}

func TestCRDEnrichmentUsesColumnsForDiscoveredKind(t *testing.T) {
	discovery := kube.DiscoveryResult{
		Resources: append([]kube.APIResource(nil), testfixture.DiscoveryBaseline.WidgetOnly...),
		Complete:  true,
	}
	catalogue, _ := BuildCatalogue(CatalogueInput{
		Discovery: discovery,
		Customs:   []kube.CustomResource{testfixture.DiscoveryBaseline.WidgetCRD},
	})

	info, ok := find(catalogue, domain.Kind("widgets.widgets.example.io"))
	if !ok {
		t.Fatal("widget must be navigable")
	}
	if info.MetadataSource != domain.MetadataCRD {
		t.Fatalf("metadata source = %q", info.MetadataSource)
	}
	if len(info.Columns) != 1 || info.Columns[0].Title != "Phase" {
		t.Fatalf("columns = %+v", info.Columns)
	}
}

func TestFailedDiscoveryKeepsUnverifiedBuiltins(t *testing.T) {
	catalogue, snapshot := BuildCatalogue(CatalogueInput{
		Discovery: kube.DiscoveryResult{Complete: false},
	})

	if len(catalogue) != len(domain.Catalogue()) {
		t.Fatalf("catalogue len = %d", len(catalogue))
	}
	info, _ := find(catalogue, domain.KindPod)
	if !info.Unverified {
		t.Fatal("built-in kinds must be marked unverified when discovery is empty")
	}
	if snapshot.Complete {
		t.Fatal("empty discovery must not be complete")
	}
}

func TestSamePluralDifferentGroupsDoNotCollide(t *testing.T) {
	discovery := kube.DiscoveryResult{
		Resources: []kube.APIResource{
			{Group: "a.example.io", Version: "v1", Resource: "widgets", Kind: "Widget", Namespaced: true, Verbs: []string{"list"}},
			{Group: "b.example.io", Version: "v1", Resource: "widgets", Kind: "Widget", Namespaced: true, Verbs: []string{"list"}},
		},
		Complete: true,
	}
	catalogue, _ := BuildCatalogue(CatalogueInput{Discovery: discovery})

	a, okA := find(catalogue, domain.Kind("widgets.a.example.io"))
	b, okB := find(catalogue, domain.Kind("widgets.b.example.io"))
	if !okA || !okB {
		t.Fatalf("kinds = %v %v", okA, okB)
	}
	if a.Group == b.Group {
		t.Fatal("different groups must produce different navigation entries")
	}
}
