package ux

import (
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/list"
	"charm.land/lipgloss/v2/table"
	"charm.land/lipgloss/v2/tree"
	"golang.org/x/term"
)

// Item is a single entry in a BulletList, with an indent Level (0 = top).
// Muted renders Text in ColorMuted (R9/R10c: distinguishing an
// already-declared/duplicate entry from a genuinely new one) instead of the
// default unstyled text.
type Item struct {
	Level int
	Text  string
	Muted bool
}

// TreeNode is a node in a Tree, used for nested reports such as install
// dependency trees or marketplace audit results.
type TreeNode struct {
	Text     string
	Children []TreeNode
}

var (
	headingStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorHeading))
	mutedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted))
	tableHeaderCell = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorBrand)).Padding(0, 1)
	tableBodyCell   = lipgloss.NewStyle().Padding(0, 1)
	boxStyle        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(ColorBrand)).Padding(0, 1)
)

// terminalWidthFor returns w's terminal column width when w is a real
// terminal, or 0 (no constraint) for a pipe, redirected file, or in-memory
// buffer -- so tests, logs, and shell pipelines keep the natural
// content-sized rendering. A package-level var so tests can force a width
// without a real TTY.
var terminalWidthFor = func(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok || !isTerminalFile(f) {
		return 0
	}
	cols, _, err := term.GetSize(int(f.Fd()))
	if err != nil || cols <= 0 {
		return 0
	}
	return cols
}

// Table renders headers and rows as a boxed table to w. Headers may be
// nil/empty to render a headerless table. lipgloss/table computes column
// widths (and CJK full-width runes) correctly, so the box stays aligned --
// unlike pterm's width engine.
//
// When w is a real terminal and the table's natural width would overflow it,
// the table is capped to the terminal width: lipgloss/table shrinks columns
// and word-wraps data cells (its default Wrap(true)) instead of letting the
// terminal hard-break the box borders mid-row. The cap is applied only on
// overflow -- a narrow table is never stretched to fill the terminal, since
// Table.Width() would otherwise expand columns too.
//
// Whenever any data cell spans multiple lines -- a "\n" in the input, or
// wrapping introduced by the width cap -- row separator lines are drawn so
// adjacent rows stay distinguishable (2026-08-07 user ruling; a deliberate
// ergonomic addition over upstream rich's show_lines=False default).
// Single-line tables keep the separator-free rendering.
func Table(w io.Writer, headers []string, rows [][]string) {
	lipgloss.Fprintln(w, TableString(w, headers, rows))
}

// TableString renders the same table Table prints, as a string, for callers
// that place it inside another surface (the clack transcript's Note box).
// w is consulted only for the terminal-width cap. The returned string
// carries lipgloss styling; the caller's own writer downsamples it.
func TableString(w io.Writer, headers []string, rows [][]string) string {
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(mutedStyle).
		BorderColumn(true).
		BorderHeader(len(headers) > 0).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return tableHeaderCell
			}
			return tableBodyCell
		})

	if len(headers) > 0 {
		t = t.Headers(headers...)
	}
	t = t.Rows(rows...)

	multiline := anyCellMultiline(rows)
	capped := false
	rendered := t.String()
	// Cap to ONE LESS than the terminal width, and trigger already at
	// exact-width: a line that occupies every terminal column puts the
	// right border glyph in the last cell, where Windows terminals'
	// auto-wrap/last-column handling clips or wraps it -- the reported
	// "right border missing" symptom. rich reserves the same 1-column
	// margin on legacy Windows consoles.
	if maxWidth := terminalWidthFor(w); maxWidth > 1 && lipgloss.Width(rendered) >= maxWidth {
		t = t.Width(maxWidth - 1)
		capped = true
	}
	if multiline || capped {
		rendered = t.BorderRow(true).String()
	}
	return rendered
}

// anyCellMultiline reports whether any data cell already contains a line
// break of its own.
func anyCellMultiline(rows [][]string) bool {
	for _, row := range rows {
		for _, cell := range row {
			if strings.Contains(cell, "\n") {
				return true
			}
		}
	}
	return false
}

// BulletList renders a leveled bullet list to w. Indentation is expressed as
// nested sub-lists (lipgloss/list's native mechanism for hierarchy).
func BulletList(w io.Writer, items []Item) {
	lipgloss.Fprintln(w, buildBulletList(items).String())
}

// List renders the stream-facing list form used by Oracle continuation
// records: two spaces, a dash, and the item text. It deliberately does not
// use the centered project-TUI SymbolList enumerator; that form is reserved
// for interactive clack content. Item.Level adds two spaces per nesting
// level, preserving the shape of Oracle logger.progress/tree_item output
// (utils/console.py STATUS_SYMBOLS and core/command_logger.py:158-160).
func List(w io.Writer, items []Item) {
	for _, item := range items {
		indent := "  " + strings.Repeat("  ", max(item.Level, 0))
		text := item.Text
		if item.Muted {
			text = mutedStyle.Render(text)
		}
		lipgloss.Fprintln(w, indent+"- "+text)
	}
}

func newBulletList() *list.List {
	// R8a: the SymbolList enumerator is centered in a fixed width-3 column,
	// same as message symbols (printer.go's printLine), so bullet items and
	// message lines align and the enumerator always has a visible gap before
	// the item text (an unstyled/unwidth-ed EnumeratorStyle collapses that
	// gap). list.Bullet is not used here: it hardcodes "•" (an East-Asian
	// Ambiguous-width glyph, see colors.go's R8/P4-7 comment) instead of
	// following the single-source symbol set.
	return list.New().Enumerator(bulletEnumerator).
		EnumeratorStyle(mutedStyle.AlignHorizontal(lipgloss.Center).Width(3))
}

// bulletEnumerator renders SymbolList for every item, in place of
// list.Bullet's hardcoded "•".
func bulletEnumerator(list.Items, int) string {
	return SymbolList
}

// buildBulletList turns a flat, leveled slice of items into nested
// lipgloss/list sub-lists: each item is appended to the currently open list
// at its Level, opening new nested lists on demand as Level increases.
func buildBulletList(items []Item) *list.List {
	root := newBulletList()
	stack := []*list.List{root}

	for _, it := range items {
		level := max(it.Level, 0)

		for level+1 > len(stack) {
			sub := newBulletList()
			stack[len(stack)-1].Item(sub)
			stack = append(stack, sub)
		}
		stack = stack[:level+1]
		text := it.Text
		if it.Muted {
			text = mutedStyle.Render(text)
		}
		stack[level].Item(text)
	}

	return root
}

// Tree renders a nested tree to w using lipgloss/tree's native connecting
// lines (├─ / └─ / │).
func Tree(w io.Writer, root TreeNode) {
	lipgloss.Fprintln(w, toLipglossTree(root).String())
}

func toLipglossTree(n TreeNode) *tree.Tree {
	t := tree.Root(n.Text)
	for _, c := range n.Children {
		t.Child(toLipglossTree(c))
	}
	return t
}

// Section renders a section heading to w.
func Section(w io.Writer, title string) {
	lipgloss.Fprintln(w, headingStyle.Render(title))
}

// Box renders title and body (one entry per line) inside a bordered box to
// w, used for a final confirmation summary (e.g. init's "About to create").
func Box(w io.Writer, title string, body []string) {
	lines := append([]string{headingStyle.Render(title)}, body...)
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	lipgloss.Fprintln(w, boxStyle.Render(content))
}

// Diff renders a unified diff to w: "+"-prefixed lines (other than the
// "+++ ..." file header) are colored green, "-"-prefixed lines (other than
// "--- ...") red; every other line (context, "@@ ... @@" hunk headers, file
// headers) is printed as-is.
func Diff(w io.Writer, diffText string) {
	lines := strings.Split(strings.TrimRight(diffText, "\n"), "\n")
	for i, line := range lines {
		lines[i] = colorDiffLine(line)
	}
	lipgloss.Fprintln(w, strings.Join(lines, "\n"))
}

func colorDiffLine(line string) string {
	switch {
	case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
		return line
	case strings.HasPrefix(line, "+"):
		return successStyle.Render(line)
	case strings.HasPrefix(line, "-"):
		return errorStyle.Render(line)
	default:
		return line
	}
}
