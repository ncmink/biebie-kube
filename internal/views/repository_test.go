package views

import (
	"path/filepath"
	"testing"
	"time"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/store"
)

func TestSaveAndListSavedViews(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(st)
	repo.now = func() time.Time { return time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC) }

	seedCluster(t, st, "cluster_a")

	view, err := repo.Save(domain.SavedViewInput{
		ClusterID:     "cluster_a",
		Title:         "Running shop pods",
		Kind:          domain.KindPod,
		Namespace:     "default",
		Mode:          domain.QueryModeExpression,
		Expression:    "restarts >= 1",
		LabelSelector: "app=shop",
		FieldSelector: "status.phase=Running",
		SortKey:       "name",
		SortDesc:      false,
		ColumnIDs:     []string{"node", "restarts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.ID == "" {
		t.Fatal("expected generated id")
	}
	if view.QueryVersion != domain.SavedViewQueryVersion {
		t.Fatalf("queryVersion = %d", view.QueryVersion)
	}

	views := repo.List("cluster_a")
	if len(views) != 1 {
		t.Fatalf("len = %d", len(views))
	}
	if views[0].Title != "Running shop pods" {
		t.Fatalf("title = %q", views[0].Title)
	}
}

func TestSaveRejectsTooManyViews(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(st)
	seedCluster(t, st, "cluster_a")

	for i := 0; i < domain.MaxSavedViewsPerCluster; i++ {
		if _, err := repo.Save(domain.SavedViewInput{
			ClusterID: "cluster_a",
			Title:     "view",
			Kind:      domain.KindPod,
		}); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	if _, err := repo.Save(domain.SavedViewInput{
		ClusterID: "cluster_a",
		Title:     "one too many",
		Kind:      domain.KindPod,
	}); err == nil {
		t.Fatal("expected limit error")
	}
}

func TestResolveFlagsMissingKindAndNamespace(t *testing.T) {
	view := domain.SavedView{
		QueryVersion: domain.SavedViewQueryVersion,
		Kind:         domain.KindPod,
		Namespace:    "missing",
		Mode:         domain.QueryModeText,
	}
	resolution := Resolve(
		view,
		[]domain.KindInfo{{Kind: domain.KindDeployment, Title: "Deployments"}},
		[]string{"default"},
		func(domain.ListQuery) domain.QueryDiagnostic { return domain.QueryDiagnostic{Valid: true} },
	)
	if resolution.Valid {
		t.Fatal("expected unresolved view")
	}
	if len(resolution.Issues) < 2 {
		t.Fatalf("issues = %#v", resolution.Issues)
	}
}

func seedCluster(t *testing.T, st *store.Store, id string) {
	t.Helper()
	if err := st.Update(func(data *store.Data) error {
		data.Clusters = append(data.Clusters, store.ClusterRecord{
			ID:        id,
			Name:      "test",
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
