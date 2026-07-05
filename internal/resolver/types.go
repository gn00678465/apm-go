package resolver

import (
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/semver"
)

// TagLister abstracts git ls-remote for testing.
type TagLister interface {
	ListTags(repoURL string) ([]semver.TagInfo, error)
}

// PackageLoader abstracts package download/clone for testing.
// Given a dependency reference and its resolved version, load and parse
// the sub-manifest to discover transitive dependencies.
type PackageLoader interface {
	LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error)
}

// MarketplaceProvenance mirrors internal/marketplace.Provenance's four
// mkt-031 fields (DiscoveredVia/MarketplacePluginName/SourceURL/
// SourceDigest), redeclared here so internal/resolver never needs to import
// internal/marketplace -- see MarketplaceResolveFunc's doc comment for why.
type MarketplaceProvenance struct {
	DiscoveredVia         string
	MarketplacePluginName string
	SourceURL             string
	SourceDigest          string
}

// MarketplaceResolveFunc collapses a Source=="marketplace" DependencyReference
// (mkt-033's apm.yml dict-form {name, marketplace, version} entry, classified
// KindMarketplace) into an ordinary git/local DependencyReference (mkt-029 --
// there is no marketplace primitive past this point) BEFORE Resolve's BFS
// bookkeeping (dedup key, constraints, kind classification) ever looks at it.
// Resolve calls this uniformly for every KindMarketplace entry it dequeues,
// root or transitive alike (mirrors the Python original's
// resolve_marketplace_plugin call sites at apm_resolver.py:547/565/711), so
// a marketplace dict dependency declared inside a transitively-fetched
// sub-manifest resolves exactly the same way a root one does.
//
// internal/marketplace deliberately does not import internal/resolver (see
// version_spec.go's TagLister doc comment), and by the same reasoning
// internal/resolver does not import internal/marketplace either -- this
// function type is the dependency-injection seam cmd/apm wires with a
// closure around marketplace.ResolvePlugin, keeping both packages
// import-cycle-free of each other.
type MarketplaceResolveFunc func(dep *manifest.DependencyReference) (resolved *manifest.DependencyReference, provenance *MarketplaceProvenance, err error)

// ResolverConfig holds resolution configuration.
type ResolverConfig struct {
	MaxDepth int // default 50 (req-rs-006)

	// MarketplaceResolve resolves KindMarketplace dependencies (mkt-033).
	// nil (the zero value, e.g. any caller that hasn't been updated to wire
	// it) means a KindMarketplace dependency is a hard error rather than a
	// silent no-op or a bogus placeholder RepoURL reaching PackageLoader/
	// TagLister (fail loud, not fail silent).
	MarketplaceResolve MarketplaceResolveFunc
}

func (c ResolverConfig) maxDepth() int {
	if c.MaxDepth <= 0 {
		return 50
	}
	return c.MaxDepth
}

// ResolvedDep represents a fully resolved dependency in the graph.
type ResolvedDep struct {
	Key         string // unique key (repo_url or repo_url/virtual_path)
	RepoURL     string
	VirtualPath string
	Kind        ReferenceKind
	Constraint  string // original manifest range (verbatim)
	ResolvedTag string // pinned tag (git-semver)
	ResolvedRef string // pinned ref (git-literal: branch/tag/SHA)
	Commit      string // resolved commit SHA
	Depth       int
	ResolvedBy  string // chain that contributed tightest constraint
}

// ConstraintEntry records one constraint path to a package identity.
type ConstraintEntry struct {
	Chain      []string // chain from root: ["owner/repo@^1.2.0", "owner/bar@^2.0.0"]
	Constraint string   // the ref/range for this entry
	Depth      int
}

// ResolutionResult is the output of the resolver.
type ResolutionResult struct {
	Deps        []ResolvedDep
	Diagnostics []manifest.Diagnostic

	// MarketplaceProvenance carries the provenance MarketplaceResolveFunc
	// returned for every KindMarketplace dependency Resolve collapsed during
	// this call (mkt-031), keyed identically to ResolvedDep.Key (the same
	// depKey/deploy.DepRefKey coordinate). nil/empty when no marketplace
	// dependency was resolved. Callers merge this into whatever provenance
	// map they build from other sources (e.g. cmd/apm's CLI
	// PLUGIN@MARKETPLACE positional-argument path).
	MarketplaceProvenance map[string]*MarketplaceProvenance
}
