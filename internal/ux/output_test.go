package ux

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// assertNoANSI fails the test if s contains any ANSI escape sequence. Used
// throughout to prove per-writer stripping on a non-terminal writer (a
// bytes.Buffer is never a terminal, see colorprofile.Detect).
func assertNoANSI(t *testing.T, name, s string) {
	t.Helper()
	if strings.Contains(s, "\x1b[") {
		t.Fatalf("%s output contains ANSI escape: %q", name, s)
	}
}

func TestTable_Golden_NonTTYAlignedNoANSI(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	headers := []string{"NAME", "SOURCE"}
	rows := [][]string{
		{"pkg-a", "github.com/a/a"},
		{"pkg-b", "github.com/b/b"},
	}

	// Act
	Table(&buf, headers, rows)
	out := buf.String()

	// Assert
	assertNoANSI(t, "Table", out)
	for _, want := range []string{"NAME", "SOURCE", "pkg-a", "pkg-b", "github.com/a/a", "github.com/b/b"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Table output missing %q: %q", want, out)
		}
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("Table output has too few lines: %d", len(lines))
	}
	width := lipgloss.Width(lines[0])
	for i, line := range lines {
		if got := lipgloss.Width(line); got != width {
			t.Fatalf("Table line %d width = %d, want %d (misaligned box)\n%s", i, got, width, out)
		}
	}
}

func TestTable_Golden_CJKAligned(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	headers := []string{"NAME", "描述"}
	rows := [][]string{
		{"pkg", "測試套件描述"},
		{"other", "short"},
	}

	// Act
	Table(&buf, headers, rows)
	out := buf.String()

	// Assert
	assertNoANSI(t, "Table CJK", out)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	width := lipgloss.Width(lines[0])
	for i, line := range lines {
		if got := lipgloss.Width(line); got != width {
			t.Fatalf("Table CJK line %d width = %d, want %d (misaligned box)\n%s", i, got, width, out)
		}
	}
}

func TestTable_Headerless(t *testing.T) {
	// Arrange
	var buf bytes.Buffer

	// Act
	Table(&buf, nil, [][]string{{"a", "b"}})
	out := buf.String()

	// Assert
	assertNoANSI(t, "Table headerless", out)
	if !strings.Contains(out, "a") || !strings.Contains(out, "b") {
		t.Fatalf("Table headerless output missing data: %q", out)
	}
}

func TestBulletList_Golden_NestedLevels(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	items := []Item{
		{Level: 0, Text: "top"},
		{Level: 1, Text: "child"},
		{Level: 0, Text: "second top"},
	}

	// Act
	BulletList(&buf, items)
	out := buf.String()

	// Assert
	assertNoANSI(t, "BulletList", out)
	for _, want := range []string{"top", "child", "second top"} {
		if !strings.Contains(out, want) {
			t.Fatalf("BulletList output missing %q: %q", want, out)
		}
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("BulletList produced %d lines, want 3:\n%s", len(lines), out)
	}
	childIndent := strings.Index(lines[1], "child")
	topIndent := strings.Index(lines[0], "top")
	if childIndent <= topIndent {
		t.Fatalf("expected child (level 1) to be indented further than top (level 0): %q vs %q", lines[1], lines[0])
	}
}

// TestBulletList_EnumeratorHasGapBeforeText is the R8a regression: the
// SymbolList enumerator must have a visible gap before the item text (an
// unstyled or unwidth-ed EnumeratorStyle collapses it to SymbolList+"text",
// see output.go/newBulletList's comment).
func TestBulletList_EnumeratorHasGapBeforeText(t *testing.T) {
	var buf bytes.Buffer
	BulletList(&buf, []Item{{Text: "hello"}})
	out := strings.TrimRight(buf.String(), "\n")

	if strings.Contains(out, SymbolList+"hello") {
		t.Fatalf("bullet enumerator has no gap before text: %q", out)
	}
	if !strings.Contains(out, SymbolList+" hello") {
		t.Fatalf("expected %q (centered 3-column enumerator), got: %q", SymbolList+" hello", out)
	}
}

// TestBulletList_MutedItemUsesColorMutedNotPlainText is the R9/R10c
// regression: a Muted item renders styled (ANSI-colored on a TTY-like
// writer) while a non-muted item at the same level does not, so muting is a
// presentation-only concern that never touches Text itself.
func TestBulletList_MutedItemUsesColorMutedNotPlainText(t *testing.T) {
	// bytes.Buffer is never a terminal, so ANSI is stripped either way
	// (see assertNoANSI's doc comment) -- this only verifies the plain text
	// is unaffected by Muted, i.e. Muted never mutates Item.Text itself.
	var buf bytes.Buffer
	BulletList(&buf, []Item{
		{Text: "new-dep"},
		{Text: "existing-dep", Muted: true},
	})
	out := buf.String()

	assertNoANSI(t, "BulletList muted", out)
	for _, want := range []string{"new-dep", "existing-dep"} {
		if !strings.Contains(out, want) {
			t.Fatalf("BulletList output missing %q: %q", want, out)
		}
	}
}

func TestTree_Golden_NestedChildren(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	root := TreeNode{
		Text: "root",
		Children: []TreeNode{
			{Text: "child-a"},
			{Text: "child-b", Children: []TreeNode{{Text: "grandchild"}}},
		},
	}

	// Act
	Tree(&buf, root)
	out := buf.String()

	// Assert
	assertNoANSI(t, "Tree", out)
	for _, want := range []string{"root", "child-a", "child-b", "grandchild"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Tree output missing %q: %q", want, out)
		}
	}
}

func TestSection_Golden(t *testing.T) {
	// Arrange
	var buf bytes.Buffer

	// Act
	Section(&buf, "Update plan")
	out := buf.String()

	// Assert
	assertNoANSI(t, "Section", out)
	if !strings.Contains(out, "Update plan") {
		t.Fatalf("Section output missing title: %q", out)
	}
}

func TestBox_Golden(t *testing.T) {
	// Arrange
	var buf bytes.Buffer

	// Act
	Box(&buf, "About to create", []string{"name: apm-go", "version: 1.0.0"})
	out := buf.String()

	// Assert
	assertNoANSI(t, "Box", out)
	for _, want := range []string{"About to create", "name: apm-go", "version: 1.0.0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Box output missing %q: %q", want, out)
		}
	}
	if !strings.ContainsAny(out, "╭╮╰╯") {
		t.Fatalf("Box output missing rounded border corners: %q", out)
	}
}

func TestDiff_Golden(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	diffText := "--- a/file\n+++ b/file\n@@ -1 +1 @@\n-old line\n+new line\n context line"

	// Act
	Diff(&buf, diffText)
	out := buf.String()

	// Assert
	assertNoANSI(t, "Diff", out)
	for _, want := range []string{"--- a/file", "+++ b/file", "@@ -1 +1 @@", "-old line", "+new line", "context line"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Diff output missing %q: %q", want, out)
		}
	}
}

// withTerminalWidth forces terminalWidthFor to report a fixed width for the
// duration of the test, standing in for a real TTY of that size.
func withTerminalWidth(t *testing.T, width int) {
	t.Helper()
	orig := terminalWidthFor
	terminalWidthFor = func(io.Writer) int { return width }
	t.Cleanup(func() { terminalWidthFor = orig })
}

// TestTable_OverflowingTerminalIsCappedAndWrapped covers the narrow-window
// case the fixed-content-width rendering used to break: a table wider than
// the terminal must shrink to the terminal width, word-wrapping cell content
// inside its column instead of letting the terminal hard-break the box.
func TestTable_OverflowingTerminalIsCappedAndWrapped(t *testing.T) {
	// Arrange
	withTerminalWidth(t, 40)
	var buf bytes.Buffer
	longCell := "a-very-long-detail message that is far wider than forty columns in total"

	// Act
	Table(&buf, []string{"NAME", "DETAIL"}, [][]string{{"tool", longCell}})

	// Assert
	out := buf.String()
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if got := lipgloss.Width(line); got > 40 {
			t.Errorf("line %q is %d columns wide, want <= 40 (terminal width)", line, got)
		}
	}
	// The wrapped content must survive: spot-check a word from the long
	// cell's head and tail.
	if !strings.Contains(out, "a-very-long-detail") || !strings.Contains(out, "total") {
		t.Errorf("output = %q, want the long cell's content preserved across wrapped lines", out)
	}
	lines := strings.Count(strings.TrimRight(out, "\n"), "\n") + 1
	if lines <= 4 {
		t.Errorf("output has %d lines, want > 4 (borders + multiple wrapped content lines)", lines)
	}
}

// TestTable_NarrowTableIsNotStretchedToTerminalWidth proves the cap only
// applies on overflow: Table.Width() would otherwise EXPAND a small table's
// columns to fill the terminal.
func TestTable_NarrowTableIsNotStretchedToTerminalWidth(t *testing.T) {
	// Arrange: render once with no width constraint as the baseline.
	var baseline bytes.Buffer
	Table(&baseline, []string{"A"}, [][]string{{"x"}})

	withTerminalWidth(t, 200)
	var buf bytes.Buffer

	// Act
	Table(&buf, []string{"A"}, [][]string{{"x"}})

	// Assert
	if buf.String() != baseline.String() {
		t.Errorf("narrow table under a wide terminal = %q, want the unconstrained rendering %q", buf.String(), baseline.String())
	}
}

// TestTerminalWidthFor_NonTerminalWriterReportsZero locks the default: a
// bytes.Buffer (tests, pipes, logs) is never width-constrained.
func TestTerminalWidthFor_NonTerminalWriterReportsZero(t *testing.T) {
	if got := terminalWidthFor(&bytes.Buffer{}); got != 0 {
		t.Errorf("terminalWidthFor(bytes.Buffer) = %d, want 0", got)
	}
}

// TestTable_MultilineCellsDrawRowSeparators covers the 2026-08-07 ruling:
// once any cell spans multiple lines, adjacent rows need separator lines to
// stay distinguishable.
func TestTable_MultilineCellsDrawRowSeparators(t *testing.T) {
	// Arrange: headerless two-row table, first cell multi-line.
	var buf bytes.Buffer

	// Act
	Table(&buf, nil, [][]string{{"first line\nsecond line"}, {"row two"}})

	// Assert: a row-separator junction appears between the two rows.
	if !strings.Contains(buf.String(), "├") {
		t.Errorf("output = %q, want a row separator between multi-line rows", buf.String())
	}
}

// TestTable_SingleLineRowsKeepSeparatorFreeRendering locks the default: a
// plain single-line table gains no separators (and no headerless "├" at
// all).
func TestTable_SingleLineRowsKeepSeparatorFreeRendering(t *testing.T) {
	// Arrange
	var buf bytes.Buffer

	// Act
	Table(&buf, nil, [][]string{{"a"}, {"b"}})

	// Assert
	if strings.Contains(buf.String(), "├") {
		t.Errorf("output = %q, want no row separators for single-line rows", buf.String())
	}
}

// TestTable_CappedWrappingDrawsRowSeparators: wrapping introduced by the
// terminal-width cap also triggers separators, so wrapped rows in a narrow
// window stay readable.
func TestTable_CappedWrappingDrawsRowSeparators(t *testing.T) {
	// Arrange
	withTerminalWidth(t, 30)
	var buf bytes.Buffer

	// Act
	Table(&buf, nil, [][]string{
		{"one", "a rather long cell that will certainly wrap at thirty columns"},
		{"two", "another long cell that will also wrap at thirty columns"},
	})

	// Assert
	out := buf.String()
	if !strings.Contains(out, "├") {
		t.Errorf("output = %q, want row separators once the width cap wraps cells", out)
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if got := lipgloss.Width(line); got > 30 {
			t.Errorf("line %q is %d columns wide, want <= 30", line, got)
		}
	}
}
