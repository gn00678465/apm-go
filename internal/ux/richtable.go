package ux

import (
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// RichJustify is a column's alignment.
type RichJustify int

const (
	RichLeft RichJustify = iota
	RichCenter
)

// RichColumn is one column of a RichTable.
type RichColumn struct {
	Header  string
	Justify RichJustify
	Style   lipgloss.Style // cell style in a TTY
}

// RichTable is the data of a rich.table.Table: a title, the column
// definitions, and the rows, one string per column.
type RichTable struct {
	Title       string
	HeaderStyle lipgloss.Style
	Columns     []RichColumn
	Rows        [][]string
}

// Each column has one cell of padding on each side (Table(padding=(0, 1))).
const richCellPadding = 2

var richTitleStyle = lipgloss.NewStyle().Italic(true)

// RenderRichTable writes t as rich.table.Table(title=..., show_header=True)
// with box.HEAVY_HEAD, padding (0, 1), no expand and every column
// overflow="ellipsis", printed on a Console of width cells, byte for byte
// as Rich 15.0.0 prints it. The title and the cells are plain text: Rich
// would read console markup and emoji codes in them, this does not.
func RenderRichTable(w io.Writer, t RichTable, width int) {
	if len(t.Columns) == 0 {
		lipgloss.Fprintln(w, "")
		return
	}
	widths := richColumnWidths(t, width)
	tableWidth := len(t.Columns) + 1
	for _, cw := range widths {
		tableWidth += cw
	}

	var out []string
	if t.Title != "" {
		for _, line := range richWrap(t.Title, tableWidth, RichCenter, false) {
			out = append(out, richTitleStyle.Render(line))
		}
	}
	out = append(out, richRule("┏", "━", "┳", "┓", widths))
	header := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		header[i] = c.Header
	}
	out = append(out, richRow(t, widths, header, true)...)
	out = append(out, richRule("┡", "━", "╇", "┩", widths))
	for _, row := range t.Rows {
		out = append(out, richRow(t, widths, row, false)...)
	}
	out = append(out, richRule("└", "─", "┴", "┘", widths))
	if tableWidth > width {
		// Console.print crops every line to the console, which matters
		// only when the console cannot even hold the borders.
		for i, line := range out {
			out[i] = ansi.Truncate(line, max(width, 0), "")
		}
	}
	lipgloss.Fprintln(w, strings.Join(out, "\n"))
}

// TerminalColumns is Python's shutil.get_terminal_size((fallback, 24))
// .columns: a positive integer COLUMNS wins, then the width of stdout when
// it is a terminal that reports one, then fallback. Unlike panelWidth (Rich's
// Console.size), stdin and stderr are not consulted.
func TerminalColumns(fallback int) int {
	if cols, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && cols > 0 {
		return cols
	}
	if cols, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && cols > 0 {
		return cols
	}
	return fallback
}

// richColumnWidths is Table._calculate_column_widths (rich/table.py:523-586)
// for a table with no expand, no fixed or minimum widths and every column
// wrappable: each column is as wide as its widest cell, header included,
// plus its padding, capped at the space the console leaves. Rich measures
// the columns again once they shrink (rich/table.py:570-574); a shrunk
// column still measures at least its new width, so that pass changes
// nothing here.
func richColumnWidths(t RichTable, width int) []int {
	maxWidth := width - (len(t.Columns) + 1)
	widths := make([]int, len(t.Columns))
	for i, c := range t.Columns {
		widths[i] = richMeasure(c.Header, maxWidth)
		for _, row := range t.Rows {
			widths[i] = max(widths[i], richMeasure(richCell(row, i), maxWidth))
		}
		widths[i] = max(widths[i], 1)
	}
	if richSum(widths) > maxWidth {
		widths = richCollapseWidths(widths, maxWidth)
		if excess := richSum(widths) - maxWidth; excess > 0 {
			ones := make([]int, len(widths))
			for i := range ones {
				ones[i] = 1
			}
			widths = richRatioReduce(excess, ones, widths, widths)
		}
	}
	return widths
}

// richCollapseWidths is Table._collapse_widths (rich/table.py:588-627) with
// every column wrappable: the widest columns shrink together toward the
// next widest until the table fits or every column is as wide as the
// widest.
func richCollapseWidths(widths []int, maxWidth int) []int {
	total := richSum(widths)
	excess := total - maxWidth
	for total > 0 && excess > 0 {
		widest := 0
		for _, cw := range widths {
			widest = max(widest, cw)
		}
		second := 0
		for _, cw := range widths {
			if cw != widest {
				second = max(second, cw)
			}
		}
		if widest == second {
			break
		}
		ratios := make([]int, len(widths))
		maxReduce := make([]int, len(widths))
		for i, cw := range widths {
			if cw == widest {
				ratios[i] = 1
			}
			maxReduce[i] = min(excess, widest-second)
		}
		widths = richRatioReduce(excess, ratios, maxReduce, widths)
		total = richSum(widths)
		excess = total - maxWidth
	}
	return widths
}

// richRatioReduce is rich/_ratio.py ratio_reduce: total is taken from
// values in proportion to ratios, each share capped by its maximum. The
// share is rounded with Python's round, half to even.
func richRatioReduce(total int, ratios, maximums, values []int) []int {
	totalRatio := 0
	for i, r := range ratios {
		if maximums[i] == 0 {
			r = 0
		}
		totalRatio += r
	}
	result := slices.Clone(values)
	if totalRatio == 0 {
		return result
	}
	remaining := total
	for i, r := range ratios {
		if maximums[i] == 0 || r == 0 || totalRatio <= 0 {
			continue
		}
		share := min(maximums[i], int(math.RoundToEven(float64(r*remaining)/float64(totalRatio))))
		result[i] = values[i] - share
		remaining -= share
		totalRatio -= r
	}
	return result
}

func richSum(widths []int) int {
	total := 0
	for _, cw := range widths {
		total += cw
	}
	return total
}

// richMeasure is the maximum of Measurement.get for one padded cell
// (rich/padding.py:125-135 around rich/text.py:707-718): the widest line
// plus the padding, capped at maxWidth.
func richMeasure(text string, maxWidth int) int {
	if maxWidth < 1 {
		return 0
	}
	widest := 0
	for _, line := range strings.Split(text, "\n") {
		widest = max(widest, ansi.StringWidth(line))
	}
	return min(min(widest, maxWidth)+richCellPadding, maxWidth)
}

func richCell(row []string, i int) string {
	if i < len(row) {
		return row[i]
	}
	return ""
}

// richRow renders one table row, which is as many lines as its tallest
// cell. A header cell sits at the bottom of the row, a body cell at the
// top (rich/table.py:850-856).
func richRow(t RichTable, widths []int, cells []string, header bool) []string {
	rendered := make([][]string, len(t.Columns))
	height := 1
	for i, c := range t.Columns {
		// A cell with no room inside its padding renders no line at all
		// (rich/console.py:1312-1314 returns nothing below one cell).
		var lines []string
		if inner := widths[i] - richCellPadding; inner >= 1 {
			lines = richWrap(richCell(cells, i), inner, c.Justify, true)
		}
		for n, line := range lines {
			lines[n] = richSetCellSize(" "+line+" ", widths[i])
		}
		rendered[i] = lines
		height = max(height, len(lines))
	}
	bar := "┃"
	if !header {
		bar = "│"
	}
	out := make([]string, height)
	for n := range out {
		var b strings.Builder
		b.WriteString(bar)
		for i, c := range t.Columns {
			lines := rendered[i]
			at := n
			if header {
				at = n - (height - len(lines))
			}
			cell := strings.Repeat(" ", widths[i])
			if at >= 0 && at < len(lines) {
				cell = lines[at]
			}
			style := c.Style
			if header {
				style = t.HeaderStyle
			}
			b.WriteString(style.Render(cell))
			b.WriteString(bar)
		}
		out[n] = b.String()
	}
	return out
}

// richRule is one horizontal line of the box (rich/box.py:95-182).
func richRule(left, fill, cross, right string, widths []int) string {
	parts := make([]string, len(widths))
	for i, cw := range widths {
		parts[i] = strings.Repeat(fill, cw)
	}
	return left + strings.Join(parts, cross) + right
}

// richWrap is Text.wrap (rich/text.py:1201-1262) followed by the padding
// render_lines adds: text is split at newlines, each line is divided into
// words that fit width (rich/_wrap.py divide_line), justified, and cut with
// an ellipsis, or folded across lines when ellipsis is false. Every line
// returned is width cells wide.
func richWrap(text string, width int, justify RichJustify, ellipsis bool) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		for _, piece := range richDivide(line, width, !ellipsis) {
			piece = richRstripEnd(piece, width)
			switch justify {
			case RichCenter:
				piece = richTruncate(strings.TrimRightFunc(piece, unicode.IsSpace), width, ellipsis)
				excess := max(width-ansi.StringWidth(piece), 0)
				piece = strings.Repeat(" ", excess/2) + piece + strings.Repeat(" ", excess-excess/2)
			default:
				piece = richTruncate(piece, width, ellipsis)
			}
			lines = append(lines, richSetCellSize(piece, width))
		}
	}
	return lines
}

// richDivide splits line where rich/_wrap.py divide_line breaks it. A word
// is a run of non-space characters with the whitespace around it. A word
// that does not fit moves to the next line; one longer than width gets a
// line of its own, folded across lines when fold is set and left whole for
// the ellipsis otherwise.
func richDivide(line string, width int, fold bool) []string {
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
		case wordCells > width && fold:
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
		case wordCells > width:
			if start > 0 {
				breaks = append(breaks, start)
			}
			offset = ansi.StringWidth(word)
		case offset > 0 && start > 0:
			breaks = append(breaks, start)
			offset = ansi.StringWidth(word)
		}
	}
	pieces := make([]string, 0, len(breaks)+1)
	prev := 0
	for _, b := range append(breaks, len(runes)) {
		pieces = append(pieces, string(runes[prev:b]))
		prev = b
	}
	return pieces
}

// richRstripEnd is Text.rstrip_end (rich/text.py:666-679): trailing
// whitespace is dropped only as far as the line, counted in characters,
// is longer than size.
func richRstripEnd(s string, size int) string {
	runes := []rune(s)
	excess := len(runes) - size
	if excess <= 0 {
		return s
	}
	end := len(runes)
	for end > 0 && excess > 0 && unicode.IsSpace(runes[end-1]) {
		end--
		excess--
	}
	return string(runes[:end])
}

// richTruncate is Text.truncate (rich/text.py:859-885): a line wider than
// width is cut to width-1 cells plus "…", or to width cells without the
// ellipsis.
func richTruncate(s string, width int, ellipsis bool) string {
	if ansi.StringWidth(s) <= width {
		return s
	}
	if ellipsis {
		return richSetCellSize(s, width-1) + "…"
	}
	return richSetCellSize(s, width)
}

// richSetCellSize is rich/cells.py set_cell_size: s cropped or padded with
// spaces to exactly total cells. A double-width character cut in half
// becomes a space.
func richSetCellSize(s string, total int) string {
	if total <= 0 {
		return ""
	}
	cells := ansi.StringWidth(s)
	if cells > total {
		s = cropCells(s, total)
		cells = ansi.StringWidth(s)
	}
	return s + strings.Repeat(" ", total-cells)
}
