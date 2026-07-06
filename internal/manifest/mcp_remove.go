package manifest

import (
	"fmt"
	"sort"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/yamlcore"
)

// RemoveMCPServersFromManifest removes every dependencies.mcp /
// devDependencies.mcp entry in doc whose Name (ParseMCPEntry's Name --
// a bare-string entry's own value, or a dict entry's "name" field) is
// present in serverNames, splicing the removal into src as a byte-level edit
// (yamlcore.SpliceSequenceElement) so every untouched entry -- including its
// hand-authored formatting/comments -- and every unrelated part of the
// document survive byte-exact. This is un-064's standalone-MCP counterpart to
// RemovePackagesFromManifest (un-020~022), symmetric with upsertMCPEntry's
// insertion path (mcpinstall.go).
//
// Mirrors RemovePackagesFromManifest's empty-section handling exactly:
//   - dependencies.mcp always stays present, even when every entry is
//     removed: it is rendered as an inline "mcp: []" rather than deleted --
//     `apm-go init` itself always writes an empty "mcp: []" under
//     dependencies, so the prod key is never deleted, only ever emptied.
//   - devDependencies.mcp is deleted outright when every entry is removed.
//   - if devDependencies then has no key other than mcp, the whole
//     devDependencies key is deleted too, leaving no empty shell behind.
//
// Returns the new document bytes and the subset of serverNames that were
// actually found (and removed) in either sequence, so callers can report
// "not found" for the rest.
func RemoveMCPServersFromManifest(src []byte, doc *yaml.Node, serverNames map[string]bool) (out []byte, removed map[string]bool, err error) {
	removed = map[string]bool{}
	if len(serverNames) == 0 {
		return src, removed, nil
	}

	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return src, removed, nil
	}

	prodPlan := planMCPSeqRemoval(root, []string{"dependencies", "mcp"}, serverNames, removed)
	devPlan := planMCPSeqRemoval(root, []string{"devDependencies", "mcp"}, serverNames, removed)

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
				return applyProdMCPRemoval(cur, doc, plan)
			},
		})
	}
	if devPlan != nil {
		plan := devPlan
		deleteWholeWrapper := len(plan.indices) == plan.total && devMappingHasOnlyKey(root, "mcp")
		ops = append(ops, pendingOp{
			line: plan.keyLine,
			run: func(cur []byte) ([]byte, error) {
				return applyDevMCPRemoval(cur, doc, plan, deleteWholeWrapper)
			},
		})
	}

	// Same ordering rule as RemovePackagesFromManifest: process the
	// physically LATER section of the document first, since each splice op
	// is computed against doc's ORIGINAL Line numbers.
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

// planMCPSeqRemoval walks path (e.g. ["dependencies","mcp"]) within root,
// parses each of its entries via ParseMCPEntry (handles both bare-string and
// dict-form entries), and collects the indices whose Name is in serverNames
// -- marking each matched name as found in removed. Returns nil when the
// sequence doesn't exist, isn't a block-style sequence, or has no matches.
func planMCPSeqRemoval(root *yaml.Node, path []string, serverNames map[string]bool, removed map[string]bool) *seqRemovalPlan {
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
				m, perr := ParseMCPEntry(entry)
				if perr != nil {
					// Malformed/unparseable entry: leave it alone rather
					// than fail the whole removal.
					continue
				}
				if m.Name == "" || !serverNames[m.Name] {
					continue
				}
				plan.indices = append(plan.indices, elIdx)
				removed[m.Name] = true
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

// devMappingHasOnlyKey reports whether root's devDependencies mapping
// contains exactly one key (key) and nothing else.
func devMappingHasOnlyKey(root *yaml.Node, key string) bool {
	idx := findKeyIndex(root, "devDependencies")
	if idx == -1 {
		return false
	}
	val := root.Content[idx+1]
	if val.Kind != yaml.MappingNode {
		return false
	}
	return len(val.Content) == 2 && val.Content[0].Value == key
}

func applyProdMCPRemoval(src []byte, doc *yaml.Node, plan *seqRemovalPlan) ([]byte, error) {
	path := []string{"dependencies", "mcp"}
	if len(plan.indices) == plan.total {
		out, ok, err := yamlcore.ReplaceSequenceWithEmptyFlow(src, doc, path)
		if err != nil {
			return nil, fmt.Errorf("empty dependencies.mcp: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("dependencies.mcp: unexpected document shape while emptying")
		}
		return out, nil
	}
	return removeSeqIndices(src, doc, path, plan.indices)
}

func applyDevMCPRemoval(src []byte, doc *yaml.Node, plan *seqRemovalPlan, deleteWholeWrapper bool) ([]byte, error) {
	if len(plan.indices) == plan.total {
		path := []string{"devDependencies", "mcp"}
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
	return removeSeqIndices(src, doc, []string{"devDependencies", "mcp"}, plan.indices)
}
