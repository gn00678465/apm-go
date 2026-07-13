// This file (version_spec.go) implements mkt-021/033's version_spec
// resolution step -- design.md's ResolvePlugin flow step 7, invoked from
// resolve_plugin.go's ResolvePlugin after mkt-026/027/035 have already
// computed a canonical.
package marketplace

import (
	"fmt"
	"strings"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/semver"
)

// TagLister abstracts `git ls-remote` for mkt-021/033's version_spec
// semver-range resolution path. Kept local to this package (mirroring
// internal/marketplace/authoring/refcheck.go's RefLister) rather than
// importing internal/resolver.TagLister: internal/resolver will itself
// import internal/marketplace in a later sub-task step (mkt-033's apm.yml
// dict-form wiring), so importing resolver.TagLister here would create an
// import cycle. gitops.RealTagLister already has the exact
// ListTags(repoURL string) ([]semver.TagInfo, error) shape resolver.TagLister
// requires, so production callers pass it in unchanged either way.
type TagLister interface {
	ListTags(repoURL string) ([]semver.TagInfo, error)
}

// DefaultTagLister is the production TagLister ResolveOptions falls back to
// when Tags is left nil: a real gitops.RealTagLister (git ls-remote),
// matching how cmd/apm-go/install.go/update.go wire the same concrete type for
// ordinary git-semver dependency resolution.
var DefaultTagLister TagLister = &gitops.RealTagLister{}

// ResolveOptions carries ResolvePlugin's optional inputs, kept separate from
// its mandatory pluginName/mktName so later steps (mkt-028's cross-repo
// gate, mkt-034's ref-swap-pin/shadow-detection advisories) can extend this
// struct without repeatedly changing ResolvePlugin's own signature --
// design.md's `ResolvePlugin(plugin, mkt string, opts) (*Resolution, error)`
// sketch.
type ResolveOptions struct {
	// VersionSpec is mkt-021/033's version_spec input: either the CLI's
	// "#REF" suffix (already validated by ParseRef to be a plain git ref --
	// it can never contain a semver range character) or an apm.yml dict
	// form's "version:" field (which DOES allow a semver range expression).
	// Empty means "no override": ResolvePlugin behaves exactly as it did
	// before this field existed.
	VersionSpec string
	// Tags is consulted only when VersionSpec is itself a semver range
	// expression (semver.IsSemverRange); nil falls back to DefaultTagLister.
	Tags TagLister
}

// tagLister returns o.Tags, falling back to DefaultTagLister when unset.
func (o ResolveOptions) tagLister() TagLister {
	if o.Tags != nil {
		return o.Tags
	}
	return DefaultTagLister
}

// applyVersionSpec resolves mkt-021/033's version_spec onto canonical (as
// already computed by resolvePluginSource plus any of ResolvePlugin's own
// prior "#ref" decisions), mirroring the Python original's "Version spec
// override" block (resolver.py:918-966):
//
//   - versionSpec that is NOT a semver range (a plain git tag/branch/SHA --
//     mkt-021's only accepted CLI "#REF" shape) replaces canonical's "#ref"
//     fragment directly; no tag lookup happens at all.
//   - versionSpec that IS a semver range expression (only reachable via
//     mkt-033's apm.yml dict "version:" field -- ParseRef already rejects
//     range characters in the CLI "#REF" form, mkt-021) is resolved against
//     real tags on canonical's own "owner/repo" coordinate via
//     tags.ListTags + semver.MaxSatisfying, picking the highest satisfying
//     tag.
//   - When no tag satisfies the range, this falls back to treating
//     versionSpec itself as a raw ref rather than failing the resolution
//     (design.md/implement.md step 4: "無相符且非嚴格 range → 回退 raw ref").
//     The Go port does not reproduce the Python original's
//     NoMatchingVersionError hard-failure for a genuine range with zero
//     matches -- every no-match case falls back the same way here.
//
// Any pre-existing "#ref" fragment already in canonical (e.g. a dict
// source's own declared "ref" field, mkt-026) is discarded once versionSpec
// is present, exactly mirroring the Python original's
// `canonical.split("#", 1)[0]` -- version_spec always wins over it.
func applyVersionSpec(canonical, versionSpec string, tags TagLister) (string, error) {
	base, _, _ := strings.Cut(canonical, "#")

	if !semver.IsSemverRange(versionSpec) {
		return base + "#" + versionSpec, nil
	}

	listed, err := tags.ListTags(repoCoordinate(base))
	if err != nil {
		return "", fmt.Errorf("list tags to resolve version spec %q: %w", versionSpec, err)
	}
	winner, ok, err := semver.MaxSatisfying(listed, versionSpec)
	if err != nil {
		return "", fmt.Errorf("resolve version spec %q: %w", versionSpec, err)
	}
	if !ok {
		return base + "#" + versionSpec, nil
	}
	return base + "#" + winner.Name, nil
}

// repoCoordinate trims a fragment-free canonical string down to just its
// leading "owner/repo" segment pair: a git tag lookup is repo-scoped, so any
// further in-marketplace subdirectory segment ("/plugins/foo", "/pkg/a")
// must never be forwarded to TagLister.ListTags.
func repoCoordinate(canonical string) string {
	parts := strings.SplitN(canonical, "/", 3)
	if len(parts) < 2 {
		return canonical
	}
	return parts[0] + "/" + parts[1]
}
