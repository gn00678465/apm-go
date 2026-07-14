package ux

import (
	"fmt"
	"io"

	"github.com/pterm/pterm"
)

// Prefix printers for the four message severities. Each uses the shared
// symbol set (colors.go) instead of pterm's default "SUCCESS"/"INFO"/
// "WARNING"/"ERROR" badges.
var (
	successPrinter = pterm.PrefixPrinter{
		Prefix: pterm.Prefix{Text: SymbolSuccess, Style: pterm.NewStyle(pterm.FgGreen)},
	}
	infoPrinter = pterm.PrefixPrinter{
		Prefix: pterm.Prefix{Text: SymbolInfo, Style: pterm.NewStyle(pterm.FgCyan)},
	}
	warnPrinter = pterm.PrefixPrinter{
		Prefix: pterm.Prefix{Text: SymbolWarn, Style: pterm.NewStyle(pterm.FgYellow)},
	}
	errorPrinter = pterm.PrefixPrinter{
		Prefix: pterm.Prefix{Text: SymbolError, Style: pterm.NewStyle(pterm.FgRed)},
	}
)

// Success prints a "✓ ..." line to w.
func Success(w io.Writer, format string, a ...any) {
	fmt.Fprint(w, renderForWriter(w, successPrinter.Sprintfln(format, a...)))
}

// Info prints an "ℹ ..." line to w.
func Info(w io.Writer, format string, a ...any) {
	fmt.Fprint(w, renderForWriter(w, infoPrinter.Sprintfln(format, a...)))
}

// Warn prints a "! ..." line to w.
func Warn(w io.Writer, format string, a ...any) {
	fmt.Fprint(w, renderForWriter(w, warnPrinter.Sprintfln(format, a...)))
}

// Error prints a "✗ ..." line to w.
func Error(w io.Writer, format string, a ...any) {
	fmt.Fprint(w, renderForWriter(w, errorPrinter.Sprintfln(format, a...)))
}
