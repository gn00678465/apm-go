package authoring

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/semver"
)

// ── mkt-046 regression: local source, zero flags, zero network ─────────────

// TestAddPackage_LocalSource_NoFlags_NeverTouchesNetwork is the explicit
// mkt-046 regression required by prd.md AC3 and implement.md step 5: a
// local ("./...") source must succeed with *no* flags at all -- no
// --no-verify, no --version, no fake SHA -- and the panicLister proves it
// never even attempts a network call to do so.
func TestAddPackage_LocalSource_NoFlags_NeverTouchesNetwork(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
		"  owner:\n    name: acme\n  packages:\n    - name: existing\n      source: ./pkgs/existing\n")

	// Act
	name, fallbackUsed, err := AddPackage(dir, "./pkgs/tool", AddOptions{}, panicLister{})

	// Assert
	if err != nil {
		t.Fatalf("AddPackage returned error for a local source with zero flags: %v", err)
	}
	if name != "tool" {
		t.Errorf("name = %q, want %q (derived from source)", name, "tool")
	}
	if fallbackUsed {
		t.Error("fallbackUsed = true, want the surgical splice path for a well-formed existing packages: list")
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatalf("LoadAuthoringConfig after add returned error: %v", lerr)
	}
	if len(cfg.Packages) != 2 || cfg.Packages[1].Name != "tool" || cfg.Packages[1].Source != "./pkgs/tool" {
		t.Errorf("Packages = %+v, want existing + tool", cfg.Packages)
	}
}

// TestAddPackage_RemoteSource_VerifiesViaListerAndCanFail proves the other
// half of mkt-046's fix: unlike local sources, a remote source *does* go
// through lister.ListRefs, and a lister failure surfaces as an AddPackage
// error.
func TestAddPackage_RemoteSource_VerifiesViaListerAndCanFail(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")
	failing := &stubLister{err: fmt.Errorf("boom: unreachable")}

	// Act
	_, _, err := AddPackage(dir, "owner/repo", AddOptions{}, failing)

	// Assert
	if err == nil {
		t.Fatal("expected AddPackage to fail when the remote source's lister call fails")
	}
	if !failing.called {
		t.Error("lister.ListRefs was never called for a remote source")
	}
}

// TestAddPackage_RemoteSource_NoVerifySkipsLister proves --no-verify skips
// the lister call for a remote source (design.md: "remote source 才走
// git ls-remote 驗證(--no-verify 跳過)").
func TestAddPackage_RemoteSource_NoVerifySkipsLister(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")

	// Act
	_, _, err := AddPackage(dir, "owner/repo", AddOptions{NoVerify: true}, panicLister{})

	// Assert
	if err != nil {
		t.Fatalf("AddPackage --no-verify returned error: %v", err)
	}
}

// ── --version/--ref mutual exclusion ────────────────────────────────────

func TestAddPackage_VersionAndRefBothGiven_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")

	// Act
	_, _, err := AddPackage(dir, "./pkgs/tool", AddOptions{Version: "^1.0.0", Ref: "v1.0.0"}, panicLister{})

	// Assert
	if err == nil {
		t.Fatal("expected --version and --ref to be rejected as mutually exclusive")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error = %v, want it to mention mutual exclusivity", err)
	}
}

func TestSetPackage_VersionAndRefBothGiven_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
		"  owner:\n    name: acme\n  packages:\n    - name: foo\n      source: ./pkgs/foo\n")
	version, ref := "^1.0.0", "v1.0.0"

	// Act
	_, err := SetPackage(dir, "foo", SetOptions{Version: &version, Ref: &ref})

	// Assert
	if err == nil {
		t.Fatal("expected --version and --ref to be rejected as mutually exclusive")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error = %v, want it to mention mutual exclusivity", err)
	}
}

// TestSetPackage_SettingVersionClearsExistingRef mirrors Python's
// update_plugin_entry: setting one of version/ref clears the other in
// storage, not just at the CLI validation layer.
func TestSetPackage_SettingVersionClearsExistingRef(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
		"  owner:\n    name: acme\n  packages:\n    - name: foo\n      source: ./pkgs/foo\n      ref: v1.0.0\n")
	version := "^2.0.0"

	// Act
	_, err := SetPackage(dir, "foo", SetOptions{Version: &version})

	// Assert
	if err != nil {
		t.Fatalf("SetPackage returned error: %v", err)
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	pkg := cfg.Packages[0]
	if pkg.Version != "^2.0.0" {
		t.Errorf("Version = %q, want %q", pkg.Version, "^2.0.0")
	}
	if pkg.Ref != "" {
		t.Errorf("Ref = %q, want cleared (empty) after --version was set", pkg.Ref)
	}
}

// ── duplicate name (case-insensitive) ───────────────────────────────────

func TestAddPackage_DuplicateNameCaseInsensitive_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
		"  owner:\n    name: acme\n  packages:\n    - name: Foo\n      source: ./pkgs/foo\n")

	// Act
	_, _, err := AddPackage(dir, "./pkgs/other", AddOptions{Name: "foo"}, panicLister{})

	// Assert
	if err == nil {
		t.Fatal("expected a case-insensitive duplicate name to be rejected")
	}
}

// ── add-only default name derivation ────────────────────────────────────

func TestAddPackage_NameFlagOverridesSourceDerivedDefault(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")

	// Act
	name, _, err := AddPackage(dir, "./pkgs/tool", AddOptions{Name: "custom-name"}, panicLister{})

	// Assert
	if err != nil {
		t.Fatalf("AddPackage returned error: %v", err)
	}
	if name != "custom-name" {
		t.Errorf("name = %q, want %q", name, "custom-name")
	}
}

func TestDefaultNameFromSource(t *testing.T) {
	tests := []struct{ source, want string }{
		{"./pkgs/tool", "tool"},
		{"owner/repo", "repo"},
		{"owner/repo.git", "repo"},
		{"owner/repo/", "repo"},
		{"https://example.com/owner/repo.git", "repo"},
	}
	for _, tt := range tests {
		if got := defaultNameFromSource(tt.source); got != tt.want {
			t.Errorf("defaultNameFromSource(%q) = %q, want %q", tt.source, got, tt.want)
		}
	}
}

// ── set's tri-state --include-prerelease ────────────────────────────────

func TestSetPackage_IncludePrereleaseNotGiven_LeavesExistingValueUnchanged(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
		"  owner:\n    name: acme\n  packages:\n    - name: foo\n      source: ./pkgs/foo\n      include_prerelease: true\n")

	// Act: SetOptions with IncludePrerelease left nil (not given) while
	// changing an unrelated field.
	subdir := "packages/foo"
	_, err := SetPackage(dir, "foo", SetOptions{Subdir: &subdir})

	// Assert
	if err != nil {
		t.Fatalf("SetPackage returned error: %v", err)
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if !cfg.Packages[0].IncludePrerelease {
		t.Error("IncludePrerelease flipped to false despite not being given to SetPackage (tri-state contract broken)")
	}
}

func TestSetPackage_IncludePrereleaseExplicitFalse_ClearsExistingTrue(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
		"  owner:\n    name: acme\n  packages:\n    - name: foo\n      source: ./pkgs/foo\n      include_prerelease: true\n")
	falseVal := false

	// Act
	_, err := SetPackage(dir, "foo", SetOptions{IncludePrerelease: &falseVal})

	// Assert
	if err != nil {
		t.Fatalf("SetPackage returned error: %v", err)
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if cfg.Packages[0].IncludePrerelease {
		t.Error("IncludePrerelease still true after an explicit --include-prerelease=false")
	}
}

// ── remove ───────────────────────────────────────────────────────────────

func TestRemovePackage_RemovesByCaseInsensitiveName(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
		"  owner:\n    name: acme\n  packages:\n    - name: Foo\n      source: ./pkgs/foo\n"+
		"    - name: bar\n      source: ./pkgs/bar\n")

	// Act
	_, err := RemovePackage(dir, "foo")

	// Assert
	if err != nil {
		t.Fatalf("RemovePackage returned error: %v", err)
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(cfg.Packages) != 1 || cfg.Packages[0].Name != "bar" {
		t.Errorf("Packages = %+v, want only 'bar' left", cfg.Packages)
	}
}

func TestRemovePackage_NotFound_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")

	// Act
	_, err := RemovePackage(dir, "nonexistent")

	// Assert
	if err == nil {
		t.Fatal("expected removing a nonexistent package to error")
	}
}

// TestRemovePackage_OutputsIncludeCodex_MissingCategoryDoesNotBlockEdit is
// F3's regression test: mkt-053's codex-category-required gate is
// compose-time-only (internal/marketplace/build/codexmapper.go's
// CategoryRequiredError) and must never block `apm marketplace package
// remove` -- even when removing the very package whose missing category
// would otherwise break a codex build. Before the fix, LoadAuthoringConfig
// itself (called by RemovePackage to locate the package) rejected this
// config before removal was ever attempted.
func TestRemovePackage_OutputsIncludeCodex_MissingCategoryDoesNotBlockEdit(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
		"  owner:\n    name: acme\n  outputs:\n    codex: {}\n  packages:\n"+
		"    - name: bad-pkg\n      source: owner/repo\n      version: \">=1.0.0\"\n")

	// Act
	_, err := RemovePackage(dir, "bad-pkg")

	// Assert
	if err != nil {
		t.Fatalf("RemovePackage returned error (F3: codex category gate must be compose-time only): %v", err)
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(cfg.Packages) != 0 {
		t.Errorf("Packages = %+v, want empty after removing bad-pkg", cfg.Packages)
	}
}

func TestSetPackage_NotFound_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")
	v := "^1.0.0"

	// Act
	_, err := SetPackage(dir, "nonexistent", SetOptions{Version: &v})

	// Assert
	if err == nil {
		t.Fatal("expected setting a nonexistent package to error")
	}
}

// ── 舊坑1: hand-authored, commented apm.yml -- edit only the target entry ──

const handAuthoredPackagesApmYML = `name: demo
version: "1.0.0"
description: A demo project

marketplace:
  owner:
    name: acme-org
    url: https://github.com/acme-org
  build:
    tagPattern: "v{version}"
  packages:
    # foo is our flagship plugin
    - name: foo
      description: Flagship tool
      source: ./packages/foo
      tags: [cli, flagship]
    - name: bar # legacy compatibility shim
      description: Legacy shim
      source: ./packages/bar
      version: "^1.0.0"
scripts:
  build: echo hi   # inline comment kept exactly
`

func TestAddPackage_HandAuthoredFixture_OnlyAppendsTargetEntry(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", handAuthoredPackagesApmYML)

	// Act
	_, fallbackUsed, err := AddPackage(dir, "./packages/qux", AddOptions{Name: "qux"}, panicLister{})

	// Assert
	if err != nil {
		t.Fatalf("AddPackage returned error: %v", err)
	}
	if fallbackUsed {
		t.Error("fallbackUsed = true, want the surgical splice path")
	}
	data, rerr := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	content := string(data)
	if !strings.Contains(content, "# foo is our flagship plugin") {
		t.Errorf("foo's leading comment was lost:\n%s", content)
	}
	if !strings.Contains(content, "name: bar # legacy compatibility shim") {
		t.Errorf("bar's inline comment was lost:\n%s", content)
	}
	if !strings.Contains(content, "build: echo hi   # inline comment kept exactly") {
		t.Errorf("unrelated scripts: block was altered:\n%s", content)
	}
	if !strings.Contains(content, "name: qux") {
		t.Errorf("new package 'qux' not found in output:\n%s", content)
	}
}

func TestRemovePackage_HandAuthoredFixture_PreservesOtherEntry(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", handAuthoredPackagesApmYML)

	// Act
	_, err := RemovePackage(dir, "bar")

	// Assert
	if err != nil {
		t.Fatalf("RemovePackage returned error: %v", err)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	content := string(data)
	if strings.Contains(content, "name: bar") {
		t.Errorf("removed package 'bar' is still present:\n%s", content)
	}
	if !strings.Contains(content, "# foo is our flagship plugin") || !strings.Contains(content, "name: foo") {
		t.Errorf("untouched package 'foo' (and its comment) was altered:\n%s", content)
	}
	if !strings.Contains(content, "build: echo hi   # inline comment kept exactly") {
		t.Errorf("unrelated scripts: block was altered:\n%s", content)
	}
}

func TestSetPackage_HandAuthoredFixture_OnlyReplacesTargetEntry(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", handAuthoredPackagesApmYML)
	subdir := "nested/foo"

	// Act
	_, err := SetPackage(dir, "foo", SetOptions{Subdir: &subdir})

	// Assert
	if err != nil {
		t.Fatalf("SetPackage returned error: %v", err)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	content := string(data)
	if !strings.Contains(content, "subdir: nested/foo") {
		t.Errorf("foo's new subdir not found:\n%s", content)
	}
	if !strings.Contains(content, "# foo is our flagship plugin") {
		t.Errorf("foo's leading comment (belongs to the sequence) was lost:\n%s", content)
	}
	if !strings.Contains(content, "name: bar # legacy compatibility shim") {
		t.Errorf("untouched package 'bar' (and its inline comment) was altered:\n%s", content)
	}
	if !strings.Contains(content, "build: echo hi   # inline comment kept exactly") {
		t.Errorf("unrelated scripts: block was altered:\n%s", content)
	}
}

// ── legacy marketplace.yml (prefix == nil) ──────────────────────────────

func TestAddPackage_LegacyMarketplaceYML_EditsRootDocument(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "marketplace.yml", "owner:\n  name: acme\npackages:\n  - name: foo\n    source: ./pkgs/foo\n")

	// Act
	_, _, err := AddPackage(dir, "./pkgs/bar", AddOptions{}, panicLister{})

	// Assert
	if err != nil {
		t.Fatalf("AddPackage against a legacy marketplace.yml returned error: %v", err)
	}
	cfg, src, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if src != ConfigSourceLegacy {
		t.Errorf("ConfigSource = %v, want ConfigSourceLegacy", src)
	}
	if len(cfg.Packages) != 2 {
		t.Errorf("Packages = %+v, want 2 entries", cfg.Packages)
	}
}

func TestAddPackage_BothConfigsExist_ReturnsMutualExclusionError(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")
	writeFile(t, dir, "marketplace.yml", "owner:\n  name: acme\npackages: []\n")

	// Act
	_, _, err := AddPackage(dir, "./pkgs/foo", AddOptions{}, panicLister{})

	// Assert
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error = %v, want the mkt-047 mutual-exclusion error", err)
	}
}

func TestAddPackage_NoConfigAtAll_PointsAtInit(t *testing.T) {
	// Arrange
	dir := t.TempDir()

	// Act
	_, _, err := AddPackage(dir, "./pkgs/foo", AddOptions{}, panicLister{})

	// Assert
	if err == nil || !strings.Contains(err.Error(), "apm marketplace init") {
		t.Errorf("error = %v, want it to point at 'apm marketplace init'", err)
	}
}

// ── fallback path: no existing packages: key, or a flow-style one ───────

func TestAddPackage_NoPackagesKeyYet_UsesFallbackAndSucceeds(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n")

	// Act
	_, fallbackUsed, err := AddPackage(dir, "./pkgs/tool", AddOptions{}, panicLister{})

	// Assert
	if err != nil {
		t.Fatalf("AddPackage returned error when packages: is entirely absent: %v", err)
	}
	if !fallbackUsed {
		t.Error("fallbackUsed = false, want the whole-value-replace fallback when packages: does not exist yet")
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(cfg.Packages) != 1 || cfg.Packages[0].Name != "tool" {
		t.Errorf("Packages = %+v, want a single 'tool' entry", cfg.Packages)
	}
}

func TestAddPackage_EmptyFlowStylePackagesList_UsesFallbackAndSucceeds(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")

	// Act
	_, fallbackUsed, err := AddPackage(dir, "./pkgs/tool", AddOptions{}, panicLister{})

	// Assert
	if err != nil {
		t.Fatalf("AddPackage returned error for an empty flow-style packages: []: %v", err)
	}
	if !fallbackUsed {
		t.Error("fallbackUsed = false, want the whole-value-replace fallback for a flow-style sequence")
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(cfg.Packages) != 1 || cfg.Packages[0].Name != "tool" {
		t.Errorf("Packages = %+v, want a single 'tool' entry", cfg.Packages)
	}
}

// ── atomic + verify + rollback (Review Gate A) ──────────────────────────

// TestEditPackagesFile_ForcedValidationFailure_LeavesFileByteExactUnchanged
// is the "回滾" test implement.md step 5 asks for: forcing the post-splice,
// pre-write validation step to fail must leave the file on disk completely
// untouched -- the memory-first validate-before-write contract achieving
// the same observable "never a corrupted file on disk" guarantee as
// Python's write-then-validate-then-restore-original, per design.md.
func TestEditPackagesFile_ForcedValidationFailure_LeavesFileByteExactUnchanged(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	original := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n" +
		"  packages:\n" +
		"    - name: foo\n      source: ./pkgs/foo\n"
	writeFile(t, dir, "apm.yml", original)

	origValidate := packageEditValidate
	packageEditValidate = func(out []byte, prefix []string) error {
		return fmt.Errorf("forced validation failure for test")
	}
	t.Cleanup(func() { packageEditValidate = origValidate })

	// Act
	_, _, err := AddPackage(dir, "./pkgs/bar", AddOptions{}, panicLister{})

	// Assert
	if err == nil {
		t.Fatal("expected AddPackage to fail when post-edit validation is forced to fail")
	}
	data, rerr := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(data) != original {
		t.Errorf("apm.yml was modified despite a forced validation failure;\ngot:\n%s\nwant unchanged:\n%s", string(data), original)
	}
}

// ── validateEditedPackageBytes: the real implementation's own negative
// branches (as opposed to the injected packageEditValidate double the
// rollback test above uses) ─────────────────────────────────────────────

func TestValidateEditedPackageBytes_RejectsUnparsableYAML(t *testing.T) {
	if err := validateEditedPackageBytes([]byte("not: [valid: yaml"), nil); err == nil {
		t.Fatal("expected an error for unparsable YAML")
	}
}

func TestValidateEditedPackageBytes_RejectsMissingPrefixKey(t *testing.T) {
	if err := validateEditedPackageBytes([]byte("name: demo\n"), []string{"marketplace"}); err == nil {
		t.Fatal("expected an error when the prefix key ('marketplace') is missing")
	}
}

func TestValidateEditedPackageBytes_AcceptsValidLegacyDocument(t *testing.T) {
	if err := validateEditedPackageBytes([]byte("owner:\n  name: acme\npackages: []\n"), nil); err != nil {
		t.Errorf("unexpected error for a valid legacy-shaped document: %v", err)
	}
}

// ── test doubles ──────────────────────────────────────────────────────────

// stubLister is a RefLister test double that records whether it was called
// and returns a canned error (or success with no refs).
type stubLister struct {
	called bool
	err    error
}

func (s *stubLister) ListRefs(source string) ([]semver.TagInfo, error) {
	s.called = true
	if s.err != nil {
		return nil, s.err
	}
	return nil, nil
}
