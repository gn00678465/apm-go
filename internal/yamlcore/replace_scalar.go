package yamlcore

import (
	"bytes"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

// ReplaceScalarValue returns src with the bytes of one single-line scalar
// value replaced by value, in the scalar's own style (plain, single quoted,
// or double quoted). Every other byte of src is kept, the rest of the
// value's line (a trailing comment, the line ending) included.
//
// It is the edit for one field of a hand-written entry: PatchMappingPath and
// SpliceSequenceElement render the whole value or element again, which
// moves comments and reorders nothing but the fields they know.
//
// node must come from SafeLoad(src). ok is false, and src is not changed,
// when the value is not a scalar that starts and ends on one line with
// nothing but a comment after it (a block or multi-line scalar, a value
// inside a flow collection, a tagged value) or when value does not render
// on one line. A replacement keeps the line count, so several nodes of the
// same SafeLoad can be replaced one after the other as long as no two of
// them share a line.
func ReplaceScalarValue(src []byte, node *yaml.Node, value string) (out []byte, ok bool) {
	if node == nil || node.Kind != yaml.ScalarNode {
		return nil, false
	}
	start, ok := nodeOffset(src, node)
	if !ok {
		return nil, false
	}
	lineEnd := len(src)
	if i := bytes.IndexByte(src[start:], '\n'); i >= 0 {
		lineEnd = start + i
	}
	line := src[start:lineEnd]

	var width int
	switch node.Style {
	case 0:
		// A plain scalar that continues on the next line has a longer
		// Value than its first line, so it fails this prefix test.
		width = len(node.Value)
		if !bytes.HasPrefix(line, []byte(node.Value)) {
			return nil, false
		}
	case yaml.SingleQuotedStyle, yaml.DoubleQuotedStyle:
		width = quotedWidth(line)
	default:
		return nil, false
	}
	if width == 0 || !onlyCommentFollows(line[width:]) {
		return nil, false
	}
	rendered, err := yaml.Marshal(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Style: node.Style, Value: value})
	if err != nil {
		return nil, false
	}
	rendered = bytes.TrimRight(rendered, "\n")
	if len(rendered) == 0 || bytes.IndexByte(rendered, '\n') >= 0 {
		return nil, false
	}

	out = make([]byte, 0, len(src)-width+len(rendered))
	out = append(out, src[:start]...)
	out = append(out, rendered...)
	out = append(out, src[start+width:]...)
	return out, true
}

// nodeOffset turns node's 1-based line and column (counted in characters)
// into a byte offset in src.
func nodeOffset(src []byte, node *yaml.Node) (int, bool) {
	if node.Line < 1 || node.Column < 1 {
		return 0, false
	}
	off := lineStartOffset(src, node.Line)
	for col := 1; col < node.Column; col++ {
		if off >= len(src) || src[off] == '\n' {
			return 0, false
		}
		_, size := utf8.DecodeRune(src[off:])
		off += size
	}
	return off, off < len(src)
}

// quotedWidth returns the byte length of the quoted scalar that line starts
// with, closing quote included, or 0 when it does not close on this line.
func quotedWidth(line []byte) int {
	if len(line) == 0 {
		return 0
	}
	quote := line[0]
	if quote != '\'' && quote != '"' {
		return 0
	}
	for i := 1; i < len(line); i++ {
		switch {
		case quote == '"' && line[i] == '\\':
			i++
		case line[i] != quote:
		case quote == '\'' && i+1 < len(line) && line[i+1] == '\'':
			i++
		default:
			return i + 1
		}
	}
	return 0
}

// onlyCommentFollows reports whether rest, the bytes after a value up to
// its line's end, holds nothing but white space and a comment.
func onlyCommentFollows(rest []byte) bool {
	trimmed := bytes.TrimLeft(rest, " \t")
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("\r")) {
		return true
	}
	return len(trimmed) < len(rest) && trimmed[0] == '#'
}
