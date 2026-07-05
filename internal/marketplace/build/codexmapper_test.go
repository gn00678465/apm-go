package build

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/marketplace/authoring"
)

// ── top-level fields (mkt-052: Codex's shape is materially different from
// Claude's -- name + interface.displayName + plugins only) ────────────────

func TestCodexMapper_TopLevel_NameAndInterfaceDisplayName(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "my-marketplace"}

	// Act
	doc, _, err := CodexMapper{}.Compose(cfg, nil)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	if doc.Name != "my-marketplace" {
		t.Errorf("Name = %q, want my-marketplace", doc.Name)
	}
	if doc.Interface.DisplayName != "my-marketplace" {
		t.Errorf("Interface.DisplayName = %q, want my-marketplace", doc.Interface.DisplayName)
	}
}

func TestCodexMapper_TopLevel_NoDescriptionVersionOwnerMetadataInJSON(t *testing.T) {
	// Arrange: description/version/owner/metadata are all present on cfg, but
	// Codex's top level never emits them (design.md: "頂層... 無
	// description/version/owner/metadata").
	cfg := &authoring.AuthoringConfig{
		Name:                  "m",
		Description:           "a description",
		DescriptionOverridden: true,
		Version:               "1.0.0",
		VersionOverridden:     true,
		Owner:                 authoring.Owner{Name: "Acme", Email: "acme@example.com"},
		Metadata:              map[string]any{"pluginRoot": "./packages"},
	}

	// Act
	doc, _, err := CodexMapper{}.Compose(cfg, nil)
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Assert
	for _, forbidden := range []string{"description", "version", "owner", "metadata", "pluginRoot"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("output JSON contains forbidden top-level field %q: %s", forbidden, raw)
		}
	}
}

// ── plugin: fixed policy values ─────────────────────────────────────────

func TestCodexMapper_Plugin_PolicyIsAlwaysFixed(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{Entry: authoring.PackageEntry{Name: "tool", Source: "owner/repo", Category: "utilities"}, SourceRepo: "owner/repo"},
	}

	// Act
	doc, _, err := CodexMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	policy := doc.Plugins[0].Policy
	if policy.Installation != "AVAILABLE" || policy.Authentication != "ON_INSTALL" {
		t.Errorf("Policy = %+v, want {AVAILABLE ON_INSTALL}", policy)
	}
}

// ── plugin: category required (mkt-053's mapper-layer defensive gate) ────

func TestCodexMapper_Plugin_CategoryEmittedWhenPresent(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{Entry: authoring.PackageEntry{Name: "tool", Source: "owner/repo", Category: "productivity"}, SourceRepo: "owner/repo"},
	}

	// Act
	doc, _, err := CodexMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	if doc.Plugins[0].Category != "productivity" {
		t.Errorf("Category = %q, want productivity", doc.Plugins[0].Category)
	}
}

func TestCodexMapper_Plugin_MissingCategory_ReturnsCategoryRequiredError(t *testing.T) {
	// Arrange: the config-loading layer (schema.go) should already catch
	// this earlier -- this is the mapper's own defensive double-check
	// (design.md's "雙重把關").
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{Entry: authoring.PackageEntry{Name: "tool", Source: "owner/repo"}, SourceRepo: "owner/repo"},
	}

	// Act
	_, _, err := CodexMapper{}.Compose(cfg, resolved)

	// Assert
	var catErr *CategoryRequiredError
	if !errors.As(err, &catErr) {
		t.Fatalf("error = %v, want a *CategoryRequiredError", err)
	}
	if catErr.Package != "tool" {
		t.Errorf("CategoryRequiredError.Package = %q, want tool", catErr.Package)
	}
	if !strings.Contains(err.Error(), "tool") {
		t.Errorf("error message %q must name the package", err.Error())
	}
}

// ── plugin: no Claude-only fields leak into the Codex JSON output ───────

func TestCodexMapper_Output_NoClaudeOnlyFieldsInJSON(t *testing.T) {
	// Arrange: description/version/author/license/repository/tags/homepage
	// are all present on the entry, but Codex's plugin level never emits
	// them (design.md: "無 description/version/author/license/repository/
	// tags/homepage").
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{
			Entry: authoring.PackageEntry{
				Name: "local-tool", Source: "./pkgs/a", Category: "utilities",
				Description: "a tool", Version: "1.0.0",
				Author:     map[string]string{"name": "Jane Doe"},
				License:    "MIT",
				Repository: "https://github.com/owner/repo",
				Homepage:   "https://example.com",
			},
			IsLocal: true,
			Subdir:  "./pkgs/a",
			Tags:    []string{"alpha", "beta"},
		},
	}

	// Act
	doc, _, err := CodexMapper{}.Compose(cfg, resolved)
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Assert
	for _, forbidden := range []string{
		"description", "\"version\"", "author", "license", "repository", "tags", "homepage", "Jane Doe", "MIT",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("output JSON contains forbidden Claude-only field %q: %s", forbidden, raw)
		}
	}
}

// ── plugin: source composition (mkt-052's Codex-specific shapes) ────────

func TestCodexMapper_Plugin_LocalPackage_SourceIsDictNotString(t *testing.T) {
	// Arrange: Codex's local source is a DICT, unlike Claude's plain string
	// (design.md: "本地 source 是 dict {source:local,path}").
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{
			Entry:   authoring.PackageEntry{Name: "local-tool", Source: "./pkgs/tool-a", Category: "utilities"},
			IsLocal: true,
			Subdir:  "./pkgs/tool-a",
		},
	}

	// Act
	doc, _, err := CodexMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	src, ok := doc.Plugins[0].Source.(*CodexLocalSource)
	if !ok {
		t.Fatalf("Source = %#v, want *CodexLocalSource", doc.Plugins[0].Source)
	}
	if src.Source != "local" || src.Path != "./pkgs/tool-a" {
		t.Errorf("Source/Path = %q/%q, want local/./pkgs/tool-a", src.Source, src.Path)
	}
}

func TestCodexMapper_Plugin_RemotePackage_DefaultHost_NoGithubShorthand(t *testing.T) {
	// Arrange: unlike Claude (which falls back to a "github" shorthand),
	// Codex ALWAYS uses "url" for a remote, non-subdir package -- even one on
	// the default host (design.md: "遠端無 github shorthand(一律
	// url/git-subdir)").
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{
			Entry:      authoring.PackageEntry{Name: "tool", Source: "owner/repo", Category: "utilities"},
			SourceRepo: "owner/repo",
			Ref:        "v1.0.0",
			SHA:        strings.Repeat("a", 40),
		},
	}

	// Act
	doc, _, err := CodexMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	src, ok := doc.Plugins[0].Source.(*RemoteSource)
	if !ok {
		t.Fatalf("Source = %#v, want *RemoteSource", doc.Plugins[0].Source)
	}
	if src.Source != "url" {
		t.Errorf("source = %q, want url (no github shorthand for Codex)", src.Source)
	}
	if src.Repo != "" {
		t.Errorf("repo = %q, want empty (Codex never emits a github-shorthand repo key)", src.Repo)
	}
	if src.URL != "owner/repo" {
		t.Errorf("url = %q, want owner/repo (bare fallback when no host override)", src.URL)
	}
	if src.Ref != "v1.0.0" || src.SHA != strings.Repeat("a", 40) {
		t.Errorf("Ref/SHA = %q/%q, unexpected", src.Ref, src.SHA)
	}
}

func TestCodexMapper_Plugin_RemotePackage_HostPrefixed_URLShape(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{
			Entry:      authoring.PackageEntry{Name: "tool", Source: "ghe.example.com/owner/repo", Category: "utilities"},
			Host:       "ghe.example.com",
			SourceRepo: "owner/repo",
			Ref:        "v1.0.0",
		},
	}

	// Act
	doc, _, err := CodexMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	src, ok := doc.Plugins[0].Source.(*RemoteSource)
	if !ok {
		t.Fatalf("Source = %#v, want *RemoteSource", doc.Plugins[0].Source)
	}
	if src.Source != "url" || src.URL != "https://ghe.example.com/owner/repo" {
		t.Errorf("source/url = %q/%q, want url/https://ghe.example.com/owner/repo", src.Source, src.URL)
	}
}

func TestCodexMapper_Plugin_RemotePackage_Subdir_GitSubdirShape(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{
			Entry:      authoring.PackageEntry{Name: "tool", Source: "owner/repo", Category: "utilities"},
			SourceRepo: "owner/repo",
			Subdir:     "packages/tool",
			Ref:        "v1.0.0",
			SHA:        strings.Repeat("b", 40),
		},
	}

	// Act
	doc, _, err := CodexMapper{}.Compose(cfg, resolved)

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
	if src.Ref != "v1.0.0" || src.SHA != strings.Repeat("b", 40) {
		t.Errorf("Ref/SHA = %q/%q, unexpected", src.Ref, src.SHA)
	}
}

func TestCodexMapper_Plugin_RemotePackage_HostPrefixedSubdir_GitSubdirUsesHostURL(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{
			Entry:      authoring.PackageEntry{Name: "tool", Source: "ghe.example.com/owner/repo", Category: "utilities"},
			Host:       "ghe.example.com",
			SourceRepo: "owner/repo",
			Subdir:     "sub/dir",
		},
	}

	// Act
	doc, _, err := CodexMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	src := doc.Plugins[0].Source.(*RemoteSource)
	if src.Source != "git-subdir" || src.URL != "https://ghe.example.com/owner/repo" || src.Path != "sub/dir" {
		t.Errorf("source/url/path = %q/%q/%q, unexpected", src.Source, src.URL, src.Path)
	}
}

func TestCodexMapper_Plugin_RemotePackage_RefOrSHAOmittedWhenEmpty(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Name: "m"}
	resolved := []ResolvedPackage{
		{Entry: authoring.PackageEntry{Name: "tool", Source: "owner/repo", Category: "utilities"}, SourceRepo: "owner/repo"},
	}

	// Act
	doc, _, err := CodexMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	src := doc.Plugins[0].Source.(*RemoteSource)
	if src.Ref != "" || src.SHA != "" {
		t.Errorf("Ref/SHA = %q/%q, want both empty", src.Ref, src.SHA)
	}
}
