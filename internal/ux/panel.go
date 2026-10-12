package ux

import (
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

const (
	panelDefaultWidth = 80
	// Two border cells plus one padding cell on each side.
	panelFrameCells = 4
)

var panelBorderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBrand))

// Panel renders body inside a rounded border that spans the console width,
// with title centered in the top border. It reproduces the Oracle's
// rich.panel.Panel(content, title=title) byte for byte (utils/console.py:175
// _rich_panel), where Box sizes itself to its content and puts the title on
// the first body row.
func Panel(w io.Writer, title string, body []string) {
	total := max(panelWidth(), panelFrameCells+1)
	inner := total - panelFrameCells

	var rows []string
	for _, entry := range body {
		for _, line := range strings.Split(entry, "\n") {
			rows = append(rows, wrapPanelLine(line, inner)...)
		}
	}

	bar := panelBorderStyle.Render("│")
	out := make([]string, 0, len(rows)+2)
	out = append(out, panelBorderStyle.Render(panelTopBorder(title, total)))
	for _, row := range rows {
		out = append(out, bar+" "+row+" "+bar)
	}
	out = append(out, panelBorderStyle.Render("╰"+strings.Repeat("─", total-2)+"╯"))
	lipgloss.Fprintln(w, strings.Join(out, "\n"))
}

// panelWidth is the console width rule of rich.console.Console.size: a
// numeric COLUMNS wins, then the size of the first of stdin, stdout, stderr
// that is a terminal, then 80. The writer is not consulted, so a piped run
// keeps the width of the terminal it was started from. Rich's one exception
// is kept too: stdout on a dumb terminal is 80 wide unless COLUMNS and LINES
// are both numeric.
func panelWidth() int {
	cols, colsErr := strconv.Atoi(os.Getenv("COLUMNS"))
	_, linesErr := strconv.Atoi(os.Getenv("LINES"))
	if kind := strings.ToLower(os.Getenv("TERM")); (kind == "dumb" || kind == "unknown") &&
		isTerminalFile(os.Stdout) && (colsErr != nil || linesErr != nil) {
		return panelDefaultWidth
	}
	if colsErr == nil && cols > 0 {
		return cols
	}
	for _, f := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		if cols, _, err := term.GetSize(int(f.Fd())); err == nil && cols > 0 {
			return cols
		}
	}
	return panelDefaultWidth
}

// panelTopBorder centers the title, padded with one space on each side, in
// the top border; an odd remainder goes to the right (rich/panel.py
// align_text, called with total-4 between one border cell on each side).
func panelTopBorder(title string, total int) string {
	if title == "" {
		return "╭" + strings.Repeat("─", total-2) + "╮"
	}
	span := total - panelFrameCells
	text := cropCells(" "+title+" ", span)
	excess := span - ansi.StringWidth(text)
	left := excess / 2
	return "╭─" + strings.Repeat("─", left) + text + strings.Repeat("─", excess-left) + "─╮"
}

// wrapPanelLine wraps one line to width cells the way rich/_wrap.py
// divide_line does: a word is a run of non-space characters with the
// whitespace around it, a word that does not fit moves to the next row, and
// a word longer than a row starts a new row and is folded. A continuation
// row therefore starts at the first column, with no indent. Each row is
// padded to width.
func wrapPanelLine(line string, width int) []string {
	runes := []rune(line)
	var breaks []int
	offset := 0
	for i := 0; i < len(runes); {
		start := i
		for i < len(runes) && unicode.IsSpace(runes[i]) {
			i++
		}
		if i == len(runes) {
			break
		}
		for i < len(runes) && !unicode.IsSpace(runes[i]) {
			i++
		}
		for i < len(runes) && unicode.IsSpace(runes[i]) {
			i++
		}
		word := string(runes[start:i])
		wordCells := ansi.StringWidth(strings.TrimRightFunc(word, unicode.IsSpace))

		switch {
		case width-offset >= wordCells:
			offset += ansi.StringWidth(word)
		case wordCells > width:
			pieces := chopCells(word, width)
			for n, piece := range pieces {
				if start > 0 {
					breaks = append(breaks, start)
				}
				if n == len(pieces)-1 {
					offset = ansi.StringWidth(piece)
				} else {
					start += len([]rune(piece))
				}
			}
		case offset > 0 && start > 0:
			breaks = append(breaks, start)
			offset = ansi.StringWidth(word)
		}
	}

	rows := make([]string, 0, len(breaks)+1)
	prev := 0
	for _, b := range append(breaks, len(runes)) {
		row := cropCells(string(runes[prev:b]), width)
		rows = append(rows, row+strings.Repeat(" ", width-ansi.StringWidth(row)))
		prev = b
	}
	return rows
}

// chopCells splits s into pieces of at most width cells each.
func chopCells(s string, width int) []string {
	var pieces []string
	var cur strings.Builder
	cells := 0
	for _, r := range s {
		rc := ansi.StringWidth(string(r))
		if cells > 0 && cells+rc > width {
			pieces = append(pieces, cur.String())
			cur.Reset()
			cells = 0
		}
		cur.WriteRune(r)
		cells += rc
	}
	return append(pieces, cur.String())
}

// cropCells cuts s at the last character that still fits in width cells.
func cropCells(s string, width int) string {
	cells := 0
	for i, r := range s {
		cells += ansi.StringWidth(string(r))
		if cells > width {
			return s[:i]
		}
	}
	return s
}
