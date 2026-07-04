package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/marketplace/authoring"
)

// chdirTemp changes the working directory to a fresh t.TempDir() for the
// duration of the test (following this file's own os.Chdir/defer
// convention, e.g. cmd/apm/install_test.go), and returns that directory.
func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })
	return dir
}

// ── flags wired (implement.md step 2, item 1) ───────────────────────────

func TestMarketplaceInitCmd_FlagsWired(t *testing.T) {
	cmd := marketplaceInitCmd()
	for _, name := range []string{"force", "no-gitignore-check", "name", "owner", "verbose"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("marketplace init is missing --%s", name)
		}
	}
	if cmd.Flags().ShorthandLookup("v") == nil {
		t.Error("marketplace init is missing the -v shorthand for --verbose")
	}
}

// ── apm.yml does not exist: build the minimal shell first ───────────────

func TestMarketplaceInit_ApmYMLMissing_ScaffoldsMinimalShellAndAppendsBlock(t *testing.T) {
	// Arrange
	chdirTemp(t)

	// Act
	out, err := runMarketplaceCmd(t, "init")

	// Assert
	if err != nil {
		t.Fatalf("marketplace init returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Created apm.yml") {
		t.Errorf("output = %q, want a \"Created apm.yml\" message when apm.yml did not exist", out)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	content := string(data)
	if !strings.HasPrefix(content, "name: my-marketplace\n") {
		t.Errorf("apm.yml = %q, want it to start with the default scaffold name", content)
	}
	if !strings.Contains(content, "marketplace:") {
		t.Errorf("apm.yml = %q, want a marketplace: block appended", content)
	}
	if !strings.Contains(content, "name: acme-org") {
		t.Errorf("apm.yml = %q, want owner.name defaulted to acme-org", content)
	}
}

func TestMarketplaceInit_NameFlagOnlyAffectsScaffoldShellName(t *testing.T) {
	// Arrange
	chdirTemp(t)

	// Act
	_, err := runMarketplaceCmd(t, "init", "--name", "my-project")

	// Assert
	if err != nil {
		t.Fatalf("marketplace init returned error: %v", err)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.HasPrefix(string(data), "name: my-project\n") {
		t.Errorf("apm.yml = %q, want top-level name overridden by --name", string(data))
	}
}

func TestMarketplaceInit_OwnerFlagThreadsThroughBlock(t *testing.T) {
	// Arrange
	chdirTemp(t)

	// Act
	_, err := runMarketplaceCmd(t, "init", "--owner", "my-org")

	// Assert
	if err != nil {
		t.Fatalf("marketplace init returned error: %v", err)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	content := string(data)
	for _, want := range []string{"name: my-org", "url: https://github.com/my-org", "source: my-org/example-package"} {
		if !strings.Contains(content, want) {
			t.Errorf("apm.yml missing %q with --owner my-org; got:\n%s", want, content)
		}
	}
}

// TestMarketplaceInit_NeverWritesRefMain covers mkt-040 修訂版's trap end to
// end: the scaffold `apm marketplace init` actually writes to disk must not
// contain the upstream "ref: main" example that `apm pack` rejects.
func TestMarketplaceInit_NeverWritesRefMain(t *testing.T) {
	// Arrange
	chdirTemp(t)

	// Act
	if _, err := runMarketplaceCmd(t, "init"); err != nil {
		t.Fatalf("marketplace init returned error: %v", err)
	}

	// Assert
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if strings.Contains(string(data), "ref: main") {
		t.Errorf("apm.yml contains %q, want the mkt-040 修訂版 fix; got:\n%s", "ref: main", string(data))
	}
}

// ── apm.yml exists, no marketplace: block yet (AC2 舊坑 1) ───────────────

// handAuthoredApmYML is a "already existing, hand-formatted" apm.yml fixture
// -- unusual spacing, an inline comment, and a trailing comment -- used to
// verify `init` only ever appends, never reformats existing bytes.
const handAuthoredApmYML = "# Hand-authored project manifest\n" +
	"name:    demo\n" +
	"version: \"1.0.0\"\n" +
	"description: >-\n" +
	"  A description written across\n" +
	"  multiple lines on purpose.\n" +
	"scripts:\n" +
	"  build: echo hi   # inline comment kept exactly\n" +
	"# trailing standalone comment\n"

func TestMarketplaceInit_ExistingApmYMLWithoutBlock_AppendsAndPreservesExistingBytes(t *testing.T) {
	// Arrange
	chdirTemp(t)
	if err := os.WriteFile("apm.yml", []byte(handAuthoredApmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "init")

	// Assert
	if err != nil {
		t.Fatalf("marketplace init returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Added 'marketplace:' block to apm.yml") {
		t.Errorf("output = %q, want the \"Added\" message (apm.yml already existed)", out)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	content := string(data)
	if !strings.HasPrefix(content, handAuthoredApmYML) {
		t.Errorf("apm.yml's existing hand-authored bytes were altered; want them as an exact prefix.\ngot:\n%s\nwant prefix:\n%s", content, handAuthoredApmYML)
	}
	if !strings.Contains(content, "marketplace:") {
		t.Errorf("apm.yml = %q, want a marketplace: block appended", content)
	}
}

func TestMarketplaceInit_ExistingApmYMLWithCRLF_AppendedBlockUsesCRLFToo(t *testing.T) {
	// Arrange
	chdirTemp(t)
	crlf := "name: demo\r\nversion: \"1.0.0\"\r\n"
	if err := os.WriteFile("apm.yml", []byte(crlf), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "init")

	// Assert
	if err != nil {
		t.Fatalf("marketplace init returned error: %v", err)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	content := string(data)
	if !strings.HasPrefix(content, crlf) {
		t.Errorf("apm.yml's existing CRLF bytes were altered; want them as an exact prefix; got:\n%q", content)
	}
	appended := strings.TrimPrefix(content, crlf)
	if strings.Contains(appended, "\r\n") == false {
		t.Errorf("appended block does not use CRLF line endings to match the existing file; got:\n%q", appended)
	}
	if strings.Contains(appended, "marketplace:\n") {
		t.Errorf("appended block has a bare LF after 'marketplace:', want CRLF to match the file; got:\n%q", appended)
	}
}

// ── apm.yml exists with an existing non-null marketplace: block ─────────

const apmYMLWithExistingBlock = "# Project manifest\n" +
	"name: demo\n" +
	"version: \"1.0.0\"\n" +
	"marketplace:\n" +
	"  owner:\n" +
	"    name: OldOwner\n" +
	"  packages:\n" +
	"    - name: old-pkg\n" +
	"      source: ./old\n" +
	"scripts:\n" +
	"  test: echo test   # keep me\n"

func TestMarketplaceInit_ExistingNonNullBlock_WithoutForce_ErrorsAndLeavesFileUntouched(t *testing.T) {
	// Arrange
	chdirTemp(t)
	if err := os.WriteFile("apm.yml", []byte(apmYMLWithExistingBlock), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "init")

	// Assert
	if err == nil {
		t.Fatal("marketplace init with an existing 'marketplace:' block and no --force returned no error")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error = %v, want it to mention --force", err)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(data) != apmYMLWithExistingBlock {
		t.Errorf("apm.yml was modified despite the rejected init; got:\n%s\nwant unchanged:\n%s", string(data), apmYMLWithExistingBlock)
	}
}

func TestMarketplaceInit_ExistingNonNullBlock_WithForce_OverwritesBlockOnly(t *testing.T) {
	// Arrange
	chdirTemp(t)
	if err := os.WriteFile("apm.yml", []byte(apmYMLWithExistingBlock), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "init", "--force", "--owner", "new-org")

	// Assert
	if err != nil {
		t.Fatalf("marketplace init --force returned error: %v (output: %s)", err, out)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	content := string(data)

	if !strings.HasPrefix(content, "# Project manifest\nname: demo\nversion: \"1.0.0\"\n") {
		t.Errorf("bytes before the marketplace: key were altered; got:\n%s", content)
	}
	if !strings.HasSuffix(content, "scripts:\n  test: echo test   # keep me\n") {
		t.Errorf("bytes after the marketplace: block (the scripts: key) were altered; got:\n%s", content)
	}
	if strings.Contains(content, "OldOwner") {
		t.Errorf("apm.yml still contains the old owner after --force overwrite; got:\n%s", content)
	}
	if strings.Contains(content, "old-pkg") {
		t.Errorf("apm.yml still contains the old package entry after --force overwrite; got:\n%s", content)
	}
	if !strings.Contains(content, "name: new-org") {
		t.Errorf("apm.yml does not contain the new owner after --force overwrite; got:\n%s", content)
	}
}

// TestMarketplaceInit_ExistingNonNullBlock_WithForce_ResultLoadsCleanly is a
// companion to the byte-level assertions above: the overwritten apm.yml
// must still be a valid marketplace authoring config the way schema.go's
// own loader expects.
func TestMarketplaceInit_ExistingNonNullBlock_WithForce_ResultLoadsCleanly(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.WriteFile("apm.yml", []byte(apmYMLWithExistingBlock), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	if _, err := runMarketplaceCmd(t, "init", "--force", "--owner", "new-org"); err != nil {
		t.Fatalf("marketplace init --force returned error: %v", err)
	}

	// Assert
	cfg, src, err := authoring.LoadAuthoringConfig(dir)
	if err != nil {
		t.Fatalf("LoadAuthoringConfig after init --force returned error: %v", err)
	}
	if src != authoring.ConfigSourceApmYML {
		t.Errorf("ConfigSource = %v, want ConfigSourceApmYML", src)
	}
	if cfg.Owner.Name != "new-org" {
		t.Errorf("Owner.Name = %q, want %q", cfg.Owner.Name, "new-org")
	}
	if len(cfg.Packages) != 1 || cfg.Packages[0].Name != "example-package" {
		t.Errorf("Packages = %+v, want the freshly scaffolded example-package entry", cfg.Packages)
	}
}

// ── apm.yml exists with a bare "marketplace:" key (explicit null) ───────

const apmYMLWithNullMarketplaceKey = "name: demo\n" +
	"version: \"1.0.0\"\n" +
	"marketplace:\n" +
	"other: value\n"

func TestMarketplaceInit_NullMarketplaceKey_ProceedsWithoutForce(t *testing.T) {
	// Arrange
	chdirTemp(t)
	if err := os.WriteFile("apm.yml", []byte(apmYMLWithNullMarketplaceKey), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "init")

	// Assert
	if err != nil {
		t.Fatalf("marketplace init with a bare (null) 'marketplace:' key returned error without --force: %v", err)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	content := string(data)
	if !strings.Contains(content, "owner:") {
		t.Errorf("apm.yml = %q, want the marketplace: block filled in", content)
	}
	if !strings.Contains(content, "other: value") {
		t.Errorf("apm.yml = %q, want the unrelated 'other' key preserved", content)
	}
}

// ── .gitignore staleness check ───────────────────────────────────────────

func TestMarketplaceInit_GitignoreWarnsWhenMarketplaceJSONIgnored(t *testing.T) {
	// Arrange
	chdirTemp(t)
	if err := os.WriteFile(".gitignore", []byte("node_modules/\nmarketplace.json\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "init")

	// Assert
	if err != nil {
		t.Fatalf("marketplace init returned error: %v", err)
	}
	if !strings.Contains(out, ".gitignore") {
		t.Errorf("output = %q, want a .gitignore warning", out)
	}
}

func TestMarketplaceInit_NoGitignoreCheckSkipsWarning(t *testing.T) {
	// Arrange
	chdirTemp(t)
	if err := os.WriteFile(".gitignore", []byte("marketplace.json\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "init", "--no-gitignore-check")

	// Assert
	if err != nil {
		t.Fatalf("marketplace init returned error: %v", err)
	}
	if strings.Contains(out, ".gitignore") {
		t.Errorf("output = %q, want no .gitignore warning with --no-gitignore-check", out)
	}
}

func TestMarketplaceInit_NoGitignore_NoWarningNoError(t *testing.T) {
	// Arrange
	chdirTemp(t)

	// Act
	out, err := runMarketplaceCmd(t, "init")

	// Assert
	if err != nil {
		t.Fatalf("marketplace init returned error with no .gitignore present: %v", err)
	}
	if strings.Contains(out, ".gitignore") {
		t.Errorf("output = %q, want no .gitignore mention when the file is absent", out)
	}
}

func TestMarketplaceInit_GitignoreWithUnrelatedRulesDoesNotWarn(t *testing.T) {
	// Arrange
	chdirTemp(t)
	if err := os.WriteFile(".gitignore", []byte("node_modules/\n# marketplace.json (commented out, should not match)\ndist/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "init")

	// Assert
	if err != nil {
		t.Fatalf("marketplace init returned error: %v", err)
	}
	if strings.Contains(out, ".gitignore") {
		t.Errorf("output = %q, want no .gitignore warning when no matching rule is present", out)
	}
}

// ── --verbose ─────────────────────────────────────────────────────────

func TestMarketplaceInit_VerbosePrintsPath(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)

	// Act
	out, err := runMarketplaceCmd(t, "init", "-v")

	// Assert
	if err != nil {
		t.Fatalf("marketplace init returned error: %v", err)
	}
	if !strings.Contains(out, filepath.Join(dir, "apm.yml")) {
		t.Errorf("output = %q, want it to print apm.yml's full path with -v", out)
	}
}

// ── `check` (mkt-041) ────────────────────────────────────────────────────

// gitCmd and initGitRepoWithTags give `marketplace check` cmd-level tests a
// real local git repository fixture -- no network access needed, mirroring
// internal/marketplace/authoring/refcheck_test.go's own helpers.
func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func initGitRepoWithTags(t *testing.T, dir string, tags ...string) {
	t.Helper()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "init")
	for _, tag := range tags {
		gitCmd(t, dir, "tag", tag)
	}
}

func TestMarketplaceCheckCmd_FlagsWired(t *testing.T) {
	cmd := marketplaceCheckCmd()
	for _, name := range []string{"offline", "verbose"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("marketplace check is missing --%s", name)
		}
	}
	if cmd.Flags().ShorthandLookup("v") == nil {
		t.Error("marketplace check is missing the -v shorthand for --verbose")
	}
}

func TestMarketplaceCheck_NoAuthoringConfig_PropagatesLoadError(t *testing.T) {
	// Arrange
	chdirTemp(t)

	// Act
	_, err := runMarketplaceCmd(t, "check")

	// Assert
	if err == nil {
		t.Fatal("marketplace check with no apm.yml/marketplace.yml returned no error")
	}
	if !strings.Contains(err.Error(), "apm-go marketplace init") {
		t.Errorf("error = %v, want the same 'apm-go marketplace init' pointer LoadAuthoringConfig returns", err)
	}
}

// TestMarketplaceCheck_AllLocalPackages_SucceedsWithoutNetwork covers
// mkt-041's "本地來源跳過網路" boundary end-to-end through the CLI: a
// marketplace with only local packages must pass `check` even though it
// never touches the network (the underlying git subprocess is simply never
// invoked, so this succeeds regardless of network availability).
func TestMarketplaceCheck_AllLocalPackages_SucceedsWithoutNetwork(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n" +
		"  packages:\n" +
		"    - name: local-a\n      source: ./pkgs/a\n      version: \"^1.0.0\"\n" +
		"    - name: local-b\n      source: ./pkgs/b\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "check")

	// Assert
	if err != nil {
		t.Fatalf("marketplace check returned error for an all-local marketplace: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "2 package(s) verified") {
		t.Errorf("output = %q, want it to report both packages verified", out)
	}
}

// TestMarketplaceCheck_RemotePackagePinnedRefFound_RealGitFixture covers
// mkt-041's remote path against a real (local, no-network) git repository.
func TestMarketplaceCheck_RemotePackagePinnedRefFound_RealGitFixture(t *testing.T) {
	// Arrange
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	// YAML backslashes in Windows paths must be escaped for a double-quoted
	// scalar; forward-slash the fixture path so this apm.yml stays a plain
	// unquoted scalar regardless of platform.
	source := filepath.ToSlash(repoDir)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n" +
		"  packages:\n" +
		"    - name: tool\n      source: " + source + "\n      ref: v1.0.0\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "check")

	// Assert
	if err != nil {
		t.Fatalf("marketplace check returned error for a real, pinned remote ref: %v (output: %s)", err, out)
	}
}

// TestMarketplaceCheck_RemotePackagePinnedRefMissing_ExitsNonZero covers
// mkt-041's "任何失敗 exit 1": a missing pinned ref must fail the command.
func TestMarketplaceCheck_RemotePackagePinnedRefMissing_ExitsNonZero(t *testing.T) {
	// Arrange
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	source := filepath.ToSlash(repoDir)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n" +
		"  packages:\n" +
		"    - name: tool\n      source: " + source + "\n      ref: v9.9.9\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "check")

	// Assert
	if err == nil {
		t.Fatal("marketplace check with a missing pinned ref returned no error, want exit 1 (mkt-041)")
	}
	if !strings.Contains(out, "[x] tool") {
		t.Errorf("output = %q, want a [x] failure line naming the package", out)
	}
}

// TestMarketplaceCheck_Offline_FailsPinnedRemotePackage covers mkt-041's
// "--offline 無快取可用視為失敗" boundary.
func TestMarketplaceCheck_Offline_FailsPinnedRemotePackage(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n" +
		"  packages:\n" +
		"    - name: tool\n      source: owner/repo\n      version: \"^1.0.0\"\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "check", "--offline")

	// Assert
	if err == nil {
		t.Fatal("marketplace check --offline with a pinned remote package returned no error, want exit 1 (mkt-041)")
	}
}

// TestMarketplaceCheck_Offline_LocalPackagesStillSucceed proves --offline
// does not fail packages that never needed the network in the first place.
func TestMarketplaceCheck_Offline_LocalPackagesStillSucceed(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n" +
		"  packages:\n" +
		"    - name: local-a\n      source: ./pkgs/a\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "check", "--offline")

	// Assert
	if err != nil {
		t.Fatalf("marketplace check --offline returned error for a local-only marketplace: %v", err)
	}
}

// TestMarketplaceCheck_LegacyConfig_PrintsDeprecationWarning covers the
// legacy-source deprecation warning `check` shares with the other producer
// subcommands (LoadAuthoringConfig's ConfigSourceLegacy signal).
func TestMarketplaceCheck_LegacyConfig_PrintsDeprecationWarning(t *testing.T) {
	// Arrange
	chdirTemp(t)
	if err := os.WriteFile("marketplace.yml", []byte("owner:\n  name: acme\npackages: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "check")

	// Assert
	if err != nil {
		t.Fatalf("marketplace check returned error for a legacy-only config: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "apm-go marketplace migrate") {
		t.Errorf("output = %q, want a deprecation warning pointing at 'apm-go marketplace migrate'", out)
	}
}

// TestMarketplaceCheck_VerbosePrintsEveryPackage covers --verbose printing
// a line for passing packages too, not just failures.
func TestMarketplaceCheck_VerbosePrintsEveryPackage(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n" +
		"  packages:\n" +
		"    - name: local-a\n      source: ./pkgs/a\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "check", "-v")

	// Assert
	if err != nil {
		t.Fatalf("marketplace check -v returned error: %v", err)
	}
	if !strings.Contains(out, "[+] local-a: ok") {
		t.Errorf("output = %q, want a per-package [+] line with -v", out)
	}
}

// ── `outdated` (mkt-042 修訂版) ──────────────────────────────────────────

func TestMarketplaceOutdatedCmd_FlagsWired(t *testing.T) {
	cmd := marketplaceOutdatedCmd()
	for _, name := range []string{"offline", "include-prerelease", "verbose"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("marketplace outdated is missing --%s", name)
		}
	}
	if cmd.Flags().ShorthandLookup("v") == nil {
		t.Error("marketplace outdated is missing the -v shorthand for --verbose")
	}
}

func TestMarketplaceOutdated_NoAuthoringConfig_PropagatesLoadError(t *testing.T) {
	// Arrange
	chdirTemp(t)

	// Act
	_, err := runMarketplaceCmd(t, "outdated")

	// Assert
	if err == nil {
		t.Fatal("marketplace outdated with no apm.yml/marketplace.yml returned no error")
	}
	if !strings.Contains(err.Error(), "apm-go marketplace init") {
		t.Errorf("error = %v, want the same 'apm-go marketplace init' pointer LoadAuthoringConfig returns", err)
	}
}

// TestMarketplaceOutdated_UpgradablePackage_ExitsNonZero covers mkt-042's
// "exit 1 僅由 upgradable 計數驅動": a real remote package with a newer
// in-range tag than its declared version must fail the command.
func TestMarketplaceOutdated_UpgradablePackage_ExitsNonZero(t *testing.T) {
	// Arrange
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir, "v1.0.0", "v1.1.0")
	source := filepath.ToSlash(repoDir)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n" +
		"  packages:\n" +
		"    - name: tool\n      source: " + source + "\n      version: \"^1.0.0\"\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "outdated")

	// Assert
	if err == nil {
		t.Fatal("marketplace outdated with an upgradable package returned no error, want exit 1 (mkt-042)")
	}
	if !strings.Contains(out, "[!] tool") {
		t.Errorf("output = %q, want an [!] line naming the package", out)
	}
}

// TestMarketplaceOutdated_NoMatchingTags_DoesNotExitNonZero covers mkt-042
// 修訂版's explicit carve-out: "no matching tags found" shares the [!] icon
// with the counted case above, but must NOT drive exit 1.
func TestMarketplaceOutdated_NoMatchingTags_DoesNotExitNonZero(t *testing.T) {
	// Arrange
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir, "release-1")
	source := filepath.ToSlash(repoDir)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n" +
		"  packages:\n" +
		"    - name: tool\n      source: " + source + "\n      version: \"^1.0.0\"\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "outdated")

	// Assert
	if err != nil {
		t.Fatalf("marketplace outdated returned error for \"no matching tags found\": %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "[!] tool") || !strings.Contains(out, "no matching tags") {
		t.Errorf("output = %q, want an [!] line noting no matching tags", out)
	}
}

// TestMarketplaceOutdated_FetchFailure_DoesNotExitNonZero covers mkt-042's
// "[x] 不影響 exit code".
func TestMarketplaceOutdated_FetchFailure_DoesNotExitNonZero(t *testing.T) {
	// Arrange
	chdirTemp(t)
	dir := t.TempDir()
	notARepo := filepath.Join(dir, "not-a-repo")
	if err := os.MkdirAll(notARepo, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.ToSlash(notARepo)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n" +
		"  packages:\n" +
		"    - name: tool\n      source: " + source + "\n      version: \"^1.0.0\"\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "outdated")

	// Assert
	if err != nil {
		t.Fatalf("marketplace outdated returned error for a fetch failure: %v (output: %s), want exit 0 (mkt-042's [x] must not affect exit code)", err, out)
	}
	if !strings.Contains(out, "[x] tool") {
		t.Errorf("output = %q, want an [x] line naming the package", out)
	}
}

// TestMarketplaceOutdated_PinnedRefPackage_SkippedIconI covers mkt-042's
// [i] icon end to end through the CLI.
func TestMarketplaceOutdated_PinnedRefPackage_SkippedIconI(t *testing.T) {
	// Arrange
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n" +
		"  packages:\n" +
		"    - name: tool\n      source: owner/repo\n      ref: v1.0.0\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "outdated")

	// Assert
	if err != nil {
		t.Fatalf("marketplace outdated returned error for a pinned-ref package: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "[i] tool") {
		t.Errorf("output = %q, want an [i] line naming the pinned package", out)
	}
}

// TestMarketplaceOutdated_LegacyConfig_PrintsDeprecationWarning mirrors
// check's own legacy-source deprecation warning.
func TestMarketplaceOutdated_LegacyConfig_PrintsDeprecationWarning(t *testing.T) {
	// Arrange
	chdirTemp(t)
	if err := os.WriteFile("marketplace.yml", []byte("owner:\n  name: acme\npackages: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "outdated")

	// Assert
	if err != nil {
		t.Fatalf("marketplace outdated returned error for a legacy-only config: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "apm-go marketplace migrate") {
		t.Errorf("output = %q, want a deprecation warning pointing at 'apm-go marketplace migrate'", out)
	}
}
