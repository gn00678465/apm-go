package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/marketplace"
	"github.com/apm-go/apm/internal/registry"
	"github.com/apm-go/apm/internal/resolver"
)

// TestResolvePositionalPackage_FallsThroughNonMarketplaceRef covers
// mkt-020's fall-through rule at the CLI layer: an ordinary "owner/repo"
// positional package must behave EXACTLY as manifest.ParseDepString alone
// would (no marketplace lookup attempted, no provenance attached).
func TestResolvePositionalPackage_FallsThroughNonMarketplaceRef(t *testing.T) {
	// Act
	got, provenance, err := resolvePositionalPackage("acme/repo")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provenance != nil {
		t.Errorf("provenance = %+v, want nil for a non-marketplace package arg", provenance)
	}
	want, wantErr := manifest.ParseDepString("acme/repo")
	if wantErr != nil {
		t.Fatalf("test setup: %v", wantErr)
	}
	if got.RepoURL != want.RepoURL || got.Owner != want.Owner || got.Repo != want.Repo {
		t.Errorf("ref = %+v, want %+v", got, want)
	}
}

// TestResolvePositionalPackage_SemverRangeInRefRejected covers mkt-021: a
// "#REF" suffix containing a semver range character is a hard error, raised
// by marketplace.ParseRef itself before any marketplace registry lookup
// (no APM_CONFIG_DIR setup needed here at all).
func TestResolvePositionalPackage_SemverRangeInRefRejected(t *testing.T) {
	// Act
	_, _, err := resolvePositionalPackage("plugin@acme#^1.0")

	// Assert
	if err == nil || !strings.Contains(err.Error(), "semver range") {
		t.Fatalf("resolvePositionalPackage() error = %v, want an error mentioning \"semver range\"", err)
	}
}

// TestResolvePositionalPackage_MarketplaceNotFound covers mkt-022's error
// surface at the CLI layer: an unregistered marketplace name propagates
// marketplace.ErrMarketplaceNotFound unchanged.
func TestResolvePositionalPackage_MarketplaceNotFound(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())

	// Act
	_, _, err := resolvePositionalPackage("plugin@does-not-exist")

	// Assert
	if !errors.Is(err, marketplace.ErrMarketplaceNotFound) {
		t.Errorf("resolvePositionalPackage() error = %v, want errors.Is(err, marketplace.ErrMarketplaceNotFound)", err)
	}
}

// TestDepRefFromMarketplaceCanonical_AbsolutePath covers mkt-025's local-
// marketplace fast path (design.md: "不經過依賴字串往返" -- does NOT round-trip
// through a dependency string): an absolute filesystem path canonical is
// built directly as a git-sourced DependencyReference, bypassing
// manifest.ParseDepString's blanket "absolute paths are rejected" rule
// (that rule is aimed at hand-written apm.yml/CLI strings, not an
// internally-computed marketplace canonical).
func TestDepRefFromMarketplaceCanonical_AbsolutePath(t *testing.T) {
	// Arrange
	abs := filepath.Join(t.TempDir(), "plugin-dir")

	// Act
	got, err := depRefFromMarketplaceCanonical(abs)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RepoURL != abs {
		t.Errorf("RepoURL = %q, want %q", got.RepoURL, abs)
	}
	if got.Source != "git" {
		t.Errorf("Source = %q, want git", got.Source)
	}
	if got.IsLocal {
		t.Error("IsLocal = true, want false")
	}
}

// TestDepRefFromMarketplaceCanonical_RelativeCanonical covers the ordinary
// (non-mkt-025) case: a ordinary "owner/repo[/path][#ref]" canonical is
// parsed via the SAME manifest.ParseDepString pipeline an ordinary
// positional package argument goes through -- no special-casing.
func TestDepRefFromMarketplaceCanonical_RelativeCanonical(t *testing.T) {
	// Act
	got, err := depRefFromMarketplaceCanonical("acme-owner/acme-repo/plugins/p#v1.0.0")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Owner != "acme-owner" || got.Repo != "acme-repo" {
		t.Errorf("Owner/Repo = %q/%q, want acme-owner/acme-repo", got.Owner, got.Repo)
	}
	if got.VirtualPath != "plugins/p" {
		t.Errorf("VirtualPath = %q, want plugins/p", got.VirtualPath)
	}
	if got.Reference != "v1.0.0" {
		t.Errorf("Reference = %q, want v1.0.0", got.Reference)
	}
}

// TestResolvePositionalPackage_LocalMarketplaceFastPath covers mkt-025 end
// to end through resolvePositionalPackage: a KindLocal marketplace's
// relative-string plugin source resolves to an absolute filesystem path,
// which must NOT error out (proving the depRefFromMarketplaceCanonical
// bypass is actually wired in), and Provenance is populated.
func TestResolvePositionalPackage_LocalMarketplaceFastPath(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	mktDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(mktDir, "marketplace.json"),
		[]byte(`{"name": "acme", "plugins": [{"name": "p", "source": "./p"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: mktDir}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	// Act
	ref, provenance, err := resolvePositionalPackage("p@acme")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantPath := filepath.Join(mktDir, "p")
	if ref.RepoURL != wantPath {
		t.Errorf("RepoURL = %q, want %q", ref.RepoURL, wantPath)
	}
	if ref.Source != "git" {
		t.Errorf("Source = %q, want git", ref.Source)
	}
	if ref.IsLocal {
		t.Error("IsLocal = true, want false (forced into a git dependency)")
	}
	if provenance == nil || provenance.DiscoveredVia != "acme" || provenance.MarketplacePluginName != "p" {
		t.Errorf("provenance = %+v, want DiscoveredVia=acme MarketplacePluginName=p", provenance)
	}
}

// TestResolvePositionalPackage_PrefersStructuredDepRef covers mkt-027's
// "DepRef non-nil wins over parsing Canonical" rule at the CLI layer. Uses a
// KindLocal marketplace (fully offline: no real network fetch) whose
// registered Host/Owner/Repo are set to a fictitious non-GitHub-family git
// host so mkt-027's structured-DepRef branch engages purely from local
// manifest content -- resolvePluginSource/mkt-027 never perform any network
// I/O themselves (only the marketplace.json fetch does, and that's a local
// file read here).
func TestResolvePositionalPackage_PrefersStructuredDepRef(t *testing.T) {
	// Arrange
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

	// Act
	ref, provenance, err := resolvePositionalPackage("p@acme")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Owner != "acme-owner" || ref.Repo != "acme-repo" {
		t.Errorf("Owner/Repo = %q/%q, want acme-owner/acme-repo", ref.Owner, ref.Repo)
	}
	if ref.VirtualPath != "pkg/a" {
		t.Errorf("VirtualPath = %q, want pkg/a", ref.VirtualPath)
	}
	if provenance == nil || provenance.MarketplacePluginName != "p" {
		t.Errorf("provenance = %+v, want MarketplacePluginName=p", provenance)
	}
}

// TestBuildLockfile_MarketplaceProvenanceAttached covers mkt-031: only the
// dependency a CLI marketplace reference actually resolved to gets provenance
// attached; an unrelated, already-declared dependency in the same resolved
// graph is untouched.
func TestBuildLockfile_MarketplaceProvenanceAttached(t *testing.T) {
	// Arrange
	result := &resolver.ResolutionResult{
		Deps: []resolver.ResolvedDep{
			{Key: "acme/foo", RepoURL: "acme/foo", Kind: resolver.KindRegistry},
			{Key: "acme/bar", RepoURL: "acme/bar", Kind: resolver.KindRegistry},
		},
	}
	provenance := map[string]*marketplace.Provenance{
		"acme/foo": {DiscoveredVia: "acme-marketplace", MarketplacePluginName: "Foo-Plugin"},
	}

	// Act
	lock, err := buildLockfile(result, nil, &registry.Loader{}, nil, nil, true, provenance)
	if err != nil {
		t.Fatalf("buildLockfile: %v", err)
	}

	// Assert
	byRepo := make(map[string]lockfile.LockedDep)
	for _, d := range lock.Dependencies {
		byRepo[d.RepoURL] = d
	}
	foo := byRepo["acme/foo"]
	if foo.DiscoveredVia != "acme-marketplace" || foo.MarketplacePluginName != "Foo-Plugin" {
		t.Errorf("acme/foo provenance = %+v, want DiscoveredVia=acme-marketplace MarketplacePluginName=Foo-Plugin", foo)
	}
	bar := byRepo["acme/bar"]
	if bar.DiscoveredVia != "" || bar.MarketplacePluginName != "" {
		t.Errorf("acme/bar (no marketplace provenance) should have empty provenance, got %+v", bar)
	}
}

// TestBuildLockfile_MarketplaceProvenance_SourceURLOnlyForURLKind covers
// mkt-031's "source_url/source_digest only for kind=url" contract at the
// data-flow level: buildLockfile copies whatever Provenance ResolvePlugin
// handed it verbatim (the "only kind=url populates these two fields"
// guarantee itself is enforced upstream, in internal/marketplace).
func TestBuildLockfile_MarketplaceProvenance_SourceURLOnlyForURLKind(t *testing.T) {
	// Arrange
	result := &resolver.ResolutionResult{
		Deps: []resolver.ResolvedDep{
			{Key: "acme/foo", RepoURL: "acme/foo", Kind: resolver.KindRegistry},
			{Key: "acme/bar", RepoURL: "acme/bar", Kind: resolver.KindRegistry},
		},
	}
	provenance := map[string]*marketplace.Provenance{
		"acme/foo": {
			DiscoveredVia: "url-marketplace", MarketplacePluginName: "Foo",
			SourceURL: "https://example.com/marketplace.json", SourceDigest: "sha256:abc",
		},
		"acme/bar": {DiscoveredVia: "github-marketplace", MarketplacePluginName: "Bar"},
	}

	// Act
	lock, err := buildLockfile(result, nil, &registry.Loader{}, nil, nil, true, provenance)
	if err != nil {
		t.Fatalf("buildLockfile: %v", err)
	}

	// Assert
	byRepo := make(map[string]lockfile.LockedDep)
	for _, d := range lock.Dependencies {
		byRepo[d.RepoURL] = d
	}
	foo := byRepo["acme/foo"]
	if foo.SourceURL == "" || foo.SourceDigest == "" {
		t.Errorf("acme/foo (kind=url) should have source_url/source_digest, got %+v", foo)
	}
	bar := byRepo["acme/bar"]
	if bar.SourceURL != "" || bar.SourceDigest != "" {
		t.Errorf("acme/bar (non-url marketplace) must NOT have source_url/source_digest, got %+v", bar)
	}
}

// TestRunInstall_MarketplacePackage_LockfileProvenanceAndPersistedCanonical
// is the full CLI-level (mkt-029/031) regression: `apm install p@acme`
// (a) writes discovered_via/marketplace_plugin_name into apm.lock.yaml, and
// (b) persists the RESOLVED canonical -- never the raw "p@acme" CLI
// string -- into apm.yml (mkt-030), which this test proves by running a
// SECOND, bare `apm install` immediately after: if apm.yml still held the
// literal "p@acme" string, manifest.ParseDepString would reject it (no "/",
// so it can't even fall through as a git shorthand) and this second call
// would fail.
func TestRunInstall_MarketplacePackage_LockfileProvenanceAndPersistedCanonical(t *testing.T) {
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

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	// Act -- first install with the marketplace CLI reference.
	if err := runInstall(deps, false, true, "", nil, []string{"p@acme"}); err != nil {
		t.Fatalf("first runInstall: %v", err)
	}

	// Assert -- lockfile carries provenance.
	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("read apm.lock.yaml: %v", err)
	}
	lockStr := string(lockData)
	if !strings.Contains(lockStr, "discovered_via: acme") {
		t.Errorf("apm.lock.yaml missing discovered_via: acme; got:\n%s", lockStr)
	}
	if !strings.Contains(lockStr, "marketplace_plugin_name: p") {
		t.Errorf("apm.lock.yaml missing marketplace_plugin_name: p; got:\n%s", lockStr)
	}

	// Assert -- apm.yml persisted the resolved canonical, not "p@acme".
	manifestData, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatalf("read apm.yml: %v", err)
	}
	manifestStr := string(manifestData)
	if strings.Contains(manifestStr, "p@acme") {
		t.Errorf("apm.yml persisted the raw CLI marketplace reference %q verbatim; got:\n%s", "p@acme", manifestStr)
	}
	if !strings.Contains(manifestStr, "acme-owner/acme-repo") {
		t.Errorf("apm.yml should persist the resolved canonical (acme-owner/acme-repo/...); got:\n%s", manifestStr)
	}

	// Act -- second, bare `apm install` (no positional args): re-parses the
	// now-persisted apm.yml. Must succeed -- proving the persisted string
	// round-trips.
	if err := runInstall(deps, false, true, "", nil, nil); err != nil {
		t.Fatalf("second (bare) runInstall failed to re-parse the persisted apm.yml: %v", err)
	}
}

// TestBuildLockfile_ProvenanceCarriesForwardFromExistingLock covers mkt-032's
// Go-variant fix (design.md's "mkt-032 修正" section, checklist mkt-032):
// buildLockfile rebuilds every LockedDep from scratch on EVERY call, so a
// dependency discovered via a CLI marketplace ref on a PRIOR call would
// otherwise lose its provenance the moment a later call rebuilds it without
// re-supplying that CLI ref (e.g. a bare `apm install`). When the new
// entry's own provenance is empty (no marketplaceProvenance entry for this
// call) and existingLock has an entry with the same UniqueKey() bearing
// provenance, all four fields must be copied forward.
func TestBuildLockfile_ProvenanceCarriesForwardFromExistingLock(t *testing.T) {
	// Arrange
	result := &resolver.ResolutionResult{
		Deps: []resolver.ResolvedDep{
			{Key: "acme/foo", RepoURL: "acme/foo", Kind: resolver.KindRegistry},
		},
	}
	existingLock := &lockfile.Lockfile{Dependencies: []lockfile.LockedDep{
		{
			RepoURL: "acme/foo", DiscoveredVia: "acme-marketplace", MarketplacePluginName: "Foo-Plugin",
			SourceURL: "https://example.com/marketplace.json", SourceDigest: "sha256:deadbeef",
		},
	}}

	// Act -- NO marketplaceProvenance for this call (nil), simulating a bare
	// `apm install` that doesn't re-supply the marketplace CLI ref.
	lock, err := buildLockfile(result, existingLock, &registry.Loader{}, nil, nil, true, nil)
	if err != nil {
		t.Fatalf("buildLockfile: %v", err)
	}

	// Assert
	if len(lock.Dependencies) != 1 {
		t.Fatalf("deps count = %d, want 1", len(lock.Dependencies))
	}
	foo := lock.Dependencies[0]
	if foo.DiscoveredVia != "acme-marketplace" || foo.MarketplacePluginName != "Foo-Plugin" ||
		foo.SourceURL != "https://example.com/marketplace.json" || foo.SourceDigest != "sha256:deadbeef" {
		t.Errorf("provenance not carried forward from existingLock, got %+v", foo)
	}
}

// TestBuildLockfile_ProvenanceCarryForward_FreshProvenanceWins ensures
// carry-forward is only a FALLBACK: when this call's own
// marketplaceProvenance supplies fresh values (e.g. re-running `apm install
// pkg@newmkt` against a different marketplace than what was locked before),
// those fresh values win outright -- the existing lockfile entry's (now
// stale) provenance must never leak through and override them.
func TestBuildLockfile_ProvenanceCarryForward_FreshProvenanceWins(t *testing.T) {
	// Arrange
	result := &resolver.ResolutionResult{
		Deps: []resolver.ResolvedDep{
			{Key: "acme/foo", RepoURL: "acme/foo", Kind: resolver.KindRegistry},
		},
	}
	existingLock := &lockfile.Lockfile{Dependencies: []lockfile.LockedDep{
		{RepoURL: "acme/foo", DiscoveredVia: "old-marketplace", MarketplacePluginName: "Old-Plugin"},
	}}
	freshProvenance := map[string]*marketplace.Provenance{
		"acme/foo": {DiscoveredVia: "new-marketplace", MarketplacePluginName: "New-Plugin"},
	}

	// Act
	lock, err := buildLockfile(result, existingLock, &registry.Loader{}, nil, nil, true, freshProvenance)
	if err != nil {
		t.Fatalf("buildLockfile: %v", err)
	}

	// Assert
	foo := lock.Dependencies[0]
	if foo.DiscoveredVia != "new-marketplace" || foo.MarketplacePluginName != "New-Plugin" {
		t.Errorf("fresh provenance was overridden by carry-forward, got %+v, want DiscoveredVia=new-marketplace MarketplacePluginName=New-Plugin", foo)
	}
}

// TestBuildLockfile_ProvenanceCarryForward_NoExistingProvenanceStaysEmpty
// covers the negative case: when neither this call's marketplaceProvenance
// nor the existingLock's matching entry carries provenance, the rebuilt
// entry's provenance fields stay empty (carry-forward must not fabricate
// provenance out of nothing).
func TestBuildLockfile_ProvenanceCarryForward_NoExistingProvenanceStaysEmpty(t *testing.T) {
	// Arrange
	result := &resolver.ResolutionResult{
		Deps: []resolver.ResolvedDep{
			{Key: "acme/foo", RepoURL: "acme/foo", Kind: resolver.KindRegistry},
		},
	}
	existingLock := &lockfile.Lockfile{Dependencies: []lockfile.LockedDep{
		{RepoURL: "acme/foo"},
	}}

	// Act
	lock, err := buildLockfile(result, existingLock, &registry.Loader{}, nil, nil, true, nil)
	if err != nil {
		t.Fatalf("buildLockfile: %v", err)
	}

	// Assert
	foo := lock.Dependencies[0]
	if foo.DiscoveredVia != "" || foo.MarketplacePluginName != "" || foo.SourceURL != "" || foo.SourceDigest != "" {
		t.Errorf("provenance fabricated out of nothing, got %+v, want all empty", foo)
	}
}

// TestRunInstall_MarketplaceProvenance_CarriesForwardAcrossNoTargetBareAndTargetedInstalls
// is the mkt-032 three-part AC4 regression (checklist mkt-032; design.md's
// "mkt-032 修正" section, "回歸測試(AC4,三段)"), adapted to how apm-go's
// install pipeline actually shapes this bug: apm-go writes apm.yml and
// apm.lock.yaml in the SAME call (no target hard-gate splitting the two like
// the Python original), so the original's "abort before lockfile write"
// data-loss path does not exist here. The real Go-variant risk this guards
// is buildLockfile's from-scratch-every-call rebuild silently dropping
// provenance on any later call that doesn't re-supply the marketplace CLI
// ref -- exercised here across three successive install calls:
//
//	(a) `apm install p@acme` with no --target (and no auto-detectable
//	    target in the project) -> deploy is skipped, but apm.lock.yaml
//	    already carries the full provenance.
//	(b) an immediately following BARE `apm install` (apm.yml now holds the
//	    resolved canonical, not "p@acme") -> provenance is carried forward
//	    unchanged (mkt-032's carry-forward fix).
//	(c) an immediately following `apm install --target claude` -> deploy
//	    actually runs (proven by a local instructions primitive landing in
//	    .claude/rules/), AND provenance is still present afterward.
func TestRunInstall_MarketplaceProvenance_CarriesForwardAcrossNoTargetBareAndTargetedInstalls(t *testing.T) {
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

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// A local instructions primitive so part (c)'s targeted install has
	// something concrete to deploy, proving deploy actually ran.
	if err := os.MkdirAll(".apm/instructions", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".apm", "instructions", "demo.instructions.md"), []byte("# demo instructions"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	readLockAssertProvenance := func(step string) {
		t.Helper()
		lockData, err := os.ReadFile("apm.lock.yaml")
		if err != nil {
			t.Fatalf("%s: read apm.lock.yaml: %v", step, err)
		}
		lockStr := string(lockData)
		if !strings.Contains(lockStr, "discovered_via: acme") {
			t.Errorf("%s: apm.lock.yaml missing discovered_via: acme; got:\n%s", step, lockStr)
		}
		if !strings.Contains(lockStr, "marketplace_plugin_name: p") {
			t.Errorf("%s: apm.lock.yaml missing marketplace_plugin_name: p; got:\n%s", step, lockStr)
		}
	}

	// (a) install with the marketplace CLI reference, no --target and no
	// auto-detectable target in this fresh project -- deploy must be
	// skipped, but the lockfile must already carry provenance.
	if err := runInstall(deps, false, true, "", nil, []string{"p@acme"}); err != nil {
		t.Fatalf("(a) runInstall: %v", err)
	}
	readLockAssertProvenance("(a)")
	if _, err := os.Stat(filepath.Join(".claude", "rules", "demo.md")); err == nil {
		t.Error("(a): local instructions were deployed even though no target was resolvable -- deploy should have been skipped")
	}

	// (b) a bare `apm install` -- no positional packages, so no fresh
	// marketplaceProvenance for this call. Must succeed (proving apm.yml
	// round-trips the persisted canonical) AND must carry provenance
	// forward unchanged.
	if err := runInstall(deps, false, true, "", nil, nil); err != nil {
		t.Fatalf("(b) bare runInstall: %v", err)
	}
	readLockAssertProvenance("(b)")

	// (c) `apm install --target claude` -- deploy now actually runs (the
	// local instructions primitive lands on disk), and provenance is still
	// present in the rebuilt lockfile.
	if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
		t.Fatalf("(c) targeted runInstall: %v", err)
	}
	readLockAssertProvenance("(c)")
	if _, err := os.Stat(filepath.Join(".claude", "rules", "demo.md")); err != nil {
		t.Errorf("(c): expected local instructions to deploy to .claude/rules/demo.md now that --target was given: %v", err)
	}
}

// TestRunInstall_MarketplacePackage_AbsoluteLocalPath_FailsClosedOnPersist
// covers the mkt-025 local-fast-path edge this task's design surfaced: a
// local-marketplace-resolved absolute path has no apm.yml dependency-string
// representation, so `apm install` must fail closed with a clear error
// rather than silently writing a broken apm.yml. Also asserts apm.yml is
// left byte-for-byte unmodified (the error fires before any write).
func TestRunInstall_MarketplacePackage_AbsoluteLocalPath_FailsClosedOnPersist(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)
	t.Setenv("APM_CONFIG_DIR", t.TempDir())

	mktDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(mktDir, "marketplace.json"),
		[]byte(`{"name": "acme", "plugins": [{"name": "p", "source": "./p"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: mktDir}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	originalManifest := "name: test\nversion: \"1.0.0\"\n"
	if err := os.WriteFile("apm.yml", []byte(originalManifest), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	// Act
	err := runInstall(deps, false, true, "", nil, []string{"p@acme"})

	// Assert
	if err == nil {
		t.Fatal("expected an error for a local-marketplace-resolved absolute path")
	}
	if !strings.Contains(err.Error(), "local filesystem path") {
		t.Errorf("error = %v, want it to mention the local filesystem path limitation", err)
	}
	if _, statErr := os.Stat("apm.lock.yaml"); statErr == nil {
		t.Error("apm.lock.yaml should not have been written")
	}
	got, readErr := os.ReadFile("apm.yml")
	if readErr != nil {
		t.Fatalf("read apm.yml: %v", readErr)
	}
	if string(got) != originalManifest {
		t.Errorf("apm.yml was modified; got:\n%s\nwant unchanged:\n%s", got, originalManifest)
	}
}
