package authoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestASessionLivesUnderTheDependencyDirectory(t *testing.T) {
	// Node resolves `require` by walking up from the file. The sessions sit
	// inside the dependency project for that reason alone, and a change that
	// moved them elsewhere would send npm install running behind a Preview.
	workspace := NewWorkspace(t.TempDir())

	session, err := workspace.NewSession()
	if err != nil {
		t.Fatal(err)
	}

	if !within(workspace.runtimeDir(), session.Dir()) {
		t.Fatalf("session %q is not inside %q", session.Dir(), workspace.runtimeDir())
	}
	if _, err := os.Stat(filepath.Join(session.Dir(), "cdk8s.yaml")); err != nil {
		t.Fatalf("the session has no cdk8s.yaml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace.runtimeDir(), "package.json")); err != nil {
		t.Fatalf("the dependency project was not written: %v", err)
	}
}

func TestASessionIdentifierCannotEscapeTheWorkspace(t *testing.T) {
	// The identifier crossed a binding, so it is untrusted. A session named
	// `../../..` would otherwise resolve to a directory this application then
	// writes main.ts into and deletes on Discard.
	workspace := NewWorkspace(t.TempDir())

	for _, id := range []string{
		"", "..", "../..", "../../etc", "a/b", `a\b`, "..%2f..", "./..",
	} {
		if _, err := workspace.Session(id); err == nil {
			t.Fatalf("session %q was accepted", id)
		}
	}
}

func TestASessionIdentifierResolvesBackToItsOwnDirectory(t *testing.T) {
	workspace := NewWorkspace(t.TempDir())

	created, err := workspace.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	found, err := workspace.Session(created.ID)
	if err != nil {
		t.Fatal(err)
	}

	if found.Dir() != created.Dir() {
		t.Fatalf("%q resolved to %q", created.Dir(), found.Dir())
	}
}

func TestDiscardingAnEscapingIdentifierRemovesNothing(t *testing.T) {
	root := t.TempDir()
	workspace := NewWorkspace(root)

	bystander := filepath.Join(root, "important")
	if err := os.MkdirAll(bystander, 0o755); err != nil {
		t.Fatal(err)
	}

	workspace.Discard("../important")

	if _, err := os.Stat(bystander); err != nil {
		t.Fatalf("a directory outside the sessions tree was removed: %v", err)
	}
}

func TestAnUnpreparedWorkspaceSaysSo(t *testing.T) {
	workspace := NewWorkspace(t.TempDir())

	if workspace.Prepared() {
		t.Fatal("an empty workspace reported itself as prepared")
	}

	// The marker is the installed package rather than a file this application
	// writes, so a half-deleted node_modules reads as unprepared instead of as
	// ready-and-broken.
	dir := filepath.Join(workspace.runtimeDir(), "node_modules", "cdk8s")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if workspace.Prepared() {
		t.Fatal("a node_modules entry with no package.json read as prepared")
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !workspace.Prepared() {
		t.Fatal("an installed cdk8s did not read as prepared")
	}
}

func TestWithinRejectsASiblingWithASharedPrefix(t *testing.T) {
	// A string prefix test says /tmp/authoring-evil is inside /tmp/authoring.
	if within("/tmp/authoring", "/tmp/authoring-evil") {
		t.Fatal("a sibling directory was reported as contained")
	}
	if !within("/tmp/authoring", "/tmp/authoring/sessions/abc") {
		t.Fatal("a real child was reported as outside")
	}
}

func TestTheTypeScriptTemplateOnlyImportsCdk8s(t *testing.T) {
	// The dependency policy of this slice, stated as a test. Nothing typed in
	// the editor can add a package, and the starter must not suggest one that
	// `cdk8s import` would have to generate.
	template := typescriptStarter(target{
		apiVersion: "apps/v1", kind: "Deployment", namespace: "reporting", namespaced: true,
	})

	for _, line := range strings.Split(template, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "import ") {
			continue
		}
		if !strings.Contains(line, "'cdk8s'") {
			t.Fatalf("the starter imports something outside the fixed dependency set: %q", line)
		}
	}
}

func TestTheGeneratedProjectRunsTypeScriptDirectly(t *testing.T) {
	// cdk8s.yaml is where the child process command lives. It is an argument
	// vector to Node, not a shell line, and a `&&` or a `$(…)` appearing here
	// would be a shell reintroduced by the back door.
	if strings.ContainsAny(cdk8sYAML, "&|$`") {
		t.Fatalf("the app command looks like shell: %q", cdk8sYAML)
	}
	if !strings.Contains(cdk8sYAML, "app: node -r ts-node/register main.ts") {
		t.Fatalf("cdk8s.yaml = %q", cdk8sYAML)
	}
}
