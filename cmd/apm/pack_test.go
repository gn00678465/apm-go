// Tests for pack.go: `apm pack`'s CLI wiring (mkt-054/055) -- flag surface,
// output-location correctness (never the repo root), --marketplace-path/
// --marketplace filtering, --dry-run, and exit-code behavior (0 success, 1
// for every marketplace config/build error; 2/3/4 are out of this
// sub-task's scope and must not be reachable).
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runPackCmd executes `pack <args...>` against a fresh packCmd() tree,
// capturing combined stdout+stderr the same way runMarketplaceCmd does.
func runPackCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := packCmd()
	var buf bytes.Buffer
	c.SetOut(&buf)
	c.SetErr(&buf)
	c.SetArgs(args)
	err := c.Execute()
	return buf.String(), err
}

func writePackApmYML(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile("apm.yml", []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func packRevParse(t *testing.T, dir, ref string) string {
	t.Helper()
	return gitCmd(t, dir, "rev-list", "-n", "1", ref)
}

// ── flags wired / deliberately absent ────────────────────────────────────

func TestPackCmd_FlagsWired(t *testing.T) {
	cmd := packCmd()
	for _, name := range []string{"offline", "include-prerelease", "dry-run", "marketplace", "marketplace-path"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("pack is missing --%s", name)
		}
	}
	if cmd.Flags().ShorthandLookup("m") == nil {
		t.Error("pack is missing the -m shorthand for --marketplace")
	}
	if cmd.Flags().ShorthandLookup("v") == nil {
		t.Error("pack is missing the -v shorthand for --verbose")
	}
}

func TestPackCmd_DoesNotExposeDeferredFlags(t *testing.T) {
	cmd := packCmd()
	for _, name := range []string{"check-versions", "check-clean", "allow-head"} {
		if cmd.Flags().Lookup(name) != nil {
			t.Errorf("pack must not expose --%s (deferred to a later sub-task, see design.md)", name)
		}
	}
}

// ── no marketplace: block -> message + exit 0 ────────────────────────────

func TestPackCmd_NoMarketplaceBlock_PrintsMessageAndExitsZero(t *testing.T) {
	// Arrange
	chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\n")

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "nothing to do") {
		t.Errorf("output = %q, want a 'nothing to do' message", out)
	}
}

func TestPackCmd_ExplicitNullMarketplaceKey_PrintsMessageAndExitsZero(t *testing.T) {
	// Arrange: a bare "marketplace:" key with nothing after it is mkt-047's
	// "_has_marketplace_block" null case -- not really present.
	chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\nmarketplace:\n")

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "nothing to do") {
		t.Errorf("output = %q, want a 'nothing to do' message", out)
	}
}

func TestPackCmd_NoApmYMLAtAll_PrintsMessageAndExitsZero(t *testing.T) {
	// Arrange: not even an apm.yml exists yet.
	chdirTemp(t)

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "nothing to do") {
		t.Errorf("output = %q, want a 'nothing to do' message", out)
	}
}

// ── output location: never the repo root, both outputs written ──────────

func TestPackCmd_TwoOutputs_WrittenAtCorrectPaths_NotRepoRoot(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    claude: {}
    codex: {}
  packages:
    - name: tool-a
      source: ./pkgs/a
      category: utility
`)

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	claudePath := filepath.Join(dir, ".claude-plugin", "marketplace.json")
	codexPath := filepath.Join(dir, ".agents", "plugins", "marketplace.json")
	if _, statErr := os.Stat(claudePath); statErr != nil {
		t.Errorf("expected claude output at %s: %v", claudePath, statErr)
	}
	if _, statErr := os.Stat(codexPath); statErr != nil {
		t.Errorf("expected codex output at %s: %v", codexPath, statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "marketplace.json")); !os.IsNotExist(statErr) {
		t.Errorf("marketplace.json must never be written to the repo root (stat err = %v)", statErr)
	}

	claudeData, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(claudeData), `"tool-a"`) || strings.Contains(string(claudeData), `"category"`) {
		t.Errorf("claude output = %s, want tool-a present and no 'category' field", claudeData)
	}
	codexData, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(codexData), `"category": "utility"`) {
		t.Errorf("codex output = %s, want category=utility present", codexData)
	}
}

// ── F1 fix: local package metadata enrichment end-to-end ─────────────────

func TestPackCmd_LocalPackage_EnrichesFromItsOwnApmYML(t *testing.T) {
	// Arrange: the marketplace.packages[] entry declares neither
	// description nor version -- pack must read them from the local
	// package's own apm.yml on disk (F1 fix; previously local packages were
	// never enriched at all).
	dir := chdirTemp(t)
	pkgDir := filepath.Join(dir, "pkgs", "tool")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "apm.yml"), []byte("name: tool\ndescription: A local tool\nversion: 3.1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: tool-a
      source: ./pkgs/tool
`)

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), `"description": "A local tool"`) {
		t.Errorf("output = %s, want description enriched from local apm.yml", data)
	}
	if !strings.Contains(string(data), `"version": "3.1.4"`) {
		t.Errorf("output = %s, want version enriched from local apm.yml", data)
	}
}

func TestPackCmd_LocalPackage_CuratorDescriptionWinsOverLocalApmYML(t *testing.T) {
	// Arrange: curator-supplied description must win over the local
	// package's own apm.yml value.
	dir := chdirTemp(t)
	pkgDir := filepath.Join(dir, "pkgs", "tool")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "apm.yml"), []byte("name: tool\ndescription: from local apm.yml\nversion: 9.9.9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: tool-a
      source: ./pkgs/tool
      description: curator description
      version: "1.0.0"
`)

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), `"description": "curator description"`) {
		t.Errorf("output = %s, want curator's description to win", data)
	}
	if strings.Contains(string(data), "from local apm.yml") {
		t.Errorf("output = %s, must not contain the local apm.yml's own description", data)
	}
}

// ── config-level path override (map form) ────────────────────────────────

func TestPackCmd_ConfigOverridePath_MapForm_IsUsed(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    claude:
      path: dist/claude-marketplace.json
  packages:
    - name: tool-a
      source: ./pkgs/a
`)

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	overridden := filepath.Join(dir, "dist", "claude-marketplace.json")
	if _, statErr := os.Stat(overridden); statErr != nil {
		t.Errorf("expected output at overridden path %s: %v", overridden, statErr)
	}
	defaultPath := filepath.Join(dir, ".claude-plugin", "marketplace.json")
	if _, statErr := os.Stat(defaultPath); !os.IsNotExist(statErr) {
		t.Errorf("default path should not have been written when overridden (stat err = %v)", statErr)
	}
}

// ── CLI --marketplace-path wins over the config-level override ──────────

func TestPackCmd_CLIPathOverride_WinsOverConfigOverride(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    claude:
      path: dist/claude-marketplace.json
  packages:
    - name: tool-a
      source: ./pkgs/a
`)

	// Act
	out, err := runPackCmd(t, "--marketplace-path", "claude=cli-dist/marketplace.json")

	// Assert
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	cliPath := filepath.Join(dir, "cli-dist", "marketplace.json")
	if _, statErr := os.Stat(cliPath); statErr != nil {
		t.Errorf("expected output at CLI-overridden path %s: %v", cliPath, statErr)
	}
	configPath := filepath.Join(dir, "dist", "claude-marketplace.json")
	if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
		t.Errorf("config-overridden path should not have been written when the CLI overrides it too (stat err = %v)", statErr)
	}
}

func TestPackCmd_MarketplacePathOverride_UnknownFormat_Errors(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages: []
`)

	_, err := runPackCmd(t, "--marketplace-path", "bogus=dist/x.json")
	if err == nil {
		t.Fatal("expected an error for an unknown --marketplace-path format")
	}
}

func TestPackCmd_MarketplacePathOverride_Malformed_Errors(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages: []
`)

	_, err := runPackCmd(t, "--marketplace-path", "claude-no-equals-sign")
	if err == nil {
		t.Fatal("expected an error for a malformed --marketplace-path value")
	}
}

func TestPackCmd_MarketplacePathOverride_Traversal_Errors(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: tool-a
      source: ./pkgs/a
`)

	// Act
	_, err := runPackCmd(t, "--marketplace-path", "claude="+filepath.Join("..", "..", "escaped-marketplace.json"))

	// Assert
	if err == nil {
		t.Fatal("expected an error: --marketplace-path must not be allowed to escape the project root")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "..", "..", "escaped-marketplace.json")); !os.IsNotExist(statErr) {
		t.Errorf("traversal path must never actually be written (stat err = %v)", statErr)
	}
}

// ── --dry-run: zero writes ───────────────────────────────────────────────

func TestPackCmd_DryRun_WritesNothing(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: tool-a
      source: ./pkgs/a
`)

	// Act
	out, err := runPackCmd(t, "--dry-run")

	// Assert
	if err != nil {
		t.Fatalf("pack --dry-run returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Would") {
		t.Errorf("output = %q, want a dry-run notice", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude-plugin", "marketplace.json")); !os.IsNotExist(statErr) {
		t.Errorf("--dry-run must not write any file (stat err = %v)", statErr)
	}
}

// ── -m/--marketplace filter ───────────────────────────────────────────────

func TestPackCmd_MarketplaceFilterNone_WritesNothing(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    claude: {}
    codex: {}
  packages:
    - name: tool-a
      source: ./pkgs/a
      category: utility
`)

	// Act
	out, err := runPackCmd(t, "-m", "none")

	// Assert
	if err != nil {
		t.Fatalf("pack -m none returned error: %v (output: %s)", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude-plugin", "marketplace.json")); !os.IsNotExist(statErr) {
		t.Errorf("-m none must not write claude output (stat err = %v)", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".agents", "plugins", "marketplace.json")); !os.IsNotExist(statErr) {
		t.Errorf("-m none must not write codex output (stat err = %v)", statErr)
	}
}

func TestPackCmd_MarketplaceFilterNarrowsToOneFormat(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    claude: {}
    codex: {}
  packages:
    - name: tool-a
      source: ./pkgs/a
      category: utility
`)

	// Act
	out, err := runPackCmd(t, "-m", "claude")

	// Assert
	if err != nil {
		t.Fatalf("pack -m claude returned error: %v (output: %s)", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude-plugin", "marketplace.json")); statErr != nil {
		t.Errorf("expected claude output to be written: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".agents", "plugins", "marketplace.json")); !os.IsNotExist(statErr) {
		t.Errorf("-m claude must not write codex output (stat err = %v)", statErr)
	}
}

func TestPackCmd_MarketplaceFilter_UnknownFormat_Errors(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages: []
`)

	_, err := runPackCmd(t, "-m", "bogus")
	if err == nil {
		t.Fatal("expected an error for an unknown -m/--marketplace format")
	}
}

// ── remote package resolution against a real local git fixture ──────────

func TestPackCmd_RemotePackage_ResolvesAgainstRealGitTags(t *testing.T) {
	// Arrange: a "remote" package whose Source points at a real local git
	// repo fixture (mirroring internal/marketplace/build/builder_test.go's
	// own convention) -- proves AC3's local+remote mixed resolution wires
	// all the way through the CLI.
	dir := chdirTemp(t)
	remoteDir := t.TempDir()
	initGitRepoWithTags(t, remoteDir, "v1.0.0", "v1.1.0")
	wantSHA := packRevParse(t, remoteDir, "v1.1.0")

	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: remote-tool
      source: `+filepath.ToSlash(remoteDir)+`
      version: "^1.0.0"
`)

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), wantSHA) {
		t.Errorf("output = %s, want resolved sha %q present", data, wantSHA)
	}
}

// ── exit codes: build/config errors are 1, never 2/3/4 ──────────────────

func TestPackCmd_MutuallyExclusiveConfigs_ExitCode1(t *testing.T) {
	// Arrange
	chdirTemp(t)
	writePackApmYML(t, "name: demo\nmarketplace:\n  owner:\n    name: Acme\n  packages: []\n")
	if err := os.WriteFile("marketplace.yml", []byte("owner:\n  name: Acme\npackages: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runPackCmd(t)

	// Assert
	if err == nil {
		t.Fatal("expected an error for mutually exclusive marketplace configs")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1 (config errors are exit 1, not 2)", exitCodeOf(err))
	}
}

func TestPackCmd_CodexMissingCategory_ExitCode1(t *testing.T) {
	// Arrange
	chdirTemp(t)
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    codex: {}
  packages:
    - name: tool-a
      source: ./pkgs/a
`)

	// Act
	_, err := runPackCmd(t)

	// Assert
	if err == nil {
		t.Fatal("expected an error: codex output requires 'category' on every package")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", exitCodeOf(err))
	}
}

// TestPackCmd_MFilterExcludesCodex_MissingCategorySucceeds is F3's
// regression test: the codex category-required gate must only trigger once
// codex is actually still in the active outputs after -m filtering. Here
// `outputs:` configures codex, but `-m claude` filters it back out before
// ClaudeMapper/CodexMapper ever composes anything, so pack must succeed and
// build claude's output despite tool-a having no category at all.
func TestPackCmd_MFilterExcludesCodex_MissingCategorySucceeds(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    claude: {}
    codex: {}
  packages:
    - name: tool-a
      source: ./pkgs/a
`)

	// Act
	out, err := runPackCmd(t, "-m", "claude")

	// Assert
	if err != nil {
		t.Fatalf("pack -m claude returned error: %v (output: %s) (F3: codex category gate must not fire when codex is filtered out)", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude-plugin", "marketplace.json")); statErr != nil {
		t.Errorf("expected claude output to be written: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".agents", "plugins", "marketplace.json")); !os.IsNotExist(statErr) {
		t.Errorf("-m claude must not write codex output (stat err = %v)", statErr)
	}
}

func TestPackCmd_HeadNotAllowed_ExitCode1(t *testing.T) {
	// Arrange
	chdirTemp(t)
	remoteDir := t.TempDir()
	initGitRepoWithTags(t, remoteDir, "v1.0.0")
	gitCmd(t, remoteDir, "branch", "feature-x")

	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: remote-tool
      source: `+filepath.ToSlash(remoteDir)+`
      ref: feature-x
`)

	// Act
	_, err := runPackCmd(t)

	// Assert
	if err == nil {
		t.Fatal("expected a HeadNotAllowedError for a branch ref")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", exitCodeOf(err))
	}
	if strings.Contains(err.Error(), "--allow-head") {
		t.Errorf("error must not mention the nonexistent --allow-head flag: %v", err)
	}
}

func TestPackCmd_Success_ExitCode0(t *testing.T) {
	// Arrange
	chdirTemp(t)
	if err := os.MkdirAll(filepath.Join("pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: tool-a
      source: ./pkgs/a
`)

	// Act
	_, err := runPackCmd(t)

	// Assert
	if exitCodeOf(err) != 0 {
		t.Errorf("exitCodeOf(err) = %d, want 0", exitCodeOf(err))
	}
}

// ── --offline: no cache layer, fails loud instead of silently degrading ─

func TestPackCmd_Offline_RemotePackageWithVersion_Errors(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: remote-tool
      source: owner/repo
      version: "^1.0.0"
`)

	_, err := runPackCmd(t, "--offline")
	if err == nil {
		t.Fatal("expected an error: --offline has no cached refs to resolve against")
	}
}

// ── legacy marketplace.yml source: deprecation warning ───────────────────

func TestPackCmd_LegacyConfigSource_PrintsDeprecationWarning(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("marketplace.yml", []byte(`owner:
  name: Acme
packages:
  - name: tool-a
    source: ./pkgs/a
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "marketplace migrate") {
		t.Errorf("output = %q, want a deprecation warning pointing at 'apm marketplace migrate'", out)
	}
}
