package resolver

import (
	"fmt"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/semver"
)

// panicLoader fails the test the instant LoadPackage is invoked -- used to
// prove a KindMarketplace dependency never reaches PackageLoader with its
// placeholder "_marketplace/<mkt>/<name>" RepoURL (the F1 bug this file
// guards against: that placeholder used to fall into the BFS switch's
// default case and get treated as an ordinary git-literal dependency).
type panicLoader struct{}

func (p *panicLoader) LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error) {
	panic(fmt.Sprintf("LoadPackage must never be called for an unresolved marketplace dependency (got RepoURL=%q)", ref.RepoURL))
}

func marketplaceDep(mkt, name string) *manifest.DependencyReference {
	return &manifest.DependencyReference{
		RepoURL:               "_marketplace/" + mkt + "/" + name,
		Source:                "marketplace",
		MarketplaceName:       mkt,
		MarketplacePluginName: name,
	}
}

// TestResolve_MarketplaceDep_RootResolvedViaInjectedFunc is F1's root-level
// regression: a Source=="marketplace" root dependency must be collapsed to
// an ordinary git/local dependency through ResolverConfig.MarketplaceResolve
// before the BFS's own dedup/kind bookkeeping runs -- not fall into the old
// default-case handling with its placeholder RepoURL.
func TestResolve_MarketplaceDep_RootResolvedViaInjectedFunc(t *testing.T) {
	resolveCalls := 0
	resolveFn := func(dep *manifest.DependencyReference) (*manifest.DependencyReference, *MarketplaceProvenance, error) {
		resolveCalls++
		if dep.MarketplaceName != "acme" || dep.MarketplacePluginName != "p" {
			t.Fatalf("resolveFn called with unexpected dep: %+v", dep)
		}
		return makeDep("acme-owner/acme-repo", "main"),
			&MarketplaceProvenance{DiscoveredVia: "acme", MarketplacePluginName: "p"}, nil
	}

	loader := &mockPackageLoader{packages: map[string]*manifest.Manifest{
		"acme-owner/acme-repo@main": makeManifest("p"),
	}}

	root := makeManifest("root", marketplaceDep("acme", "p"))
	result, err := Resolve(root, nil, &mockTagLister{}, loader, ResolverConfig{MarketplaceResolve: resolveFn})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolveCalls != 1 {
		t.Fatalf("resolveFn called %d times, want 1", resolveCalls)
	}
	if len(result.Deps) != 1 {
		t.Fatalf("deps count = %d, want 1: %+v", len(result.Deps), result.Deps)
	}
	dep := result.Deps[0]
	if dep.Key != "acme-owner/acme-repo" {
		t.Errorf("dep.Key = %q, want acme-owner/acme-repo (placeholder RepoURL must not leak into the resolved graph)", dep.Key)
	}
	if dep.Kind != KindGitLiteral {
		t.Errorf("dep.Kind = %v, want KindGitLiteral (collapsed dep is classified as an ordinary dependency)", dep.Kind)
	}
	prov := result.MarketplaceProvenance["acme-owner/acme-repo"]
	if prov == nil || prov.DiscoveredVia != "acme" || prov.MarketplacePluginName != "p" {
		t.Errorf("MarketplaceProvenance[acme-owner/acme-repo] = %+v, want DiscoveredVia=acme MarketplacePluginName=p", prov)
	}
}

// TestResolve_MarketplaceDep_TransitiveResolvedViaInjectedFunc is F1's
// transitive-level regression (mirroring apm_resolver.py:711's root+
// transitive symmetry): a marketplace dict dependency declared inside a
// sub-manifest fetched partway through BFS must go through the SAME
// MarketplaceResolve injection a root one goes through -- not just the root
// queue seed.
func TestResolve_MarketplaceDep_TransitiveResolvedViaInjectedFunc(t *testing.T) {
	var resolvedFor []string
	resolveFn := func(dep *manifest.DependencyReference) (*manifest.DependencyReference, *MarketplaceProvenance, error) {
		resolvedFor = append(resolvedFor, dep.MarketplacePluginName+"@"+dep.MarketplaceName)
		return makeDep("acme/q", "main"),
			&MarketplaceProvenance{DiscoveredVia: dep.MarketplaceName, MarketplacePluginName: dep.MarketplacePluginName}, nil
	}

	tags := &mockTagLister{tags: map[string][]semver.TagInfo{
		"acme/a": makeTags("v1.0.0"),
	}}
	loader := &mockPackageLoader{packages: map[string]*manifest.Manifest{
		// acme/a's own apm.yml declares a transitive marketplace dict dep
		// (mkt2/q), exactly like a root manifest can.
		"acme/a@v1.0.0": makeManifest("a", marketplaceDep("mkt2", "q")),
		"acme/q@main":   makeManifest("q"),
	}}

	root := makeManifest("root", makeDep("acme/a", "^1.0.0"))
	result, err := Resolve(root, nil, tags, loader, ResolverConfig{MarketplaceResolve: resolveFn})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolvedFor) != 1 || resolvedFor[0] != "q@mkt2" {
		t.Fatalf("resolvedFor = %v, want [q@mkt2] (transitive KindMarketplace dep must reach the injected resolver)", resolvedFor)
	}

	keys := map[string]bool{}
	for _, d := range result.Deps {
		keys[d.Key] = true
	}
	if !keys["acme/a"] {
		t.Error("result.Deps missing acme/a")
	}
	if !keys["acme/q"] {
		t.Error("result.Deps missing acme/q (collapsed transitive marketplace dep)")
	}
	prov := result.MarketplaceProvenance["acme/q"]
	if prov == nil || prov.DiscoveredVia != "mkt2" {
		t.Errorf("MarketplaceProvenance[acme/q] = %+v, want DiscoveredVia=mkt2", prov)
	}
}

// TestResolve_MarketplaceDep_NilResolverFailsLoud covers the fail-loud
// requirement: an existing caller that hasn't wired
// ResolverConfig.MarketplaceResolve (the zero value, nil) must get an
// explicit error for a KindMarketplace dependency, never a silent skip and
// never the old default-case behavior that would hand panicLoader the
// placeholder RepoURL.
func TestResolve_MarketplaceDep_NilResolverFailsLoud(t *testing.T) {
	root := makeManifest("root", marketplaceDep("acme", "p"))
	_, err := Resolve(root, nil, &mockTagLister{}, &panicLoader{}, ResolverConfig{})
	if err == nil {
		t.Fatal("expected error when no MarketplaceResolve is configured")
	}
	if !strings.Contains(err.Error(), "marketplace") {
		t.Errorf("error should mention marketplace: %v", err)
	}
}

// TestResolve_MarketplaceDep_ResolverErrorPropagated proves a resolution
// failure (e.g. marketplace.ErrMarketplaceNotFound in production) surfaces
// as a Resolve error instead of being swallowed.
func TestResolve_MarketplaceDep_ResolverErrorPropagated(t *testing.T) {
	resolveFn := func(dep *manifest.DependencyReference) (*manifest.DependencyReference, *MarketplaceProvenance, error) {
		return nil, nil, fmt.Errorf("marketplace %q is not registered", dep.MarketplaceName)
	}
	root := makeManifest("root", marketplaceDep("acme", "p"))
	_, err := Resolve(root, nil, &mockTagLister{}, &panicLoader{}, ResolverConfig{MarketplaceResolve: resolveFn})
	if err == nil || !strings.Contains(err.Error(), `marketplace "acme" is not registered`) {
		t.Fatalf("expected wrapped resolver error, got %v", err)
	}
}

// TestResolve_MarketplaceDep_ResolverReturnsUnresolved_Errors proves a buggy
// MarketplaceResolveFunc that hands back a dep still marked
// Source=="marketplace" is rejected rather than looping or being silently
// accepted (mkt-029's invariant: past this point, nothing is ever
// Source=="marketplace" again).
func TestResolve_MarketplaceDep_ResolverReturnsUnresolved_Errors(t *testing.T) {
	resolveFn := func(dep *manifest.DependencyReference) (*manifest.DependencyReference, *MarketplaceProvenance, error) {
		return dep, nil, nil // still Source=="marketplace" -- buggy resolver
	}
	root := makeManifest("root", marketplaceDep("acme", "p"))
	_, err := Resolve(root, nil, &mockTagLister{}, &panicLoader{}, ResolverConfig{MarketplaceResolve: resolveFn})
	if err == nil {
		t.Fatal("expected error when MarketplaceResolve returns a still-unresolved dep")
	}
}
