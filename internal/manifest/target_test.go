package manifest

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
)

func TestValidateTarget(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr string // empty = no error; non-empty = error must contain this
	}{
		// canonical
		{"claude", "claude", ""},
		{"copilot", "copilot", ""},
		{"codex", "codex", ""},
		{"opencode", "opencode", ""},
		{"antigravity", "antigravity", ""},
		{"cursor", "cursor", ""},
		{"gemini", "gemini", ""},
		{"windsurf", "windsurf", ""},
		{"agent-skills", "agent-skills", ""},
		{"kiro", "kiro", ""},
		{"all", "all", ""},

		// aliases
		{"vscode", "copilot", ""},
		{"agents", "copilot", ""},
		{"agy", "antigravity", ""},

		// x-vendor (tg-004)
		{"x-acme-tool", "x-acme-tool", ""},
		{"x-my-custom", "x-my-custom", ""},
		{"x-a0-b1", "x-a0-b1", ""},

		// minimal rejected (mf-005)
		{"minimal", "", "minimal"},

		// unknown
		{"notarealtool", "", "notarealtool"},
		{"vim", "", "vim"},

		// x-vendor bad format (needs two segments)
		{"x-a", "", "unknown target"},
		{"x-", "", "unknown target"},
		{"x-acme", "", "unknown target"},

		// not x-vendor pattern
		{"X-acme-tool", "", "unknown target"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ValidateTarget(tt.input)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if got != tt.want {
					t.Errorf("got %q, want %q", got, tt.want)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestHasAdapter(t *testing.T) {
	tests := []struct {
		target string
		want   bool
	}{
		{"claude", true},
		{"codex", true},
		{"copilot", true},
		{"opencode", true},
		{"antigravity", true},
		{"agent-skills", true},
		{"gemini", false},
		{"cursor", false},
		{"windsurf", false},
		{"kiro", false},
		{"x-acme-tool", false},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			if got := HasAdapter(tt.target); got != tt.want {
				t.Errorf("HasAdapter(%q) = %v, want %v", tt.target, got, tt.want)
			}
		})
	}
}

// TestAdapterTargetsSet_MatchesDeployTargets is the codex-audit companion
// to AC25 (cmd/apm-go's TestSupportedTargetsSet_MatchesAdapterTargetsAndPromptMenu):
// that cmd/apm-go test can only check "every SupportedTargets entry has an
// adapter" (one direction), because adapterTargets is unexported (invisible
// outside this package) and SupportedTargets is a reassignable exported var
// a test can swap out from under it (as TestTargetsCommentLines_DerivedFromInput
// does). Neither structural fact lets a cmd/apm-go test catch a target added
// directly to adapterTargets while bypassing deployTargets. This test lives
// in package manifest, where adapterTargets is visible, and checks BOTH
// directions against deployTargets -- the only way to actually detect that
// drift, closing the gap codex's audit found in the AC25 test.
func TestAdapterTargetsSet_MatchesDeployTargets(t *testing.T) {
	if len(adapterTargets) != len(deployTargets) {
		t.Fatalf("adapterTargets has %d entries, deployTargets has %d", len(adapterTargets), len(deployTargets))
	}
	for _, tgt := range deployTargets {
		if !adapterTargets[tgt] {
			t.Errorf("deployTargets contains %q, but adapterTargets[%q] is not true", tgt, tgt)
		}
	}
	for tgt := range adapterTargets {
		found := false
		for _, d := range deployTargets {
			if d == tgt {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("adapterTargets contains %q, but deployTargets does not -- drift", tgt)
		}
	}
}

// TestSupportedTargets_MatchesDeployTargetsExactly is the 2026-07-30 codex
// Tier 2 B2 fix: TestSupportedTargetsSet_MatchesAdapterTargetsAndPromptMenu
// (cmd/apm-go) and TestAdapterTargetsSet_MatchesDeployTargets (this package)
// both only compare two DERIVED vars against each other -- neither compares
// SupportedTargets directly against its source, deployTargets. Proof: mutate
// target.go:38 to `SupportedTargets = deployTargets[:5]` and both of those
// tests stay green, because cmd/apm-go's targetSelectOptions and this
// package's adapterTargets check are each internally consistent with a
// SHRUNK SupportedTargets, not with deployTargets itself. This test can only
// live in package manifest (not cmd/apm-go) because deployTargets is
// unexported.
func TestSupportedTargets_MatchesDeployTargetsExactly(t *testing.T) {
	if len(SupportedTargets) != len(deployTargets) {
		t.Fatalf("len(SupportedTargets) = %d, len(deployTargets) = %d -- SupportedTargets must be exactly deployTargets, not a subset",
			len(SupportedTargets), len(deployTargets))
	}
	for i, tgt := range deployTargets {
		if SupportedTargets[i] != tgt {
			t.Errorf("SupportedTargets[%d] = %q, deployTargets[%d] = %q -- must match exactly (including order)",
				i, SupportedTargets[i], i, tgt)
		}
	}
}

// TestCanonicalTargets_UnchangedAndCursorStillParses is AC27 (R8.4):
// unifying SupportedTargets/adapterTargets/the init prompt list around
// deployTargets must not touch CanonicalTargets, which is a wider apm.yml
// vocabulary (parity with upstream CANONICAL_TARGETS) than the 6 targets
// with a deploy adapter. cursor has no adapter but remains a valid apm.yml
// target token that only produces a req-tg-004 warning, never a parse
// failure.
func TestCanonicalTargets_UnchangedAndCursorStillParses(t *testing.T) {
	wantCanonical := map[string]bool{
		"copilot":      true,
		"claude":       true,
		"cursor":       true,
		"codex":        true,
		"gemini":       true,
		"opencode":     true,
		"windsurf":     true,
		"agent-skills": true,
		"kiro":         true,
		"all":          true,
		"antigravity":  true,
		// v0.28.0 (PR #2420): canonical, no deploy adapter yet (same
		// vocabulary-only footing as cursor/gemini/windsurf/kiro).
		// grok-cloud is deliberately absent: experimental-gated upstream.
		"grok-build": true,
	}
	if len(CanonicalTargets) != len(wantCanonical) {
		t.Fatalf("CanonicalTargets has %d entries, want %d (R8.4: must not be modified by this task)",
			len(CanonicalTargets), len(wantCanonical))
	}
	for k, v := range wantCanonical {
		if CanonicalTargets[k] != v {
			t.Errorf("CanonicalTargets[%q] = %v, want %v", k, CanonicalTargets[k], v)
		}
	}

	data := []byte("name: p\nversion: \"1.0.0\"\ntargets:\n  - cursor\n")
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	_, diags, err := ParseManifest(node)
	if err != nil {
		t.Fatalf("targets: [cursor] should be accepted: %v", err)
	}
	found := false
	for _, d := range diags {
		if d.Req == "req-tg-004" && strings.Contains(d.Message, "cursor") {
			found = true
		}
	}
	if !found {
		t.Error("expected req-tg-004 warning for cursor (no adapter)")
	}
}

// TestPromptTargets_ExactMembership locks the interactive menu against a
// LITERAL list. Every existing prompt-menu test compares against
// manifest.PromptTargets itself, so changing PromptTargets moves the test with
// it -- the same self-referential hole deployTargets' own comment warns about.
// Both init and plugin init read this one slice, so this test also enforces
// that the two menus stay identical.
func TestPromptTargets_ExactMembership(t *testing.T) {
	want := []string{"copilot", "claude", "opencode", "codex", "antigravity"}

	if len(PromptTargets) != len(want) {
		t.Fatalf("PromptTargets = %v (%d), want %v (%d)", PromptTargets, len(PromptTargets), want, len(want))
	}
	for i := range want {
		if PromptTargets[i] != want[i] {
			t.Errorf("PromptTargets[%d] = %q, want %q (full: %v)", i, PromptTargets[i], want[i], PromptTargets)
		}
	}
	for _, w := range want {
		if ExplicitOnlyTargets[w] {
			t.Errorf("%q is in the prompt menu but also marked explicit-only", w)
		}
	}
}
