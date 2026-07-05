// Package build implements `apm pack`'s marketplace.json build pipeline
// (mkt-050~055): resolving each marketplace.packages[] entry's declared
// ref/version against real git refs, composing the Claude/Codex output
// documents, and writing them to their profile paths.
//
// This file (builder.go) implements ResolvePackages -- mkt-051's "本地跳過
// git、遠端依 ref/semver range 對照真實 git tag 解析", mkt-055's branch-ref/HEAD
// HeadNotAllowedError gate, and (via metadata.go's enrichRemoteMetadata)
// mkt-050 修訂版 (c)'s remote apm.yml description/version enrichment. Later
// stages (output composition, output writing, CLI wiring) are separate,
// not-yet-landed steps of this sub-task's implement.md.
package build

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/apm-go/apm/internal/marketplace/authoring"
	"github.com/apm-go/apm/internal/marketplace/tagpattern"
	"github.com/apm-go/apm/internal/semver"
)

// ResolvedPackage is one marketplace.packages[] entry after ref/version
// resolution (mkt-051). Field comments mirror design.md's table.
//
// RemoteDescription/RemoteVersion hold this package's already-enriched
// description/version (mkt-050 修訂版 (c)): the curator's own PackageEntry
// value when present and usable, otherwise whatever the package's own
// apm.yml declares -- a remote package's own apm.yml at its resolved ref
// (metadata.go's enrichRemoteMetadata) or a local package's own apm.yml on
// disk within the project (metadata.go's enrichLocalMetadata, F1 fix) --
// see those functions for the exact precedence. May still be "" (for
// either kind of package) when neither the curator nor the package's own
// apm.yml supplies a usable value.
type ResolvedPackage struct {
	Entry   authoring.PackageEntry
	IsLocal bool

	// Ref is the resolved tag name (remote only; "" for local packages).
	// It is always a literal, checkoutable git ref -- never just the
	// "{version}" portion tagPattern extracted, since a custom pattern
	// like "{name}-v{version}" means those differ.
	Ref string
	// SHA is the resolved 40-character commit hash (remote only; "" for
	// local packages).
	SHA string

	// Host is the non-default git host parsed from Entry.Source ("" when
	// Entry.Source names the default host, github.com, or is local) --
	// determines whether a later output-mapping step must emit a
	// url-shaped source instead of a github-shorthand one.
	Host string
	// SourceRepo is the "owner/repo" coordinate parsed from Entry.Source
	// (remote only; "" for local packages).
	SourceRepo string
	// Subdir is Entry.Subdir for a remote package, or Entry.Source itself
	// (the package's own relative path) for a local package.
	Subdir string
	// Tags is Entry.Tags (already tags+keywords-merged by the authoring
	// package's loader).
	Tags []string

	RemoteDescription, RemoteVersion string
}

// Options carries ResolvePackages' optional inputs.
type Options struct {
	// IncludePrerelease mirrors `apm pack --include-prerelease`: when
	// true, a semver-range package also considers prerelease-tagged
	// candidates. A package entry's own IncludePrerelease (mkt-045)
	// always wins even when this is false.
	IncludePrerelease bool
	// Offline mirrors `apm pack --offline`: apm-go keeps no on-disk ref
	// cache (design.md), so a remote package with a Ref or Version pinned
	// to resolve fails loudly instead of silently falling back to the
	// network -- mirroring internal/marketplace/authoring/refcheck.go's
	// identical --offline convention for `marketplace check`/`outdated`.
	// Local packages are unaffected (they never touch the network at
	// all, offline or not).
	Offline bool
	// Lister is the RefLister ResolvePackages queries for remote refs;
	// nil falls back to DefaultRefLister (a real `git ls-remote`).
	Lister RefLister
	// MetadataFetcher is queried for each remote package's own apm.yml
	// description/version (mkt-050 修訂版 (c)); nil falls back to
	// DefaultMetadataFetcher (a real git clone).
	MetadataFetcher MetadataFetcher
	// ProjectRoot is the project root a local package's own apm.yml is
	// resolved relative to for metadata enrichment (F1/mkt-050 修訂版 (c)):
	// <ProjectRoot>/<entry.Source>/apm.yml. "" falls back to ".", mirroring
	// cmd/apm/pack.go's own authoring.LoadAuthoringConfig(".") call (pack
	// always resolves relative to the current working directory).
	ProjectRoot string
}

func (o Options) lister() RefLister {
	if o.Lister != nil {
		return o.Lister
	}
	return DefaultRefLister
}

func (o Options) metadataFetcher() MetadataFetcher {
	if o.MetadataFetcher != nil {
		return o.MetadataFetcher
	}
	return DefaultMetadataFetcher
}

func (o Options) projectRoot() string {
	if o.ProjectRoot != "" {
		return o.ProjectRoot
	}
	return "."
}

// sha40LowerRe matches a 40-character lowercase hex commit SHA (mkt-051).
// Deliberately lowercase-only, mirroring the Python original's _SHA40_RE
// exactly: an uppercase 40-hex string is NOT accepted as a literal SHA and
// instead falls back to ordinary tag/branch ref lookup (design.md).
var sha40LowerRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ResolvePackages resolves every marketplace.packages[] entry in cfg to a
// concrete ResolvedPackage (mkt-051): local packages skip git entirely;
// remote packages resolve either an explicit ref (accepting a 40-char
// lowercase SHA directly, otherwise comparing against real tags/branches --
// rejecting a branch/HEAD match via HeadNotAllowedError, mkt-055) or a
// semver version range (tags filtered through the package's tagPattern,
// then internal/semver.MaxSatisfying), and enriches each remote package's
// description/version from its own apm.yml (mkt-050 修訂版 (c)).
//
// The second return value collects one warning per remote package whose
// metadata fetch failed -- a failed fetch never fails the build (design.md:
// "抓取失敗降級為「無 metadata」警告,不中斷 build"), so callers should
// surface these to the user without treating them as errors.
func ResolvePackages(cfg *authoring.AuthoringConfig, opts Options) ([]ResolvedPackage, []string, error) {
	lister := opts.lister()
	fetcher := opts.metadataFetcher()
	out := make([]ResolvedPackage, 0, len(cfg.Packages))
	var warnings []string
	for _, entry := range cfg.Packages {
		rp, warning, err := resolvePackage(cfg, entry, lister, fetcher, opts)
		if err != nil {
			return nil, nil, fmt.Errorf("package %q: %w", entry.Name, err)
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
		out = append(out, rp)
	}
	return out, warnings, nil
}

func resolvePackage(cfg *authoring.AuthoringConfig, entry authoring.PackageEntry, lister RefLister, fetcher MetadataFetcher, opts Options) (ResolvedPackage, string, error) {
	if isLocalPackageSource(entry.Source) {
		// F1 fix: enrich from the package's own apm.yml on disk, the same
		// curator-wins precedence enrichRemoteMetadata applies to a remote
		// package -- previously local packages were never enriched at all,
		// leaving description/version permanently blank whenever the
		// curator's own entry omitted them.
		description, version, warning := enrichLocalMetadata(entry, opts.projectRoot())
		return ResolvedPackage{
			Entry:             entry,
			IsLocal:           true,
			Subdir:            entry.Source,
			Tags:              entry.Tags,
			RemoteDescription: description,
			RemoteVersion:     version,
		}, warning, nil
	}

	if entry.Ref == "" && entry.Version == "" {
		return ResolvedPackage{}, "", fmt.Errorf("remote package declares neither ref nor version; at least one is required to resolve it")
	}

	if opts.Offline {
		return ResolvedPackage{}, "", fmt.Errorf("package %q: --offline has no cached refs to resolve %q against", entry.Name, entry.Source)
	}

	host, sourceRepo := splitHostFromSource(entry.Source)
	if host == defaultGitHost() {
		// F2 fix: an EXPLICIT "github.com/..."/"https://github.com/..."
		// source names the same default host a bare "owner/repo" shorthand
		// already converges to Host="" for -- without this, the mapper sees
		// a non-empty Host and wrongly emits a url-shaped source instead of
		// the github shorthand (Claude) / a full github.com URL instead of
		// a bare "owner/repo" (Codex), mirroring the Python original's
		// _effective_host (builder.py).
		host = ""
	}
	base := ResolvedPackage{
		Entry:      entry,
		Host:       host,
		SourceRepo: sourceRepo,
		Subdir:     entry.Subdir,
		Tags:       entry.Tags,
	}

	if entry.Ref != "" {
		ref, sha, err := resolveExplicitRef(entry, lister)
		if err != nil {
			return ResolvedPackage{}, "", err
		}
		base.Ref, base.SHA = ref, sha
	} else {
		pattern := entry.TagPattern
		if pattern == "" {
			pattern = cfg.Build.TagPattern
		}
		includePre := entry.IncludePrerelease || opts.IncludePrerelease
		ref, sha, err := resolveVersionRange(entry, pattern, includePre, lister)
		if err != nil {
			return ResolvedPackage{}, "", err
		}
		base.Ref, base.SHA = ref, sha
	}

	description, version, warning := enrichRemoteMetadata(entry, base.Ref, base.Subdir, entry.Source, fetcher)
	base.RemoteDescription, base.RemoteVersion = description, version
	return base, warning, nil
}

// isLocalPackageSource reports whether source names a local (in-repo)
// package -- req-mf-017/manifest.ValidateMarketplaceSource's "local path
// must start with './'" rule, which is also mkt-051's "本地套件跳過網路"
// boundary.
func isLocalPackageSource(source string) bool {
	return strings.HasPrefix(source, "./")
}

// resolveExplicitRef implements mkt-051/mkt-055's explicit ref: resolution:
//
//  1. A 40-char LOWERCASE hex string is accepted directly as both the ref
//     and the SHA, without ever calling lister (an uppercase 40-hex string
//     does not match and falls through to steps 2-5 below).
//  2. Otherwise, list the remote's tags and branches and try ref_text as a
//     tag name first -- a tag always wins over a same-named branch
//     (design.md's "同名 tag 優先於 branch").
//  3. Try ref_text as a fully-qualified ref name (covers a curator pinning
//     "refs/heads/<branch>" directly); a branch match here is rejected via
//     HeadNotAllowedError.
//  4. Try ref_text as a bare branch name; any match is rejected via
//     HeadNotAllowedError (apm pack exposes no --allow-head escape hatch).
//  5. ref_text spelled "HEAD" (any case) is always rejected via
//     HeadNotAllowedError, whether or not it literally appears in the
//     listed refs.
//  6. Otherwise, RefNotFoundError.
func resolveExplicitRef(entry authoring.PackageEntry, lister RefLister) (ref, sha string, err error) {
	refText := entry.Ref

	if sha40LowerRe.MatchString(refText) {
		return refText, refText, nil
	}

	refs, err := lister.ListRemoteRefs(entry.Source)
	if err != nil {
		return "", "", fmt.Errorf("resolve ref %q: %w", refText, err)
	}

	for _, r := range refs {
		if name, ok := strings.CutPrefix(r.Name, "refs/tags/"); ok && name == refText {
			return name, r.Commit, nil
		}
	}

	for _, r := range refs {
		if r.Name != refText {
			continue
		}
		if branch, isBranch := strings.CutPrefix(r.Name, "refs/heads/"); isBranch {
			return "", "", &HeadNotAllowedError{Package: entry.Name, Ref: branch}
		}
		return strings.TrimPrefix(r.Name, "refs/tags/"), r.Commit, nil
	}

	for _, r := range refs {
		if name, ok := strings.CutPrefix(r.Name, "refs/heads/"); ok && name == refText {
			return "", "", &HeadNotAllowedError{Package: entry.Name, Ref: refText}
		}
	}

	if strings.EqualFold(refText, "HEAD") {
		return "", "", &HeadNotAllowedError{Package: entry.Name, Ref: "HEAD"}
	}

	return "", "", &RefNotFoundError{Package: entry.Name, Ref: refText, Remote: entry.Source}
}

// versionCandidate is one remote tag that matched a package's tagPattern:
// tag is the full original tag name (used as ResolvedPackage.Ref, since a
// custom tagPattern like "{name}-v{version}" means the extracted version
// alone is not itself a checkoutable git ref), version is just the
// extracted "{version}" portion (fed to internal/semver.MaxSatisfying), and
// commit is the tag's SHA.
type versionCandidate struct {
	tag     string
	version string
	commit  string
}

// resolveVersionRange implements mkt-051's semver-range resolution: only
// TAG refs are ever considered (a branch head can never satisfy a version
// range, mirroring the Python original's iter_semver_tags), each candidate
// tag's version is extracted via the package's tagPattern
// (internal/marketplace/tagpattern, reused rather than reimplemented), then
// internal/semver.MaxSatisfying picks the highest tag satisfying
// entry.Version.
func resolveVersionRange(entry authoring.PackageEntry, pattern string, includePrerelease bool, lister RefLister) (ref, sha string, err error) {
	refs, err := lister.ListRemoteRefs(entry.Source)
	if err != nil {
		return "", "", fmt.Errorf("resolve version %q: %w", entry.Version, err)
	}

	re := tagpattern.Compile(pattern, entry.Name)
	var candidates []versionCandidate
	for _, r := range refs {
		tagName, ok := strings.CutPrefix(r.Name, "refs/tags/")
		if !ok {
			continue
		}
		version, ok := tagpattern.ExtractVersion(re, tagName)
		if !ok {
			continue
		}
		if !includePrerelease && semver.IsPrerelease(version) {
			continue
		}
		candidates = append(candidates, versionCandidate{tag: tagName, version: version, commit: r.Commit})
	}

	noMatch := func() (string, string, error) {
		return "", "", &NoMatchingVersionError{
			Package: entry.Name,
			Range:   entry.Version,
			Detail:  fmt.Sprintf("pattern=%q, remote=%q", pattern, entry.Source),
		}
	}
	if len(candidates) == 0 {
		return noMatch()
	}

	rangeTags := make([]semver.TagInfo, len(candidates))
	byVersion := make(map[string]versionCandidate, len(candidates))
	for i, c := range candidates {
		rangeTags[i] = semver.TagInfo{Name: c.version, Commit: c.commit}
		byVersion[c.version] = c
	}

	winner, ok, err := semver.MaxSatisfying(rangeTags, entry.Version)
	if err != nil {
		return "", "", fmt.Errorf("package %q: parse version range %q: %w", entry.Name, entry.Version, err)
	}
	if !ok {
		return noMatch()
	}

	best := byVersion[winner.Name]
	return best.tag, best.commit, nil
}

// resolveCloneURL turns a marketplace.packages[].source string (already
// req-mf-017-validated by authoring's schema.go) into a URL `git ls-remote`
// can use directly: a full URL or SCP-style remote passes through
// unchanged, an absolute filesystem path passes through unchanged (this is
// what lets this package's own tests point a package's Source at a real
// local git repo fixture standing in for "a remote", mirroring
// internal/marketplace/authoring/refcheck.go's tests), and an OWNER/REPO or
// HOST/OWNER/REPO shorthand expands against the appropriate host.
func resolveCloneURL(source string) string {
	if strings.Contains(source, "://") || strings.HasPrefix(source, "git@") {
		return source
	}
	if filepath.IsAbs(source) {
		return source
	}
	host, ownerRepo := splitHostFromSource(source)
	if host == "" {
		host = "github.com"
	}
	return "https://" + host + "/" + ownerRepo + ".git"
}

// defaultGitHostEnvVar names the environment variable that overrides the
// default git host used by F2's host-convergence check (defaultGitHost),
// mirroring Python apm's utils/github_host.py default_host() and
// internal/marketplace/models.go's own identically-named GITHUB_HOST
// convention for GitHub Enterprise Server.
const defaultGitHostEnvVar = "GITHUB_HOST"

// defaultGitHost returns the default git host a package's Host field
// converges to "" against (splitHostFromSource's "no non-default host to
// report" case): GITHUB_HOST when set, otherwise "github.com" -- mirroring
// Python's default_host().
func defaultGitHost() string {
	if h := os.Getenv(defaultGitHostEnvVar); h != "" {
		return h
	}
	return "github.com"
}

// splitHostFromSource splits a non-local Entry.Source into (host,
// owner/repo), mirroring Python apm's yml_schema.split_host_from_source: a
// bare "owner/repo" shorthand, or an opaque form (full URL with no
// decomposable host segment, SCP remote, absolute filesystem test-fixture
// path), returns ("", source) -- "no non-default host to report". A
// "host.tld/owner/repo" shorthand, or a full "https://host.tld/owner/repo
// [.git]" URL, returns (host, "owner/repo").
func splitHostFromSource(source string) (host, ownerRepo string) {
	if rest, ok := strings.CutPrefix(source, "https://"); ok {
		rest = strings.TrimSuffix(rest, "/")
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) == 2 && parts[1] != "" {
			return parts[0], strings.TrimSuffix(parts[1], ".git")
		}
		return "", source
	}
	if strings.Contains(source, "://") || strings.HasPrefix(source, "git@") || filepath.IsAbs(source) {
		return "", source
	}
	segments := strings.Split(source, "/")
	if len(segments) >= 3 && strings.Contains(segments[0], ".") {
		return segments[0], strings.Join(segments[1:], "/")
	}
	return "", source
}
