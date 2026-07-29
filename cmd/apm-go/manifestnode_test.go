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
