package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/marketplace"
	"github.com/apm-go/apm/internal/ux"
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

// ── C5: `add --name`/`--host` validation ────────────────────────────────

// TestMarketplaceAdd_InvalidNameRejected covers C5's first half: an
// explicit --name that fails mkt-004's alias format (marketplaceAliasPattern,
// reused from resolveMarketplaceAlias's fallback path) must be rejected
// outright -- Python (__init__.py:621-628) exits 1 rather than storing a
// name that would break the "plugin@marketplace" install syntax.
func TestMarketplaceAdd_InvalidNameRejected(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme-tools"}`)

	// Act
	_, err := runMarketplaceCmd(t, "add", dir, "--name", "bad name!")

	// Assert
	if err == nil {
		t.Fatal("marketplace add --name 'bad name!' returned no error, want a rejection (C5)")
	}
	if !strings.Contains(err.Error(), `"bad name!"`) {
		t.Errorf("error = %q, want it to name the invalid value", err.Error())
	}
	sources, lerr := marketplace.LoadRegistry()
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(sources) != 0 {
		t.Errorf("LoadRegistry() = %+v, want nothing registered after an invalid --name rejection", sources)
	}
}

// TestMarketplaceAdd_ValidNameStillSucceeds is C5's regression guard: a
// --name that does pass the alias format must still register normally.
func TestMarketplaceAdd_ValidNameStillSucceeds(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme-tools"}`)

	// Act
	_, err := runMarketplaceCmd(t, "add", dir, "--name", "valid-name.2")

	// Assert
	if err != nil {
		t.Fatalf("marketplace add with a valid --name returned error: %v", err)
	}
	if src, _ := marketplace.FindByName("valid-name.2"); src == nil {
		t.Fatal("FindByName(valid-name.2) = nil, want the source registered under the valid --name")
	}
}

// TestMarketplaceAdd_InvalidHostRejected covers C5's second half: a
// malformed --host FQDN must be rejected before ever reaching the network,
// mirroring Python's is_valid_fqdn pre-check (__init__.py:565-570). Applies
// even to a local-path SOURCE (where --host is otherwise ignored) since
// Python's check runs unconditionally, before the "ignored" warnings.
func TestMarketplaceAdd_InvalidHostRejected(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme-tools"}`)

	// Act
	_, err := runMarketplaceCmd(t, "add", dir, "--host", "not a valid host!!")

	// Assert
	if err == nil {
		t.Fatal("marketplace add --host 'not a valid host!!' returned no error, want a rejection (C5)")
	}
	if !strings.Contains(err.Error(), `"not a valid host!!"`) {
		t.Errorf("error = %q, want it to name the invalid value", err.Error())
	}
	sources, lerr := marketplace.LoadRegistry()
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(sources) != 0 {
		t.Errorf("LoadRegistry() = %+v, want nothing registered after an invalid --host rejection", sources)
	}
}

// TestMarketplaceAdd_ValidHostStillSucceeds is C5's regression guard for a
// local-path SOURCE: a well-formed --host FQDN must not be rejected (it is
// merely ignored-with-warning for a local source, never a hard error).
func TestMarketplaceAdd_ValidHostStillSucceeds(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme-tools"}`)

	// Act
	_, err := runMarketplaceCmd(t, "add", dir, "--host", "example.com")

	// Assert
	if err != nil {
		t.Fatalf("marketplace add with a valid --host returned error: %v", err)
	}
	if src, _ := marketplace.FindByName("acme-tools"); src == nil {
		t.Fatal("FindByName(acme-tools) = nil, want the source registered despite the (ignored) valid --host")
	}
}

// ── C1: --verbose/-v on update/remove/validate ──────────────────────────

func TestMarketplaceUpdateCmd_HasVerboseFlag(t *testing.T) {
	cmd := marketplaceUpdateCmd()
	if cmd.Flags().Lookup("verbose") == nil {
		t.Error("marketplace update is missing --verbose (C1)")
	}
	if cmd.Flags().ShorthandLookup("v") == nil {
		t.Error("marketplace update is missing the -v shorthand for --verbose (C1)")
	}
}

func TestMarketplaceRemoveCmd_HasVerboseFlag(t *testing.T) {
	cmd := marketplaceRemoveCmd()
	if cmd.Flags().Lookup("verbose") == nil {
		t.Error("marketplace remove is missing --verbose (C1)")
	}
	if cmd.Flags().ShorthandLookup("v") == nil {
		t.Error("marketplace remove is missing the -v shorthand for --verbose (C1)")
	}
}

func TestMarketplaceValidateCmd_HasVerboseFlag(t *testing.T) {
	cmd := marketplaceValidateCmd()
	if cmd.Flags().Lookup("verbose") == nil {
		t.Error("marketplace validate is missing --verbose (C1)")
	}
	if cmd.Flags().ShorthandLookup("v") == nil {
		t.Error("marketplace validate is missing the -v shorthand for --verbose (C1)")
	}
}

// TestMarketplaceUpdate_VerboseFlagAccepted proves update's -v/--verbose
// parses without the "unknown flag" error C1 found, on both the named and
// long forms, and both the single-NAME and refresh-all code paths.
func TestMarketplaceUpdate_VerboseFlagAccepted(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [{"name": "p", "source": "./p"}]}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "update", "acme", "-v")

	// Assert
	if err != nil {
		t.Fatalf("marketplace update acme -v returned error: %v", err)
	}
	if !strings.Contains(out, "source: "+dir) {
		t.Errorf("output = %q, want -v's extra source line", out)
	}

	// --verbose (long form) and the refresh-all path too.
	if _, err := runMarketplaceCmd(t, "update", "--verbose"); err != nil {
		t.Fatalf("marketplace update --verbose returned error: %v", err)
	}
}

// TestMarketplaceRemove_VerboseFlagAccepted proves remove's -v/--verbose
// parses without erroring, alongside -y.
func TestMarketplaceRemove_VerboseFlagAccepted(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: "/abs/path", Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "remove", "acme", "-y", "--verbose")

	// Assert
	if err != nil {
		t.Fatalf("marketplace remove -y --verbose returned error: %v", err)
	}
	if !strings.Contains(out, "source: /abs/path") {
		t.Errorf("output = %q, want --verbose's extra source line", out)
	}
	if src, _ := marketplace.FindByName("acme"); src != nil {
		t.Error("marketplace still registered after remove -y --verbose")
	}
}

// TestMarketplaceValidate_VerboseFlagAccepted proves validate's -v/--verbose
// parses without erroring and prints the per-plugin source-type detail
// mirroring Python's validate.py:38-42.
func TestMarketplaceValidate_VerboseFlagAccepted(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [{"name": "p", "source": "./p"}]}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "validate", "acme", "-v")

	// Assert
	if err != nil {
		t.Fatalf("marketplace validate acme -v returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "p: source type: string") {
		t.Errorf("output = %q, want -v's per-plugin source-type line", out)
	}
}

// ── C10: EOF/non-interactive confirm read must never read as "declined" ──

// forceRich overrides richCheck (marketplace.go) so confirmOrRequireYes's
// "genuinely interactive" branch can be driven deterministically,
// independent of whatever the test process's real stdin/stderr happen to
// be -- ux.CanPrompt() itself cannot be forced true from outside the ux
// package, since the underlying TTY seams are unexported there. See
// TestConfirmOrRequireYes_ProductionCanPromptGate below for a test that
// drives the real ux.CanPrompt() gate end to end instead of stubbing this
// var directly.
func forceRich(t *testing.T, rich bool) {
	t.Helper()
	orig := richCheck
	richCheck = func() bool { return rich }
	t.Cleanup(func() { richCheck = orig })
}

// stubConfirm overrides confirmFn (marketplace.go), the seam
// confirmOrRequireYes uses in place of a direct ux.Confirm call, so a test
// can simulate a successful "yes"/"no" read or a failed prompt (the huh
// equivalent of C10's original EOF case) without a real terminal.
func stubConfirm(t *testing.T, fn func(prompt string, def bool) (bool, error)) {
	t.Helper()
	orig := confirmFn
	confirmFn = fn
	t.Cleanup(func() { confirmFn = orig })
}

func TestConfirmOrRequireYes_NonInteractive_RequiresYes(t *testing.T) {
	forceRich(t, false)

	proceed, err := confirmOrRequireYes("Remove?", "requires -y/--yes")

	if err == nil {
		t.Fatal("confirmOrRequireYes() err = nil for a non-interactive session, want the requires-yes error")
	}
	if proceed {
		t.Error("confirmOrRequireYes() proceed = true for a non-interactive session, want false")
	}
}

// TestConfirmOrRequireYes_RichButPromptFails is C10's core regression case
// reproduced against the huh-backed confirm: even when the session is
// genuinely rich, a failed confirmation prompt (huh aborted, equivalent to
// the original EOF case) must never be silently treated as "declined" -- a
// clean exit 0 -- which reads as success to a CI/script caller despite
// nothing having been removed. This proves it now requires -y/--yes
// instead, matching the outright-non-interactive path.
func TestConfirmOrRequireYes_RichButPromptFails(t *testing.T) {
	forceRich(t, true)
	stubConfirm(t, func(string, bool) (bool, error) { return false, errors.New("prompt aborted") })

	proceed, err := confirmOrRequireYes("Remove?", "requires -y/--yes")

	if err == nil {
		t.Fatal("confirmOrRequireYes() err = nil for a failed prompt even though richCheck() reported true, want the requires-yes error (C10)")
	}
	if proceed {
		t.Error("confirmOrRequireYes() proceed = true for a failed prompt, want false")
	}
	if !strings.Contains(err.Error(), "requires -y/--yes") {
		t.Errorf("err = %v, want it to mention requires -y/--yes", err)
	}
}

// TestConfirmOrRequireYes_InteractiveExplicitNo_CleanDecline proves the
// fix's boundary: a prompt that genuinely completes with "no" is still a
// clean, non-error decline -- only a failed prompt is now treated as
// requiring -y/--yes.
func TestConfirmOrRequireYes_InteractiveExplicitNo_CleanDecline(t *testing.T) {
	forceRich(t, true)
	stubConfirm(t, func(string, bool) (bool, error) { return false, nil })

	proceed, err := confirmOrRequireYes("Remove?", "requires -y/--yes")

	if err != nil {
		t.Fatalf("confirmOrRequireYes() err = %v for a genuine explicit decline, want nil (clean Aborted)", err)
	}
	if proceed {
		t.Error(`confirmOrRequireYes() proceed = true for an explicit "no", want false`)
	}
}

func TestConfirmOrRequireYes_InteractiveExplicitYes_Proceeds(t *testing.T) {
	forceRich(t, true)
	stubConfirm(t, func(string, bool) (bool, error) { return true, nil })

	proceed, err := confirmOrRequireYes("Remove?", "requires -y/--yes")

	if err != nil {
		t.Fatalf(`confirmOrRequireYes() err = %v for an explicit "yes", want nil`, err)
	}
	if !proceed {
		t.Error(`confirmOrRequireYes() proceed = false for an explicit "yes", want true`)
	}
}

// TestMarketplaceRemove_LooksInteractiveButEOF_RequiresYesAndDoesNotRemove
// is C10's full-CLI reproduction for `marketplace remove`: it must exit
// non-zero and must NOT remove the entry -- asserted directly against the
// registry, not just the exit code, matching the actual footgun (a script
// checking only the exit code would previously see 0).
func TestMarketplaceRemove_LooksInteractiveButEOF_RequiresYesAndDoesNotRemove(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	forceRich(t, true)
	stubConfirm(t, func(string, bool) (bool, error) { return false, errors.New("prompt aborted") })
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: "/abs/path", Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "remove", "acme")

	// Assert
	if err == nil {
		t.Fatal("marketplace remove with a failed confirmation prompt returned no error, want the requires -y/--yes error (C10)")
	}
	if src, _ := marketplace.FindByName("acme"); src == nil {
		t.Error("marketplace was removed despite the confirmation prompt failing (C10 footgun)")
	}
}

// TestMarketplaceRemove_InteractiveExplicitNo_AbortsCleanly is the CLI-level
// boundary case: a genuine interactive "n" is unaffected by the fix.
func TestMarketplaceRemove_InteractiveExplicitNo_AbortsCleanly(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	forceRich(t, true)
	stubConfirm(t, func(string, bool) (bool, error) { return false, nil })
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: "/abs/path", Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "remove", "acme")

	// Assert
	if err != nil {
		t.Fatalf(`marketplace remove with an explicit interactive "n" returned error: %v, want a clean exit 0 Cancelled`, err)
	}
	if !strings.Contains(out, "Cancelled") {
		t.Errorf("output = %q, want a Cancelled message", out)
	}
	if src, _ := marketplace.FindByName("acme"); src == nil {
		t.Error("marketplace was removed despite an explicit decline")
	}
}

// TestConfirmOrRequireYes_ProductionCanPromptGate_NonTTY_RequiresYes drives
// confirmOrRequireYes through the *real* production gate -- richCheck's
// actual default (ux.CanPrompt) and confirmFn's actual default (ux.Confirm)
// -- forcing only the underlying stdin/stderr TTY seams inside internal/ux
// via ux.SetTTYSeamsForTest. Every other test in this file proves
// confirmOrRequireYes's own branching by stubbing richCheck/confirmFn
// (forceRich/stubConfirm) directly, which says nothing about whether
// ux.CanPrompt() itself is wired to the right TTY signals; this test closes
// that gap and, since it never reaches ux.Confirm at all when CanPrompt()
// is false, also demonstrates the non-TTY path returns immediately instead
// of attempting to read stdin (no hang risk).
func TestConfirmOrRequireYes_ProductionCanPromptGate_NonTTY_RequiresYes(t *testing.T) {
	// Arrange: use the real richCheck/confirmFn (no forceRich/stubConfirm),
	// forcing only the ux-internal TTY seams to look non-interactive.
	restore := ux.SetTTYSeamsForTest(false, false, false)
	t.Cleanup(restore)

	// Act
	proceed, err := confirmOrRequireYes("Remove?", "requires -y/--yes")

	// Assert
	if err == nil {
		t.Fatal("confirmOrRequireYes() err = nil with the real CanPrompt gate forced non-TTY, want the requires-yes error")
	}
	if !strings.Contains(err.Error(), "requires -y/--yes") {
		t.Errorf("err = %v, want it to mention requires -y/--yes", err)
	}
	if proceed {
		t.Error("confirmOrRequireYes() proceed = true with the real CanPrompt gate forced non-TTY, want false")
	}
}

// TestMarketplaceRemove_ProductionNonTTY_RequiresYesAndDoesNotRemove is the
// full-CLI counterpart of the test above: `marketplace remove` without -y,
// run against the real richCheck/confirmFn (not stubbed), with only the
// ux-internal TTY seams forced non-interactive via ux.SetTTYSeamsForTest.
// It must fail fast and must not remove the entry -- proving the real
// integration between marketplace.go's confirmOrRequireYes and ux.CanPrompt
// works end to end, not just the local var seams other tests stub out.
func TestMarketplaceRemove_ProductionNonTTY_RequiresYesAndDoesNotRemove(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	restore := ux.SetTTYSeamsForTest(false, false, false)
	t.Cleanup(restore)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: "/abs/path", Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "remove", "acme")

	// Assert
	if err == nil {
		t.Fatal("marketplace remove with the real CanPrompt gate forced non-TTY returned no error, want the requires -y/--yes error")
	}
	if src, _ := marketplace.FindByName("acme"); src == nil {
		t.Error("marketplace was removed despite the real CanPrompt gate reporting non-TTY")
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

// TestMarketplaceBrowse_RendersPluginTable locks the mkt-013 表格呈現 shape.
// Since 07-14-init-tui-beautify, the box table is rendered by ux.Table
// (lipgloss/table), not the original's rich-parity HEAVY_HEAD box (design.md
// explicitly accepts this visual difference): a rounded "│"/"─" box border
// with a header/body separator row, instead of the original's "┃"/"│", but
// the same Plugin/Description/Version/Install columns, `--` placeholders,
// and a bare `<plugin>@<mkt>` Install cell (no command prefix) survive.
func TestMarketplaceBrowse_RendersPluginTable(t *testing.T) {
	// Arrange -- one fully-described plugin whose description is long enough
	// that it must word-wrap inside the table cell, plus one bare plugin
	// exercising the `--` placeholders.
	isolatedMarketplaceRegistry(t)
	longDesc := strings.TrimSpace(strings.Repeat("behavioral guidelines ", 8))
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [`+
		`{"name": "cool-plugin", "description": "`+longDesc+`", "version": "1.0.0", "source": "./p"},`+
		`{"name": "bare-plugin", "source": "./q"}]}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "browse", "acme")

	// Assert
	if err != nil {
		t.Fatalf("marketplace browse returned error: %v", err)
	}
	for _, want := range []string{
		"[i] Fetching plugins from 'acme'...",
		"Plugins in 'acme'",
		"│ Plugin",
		"│ cool-plugin",
		"cool-plugin@acme",
		"bare-plugin@acme",
		"--",
		"[i] Install a plugin: apm-go install <plugin-name>@acme",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to contain %q", out, want)
		}
	}
	// The Install CELL is bare `<plugin>@<mkt>`; only the footer carries a
	// command prefix.
	if strings.Contains(out, "install cool-plugin@acme") {
		t.Errorf("output = %q, want the Install cell without a command prefix", out)
	}
	// The long description wraps into a continuation row inside its own
	// cell (a table line still carrying description text but neither the
	// plugin name nor its Install cell) instead of a single unbroken line.
	wrapped := false
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "guidelines") && !strings.Contains(line, "cool-plugin") {
			wrapped = true
			break
		}
	}
	if !wrapped {
		t.Errorf("output = %q, want the long description to word-wrap into a continuation row", out)
	}
}

// The original warns and exits 0 without rendering a table when the
// marketplace has no plugins.
func TestMarketplaceBrowse_EmptyMarketplaceWarnsWithoutTable(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": []}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "browse", "acme")

	// Assert
	if err != nil {
		t.Fatalf("marketplace browse returned error: %v", err)
	}
	if !strings.Contains(out, "Marketplace 'acme' has no plugins") {
		t.Errorf("output = %q, want a no-plugins warning", out)
	}
	if strings.Contains(out, "┏") {
		t.Errorf("output = %q, want no table for an empty marketplace", out)
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

// TestMarketplaceUpdate_NotRegistered_MatchesOracleFixedFormat replaces the
// pre-ticket-14 SuggestsAliasFromOwnerRepo/NoSlashSkipsFuzzyMatch tests: the
// Oracle's MarketplaceNotFoundError (marketplace/errors.py:10-24, wrapped by
// commands/marketplace/__init__.py:1005's "Failed to update marketplace: "
// prefix) is a FIXED-FORMAT message that never varies by registration state
// -- no "Did you mean" fuzzy-alias hint, no "Registered: <list>"
// enumeration, even when another marketplace happens to be registered under
// the derived alias of the queried OWNER/REPO string.
func TestMarketplaceUpdate_NotRegistered_MatchesOracleFixedFormat(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "ponytail", URL: "/abs/ponytail", Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "update", "DietrichGebert/ponytail")

	// Assert
	if err == nil {
		t.Fatal("marketplace update DietrichGebert/ponytail returned no error, want a not-registered error")
	}
	want := "Failed to update marketplace: Marketplace 'DietrichGebert/ponytail' is not registered. " +
		"Run 'apm-go marketplace add https://github.com/OWNER/REPO' or " +
		"'apm-go marketplace add OWNER/REPO' to register it, or " +
		"'apm-go marketplace list' to see registered marketplaces."
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// TestMarketplaceUpdate_NotRegistered_EmptyRegistryHintsAddCommand covers the
// case where nothing at all is registered: the error should point the user
// at `marketplace add` instead of an empty "Registered:" list.
func TestMarketplaceUpdate_NotRegistered_EmptyRegistryHintsAddCommand(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)

	// Act
	_, err := runMarketplaceCmd(t, "update", "whatever")

	// Assert
	if err == nil {
		t.Fatal("marketplace update whatever returned no error, want a not-registered error")
	}
	if !strings.Contains(err.Error(), "is not registered") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "is not registered")
	}
	if !strings.Contains(err.Error(), "apm-go marketplace add") {
		t.Errorf("error = %q, want it to point at 'apm-go marketplace add' when nothing is registered", err.Error())
	}
}

// TestMarketplaceUpdate_RegisteredAliasStillWorks is the regression guard
// for the fix above: querying by the alias it actually registered under
// (not the raw OWNER/REPO string) must still refresh normally, unaffected by
// the new not-registered error path.
func TestMarketplaceUpdate_RegisteredAliasStillWorks(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "ponytail", "plugins": [{"name": "p", "source": "./p"}]}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "ponytail", URL: dir, Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "update", "ponytail")

	// Assert
	if err != nil {
		t.Fatalf("marketplace update ponytail returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, `Refreshed marketplace "ponytail"`) {
		t.Errorf("output = %q, want confirmation that ponytail was refreshed", out)
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

// TestMarketplaceRemove_NotRegistered_DoesNotRemove covers the remove case
// where only the derived alias is registered: `remove OWNER/REPO` must not
// remove the alias entry it never actually matched (the raw OWNER/REPO
// string itself was never found). Ticket 14 dropped the "Did you mean" hint
// this test used to also assert on -- the Oracle's MarketplaceNotFoundError
// never included one (marketplace/errors.py:10-24).
func TestMarketplaceRemove_NotRegistered_DoesNotRemove(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "ponytail", URL: "/abs/ponytail", Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "remove", "DietrichGebert/ponytail", "-y")

	// Assert
	if err == nil {
		t.Fatal("marketplace remove DietrichGebert/ponytail returned no error, want a not-registered error")
	}
	if !strings.Contains(err.Error(), "is not registered") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "is not registered")
	}
	if src, _ := marketplace.FindByName("ponytail"); src == nil {
		t.Error("ponytail was removed despite the raw OWNER/REPO string never matching a registered name")
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
	if !strings.Contains(out, "Summary: 3 passed, 0 warnings, 0 errors") {
		t.Errorf("output = %q, want the passing summary line", out)
	}
	// Upstream validate.py:54-63: passing checks each print their own line
	// under a "Validation Results:" header, and the fetch is bracketed by
	// progress lines -- a clean manifest must not collapse to a bare Summary.
	// Ticket 11: Structure joins Schema/Names as a third passing check, in
	// that order, each line reading "  <name>: passed" (validate.py's own
	// f"  {check_name}: passed" literal, not "all plugins valid").
	for _, want := range []string{
		`Validating marketplace 'acme'...`,
		"Found 1 plugins",
		"Validation Results:",
		"  Structure: passed",
		"  Schema: passed",
		"  Names: passed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to contain %q", out, want)
		}
	}
}

// TestMarketplaceValidate_HeaderAndPassRows_ExactOracleBytes is ticket 22
// AC7's byte-exact regression: the header line uses the Oracle's "[*]" glyph
// (validate.py:29's logger.start(..., symbol="gear"), NOT "[i]") and single
// quotes (NOT the %q double-quote apm-go used to render), and each passing
// check row uses the Oracle's "[+]" glyph (validate.py:66's
// logger.success(..., symbol="check")), preserving the exact three-space gap
// before the check name -- not ux.Success's centered " + " convention.
func TestMarketplaceValidate_HeaderAndPassRows_ExactOracleBytes(t *testing.T) {
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
	for _, line := range []string{
		`[*] Validating marketplace 'acme'...`,
		"[+]   Structure: passed",
		"[+]   Schema: passed",
		"[+]   Names: passed",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("output = %q, want it to contain the exact line %q", out, line)
		}
	}
	for _, notWant := range []string{
		`Validating marketplace "acme"`, // the old %q double-quote rendering
		"[i] Validating marketplace",    // the old ux.Info glyph
	} {
		if strings.Contains(out, notWant) {
			t.Errorf("output = %q, want it NOT to contain the pre-ticket-22 rendering %q", out, notWant)
		}
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

// TestMarketplaceValidate_StructureCheckFailsOnBrokenManifest is ticket
// 11's core case: a "plugins" value that isn't a JSON array is a Structure
// error (models.py:595/validator.go's Structure check), reported with the
// Oracle's own message, and -- validate.py:31-42/60-64 -- suppresses the
// plugin count, the verbose per-plugin detail, and every OTHER check's
// passing line, since a broken manifest can't meaningfully say "N plugins"
// or "Schema: passed".
func TestMarketplaceValidate_StructureCheckFailsOnBrokenManifest(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": "oops"}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "validate", "acme")

	// Assert
	if err == nil {
		t.Fatal("marketplace validate returned no error for a manifest with plugins not a list")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", exitCodeOf(err))
	}
	for _, want := range []string{
		"  Structure: plugins: expected a list",
		"Summary: 0 passed, 0 warnings, 1 errors",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to contain %q", out, want)
		}
	}
	for _, notWant := range []string{"Found ", "Schema:", "Names:"} {
		if strings.Contains(out, notWant) {
			t.Errorf("output = %q, must not contain %q once Structure failed", out, notWant)
		}
	}
}

// TestMarketplaceValidate_StructurePerElementDiagnostics is ticket 11 eval
// attempt 2's two blocking reproducers, at the cobra command level: an
// invalid array element used to pass silently (Structure/Schema/Names all
// "passed", exit 0) because parseManifestPlugins only ported the two
// top-level "plugins" diagnostics, not _parse_plugin_entry's own per-field
// ones. Both must now report the Oracle's exact Structure message and
// exit 1, with every other check's row suppressed (has_structure_errors).
func TestMarketplaceValidate_StructurePerElementDiagnostics(t *testing.T) {
	tests := []struct {
		name        string
		manifest    string
		wantMessage string
	}{
		{
			name:        "eval reproducer 1: a null plugin element",
			manifest:    `{"name": "acme", "plugins": [null]}`,
			wantMessage: "  Structure: plugins[0]: expected an object",
		},
		{
			name:        "eval reproducer 2: an empty object plugin element",
			manifest:    `{"name": "acme", "plugins": [{}]}`,
			wantMessage: "  Structure: plugins[0].name: expected a non-empty string",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			isolatedMarketplaceRegistry(t)
			dir := writeLocalManifestDir(t, tt.manifest)
			if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json"}); err != nil {
				t.Fatal(err)
			}

			// Act
			out, err := runMarketplaceCmd(t, "validate", "acme")

			// Assert
			if err == nil {
				t.Fatalf("marketplace validate returned no error for %s", tt.manifest)
			}
			if exitCodeOf(err) != 1 {
				t.Errorf("exitCodeOf(err) = %d, want 1", exitCodeOf(err))
			}
			for _, want := range []string{tt.wantMessage, "Summary: 0 passed, 0 warnings, 1 errors"} {
				if !strings.Contains(out, want) {
					t.Errorf("output = %q, want it to contain %q", out, want)
				}
			}
			for _, notWant := range []string{"Found ", "Schema:", "Names:"} {
				if strings.Contains(out, notWant) {
					t.Errorf("output = %q, must not contain %q once Structure failed", out, notWant)
				}
			}
		})
	}
}

// TestMarketplaceValidate_TagPatternDeferral is ticket 11 eval attempt 3's
// third blocking reproducer, PLUS the requested blast-radius check: a
// malformed source.tag_pattern is a per-element Structure diagnostic
// (models.py:521-533), not a whole-document parse failure. Before this
// fix, `marketplace add` itself failed while parsing (parsePluginEntry
// returned a hard error), so the marketplace was never registered at all
// -- browse/search/install for EVERY plugin in that marketplace, including
// perfectly valid ones, failed too. Verified against the real pinned
// Oracle directly: `marketplace add` registers successfully (with a
// CommandLogger warning naming the malformed-entry count), `browse` and
// root `search` both list only the valid plugin, `install` succeeds for
// the valid plugin and fails (not-found, same category on both sides) for
// the malformed one, and `validate` reports the Structure diagnostic.
//
// Ticket 11 eval attempt 4 correction: attempt 3's version of this test
// called marketplace.AddSource directly and never exercised search or
// install, despite the doc comment above already claiming that coverage --
// the eval verified the real behavior matches and asked for the test to
// actually do what its comment says. It now runs the real `marketplace
// add` and root `search` COBRA COMMANDS end to end.
//
// Ticket 11 eval attempt 5 correction: the eval flagged that "install"
// above overstated its own coverage too -- the install assertion below
// calls the internal runInstall function (with a mocked, network-free
// loader/tag-lister) rather than the root Cobra `install` command's RunE.
// This is deliberate, not fixed to use the real command: installCmd()'s
// RunE hardcodes gitops.RealTagLister/RealPackageLoader with no seam to
// inject the same mocks, and "good-plugin"'s source (type: github, repo:
// acme/good) is not a real repository -- routing this test through the
// real command would mean an actual network git-clone attempt against a
// nonexistent repo, trading a fast, hermetic unit test for a flaky,
// network-dependent one. The resolver path runInstall exercises
// (marketplace.ResolvePlugin -> manifest.ParseDepString -> lockfile
// write) is the behavior that actually matters here and already agrees
// with the isolated Oracle/target command-level probe recorded in this
// function's own top comment.
func TestMarketplaceValidate_TagPatternDeferral(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	mktDir := writeLocalManifestDir(t, `{"name": "broken", "plugins": [`+
		`{"name": "good-plugin", "source": {"type": "github", "repo": "acme/good"}},`+
		`{"name": "bad-plugin", "source": {"type": "github", "repo": "acme/bad", "tag_pattern": "{name}"}}`+
		`]}`)

	// Act: registering the marketplace through the real `marketplace add`
	// command must succeed despite the malformed entry (the blast-radius
	// regression this fix closes).
	addOut, addErr := runMarketplaceCmd(t, "add", mktDir, "--name", "broken")
	if addErr != nil {
		t.Fatalf("marketplace add returned error: %v (output: %s)", addErr, addOut)
	}

	// Assert -- browse.
	browseOut, browseErr := runMarketplaceCmd(t, "browse", "broken")
	if browseErr != nil {
		t.Fatalf("marketplace browse returned error: %v", browseErr)
	}
	if !strings.Contains(browseOut, "good-plugin") {
		t.Errorf("browse output = %q, want it to list the valid plugin", browseOut)
	}
	if strings.Contains(browseOut, "bad-plugin") {
		t.Errorf("browse output = %q, must NOT list the tag_pattern-malformed plugin", browseOut)
	}

	// Assert -- validate reports the Structure diagnostic.
	validateOut, validateErr := runMarketplaceCmd(t, "validate", "broken")
	if validateErr == nil {
		t.Fatal("marketplace validate returned no error for a manifest with a malformed tag_pattern")
	}
	if exitCodeOf(validateErr) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", exitCodeOf(validateErr))
	}
	wantMessage := "  Structure: plugins[1].source.tag_pattern: 'Plugin 'bad-plugin' source.tag_pattern' must contain exactly one {version} placeholder, got '{name}'"
	if !strings.Contains(validateOut, wantMessage) {
		t.Errorf("validate output = %q, want it to contain %q", validateOut, wantMessage)
	}

	// Assert -- install: runInstall (the root `install` command's own
	// implementation function, NOT the Cobra command layer -- see this
	// function's top comment) resolves and installs the valid plugin via
	// a mocked, network-free loader/tag lister (same pattern as
	// TestRunInstall_MarketplacePackage_LockfileProvenanceAndPersistedCanonical);
	// the malformed one is simply never found, the same "not found in
	// marketplace" category on both apm-go and the Oracle (verified
	// directly -- different exact wording, not a divergence this ticket is
	// positioned to close). Run BEFORE root `search` below: runSearchCmd
	// sets CI=1 for the rest of this test via t.Setenv, which would
	// otherwise make install default to a frozen (lockfile-required) mode
	// it isn't set up for here.
	projDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)
	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	if err := runInstall(deps, false, true, "claude", nil, []string{"good-plugin@broken"}); err != nil {
		t.Errorf("install of the valid plugin failed: %v", err)
	}
	if err := runInstall(deps, false, true, "claude", nil, []string{"bad-plugin@broken"}); err == nil {
		t.Error("install of the tag_pattern-malformed plugin succeeded, want a not-found error")
	}

	// Assert -- root `search` (a separate top-level command tree from
	// `marketplace browse`, sharing the same parsed manifest) also excludes
	// the malformed entry.
	searchOut, searchErr := runSearchCmd(t, "plugin@broken")
	if searchErr != nil {
		t.Fatalf("search returned error: %v", searchErr)
	}
	if !strings.Contains(searchOut, "good-plugin") {
		t.Errorf("search output = %q, want it to list the valid plugin", searchOut)
	}
	if strings.Contains(searchOut, "bad-plugin") {
		t.Errorf("search output = %q, must NOT list the tag_pattern-malformed plugin", searchOut)
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
	if !strings.Contains(err.Error(), "apm-go pack") {
		t.Errorf("error = %v, want it to point at 'apm-go pack'", err)
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

// ── `validate --check-refs` hidden no-op (ticket 06) ─────────────────────

// TestMarketplaceValidate_CheckRefsFlagIsHidden proves --check-refs parses
// (accepted) but is marked hidden, so it never appears in --help output
// (upstream validate.py:16-18's own `hidden=True`).
func TestMarketplaceValidate_CheckRefsFlagIsHidden(t *testing.T) {
	cmd := marketplaceValidateCmd()
	f := cmd.Flags().Lookup("check-refs")
	if f == nil {
		t.Fatal("marketplace validate has no --check-refs flag, want it accepted (ticket 06)")
	}
	if !f.Hidden {
		t.Error("--check-refs flag is not marked Hidden")
	}

	var helpBuf bytes.Buffer
	cmd.SetOut(&helpBuf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("validate --help: %v", err)
	}
	if strings.Contains(helpBuf.String(), "check-refs") {
		t.Errorf("--help output = %q, want it to omit the hidden --check-refs flag", helpBuf.String())
	}
}

// TestMarketplaceValidate_CheckRefsPrintsPlaceholderWarning proves
// --check-refs is accepted without error, emits the exact upstream
// validate.py:51-54 warning, and otherwise produces byte-identical output
// to the same invocation without the flag once that one line is removed --
// no ref lookup or network call changes any other output line, since it
// performs neither.
func TestMarketplaceValidate_CheckRefsPrintsPlaceholderWarning(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [{"name": "p", "source": "./p"}]}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	withFlag, errWith := runMarketplaceCmd(t, "validate", "acme", "--check-refs")
	withoutFlag, errWithout := runMarketplaceCmd(t, "validate", "acme")

	// Assert
	if errWith != nil {
		t.Fatalf("marketplace validate --check-refs returned error: %v (output: %s)", errWith, withFlag)
	}
	if errWithout != nil {
		t.Fatalf("marketplace validate (no flag) returned error: %v (output: %s)", errWithout, withoutFlag)
	}

	const warning = "Ref checking not yet implemented -- skipping ref reachability checks"
	if !strings.Contains(withFlag, warning) {
		t.Errorf("--check-refs output = %q, want it to contain the exact upstream warning %q", withFlag, warning)
	}
	if strings.Contains(withoutFlag, warning) {
		t.Errorf("output without --check-refs unexpectedly contains the placeholder warning: %q", withoutFlag)
	}
	assertLineSeverity(t, withFlag, warning, ux.SymbolWarn)

	// Every other line must be identical: strip the warning line (and its
	// Oracle-mirrored bracket prefix, "[!] " -- see assertLineSeverity) and
	// diff what remains against the same invocation without the flag.
	warningLine := "[" + ux.SymbolWarn + "] " + warning + "\n"
	strippedWith := strings.Replace(withFlag, warningLine, "", 1)
	if strippedWith != withoutFlag {
		t.Errorf("output with --check-refs (warning line removed) = %q, want identical to without the flag %q", strippedWith, withoutFlag)
	}
}

// buildRecordingFakeGit compiles a stand-in "git" executable that, on every
// invocation, appends its arguments as one line to the file named by the
// GIT_RECORD_FILE env var and exits 0. It returns the directory to prepend
// to PATH. This is deliberately its own tiny fake local to this test file
// (not internal/gitops/testdata/fakegit, which this ticket does not touch)
// since proving a negative -- git is NEVER invoked -- needs an invocation
// log, not just controllable output.
func buildRecordingFakeGit(t *testing.T) (dir string) {
	t.Helper()
	dir = t.TempDir()
	const program = `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	if rec := os.Getenv("GIT_RECORD_FILE"); rec != "" {
		f, err := os.OpenFile(rec, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintln(f, strings.Join(os.Args[1:], " "))
			f.Close()
		}
	}
	os.Exit(0)
}
`
	src := filepath.Join(dir, "fakegit_main.go")
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		t.Fatalf("writing fake git source: %v", err)
	}

	name := "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", out, src)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build fake git: %v\n%s", err, output)
	}
	return dir
}

// TestMarketplaceValidate_CheckRefsPerformsNoGitOrNetworkCall proves
// --check-refs does exactly what it claims: no ref lookup, no network call.
// A recording fake "git" is prepended to PATH and must never be invoked;
// the marketplace itself is registered via the local-file fetch path (a
// plain directory URL, no git/HTTP transport at all), so a genuine network
// call would have nothing to route through either.
func TestMarketplaceValidate_CheckRefsPerformsNoGitOrNetworkCall(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [{"name": "p", "source": "./p"}]}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	fakeGitDir := buildRecordingFakeGit(t)
	recordFile := filepath.Join(t.TempDir(), "git-invocations.log")
	t.Setenv("PATH", fakeGitDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_RECORD_FILE", recordFile)

	// Act
	out, err := runMarketplaceCmd(t, "validate", "acme", "--check-refs")

	// Assert
	if err != nil {
		t.Fatalf("marketplace validate --check-refs returned error: %v (output: %s)", err, out)
	}
	if data, statErr := os.ReadFile(recordFile); statErr == nil {
		t.Errorf("git was invoked during --check-refs (it performs no ref lookup): %s", data)
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("reading git invocation record: %v", statErr)
	}
}

// TestMarketplaceUpdate_EmptyRegistryReportsInsteadOfSilence covers upstream
// __init__.py:980-982: `marketplace update` with nothing registered must say
// so, never exit 0 with zero output.
func TestMarketplaceUpdate_EmptyRegistryReportsInsteadOfSilence(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)

	// Act
	out, err := runMarketplaceCmd(t, "update")

	// Assert
	if err != nil {
		t.Fatalf("marketplace update with an empty registry returned error: %v", err)
	}
	if !strings.Contains(out, "No marketplaces registered") {
		t.Errorf("output = %q, want the empty-registry notice instead of silence", out)
	}
}

// TestMarketplaceUpdate_AllPrintsStartAndClosingLines covers upstream
// __init__.py:983/:993: the refresh-all path is bracketed by a start line
// carrying the count and a closing "cache refreshed" line.
func TestMarketplaceUpdate_AllPrintsStartAndClosingLines(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [{"name": "p", "source": "./p"}]}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "update")

	// Assert
	if err != nil {
		t.Fatalf("marketplace update returned error: %v", err)
	}
	for _, want := range []string{"Refreshing 1 marketplace(s)...", "Marketplace cache refreshed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to contain %q", out, want)
		}
	}
}

// TestMarketplaceList_PrintsBrowseHintAfterTable covers upstream
// __init__.py:883-886's post-table usage hint.
func TestMarketplaceList_PrintsBrowseHintAfterTable(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: "/abs/path", Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "list")

	// Assert
	if err != nil {
		t.Fatalf("marketplace list returned error: %v", err)
	}
	if !strings.Contains(out, "marketplace browse <name>") {
		t.Errorf("output = %q, want the browse usage hint after the table", out)
	}
}

// TestMarketplaceAdd_SuccessLineCarriesPluginCount covers upstream
// __init__.py:721-724: the default (non-verbose) success line reports how
// many plugins the registered marketplace serves.
func TestMarketplaceAdd_SuccessLineCarriesPluginCount(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [{"name": "p", "source": "./p"}, {"name": "q", "source": "./q"}]}`)

	// Act
	out, err := runMarketplaceCmd(t, "add", dir)

	// Assert
	if err != nil {
		t.Fatalf("marketplace add returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, `Marketplace "acme" registered (2 plugins)`) {
		t.Errorf("output = %q, want the registered line with the plugin count", out)
	}
}
