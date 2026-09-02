package manifest

import (
	"strings"
	"testing"

	"biebie-kube/internal/domain"
)

// Original against Edited. Every test here is about the editor being able to
// answer "what have I changed since I opened this?" — which stops being
// answerable the moment Original is allowed to move, or the moment "dirty" and
// "would actually change the object" are collapsed into one flag.

const deployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: super-report
  namespace: reporting
spec:
  replicas: 3
  template:
    spec:
      containers:
      - image: acme/report:1.4.0
        name: api
`

func TestAnUntouchedEditorIsClean(t *testing.T) {
	out := CompareEdit(deployment, deployment)

	if out.Dirty {
		t.Fatal("an editor that opened and was not typed in reports as modified")
	}
	if !out.Equivalent {
		t.Fatal("identical text was not reported as the same object")
	}
	if out.Hunks != 0 || out.Added != 0 || out.Removed != 0 {
		t.Fatalf("changes counted in an untouched editor: %+v", out)
	}
}

func TestChangingAValueMakesTheSessionDirty(t *testing.T) {
	edited := strings.Replace(deployment, "replicas: 3", "replicas: 5", 1)

	out := CompareEdit(deployment, edited)

	if !out.Dirty {
		t.Fatal("a changed value did not mark the session modified")
	}
	if out.Equivalent {
		t.Fatal("a changed replica count was reported as the same object")
	}
	if out.Hunks != 1 {
		t.Fatalf("hunks = %d, want the one place the two differ", out.Hunks)
	}
	if out.Added != 1 || out.Removed != 1 {
		t.Fatalf("added = %d, removed = %d", out.Added, out.Removed)
	}
}

func TestSeparateEditsAreCountedSeparately(t *testing.T) {
	// The number the editor shows is places, not lines. Three edits are three
	// changes whether they touch one line each or thirty.
	edited := strings.Replace(deployment, "replicas: 3", "replicas: 5", 1)
	edited = strings.Replace(edited, "acme/report:1.4.0", "acme/report:1.5.0", 1)

	if out := CompareEdit(deployment, edited); out.Hunks != 2 {
		t.Fatalf("hunks = %d, want 2: %+v", out.Hunks, out)
	}
}

func TestRevertingToTheSnapshotIsCleanAgain(t *testing.T) {
	// Revert is restoring the original text and nothing else. What proves it
	// worked is that the comparison agrees the session is untouched — the
	// editor does not get to keep its own opinion about being dirty.
	edited := strings.Replace(deployment, "replicas: 3", "replicas: 5", 1)
	if !CompareEdit(deployment, edited).Dirty {
		t.Fatal("the edit under test did not register")
	}

	out := CompareEdit(deployment, deployment)

	if out.Dirty || out.Hunks != 0 {
		t.Fatalf("restoring the original snapshot left the session modified: %+v", out)
	}
}

func TestReformattingIsModifiedAndStillTheSameObject(t *testing.T) {
	// The two questions coming apart, which is why both are answered. The text
	// differs, so the editor says modified; the object does not, so applying
	// would do nothing and the screen can say that instead of promising a
	// change it will not make.
	reordered := `kind: Deployment
apiVersion: apps/v1
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: api
          image: acme/report:1.4.0
metadata:
  namespace: reporting
  name: super-report
`

	out := CompareEdit(deployment, reordered)

	if !out.Dirty {
		t.Fatal("reordered text was reported as untouched")
	}
	if !out.Equivalent {
		t.Fatal("reordering keys was reported as a different object")
	}
}

func TestATrailingNewlineIsNotAChange(t *testing.T) {
	// A phantom change on every file that came back without its final newline
	// is how people learn to stop reading the diff.
	if out := CompareEdit(deployment, strings.TrimRight(deployment, "\n")); out.Dirty {
		t.Fatalf("a trailing newline was counted as an edit: %+v", out)
	}
}

func TestTextMidEditIsReportedRatherThanRaised(t *testing.T) {
	// YAML is invalid most of the time somebody is typing it. An editor that
	// treated that as an error would shout constantly, and nothing may be
	// claimed about the meaning of text that does not parse.
	out := CompareEdit(deployment, "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: [unclosed\n")

	if out.Invalid == "" {
		t.Fatal("unparseable text was accepted silently")
	}
	if out.Equivalent {
		t.Fatal("text that does not parse was reported as the same object")
	}
	if !out.Dirty {
		t.Fatal("an editor full of invalid text is not an untouched editor")
	}
}

func TestAnAddedBlockCountsItsLines(t *testing.T) {
	edited := strings.Replace(deployment, "      containers:", `      nodeSelector:
        disk: ssd
      containers:`, 1)

	out := CompareEdit(deployment, edited)

	if out.Added != 2 || out.Removed != 0 {
		t.Fatalf("added = %d, removed = %d", out.Added, out.Removed)
	}
	if out.Hunks != 1 {
		t.Fatalf("hunks = %d, want one insertion", out.Hunks)
	}
}

func TestAVeryLargeDocumentStillAnswersTheQuestionThatMatters(t *testing.T) {
	// Past the alignment budget the counts get rougher. Whether anything
	// changed at all does not, because that is what the Revert button and the
	// Apply button both depend on.
	big := strings.Repeat("  key: value\n", maxDiffLines+10)

	if CompareEdit(big, big).Dirty {
		t.Fatal("an unchanged large document reported as modified")
	}
	if !CompareEdit(big, big+"  extra: true\n").Dirty {
		t.Fatal("a changed large document reported as clean")
	}
}

func TestTheComparisonNeverTouchesACluster(t *testing.T) {
	// Guarded by construction rather than by a mock: CompareEdit is a function
	// of two strings on a package-level receiver-free path, so there is no
	// client for it to reach. This test states the intent so a later change
	// that adds one has to delete a test that says why not.
	var _ func(string, string) domain.EditComparison = CompareEdit
}
