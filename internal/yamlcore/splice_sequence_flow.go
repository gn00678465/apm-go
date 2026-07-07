package yamlcore

import "go.yaml.in/yaml/v4"

// RebuildSequenceValueDropping re-renders the entire sequence value located
// at the dotted mapping key path within doc (e.g. []string{"dependencies",
// "apm"}), dropping the given element indices, and splices the result into
// src via PatchMappingPath.
//
// Unlike SpliceSequenceElement / ReplaceSequenceWithEmptyFlow, this accepts
// a sequence of ANY style -- including flow ("apm: [a, b]") -- which those
// two element-level/byte-span primitives intentionally reject (see their own
// doc comments: "callers must fall back to a full-value replace"). This is
// that fallback: a flow-style dependencies.apm/dependencies.mcp otherwise
// breaks uninstall's manifest removal entirely (reported bug: "unexpected
// document shape while emptying").
//
// doc must be the *yaml.Node tree produced by SafeLoad(src). This mutates
// the located sequence node's Content in place (dropping drop's indices)
// before delegating to PatchMappingPath, which re-renders just that key's
// value span -- every other byte of src, including sibling keys and their
// formatting, survives untouched. Because this goes through
// PatchMappingPath's whole-value re-encode rather than SpliceSequenceElement's
// per-element byte splice, the *sequence's own* hand formatting (e.g.
// multi-line flow wrapping) is not preserved -- only PatchMappingPath's
// guarantee applies, same as any other PatchMappingPath caller. The
// sequence's existing Style is preserved on the mutated node, so a flow
// sequence re-renders flow (and an empty result renders "[]", never a bare
// "key:" that would re-parse as null).
//
// drop may be empty (keep every element) or cover every index (rebuild to
// an empty sequence).
//
// Returns ok=false (err=nil) when path does not resolve to an existing
// mapping key whose value is a SequenceNode.
func RebuildSequenceValueDropping(src []byte, doc *yaml.Node, path []string, drop []int) (out []byte, ok bool, err error) {
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

	cur := root
	var seq *yaml.Node
	for i, key := range path {
		idx := findMappingKeyIndex(cur, key)
		if idx == -1 || cur.Content[idx].Line == 0 {
			return nil, false, nil
		}
		valNode := cur.Content[idx+1]
		if i == len(path)-1 {
			if valNode.Kind != yaml.SequenceNode {
				return nil, false, nil
			}
			seq = valNode
			break
		}
		if valNode.Kind != yaml.MappingNode {
			return nil, false, nil
		}
		cur = valNode
	}
	if seq == nil {
		return nil, false, nil
	}

	if len(drop) > 0 {
		dropSet := make(map[int]bool, len(drop))
		for _, i := range drop {
			dropSet[i] = true
		}
		keep := make([]*yaml.Node, 0, len(seq.Content))
		for i, el := range seq.Content {
			if dropSet[i] {
				continue
			}
			keep = append(keep, el)
		}
		seq.Content = keep
	}

	return PatchMappingPath(src, doc, path)
}
