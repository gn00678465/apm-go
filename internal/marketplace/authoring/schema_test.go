package authoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile is a small test helper: create parent dirs if needed and write
// content to dir/name.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

// ── mkt-047: apm.yml `marketplace:` block vs legacy marketplace.yml ──

func TestLoadAuthoringConfig_BothPresentIsHardError(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
# hand-authored comment kept here on purpose
marketplace:
  owner:
    name: Acme
    url: https://acme.example.com
  packages: []
`)
	writeFile(t, dir, "marketplace.yml", `owner:
  name: Acme
packages: []
`)

	// Act
	cfg, src, err := LoadAuthoringConfig(dir)

	// Assert
	if err == nil {
		t.Fatal("expected mutual-exclusion error, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error %q should mention mutual exclusivity", err.Error())
	}
	if cfg != nil {
		t.Errorf("expected nil config on error, got %+v", cfg)
	}
	_ = src
}

// TestLoadAuthoringConfig_BothPresentIsHardError_EmptyLegacyFile locks
// mkt-047's boundary case: legacy detection is bare file *existence*, not
// non-empty content -- an empty marketplace.yml must still trigger the hard
// error when apm.yml has a non-null marketplace: block.
func TestLoadAuthoringConfig_BothPresentIsHardError_EmptyLegacyFile(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
marketplace:
  owner:
    name: Acme
`)
	writeFile(t, dir, "marketplace.yml", "") // zero bytes, still "exists"

	// Act
	_, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err == nil {
		t.Fatal("expected mutual-exclusion error for empty legacy file, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error %q should mention mutual exclusivity", err.Error())
	}
}

func TestLoadAuthoringConfig_LegacyOnly_ReturnsConfigSourceLegacy(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "marketplace.yml", `# legacy standalone marketplace config
owner:
  name: Acme Legacy
  url: https://acme.example.com
build:
  tagPattern: "v{version}"
packages:
  - name: tool-a
    source: ./pkgs/tool-a
`)

	// Act
	cfg, src, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src != ConfigSourceLegacy {
		t.Errorf("ConfigSource = %v, want ConfigSourceLegacy", src)
	}
	if cfg.Owner.Name != "Acme Legacy" {
		t.Errorf("Owner.Name = %q, want %q", cfg.Owner.Name, "Acme Legacy")
	}
	if len(cfg.Packages) != 1 || cfg.Packages[0].Name != "tool-a" {
		t.Errorf("Packages = %+v, want one entry named tool-a", cfg.Packages)
	}
}

func TestLoadAuthoringConfig_ApmYMLOnly_ReturnsConfigSourceApmYML(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
description: a demo project
# marketplace authoring block
marketplace:
  owner:
    name: Acme
    url: https://acme.example.com
  build:
    tagPattern: "v{version}"
  outputs:
    claude: {}
    # codex: {}
  packages:
    - name: tool-a
      description: a tool
      source: owner/repo
      version: ">=1.0.0"
`)

	// Act
	cfg, src, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src != ConfigSourceApmYML {
		t.Errorf("ConfigSource = %v, want ConfigSourceApmYML", src)
	}
	if cfg.Owner.Name != "Acme" || cfg.Owner.URL != "https://acme.example.com" {
		t.Errorf("Owner = %+v, unexpected", cfg.Owner)
	}
	if cfg.Build.TagPattern != "v{version}" {
		t.Errorf("Build.TagPattern = %q, want %q", cfg.Build.TagPattern, "v{version}")
	}
	if len(cfg.Outputs) != 1 || cfg.Outputs[0] != "claude" {
		t.Errorf("Outputs = %v, want [claude]", cfg.Outputs)
	}
	if len(cfg.Packages) != 1 {
		t.Fatalf("Packages = %+v, want 1 entry", cfg.Packages)
	}
	pkg := cfg.Packages[0]
	if pkg.Name != "tool-a" || pkg.Description != "a tool" || pkg.Source != "owner/repo" || pkg.Version != ">=1.0.0" {
		t.Errorf("Packages[0] = %+v, unexpected", pkg)
	}
}

// TestLoadAuthoringConfig_NullMarketplaceBlockTreatedAsAbsent locks the
// "非 null" half of mkt-047: an explicit `marketplace:` key with no value
// (parses to YAML null) must NOT count as "apm.yml has a marketplace
// block" -- otherwise a bare placeholder key would falsely trigger the
// both-present hard error against a legacy file.
func TestLoadAuthoringConfig_NullMarketplaceBlockTreatedAsAbsent(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
marketplace:
`)
	writeFile(t, dir, "marketplace.yml", `owner:
  name: Acme
packages: []
`)

	// Act
	cfg, src, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src != ConfigSourceLegacy {
		t.Errorf("ConfigSource = %v, want ConfigSourceLegacy (null marketplace: key should not count)", src)
	}
	if cfg.Owner.Name != "Acme" {
		t.Errorf("Owner.Name = %q, want %q", cfg.Owner.Name, "Acme")
	}
}

func TestLoadAuthoringConfig_NeitherPresent_ReturnsExplicitError(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
`)

	// Act
	cfg, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err == nil {
		t.Fatal("expected explicit error when neither source exists, got nil")
	}
	if !strings.Contains(err.Error(), "apm marketplace init") {
		t.Errorf("error %q should point at 'apm marketplace init'", err.Error())
	}
	if cfg != nil {
		t.Errorf("expected nil config on error, got %+v", cfg)
	}
}

func TestLoadAuthoringConfig_NeitherPresent_NoApmYMLAtAll(t *testing.T) {
	// Arrange: a completely empty directory (no apm.yml at all)
	dir := t.TempDir()

	// Act
	_, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err == nil {
		t.Fatal("expected explicit error when no config file exists at all")
	}
}

// ── req-mf-017: packages[].source validation must reuse
// manifest.ValidateMarketplaceSource, not reimplement it. ──

func TestLoadAuthoringConfig_SourceValidation_ReusesManifestValidator(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantErrSub string
	}{
		{"dotdot segment", "../escape", ".."},
		{"dotdot deep in local path", "./packages/../../../etc/passwd", ".."},
		{"non-https scheme", "http://example.com/repo", "https://"},
		{"userinfo in URL", "https://user@example.com/repo", "userinfo"},
		{"port in URL", "https://example.com:8080/repo", "port"},
		{"query string in URL", "https://example.com/repo?q=1", "query"},
		{"local without leading ./", ".packages/foo", "start with './'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			dir := t.TempDir()
			writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
				"  owner:\n    name: Acme\n  packages:\n    - name: tool-a\n      source: "+tt.source+"\n")

			// Act
			_, _, err := LoadAuthoringConfig(dir)

			// Assert
			if err == nil {
				t.Fatalf("expected source %q to be rejected", tt.source)
			}
			if !strings.Contains(err.Error(), tt.wantErrSub) {
				t.Errorf("error %q should contain %q (from manifest.ValidateMarketplaceSource)", err.Error(), tt.wantErrSub)
			}
		})
	}
}

func TestLoadAuthoringConfig_SourceValidation_AcceptsValidShapes(t *testing.T) {
	tests := []string{
		"./packages/foo",
		"https://example.com/owner/repo",
		"owner/repo",
		"github.com/owner/repo",
	}

	for _, source := range tests {
		t.Run(source, func(t *testing.T) {
			// Arrange
			dir := t.TempDir()
			writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n"+
				"  owner:\n    name: Acme\n  packages:\n    - name: tool-a\n      source: "+source+"\n")

			// Act
			_, _, err := LoadAuthoringConfig(dir)

			// Assert
			if err != nil {
				t.Errorf("unexpected error for valid source %q: %v", source, err)
			}
		})
	}
}

// ── PackageEntry.Tags: `tags` + `keywords` merged, deduplicated ──

func TestLoadAuthoringConfig_PackageTagsAndKeywordsMerged(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
marketplace:
  owner:
    name: Acme
  packages:
    - name: tool-a
      source: ./pkgs/tool-a
      tags: [alpha, beta]
      keywords: [beta, gamma]
`)

	// Act
	cfg, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := cfg.Packages[0].Tags
	want := []string{"alpha", "beta", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("Tags = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Tags = %v, want %v", got, want)
		}
	}
}

// ── PackageEntry field completeness (mkt-047 data-model shape) ──

func TestLoadAuthoringConfig_PackageEntryFields(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
marketplace:
  owner:
    name: Acme
  packages:
    - name: tool-a
      description: does a thing
      source: owner/repo
      ref: v1.2.3
      subdir: packages/tool-a
      tag_pattern: "tool-a-v{version}"
      include_prerelease: true
      category: utilities
`)

	// Act
	cfg, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pkg := cfg.Packages[0]
	if pkg.Ref != "v1.2.3" {
		t.Errorf("Ref = %q, want %q", pkg.Ref, "v1.2.3")
	}
	if pkg.Subdir != "packages/tool-a" {
		t.Errorf("Subdir = %q, want %q", pkg.Subdir, "packages/tool-a")
	}
	if pkg.TagPattern != "tool-a-v{version}" {
		t.Errorf("TagPattern = %q, want %q", pkg.TagPattern, "tool-a-v{version}")
	}
	if !pkg.IncludePrerelease {
		t.Error("IncludePrerelease = false, want true")
	}
	if pkg.Category != "utilities" {
		t.Errorf("Category = %q, want %q", pkg.Category, "utilities")
	}
}

// ── AuthoringConfig top-level name/description/version inheritance ──────
// (needed by internal/marketplace/build's ClaudeMapper, mkt-050/052 修訂版)

func TestLoadAuthoringConfig_TopLevelFieldsInheritedByDefault(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
description: a demo project
marketplace:
  owner:
    name: Acme
  packages: []
`)

	// Act
	cfg, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "demo" {
		t.Errorf("Name = %q, want %q", cfg.Name, "demo")
	}
	if cfg.Description != "a demo project" || cfg.DescriptionOverridden {
		t.Errorf("Description = %q DescriptionOverridden = %v, want inherited value + false", cfg.Description, cfg.DescriptionOverridden)
	}
	if cfg.Version != "1.0.0" || cfg.VersionOverridden {
		t.Errorf("Version = %q VersionOverridden = %v, want inherited value + false", cfg.Version, cfg.VersionOverridden)
	}
}

func TestLoadAuthoringConfig_MarketplaceBlockOverridesTopLevelFields(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
description: a demo project
marketplace:
  name: demo-marketplace
  description: a curated marketplace
  version: 2.0.0
  owner:
    name: Acme
  packages: []
`)

	// Act
	cfg, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "demo-marketplace" {
		t.Errorf("Name = %q, want the marketplace block's override", cfg.Name)
	}
	if cfg.Description != "a curated marketplace" || !cfg.DescriptionOverridden {
		t.Errorf("Description = %q DescriptionOverridden = %v, want override value + true", cfg.Description, cfg.DescriptionOverridden)
	}
	if cfg.Version != "2.0.0" || !cfg.VersionOverridden {
		t.Errorf("Version = %q VersionOverridden = %v, want override value + true", cfg.Version, cfg.VersionOverridden)
	}
}

func TestLoadAuthoringConfig_Legacy_AlwaysOverridden(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "marketplace.yml", `name: legacy-marketplace
description: a legacy marketplace
version: 1.0.0
owner:
  name: Acme Legacy
packages: []
`)

	// Act
	cfg, src, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src != ConfigSourceLegacy {
		t.Fatalf("ConfigSource = %v, want ConfigSourceLegacy", src)
	}
	if cfg.Name != "legacy-marketplace" || cfg.Description != "a legacy marketplace" || cfg.Version != "1.0.0" {
		t.Errorf("top-level fields = %+v, unexpected", cfg)
	}
	if !cfg.DescriptionOverridden || !cfg.VersionOverridden {
		t.Errorf("DescriptionOverridden/VersionOverridden = %v/%v, want both true for a legacy config",
			cfg.DescriptionOverridden, cfg.VersionOverridden)
	}
}

// ── Owner.Email ───────────────────────────────────────────────────────────

func TestLoadAuthoringConfig_OwnerEmail(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
marketplace:
  owner:
    name: Acme
    email: acme@example.com
    url: https://acme.example.com
  packages: []
`)

	// Act
	cfg, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Owner.Email != "acme@example.com" {
		t.Errorf("Owner.Email = %q, want acme@example.com", cfg.Owner.Email)
	}
}

// ── marketplace.metadata pass-through ─────────────────────────────────────

func TestLoadAuthoringConfig_MetadataPassthrough(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
marketplace:
  owner:
    name: Acme
  metadata:
    pluginRoot: ./packages
    customKey: customValue
  packages: []
`)

	// Act
	cfg, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Metadata["pluginRoot"] != "./packages" {
		t.Errorf("Metadata[pluginRoot] = %v, want ./packages", cfg.Metadata["pluginRoot"])
	}
	if cfg.Metadata["customKey"] != "customValue" {
		t.Errorf("Metadata[customKey] = %v, want customValue", cfg.Metadata["customKey"])
	}
}

func TestLoadAuthoringConfig_MetadataExplicitNull_NilMap(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
marketplace:
  owner:
    name: Acme
  metadata:
  packages: []
`)

	// Act
	cfg, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Metadata) != 0 {
		t.Errorf("Metadata = %v, want empty for an explicit null metadata: key", cfg.Metadata)
	}
}

func TestLoadAuthoringConfig_MetadataAbsent_NilMap(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
marketplace:
  owner:
    name: Acme
  packages: []
`)

	// Act
	cfg, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Metadata) != 0 {
		t.Errorf("Metadata = %v, want empty", cfg.Metadata)
	}
}

// ── PackageEntry: homepage/author/license/repository ─────────────────────

func TestLoadAuthoringConfig_PackageEntry_HomepageAuthorLicenseRepository(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
marketplace:
  owner:
    name: Acme
  packages:
    - name: tool-a
      source: ./pkgs/tool-a
      homepage: https://example.com/tool-a
      license: MIT
      repository: https://github.com/acme/tool-a
      author:
        name: Jane Doe
        email: jane@example.com
        url: https://jane.example.com
`)

	// Act
	cfg, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pkg := cfg.Packages[0]
	if pkg.Homepage != "https://example.com/tool-a" {
		t.Errorf("Homepage = %q, unexpected", pkg.Homepage)
	}
	if pkg.License != "MIT" {
		t.Errorf("License = %q, unexpected", pkg.License)
	}
	if pkg.Repository != "https://github.com/acme/tool-a" {
		t.Errorf("Repository = %q, unexpected", pkg.Repository)
	}
	want := map[string]string{"name": "Jane Doe", "email": "jane@example.com", "url": "https://jane.example.com"}
	if len(pkg.Author) != len(want) {
		t.Fatalf("Author = %+v, want %+v", pkg.Author, want)
	}
	for k, v := range want {
		if pkg.Author[k] != v {
			t.Errorf("Author[%q] = %q, want %q", k, pkg.Author[k], v)
		}
	}
}

func TestLoadAuthoringConfig_PackageEntry_AuthorAsBareString(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
marketplace:
  owner:
    name: Acme
  packages:
    - name: tool-a
      source: ./pkgs/tool-a
      author: Jane Doe
`)

	// Act
	cfg, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pkg := cfg.Packages[0]
	if len(pkg.Author) != 1 || pkg.Author["name"] != "Jane Doe" {
		t.Errorf("Author = %+v, want {name: Jane Doe}", pkg.Author)
	}
}

func TestLoadAuthoringConfig_PackageEntry_NoAuthor_NilMap(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
marketplace:
  owner:
    name: Acme
  packages:
    - name: tool-a
      source: ./pkgs/tool-a
`)

	// Act
	cfg, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Packages[0].Author != nil {
		t.Errorf("Author = %+v, want nil", cfg.Packages[0].Author)
	}
}

// ── outputs: map-form parsing (design.md init scaffold shape) ──

func TestLoadAuthoringConfig_OutputsMapForm(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
marketplace:
  owner:
    name: Acme
  outputs:
    claude: {}
    codex: {}
  packages: []
`)

	// Act
	cfg, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"claude", "codex"}
	if len(cfg.Outputs) != len(want) {
		t.Fatalf("Outputs = %v, want %v", cfg.Outputs, want)
	}
	for i := range want {
		if cfg.Outputs[i] != want[i] {
			t.Errorf("Outputs = %v, want %v", cfg.Outputs, want)
		}
	}
}

// ── mkt-053: outputs 含 codex 時 category 為每個 package 必填 ──
//
// F3 fix: this gate is compose-time-only (internal/marketplace/build/
// codexmapper.go's CategoryRequiredError, triggered only when
// CodexMapper.Compose actually runs -- i.e. after `apm pack`'s -m filtering
// still leaves codex in the active outputs). LoadAuthoringConfig itself
// must NEVER reject a missing category: it is shared by `apm pack`'s config
// loading (which may filter codex out via -m before ever composing it),
// `apm marketplace package add/remove/set`'s pre-edit load (editor.go), and
// `apm marketplace migrate` -- none of which should be blocked by a
// category gate that only matters for an actual codex build.
func TestLoadAuthoringConfig_CodexOutput_MissingCategory_NoErrorAtLoadTime(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
marketplace:
  owner:
    name: Acme
  outputs:
    codex: {}
  packages:
    - name: tool-a
      source: owner/repo
      version: ">=1.0.0"
`)

	// Act
	_, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("LoadAuthoringConfig must not enforce codex's category-required gate at load time (F3): %v", err)
	}
}

func TestLoadAuthoringConfig_CodexOutput_AllPackagesHaveCategory_NoError(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
marketplace:
  owner:
    name: Acme
  outputs:
    codex: {}
  packages:
    - name: tool-a
      source: owner/repo
      version: ">=1.0.0"
      category: utilities
`)

	// Act
	_, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadAuthoringConfig_ClaudeOnlyOutput_MissingCategory_NoError(t *testing.T) {
	// Arrange: category is a Codex-only requirement -- claude-only outputs
	// (the default) must not reject a package that never declared one
	// (design.md: "outputs 只有 claude 時缺 category 不報錯").
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
marketplace:
  owner:
    name: Acme
  outputs:
    claude: {}
  packages:
    - name: tool-a
      source: owner/repo
      version: ">=1.0.0"
`)

	// Act
	_, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadAuthoringConfig_CodexOutput_MultiplePackagesMissingCategory_NoErrorAtLoadTime(t *testing.T) {
	// Arrange: F3 fix -- even multiple packages missing category, with
	// outputs including codex, must not fail LoadAuthoringConfig itself.
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", `name: demo
version: 1.0.0
marketplace:
  owner:
    name: Acme
  outputs:
    codex: {}
  packages:
    - name: tool-a
      source: owner/repo-a
      version: ">=1.0.0"
    - name: tool-b
      source: owner/repo-b
      version: ">=1.0.0"
      category: utilities
    - name: tool-c
      source: owner/repo-c
      version: ">=1.0.0"
`)

	// Act
	_, _, err := LoadAuthoringConfig(dir)

	// Assert
	if err != nil {
		t.Fatalf("LoadAuthoringConfig must not enforce codex's category-required gate at load time (F3): %v", err)
	}
}
