package marketplace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLocalManifest writes manifest as JSON at dir/relPath, creating any
// intermediate directories the mkt-003 fallback paths need (e.g.
// ".github/plugin/").
func writeLocalManifest(t *testing.T, dir, relPath string, manifest MarketplaceManifest) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("json.Marshal fixture manifest: %v", err)
	}
	full := filepath.Join(dir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", full, err)
	}
}

// TestFetchLocal_ProbePathOrder covers mkt-003: when Path is the parser's
// default, fetchLocal tries marketplace.json, then
// .github/plugin/marketplace.json, then .claude-plugin/marketplace.json --
// in that order -- and uses whichever candidate exists.
func TestFetchLocal_ProbePathOrder(t *testing.T) {
	tests := []struct {
		name       string
		manifestAt string
	}{
		{"top-level marketplace.json wins", "marketplace.json"},
		{"github plugin fallback", ".github/plugin/marketplace.json"},
		{"claude-plugin fallback", ".claude-plugin/marketplace.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			dir := t.TempDir()
			want := MarketplaceManifest{Name: "acme", Owner: "acme-owner"}
			writeLocalManifest(t, dir, tt.manifestAt, want)
			src := &MarketplaceSource{URL: dir, Path: defaultManifestPath}

			// Act
			got, err := fetchLocal(context.Background(), src)

			// Assert
			if err != nil {
				t.Fatalf("fetchLocal() returned error: %v", err)
			}
			if got.Name != want.Name || got.Owner != want.Owner {
				t.Errorf("fetchLocal() manifest = %+v, want Name=%q Owner=%q", got, want.Name, want.Owner)
			}
		})
	}
}

// TestFetchLocal_PrefersEarlierCandidate covers the "first match wins, not
// closest match" probing rule: when more than one candidate path exists,
// the earliest one in mkt-003's order is used.
func TestFetchLocal_PrefersEarlierCandidate(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeLocalManifest(t, dir, "marketplace.json", MarketplaceManifest{Name: "top-level"})
	writeLocalManifest(t, dir, ".github/plugin/marketplace.json", MarketplaceManifest{Name: "github-fallback"})
	src := &MarketplaceSource{URL: dir, Path: defaultManifestPath}

	// Act
	got, err := fetchLocal(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchLocal() returned error: %v", err)
	}
	if got.Name != "top-level" {
		t.Errorf("fetchLocal().Name = %q, want %q (earliest candidate must win)", got.Name, "top-level")
	}
}

// TestFetchLocal_NoManifestFound covers the miss case: none of the three
// candidate paths exist under the source directory.
func TestFetchLocal_NoManifestFound(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	src := &MarketplaceSource{URL: dir, Path: defaultManifestPath}

	// Act
	_, err := fetchLocal(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchLocal() returned no error, want one for a directory with no manifest")
	}
}

// TestFetchLocal_ExplicitNonDefaultPathSkipsFallback covers the case where
// Path is something other than the parser's default: only that exact path
// is tried, with no mkt-003 fallback probing.
func TestFetchLocal_ExplicitNonDefaultPathSkipsFallback(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeLocalManifest(t, dir, ".claude-plugin/marketplace.json", MarketplaceManifest{Name: "fallback-only"})
	src := &MarketplaceSource{URL: dir, Path: "custom/manifest.json"}

	// Act
	_, err := fetchLocal(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchLocal() returned no error, want one: an explicit non-default Path must not fall back to mkt-003's probe order")
	}
}

// TestFetchLocal_ProvenanceEmpty covers design.md's "SourceURL/SourceDigest
// empty for local" contract.
func TestFetchLocal_ProvenanceEmpty(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeLocalManifest(t, dir, "marketplace.json", MarketplaceManifest{Name: "acme"})
	src := &MarketplaceSource{URL: dir, Path: defaultManifestPath}

	// Act
	got, err := fetchLocal(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchLocal() returned error: %v", err)
	}
	if got.SourceURL != "" || got.SourceDigest != "" {
		t.Errorf("fetchLocal() SourceURL/SourceDigest = %q/%q, want both empty", got.SourceURL, got.SourceDigest)
	}
}

// TestFetchLocal_InvalidJSON covers a malformed manifest file.
func TestFetchLocal_InvalidJSON(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marketplace.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	src := &MarketplaceSource{URL: dir, Path: defaultManifestPath}

	// Act
	_, err := fetchLocal(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchLocal() returned no error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("fetchLocal() error = %v, want it to mention parsing", err)
	}
}

// TestFetchLocal_DirectFileSource covers mkt B5: when s.URL names a file
// directly (rather than a directory), fetchLocal reads that file as-is and
// never falls back to mkt-003's marketplace.json probing underneath it --
// even when a marketplace.json also happens to exist alongside it, and even
// when Path still carries the parser's default value.
func TestFetchLocal_DirectFileSource(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"Path left empty by the parser (mkt B5's normal case)", ""},
		{"Path still the parser's default is overridden by the file check", defaultManifestPath},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			dir := t.TempDir()
			manifestFile := filepath.Join(dir, "custom-manifest.json")
			want := MarketplaceManifest{Name: "direct-file"}
			data, err := json.Marshal(want)
			if err != nil {
				t.Fatalf("json.Marshal fixture manifest: %v", err)
			}
			if err := os.WriteFile(manifestFile, data, 0o644); err != nil {
				t.Fatalf("WriteFile(%q): %v", manifestFile, err)
			}
			// A marketplace.json sitting alongside it must not be probed --
			// if it were, the wrong manifest would win.
			writeLocalManifest(t, dir, "marketplace.json", MarketplaceManifest{Name: "wrong-if-probed"})
			src := &MarketplaceSource{URL: manifestFile, Path: tt.path}

			// Act
			got, err := fetchLocal(context.Background(), src)

			// Assert
			if err != nil {
				t.Fatalf("fetchLocal() returned error: %v", err)
			}
			if got.Name != want.Name {
				t.Errorf("fetchLocal().Name = %q, want %q (direct file must win over directory probing)", got.Name, want.Name)
			}
		})
	}
}

// TestFetchLocal_TolerantOfRegistryKey re-confirms mkt-005's tolerant
// parsing through the actual fetch path (models_test.go already covers the
// bare json.Unmarshal case).
func TestFetchLocal_TolerantOfRegistryKey(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	raw := `{"name": "acme", "plugins": [{"name": "p", "source": "./p", "registry": "custom"}]}`
	if err := os.WriteFile(filepath.Join(dir, "marketplace.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	src := &MarketplaceSource{URL: dir, Path: defaultManifestPath}

	// Act
	got, err := fetchLocal(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchLocal() returned error for a manifest with a 'registry' key: %v", err)
	}
	if len(got.Plugins) != 1 || got.Plugins[0].Registry != "custom" {
		t.Errorf("fetchLocal() Plugins = %+v, want one plugin with Registry=%q", got.Plugins, "custom")
	}
}
