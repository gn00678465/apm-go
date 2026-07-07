package manifest

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/yamlcore"
)

// findMappingChildForMCPFlowTest looks up key's value node within block
// mapping m -- a local equivalent of yamlcore's unexported
// findMappingChildForTest, needed here because this test lives in a
// different package.
func findMappingChildForMCPFlowTest(t *testing.T, m *yaml.Node, key string) *yaml.Node {
	t.Helper()
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	t.Fatalf("mapping key %q not found", key)
	return nil
}

// flowUninstallMCPManifestFixture mirrors uninstallMCPManifestFixture's
// shape but writes dependencies.mcp / devDependencies.mcp as FLOW-style
// sequences ("mcp: [a, b]") -- the same reported-bug shape as
// flowUninstallManifestFixture (remove_flow_test.go), for the mcp sibling
// removal path (applyProdMCPRemoval / removeSeqIndices's dev-mcp branch).
const flowUninstallMCPManifestFixture = `name: demo-project
version: 1.0.0

dependencies:
  apm:
    - acme/foo
  mcp: [bare-server, {name: dict-server, transport: stdio, command: my-cmd}]
devDependencies:
  apm:
    - acme/dev-only
  mcp: [dev-server, other-dev-server]
scripts:
  test: echo hi
`

// flowSingleEntryMCPManifestFixture is the single-element flow shape,
// removed entirely -> must render "mcp: []".
const flowSingleEntryMCPManifestFixture = `name: demo-project
version: 1.0.0
dependencies:
  apm:
    - acme/foo
  mcp: [only-server]
scripts:
  test: echo hi
`

func TestRemoveMCPServersFromManifest_FlowSingleEntry_RemoveAll_RendersEmptyFlow(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, flowSingleEntryMCPManifestFixture)

	out, removed, err := RemoveMCPServersFromManifest(src, doc, map[string]bool{"only-server": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["only-server"] {
		t.Errorf("expected only-server to be reported as removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "only-server") {
		t.Errorf("only-server should have been removed:\n%s", outStr)
	}
	if !strings.Contains(outStr, "mcp: []") {
		t.Errorf("expected an inline empty 'mcp: []', got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/foo") {
		t.Errorf("dependencies.apm should be untouched:\n%s", outStr)
	}

	if _, err := yamlcore.SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

func TestRemoveMCPServersFromManifest_FlowSequence_RemoveOneOfTwo_KeepsFlowList(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, flowUninstallMCPManifestFixture)

	out, removed, err := RemoveMCPServersFromManifest(src, doc, map[string]bool{"bare-server": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["bare-server"] {
		t.Errorf("expected bare-server to be reported as removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "bare-server") {
		t.Errorf("bare-server should have been removed:\n%s", outStr)
	}
	if !strings.Contains(outStr, "dict-server") || !strings.Contains(outStr, "my-cmd") {
		t.Errorf("dict-server should survive:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/foo") {
		t.Errorf("dependencies.apm should be untouched:\n%s", outStr)
	}
	if !strings.Contains(outStr, "dev-server") || !strings.Contains(outStr, "other-dev-server") {
		t.Errorf("devDependencies.mcp should be untouched:\n%s", outStr)
	}

	reparsed, err := yamlcore.SafeLoad(out)
	if err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
	root := reparsed.Content[0]
	deps := findMappingChildForMCPFlowTest(t, root, "dependencies")
	mcpSeq := findMappingChildForMCPFlowTest(t, deps, "mcp")
	if len(mcpSeq.Content) != 1 {
		t.Errorf("expected exactly one remaining dependencies.mcp entry, got %+v", mcpSeq)
	}
}

func TestRemoveMCPServersFromManifest_FlowSequence_RemoveBoth_RendersEmptyFlow(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, flowUninstallMCPManifestFixture)

	out, removed, err := RemoveMCPServersFromManifest(src, doc, map[string]bool{"bare-server": true, "dict-server": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["bare-server"] || !removed["dict-server"] {
		t.Errorf("expected both entries removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "bare-server") || strings.Contains(outStr, "dict-server") {
		t.Errorf("both targeted entries should be gone:\n%s", outStr)
	}
	if !strings.Contains(outStr, "mcp: []") {
		t.Errorf("expected dependencies.mcp to become an inline empty list, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/foo") {
		t.Errorf("dependencies.apm should be untouched:\n%s", outStr)
	}

	if _, err := yamlcore.SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

// TestRemoveMCPServersFromManifest_FlowDevSequence_PartialRemove_SharesFallback
// exercises removeSeqIndices's flow fallback via the dev-mcp path, proving
// the shared helper picks up the RebuildSequenceValueDropping fallback for
// MCP (not just dependencies.apm).
func TestRemoveMCPServersFromManifest_FlowDevSequence_PartialRemove_SharesFallback(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, flowUninstallMCPManifestFixture)

	out, removed, err := RemoveMCPServersFromManifest(src, doc, map[string]bool{"dev-server": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["dev-server"] {
		t.Errorf("expected dev-server removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "dev-server") && !strings.Contains(outStr, "other-dev-server") {
		t.Errorf("dev-server should have been removed while other-dev-server survives:\n%s", outStr)
	}
	if !strings.Contains(outStr, "other-dev-server") {
		t.Errorf("other-dev-server should survive:\n%s", outStr)
	}
	if !strings.Contains(outStr, "bare-server") || !strings.Contains(outStr, "dict-server") {
		t.Errorf("dependencies.mcp should be untouched:\n%s", outStr)
	}

	if _, err := yamlcore.SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

// TestRemoveMCPServersFromManifest_BlockStyle_StillByteIdentical is a
// regression guard mirroring
// TestRemovePackagesFromManifest_BlockStyle_StillByteIdentical: the new
// flow-fallback wiring must not change output for the existing byte-exact
// block-style MCP fixture at all.
func TestRemoveMCPServersFromManifest_BlockStyle_StillByteIdentical(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, uninstallMCPManifestFixture)

	out, removed, err := RemoveMCPServersFromManifest(src, doc, map[string]bool{"bare-server": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["bare-server"] {
		t.Errorf("expected bare-server to be reported as removed, got %v", removed)
	}

	const wantExact = `name: demo-project
version: 1.0.0

dependencies:
  apm:
    - acme/foo
  mcp:
    # scalar-form entry
    - name: dict-server
      transport: stdio
      command: my-cmd
    - name: other-server
devDependencies:
  apm:
    - acme/dev-only
  mcp:
    - name: dev-server
scripts:
  test: echo hi
`
	if string(out) != wantExact {
		t.Errorf("block-style output changed by the flow-fallback wiring, want byte-exact:\n%s\ngot:\n%s", wantExact, out)
	}
}
