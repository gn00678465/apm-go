package main

import (
	"strings"

	yamllib "go.yaml.in/yaml/v4"
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

// oracleManifestTargetNames is the sorted manifest target catalog used by
// the Oracle's apm.yml comment (commands/_helpers.py:718-721 and
// core/target_catalog.py:258-264). It is intentionally broader than
// manifest.SupportedTargets, which is apm-go's adapter/--target whitelist.
var oracleManifestTargetNames = []string{
	"agent-skills", "antigravity", "claude", "codex", "copilot",
	"cursor", "gemini", "grok-build", "kiro", "opencode", "windsurf",
}

// targetsCommentLines returns the three header lines placed above the
// targets: key. The text and accepted-target catalog reproduce the Oracle's
// _create_minimal_apm_yml post-processing byte-for-byte.
func targetsCommentLines() []string {
	return []string{
		"Which agent platforms to deploy to.",
		"Resolution order: --target flag > this field > auto-detect from filesystem.",
		"Accepted values: " + strings.Join(oracleManifestTargetNames, ", "),
	}
}

// targetsSkeletonComment is the commented-out targets: block inserted after
// author when no target was selected. This is the exact Oracle skeleton
// (commands/_helpers.py:731-737), including its two examples.
func targetsSkeletonComment() string {
	return strings.Join([]string{
		"Which agent platforms to deploy to (uncomment to pin):",
		"targets:",
		"  - copilot",
		"  - claude",
	}, "\n")
}

// buildManifestNode builds a fresh apm.yml *yaml.Node tree in semantic key
// order: name -> version -> description -> author -> targets ->
// dependencies -> includes -> [devDependencies] -> scripts (R2.1). Callers
// must round-trip the result through yamlcore.SafeDumpManifest -> SafeLoad ->
// manifest.ParseManifest before writing it to disk, so the validated bytes
// are exactly the bytes written (design.md §2).
func buildManifestNode(spec manifestSpec) *yamllib.Node {
	pairs := []*yamllib.Node{
		manifestKeyNode("name"), manifestStrNode(spec.Name),
		manifestKeyNode("version"), manifestStrNode(spec.Version),
		manifestKeyNode("description"), manifestStrNode(spec.Description),
	}
	authorKey := manifestKeyNode("author")
	pairs = append(pairs, authorKey, manifestStrNode(spec.Author))

	depsKey := manifestKeyNode("dependencies")
	if len(spec.Targets) > 0 {
		targetsKey := manifestKeyNode("targets")
		targetsKey.HeadComment = strings.Join(targetsCommentLines(), "\n")
		pairs = append(pairs, targetsKey, manifestSeqNode(spec.Targets))
	} else {
		// PyYAML inserts a blank line after the comment block. A FootComment
		// on the author value reproduces that placement and keeps the
		// skeleton attached to the field it documents.
		authorKey.FootComment = targetsSkeletonComment()
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
	n := &yamllib.Node{Kind: yamllib.ScalarNode, Tag: "!!str", Value: v}
	// PyYAML's resolver treats YAML 1.1 boolean spellings such as "yes" as
	// values that need quoting even though they are strings in the manifest.
	// go-yaml's programmatic Node path uses YAML 1.2 resolution here, so make
	// the Oracle-required quote explicit (the same applies to the other legacy
	// boolean spellings).
	switch v {
	case "y", "Y", "yes", "Yes", "YES", "n", "N", "no", "No", "NO", "on", "On", "ON", "off", "Off", "OFF":
		n.Style = yamllib.SingleQuotedStyle
	}
	return n
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
