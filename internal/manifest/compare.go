package manifest

import (
	"reflect"
	"strings"

	"biebie-kube/internal/domain"
)

// Comparing the snapshot an editor opened with what is in it now.
//
// This is Original against Edited, and it is not the comparison in the GitOps
// panel. That one reads a repository and asks whether the cluster has drifted
// from what somebody committed. This one reads nothing, asks whether one person
// has changed anything since they opened an editor, and is the only comparison
// that can honestly be shown beside a Revert button.
//
// Nothing here touches a cluster, which is the point: it is the part of the
// feature that can be wrong in a way nobody notices, so it is a pure function
// over two strings.

// maxDiffLines bounds the line comparison.
//
// The algorithm below is quadratic in the number of lines, which is nothing at
// all for a manifest and would be a frozen window for a ConfigMap holding a
// megabyte of embedded configuration. Past this size the counts are reported
// from a cheaper positional comparison, which is less precise about where the
// difference is and exactly as correct about whether there is one.
const maxDiffLines = 3000

// CompareEdit holds an editor's original snapshot against its current text.
//
// Both questions are answered because they disagree in a way that matters.
// Dirty is about the text and is what a "modified" marker means: it goes true
// on the first keystroke, including one that only moves a key. Equivalent is
// about the object and is what decides whether applying would change anything,
// so a file whose keys were reordered is dirty and equivalent at once.
//
// Equivalence is judged on the parsed objects rather than on re-encoded text.
// Re-encoding would answer a slightly different question — whether this
// application's YAML writer produces the same bytes — and would call two
// genuinely identical objects different if it ever changed how it wraps a line.
func CompareEdit(original, edited string) domain.EditComparison {
	out := domain.EditComparison{Dirty: trimEnd(original) != trimEnd(edited)}

	before, beforeErr := parse(original)
	after, afterErr := parse(edited)
	switch {
	case afterErr != nil:
		// Text mid-edit is not YAML most of the time. It is reported and not
		// raised, and nothing is claimed about its meaning.
		out.Invalid = afterErr.Error()
	case beforeErr == nil:
		out.Equivalent = reflect.DeepEqual(before.Object, after.Object)
	}

	out.Added, out.Removed, out.Hunks = countLines(lines(original), lines(edited))
	return out
}

// countLines reports how much of the text differs and in how many places.
//
// Hunks is the number the editor shows, because it is the number a person
// would say out loud: three separate edits are three changes whether they add
// one line or thirty. The line counts are carried alongside it, since three
// changes across three lines and three across three hundred are not the same
// review.
func countLines(before, after []string) (added, removed, hunks int) {
	if len(before) > maxDiffLines || len(after) > maxDiffLines {
		return roughly(before, after)
	}

	table := lcs(before, after)

	// Walking the table forwards produces the edit script in file order, which
	// is what makes a delete immediately followed by an insert readable as one
	// change rather than two.
	i, j := 0, 0
	inHunk := false
	for i < len(before) || j < len(after) {
		switch {
		case i < len(before) && j < len(after) && before[i] == after[j]:
			i, j = i+1, j+1
			inHunk = false
		case j < len(after) && (i == len(before) || table[i][j+1] >= table[i+1][j]):
			added++
			j++
			if !inHunk {
				hunks++
				inHunk = true
			}
		default:
			removed++
			i++
			if !inHunk {
				hunks++
				inHunk = true
			}
		}
	}
	return added, removed, hunks
}

// lcs builds the longest-common-subsequence table for two line sequences.
func lcs(before, after []string) [][]int {
	table := make([][]int, len(before)+1)
	for i := range table {
		table[i] = make([]int, len(after)+1)
	}
	for i := len(before) - 1; i >= 0; i-- {
		for j := len(after) - 1; j >= 0; j-- {
			if before[i] == after[j] {
				table[i][j] = table[i+1][j+1] + 1
				continue
			}
			table[i][j] = max(table[i+1][j], table[i][j+1])
		}
	}
	return table
}

// roughly counts a difference too large to align properly.
//
// It compares line by line at the same index, which over-reports whenever
// something was inserted near the top. That is stated rather than hidden: the
// counts are a size, the diff on screen is the truth, and a document this large
// is one nobody is reviewing by counting hunks anyway.
func roughly(before, after []string) (added, removed, hunks int) {
	shared := min(len(before), len(after))
	inHunk := false
	for i := 0; i < shared; i++ {
		if before[i] == after[i] {
			inHunk = false
			continue
		}
		added++
		removed++
		if !inHunk {
			hunks++
			inHunk = true
		}
	}
	if extra := len(after) - shared; extra > 0 {
		added += extra
		if !inHunk {
			hunks++
		}
	}
	if extra := len(before) - shared; extra > 0 {
		removed += extra
		if !inHunk {
			hunks++
		}
	}
	return added, removed, hunks
}

// lines splits text for comparison, ignoring a trailing newline.
//
// A file that ends with a newline and one that does not are the same manifest,
// and reporting the difference as an added empty line is the kind of phantom
// change that teaches people to stop reading the diff.
func lines(text string) []string {
	text = trimEnd(text)
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func trimEnd(text string) string { return strings.TrimRight(text, "\n") }
