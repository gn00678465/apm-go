package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── flag wiring (mkt-045 修訂版's "並非完全共用" table) ───────────────────

func TestMarketplacePackageAddCmd_FlagsWired(t *testing.T) {
	cmd := marketplacePackageAddCmd()
	for _, name := range []string{"name", "version", "ref", "subdir", "tag-pattern", "tags", "include-prerelease", "no-verify"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("package add is missing --%s", name)
		}
	}
	if cmd.Flags().ShorthandLookup("s") == nil {
		t.Error("package add is missing the -s shorthand for --subdir")
	}
}

func TestMarketplacePackageSetCmd_FlagsWired(t *testing.T) {
	cmd := marketplacePackageSetCmd()
	for _, name := range []string{"version", "ref", "subdir", "tag-pattern", "tags", "include-prerelease"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("package set is missing --%s", name)
		}
	}
}

// TestMarketplacePackageSetCmd_HasNoAddOnlyFlags locks mkt-045 修訂版's
// explicit "並非完全共用": --name and -s/--subdir's shorthand, and
// --no-verify, belong only to `add`.
func TestMarketplacePackageSetCmd_HasNoAddOnlyFlags(t *testing.T) {
	cmd := marketplacePackageSetCmd()
	if cmd.Flags().Lookup("name") != nil {
		t.Error("package set must not have an add-only --name flag")
	}
	if cmd.Flags().Lookup("no-verify") != nil {
		t.Error("package set must not have an add-only --no-verify flag")
	}
	if cmd.Flags().ShorthandLookup("s") != nil {
		t.Error("package set's --subdir must not have add's -s shorthand")
	}
}

func TestMarketplacePackageRemoveCmd_FlagsWired(t *testing.T) {
	cmd := marketplacePackageRemoveCmd()
	if cmd.Flags().Lookup("yes") == nil {
		t.Error("package remove is missing --yes")
	}
	if cmd.Flags().ShorthandLookup("y") == nil {
		t.Error("package remove is missing the -y shorthand for --yes")
	}
}

// TestMarketplacePackageRemoveCmd_HasNoEditFlags locks remove down to just
// --yes/-y -- none of add/set's editing flags apply to a deletion.
func TestMarketplacePackageRemoveCmd_HasNoEditFlags(t *testing.T) {
	cmd := marketplacePackageRemoveCmd()
	for _, name := range []string{"name", "version", "ref", "subdir", "tag-pattern", "tags", "include-prerelease", "no-verify"} {
		if cmd.Flags().Lookup(name) != nil {
			t.Errorf("package remove must not have --%s", name)
		}
	}
}

// ── C1: --verbose/-v on package add/set/remove ──────────────────────────

func TestMarketplacePackageAddCmd_HasVerboseFlag(t *testing.T) {
	cmd := marketplacePackageAddCmd()
	if cmd.Flags().Lookup("verbose") == nil {
		t.Error("package add is missing --verbose (C1)")
	}
	if cmd.Flags().ShorthandLookup("v") == nil {
		t.Error("package add is missing the -v shorthand for --verbose (C1)")
	}
}

func TestMarketplacePackageSetCmd_HasVerboseFlag(t *testing.T) {
	cmd := marketplacePackageSetCmd()
	if cmd.Flags().Lookup("verbose") == nil {
		t.Error("package set is missing --verbose (C1)")
	}
	if cmd.Flags().ShorthandLookup("v") == nil {
		t.Error("package set is missing the -v shorthand for --verbose (C1)")
	}
}

func TestMarketplacePackageRemoveCmd_HasVerboseFlag(t *testing.T) {
	cmd := marketplacePackageRemoveCmd()
	if cmd.Flags().Lookup("verbose") == nil {
		t.Error("package remove is missing --verbose (C1)")
	}
	if cmd.Flags().ShorthandLookup("v") == nil {
		t.Error("package remove is missing the -v shorthand for --verbose (C1)")
	}
}

// TestMarketplacePackageAdd_VerboseFlagAccepted proves `package add`'s
// -v/--verbose parses without the "unknown flag" error C1 found.
func TestMarketplacePackageAdd_VerboseFlagAccepted(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "add", "./pkgs/tool", "-v")

	// Assert
	if err != nil {
		t.Fatalf("package add ./pkgs/tool -v returned error: %v", err)
	}
}

// TestMarketplacePackageSet_VerboseFlagAccepted proves `package set`'s
// -v/--verbose parses without erroring, alongside a real field flag.
func TestMarketplacePackageSet_VerboseFlagAccepted(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./pkgs/tool\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "set", "tool", "--verbose", "--tag-pattern", "v{version}")

	// Assert
	if err != nil {
		t.Fatalf("package set --verbose --tag-pattern ... returned error: %v", err)
	}
}

// TestMarketplacePackageRemove_VerboseFlagAccepted proves `package remove`'s
// -v/--verbose parses without erroring, alongside -y.
func TestMarketplacePackageRemove_VerboseFlagAccepted(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./pkgs/tool\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "remove", "tool", "-y", "-v")

	// Assert
	if err != nil {
		t.Fatalf("package remove -y -v returned error: %v", err)
	}
}

// ── C2: `package set` with zero field flags must error, not no-op ───────

// TestMarketplacePackageSet_NoFieldsSpecified_ExitsCode1 covers C2: Python
// (set.py:98-103) exits 1 with "No fields specified..." rather than
// silently rewriting the entry as a no-op. The guard fires before any I/O,
// so apm.yml must be byte-for-byte unchanged.
func TestMarketplacePackageSet_NoFieldsSpecified_ExitsCode1(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./pkgs/tool\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "set", "tool")

	// Assert
	if err == nil {
		t.Fatal("package set tool with zero field flags returned no error, want Python's exit-1 guard (C2)")
	}
	if got := exitCodeOf(err); got != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1 (Python's sys.exit(1), not mkt-045's usual 2)", got)
	}
	if !strings.Contains(err.Error(), "No fields specified. Pass at least one option (e.g. --version, --ref, --subdir).") {
		t.Errorf("err = %v, want Python's exact message", err)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(data) != apmYML {
		t.Errorf("apm.yml = %q, want it byte-for-byte unchanged (the guard must fire before any I/O)", string(data))
	}
}

// TestMarketplacePackageSet_WithVersionFlag_StillWorks is C2's regression
// guard: giving at least one field flag must still succeed as before.
func TestMarketplacePackageSet_WithVersionFlag_StillWorks(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./pkgs/tool\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "set", "tool", "--version", "^1.0.0")

	// Assert
	if err != nil {
		t.Fatalf("package set tool --version ^1.0.0 returned error: %v", err)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "version: ^1.0.0") {
		t.Errorf("apm.yml = %q, want the new version recorded", string(data))
	}
}

// ── C10: EOF/non-interactive confirm read must never read as "declined" ──

// TestMarketplacePackageRemove_LooksInteractiveButEOF_RequiresYesAndDoesNotRemove
// is C10's full-CLI reproduction for `marketplace package remove`: it must
// exit non-zero and must NOT remove the package entry -- asserted directly
// against apm.yml's content, not just the exit code.
func TestMarketplacePackageRemove_LooksInteractiveButEOF_RequiresYesAndDoesNotRemove(t *testing.T) {
	// Arrange
	chdirTemp(t)
	forceRich(t, true)
	stubConfirm(t, func(string, bool) (bool, error) { return false, errors.New("prompt aborted") })
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./pkgs/tool\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "remove", "tool")

	// Assert
	if err == nil {
		t.Fatal("package remove with a failed confirmation prompt returned no error, want the requires -y/--yes error (C10)")
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "name: tool") {
		t.Error("package was removed despite the confirmation read failing (C10 footgun)")
	}
}

// TestMarketplacePackageRemove_InteractiveExplicitNo_AbortsCleanly is the
// CLI-level boundary case: a genuine interactive "n" is unaffected by the
// fix.
func TestMarketplacePackageRemove_InteractiveExplicitNo_AbortsCleanly(t *testing.T) {
	// Arrange
	chdirTemp(t)
	forceRich(t, true)
	stubConfirm(t, func(string, bool) (bool, error) { return false, nil })
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./pkgs/tool\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "package", "remove", "tool")

	// Assert
	if err != nil {
		t.Fatalf(`package remove with an explicit interactive "n" returned error: %v, want a clean exit 0 Aborted`, err)
	}
	if !strings.Contains(out, "Aborted") {
		t.Errorf("output = %q, want an Aborted message", out)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "name: tool") {
		t.Error("package was removed despite an explicit decline")
	}
}

// ── mkt-046 regression, end to end through the CLI (prd.md AC3) ─────────

func TestMarketplacePackageAdd_LocalSource_NoFlags_SucceedsEndToEnd(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "package", "add", "./pkgs/tool")

	// Assert
	if err != nil {
		t.Fatalf("package add ./pkgs/tool with zero flags returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "tool") {
		t.Errorf("output = %q, want it to mention the added package", out)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "source: ./pkgs/tool") {
		t.Errorf("apm.yml = %q, want the new package's source recorded", string(data))
	}
}

// TestMarketplacePackageAdd_RemoteSource_GoesThroughLsRemote_RealGitFixture
// covers mkt-046's other half end to end through the CLI: unlike a local
// source, a remote source *does* verify via `git ls-remote` (a real, local
// git repo fixture stands in for "remote" here, following
// marketplace_authoring_test.go's own initGitRepoWithTags convention --
// no real network access needed) -- and a nonexistent one fails the add.
func TestMarketplacePackageAdd_RemoteSource_GoesThroughLsRemote_RealGitFixture(t *testing.T) {
	// Arrange
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	source := filepath.ToSlash(repoDir)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "add", source)

	// Assert
	if err != nil {
		t.Fatalf("package add for a reachable remote source returned error: %v", err)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "source: "+source) {
		t.Errorf("apm.yml = %q, want the new package's source recorded", string(data))
	}
}

// TestMarketplacePackageAdd_UnreachableRemoteSource_Fails proves the
// negative side: an unreachable remote source (not a real git repo) must
// fail `package add` rather than silently being accepted the way mkt-046's
// bug let *local* sources slip through unverified.
func TestMarketplacePackageAdd_UnreachableRemoteSource_Fails(t *testing.T) {
	// Arrange
	chdirTemp(t)
	notARepo := filepath.Join(t.TempDir(), "not-a-repo")
	if err := os.MkdirAll(notARepo, 0o755); err != nil {
		t.Fatal(err)
	}
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "add", filepath.ToSlash(notARepo))

	// Assert
	if err == nil {
		t.Fatal("expected package add against an unreachable remote source to error")
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", got)
	}
}

// ── error paths exit code 2 ───────────────────────────────────────────

func TestMarketplacePackageAdd_DuplicateName_ExitsCode2(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./pkgs/tool\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "add", "./pkgs/tool")

	// Assert
	if err == nil {
		t.Fatal("expected a duplicate package name to error")
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2 (mkt-045)", got)
	}
}

func TestMarketplacePackageSet_NotFound_ExitsCode2(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "set", "nonexistent", "--version", "^1.0.0")

	// Assert
	if err == nil {
		t.Fatal("expected setting a nonexistent package to error")
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2 (mkt-045)", got)
	}
}

func TestMarketplacePackageRemove_NotFound_ExitsCode2(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "remove", "nonexistent", "-y")

	// Assert
	if err == nil {
		t.Fatal("expected removing a nonexistent package to error")
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2 (mkt-045)", got)
	}
}

// ── remove's non-interactive confirmation guard: exit 1, not 2 ──────────

func TestMarketplacePackageRemove_NonInteractiveWithoutYes_ExitsCode1(t *testing.T) {
	// Arrange
	chdirTemp(t)
	withNonInteractiveStdin(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./pkgs/tool\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "remove", "tool")

	// Assert
	if err == nil {
		t.Fatal("expected package remove without -y in a non-interactive session to error")
	}
	if got := exitCodeOf(err); got != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1 (the same default every other 'apm marketplace *' confirmation guard uses, not mkt-045's 2)", got)
	}

	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "name: tool") {
		t.Error("package was removed despite the non-interactive guard rejecting the command")
	}
}

func TestMarketplacePackageRemove_WithYes_SucceedsNonInteractively(t *testing.T) {
	// Arrange
	chdirTemp(t)
	withNonInteractiveStdin(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./pkgs/tool\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "remove", "tool", "-y")

	// Assert
	if err != nil {
		t.Fatalf("package remove -y returned error: %v", err)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if strings.Contains(string(data), "name: tool") {
		t.Error("package was not removed despite -y")
	}
}

// ── set's tri-state --include-prerelease through the CLI ────────────────

func TestMarketplacePackageSet_IncludePrereleaseNotGiven_LeavesExistingValueUnchanged(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./pkgs/tool\n      include_prerelease: true\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act: change an unrelated field, never mention --include-prerelease.
	_, err := runMarketplaceCmd(t, "package", "set", "tool", "--tag-pattern", "v{version}")

	// Assert
	if err != nil {
		t.Fatalf("package set returned error: %v", err)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "include_prerelease: true") {
		t.Errorf("apm.yml = %q, want include_prerelease untouched by an unrelated set", string(data))
	}
}

func TestMarketplacePackageSet_IncludePrereleaseGivenFalse_ClearsExistingTrue(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./pkgs/tool\n      include_prerelease: true\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "set", "tool", "--include-prerelease=false")

	// Assert
	if err != nil {
		t.Fatalf("package set --include-prerelease=false returned error: %v", err)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if strings.Contains(string(data), "include_prerelease: true") {
		t.Errorf("apm.yml = %q, want include_prerelease cleared", string(data))
	}
}

// ── --version/--ref mutual exclusion through the CLI ─────────────────────

func TestMarketplacePackageAdd_VersionAndRefBothGiven_ExitsCode2(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "add", "./pkgs/tool", "--version", "^1.0.0", "--ref", "v1.0.0")

	// Assert
	if err == nil {
		t.Fatal("expected --version and --ref together to error")
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", got)
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("err = %v, want a mutually-exclusive message", err)
	}
}

// TestMarketplacePackageSet_VersionAndRefBothGiven_ExitsCode2 mirrors the
// add command's own case above: mkt-045 requires the --version/--ref
// mutual-exclusion guard at both the command layer (cmd.Flags().Changed)
// and the editor layer (authoring.SetPackage) for set too, not just add.
func TestMarketplacePackageSet_VersionAndRefBothGiven_ExitsCode2(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./pkgs/tool\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "set", "tool", "--version", "^1.0.0", "--ref", "v1.0.0")

	// Assert
	if err == nil {
		t.Fatal("expected --version and --ref together to error")
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", got)
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("err = %v, want a mutually-exclusive message", err)
	}
}
