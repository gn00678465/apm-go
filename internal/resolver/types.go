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

// ResolverConfig holds resolution configuration.
type ResolverConfig struct {
	MaxDepth int // default 50 (req-rs-006)
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
}
