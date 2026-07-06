package manifest

import (
	"fmt"
	"sort"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/yamlcore"
)

// RemovePackagesFromManifest removes every dependencies.apm /
// devDependencies.apm entry in doc whose identity
// (DependencyReference.IdentityKey(), which ignores git ref and alias per
// un-011) is present in identities, splicing the removal into src as a
// byte-level edit (yamlcore.SpliceSequenceElement) so every untouched entry
// -- including its hand-authored formatting/comments -- and every unrelated
// part of the document survive byte-exact (un-022).
//
// Also implements un-021's devDependencies-wrapper cleanup (cli.py:140-162's
// intent, adapted for this byte-splice architecture -- see the "deviation
// from a literal cli.py transliteration" note below):
//   - dependencies.apm always stays present, even when every entry is
//     removed: it is rendered as an inline "apm: []" rather than deleted
//     (Python's equivalent never deletes the prod key, only ever
//     reassigns it to an empty list).
//   - devDependencies.apm is deleted outright when every entry is removed.
//   - if devDependencies then has no key other than apm, the whole
//     devDependencies key is deleted too, leaving no empty shell behind.
//
// Deviation from a literal cli.py:151-156 transliteration: Python guards
// that last step with "had_dev_section" (only delete the wrapper if
// devDependencies didn't exist in the file before this call), because
// Python's implementation always synthesizes an in-memory devDependencies.apm
// = [] scaffold up front to scan prod+dev uniformly, and must remember to
// clean that scaffold up if it turns out to have been unused. This
// implementation never synthesizes anything -- planSequenceRemoval only ever
// matches real, on-disk entries -- so devPlan is non-nil here only when
// devDependencies.apm already existed AND had a real match; "existed only as
// this call's synthesized scaffold" can't occur. Given that, always deleting
// a devDependencies wrapper left with nothing but an empty apm list is both
// safe -- leaving "devDependencies:" behind with only its apm key removed
// would re-parse as null, not an empty mapping, since this is a byte-splice
// edit rather than Python's from-scratch dict re-serialization -- and
// strictly cleaner (no empty-shell wrapper survives at all).
//
// Returns the new document bytes and the subset of identities that were
// actually found (and removed) in either sequence, so callers can report
// "not found" for the rest (un-013).
func RemovePackagesFromManifest(src []byte, doc *yaml.Node, identities map[string]bool) (out []byte, removed map[string]bool, err error) {
	removed = map[string]bool{}
	if len(identities) == 0 {
		return src, removed, nil
	}

	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return src, removed, nil
	}

	prodPlan := planSequenceRemoval(root, []string{"dependencies", "apm"}, identities, removed)
	devPlan := planSequenceRemoval(root, []string{"devDependencies", "apm"}, identities, removed)

	type pendingOp struct {
		line int
		run  func(cur []byte) ([]byte, error)
	}
	var ops []pendingOp

	if prodPlan != nil {
		plan := prodPlan
		ops = append(ops, pendingOp{
			line: plan.keyLine,
			run: func(cur []byte) ([]byte, error) {
				return applyProdRemoval(cur, doc, plan)
			},
		})
	}
	if devPlan != nil {
		plan := devPlan
		deleteWholeWrapper := len(plan.indices) == plan.total && devMappingHasOnlyApmKey(root)
		ops = append(ops, pendingOp{
			line: plan.keyLine,
			run: func(cur []byte) ([]byte, error) {
				return applyDevRemoval(cur, doc, plan, deleteWholeWrapper)
			},
		})
	}

	// Process the physically LATER section of the document first: each
	// splice op is computed against doc's ORIGINAL (never re-parsed) Line
	// numbers, so once an earlier op shrinks the byte stream, any op
	// targeting content further down the file would be looking up stale
	// line offsets. Sections whose start line is larger are processed
	// first, so every subsequent op still targets untouched, byte-identical
	// content up to its own start line.
	sort.Slice(ops, func(i, j int) bool { return ops[i].line > ops[j].line })

	cur := src
	for _, op := range ops {
		cur, err = op.run(cur)
		if err != nil {
			return nil, nil, err
		}
	}
	return cur, removed, nil
}

// seqRemovalPlan describes which elements of one apm.yml dependency sequence
// (dependencies.apm or devDependencies.apm) are to be removed.
type seqRemovalPlan struct {
	keyLine int   // source line of the sequence's own key (e.g. "apm:")
	total   int   // total elements currently in the sequence
	indices []int // matched element indices, sorted descending
}

// planSequenceRemoval walks path (e.g. ["dependencies","apm"]) within root,
// parses each of its entries into a DependencyReference (reusing the same
// dict/string parsers validateDepBlock's "apm" branch uses), and collects
// the indices whose IdentityKey() is in identities -- marking each matched
// identity as found in removed. Returns nil when the sequence doesn't
// exist, isn't a block-style sequence, or has no matches (nothing to do).
func planSequenceRemoval(root *yaml.Node, path []string, identities map[string]bool, removed map[string]bool) *seqRemovalPlan {
	cur := root
	for i, key := range path {
		idx := findKeyIndex(cur, key)
		if idx == -1 {
			return nil
		}
		val := cur.Content[idx+1]
		if i == len(path)-1 {
			if val.Kind != yaml.SequenceNode {
				return nil
			}
			plan := &seqRemovalPlan{keyLine: cur.Content[idx].Line, total: len(val.Content)}
			for elIdx, entry := range val.Content {
				ref, perr := parseApmSeqEntry(entry, elIdx)
				if perr != nil {
					// Malformed/unparseable entry: leave it alone rather
					// than fail the whole removal.
					continue
				}
				idKey := ref.IdentityKey()
				if idKey == "" || !identities[idKey] {
					continue
				}
				plan.indices = append(plan.indices, elIdx)
				removed[idKey] = true
			}
			if len(plan.indices) == 0 {
				return nil
			}
			sort.Sort(sort.Reverse(sort.IntSlice(plan.indices)))
			return plan
		}
		if val.Kind != yaml.MappingNode {
			return nil
		}
		cur = val
	}
	return nil
}

func parseApmSeqEntry(entry *yaml.Node, idx int) (*DependencyReference, error) {
	switch entry.Kind {
	case yaml.MappingNode:
		return ParseDepDict(entry, idx)
	case yaml.ScalarNode:
		return ParseDepString(entry.Value)
	default:
		return nil, fmt.Errorf("dependency entry %d: unsupported node kind", idx)
	}
}

func findKeyIndex(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// devMappingHasOnlyApmKey reports whether root's devDependencies mapping
// contains exactly one key ("apm") and nothing else.
func devMappingHasOnlyApmKey(root *yaml.Node) bool {
	idx := findKeyIndex(root, "devDependencies")
	if idx == -1 {
		return false
	}
	val := root.Content[idx+1]
	if val.Kind != yaml.MappingNode {
		return false
	}
	return len(val.Content) == 2 && val.Content[0].Value == "apm"
}

func applyProdRemoval(src []byte, doc *yaml.Node, plan *seqRemovalPlan) ([]byte, error) {
	path := []string{"dependencies", "apm"}
	if len(plan.indices) == plan.total {
		out, ok, err := yamlcore.ReplaceSequenceWithEmptyFlow(src, doc, path)
		if err != nil {
			return nil, fmt.Errorf("empty dependencies.apm: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("dependencies.apm: unexpected document shape while emptying")
		}
		return out, nil
	}
	return removeSeqIndices(src, doc, path, plan.indices)
}

func applyDevRemoval(src []byte, doc *yaml.Node, plan *seqRemovalPlan, deleteWholeWrapper bool) ([]byte, error) {
	if len(plan.indices) == plan.total {
		path := []string{"devDependencies", "apm"}
		if deleteWholeWrapper {
			path = []string{"devDependencies"}
		}
		out, ok, err := yamlcore.RemoveMappingKey(src, doc, path)
		if err != nil {
			return nil, fmt.Errorf("remove %v: %w", path, err)
		}
		if !ok {
			return nil, fmt.Errorf("%v: unexpected document shape while removing", path)
		}
		return out, nil
	}
	return removeSeqIndices(src, doc, []string{"devDependencies", "apm"}, plan.indices)
}

// removeSeqIndices removes indices (already sorted descending) one at a
// time via SpliceSequenceElement, feeding each op's output into the next --
// safe because descending order always operates on content still located
// further down the file than anything already removed.
func removeSeqIndices(src []byte, doc *yaml.Node, path []string, indices []int) ([]byte, error) {
	cur := src
	for _, idx := range indices {
		out, ok, err := yamlcore.SpliceSequenceElement(cur, doc, path, yamlcore.SeqRemove, idx, nil)
		if err != nil {
			return nil, fmt.Errorf("remove %v[%d]: %w", path, idx, err)
		}
		if !ok {
			return nil, fmt.Errorf("%v[%d]: unexpected document shape while removing", path, idx)
		}
		cur = out
	}
	return cur, nil
}
