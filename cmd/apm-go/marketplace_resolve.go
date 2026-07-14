package main

import (
	"context"
	"fmt"
	"os"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/marketplace"
	"github.com/apm-go/apm/internal/resolver"
)

// newMarketplaceResolveFunc builds the resolver.MarketplaceResolveFunc
// closure that lets internal/resolver's BFS collapse a Source=="marketplace"
// apm.yml dict-form dependency (mkt-033: {name, marketplace, version}) into
// an ordinary git/local DependencyReference via marketplace.ResolvePlugin --
// the SAME resolution path resolvePositionalPackage already uses for a CLI
// "PLUGIN@MARKETPLACE[#REF]" argument, now reused for both root and
// transitive marketplace dict deps discovered during BFS (mkt-029). F1 fix:
// previously, resolver.Resolve's BFS had no such path at all -- a dict-form
// marketplace dependency fell into resolver.go's git-literal default case
// and was "resolved" against its own bogus "_marketplace/<mkt>/<name>"
// RepoURL placeholder instead of ever being installed.
//
// internal/resolver never imports internal/marketplace, and vice versa (see
// resolver.MarketplaceResolveFunc's doc comment and
// internal/marketplace/version_spec.go's TagLister comment) -- this closure
// is the dependency-injection seam cmd/apm-go uses to wire the two together
// without an import cycle.
//
// mkt-034's ref-swap-pin/shadow warnings are printed to stderr here, never
// blocking (mirroring resolvePositionalPackage's own handling). Provenance
// comes back through resolver.ResolutionResult's own MarketplaceProvenance
// map (see mergeMarketplaceProvenance below), not through any side channel
// this closure writes to directly -- keeping it a pure function of its
// argument, safe to call from concurrent BFS re-expansion if that is ever
// introduced.
func newMarketplaceResolveFunc() resolver.MarketplaceResolveFunc {
	return func(dep *manifest.DependencyReference) (*manifest.DependencyReference, *resolver.MarketplaceProvenance, error) {
		res, err := marketplace.ResolvePlugin(context.Background(), dep.MarketplacePluginName, dep.MarketplaceName,
			marketplace.ResolveOptions{VersionSpec: dep.MarketplaceVersionSpec})
		if err != nil {
			return nil, nil, err
		}
		for _, w := range res.Warnings {
			fmt.Fprintf(os.Stderr, "[!] %s\n", w)
		}

		// mkt-027: a structured DepRef always wins over parsing Canonical --
		// same rule resolvePositionalPackage applies for the CLI path.
		resolved := res.DepRef
		if resolved == nil {
			resolved, err = depRefFromMarketplaceCanonical(res.Canonical)
			if err != nil {
				return nil, nil, err
			}
		}

		var provenance *resolver.MarketplaceProvenance
		if res.Provenance != nil {
			provenance = &resolver.MarketplaceProvenance{
				DiscoveredVia:         res.Provenance.DiscoveredVia,
				MarketplacePluginName: res.Provenance.MarketplacePluginName,
				SourceURL:             res.Provenance.SourceURL,
				SourceDigest:          res.Provenance.SourceDigest,
			}
		}
		return resolved, provenance, nil
	}
}

// mergeMarketplaceProvenance converts resolver's package-local provenance map
// (keyed identically to ResolvedDep.Key/deploy.DepRefKey) into cmd/apm-go's
// marketplace.Provenance-keyed map and merges it into dst, so buildLockfile's
// single marketplaceProvenance parameter serves both the CLI
// PLUGIN@MARKETPLACE positional-package path and the apm.yml dict-form path
// uniformly (mkt-031). The two paths can never collide on the same key: a
// CLI-resolved package is added to m.ParsedDeps as an already-resolved
// git/local dep before BFS ever runs, so it's never Source=="marketplace"
// by the time Resolve sees it and therefore never appears in src.
func mergeMarketplaceProvenance(dst map[string]*marketplace.Provenance, src map[string]*resolver.MarketplaceProvenance) {
	for key, p := range src {
		dst[key] = &marketplace.Provenance{
			DiscoveredVia:         p.DiscoveredVia,
			MarketplacePluginName: p.MarketplacePluginName,
			SourceURL:             p.SourceURL,
			SourceDigest:          p.SourceDigest,
		}
	}
}

// validatePersistableRef guards install.go's apm.yml persist path (mkt-030):
// a dependency about to be turned into apm.yml-persisted content (either the
// raw CLI string, for an ordinary package, or its resolved canonical, for a
// marketplace one) must not still be an unresolved marketplace reference.
// resolvePositionalPackage can never itself produce such a ref today --
// ParseRef's hit path always routes through marketplace.ResolvePlugin before
// returning (mkt-029) -- so in practice this can only ever fire if that
// invariant is broken by a future change; the point is that such a break
// fails loud with a clear "cannot persist" error instead of silently writing
// broken/incomplete content, mirroring the Python original's
// to_apm_yml_entry() raise ValueError guard. Extracted as its own function
// purely so the guard itself is unit-testable in isolation.
func validatePersistableRef(pkg string, ref *manifest.DependencyReference) error {
	if err := ref.ValidateResolved(); err != nil {
		return fmt.Errorf("cannot persist package %q to apm.yml: %w", pkg, err)
	}
	return nil
}
