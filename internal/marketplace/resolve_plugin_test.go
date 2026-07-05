package marketplace

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/semver"
)

// TestResolvePlugin_MarketplaceNotFound_IgnoresProjectManifest covers
// mkt-022's negative regression: a project's own apm.yml "marketplace:"
// block that happens to declare a marketplace of the same name (and even a
// plugin of the same name inside it) must NOT be consulted -- only the
// global registry (~/.apm/marketplaces.json, empty here) decides whether
// "acme" is a registered marketplace.
func TestResolvePlugin_MarketplaceNotFound_IgnoresProjectManifest(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir()) // empty global registry -- never AddSource'd

	projectDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd(): %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("Chdir(%q): %v", projectDir, err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	apmYML := `name: test-project
version: "1.0.0"
marketplace:
  name: acme
  owner:
    name: acme-owner
  packages:
    - name: some-plugin
      source: ./plugins/some-plugin
`
	if err := os.WriteFile(filepath.Join(projectDir, "apm.yml"), []byte(apmYML), 0o644); err != nil {
		t.Fatalf("WriteFile(apm.yml): %v", err)
	}

	// Act -- query the exact marketplace/plugin names the local apm.yml
	// declares; a buggy implementation that "helpfully" also checks the
	// project's own marketplace: block would resolve this successfully.
	got, resolveErr := ResolvePlugin(context.Background(), "some-plugin", "acme", ResolveOptions{})

	// Assert
	if resolveErr == nil {
		t.Fatalf("ResolvePlugin() = %+v, nil; want an ErrMarketplaceNotFound error (must not read the project's own apm.yml)", got)
	}
	if !errors.Is(resolveErr, ErrMarketplaceNotFound) {
		t.Errorf("ResolvePlugin() error = %v; want errors.Is(err, ErrMarketplaceNotFound)", resolveErr)
	}
	if got != nil {
		t.Errorf("ResolvePlugin() Resolution = %+v, want nil alongside the error", got)
	}
}

// TestResolvePlugin_PluginNameCaseInsensitive covers mkt-024: plugin name
// lookup ignores case, and the resolved Provenance keeps the manifest's
// original casing (mkt-024 is about lookup, not normalization).
func TestResolvePlugin_PluginNameCaseInsensitive(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	mktDir := t.TempDir()
	writeManifest(t, mktDir, `{
		"name": "acme",
		"owner": "acme-owner",
		"plugins": [{"name": "MyPlugin", "source": "./p"}]
	}`)
	if err := AddSource(MarketplaceSource{Name: "acme", URL: mktDir}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	// Act
	got, err := ResolvePlugin(context.Background(), "myplugin", "acme", ResolveOptions{})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePlugin() returned unexpected error: %v", err)
	}
	wantCanonical := filepath.Join(mktDir, "p")
	if got.Canonical != wantCanonical {
		t.Errorf("ResolvePlugin() Canonical = %q, want %q", got.Canonical, wantCanonical)
	}
	if got.Provenance == nil || got.Provenance.MarketplacePluginName != "MyPlugin" {
		t.Errorf("ResolvePlugin() Provenance = %+v, want MarketplacePluginName=%q (original manifest casing)", got.Provenance, "MyPlugin")
	}
	if got.Provenance.DiscoveredVia != "acme" {
		t.Errorf("ResolvePlugin() Provenance.DiscoveredVia = %q, want %q", got.Provenance.DiscoveredVia, "acme")
	}
}

// TestResolvePlugin_PluginNotFound covers mkt-024's miss case: an unknown
// plugin name reports ErrPluginNotFound, distinct from ErrMarketplaceNotFound.
func TestResolvePlugin_PluginNotFound(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	mktDir := t.TempDir()
	writeManifest(t, mktDir, `{"name": "acme", "plugins": [{"name": "p", "source": "./p"}]}`)
	if err := AddSource(MarketplaceSource{Name: "acme", URL: mktDir}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	// Act
	got, err := ResolvePlugin(context.Background(), "does-not-exist", "acme", ResolveOptions{})

	// Assert
	if err == nil {
		t.Fatalf("ResolvePlugin() = %+v, nil; want ErrPluginNotFound", got)
	}
	if !errors.Is(err, ErrPluginNotFound) {
		t.Errorf("ResolvePlugin() error = %v; want errors.Is(err, ErrPluginNotFound)", err)
	}
}

// writeManifest writes name-shaped marketplace.json content into dir,
// matching fetchLocal's expected layout.
func writeManifest(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "marketplace.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(marketplace.json): %v", err)
	}
}

// TestResolvePlugin_RegisteredRefPropagation covers mkt-035: a marketplace
// registered with a non-main/HEAD ref propagates that ref onto a relative
// -string-source plugin's canonical, but only when the canonical doesn't
// already carry a "#ref" and the registered ref isn't main/HEAD/empty.
// Uses a GitHub-family marketplace (fake API via httptest) specifically so
// mkt-027's structured-DepRef branch does NOT engage (DepRef stays nil),
// isolating mkt-035's own propagation logic.
func TestResolvePlugin_RegisteredRefPropagation(t *testing.T) {
	tests := []struct {
		name          string
		registeredRef string
		wantCanonical string
	}{
		{"non-default ref propagates", "feat/x", "acme-owner/acme-repo/plugins/p#feat/x"},
		{"main is excluded (no-op default branch)", "main", "acme-owner/acme-repo/plugins/p"},
		{"HEAD is excluded", "HEAD", "acme-owner/acme-repo/plugins/p"},
		// An empty Ref is not a distinct case worth its own registry
		// round-trip: MarketplaceSource's JSON marshaling normalizes ""
		// to "main" on write/read (models.go's to_dict/from_dict parity),
		// so it is unreachable as a genuinely empty value once persisted
		// through AddSource/FindByName. The true empty-string input is
		// covered directly by TestIsPropagatableRef instead.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			t.Setenv("APM_CONFIG_DIR", t.TempDir())
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`{"name": "acme", "plugins": [{"name": "p", "source": "./plugins/p"}]}`))
			}))
			t.Cleanup(srv.Close)
			withGitHubAPIBase(t, srv.URL)
			src := MarketplaceSource{
				Name: "acme", URL: "https://github.com/acme-owner/acme-repo", Ref: tt.registeredRef,
				Path: "marketplace.json", Owner: "acme-owner", Repo: "acme-repo", Host: "github.com",
			}
			if err := AddSource(src); err != nil {
				t.Fatalf("AddSource(): %v", err)
			}

			// Act
			got, err := ResolvePlugin(context.Background(), "p", "acme", ResolveOptions{})

			// Assert
			if err != nil {
				t.Fatalf("ResolvePlugin() returned unexpected error: %v", err)
			}
			if got.Canonical != tt.wantCanonical {
				t.Errorf("ResolvePlugin() Canonical = %q, want %q", got.Canonical, tt.wantCanonical)
			}
			if got.DepRef != nil {
				t.Errorf("ResolvePlugin() DepRef = %+v, want nil for a GitHub-family marketplace", got.DepRef)
			}
		})
	}
}

// TestResolvePlugin_RegisteredRefPropagation_SkippedWhenCanonicalHasRef
// covers mkt-035's other guard: a dict source that already declares its own
// "ref" must not have the marketplace's registered ref appended on top.
func TestResolvePlugin_RegisteredRefPropagation_SkippedWhenCanonicalHasRef(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"name": "acme",
			"plugins": [{"name": "p", "source": {"type": "github", "repo": "other-owner/other-repo", "ref": "v9.0"}}]
		}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	if err := AddSource(MarketplaceSource{
		Name: "acme", URL: "https://github.com/acme-owner/acme-repo", Ref: "feat/x",
		Path: "marketplace.json", Owner: "acme-owner", Repo: "acme-repo", Host: "github.com",
	}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	// Act
	got, err := ResolvePlugin(context.Background(), "p", "acme", ResolveOptions{})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePlugin() returned unexpected error: %v", err)
	}
	want := "other-owner/other-repo#v9.0"
	if got.Canonical != want {
		t.Errorf("ResolvePlugin() Canonical = %q, want %q (dict source's own ref must win, marketplace ref must not double-append)", got.Canonical, want)
	}
}

// TestIsPropagatableRef covers mkt-035's ref-worth-propagating gate in
// isolation, including the genuinely-empty-string input that is
// unreachable through a registered MarketplaceSource's JSON round-trip
// (see TestResolvePlugin_RegisteredRefPropagation's comment).
func TestIsPropagatableRef(t *testing.T) {
	tests := []struct {
		ref  string
		want bool
	}{
		{"feat/x", true},
		{"main", false},
		{"HEAD", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			if got := isPropagatableRef(tt.ref); got != tt.want {
				t.Errorf("isPropagatableRef(%q) = %v, want %v", tt.ref, got, tt.want)
			}
		})
	}
}

// TestResolvePlugin_StructuredDepRef_InMarketplaceStringSource covers
// mkt-027: a relative-string-source plugin on a non-GitHub-family
// marketplace host (GitLab, via fake API) resolves to a structured
// {git,path,ref} DepRef instead of a bare "owner/repo/subdir" canonical.
func TestResolvePlugin_StructuredDepRef_InMarketplaceStringSource(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name": "acme", "plugins": [{"name": "p", "source": "./plugins/p"}]}`))
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	if err := AddSource(MarketplaceSource{
		Name: "acme", URL: "https://gitlab.com/acme-owner/acme-repo", Ref: "main",
		Path: "marketplace.json", Owner: "acme-owner", Repo: "acme-repo", Host: "gitlab.com",
	}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	// Act
	got, err := ResolvePlugin(context.Background(), "p", "acme", ResolveOptions{})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePlugin() returned unexpected error: %v", err)
	}
	if got.DepRef == nil {
		t.Fatalf("ResolvePlugin() DepRef = nil, want a structured DepRef for a non-GitHub-family in-marketplace subdirectory plugin")
	}
	if got.DepRef.Owner != "acme-owner" || got.DepRef.Repo != "acme-repo" {
		t.Errorf("ResolvePlugin() DepRef.Owner/Repo = %q/%q, want acme-owner/acme-repo", got.DepRef.Owner, got.DepRef.Repo)
	}
	if got.DepRef.VirtualPath != "plugins/p" {
		t.Errorf("ResolvePlugin() DepRef.VirtualPath = %q, want %q", got.DepRef.VirtualPath, "plugins/p")
	}
	if got.DepRef.VirtualType != "subdirectory" {
		t.Errorf("ResolvePlugin() DepRef.VirtualType = %q, want %q", got.DepRef.VirtualType, "subdirectory")
	}
	if got.DepRef.Reference != "" {
		t.Errorf("ResolvePlugin() DepRef.Reference = %q, want empty (registered ref is \"main\", excluded)", got.DepRef.Reference)
	}
	wantCanonical := "gitlab.com/acme-owner/acme-repo/plugins/p"
	if got.Canonical != wantCanonical {
		t.Errorf("ResolvePlugin() Canonical = %q, want %q", got.Canonical, wantCanonical)
	}
}

// TestResolvePlugin_StructuredDepRef_InMarketplaceDictSource covers
// mkt-027's dict-source variant: a git-subdir dict source whose "repo"
// field matches the marketplace's own project is still in-marketplace, and
// its own "ref" field takes priority over the marketplace's registered ref.
func TestResolvePlugin_StructuredDepRef_InMarketplaceDictSource(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"name": "acme",
			"plugins": [{"name": "p", "source": {
				"type": "git-subdir", "repo": "acme-owner/acme-repo", "subdir": "pkg/a", "ref": "v2.0"
			}}]
		}`))
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	if err := AddSource(MarketplaceSource{
		Name: "acme", URL: "https://gitlab.com/acme-owner/acme-repo", Ref: "feat/x",
		Path: "marketplace.json", Owner: "acme-owner", Repo: "acme-repo", Host: "gitlab.com",
	}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	// Act
	got, err := ResolvePlugin(context.Background(), "p", "acme", ResolveOptions{})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePlugin() returned unexpected error: %v", err)
	}
	if got.DepRef == nil {
		t.Fatalf("ResolvePlugin() DepRef = nil, want a structured DepRef")
	}
	if got.DepRef.VirtualPath != "pkg/a" {
		t.Errorf("ResolvePlugin() DepRef.VirtualPath = %q, want %q", got.DepRef.VirtualPath, "pkg/a")
	}
	if got.DepRef.Reference != "v2.0" {
		t.Errorf("ResolvePlugin() DepRef.Reference = %q, want %q (dict source's own ref beats the marketplace's registered ref)", got.DepRef.Reference, "v2.0")
	}
}

// TestResolvePlugin_NoStructuredDepRef_CrossRepoDictSource covers mkt-027's
// negative boundary: a dict source on a non-GitHub-family marketplace host
// whose "repo" field points at a genuinely DIFFERENT project (not the
// marketplace's own) is NOT in-marketplace, so it must fall back to a plain
// resolvePluginSource canonical instead of a structured DepRef.
func TestResolvePlugin_NoStructuredDepRef_CrossRepoDictSource(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"name": "acme",
			"plugins": [{"name": "p", "source": {
				"type": "git-subdir", "repo": "other-owner/other-repo", "subdir": "pkg/a"
			}}]
		}`))
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	if err := AddSource(MarketplaceSource{
		Name: "acme", URL: "https://gitlab.com/acme-owner/acme-repo", Ref: "main",
		Path: "marketplace.json", Owner: "acme-owner", Repo: "acme-repo", Host: "gitlab.com",
	}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	// Act
	got, err := ResolvePlugin(context.Background(), "p", "acme", ResolveOptions{})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePlugin() returned unexpected error: %v", err)
	}
	if got.DepRef != nil {
		t.Errorf("ResolvePlugin() DepRef = %+v, want nil for a cross-repo dict source (not in-marketplace)", got.DepRef)
	}
	want := "other-owner/other-repo/pkg/a"
	if got.Canonical != want {
		t.Errorf("ResolvePlugin() Canonical = %q, want %q", got.Canonical, want)
	}
}

// TestFindPluginCaseInsensitive is a focused unit test on the lookup helper
// itself (mkt-024), independent of the full ResolvePlugin/Fetch pipeline.
func TestFindPluginCaseInsensitive(t *testing.T) {
	plugins := []MarketplacePlugin{{Name: "Foo"}, {Name: "bar"}}

	// Act + Assert
	if got := findPluginCaseInsensitive(plugins, "FOO"); got == nil || got.Name != "Foo" {
		t.Errorf("findPluginCaseInsensitive(%q) = %+v, want Name=%q", "FOO", got, "Foo")
	}
	if got := findPluginCaseInsensitive(plugins, "BAR"); got == nil || got.Name != "bar" {
		t.Errorf("findPluginCaseInsensitive(%q) = %+v, want Name=%q", "BAR", got, "bar")
	}
	if got := findPluginCaseInsensitive(plugins, "baz"); got != nil {
		t.Errorf("findPluginCaseInsensitive(%q) = %+v, want nil", "baz", got)
	}
}

// TestSourceNeedsExplicitGitPath covers mkt-027's host-family gate in
// isolation: GitHub-family hosts never need the structured DepRef;
// GitLab/generic-git hosts always do.
func TestSourceNeedsExplicitGitPath(t *testing.T) {
	tests := []struct {
		name string
		src  *MarketplaceSource
		want bool
	}{
		{"github.com", &MarketplaceSource{URL: "https://github.com/o/r"}, false},
		{"ghe.com enterprise", &MarketplaceSource{URL: "https://corp.ghe.com/o/r"}, false},
		{"gitlab.com", &MarketplaceSource{URL: "https://gitlab.com/o/r"}, true},
		{"generic git host", &MarketplaceSource{URL: "https://git.example.com/o/r"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sourceNeedsExplicitGitPath(tt.src); got != tt.want {
				t.Errorf("sourceNeedsExplicitGitPath(%q) = %v, want %v", tt.src.URL, got, tt.want)
			}
		})
	}
}

// TestIsInMarketplaceSource covers mkt-027's in-marketplace detection
// directly: plain string sources are always in-marketplace; dict sources
// only when their repo field resolves to the marketplace's own project.
func TestIsInMarketplaceSource(t *testing.T) {
	mkt := &MarketplaceSource{Owner: "acme-owner", Repo: "acme-repo", Host: "gitlab.com"}
	tests := []struct {
		name   string
		plugin *MarketplacePlugin
		want   bool
	}{
		{"relative string source", &MarketplacePlugin{Source: "./p"}, true},
		{"nil source", &MarketplacePlugin{Source: nil}, false},
		{
			"dict source matching marketplace repo",
			&MarketplacePlugin{Source: map[string]any{"type": "git-subdir", "repo": "acme-owner/acme-repo"}},
			true,
		},
		{
			"dict source naming a different repo (cross-repo)",
			&MarketplacePlugin{Source: map[string]any{"type": "git-subdir", "repo": "other-owner/other-repo"}},
			false,
		},
		{
			"dict source with url type never counts as in-marketplace",
			&MarketplacePlugin{Source: map[string]any{"type": "url", "url": "https://gitlab.com/acme-owner/acme-repo"}},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isInMarketplaceSource(tt.plugin, mkt); got != tt.want {
				t.Errorf("isInMarketplaceSource() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestExtractInRepoPathAndRef covers mkt-027's path/ref extraction across
// plugin.Source shapes.
func TestExtractInRepoPathAndRef(t *testing.T) {
	tests := []struct {
		name       string
		plugin     *MarketplacePlugin
		pluginRoot string
		wantPath   string
		wantRef    string
	}{
		{"relative string, no plugin root", &MarketplacePlugin{Source: "./plugins/p"}, "", "plugins/p", ""},
		{"relative string at marketplace root", &MarketplacePlugin{Source: "."}, "", "", ""},
		{"bare-name source backfilled with plugin root", &MarketplacePlugin{Source: "p"}, "packages", "packages/p", ""},
		{
			"github dict with path and ref",
			&MarketplacePlugin{Source: map[string]any{"type": "github", "path": "sub/dir", "ref": "v1.0"}},
			"", "sub/dir", "v1.0",
		},
		{
			"github dict with no path -> repo root",
			&MarketplacePlugin{Source: map[string]any{"type": "github", "ref": "v1.0"}},
			"", "", "v1.0",
		},
		{
			"git-subdir dict uses subdir",
			&MarketplacePlugin{Source: map[string]any{"type": "git-subdir", "subdir": "pkg/a"}},
			"", "pkg/a", "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, ref, err := extractInRepoPathAndRef(tt.plugin, tt.pluginRoot)
			if err != nil {
				t.Fatalf("extractInRepoPathAndRef() returned unexpected error: %v", err)
			}
			if path != tt.wantPath || ref != tt.wantRef {
				t.Errorf("extractInRepoPathAndRef() = (%q, %q), want (%q, %q)", path, ref, tt.wantPath, tt.wantRef)
			}
		})
	}
}

// TestExtractInRepoPathAndRef_TraversalRejected covers the traversal guard:
// a ".." segment in a relative/dict path must error, not silently resolve.
func TestExtractInRepoPathAndRef_TraversalRejected(t *testing.T) {
	_, _, err := extractInRepoPathAndRef(&MarketplacePlugin{Source: "../escape"}, "")
	if err == nil {
		t.Fatal("extractInRepoPathAndRef() returned no error for a traversal path")
	}
	if !strings.Contains(err.Error(), "traversal") {
		t.Errorf("extractInRepoPathAndRef() error = %q, want it to mention traversal", err.Error())
	}
}

// TestMarketplaceHTTPSGitURL covers the clone-URL synthesis helper mkt-027
// builds structured DepRefs from.
func TestMarketplaceHTTPSGitURL(t *testing.T) {
	tests := []struct {
		name string
		src  *MarketplaceSource
		want string
	}{
		{"https URL without .git suffix", &MarketplaceSource{URL: "https://gitlab.com/acme-owner/acme-repo"}, "https://gitlab.com/acme-owner/acme-repo.git"},
		{"https URL already has .git suffix", &MarketplaceSource{URL: "https://gitlab.com/acme-owner/acme-repo.git"}, "https://gitlab.com/acme-owner/acme-repo.git"},
		{"SCP-style SSH remote passed through verbatim", &MarketplaceSource{URL: "git@gitlab.example.com:acme-owner/acme-repo.git"}, "git@gitlab.example.com:acme-owner/acme-repo.git"},
		{"legacy owner/repo/host synthesis", &MarketplaceSource{Owner: "acme-owner", Repo: "acme-repo", Host: "git.example.com"}, "https://git.example.com/acme-owner/acme-repo.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := marketplaceHTTPSGitURL(tt.src); got != tt.want {
				t.Errorf("marketplaceHTTPSGitURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── mkt-021/033: version_spec wired through ResolvePlugin ───────────────────

// TestResolvePlugin_VersionSpec_RawRefAppliedDirectly covers mkt-021's CLI
// "#REF" shape end to end: a non-range version_spec replaces the resolved
// canonical's "#ref" fragment directly, with zero TagLister calls
// (panicTagLister proves that).
func TestResolvePlugin_VersionSpec_RawRefAppliedDirectly(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name": "acme", "plugins": [{"name": "p", "source": "./plugins/p"}]}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	if err := AddSource(MarketplaceSource{
		Name: "acme", URL: "https://github.com/acme-owner/acme-repo", Ref: "main",
		Path: "marketplace.json", Owner: "acme-owner", Repo: "acme-repo", Host: "github.com",
	}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	// Act
	got, err := ResolvePlugin(context.Background(), "p", "acme", ResolveOptions{
		VersionSpec: "v3.0.0",
		Tags:        panicTagLister{},
	})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePlugin() returned unexpected error: %v", err)
	}
	want := "acme-owner/acme-repo/plugins/p#v3.0.0"
	if got.Canonical != want {
		t.Errorf("ResolvePlugin() Canonical = %q, want %q", got.Canonical, want)
	}
}

// TestResolvePlugin_VersionSpec_RangeResolvesHighestMatchingTag covers
// mkt-033's apm.yml dict "version:" range path end to end via ResolvePlugin.
func TestResolvePlugin_VersionSpec_RangeResolvesHighestMatchingTag(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name": "acme", "plugins": [{"name": "p", "source": "./plugins/p"}]}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	if err := AddSource(MarketplaceSource{
		Name: "acme", URL: "https://github.com/acme-owner/acme-repo", Ref: "main",
		Path: "marketplace.json", Owner: "acme-owner", Repo: "acme-repo", Host: "github.com",
	}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}
	tags := &mapTagLister{tags: map[string][]semver.TagInfo{
		"acme-owner/acme-repo": {
			{Name: "v1.0.0", Commit: "c1"},
			{Name: "v1.2.0", Commit: "c2"},
			{Name: "v2.0.0", Commit: "c3"},
		},
	}}

	// Act
	got, err := ResolvePlugin(context.Background(), "p", "acme", ResolveOptions{
		VersionSpec: "^1.0.0",
		Tags:        tags,
	})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePlugin() returned unexpected error: %v", err)
	}
	want := "acme-owner/acme-repo/plugins/p#v1.2.0"
	if got.Canonical != want {
		t.Errorf("ResolvePlugin() Canonical = %q, want %q", got.Canonical, want)
	}
}

// TestResolvePlugin_VersionSpec_SuppressesRegisteredRefPropagation covers
// the interaction fix mkt-021/033 required on the existing mkt-035 block: a
// version_spec must win over the marketplace's registered ref, not be
// appended alongside/after it -- mirrors the Python original's `and not
// version_spec` guard on that block.
func TestResolvePlugin_VersionSpec_SuppressesRegisteredRefPropagation(t *testing.T) {
	// Arrange -- registered with Ref: "feat/x": without the version_spec
	// guard, mkt-035 would append "#feat/x" onto the relative-string-source
	// plugin's canonical before version_spec ever got a chance to run.
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name": "acme", "plugins": [{"name": "p", "source": "./plugins/p"}]}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	if err := AddSource(MarketplaceSource{
		Name: "acme", URL: "https://github.com/acme-owner/acme-repo", Ref: "feat/x",
		Path: "marketplace.json", Owner: "acme-owner", Repo: "acme-repo", Host: "github.com",
	}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	// Act
	got, err := ResolvePlugin(context.Background(), "p", "acme", ResolveOptions{
		VersionSpec: "v9.0.0",
		Tags:        panicTagLister{},
	})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePlugin() returned unexpected error: %v", err)
	}
	want := "acme-owner/acme-repo/plugins/p#v9.0.0"
	if got.Canonical != want {
		t.Errorf("ResolvePlugin() Canonical = %q, want %q (version_spec must win over mkt-035's registered-ref propagation)", got.Canonical, want)
	}
}

// TestResolvePlugin_VersionSpec_SkippedWhenDepRefSet covers the other guard
// mkt-021/033 required: mkt-027's structured DepRef already carries its own
// path/ref decision, so a version_spec must not be layered on top of it --
// mirrors the Python original's `if version_spec and dep_ref is None` guard.
// panicTagLister proves ListTags is never reached.
func TestResolvePlugin_VersionSpec_SkippedWhenDepRefSet(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name": "acme", "plugins": [{"name": "p", "source": "./plugins/p"}]}`))
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	if err := AddSource(MarketplaceSource{
		Name: "acme", URL: "https://gitlab.com/acme-owner/acme-repo", Ref: "main",
		Path: "marketplace.json", Owner: "acme-owner", Repo: "acme-repo", Host: "gitlab.com",
	}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	// Act
	got, err := ResolvePlugin(context.Background(), "p", "acme", ResolveOptions{
		VersionSpec: "v9.0.0",
		Tags:        panicTagLister{},
	})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePlugin() returned unexpected error: %v", err)
	}
	if got.DepRef == nil {
		t.Fatalf("ResolvePlugin() DepRef = nil, want a structured DepRef (test setup regression)")
	}
	if got.DepRef.Reference != "" {
		t.Errorf("ResolvePlugin() DepRef.Reference = %q, want empty -- version_spec must not apply when DepRef is already structured", got.DepRef.Reference)
	}
	if got.Canonical != "gitlab.com/acme-owner/acme-repo/plugins/p" {
		t.Errorf("ResolvePlugin() Canonical = %q, want the plain mkt-027 canonical, unaffected by version_spec", got.Canonical)
	}
}

// ── mkt-028: cross-repo dependency-confusion fail-closed gate ──────────────

// TestResolvePlugin_CrossRepoMisconfig_RejectedBeforeAnyNetworkProbe covers
// mkt-028's core security requirement end to end: a cross-repo `type:
// github` plugin source with a bare "owner/repo" repo field on an
// enterprise GitHub-family marketplace must be refused BEFORE any further
// network probing -- proven here by supplying a semver-range VersionSpec
// (which would otherwise definitely trigger mkt-021/033's tags.ListTags
// call) alongside panicTagLister: if ResolvePlugin's cross-repo gate did not
// short-circuit before reaching version_spec resolution, this test would
// panic instead of failing normally.
func TestResolvePlugin_CrossRepoMisconfig_RejectedBeforeAnyNetworkProbe(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"name": "acme",
			"plugins": [{"name": "p", "source": {"type": "github", "repo": "some-org/some-repo"}}]
		}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	if err := AddSource(MarketplaceSource{
		Name: "acme", URL: "https://corp.ghe.com/acme-owner/acme-repo", Ref: "main",
		Path: "marketplace.json", Owner: "acme-owner", Repo: "acme-repo", Host: "corp.ghe.com",
	}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	// Act -- a semver-range VersionSpec would reach panicTagLister.ListTags
	// were the gate not to short-circuit first.
	got, err := ResolvePlugin(context.Background(), "p", "acme", ResolveOptions{
		VersionSpec: "^1.0.0",
		Tags:        panicTagLister{},
	})

	// Assert
	if err == nil {
		t.Fatalf("ResolvePlugin() = %+v, nil; want ErrCrossRepoMisconfig", got)
	}
	if !errors.Is(err, ErrCrossRepoMisconfig) {
		t.Errorf("ResolvePlugin() error = %v; want errors.Is(err, ErrCrossRepoMisconfig)", err)
	}
	if got != nil {
		t.Errorf("ResolvePlugin() Resolution = %+v, want nil alongside the error", got)
	}
	for _, want := range []string{"corp.ghe.com/some-org/some-repo", "github.com/some-org/some-repo"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ResolvePlugin() error = %q, want it to contain remediation option %q", err.Error(), want)
		}
	}
}

// TestResolvePlugin_CrossRepoMisconfig_HostQualifiedFormsExempt covers
// mkt-028's exemptions end to end: host-qualified (own host and a different
// host), URL, and SCP-style repo fields all resolve successfully instead of
// tripping the gate.
func TestResolvePlugin_CrossRepoMisconfig_HostQualifiedFormsExempt(t *testing.T) {
	tests := []struct {
		name string
		repo string
	}{
		{"host-qualified to a different host (public github.com)", "github.com/some-org/some-repo"},
		{"host-qualified to the marketplace's own enterprise host", "corp.ghe.com/some-org/some-repo"},
		{"full HTTPS URL", "https://github.com/some-org/some-repo"},
		{"SCP-style SSH remote", "git@github.com:some-org/some-repo.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			t.Setenv("APM_CONFIG_DIR", t.TempDir())
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"name": "acme", "plugins": [{"name": "p", "source": {"type": "github", "repo": %q}}]}`, tt.repo)
			}))
			t.Cleanup(srv.Close)
			withGitHubAPIBase(t, srv.URL)
			if err := AddSource(MarketplaceSource{
				Name: "acme", URL: "https://corp.ghe.com/acme-owner/acme-repo", Ref: "main",
				Path: "marketplace.json", Owner: "acme-owner", Repo: "acme-repo", Host: "corp.ghe.com",
			}); err != nil {
				t.Fatalf("AddSource(): %v", err)
			}

			// Act -- panicTagLister proves the (unrelated) success path also
			// takes zero tag-lookup network action here, since VersionSpec is
			// left empty.
			got, err := ResolvePlugin(context.Background(), "p", "acme", ResolveOptions{Tags: panicTagLister{}})

			// Assert
			if err != nil {
				t.Fatalf("ResolvePlugin() returned unexpected error: %v", err)
			}
			if got == nil {
				t.Fatal("ResolvePlugin() = nil, nil; want a Resolution")
			}
		})
	}
}

// TestResolvePlugin_CrossRepoMisconfig_InMarketplaceExempt covers the
// in-marketplace exemption end to end: a cross-repo-shaped dict source whose
// repo field actually names the marketplace's OWN project is not a
// dependency-confusion risk (PR #1292/#1305's own carve-out) and resolves
// normally.
func TestResolvePlugin_CrossRepoMisconfig_InMarketplaceExempt(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"name": "acme",
			"plugins": [{"name": "p", "source": {"type": "github", "repo": "acme-owner/acme-repo", "path": "plugins/p"}}]
		}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	if err := AddSource(MarketplaceSource{
		Name: "acme", URL: "https://corp.ghe.com/acme-owner/acme-repo", Ref: "main",
		Path: "marketplace.json", Owner: "acme-owner", Repo: "acme-repo", Host: "corp.ghe.com",
	}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	// Act
	got, err := ResolvePlugin(context.Background(), "p", "acme", ResolveOptions{Tags: panicTagLister{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePlugin() returned unexpected error: %v", err)
	}
	want := "acme-owner/acme-repo/plugins/p"
	if got.Canonical != want {
		t.Errorf("ResolvePlugin() Canonical = %q, want %q", got.Canonical, want)
	}
}

// ── mkt-034: ref-swap-pin + shadow-detection advisories wired end to end ───

// TestResolvePlugin_MKT034_RefSwapPinWarning covers mkt-034a wired all the
// way through ResolvePlugin: resolving the same plugin@marketplace a SECOND
// time after the marketplace's registered ref changed (feat/x -> v2.0, which
// mkt-035 propagates onto the canonical's "#ref") must surface exactly one
// ref-swap warning; the first-ever resolution must not.
func TestResolvePlugin_MKT034_RefSwapPinWarning(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name": "acme", "plugins": [{"name": "p", "source": "./plugins/p"}]}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	if err := AddSource(MarketplaceSource{
		Name: "acme", URL: "https://github.com/acme-owner/acme-repo", Ref: "feat/x",
		Path: "marketplace.json", Owner: "acme-owner", Repo: "acme-repo", Host: "github.com",
	}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	first, err := ResolvePlugin(context.Background(), "p", "acme", ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolvePlugin() first call returned error: %v", err)
	}
	if len(first.Warnings) != 0 {
		t.Fatalf("ResolvePlugin() first call Warnings = %v, want none (nothing pinned yet)", first.Warnings)
	}

	// Re-register the SAME marketplace name with a DIFFERENT registered ref
	// -- the canonical's "#ref" changes (feat/x -> v2.0) while the pin key
	// (acme/p, no declared plugin version) stays identical.
	if err := AddSource(MarketplaceSource{
		Name: "acme", URL: "https://github.com/acme-owner/acme-repo", Ref: "v2.0",
		Path: "marketplace.json", Owner: "acme-owner", Repo: "acme-repo", Host: "github.com",
	}); err != nil {
		t.Fatalf("AddSource() re-register: %v", err)
	}

	// Act
	second, err := ResolvePlugin(context.Background(), "p", "acme", ResolveOptions{})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePlugin() second call returned error: %v", err)
	}
	if len(second.Warnings) != 1 {
		t.Fatalf("ResolvePlugin() second call Warnings = %#v, want exactly 1 ref-swap warning", second.Warnings)
	}
	if !strings.Contains(second.Warnings[0], "ref swap") {
		t.Errorf("ResolvePlugin() warning = %q, want it to mention a ref swap", second.Warnings[0])
	}
}

// TestResolvePlugin_MKT034_ShadowWarning covers mkt-034b wired all the way
// through ResolvePlugin: a plugin of the same name registered under a
// SECOND marketplace produces a shadow warning naming it, alongside a
// successful resolution against the primary marketplace.
func TestResolvePlugin_MKT034_ShadowWarning(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name": "acme", "plugins": [{"name": "p", "source": "./plugins/p"}]}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	if err := AddSource(MarketplaceSource{
		Name: "acme", URL: "https://github.com/acme-owner/acme-repo", Ref: "main",
		Path: "marketplace.json", Owner: "acme-owner", Repo: "acme-repo", Host: "github.com",
	}); err != nil {
		t.Fatalf("AddSource(acme): %v", err)
	}
	shadowDir := t.TempDir()
	writeManifest(t, shadowDir, `{"name": "shadow-mkt", "plugins": [{"name": "p", "source": "./p"}]}`)
	if err := AddSource(MarketplaceSource{Name: "shadow-mkt", URL: shadowDir}); err != nil {
		t.Fatalf("AddSource(shadow-mkt): %v", err)
	}

	// Act
	got, err := ResolvePlugin(context.Background(), "p", "acme", ResolveOptions{})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePlugin() returned unexpected error: %v", err)
	}
	if got.Canonical != "acme-owner/acme-repo/plugins/p" {
		t.Errorf("ResolvePlugin() Canonical = %q, want the plain acme resolution, unaffected by the shadow warning", got.Canonical)
	}
	if len(got.Warnings) != 1 {
		t.Fatalf("ResolvePlugin() Warnings = %#v, want exactly 1 shadow warning", got.Warnings)
	}
	if !strings.Contains(got.Warnings[0], "shadow-mkt") {
		t.Errorf("ResolvePlugin() warning = %q, want it to mention shadow-mkt", got.Warnings[0])
	}
}

// TestResolvePlugin_MKT034_AdvisoryFailuresNeverBlockInstall covers the
// "must never interrupt installation" requirement from both halves of
// mkt-034 at once: a corrupt on-disk pin file AND an unfetchable registered
// marketplace (for shadow detection) must still let ResolvePlugin succeed,
// with no error and no panic.
func TestResolvePlugin_MKT034_AdvisoryFailuresNeverBlockInstall(t *testing.T) {
	// Arrange
	configDir := t.TempDir()
	t.Setenv("APM_CONFIG_DIR", configDir)
	pinsDir := filepath.Join(configDir, "cache", "marketplace")
	if err := os.MkdirAll(pinsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", pinsDir, err)
	}
	if err := os.WriteFile(filepath.Join(pinsDir, "version-pins.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("WriteFile(corrupt pins): %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name": "acme", "plugins": [{"name": "p", "source": "./plugins/p"}]}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	if err := AddSource(MarketplaceSource{
		Name: "acme", URL: "https://github.com/acme-owner/acme-repo", Ref: "main",
		Path: "marketplace.json", Owner: "acme-owner", Repo: "acme-repo", Host: "github.com",
	}); err != nil {
		t.Fatalf("AddSource(acme): %v", err)
	}
	// A second registered marketplace whose manifest can never be fetched
	// (no marketplace.json written at all) -- shadow detection must swallow
	// this failure rather than letting it bubble up.
	if err := AddSource(MarketplaceSource{Name: "broken", URL: t.TempDir()}); err != nil {
		t.Fatalf("AddSource(broken): %v", err)
	}

	// Act -- must not panic or error.
	got, err := ResolvePlugin(context.Background(), "p", "acme", ResolveOptions{})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePlugin() returned error despite advisory-only failures: %v", err)
	}
	if got.Canonical != "acme-owner/acme-repo/plugins/p" {
		t.Errorf("ResolvePlugin() Canonical = %q, want the normal resolution to still succeed", got.Canonical)
	}
}
