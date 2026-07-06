package yamlcore

import "go.yaml.in/yaml/v4"

// ReplaceSequenceWithEmptyFlow replaces the entire value of the block-style
// sequence located at the dotted mapping key path within doc (e.g.
// []string{"dependencies", "apm"}) with an inline empty flow sequence
// ("key: []"), splicing the change into src the same way PatchMappingPath's
// replace path does -- every other byte of src, including sibling keys and
// their formatting, survives untouched.
//
// This exists for callers like uninstall's dependencies.apm cleanup that
// must keep a key present as an (now empty) list rather than delete it:
// removing every element one at a time via SpliceSequenceElement(...,
// SeqRemove, ...) would otherwise leave a bare "key:" line, which re-parses
// as null rather than an empty list, breaking any code that requires the
// key to still be a sequence.
//
// doc must be the unmutated *yaml.Node tree produced by SafeLoad(src).
// Returns ok=false (err=nil) when the path doesn't resolve to a block-style
// sequence (flow style, wrong node kind, or the path doesn't resolve at all).
func ReplaceSequenceWithEmptyFlow(src []byte, doc *yaml.Node, path []string) (out []byte, ok bool, err error) {
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
		valNode := cur.Content[idx+1]
		isLast := i == len(path)-1
		if isLast {
			if valNode.Kind != yaml.SequenceNode || valNode.Style&yaml.FlowStyle != 0 {
				return nil, false, nil
			}
			frames = append(frames, pathFrame{mapping: cur, keyIdx: idx})
			empty := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
			return replaceValueSpan(src, frames, empty)
		}
		if valNode.Kind != yaml.MappingNode {
			return nil, false, nil
		}
		frames = append(frames, pathFrame{mapping: cur, keyIdx: idx})
		cur = valNode
	}
	return nil, false, nil
}
