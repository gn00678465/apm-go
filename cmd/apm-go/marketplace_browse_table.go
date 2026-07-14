package main

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/apm-go/apm/internal/ux"
)

// browseDescriptionWrapWidth caps the rendered width of the Description
// column so a long description word-wraps into a multi-line table cell
// instead of stretching the whole table arbitrarily wide.
const browseDescriptionWrapWidth = 60

var browseTableHeaders = []string{"Plugin", "Description", "Version", "Install"}

// renderBrowseTable prints the plugin listing for `marketplace browse` as a
// pterm-boxed table (ux.Table). This intentionally does not reproduce the
// Python original's rich HEAVY_HEAD box styling byte-for-byte (design.md:
// "browse box table 遷移到 pterm.Table" accepts the visual difference) --
// only the Description column is still pre-wrapped here, since ux.Table
// itself does not word-wrap.
func renderBrowseTable(w io.Writer, title string, rows [][]string) {
	fmt.Fprintln(w, title)

	wrapped := make([][]string, len(rows))
	for i, row := range rows {
		cells := make([]string, len(row))
		copy(cells, row)
		if len(cells) > 1 {
			cells[1] = strings.Join(wrapRunes(cells[1], browseDescriptionWrapWidth), "\n")
		}
		wrapped[i] = cells
	}
	ux.Table(w, browseTableHeaders, wrapped)
}

// wrapRunes greedily word-wraps s to at most width runes per line,
// hard-splitting any single word longer than width so the cell never
// blows the column width.
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
