package main

import (
	"context"
	"strings"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/marketplace"
)

// Step 6 of implement.md: PACKAGE-argument resolution and comparison for
// `apm-go uninstall` (un-010~019). This file is a standalone library --
// nothing here is wired into the cobra CLI yet (that is step 8's job); it
// only classifies each PACKAGE argument the user typed into exactly one of
// four buckets (uninstallResolution below) so a future orchestrator can
// decide what to actually remove.

// uninstallNotFoundReason distinguishes *why* a PACKAGE argument produced no
// target, purely for a caller's warning message -- un-013 (plain "not in
// apm.yml"), un-017 (marketplace ref unresolvable via lockfile or registry),
// and un-081 (marketplace ref that would need a registry call, but
// --dry-run forbids one) all still fold into the same "warn and continue,
// don't abort the rest" bucket (mirrors cli.py:277-288's uniform
// packages_not_found handling).
type uninstallNotFoundReason int

const (
	uninstallNotFoundNoMatch uninstallNotFoundReason = iota
	uninstallNotFoundDryRunSkipped
	uninstallNotFoundUnresolvable
)

// uninstallNotFound records a PACKAGE argument that resolved to nothing.
type uninstallNotFound struct {
	Name   string
	Reason uninstallNotFoundReason
	Detail string // extra context (e.g. underlying resolve error); may be empty
}

// uninstallSupplyChainRejection records a marketplace-ref PACKAGE argument
// whose registry-resolved canonical identity is absent from the lockfile --
// un-015's supply-chain guard. Deliberately a separate bucket from
// uninstallNotFound: a compromised/rogue registry response must never be
// able to make uninstall touch a package the project never actually locked,
// and the rejection names the canonical that was refused so it is
// diagnostically distinguishable from an ordinary "not found".
type uninstallSupplyChainRejection struct {
	Name      string // as typed on the command line
	Canonical string // the identity the registry resolved to and that was rejected
}

// uninstallAPMTarget is a PACKAGE argument matched to an existing
// dependencies.apm / devDependencies.apm entry (un-010/011/012).
type uninstallAPMTarget struct {
	Name        string // as typed on the command line
	IdentityKey string // manifest.DependencyReference identity space (see uninstallIdentity)
	IsDev       bool   // true when matched in devDependencies.apm
}

// uninstallMCPTarget is a PACKAGE argument matched to a standalone
// dependencies.mcp / devDependencies.mcp server entry (un-019, apm-go
// enhancement -- Python's uninstall never looks at dependencies.mcp at all,
// see un-019's documented-deviation note).
type uninstallMCPTarget struct {
	Name string // == the matched MCPDependency.Name
}

// uninstallResolution is resolveUninstallTargets's full result: every
// PACKAGE argument ends up in exactly one of these four slices.
type uninstallResolution struct {
	APMTargets          []uninstallAPMTarget
	MCPTargets          []uninstallMCPTarget
	NotFound            []uninstallNotFound
	SupplyChainRejected []uninstallSupplyChainRejection
}

// marketplaceRegistryResolver abstracts marketplace.ResolvePlugin so tests
// can inject a fake -- notably one that panics if called at all, which is
// how un-014's "lockfile/manifest offline match must never reach the
// registry" and un-081's "--dry-run must never reach the registry"
// behaviors get proven (Review Gate A).
type marketplaceRegistryResolver func(plugin, mkt string, opts marketplace.ResolveOptions) (*marketplace.Resolution, error)

// defaultMarketplaceRegistryResolver is the real implementation; callers
// outside tests should always pass this (mirrors resolvePositionalPackage's
// own context.Background() use -- no cancellation/timeout plumbing exists
// anywhere else in this resolution path either).
func defaultMarketplaceRegistryResolver(plugin, mkt string, opts marketplace.ResolveOptions) (*marketplace.Resolution, error) {
	return marketplace.ResolvePlugin(context.Background(), plugin, mkt, opts)
}

// resolveUninstallTargets resolves every uninstall PACKAGE argument in names
// against m (the already-parsed apm.yml) and lock (the already-parsed
// lockfile, nil if apm.lock.yaml doesn't exist -- un-018). It never mutates
// m or lock, and never touches disk; it only classifies. resolve is never
// invoked when dryRun is true (un-081), regardless of whether it is nil --
// passing a nil resolver together with dryRun=false will panic if a
// marketplace ref actually needs the registry fallback, which is
// intentional: production callers must always supply
// defaultMarketplaceRegistryResolver.
func resolveUninstallTargets(names []string, m *manifest.Manifest, lock *lockfile.Lockfile, resolve marketplaceRegistryResolver, dryRun bool) *uninstallResolution {
	res := &uninstallResolution{}
	for _, name := range names {
		resolveUninstallTarget(name, m, lock, resolve, dryRun, res)
	}
	return res
}

func resolveUninstallTarget(name string, m *manifest.Manifest, lock *lockfile.Lockfile, resolve marketplaceRegistryResolver, dryRun bool, res *uninstallResolution) {
	plugin, mkt, ref, ok, err := marketplace.ParseRef(name)
	if err != nil {
		// e.g. a "#ref" that looks like a semver range constraint --
		// malformed input, not a crash: log-and-skip like un-017.
		res.NotFound = append(res.NotFound, uninstallNotFound{Name: name, Reason: uninstallNotFoundUnresolvable, Detail: err.Error()})
		return
	}
	if ok {
		resolveMarketplaceUninstallTarget(name, plugin, mkt, ref, m, lock, resolve, dryRun, res)
		return
	}
	resolvePlainUninstallTarget(name, m, res)
}

// resolvePlainUninstallTarget handles a PACKAGE argument that is NOT
// "PLUGIN@MARKETPLACE"-shaped: owner/repo, HTTPS/SSH/SCP URL, FQDN shorthand
// (un-010/011), falling back to un-019's standalone-MCP match when it
// doesn't parse as a dependency string at all, or parses fine but matches no
// existing apm.yml entry.
func resolvePlainUninstallTarget(name string, m *manifest.Manifest, res *uninstallResolution) {
	if d, err := manifest.ParseDepString(name); err == nil {
		if identity, ok := uninstallIdentity(d); ok {
			if isDev, found := findManifestAPMTarget(m, identity); found {
				res.APMTargets = append(res.APMTargets, uninstallAPMTarget{Name: name, IdentityKey: identity, IsDev: isDev})
				return
			}
		}
	}
	// un-019 (apm-go enhancement): apm package identity not found (or the
	// argument doesn't even parse as one) -- try it as a standalone MCP
	// server name from `install --mcp NAME`.
	if findManifestMCPTarget(m, name) {
		res.MCPTargets = append(res.MCPTargets, uninstallMCPTarget{Name: name})
		return
	}
	res.NotFound = append(res.NotFound, uninstallNotFound{Name: name, Reason: uninstallNotFoundNoMatch})
}

// resolveMarketplaceUninstallTarget handles a "PLUGIN@MARKETPLACE[#REF]"
// PACKAGE argument (un-014~018). ref is deliberately never consulted for
// identity comparison anywhere in this function (un-016: "#ref" is ignored
// for uninstall matching) -- it is only ever forwarded into
// marketplace.ResolveOptions.VersionSpec for the registry-fallback branch,
// exactly as install already does.
func resolveMarketplaceUninstallTarget(name, plugin, mkt, ref string, m *manifest.Manifest, lock *lockfile.Lockfile, resolve marketplaceRegistryResolver, dryRun bool, res *uninstallResolution) {
	// Step A: apm.yml may already carry this exact plugin/marketplace pair
	// as an unresolved dict-form entry ({name, marketplace, version}) --
	// match it directly. No lockfile/registry lookup applies here at all:
	// the identity is already sitting in apm.yml, nothing to look up.
	if isDev, identity, found := findManifestMarketplaceDictTarget(m, plugin, mkt); found {
		res.APMTargets = append(res.APMTargets, uninstallAPMTarget{Name: name, IdentityKey: identity, IsDev: isDev})
		return
	}

	// Step B (un-014): lockfile offline-first. A CLI `install PLUGIN@MKT`
	// persists an ordinary already-resolved git/local apm.yml entry, so the
	// only surviving evidence of its marketplace origin is the lockfile's
	// provenance (DiscoveredVia/MarketplacePluginName, mkt-031).
	if lock != nil {
		for i := range lock.Dependencies {
			dep := &lock.Dependencies[i]
			if !strings.EqualFold(dep.DiscoveredVia, mkt) || !strings.EqualFold(dep.MarketplacePluginName, plugin) {
				continue
			}
			identity := dep.UniqueKey()
			if isDev, found := findManifestAPMTarget(m, identity); found {
				res.APMTargets = append(res.APMTargets, uninstallAPMTarget{Name: name, IdentityKey: identity, IsDev: isDev})
			} else {
				// Lockfile still remembers it, but apm.yml no longer lists
				// it -- nothing left to uninstall from apm.yml (un-013).
				res.NotFound = append(res.NotFound, uninstallNotFound{Name: name, Reason: uninstallNotFoundNoMatch})
			}
			return
		}
	}

	// Step C (un-081): --dry-run never reaches the registry.
	if dryRun {
		res.NotFound = append(res.NotFound, uninstallNotFound{Name: name, Reason: uninstallNotFoundDryRunSkipped})
		return
	}

	// Step D: registry fallback, then un-015/un-018's supply-chain guard.
	result, err := resolve(plugin, mkt, marketplace.ResolveOptions{VersionSpec: ref})
	if err != nil {
		// un-017: unresolvable via lockfile or registry -- log and skip.
		res.NotFound = append(res.NotFound, uninstallNotFound{Name: name, Reason: uninstallNotFoundUnresolvable, Detail: err.Error()})
		return
	}
	resolvedRef := result.DepRef
	if resolvedRef == nil {
		resolvedRef, err = depRefFromMarketplaceCanonical(result.Canonical)
		if err != nil {
			res.NotFound = append(res.NotFound, uninstallNotFound{Name: name, Reason: uninstallNotFoundUnresolvable, Detail: err.Error()})
			return
		}
	}
	identity, idOK := uninstallIdentity(resolvedRef)
	if !idOK {
		res.NotFound = append(res.NotFound, uninstallNotFound{Name: name, Reason: uninstallNotFoundUnresolvable})
		return
	}

	// un-015: when the lockfile has any anchor at all, the registry's
	// canonical MUST already be locked, or this is refused outright --
	// named, so a caller can distinguish it from an ordinary "not found"
	// (this is what proves the guard runs before any removal: a rejected
	// package never reaches APMTargets). un-018: with no lockfile anchor,
	// there's nothing to cross-check against, so this guard is skipped and
	// the resolved identity is only required to already be a live apm.yml
	// entry (checked immediately below either way).
	if lockHasAnchor(lock) && lock.FindByKey(identity) == nil {
		res.SupplyChainRejected = append(res.SupplyChainRejected, uninstallSupplyChainRejection{Name: name, Canonical: identity})
		return
	}
	if isDev, found := findManifestAPMTarget(m, identity); found {
		res.APMTargets = append(res.APMTargets, uninstallAPMTarget{Name: name, IdentityKey: identity, IsDev: isDev})
		return
	}
	res.NotFound = append(res.NotFound, uninstallNotFound{Name: name, Reason: uninstallNotFoundNoMatch})
}

// lockHasAnchor reports whether lock provides any offline anchor at all for
// the supply-chain guard (un-015 vs un-018's no-lockfile weaker path).
func lockHasAnchor(lock *lockfile.Lockfile) bool {
	return lock != nil && len(lock.Dependencies) > 0
}

// findManifestMarketplaceDictTarget looks for an apm.yml dependencies.apm /
// devDependencies.apm dict-form entry ({name, marketplace, version},
// Source=="marketplace") whose plugin/marketplace names match plugin/mkt
// case-insensitively (mirrors marketplace.ResolvePlugin's own
// case-insensitive plugin lookup, mkt-024). The returned identity is the
// entry's own DependencyReference.IdentityKey() (== its
// "_marketplace/<mkt>/<name>" RepoURL placeholder, which already uniquely
// encodes the pair with apm.yml's original casing).
func findManifestMarketplaceDictTarget(m *manifest.Manifest, plugin, mkt string) (isDev bool, identity string, found bool) {
	for _, d := range m.ParsedDeps {
		if d.Source == "marketplace" && strings.EqualFold(d.MarketplaceName, mkt) && strings.EqualFold(d.MarketplacePluginName, plugin) {
			return false, d.IdentityKey(), true
		}
	}
	for _, d := range m.ParsedDevDeps {
		if d.Source == "marketplace" && strings.EqualFold(d.MarketplaceName, mkt) && strings.EqualFold(d.MarketplacePluginName, plugin) {
			return true, d.IdentityKey(), true
		}
	}
	return false, "", false
}

// findManifestAPMTarget reports whether identity matches any
// dependencies.apm / devDependencies.apm entry in m (un-012: both sections
// are always scanned), and if so which section it was found in.
func findManifestAPMTarget(m *manifest.Manifest, identity string) (isDev bool, found bool) {
	if identity == "" {
		return false, false
	}
	for _, d := range m.ParsedDeps {
		if k, ok := uninstallIdentity(d); ok && k == identity {
			return false, true
		}
	}
	for _, d := range m.ParsedDevDeps {
		if k, ok := uninstallIdentity(d); ok && k == identity {
			return true, true
		}
	}
	return false, false
}

// findManifestMCPTarget reports whether name exactly matches a
// dependencies.mcp / devDependencies.mcp server's Name (un-019; exact
// string match, same as mcpinstall.go's upsertMCPEntry lookup -- MCP server
// names are not case-folded anywhere else in this codebase).
func findManifestMCPTarget(m *manifest.Manifest, name string) bool {
	for _, s := range m.MCPServers {
		if s.Name == name {
			return true
		}
	}
	for _, s := range m.MCPDevServers {
		if s.Name == name {
			return true
		}
	}
	return false
}

// uninstallIdentity returns the comparison key used to match a parsed CLI
// PACKAGE argument against an apm.yml entry or a lockfile dependency
// (un-011: ignores git ref/alias -- same key space as
// DependencyReference.IdentityKey() / LockedDep.UniqueKey(), which also
// deliberately ignores Host, so owner/repo, HTTPS URL, SSH URL, SCP form,
// and FQDN shorthand all converge on the same identity for the same
// repository). Local dependencies have no IdentityKey() at all
// (deliberately, matching deploy.DepRefKey), so they get their own
// synthetic "local:"-prefixed key here instead -- un-010 only requires
// owner/repo/URL/SSH/FQDN coverage, but there's no reason a local-path
// package couldn't also be a valid uninstall target by its path. Parent
// references (git: parent) have no stable identity at all and are never a
// valid uninstall target.
func uninstallIdentity(d *manifest.DependencyReference) (string, bool) {
	if d.IsParent {
		return "", false
	}
	if d.IsLocal {
		return "local:" + d.LocalPath, true
	}
	key := d.IdentityKey()
	if key == "" {
		return "", false
	}
	return key, true
}
