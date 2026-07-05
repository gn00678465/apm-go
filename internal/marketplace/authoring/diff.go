// This file (diff.go) is migrate.go's small unified-diff helper: mkt-044's
// `apm marketplace migrate --dry-run` prints a preview of the proposed
// apm.yml change instead of writing it, and design.md calls that preview a
// "unified diff" (Python's own migrate command uses
// difflib.unified_diff). There is no third-party diff dependency in this
// module (go.mod), so this file implements the minimal subset needed here.
package authoring

import (
	"fmt"
	"strings"
)

// diffOp is one line-level unified-diff opcode: ' ' (an unchanged line kept
// as context), '-' (only in the old text), or '+' (only in the new text).
type diffOp struct {
	kind byte
	text string
}

// unifiedDiff renders a single-hunk POSIX unified diff (--- / +++ / @@
// header, 3 lines of surrounding context) between oldText and newText.
// Migrate always changes exactly one contiguous span of apm.yml
// (yamlcore.PatchMappingPath's own single-key-value-span contract), so one
// hunk is always sufficient -- unlike difflib.unified_diff, this does not
// need to group multiple separate change regions. Returns "" when the two
// texts are identical.
func unifiedDiff(oldText, newText, fromFile, toFile string) string {
	oldLines := splitLines(oldText)
	newLines := splitLines(newText)
	ops := lcsDiff(oldLines, newLines)

	first, last, changed := changedRange(ops)
	if !changed {
		return ""
	}

	const context = 3
	lo := first - context
	if lo < 0 {
		lo = 0
	}
	hi := last + context + 1
	if hi > len(ops) {
		hi = len(ops)
	}

	// Every op before index `first` (the first change) is necessarily a ' '
	// (context) op -- lo <= first by construction above -- so ops[:lo]
	// consumes exactly one old and one new line per entry: the old/new
	// start line numbers are both simply lo+1.
	oldStart, newStart := lo+1, lo+1
	oldCount, newCount := 0, 0
	for _, op := range ops[lo:hi] {
		switch op.kind {
		case ' ':
			oldCount++
			newCount++
		case '-':
			oldCount++
		case '+':
			newCount++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n", fromFile)
	fmt.Fprintf(&b, "+++ %s\n", toFile)
	fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
	for _, op := range ops[lo:hi] {
		b.WriteByte(op.kind)
		b.WriteString(op.text)
		b.WriteByte('\n')
	}
	return b.String()
}

// splitLines splits text into lines without trailing newline characters
// (unlike strings.SplitAfter, so hunk lines can always be rejoined with a
// single '\n' on output regardless of whether text itself ended in one).
// Returns nil for an empty string, so an empty old/new text contributes no
// diffOps at all rather than one spurious empty-string "line".
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// changedRange returns the index of the first and last non-' ' op in ops,
// and whether any exist at all.
func changedRange(ops []diffOp) (first, last int, changed bool) {
	for i, op := range ops {
		if op.kind != ' ' {
			if !changed {
				first = i
				changed = true
			}
			last = i
		}
	}
	return first, last, changed
}

// lcsDiff computes a line-level edit script between a and b via a classic
// O(n*m) longest-common-subsequence table. apm.yml files are small enough
// (well under the sizes where this would matter) for the quadratic table to
// be a non-issue.
func lcsDiff(a, b []string) []diffOp {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}
