package ux

import (
	"io"
	"strings"

	"charm.land/lipgloss/v2"
)

// RichNode is one node of a RichTree: its label, the label's style in a
// TTY, and its children in display order.
type RichNode struct {
	Label    string
	Style    lipgloss.Style
	Children []RichNode
}

// RenderRichTree writes root as rich.tree.Tree prints it with the default
// guides (rich/tree.py:36-41): the root label alone, then each child behind
// "├── ", or "└── " for the last, under the guides of its ancestors, "│   "
// or "    ". A label's further lines carry the guides of its children.
// Deliberate deviation: Rich wraps a label at the console width, this does
// not, so a label wider than the terminal stays on one line.
func RenderRichTree(w io.Writer, root RichNode) {
	var out []string
	var walk func(n RichNode, ancestors, fork, rest string)
	walk = func(n RichNode, ancestors, fork, rest string) {
		for i, line := range strings.Split(n.Label, "\n") {
			guide := fork
			if i > 0 {
				guide = rest
			}
			out = append(out, ancestors+guide+n.Style.Render(line))
		}
		for i, child := range n.Children {
			if i == len(n.Children)-1 {
				walk(child, ancestors+rest, "└── ", "    ")
			} else {
				walk(child, ancestors+rest, "├── ", "│   ")
			}
		}
	}
	walk(root, "", "", "")
	lipgloss.Fprintln(w, strings.Join(out, "\n"))
}
