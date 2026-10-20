package main

import (
	"strings"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/manifest"
)

// Oracle 0.33.0 rewrites the apm.yml entry of a package the command line names
// again (install/package_resolution.py:345-378, commands/install.py:432,482-503),
// whatever its ref, and prints "(updated ref in apm.yml)"
// (core/command_logger.py:289-291). It replaces the whole entry with the one the
// command line gives, so skills, alias, targets and the spelling are lost
// (measured, issue #156). apm-go changes the ref alone (issue #156 comment
// 6290306546, item 1).

// declaredEntryUpdated reports whether the Oracle's rewrite covers the declared
// dependency d: a registry, marketplace or parent entry keeps main's behavior.
func declaredEntryUpdated(d *manifest.DependencyReference) bool {
	return d != nil && !d.IsParent && d.Source != "registry" && d.Source != "marketplace"
}

// declaredRefRewritable reports whether the ref of the declared dependency d is
// the one a command-line ref replaces. A local package has no ref to move.
func declaredRefRewritable(d *manifest.DependencyReference) bool {
	return declaredEntryUpdated(d) && !d.IsLocal && d.LocalSourcePath == ""
}

// refOfDepString returns the ref of a git dependency string, and false for a
// string with no git identity (a local path, a string that does not parse).
func refOfDepString(s string) (string, bool) {
	ref, err := manifest.ParseDepString(s)
	if err != nil || ref.IsLocal || ref.IsParent {
		return "", false
	}
	return ref.Reference, true
}

// withDepStringRef returns value with its trailing "#ref" replaced by ref, or
// removed when ref is empty. The part before the "#" keeps its spelling.
func withDepStringRef(value, ref string) string {
	if idx := strings.LastIndex(value, "#"); idx >= 0 {
		value = value[:idx]
	}
	if ref == "" {
		return value
	}
	return value + "#" + ref
}

// setEntryRef sets the ref of apmSeq.Content[idx], an entry that declares a git
// dependency, and leaves every other field, the comments and the spelling of
// the repository as they are. A scalar carries the ref after "#"; an object
// carries it in `ref:`, else after the "#" of its `git:` value, else gets a
// `ref:` after `git:`. A ref that is already the entry's is not written.
func setEntryRef(apmSeq *yamllib.Node, idx int, ref string) {
	entry := apmSeq.Content[idx]
	switch entry.Kind {
	case yamllib.ScalarNode:
		if current, ok := refOfDepString(entry.Value); !ok || current == ref {
			return
		}
		entry.Value = withDepStringRef(entry.Value, ref)
	case yamllib.MappingNode:
		setObjectEntryRef(entry, ref)
	}
}

// setObjectEntryRef is setEntryRef for an object entry. The source is its
// `git:` value, or the `name:` value of the legacy `{name: owner/repo}` form,
// which manifest.ParseDepDict reads as a git shorthand and for which it never
// reads `ref:` (a `path:` beside `name:` is ignored too). A `path:` object,
// `id:` and marketplace entry has no ref to move here.
func setObjectEntryRef(entry *yamllib.Node, ref string) {
	srcIdx, refIdx := -1, -1
	hasOther := false
	for j := 0; j < len(entry.Content)-1; j += 2 {
		switch entry.Content[j].Value {
		case "git":
			srcIdx = j
		case "ref":
			refIdx = j
		case "id", "marketplace":
			hasOther = true
		}
	}
	useRefKey := true
	if srcIdx < 0 {
		useRefKey = false
		for j := 0; j < len(entry.Content)-1; j += 2 {
			if entry.Content[j].Value == "name" && !hasOther {
				srcIdx = j
			}
		}
	}
	if srcIdx < 0 {
		return
	}
	src := entry.Content[srcIdx+1]
	if _, ok := refOfDepString(src.Value); !ok {
		return
	}
	// manifest.ParseDepDict lets a non-empty `ref:` win over the "#ref" of the
	// git value.
	refKey := useRefKey && refIdx >= 0 && entry.Content[refIdx+1].Value != ""
	var current string
	if refKey {
		current = entry.Content[refIdx+1].Value
	} else {
		current, _ = refOfDepString(src.Value)
	}
	if current == ref {
		return
	}
	switch {
	case ref == "":
		src.Value = withDepStringRef(src.Value, "")
		if useRefKey && refIdx >= 0 {
			entry.Content = append(entry.Content[:refIdx], entry.Content[refIdx+2:]...)
		}
	case refKey:
		setScalarValue(entry.Content[refIdx+1], ref)
	case strings.Contains(src.Value, "#") || !useRefKey:
		src.Value = withDepStringRef(src.Value, ref)
	case refIdx >= 0:
		setScalarValue(entry.Content[refIdx+1], ref)
	default:
		pair := []*yamllib.Node{
			{Kind: yamllib.ScalarNode, Value: "ref", Tag: "!!str"},
			{Kind: yamllib.ScalarNode, Value: ref, Tag: "!!str"},
		}
		rest := append([]*yamllib.Node{}, entry.Content[srcIdx+2:]...)
		entry.Content = append(append(entry.Content[:srcIdx+2], pair...), rest...)
	}
}

// setScalarValue makes n a plain string scalar holding value. manifest.ParseDepDict
// reads only scalar values, so a `ref: []` or `ref: {}` is no ref; writing over it
// has to drop the children too, and keeps the comments.
func setScalarValue(n *yamllib.Node, value string) {
	n.Kind, n.Content, n.Value, n.Tag, n.Style = yamllib.ScalarNode, nil, value, "!!str", 0
}
