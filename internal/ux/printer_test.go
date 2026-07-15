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
