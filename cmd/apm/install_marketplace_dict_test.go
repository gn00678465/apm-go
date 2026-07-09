package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/marketplace"
)

// ── F1 (CRITICAL): apm.yml dict-form marketplace dependencies must actually
// resolve through resolver.Resolve's BFS, not fall into the git-literal
// default case with a bogus "_marketplace/<mkt>/<name>" RepoURL. ──

// handAuthoredApmYMLWithDictMarketplaceDep mirrors the project's established
// "舊坑 1" fixture convention (see internal/manifest/depref_marketplace_test.go's
// handAuthoredApmYMLWithMarketplaceDep and cmd/apm/marketplace_authoring_test.go's
// handAuthoredApmYML): unusual spacing, inline comments on the very lines
// under test, and unrelated surrounding content -- proving F1's fix resolves
// a dict-form marketplace dependency even embedded in a realistic,
// hand-edited apm.yml, not just a freshly-generated one.
const handAuthoredApmYMLWithDictMarketplaceDep = "# Hand-authored project manifest\n" +
	"name:    demo-project\n" +
	"version: \"1.0.0\"\n" +
	"dependencies:\n" +
	"  apm:\n" +
	"    - name: p                # the marketplace plugin's name\n" +
	"      marketplace: acme      # registered marketplace\n" +
	"scripts:\n" +
	"  build: echo hi   # keep me\n"

// TestRunInstall_MarketplaceDictDep_RootResolvedIntoLockfile is F1's core
// end-to-end regression: an apm.yml dependencies.apm dict-form entry
// {name, marketplace} (mkt-033) must be resolved by resolver.Resolve's BFS
// itself into a concrete git dependency and land in apm.lock.yaml. Before
// the fix, internal/resolver's BFS switch had no KindMarketplace case at
// all -- it fell into the default (git-literal) case and was "resolved"
// against its own unresolved "_marketplace/acme/p" RepoURL placeholder,
// which is not a real repository: this dependency was never actually
// installed. This test is a BARE `apm install` (no positional CLI package
// argument at all), so the only path that can possibly resolve the
// dependency is the BFS-level one this task adds.
func TestRunInstall_MarketplaceDictDep_RootResolvedIntoLockfile(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)
	t.Setenv("APM_CONFIG_DIR", t.TempDir())

	mktDir := t.TempDir()
	manifestJSON := `{
		"name": "acme",
		"plugins": [{"name": "p", "source": {
			"type": "git-subdir", "repo": "acme-owner/acme-repo", "subdir": "pkg/a"
		}}]
	}`
	if err := os.WriteFile(filepath.Join(mktDir, "marketplace.json"), []byte(manifestJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := marketplace.AddSource(marketplace.MarketplaceSource{
		Name: "acme", URL: mktDir, Owner: "acme-owner", Repo: "acme-repo",
		Host: "git.internal.example.com", Ref: "main",
	}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	if err := os.WriteFile("apm.yml", []byte(handAuthoredApmYMLWithDictMarketplaceDep), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	// Act -- a BARE `apm install`: the marketplace dependency comes entirely
	// from apm.yml's dependencies.apm dict entry, never from a CLI
	// positional package argument. --target claude is only here to satisfy
	// the "dependencies present but no deployment target" exit-2 guard
	// (F2) -- this test's actual subject is lockfile resolution, not deploy.
	if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	// Assert -- the dependency actually resolved to a real git coordinate
	// and landed in the lockfile, not a bogus "_marketplace/..." entry.
	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("read apm.lock.yaml: %v", err)
	}
	lockStr := string(lockData)
	if strings.Contains(lockStr, "_marketplace/") {
		t.Errorf("apm.lock.yaml still contains the unresolved marketplace placeholder RepoURL; got:\n%s", lockStr)
	}
	if !strings.Contains(lockStr, "acme-owner/acme-repo") {
		t.Errorf("apm.lock.yaml missing the resolved dependency acme-owner/acme-repo; got:\n%s", lockStr)
	}
	if !strings.Contains(lockStr, "discovered_via: acme") {
		t.Errorf("apm.lock.yaml missing discovered_via: acme (mkt-031 provenance); got:\n%s", lockStr)
	}
	if !strings.Contains(lockStr, "marketplace_plugin_name: p") {
		t.Errorf("apm.lock.yaml missing marketplace_plugin_name: p; got:\n%s", lockStr)
	}
}

// TestRunInstall_MarketplaceDictDep_TransitiveResolvedIntoLockfile is F1's
// transitive-dependency regression (mirroring the Python original's
// apm_resolver.py:711 transitive resolve_marketplace_plugin call): a
// dependency fetched partway through BFS (acme/a) declares its OWN
// dependencies.apm dict-form marketplace entry ({name: q, marketplace:
// acme2}); that transitive marketplace reference must resolve through the
// exact same BFS-level path a root one does.
func TestRunInstall_MarketplaceDictDep_TransitiveResolvedIntoLockfile(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)
	t.Setenv("APM_CONFIG_DIR", t.TempDir())

	mktDir := t.TempDir()
	manifestJSON := `{
		"name": "acme2",
		"plugins": [{"name": "q", "source": {
			"type": "git-subdir", "repo": "acme2-owner/acme2-repo", "subdir": "pkg/q"
		}}]
	}`
	if err := os.WriteFile(filepath.Join(mktDir, "marketplace.json"), []byte(manifestJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := marketplace.AddSource(marketplace.MarketplaceSource{
		Name: "acme2", URL: mktDir, Owner: "acme2-owner", Repo: "acme2-repo",
		Host: "git.internal.example.com", Ref: "main",
	}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/a\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// acme/a's own sub-manifest (as fetched by the loader) declares a
	// transitive marketplace dict dependency, exactly like a root manifest
	// can -- proving the BFS collapses it the same way regardless of depth.
	subManifest := &manifest.Manifest{
		Name:    "a",
		Version: "1.0.0",
		ParsedDeps: []*manifest.DependencyReference{
			{
				RepoURL:               "_marketplace/acme2/q",
				Source:                "marketplace",
				MarketplaceName:       "acme2",
				MarketplacePluginName: "q",
			},
		},
	}

	deps := &installDeps{
		tags: &mockInstallTagLister{},
		loader: &mockInstallLoader{packages: map[string]*manifest.Manifest{
			// acme/a has no "#ref"/semver constraint in apm.yml above, so it
			// resolves as a git-literal dependency with an empty pinned ref
			// (RepoURL + "@" + "" -- mirrors mockInstallLoader's own key
			// convention, "ref@resolvedRef").
			"acme/a@": subManifest,
		}},
	}

	// Act -- --target claude only satisfies the "dependencies present but no
	// deployment target" exit-2 guard (F2); this test's subject is
	// transitive marketplace-dict lockfile resolution, not deploy.
	if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	// Assert
	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("read apm.lock.yaml: %v", err)
	}
	lockStr := string(lockData)
	if strings.Contains(lockStr, "_marketplace/") {
		t.Errorf("apm.lock.yaml still contains the unresolved marketplace placeholder RepoURL; got:\n%s", lockStr)
	}
	if !strings.Contains(lockStr, "acme2-owner/acme2-repo") {
		t.Errorf("apm.lock.yaml missing the transitively-resolved dependency acme2-owner/acme2-repo; got:\n%s", lockStr)
	}
	if !strings.Contains(lockStr, "discovered_via: acme2") {
		t.Errorf("apm.lock.yaml missing discovered_via: acme2 (mkt-031 provenance on a transitive marketplace dep); got:\n%s", lockStr)
	}
}
