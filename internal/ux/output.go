package ux

import (
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/list"
	"charm.land/lipgloss/v2/table"
	"charm.land/lipgloss/v2/tree"
)

// Item is a single entry in a BulletList, with an indent Level (0 = top).
type Item struct {
	Level int
	Text  string
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

// Table renders headers and rows as a boxed table to w. Headers may be
// nil/empty to render a headerless table. lipgloss/table computes column
// widths (and CJK full-width runes) correctly, so the box stays aligned --
// unlike pterm's width engine.
func Table(w io.Writer, headers []string, rows [][]string) {
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

	lipgloss.Fprintln(w, t.String())
}

// BulletList renders a leveled bullet list to w. Indentation is expressed as
// nested sub-lists (lipgloss/list's native mechanism for hierarchy).
func BulletList(w io.Writer, items []Item) {
	lipgloss.Fprintln(w, buildBulletList(items).String())
}

func newBulletList() *list.List {
	return list.New().Enumerator(list.Bullet).EnumeratorStyle(mutedStyle)
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
		stack[level].Item(it.Text)
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
