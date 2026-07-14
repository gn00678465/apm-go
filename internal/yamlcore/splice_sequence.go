package yamlcore

import (
	"bytes"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v4"
)

// SeqOp identifies which element-level edit SpliceSequenceElement performs.
type SeqOp int

const (
	// SeqAdd inserts newNode as a new element at the tail of the sequence.
	// idx is ignored.
	SeqAdd SeqOp = iota
	// SeqRemove deletes the element at idx. newNode is ignored.
	SeqRemove
	// SeqSet replaces the element at idx with newNode.
	SeqSet
)

// SpliceSequenceElement performs a surgical, element-level edit (add/
// remove/set) of a block-style YAML sequence located at the dotted mapping
// key path within doc (e.g. []string{"marketplace", "packages"}), splicing
// the change into the original src bytes so every other byte of src is
// preserved verbatim -- including hand-authored comments and formatting on
// every untouched element, per-element inline/leading comments, and any
// unrelated content elsewhere in the document.
//
// This is deliberately narrower than yamlcore.PatchMappingPath, which only
// knows how to replace an entire mapping key's value span: PatchMappingPath
// re-serializing marketplace.packages as a whole would rewrite every
// existing entry's hand formatting, which `apm marketplace package
// add/remove/set` must not do (see design.md's "陣列元素編輯策略").
// SpliceSequenceElement instead locates the byte span of just the one
// element being touched, using each element node's parsed Line/Column.
//
// doc must be the unmutated *yaml.Node tree produced by SafeLoad(src) --
// unlike PatchMappingPath, callers must NOT pre-append newNode into the
// sequence's Content themselves; SpliceSequenceElement renders newNode
// itself (via SafeDump) when splicing it in.
//
// Element span rule (spike-verified against go.yaml.in/yaml/v4
// v4.0.0-rc.6): element i's span starts at the first byte of element i's
// own leading "-" marker line -- usually element i's own parsed Line, but
// one line earlier when the dash is written on its own line (see dashLine)
// -- and ends at element i+1's own dash line in the same sense, or, for the
// last element, at the first following line that is blank, comment-only,
// or indented no deeper than the sequence's own dash indent (see
// elementContentEnd). That last-element rule deliberately does NOT reuse
// spanEndOffset's "next sibling key at any enclosing level" walk:
// spanEndOffset is correct for PatchMappingPath, which replaces a whole
// mapping value and so may legitimately consume a trailing blank/comment
// region right up to the next key, but a removed/replaced *element* must
// never reach past its own content, or a comment/blank line that actually
// leads the next sibling key gets swept away along with it. Two
// consequences follow directly from this line-based, comment-field-agnostic
// boundary:
//   - A comment leading the *first* element (on a line before the element's
//     own dash line) falls outside every element's span, so it survives
//     even when element 0 is removed -- it "belongs to the sequence".
//   - A standalone comment line *between* two elements falls inside the
//     *earlier* element's span (its own end boundary is the later
//     element's dash line), so removing the earlier element also removes
//     that comment -- even though a human reader might associate the
//     comment with the element that follows it. This mirrors ruamel's
//     behavior and is an accepted, tested boundary, not a bug.
//
// Returns ok=false (with err=nil) when the located value is not a
// block-style sequence (flow style, wrong node kind, or the path does not
// resolve at all) -- callers must fall back to a full-value replace (e.g.
// PatchMappingPath replacing the whole sequence, with a warning about lost
// formatting) rather than a full-document re-encode. An out-of-range idx
// for Remove/Set, or a nil newNode for Add/Set, is caller misuse and
// returns a non-nil error instead.
func SpliceSequenceElement(src []byte, doc *yaml.Node, path []string, op SeqOp, idx int, newNode *yaml.Node) (out []byte, ok bool, err error) {
	seq, ok := locateSequence(doc, path)
	if !ok {
		return nil, false, nil
	}

	switch op {
	case SeqAdd:
		return spliceAdd(src, seq, newNode)
	case SeqRemove:
		return spliceRemove(src, seq, idx)
	case SeqSet:
		return spliceSet(src, seq, idx, newNode)
	default:
		return nil, false, fmt.Errorf("yamlcore: unknown SeqOp %d", op)
	}
}

// locateSequence walks doc via path the same way PatchMappingPath does
// (every segment except the last must step through a block mapping key),
// resolving the final segment to a node that must be a block-style
// (non-flow) SequenceNode. It returns that sequence node, and ok=false when
// the path doesn't resolve this way.
func locateSequence(doc *yaml.Node, path []string) (seq *yaml.Node, ok bool) {
	if len(path) == 0 {
		return nil, false
	}
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil, false
	}

	cur := root
	for i, key := range path {
		keyIdx := findMappingKeyIndex(cur, key)
		if keyIdx == -1 || cur.Content[keyIdx].Line == 0 {
			return nil, false
		}
		valNode := cur.Content[keyIdx+1]
		isLast := i == len(path)-1
		if isLast {
			if valNode.Kind != yaml.SequenceNode || valNode.Style&yaml.FlowStyle != 0 {
				return nil, false
			}
			return valNode, true
		}
		if valNode.Kind != yaml.MappingNode {
			return nil, false
		}
		cur = valNode
	}
	return nil, false
}

// elementSpan returns the byte span [start, end) of seq.Content[i] within
// src, per SpliceSequenceElement's doc-comment span rule.
func elementSpan(src []byte, seq *yaml.Node, i int) (start, end int) {
	start = lineStartOffset(src, dashLine(src, seq.Content[i]))
	if i+1 < len(seq.Content) {
		end = lineStartOffset(src, dashLine(src, seq.Content[i+1]))
	} else {
		end = elementContentEnd(src, seq.Content[i])
	}
	return start, end
}

// elementIndent returns the number of spaces before an element's "- "
// marker on its own source line, derived from el.Column: for a block
// sequence item, an element's own content always starts 2 columns past the
// dash ("- "), so indent = Column - 1 (0-base) - 2 = Column - 3. Returns -1
// when el.Column is too small for that to hold (or is 0, an unparsed node).
func elementIndent(el *yaml.Node) int {
	if el.Column < 3 {
		return -1
	}
	return el.Column - 3
}

// lineBytes returns the raw bytes of the given 1-based source line
// (including its line terminator, if present), and whether that line
// exists in src at all -- false once line is past the last line src has.
func lineBytes(src []byte, line int) ([]byte, bool) {
	start := lineStartOffset(src, line)
	if start >= len(src) {
		return nil, false
	}
	end := lineStartOffset(src, line+1)
	return src[start:end], true
}

// lineIsDashAt reports whether source line (1-based) begins with exactly
// indent spaces followed by a '-' byte -- i.e. this line itself carries a
// block sequence item's "-" marker at the given (0-based) column.
func lineIsDashAt(src []byte, line, indent int) bool {
	b, ok := lineBytes(src, line)
	if !ok || len(b) <= indent {
		return false
	}
	for i := 0; i < indent; i++ {
		if b[i] != ' ' {
			return false
		}
	}
	return b[indent] == '-'
}

// dashLine returns the 1-based source line where element el's leading "-"
// marker sits. In the common "- name: foo" style the dash shares el's own
// parsed Line (the mapping's first key). When the dash is written alone on
// its own line instead --
//
//	-
//	  name: foo
//
// the go.yaml.in/yaml parser records el.Line as the *content* line, not
// the dash's, so this walks back one line to find it (confirmed via
// elementIndent(el), the dash's own column). Getting this right matters:
// splicing an element out or in by its byte span must move the dash along
// with it, or the sequence is left with (respectively) an orphaned "-" that
// parses as a phantom null item, or a new item missing its dash entirely.
func dashLine(src []byte, el *yaml.Node) int {
	indent := elementIndent(el)
	if indent < 0 {
		return el.Line
	}
	if lineIsDashAt(src, el.Line, indent) {
		return el.Line
	}
	if el.Line > 1 && lineIsDashAt(src, el.Line-1, indent) {
		return el.Line - 1
	}
	return el.Line
}

// elementContentEnd returns the byte offset where the *last* element in a
// sequence ends: starting right after el's own dash line, it consumes every
// following line that still belongs to el (indented deeper than the
// sequence's own dash indent) and stops at the first line that is blank,
// comment-only, or indented no deeper than that dash indent. That stopping
// line -- and everything from it onward, including any sibling key's own
// leading blank/comment lines -- is left out of the span entirely. Unlike
// elementSpan's inter-element case (bounded by the next element's own dash
// line), there is no next-sibling-element line to bound the last element,
// which is exactly what F1 covers: naively extending to spanEndOffset's
// "next sibling *key*" would sweep up any comment or blank line the
// sequence's own sibling key leans on.
func elementContentEnd(src []byte, el *yaml.Node) int {
	indent := elementIndent(el)
	line := dashLine(src, el) + 1
	for {
		b, ok := lineBytes(src, line)
		if !ok {
			return len(src)
		}
		trimmed := bytes.TrimRight(b, "\r\n")
		stripped := bytes.TrimLeft(trimmed, " ")
		lineIndent := len(trimmed) - len(stripped)
		if len(stripped) == 0 || stripped[0] == '#' || lineIndent <= indent {
			return lineStartOffset(src, line)
		}
		line++
	}
}

// renderSequenceItem renders item as a single block-sequence entry at the
// given indent (spaces before the "-" marker): "<indent>- key: value\n
// <indent+2>...\n" for a mapping item. It delegates the actual value
// rendering to SafeDump (dumping item as if it were its own document) and
// then prefixes the first rendered line with indent spaces + "- " and every
// continuation line with indent+2 spaces, matching the alignment
// hand-authored apm.yml packages[] entries use.
func renderSequenceItem(item *yaml.Node, indent int) ([]byte, error) {
	rendered, err := SafeDump(item)
	if err != nil {
		return nil, err
	}
	text := strings.TrimRight(string(rendered), "\n")
	lines := strings.Split(text, "\n")
	dashPrefix := strings.Repeat(" ", indent) + "- "
	contPrefix := strings.Repeat(" ", indent+2)
	for i, l := range lines {
		if l == "" {
			continue
		}
		if i == 0 {
			lines[i] = dashPrefix + l
		} else {
			lines[i] = contPrefix + l
		}
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

func spliceAdd(src []byte, seq *yaml.Node, newNode *yaml.Node) ([]byte, bool, error) {
	if len(seq.Content) == 0 {
		// No existing element to derive the indent/dash style from.
		return nil, false, nil
	}
	if newNode == nil {
		return nil, false, fmt.Errorf("yamlcore: SpliceSequenceElement add requires a non-nil newNode")
	}
	indent := elementIndent(seq.Content[0])
	if indent < 0 {
		return nil, false, nil
	}

	fragment, err := renderSequenceItem(newNode, indent)
	if err != nil {
		return nil, false, err
	}
	fragment = matchLineEndings(src, fragment)

	insertAt := elementContentEnd(src, seq.Content[len(seq.Content)-1])

	var buf bytes.Buffer
	buf.Write(src[:insertAt])
	if insertAt == len(src) && insertAt > 0 && src[insertAt-1] != '\n' {
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

func spliceRemove(src []byte, seq *yaml.Node, idx int) ([]byte, bool, error) {
	if idx < 0 || idx >= len(seq.Content) {
		return nil, false, fmt.Errorf("yamlcore: sequence index %d out of range (length %d)", idx, len(seq.Content))
	}
	start, end := elementSpan(src, seq, idx)
	if end < start {
		return nil, false, nil
	}

	var buf bytes.Buffer
	buf.Write(src[:start])
	buf.Write(src[end:])
	return buf.Bytes(), true, nil
}

func spliceSet(src []byte, seq *yaml.Node, idx int, newNode *yaml.Node) ([]byte, bool, error) {
	if idx < 0 || idx >= len(seq.Content) {
		return nil, false, fmt.Errorf("yamlcore: sequence index %d out of range (length %d)", idx, len(seq.Content))
	}
	if newNode == nil {
		return nil, false, fmt.Errorf("yamlcore: SpliceSequenceElement set requires a non-nil newNode")
	}
	indent := elementIndent(seq.Content[idx])
	if indent < 0 {
		return nil, false, nil
	}

	fragment, err := renderSequenceItem(newNode, indent)
	if err != nil {
		return nil, false, err
	}
	fragment = matchLineEndings(src, fragment)

	start, end := elementSpan(src, seq, idx)
	if end < start {
		return nil, false, nil
	}

	var buf bytes.Buffer
	buf.Write(src[:start])
	buf.Write(fragment)
	buf.Write(src[end:])
	return buf.Bytes(), true, nil
}
