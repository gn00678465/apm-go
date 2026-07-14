package ux

import (
	"bytes"
	"strings"
	"testing"
)

func TestOutputGolden_NoANSIWhenStylingDisabled(t *testing.T) {
	withDisabledStyling(t)

	t.Run("Table", func(t *testing.T) {
		// Arrange
		var buf bytes.Buffer
		headers := []string{"NAME", "SOURCE"}
		rows := [][]string{{"foo", "git"}, {"bar", "local"}}

		// Act
		Table(&buf, headers, rows)
		out := buf.String()

		// Assert
		if ansiEscape.MatchString(out) {
			t.Fatalf("Table output contains ANSI escape codes: %q", out)
		}
		for _, want := range []string{"NAME", "SOURCE", "foo", "git", "bar", "local"} {
			if !strings.Contains(out, want) {
				t.Fatalf("Table output %q missing %q", out, want)
			}
		}
	})

	t.Run("BulletList", func(t *testing.T) {
		// Arrange
		var buf bytes.Buffer
		items := []Item{
			{Level: 0, Text: "top level"},
			{Level: 1, Text: "nested"},
		}

		// Act
		BulletList(&buf, items)
		out := buf.String()

		// Assert
		if ansiEscape.MatchString(out) {
			t.Fatalf("BulletList output contains ANSI escape codes: %q", out)
		}
		for _, want := range []string{"top level", "nested"} {
			if !strings.Contains(out, want) {
				t.Fatalf("BulletList output %q missing %q", out, want)
			}
		}
	})

	t.Run("Tree", func(t *testing.T) {
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
		if ansiEscape.MatchString(out) {
			t.Fatalf("Tree output contains ANSI escape codes: %q", out)
		}
		for _, want := range []string{"root", "child-a", "child-b", "grandchild"} {
			if !strings.Contains(out, want) {
				t.Fatalf("Tree output %q missing %q", out, want)
			}
		}
	})

	t.Run("Section", func(t *testing.T) {
		// Arrange
		var buf bytes.Buffer

		// Act
		Section(&buf, "Update plan for apm.yml")
		out := buf.String()

		// Assert
		if ansiEscape.MatchString(out) {
			t.Fatalf("Section output contains ANSI escape codes: %q", out)
		}
		if !strings.Contains(out, "Update plan for apm.yml") {
			t.Fatalf("Section output %q missing title", out)
		}
	})

	t.Run("Diff", func(t *testing.T) {
		// Arrange
		var buf bytes.Buffer
		diff := "--- apm.yml (current)\n+++ apm.yml (after migrate)\n@@ -1,2 +1,3 @@\n name: demo\n-version: 1.0.0\n+version: 1.0.0\n+marketplace:\n"

		// Act
		Diff(&buf, diff)
		out := buf.String()

		// Assert
		if ansiEscape.MatchString(out) {
			t.Fatalf("Diff output contains ANSI escape codes: %q", out)
		}
		for _, want := range []string{"name: demo", "-version: 1.0.0", "+version: 1.0.0", "+marketplace:"} {
			if !strings.Contains(out, want) {
				t.Fatalf("Diff output %q missing %q", out, want)
			}
		}
	})
}
