package testfixture

import (
	"testing"

	"biebie-kube/internal/cluster"
	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
)

// Baseline documents IW-00 fixture coverage used by later incident workspace tests.
func TestBaselineFixturesCompile(t *testing.T) {
	if DiscoveryBaseline.WidgetCRD.Group == "" {
		t.Fatal("widget CRD fixture must be populated")
	}
	if PodCrashLoop("demo", "team-a") == nil {
		t.Fatal("crash loop pod fixture must build")
	}
}

func TestBaselineRestrictedRBACCatalogue(t *testing.T) {
	catalogue, _ := cluster.BuildCatalogue(cluster.CatalogueInput{
		Discovery: kube.DiscoveryResult{
			Resources: DiscoveryBaseline.WidgetOnly,
			Complete:  true,
		},
		CRDErr: DiscoveryBaseline.ForbiddenCRDError,
	})
	if _, ok := findKind(catalogue, domain.Kind("widgets.widgets.example.io")); !ok {
		t.Fatal("restricted RBAC account must still navigate discovered custom kinds")
	}
}

func findKind(catalogue []domain.KindInfo, kind domain.Kind) (domain.KindInfo, bool) {
	for _, info := range catalogue {
		if info.Kind == kind {
			return info, true
		}
	}
	return domain.KindInfo{}, false
}
