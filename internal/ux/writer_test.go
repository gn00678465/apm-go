package ux

import (
	"bytes"
	"os"
	"testing"

	"github.com/pterm/pterm"
)

func TestIsTerminalWriter_BytesBufferIsNeverATerminal(t *testing.T) {
	// Arrange
	var buf bytes.Buffer

	// Act / Assert
	if isTerminalWriter(&buf) {
		t.Fatal("isTerminalWriter(bytes.Buffer) = true, want false")
	}
}

func TestIsTerminalWriter_NonTerminalFileIsNotATerminal(t *testing.T) {
	// Arrange: os.DevNull is a real *os.File but never a terminal device,
	// unlike a bare os.ModeCharDevice check would report.
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("failed to open %s: %v", os.DevNull, err)
	}
	defer f.Close()

	// Act / Assert
	if isTerminalWriter(f) {
		t.Fatalf("isTerminalWriter(%s) = true, want false", os.DevNull)
	}
}

// TestPerWriterStyle_NonTerminalWriterStaysPlainEvenWhenGloballyRich guards
// against the bug where a single process-wide "rich" decision (captured once
// from stdin at Init time) leaked ANSI styling into any writer, including a
// redirected/non-terminal one. Styling must be decided per writer, per call
// - and, unlike an earlier revision, without ever flipping pterm's global
// styling flag (pterm.RawOutput) to do it: that flag is read, without a
// lock, by a spinner's background render goroutine (see spinner.go), so
// toggling it per call raced under `go test -race` whenever a spinner was
// active concurrently. Success() now renders through pterm once (reflecting
// whatever Init() decided for the whole process) and strips the result's
// ANSI escape codes per writer instead.
func TestPerWriterStyle_NonTerminalWriterStaysPlainEvenWhenGloballyRich(t *testing.T) {
	// Arrange: simulate a prior rich decision that left pterm's global
	// styling flag enabled (e.g. Init() ran while stdin/stderr were TTYs).
	wasRaw := pterm.RawOutput
	pterm.EnableStyling()
	t.Cleanup(func() {
		if wasRaw {
			pterm.DisableStyling()
		} else {
			pterm.EnableStyling()
		}
	})

	var buf bytes.Buffer

	// Act: write to a plain in-memory buffer, which stands in for a
	// redirected file and is never a real terminal regardless of the
	// global styling flag's state.
	Success(&buf, "%s done", "task")
	out := buf.String()

	// Assert
	if ansiEscape.MatchString(out) {
		t.Fatalf("Success() leaked ANSI into a non-terminal writer while pterm styling was globally enabled: %q", out)
	}

	// Assert: Success() must not have touched the global styling flag at
	// all (it should still read exactly as this test set it up).
	if pterm.RawOutput {
		t.Fatal("Success() changed pterm.RawOutput; output functions must render per-writer without mutating pterm's global styling flag")
	}
}
