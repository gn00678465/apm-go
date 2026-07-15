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

// TestOutputFunctions_PinPtermGlobalStyling characterizes -- and thereby
// makes reviewable -- a KNOWN LIMITATION of the pterm-native design, not a
// desired behavior: pterm has no per-writer styling of its own (every
// Sprint*/Print* method renders against the single process-wide
// pterm.RawOutput flag regardless of which writer the result is written to --
// confirmed by reading pterm's PrefixPrinter.Sprint/Print source), so
// Success/Info/Warn/Error render through pterm's native WithWriter(w).Printfln
// directly, with no per-writer strip. Consequence: if the global flag is on
// and a caller passes a NON-terminal writer, ANSI leaks into it.
//
// Why this is safe in apm-go (see terminal-ux-contract.md "Known limitation"):
//  1. Init() only turns the global flag on when *both* os.Stdout and os.Stderr
//     are real terminals; and
//  2. every ux caller passes exactly those streams (os.Stdout / os.Stderr /
//     cmd.OutOrStdout() / cmd.ErrOrStderr()), never a divergent file/buffer.
//
// So the flag is only ever on for writers that ARE terminals. This test forces
// the flag on and writes to a buffer purely to pin pterm's mechanical
// behavior, so that anyone who later assumes per-writer stripping still
// happens (it does not) fails here first and reads the contract limitation.
func TestOutputFunctions_PinPtermGlobalStyling(t *testing.T) {
	// Arrange: force pterm's global flag on (as Init() would when both
	// std streams are terminals). This is an artificial state -- see the
	// doc comment: a non-terminal buffer never sees this flag on in real use.
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
	Success(&buf, "%s done", "task")

	// Assert pterm's documented global behavior (NOT an endorsement of the
	// leak): with the flag on, ANSI is emitted regardless of the writer,
	// because there is no per-writer strip. apm-go relies on Init()+contract
	// (above) to keep this state unreachable for non-terminal writers.
	if !ansiEscape.MatchString(buf.String()) {
		t.Fatalf("Success() did not follow pterm's global styling flag: %q", buf.String())
	}
}
