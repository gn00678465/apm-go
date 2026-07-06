package yamlcore

import (
	"bytes"

	"go.yaml.in/yaml/v4"
)

// RemoveMappingKey deletes the mapping key-value pair located at the dotted
// mapping key path within doc (e.g. []string{"devDependencies", "apm"}),
// splicing the deletion into the original src bytes so every other byte of
// src is preserved verbatim -- reusing the exact same span rule
// PatchMappingPath's replace path uses (patch.go's spanEndOffset/
// lineStartOffset): the key's own span runs from the start of its key's
// line through the byte immediately before the next sibling key at that
// level (or through end of the enclosing mapping/document when it's the
// last key).
//
// This is the "delete a whole key" counterpart to PatchMappingPath (replace
// a key's value) and SpliceSequenceElement (add/remove/set one sequence
// element) -- needed by uninstall's un-021 devDependencies-wrapper cleanup,
// which must remove an entire mapping key (not just splice a sequence
// element), while leaving unrelated sibling keys and their formatting
// byte-exact.
//
// doc must be the unmutated *yaml.Node tree produced by SafeLoad(src).
// Returns ok=false (err=nil) when the path doesn't resolve to an existing
// mapping key (already absent, an intermediate segment isn't a block
// mapping, or path is empty) -- callers should treat that as a no-op.
func RemoveMappingKey(src []byte, doc *yaml.Node, path []string) (out []byte, ok bool, err error) {
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
		if idx == -1 || cur.Content[idx].Line == 0 {
			return nil, false, nil
		}
		isLast := i == len(path)-1
		if isLast {
			frames = append(frames, pathFrame{mapping: cur, keyIdx: idx})
			return removeKeySpan(src, frames)
		}
		valNode := cur.Content[idx+1]
		if valNode.Kind != yaml.MappingNode {
			return nil, false, nil
		}
		frames = append(frames, pathFrame{mapping: cur, keyIdx: idx})
		cur = valNode
	}
	return nil, false, nil
}

func removeKeySpan(src []byte, frames []pathFrame) ([]byte, bool, error) {
	level := len(frames) - 1
	f := frames[level]
	keyNode := f.mapping.Content[f.keyIdx]

	start := lineStartOffset(src, keyNode.Line)
	end := spanEndOffset(src, frames, level)
	if end < start {
		return nil, false, nil
	}

	var buf bytes.Buffer
	buf.Write(src[:start])
	buf.Write(src[end:])
	return buf.Bytes(), true, nil
}
