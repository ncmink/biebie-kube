package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestATreeIsReadAtTheCommitItResolved(t *testing.T) {
	origin := repository(t)
	cache := NewCache(filepath.Join(t.TempDir(), "mirrors"))

	tree, err := cache.Open(t.Context(), local(origin), revision(t, "main"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// The commit rather than the branch name, because `main` is a different
	// tree tomorrow and an answer nobody can check is not worth giving.
	if len(tree.Commit()) != 40 {
		t.Fatalf("commit = %q", tree.Commit())
	}

	entries, err := tree.List(t.Context(), directory(t, "apps/payment/prod"))
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	found := names(entries)
	want := []string{
		"apps/payment/prod/deployment.yaml",
		"apps/payment/prod/nested/config.yaml",
		"apps/payment/prod/service.yaml",
	}
	if strings.Join(found, ",") != strings.Join(want, ",") {
		t.Fatalf("listed %v, want %v", found, want)
	}

	content, err := tree.Read(t.Context(), "apps/payment/prod/deployment.yaml")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(content), "payment-api") {
		t.Fatalf("content = %q", content)
	}
}

func TestListingADirectoryDoesNotReachIntoItsSiblings(t *testing.T) {
	// An Application points at one directory. Reading the one beside it would
	// find manifests that belong to a different Application and name the wrong
	// file as this object's desired state.
	origin := repository(t)
	cache := NewCache(filepath.Join(t.TempDir(), "mirrors"))

	tree, err := cache.Open(t.Context(), local(origin), revision(t, "main"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	entries, err := tree.List(t.Context(), directory(t, "apps/payment/prod"))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Path, "apps/other/") {
			t.Fatalf("a sibling directory was listed: %q", entry.Path)
		}
	}
}

func TestASizeArrivesWithEachNameSoALargeFileCanBeSkipped(t *testing.T) {
	origin := repository(t)
	cache := NewCache(filepath.Join(t.TempDir(), "mirrors"))

	tree, err := cache.Open(t.Context(), local(origin), revision(t, "main"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	entries, err := tree.List(t.Context(), directory(t, "apps/payment/prod"))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, entry := range entries {
		if entry.Size <= 0 {
			t.Fatalf("%s has size %d", entry.Path, entry.Size)
		}
	}
}

func TestARevisionThatIsNotThereIsSaidSoRatherThanGuessedAt(t *testing.T) {
	origin := repository(t)
	cache := NewCache(filepath.Join(t.TempDir(), "mirrors"))

	_, err := cache.Open(t.Context(), local(origin), revision(t, "release/never-existed"))
	if err == nil {
		t.Fatal("a missing branch was accepted")
	}
	if got := kind(t, err); got != ErrorNoRevision {
		t.Fatalf("kind = %q: %v", got, err)
	}
}

func TestATagIsPeeledToTheCommitUnderneathIt(t *testing.T) {
	origin := repository(t)
	git(t, origin, "tag", "-a", "v1.0.0", "-m", "release")

	cache := NewCache(filepath.Join(t.TempDir(), "mirrors"))
	tree, err := cache.Open(t.Context(), local(origin), revision(t, "v1.0.0"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// An annotated tag is its own object. Reading it as a tree without peeling
	// would fail, so a repository that tags its releases would be unreadable.
	if _, err := tree.List(t.Context(), directory(t, "apps/payment/prod")); err != nil {
		t.Fatalf("list at a tag: %v", err)
	}
}

func TestAMirrorIsReusedAndPicksUpACommitMadeAfterIt(t *testing.T) {
	origin := repository(t)
	root := filepath.Join(t.TempDir(), "mirrors")
	cache := NewCache(root)

	first, err := cache.Open(t.Context(), local(origin), revision(t, "main"))
	if err != nil {
		t.Fatalf("first open: %v", err)
	}

	write(t, origin, "apps/payment/prod/extra.yaml", "kind: ConfigMap\n")
	git(t, origin, "add", ".")
	git(t, origin, "commit", "-m", "another")

	second, err := cache.Open(t.Context(), local(origin), revision(t, "main"))
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	if first.Commit() == second.Commit() {
		t.Fatal("the mirror served a stale commit rather than fetching")
	}
	if first.dir != second.dir {
		t.Fatal("the same repository was mirrored twice")
	}
}

func TestAnEntryThatIsNotAFileIsSkipped(t *testing.T) {
	// A submodule arrives as `commit` and has no contents in this repository
	// to read at all.
	if _, ok := parseEntry("160000 commit a1b2c3\t   -\tvendor/thing"); ok {
		t.Fatal("a submodule was taken for a file")
	}
	if _, ok := parseEntry(""); ok {
		t.Fatal("an empty record was taken for a file")
	}
	entry, ok := parseEntry("100644 blob 5f2c9a1b2c3d4e5f6071829304050607080910ab     1234\tapps/x.yaml")
	if !ok || entry.Path != "apps/x.yaml" || entry.Size != 1234 {
		t.Fatalf("entry = %+v, ok = %v", entry, ok)
	}
}

// local builds a Remote pointing at a directory.
//
// ParseRemote refuses a filesystem path on purpose, so this exists only inside
// the package and only for these tests: the alternative is testing the clone
// against somebody's real repository over the network.
func local(dir string) Remote { return Remote{url: dir} }

func revision(t *testing.T, raw string) Revision {
	t.Helper()
	value, err := ParseRevision(raw)
	if err != nil {
		t.Fatalf("revision %q: %v", raw, err)
	}
	return value
}

func directory(t *testing.T, raw string) Path {
	t.Helper()
	value, err := ParsePath(raw)
	if err != nil {
		t.Fatalf("path %q: %v", raw, err)
	}
	return value
}

func names(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Path)
	}
	return out
}

// repository builds a small git repository to read.
func repository(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed on this machine")
	}

	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")

	write(t, dir, "README.md", "# infra\n")
	write(t, dir, "apps/payment/prod/deployment.yaml", "kind: Deployment\nmetadata:\n  name: payment-api\n")
	write(t, dir, "apps/payment/prod/service.yaml", "kind: Service\nmetadata:\n  name: payment-api\n")
	write(t, dir, "apps/payment/prod/nested/config.yaml", "kind: ConfigMap\nmetadata:\n  name: payment\n")
	write(t, dir, "apps/other/deployment.yaml", "kind: Deployment\nmetadata:\n  name: other\n")

	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "first")
	return dir
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		// The machine running this has a global git configuration and it is
		// not this test's business what is in it.
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
