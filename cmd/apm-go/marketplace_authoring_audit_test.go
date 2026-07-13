package main

import (
	"errors"
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

func TestMarketplaceAudit_Strict_OnlyCountsNetworkAndParseFailures(t *testing.T) {
	// Arrange: one plugin is unsupported (a bare relative-path source), one
	// has no manifest at all (404) -- neither must trip --strict on its own.
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
	if err != nil {
		t.Fatalf("marketplace audit --strict returned error for unsupported/no-manifest-only findings: %v (output: %s)", err, out)
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
