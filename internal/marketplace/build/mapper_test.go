package build

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/marketplace/authoring"
)

// ── top-level fields ──────────────────────────────────────────────────────

func TestClaudeMapper_TopLevel_NameAlwaysPresent(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "my-marketplace"}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, nil)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	if doc.Name != "my-marketplace" {
		t.Errorf("Name = %q, want my-marketplace", doc.Name)
	}
}

func TestClaudeMapper_TopLevel_DescriptionVersionConditionalEmission(t *testing.T) {
	tests := []struct {
		name                  string
		description           string
		descriptionOverridden bool
		version               string
		versionOverridden     bool
		wantDescription       string
		wantVersion           string
	}{
		{"overridden and non-empty: emitted", "curated desc", true, "2.0.0", true, "curated desc", "2.0.0"},
		{"not overridden: omitted even though non-empty", "inherited desc", false, "1.0.0", false, "", ""},
		{"overridden but empty: omitted", "", true, "", true, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			cfg := &authoring.AuthoringConfig{
				Name:                  "m",
				Description:           tt.description,
				DescriptionOverridden: tt.descriptionOverridden,
				Version:               tt.version,
				VersionOverridden:     tt.versionOverridden,
			}

			// Act
			doc, _, err := ClaudeMapper{}.Compose(cfg, nil)

			// Assert
			if err != nil {
				t.Fatalf("Compose() error = %v", err)
			}
			if doc.Description != tt.wantDescription {
				t.Errorf("Description = %q, want %q", doc.Description, tt.wantDescription)
			}
			if doc.Version != tt.wantVersion {
				t.Errorf("Version = %q, want %q", doc.Version, tt.wantVersion)
			}
		})
	}
}

func TestClaudeMapper_TopLevel_OwnerEmailAndURLConditional(t *testing.T) {
	tests := []struct {
		name      string
		owner     authoring.Owner
		wantEmail string
		wantURL   string
	}{
		{"name only", authoring.Owner{Name: "Acme"}, "", ""},
		{"name + email", authoring.Owner{Name: "Acme", Email: "acme@example.com"}, "acme@example.com", ""},
		{"name + url", authoring.Owner{Name: "Acme", URL: "https://acme.example.com"}, "", "https://acme.example.com"},
		{"all three", authoring.Owner{Name: "Acme", Email: "a@b.com", URL: "https://acme.example.com"}, "a@b.com", "https://acme.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			cfg := &authoring.AuthoringConfig{Name: "m", Owner: tt.owner}

			// Act
			doc, _, err := ClaudeMapper{}.Compose(cfg, nil)

			// Assert
			if err != nil {
				t.Fatalf("Compose() error = %v", err)
			}
			if doc.Owner.Name != tt.owner.Name {
				t.Errorf("Owner.Name = %q, want %q", doc.Owner.Name, tt.owner.Name)
			}
			if doc.Owner.Email != tt.wantEmail {
				t.Errorf("Owner.Email = %q, want %q", doc.Owner.Email, tt.wantEmail)
			}
			if doc.Owner.URL != tt.wantURL {
				t.Errorf("Owner.URL = %q, want %q", doc.Owner.URL, tt.wantURL)
			}
		})
	}
}

func TestClaudeMapper_TopLevel_MetadataPassthroughWhenNonEmpty(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{
		Name:     "m",
		Metadata: map[string]any{"pluginRoot": "./packages", "customKey": "customValue"},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, nil)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	if doc.Metadata["customKey"] != "customValue" {
		t.Errorf("Metadata[customKey] = %v, want customValue", doc.Metadata["customKey"])
	}
}

func TestClaudeMapper_TopLevel_MetadataOmittedWhenEmpty(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m"}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, nil)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	if len(doc.Metadata) != 0 {
		t.Errorf("Metadata = %v, want empty", doc.Metadata)
	}
}

// ── plugin: local package ────────────────────────────────────────────────

func TestClaudeMapper_Plugin_LocalPackage_SourceIsPlainString(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{
			Entry:   authoring.PackageEntry{Name: "local-tool", Source: "./pkgs/tool-a", Description: "a local tool", Version: "0.1.0"},
			IsLocal: true,
			Subdir:  "./pkgs/tool-a",
		},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	plugin := doc.Plugins[0]
	if plugin.Name != "local-tool" {
		t.Errorf("Name = %q, want local-tool", plugin.Name)
	}
	source, ok := plugin.Source.(string)
	if !ok || source != "./pkgs/tool-a" {
		t.Errorf("Source = %#v, want plain string ./pkgs/tool-a", plugin.Source)
	}
	if plugin.Description != "a local tool" || plugin.Version != "0.1.0" {
		t.Errorf("Description/Version = %q/%q, want a local tool/0.1.0", plugin.Description, plugin.Version)
	}
}

// ── plugin: remote source composition (mkt-050 修訂版's four rules) ──────

func TestClaudeMapper_Plugin_RemotePackage_GithubShorthand(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{
			Entry:      authoring.PackageEntry{Name: "tool", Source: "owner/repo"},
			SourceRepo: "owner/repo",
			Ref:        "v1.0.0",
			SHA:        strings.Repeat("a", 40),
		},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	src, ok := doc.Plugins[0].Source.(*RemoteSource)
	if !ok {
		t.Fatalf("Source = %#v, want *RemoteSource", doc.Plugins[0].Source)
	}
	if src.Source != "github" || src.Repo != "owner/repo" {
		t.Errorf("source/repo = %q/%q, want github/owner/repo", src.Source, src.Repo)
	}
	if src.URL != "" || src.Path != "" {
		t.Errorf("URL/Path = %q/%q, want both empty for github shorthand", src.URL, src.Path)
	}
	if src.Ref != "v1.0.0" || src.SHA != strings.Repeat("a", 40) {
		t.Errorf("Ref/SHA = %q/%q, unexpected", src.Ref, src.SHA)
	}
}

func TestClaudeMapper_Plugin_RemotePackage_Subdir_GitSubdirShape(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{
			Entry:      authoring.PackageEntry{Name: "tool", Source: "owner/repo"},
			SourceRepo: "owner/repo",
			Subdir:     "packages/tool",
			Ref:        "v1.0.0",
			SHA:        strings.Repeat("b", 40),
		},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	src, ok := doc.Plugins[0].Source.(*RemoteSource)
	if !ok {
		t.Fatalf("Source = %#v, want *RemoteSource", doc.Plugins[0].Source)
	}
	if src.Source != "git-subdir" {
		t.Errorf("source = %q, want git-subdir", src.Source)
	}
	if src.URL != "owner/repo" {
		t.Errorf("url = %q, want owner/repo (no host override)", src.URL)
	}
	if src.Path != "packages/tool" {
		t.Errorf("path = %q, want packages/tool", src.Path)
	}
	if src.Repo != "" {
		t.Errorf("repo = %q, want empty (git-subdir never sets repo)", src.Repo)
	}
}

func TestClaudeMapper_Plugin_RemotePackage_HostPrefixed_URLShape(t *testing.T) {
	// Arrange: a GHE host must emit a real https:// URL, never the github
	// shorthand (design.md: "github shorthand 只解析到 github.com").
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{
			Entry:      authoring.PackageEntry{Name: "tool", Source: "ghe.example.com/owner/repo"},
			Host:       "ghe.example.com",
			SourceRepo: "owner/repo",
			Ref:        "v1.0.0",
			SHA:        strings.Repeat("c", 40),
		},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	src, ok := doc.Plugins[0].Source.(*RemoteSource)
	if !ok {
		t.Fatalf("Source = %#v, want *RemoteSource", doc.Plugins[0].Source)
	}
	if src.Source != "url" {
		t.Errorf("source = %q, want url", src.Source)
	}
	if src.URL != "https://ghe.example.com/owner/repo" {
		t.Errorf("url = %q, want https://ghe.example.com/owner/repo", src.URL)
	}
	if src.Repo != "" {
		t.Errorf("repo = %q, want empty for url-shaped source", src.Repo)
	}
}

func TestClaudeMapper_Plugin_RemotePackage_HostPrefixedSubdir_GitSubdirUsesHostURL(t *testing.T) {
	// Arrange: host-prefixed AND subdir -- subdir rule wins (rule order 2
	// before rule 3), but the URL must still reflect the non-default host.
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{
			Entry:      authoring.PackageEntry{Name: "tool", Source: "ghe.example.com/owner/repo"},
			Host:       "ghe.example.com",
			SourceRepo: "owner/repo",
			Subdir:     "sub/dir",
			Ref:        "v1.0.0",
		},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	src, ok := doc.Plugins[0].Source.(*RemoteSource)
	if !ok {
		t.Fatalf("Source = %#v, want *RemoteSource", doc.Plugins[0].Source)
	}
	if src.Source != "git-subdir" || src.URL != "https://ghe.example.com/owner/repo" || src.Path != "sub/dir" {
		t.Errorf("source/url/path = %q/%q/%q, unexpected", src.Source, src.URL, src.Path)
	}
}

func TestClaudeMapper_Plugin_RemotePackage_RefOrSHAOmittedWhenEmpty(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{Entry: authoring.PackageEntry{Name: "tool", Source: "owner/repo"}, SourceRepo: "owner/repo"},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	src := doc.Plugins[0].Source.(*RemoteSource)
	if src.Ref != "" || src.SHA != "" {
		t.Errorf("Ref/SHA = %q/%q, want both empty", src.Ref, src.SHA)
	}
}

// ── plugin: description/version enrichment threading ────────────────────

func TestClaudeMapper_Plugin_RemotePackage_UsesAlreadyEnrichedRemoteFields(t *testing.T) {
	// Arrange: RemoteDescription/RemoteVersion are already the final,
	// curator-wins-resolved values (metadata.go's enrichRemoteMetadata,
	// this sub-task's step 2) -- the mapper must use them as-is, with no
	// extra precedence logic of its own.
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{
			Entry:             authoring.PackageEntry{Name: "tool", Source: "owner/repo", Description: "curator wins", Version: "^1.0.0"},
			SourceRepo:        "owner/repo",
			RemoteDescription: "curator wins",
			RemoteVersion:     "1.2.3",
		},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	plugin := doc.Plugins[0]
	if plugin.Description != "curator wins" || plugin.Version != "1.2.3" {
		t.Errorf("Description/Version = %q/%q, want curator wins/1.2.3", plugin.Description, plugin.Version)
	}
}

func TestClaudeMapper_Plugin_SemverRangeNeverLeaksIntoOutputVersion(t *testing.T) {
	// Arrange: entry.Version is a semver RANGE; RemoteVersion is empty
	// (metadata fetch found/kept nothing usable) -- the range itself must
	// never appear as the output plugin's "version".
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{
			Entry:      authoring.PackageEntry{Name: "tool", Source: "owner/repo", Version: "^1.0.0"},
			SourceRepo: "owner/repo",
			// RemoteVersion deliberately left "" -- see metadata.go's
			// isDisplayVersion, which already filtered this range out.
		},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	if doc.Plugins[0].Version != "" {
		t.Errorf("Version = %q, want empty (a semver range must never leak into output)", doc.Plugins[0].Version)
	}
}

func TestClaudeMapper_Plugin_LocalPackage_UsesLocalApmYMLMetadataWhenCuratorOmits(t *testing.T) {
	// Arrange: F1 fix -- a local package with no curator-declared
	// description/version is enriched from its own apm.yml on disk
	// (builder.go's enrichLocalMetadata); RemoteDescription/RemoteVersion
	// already carry that resolved value by the time Compose sees them (this
	// test previously locked in the pre-fix "always omitted" behavior).
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{
			Entry:             authoring.PackageEntry{Name: "local-tool", Source: "./pkgs/a"},
			IsLocal:           true,
			Subdir:            "./pkgs/a",
			RemoteDescription: "from local apm.yml",
			RemoteVersion:     "2.0.0",
		},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	if doc.Plugins[0].Description != "from local apm.yml" || doc.Plugins[0].Version != "2.0.0" {
		t.Errorf("Description/Version = %q/%q, want from local apm.yml/2.0.0", doc.Plugins[0].Description, doc.Plugins[0].Version)
	}
}

func TestClaudeMapper_Plugin_LocalPackage_MissingDescriptionVersion_Omitted(t *testing.T) {
	// Arrange: no curator value AND no local apm.yml metadata at all --
	// still omitted, not defaulted to some placeholder.
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{Entry: authoring.PackageEntry{Name: "local-tool", Source: "./pkgs/a"}, IsLocal: true, Subdir: "./pkgs/a"},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	if doc.Plugins[0].Description != "" || doc.Plugins[0].Version != "" {
		t.Errorf("Description/Version = %q/%q, want both empty", doc.Plugins[0].Description, doc.Plugins[0].Version)
	}
}

func TestClaudeMapper_Plugin_LocalPackage_CuratorDescriptionWinsOverLocalApmYML(t *testing.T) {
	// Arrange: curator-wins precedence -- entry.Description/.Version take
	// priority over the local apm.yml value even when both are present.
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{
			Entry:             authoring.PackageEntry{Name: "local-tool", Source: "./pkgs/a", Description: "curator description", Version: "1.0.0"},
			IsLocal:           true,
			Subdir:            "./pkgs/a",
			RemoteDescription: "from local apm.yml",
			RemoteVersion:     "9.9.9",
		},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	if doc.Plugins[0].Description != "curator description" || doc.Plugins[0].Version != "1.0.0" {
		t.Errorf("Description/Version = %q/%q, want curator description/1.0.0", doc.Plugins[0].Description, doc.Plugins[0].Version)
	}
}

// ── plugin: author/license/repository/tags/homepage conditional emission ──

func TestClaudeMapper_Plugin_AuthorLicenseRepositoryTags_EmittedWhenPresent(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{
			Entry: authoring.PackageEntry{
				Name: "tool", Source: "owner/repo",
				Author:     map[string]string{"name": "Jane Doe"},
				License:    "MIT",
				Repository: "https://github.com/owner/repo",
			},
			SourceRepo: "owner/repo",
			Tags:       []string{"alpha", "beta"},
		},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	plugin := doc.Plugins[0]
	if plugin.Author["name"] != "Jane Doe" {
		t.Errorf("Author = %+v, want {name: Jane Doe}", plugin.Author)
	}
	if plugin.License != "MIT" {
		t.Errorf("License = %q, want MIT", plugin.License)
	}
	if plugin.Repository != "https://github.com/owner/repo" {
		t.Errorf("Repository = %q, unexpected", plugin.Repository)
	}
	if len(plugin.Tags) != 2 || plugin.Tags[0] != "alpha" || plugin.Tags[1] != "beta" {
		t.Errorf("Tags = %v, want [alpha beta]", plugin.Tags)
	}
}

func TestClaudeMapper_Plugin_AuthorLicenseRepositoryTags_OmittedWhenAbsent(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{Entry: authoring.PackageEntry{Name: "tool", Source: "owner/repo"}, SourceRepo: "owner/repo"},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	plugin := doc.Plugins[0]
	if plugin.Author != nil || plugin.License != "" || plugin.Repository != "" || len(plugin.Tags) != 0 {
		t.Errorf("plugin = %+v, want author/license/repository/tags all absent", plugin)
	}
}

func TestClaudeMapper_Plugin_Homepage_OnlyEmittedForLocalPackage(t *testing.T) {
	// Arrange: a remote package's `homepage:` must never be emitted
	// (design.md: "homepage | 僅本地套件且 curator 條目有才出").
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{
			Entry:   authoring.PackageEntry{Name: "local-tool", Source: "./pkgs/a", Homepage: "https://example.com/local"},
			IsLocal: true,
			Subdir:  "./pkgs/a",
		},
		{
			Entry:      authoring.PackageEntry{Name: "remote-tool", Source: "owner/repo", Homepage: "https://example.com/remote"},
			SourceRepo: "owner/repo",
		},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	if doc.Plugins[0].Homepage != "https://example.com/local" {
		t.Errorf("local package Homepage = %q, want https://example.com/local", doc.Plugins[0].Homepage)
	}
	if doc.Plugins[1].Homepage != "" {
		t.Errorf("remote package Homepage = %q, want empty (never emitted for remote)", doc.Plugins[1].Homepage)
	}
}

// ── no APM-only / Codex-only fields leak into the Claude JSON output ─────

func TestClaudeMapper_Output_NoCategoryOrAPMFieldsInJSON(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{
		Name:  "m",
		Build: authoring.Build{TagPattern: "v{version}"},
	}
	resolved := []ResolvedPackage{
		{
			Entry: authoring.PackageEntry{
				Name: "tool", Source: "owner/repo", Category: "Productivity",
				TagPattern: "v{version}", IncludePrerelease: true,
			},
			SourceRepo: "owner/repo",
			Ref:        "v1.0.0",
			SHA:        strings.Repeat("d", 40),
		},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Assert
	for _, forbidden := range []string{"category", "tagPattern", "tag_pattern", "include_prerelease", "\"build\""} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("output JSON contains forbidden APM/Codex-only field %q: %s", forbidden, raw)
		}
	}
}

// ── duplicate package names ────────────────────────────────────────────────

func TestClaudeMapper_DuplicateNameWarning(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{Entry: authoring.PackageEntry{Name: "tool", Source: "owner/repo-a"}, SourceRepo: "owner/repo-a"},
		{Entry: authoring.PackageEntry{Name: "tool", Source: "owner/repo-b"}, SourceRepo: "owner/repo-b"},
	}

	// Act
	_, warnings, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one duplicate-name warning", warnings)
	}
	if !strings.Contains(warnings[0], "tool") {
		t.Errorf("warning = %q, want it to name the duplicated package", warnings[0])
	}
}

func TestClaudeMapper_NoDuplicateNames_NoWarning(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{Entry: authoring.PackageEntry{Name: "tool-a", Source: "owner/repo-a"}, SourceRepo: "owner/repo-a"},
		{Entry: authoring.PackageEntry{Name: "tool-b", Source: "owner/repo-b"}, SourceRepo: "owner/repo-b"},
	}

	// Act
	_, warnings, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
}

// ── pluginRoot stripping (mkt-050 修訂版, output_mappers.py:150-178) ──────

func TestClaudeMapper_PluginRoot_StripsLocalSourceRelativeToRoot(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m", Metadata: map[string]any{"pluginRoot": "./packages"}}
	resolved := []ResolvedPackage{
		{Entry: authoring.PackageEntry{Name: "tool-a", Source: "./packages/tool-a"}, IsLocal: true, Subdir: "./packages/tool-a"},
	}

	// Act
	doc, warnings, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	source, ok := doc.Plugins[0].Source.(string)
	if !ok || source != "./tool-a" {
		t.Errorf("Source = %#v, want ./tool-a", doc.Plugins[0].Source)
	}
}

func TestClaudeMapper_PluginRoot_SourceOutsideRoot_WarningKeepsOriginal(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m", Metadata: map[string]any{"pluginRoot": "./packages"}}
	resolved := []ResolvedPackage{
		{Entry: authoring.PackageEntry{Name: "tool-a", Source: "./other/tool-a"}, IsLocal: true, Subdir: "./other/tool-a"},
	}

	// Act
	doc, warnings, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v, want the build to succeed with a warning instead", err)
	}
	source, ok := doc.Plugins[0].Source.(string)
	if !ok || source != "./other/tool-a" {
		t.Errorf("Source = %#v, want the original source emitted as-is", doc.Plugins[0].Source)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one 'outside pluginRoot' warning", warnings)
	}
	if !strings.Contains(warnings[0], "tool-a") || !strings.Contains(warnings[0], "pluginRoot") {
		t.Errorf("warning = %q, want it to mention the package and pluginRoot", warnings[0])
	}
}

func TestClaudeMapper_PluginRoot_EqualPaths_HardBuildErrorAbortsCompose(t *testing.T) {
	// Arrange: source == pluginRoot -- _subtract_plugin_root's "empty
	// result" BuildError, which (unlike the "outside root" ValueError) is
	// NOT downgraded to a warning; it must abort the whole build.
	cfg := &authoring.AuthoringConfig{Name: "m", Metadata: map[string]any{"pluginRoot": "./packages"}}
	resolved := []ResolvedPackage{
		{Entry: authoring.PackageEntry{Name: "tool-a", Source: "./packages"}, IsLocal: true, Subdir: "./packages"},
	}

	// Act
	_, _, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	var prErr *PluginRootError
	if !errors.As(err, &prErr) {
		t.Fatalf("error = %v, want a *PluginRootError", err)
	}
}

func TestClaudeMapper_PluginRoot_NotSet_LocalSourceEmittedUnchanged(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{Entry: authoring.PackageEntry{Name: "tool-a", Source: "./packages/tool-a"}, IsLocal: true, Subdir: "./packages/tool-a"},
	}

	// Act
	doc, warnings, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	source, ok := doc.Plugins[0].Source.(string)
	if !ok || source != "./packages/tool-a" {
		t.Errorf("Source = %#v, want the original source unchanged", doc.Plugins[0].Source)
	}
}

// ── subtractPluginRoot / validatePluginRootResult: direct unit tests ─────
// (the three hard-error boundaries + the "outside root" distinction)

func TestValidatePluginRootResult_EmptyResult_ReturnsError(t *testing.T) {
	// Act
	_, err := validatePluginRootResult("./packages", "./packages", "")

	// Assert
	var prErr *PluginRootError
	if !errors.As(err, &prErr) {
		t.Fatalf("error = %v, want a *PluginRootError", err)
	}
}

func TestValidatePluginRootResult_DotResult_ReturnsError(t *testing.T) {
	// Act
	_, err := validatePluginRootResult("./packages", "./packages", ".")

	// Assert
	var prErr *PluginRootError
	if !errors.As(err, &prErr) {
		t.Fatalf("error = %v, want a *PluginRootError", err)
	}
}

func TestValidatePluginRootResult_AbsoluteResult_ReturnsError(t *testing.T) {
	// Act
	_, err := validatePluginRootResult("./packages/tool-a", "./packages", "/etc/passwd")

	// Assert
	var prErr *PluginRootError
	if !errors.As(err, &prErr) {
		t.Fatalf("error = %v, want a *PluginRootError", err)
	}
}

func TestValidatePluginRootResult_TraversalResult_ReturnsError(t *testing.T) {
	// Act
	_, err := validatePluginRootResult("./packages/tool-a", "./packages", "../evil")

	// Assert
	var prErr *PluginRootError
	if !errors.As(err, &prErr) {
		t.Fatalf("error = %v, want a *PluginRootError", err)
	}
}

func TestValidatePluginRootResult_ValidResult_ReturnsDotSlashPrefixed(t *testing.T) {
	// Act
	got, err := validatePluginRootResult("./packages/tool-a", "./packages", "tool-a")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "./tool-a" {
		t.Errorf("got %q, want ./tool-a", got)
	}
}

func TestSubtractPluginRoot_SourceOutsideRoot_ReturnsSentinelError(t *testing.T) {
	// Act
	_, err := subtractPluginRoot("./other/tool-a", "./packages")

	// Assert
	if !errors.Is(err, errSourceOutsidePluginRoot) {
		t.Fatalf("error = %v, want errSourceOutsidePluginRoot", err)
	}
}

func TestSubtractPluginRoot_StripsPrefix(t *testing.T) {
	// Act
	got, err := subtractPluginRoot("./packages/nested/tool-a", "./packages")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "./nested/tool-a" {
		t.Errorf("got %q, want ./nested/tool-a", got)
	}
}
