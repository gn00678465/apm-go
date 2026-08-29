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
		{name: "Success", fn: func(buf *bytes.Buffer) { Success(buf, "done: %s", "ok") }, symbol: " + "},
		{name: "Info", fn: func(buf *bytes.Buffer) { Info(buf, "info: %s", "ok") }, symbol: " i "},
		{name: "Running", fn: func(buf *bytes.Buffer) { Running(buf, "running: %s", "ok") }, symbol: " > "},
		{name: "Warn", fn: func(buf *bytes.Buffer) { Warn(buf, "warn: %s", "ok") }, symbol: " ! "},
		{name: "Error", fn: func(buf *bytes.Buffer) { Error(buf, "error: %s", "ok") }, symbol: " x "},
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

// TestSuccess_UsesCenteredTUIPrefix pins the stream-facing success contract.
func TestSuccess_UsesCenteredTUIPrefix(t *testing.T) {
	var buf bytes.Buffer
	Success(&buf, "msg")
	if got, want := strings.TrimSuffix(buf.String(), "\n"), " + msg"; got != want {
		t.Errorf("Success output = %q, want %q", got, want)
	}
}

// TestPrintLine_CenteredTUISymbols pins the shared width-3 format used by all
// stream status records.
func TestPrintLine_CenteredTUISymbols(t *testing.T) {
	tests := []struct {
		name string
		fn   func(buf *bytes.Buffer)
		want string
	}{
		{name: "Info", fn: func(buf *bytes.Buffer) { Info(buf, "msg") }, want: " i msg"},
		{name: "Running", fn: func(buf *bytes.Buffer) { Running(buf, "msg") }, want: " > msg"},
		{name: "Warn", fn: func(buf *bytes.Buffer) { Warn(buf, "msg") }, want: " ! msg"},
		{name: "Error", fn: func(buf *bytes.Buffer) { Error(buf, "msg") }, want: " x msg"},
		{name: "Sparkle", fn: func(buf *bytes.Buffer) { Sparkle(buf, "msg") }, want: " + msg"},
		{name: "Gear", fn: func(buf *bytes.Buffer) { Gear(buf, "msg") }, want: " + msg"},
		{name: "Check", fn: func(buf *bytes.Buffer) { Check(buf, "msg") }, want: " + msg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tt.fn(&buf)
			out := strings.TrimSuffix(buf.String(), "\n")
			if out != tt.want {
				t.Errorf("%s output = %q, want %q", tt.name, out, tt.want)
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

// Plain is the symbol-free line printer for callers whose content is already
// a complete row. It still goes through the per-writer colour policy.
func TestPlain_NoSymbol_NoANSI_Newline(t *testing.T) {
	var buf bytes.Buffer
	Plain(&buf, "  %s %s: %s", SymbolSuccess, "git", "ok")
	got := buf.String()
	if got != "  + git: ok\n" {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("ANSI leaked into non-TTY writer: %q", got)
	}
}
