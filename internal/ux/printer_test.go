package ux

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// TestPrinters_Golden_NonTTYWriterHasNoANSI proves per-writer color
// downsampling: Success/Info/Warn/Error write to a bytes.Buffer (never a
// terminal), so lipgloss.Fprintln must strip all ANSI escapes regardless of
// the process-wide styleEnabled/richMode decision -- no renderForWriter or
// global styling flag involved.
func TestPrinters_Golden_NonTTYWriterHasNoANSI(t *testing.T) {
	tests := []struct {
		name   string
		fn     func(buf *bytes.Buffer)
		symbol string
	}{
		{name: "Success", fn: func(buf *bytes.Buffer) { Success(buf, "done: %s", "ok") }, symbol: SymbolSuccess},
		{name: "Info", fn: func(buf *bytes.Buffer) { Info(buf, "info: %s", "ok") }, symbol: "i"},
		{name: "Running", fn: func(buf *bytes.Buffer) { Running(buf, "running: %s", "ok") }, symbol: ">"},
		{name: "Warn", fn: func(buf *bytes.Buffer) { Warn(buf, "warn: %s", "ok") }, symbol: SymbolWarn},
		{name: "Error", fn: func(buf *bytes.Buffer) { Error(buf, "error: %s", "ok") }, symbol: SymbolError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			var buf bytes.Buffer

			// Act
			tt.fn(&buf)
			out := buf.String()

			// Assert
			if strings.Contains(out, "\x1b[") {
				t.Fatalf("%s output contains ANSI escape: %q", tt.name, out)
			}
			if !strings.Contains(out, tt.symbol) {
				t.Fatalf("%s output missing symbol %q: %q", tt.name, tt.symbol, out)
			}
			if !strings.Contains(out, "ok") {
				t.Fatalf("%s output missing formatted message: %q", tt.name, out)
			}
		})
	}
}

// TestPrintLine_SymbolFixedWidthThreeCentered is the R8/P4-5/P4-6 regression:
// Success's message symbol renders centered in a fixed 3-rune column
// (padding survives ANSI stripping since it's plain whitespace, not color),
// and the message text starts immediately after that column with no
// additional space -- so multi-line output stays aligned and there's no
// double gap. Info/Warn/Error deliberately left this shared convention under
// ticket 10's decision (A): they render the Oracle's literal "[i] "/"[!]
// "/"[x] " bracket prefix instead (see TestOracleLine_BracketPrefixNoExtraSpace).
func TestPrintLine_SymbolFixedWidthThreeCentered(t *testing.T) {
	tests := []struct {
		name   string
		fn     func(buf *bytes.Buffer)
		symbol string
	}{
		{name: "Success", fn: func(buf *bytes.Buffer) { Success(buf, "msg") }, symbol: SymbolSuccess},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tt.fn(&buf)
			out := strings.TrimSuffix(buf.String(), "\n")

			runes := []rune(out)
			if len(runes) < 4 {
				t.Fatalf("%s output too short to contain a 3-rune symbol column: %q", tt.name, out)
			}
			symbolColumn := string(runes[:3])
			wantColumn := " " + tt.symbol + " "
			if symbolColumn != wantColumn {
				t.Errorf("%s symbol column = %q, want %q (3-rune centered)", tt.name, symbolColumn, wantColumn)
			}
			rest := string(runes[3:])
			if rest != "msg" {
				t.Errorf("%s message = %q, want %q (no extra space after the symbol column)", tt.name, rest, "msg")
			}
		})
	}
}

// TestOracleLine_BracketPrefixNoExtraSpace pins Info/Running/Warn/Error's
// Oracle-mirrored format (ticket 10 decisions A and attempt-3's Info/Running
// extension): a literal "[i] "/"[>] "/"[!] "/"[x] " prefix immediately
// followed by the message, no centering/padding.
func TestOracleLine_BracketPrefixNoExtraSpace(t *testing.T) {
	tests := []struct {
		name   string
		fn     func(buf *bytes.Buffer)
		prefix string
	}{
		{name: "Info", fn: func(buf *bytes.Buffer) { Info(buf, "msg") }, prefix: oracleInfoPrefix},
		{name: "Running", fn: func(buf *bytes.Buffer) { Running(buf, "msg") }, prefix: oracleRunningPrefix},
		{name: "Warn", fn: func(buf *bytes.Buffer) { Warn(buf, "msg") }, prefix: oracleWarnPrefix},
		{name: "Error", fn: func(buf *bytes.Buffer) { Error(buf, "msg") }, prefix: oracleErrorPrefix},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tt.fn(&buf)
			out := strings.TrimSuffix(buf.String(), "\n")
			if want := tt.prefix + "msg"; out != want {
				t.Errorf("%s output = %q, want %q", tt.name, out, want)
			}
		})
	}
}

// TestErrWriter_RedirectsProcessStderrToStdout pins ticket 10's channel
// switch: a writer that is literally os.Stderr is redirected to os.Stdout;
// any other writer (a test's bytes.Buffer, cmd.OutOrStdout(), ...) passes
// through unchanged.
func TestErrWriter_RedirectsProcessStderrToStdout(t *testing.T) {
	if got := errWriter(os.Stderr); got != io.Writer(os.Stdout) {
		t.Errorf("errWriter(os.Stderr) = %v, want os.Stdout", got)
	}

	var buf bytes.Buffer
	if got := errWriter(&buf); got != io.Writer(&buf) {
		t.Errorf("errWriter(&buf) = %v, want &buf unchanged", got)
	}
}

// Plain is the symbol-free line printer for callers whose status glyph is
// part of the message itself (doctor's upstream "[+] name: detail" rows,
// Finding 9). It still goes through the per-writer colour policy.
func TestPlain_NoSymbol_NoANSI_Newline(t *testing.T) {
	var buf bytes.Buffer
	Plain(&buf, "  [%s] %s: %s", "+", "git", "ok")
	got := buf.String()
	if got != "  [+] git: ok\n" {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("ANSI leaked into non-TTY writer: %q", got)
	}
}
