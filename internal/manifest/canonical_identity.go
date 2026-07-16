package manifest

import (
	"fmt"
	"strings"
)

// CanonicalRepoIdentity returns the identity used to decide whether two
// DependencyReferences point at the "same repository", independent of
// their resolution selector (git ref/tag, version constraint, virtual
// path, alias). It is the single canonicalization point design.md §0
// requires for every repo-identity comparison across the codebase --
// resolver dedup, requestedKeys/existing maps, manifest lookup/update/
// reset, lockfile comparison, and deploy filter map keys must all call
// this function instead of normalizing (e.g. strings.ToLower) on their
// own, to avoid split-brain identity decisions across call sites.
//
// Only a GitHub host -- an explicit "github.com" (case-insensitively) or
// an empty Host, meaning "default host" -- case-folds Owner/Repo, since
// GitHub itself treats owner/repo names case-insensitively. A self-hosted
// (non-GitHub) host preserves Owner/Repo case exactly: "Owner/Repo" and
// "owner/repo" on a self-hosted git server are two different identities.
// The host component itself is always case-folded (DNS host names are
// case-insensitive regardless of which git host it is).
//
// Reference, VirtualPath, and Alias are selector-level, not
// identity-level, and are deliberately excluded and never case-folded
// here -- a dependency's resolution selector never changes whether it's
// the "same repository" as another reference. Two references that share
// an identity but differ in selector are NOT silently merged by this
// function; callers decide that policy themselves (see design.md §0/§2).
//
// A "git: ./path" local-repo-as-git-source reference (Owner/Repo empty,
// RepoURL holding the path -- see ToCanonical's matching special case)
// keeps its path as the identity. Local and parent references have no
// stable repository identity and always return "" (mirrors
// DependencyReference.IdentityKey/deploy.DepRefKey). A nil ref returns
// "" so callers never need their own nil guard.
func CanonicalRepoIdentity(ref *DependencyReference) string {
	if ref == nil || ref.IsLocal || ref.IsParent {
		return ""
	}

	// Non-git sources get their own prefixed identity namespace so a
	// registry id or marketplace plugin whose name happens to spell
	// "owner/repo" can never collide with a real git repository identity
	// (codex gate finding on the unprefixed first draft). Case is
	// preserved -- registry/marketplace name case-sensitivity is decided
	// at lookup time (mkt-033), not at identity time.
	// Components are %q-quoted so the ":" joiner is unambiguous even if a
	// component itself contains a colon (marketplace names are segmentRe-
	// constrained and can't, but RegistryName has no such charset guard):
	// ("a:b","c") and ("a","b:c") must never encode identically.
	switch ref.Source {
	case "registry":
		return fmt.Sprintf("registry:%q:%q", ref.RegistryName, ref.RepoURL)
	case "marketplace":
		return fmt.Sprintf("marketplace:%q:%q", ref.MarketplaceName, ref.MarketplacePluginName)
	}

	// Owner/Repo both empty happens for two git-flavored shapes that only
	// carry a RepoURL: a local repo-as-git-source ("git: ./path", RepoURL
	// holds the path -- preserved verbatim, filesystem paths are not
	// GitHub-case-insensitive) and a bare name literal ("name: owner/repo",
	// Source "" -- a default-host git shorthand, so it case-folds exactly
	// like the parsed owner/repo form below and merges with it).
	if ref.Owner == "" && ref.Repo == "" && ref.RepoURL != "" {
		if strings.HasPrefix(ref.RepoURL, "./") || strings.HasPrefix(ref.RepoURL, "../") || IsAbsoluteLocalPath(ref.RepoURL) {
			return ref.RepoURL
		}
		return strings.ToLower(strings.TrimSuffix(ref.RepoURL, ".git"))
	}

	host := ref.Host
	isGitHub := host == "" || strings.EqualFold(host, "github.com")

	owner := ref.Owner
	repo := strings.TrimSuffix(ref.Repo, ".git")
	if isGitHub {
		// Default-host and explicit "github.com" collapse to the same
		// identity: the host component is dropped entirely rather than
		// normalized to a fixed spelling, so "owner/repo" (no host) and
		// "github.com/owner/repo" (explicit host) produce the identical
		// string.
		return strings.ToLower(owner) + "/" + strings.ToLower(repo)
	}

	normalizedHost := strings.ToLower(host)
	return normalizedHost + "/" + owner + "/" + repo
}
