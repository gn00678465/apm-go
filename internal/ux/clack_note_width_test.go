package ux

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// A Note wider than the terminal soft-wraps every row and breaks the whole
// transcript (user report 2026-08-29). The box must instead cap its inner
// width to the terminal and word-wrap overlong lines.
func TestClackNote_CapsWidthToTerminalAndWraps(t *testing.T) {
	const cols = 80
	orig := terminalWidthFor
	terminalWidthFor = func(io.Writer) int { return cols }
	t.Cleanup(func() { terminalWidthFor = orig })

	var buf bytes.Buffer
	ck := NewClack(&buf)
	long := "Tip: Use agentrc to generate tailored agent instructions from your codebase. https://github.com/microsoft/agentrc"
	ck.Note("Initializing", []string{"", "short line", long, ""})

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) < 6 {
		t.Fatalf("expected the long line to wrap into extra rows, got %d lines:\n%s", len(lines), buf.String())
	}
	widths := map[int]bool{}
	for i, line := range lines {
		w := lipgloss.Width(line)
		if w >= cols {
			t.Errorf("line %d is %d columns wide, must stay below the %d-column terminal: %q", i+1, w, cols, line)
		}
		if i > 0 && i < len(lines)-1 {
			widths[w] = true
		}
	}
	if len(widths) != 1 {
		t.Errorf("body rows are not all the same width (right border misaligned): %v\n%s", widths, buf.String())
	}
	if !strings.Contains(buf.String(), "agentrc") || !strings.Contains(buf.String(), "https://github.com/microsoft/agentrc") {
		t.Errorf("wrapped body lost content:\n%s", buf.String())
	}
}

// Without a terminal (pipe/buffer) the natural content width is kept.
func TestClackNote_UnconstrainedWithoutTerminal(t *testing.T) {
	orig := terminalWidthFor
	terminalWidthFor = func(io.Writer) int { return 0 }
	t.Cleanup(func() { terminalWidthFor = orig })

	var buf bytes.Buffer
	ck := NewClack(&buf)
	long := strings.Repeat("x", 150)
	ck.Note("T", []string{long})
	if !strings.Contains(buf.String(), long) {
		t.Fatalf("unconstrained Note wrapped its body:\n%s", buf.String())
	}
}
