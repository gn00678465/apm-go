package manifest

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
)

// uninstallMCPManifestFixture is a hand-authored apm.yml with mixed
// scalar/dict-form dependencies.mcp entries, an unrelated dependencies.apm
// section, and devDependencies.mcp -- the removal test matrix must prove only
// the targeted server entries disappear, with every other entry's hand
// formatting and unrelated apm.yml content untouched.
const uninstallMCPManifestFixture = `name: demo-project
version: 1.0.0

dependencies:
  apm:
    - acme/foo
  mcp:
    # scalar-form entry
    - bare-server
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

func TestRemoveMCPServersFromManifest_RemovesScalarEntry_PreservesRestByteExact(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, uninstallMCPManifestFixture)

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
	if !strings.Contains(outStr, "name: dict-server") || !strings.Contains(outStr, "command: my-cmd") {
		t.Errorf("untouched dict-server entry should survive:\n%s", outStr)
	}
	if !strings.Contains(outStr, "name: other-server") {
		t.Errorf("untouched other-server entry should survive:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/foo") {
		t.Errorf("dependencies.apm should be untouched:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/dev-only") {
		t.Errorf("devDependencies.apm should be untouched:\n%s", outStr)
	}
	if !strings.Contains(outStr, "name: dev-server") {
		t.Errorf("devDependencies.mcp should be untouched:\n%s", outStr)
	}
	if !strings.Contains(outStr, "test: echo hi") {
		t.Errorf("scripts should be untouched:\n%s", outStr)
	}

	if _, err := yamlcore.SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

func TestRemoveMCPServersFromManifest_RemovesDictEntry_PreservesOthers(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, uninstallMCPManifestFixture)

	out, removed, err := RemoveMCPServersFromManifest(src, doc, map[string]bool{"dict-server": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["dict-server"] {
		t.Errorf("expected dict-server removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "dict-server") || strings.Contains(outStr, "my-cmd") {
		t.Errorf("dict-server entry should have been removed:\n%s", outStr)
	}
	if !strings.Contains(outStr, "bare-server") {
		t.Errorf("bare-server should survive:\n%s", outStr)
	}
	if !strings.Contains(outStr, "name: other-server") {
		t.Errorf("other-server should survive:\n%s", outStr)
	}

	if _, err := yamlcore.SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

func TestRemoveMCPServersFromManifest_MultipleIndices_RemovesAllInOnePass(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, uninstallMCPManifestFixture)

	out, removed, err := RemoveMCPServersFromManifest(src, doc, map[string]bool{"bare-server": true, "other-server": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["bare-server"] || !removed["other-server"] {
		t.Errorf("expected both removed, got %v", removed)
	}
	outStr := string(out)
	if strings.Contains(outStr, "bare-server") || strings.Contains(outStr, "other-server") {
		t.Errorf("both targeted entries should be gone:\n%s", outStr)
	}
	if !strings.Contains(outStr, "dict-server") {
		t.Errorf("untouched middle entry should survive:\n%s", outStr)
	}

	if _, err := yamlcore.SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

// TestRemoveMCPServersFromManifest_EmptiesProdMCP_KeepsKeyAsEmptyList covers
// "dependencies.mcp always stays present": removing every prod mcp server
// must leave "mcp: []", never delete the key, and never leave a bare "mcp:"
// that would re-parse as null (mirrors apm-go init's own "mcp: []" default).
func TestRemoveMCPServersFromManifest_EmptiesProdMCP_KeepsKeyAsEmptyList(t *testing.T) {
	const fixture = `name: demo-project
version: 1.0.0
dependencies:
  apm:
    - acme/foo
  mcp:
    - bare-server
    - name: dict-server
scripts:
  test: echo hi
`
	doc, src := mustLoadManifestFixture(t, fixture)

	out, removed, err := RemoveMCPServersFromManifest(src, doc, map[string]bool{"bare-server": true, "dict-server": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["bare-server"] || !removed["dict-server"] {
		t.Errorf("expected both removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if !strings.Contains(outStr, "mcp: []") {
		t.Errorf("expected dependencies.mcp to become an inline empty list, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/foo") {
		t.Errorf("dependencies.apm should be untouched:\n%s", outStr)
	}
	if !strings.Contains(outStr, "test: echo hi") {
		t.Errorf("scripts should survive:\n%s", outStr)
	}

	if _, err := yamlcore.SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

// TestRemoveMCPServersFromManifest_EmptiesDevMCP_DeletesMCPKey_KeepsAPMSibling
// covers devDependencies.mcp becoming empty: the mcp key itself is deleted,
// but the devDependencies wrapper survives because it still has a sibling
// "apm" key.
func TestRemoveMCPServersFromManifest_EmptiesDevMCP_DeletesMCPKey_KeepsAPMSibling(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, uninstallMCPManifestFixture)

	out, removed, err := RemoveMCPServersFromManifest(src, doc, map[string]bool{"dev-server": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["dev-server"] {
		t.Errorf("expected dev-server removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "dev-server") {
		t.Errorf("dev-server should be gone:\n%s", outStr)
	}
	if !strings.Contains(outStr, "devDependencies:") {
		t.Errorf("devDependencies wrapper should survive (it still has apm):\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/dev-only") {
		t.Errorf("devDependencies.apm should survive:\n%s", outStr)
	}

	if _, err := yamlcore.SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

// TestRemoveMCPServersFromManifest_EmptiesDevMCP_OnlyKey_DeletesWholeWrapper
// covers the "no empty shell left behind" case: when devDependencies.mcp is
// the ONLY key under devDependencies and it becomes empty, the whole
// devDependencies wrapper is deleted.
func TestRemoveMCPServersFromManifest_EmptiesDevMCP_OnlyKey_DeletesWholeWrapper(t *testing.T) {
	const fixture = `name: demo-project
version: 1.0.0
dependencies:
  apm:
    - acme/foo
devDependencies:
  mcp:
    - dev-server
scripts:
  test: echo hi
`
	doc, src := mustLoadManifestFixture(t, fixture)

	out, removed, err := RemoveMCPServersFromManifest(src, doc, map[string]bool{"dev-server": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["dev-server"] {
		t.Errorf("expected dev-server removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "devDependencies") {
		t.Errorf("the whole devDependencies wrapper should have been deleted (no empty shell):\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/foo") {
		t.Errorf("dependencies.apm should be untouched:\n%s", outStr)
	}
	if !strings.Contains(outStr, "test: echo hi") {
		t.Errorf("scripts should survive:\n%s", outStr)
	}

	if _, err := yamlcore.SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

func TestRemoveMCPServersFromManifest_UnmatchedName_NoChanges(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, uninstallMCPManifestFixture)

	out, removed, err := RemoveMCPServersFromManifest(src, doc, map[string]bool{"does-not-exist": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("expected nothing removed, got %v", removed)
	}
	if string(out) != uninstallMCPManifestFixture {
		t.Errorf("expected byte-exact passthrough when nothing matches, got:\n%s", out)
	}
}

func TestRemoveMCPServersFromManifest_EmptyServerNames_NoChanges(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, uninstallMCPManifestFixture)

	out, removed, err := RemoveMCPServersFromManifest(src, doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("expected nothing removed, got %v", removed)
	}
	if string(out) != uninstallMCPManifestFixture {
		t.Errorf("expected byte-exact passthrough for empty serverNames, got:\n%s", out)
	}
}

func TestRemoveMCPServersFromManifest_ProdAndDevTogether_BothSectionsUpdated(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, uninstallMCPManifestFixture)

	out, removed, err := RemoveMCPServersFromManifest(src, doc, map[string]bool{
		"bare-server": true,
		"dev-server":  true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["bare-server"] || !removed["dev-server"] {
		t.Errorf("expected both prod and dev entries removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)
	if strings.Contains(outStr, "bare-server") || strings.Contains(outStr, "dev-server") {
		t.Errorf("both targeted entries should be gone:\n%s", outStr)
	}
	if !strings.Contains(outStr, "dict-server") || !strings.Contains(outStr, "other-server") {
		t.Errorf("untouched prod entries should survive:\n%s", outStr)
	}
	if !strings.Contains(outStr, "devDependencies:") {
		t.Errorf("devDependencies wrapper should survive (it still has apm):\n%s", outStr)
	}

	if _, err := yamlcore.SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}
