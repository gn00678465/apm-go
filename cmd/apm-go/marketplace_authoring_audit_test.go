package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/marketplace"
	"github.com/apm-go/apm/internal/marketplace/authoring"
)

// fakeCmdApmYMLFetcher is a cmd-layer test double for
// authoring.ApmYMLFetcher, swapped into authoring.DefaultApmYMLFetcher for
// the duration of a test (mirrors client_github_test.go's
// withGitHubAPIBase-style package-var redirection, applied one level up
// since audit's production fetch target -- a live GitHub Contents API -- has
// no local-repo equivalent to exercise for real, the way refcheck.go's `git
// ls-remote` does against a t.TempDir() git fixture).
type fakeCmdApmYMLFetcher struct {
	responses map[string][]byte
	errs      map[string]error
}

func (f *fakeCmdApmYMLFetcher) FetchRaw(host, owner, repo, path, ref string) ([]byte, error) {
	k := host + ":" + owner + "/" + repo + "/" + path + "@" + ref
	if err, ok := f.errs[k]; ok {
		return nil, err
	}
	if data, ok := f.responses[k]; ok {
		return data, nil
	}
	return nil, authoring.ErrApmYMLNotFound
}

// withApmYMLFetcher swaps authoring.DefaultApmYMLFetcher for the duration of
// a test, restoring the real production fetcher afterward.
func withApmYMLFetcher(t *testing.T, f authoring.ApmYMLFetcher) {
	t.Helper()
	orig := authoring.DefaultApmYMLFetcher
	authoring.DefaultApmYMLFetcher = f
	t.Cleanup(func() { authoring.DefaultApmYMLFetcher = orig })
}

func githubSourcePlugin(name, repo string) marketplace.MarketplacePlugin {
	return marketplace.MarketplacePlugin{Name: name, Source: map[string]any{"type": "github", "repo": repo}}
}

// ── flags wired ───────────────────────────────────────────────────────────

func TestMarketplaceAuditCmd_FlagsWired(t *testing.T) {
	cmd := marketplaceAuditCmd()
	for _, name := range []string{"strict", "verbose"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("marketplace audit is missing --%s", name)
		}
	}
	if cmd.Flags().ShorthandLookup("v") == nil {
		t.Error("marketplace audit is missing the -v shorthand for --verbose")
	}
}

func TestMarketplaceAudit_NotRegisteredErrors(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)

	// Act
	_, err := runMarketplaceCmd(t, "audit", "does-not-exist")

	// Assert
	if err == nil {
		t.Fatal("marketplace audit for an unregistered name returned no error")
	}
}

// TestMarketplaceAudit_NotRegistered_MentionsAddAndListRemedies is R6/AC22:
// the error must name the concrete remedy commands (`marketplace add` to
// register a source, `marketplace list` to see what is registered), not
// just say "is not registered" and stop -- both with and without any other
// marketplace already registered, since before this fix the "Registered:
// ..." branch (at least one other marketplace registered) named the list
// but never the commands themselves.
func TestMarketplaceAudit_NotRegistered_MentionsAddAndListRemedies(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "foo", URL: "/abs/foo", Path: "marketplace.json"}); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "audit", "does-not-exist")

	// Assert
	if err == nil {
		t.Fatal("marketplace audit for an unregistered name returned no error")
	}
	if !strings.Contains(err.Error(), "marketplace add") {
		t.Errorf("error = %q, want it to mention the `marketplace add` remedy", err.Error())
	}
	if !strings.Contains(err.Error(), "marketplace list") {
		t.Errorf("error = %q, want it to mention the `marketplace list` remedy", err.Error())
	}
}

// TestMarketplaceAudit_NotRegistered_EmptyRegistry_MentionsAddAndListRemedies
// covers the other branch: nothing at all registered yet.
func TestMarketplaceAudit_NotRegistered_EmptyRegistry_MentionsAddAndListRemedies(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)

	// Act
	_, err := runMarketplaceCmd(t, "audit", "does-not-exist")

	// Assert
	if err == nil {
		t.Fatal("marketplace audit for an unregistered name returned no error")
	}
	if !strings.Contains(err.Error(), "marketplace add") {
		t.Errorf("error = %q, want it to mention the `marketplace add` remedy", err.Error())
	}
	if !strings.Contains(err.Error(), "marketplace list") {
		t.Errorf("error = %q, want it to mention the `marketplace list` remedy", err.Error())
	}
}

// ── happy path: clean marketplace, no --strict needed ────────────────────

func TestMarketplaceAudit_AllCleanDeps_Succeeds(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [`+
		`{"name": "clean", "source": {"type": "github", "repo": "acme/clean", "ref": "v1.0.0"}}`+
		`]}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json", Host: "github.com"}); err != nil {
		t.Fatal(err)
	}
	withApmYMLFetcher(t, &fakeCmdApmYMLFetcher{responses: map[string][]byte{
		"github.com:acme/clean/apm.yml@v1.0.0": []byte("name: clean\nversion: 1.0.0\ndependencies:\n  apm:\n    - name: ok\n      marketplace: acme\n"),
	}})

	// Act
	out, err := runMarketplaceCmd(t, "audit", "acme")

	// Assert
	if err != nil {
		t.Fatalf("marketplace audit returned error for an all-clean marketplace: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "1 clean") {
		t.Errorf("output = %q, want it to report 1 clean plugin", out)
	}
}

// ── bypass found: --strict flips exit code, plain run does not ───────────

func TestMarketplaceAudit_BypassFound_OnlyStrictFails(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [`+
		`{"name": "leaky", "source": {"type": "github", "repo": "acme/leaky", "ref": "v1.0.0"}}`+
		`]}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json", Host: "github.com"}); err != nil {
		t.Fatal(err)
	}
	withApmYMLFetcher(t, &fakeCmdApmYMLFetcher{responses: map[string][]byte{
		"github.com:acme/leaky/apm.yml@v1.0.0": []byte("name: leaky\nversion: 1.0.0\ndependencies:\n  apm:\n    - owner/repo#v1\n"),
	}})

	// Act: without --strict, a bypass warning must not fail the command.
	out, err := runMarketplaceCmd(t, "audit", "acme")

	// Assert
	if err != nil {
		t.Fatalf("marketplace audit (no --strict) returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "owner/repo#v1") {
		t.Errorf("output = %q, want it to list the bypassing dependency", out)
	}
	if strings.Contains(out, "owner/repo#v1@acme") {
		t.Errorf("output = %q, want the suggestion to not use the string-shorthand form", out)
	}

	// Act: with --strict, the same bypass must fail the command.
	out, err = runMarketplaceCmd(t, "audit", "acme", "--strict")

	// Assert
	if err == nil {
		t.Fatalf("marketplace audit --strict returned no error for a bypass finding (output: %s)", out)
	}
}

// ── strict counts NETWORK/PARSE, not NO_MANIFEST/UNSUPPORTED_SOURCE ──────

// v0.28.0 (PR #2460) reversed the old "skipped/404 never trip --strict"
// rule: a strict audit that verified NOTHING (zero clean, zero bypass) now
// exits 1, because it cannot claim supply-chain integrity.
func TestMarketplaceAudit_Strict_NothingAudited_FailsWithHint(t *testing.T) {
	// Arrange: one skipped plugin (no local apm.yml), one 404 -- zero
	// plugins actually audited.
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [`+
		`{"name": "unsupported", "source": "./relative"},`+
		`{"name": "no-manifest", "source": {"type": "github", "repo": "acme/gone", "ref": "v1.0.0"}}`+
		`]}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json", Host: "github.com"}); err != nil {
		t.Fatal(err)
	}
	withApmYMLFetcher(t, &fakeCmdApmYMLFetcher{})

	// Act
	out, err := runMarketplaceCmd(t, "audit", "acme", "--strict")

	// Assert
	if err == nil {
		t.Fatalf("marketplace audit --strict with zero audited plugins returned no error, want exit 1 (v0.28.0) (output: %s)", out)
	}
	if !strings.Contains(err.Error(), "no plugins were audited") {
		t.Errorf("error = %q, want the 'no plugins were audited' explanation", err)
	}
	if !strings.Contains(out, "--strict --verbose") {
		t.Errorf("output = %q, want the '--strict --verbose' hint for skipped reasons", out)
	}
}

// v0.28.0: --strict also fails when anything was skipped, even if other
// plugins audited clean -- a partial audit is not a complete one.
func TestMarketplaceAudit_Strict_SkippedPluginsFail_EvenWithCleanOnes(t *testing.T) {
	// Arrange: one clean local plugin + one skipped (404 github dict).
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [`+
		`{"name": "local-clean", "source": "./local-clean"},`+
		`{"name": "no-manifest", "source": {"type": "github", "repo": "acme/gone", "ref": "v1.0.0"}}`+
		`]}`)
	if err := os.MkdirAll(filepath.Join(dir, "local-clean"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "local-clean", "apm.yml"), []byte("name: c\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json", Host: "github.com"}); err != nil {
		t.Fatal(err)
	}
	withApmYMLFetcher(t, &fakeCmdApmYMLFetcher{})

	// Act
	out, err := runMarketplaceCmd(t, "audit", "acme", "--strict")

	// Assert
	if err == nil {
		t.Fatalf("marketplace audit --strict with a skipped plugin returned no error, want exit 1 (v0.28.0) (output: %s)", out)
	}
	if !strings.Contains(err.Error(), "skipped") {
		t.Errorf("error = %q, want the skipped-plugins explanation", err)
	}
}

// v0.28.0 (PR #2460): a LOCAL marketplace's string-source plugins are
// genuinely audited against their on-disk apm.yml -- a bypass dependency in
// one must surface, not be skipped as an unaddressable source.
func TestMarketplaceAudit_LocalMarketplace_AuditsStringSourcePlugins(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [`+
		`{"name": "local-bypass", "source": "./local-bypass"}`+
		`]}`)
	if err := os.MkdirAll(filepath.Join(dir, "local-bypass"), 0o755); err != nil {
		t.Fatal(err)
	}
	pluginYML := "name: b\nversion: 1.0.0\ndependencies:\n  apm:\n    - owner/repo\n"
	if err := os.WriteFile(filepath.Join(dir, "local-bypass", "apm.yml"), []byte(pluginYML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json", Host: "github.com"}); err != nil {
		t.Fatal(err)
	}
	withApmYMLFetcher(t, &fakeCmdApmYMLFetcher{})

	// Act
	out, err := runMarketplaceCmd(t, "audit", "acme")

	// Assert
	if err != nil {
		t.Fatalf("marketplace audit returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "local-bypass") || !strings.Contains(out, "1 dependency bypasses") {
		t.Errorf("output = %q, want the local plugin's bypass finding (v0.28.0 local audit)", out)
	}
	if !strings.Contains(out, "1 bypass warning(s)") {
		t.Errorf("output = %q, want the Summary to count the local bypass", out)
	}
}

func TestMarketplaceAudit_Strict_NetworkErrorFails(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [`+
		`{"name": "broken", "source": {"type": "github", "repo": "acme/broken", "ref": "v1.0.0"}}`+
		`]}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json", Host: "github.com"}); err != nil {
		t.Fatal(err)
	}
	withApmYMLFetcher(t, &fakeCmdApmYMLFetcher{errs: map[string]error{
		"github.com:acme/broken/apm.yml@v1.0.0": errors.New("could not reach GitHub (network error)"),
	}})

	// Act
	out, err := runMarketplaceCmd(t, "audit", "acme", "--strict")

	// Assert
	if err == nil {
		t.Fatalf("marketplace audit --strict returned no error for a network-error finding (output: %s)", out)
	}
}
