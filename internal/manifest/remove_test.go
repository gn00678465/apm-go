package manifest

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/yamlcore"
)

// uninstallManifestFixture is a hand-authored (not machine-generated),
// commented apm.yml with existing content, manual formatting, and multiple
// unrelated dependencies -- 舊坑 1: the removal test matrix must prove only
// the targeted entries disappear, with every other entry's hand formatting
// and unrelated apm.yml content untouched.
const uninstallManifestFixture = `name: demo-project
version: 1.0.0

dependencies:
  apm:
    # foo is the flagship plugin
    - acme/foo
    - acme/bar # kept around
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

func mustLoadManifestFixture(t *testing.T, src string) (*yaml.Node, []byte) {
	t.Helper()
	doc, err := yamlcore.SafeLoad([]byte(src))
	if err != nil {
		t.Fatalf("fixture does not parse: %v", err)
	}
	return doc, []byte(src)
}

func TestRemovePackagesFromManifest_RemovesSingleProdEntry_PreservesRestByteExact(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, uninstallManifestFixture)

	out, removed, err := RemovePackagesFromManifest(src, doc, map[string]bool{"acme/bar": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["acme/bar"] {
		t.Errorf("expected acme/bar to be reported as removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "acme/bar") {
		t.Errorf("acme/bar should have been removed:\n%s", outStr)
	}
	if !strings.Contains(outStr, "# foo is the flagship plugin") || !strings.Contains(outStr, "acme/foo") {
		t.Errorf("untouched entry acme/foo (and its leading comment) should survive:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/baz#v1.0.0") {
		t.Errorf("untouched entry acme/baz should survive:\n%s", outStr)
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
	if !strings.Contains(outStr, "test: echo hi") {
		t.Errorf("scripts should be untouched:\n%s", outStr)
	}

	if _, err := yamlcore.SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

// TestRemovePackagesFromManifest_IgnoresRefAndAlias_ForIdentityMatch: the
// caller's target identity is "acme/baz" (no ref); the apm.yml entry is
// "acme/baz#v1.0.0" (with ref) -- un-011 requires this to still match.
func TestRemovePackagesFromManifest_IgnoresRefAndAlias_ForIdentityMatch(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, uninstallManifestFixture)

	out, removed, err := RemovePackagesFromManifest(src, doc, map[string]bool{"acme/baz": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["acme/baz"] {
		t.Errorf("expected acme/baz to be reported as removed (ref ignored), got %v", removed)
	}
	outStr := string(out)
	if strings.Contains(outStr, "acme/baz") {
		t.Errorf("acme/baz#v1.0.0 should have been removed despite the target not specifying a ref:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/foo") || !strings.Contains(outStr, "acme/bar") {
		t.Errorf("untouched entries should survive:\n%s", outStr)
	}
}

func TestRemovePackagesFromManifest_MultipleIndices_RemovesAllInOnePass(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, uninstallManifestFixture)

	// Remove first and last prod entries in one call -- exercises
	// descending-index processing within a single sequence.
	out, removed, err := RemovePackagesFromManifest(src, doc, map[string]bool{"acme/foo": true, "acme/baz": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["acme/foo"] || !removed["acme/baz"] {
		t.Errorf("expected both acme/foo and acme/baz removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)
	if strings.Contains(outStr, "acme/foo") || strings.Contains(outStr, "acme/baz") {
		t.Errorf("both targeted entries should be gone:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/bar # kept around") {
		t.Errorf("untouched middle entry (with its comment) should survive:\n%s", outStr)
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

func TestRemovePackagesFromManifest_RemovesDevEntry_KeepsOtherDevEntryAndMCP(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, uninstallManifestFixture)

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
}

// TestRemovePackagesFromManifest_EmptiesDevApm_DeletesApmKey_KeepsMCPSibling
// covers un-021: devDependencies.apm becomes empty (both entries removed),
// so the apm key itself is deleted, but the devDependencies wrapper survives
// because it still has a sibling "mcp" key.
func TestRemovePackagesFromManifest_EmptiesDevApm_DeletesApmKey_KeepsMCPSibling(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, uninstallManifestFixture)

	out, removed, err := RemovePackagesFromManifest(src, doc, map[string]bool{
		"acme/dev-only": true,
		"acme/dev-two":  true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["acme/dev-only"] || !removed["acme/dev-two"] {
		t.Errorf("expected both dev entries removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if strings.Contains(outStr, "acme/dev-only") || strings.Contains(outStr, "acme/dev-two") {
		t.Errorf("both dev entries should be gone:\n%s", outStr)
	}
	if !strings.Contains(outStr, "devDependencies:") {
		t.Errorf("devDependencies wrapper should survive (it still has mcp):\n%s", outStr)
	}
	if !strings.Contains(outStr, "name: devserver") {
		t.Errorf("devDependencies.mcp should survive:\n%s", outStr)
	}
	if strings.Contains(outStr, "apm:\n") && strings.Contains(outStr, "devDependencies:\n  apm:") {
		t.Errorf("devDependencies.apm key should have been deleted entirely, not left empty:\n%s", outStr)
	}

	if _, err := yamlcore.SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

// TestRemovePackagesFromManifest_EmptiesDevApm_OnlyKey_DeletesWholeWrapper
// covers un-021's "no empty shell left behind": when devDependencies.apm is
// the ONLY key under devDependencies and it becomes empty, the whole
// devDependencies wrapper is deleted.
func TestRemovePackagesFromManifest_EmptiesDevApm_OnlyKey_DeletesWholeWrapper(t *testing.T) {
	const fixture = `name: demo-project
version: 1.0.0
dependencies:
  apm:
    - acme/foo
devDependencies:
  apm:
    - acme/dev-only
scripts:
  test: echo hi
`
	doc, src := mustLoadManifestFixture(t, fixture)

	out, removed, err := RemovePackagesFromManifest(src, doc, map[string]bool{"acme/dev-only": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["acme/dev-only"] {
		t.Errorf("expected acme/dev-only removed, got %v", removed)
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

// TestRemovePackagesFromManifest_EmptiesProdApm_KeepsKeyAsEmptyList covers
// un-020/022's "dependencies.apm always stays present": removing every prod
// dependency must leave "apm: []", never delete the key, and never leave a
// bare "apm:" that would re-parse as null.
func TestRemovePackagesFromManifest_EmptiesProdApm_KeepsKeyAsEmptyList(t *testing.T) {
	const fixture = `name: demo-project
version: 1.0.0
dependencies:
  apm:
    - acme/foo
    - acme/bar
scripts:
  test: echo hi
`
	doc, src := mustLoadManifestFixture(t, fixture)

	out, removed, err := RemovePackagesFromManifest(src, doc, map[string]bool{"acme/foo": true, "acme/bar": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["acme/foo"] || !removed["acme/bar"] {
		t.Errorf("expected both entries removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)

	if !strings.Contains(outStr, "apm: []") {
		t.Errorf("expected dependencies.apm to become an inline empty list, got:\n%s", outStr)
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

func TestRemovePackagesFromManifest_UnmatchedIdentity_NoChanges(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, uninstallManifestFixture)

	out, removed, err := RemovePackagesFromManifest(src, doc, map[string]bool{"acme/does-not-exist": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("expected nothing removed, got %v", removed)
	}
	if string(out) != uninstallManifestFixture {
		t.Errorf("expected byte-exact passthrough when nothing matches, got:\n%s", out)
	}
}

func TestRemovePackagesFromManifest_EmptyIdentities_NoChanges(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, uninstallManifestFixture)

	out, removed, err := RemovePackagesFromManifest(src, doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("expected nothing removed, got %v", removed)
	}
	if string(out) != uninstallManifestFixture {
		t.Errorf("expected byte-exact passthrough for empty identities, got:\n%s", out)
	}
}

func TestRemovePackagesFromManifest_ProdAndDevTogether_BothSectionsUpdated(t *testing.T) {
	doc, src := mustLoadManifestFixture(t, uninstallManifestFixture)

	out, removed, err := RemovePackagesFromManifest(src, doc, map[string]bool{
		"acme/foo":      true,
		"acme/dev-only": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["acme/foo"] || !removed["acme/dev-only"] {
		t.Errorf("expected both prod and dev entries removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)
	if strings.Contains(outStr, "acme/foo") || strings.Contains(outStr, "acme/dev-only") {
		t.Errorf("both targeted entries should be gone:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/bar") || !strings.Contains(outStr, "acme/baz") {
		t.Errorf("untouched prod entries should survive:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/dev-two") {
		t.Errorf("untouched dev entry should survive:\n%s", outStr)
	}

	if _, err := yamlcore.SafeLoad(out); err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
}

// TestRemovePackagesFromManifest_DevSectionBeforeProdInFile_StillCorrect
// covers the cross-region ordering invariant explicitly: when devDependencies
// is written BEFORE dependencies in the file (uncommon but legal), removing
// entries from both sections must still produce a valid, byte-correct
// result -- proving the "process the physically later section first" rule
// generalizes regardless of section order in the source file.
func TestRemovePackagesFromManifest_DevSectionBeforeProdInFile_StillCorrect(t *testing.T) {
	const fixture = `name: demo-project
version: 1.0.0
devDependencies:
  apm:
    - acme/dev-only
    - acme/dev-two
dependencies:
  apm:
    - acme/foo
    - acme/bar
scripts:
  test: echo hi
`
	doc, src := mustLoadManifestFixture(t, fixture)

	out, removed, err := RemovePackagesFromManifest(src, doc, map[string]bool{
		"acme/foo":      true,
		"acme/dev-only": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["acme/foo"] || !removed["acme/dev-only"] {
		t.Errorf("expected both entries removed, got %v", removed)
	}
	outStr := string(out)
	t.Logf("output:\n%s", outStr)
	if strings.Contains(outStr, "acme/foo") || strings.Contains(outStr, "acme/dev-only") {
		t.Errorf("both targeted entries should be gone:\n%s", outStr)
	}
	if !strings.Contains(outStr, "acme/bar") || !strings.Contains(outStr, "acme/dev-two") {
		t.Errorf("untouched entries should survive:\n%s", outStr)
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
	if len(m.ParsedDeps) != 1 || m.ParsedDeps[0].RepoURL != "acme/bar" {
		t.Errorf("expected exactly acme/bar to remain, got %+v", m.ParsedDeps)
	}
	if len(m.ParsedDevDeps) != 1 || m.ParsedDevDeps[0].RepoURL != "acme/dev-two" {
		t.Errorf("expected exactly acme/dev-two to remain, got %+v", m.ParsedDevDeps)
	}
}

func TestDependencyReference_IdentityKey_IgnoresRefAndAlias(t *testing.T) {
	a, err := ParseDepString("acme/foo#v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseDepString("acme/foo#main")
	if err != nil {
		t.Fatal(err)
	}
	if a.IdentityKey() != b.IdentityKey() {
		t.Errorf("IdentityKey should ignore ref: %q vs %q", a.IdentityKey(), b.IdentityKey())
	}
	if a.IdentityKey() != "acme/foo" {
		t.Errorf("IdentityKey = %q, want acme/foo", a.IdentityKey())
	}
}

func TestDependencyReference_IdentityKey_LocalAndParentAreEmpty(t *testing.T) {
	local, err := ParseDepString("./vendor/thing")
	if err != nil {
		t.Fatal(err)
	}
	if local.IdentityKey() != "" {
		t.Errorf("expected empty IdentityKey for a local dep, got %q", local.IdentityKey())
	}

	parent := &DependencyReference{IsParent: true, VirtualPath: "some/path"}
	if parent.IdentityKey() != "" {
		t.Errorf("expected empty IdentityKey for a parent dep, got %q", parent.IdentityKey())
	}
}
