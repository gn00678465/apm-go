package main

import (
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/marketplace"
)

// panicResolver proves a resolveUninstallTargets code path never reaches the
// registry (un-014's lockfile-offline-first and un-081's --dry-run-skips-
// registry guarantees) -- if resolve is ever actually invoked when it
// shouldn't be, the test crashes loudly instead of silently passing.
func panicResolver(_, _ string, _ marketplace.ResolveOptions) (*marketplace.Resolution, error) {
	panic("marketplaceRegistryResolver must not be called here")
}

// fixedCanonicalResolver returns a resolver stub that always resolves to
// canonical, regardless of the plugin/mkt/ref it's called with.
func fixedCanonicalResolver(canonical string) marketplaceRegistryResolver {
	return func(_, _ string, _ marketplace.ResolveOptions) (*marketplace.Resolution, error) {
		return &marketplace.Resolution{
			Canonical:  canonical,
			Provenance: &marketplace.Provenance{},
		}, nil
	}
}

// TestResolveUninstallTargets_FiveInputShapes_SameIdentity proves un-010/011:
// owner/repo shorthand, HTTPS URL, SSH URL, SCP form, and FQDN shorthand all
// resolve to the identical apm.yml entry (identity ignores Host, matching
// DependencyReference.IdentityKey()'s existing, documented behavior).
func TestResolveUninstallTargets_FiveInputShapes_SameIdentity(t *testing.T) {
	m := &manifest.Manifest{
		ParsedDeps: []*manifest.DependencyReference{
			{Owner: "acme", Repo: "foo", RepoURL: "acme/foo", Source: "git"},
		},
	}
	names := []string{
		"acme/foo",
		"https://github.com/acme/foo",
		"ssh://git@github.com/acme/foo",
		"git@github.com:acme/foo",
		"github.com/acme/foo",
	}

	res := resolveUninstallTargets(names, m, nil, panicResolver, false)

	if len(res.NotFound) != 0 {
		t.Fatalf("unexpected NotFound: %+v", res.NotFound)
	}
	if len(res.APMTargets) != len(names) {
		t.Fatalf("expected %d APM targets, got %d: %+v", len(names), len(res.APMTargets), res.APMTargets)
	}
	for _, tgt := range res.APMTargets {
		if tgt.IdentityKey != "acme/foo" {
			t.Errorf("name %q: identity = %q, want acme/foo", tgt.Name, tgt.IdentityKey)
		}
		if tgt.IsDev {
			t.Errorf("name %q: IsDev = true, want false", tgt.Name)
		}
	}
}

// TestResolveUninstallTargets_ScansBothProdAndDev proves un-012: a
// dev-installed package must be uninstallable, and IsDev correctly reflects
// which section matched.
func TestResolveUninstallTargets_ScansBothProdAndDev(t *testing.T) {
	m := &manifest.Manifest{
		ParsedDeps:    []*manifest.DependencyReference{{RepoURL: "acme/prod", Source: "git"}},
		ParsedDevDeps: []*manifest.DependencyReference{{RepoURL: "acme/dev", Source: "git"}},
	}

	res := resolveUninstallTargets([]string{"acme/prod", "acme/dev"}, m, nil, panicResolver, false)

	if len(res.APMTargets) != 2 {
		t.Fatalf("expected 2 APM targets, got %d: %+v", len(res.APMTargets), res.APMTargets)
	}
	byName := map[string]uninstallAPMTarget{}
	for _, tgt := range res.APMTargets {
		byName[tgt.Name] = tgt
	}
	if byName["acme/prod"].IsDev {
		t.Errorf("acme/prod: IsDev = true, want false")
	}
	if !byName["acme/dev"].IsDev {
		t.Errorf("acme/dev: IsDev = false, want true")
	}
}

// TestResolveUninstallTargets_MarketplaceRef_LockfileOfflineFirst proves
// un-014: a marketplace ref resolves via lockfile provenance alone, never
// touching the registry.
func TestResolveUninstallTargets_MarketplaceRef_LockfileOfflineFirst(t *testing.T) {
	m := &manifest.Manifest{
		ParsedDeps: []*manifest.DependencyReference{{RepoURL: "acme/foo", Source: "git"}},
	}
	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/foo", DiscoveredVia: "mkt1", MarketplacePluginName: "plug1"},
		},
	}

	res := resolveUninstallTargets([]string{"plug1@mkt1"}, m, lock, panicResolver, false)

	if len(res.NotFound) != 0 || len(res.SupplyChainRejected) != 0 {
		t.Fatalf("unexpected non-target buckets: notFound=%+v rejected=%+v", res.NotFound, res.SupplyChainRejected)
	}
	if len(res.APMTargets) != 1 || res.APMTargets[0].IdentityKey != "acme/foo" {
		t.Fatalf("expected single acme/foo target, got %+v", res.APMTargets)
	}
}

// TestResolveUninstallTargets_MarketplaceRef_HashRefIgnored proves un-016:
// the "#ref" fragment plays no role in identity matching -- two different
// refs against the same lockfile provenance resolve identically, still
// never touching the registry.
func TestResolveUninstallTargets_MarketplaceRef_HashRefIgnored(t *testing.T) {
	m := &manifest.Manifest{
		ParsedDeps: []*manifest.DependencyReference{{RepoURL: "acme/foo", Source: "git"}},
	}
	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/foo", DiscoveredVia: "mkt1", MarketplacePluginName: "plug1"},
		},
	}

	res := resolveUninstallTargets([]string{"plug1@mkt1#v1.0.0", "plug1@mkt1#v2.0.0"}, m, lock, panicResolver, false)

	if len(res.APMTargets) != 2 {
		t.Fatalf("expected 2 APM targets, got %d: %+v", len(res.APMTargets), res.APMTargets)
	}
	for _, tgt := range res.APMTargets {
		if tgt.IdentityKey != "acme/foo" {
			t.Errorf("identity = %q, want acme/foo", tgt.IdentityKey)
		}
	}
}

// TestResolveUninstallTargets_SupplyChainGuard_RejectsUnlockedCanonical
// proves un-015 (Review Gate A): when a registry resolves a marketplace ref
// to a canonical absent from the lockfile, uninstall must refuse it --
// never adding it to APMTargets -- rather than silently letting a
// compromised registry response cause an unrelated package's removal.
func TestResolveUninstallTargets_SupplyChainGuard_RejectsUnlockedCanonical(t *testing.T) {
	m := &manifest.Manifest{}
	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{{RepoURL: "other/pkg"}},
	}
	resolve := fixedCanonicalResolver("rogue/pkg")

	res := resolveUninstallTargets([]string{"plug1@mkt1"}, m, lock, resolve, false)

	if len(res.APMTargets) != 0 {
		t.Fatalf("guard failed: rejected canonical reached APMTargets: %+v", res.APMTargets)
	}
	if len(res.SupplyChainRejected) != 1 || res.SupplyChainRejected[0].Canonical != "rogue/pkg" {
		t.Fatalf("expected single rogue/pkg rejection, got %+v", res.SupplyChainRejected)
	}
}

// TestResolveUninstallTargets_SupplyChainGuard_AllowsLockedCanonical proves
// the guard's happy path: a registry-resolved canonical that IS in the
// lockfile (and still a live apm.yml entry) is accepted.
func TestResolveUninstallTargets_SupplyChainGuard_AllowsLockedCanonical(t *testing.T) {
	m := &manifest.Manifest{
		ParsedDeps: []*manifest.DependencyReference{{RepoURL: "acme/foo", Source: "git"}},
	}
	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{{RepoURL: "acme/foo"}},
	}
	resolve := fixedCanonicalResolver("acme/foo")

	res := resolveUninstallTargets([]string{"plug1@mkt1"}, m, lock, resolve, false)

	if len(res.SupplyChainRejected) != 0 {
		t.Fatalf("unexpected rejection: %+v", res.SupplyChainRejected)
	}
	if len(res.APMTargets) != 1 || res.APMTargets[0].IdentityKey != "acme/foo" {
		t.Fatalf("expected single acme/foo target, got %+v", res.APMTargets)
	}
}

// TestResolveUninstallTargets_NoLockfile_RegistryCanonicalMatchesManifest
// proves un-018: with no lockfile anchor at all, the supply-chain guard is
// skipped, but the resolved canonical must still match a live apm.yml entry.
func TestResolveUninstallTargets_NoLockfile_RegistryCanonicalMatchesManifest(t *testing.T) {
	m := &manifest.Manifest{
		ParsedDeps: []*manifest.DependencyReference{{RepoURL: "acme/foo", Source: "git"}},
	}
	resolve := fixedCanonicalResolver("acme/foo")

	res := resolveUninstallTargets([]string{"plug1@mkt1"}, m, nil, resolve, false)

	if len(res.SupplyChainRejected) != 0 {
		t.Fatalf("unexpected rejection with no lockfile anchor: %+v", res.SupplyChainRejected)
	}
	if len(res.APMTargets) != 1 || res.APMTargets[0].IdentityKey != "acme/foo" {
		t.Fatalf("expected single acme/foo target, got %+v", res.APMTargets)
	}
}

// TestResolveUninstallTargets_NoLockfile_RegistryCanonicalNotInManifest
// proves un-018's failure path is "not found", not a supply-chain
// rejection: there's no lockfile anchor to have been violated.
func TestResolveUninstallTargets_NoLockfile_RegistryCanonicalNotInManifest(t *testing.T) {
	m := &manifest.Manifest{}
	resolve := fixedCanonicalResolver("other/pkg")

	res := resolveUninstallTargets([]string{"plug1@mkt1"}, m, nil, resolve, false)

	if len(res.SupplyChainRejected) != 0 {
		t.Fatalf("expected no-lockfile failure to be NotFound, not rejected: %+v", res.SupplyChainRejected)
	}
	if len(res.NotFound) != 1 || res.NotFound[0].Reason != uninstallNotFoundNoMatch {
		t.Fatalf("expected single NotFound(NoMatch), got %+v", res.NotFound)
	}
}

// TestResolveUninstallTargets_DryRun_SkipsRegistry proves un-081: a
// marketplace ref that isn't resolvable offline is reported as
// not-found-due-to-dry-run, never reaching the registry even though a
// resolver is supplied.
func TestResolveUninstallTargets_DryRun_SkipsRegistry(t *testing.T) {
	m := &manifest.Manifest{}

	res := resolveUninstallTargets([]string{"plug1@mkt1"}, m, nil, panicResolver, true)

	if len(res.NotFound) != 1 || res.NotFound[0].Reason != uninstallNotFoundDryRunSkipped {
		t.Fatalf("expected single NotFound(DryRunSkipped), got %+v", res.NotFound)
	}
}

// TestResolveUninstallTargets_MarketplaceDictForm_DirectManifestMatch
// proves the Step-A fast path: an apm.yml dict-form marketplace entry
// ({name, marketplace, version}) matches directly against the CLI argument,
// with no lockfile or registry involvement, case-insensitively (mkt-024).
func TestResolveUninstallTargets_MarketplaceDictForm_DirectManifestMatch(t *testing.T) {
	m := &manifest.Manifest{
		ParsedDeps: []*manifest.DependencyReference{
			{Source: "marketplace", MarketplaceName: "Mkt1", MarketplacePluginName: "Plug1", RepoURL: "_marketplace/Mkt1/Plug1"},
		},
		ParsedDevDeps: []*manifest.DependencyReference{
			{Source: "marketplace", MarketplaceName: "Mkt2", MarketplacePluginName: "Plug2", RepoURL: "_marketplace/Mkt2/Plug2"},
		},
	}

	res := resolveUninstallTargets([]string{"plug1@mkt1", "plug2@mkt2"}, m, nil, panicResolver, false)

	if len(res.NotFound) != 0 {
		t.Fatalf("unexpected NotFound: %+v", res.NotFound)
	}
	if len(res.APMTargets) != 2 {
		t.Fatalf("expected 2 APM targets, got %d: %+v", len(res.APMTargets), res.APMTargets)
	}
	byName := map[string]uninstallAPMTarget{}
	for _, tgt := range res.APMTargets {
		byName[tgt.Name] = tgt
	}
	if byName["plug1@mkt1"].IsDev {
		t.Errorf("plug1@mkt1: IsDev = true, want false")
	}
	if !byName["plug2@mkt2"].IsDev {
		t.Errorf("plug2@mkt2: IsDev = false, want true")
	}
}

// TestResolveUninstallTargets_NotFound_WarnsAndContinues proves un-013: an
// unmatched PACKAGE argument doesn't abort resolution of the rest.
func TestResolveUninstallTargets_NotFound_WarnsAndContinues(t *testing.T) {
	m := &manifest.Manifest{
		ParsedDeps: []*manifest.DependencyReference{{RepoURL: "acme/foo", Source: "git"}},
	}

	res := resolveUninstallTargets([]string{"nope/nope", "acme/foo"}, m, nil, panicResolver, false)

	if len(res.NotFound) != 1 || res.NotFound[0].Name != "nope/nope" {
		t.Fatalf("expected single NotFound for nope/nope, got %+v", res.NotFound)
	}
	if len(res.APMTargets) != 1 || res.APMTargets[0].Name != "acme/foo" {
		t.Fatalf("expected single APM target for acme/foo, got %+v", res.APMTargets)
	}
}

// TestResolveUninstallTargets_AllNotFound proves the "everything unmatched"
// case leaves every other bucket empty (a caller doing "all not-found ->
// exit without changes" only needs to check len(NotFound) == len(names)).
func TestResolveUninstallTargets_AllNotFound(t *testing.T) {
	m := &manifest.Manifest{}

	res := resolveUninstallTargets([]string{"nope/one", "nope/two"}, m, nil, panicResolver, false)

	if len(res.NotFound) != 2 {
		t.Fatalf("expected 2 NotFound, got %+v", res.NotFound)
	}
	if len(res.APMTargets) != 0 || len(res.MCPTargets) != 0 || len(res.SupplyChainRejected) != 0 {
		t.Fatalf("expected only NotFound populated, got %+v", res)
	}
}

// TestResolveUninstallTargets_StandaloneMCP proves un-019 (apm-go
// enhancement): a PACKAGE argument that isn't any apm.yml dependency, but
// matches a dependencies.mcp / devDependencies.mcp server's Name, resolves
// as a standalone MCP-removal target.
func TestResolveUninstallTargets_StandaloneMCP(t *testing.T) {
	m := &manifest.Manifest{
		MCPServers:    []*manifest.MCPDependency{{Name: "foo-mcp"}},
		MCPDevServers: []*manifest.MCPDependency{{Name: "dev-mcp"}},
	}

	res := resolveUninstallTargets([]string{"foo-mcp", "dev-mcp"}, m, nil, panicResolver, false)

	if len(res.NotFound) != 0 || len(res.APMTargets) != 0 {
		t.Fatalf("expected only MCP targets, got notFound=%+v apm=%+v", res.NotFound, res.APMTargets)
	}
	if len(res.MCPTargets) != 2 {
		t.Fatalf("expected 2 MCP targets, got %+v", res.MCPTargets)
	}
}

// TestResolveUninstallTargets_APMIdentityWinsOverMCPName proves un-019's
// documented ordering: apm package identity is checked before falling back
// to a standalone MCP server name, even if a same-named MCP entry exists.
func TestResolveUninstallTargets_APMIdentityWinsOverMCPName(t *testing.T) {
	m := &manifest.Manifest{
		ParsedDeps: []*manifest.DependencyReference{{RepoURL: "acme/foo", Source: "git"}},
		MCPServers: []*manifest.MCPDependency{{Name: "acme/foo"}},
	}

	res := resolveUninstallTargets([]string{"acme/foo"}, m, nil, panicResolver, false)

	if len(res.MCPTargets) != 0 {
		t.Fatalf("expected apm match to win, got MCPTargets=%+v", res.MCPTargets)
	}
	if len(res.APMTargets) != 1 {
		t.Fatalf("expected single APM target, got %+v", res.APMTargets)
	}
}

// TestResolveUninstallTargets_MarketplaceRefParseError_NotFoundAndContinues
// proves a malformed "#ref" (a semver range constraint, rejected by
// marketplace.ParseRef) is folded into NotFound rather than aborting
// resolution of subsequent PACKAGE arguments.
func TestResolveUninstallTargets_MarketplaceRefParseError_NotFoundAndContinues(t *testing.T) {
	m := &manifest.Manifest{
		ParsedDeps: []*manifest.DependencyReference{{RepoURL: "acme/foo", Source: "git"}},
	}

	res := resolveUninstallTargets([]string{"plug1@mkt1#>=1.0.0", "acme/foo"}, m, nil, panicResolver, false)

	if len(res.NotFound) != 1 || res.NotFound[0].Reason != uninstallNotFoundUnresolvable {
		t.Fatalf("expected single NotFound(Unresolvable), got %+v", res.NotFound)
	}
	if len(res.APMTargets) != 1 || res.APMTargets[0].Name != "acme/foo" {
		t.Fatalf("expected acme/foo still resolved, got %+v", res.APMTargets)
	}
}

// TestUninstallIdentity_LocalAndParent proves uninstallIdentity's local-path
// synthetic key and parent-reference exclusion.
func TestUninstallIdentity_LocalAndParent(t *testing.T) {
	local := &manifest.DependencyReference{IsLocal: true, LocalPath: "./vendor/thing"}
	if key, ok := uninstallIdentity(local); !ok || key != "local:./vendor/thing" {
		t.Fatalf("local identity = (%q, %v), want (local:./vendor/thing, true)", key, ok)
	}

	parent := &manifest.DependencyReference{IsParent: true}
	if key, ok := uninstallIdentity(parent); ok {
		t.Fatalf("parent identity = (%q, %v), want ok=false", key, ok)
	}
}

// TestResolveUninstallTargets_LocalDependency proves a local-path
// dependency can be uninstalled by re-typing its exact path.
func TestResolveUninstallTargets_LocalDependency(t *testing.T) {
	m := &manifest.Manifest{
		ParsedDeps: []*manifest.DependencyReference{
			{IsLocal: true, LocalPath: "./vendor/thing", Source: "local"},
		},
	}

	res := resolveUninstallTargets([]string{"./vendor/thing"}, m, nil, panicResolver, false)

	if len(res.APMTargets) != 1 {
		t.Fatalf("expected single APM target, got notFound=%+v apm=%+v", res.NotFound, res.APMTargets)
	}
}

// TestResolveUninstallTargets_RegistryCanonicalUnparseable proves that a
// registry Resolution whose Canonical string doesn't even parse as a
// dependency string is folded into NotFound(Unresolvable) rather than
// crashing or silently accepting garbage.
func TestResolveUninstallTargets_RegistryCanonicalUnparseable(t *testing.T) {
	m := &manifest.Manifest{}
	resolve := fixedCanonicalResolver("not-a-valid-ref")

	res := resolveUninstallTargets([]string{"plug1@mkt1"}, m, nil, resolve, false)

	if len(res.NotFound) != 1 || res.NotFound[0].Reason != uninstallNotFoundUnresolvable {
		t.Fatalf("expected single NotFound(Unresolvable), got %+v", res.NotFound)
	}
}

// TestResolveUninstallTargets_RegistryDepRefHasNoIdentity proves that when
// ResolvePlugin's structured DepRef itself carries no stable identity (a
// defensive branch -- a marketplace source should never actually produce a
// "git: parent" DepRef, but resolveMarketplaceUninstallTarget must not
// mis-handle it if it somehow did), the result is NotFound(Unresolvable),
// never a target.
func TestResolveUninstallTargets_RegistryDepRefHasNoIdentity(t *testing.T) {
	m := &manifest.Manifest{}
	resolve := func(_, _ string, _ marketplace.ResolveOptions) (*marketplace.Resolution, error) {
		return &marketplace.Resolution{
			DepRef:     &manifest.DependencyReference{IsParent: true},
			Provenance: &marketplace.Provenance{},
		}, nil
	}

	res := resolveUninstallTargets([]string{"plug1@mkt1"}, m, nil, resolve, false)

	if len(res.APMTargets) != 0 {
		t.Fatalf("expected no targets, got %+v", res.APMTargets)
	}
	if len(res.NotFound) != 1 || res.NotFound[0].Reason != uninstallNotFoundUnresolvable {
		t.Fatalf("expected single NotFound(Unresolvable), got %+v", res.NotFound)
	}
}

// TestUninstallIdentity_EmptyRepoURL proves the defensive fallback for a
// non-local, non-parent reference that somehow carries no RepoURL at all.
func TestUninstallIdentity_EmptyRepoURL(t *testing.T) {
	d := &manifest.DependencyReference{Source: "git"}
	if key, ok := uninstallIdentity(d); ok {
		t.Fatalf("identity = (%q, %v), want ok=false", key, ok)
	}
}
