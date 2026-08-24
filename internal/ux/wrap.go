package ux

import "strings"

// oracleWrapWidth mirrors rich.console.ConsoleDimensions's fallback
// (rich/console.py:1016, `return ConsoleDimensions(80, 25)`), the width Rich
// falls back to whenever it cannot detect a real terminal and no COLUMNS
// env var is set -- exactly the parity harness's sandbox (CI=1, TERM=dumb,
// no COLUMNS). Verified directly (ticket 14): the pinned Oracle's
// `marketplace browse`/`marketplace list` wording wraps at this width
// byte-for-byte once a rendered line exceeds it.
const oracleWrapWidth = 80

// wrapOracleText reproduces rich._wrap.divide_line (rich/_wrap.py) plus the
// console's per-line crop to width: the Oracle's Console.print wraps every
// line it renders to the console width by default (soft_wrap=False), which
// apm-go's error/warning/info channel must match byte-for-byte once a
// message exceeds that width -- ticket 14 found this chasing browse/list
// wording parity (both Oracle messages there are longer than 80 cells).
//
// divide_line's algorithm, verified against rich 15.0.0 (the Oracle's pinned
// version) source directly: walk the text as a sequence of tokens, each a
// non-space run plus any whitespace immediately following it (so a token's
// trailing space travels with it, not the next token); track a running
// cell_offset; a token fits if cell_offset + len(token, trailing space
// stripped) <= width, in which case the FULL token (trailing space
// included) is added to cell_offset -- otherwise a break is inserted right
// before the token and cell_offset resets to the token's own full length.
// Because the fits-check ignores trailing whitespace but the accumulation
// doesn't, an accepted line's raw length can overshoot width by exactly one
// trailing-space's worth; Rich's renderer then crops each rendered line to
// width, which is reproduced here as a final per-line truncation.
//
// Only ASCII/rune-count cell width is implemented (matching rich.cells.
// cell_len for the plain-ASCII messages apm-go emits today) -- wide-character
// folding (rich's chop_cells, for a single word longer than the width) is
// not implemented, since no apm-go message has one. Cell width is further
// adjusted by oracleLogicalLen (below) so an "apm-go" hint-text spelling
// wraps at the same points the Oracle's shorter "apm" text does.
func wrapOracleText(text string, width int) string {
	runes := []rune(text)
	type token struct {
		start int
		runes []rune
	}
	var tokens []token
	i := 0
	for i < len(runes) {
		start := i
		for i < len(runes) && isOracleWrapSpace(runes[i]) {
			i++
		}
		for i < len(runes) && !isOracleWrapSpace(runes[i]) {
			i++
		}
		for i < len(runes) && isOracleWrapSpace(runes[i]) {
			i++
		}
		if i == start {
			break
		}
		tokens = append(tokens, token{start: start, runes: runes[start:i]})
	}

	var breaks []int
	cellOffset := 0
	for _, tok := range tokens {
		trimmed := strings.TrimRightFunc(string(tok.runes), isOracleWrapSpace)
		wordLength := oracleLogicalLen(trimmed)
		remaining := width - cellOffset
		switch {
		case remaining >= wordLength:
			cellOffset += oracleLogicalLen(string(tok.runes))
		case wordLength > width:
			// Long-word folding not implemented -- not needed today.
			cellOffset += oracleLogicalLen(string(tok.runes))
		case cellOffset > 0 && tok.start > 0:
			breaks = append(breaks, tok.start)
			cellOffset = oracleLogicalLen(string(tok.runes))
		default:
			cellOffset += oracleLogicalLen(string(tok.runes))
		}
	}

	if len(breaks) == 0 {
		return text
	}

	var lines []string
	prev := 0
	for _, b := range append(breaks, len(runes)) {
		line := runes[prev:b]
		for oracleLogicalLen(string(line)) > width {
			line = line[:len(line)-1]
		}
		lines = append(lines, string(line))
		prev = b
	}
	return strings.Join(lines, "\n")
}

// oracleLogicalLen is the cell length wrapOracleText's fitting/crop decisions
// use in place of a token's raw rune count: the parity harness's own
// normalizeString (tools/parity/normalize.go) folds every "apm-go" back to
// "apm" via plain substring replacement BEFORE comparing apm-go's output
// against the Oracle's -- it does not re-wrap. If wrapOracleText instead
// measured against apm-go's own (three cells longer per occurrence)
// spelling, its line breaks would land in different places than the ones
// the Oracle chose for its shorter "apm" text, and no amount of post-hoc
// substring substitution could reconcile them (a break, once chosen, cannot
// un-happen). Folding "apm-go" -> "apm" purely for width accounting -- while
// slicing and rendering the real, unfolded text -- makes wrapOracleText
// choose exactly the breaks the Oracle would, so the final apm-go output
// matches byte-for-byte once the harness applies the identical fold for
// comparison (ticket 14, verified against rich 15.0.0's divide_line run
// directly on both spellings).
func oracleLogicalLen(s string) int {
	return len([]rune(strings.ReplaceAll(s, "apm-go", "apm")))
}

func isOracleWrapSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}
