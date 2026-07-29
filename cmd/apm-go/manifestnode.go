package main

import (
	"sort"
	"strings"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/manifest"
)

// manifestSpec bundles the metadata and target selection needed to build a
// fresh apm.yml node tree in semantic key order (R2). Plugin toggles the
// devDependencies section plugin init inserts between includes and scripts
// (07-29-plugin-init's E1 dependency on this function).
type manifestSpec struct {
	Name        string
	Version     string
	Description string
	Author      string
	Targets     []string
	Plugin      bool
}

// targetsCommentLines returns the three header lines placed above the
// targets: key (R2.2). The "Accepted values" line is derived from
// manifest.SupportedTargets (sorted) rather than a fourth independent
// literal (R2.3) -- replacing manifest.SupportedTargets in a test changes
// this output (AC26).
func targetsCommentLines() []string {
	sorted := append([]string(nil), manifest.SupportedTargets...)
	sort.Strings(sorted)
	return []string{
		"Which agent platforms to deploy to.",
		"Resolution order: --target flag > this field > auto-detect from filesystem.",
		"Accepted values: " + strings.Join(sorted, ", "),
	}
}

// targetsSkeletonComment is the commented-out targets: block inserted after
// author when no target was selected (R2.4), design.md §2: the same three
// header lines plus two commented-out example lines.
func targetsSkeletonComment() string {
	lines := append(targetsCommentLines(), "targets:", "  - claude")
	return strings.Join(lines, "\n")
}

// buildManifestNode builds a fresh apm.yml *yaml.Node tree in semantic key
// order: name -> version -> description -> author -> targets ->
// dependencies -> includes -> [devDependencies] -> scripts (R2.1). Callers
// must round-trip the result through yamlcore.SafeDump -> SafeLoad ->
// manifest.ParseManifest before writing it to disk, so the validated bytes
// are exactly the bytes written (design.md §2).
func buildManifestNode(spec manifestSpec) *yamllib.Node {
	pairs := []*yamllib.Node{
		manifestKeyNode("name"), manifestStrNode(spec.Name),
		manifestKeyNode("version"), manifestStrNode(spec.Version),
		manifestKeyNode("description"), manifestStrNode(spec.Description),
		manifestKeyNode("author"), manifestStrNode(spec.Author),
	}

	depsKey := manifestKeyNode("dependencies")
	if len(spec.Targets) > 0 {
		targetsKey := manifestKeyNode("targets")
		targetsKey.HeadComment = strings.Join(targetsCommentLines(), "\n")
		pairs = append(pairs, targetsKey, manifestSeqNode(spec.Targets))
	} else {
		// Deliberate deviation from design.md §2's literal suggestion of a
		// FootComment on author (codex-audit low finding): a FootComment
		// (on either the author key or its value -- both were probed)
		// forces the yaml dumper to emit a blank line between the comment
		// block and the next key, breaking AC7's verbatim five-line
		// skeleton. A HeadComment on the following key (dependencies, the
		// only key ever adjacent to author in this fixed order) has no
		// such artifact and produces the exact required bytes. Tracked so
		// this isn't later read as an oversight: if a future key is ever
		// inserted between author and dependencies, this HeadComment must
		// move with it.
		depsKey.HeadComment = targetsSkeletonComment()
	}

	pairs = append(pairs, depsKey, manifestDependenciesNode())
	pairs = append(pairs, manifestKeyNode("includes"), manifestStrNode("auto"))

	if spec.Plugin {
		pairs = append(pairs, manifestKeyNode("devDependencies"), manifestDevDependenciesNode())
	}

	pairs = append(pairs, manifestKeyNode("scripts"), manifestEmptyFlowMapNode())

	return &yamllib.Node{Kind: yamllib.MappingNode, Tag: "!!map", Content: pairs}
}

func manifestStrNode(v string) *yamllib.Node {
	return &yamllib.Node{Kind: yamllib.ScalarNode, Tag: "!!str", Value: v}
}

func manifestKeyNode(v string) *yamllib.Node {
	return &yamllib.Node{Kind: yamllib.ScalarNode, Tag: "!!str", Value: v}
}

func manifestSeqNode(items []string) *yamllib.Node {
	n := &yamllib.Node{Kind: yamllib.SequenceNode, Tag: "!!seq"}
	for _, item := range items {
		n.Content = append(n.Content, manifestStrNode(item))
	}
	return n
}

func manifestEmptyFlowSeqNode() *yamllib.Node {
	return &yamllib.Node{Kind: yamllib.SequenceNode, Tag: "!!seq", Style: yamllib.FlowStyle}
}

func manifestEmptyFlowMapNode() *yamllib.Node {
	return &yamllib.Node{Kind: yamllib.MappingNode, Tag: "!!map", Style: yamllib.FlowStyle}
}

func manifestDependenciesNode() *yamllib.Node {
	return &yamllib.Node{
		Kind: yamllib.MappingNode,
		Tag:  "!!map",
		Content: []*yamllib.Node{
			manifestKeyNode("apm"), manifestEmptyFlowSeqNode(),
			manifestKeyNode("mcp"), manifestEmptyFlowSeqNode(),
		},
	}
}

func manifestDevDependenciesNode() *yamllib.Node {
	return &yamllib.Node{
		Kind: yamllib.MappingNode,
		Tag:  "!!map",
		Content: []*yamllib.Node{
			manifestKeyNode("apm"), manifestEmptyFlowSeqNode(),
		},
	}
}
