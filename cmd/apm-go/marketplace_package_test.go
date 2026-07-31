package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/ux"
	"github.com/spf13/pflag"
)

// assertLineSeverity finds the line in out containing marker and asserts it
// starts with wantSymbol's centered 3-rune column (ux/printer.go's
// printLine convention: " <symbol> ") -- MAJOR 3 (external audit round 2,
// 2026-07-30, REGR-B1/REGR-M1): checking for the message text or for
// ux.SymbolWarn's bare presence anywhere in out does not catch a mutation
// that swaps ux.Warn for ux.Info, since the message text is identical
// either way and ux.SymbolWarn ("!") could coincidentally appear elsewhere
// in the same combined output. This checks the specific line's own leading
// symbol column.
func assertLineSeverity(t *testing.T, out, marker, wantSymbol string) {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, marker) {
			want := " " + wantSymbol + " "
			if !strings.HasPrefix(line, want) {
				t.Errorf("line %q, want it to start with %q (severity)", line, want)
			}
			return
		}
	}
	t.Fatalf("no output line contains %q: %q", marker, out)
}

// ── flag wiring (mkt-045 修訂版's "並非完全共用" table) ───────────────────

func TestMarketplacePackageAddCmd_FlagsWired(t *testing.T) {
	cmd := marketplacePackageAddCmd()
	for _, name := range []string{"name", "version", "ref", "subdir", "tag-pattern", "tags", "include-prerelease", "no-verify", "category"} {
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
// explicit "並非完全共用": --name and -s/--subdir's shorthand, --no-verify,
// and (R10/AC49) --category, belong only to `add`.
func TestMarketplacePackageSetCmd_HasNoAddOnlyFlags(t *testing.T) {
	cmd := marketplacePackageSetCmd()
	if cmd.Flags().Lookup("name") != nil {
		t.Error("package set must not have an add-only --name flag")
	}
	if cmd.Flags().Lookup("no-verify") != nil {
		t.Error("package set must not have an add-only --no-verify flag")
	}
	if cmd.Flags().Lookup("category") != nil {
		t.Error("package set must not have an add-only --category flag (R10/AC49; upstream set.py has no --category)")
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

// TestMarketplacePackageSet_RefFlag_ResolvesViaListerThroughCLI is MAJOR 3's
// REGR-B2 fix (external audit round 2, 2026-07-30): the previous round's
// REGR-B2 regression tests only ever called authoring.SetPackage directly,
// so a CLI-layer mutation like `if false && cmd.Flags().Changed("ref")`
// (marketplace_package.go, guarding the opts.Ref assignment) -- which would
// make `package set --ref` silently do nothing at the CLI layer -- went
// undetected. This drives the real `package set NAME --ref ...` command
// (marketplacePackageSetCmd's RunE, through runMarketplaceCmd) against a
// real local git repo fixture, asserting the ref was actually resolved and
// written -- not just that authoring.SetPackage can do it when called
// in-process.
func TestMarketplacePackageSet_RefFlag_ResolvesViaListerThroughCLI(t *testing.T) {
	// Arrange: the fixture repo must be a relative "./..." source (BLOCKING
	// 1, external audit round 4, 2026-07-30: manifest.ValidateMarketplaceSource
	// now rejects an absolute filesystem path as a marketplace source
	// outright) -- `set` still resolves an explicit --ref via the real
	// lister for a local source regardless (skipLocalSource=false, unlike
	// `add`), so this keeps exercising the genuine gitRefLister/git
	// ls-remote path, just against a relative fixture.
	dir := chdirTemp(t)
	repoDir := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	wantSHA := gitCmd(t, repoDir, "rev-parse", "v1.0.0")
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./repo\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "set", "tool", "--ref", "v1.0.0")

	// Assert
	if err != nil {
		t.Fatalf("package set tool --ref v1.0.0 returned error: %v", err)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "ref: "+wantSHA) {
		t.Errorf("apm.yml = %q, want the resolved SHA %s written for ref: (a --ref mutation at the CLI layer must not be silently ignored)", string(data), wantSHA)
	}
}

// TestMarketplacePackageSet_RefFlag_BranchName_ResolvesViaListerThroughCLI is
// MAJOR (external audit round 3, 2026-07-30, ROUND2-M3-CLI)'s missing
// branch-name fixture: every existing CLI `set --ref` fixture used a tag
// value starting with "v" ("v1.0.0"), so a mutation narrowing the CLI's own
// `cmd.Flags().Changed("ref")` guard to
// `cmd.Flags().Changed("ref") && strings.HasPrefix(ref, "v")` (silently
// ignoring any --ref value that is NOT tag-shaped, e.g. an ordinary branch
// name) would still pass every existing test here. This drives `package set
// --ref <branch-name>` (not a tag) through the real CLI against a real git
// branch.
func TestMarketplacePackageSet_RefFlag_BranchName_ResolvesViaListerThroughCLI(t *testing.T) {
	// Arrange: relative "./..." fixture, same BLOCKING 1 reasoning as
	// TestMarketplacePackageSet_RefFlag_ResolvesViaListerThroughCLI above.
	dir := chdirTemp(t)
	repoDir := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	gitCmd(t, repoDir, "branch", "feature-branch")
	wantSHA := gitCmd(t, repoDir, "rev-parse", "feature-branch")
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./repo\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "set", "tool", "--ref", "feature-branch")

	// Assert
	if err != nil {
		t.Fatalf("package set tool --ref feature-branch returned error: %v", err)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "ref: "+wantSHA) {
		t.Errorf("apm.yml = %q, want the resolved SHA %s written for ref: (a non-tag-shaped --ref value must not be silently ignored at the CLI layer)", string(data), wantSHA)
	}
}

// TestMarketplacePackageSet_RefFlagHead_PrintsMutableRefWarning is BLOCKING
// 3 (external audit round 4, 2026-07-30): `set --ref HEAD` must print the
// same mutable-ref warning `add --ref HEAD` does -- upstream warns on `set`
// too (commands/marketplace/plugin/set.py:80 calls the same _resolve_ref
// plugin/__init__.py:120-137 warns from), but SetPackage used to hardcode
// nil for resolveRef's onExplicitHeadWillResolve hook, so `set` never
// warned at all. Every existing `set --ref` CLI test used a tag/branch ref,
// never HEAD, so nothing caught this gap.
func TestMarketplacePackageSet_RefFlagHead_PrintsMutableRefWarning(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	repoDir := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./repo\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "package", "set", "tool", "--ref", "HEAD")

	// Assert
	if err != nil {
		t.Fatalf("package set tool --ref HEAD returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "'HEAD' is a mutable ref. Resolving to current SHA for safety.") {
		t.Errorf("output = %q, want the mutable-ref warning (BLOCKING 3)", out)
	}
	assertLineSeverity(t, out, "'HEAD' is a mutable ref", ux.SymbolWarn)
}

// TestMarketplacePackageSet_RefFlagHead_MixedCase_PrintsMutableRefWarningOnce
// is MAJOR 3 (external audit round 4, 2026-07-30)'s exact-count-and-severity
// discipline applied to `set`: a mutation firing the callback twice for a
// mixed-case "Head", or downgrading its severity to ux.Info for "Head"
// only, would pass a test that merely substring-matches the message text
// once (a plain t.Contains check cannot distinguish 1 occurrence from 2).
// Asserts BOTH the exact occurrence count and the line's own severity
// symbol in the same test.
func TestMarketplacePackageSet_RefFlagHead_MixedCase_PrintsMutableRefWarningOnce(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	repoDir := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./repo\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "package", "set", "tool", "--ref", "Head")

	// Assert
	if err != nil {
		t.Fatalf("package set tool --ref Head returned error: %v (output: %s)", err, out)
	}
	count := strings.Count(out, "'HEAD' is a mutable ref. Resolving to current SHA for safety.")
	if count != 1 {
		t.Errorf("output = %q, want the mutable-ref warning printed exactly once, got %d", out, count)
	}
	assertLineSeverity(t, out, "'HEAD' is a mutable ref", ux.SymbolWarn)
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
	// Arrange: source must be req-mf-017-compliant (BLOCKING 1, external
	// audit round 4, 2026-07-30: manifest.ValidateMarketplaceSource now
	// rejects an absolute filesystem path outright), so the real repo
	// fixture is wired in via withFixtureRemoteLister instead of being named
	// directly as the source.
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	withFixtureRemoteLister(t, repoDir)
	source := "owner/repo"
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
	withFixtureRemoteLister(t, notARepo)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "add", "owner/repo")

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

// ── R5: implicit/explicit HEAD resolution through the CLI (AC17-20) ──────

// TestMarketplacePackageAdd_ZeroFlags_RemoteSource_WritesResolvedHeadSHA is
// AC17: a zero-flag add against a remote source now resolves the implicit
// HEAD ref and pins it, unlike the pre-fix behavior of writing no `ref:` at
// all (design.md §9's "唯一的行為破壞").
func TestMarketplacePackageAdd_ZeroFlags_RemoteSource_WritesResolvedHeadSHA(t *testing.T) {
	// Arrange
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	withFixtureRemoteLister(t, repoDir)
	source := "owner/repo"
	wantSHA := gitCmd(t, repoDir, "rev-parse", "HEAD")
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "add", source)

	// Assert
	if err != nil {
		t.Fatalf("package add with zero flags against a real remote-like source returned error: %v", err)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "ref: "+wantSHA) {
		t.Errorf("apm.yml = %q, want ref: %s written (AC17: implicit HEAD resolved and pinned)", string(data), wantSHA)
	}
}

// TestMarketplacePackageAdd_NoVerify_ImplicitHead_ExitsCode2 is AC18's
// Go-level complement to verify.ps1's binary-based exit-code probe: the
// exact upstream message plus exit code 2 (via withExitCode, unaffected by
// go test's own process boundary the way `go run` would be).
func TestMarketplacePackageAdd_NoVerify_ImplicitHead_ExitsCode2(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "add", "owner/repo", "--no-verify")

	// Assert
	if err == nil {
		t.Fatal("expected package add --no-verify with an implicit HEAD ref to error (AC18)")
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2 (AC18)", got)
	}
	if !strings.Contains(err.Error(), "Cannot resolve HEAD ref without network access. Provide an explicit --ref SHA.") {
		t.Errorf("err = %v, want the exact upstream-parity message (AC18)", err)
	}
}

// TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning is
// AC19: `--ref HEAD` prints the mutable-ref warning and still resolves
// normally (proven by the successful add below).
func TestMarketplacePackageAdd_ExplicitRefHead_PrintsMutableRefWarning(t *testing.T) {
	// Arrange
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	withFixtureRemoteLister(t, repoDir)
	source := "owner/repo"
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "package", "add", source, "--ref", "HEAD")

	// Assert
	if err != nil {
		t.Fatalf("package add --ref HEAD returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "'HEAD' is a mutable ref. Resolving to current SHA for safety.") {
		t.Errorf("output = %q, want the mutable-ref warning (AC19)", out)
	}
	// MAJOR 3 (external audit round 2, 2026-07-30, REGR-B1): a mutation from
	// ux.Warn to ux.Info at this call site leaves the message text
	// unchanged, so it must be caught by checking the line's own severity
	// symbol, not just the message substring above.
	assertLineSeverity(t, out, "'HEAD' is a mutable ref", ux.SymbolWarn)
}

// TestMarketplacePackageAdd_LocalSource_ExplicitRefHead_NoMutableRefWarning
// is BLOCKING 1's regression (external audit, 2026-07-30): a local source
// never resolves any ref at all (mkt-046), so `--ref HEAD` on one must NOT
// print the "'HEAD' is a mutable ref..." warning -- printing it claims a
// resolution that never happens. Reported repro:
// `apm-go marketplace package add ./localpkg --name loc2 --ref HEAD`
// printed the warning despite resolveRef's local short-circuit resolving
// nothing.
func TestMarketplacePackageAdd_LocalSource_ExplicitRefHead_NoMutableRefWarning(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "package", "add", "./localpkg", "--name", "loc2", "--ref", "HEAD")

	// Assert
	if err != nil {
		t.Fatalf("package add ./localpkg --ref HEAD returned error: %v (output: %s)", err, out)
	}
	if strings.Contains(out, "mutable ref") {
		t.Errorf("output = %q, want NO mutable-ref warning for a local source (nothing is ever resolved for one, BLOCKING 1)", out)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if strings.Contains(string(data), "ref:") {
		t.Errorf("apm.yml = %q, want no ref: written for a local source", string(data))
	}
}

// TestMarketplacePackageAdd_ExplicitRefHead_MixedCase_PrintsMutableRefWarning
// is MAJOR (external audit round 3, 2026-07-30, ROUND2-B1)'s missing
// mixed-case CLI fixture: every existing CLI mutable-ref-warning test used
// literal "HEAD" only, so a predictor mutation carving out an exception for
// title-case "Head" (e.g. `... == refKindHead && ref != "Head"`) would still
// pass. This drives `--ref Head` (title case) through the real CLI.
func TestMarketplacePackageAdd_ExplicitRefHead_MixedCase_PrintsMutableRefWarning(t *testing.T) {
	// Arrange
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	withFixtureRemoteLister(t, repoDir)
	source := "owner/repo"
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "package", "add", source, "--ref", "Head")

	// Assert
	if err != nil {
		t.Fatalf("package add --ref Head returned error: %v (output: %s)", err, out)
	}
	// MAJOR 3 (external audit round 4, 2026-07-30): a plain substring match
	// cannot distinguish the callback firing once from firing twice, and
	// cannot catch a severity downgrade (ux.Warn -> ux.Info) specific to
	// "Head" -- both would still contain this substring. Assert the exact
	// occurrence count AND the line's own severity symbol.
	count := strings.Count(out, "'HEAD' is a mutable ref. Resolving to current SHA for safety.")
	if count != 1 {
		t.Errorf("output = %q, want the mutable-ref warning for a mixed-case 'Head' printed exactly once, got %d", out, count)
	}
	assertLineSeverity(t, out, "'HEAD' is a mutable ref", ux.SymbolWarn)
}

// ── BLOCKING 2 (external audit round 3, 2026-07-30): the mutable-ref
// warning must print only immediately before resolveRef's real HEAD
// resolution, never before AddPackage's other pre-flight checks have run.
// Each test below combines an explicit --ref HEAD with a reason AddPackage
// is about to fail (or, for --no-verify, a reason resolution itself is
// impossible) for a completely unrelated cause, and asserts the warning
// text is ABSENT from the output -- reproducing the live-binary finding
// that `add owner/repo --ref HEAD --no-verify` against a directory with no
// marketplace config printed the warning before erroring on "no marketplace
// authoring config found". ──────────────────────────────────────────────

func TestMarketplacePackageAdd_ExplicitRefHead_NoVerify_NoMutableRefWarning_ExitsCode2(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "package", "add", "owner/repo", "--ref", "HEAD", "--no-verify")

	// Assert
	if err == nil {
		t.Fatal("expected package add --ref HEAD --no-verify to error (HEAD cannot be resolved offline)")
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", got)
	}
	if !strings.Contains(err.Error(), "Cannot resolve HEAD ref without network access. Provide an explicit --ref SHA.") {
		t.Errorf("err = %v, want the exact upstream-parity offline message", err)
	}
	if strings.Contains(out, "mutable ref") {
		t.Errorf("output = %q, want NO mutable-ref warning: --no-verify makes HEAD resolution impossible, so the warning must never print", out)
	}
}

func TestMarketplacePackageAdd_ExplicitRefHead_MissingConfig_NoMutableRefWarning(t *testing.T) {
	// Arrange: chdirTemp with no apm.yml written at all -- AddPackage must
	// fail at LoadAuthoringConfig, before ever reaching resolveRef.
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	withFixtureRemoteLister(t, repoDir)
	source := "owner/repo"

	// Act
	out, err := runMarketplaceCmd(t, "package", "add", source, "--ref", "HEAD")

	// Assert
	if err == nil {
		t.Fatal("expected package add with no marketplace config present to error")
	}
	if strings.Contains(out, "mutable ref") {
		t.Errorf("output = %q, want NO mutable-ref warning: the command fails before AddPackage ever reaches ref resolution", out)
	}
}

func TestMarketplacePackageAdd_ExplicitRefHead_UnreachableSource_NoMutableRefWarning(t *testing.T) {
	// Arrange
	chdirTemp(t)
	notARepo := filepath.Join(t.TempDir(), "not-a-repo")
	if err := os.MkdirAll(notARepo, 0o755); err != nil {
		t.Fatal(err)
	}
	withFixtureRemoteLister(t, notARepo)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "package", "add", "owner/repo", "--ref", "HEAD")

	// Assert
	if err == nil {
		t.Fatal("expected package add against an unreachable source to error")
	}
	if strings.Contains(out, "mutable ref") {
		t.Errorf("output = %q, want NO mutable-ref warning: verifyPackageSource fails before AddPackage ever reaches ref resolution", out)
	}
}

func TestMarketplacePackageAdd_ExplicitRefHead_DuplicateName_NoMutableRefWarning(t *testing.T) {
	// Arrange
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	withFixtureRemoteLister(t, repoDir)
	source := "owner/repo"
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n  packages:\n    - name: tool\n      source: ./pkgs/tool\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act: --name tool collides with the existing "tool" entry.
	out, err := runMarketplaceCmd(t, "package", "add", source, "--ref", "HEAD", "--name", "tool")

	// Assert
	if err == nil {
		t.Fatal("expected a duplicate package name to error")
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", got)
	}
	if strings.Contains(out, "mutable ref") {
		t.Errorf("output = %q, want NO mutable-ref warning: the duplicate-name check fails before AddPackage ever reaches ref resolution", out)
	}
}

// TestMarketplacePackageAdd_VersionGiven_DoesNotWriteRef is AC20: a
// --version range never resolves or writes a ref, even against a
// reachable remote source (a reachability check still happens per
// verifyPackageSource/add.py's ordering -- only ref *resolution* is
// skipped).
func TestMarketplacePackageAdd_VersionGiven_DoesNotWriteRef(t *testing.T) {
	// Arrange
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	withFixtureRemoteLister(t, repoDir)
	source := "owner/repo"
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "add", source, "--version", "^1.0.0")

	// Assert
	if err != nil {
		t.Fatalf("package add --version returned error: %v", err)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if strings.Contains(string(data), "ref:") {
		t.Errorf("apm.yml = %q, want no ref: written when --version is given (AC20)", string(data))
	}
	if !strings.Contains(string(data), "version: ^1.0.0") {
		t.Errorf("apm.yml = %q, want version: ^1.0.0 written", string(data))
	}
}

// ── R10: --category (AC47/AC48/AC50) ──────────────────────────────────────

// TestMarketplacePackageAdd_CategoryFlag_WritesCategory is AC47.
func TestMarketplacePackageAdd_CategoryFlag_WritesCategory(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "package", "add", "./pkgs/tool", "--category", "Productivity")

	// Assert
	if err != nil {
		t.Fatalf("package add --category returned error: %v", err)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "category: Productivity") {
		t.Errorf("apm.yml = %q, want category: Productivity written (AC47)", string(data))
	}
}

// TestMarketplacePackageAdd_CategoryFlagHelp_MetavarIsString is B-MINOR-2
// (external audit round 8, 2026-07-31 follow-up): the --category flag's
// pflag Usage string used to read `...at `pack` time)` -- a backtick pair
// anywhere in a pflag Usage string is not decoration, it is parsed by
// pflag's UnquoteUsage as an explicit metavar override, so `--help` printed
// "--category pack" (implying the flag's argument is a fixed literal named
// "pack") instead of "--category string". Locks the metavar directly via
// pflag's own Flag.Value.Type()-based unquoting helper rather than
// substring-matching the rendered help text, so it fails for the right
// reason (a stray backtick pair) rather than any other incidental wording
// change.
func TestMarketplacePackageAdd_CategoryFlagHelp_MetavarIsString(t *testing.T) {
	cmd := marketplacePackageAddCmd()
	f := cmd.Flags().Lookup("category")
	if f == nil {
		t.Fatal("package add is missing --category")
	}
	name, _ := pflag.UnquoteUsage(f)
	if name != "string" {
		t.Errorf("--category metavar = %q, want %q (a stray backtick pair in the Usage string overrides pflag's default type-derived metavar, AC47 help text)", name, "string")
	}
}

// TestMarketplacePackageAdd_OutputsIncludeCodex_NoCategory_WarnsButSucceeds
// is AC48: design.md §13's deliberate divergence from upstream (which
// blocks add entirely here) -- add must still succeed, only warning.
func TestMarketplacePackageAdd_OutputsIncludeCodex_NoCategory_WarnsButSucceeds(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  outputs:\n    codex: {}\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "package", "add", "./pkgs/tool")

	// Assert
	if err != nil {
		t.Fatalf("package add without --category (outputs includes codex) returned error, want success with a warning (AC48): %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "category") {
		t.Errorf("output = %q, want a warning mentioning the missing category (AC48)", out)
	}
	// MAJOR 3 (external audit round 2, 2026-07-30, REGR-M1): a mutation
	// from ux.Warn to ux.Info at this call site leaves the message text
	// unchanged, so severity must be checked via the line's own leading
	// symbol, not just the "category" substring above.
	assertLineSeverity(t, out, "no --category", ux.SymbolWarn)
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "name: tool") {
		t.Errorf("apm.yml = %q, want the package added despite the missing category", string(data))
	}
}

// TestMarketplacePackageAdd_CategoryGiven_OutputsIncludeCodex_NoWarning is
// MAJOR 1's first negative test (external audit, 2026-07-30): giving
// --category must NOT print the missing-category warning even when
// outputs: includes codex -- catches the mutation
// `warnMissingCategory := marketplaceOutputsIncludeCodex(".")` (dropping
// the `category == ""` half of the condition), which would warn here even
// though --category was given.
func TestMarketplacePackageAdd_CategoryGiven_OutputsIncludeCodex_NoWarning(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  outputs:\n    codex: {}\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "package", "add", "./pkgs/tool", "--category", "Productivity")

	// Assert
	if err != nil {
		t.Fatalf("package add --category returned error: %v (output: %s)", err, out)
	}
	if strings.Contains(out, "no --category") {
		t.Errorf("output = %q, want NO missing-category warning when --category was given", out)
	}
}

// TestMarketplacePackageAdd_NoCategory_OutputsExcludeCodex_NoWarning is
// MAJOR 1's second negative test: no --category and outputs: does NOT
// include codex must NOT print the warning either -- catches the mutation
// `warnMissingCategory := category == ""` (dropping the
// `marketplaceOutputsIncludeCodex` half), which would warn here even
// though outputs never mentions codex at all.
func TestMarketplacePackageAdd_NoCategory_OutputsExcludeCodex_NoWarning(t *testing.T) {
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
		t.Fatalf("package add returned error: %v (output: %s)", err, out)
	}
	if strings.Contains(out, "no --category") {
		t.Errorf("output = %q, want NO missing-category warning when outputs does not include codex", out)
	}
}

// TestMarketplacePackageAdd_CategoryFlag_ThenPackCodex_Succeeds is AC50:
// the end-to-end proof that --category removes upstream's dead end
// (research/eval-real-run-20260728.md §C4: with outputs: codex configured
// and no --category on add, upstream's own `add` is permanently blocked).
func TestMarketplacePackageAdd_CategoryFlag_ThenPackCodex_Succeeds(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  outputs:\n    codex: {}\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, addErr := runMarketplaceCmd(t, "package", "add", "./pkgs/a", "--name", "tool-a", "--category", "utility")
	if addErr != nil {
		t.Fatalf("package add --category returned error: %v", addErr)
	}
	_, packErr := runPackCmd(t)

	// Assert
	if packErr != nil {
		t.Fatalf("pack returned error after add --category (AC50): %v", packErr)
	}
	codexPath := filepath.Join(dir, ".agents", "plugins", "marketplace.json")
	data, rerr := os.ReadFile(codexPath)
	if rerr != nil {
		t.Fatalf("codex output not written: %v", rerr)
	}
	if !strings.Contains(string(data), `"category": "utility"`) {
		t.Errorf("codex output = %s, want category=utility present", data)
	}
}

// TestMarketplacePackageAdd_LocalSourceEscapingRoot_Rejected is BLOCKING 2's
// (2026-07-31 follow-up) live end-to-end reproduction, at the CLI layer:
// `package add ./linked`, where "linked" is a Windows directory junction
// (needs no special privilege to create, unlike a real symlink) physically
// inside the project directory but pointing at a real directory OUTSIDE it,
// used to be accepted outright (verifyPackageSource's local branch never
// containment-checked the resolved path), and a subsequent `pack` would
// faithfully read the escaping "outside" directory's apm.yml into the
// marketplace.json output. Asserts both that the add itself fails (non-zero
// exit, via a returned error) and that apm.yml is left byte-for-byte
// unchanged -- the escaping entry must never be spliced in at all.
func TestMarketplacePackageAdd_LocalSourceEscapingRoot_Rejected(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("directory junctions are a Windows-only concept")
	}

	// Arrange
	parent := t.TempDir()
	project := filepath.Join(parent, "project")
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideYML := "name: outside-secret\nversion: 9.9.9\ndescription: leaked\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile(filepath.Join(outside, "apm.yml"), []byte(outsideYML), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(project, "linked")
	mklink := exec.Command("cmd", "/c", "mklink", "/J", link, outside)
	if out, err := mklink.CombinedOutput(); err != nil {
		t.Skipf("SKIPPED: cannot create a directory junction in this environment (%v: %s); the junction-escape guard is untested by this run", err, out)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, addErr := runMarketplaceCmd(t, "package", "add", "./linked")

	// Assert
	if addErr == nil {
		t.Fatalf("package add ./linked (a junction escaping the project root) succeeded, want rejection (output: %s)", out)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(data) != apmYML {
		t.Errorf("apm.yml was modified despite the rejected add;\ngot:\n%s\nwant unchanged:\n%s", string(data), apmYML)
	}
}
