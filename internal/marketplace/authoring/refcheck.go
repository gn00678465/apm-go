// This file (refcheck.go) implements mkt-041 (`check`) and mkt-042 修訂版
// (`outdated`): the shared "does this package's pinned ref/version genuinely
// exist on its remote" query, backed by one `git ls-remote --tags --heads`
// per remote package.
//
// Local (Source starting with "./") packages never touch the network: this
// mirrors mkt-046's lesson (package add must not require network access for
// a local source) applied symmetrically on the read side, and is proven by
// this file's own tests via a RefLister fake that panics if ever called.
package authoring

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/marketplace/tagpattern"
	"github.com/apm-go/apm/internal/semver"
)

// RefLister abstracts `git ls-remote --tags --heads` for testing. Kept
// local to this package (rather than importing internal/resolver.TagLister)
// because a package's pinned Ref may name a branch, not just a tag --
// design.md's "需要 heads 時擴充" -- so this lists both.
type RefLister interface {
	// ListRefs returns every tag and branch head advertised by source
	// (a marketplace.packages[].source string), plus a synthetic entry
	// named "HEAD" pointing at the remote's current default-branch tip.
	ListRefs(source string) ([]semver.TagInfo, error)
}

// DefaultRefLister is `check`/`outdated`'s production RefLister: real `git
// ls-remote` subprocess calls, mirroring internal/gitops.RealTagLister's
// subprocess pattern.
var DefaultRefLister RefLister = gitRefLister{}

type gitRefLister struct{}

// listRefsTimeout bounds how long a single `git ls-remote` subprocess may
// run before being killed, so an unreachable or private remote can never
// hang this call indefinitely (review finding F3 HIGH: without this, a
// private remote would sit waiting on an interactive credential prompt
// forever). A var, not a const, so tests can shrink it to prove the
// timeout actually fires.
var listRefsTimeout = 30 * time.Second

func (gitRefLister) ListRefs(source string) ([]semver.TagInfo, error) {
	cloneURL, err := resolveCloneURL(source)
	if err != nil {
		return nil, err
	}
	safeURL := gitops.SanitizeGitOutput(cloneURL)

	ctx, cancel := context.WithTimeout(context.Background(), listRefsTimeout)
	defer cancel()

	cmd := newListRefsCmd(ctx, cloneURL)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("git ls-remote %s: timed out after %s", safeURL, listRefsTimeout)
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("git ls-remote %s: %s", safeURL, gitops.SanitizeGitOutput(strings.TrimSpace(string(ee.Stderr))))
		}
		return nil, fmt.Errorf("git ls-remote %s: %w", safeURL, err)
	}
	return parseRefsOutput(string(out)), nil
}

// newListRefsCmd builds the `git ls-remote <cloneURL> HEAD refs/tags/*
// refs/heads/*` subprocess command, hardened against interactive credential
// prompts via gitops.ApplySecureGitEnv. Split out from ListRefs so tests can
// assert on the constructed command without spawning a subprocess.
//
// Explicit refspec patterns are used instead of the `--tags --heads` flags
// so a literal "HEAD" line is included in the output alongside every tag and
// branch (verified to return an identical tag/branch set to `--tags
// --heads` otherwise) -- required for F4's mutable-ref resolution
// (editor.go's resolveRef) to ever successfully resolve `--ref HEAD` to a
// concrete SHA, matching marketplace.md's documented "Mutable refs (HEAD,
// branches) are auto-resolved to a concrete SHA at write time" promise.
// `--tags --heads` alone can never produce a HEAD line: HEAD lives in
// neither the refs/tags/ nor refs/heads/ namespace those flags filter to.
func newListRefsCmd(ctx context.Context, cloneURL string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--", cloneURL, "HEAD", "refs/tags/*", "refs/heads/*")
	gitops.ApplyCloneEnv(cmd, cloneURL)
	return cmd
}

// resolveCloneURL turns a marketplace.packages[].source string (already
// req-mf-017-validated by schema.go's parsePackages) into a URL `git
// ls-remote` can use directly: a full URL or an SCP-style remote passes
// through unchanged; an already-absolute filesystem path (accepted by
// manifest.ValidateMarketplaceSource alongside the "./"-prefixed relative
// form -- this package's own test fixtures, and a manually authored
// self-hosted-git checkout, both use an absolute path; see
// TestResolveCloneURL's "absolute filesystem path passes through unchanged"
// case) also passes through unchanged; a relative "./..." local source
// (isLocalPackageSource's own prefix check) is resolved to an absolute path
// against the process's current working directory via filepath.Abs, then
// checked against that same cwd as a traversal boundary (BLOCKING 1, below),
// so `set --ref` on one still reaches a real, existing git repository
// instead of being misread as an OWNER/REPO shorthand; every other
// (non-"./", non-absolute) string is an OWNER/REPO or HOST/OWNER/REPO
// shorthand, expanded against github.com, mirroring
// internal/marketplace/source.go's defaultSourceHost default.
//
// MAJOR 2 (external audit round 2, 2026-07-30): before this fix, a relative
// local source fell through to the OWNER/REPO branch untouched --
// resolveCloneURL("./x") produced "https://github.com/./x.git" -- so
// `package set NAME --ref <mutable ref>` on any local package (a local
// package's source is normally a relative "./..." path, though
// manifest.ValidateMarketplaceSource also accepts an absolute one -- see
// this function's "absolute filesystem path passes through unchanged"
// branch above, and TestResolveCloneURL's own absolute-path case) failed
// with a bogus GitHub 404 rather than resolving against the real local
// repository. Reading install-marketplace-contracts.md:87 ("set always
// resolves a given ref (no --no-verify escape hatch on set)", no
// local-source exemption) together with BLOCKING 2's fix (SetPackage must
// resolve a local source's ref, not short-circuit it): the contract
// requires this to succeed, not fail closed. See
// TestGitRefLister_ListRefs_RelativeLocalSource_ProductionLister
// (refcheck_test.go) for the real-repo, production-lister proof, and
// TestSetPackage_RelativeLocalSource_MutableRef_ResolvesViaProductionLister
// (editor_test.go) for the SetPackage-level end-to-end regression.
//
// BLOCKING 1 (external audit round 3, 2026-07-30): the relative branch above
// resolves source against the process's cwd via filepath.Abs with no
// boundary check -- manifest.ValidateMarketplaceSource's own "reject '..'
// path segments" guard (mcp.go) is the primary defense against a malicious
// "./../../outside" source, but a Windows-style "./..\\..\\outside" source
// used to slip past that guard too (it only split on "/"), resolve to a
// real path OUTSIDE the project root, and then have gitops.ApplyCloneEnv
// classify it as local and grant it the "file" transport -- a path-
// traversal bypass of req-mf-017's boundary. This is layer 2, independent
// defense-in-depth: even if a caller somehow reaches this function with an
// unvalidated source (or a future validator regression), a resolved path
// that escapes the project root (the same cwd this function already
// resolves relative sources against) is rejected here too, not just
// upstream. See TestResolveCloneURL's
// "relative local source escaping cwd via backslash traversal is rejected"
// and "relative local source escaping cwd via forward-slash traversal is
// rejected" subtests.
func resolveCloneURL(source string) (string, error) {
	if strings.Contains(source, "://") || strings.HasPrefix(source, "git@") {
		return source, nil
	}
	if filepath.IsAbs(source) {
		return source, nil
	}
	if isLocalPackageSource(source) {
		abs, err := filepath.Abs(source)
		if err != nil {
			return "", fmt.Errorf("resolve local source %q: %w", source, err)
		}
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve local source %q: %w", source, err)
		}
		if !pathWithinRoot(cwd, abs) {
			return "", fmt.Errorf("local marketplace source %q resolves outside the project root", source)
		}
		return abs, nil
	}
	return "https://github.com/" + source + ".git", nil
}

// pathWithinRoot reports whether target is root itself or a descendant of
// it. Used by resolveCloneURL's layer-2 traversal guard (BLOCKING 1, above):
// filepath.Rel's own ".." prefix on its result is the standard way to detect
// an escape, since target has already been filepath.Clean-ed by
// filepath.Abs by the time this is called.
func pathWithinRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// parseRefsOutput parses `git ls-remote --tags --heads` output into
// TagInfo entries, stripping both the "refs/tags/" and "refs/heads/"
// prefixes so tag names and branch names are compared/matched uniformly.
func parseRefsOutput(output string) []semver.TagInfo {
	var refs []semver.TagInfo
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		sha := parts[0]
		name := parts[1]
		name = strings.TrimPrefix(name, "refs/tags/")
		name = strings.TrimPrefix(name, "refs/heads/")
		refs = append(refs, semver.TagInfo{Name: name, Commit: sha})
	}
	return refs
}

// CheckResult is one package's `apm marketplace check` outcome (mkt-041).
// Err is nil when the pin was verified, or when the package had nothing
// that needed verifying (local source, or no Ref/Version pinned at all).
type CheckResult struct {
	Package PackageEntry
	Err     error
}

// isLocalPackageSource reports whether source names a local (in-repo)
// package -- req-mf-017/manifest.ValidateMarketplaceSource's own "local
// path must start with './'" rule, which is also mkt-041's "本地套件跳過
// 網路" boundary and mkt-046's local-source detection.
func isLocalPackageSource(source string) bool {
	return strings.HasPrefix(source, "./")
}

// CheckPackages implements mkt-041: for every package in cfg, verify a
// remote package's pinned Ref or Version range genuinely exists on the
// remote via lister.ListRefs. Local packages are always OK and are
// resolved *before* the offline/lister branches below, so a caller's
// lister that panics on any call -- proving zero network I/O -- stays
// silent for an all-local marketplace (or for remote packages with nothing
// pinned to verify). offline=true fails every remote package that has a
// Ref or Version pinned to verify: this MVP keeps no on-disk ref cache to
// fall back on (design.md: "無快取可用視為失敗,不寬貸").
func CheckPackages(cfg *AuthoringConfig, lister RefLister, offline bool) []CheckResult {
	results := make([]CheckResult, 0, len(cfg.Packages))
	for _, pkg := range cfg.Packages {
		results = append(results, CheckResult{Package: pkg, Err: checkPackage(cfg, pkg, lister, offline)})
	}
	return results
}

func checkPackage(cfg *AuthoringConfig, pkg PackageEntry, lister RefLister, offline bool) error {
	if isLocalPackageSource(pkg.Source) {
		return nil
	}
	if pkg.Ref == "" && pkg.Version == "" {
		return nil
	}
	if offline {
		return fmt.Errorf("package %q: --offline has no cached refs to verify %q against", pkg.Name, pkg.Source)
	}

	refs, err := lister.ListRefs(pkg.Source)
	if err != nil {
		return fmt.Errorf("package %q: %w", pkg.Name, err)
	}

	if pkg.Ref != "" {
		if !hasRefNamed(refs, pkg.Ref) {
			return fmt.Errorf("package %q: pinned ref %q not found on %q", pkg.Name, pkg.Ref, pkg.Source)
		}
		return nil
	}

	pattern := pkg.TagPattern
	if pattern == "" {
		pattern = cfg.Build.TagPattern
	}
	versionTags := tagpattern.FilterTags(refs, pattern, pkg.Name)
	_, ok, err := semver.MaxSatisfying(versionTags, pkg.Version)
	if err != nil {
		return fmt.Errorf("package %q: %w", pkg.Name, err)
	}
	if !ok {
		return fmt.Errorf("package %q: no tag on %q matches version range %q", pkg.Name, pkg.Source, pkg.Version)
	}
	return nil
}

// DuplicatePackageNames implements mkt-041's non-fatal duplicate-package-
// name diagnostic (Python's commands/marketplace/check.py's
// _warn_duplicate_names, __init__.py:170-184): every package after the
// first whose name collides with an earlier one case-insensitively
// produces one warning message naming both 0-based package indices.
// Callers must treat these as WARNING-ONLY -- they must never affect
// `check`'s exit code, only its diagnostic output (Python: the equivalent
// yml_schema-level rejection is a *separate*, stricter parse-time check;
// this one is deliberately "defence-in-depth" and non-fatal).
func DuplicatePackageNames(cfg *AuthoringConfig) []string {
	var warnings []string
	seen := make(map[string]int, len(cfg.Packages))
	for idx, pkg := range cfg.Packages {
		lower := strings.ToLower(pkg.Name)
		if firstIdx, ok := seen[lower]; ok {
			warnings = append(warnings, fmt.Sprintf(
				"duplicate package name %q (packages[%d] and packages[%d]); consumers will see duplicate entries in browse",
				pkg.Name, firstIdx, idx,
			))
			continue
		}
		seen[lower] = idx
	}
	return warnings
}

func hasRefNamed(refs []semver.TagInfo, name string) bool {
	for _, r := range refs {
		if r.Name == name {
			return true
		}
	}
	return false
}

// ── mkt-042 修訂版: `outdated` ────────────────────────────────────────────

// OutdatedRow is one package's `apm marketplace outdated` result: its
// current published pin, the highest tag satisfying its declared version
// range, the highest tag overall, a status icon, and a human-readable note.
type OutdatedRow struct {
	Package       PackageEntry
	Current       string // "--" when unknown
	LatestInRange string // "--" when no tag satisfies Package.Version
	LatestOverall string // "--" when no tag matches the tag pattern at all
	Status        string // "[+]" / "[!]" / "[*]" / "[i]" / "[x]"
	Note          string
	// Upgradable is this row's contribution to mkt-042's exit-1 decision.
	// It is set only by the [+]/[!] branch *before* the later "latest
	// overall != latest in range" check can visually override Status to
	// "[*]" -- mirroring Python outdated.py:116-128, where that override
	// never touches the upgradable/up_to_date counters it already
	// incremented. Exit 1 must be driven exclusively by counting this
	// field across every row, never by comparing Status strings (mkt-042's
	// "exit 1 僅由 upgradable 計數驅動").
	Upgradable bool
}

// OutdatedPackages implements mkt-042 修訂版: for every package in cfg,
// compare its declared version range against real git tags (one
// `git ls-remote` per remote package, via lister) to report one of five
// status icons.
//
// current maps a package name to whatever version it is *currently*
// published/pinned at -- e.g. from a prior `apm pack` run's marketplace.json
// -- so "[+] already up to date" can be told apart from "[!] a newer
// in-range tag exists". A name absent from current (or a nil/empty map) is
// treated as unknown ("--"); this sub-task's CLI wiring passes nil today,
// since `apm pack` (mkt-050+) is a separate, not-yet-landed sub-task, but
// every other icon ([!]/[*]/[i]/[x]) is still reported correctly without it.
func OutdatedPackages(cfg *AuthoringConfig, lister RefLister, offline, includePrerelease bool, current map[string]string) []OutdatedRow {
	rows := make([]OutdatedRow, 0, len(cfg.Packages))
	for _, pkg := range cfg.Packages {
		rows = append(rows, outdatedForPackage(cfg, pkg, lister, offline, includePrerelease, current[pkg.Name]))
	}
	return rows
}

func outdatedForPackage(cfg *AuthoringConfig, pkg PackageEntry, lister RefLister, offline, includePrerelease bool, current string) OutdatedRow {
	row := OutdatedRow{Package: pkg, Current: "--", LatestInRange: "--", LatestOverall: "--"}
	if current != "" {
		row.Current = current
	}

	switch {
	case isLocalPackageSource(pkg.Source):
		row.Status, row.Note = "[i]", "local package; skipped"
		return row
	case pkg.Ref != "":
		row.Status, row.Note = "[i]", "pinned to ref; skipped"
		return row
	case pkg.Version == "":
		row.Status, row.Note = "[i]", "no version range"
		return row
	}

	if offline {
		row.Status, row.Note = "[x]", "--offline has no cached refs to check against"
		return row
	}

	refs, err := lister.ListRefs(pkg.Source)
	if err != nil {
		row.Status, row.Note = "[x]", err.Error()
		return row
	}

	pattern := pkg.TagPattern
	if pattern == "" {
		pattern = cfg.Build.TagPattern
	}
	candidates := extractOutdatedCandidates(refs, pattern, pkg.Name, includePrerelease || pkg.IncludePrerelease)
	if len(candidates) == 0 {
		row.Status, row.Note = "[!]", "no matching tags found"
		return row
	}

	overall := candidates[0]
	for _, c := range candidates[1:] {
		if semver.CompareVersions(c.version, overall.version) > 0 {
			overall = c
		}
	}
	row.LatestOverall = overall.tag

	rangeTags := make([]semver.TagInfo, len(candidates))
	versionToTag := make(map[string]string, len(candidates))
	for i, c := range candidates {
		rangeTags[i] = semver.TagInfo{Name: c.version, Commit: c.commit}
		versionToTag[c.version] = c.tag
	}
	winner, ok, err := semver.MaxSatisfying(rangeTags, pkg.Version)
	if err != nil {
		row.Status, row.Note = "[x]", err.Error()
		return row
	}
	if ok {
		row.LatestInRange = versionToTag[winner.Name]
	}

	if row.Current == row.LatestInRange {
		row.Status = "[+]"
	} else {
		row.Status = "[!]"
		row.Upgradable = true
	}
	if row.LatestOverall != row.LatestInRange {
		row.Status = "[*]"
	}
	return row
}

// outdatedCandidate is one remote ref that matched a package's tag pattern:
// tag is the full original ref name (e.g. "v1.1.0", used for Current/
// LatestInRange/LatestOverall display and comparison against a package's
// published pin), version is just the extracted "{version}" portion (used
// for semver ranking/range matching), and commit is the ref's SHA.
type outdatedCandidate struct {
	tag     string
	version string
	commit  string
}

// extractOutdatedCandidates mirrors Python's iter_semver_tags +
// _extract_tag_versions: it keeps only refs matching pattern (branch heads
// among refs are filtered out in practice by simply never matching a
// version-shaped pattern like "v{version}" -- RefLister's own ListRefs, per
// mkt-041's design, does not distinguish tags from heads, since a `check`
// pin may legitimately name a branch), and drops prerelease-tagged
// candidates unless includePrerelease is set.
func extractOutdatedCandidates(refs []semver.TagInfo, pattern, name string, includePrerelease bool) []outdatedCandidate {
	re := tagpattern.Compile(pattern, name)
	var out []outdatedCandidate
	for _, r := range refs {
		version, ok := tagpattern.ExtractVersion(re, r.Name)
		if !ok {
			continue
		}
		if !includePrerelease && semver.IsPrerelease(version) {
			continue
		}
		out = append(out, outdatedCandidate{tag: r.Name, version: version, commit: r.Commit})
	}
	return out
}
