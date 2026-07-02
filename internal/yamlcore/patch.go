package yamlcore

import (
	"bytes"
	"strings"

	"go.yaml.in/yaml/v4"
)

// pathFrame records one level of descent while walking a dotted mapping
// key path: the mapping node at that level, and the index (in
// mapping.Content) of the key that led one level deeper.
type pathFrame struct {
	mapping *yaml.Node
	keyIdx  int
}

// PatchMappingPath re-serializes only the value at the given dotted
// mapping key path within doc (e.g. []string{"dependencies", "mcp"}),
// splicing the change into the original src bytes so every other byte of
// src is preserved verbatim -- including hand-authored multi-line
// flow-style formatting elsewhere in the document that a full SafeDump
// re-encode cannot reproduce (SafeDump's Node model does not retain the
// original line-wrapping of untouched flow collections).
//
// doc must be the *yaml.Node tree produced by SafeLoad(src), possibly
// already mutated in place along path (e.g. via a find-or-create helper
// that appends to a sequence's Content). Newly created nodes are detected
// by Line == 0 (SafeLoad-parsed nodes always have Line >= 1).
//
// Only supports a document whose root, and every mapping named by path
// except the final segment, is a block-style YAML mapping -- the normal
// shape for apm.yml. Returns ok=false when this doesn't hold (or the walk
// hits anything unexpected); callers must fall back to a full SafeDump in
// that case.
func PatchMappingPath(src []byte, doc *yaml.Node, path []string) (out []byte, ok bool, err error) {
	if len(path) == 0 {
		return nil, false, nil
	}
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil, false, nil
	}

	var frames []pathFrame
	cur := root
	for i, key := range path {
		idx := findMappingKeyIndex(cur, key)
		isLast := i == len(path)-1

		if idx == -1 || cur.Content[idx].Line == 0 {
			return insertMissingPath(src, root, frames, cur, path[i:])
		}

		valNode := cur.Content[idx+1]
		if isLast {
			frames = append(frames, pathFrame{mapping: cur, keyIdx: idx})
			return replaceValueSpan(src, frames, valNode)
		}
		if valNode.Kind != yaml.MappingNode {
			return nil, false, nil
		}
		frames = append(frames, pathFrame{mapping: cur, keyIdx: idx})
		cur = valNode
	}
	return nil, false, nil
}

func findMappingKeyIndex(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// spanEndOffset returns the byte offset where the span of frames[level]'s
// current key ends: the start of the next sibling key at that level, or
// (recursing up through frames) the next sibling of an ancestor's key, or
// len(src) if none exists at any level.
func spanEndOffset(src []byte, frames []pathFrame, level int) int {
	for l := level; l >= 0; l-- {
		f := frames[l]
		if f.keyIdx+2 < len(f.mapping.Content) {
			next := f.mapping.Content[f.keyIdx+2]
			return lineStartOffset(src, next.Line)
		}
	}
	return len(src)
}

func lineStartOffset(src []byte, line int) int {
	if line <= 1 {
		return 0
	}
	count := 1
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			count++
			if count == line {
				return i + 1
			}
		}
	}
	return len(src)
}

func replaceValueSpan(src []byte, frames []pathFrame, valNode *yaml.Node) ([]byte, bool, error) {
	level := len(frames) - 1
	f := frames[level]
	keyNode := f.mapping.Content[f.keyIdx]

	start := lineStartOffset(src, keyNode.Line)
	end := spanEndOffset(src, frames, level)
	if end < start {
		return nil, false, nil
	}

	indent := keyNode.Column - 1
	if indent < 0 {
		indent = 0
	}
	fragment, err := renderKeyValue(keyNode, valNode, indent)
	if err != nil {
		return nil, false, err
	}
	fragment = matchLineEndings(src, fragment)

	var buf bytes.Buffer
	buf.Write(src[:start])
	buf.Write(fragment)
	buf.Write(src[end:])
	return buf.Bytes(), true, nil
}

func insertMissingPath(src []byte, root *yaml.Node, frames []pathFrame, cur *yaml.Node, remaining []string) ([]byte, bool, error) {
	idx := findMappingKeyIndex(cur, remaining[0])
	if idx == -1 {
		// The caller was supposed to have already created this key
		// in-memory (e.g. via a find-or-create helper) before calling us.
		return nil, false, nil
	}
	keyNode := cur.Content[idx]
	valNode := cur.Content[idx+1]

	var insertAt, indent int
	if len(frames) == 0 {
		indent = 0
		insertAt = len(src)
	} else {
		level := len(frames) - 1
		parentKeyNode := frames[level].mapping.Content[frames[level].keyIdx]
		indent = parentKeyNode.Column - 1 + 2
		insertAt = spanEndOffset(src, frames, level)
	}
	if indent < 0 {
		indent = 0
	}

	fragment, err := renderKeyValue(keyNode, valNode, indent)
	if err != nil {
		return nil, false, err
	}
	fragment = matchLineEndings(src, fragment)

	var buf bytes.Buffer
	buf.Write(src[:insertAt])
	if insertAt > 0 && src[insertAt-1] != '\n' {
		if usesCRLF(src) {
			buf.WriteString("\r\n")
		} else {
			buf.WriteByte('\n')
		}
	}
	buf.Write(fragment)
	buf.Write(src[insertAt:])
	return buf.Bytes(), true, nil
}

// usesCRLF reports whether src's line endings are CRLF ("\r\n"), so a
// freshly rendered fragment (SafeDump/the yaml dumper always emits bare
// "\n") can be normalized to match instead of leaving a splice with mixed
// line endings inside an otherwise-CRLF document.
func usesCRLF(src []byte) bool {
	return bytes.Contains(src, []byte("\r\n"))
}

// matchLineEndings converts fragment's line endings to CRLF when src is a
// CRLF document. fragment is always LF-only coming out of renderKeyValue.
func matchLineEndings(src, fragment []byte) []byte {
	if !usesCRLF(src) {
		return fragment
	}
	return bytes.ReplaceAll(fragment, []byte("\n"), []byte("\r\n"))
}

// renderKeyValue renders "key: <value>\n" (or, for a multi-line block
// value, "key:\n  - item\n  ...\n") at the given indent (spaces),
// delegating the actual key/value/indent rendering rules to SafeDump
// itself (encoding a synthetic 1-pair mapping at indent 0) and then
// left-padding every output line by indent spaces.
//
// keyNode's LineComment and FootComment are re-attached to the rendered
// fragment so a comment on the key's own line (e.g. "mcp: # servers") or a
// trailing comment before the next sibling key (both land on keyNode, not
// valNode, per the yaml parser) survive instead of being silently dropped
// -- both fall inside the byte span this function's caller replaces.
// HeadComment is deliberately NOT carried over: it lives on lines strictly
// before the span being replaced, so it is already preserved verbatim in
// the untouched prefix bytes; re-adding it here would duplicate it.
//
// LineComment is appended manually (to the rendered fragment's first line)
// rather than set on the synthetic key node and left to the dumper: the
// dumper drops a key's LineComment when the value renders in flow style on
// the same line (verified against go.yaml.in/yaml/v4 v4.0.0-rc.6) --
// exactly the style dependencies.mcp/dependencies.apm normally use.
// FootComment does not have that failure mode, so it is still set directly
// on the synthetic key and left to the dumper.
func renderKeyValue(keyNode *yaml.Node, valNode *yaml.Node, indent int) ([]byte, error) {
	syntheticKey := &yaml.Node{
		Kind:        yaml.ScalarNode,
		Value:       keyNode.Value,
		Tag:         "!!str",
		FootComment: keyNode.FootComment,
	}
	pair := &yaml.Node{
		Kind:    yaml.MappingNode,
		Tag:     "!!map",
		Content: []*yaml.Node{syntheticKey, valNode},
	}
	rendered, err := SafeDump(pair)
	if err != nil {
		return nil, err
	}
	text := strings.TrimRight(string(rendered), "\n")
	lines := strings.Split(text, "\n")
	if keyNode.LineComment != "" && len(lines) > 0 {
		lines[0] += " " + keyNode.LineComment
	}
	prefix := strings.Repeat(" ", indent)
	for i, l := range lines {
		if l == "" {
			continue
		}
		lines[i] = prefix + l
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}
