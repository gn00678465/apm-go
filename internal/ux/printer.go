package ux

import (
	"fmt"
	"io"
	"os"

	"charm.land/lipgloss/v2"
)

// Prefix styles for the four message severities. Each uses the shared
// symbol set (colors.go) instead of a "SUCCESS"/"INFO"/"WARNING"/"ERROR"
// badge.
var (
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSuccess))
	infoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBrand))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorWarning))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorError))
)

// oracleErrorPrefix and oracleWarnPrefix mirror the Oracle's bracketed
// STATUS_SYMBOLS glyphs (utils/console.py:37-61: "[x]" error, "[!]"
// warning) verbatim, deliberately diverging from colors.go's bare
// width-3-centered SymbolError/SymbolWarn ("x"/"!") that every other
// severity still uses -- ticket 10's decision (A) (see
// .scratch/parity-runner/issues/10-error-output-contract.md) aligns
// apm-go's error/warning channel and prefix to the Oracle's observable
// contract; Success/Info were never found to differ, so they keep the
// existing centered-symbol convention untouched.
const (
	oracleErrorPrefix = "[x] "
	oracleWarnPrefix  = "[!] "
)

// Success prints a "+ ..." line to w.
func Success(w io.Writer, format string, a ...any) {
	printLine(w, successStyle, SymbolSuccess, format, a...)
}

// Info prints an "i ..." line to w.
func Info(w io.Writer, format string, a ...any) {
	printLine(w, infoStyle, SymbolInfo, format, a...)
}

// Warn prints a "[!] ..." line to w, redirected to stdout if w is the
// process's stderr stream (see errWriter) -- the Oracle's warning channel.
func Warn(w io.Writer, format string, a ...any) {
	oracleLine(w, warnStyle, oracleWarnPrefix, format, a...)
}

// Error prints a "[x] ..." line to w, redirected to stdout if w is the
// process's stderr stream (see errWriter) -- the Oracle's error channel.
func Error(w io.Writer, format string, a ...any) {
	oracleLine(w, errorStyle, oracleErrorPrefix, format, a...)
}

// oracleLine renders "<prefix><message>" (prefix colored, message plain --
// matching printLine's existing "color the symbol, not the message"
// convention) to errWriter(w).
func oracleLine(w io.Writer, style lipgloss.Style, prefix, format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	line := style.Bold(true).Render(prefix) + msg
	lipgloss.Fprintln(errWriter(w), line)
}

// errWriter is the single point where apm-go's error/warning channel is
// decided (ticket 10, decision (A)): the Oracle's _get_console()
// (commands/_helpers.py:72-93) builds a Console(stderr=_console_stderr)
// whose module-level _console_stderr defaults false and is only ever
// flipped by set_console_stderr(true), reserved for a --json mode apm-go
// doesn't have yet -- so ordinary errors/warnings always land on the
// Oracle's stdout. Every existing Warn/Error call site keeps passing
// os.Stderr/cmd.ErrOrStderr() unchanged; this is the one place that
// redirects it to os.Stdout instead. A writer that isn't literally the
// process's os.Stderr (a test's bytes.Buffer, cmd.OutOrStdout(), ...)
// passes through untouched.
func errWriter(w io.Writer) io.Writer {
	if w == io.Writer(os.Stderr) {
		return os.Stdout
	}
	return w
}

// Plain prints a line with no severity symbol, for callers whose status
// glyph is part of the message itself (e.g. doctor's upstream
// "  [+] name: detail" rows). Routed through lipgloss.Fprintln like every
// other printer so the per-writer colour/TTY policy still applies.
func Plain(w io.Writer, format string, a ...any) {
	lipgloss.Fprintln(w, fmt.Sprintf(format, a...))
}

// printLine renders "<symbol><message>" with the symbol centered in a
// fixed-width-3 column (R8: all message symbols share one visual width, so
// multi-line output stays aligned; the centered padding already supplies the
// gap before msg -- no separate " " is added), then writes it to w via
// lipgloss.Fprintln, which downsamples/strips colors per-writer (see
// writer.go's use of colorprofile.NewWriter) -- no renderForWriter or global
// styling flag needed.
func printLine(w io.Writer, style lipgloss.Style, symbol, format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	symStyle := style.Bold(true).AlignHorizontal(lipgloss.Center).Width(3)
	line := symStyle.Render(symbol) + msg
	lipgloss.Fprintln(w, line)
}
