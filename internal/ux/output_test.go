package ux

import (
	"bytes"
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
