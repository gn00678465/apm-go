package ux

import (
	"strings"
	"testing"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// renderWithTheme builds a grouped form the way inputFormWith does and returns
// its rendered view with styling stripped, so the gutter can be inspected
// column by column.
func renderWithTheme(t *testing.T, theme huh.Theme, title string, labels ...string) string {
	t.Helper()

	fields := make([]huh.Field, len(labels))
	for i, label := range labels {
		val := "value"
		fields[i] = huh.NewInput().Title(label).Value(&val).WithTheme(theme)
	}
	group := huh.NewGroup(fields...)
	if title != "" {
		group = group.Title(title)
	}
	form := huh.NewForm(group).WithTheme(theme).WithShowHelp(false)
	form.Init()
	return ansiPattern.ReplaceAllString(form.View(), "")
}

// TestClackTheme_EveryRenderedLineSitsOnTheGutter is the regression for the
// defect the first cut of this theme shipped: a grouped form drew three
// different left edges -- huh's thick "┃" on the focused field, nothing at all
// on the blurred ones (this package hides their border), and no border on the
// group title -- so the whole form floated off the transcript's connecting
// line.
//
// The spacing assertion pins the FieldSeparator choice: fields are separated
// by exactly one gutter-only line, which comes from each field's
// PaddingBottom. Moving the bar into the separator string instead stacks the
// two and renders a doubled blank line between every pair of fields (verified
// by rendering that variant, not assumed).
func TestClackTheme_EveryRenderedLineSitsOnTheGutter(t *testing.T) {
	// Arrange
	bar := unicodeClackSymbols.Bar

	// Act
	view := renderWithTheme(t, clackTheme(unicodeClackSymbols), "Project metadata", "Project name", "Version")

	// Assert
	var checked int
	var prevWasBlankGutter bool
	for _, line := range strings.Split(view, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, bar) {
			t.Fatalf("line %q does not start on the gutter %q; full view:\n%s", line, bar, view)
		}
		checked++

		blankGutter := strings.TrimSpace(strings.TrimPrefix(line, bar)) == ""
		if blankGutter && prevWasBlankGutter {
			t.Fatalf("two consecutive gutter-only lines; the gap between fields should be a single line:\n%s", view)
		}
		prevWasBlankGutter = blankGutter
	}
	if checked < 5 {
		t.Fatalf("only %d content lines rendered, expected the title plus two labelled fields:\n%s", checked, view)
	}
}

// TestClackTheme_UsesTheASCIIBarWhenUnicodeIsUnavailable keeps the prompt in
// step with the transcript on a terminal that falls back to ASCII: a Unicode
// gutter under an ASCII transcript would be two different lines.
func TestClackTheme_UsesTheASCIIBarWhenUnicodeIsUnavailable(t *testing.T) {
	// Act
	view := renderWithTheme(t, clackTheme(asciiClackSymbols), "Project metadata", "Project name")

	// Assert
	if strings.Contains(view, unicodeClackSymbols.Bar) {
		t.Fatalf("ASCII theme rendered the Unicode bar:\n%s", view)
	}
	if !strings.Contains(view, asciiClackSymbols.Bar+"  Project name") {
		t.Fatalf("ASCII theme did not render the fallback gutter:\n%s", view)
	}
}

// TestTheme_KeepsHuhsOwnChrome guards the boundary the clack restyle was
// deliberately kept inside: the shared theme backs the credential prompts in
// cmd/apm-go/mcp_prompt.go, which are not part of any transcript, so it must
// keep huh's default thick border rather than the connecting line.
func TestTheme_KeepsHuhsOwnChrome(t *testing.T) {
	// Act
	view := renderWithTheme(t, Theme(), "", "Token")

	// Assert
	if strings.Contains(view, unicodeClackSymbols.Bar) {
		t.Fatalf("the shared theme leaked the clack gutter into a plain prompt:\n%s", view)
	}
	thick := lipgloss.ThickBorder().Left
	if !strings.Contains(view, thick) {
		t.Fatalf("the shared theme lost huh's own %q border:\n%s", thick, view)
	}
}
