// Tests for output.go: mkt-054's output-path resolution (default path per
// profile, apm.yml-declared overrides in both YAML forms, CLI overrides,
// and the path-traversal guard) plus the atomic JSON writer.
package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/marketplace/authoring"
)

// ── DefaultOutputPath ─────────────────────────────────────────────────────

func TestDefaultOutputPath(t *testing.T) {
	tests := []struct {
		format string
		want   string
		wantOK bool
	}{
		{"claude", filepath.Join(".claude-plugin", "marketplace.json"), true},
		{"codex", filepath.Join(".agents", "plugins", "marketplace.json"), true},
		{"bogus", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got, ok := DefaultOutputPath(tt.format)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("DefaultOutputPath(%q) = (%q, %v), want (%q, %v)", tt.format, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// ── ResolveOutputPath: CLI > apm.yml override > profile default ─────────

func TestResolveOutputPath_DefaultsWhenNoOverrides(t *testing.T) {
	got, err := ResolveOutputPath("claude", nil, nil)
	if err != nil {
		t.Fatalf("ResolveOutputPath() error = %v", err)
	}
	want := filepath.Join(".claude-plugin", "marketplace.json")
	if got != want {
		t.Errorf("ResolveOutputPath() = %q, want %q", got, want)
	}
}

func TestResolveOutputPath_ConfigOverrideWinsOverDefault(t *testing.T) {
	configPaths := map[string]string{"claude": "dist/marketplace.json"}
	got, err := ResolveOutputPath("claude", configPaths, nil)
	if err != nil {
		t.Fatalf("ResolveOutputPath() error = %v", err)
	}
	if got != "dist/marketplace.json" {
		t.Errorf("ResolveOutputPath() = %q, want dist/marketplace.json", got)
	}
}

func TestResolveOutputPath_CLIOverrideWinsOverConfigOverride(t *testing.T) {
	configPaths := map[string]string{"claude": "dist/marketplace.json"}
	cliOverrides := map[string]string{"claude": "cli-dist/marketplace.json"}
	got, err := ResolveOutputPath("claude", configPaths, cliOverrides)
	if err != nil {
		t.Fatalf("ResolveOutputPath() error = %v", err)
	}
	if got != "cli-dist/marketplace.json" {
		t.Errorf("ResolveOutputPath() = %q, want cli-dist/marketplace.json", got)
	}
}

func TestResolveOutputPath_UnknownFormat_ReturnsError(t *testing.T) {
	_, err := ResolveOutputPath("bogus", nil, nil)
	if err == nil {
		t.Fatal("expected an error for an unknown output format")
	}
}

// ── LoadOutputPathOverrides: map form + legacy sibling form ──────────────

func writeApmYML(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "apm.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOutputPathOverrides_MapForm(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeApmYML(t, dir, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    claude:
      path: dist/claude-marketplace.json
    codex:
      path: dist/codex-marketplace.json
  packages: []
`)

	// Act
	got, err := LoadOutputPathOverrides(dir, authoring.ConfigSourceApmYML)

	// Assert
	if err != nil {
		t.Fatalf("LoadOutputPathOverrides() error = %v", err)
	}
	want := map[string]string{
		"claude": "dist/claude-marketplace.json",
		"codex":  "dist/codex-marketplace.json",
	}
	if len(got) != len(want) {
		t.Fatalf("got = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("got[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestLoadOutputPathOverrides_CompatSiblingForm(t *testing.T) {
	// Arrange: the legacy per-format sub-block form, `marketplace.<fmt>.output`.
	dir := t.TempDir()
	writeApmYML(t, dir, `name: demo
marketplace:
  owner:
    name: Acme
  claude:
    output: legacy/claude-marketplace.json
  packages: []
`)

	// Act
	got, err := LoadOutputPathOverrides(dir, authoring.ConfigSourceApmYML)

	// Assert
	if err != nil {
		t.Fatalf("LoadOutputPathOverrides() error = %v", err)
	}
	if got["claude"] != "legacy/claude-marketplace.json" {
		t.Errorf("got[claude] = %q, want legacy/claude-marketplace.json", got["claude"])
	}
}

func TestLoadOutputPathOverrides_MapFormWinsOverCompatForm(t *testing.T) {
	// Arrange: both forms declare a path for "claude" -- design.md's stated
	// priority is "map 形式優先".
	dir := t.TempDir()
	writeApmYML(t, dir, `name: demo
marketplace:
  owner:
    name: Acme
  claude:
    output: legacy/claude-marketplace.json
  outputs:
    claude:
      path: dist/claude-marketplace.json
  packages: []
`)

	// Act
	got, err := LoadOutputPathOverrides(dir, authoring.ConfigSourceApmYML)

	// Assert
	if err != nil {
		t.Fatalf("LoadOutputPathOverrides() error = %v", err)
	}
	if got["claude"] != "dist/claude-marketplace.json" {
		t.Errorf("got[claude] = %q, want the map form's path to win", got["claude"])
	}
}

func TestLoadOutputPathOverrides_NoOverridesDeclared_ReturnsNil(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeApmYML(t, dir, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    claude: {}
  packages: []
`)

	// Act
	got, err := LoadOutputPathOverrides(dir, authoring.ConfigSourceApmYML)

	// Assert
	if err != nil {
		t.Fatalf("LoadOutputPathOverrides() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got = %v, want no overrides", got)
	}
}

func TestLoadOutputPathOverrides_LegacyConfigSource_ReadsMarketplaceYml(t *testing.T) {
	// Arrange: a legacy standalone marketplace.yml holds the block at its
	// own document root (no "marketplace:" key wrapper).
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marketplace.yml"), []byte(`owner:
  name: Acme
outputs:
  claude:
    path: legacy-root/marketplace.json
packages: []
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	got, err := LoadOutputPathOverrides(dir, authoring.ConfigSourceLegacy)

	// Assert
	if err != nil {
		t.Fatalf("LoadOutputPathOverrides() error = %v", err)
	}
	if got["claude"] != "legacy-root/marketplace.json" {
		t.Errorf("got[claude] = %q, want legacy-root/marketplace.json", got["claude"])
	}
}

// ── EnsureWithinRoot: mkt-054's path-traversal guard ──────────────────────

func TestEnsureWithinRoot_RelativePathInsideRoot_Passes(t *testing.T) {
	root := t.TempDir()
	got, err := EnsureWithinRoot(root, filepath.Join("dist", "marketplace.json"))
	if err != nil {
		t.Fatalf("EnsureWithinRoot() error = %v", err)
	}
	wantPrefix, _ := filepath.Abs(root)
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("EnsureWithinRoot() = %q, want it rooted under %q", got, wantPrefix)
	}
}

func TestEnsureWithinRoot_TraversalEscapingRoot_ReturnsError(t *testing.T) {
	root := t.TempDir()
	_, err := EnsureWithinRoot(root, filepath.Join("..", "..", "etc", "passwd"))
	if err == nil {
		t.Fatal("expected an error: path escapes the project root")
	}
}

func TestEnsureWithinRoot_AbsolutePathOutsideRoot_ReturnsError(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	_, err := EnsureWithinRoot(root, filepath.Join(outside, "marketplace.json"))
	if err == nil {
		t.Fatal("expected an error: absolute path outside the project root")
	}
}

// ── WriteOutput: 2-space indent + trailing newline, atomic write ────────

func TestWriteOutput_WritesTwoSpaceIndentedJSONWithTrailingNewline(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "marketplace.json")
	doc := map[string]any{"name": "demo", "plugins": []any{}}

	// Act
	if err := WriteOutput(path, doc); err != nil {
		t.Fatalf("WriteOutput() error = %v", err)
	}

	// Assert
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Errorf("output does not end with a trailing newline: %q", data)
	}
	if !strings.Contains(string(data), "\n  \"name\": \"demo\"") {
		t.Errorf("output is not 2-space indented: %s", data)
	}
	var roundTrip map[string]any
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestWriteOutput_CreatesMissingParentDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "marketplace.json")
	if err := WriteOutput(path, map[string]any{"name": "demo"}); err != nil {
		t.Fatalf("WriteOutput() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

func TestWriteOutput_OverwritesExistingFileAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "marketplace.json")
	if err := os.WriteFile(path, []byte("stale content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteOutput(path, map[string]any{"name": "fresh"}); err != nil {
		t.Fatalf("WriteOutput() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "stale") {
		t.Errorf("expected stale content to be replaced, got: %s", data)
	}
	if !strings.Contains(string(data), "fresh") {
		t.Errorf("expected fresh content to be written, got: %s", data)
	}
}
