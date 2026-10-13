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

			if err := persistPackagesToManifest(doc, tt.packages, tt.effectiveSubsets, false); err != nil {
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

	if err := persistPackagesToManifest(doc, []string{"acme/foo"}, nil, false); err != nil {
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

// TestPersistPackagesToManifest_PreservesSiblingEntryFields is the final
// codex gate regression: updating an existing entry's skills: subset must
// perform key surgery on the mapping, never rebuild it as {git, skills} --
// the first draft silently dropped sibling fields like a `ref:` version
// pin, so the very next install/update could resolve a different version.
func TestPersistPackagesToManifest_PreservesSiblingEntryFields(t *testing.T) {
	src := "name: d\nversion: 1.0.0\ndependencies:\n  apm:\n    - git: acme/foo\n      ref: stable\n      skills:\n        - a\n"

	// Union update: skills grows, ref must survive.
	doc, err := yamlcore.SafeLoad([]byte(src))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	if err := persistPackagesToManifest(doc, []string{"acme/foo"}, map[string][]string{"acme/foo": {"a", "b"}}, false); err != nil {
		t.Fatalf("persistPackagesToManifest: %v", err)
	}
	out, err := yamlcore.SafeDump(doc)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	got := string(out)
	for _, want := range []string{"ref: stable", "- a", "- b"} {
		if !strings.Contains(got, want) {
			t.Errorf("union update lost %q; got:\n%s", want, got)
		}
	}

	// RESET: skills pair is removed, but the mapping (and its ref pin)
	// survives -- it must NOT collapse to the scalar form.
	doc2, err := yamlcore.SafeLoad([]byte(src))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	if err := persistPackagesToManifest(doc2, []string{"acme/foo"}, nil, false); err != nil {
		t.Fatalf("persistPackagesToManifest (reset): %v", err)
	}
	out2, err := yamlcore.SafeDump(doc2)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	got2 := string(out2)
	if !strings.Contains(got2, "ref: stable") {
		t.Errorf("RESET lost the ref: pin; got:\n%s", got2)
	}
	if strings.Contains(got2, "skills:") {
		t.Errorf("RESET must remove the skills: key; got:\n%s", got2)
	}
	if strings.Contains(got2, "- acme/foo\n") {
		t.Errorf("RESET must not collapse a ref-pinned mapping to scalar; got:\n%s", got2)
	}
}

// TestPersistPackagesToManifest_MonorepoPathEntryNotMisHit: an entry's
// identity must come from the WHOLE dict (path: included) -- a positional
// base-repo install must not locate and rewrite the `{git: repo, path: sub}`
// sub-package entry just because they share a git value.
func TestPersistPackagesToManifest_MonorepoPathEntryNotMisHit(t *testing.T) {
	src := "name: d\nversion: 1.0.0\ndependencies:\n  apm:\n    - git: acme/foo\n      path: skills/sub\n      skills:\n        - x\n"

	doc, err := yamlcore.SafeLoad([]byte(src))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	if err := persistPackagesToManifest(doc, []string{"acme/foo"}, map[string][]string{"acme/foo": {"y"}}, false); err != nil {
		t.Fatalf("persistPackagesToManifest: %v", err)
	}
	out, err := yamlcore.SafeDump(doc)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "path: skills/sub") {
		t.Errorf("sub-package entry lost its path:; got:\n%s", got)
	}
	if !strings.Contains(got, "- x") {
		t.Errorf("sub-package entry lost its own subset; got:\n%s", got)
	}
	// The base repo is a DIFFERENT dependency: it must be appended as its
	// own entry with its own subset, not folded onto the sub-package.
	if !strings.Contains(got, "- y") {
		t.Errorf("base-repo entry missing its subset; got:\n%s", got)
	}
}

// persistLocalSubset runs persistPackagesToManifest for the package ./dep
// with the given effective subset and returns the dumped manifest.
func persistLocalSubset(t *testing.T, src string, subset []string) string {
	t.Helper()
	chdirTemp(t)
	doc, err := yamlcore.SafeLoad([]byte(src))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	var subsets map[string][]string
	if len(subset) > 0 {
		subsets = map[string][]string{canonicalIdentityForDepString("./dep"): subset}
	}
	if err := persistPackagesToManifest(doc, []string{"./dep"}, subsets, false); err != nil {
		t.Fatalf("persistPackagesToManifest: %v", err)
	}
	out, err := yamlcore.SafeDump(doc)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	return string(out)
}

// Issue #61: a local package's skill subset is a `path:` entry, the bytes the
// Oracle v0.33.0 writes; `git: ./dep` makes the next install clone the path.
func TestPersistPackagesToManifest_LocalSkillSubsetIsPathEntry(t *testing.T) {
	const head = "name: proj\nversion: 1.0.0\ntarget: claude\n"
	tests := []struct {
		name   string
		src    string
		subset []string
		want   string
	}{
		{
			name:   "plain string entry gains a subset",
			src:    head + "dependencies:\n  apm:\n  - ./dep\n",
			subset: []string{"sk"},
			want:   head + "dependencies:\n  apm:\n    - path: ./dep\n      skills:\n        - sk\n",
		},
		{
			name:   "new local package with a subset",
			src:    head,
			subset: []string{"sk"},
			want:   head + "dependencies:\n  apm:\n    - path: ./dep\n      skills:\n        - sk\n",
		},
		{
			name: "path and skills reset to the plain string",
			src:  head + "dependencies:\n  apm:\n    - path: ./dep\n      skills:\n        - sk\n",
			want: head + "dependencies:\n  apm:\n    - ./dep\n",
		},
		{
			// Both forms read `./dep ` as ./dep, so the value is written
			// back as the user spelled it.
			name: "padded path and skills reset to the same padded string",
			src:  head + "dependencies:\n  apm:\n    - path: './dep '\n      skills:\n        - sk\n",
			want: head + "dependencies:\n  apm:\n    - './dep '\n",
		},
		{
			// Deliberate deviation: the Oracle v0.33.0 writes {path, skills}
			// here and drops the alias (owner ruling 2026-10-13).
			name:   "path and alias gains a subset and keeps the alias",
			src:    head + "dependencies:\n  apm:\n    - path: ./dep\n      alias: mydep\n",
			subset: []string{"sk"},
			want:   head + "dependencies:\n  apm:\n    - path: ./dep\n      alias: mydep\n      skills:\n        - sk\n",
		},
		{
			name: "path, alias and skills reset keeps path and alias",
			src:  head + "dependencies:\n  apm:\n    - path: ./dep\n      alias: mydep\n      skills:\n        - sk\n",
			want: head + "dependencies:\n  apm:\n    - path: ./dep\n      alias: mydep\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := persistLocalSubset(t, tt.src, tt.subset); got != tt.want {
				t.Errorf("apm.yml:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}
