package main

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
)

// TestPersistPackagesToManifest_FlowStyleNormalization is a regression test
// for bug #2: `apm install <pkg>` wrote `dependencies.apm` as a FLOW
// sequence (`apm: [acme/foo]`) instead of BLOCK (matching
// `dependencies.mcp` and the Python original) whenever the existing apm.yml
// already had `dependencies.apm` as a flow node (most commonly a scaffolded
// `apm: []`). persistPackagesToManifest reused that existing sequence node,
// which retains its parsed FlowStyle bit, so re-serializing via
// yamlcore.SafeDump rendered flow even though a fresh project (no prior
// dependencies.apm node) already rendered block correctly.
func TestPersistPackagesToManifest_FlowStyleNormalization(t *testing.T) {
	tests := []struct {
		name             string
		src              string
		packages         []string
		effectiveSubsets map[string][]string
		wantContain      []string
		wantAbsent       []string
	}{
		{
			// Reported bug: a scaffolded `apm: []` flow-empty sequence is
			// reused and appended to, but must render block afterwards.
			name:        "flow empty seq normalizes to block",
			src:         "name: d\nversion: 1.0.0\ndependencies:\n  apm: []\n",
			packages:    []string{"acme/foo"},
			wantContain: []string{"apm:\n    - acme/foo"},
			wantAbsent:  []string{"apm: [acme/foo]", "apm: []"},
		},
		{
			// Existing flow sequence with entries: appending must also
			// normalize the whole sequence to block, keeping both entries.
			name:        "flow seq with entries normalizes to block with both entries",
			src:         "name: d\nversion: 1.0.0\ndependencies:\n  apm: [acme/a]\n",
			packages:    []string{"acme/b"},
			wantContain: []string{"- acme/a", "- acme/b"},
			wantAbsent:  []string{"apm: [acme/a]", "apm: [acme/a, acme/b]"},
		},
		{
			// Regression guard: a fresh project with no dependencies.apm
			// node at all was already correct (a brand-new SequenceNode has
			// no FlowStyle bit) -- must stay block.
			name:        "fresh project with no dependencies node stays block",
			src:         "name: d\nversion: 1.0.0\n",
			packages:    []string{"acme/foo"},
			wantContain: []string{"apm:\n    - acme/foo"},
			wantAbsent:  []string{"apm: [acme/foo]"},
		},
		{
			// Object form (git+skills) entries must also land under a
			// normalized block sequence, not a flow one.
			name:             "object form entry lands under block seq",
			src:              "name: d\nversion: 1.0.0\ndependencies:\n  apm: []\n",
			packages:         []string{"acme/foo"},
			effectiveSubsets: map[string][]string{"acme/foo": {"x"}},
			wantContain:      []string{"apm:\n    - git: acme/foo"},
			wantAbsent:       []string{"apm: [{"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := yamlcore.SafeLoad([]byte(tt.src))
			if err != nil {
				t.Fatalf("SafeLoad: %v", err)
			}

			if err := persistPackagesToManifest(doc, tt.packages, tt.effectiveSubsets); err != nil {
				t.Fatalf("persistPackagesToManifest: %v", err)
			}

			out, err := yamlcore.SafeDump(doc)
			if err != nil {
				t.Fatalf("SafeDump: %v", err)
			}
			got := string(out)

			for _, want := range tt.wantContain {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q; got:\n%s", want, got)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(got, absent) {
					t.Errorf("output must not contain %q; got:\n%s", absent, got)
				}
			}
		})
	}
}

// TestPersistPackagesToManifest_DoesNotTouchMCPBlockSequence guards against
// collateral reformatting: normalizing dependencies.apm's style must not
// touch a sibling dependencies.mcp block sequence's own formatting.
func TestPersistPackagesToManifest_DoesNotTouchMCPBlockSequence(t *testing.T) {
	src := "name: d\nversion: 1.0.0\ndependencies:\n  apm: []\n  mcp:\n    - acme/mcp-server\n"

	doc, err := yamlcore.SafeLoad([]byte(src))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}

	if err := persistPackagesToManifest(doc, []string{"acme/foo"}, nil); err != nil {
		t.Fatalf("persistPackagesToManifest: %v", err)
	}

	out, err := yamlcore.SafeDump(doc)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	got := string(out)

	if !strings.Contains(got, "mcp:\n    - acme/mcp-server\n") {
		t.Errorf("dependencies.mcp block sequence must be byte-unchanged; got:\n%s", got)
	}
	if !strings.Contains(got, "apm:\n    - acme/foo\n") {
		t.Errorf("dependencies.apm must normalize to block; got:\n%s", got)
	}
}
