package main

import (
	"context"
	"path/filepath"
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
// target: un-013 (plain "not in apm.yml") warns and lets the rest proceed,
// by ruling; un-017 (marketplace ref unresolvable via lockfile or registry)
// and un-081 (marketplace ref that would need a registry call, but --dry-run
// forbids one) abort the whole uninstall, as the Oracle's packages_not_found
// does (commands/uninstall/cli.py:323-329).
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
	Detail string // the registry's error, for a lookup that failed; may be empty
	// Ref is a marketplace argument's PLUGIN@MARKETPLACE without its #REF,
	// the name the Oracle's resolution lines give it.
	Ref string
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
	Ref       string // PLUGIN@MARKETPLACE without #REF
	Canonical string // what the registry resolved to and that was rejected
}

// uninstallAmbiguous records a PACKAGE argument that matches more than one
// apm.yml entry, which the Oracle refuses rather than guess (Matches is how
// many). Local is the lock name of an argument that is a local path, which
// the Oracle's message names in place of the path (commands/uninstall/
// engine.py:686-698).
type uninstallAmbiguous struct {
	Name    string
	Matches int
	Local   string
}

// uninstallAPMTarget is a PACKAGE argument matched to an existing
// dependencies.apm / devDependencies.apm entry (un-010/011/012).
type uninstallAPMTarget struct {
	Name        string // as typed on the command line
	IdentityKey string // manifest.DependencyReference identity space (see uninstallIdentity)
	IsDev       bool   // true when matched in devDependencies.apm
	// Canonical is what a marketplace argument resolved to through the lock
	// or the registry, which its found line names (engine.py:675-679).
	Canonical string
}

// uninstallMCPTarget is a PACKAGE argument matched to a standalone
// dependencies.mcp / devDependencies.mcp server entry (un-019, apm-go
// enhancement -- Python's uninstall never looks at dependencies.mcp at all,
// see un-019's documented-deviation note).
type uninstallMCPTarget struct {
	Name  string // == the matched MCPDependency.Name
	InDep bool   // declared in dependencies.mcp
	InDev bool   // declared in devDependencies.mcp
}

// uninstallResolution is resolveUninstallTargets's full result: every
// PACKAGE argument ends up in exactly one of these five slices.
type uninstallResolution struct {
	APMTargets          []uninstallAPMTarget
	MCPTargets          []uninstallMCPTarget
	NotFound            []uninstallNotFound
	SupplyChainRejected []uninstallSupplyChainRejection
	Ambiguous           []uninstallAmbiguous
	// RegistryLookup is the PLUGIN@MARKETPLACE of a marketplace argument
	// that was looked up in the registry, which the Oracle announces first
	// (engine.py:528-531).
	RegistryLookup string
	// Invalid is the error of a marketplace argument whose #REF is a semver
	// range, which ends the Oracle's uninstall before any argument's line
	// (engine.py:35-39, cli.py:715-717).
	Invalid error
	// Arguments holds each argument's own resolution, with its one outcome,
	// in command-line order: the Oracle prints each argument's line as it
	// reaches it (commands/uninstall/engine.py:633-731).
	Arguments []*uninstallResolution
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
		arg := &uninstallResolution{}
		resolveUninstallTarget(name, m, lock, resolve, dryRun, arg)
		res.Arguments = append(res.Arguments, arg)
		res.APMTargets = append(res.APMTargets, arg.APMTargets...)
		res.MCPTargets = append(res.MCPTargets, arg.MCPTargets...)
		res.NotFound = append(res.NotFound, arg.NotFound...)
		res.SupplyChainRejected = append(res.SupplyChainRejected, arg.SupplyChainRejected...)
		res.Ambiguous = append(res.Ambiguous, arg.Ambiguous...)
		if res.Invalid == nil {
			res.Invalid = arg.Invalid
		}
	}
	return res
}

// unselected counts the arguments that abort the uninstall: the ambiguous
// ones and the marketplace arguments that could not be resolved. A plain
// not-found argument does not abort, by the ruling.
func (r *uninstallResolution) unselected() int {
	n := len(r.Ambiguous) + len(r.SupplyChainRejected)
	for _, nf := range r.NotFound {
		if nf.Reason != uninstallNotFoundNoMatch {
			n++
		}
	}
	return n
}

func resolveUninstallTarget(name string, m *manifest.Manifest, lock *lockfile.Lockfile, resolve marketplaceRegistryResolver, dryRun bool, res *uninstallResolution) {
	plugin, mkt, ref, ok, err := marketplace.ParseRef(name)
	if err != nil {
		res.Invalid = err
		return
	}
	if ok {
		resolveMarketplaceUninstallTarget(name, plugin, mkt, ref, m, lock, resolve, dryRun, res)
		return
	}
	resolvePlainUninstallTarget(name, m, lock, res)
}

// resolvePlainUninstallTarget handles a PACKAGE argument that is NOT
// "PLUGIN@MARKETPLACE"-shaped: owner/repo, HTTPS/SSH/SCP URL, FQDN shorthand
// (un-010/011), falling back to un-019's standalone-MCP match when it
// doesn't parse as a dependency string at all, or parses fine but matches no
// existing apm.yml entry.
func resolvePlainUninstallTarget(name string, m *manifest.Manifest, lock *lockfile.Lockfile, res *uninstallResolution) {
	if d, err := manifest.ParseDepString(name); err == nil {
		if identity, ok := uninstallIdentity(d); ok {
			switch isDev, matches := findManifestAPMTarget(m, identity); {
			case matches == 1:
				res.APMTargets = append(res.APMTargets, uninstallAPMTarget{Name: name, IdentityKey: identity, IsDev: isDev})
				return
			case matches > 1:
				a := uninstallAmbiguous{Name: name, Matches: matches}
				if d.IsLocal {
					a.Local = localModulesKey(resolveLocalSourceAbs(d.LocalPath))
				}
				res.Ambiguous = append(res.Ambiguous, a)
				return
			}
		}
	}
	switch target, matches := localAliasTarget(name, m, lock); {
	case matches == 1:
		res.APMTargets = append(res.APMTargets, target)
		return
	case matches > 1:
		res.Ambiguous = append(res.Ambiguous, uninstallAmbiguous{Name: name, Matches: matches})
		return
	}
	// un-019 (apm-go enhancement): apm package identity not found (or the
	// argument doesn't even parse as one) -- try it as a standalone MCP
	// server name from `install --mcp NAME`.
	if t := findManifestMCPTarget(m, name); t.InDep || t.InDev {
		res.MCPTargets = append(res.MCPTargets, t)
		return
	}
	res.NotFound = append(res.NotFound, uninstallNotFound{Name: name, Reason: uninstallNotFoundNoMatch})
}

// localAliasTarget selects the apm.yml entry a local package's lock name,
// _local/<name>, stands for, and counts the entries it matches. As in the
// Oracle (models/dependency/selection.py:69-86), only lock entries with
// source local make the alias, through the paths they record, which are the
// declared ones; every local entry declaring one of those paths matches, so
// one path declared twice, or two paths that share a lock name, match twice.
func localAliasTarget(name string, m *manifest.Manifest, lock *lockfile.Lockfile) (target uninstallAPMTarget, matches int) {
	if lock == nil {
		return target, 0
	}
	paths := map[string]bool{}
	for _, dep := range lock.Dependencies {
		if dep.Source == "local" && dep.RepoURL == name && dep.LocalPath != "" {
			paths[dep.LocalPath] = true
		}
	}
	for i, deps := range [][]*manifest.DependencyReference{m.ParsedDeps, m.ParsedDevDeps} {
		for _, d := range deps {
			if !d.IsLocal || !paths[d.LocalPath] {
				continue
			}
			if matches == 0 {
				target = uninstallAPMTarget{Name: name, IdentityKey: "local:" + d.LocalPath, IsDev: i == 1}
			}
			matches++
		}
	}
	return target, matches
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
			identity, canonical := lockedIdentity(dep)
			addMarketplaceTarget(name, identity, canonical, m, res)
			return
		}
	}

	pluginRef := plugin + "@" + mkt
	// Step C (un-081): --dry-run never reaches the registry.
	if dryRun {
		res.NotFound = append(res.NotFound, uninstallNotFound{Name: name, Reason: uninstallNotFoundDryRunSkipped, Ref: pluginRef})
		return
	}

	// Step D: registry fallback, then un-015/un-018's supply-chain guard.
	res.RegistryLookup = pluginRef
	result, err := resolve(plugin, mkt, marketplace.ResolveOptions{VersionSpec: ref})
	if err != nil {
		// un-017: unresolvable via lockfile or registry.
		res.NotFound = append(res.NotFound, uninstallNotFound{Name: name, Reason: uninstallNotFoundUnresolvable, Detail: err.Error(), Ref: pluginRef})
		return
	}
	resolvedRef := result.DepRef
	if resolvedRef == nil {
		if filepath.IsAbs(result.Canonical) {
			// A local marketplace's plugin is the local package at that
			// path, as the Oracle selects its canonical (models/dependency/
			// selection.py:56-59).
			resolvedRef = &manifest.DependencyReference{IsLocal: true, LocalPath: result.Canonical}
		} else if resolvedRef, err = depRefFromMarketplaceCanonical(result.Canonical); err != nil {
			res.NotFound = append(res.NotFound, uninstallNotFound{Name: name, Reason: uninstallNotFoundUnresolvable, Ref: pluginRef})
			return
		}
	}
	identity, idOK := uninstallIdentity(resolvedRef)
	if !idOK {
		res.NotFound = append(res.NotFound, uninstallNotFound{Name: name, Reason: uninstallNotFoundUnresolvable, Ref: pluginRef})
		return
	}

	// un-015: when a lockfile exists, the registry's canonical MUST already
	// be locked, or this is refused outright (engine.py:539-547). un-018:
	// with no lockfile there's nothing to cross-check against, so the
	// resolved identity is only required to be a live apm.yml entry.
	if lock != nil && !lockHasIdentity(lock, identity) {
		res.SupplyChainRejected = append(res.SupplyChainRejected, uninstallSupplyChainRejection{Name: name, Ref: pluginRef, Canonical: result.Canonical})
		return
	}
	addMarketplaceTarget(name, identity, result.Canonical, m, res)
}

// addMarketplaceTarget selects the apm.yml entry a marketplace argument
// resolved to, as select_manifest_dependency selects it (engine.py:673-698).
func addMarketplaceTarget(name, identity, canonical string, m *manifest.Manifest, res *uninstallResolution) {
	switch isDev, matches := findManifestAPMTarget(m, identity); {
	case matches == 1:
		res.APMTargets = append(res.APMTargets, uninstallAPMTarget{Name: name, IdentityKey: identity, IsDev: isDev, Canonical: canonical})
	case matches > 1:
		res.Ambiguous = append(res.Ambiguous, uninstallAmbiguous{Name: name, Matches: matches})
	default:
		// Resolved, but apm.yml no longer lists it (un-013).
		res.NotFound = append(res.NotFound, uninstallNotFound{Name: name, Reason: uninstallNotFoundNoMatch})
	}
}

// lockedIdentity is a lock entry's identity in uninstallIdentity's space and
// the canonical name the Oracle gives it, its unique key: the declared path
// of a local package (models/dependency/identity.py:212-215), else its key.
func lockedIdentity(dep *lockfile.LockedDep) (identity, canonical string) {
	if dep.Source == "local" && dep.LocalPath != "" {
		return "local:" + dep.LocalPath, dep.LocalPath
	}
	return dep.UniqueKey(), dep.UniqueKey()
}

// lockHasIdentity reports whether lock records the package of identity.
func lockHasIdentity(lock *lockfile.Lockfile, identity string) bool {
	for i := range lock.Dependencies {
		if id, _ := lockedIdentity(&lock.Dependencies[i]); id == identity {
			return true
		}
	}
	return false
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

// findManifestAPMTarget counts the dependencies.apm / devDependencies.apm
// entries of m that identity matches (un-012: both sections are always
// scanned) and reports the section of the first. More than one is ambiguous:
// the Oracle selects none of them (models/dependency/selection.py:88-104).
func findManifestAPMTarget(m *manifest.Manifest, identity string) (isDev bool, matches int) {
	if identity == "" {
		return false, 0
	}
	for i, deps := range [][]*manifest.DependencyReference{m.ParsedDeps, m.ParsedDevDeps} {
		for _, d := range deps {
			if k, ok := uninstallIdentity(d); ok && k == identity {
				if matches == 0 {
					isDev = i == 1
				}
				matches++
			}
		}
	}
	return isDev, matches
}

// findManifestMCPTarget reports which of dependencies.mcp and
// devDependencies.mcp have a server whose Name exactly matches name (un-019;
// exact string match, same as mcpinstall.go's upsertMCPEntry lookup -- MCP
// server names are not case-folded anywhere else in this codebase).
func findManifestMCPTarget(m *manifest.Manifest, name string) uninstallMCPTarget {
	t := uninstallMCPTarget{Name: name}
	for _, s := range m.MCPServers {
		t.InDep = t.InDep || s.Name == name
	}
	for _, s := range m.MCPDevServers {
		t.InDev = t.InDev || s.Name == name
	}
	return t
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
