package authoring

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode"

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
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}

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

// TestAddPackage_RemoteSource_NoVerify_ImplicitHead_Errors is R5/AC18's
// corrected contract, replacing this test's former assertion (that
// --no-verify with no --ref at all silently succeeded with no `ref:`
// written): design.md's decision tree makes a missing --ref an *implicit*
// HEAD pin for a remote source, and resolving HEAD to a concrete SHA
// requires a network call -- exactly the one --no-verify forbids. So
// --no-verify only ever skips *reachability* verification
// (verifyPackageSource, still proven by TestAddPackage_ShaRef_
// StoredVerbatim_NoListerCall below), never HEAD resolution: this
// combination must now fail with the exact upstream-parity message, not
// silently write an unpinned entry. panicLister here additionally proves
// the failure itself never touches the network either (the error fires
// before any lister call).
func TestAddPackage_RemoteSource_NoVerify_ImplicitHead_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")

	// Act
	_, _, err := AddPackage(dir, "owner/repo", AddOptions{NoVerify: true}, panicLister{})

	// Assert
	if err == nil {
		t.Fatal("expected AddPackage to fail: --no-verify cannot resolve an implicit HEAD ref without network access (AC18)")
	}
	if !strings.Contains(err.Error(), "Cannot resolve HEAD ref without network access. Provide an explicit --ref SHA.") {
		t.Errorf("error = %v, want the exact upstream-parity HEAD/--no-verify message", err)
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

// TestAddPackage_VersionGiven_RemoteSource_StillCallsListerForReachability is
// AC20's second half (checklist.md AC20, R5.4): a --version range on a
// *remote* source must skip ref *resolution* (proven by
// TestMarketplacePackageAdd_VersionGiven_DoesNotWriteRef in
// cmd/apm-go/marketplace_package_test.go) but must NOT skip the
// verifyPackageSource reachability check -- that check still calls
// lister.ListRefs exactly once. The CLI-level test above only proves "no
// ref: written" against an actually-reachable fixture; it would still pass
// even if the reachability call were deleted entirely. This test closes that
// gap with an explicit stubLister.called assertion, directly against
// AddPackage rather than through the CLI.
func TestAddPackage_VersionGiven_RemoteSource_StillCallsListerForReachability(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")
	lister := &stubLister{}

	// Act
	_, _, err := AddPackage(dir, "owner/repo", AddOptions{Version: "^1.0.0"}, lister)

	// Assert
	if err != nil {
		t.Fatalf("AddPackage with --version against a reachable remote source returned error: %v", err)
	}
	if !lister.called {
		t.Error("lister.ListRefs was never called: --version must still trigger verifyPackageSource's reachability check (AC20)")
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(cfg.Packages) != 1 || cfg.Packages[0].Ref != "" {
		t.Errorf("Packages = %+v, want the new entry with no Ref (a --version range never resolves/writes a ref)", cfg.Packages)
	}
}

func TestSetPackage_VersionAndRefBothGiven_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
		"  owner:\n    name: acme\n  packages:\n    - name: foo\n      source: ./pkgs/foo\n")
	version, ref := "^1.0.0", "v1.0.0"

	// Act
	_, err := SetPackage(dir, "foo", SetOptions{Version: &version, Ref: &ref}, panicLister{})

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
	_, err := SetPackage(dir, "foo", SetOptions{Version: &version}, panicLister{})

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
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "other"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Act
	_, _, err := AddPackage(dir, "./pkgs/other", AddOptions{Name: "foo"}, panicLister{})

	// Assert
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v, want a case-insensitive duplicate name to be rejected", err)
	}
}

// ── add-only default name derivation ────────────────────────────────────

func TestAddPackage_NameFlagOverridesSourceDerivedDefault(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}

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
	_, err := SetPackage(dir, "foo", SetOptions{Subdir: &subdir}, panicLister{})

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
	_, err := SetPackage(dir, "foo", SetOptions{IncludePrerelease: &falseVal}, panicLister{})

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

// ── S2 security fix: --subdir path-traversal rejection ─────────────────
//
// Mirrors Python's yml_editor._validate_subdir / path_security.
// validate_path_segments(subdir, context="subdir"): any "." or ".."
// path segment, or an absolute path, must be rejected on both `add` and
// `set` before the entry is ever written to apm.yml.

func TestAddPackage_SubdirTraversal_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	original := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	writeFile(t, dir, "apm.yml", original)

	// Act
	_, _, err := AddPackage(dir, "./pkgs/tool", AddOptions{Subdir: "../../etc"}, panicLister{})

	// Assert
	if err == nil {
		t.Fatal("expected AddPackage to reject a --subdir containing '..' traversal segments")
	}
	if !strings.Contains(err.Error(), "../../etc") {
		t.Errorf("error = %v, want it to name the offending subdir", err)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(data) != original {
		t.Errorf("apm.yml was modified despite a rejected --subdir;\ngot:\n%s\nwant unchanged:\n%s", string(data), original)
	}
}

func TestAddPackage_SubdirAbsolutePath_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")

	// Act
	_, _, err := AddPackage(dir, "./pkgs/tool", AddOptions{Subdir: "/etc/passwd"}, panicLister{})

	// Assert
	if err == nil {
		t.Fatal("expected AddPackage to reject an absolute --subdir")
	}
}

func TestAddPackage_SubdirLegitimateRelative_Succeeds(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Act
	_, _, err := AddPackage(dir, "./pkgs/tool", AddOptions{Subdir: "src/skills"}, panicLister{})

	// Assert
	if err != nil {
		t.Fatalf("AddPackage rejected a legitimate relative --subdir: %v", err)
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(cfg.Packages) != 1 || cfg.Packages[0].Subdir != "src/skills" {
		t.Errorf("Packages = %+v, want a single entry with subdir 'src/skills'", cfg.Packages)
	}
}

// ── BLOCKING 2 (2026-07-31 follow-up, live end-to-end reproduction):
// `package add`'s local-source path-containment check ─────────────────────
//
// verifyPackageSource's local branch used to unconditionally `return nil`
// with NO path check at all: `package add ./linked`, where "linked" is a
// directory symlink or Windows junction (the latter needs no special
// privilege to create) physically inside the project directory but pointing
// outside it, was accepted outright -- no call anywhere in AddPackage's
// pipeline ever resolved or containment-checked the local source's actual
// path. A subsequent `pack` then faithfully read the escaping target's
// apm.yml contents into the marketplace.json output. This drives AddPackage
// through its real pipeline (manifest.ValidateMarketplaceSource, then
// verifyPackageSource) rather than isolating either check alone, since a
// percent-encoded traversal (e.g. "./%2e%2e/outside") is rejected at the
// manifest layer -- it is a literal, non-decoded "%2e%2e" path segment at
// the OS/filesystem level, so it is NOT itself a filesystem escape for
// verifyPackageSource's own containment check to catch -- while a symlink or
// junction escape is invisible to the manifest layer's string-only check and
// is only caught by verifyPackageSource's new containment check. Together
// the two layers close every angle; testing either in isolation would miss
// the other's case entirely, and is not how AddPackage's callers ever invoke
// them anyway (none of AddPackage's other tests call verifyPackageSource
// directly either -- it is unexported and always exercised through
// AddPackage, the same convention this test follows).
func TestVerifyPackageSource_LocalSourceEscapingRoot_Rejected(t *testing.T) {
	t.Run("backslash traversal", func(t *testing.T) {
		parent := t.TempDir()
		dir := filepath.Join(parent, "project")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")

		_, _, err := AddPackage(dir, `./..\..\outside`, AddOptions{}, panicLister{})
		if err == nil {
			t.Fatal("AddPackage(`./..\\..\\outside`) = nil error, want a rejection (path escapes the project root)")
		}
	})

	t.Run("percent-encoded traversal", func(t *testing.T) {
		parent := t.TempDir()
		dir := filepath.Join(parent, "project")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")

		_, _, err := AddPackage(dir, "./%2e%2e/outside", AddOptions{}, panicLister{})
		if err == nil {
			t.Fatal(`AddPackage("./%2e%2e/outside") = nil error, want a rejection (percent-encoded ".." path segment)`)
		}
	})

	t.Run("junction escape", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("directory junctions are a Windows-only concept")
		}
		parent := t.TempDir()
		dir := filepath.Join(parent, "project")
		outside := filepath.Join(parent, "outside")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(outside, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")
		writeFile(t, outside, "apm.yml", "name: outside-secret\nversion: 9.9.9\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")

		link := filepath.Join(dir, "linked")
		cmd := exec.Command("cmd", "/c", "mklink", "/J", link, outside)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("SKIPPED: cannot create a directory junction in this environment (%v: %s); the junction-escape guard is untested by this run", err, out)
		}

		_, _, err := AddPackage(dir, "./linked", AddOptions{}, panicLister{})
		if err == nil {
			t.Fatal(`AddPackage("./linked" via a junction) = nil error, want a rejection (junction resolves outside the project root)`)
		}
	})
}

// TestAddPackage_LocalSource_LegitimateNestedExistingPath_Succeeds is the
// regression companion to TestVerifyPackageSource_LocalSourceEscapingRoot_
// Rejected above: the containment check in verifyPackageSource must not
// reject an ordinary, legitimate, existing local source that genuinely
// stays within the project root -- a real, existing nested directory here,
// as opposed to that test's escaping ones. (Ticket 20 AC1 later made
// existence itself a requirement -- see verifyLocalSourceExists -- so
// "doesn't exist yet" is no longer a case any AddPackage call succeeds on.)
func TestAddPackage_LocalSource_LegitimateNestedExistingPath_Succeeds(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "tool-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")

	// Act
	name, _, err := AddPackage(dir, "./pkgs/tool-a", AddOptions{}, panicLister{})

	// Assert
	if err != nil {
		t.Fatalf("AddPackage rejected a legitimate, existing local source: %v", err)
	}
	if name != "tool-a" {
		t.Errorf("name = %q, want %q", name, "tool-a")
	}
}

func TestSetPackage_SubdirTraversal_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	original := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: foo\n      source: ./pkgs/foo\n"
	writeFile(t, dir, "apm.yml", original)
	subdir := "../../etc"

	// Act
	_, err := SetPackage(dir, "foo", SetOptions{Subdir: &subdir}, panicLister{})

	// Assert
	if err == nil {
		t.Fatal("expected SetPackage to reject a --subdir containing '..' traversal segments")
	}
	if !strings.Contains(err.Error(), "../../etc") {
		t.Errorf("error = %v, want it to name the offending subdir", err)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(data) != original {
		t.Errorf("apm.yml was modified despite a rejected --subdir;\ngot:\n%s\nwant unchanged:\n%s", string(data), original)
	}
}

func TestSetPackage_SubdirLegitimateRelative_Succeeds(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
		"  owner:\n    name: acme\n  packages:\n    - name: foo\n      source: ./pkgs/foo\n")
	subdir := "src/skills"

	// Act
	_, err := SetPackage(dir, "foo", SetOptions{Subdir: &subdir}, panicLister{})

	// Assert
	if err != nil {
		t.Fatalf("SetPackage rejected a legitimate relative --subdir: %v", err)
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if cfg.Packages[0].Subdir != "src/skills" {
		t.Errorf("Subdir = %q, want %q", cfg.Packages[0].Subdir, "src/skills")
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
	_, err := SetPackage(dir, "nonexistent", SetOptions{Version: &v}, panicLister{})

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
	if err := os.MkdirAll(filepath.Join(dir, "packages", "qux"), 0o755); err != nil {
		t.Fatal(err)
	}

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
	_, err := SetPackage(dir, "foo", SetOptions{Subdir: &subdir}, panicLister{})

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
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "bar"), 0o755); err != nil {
		t.Fatal(err)
	}

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
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}

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
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Act
	_, _, err := AddPackage(dir, "./pkgs/foo", AddOptions{}, panicLister{})

	// Assert
	if err == nil || !strings.Contains(err.Error(), "apm-go marketplace init") {
		t.Errorf("error = %v, want it to point at 'apm-go marketplace init'", err)
	}
}

// ── fallback path: no existing packages: key, or a flow-style one ───────

func TestAddPackage_NoPackagesKeyYet_UsesFallbackAndSucceeds(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n")
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}

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
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}

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
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "bar"), 0o755); err != nil {
		t.Fatal(err)
	}

	origValidate := packageEditValidate
	packageEditValidate = func(out []byte, prefix []string) error {
		return fmt.Errorf("forced validation failure for test")
	}
	t.Cleanup(func() { packageEditValidate = origValidate })

	// Act
	_, _, err := AddPackage(dir, "./pkgs/bar", AddOptions{}, panicLister{})

	// Assert
	if err == nil || !strings.Contains(err.Error(), "forced validation failure") {
		t.Fatalf("error = %v, want the forced validation failure to be surfaced", err)
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

// ── F4: mutable-ref auto-resolution (marketplace.md:253-254's documented
// promise: "Mutable refs (HEAD, branches) are auto-resolved to a concrete
// SHA at write time") ───────────────────────────────────────────────────

const testResolvedSHA = "abcdef0123456789abcdef0123456789abcdef01"

func TestAddPackage_MutableRef_ResolvesToConcreteSHA(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")
	lister := mapRefLister{refs: []semver.TagInfo{{Name: "main", Commit: testResolvedSHA}}}

	// Act
	_, _, err := AddPackage(dir, "owner/repo", AddOptions{Ref: "main"}, lister)

	// Assert
	if err != nil {
		t.Fatalf("AddPackage returned error: %v", err)
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(cfg.Packages) != 1 || cfg.Packages[0].Ref != testResolvedSHA {
		t.Errorf("Packages = %+v, want a single entry pinned to the resolved SHA %q", cfg.Packages, testResolvedSHA)
	}
}

func TestAddPackage_ShaRef_StoredVerbatim_NoListerCall(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")

	// Act: NoVerify skips verifyPackageSource's own (unrelated) lister
	// call, isolating this assertion to ref resolution; panicLister then
	// proves an already-concrete 40-hex SHA never triggers a lister call
	// -- there is nothing to resolve.
	_, _, err := AddPackage(dir, "owner/repo", AddOptions{Ref: testResolvedSHA, NoVerify: true}, panicLister{})

	// Assert
	if err != nil {
		t.Fatalf("AddPackage returned error for an already-concrete SHA ref: %v", err)
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(cfg.Packages) != 1 || cfg.Packages[0].Ref != testResolvedSHA {
		t.Errorf("Packages = %+v, want Ref stored verbatim as %q", cfg.Packages, testResolvedSHA)
	}
}

func TestAddPackage_UnresolvableRef_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	original := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	writeFile(t, dir, "apm.yml", original)
	lister := mapRefLister{refs: []semver.TagInfo{{Name: "main", Commit: testResolvedSHA}}}

	// Act
	_, _, err := AddPackage(dir, "owner/repo", AddOptions{Ref: "does-not-exist"}, lister)

	// Assert
	if err == nil {
		t.Fatal("expected AddPackage to fail when --ref cannot be resolved on the remote")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error = %v, want it to name the unresolved ref", err)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(data) != original {
		t.Errorf("apm.yml was modified despite an unresolvable --ref;\ngot:\n%s\nwant unchanged:\n%s", string(data), original)
	}
}

func TestAddPackage_RefResolutionListerFailure_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")
	lister := mapRefLister{err: fmt.Errorf("boom: unreachable")}

	// Act: NoVerify skips verifyPackageSource's own (unrelated) lister
	// call, isolating this assertion to resolveRef's own failure handling.
	_, _, err := AddPackage(dir, "owner/repo", AddOptions{Ref: "main", NoVerify: true}, lister)

	// Assert
	if err == nil {
		t.Fatal("expected AddPackage to fail when the ref lister call itself fails")
	}
}

func TestSetPackage_MutableRef_ResolvesToConcreteSHA(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
		"  owner:\n    name: acme\n  packages:\n    - name: foo\n      source: owner/repo\n")
	lister := mapRefLister{refs: []semver.TagInfo{{Name: "develop", Commit: testResolvedSHA}}}
	ref := "develop"

	// Act
	_, err := SetPackage(dir, "foo", SetOptions{Ref: &ref}, lister)

	// Assert
	if err != nil {
		t.Fatalf("SetPackage returned error: %v", err)
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if cfg.Packages[0].Ref != testResolvedSHA {
		t.Errorf("Ref = %q, want resolved SHA %q", cfg.Packages[0].Ref, testResolvedSHA)
	}
}

func TestSetPackage_ShaRef_StoredVerbatim_NoListerCall(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
		"  owner:\n    name: acme\n  packages:\n    - name: foo\n      source: owner/repo\n")
	ref := testResolvedSHA

	// Act: panicLister proves an already-concrete SHA never triggers a
	// lister call.
	_, err := SetPackage(dir, "foo", SetOptions{Ref: &ref}, panicLister{})

	// Assert
	if err != nil {
		t.Fatalf("SetPackage returned error for an already-concrete SHA ref: %v", err)
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if cfg.Packages[0].Ref != testResolvedSHA {
		t.Errorf("Ref = %q, want stored verbatim as %q", cfg.Packages[0].Ref, testResolvedSHA)
	}
}

func TestSetPackage_UnresolvableRef_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	original := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: foo\n      source: owner/repo\n"
	writeFile(t, dir, "apm.yml", original)
	lister := mapRefLister{refs: []semver.TagInfo{{Name: "main", Commit: testResolvedSHA}}}
	ref := "does-not-exist"

	// Act
	_, err := SetPackage(dir, "foo", SetOptions{Ref: &ref}, lister)

	// Assert
	if err == nil {
		t.Fatal("expected SetPackage to fail when --ref cannot be resolved on the remote")
	}
	data, rerr := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(data) != original {
		t.Errorf("apm.yml was modified despite an unresolvable --ref;\ngot:\n%s\nwant unchanged:\n%s", string(data), original)
	}
}

// ── BLOCKING 3 (external audit round 4, 2026-07-30): `set --ref HEAD` must
// invoke SetOptions.OnExplicitHeadWillResolve, the same as AddOptions'
// equivalent hook -- SetPackage used to hardcode nil for resolveRef's
// onExplicitHeadWillResolve parameter, so `set` never printed upstream's
// mutable-ref warning at all. ────────────────────────────────────────────

// TestSetPackage_ExplicitRefHead_InvokesOnExplicitHeadWillResolve proves the
// callback actually fires for an explicit "HEAD" --ref.
func TestSetPackage_ExplicitRefHead_InvokesOnExplicitHeadWillResolve(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
		"  owner:\n    name: acme\n  packages:\n    - name: foo\n      source: owner/repo\n")
	lister := mapRefLister{refs: []semver.TagInfo{{Name: "HEAD", Commit: testResolvedSHA}}}
	ref := "HEAD"
	called := 0

	// Act
	_, err := SetPackage(dir, "foo", SetOptions{Ref: &ref, OnExplicitHeadWillResolve: func() { called++ }}, lister)

	// Assert
	if err != nil {
		t.Fatalf("SetPackage returned error resolving --ref HEAD: %v", err)
	}
	if called != 1 {
		t.Errorf("OnExplicitHeadWillResolve called %d times, want exactly 1", called)
	}
}

// TestSetPackage_NonHeadRef_DoesNotInvokeOnExplicitHeadWillResolve is the
// negative control: an ordinary tag ref must never fire the HEAD-specific
// hook.
func TestSetPackage_NonHeadRef_DoesNotInvokeOnExplicitHeadWillResolve(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
		"  owner:\n    name: acme\n  packages:\n    - name: foo\n      source: owner/repo\n")
	lister := mapRefLister{refs: []semver.TagInfo{{Name: "v1.0.0", Commit: testResolvedSHA}}}
	ref := "v1.0.0"
	called := 0

	// Act
	_, err := SetPackage(dir, "foo", SetOptions{Ref: &ref, OnExplicitHeadWillResolve: func() { called++ }}, lister)

	// Assert
	if err != nil {
		t.Fatalf("SetPackage returned error: %v", err)
	}
	if called != 0 {
		t.Errorf("OnExplicitHeadWillResolve called %d times for a non-HEAD ref, want 0", called)
	}
}

// ── BLOCKING 2 (external audit, 2026-07-30): `package set --ref` on a
// local package must still resolve the given ref via lister, not silently
// clear it to "". Reported repro: an entry with both `version:` and a
// stale `ref:` already set, `set foo --ref main` on a `./localpkg` source
// used to leave BOTH fields empty and report success -- resolveRef's local
// short-circuit (mkt-046, add-only) was applying unconditionally. Fixed by
// threading skipLocalSource=false through SetPackage's own resolveRef call
// (editor.go). install-marketplace-contracts.md:87 documents `set` as
// always resolving a given ref with no local-source exemption. ──────────

// TestSetPackage_LocalSource_MutableRef_ResolvesToConcreteSHA is the "ref
// preserved/resolved" half of BLOCKING 2's regression: --ref on a local
// package must come back as the lister-resolved SHA, not "".
func TestSetPackage_LocalSource_MutableRef_ResolvesToConcreteSHA(t *testing.T) {
	// Arrange: mirrors the reported repro's odd dual-pinned fixture (both
	// version: and a stale ref: already present on one entry).
	dir := t.TempDir()
	staleSHA := "abcdef1234567890abcdef1234567890abcdef12"
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
		"  owner:\n    name: acme\n  packages:\n    - name: foo\n      source: ./localpkg\n"+
		"      version: ^1.0.0\n      ref: "+staleSHA+"\n")
	lister := mapRefLister{refs: []semver.TagInfo{{Name: "main", Commit: testResolvedSHA}}}
	ref := "main"

	// Act
	_, err := SetPackage(dir, "foo", SetOptions{Ref: &ref}, lister)

	// Assert
	if err != nil {
		t.Fatalf("SetPackage returned error: %v", err)
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if cfg.Packages[0].Ref != testResolvedSHA {
		t.Errorf("Ref = %q, want the resolved SHA %q (a local package's explicitly-given --ref must still resolve, not silently clear to empty)", cfg.Packages[0].Ref, testResolvedSHA)
	}
	// version is expected to clear here -- opts.Ref != nil always clears the
	// other of version/ref in storage (mirrors Python's update_plugin_entry,
	// see TestSetPackage_SettingVersionClearsExistingRef above); the bug was
	// that Ref ALSO silently became "", not that Version stayed.
	if cfg.Packages[0].Version != "" {
		t.Errorf("Version = %q, want cleared (giving --ref clears the other of version/ref in storage)", cfg.Packages[0].Version)
	}
}

// TestSetPackage_LocalSource_UnrelatedFieldChange_PreservesVersion is the
// "version: NOT clobbered" half of BLOCKING 2's regression: a `set` call
// that does not touch --ref at all (SetOptions.Ref == nil, so resolveRef is
// never invoked) must leave a local package's existing version: untouched,
// exactly like the pre-existing remote-source contract
// (TestSetPackage_IncludePrereleaseNotGiven_LeavesExistingValueUnchanged)
// -- proving the resolveRef signature change did not introduce a new way to
// touch fields an unrelated `set` call never mentioned.
func TestSetPackage_LocalSource_UnrelatedFieldChange_PreservesVersion(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
		"  owner:\n    name: acme\n  packages:\n    - name: foo\n      source: ./localpkg\n"+
		"      version: ^1.0.0\n")
	subdir := "skills"

	// Act: change --subdir only, panicLister proves no network I/O at all.
	_, err := SetPackage(dir, "foo", SetOptions{Subdir: &subdir}, panicLister{})

	// Assert
	if err != nil {
		t.Fatalf("SetPackage returned error: %v", err)
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if cfg.Packages[0].Version != "^1.0.0" {
		t.Errorf("Version = %q, want ^1.0.0 preserved (an unrelated set must not clobber it)", cfg.Packages[0].Version)
	}
	if cfg.Packages[0].Subdir != "skills" {
		t.Errorf("Subdir = %q, want skills", cfg.Packages[0].Subdir)
	}
}

// TestSetPackage_RelativeLocalSource_MutableRef_ResolvesViaProductionLister
// is MAJOR 2's SetPackage-level end-to-end regression (external audit round
// 2, 2026-07-30): the config's own package source is a relative "./..."
// path (the only shape a real local package's source ever takes -- see
// manifest.ValidateMarketplaceSource), and this uses gitRefLister{} (the
// real production RefLister, not mapRefLister) against a real git repo
// fixture. Before MAJOR 2's resolveCloneURL fix, this failed with a bogus
// "git ls-remote https://github.com/./pkgs/tool.git: remote: Not Found"
// instead of resolving against the real local repository.
func TestSetPackage_RelativeLocalSource_MutableRef_ResolvesViaProductionLister(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "pkgs", "tool")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	wantSHA := gitCmd(t, repoDir, "rev-parse", "v1.0.0")
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./pkgs/tool\n")
	chdirTo(t, dir)
	ref := "v1.0.0"

	// Act
	_, err := SetPackage(".", "tool", SetOptions{Ref: &ref}, gitRefLister{})

	// Assert
	if err != nil {
		t.Fatalf("SetPackage with a relative local source's --ref returned error: %v", err)
	}
	cfg, _, lerr := LoadAuthoringConfig(".")
	if lerr != nil {
		t.Fatal(lerr)
	}
	if cfg.Packages[0].Ref != wantSHA {
		t.Errorf("Ref = %q, want the resolved SHA %q (a relative local source's --ref must resolve through the real production lister, not fail closed)", cfg.Packages[0].Ref, wantSHA)
	}
}

// TestAddPackage_RefHEAD_ResolvesToConcreteSHA is the F4/marketplace.md
// regression: `--ref HEAD` is documented ("Pin to a git ref (SHA, tag, or
// HEAD)") to auto-resolve to a concrete SHA, but resolveRef's lookup went
// through the production gitRefLister's `--tags --heads`-filtered ref list,
// which can never contain a "HEAD" entry -- so `--ref HEAD` always failed
// with "ref \"HEAD\" not found", contradicting the documented contract.
// Proven end to end (AddPackage -> real gitRefLister -> real local git repo
// fixture, no network) rather than against a hand-built fake ref list, so a
// regression in the actual git ls-remote invocation is caught too.
func TestAddPackage_RefHEAD_ResolvesToConcreteSHA(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	wantSHA := gitCmd(t, repoDir, "rev-parse", "HEAD")

	// Act: AddPackage's own source argument must be req-mf-017-compliant
	// (BLOCKING 1, external audit round 4, 2026-07-30:
	// manifest.ValidateMarketplaceSource now rejects an absolute filesystem
	// path outright), so realRepoLister decouples that string from the real
	// repository fixture the lister actually queries.
	_, _, err := AddPackage(dir, "owner/repo", AddOptions{Ref: "HEAD"}, realRepoLister{dir: repoDir})

	// Assert
	if err != nil {
		t.Fatalf("AddPackage returned error resolving --ref HEAD: %v", err)
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(cfg.Packages) != 1 || cfg.Packages[0].Ref != wantSHA {
		t.Errorf("Packages = %+v, want a single entry pinned to HEAD's actual SHA %q", cfg.Packages, wantSHA)
	}
}

// ── ticket 20 (user-reported, 2026-08-25): local source existence (AC1/
// AC2) and package name charset (AC3) ────────────────────────────────────

// TestAddPackage_LocalSource_NonexistentPath_Rejected is AC1's direct
// regression: a local source whose resolved path does not exist on disk
// must be refused, and apm.yml must stay byte-for-byte unchanged.
func TestAddPackage_LocalSource_NonexistentPath_Rejected(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	original := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	writeFile(t, dir, "apm.yml", original)

	// Act
	_, _, err := AddPackage(dir, "./llm-wiki", AddOptions{}, panicLister{})

	// Assert
	if err == nil || !strings.Contains(err.Error(), "./llm-wiki") {
		t.Fatalf("error = %v, want AddPackage to reject a local source that does not exist, naming the path", err)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(data) != original {
		t.Errorf("apm.yml was modified despite the rejected nonexistent local source;\ngot:\n%s\nwant unchanged:\n%s", string(data), original)
	}
}

// TestAddPackage_LocalSource_TrailingSeparator_Rejected is ticket 20's exact
// reported reproducer: `./llm-wiki\` (a trailing backslash, e.g. a Windows/
// PowerShell tab-completion artifact) where "llm-wiki" (without the
// trailing "\") exists on disk. The Oracle accepts this verbatim (utils/
// path_security.py:64-82's reject_empty=False lets the trailing separator's
// empty path segment through).
//
// Ticket 21 (evaluator follow-up on ticket 20, AC4): this comment used to
// claim the resolved path "llm-wiki\" itself does not exist, and that the
// rejection came from AC1's existence check. That was wrong --
// resolveLocalSourceAgainstRoot normalises "\" -> "/" before resolving, so
// "./llm-wiki\" resolves to the real, existing "llm-wiki" directory and
// clears AC1 cleanly. The rejection actually comes from AC3's
// packageNameIssue check on the DERIVED name: defaultNameFromSource only
// trims a trailing "/" or ".git", never "\", so the derived name stays
// "llm-wiki\" -- which contains a path separator. This test also locks
// ticket 21's fix: the error must name the source as the user typed it
// (AC1), not just the derived name, and must not double the backslash the
// way `%q` would (AC3).
func TestAddPackage_LocalSource_TrailingSeparator_Rejected(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")
	if err := os.MkdirAll(filepath.Join(dir, "llm-wiki"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Act
	_, _, err := AddPackage(dir, `./llm-wiki\`, AddOptions{}, panicLister{})

	// Assert
	if err == nil {
		t.Fatal(`AddPackage("./llm-wiki\\") = nil error, want a rejection (the derived name "llm-wiki\" contains a path separator)`)
	}
	if wantSource := `local source "./llm-wiki\"`; !strings.Contains(err.Error(), wantSource) {
		t.Errorf("error = %v, want it to name the source as typed (%s), not just the derived name (ticket 21 AC1)", err, wantSource)
	}
	if strings.Contains(err.Error(), `\\`) {
		t.Errorf(`error = %v, want no doubled backslash / %%q rendering (ticket 21 AC3)`, err)
	}
}

// TestAddPackage_LocalSource_ExistsButIsAFile_Rejected covers the "exists
// but is not a directory" half of AC1.
func TestAddPackage_LocalSource_ExistsButIsAFile_Rejected(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")
	if err := os.WriteFile(filepath.Join(dir, "notadir"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, _, err := AddPackage(dir, "./notadir", AddOptions{}, panicLister{})

	// Assert
	if err == nil {
		t.Fatal("expected AddPackage to reject a local source that exists but is not a directory")
	}
}

// TestAddPackage_LocalSource_NoVerify_SkipsExistenceCheck is AC2: --no-verify
// already means "skip the reachability check" for a remote source, and it
// extends to the new existence check for a local one -- a nonexistent local
// path is accepted with --no-verify, exactly like an unreachable remote one
// is.
func TestAddPackage_LocalSource_NoVerify_SkipsExistenceCheck(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")

	// Act: panicLister proves --no-verify still never touches the network.
	name, _, err := AddPackage(dir, "./not-yet-created", AddOptions{NoVerify: true}, panicLister{})

	// Assert
	if err != nil {
		t.Fatalf("AddPackage with --no-verify rejected a nonexistent local source: %v", err)
	}
	if name != "not-yet-created" {
		t.Errorf("name = %q, want %q", name, "not-yet-created")
	}
}

// TestAddPackage_LocalSource_NoVerify_ContainmentGuardStillRuns is AC2's
// other half: --no-verify must NOT bypass the containment/traversal guard --
// only the existence check is behind the flag.
func TestAddPackage_LocalSource_NoVerify_ContainmentGuardStillRuns(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")

	// Act
	_, _, err := AddPackage(dir, `./..\..\outside`, AddOptions{NoVerify: true}, panicLister{})

	// Assert
	if err == nil {
		t.Fatal("AddPackage with --no-verify accepted a local source escaping the project root, want the containment guard to still run")
	}
}

// TestValidatePackageName_RejectsBrokenNames is AC3's table: a name that
// could never be a legitimate single path segment must be rejected,
// regardless of whether it came from --name or defaultNameFromSource.
func TestValidatePackageName_RejectsBrokenNames(t *testing.T) {
	tests := []string{
		"llm-wiki\\", // ticket 20's exact reproducer (trailing backslash)
		"foo/bar",    // embedded forward slash
		"foo\\bar",   // embedded backslash
		".",          // current-dir sentinel
		"..",         // parent-dir sentinel
		"foo bar",    // whitespace
		"foo\tbar",   // control character (tab)
		"foo\nbar",   // control character (newline)
	}
	for _, name := range tests {
		if err := validatePackageName(name); err == nil {
			t.Errorf("validatePackageName(%q) = nil, want a rejection", name)
		}
	}
}

// TestValidatePackageName_AcceptsLegitimateNames is AC3's explicit
// non-goal: apm-go's own charset guard here must stay looser than init's
// pluginNameRe (`^[a-z][a-z0-9-]{0,63}$`) -- an existing legitimate
// marketplace package name like "My_Tool" must keep working.
func TestValidatePackageName_AcceptsLegitimateNames(t *testing.T) {
	tests := []string{"tool", "My_Tool", "tool-a", "tool.js", "a1b2c3", "工具"}
	for _, name := range tests {
		if err := validatePackageName(name); err != nil {
			t.Errorf("validatePackageName(%q) = %v, want nil (legitimate name)", name, err)
		}
	}
}

// TestAddPackage_NameFlag_TrailingSeparator_Rejected proves AC3 applies to
// an explicit --name too, not just a defaultNameFromSource-derived one.
// Ticket 21 AC2: an explicit --name rejection must keep blaming the name
// itself (not the source) -- unchanged behavior, just verified explicitly
// now that AddPackage has two different message shapes to choose between.
func TestAddPackage_NameFlag_TrailingSeparator_Rejected(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Act
	_, _, err := AddPackage(dir, "./pkgs/tool", AddOptions{Name: "tool/evil"}, panicLister{})

	// Assert
	if err == nil {
		t.Fatal(`AddPackage(--name "tool/evil") = nil error, want a rejection (embedded path separator)`)
	}
	if !strings.Contains(err.Error(), `"tool/evil"`) {
		t.Errorf(`error = %v, want it to name the rejected --name value`, err)
	}
	if strings.Contains(err.Error(), "local source") {
		t.Errorf("error = %v, want an explicit --name rejection to blame the name, not the source (ticket 21 AC2)", err)
	}
}

// TestAddPackage_LocalSource_DerivedNameControlChar_SafeWithPercentSign is
// ticket 21 AC3's format-string-safety half: --name is attacker/user
// controlled, and ux.Error (the CLI's eventual printer for this error) takes
// a format string -- a name containing a literal "%" must never be treated
// as one. AddPackage builds this error via fmt.Errorf's own %s verb, so a
// "%s"/"%d"-bearing name is interpolated as a plain value, never
// re-interpreted as a nested format directive; this asserts the resulting
// message contains the name verbatim rather than, say, garbled or expanded
// output.
func TestAddPackage_LocalSource_DerivedNameControlChar_SafeWithPercentSign(t *testing.T) {
	// Arrange: resolveLocalSourceAgainstRoot normalises "\" -> "/" before
	// resolving containment, so the on-disk fixture for "./100%\done" is
	// the two-level "100%/done", not a single "100%\done" directory name --
	// defaultNameFromSource, in contrast, derives the name from the
	// ORIGINAL (un-normalised) source string, so the derived name stays the
	// single segment "100%\done" and is rejected as containing a path
	// separator.
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")
	if err := os.MkdirAll(filepath.Join(dir, "100%", "done"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Act
	_, _, err := AddPackage(dir, `./100%\done`, AddOptions{}, panicLister{})

	// Assert
	if err == nil {
		t.Fatal(`AddPackage("./100%\\done") = nil error, want a rejection (derived name contains a path separator)`)
	}
	if !strings.Contains(err.Error(), `100%\done`) {
		t.Errorf(`error = %v, want the "%%"-bearing name/source rendered verbatim, not treated as a format directive`, err)
	}
}

// TestPackageNameDiagnosticQuote_EscapesOnlyControlCharacters pins the two
// halves of ticket 21 AC3 that pull in opposite directions: a backslash (and
// every other printable rune) must survive verbatim so the user recognises
// what they typed, while a control character must NOT -- the terminal is a
// parser, and a raw newline in a rejected name splits the single-line
// diagnostic into a second, unprefixed line that could be mistaken for an
// independent status line. AC3's first implementation escaped nothing at
// all and let both a raw newline and a raw ESC reach the writer.
func TestPackageNameDiagnosticQuote_EscapesOnlyControlCharacters(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"backslash stays single", `llm-wiki\`, `"llm-wiki\"`},
		{"double quote stays verbatim", `a"b`, `"a"b"`},
		{"percent stays verbatim", `100%done`, `"100%done"`},
		{"space stays verbatim", "bad name", `"bad name"`},
		{"non-ascii stays verbatim", "套件-名稱", `"套件-名稱"`},
		{"newline escaped", "bad\nname", `"bad\x0aname"`},
		{"tab escaped", "bad\tname", `"bad\x09name"`},
		{"escape byte escaped", "bad\x1b[31mred", `"bad\x1b[31mred"`},
		{"bell escaped", "bad\aname", `"bad\x07name"`},
		{"del escaped", "bad\x7fname", `"bad\x7fname"`},
		{"u+0085 nel escaped", "badname", `"bad\x85name"`},
		// The three below are not category-Cc controls, so unicode.IsControl
		// would let them through raw -- they are why the predicate is
		// unicode.IsPrint.
		{"u+2028 line separator escaped", "bad name", `"bad\u2028name"`},
		{"u+00a0 no-break space escaped", "bad name", `"bad\xa0name"`},
		{"u+200b zero-width space escaped", "bad​name", `"bad\u200bname"`},
		{"invalid utf-8 byte escaped", "bad\xffname", `"bad\xffname"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := packageNameDiagnosticQuote(tt.in); got != tt.want {
				t.Errorf("packageNameDiagnosticQuote(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

// TestAddPackage_NameFlag_ControlCharacter_NotEmittedRaw is the end-to-end
// half of the test above: the rejection message for a control-bearing name
// must not itself contain that control character. Rejecting a name for
// containing control characters and then printing them raw is the one thing
// packageNameProblemControl's own wording rules out.
func TestAddPackage_NameFlag_ControlCharacter_NotEmittedRaw(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n")
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{"tool\nname", "tool\x1b[31mname", "tool\aname"} {
		// Act
		_, _, err := AddPackage(dir, "./pkgs/tool", AddOptions{Name: bad}, panicLister{})

		// Assert
		if err == nil {
			t.Fatalf("AddPackage(--name %q) = nil error, want a rejection", bad)
		}
		for _, r := range err.Error() {
			if unicode.IsControl(r) {
				t.Errorf("AddPackage(--name %q) error = %q, want no raw control character in the message", bad, err.Error())
				break
			}
		}
	}
}

// ── test doubles ──────────────────────────────────────────────────────────

// realRepoLister is a RefLister test double used when the source argument
// reaching AddPackage/SetPackage must itself be a req-mf-017-compliant
// shape (e.g. "owner/repo", so manifest.ValidateMarketplaceSource accepts
// it -- BLOCKING 1, external audit round 4, 2026-07-30: an absolute
// filesystem path is now rejected outright) while the underlying git target
// actually queried is a real local repository fixture at dir. Reuses
// newListRefsCmd/parseRefsOutput directly, skipping resolveCloneURL's
// source-string translation entirely, since dir is already a literal
// filesystem path `git ls-remote` accepts as-is.
type realRepoLister struct{ dir string }

func (r realRepoLister) ListRefs(string) ([]semver.TagInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), listRefsTimeout)
	defer cancel()
	cmd := newListRefsCmd(ctx, r.dir)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseRefsOutput(string(out)), nil
}

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

// mapRefLister is a RefLister test double that returns a fixed set of named
// refs regardless of source -- used to test F4's mutable-ref resolution
// deterministically, with no real network calls.
type mapRefLister struct {
	refs []semver.TagInfo
	err  error
}

func (m mapRefLister) ListRefs(source string) ([]semver.TagInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.refs, nil
}
