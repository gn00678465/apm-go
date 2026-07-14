package ux

import (
	"fmt"
	"io"
	"strings"

	"github.com/pterm/pterm"
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

// Table renders headers and rows as a boxed table to w. Headers may be
// nil/empty to render a headerless table.
func Table(w io.Writer, headers []string, rows [][]string) {
	data := make(pterm.TableData, 0, len(rows)+1)
	if len(headers) > 0 {
		data = append(data, headers)
	}
	data = append(data, rows...)

	s, _ := pterm.DefaultTable.
		WithHasHeader(len(headers) > 0).
		WithBoxed(true).
		WithData(data).
		Srender()
	fmt.Fprintln(w, renderForWriter(w, s))
}

// BulletList renders a leveled bullet list to w.
func BulletList(w io.Writer, items []Item) {
	list := make([]pterm.BulletListItem, len(items))
	for i, it := range items {
		list[i] = pterm.BulletListItem{Level: it.Level, Text: it.Text}
	}

	s, _ := pterm.DefaultBulletList.WithItems(list).Srender()
	fmt.Fprintln(w, renderForWriter(w, s))
}

// Tree renders a nested tree to w.
func Tree(w io.Writer, root TreeNode) {
	s, _ := pterm.DefaultTree.WithRoot(toPtermTreeNode(root)).Srender()
	fmt.Fprintln(w, renderForWriter(w, s))
}

func toPtermTreeNode(n TreeNode) pterm.TreeNode {
	children := make([]pterm.TreeNode, len(n.Children))
	for i, c := range n.Children {
		children[i] = toPtermTreeNode(c)
	}
	return pterm.TreeNode{Text: n.Text, Children: children}
}

// Section renders a section heading to w.
func Section(w io.Writer, title string) {
	fmt.Fprint(w, renderForWriter(w, pterm.DefaultSection.Sprintln(title)))
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
	fmt.Fprintln(w, renderForWriter(w, strings.Join(lines, "\n")))
}

func colorDiffLine(line string) string {
	switch {
	case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
		return line
	case strings.HasPrefix(line, "+"):
		return pterm.NewStyle(pterm.FgGreen).Sprint(line)
	case strings.HasPrefix(line, "-"):
		return pterm.NewStyle(pterm.FgRed).Sprint(line)
	default:
		return line
	}
}
