package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/marketplace"
)

// ── test helpers ──────────────────────────────────────────────────────────

// runMarketplaceCmd executes `marketplace <args...>` against a fresh
// marketplaceCmd() tree, capturing combined stdout+stderr (this file's RunE
// bodies write through cmd.OutOrStdout()/cmd.ErrOrStderr(), never the raw os
// streams, specifically so tests can capture them this way).
func runMarketplaceCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := marketplaceCmd()
	var buf bytes.Buffer
	c.SetOut(&buf)
	c.SetErr(&buf)
	c.SetArgs(args)
	err := c.Execute()
	return buf.String(), err
}

// isolatedMarketplaceRegistry points ~/.apm/marketplaces.json at a fresh
// per-test temp dir, so tests never touch a real developer's registry.
func isolatedMarketplaceRegistry(t *testing.T) {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
}

// withNonInteractiveStdin forces isInteractive() (init.go) to false for the
// duration of the test, regardless of whether the test process's own real
// stdin happens to be a terminal (it can be, depending on how `go test` was
// launched) -- a pipe read-end is never a character device, matching how
// CI/non-interactive invocations actually present.
func withNonInteractiveStdin(t *testing.T) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = orig
		r.Close()
	})
}

// writeLocalManifestDir creates a KindLocal marketplace fixture directory
// containing marketplace.json, requiring no network access -- every CLI test
// in this file that needs a Fetch to actually succeed uses this, not a live
// GitHub/GitLab/git remote (those transports are already exhaustively
// covered at the internal/marketplace package level, steps 4-6).
func writeLocalManifestDir(t *testing.T, manifestJSON string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marketplace.json"), []byte(manifestJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeMarketplaceRegistryFixture writes ~/.apm/marketplaces.json directly
// (bypassing marketplace.AddSource/SaveRegistry), so tests exercise a
// registry that "already existed with unrelated content" on disk, not only
// ever a round-trip through this task's own writer (AC3 / marketplace-
// checklist.md's "舊坑 1"). The on-disk shape is still the wrapping
// {"marketplaces": [...]} object (mkt-002) MarketplaceSource's own
// MarshalJSON produces, since "bypassing" here only means skipping
// SaveRegistry's atomic temp-file dance, not writing a shape the package
// itself would never produce.
func writeMarketplaceRegistryFixture(t *testing.T, sources []marketplace.MarketplaceSource) {
	t.Helper()
	p, err := marketplace.RegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(struct {
		Marketplaces []marketplace.MarketplaceSource `json:"marketplaces"`
	}{Marketplaces: sources}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// unrelatedFixtureEntries mirrors internal/marketplace's
// existingUnrelatedFixture: every entry uses the canonical (already-
// defaulted) Ref/Path/Host values LoadRegistry always fills in for an
// absent key (A2 parity), so these fixtures stay stable across a
// write+read round trip regardless of source kind.
func unrelatedFixtureEntries() []marketplace.MarketplaceSource {
	return []marketplace.MarketplaceSource{
		{Name: "unrelated-one", URL: "https://github.com/foo/bar", Ref: "main", Path: "marketplace.json", Owner: "foo", Repo: "bar", Host: "github.com"},
		{Name: "unrelated-two", URL: "/abs/local/path", Ref: "main", Path: "marketplace.json", Host: "github.com"},
	}
}

// ── pure-function unit tests ────────────────────────────────────────────

// TestSplitHTTPSSourceFragment covers mkt-018's "#ref" fragment support:
// only a full "https://" SOURCE ever has its fragment split off.
func TestSplitHTTPSSourceFragment(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantSource string
		wantRef    string
	}{
		{"https with fragment", "https://github.com/owner/repo#v1.2.3", "https://github.com/owner/repo", "v1.2.3"},
		{"https without fragment", "https://github.com/owner/repo", "https://github.com/owner/repo", ""},
		{"case-insensitive https scheme", "HTTPS://github.com/owner/repo#main", "HTTPS://github.com/owner/repo", "main"},
		{"shorthand with a literal # is left untouched", "owner/repo#branch", "owner/repo#branch", ""},
		{"local path with a literal # is left untouched", "./local#weird", "./local#weird", ""},
		{"scp remote with a literal # is left untouched", "git@github.com:owner/repo.git#x", "git@github.com:owner/repo.git#x", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSource, gotRef := splitHTTPSSourceFragment(tt.raw)
			if gotSource != tt.wantSource || gotRef != tt.wantRef {
				t.Errorf("splitHTTPSSourceFragment(%q) = (%q, %q), want (%q, %q)", tt.raw, gotSource, gotRef, tt.wantSource, tt.wantRef)
			}
		})
	}
}

// TestNeedsUnpinnedGitRefWarning covers mkt-018's "Pin this git marketplace
// with a #ref" decision in isolation from any Fetch call.
func TestNeedsUnpinnedGitRefWarning(t *testing.T) {
	tests := []struct {
		name         string
		wasFullHTTPS bool
		kind         marketplace.SourceKind
		effectiveRef string
		want         bool
	}{
		{"full https github with no ref warns", true, marketplace.KindGitHub, "", true},
		{"full https gitlab with no ref warns", true, marketplace.KindGitLab, "", true},
		{"full https generic git with no ref warns", true, marketplace.KindGit, "", true},
		{"full https with an explicit ref does not warn", true, marketplace.KindGitHub, "v1", false},
		{"full https direct manifest URL never warns (no ref concept)", true, marketplace.KindURL, "", false},
		{"full https local never warns", true, marketplace.KindLocal, "", false},
		{"shorthand (not a full https SOURCE) never warns", false, marketplace.KindGitHub, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsUnpinnedGitRefWarning(tt.wasFullHTTPS, tt.kind, tt.effectiveRef); got != tt.want {
				t.Errorf("needsUnpinnedGitRefWarning(%v, %q, %q) = %v, want %v", tt.wasFullHTTPS, tt.kind, tt.effectiveRef, got, tt.want)
			}
		})
	}
}

// TestIsValidMarketplaceAlias covers mkt-004's alias format rule as
// consulted by mkt-018's fallback logic.
func TestIsValidMarketplaceAlias(t *testing.T) {
	tests := []struct {
		name  string
		alias string
		want  bool
	}{
		{"alnum with dash/underscore/dot", "acme-tools_v2.1", true},
		{"single char", "a", true},
		{"empty is invalid", "", false},
		{"space is invalid", "not a valid alias", false},
		{"slash is invalid", "owner/repo", false},
		{"at-sign is invalid", "name@host", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidMarketplaceAlias(tt.alias); got != tt.want {
				t.Errorf("isValidMarketplaceAlias(%q) = %v, want %v", tt.alias, got, tt.want)
			}
		})
	}
}

// TestResolveMarketplaceAlias covers mkt-018's full --name fallback chain.
func TestResolveMarketplaceAlias(t *testing.T) {
	src := &marketplace.MarketplaceSource{Repo: "fallback-repo"}

	t.Run("explicit name always wins", func(t *testing.T) {
		name, warn := resolveMarketplaceAlias("explicit", "valid-manifest-name", src)
		if name != "explicit" || warn != "" {
			t.Errorf("resolveMarketplaceAlias() = (%q, %q), want (\"explicit\", \"\")", name, warn)
		}
	})

	t.Run("valid manifest name used when no explicit name", func(t *testing.T) {
		name, warn := resolveMarketplaceAlias("", "valid-manifest-name", src)
		if name != "valid-manifest-name" || warn != "" {
			t.Errorf("resolveMarketplaceAlias() = (%q, %q), want (\"valid-manifest-name\", \"\")", name, warn)
		}
	})

	t.Run("invalid manifest name warns and falls back", func(t *testing.T) {
		name, warn := resolveMarketplaceAlias("", "Not A Valid Alias!", src)
		if name != "fallback-repo" {
			t.Errorf("name = %q, want %q", name, "fallback-repo")
		}
		if warn == "" {
			t.Error("warn = \"\", want a non-empty warning naming the invalid manifest name")
		}
	})

	t.Run("empty manifest name falls back silently", func(t *testing.T) {
		name, warn := resolveMarketplaceAlias("", "", src)
		if name != "fallback-repo" || warn != "" {
			t.Errorf("resolveMarketplaceAlias() = (%q, %q), want (\"fallback-repo\", \"\") -- no manifest name means nothing invalid to warn about", name, warn)
		}
	})
}

// TestFallbackMarketplaceAlias covers the repo-name derivation
// resolveMarketplaceAlias falls back to.
func TestFallbackMarketplaceAlias(t *testing.T) {
	t.Run("prefers Owner/Repo when set", func(t *testing.T) {
		src := &marketplace.MarketplaceSource{Repo: "my-repo", URL: "https://github.com/owner/my-repo"}
		if got := fallbackMarketplaceAlias(src); got != "my-repo" {
			t.Errorf("fallbackMarketplaceAlias() = %q, want %q", got, "my-repo")
		}
	})

	t.Run("local source uses the directory's base name", func(t *testing.T) {
		dir := t.TempDir()
		src := &marketplace.MarketplaceSource{URL: dir, Path: "marketplace.json"}
		want := filepath.Base(dir)
		if got := fallbackMarketplaceAlias(src); got != want {
			t.Errorf("fallbackMarketplaceAlias() = %q, want %q", got, want)
		}
	})

	t.Run("direct manifest URL uses the parent path segment", func(t *testing.T) {
		src := &marketplace.MarketplaceSource{URL: "https://example.com/some-repo/marketplace.json", Path: ""}
		if got := fallbackMarketplaceAlias(src); got != "some-repo" {
			t.Errorf("fallbackMarketplaceAlias() = %q, want %q", got, "some-repo")
		}
	})
}

// TestSummarizeFindings covers validate's Summary-line arithmetic.
func TestSummarizeFindings(t *testing.T) {
	tests := []struct {
		name         string
		manifest     *marketplace.MarketplaceManifest
		findings     []marketplace.Finding
		wantPassed   int
		wantWarnings int
		wantErrs     int
	}{
		{
			name:       "no findings: everything passed",
			manifest:   &marketplace.MarketplaceManifest{Name: "acme", Plugins: []marketplace.MarketplacePlugin{{Name: "p"}}},
			findings:   nil,
			wantPassed: 2, wantWarnings: 0, wantErrs: 0,
		},
		{
			name:       "one error reduces passed by one",
			manifest:   &marketplace.MarketplaceManifest{Name: "acme", Plugins: []marketplace.MarketplacePlugin{{Name: "p"}, {Name: "q"}}},
			findings:   []marketplace.Finding{{Level: marketplace.LevelError, Message: "x"}},
			wantPassed: 2, wantWarnings: 0, wantErrs: 1,
		},
		{
			name:       "errors cannot drive passed below zero",
			manifest:   &marketplace.MarketplaceManifest{Name: "acme"},
			findings:   []marketplace.Finding{{Level: marketplace.LevelError, Message: "a"}, {Level: marketplace.LevelError, Message: "b"}, {Level: marketplace.LevelError, Message: "c"}},
			wantPassed: 0, wantWarnings: 0, wantErrs: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			passed, warnings, errs := summarizeFindings(tt.manifest, tt.findings)
			if passed != tt.wantPassed || warnings != tt.wantWarnings || errs != tt.wantErrs {
				t.Errorf("summarizeFindings() = (%d, %d, %d), want (%d, %d, %d)", passed, warnings, errs, tt.wantPassed, tt.wantWarnings, tt.wantErrs)
			}
		})
	}
}

// ── `add` (mkt-010, mkt-011, mkt-018) ───────────────────────────────────

func TestMarketplaceAdd_LocalPath_FallsBackToManifestNameAlias(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme-tools", "plugins": [{"name": "p", "source": "./p"}]}`)

	// Act
	out, err := runMarketplaceCmd(t, "add", dir)

	// Assert
	if err != nil {
		t.Fatalf("marketplace add returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, `"acme-tools"`) {
		t.Errorf("output = %q, want it to mention the registered alias acme-tools", out)
	}
	src, ferr := marketplace.FindByName("acme-tools")
	if ferr != nil {
		t.Fatal(ferr)
	}
	if src == nil {
		t.Fatal("FindByName(acme-tools) = nil, want the newly added source")
	}
	if src.URL != dir {
		t.Errorf("registered URL = %q, want %q", src.URL, dir)
	}
}

// TestMarketplaceAdd_LocalPathPointingDirectlyToManifestFile covers mkt B5:
// `apm marketplace add ./dir/marketplace.json` (SOURCE naming the manifest
// file itself, not its containing directory) must read that exact file --
// not probe mkt-003's fallback candidates underneath the directory it lives
// in, which would find a *different* marketplace.json planted there.
func TestMarketplaceAdd_LocalPathPointingDirectlyToManifestFile(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := t.TempDir()
	manifestFile := filepath.Join(dir, "acme-marketplace.json")
	if err := os.WriteFile(manifestFile, []byte(`{"name": "acme-tools", "plugins": [{"name": "p", "source": "./p"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A decoy marketplace.json in the same directory: probing would find
	// this one first if the direct-file check were not honored.
	if err := os.WriteFile(filepath.Join(dir, "marketplace.json"), []byte(`{"name": "decoy"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "add", manifestFile)

	// Assert
	if err != nil {
		t.Fatalf("marketplace add returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, `"acme-tools"`) {
		t.Errorf("output = %q, want it to mention the registered alias acme-tools (not the decoy manifest's name)", out)
	}
	src, ferr := marketplace.FindByName("acme-tools")
	if ferr != nil {
		t.Fatal(ferr)
	}
	if src == nil {
		t.Fatal("FindByName(acme-tools) = nil, want the newly added source")
	}
	if src.Path != "" {
		t.Errorf("registered Path = %q, want empty (direct-file read mode)", src.Path)
	}
}

func TestMarketplaceAdd_ExplicitNameWinsOverManifestName(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme-tools"}`)

	// Act
	_, err := runMarketplaceCmd(t, "add", dir, "--name", "my-alias")

	// Assert
	if err != nil {
		t.Fatalf("marketplace add returned error: %v", err)
	}
	if src, _ := marketplace.FindByName("my-alias"); src == nil {
		t.Fatal("FindByName(my-alias) = nil, want the source registered under the explicit --name")
	}
	if src, _ := marketplace.FindByName("acme-tools"); src != nil {
		t.Error("FindByName(acme-tools) found an entry, want the manifest name unused when --name was given")
	}
}

func TestMarketplaceAdd_InvalidManifestNameWarnsAndFallsBackToRepoName(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "Not A Valid Alias!"}`)
	wantAlias := filepath.Base(dir)

	// Act
	out, err := runMarketplaceCmd(t, "add", dir)

	// Assert
	if err != nil {
		t.Fatalf("marketplace add returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "not a valid marketplace alias") {
		t.Errorf("output = %q, want a warning about the invalid manifest name", out)
	}
	if src, _ := marketplace.FindByName(wantAlias); src == nil {
		t.Fatalf("FindByName(%q) = nil, want the repo-name fallback to have been registered", wantAlias)
	}
}

func TestMarketplaceAdd_RejectsBareHTTP(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)

	// Act
	_, err := runMarketplaceCmd(t, "add", "http://example.com/repo")

	// Assert
	if err == nil {
		t.Fatal("marketplace add http://... returned no error, want a rejection (mkt-010 rule 2)")
	}
}

func TestMarketplaceAdd_HostConflictIsHardError(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)

	// Act
	_, err := runMarketplaceCmd(t, "add", "https://github.com/owner/repo", "--host", "gitlab.com")

	// Assert
	if err == nil {
		t.Fatal("marketplace add with a conflicting --host returned no error, want a hard error (mkt-011)")
	}
	sources, lerr := marketplace.LoadRegistry()
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(sources) != 0 {
		t.Errorf("LoadRegistry() = %+v, want nothing registered after a --host conflict error", sources)
	}
}

func TestMarketplaceAdd_RefFragmentConflictsWithRefFlag(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)

	// Act
	_, err := runMarketplaceCmd(t, "add", "https://github.com/owner/repo#v1", "--ref", "v2")

	// Assert
	if err == nil {
		t.Fatal("marketplace add with both a #ref fragment and --ref returned no error, want a hard error (mkt-018)")
	}
}

func TestMarketplaceAdd_RefAndBranchFlagsMutuallyExclusive(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)

	// Act
	_, err := runMarketplaceCmd(t, "add", "https://github.com/owner/repo", "--ref", "v1", "--branch", "v2")

	// Assert
	if err == nil {
		t.Fatal("marketplace add with both --ref and --branch returned no error, want a hard error")
	}
}

// TestMarketplaceAdd_SameNameSilentlyReplaces covers mkt-006 wired through
// the CLI: re-adding under a case-different existing name replaces in
// place, no error.
func TestMarketplaceAdd_SameNameSilentlyReplaces(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir1 := writeLocalManifestDir(t, `{"name": "v1"}`)
	dir2 := writeLocalManifestDir(t, `{"name": "v2"}`)

	// Act
	if _, err := runMarketplaceCmd(t, "add", dir1, "--name", "acme"); err != nil {
		t.Fatalf("first add returned error: %v", err)
	}
	if _, err := runMarketplaceCmd(t, "add", dir2, "--name", "ACME"); err != nil {
		t.Fatalf("second add (different case) returned error: %v", err)
	}

	// Assert
	sources, err := marketplace.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 {
		t.Fatalf("LoadRegistry() = %d entries, want 1 (same-name add must replace, not append)", len(sources))
	}
	if sources[0].URL != dir2 {
		t.Errorf("registered URL = %q, want %q (the replacement)", sources[0].URL, dir2)
	}
}

// TestMarketplaceAdd_PreservesUnrelatedRegistryEntries is AC3's "add" case:
// adding to a registry that already has other, unrelated entries (written
// directly to disk, not through this package) must not disturb them.
func TestMarketplaceAdd_PreservesUnrelatedRegistryEntries(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	writeMarketplaceRegistryFixture(t, unrelatedFixtureEntries())
	dir := writeLocalManifestDir(t, `{"name": "acme"}`)

	// Act
	if _, err := runMarketplaceCmd(t, "add", dir); err != nil {
		t.Fatalf("marketplace add returned error: %v", err)
	}

	// Assert
	sources, err := marketplace.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 3 {
		t.Fatalf("LoadRegistry() = %d entries, want 3 (2 unrelated + 1 new)", len(sources))
	}
	for i, want := range unrelatedFixtureEntries() {
		if sources[i] != want {
			t.Errorf("unrelated entry %d = %+v, want unchanged %+v", i, sources[i], want)
		}
	}
}

// ── `list` (mkt-012) ─────────────────────────────────────────────────────

func TestMarketplaceList_EmptyRegistry(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)

	// Act
	out, err := runMarketplaceCmd(t, "list")

	// Assert
	if err != nil {
		t.Fatalf("marketplace list returned error: %v", err)
	}
	if !strings.Contains(out, "No marketplaces registered") {
		t.Errorf("output = %q, want an empty-registry message", out)
	}
}

func TestMarketplaceList_TableIncludesEveryRegisteredSource(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	writeMarketplaceRegistryFixture(t, unrelatedFixtureEntries())

	// Act
	out, err := runMarketplaceCmd(t, "list")

	// Assert
	if err != nil {
		t.Fatalf("marketplace list returned error: %v", err)
	}
	for _, want := range []string{"unrelated-one", "unrelated-two", "NAME", "SOURCE", "REF", "PATH"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to contain %q", out, want)
		}
	}
}

// ── `browse` (mkt-013) ───────────────────────────────────────────────────

func TestMarketplaceBrowse_ForceRefreshesAndPrintsInstallHint(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [{"name": "cool-plugin", "description": "does things", "version": "1.0.0", "source": "./p"}]}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "browse", "acme")

	// Assert
	if err != nil {
		t.Fatalf("marketplace browse returned error: %v", err)
	}
	if !strings.Contains(out, "cool-plugin") {
		t.Errorf("output = %q, want it to list cool-plugin", out)
	}
	if !strings.Contains(out, "apm install cool-plugin@acme") {
		t.Errorf("output = %q, want a per-plugin install hint", out)
	}
	if !strings.Contains(out, "apm install <plugin-name>@acme") {
		t.Errorf("output = %q, want the generic install tip (mkt-013)", out)
	}
}

func TestMarketplaceBrowse_NotRegisteredErrors(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)

	// Act
	_, err := runMarketplaceCmd(t, "browse", "does-not-exist")

	// Assert
	if err == nil {
		t.Fatal("marketplace browse for an unregistered name returned no error")
	}
}

// ── `update` (mkt-014) ───────────────────────────────────────────────────

func TestMarketplaceUpdate_NamedRefreshesOne(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [{"name": "p", "source": "./p"}]}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "update", "acme")

	// Assert
	if err != nil {
		t.Fatalf("marketplace update acme returned error: %v", err)
	}
	if !strings.Contains(out, "acme") || !strings.Contains(out, "1 plugins") {
		t.Errorf("output = %q, want confirmation naming the marketplace and its plugin count", out)
	}
}

func TestMarketplaceUpdate_NamedNotRegisteredErrors(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)

	// Act
	_, err := runMarketplaceCmd(t, "update", "does-not-exist")

	// Assert
	if err == nil {
		t.Fatal("marketplace update for an unregistered name returned no error")
	}
}

// TestMarketplaceUpdate_AllContinuesPastOneFailure covers design.md's "任何
// 一個失敗記診斷、不中斷其餘": refreshing every registered marketplace must
// not abort just because one entry's source has since gone missing.
func TestMarketplaceUpdate_AllContinuesPastOneFailure(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	goodDir := writeLocalManifestDir(t, `{"name": "good", "plugins": [{"name": "p", "source": "./p"}]}`)
	brokenDir := filepath.Join(t.TempDir(), "does-not-exist")
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "broken", URL: brokenDir, Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "good", URL: goodDir, Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "update")

	// Assert
	if err != nil {
		t.Fatalf("marketplace update (all) returned error: %v, want it to continue past the broken entry", err)
	}
	if !strings.Contains(out, `Refreshed marketplace "good"`) {
		t.Errorf("output = %q, want the good marketplace refreshed despite the broken one", out)
	}
	if !strings.Contains(out, `failed to refresh marketplace "broken"`) {
		t.Errorf("output = %q, want a diagnostic for the broken marketplace", out)
	}
}

// ── `remove` (mkt-015) ───────────────────────────────────────────────────

func TestMarketplaceRemove_YesFlagSkipsConfirmation(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: "/abs/path", Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "remove", "acme", "-y")

	// Assert
	if err != nil {
		t.Fatalf("marketplace remove -y returned error: %v", err)
	}
	if src, _ := marketplace.FindByName("acme"); src != nil {
		t.Error("marketplace still registered after remove -y")
	}
}

// TestMarketplaceRemove_NonInteractiveWithoutYesFails covers mkt-015: with
// stdin forced non-interactive (matching a CI invocation), removal without
// -y must be a hard error, not a silent no-confirm removal.
func TestMarketplaceRemove_NonInteractiveWithoutYesFails(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	withNonInteractiveStdin(t)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: "/abs/path", Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "remove", "acme")

	// Assert
	if err == nil {
		t.Fatal("marketplace remove without -y in a non-interactive process returned no error (mkt-015)")
	}
	if src, _ := marketplace.FindByName("acme"); src == nil {
		t.Error("marketplace was removed despite the missing confirmation")
	}
}

func TestMarketplaceRemove_NotRegisteredErrors(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)

	// Act
	_, err := runMarketplaceCmd(t, "remove", "does-not-exist", "-y")

	// Assert
	if err == nil {
		t.Fatal("marketplace remove for an unregistered name returned no error")
	}
}

// TestMarketplaceRemove_PreservesUnrelatedEntries is AC3's "remove" case.
func TestMarketplaceRemove_PreservesUnrelatedEntries(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	writeMarketplaceRegistryFixture(t, unrelatedFixtureEntries())

	// Act
	if _, err := runMarketplaceCmd(t, "remove", "unrelated-one", "-y"); err != nil {
		t.Fatalf("marketplace remove returned error: %v", err)
	}

	// Assert
	sources, err := marketplace.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Name != "unrelated-two" {
		t.Errorf("LoadRegistry() = %+v, want only unrelated-two left", sources)
	}
}

// ── `validate` (mkt-016) ─────────────────────────────────────────────────

func TestMarketplaceValidate_HappyPathPrintsSummaryAndSucceeds(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [{"name": "p", "source": "./p"}]}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "validate", "acme")

	// Assert
	if err != nil {
		t.Fatalf("marketplace validate returned error for a valid manifest: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Summary: 2 passed, 0 warnings, 0 errors") {
		t.Errorf("output = %q, want the passing summary line", out)
	}
}

func TestMarketplaceValidate_ErrorsFailTheCommand(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [{"name": "dup", "source": "./a"}, {"name": "DUP", "source": "./b"}]}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "validate", "acme")

	// Assert
	if err == nil {
		t.Fatal("marketplace validate returned no error for a manifest with a duplicate plugin name (mkt-016)")
	}
	if !strings.Contains(out, "Summary:") || !strings.Contains(out, "1 errors") {
		t.Errorf("output = %q, want the Summary line to report 1 error", out)
	}
}

func TestMarketplaceValidate_NotRegisteredErrors(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)

	// Act
	_, err := runMarketplaceCmd(t, "validate", "does-not-exist")

	// Assert
	if err == nil {
		t.Fatal("marketplace validate for an unregistered name returned no error")
	}
}

// ── `build` tombstone (mkt-019) ──────────────────────────────────────────

func TestMarketplaceBuild_Tombstone(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)

	// Act
	_, err := runMarketplaceCmd(t, "build")

	// Assert
	if err == nil {
		t.Fatal("marketplace build returned no error, want the mkt-019 tombstone rejection")
	}
	if !strings.Contains(err.Error(), "apm pack") {
		t.Errorf("error = %v, want it to point at 'apm pack'", err)
	}
}

// ── Phase M5 negative assertions ────────────────────────────────────────

// TestMarketplaceCmd_PhaseM5AbsentSubcommands covers mkt-060 (search),
// mkt-061 (doctor), mkt-062 (publish), and mkt-064 (no "refresh" alias for
// update): none of these are real `apm marketplace` subcommands.
func TestMarketplaceCmd_PhaseM5AbsentSubcommands(t *testing.T) {
	// Arrange
	cmd := marketplaceCmd()
	forbidden := map[string]bool{"search": true, "doctor": true, "publish": true, "refresh": true}

	// Act / Assert
	for _, sub := range cmd.Commands() {
		if forbidden[sub.Name()] {
			t.Errorf("marketplace has a %q subcommand, want it absent (Phase M5)", sub.Name())
		}
	}
}

// TestMarketplaceBrowse_NoJSONFlag covers mkt-063: browse only accepts NAME
// and --verbose.
func TestMarketplaceBrowse_NoJSONFlag(t *testing.T) {
	cmd := marketplaceBrowseCmd()
	if cmd.Flags().Lookup("json") != nil {
		t.Error("marketplace browse has a --json flag, want it absent (mkt-063)")
	}
}

// TestMarketplaceValidate_NoCheckRefsFlag covers mkt-017: the upstream
// --check-refs flag was a placeholder that never did anything and is not
// ported.
func TestMarketplaceValidate_NoCheckRefsFlag(t *testing.T) {
	cmd := marketplaceValidateCmd()
	if cmd.Flags().Lookup("check-refs") != nil {
		t.Error("marketplace validate has a --check-refs flag, want it absent (mkt-017)")
	}
}
