package kubeconfig

import (
	"path/filepath"
	"testing"

	"biebie-kube/internal/store"
)

// Forgetting a kubeconfig has to forget that its contexts were ever offered,
// or importing the same file again would find nothing to add.
func TestForgetClearsTheContextsItOffered(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "data.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	err = st.Update(func(data *store.Data) error {
		data.Kubeconfigs = []store.KubeconfigRecord{
			{Ref: "gone", Name: "Retired", Path: filepath.Join(dir, "retired")},
			{Ref: "kept", Name: "Default", Path: filepath.Join(dir, "config")},
		}
		data.SeenContexts = []store.SeenContextRecord{
			{KubeconfigRef: "gone", ContextName: "acme-dev"},
			{KubeconfigRef: "kept", ContextName: "acme-prod"},
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed store: %v", err)
	}

	if err := NewService(st, filepath.Join(dir, "managed")).Forget("gone"); err != nil {
		t.Fatalf("Forget: %v", err)
	}

	remaining := st.Read().SeenContexts
	if len(remaining) != 1 {
		t.Fatalf("%d seen contexts remain, want 1", len(remaining))
	}
	if remaining[0].KubeconfigRef != "kept" {
		t.Errorf("the surviving record belongs to %q, want kept", remaining[0].KubeconfigRef)
	}
}
