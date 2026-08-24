package ux

import (
	"os"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
)

// oracleFallbackWidth mirrors rich.console.ConsoleDimensions's ultimate
// fallback (rich/console.py:1042, `width = width or 80`) once every other
// source (an explicit Console(width=...), a real terminal's os.get_terminal_
// size, and the COLUMNS env var) has come up empty.
const oracleFallbackWidth = 80

// oracleConsoleWidth mirrors Console.size's width resolution (rich/console.
// py:1005-1045) as it actually behaves in the parity harness's sandbox.
// Ticket 14 attempt 1 assumed the fixed ConsoleDimensions(80, 25) dumb-
// terminal short-circuit (console.py:1015-1016) always applied because the
// sandbox sets TERM=dumb -- but that branch is gated on `is_terminal`
// (console.py:979-985: `is_dumb_terminal = is_terminal and TERM in
// {dumb,unknown}`), and is_terminal is False whenever stdout/stderr is
// piped to a file rather than a real tty (`isatty()` returns False) --
// exactly every parity-harness invocation. So the dumb-terminal fallback
// never actually fires here: Console.size instead tries os.get_terminal_
// size (fails, not a tty), then falls back to reading COLUMNS from the
// environment, and only then to the literal 80 default. Verified directly
// against the pinned Oracle with COLUMNS=100 (ticket 14 attempt 2,
// eval-ticket-14.md reproducer 1): the Oracle wrapped at 100, not 80.
//
// COLUMNS is honored only when it is a non-empty run of ASCII digits
// (Python's `columns.isdigit()`, approximated here as ASCII 0-9 -- COLUMNS
// is never anything else in practice) and parses to a positive int; any
// other value (empty, non-numeric, zero) falls through to the 80 default,
// matching Rich's own `width = width or 80` (an explicit 0 is falsy too).
func oracleConsoleWidth() int {
	if v, ok := os.LookupEnv("COLUMNS"); ok && isASCIIDigits(v) {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return oracleFallbackWidth
}

func isASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// oracleCellWidth is rich.cells.get_character_cell_size's Go counterpart,
// backed by the same category of Unicode East-Asian-width data (via
// go-runewidth/uniseg, already vendored transitively through lipgloss) --
// verified directly against the pinned Oracle for the reachable case that
// matters here, CJK ideographs (e.g. 市場), which both sides count as 2
// cells. Exotic combining-mark/ZWJ grapheme-cluster merging (rich.cells.
// split_graphemes's SPECIAL handling) is not replicated -- apm-go's
// messages are plain marketplace names, never emoji sequences.
func oracleCellWidth(r rune) int {
	return runewidth.RuneWidth(r)
}

func oracleCellLen(runes []rune) int {
	n := 0
	for _, r := range runes {
		n += oracleCellWidth(r)
	}
	return n
}

// wrapOracleText reproduces rich.text.Text.wrap (rich/text.py:1201-1246) as
// exercised by the Oracle's default Console.print: split into hard lines on
// "\n" FIRST (each an independent render boundary -- Rich resets its width
// accounting at every embedded newline rather than treating "\n" as
// ordinary wrappable whitespace, ticket 14 attempt 2's reproducer 4), then
// word-wrap each hard line independently via divide_line + a per-line
// rstrip_end crop (wrapHardLine, below), and rejoin with "\n".
func wrapOracleText(text string, width int) string {
	if width < 1 {
		// Rich itself would raise (chop_cells's range(...,...,width) with
		// width<=0 is a Python ValueError) -- COLUMNS is never 0 or
		// negative in practice (oracleConsoleWidth already rejects
		// non-positive values), so this is a defensive floor only, not a
		// behavior verified against the Oracle.
		width = 1
	}
	hardLines := strings.Split(text, "\n")
	out := make([]string, len(hardLines))
	for i, hl := range hardLines {
		out[i] = strings.Join(wrapHardLine([]rune(hl), width), "\n")
	}
	return strings.Join(out, "\n")
}

// wrapHardLine wraps a single line already known to contain no "\n"
// (wrapOracleText splits on hard newlines first). It ports rich._wrap.
// divide_line (rich/_wrap.py) plus Text.rstrip_end (rich/text.py:666-677),
// verified against rich 15.0.0 (the Oracle's pinned version) source and
// against live captures for all four of the evaluator's reproducers.
//
// Tokens are non-space runs with any immediately-following whitespace
// attached (so a token's trailing space travels with it, not the next
// token) -- matching rich._wrap.words's `\s*\S+\s*` regex, since a prior
// token's own trailing `\s*` normally already consumes any gap before the
// next one starts. A running cell_offset tracks the current line's cell
// width; per token:
//   - it fits if cell_offset + cell_width(token, trailing space stripped)
//     <= width: the FULL token (trailing space included) is added to
//     cell_offset.
//   - if the token's own (trailing-space-stripped) cell width exceeds
//     width outright, it cannot fit on any line uncut: chopCells folds it
//     into width-sized pieces (rich.cells.chop_cells), each piece (the
//     first included -- Rich does not try to cram the fold's first piece
//     into the current line's remaining space) starting a fresh line
//     except the very last, whose cell width becomes the new cell_offset
//     so following tokens can still continue that line.
//   - otherwise (doesn't fit remaining space, but fits on a fresh empty
//     line) a break is inserted before the token and cell_offset resets to
//     the token's own full width.
//
// Because the fits-check ignores trailing whitespace but the accumulation
// doesn't, an accepted line's raw length can overshoot width by exactly one
// trailing-space's worth; rstrip_end (rstripEnd, below) reproduces Rich's
// own crop for that overshoot.
func wrapHardLine(runes []rune, width int) []string {
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
		trimmed := trimRightSpace(tok.runes)
		wordLength := oracleCellLen(trimmed)
		remaining := width - cellOffset
		switch {
		case remaining >= wordLength:
			cellOffset += oracleCellLen(tok.runes)
		case wordLength > width:
			chunks := chopCells(tok.runes, width)
			pos := tok.start
			for idx, chunk := range chunks {
				last := idx == len(chunks)-1
				if pos > 0 {
					breaks = append(breaks, pos)
				}
				if last {
					cellOffset = oracleCellLen(chunk)
				} else {
					pos += len(chunk)
				}
			}
		case cellOffset > 0 && tok.start > 0:
			breaks = append(breaks, tok.start)
			cellOffset = oracleCellLen(tok.runes)
		default:
			cellOffset += oracleCellLen(tok.runes)
		}
	}

	if len(breaks) == 0 {
		return []string{rstripEnd(runes, width)}
	}

	lines := make([]string, 0, len(breaks)+1)
	prev := 0
	for _, b := range append(breaks, len(runes)) {
		lines = append(lines, rstripEnd(runes[prev:b], width))
		prev = b
	}
	return lines
}

// chopCells mirrors rich.cells.chop_cells (rich/cells.py): split runes into
// consecutive chunks whose cell width never exceeds width, breaking before
// whichever rune would push a chunk over. Rich special-cases an all-single-
// cell-width input to a plain width-sized character slice, but that is
// mathematically identical to this general accumulation when every rune's
// cell width is 1, so there is no need for a separate fast path. Grapheme-
// cluster merging (rich.cells.split_graphemes's ZWJ/variation-selector
// handling) is not implemented -- see oracleCellWidth.
func chopCells(runes []rune, width int) [][]rune {
	var chunks [][]rune
	lineSize := 0
	lineStart := 0
	for i, r := range runes {
		w := oracleCellWidth(r)
		if lineSize+w > width {
			chunks = append(chunks, runes[lineStart:i])
			lineStart = i
			lineSize = 0
		}
		lineSize += w
	}
	if lineSize > 0 {
		chunks = append(chunks, runes[lineStart:])
	}
	return chunks
}

// rstripEnd mirrors Text.rstrip_end (rich/text.py:666-677): if a subline's
// CHARACTER count -- not its cell count, this is Rich's own literal
// behavior, comparing a rune count against a cell-width bound -- exceeds
// width, strip up to (that excess) characters from its trailing whitespace
// run. divide_line's own invariants mean the only reachable overshoot is a
// single trailing ASCII space left over from the fits-check ignoring
// trailing whitespace (see wrapHardLine's doc comment), where character
// count and cell count coincide, so this narrower rune-count-based
// reproduction matches Rich's behavior on every input apm-go can produce.
func rstripEnd(runes []rune, width int) string {
	textLength := len(runes)
	if textLength <= width {
		return string(runes)
	}
	excess := textLength - width
	wsLen := trailingWhitespaceRunLength(runes)
	if wsLen == 0 {
		return string(runes)
	}
	crop := wsLen
	if excess < crop {
		crop = excess
	}
	return string(runes[:len(runes)-crop])
}

func trailingWhitespaceRunLength(runes []rune) int {
	n := 0
	for n < len(runes) && isOracleWrapSpace(runes[len(runes)-1-n]) {
		n++
	}
	return n
}

func trimRightSpace(runes []rune) []rune {
	end := len(runes)
	for end > 0 && isOracleWrapSpace(runes[end-1]) {
		end--
	}
	return runes[:end]
}

func isOracleWrapSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}
