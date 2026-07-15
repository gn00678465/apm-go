package ux

import (
	"fmt"
	"io"

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

// Success prints a "✓ ..." line to w.
func Success(w io.Writer, format string, a ...any) {
	printLine(w, successStyle, SymbolSuccess, format, a...)
}

// Info prints an "ℹ ..." line to w.
func Info(w io.Writer, format string, a ...any) {
	printLine(w, infoStyle, SymbolInfo, format, a...)
}

// Warn prints a "! ..." line to w.
func Warn(w io.Writer, format string, a ...any) {
	printLine(w, warnStyle, SymbolWarn, format, a...)
}

// Error prints a "✗ ..." line to w.
func Error(w io.Writer, format string, a ...any) {
	printLine(w, errorStyle, SymbolError, format, a...)
}

// printLine renders "<symbol> <message>" with symbol in style, then writes
// it to w via lipgloss.Fprintln, which downsamples/strips colors per-writer
// (see writer.go's use of colorprofile.NewWriter) -- no renderForWriter or
// global styling flag needed.
func printLine(w io.Writer, style lipgloss.Style, symbol, format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	line := style.Render(symbol) + " " + msg
	lipgloss.Fprintln(w, line)
}
