package manifest

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
)

// flowUninstallManifestFixture mirrors uninstallManifestFixture's shape but
// writes dependencies.apm / devDependencies.apm as FLOW-style sequences
// ("apm: [a, b]") -- reproducing the reported bug: `apm-go uninstall
// mattpocock/skills` against a flow-style dependencies.apm failed with
// "remove apm.yml dependency entries: dependencies.apm: unexpected document
// shape while emptying", because SpliceSequenceElement /
// ReplaceSequenceWithEmptyFlow intentionally reject flow-style sequences and
// the manifest removers had no fallback for that ok=false.
const flowUninstallManifestFixture = `name: demo-project
version: 1.0.0

dependencies:
  apm: [acme/foo, acme/bar]
  conflict_resolution: first-wins
devDependencies:
  apm: [acme/dev-only, acme/dev-two]
  mcp:
    - name: devserver
scripts:
  test: echo hi
`

// flowSingleEntryManifestFixture is the exact minimal shape from the
// reported bug: a single-element flow sequence ("apm: [mattpocock/skills]"
// in the wild, "acme/only" here), removed entirely -> must render "apm: []".
const flowSingleEntryManifestFixture = `name: demo-project
version: 1.0.0
dependencies:
  apm: [acme/only]
scripts:
  test: echo hi
`

func TestRemovePackagesFromManifest_FlowSingleEntry_RemoveAll_RendersEmptyFlow(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, flowSingleEntryManifestFixture)

	out, removed, err := RemovePackagesFromManifest(src, doc, map[string]bool{"acme/only": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["acme/only"] {
		t.Errorf("expected acme/only to be reported as removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "acme/only") {
		t.Errorf("acme/only should have been removed:\n%s", outStr)
	}
	if !strings.Contains(outStr, "apm: []") {
		t.Errorf("expected an inline empty 'apm: []', got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "test: echo hi") {
		t.Errorf("scripts should survive:\n%s", outStr)
	}

	reparsed, err := yamlcore.SafeLoad(out)
	if err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
	m, _, err := ParseManifest(reparsed)
	if err != nil {
		t.Fatalf("output does not validate as a manifest: %v", err)
	}
	if len(m.ParsedDeps) != 0 {
		t.Errorf("expected zero remaining prod deps, got %+v", m.ParsedDeps)
	}
}

func TestRemovePackagesFromManifest_FlowSequence_RemoveOneOfTwo_KeepsFlowList(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, flowUninstallManifestFixture)

	out, removed, err := RemovePackagesFromManifest(src, doc, map[string]bool{"acme/foo": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["acme/foo"] {
		t.Errorf("expected acme/foo to be reported as removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "acme/foo") {
		t.Errorf("acme/foo should have been removed:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/bar") {
		t.Errorf("acme/bar should survive:\n%s", outStr)
	}
	if !strings.Contains(outStr, "conflict_resolution: first-wins") {
		t.Errorf("sibling dependencies.conflict_resolution should survive:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/dev-only") || !strings.Contains(outStr, "acme/dev-two") {
		t.Errorf("devDependencies.apm should be untouched:\n%s", outStr)
	}
	if !strings.Contains(outStr, "name: devserver") {
		t.Errorf("devDependencies.mcp should be untouched:\n%s", outStr)
	}

	reparsed, err := yamlcore.SafeLoad(out)
	if err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
	m, _, err := ParseManifest(reparsed)
	if err != nil {
		t.Fatalf("output does not validate as a manifest: %v", err)
	}
	if len(m.ParsedDeps) != 1 || m.ParsedDeps[0].RepoURL != "acme/bar" {
		t.Errorf("expected exactly acme/bar to remain in dependencies.apm, got %+v", m.ParsedDeps)
	}
}

func TestRemovePackagesFromManifest_FlowSequence_RemoveBoth_RendersEmptyFlow(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, flowUninstallManifestFixture)

	out, removed, err := RemovePackagesFromManifest(src, doc, map[string]bool{"acme/foo": true, "acme/bar": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["acme/foo"] || !removed["acme/bar"] {
		t.Errorf("expected both entries removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "acme/foo") || strings.Contains(outStr, "acme/bar") {
		t.Errorf("both targeted entries should be gone:\n%s", outStr)
	}
	if !strings.Contains(outStr, "apm: []") {
		t.Errorf("expected dependencies.apm to become an inline empty list, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/dev-only") || !strings.Contains(outStr, "acme/dev-two") {
		t.Errorf("devDependencies.apm should be untouched:\n%s", outStr)
	}

	reparsed, err := yamlcore.SafeLoad(out)
	if err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
	m, _, err := ParseManifest(reparsed)
	if err != nil {
		t.Fatalf("output does not validate as a manifest: %v", err)
	}
	if len(m.ParsedDeps) != 0 {
		t.Errorf("expected zero remaining prod deps, got %+v", m.ParsedDeps)
	}
}

// TestRemovePackagesFromManifest_FlowDevSequence_PartialRemove_SharesFallback
// exercises removeSeqIndices's flow fallback via the dev (not prod) path,
// proving the shared helper (not just applyProdRemoval) picks up the
// RebuildSequenceValueDropping fallback.
func TestRemovePackagesFromManifest_FlowDevSequence_PartialRemove_SharesFallback(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, flowUninstallManifestFixture)

	out, removed, err := RemovePackagesFromManifest(src, doc, map[string]bool{"acme/dev-only": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["acme/dev-only"] {
		t.Errorf("expected acme/dev-only removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "acme/dev-only") {
		t.Errorf("acme/dev-only should have been removed:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/dev-two") {
		t.Errorf("acme/dev-two should survive:\n%s", outStr)
	}
	if !strings.Contains(outStr, "name: devserver") {
		t.Errorf("devDependencies.mcp should survive:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/foo") || !strings.Contains(outStr, "acme/bar") {
		t.Errorf("dependencies.apm should be untouched:\n%s", outStr)
	}

	if _, err := yamlcore.SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

// TestRemovePackagesFromManifest_BlockStyle_StillByteIdentical is a
// regression guard: this new flow-fallback wiring must not change the
// output for the existing byte-exact block-style test fixture at all --
// same input, same call, same assertion as
// TestRemovePackagesFromManifest_RemovesSingleProdEntry_PreservesRestByteExact
// in remove_test.go, restated here explicitly so a future change to that
// file can't silently drop the regression coverage this task requires.
func TestRemovePackagesFromManifest_BlockStyle_StillByteIdentical(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, uninstallManifestFixture)

	out, removed, err := RemovePackagesFromManifest(src, doc, map[string]bool{"acme/bar": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["acme/bar"] {
		t.Errorf("expected acme/bar to be reported as removed, got %v", removed)
	}

	const wantExact = `name: demo-project
version: 1.0.0

dependencies:
  apm:
    # foo is the flagship plugin
    - acme/foo
    - acme/baz#v1.0.0
  conflict_resolution: first-wins
devDependencies:
  apm:
    - acme/dev-only
    - acme/dev-two
  mcp:
    - name: devserver
scripts:
  test: echo hi
`
	if string(out) != wantExact {
		t.Errorf("block-style output changed by the flow-fallback wiring, want byte-exact:\n%s\ngot:\n%s", wantExact, out)
	}
}
