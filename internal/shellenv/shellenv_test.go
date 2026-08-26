package shellenv

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestExtractSurvivesStartupChatter is the test that matters for the marker
// scheme: an interactive shell prints upgrade notices, motd banners and
// version manager warnings on the same stream as the answer, and an
// implementation that read the whole output would set PATH to that text.
func TestExtractSurvivesStartupChatter(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string
		ok   bool
	}{
		{
			name: "surrounded by banners",
			out: "Last login: Wed Aug 26\n2 casks are outdated.\n" +
				markerBegin + "/opt/homebrew/bin:/usr/bin" + markerEnd +
				"\nnvm: no .nvmrc found\n",
			want: "/opt/homebrew/bin:/usr/bin",
			ok:   true,
		},
		{
			name: "no markers at all",
			out:  "zsh: command not found: printf\n",
			ok:   false,
		},
		{
			name: "truncated before the closing marker",
			out:  markerBegin + "/usr/bin",
			ok:   false,
		},
		{
			name: "shell exported an empty PATH",
			out:  markerBegin + markerEnd,
			ok:   false,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, ok := extract(test.out)
			if ok != test.ok {
				t.Fatalf("extract ok = %v, want %v", ok, test.ok)
			}
			if got != test.want {
				t.Errorf("extract = %q, want %q", got, test.want)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	cases := []struct {
		name  string
		lists []string
		want  []string
	}{
		{
			name:  "the shell's answer is preferred but nothing is lost",
			lists: []string{"/opt/homebrew/bin:/usr/bin", "/usr/bin:/usr/sbin"},
			want:  []string{"/opt/homebrew/bin", "/usr/bin", "/usr/sbin"},
		},
		{
			// A literal ~ is common in a hand-edited .zshrc and exec.LookPath
			// does not expand it, so keeping the entry would only slow every
			// lookup down with a stat of a directory named "~".
			name:  "tilde and relative entries are dropped",
			lists: []string{"~/.dotnet/tools:./bin:/usr/bin"},
			want:  []string{"/usr/bin"},
		},
		{
			name:  "empty segments are dropped",
			lists: []string{"/usr/bin::"},
			want:  []string{"/usr/bin"},
		},
		{
			name:  "duplicates differing only by a trailing slash collapse",
			lists: []string{"/usr/bin/:/usr/bin"},
			want:  []string{"/usr/bin"},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := filepath.SplitList(merge(test.lists...))
			if !slices.Equal(got, test.want) {
				t.Errorf("merge = %v, want %v", got, test.want)
			}
		})
	}
}

// TestLoginShellPathReadsTheExportedValue exercises the real shell invocation,
// because the risk in this package is the quoting of the script rather than the
// parsing of its output.
func TestLoginShellPathReadsTheExportedValue(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh on this machine")
	}
	t.Setenv("SHELL", "/bin/sh")

	got, err := loginShellPath(context.Background())
	if err != nil {
		t.Fatalf("loginShellPath: %v", err)
	}

	// Any login shell on a Unix machine exports a PATH that can run the
	// commands in /bin, so its absence means the script did not report the
	// shell's own value.
	entries := filepath.SplitList(got)
	if !slices.Contains(entries, "/bin") && !slices.Contains(entries, "/usr/bin") {
		t.Errorf("loginShellPath = %q, want a PATH containing /bin or /usr/bin", got)
	}
	if strings.Contains(got, markerBegin) || strings.Contains(got, markerEnd) {
		t.Errorf("loginShellPath = %q, markers were not stripped", got)
	}
}

// TestApplyKeepsWhatThisProcessCouldAlreadyReach guards the merge direction: a
// user whose rc file overwrites PATH instead of appending to it must not end up
// with fewer usable directories than before Biebie Kube asked.
func TestApplyKeepsWhatThisProcessCouldAlreadyReach(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh on this machine")
	}
	sentinel := t.TempDir()
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("PATH", sentinel)

	merged, err := Apply(context.Background())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !slices.Contains(filepath.SplitList(merged), sentinel) {
		t.Errorf("Apply = %q, want it to still contain %q", merged, sentinel)
	}
	if os.Getenv("PATH") != merged {
		t.Errorf("PATH = %q, want the merged value %q", os.Getenv("PATH"), merged)
	}
}

// TestApplyLeavesPathAloneWhenThereIsNoShell covers a process started by a
// launch agent with a scrubbed environment: reporting the error is useful, but
// clearing PATH would break the helpers that were reachable.
func TestApplyLeavesPathAloneWhenThereIsNoShell(t *testing.T) {
	t.Setenv("SHELL", "")
	t.Setenv("PATH", "/usr/bin")

	got, err := Apply(context.Background())
	if err == nil {
		t.Error("Apply returned no error with SHELL unset")
	}
	if got != "/usr/bin" {
		t.Errorf("Apply = %q, want the untouched /usr/bin", got)
	}
	if os.Getenv("PATH") != "/usr/bin" {
		t.Errorf("PATH = %q, want it left at /usr/bin", os.Getenv("PATH"))
	}
}
