package marketplace

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolvePluginSource_FiveShapes covers mkt-026's five plugin.Source
// mapping shapes: relative string, github dict, git-subdir dict, gitlab
// dict, and url dict -- each mapped to a canonical
// "owner/repo[/path][#ref]" dependency string.
func TestResolvePluginSource_FiveShapes(t *testing.T) {
	tests := []struct {
		name          string
		plugin        MarketplacePlugin
		mktOwner      string
		mktRepo       string
		wantCanonical string
	}{
		{
			name:          "relative string source with subdirectory",
			plugin:        MarketplacePlugin{Name: "p", Source: "plugins/foo"},
			mktOwner:      "acme",
			mktRepo:       "mkt-repo",
			wantCanonical: "acme/mkt-repo/plugins/foo",
		},
		{
			name:          "relative string source, leading ./ and trailing / stripped",
			plugin:        MarketplacePlugin{Name: "p", Source: "./plugins/foo/"},
			mktOwner:      "acme",
			mktRepo:       "mkt-repo",
			wantCanonical: "acme/mkt-repo/plugins/foo",
		},
		{
			name:          "relative string source at marketplace root",
			plugin:        MarketplacePlugin{Name: "p", Source: "."},
			mktOwner:      "acme",
			mktRepo:       "mkt-repo",
			wantCanonical: "acme/mkt-repo",
		},
		{
			name:          "relative string source empty -> marketplace root",
			plugin:        MarketplacePlugin{Name: "p", Source: ""},
			mktOwner:      "acme",
			mktRepo:       "mkt-repo",
			wantCanonical: "acme/mkt-repo",
		},
		{
			name: "github dict source with path and ref",
			plugin: MarketplacePlugin{Name: "p", Source: map[string]any{
				"type": "github", "repo": "owner/repo", "path": "sub/dir", "ref": "v1.0",
			}},
			wantCanonical: "owner/repo/sub/dir#v1.0",
		},
		{
			name: "github dict source, repo only",
			plugin: MarketplacePlugin{Name: "p", Source: map[string]any{
				"type": "github", "repo": "owner/repo",
			}},
			wantCanonical: "owner/repo",
		},
		{
			name: "github dict source via Copilot 'repository' alias",
			plugin: MarketplacePlugin{Name: "p", Source: map[string]any{
				"type": "github", "repository": "owner/repo",
			}},
			wantCanonical: "owner/repo",
		},
		{
			name: "git-subdir dict source with subdir and ref",
			plugin: MarketplacePlugin{Name: "p", Source: map[string]any{
				"type": "git-subdir", "repo": "owner/repo", "subdir": "pkg/a", "ref": "main",
			}},
			wantCanonical: "owner/repo/pkg/a#main",
		},
		{
			name: "git-subdir dict source, repo only",
			plugin: MarketplacePlugin{Name: "p", Source: map[string]any{
				"type": "git-subdir", "repo": "owner/repo",
			}},
			wantCanonical: "owner/repo",
		},
		{
			name: "gitlab dict source shares git-subdir mapping, uses 'path' key",
			plugin: MarketplacePlugin{Name: "p", Source: map[string]any{
				"type": "gitlab", "repo": "owner/repo", "path": "pkg/a",
			}},
			wantCanonical: "owner/repo/pkg/a",
		},
		{
			name: "url dict source resolves via manifest.ParseDepString, drops host",
			plugin: MarketplacePlugin{Name: "p", Source: map[string]any{
				"type": "url", "url": "https://github.com/owner/repo#v2.0",
			}},
			wantCanonical: "owner/repo#v2.0",
		},
		{
			name: "url dict source without ref",
			plugin: MarketplacePlugin{Name: "p", Source: map[string]any{
				"type": "url", "url": "https://gitlab.example.com/owner/repo",
			}},
			wantCanonical: "owner/repo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange -- tt.plugin, tt.mktOwner, tt.mktRepo

			// Act
			got, err := resolvePluginSource(&tt.plugin, tt.mktOwner, tt.mktRepo)

			// Assert
			if err != nil {
				t.Fatalf("resolvePluginSource() returned unexpected error: %v", err)
			}
			if got != tt.wantCanonical {
				t.Errorf("resolvePluginSource() = %q, want %q", got, tt.wantCanonical)
			}
		})
	}
}

// TestResolvePluginSource_Errors covers resolvePluginSource's error paths:
// missing/invalid coordinates, path traversal, and the un-inferrable dict
// type case.
func TestResolvePluginSource_Errors(t *testing.T) {
	tests := []struct {
		name       string
		plugin     MarketplacePlugin
		wantErrHas string
	}{
		{
			name:       "nil source",
			plugin:     MarketplacePlugin{Name: "p", Source: nil},
			wantErrHas: "no source defined",
		},
		{
			name:       "unrecognized source format (number)",
			plugin:     MarketplacePlugin{Name: "p", Source: 42.0},
			wantErrHas: "unrecognized source format",
		},
		{
			name: "github dict missing repo",
			plugin: MarketplacePlugin{Name: "p", Source: map[string]any{
				"type": "github",
			}},
			wantErrHas: "invalid github source",
		},
		{
			name: "github dict path traversal rejected",
			plugin: MarketplacePlugin{Name: "p", Source: map[string]any{
				"type": "github", "repo": "owner/repo", "path": "../escape",
			}},
			wantErrHas: "traversal",
		},
		{
			name: "git-subdir dict full URL in repo rejected",
			plugin: MarketplacePlugin{Name: "p", Source: map[string]any{
				"type": "git-subdir", "repo": "https://example.com/owner/repo",
			}},
			wantErrHas: "expected 'owner/repo' but got a URL",
		},
		{
			name: "git-subdir dict missing repo",
			plugin: MarketplacePlugin{Name: "p", Source: map[string]any{
				"type": "git-subdir",
			}},
			wantErrHas: "invalid git-subdir source",
		},
		{
			name: "url dict empty url field",
			plugin: MarketplacePlugin{Name: "p", Source: map[string]any{
				"type": "url", "url": "",
			}},
			wantErrHas: "empty 'url' field",
		},
		{
			name: "url dict resolves to a local path",
			plugin: MarketplacePlugin{Name: "p", Source: map[string]any{
				"type": "url", "url": "./local/path",
			}},
			wantErrHas: "resolves to a local path",
		},
		{
			name: "dict source with no type and no inferrable repo",
			plugin: MarketplacePlugin{Name: "p", Source: map[string]any{
				"package": "left-field",
			}},
			wantErrHas: "no inferrable 'repo' field",
		},
		{
			name: "dict source with unsupported type",
			plugin: MarketplacePlugin{Name: "p", Source: map[string]any{
				"type": "svn", "repo": "owner/repo",
			}},
			wantErrHas: "unsupported source type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange -- tt.plugin

			// Act
			got, err := resolvePluginSource(&tt.plugin, "mktOwner", "mktRepo")

			// Assert
			if err == nil {
				t.Fatalf("resolvePluginSource() = %q, nil; want an error containing %q", got, tt.wantErrHas)
			}
			if !strings.Contains(err.Error(), tt.wantErrHas) {
				t.Errorf("resolvePluginSource() error = %q; want it to contain %q", err.Error(), tt.wantErrHas)
			}
		})
	}
}

// TestResolvePluginSource_NPMRejected covers mkt-026's npm rejection at the
// resolve layer, for both the "type" key and the "kind" key variant --
// coercePluginType reads all three (type/source/kind) discriminator keys,
// unlike the manifest parse layer (models.go), which only reads
// type/source (see TestResolvePluginSource_NPMDualLayer for the two-layer
// regression case).
func TestResolvePluginSource_NPMRejected(t *testing.T) {
	tests := []struct {
		name   string
		source map[string]any
	}{
		{"type key", map[string]any{"type": "npm", "package": "left-pad"}},
		{"source key", map[string]any{"source": "npm", "package": "left-pad"}},
		{"kind key", map[string]any{"kind": "npm", "package": "left-pad"}},
		{"case-insensitive NPM", map[string]any{"type": "NPM", "package": "left-pad"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			plugin := MarketplacePlugin{Name: "npm-plugin", Source: tt.source}

			// Act
			got, err := resolvePluginSource(&plugin, "mktOwner", "mktRepo")

			// Assert
			if err == nil {
				t.Fatalf("resolvePluginSource() = %q, nil; want an npm-rejection error", got)
			}
			if !strings.Contains(err.Error(), "npm") {
				t.Errorf("resolvePluginSource() error = %q; want it to mention npm", err.Error())
			}
		})
	}
}

// TestResolvePluginSource_NPMDualLayer covers mkt-026's dual-layer npm
// behavior end to end: a "type: npm" plugin is dropped by the manifest
// parse layer (models.go) and never reaches resolution at all, while a
// "kind: npm" plugin survives manifest parsing (models.go's npm check only
// reads "type"/"source") and is rejected only here, at resolvePluginSource
// (which uses the three-key coercePluginType).
func TestResolvePluginSource_NPMDualLayer(t *testing.T) {
	doc := `{
		"name": "m",
		"plugins": [
			{"name": "npm-typed", "source": {"type": "npm", "package": "left-pad"}},
			{"name": "npm-kind-variant", "source": {"kind": "npm", "package": "left-pad"}},
			{"name": "ok", "source": "./ok"}
		]
	}`
	var m MarketplaceManifest
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	byName := map[string]MarketplacePlugin{}
	for _, p := range m.Plugins {
		byName[p.Name] = p
	}

	// Layer 1 (manifest parse): "type: npm" never survives -- a lookup by
	// name for it would report PluginNotFound in the full ResolvePlugin flow
	// (a later step); here we assert its absence from the parsed manifest.
	if _, ok := byName["npm-typed"]; ok {
		t.Error(`"npm-typed" plugin must be dropped at manifest parse time (mkt-026 layer 1), not survive to resolution`)
	}

	// Layer 2 (resolve): "kind: npm" survives manifest parsing and is only
	// rejected here.
	kindVariant, ok := byName["npm-kind-variant"]
	if !ok {
		t.Fatal(`"npm-kind-variant" plugin must survive manifest parsing (mkt-026 layer 1 only checks type/source, not kind)`)
	}
	if _, err := resolvePluginSource(&kindVariant, "mktOwner", "mktRepo"); err == nil {
		t.Error(`resolvePluginSource() on "kind: npm" plugin returned no error; want npm rejection at the resolve layer (mkt-026 layer 2)`)
	} else if !strings.Contains(err.Error(), "npm") {
		t.Errorf("resolvePluginSource() error = %q; want it to mention npm", err.Error())
	}
}

// TestResolveLocalRelativeSource covers mkt-025's local-marketplace fast
// path: a purely relative-path plugin source inside a Kind() == KindLocal
// marketplace resolves directly to an absolute local filesystem path,
// without ever round-tripping through a dependency string.
func TestResolveLocalRelativeSource(t *testing.T) {
	root := t.TempDir()
	mkt := &MarketplaceSource{Name: "local-mkt", URL: root}
	if mkt.Kind() != KindLocal {
		t.Fatalf("test setup: MarketplaceSource{URL: %q}.Kind() = %v, want KindLocal", root, mkt.Kind())
	}

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{"relative subdirectory", "plugins/foo", filepath.Join(root, "plugins", "foo")},
		{"leading ./ stripped", "./plugins/foo", filepath.Join(root, "plugins", "foo")},
		{"marketplace root ('.')", ".", root},
		{"marketplace root (empty string)", "", root},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange -- mkt, tt.source

			// Act
			got, err := resolveLocalRelativeSource(tt.source, mkt)

			// Assert
			if err != nil {
				t.Fatalf("resolveLocalRelativeSource(%q) returned unexpected error: %v", tt.source, err)
			}
			if got != tt.want {
				t.Errorf("resolveLocalRelativeSource(%q) = %q, want %q", tt.source, got, tt.want)
			}
		})
	}
}

// TestResolveLocalRelativeSource_Errors covers the fast path's error cases:
// a marketplace with no resolvable filesystem path, and a traversal
// attempt in the relative source.
func TestResolveLocalRelativeSource_Errors(t *testing.T) {
	tests := []struct {
		name       string
		mkt        *MarketplaceSource
		source     string
		wantErrHas string
	}{
		{
			name:       "marketplace has no resolvable filesystem path",
			mkt:        &MarketplaceSource{Name: "no-path"},
			source:     "plugins/foo",
			wantErrHas: "no resolvable filesystem path",
		},
		{
			name:       "traversal segment rejected",
			mkt:        &MarketplaceSource{Name: "local-mkt", URL: t.TempDir()},
			source:     "../escape",
			wantErrHas: "traversal",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange -- tt.mkt, tt.source

			// Act
			got, err := resolveLocalRelativeSource(tt.source, tt.mkt)

			// Assert
			if err == nil {
				t.Fatalf("resolveLocalRelativeSource(%q) = %q, nil; want an error containing %q", tt.source, got, tt.wantErrHas)
			}
			if !strings.Contains(err.Error(), tt.wantErrHas) {
				t.Errorf("resolveLocalRelativeSource(%q) error = %q; want it to contain %q", tt.source, err.Error(), tt.wantErrHas)
			}
		})
	}
}

// TestCoercePluginType covers mkt-026's three-key discriminator lookup
// (type -> source -> kind) plus the repo/subdir-based inference fallback
// (design.md gaps A4).
func TestCoercePluginType(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]any
		want string
	}{
		{"type key wins", map[string]any{"type": "github", "source": "gitlab", "kind": "git"}, "github"},
		{"falls back to source when type absent", map[string]any{"source": "gitlab"}, "gitlab"},
		{"falls back to kind when type and source absent", map[string]any{"kind": "git-subdir"}, "git-subdir"},
		{"lower-cases and trims", map[string]any{"type": "  GitHub  "}, "github"},
		{"infers github from repo when no discriminator key", map[string]any{"repo": "owner/repo"}, "github"},
		{"infers git-subdir from repo+subdir when no discriminator key", map[string]any{"repo": "owner/repo", "subdir": "pkg/a"}, "git-subdir"},
		{"no usable repo -> empty", map[string]any{"repo": "not-a-slug"}, ""},
		{"nothing at all -> empty", map[string]any{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange -- tt.in

			// Act
			got := coercePluginType(tt.in)

			// Assert
			if got != tt.want {
				t.Errorf("coercePluginType(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
