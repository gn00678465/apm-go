package main

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

// TestBuildManifestNode_KeySemanticOrder is AC5: apm.yml produced by init
// must use the semantic key order name -> version -> description -> author
// -> targets -> dependencies -> includes -> scripts, not go-yaml's default
// alphabetical map ordering.
func TestBuildManifestNode_KeySemanticOrder(t *testing.T) {
	node := buildManifestNode(manifestSpec{
		Name: "shape-probe", Version: "1.0.0", Description: "desc", Author: "author",
		Targets: []string{"claude", "codex", "opencode"},
	})
	out, err := yamlcore.SafeDump(node)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}

	want := []string{"name", "version", "description", "author", "targets", "dependencies", "includes", "scripts"}
	var got []string
	for _, line := range strings.Split(string(out), "\n") {
		for _, k := range want {
			if strings.HasPrefix(line, k+":") {
				got = append(got, k)
				break
			}
		}
	}
	if len(got) != len(want) {
		t.Fatalf("found keys %v, want all of %v in:\n%s", got, want, out)
	}
	for i, k := range want {
		if got[i] != k {
			t.Fatalf("key order = %v, want %v", got, want)
		}
	}
}

// TestBuildManifestNode_TargetsCommentThreeLines is AC6: targets: is
// preceded by three comment lines, the third listing the six
// apm-go-supported targets in alphabetical order.
func TestBuildManifestNode_TargetsCommentThreeLines(t *testing.T) {
	node := buildManifestNode(manifestSpec{
		Name: "p", Version: "1.0.0", Description: "d", Author: "a",
		Targets: []string{"claude"},
	})
	out, err := yamlcore.SafeDump(node)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	content := string(out)

	want := "# Which agent platforms to deploy to.\n" +
		"# Resolution order: --target flag > this field > auto-detect from filesystem.\n" +
		"# Accepted values: agent-skills, antigravity, claude, codex, copilot, opencode\n" +
		"targets:\n"
	if !strings.Contains(content, want) {
		t.Fatalf("targets: comment block = %q, want it to contain:\n%s", content, want)
	}
}

// TestBuildManifestNode_NoTargets_CommentedSkeleton is AC7: when no target
// is selected, the output contains the verbatim five-line commented-out
// skeleton (three explainer lines + "# targets:" + "#   - claude") -- not
// just "some comment starting with # targets:".
func TestBuildManifestNode_NoTargets_CommentedSkeleton(t *testing.T) {
	node := buildManifestNode(manifestSpec{Name: "p", Version: "1.0.0", Description: "d", Author: "a"})
	out, err := yamlcore.SafeDump(node)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	content := string(out)

	want := "# Which agent platforms to deploy to.\n" +
		"# Resolution order: --target flag > this field > auto-detect from filesystem.\n" +
		"# Accepted values: agent-skills, antigravity, claude, codex, copilot, opencode\n" +
		"# targets:\n" +
		"#   - claude\n"
	if !strings.Contains(content, want) {
		t.Fatalf("output = %q, want it to contain the verbatim skeleton:\n%s", content, want)
	}
	if strings.Contains(content, "\ntargets:\n") {
		t.Errorf("output must not contain an uncommented targets: key when no target was selected:\n%s", content)
	}
}

// TestBuildManifestNode_PluginMode_DevDependenciesKeyOrder is the 2026-07-30
// codex Tier 2 M1 fix: the plugin branch (spec.Plugin=true), which inserts a
// devDependencies section between includes and scripts, had zero test
// coverage before this. Locks both the key order and the section's content.
func TestBuildManifestNode_PluginMode_DevDependenciesKeyOrder(t *testing.T) {
	node := buildManifestNode(manifestSpec{
		Name: "p", Version: "1.0.0", Description: "d", Author: "a",
		Targets: []string{"claude"}, Plugin: true,
	})
	out, err := yamlcore.SafeDump(node)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	content := string(out)

	want := []string{"name", "version", "description", "author", "targets", "dependencies", "includes", "devDependencies", "scripts"}
	var got []string
	for _, line := range strings.Split(content, "\n") {
		for _, k := range want {
			if strings.HasPrefix(line, k+":") {
				got = append(got, k)
				break
			}
		}
	}
	if len(got) != len(want) {
		t.Fatalf("found keys %v, want all of %v in:\n%s", got, want, content)
	}
	for i, k := range want {
		if got[i] != k {
			t.Fatalf("key order = %v, want %v", got, want)
		}
	}
	if !strings.Contains(content, "devDependencies:\n  apm: []\n") {
		t.Errorf("output missing devDependencies: {apm: []} content:\n%s", content)
	}
}

// TestBuildManifestNode_NoTargets_AuthorImmediatelyPrecedesDependencies is
// the 2026-07-30 codex Tier 2 M5 fix: buildManifestNode's no-targets branch
// deliberately attaches the commented-out targets skeleton as a HeadComment
// on the dependencies key rather than a FootComment on author (see the
// comment at manifestnode.go's depsKey.HeadComment assignment), because
// dependencies is assumed to be the very next key after author in this fixed
// order. This test locks that adjacency assumption: if a future key is ever
// inserted between author and dependencies, this test goes red as a signal
// that the HeadComment placement must move with it.
func TestBuildManifestNode_NoTargets_AuthorImmediatelyPrecedesDependencies(t *testing.T) {
	node := buildManifestNode(manifestSpec{Name: "p", Version: "1.0.0", Description: "d", Author: "a"})

	authorIdx, depsIdx := -1, -1
	for i := 0; i < len(node.Content)-1; i += 2 {
		switch node.Content[i].Value {
		case "author":
			authorIdx = i
		case "dependencies":
			depsIdx = i
		}
	}
	if authorIdx == -1 || depsIdx == -1 {
		t.Fatalf("expected both author and dependencies keys present in node.Content")
	}
	if depsIdx != authorIdx+2 {
		t.Errorf("dependencies key is not immediately after author (author pair at index %d, dependencies pair at index %d); "+
			"the no-targets HeadComment placement in buildManifestNode assumes adjacency", authorIdx, depsIdx)
	}
}

// TestBuildManifestNode_SpecialCharacterScalars_RoundTrip is the 2026-07-30
// codex Tier 2 M6 fix: name/description/author containing YAML-significant
// characters (colon, quotes, embedded newline) had no test proving the
// scalar encoding survives the full SafeDump -> SafeLoad -> ParseManifest
// round trip buildManifestNode's own doc comment requires before writing to
// disk.
func TestBuildManifestNode_SpecialCharacterScalars_RoundTrip(t *testing.T) {
	spec := manifestSpec{
		Name:        "weird-name",
		Version:     "1.0.0",
		Description: "desc: with a colon, \"quotes\", and a\nnewline",
		Author:      "Jane \"JD\" Doe",
		Targets:     []string{"claude"},
	}
	node := buildManifestNode(spec)
	out, err := yamlcore.SafeDump(node)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	loaded, err := yamlcore.SafeLoad(out)
	if err != nil {
		t.Fatalf("SafeLoad: %v\n%s", err, out)
	}
	m, _, err := manifest.ParseManifest(loaded)
	if err != nil {
		t.Fatalf("ParseManifest: %v\n%s", err, out)
	}
	if m.Description != spec.Description {
		t.Errorf("Description round-trip = %q, want %q", m.Description, spec.Description)
	}
	if m.Author != spec.Author {
		t.Errorf("Author round-trip = %q, want %q", m.Author, spec.Author)
	}
}

// TestSupportedTargetsSet_MatchesAdapterTargetsAndPromptMenu is AC25:
// manifest.SupportedTargets, manifest.HasAdapter's target set, and the init
// MultiSelect prompt's actual option set (targetSelectOptions, what the user
// is offered) must all be the same set of targets. If any of the three
// gains or loses a target without the others following, this test goes red.
// This test lives in cmd/apm-go (not internal/manifest) because the third
// set -- the prompt menu -- only exists here.
func TestSupportedTargetsSet_MatchesAdapterTargetsAndPromptMenu(t *testing.T) {
	for _, tgt := range manifest.SupportedTargets {
		if !manifest.HasAdapter(tgt) {
			t.Errorf("SupportedTargets contains %q, but HasAdapter(%q) = false", tgt, tgt)
		}
	}

	opts := targetSelectOptions(nil, nil)
	if len(opts) != len(manifest.SupportedTargets) {
		t.Fatalf("prompt menu has %d options, want %d (one per SupportedTargets entry)",
			len(opts), len(manifest.SupportedTargets))
	}
	menuSet := make(map[string]bool, len(opts))
	for _, o := range opts {
		menuSet[o.Value] = true
		if !manifest.HasAdapter(o.Value) {
			t.Errorf("prompt menu offers %q, but HasAdapter(%q) = false", o.Value, o.Value)
		}
	}
	for _, tgt := range manifest.SupportedTargets {
		if !menuSet[tgt] {
			t.Errorf("SupportedTargets contains %q, but the prompt menu does not offer it", tgt)
		}
	}
}

// TestTargetsCommentLines_DerivedFromInput is AC26: the Accepted values line
// must be derived from manifest.SupportedTargets, not an independent
// literal. This is a behavioral test (not a grep for the current six
// values): temporarily replacing manifest.SupportedTargets with a
// different slice must change targetsCommentLines' output accordingly.
func TestTargetsCommentLines_DerivedFromInput(t *testing.T) {
	orig := manifest.SupportedTargets
	t.Cleanup(func() { manifest.SupportedTargets = orig })

	manifest.SupportedTargets = []string{"zzz-fake-target", "aaa-fake-target"}

	lines := targetsCommentLines()
	if len(lines) != 3 {
		t.Fatalf("targetsCommentLines() = %v, want 3 lines", lines)
	}
	want := "Accepted values: aaa-fake-target, zzz-fake-target"
	if lines[2] != want {
		t.Errorf("targetsCommentLines()[2] = %q, want %q (fake target list not reflected)", lines[2], want)
	}
}
