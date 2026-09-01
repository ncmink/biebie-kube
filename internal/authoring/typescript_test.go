package authoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"biebie-kube/internal/domain"
)

func TestEveryChartCdk8sWroteIsRead(t *testing.T) {
	// cdk8s writes one file per Chart, and an application that read the first
	// would show one object and create one where the TypeScript declared six.
	dir := t.TempDir()
	write(t, dir, "0000-first.k8s.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: one\n")
	write(t, dir, "0001-second.k8s.yaml", "apiVersion: v1\nkind: Service\nmetadata:\n  name: two\n")

	manifest, err := readDist(dir)
	if err != nil {
		t.Fatal(err)
	}

	objects, problems := parseAll(manifest)
	if len(problems) != 0 {
		t.Fatalf("problems = %+v", problems)
	}
	if len(objects) != 2 {
		t.Fatalf("read %d objects from %q", len(objects), manifest)
	}
	// The order is fixed rather than whatever the filesystem returns, so the
	// preview a person reads is the order the objects are created in.
	if objects[0].GetName() != "one" || objects[1].GetName() != "two" {
		t.Fatalf("order = %q then %q", objects[0].GetName(), objects[1].GetName())
	}
}

func TestASynthThatWroteNothingIsAFailureRatherThanAnEmptyManifest(t *testing.T) {
	if _, err := readDist(t.TempDir()); err == nil {
		t.Fatal("an empty output directory produced a manifest")
	}
	if _, err := readDist(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a missing output directory produced a manifest")
	}
}

func TestTheTypeScriptErrorIsLedWithRatherThanTheFraming(t *testing.T) {
	// ts-node surrounds the one useful line with a page of its own. A person
	// shown the first line of that page has been told that cdk8s ran.
	out := errorLines(`Synthesizing...
/workspace/node_modules/ts-node/src/index.ts:859
    return new TSError(diagnosticText, diagnosticCodes, diagnostics);
main.ts(12,3): error TS2551: Property 'metadta' does not exist on type 'ApiObjectProps'.
    at createTSError (/workspace/node_modules/ts-node/src/index.ts:859:12)`)

	if !strings.Contains(firstLine(out), "TS2551") {
		t.Fatalf("the error was not led with: %q", out)
	}
}

func TestOutputWithNoRecognisableErrorIsKeptWhole(t *testing.T) {
	// A failure this parser has never seen must not be filtered down to
	// nothing. Showing everything is worse than showing one line, and both are
	// better than showing none.
	raw := "cdk8s stopped for a reason nothing here recognises"

	if errorLines(raw) != raw {
		t.Fatalf("unfamiliar output was discarded: %q", errorLines(raw))
	}
}

func TestAPartialCreationIsReportedAsOneAndNotAsARollback(t *testing.T) {
	// Kubernetes has no transaction across objects, and there is no rollback
	// here. Claiming one would be worse than the partial state itself.
	out := domain.CreateOutcome{
		Created: []domain.CreatedResource{
			{Kind: "ConfigMap", Ref: domain.ResourceRef{Name: "config"}},
			{Kind: "Service", Ref: domain.ResourceRef{Name: "service"}},
		},
		Failed: []domain.CreateFailure{
			{Kind: "Deployment", Name: "api", Error: "an object of that name already exists"},
		},
	}

	message := outcomeMessage(out)

	if !strings.Contains(message, "2 of 3 created") {
		t.Fatalf("message = %q", message)
	}
	if !strings.Contains(message, "did not remove") {
		t.Fatalf("the message does not say what is left behind: %q", message)
	}
	if strings.Contains(strings.ToLower(message), "rolled back") {
		t.Fatalf("the message claims a rollback that does not exist: %q", message)
	}
}

func TestNothingCreatedSaysSoPlainly(t *testing.T) {
	out := domain.CreateOutcome{
		Failed: []domain.CreateFailure{{Kind: "ConfigMap", Name: "example", Error: "this account may not create that object"}},
	}

	if message := outcomeMessage(out); !strings.HasPrefix(message, "Nothing was created") {
		t.Fatalf("message = %q", message)
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
