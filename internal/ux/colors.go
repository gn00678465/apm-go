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
//
// R8/P4-7 (design.md): all five of the original glyphs (✓ ℹ ✗ ▸ •) other
// than "!" are East-Asian Ambiguous width -- some terminal fonts render them
// two columns wide, which breaks printLine/newBulletList's fixed-width-3
// centered alignment (a width-1-vs-width-2 glyph can't be aligned by a
// single lipgloss.Width() column). The symbol set below replaces every
// Ambiguous glyph with an ASCII (Narrow, width-1-guaranteed) equivalent, so
// Width(3) centering is reliable across terminals; "!" was already Narrow
// and is unchanged.
const (
	SymbolSuccess  = "+"
	SymbolInfo     = "i"
	SymbolWarn     = "!"
	SymbolError    = "x"
	SymbolProgress = ">"
	SymbolList     = "*"
)
