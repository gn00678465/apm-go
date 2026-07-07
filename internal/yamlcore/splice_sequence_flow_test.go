package yamlcore

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

// flowApmManifestFixture mirrors apmManifestFixture's shape but writes
// dependencies.apm / devDependencies.apm as FLOW-style sequences ("apm:
// [a, b]") -- the shape SpliceSequenceElement / ReplaceSequenceWithEmptyFlow
// intentionally reject (see their own doc comments), reproducing the actual
// reported bug: `apm-go uninstall mattpocock/skills` against a flow-style
// dependencies.apm failing with "unexpected document shape while emptying".
const flowApmManifestFixture = `name: demo-project
version: 1.0.0

dependencies:
  apm: [acme/foo, acme/bar]
  conflict_resolution: first-wins
devDependencies:
  apm: [acme/dev-only]
scripts:
  test: echo hi
`

func TestRebuildSequenceValueDropping_FlowSequence_DropAll_RendersEmptyFlow(t *testing.T) {
	src := []byte(flowApmManifestFixture)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	out, ok, err := RebuildSequenceValueDropping(src, doc, []string{"dependencies", "apm"}, []int{0, 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for a flow-style sequence")
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "acme/foo") || strings.Contains(outStr, "acme/bar") {
		t.Errorf("dependencies.apm elements should be gone:\n%s", outStr)
	}
	if !strings.Contains(outStr, "apm: []") {
		t.Errorf("expected an inline empty 'apm: []', got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "conflict_resolution: first-wins") {
		t.Errorf("sibling dependencies.conflict_resolution should survive:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/dev-only") {
		t.Errorf("devDependencies.apm (a later root sibling) should survive untouched:\n%s", outStr)
	}
	if !strings.Contains(outStr, "test: echo hi") {
		t.Errorf("scripts (a later root sibling) should survive untouched:\n%s", outStr)
	}

	reparsed, err := SafeLoad(out)
	if err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
	root := reparsed.Content[0]
	deps := findMappingChildForTest(t, root, "dependencies")
	apmSeq := findMappingChildForTest(t, deps, "apm")
	if apmSeq.Kind != yaml.SequenceNode || len(apmSeq.Content) != 0 {
		t.Errorf("expected dependencies.apm to re-parse as an empty sequence, got kind=%v len=%d", apmSeq.Kind, len(apmSeq.Content))
	}
}

func TestRebuildSequenceValueDropping_FlowSequence_DropSubset_KeepsFlowList(t *testing.T) {
	src := []byte(flowApmManifestFixture)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	out, ok, err := RebuildSequenceValueDropping(src, doc, []string{"dependencies", "apm"}, []int{0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for a flow-style sequence")
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "acme/foo") {
		t.Errorf("acme/foo should have been dropped:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/bar") {
		t.Errorf("acme/bar should survive:\n%s", outStr)
	}
	if !strings.Contains(outStr, "conflict_resolution: first-wins") {
		t.Errorf("sibling dependencies.conflict_resolution should survive:\n%s", outStr)
	}

	reparsed, err := SafeLoad(out)
	if err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
	root := reparsed.Content[0]
	deps := findMappingChildForTest(t, root, "dependencies")
	apmSeq := findMappingChildForTest(t, deps, "apm")
	if apmSeq.Kind != yaml.SequenceNode || len(apmSeq.Content) != 1 || apmSeq.Content[0].Value != "acme/bar" {
		t.Errorf("expected dependencies.apm to re-parse as [acme/bar], got %+v", apmSeq)
	}
}

func TestRebuildSequenceValueDropping_MissingPath_ReturnsOkFalse(t *testing.T) {
	src := []byte(flowApmManifestFixture)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	out, ok, err := RebuildSequenceValueDropping(src, doc, []string{"noSuchKey", "apm"}, []int{0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for a missing path")
	}
	if out != nil {
		t.Errorf("expected nil out on ok=false, got %q", out)
	}
}

func TestRebuildSequenceValueDropping_NotASequence_ReturnsOkFalse(t *testing.T) {
	src := []byte(flowApmManifestFixture)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	out, ok, err := RebuildSequenceValueDropping(src, doc, []string{"dependencies", "conflict_resolution"}, []int{0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false when the path resolves to a scalar, not a sequence")
	}
	if out != nil {
		t.Errorf("expected nil out on ok=false, got %q", out)
	}
}

// TestRebuildSequenceValueDropping_BlockSequence_AlsoWorks proves this
// primitive is not flow-only -- it accepts a block-style sequence too (the
// doc comment says "any style"), even though block-style removal normally
// goes through SpliceSequenceElement instead.
func TestRebuildSequenceValueDropping_BlockSequence_AlsoWorks(t *testing.T) {
	src := []byte(apmManifestFixture)
	doc, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}

	out, ok, err := RebuildSequenceValueDropping(src, doc, []string{"dependencies", "apm"}, []int{0, 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for a block-style sequence")
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "acme/foo") || strings.Contains(outStr, "acme/bar") {
		t.Errorf("dependencies.apm elements should be gone:\n%s", outStr)
	}
	if !strings.Contains(outStr, "apm: []") {
		t.Errorf("expected an inline empty 'apm: []', got:\n%s", outStr)
	}

	if _, err := SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}
