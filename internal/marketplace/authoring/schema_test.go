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
