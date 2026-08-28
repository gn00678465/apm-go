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
	out, err := yamlcore.SafeDumpManifest(node)
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

// TestBuildManifestNode_TargetsCommentThreeLines is Finding 1's selected
// target assertion: the target comment is the Oracle's three-line block and
// its manifest catalog, not apm-go's smaller adapter whitelist.
func TestBuildManifestNode_TargetsCommentThreeLines(t *testing.T) {
	node := buildManifestNode(manifestSpec{
		Name: "p", Version: "1.0.0", Description: "d", Author: "a",
		Targets: []string{"claude"},
	})
	out, err := yamlcore.SafeDumpManifest(node)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	content := string(out)

	want := "# Which agent platforms to deploy to.\n" +
		"# Resolution order: --target flag > this field > auto-detect from filesystem.\n" +
		"# Accepted values: agent-skills, antigravity, claude, codex, copilot, cursor, gemini, grok-build, kiro, opencode, windsurf\n" +
		"targets:\n- claude\n"
	if !strings.Contains(content, want) {
		t.Fatalf("targets: comment block = %q, want it to contain:\n%s", content, want)
	}
}

// TestBuildManifestNode_NoTargets_CommentedSkeleton locks the Oracle's
// four-line no-target skeleton and its blank line before dependencies.
func TestBuildManifestNode_NoTargets_CommentedSkeleton(t *testing.T) {
	node := buildManifestNode(manifestSpec{Name: "p", Version: "1.0.0", Description: "d", Author: "a"})
	out, err := yamlcore.SafeDumpManifest(node)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	content := string(out)

	want := "# Which agent platforms to deploy to (uncomment to pin):\n" +
		"# targets:\n" +
		"#   - copilot\n" +
		"#   - claude\n\n"
	if !strings.Contains(content, want) {
		t.Fatalf("output = %q, want it to contain the verbatim skeleton:\n%s", content, want)
	}
	if strings.Contains(content, "\ntargets:\n") {
		t.Errorf("output must not contain an uncommented targets: key when no target was selected:\n%s", content)
	}
}

// TestBuildManifestNode_NoTargets_SkeletonHasOracleBlankLineBeforeDependencies
// guards the exact blank line emitted by the Oracle's post-processing.
func TestBuildManifestNode_NoTargets_SkeletonHasOracleBlankLineBeforeDependencies(t *testing.T) {
	node := buildManifestNode(manifestSpec{Name: "p", Version: "1.0.0", Description: "d", Author: "a"})
	out, err := yamlcore.SafeDumpManifest(node)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	content := string(out)

	if !strings.Contains(content, "#   - claude\n\ndependencies:\n") {
		t.Errorf("expected the commented-out skeleton's last line to be followed by one blank line before dependencies: got:\n%s", content)
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
	out, err := yamlcore.SafeDumpManifest(node)
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
// the no-target skeleton remains attached to the author field's rendered
// position. The fixed key order still keeps dependencies immediately after
// author in the node tree.
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

// TestSupportedTargetsSet_MatchesAdapterTargetsAndPromptMenu is AC25,
// revised 2026-08-02 for parity with Python apm_cli's EXPLICIT_ONLY_TARGETS
// (core/target_detection.py:430-431, v0.26.0): the prompt menu is no longer
// required to equal the full SupportedTargets set -- explicit-only targets
// (antigravity, agent-skills) have an adapter and remain --target-selectable
// but are deliberately omitted from the interactive menu (commands/init.py:629,
// `[t for t in _PROMPT_TARGETS_ORDERED if t not in EXPLICIT_ONLY_TARGETS]`).
// The invariant this test now locks: (1) every SupportedTargets entry has an
// adapter, (2) the prompt menu is exactly SupportedTargets minus
// ExplicitOnlyTargets -- not independently drifted in either direction, and
// (3) every ExplicitOnlyTargets entry is present in SupportedTargets (still
// selectable) but absent from the prompt menu. This test lives in cmd/apm-go
// (not internal/manifest) because the prompt menu set only exists here.
func TestSupportedTargetsSet_MatchesAdapterTargetsAndPromptMenu(t *testing.T) {
	for _, tgt := range manifest.SupportedTargets {
		if !manifest.HasAdapter(tgt) {
			t.Errorf("SupportedTargets contains %q, but HasAdapter(%q) = false", tgt, tgt)
		}
	}

	opts := targetSelectOptions(nil, nil)
	wantMenuCount := len(manifest.SupportedTargets) - len(manifest.ExplicitOnlyTargets)
	if len(opts) != wantMenuCount {
		t.Fatalf("prompt menu has %d options, want %d (SupportedTargets minus ExplicitOnlyTargets)",
			len(opts), wantMenuCount)
	}
	menuSet := make(map[string]bool, len(opts))
	for _, o := range opts {
		menuSet[o.Value] = true
		if !manifest.HasAdapter(o.Value) {
			t.Errorf("prompt menu offers %q, but HasAdapter(%q) = false", o.Value, o.Value)
		}
		if manifest.ExplicitOnlyTargets[o.Value] {
			t.Errorf("prompt menu offers explicit-only target %q; it must only be reachable via --target", o.Value)
		}
	}
	for _, tgt := range manifest.SupportedTargets {
		if manifest.ExplicitOnlyTargets[tgt] {
			if menuSet[tgt] {
				t.Errorf("explicit-only target %q must not appear in the prompt menu", tgt)
			}
			continue
		}
		if !menuSet[tgt] {
			t.Errorf("SupportedTargets contains non-explicit-only %q, but the prompt menu does not offer it", tgt)
		}
	}
}

// TestTargetsCommentLines_MatchesOracle locks the pinned Oracle catalog used
// in the generated comment, including targets that do not yet have apm-go
// deploy adapters.
func TestTargetsCommentLines_MatchesOracle(t *testing.T) {
	lines := targetsCommentLines()
	want := []string{
		"Which agent platforms to deploy to.",
		"Resolution order: --target flag > this field > auto-detect from filesystem.",
		"Accepted values: agent-skills, antigravity, claude, codex, copilot, cursor, gemini, grok-build, kiro, opencode, windsurf",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("targetsCommentLines() = %q, want %q", lines, want)
	}
}

const oracleNoTargetManifestPrefix = "name: p\nversion: 1.0.0\ndescription: d\nauthor: "

const oracleNoTargetManifestSuffix = "\n# Which agent platforms to deploy to (uncomment to pin):\n# targets:\n#   - copilot\n#   - claude\n\ndependencies:\n  apm: []\n  mcp: []\nincludes: auto\nscripts: {}\n"

// TestBuildManifestNode_OracleScalarBytes records the Oracle's exact scalar
// rendering for values that commonly expose YAML emitter differences. The
// full byte sequence is compared, not just the parsed value.
func TestBuildManifestNode_OracleScalarBytes(t *testing.T) {
	tests := []struct {
		name   string
		author string
		value  string
	}{
		{"colon", "a: b", "'a: b'"},
		{"hash", "#x", "'#x'"},
		{"leading space", " leading", "' leading'"},
		{"trailing space", "trailing ", "'trailing '"},
		{"yaml boolean word", "yes", "'yes'"},
		{"float-looking", "1.0", "'1.0'"},
		{"empty", "", "''"},
		{"emoji-only", "😀", "😀"},
		{"control character", "line\x01char", `"line\x01char"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := yamlcore.SafeDumpManifest(buildManifestNode(manifestSpec{
				Name: "p", Version: "1.0.0", Description: "d", Author: tt.author,
			}))
			if err != nil {
				t.Fatalf("SafeDump: %v", err)
			}
			want := oracleNoTargetManifestPrefix + tt.value + oracleNoTargetManifestSuffix
			if string(out) != want {
				t.Fatalf("bytes = %q, want Oracle bytes %q", out, want)
			}
			loaded, err := yamlcore.SafeLoad(out)
			if err != nil {
				t.Fatalf("SafeLoad: %v", err)
			}
			m, _, err := manifest.ParseManifest(loaded)
			if err != nil {
				t.Fatalf("ParseManifest: %v", err)
			}
			if m.Author != tt.author {
				t.Errorf("round-trip author = %q, want %q", m.Author, tt.author)
			}
		})
	}
}

// TestBuildManifestNode_OracleSelectedAndPluginBytes locks both shared init
// modes and the detected-target sequence observed from the pinned Oracle.
func TestBuildManifestNode_OracleSelectedAndPluginBytes(t *testing.T) {
	const selectedPrefix = "name: p\nversion: 1.0.0\ndescription: d\nauthor: 名😀<\n# Which agent platforms to deploy to.\n# Resolution order: --target flag > this field > auto-detect from filesystem.\n# Accepted values: agent-skills, antigravity, claude, codex, copilot, cursor, gemini, grok-build, kiro, opencode, windsurf\ntargets:\n- claude\n- codex\n- copilot\n- opencode\ndependencies:\n  apm: []\n  mcp: []\nincludes: auto\n"
	const consumerWant = selectedPrefix + "scripts: {}\n"
	const pluginWant = selectedPrefix + "devDependencies:\n  apm: []\nscripts: {}\n"

	for _, tt := range []struct {
		name   string
		plugin bool
		want   string
	}{
		{"consumer", false, consumerWant},
		{"plugin", true, pluginWant},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, err := yamlcore.SafeDumpManifest(buildManifestNode(manifestSpec{
				Name: "p", Version: "1.0.0", Description: "d", Author: "名😀<",
				Targets: []string{"claude", "codex", "copilot", "opencode"}, Plugin: tt.plugin,
			}))
			if err != nil {
				t.Fatalf("SafeDump: %v", err)
			}
			if string(out) != tt.want {
				t.Fatalf("bytes = %q, want Oracle bytes %q", out, tt.want)
			}
		})
	}
}
