package ux

import (
	"fmt"
	"io"
	"os"

	"charm.land/lipgloss/v2"
)

// Status styles for the message severities. Each uses the shared narrow ASCII
// symbol set from colors.go.
var (
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSuccess))
	infoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBrand))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorWarning))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorError))
)

// Success prints a centered width-3 success record to w.
func Success(w io.Writer, format string, a ...any) {
	printLine(w, successStyle, SymbolSuccess, format, a...)
}

// Progress prints a centered width-3 progress record to w.
func Progress(w io.Writer, format string, a ...any) {
	printLine(w, infoStyle, SymbolProgress, format, a...)
}

// Hint prints a centered width-3 informational record to w.
func Hint(w io.Writer, format string, a ...any) {
	printLine(w, infoStyle, SymbolInfo, format, a...)
}

// Sparkle prints a success record. It remains a semantic alias for callers
// whose source operation is a completion event.
func Sparkle(w io.Writer, format string, a ...any) {
	printLine(w, successStyle, SymbolSuccess, format, a...)
}

// Gear prints a success record for a preparation/configuration step.
func Gear(w io.Writer, format string, a ...any) {
	printLine(w, successStyle, SymbolSuccess, format, a...)
}

// Check prints a success record for a completed check.
func Check(w io.Writer, format string, a ...any) {
	printLine(w, successStyle, SymbolSuccess, format, a...)
}

// Info prints a centered width-3 informational record to w.
func Info(w io.Writer, format string, a ...any) {
	printLine(w, infoStyle, SymbolInfo, format, a...)
}

// Running prints a centered width-3 progress record to w.
func Running(w io.Writer, format string, a ...any) {
	printLine(w, infoStyle, SymbolProgress, format, a...)
}

// Warn prints a centered width-3 warning record to w.
func Warn(w io.Writer, format string, a ...any) {
	printLine(w, warnStyle, SymbolWarn, format, a...)
}

// Error prints a centered width-3 error record to w.
func Error(w io.Writer, format string, a ...any) {
	printLine(w, errorStyle, SymbolError, format, a...)
}

// errWriter is the single point where apm-go's error/warning channel is
// decided (ticket 10, decision (A)). Ordinary status records written to the
// process stderr stream are redirected to stdout; a non-process writer, such
// as a test buffer or command-owned output, passes through unchanged.
func errWriter(w io.Writer) io.Writer {
	if w == io.Writer(os.Stderr) && !consoleStderr {
		return os.Stdout
	}
	return w
}

// consoleStderr is enabled by pack's JSON mode so logs stay off stdout.
var consoleStderr bool

// SetConsoleStderr moves status records passed the process stderr writer to
// stderr, which keeps pack's JSON stdout machine-readable.
func SetConsoleStderr(on bool) { consoleStderr = on }

// Plain prints a line with no severity symbol, for callers whose content is
// already a complete table or message row. Routed through lipgloss.Fprintln
// like every other printer so the per-writer colour/TTY policy still applies.
func Plain(w io.Writer, format string, a ...any) {
	lipgloss.Fprintln(w, fmt.Sprintf(format, a...))
}

// printLine renders "<symbol><message>" with the symbol centered in a
// fixed-width-3 column (R8: all message symbols share one visual width, so
// multi-line output stays aligned; the centered padding already supplies the
// gap before msg -- no separate " " is added), then writes it via
// lipgloss.Fprintln after applying the channel policy. The writer-specific
// color profile strips colors for pipes and buffers.
func printLine(w io.Writer, style lipgloss.Style, symbol, format string, a ...any) {
	lipgloss.Fprintln(errWriter(w), symbolLine(style, symbol, format, a...))
}

// symbolLine is printLine's rendering step: "<symbol><message>" with the
// symbol styled and centered in a width-3 column. Exposed through
// ProgressText/HintText for callers that embed a status record in another
// surface (the clack transcript) instead of printing it to a stream --
// rendering to a string keeps the styling, which a non-TTY buffer written
// through lipgloss.Fprintln would strip.
func symbolLine(style lipgloss.Style, symbol, format string, a ...any) string {
	msg := fmt.Sprintf(format, a...)
	symStyle := style.Bold(true).AlignHorizontal(lipgloss.Center).Width(3)
	return symStyle.Render(symbol) + msg
}

// ProgressText returns Progress's line as a styled string.
func ProgressText(format string, a ...any) string {
	return symbolLine(infoStyle, SymbolProgress, format, a...)
}

// HintText returns Hint's line as a styled string.
func HintText(format string, a ...any) string {
	return symbolLine(infoStyle, SymbolInfo, format, a...)
}
