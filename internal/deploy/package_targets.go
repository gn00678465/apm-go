package deploy

import (
	"slices"
	"strings"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
	"go.yaml.in/yaml/v4"
)

// packageTargets is the set of targets a dependency's own apm.yml restricts
// it to, sorted and unique, or nil when it sets none: the Oracle's
// canonical_targets (models/apm_package.py:571-600,736-750) as
// resolve_effective_package_targets reads them (install/target_filter.py:
// 113-115). A plural `targets:` keeps each name as written once stripped
// (core/apm_yml.py:128-163), so `vscode` there is not copilot; a singular
// `target:` is lowercased, split at commas and its aliases resolved
// (core/target_detection.py:589-700, core/target_catalog.py:269-273). `all`
// anywhere restricts nothing. Values the Oracle refuses are left to the
// apm.yml validation of the resolve.
func packageTargets(data []byte) []string {
	if data == nil {
		return nil
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	root := doc.Content[0]
	var plural, singular *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		switch root.Content[i].Value {
		case "targets":
			plural = root.Content[i+1]
		case "target":
			singular = root.Content[i+1]
		}
	}
	var names []string
	switch {
	case plural != nil && singular != nil:
		return nil
	case plural != nil:
		for _, n := range scalarItems(plural) {
			if n = pyStrip(n); n != "" {
				names = append(names, n)
			}
		}
	case singular != nil:
		for _, item := range scalarItems(singular) {
			parts := []string{item}
			if singular.Kind == yaml.ScalarNode {
				parts = strings.Split(item, ",")
			}
			for _, n := range parts {
				n = strings.ToLower(pyStrip(n))
				if alias, ok := manifest.TargetAliases[n]; ok {
					n = alias
				}
				if n != "" {
					names = append(names, n)
				}
			}
		}
	}
	if len(names) == 0 || slices.Contains(names, "all") {
		return nil
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// scalarItems is the scalar values of a node that is a scalar or a list; a
// null and anything else give none.
func scalarItems(n *yaml.Node) []string {
	switch {
	case n.Kind == yaml.ScalarNode && n.Tag != "!!null":
		return []string{n.Value}
	case n.Kind == yaml.SequenceNode:
		var out []string
		for _, item := range n.Content {
			if item.Kind == yaml.ScalarNode && item.Tag != "!!null" {
				out = append(out, item.Value)
			}
		}
		return out
	}
	return nil
}
