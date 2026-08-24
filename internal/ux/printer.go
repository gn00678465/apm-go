package ux

import (
	"fmt"
	"io"
	"os"
	"strings"

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

// oracleErrorPrefix, oracleWarnPrefix, oracleInfoPrefix, and
// oracleRunningPrefix mirror the Oracle's bracketed STATUS_SYMBOLS glyphs
// (utils/console.py:37-61: "[x]" error, "[!]" warning, "[i]" info, "[>]"
// running/search) verbatim, deliberately diverging from colors.go's bare
// width-3-centered SymbolError/SymbolWarn/SymbolInfo ("x"/"!"/"i") that
// Success alone still uses -- ticket 10's decision (A) (see
// .scratch/parity-runner/issues/10-error-output-contract.md) aligns apm-go's
// error/warning/info/running channel and prefix to the Oracle's observable
// contract; Success was never found to differ, so it keeps the existing
// centered-symbol convention untouched. oracleRunningPrefix backs the new
// Running printer (attempt 3): CommandLogger.start(symbol="search")
// (core/command_logger.py:81-83) and the "running"/"search" STATUS_SYMBOLS
// entries both resolve to "[>]".
const (
	oracleErrorPrefix   = "[x] "
	oracleWarnPrefix    = "[!] "
	oracleInfoPrefix    = "[i] "
	oracleRunningPrefix = "[>] "
	oracleSparklePrefix = "[*] "
)

// Success prints a "+ ..." line to w.
func Success(w io.Writer, format string, a ...any) {
	printLine(w, successStyle, SymbolSuccess, format, a...)
}

// Sparkle prints a "[*] ..." line to w, redirected to stdout if w is the
// process's stderr stream (see errWriter) -- ticket 13: CommandLogger.
// success's OWN default symbol is "sparkles" (core/command_logger.py:120,
// `def success(self, message, symbol="sparkles")`), which STATUS_SYMBOLS
// maps to "[*]" (utils/console.py:37-61) -- NOT the "[+]" a reader might
// expect from Success's name. `apm pack`'s own "Packed N file(s) -> ..."
// and "Built marketplace.json [...] -> ..." lines both call
// logger.success(...) with no symbol override, so they render "[*]",
// verified directly against the pinned Oracle. A different call site can
// still override to symbol="check" ("[+]", e.g. `marketplace validate`'s
// per-check "passed" lines, rendered directly rather than through a shared
// helper) -- Sparkle is deliberately scoped to the "sparkles"/default
// case, not a blanket replacement for every existing Success call site
// (most of which have not been individually verified against their own
// Oracle counterpart's symbol; see this function's ticket for the survey
// that would be needed before widening this further).
func Sparkle(w io.Writer, format string, a ...any) {
	oracleLine(w, successStyle, oracleSparklePrefix, format, a...)
}

// Info prints a "[i] ..." line to w, redirected to stdout if w is the
// process's stderr stream (see errWriter) -- the Oracle's info channel
// (CommandLogger.info/.progress, both _rich_info under the hood, always land
// on the same Console as Warn/Error; attempt 3 closes the gap where Info
// previously stayed on the centered-symbol convention regardless of channel).
func Info(w io.Writer, format string, a ...any) {
	oracleLine(w, infoStyle, oracleInfoPrefix, format, a...)
}

// Running prints a "[>] ..." line to w, redirected to stdout if w is the
// process's stderr stream (see errWriter) -- CommandLogger.start's default
// symbol ("running", and "search" specifically for `apm search`'s progress
// line; core/command_logger.py:81-83), which STATUS_SYMBOLS maps to the same
// "[>]" glyph and _rich_info routes to the same channel as Info.
func Running(w io.Writer, format string, a ...any) {
	oracleLine(w, infoStyle, oracleRunningPrefix, format, a...)
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
// convention) to errWriter(w), word-wrapped to the Oracle's actual console
// width (wrap.go's oracleConsoleWidth, which honors COLUMNS the same way
// the pinned Oracle does) once prefix+message exceeds it -- verified
// directly against the pinned Oracle (ticket 14): a long Info/Error/Warn
// line reflows exactly the way Rich's Console.print would, not as one
// unwrapped line.
func oracleLine(w io.Writer, style lipgloss.Style, prefix, format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	wrapped := wrapOracleText(prefix+msg, oracleConsoleWidth())
	body := strings.TrimPrefix(wrapped, prefix)
	line := style.Bold(true).Render(prefix) + body
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
