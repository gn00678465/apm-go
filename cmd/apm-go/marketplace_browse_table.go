package main

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// browseTableMaxWidth caps the rendered table width.
// ponytail: fixed budget instead of probing the terminal size (rich uses the
// real terminal width); add golang.org/x/term if the difference bites.
const browseTableMaxWidth = 120

var browseTableHeaders = [4]string{"Plugin", "Description", "Version", "Install"}

// renderBrowseTable prints the rich-style HEAVY_HEAD box table the Python
// original draws via rich.table.Table for `marketplace browse`: a centered
// title, heavy borders around the header row, light borders around the body.
// Only the Description column wraps (rich's ratio=1 column); Version is
// centered; everything else is left-aligned.
// ponytail: widths are rune counts, not terminal cells -- CJK text may
// misalign; switch to go-runewidth if that matters.
func renderBrowseTable(w io.Writer, title string, rows [][4]string) {
	var widths [4]int
	for i, h := range browseTableHeaders {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if n := utf8.RuneCountInString(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}
	// 5 border runes + 2 padding runes per column; shrink only Description
	// to stay within the budget.
	const chrome = 5 + 4*2
	fixed := chrome + widths[0] + widths[2] + widths[3]
	if fixed+widths[1] > browseTableMaxWidth {
		widths[1] = browseTableMaxWidth - fixed
		if min := utf8.RuneCountInString(browseTableHeaders[1]); widths[1] < min {
			widths[1] = min
		}
	}
	total := fixed + widths[1]

	if pad := (total - utf8.RuneCountInString(title)) / 2; pad > 0 {
		fmt.Fprint(w, strings.Repeat(" ", pad))
	}
	fmt.Fprintln(w, title)

	rule := func(left, fill, mid, right string) {
		parts := make([]string, len(widths))
		for i, cw := range widths {
			parts[i] = strings.Repeat(fill, cw+2)
		}
		fmt.Fprintln(w, left+strings.Join(parts, mid)+right)
	}
	line := func(sep string, cells [4]string) {
		parts := make([]string, len(cells))
		for i, c := range cells {
			pad := widths[i] - utf8.RuneCountInString(c)
			if i == 2 { // Version is centered
				left := pad / 2
				parts[i] = strings.Repeat(" ", left) + c + strings.Repeat(" ", pad-left)
			} else {
				parts[i] = c + strings.Repeat(" ", pad)
			}
			parts[i] = " " + parts[i] + " "
		}
		fmt.Fprintln(w, sep+strings.Join(parts, sep)+sep)
	}

	rule("┏", "━", "┳", "┓")
	line("┃", browseTableHeaders)
	rule("┡", "━", "╇", "┩")
	for _, row := range rows {
		for j, desc := range wrapRunes(row[1], widths[1]) {
			cells := [4]string{"", desc, "", ""}
			if j == 0 {
				cells = [4]string{row[0], desc, row[2], row[3]}
			}
			line("│", cells)
		}
	}
	rule("└", "─", "┴", "┘")
}

// wrapRunes greedily word-wraps s to at most width runes per line,
// hard-splitting any single word longer than width so the box never breaks.
func wrapRunes(s string, width int) []string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(s) {
		for utf8.RuneCountInString(word) > width {
			if cur != "" {
				lines = append(lines, cur)
				cur = ""
			}
			r := []rune(word)
			lines = append(lines, string(r[:width]))
			word = string(r[width:])
		}
		switch {
		case cur == "":
			cur = word
		case utf8.RuneCountInString(cur)+1+utf8.RuneCountInString(word) <= width:
			cur += " " + word
		default:
			lines = append(lines, cur)
			cur = word
		}
	}
	if cur != "" || len(lines) == 0 {
		lines = append(lines, cur)
	}
	return lines
}
