package ux

import (
	"bytes"
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
		{name: "Info", fn: func(buf *bytes.Buffer) { Info(buf, "info: %s", "ok") }, symbol: SymbolInfo},
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
// every message symbol renders centered in a fixed 3-rune column (padding
// survives ANSI stripping since it's plain whitespace, not color), and the
// message text starts immediately after that column with no additional
// space -- so multi-line output stays aligned and there's no double gap.
func TestPrintLine_SymbolFixedWidthThreeCentered(t *testing.T) {
	tests := []struct {
		name   string
		fn     func(buf *bytes.Buffer)
		symbol string
	}{
		{name: "Success", fn: func(buf *bytes.Buffer) { Success(buf, "msg") }, symbol: SymbolSuccess},
		{name: "Info", fn: func(buf *bytes.Buffer) { Info(buf, "msg") }, symbol: SymbolInfo},
		{name: "Warn", fn: func(buf *bytes.Buffer) { Warn(buf, "msg") }, symbol: SymbolWarn},
		{name: "Error", fn: func(buf *bytes.Buffer) { Error(buf, "msg") }, symbol: SymbolError},
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
