package authoring

import (
	"strings"
	"testing"
)

func TestUnifiedDiff_NoChanges_ReturnsEmptyString(t *testing.T) {
	// Arrange
	text := "a: 1\nb: 2\n"

	// Act
	diff := unifiedDiff(text, text, "old", "new")

	// Assert
	if diff != "" {
		t.Errorf("diff = %q, want empty string for identical texts", diff)
	}
}

func TestUnifiedDiff_AppendedLines_ShowsAddedContextAndHeader(t *testing.T) {
	// Arrange
	old := "a: 1\nb: 2\n"
	newText := "a: 1\nb: 2\nc: 3\n"

	// Act
	diff := unifiedDiff(old, newText, "apm.yml (current)", "apm.yml (after migrate)")

	// Assert
	if !strings.HasPrefix(diff, "--- apm.yml (current)\n+++ apm.yml (after migrate)\n") {
		t.Errorf("diff = %q, want the standard --- / +++ header", diff)
	}
	if !strings.Contains(diff, "@@ ") {
		t.Errorf("diff = %q, want a unified-diff hunk header", diff)
	}
	if !strings.Contains(diff, "+c: 3") {
		t.Errorf("diff = %q, want the added line prefixed with '+'", diff)
	}
	if !strings.Contains(diff, " a: 1") || !strings.Contains(diff, " b: 2") {
		t.Errorf("diff = %q, want unchanged lines kept as ' ' context", diff)
	}
}

func TestUnifiedDiff_ReplacedSpan_ShowsRemovedAndAddedLines(t *testing.T) {
	// Arrange
	old := "top: 1\nmarketplace:\n  owner: old\n"
	newText := "top: 1\nmarketplace:\n  owner: new\n"

	// Act
	diff := unifiedDiff(old, newText, "old", "new")

	// Assert
	if !strings.Contains(diff, "-  owner: old") {
		t.Errorf("diff = %q, want the removed line prefixed with '-'", diff)
	}
	if !strings.Contains(diff, "+  owner: new") {
		t.Errorf("diff = %q, want the added line prefixed with '+'", diff)
	}
}

func TestUnifiedDiff_EmptyOldText_TreatsAllNewLinesAsAdded(t *testing.T) {
	// Arrange & Act
	diff := unifiedDiff("", "a: 1\n", "old", "new")

	// Assert
	if !strings.Contains(diff, "+a: 1") {
		t.Errorf("diff = %q, want the sole new line prefixed with '+'", diff)
	}
}
