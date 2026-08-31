package gitops

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/git"
)

func TestOnlyTheFilesWorthOpeningAreOpened(t *testing.T) {
	files := fakeTree{
		"apps/deployment.yaml": manifest("payment-api", "payment"),
		"apps/README.md":       "payment-api lives here\n",
		"apps/Chart.lock":      "generated\n",
	}
	entries := []git.Entry{
		{Path: "apps/deployment.yaml", Size: 200},
		{Path: "apps/README.md", Size: 30},
		{Path: "apps/Chart.lock", Size: 20},
	}

	out := scan(t.Context(), files, domain.GitSource{}, entries, deployment)
	if out.scanned != 1 {
		t.Fatalf("scanned %d files, want 1", out.scanned)
	}
	if len(out.locations) != 1 || out.locations[0].Path != "apps/deployment.yaml" {
		t.Fatalf("located %+v", out.locations)
	}
}

func TestAFileTooLargeToBeAManifestIsSkipped(t *testing.T) {
	// A repository that vendors every CRD in the world has one of these, and
	// pulling it through a pipe costs more than the answer is worth.
	entries := []git.Entry{
		{Path: "apps/bundle.yaml", Size: fileSizeLimit + 1},
		{Path: "apps/deployment.yaml", Size: 200},
	}
	files := fakeTree{
		"apps/bundle.yaml":     manifest("payment-api", "payment"),
		"apps/deployment.yaml": manifest("payment-api", "payment"),
	}

	out := scan(t.Context(), files, domain.GitSource{}, entries, deployment)
	if out.scanned != 1 {
		t.Fatalf("scanned %d files, want 1", out.scanned)
	}
	if len(out.locations) != 1 || out.locations[0].Path != "apps/deployment.yaml" {
		t.Fatalf("located %+v", out.locations)
	}
}

func TestASearchThatStoppedEarlySaysSo(t *testing.T) {
	// This is the one that matters. A scan that quietly hit its budget and
	// reported nothing would read as "the repository does not declare this
	// object", which is a wrong answer rather than a missing one.
	files := fakeTree{}
	var entries []git.Entry
	for index := range fileBudget + 10 {
		name := fmt.Sprintf("apps/%03d.yaml", index)
		files[name] = manifest("something-else", "payment")
		entries = append(entries, git.Entry{Path: name, Size: 100})
	}

	out := scan(t.Context(), files, domain.GitSource{}, entries, deployment)
	if !out.truncated {
		t.Fatal("a search that stopped early did not say so")
	}
	if out.scanned != fileBudget {
		t.Fatalf("scanned %d files, want %d", out.scanned, fileBudget)
	}
}

func TestADirectoryInsideTheBudgetIsNotCalledTruncated(t *testing.T) {
	files := fakeTree{}
	var entries []git.Entry
	for index := range fileBudget {
		name := fmt.Sprintf("apps/%03d.yaml", index)
		files[name] = manifest("something-else", "payment")
		entries = append(entries, git.Entry{Path: name, Size: 100})
	}

	out := scan(t.Context(), files, domain.GitSource{}, entries, deployment)
	if out.truncated {
		t.Fatalf("scanned exactly %d files and called it truncated", out.scanned)
	}
}

func TestOneUnreadableFileDoesNotAbandonTheDirectory(t *testing.T) {
	files := fakeTree{"apps/deployment.yaml": manifest("payment-api", "payment")}
	entries := []git.Entry{
		{Path: "apps/gone.yaml", Size: 100},
		{Path: "apps/deployment.yaml", Size: 200},
	}

	out := scan(t.Context(), files, domain.GitSource{}, entries, deployment)
	if len(out.locations) != 1 {
		t.Fatalf("located %+v", out.locations)
	}
}

func TestTwoFilesDeclaringTheSameObjectAreBothKept(t *testing.T) {
	// A base beside its overlay. Returning one of them would be inventing an
	// answer the repository does not give.
	files := fakeTree{
		"apps/base/deployment.yaml":    manifest("payment-api", ""),
		"apps/overlay/deployment.yaml": manifest("payment-api", "payment"),
	}
	entries := []git.Entry{
		{Path: "apps/base/deployment.yaml", Size: 200},
		{Path: "apps/overlay/deployment.yaml", Size: 200},
	}

	out := scan(t.Context(), files, domain.GitSource{}, entries, deployment)
	if len(out.locations) != 2 {
		t.Fatalf("located %+v", out.locations)
	}
}

func TestSourcesAreSplitByWhetherThereIsAFileToLookFor(t *testing.T) {
	sources := []domain.GitSource{
		{Path: "charts/payment", Certainty: domain.ManifestGenerated, Renderer: domain.RendererHelm,
			Note: "Helm renders this object from templates."},
		{Path: "apps/payment/prod", Certainty: domain.ManifestTree},
	}
	if trees := treeSources(sources); len(trees) != 1 || trees[0].Path != "apps/payment/prod" {
		t.Fatalf("trees = %+v", trees)
	}

	// An Application that renders everything is not searched at all, and what
	// it says about itself is a better sentence than an empty search.
	certainty, reason := nothingToSearch(sources[:1])
	if certainty != domain.ManifestGenerated || reason != sources[0].Note {
		t.Fatalf("certainty = %q, reason = %q", certainty, reason)
	}
}

// fakeTree stands in for a repository so the budget can be tested without one.
type fakeTree map[string]string

func (f fakeTree) Read(_ context.Context, file string) ([]byte, error) {
	content, ok := f[file]
	if !ok {
		return nil, errors.New("no such file")
	}
	return []byte(content), nil
}

func manifest(name, namespace string) string {
	out := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: " + name + "\n"
	if namespace != "" {
		out += "  namespace: " + namespace + "\n"
	}
	return out
}
