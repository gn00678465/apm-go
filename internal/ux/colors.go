// Package ux is the single facade for terminal output and interactive
// prompts across apm-go's cmd/apm-go commands. It wraps charm.land/huh/v2
// (interactive forms, spinner) and charm.land/lipgloss/v2 (styled output,
// tables, trees, lists) behind a consistent color/symbol theme, and degrades
// to plain text automatically when the terminal is not interactive (non-TTY,
// NO_COLOR, or CI).
package ux

// Color palette shared by huh forms and lipgloss styles.
const (
	ColorBrand   = "#2dd4bf"
	ColorHeading = "#8aa0ff"
	ColorSuccess = "#3fb950"
	ColorWarning = "#d29922"
	ColorError   = "#f85149"
	ColorMuted   = "#8b949e"
)

// Symbol prefixes used across stdout/stderr output. These replace the old
// mixed-prefix conventions ([+] [i] [!] [warn] [>] [*] [x] [dry-run] [-]).
const (
	SymbolSuccess  = "✓"
	SymbolInfo     = "ℹ"
	SymbolWarn     = "!"
	SymbolError    = "✗"
	SymbolProgress = "▸"
	SymbolList     = "•"
)
