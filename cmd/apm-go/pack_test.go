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

// ── no producer applies -> exit 1, "nothing to pack" ─────────────────────
//
// Phase 2-5 (design.md Gate 1 disposition) replaced the P0 quick-win's
// exit-0 "nothing to do" info with Python's own BuildOrchestrator.run
// BuildError semantics: apm-go now implements all three producers
// (Bundle/Marketplace/PluginManifest), so a project with none of
// dependencies:/marketplace:/target:{claude,copilot} genuinely has nothing
// any producer can act on, and that is a user-facing failure (exit 1), not
// a silent no-op.

// wantNothingToPack duplicates pack.ErrNothingToPack's text as an
// independent string literal -- not a reference to the production
// identifier -- so a wording change in internal/pack breaks this test with
// a red diff instead of both sides silently drifting together (same
// verbatim-lock pattern as errNoDeployTarget's literal check in
// install_test.go).
const wantNothingToPack = "apm.yml has neither 'dependencies:' nor 'marketplace:' block, and 'target:' does not include 'claude' or 'copilot'. Nothing to pack. Add dependencies via 'apm-go install <pkg>', configure a 'marketplace:' block, or set 'target:' to include 'claude' or 'copilot'."

func TestPackCmd_NoMarketplaceBlock_ExitsOne(t *testing.T) {
	// Arrange
	chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\n")

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err == nil {
		t.Fatalf("expected an error for a manifest with no producer inputs (output: %s)", out)
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", exitCodeOf(err))
	}
	if err.Error() != wantNothingToPack {
		t.Errorf("err = %q, want %q", err.Error(), wantNothingToPack)
	}
}

func TestPackCmd_ExplicitNullMarketplaceKey_ExitsOne(t *testing.T) {
	// Arrange: a bare "marketplace:" key with nothing after it is mkt-047's
	// "_has_marketplace_block" null case -- not really present.
	chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\nmarketplace:\n")

	// Act
	_, err := runPackCmd(t)

	// Assert
	if err == nil {
		t.Fatal("expected an error for a null marketplace: key")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", exitCodeOf(err))
	}
}

func TestPackCmd_NoApmYMLAtAll_ExitsOne(t *testing.T) {
	// Arrange: not even an apm.yml exists yet.
	chdirTemp(t)

	// Act
	_, err := runPackCmd(t)

	// Assert
	if err == nil {
		t.Fatal("expected an error when no apm.yml exists at all")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", exitCodeOf(err))
	}
}

// ── pack --help documents all three producers ────────────────────────────

func TestPackCmd_HelpDocumentsThreeProducers(t *testing.T) {
	out, err := runPackCmd(t, "--help")
	if err != nil {
		t.Fatalf("pack --help returned error: %v", err)
	}
	for _, token := range []string{"marketplace.json", "dependencies:", "plugin.json", "target:"} {
		if !strings.Contains(out, token) {
			t.Errorf("pack --help output missing %q:\n%s", token, out)
		}
	}
	// Full scope lines locked verbatim -- must stay in sync with packCmd's
	// Long text.
	for _, line := range []string{
		"a plugin-native bundle, from a 'dependencies:' block",
		"a standalone plugin.json, from 'target:'/'targets:' containing",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("pack --help output missing scope line %q:\n%s", line, out)
		}
	}
}

// ── Gate 2: dependencies:/target: without marketplace: actually build ────
//
// Phase 2-5 upgraded BundleProducer/PluginManifestProducer from P0's
// warn-only stubs to full producers -- dependencies: now really builds a
// bundle under ./build/, and target: claude/copilot now really writes
// plugin.json, matching Python's oracle instead of deferring to a warning.

func TestRunPack_DependenciesOnly_BuildsRealBundle(t *testing.T) {
	// A remote dependencies.apm entry with no apm.lock.yaml present is
	// enough to trigger BundleProducer (hasDeps only looks at
	// ParsedDeps -- it does not require the dependency to actually be
	// resolved/materialized); with no lockfile, BundleProducer's
	// dependency-collection loop is simply empty and the bundle is built
	// purely from the project's own local .apm/ content.
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, ".apm", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".apm", "agents", "foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/tool\n")

	out, err := runPackCmd(t)
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Packed") {
		t.Errorf("output = %q, want a real bundle-built confirmation", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build", "demo-1.0.0", "plugin.json")); statErr != nil {
		t.Errorf("expected a real bundle at build/demo-1.0.0/plugin.json: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build", "demo-1.0.0", "agents", "foo.md")); statErr != nil {
		t.Errorf("expected local .apm/agents content bundled: %v", statErr)
	}
}

func TestRunPack_TargetClaudeOnly_WritesRealPluginJSON(t *testing.T) {
	dir := chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ntarget:\n  - claude\n")

	out, err := runPackCmd(t)
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Generated plugin manifest") {
		t.Errorf("output = %q, want a real plugin.json confirmation", out)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if rerr != nil {
		t.Fatalf("expected a real plugin.json: %v", rerr)
	}
	if !strings.Contains(string(data), `"name": "demo"`) {
		t.Errorf("plugin.json = %s, want the synthesized name field", data)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build")); !os.IsNotExist(statErr) {
		t.Errorf("target-only input must not also build a bundle (stat err = %v)", statErr)
	}
}

func TestRunPack_TargetCodexOnly_ExitsOne_NotPluginManifestEcosystem(t *testing.T) {
	// codex is a valid target but NOT a plugin-manifest ecosystem
	// (claude/copilot only) -- with no dependencies:/marketplace: either,
	// this is still "nothing to pack".
	dir := chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ntarget:\n  - codex\n")

	_, err := runPackCmd(t)
	if err == nil {
		t.Fatal("expected an error: codex alone triggers no producer")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", exitCodeOf(err))
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude-plugin")); !os.IsNotExist(statErr) {
		t.Errorf("apm-go must not produce a plugin.json for a non-plugin-manifest target (stat err = %v)", statErr)
	}
}

func TestRunPack_GenuinelyEmptyApmYML_ExitsOne(t *testing.T) {
	dir := chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\n")

	_, err := runPackCmd(t)
	if err == nil {
		t.Fatal("expected an error for a genuinely empty apm.yml")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", exitCodeOf(err))
	}
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if len(entries) != 1 || entries[0].Name() != "apm.yml" {
		t.Errorf("pack must not write any file/dir for a genuinely empty apm.yml, got %v", entries)
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

// ── authoring-path license nudge (export/authoring.py's _WARN_MESSAGE) ──

// wantLicenseUndeclaredWarning duplicates pack.go's licenseUndeclaredWarning
// as an independent string literal, matching this file's verbatim-lock
// convention.
const wantLicenseUndeclaredWarning = "[warn] No 'license:' field in apm.yml; the SBOM will record NOASSERTION for this package. Add a 'license:' field to apm.yml (an SPDX expression such as MIT or Apache-2.0, or UNLICENSED) to declare it."

func TestRunPack_NoLicenseField_WarnsEvenOnNothingToPack(t *testing.T) {
	// The nudge fires before producer routing, independent of whether pack
	// ultimately succeeds -- mirrors Python firing it before
	// BuildOrchestrator().run() is ever called.
	chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\n")

	out, err := runPackCmd(t)
	if err == nil {
		t.Fatal("expected the usual nothing-to-pack error")
	}
	if !strings.Contains(out, wantLicenseUndeclaredWarning) {
		t.Errorf("output = %q, want the license-undeclared warning", out)
	}
}

func TestRunPack_LicenseDeclared_NoWarning(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\nlicense: MIT\ntarget:\n  - claude\n")

	out, err := runPackCmd(t)
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if strings.Contains(out, "NOASSERTION") {
		t.Errorf("output = %q, must not warn when license: is declared", out)
	}
}

func TestRunPack_EmptyLicenseField_StillWarns(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\nlicense: \"\"\ntarget:\n  - claude\n")

	out, err := runPackCmd(t)
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, wantLicenseUndeclaredWarning) {
		t.Errorf("output = %q, want the license-undeclared warning for an empty license: value", out)
	}
}
