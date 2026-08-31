// This file (refcheck.go) implements mkt-041 (`check`) and mkt-042 修訂版
// (`outdated`): the shared "does this package's pinned ref/version genuinely
// exist on its remote" query, backed by one `git ls-remote --tags --heads`
// per remote package.
//
// Local (Source starting with "./") packages never touch the network: this
// mirrors mkt-046's lesson (package add must not require network access for
// a local source) applied symmetrically on the read side, and is proven by
// this file's own tests via a RefLister fake that panics if ever called.
// `check`'s local branch does stat the resolved path on disk (ticket 20
// AC4) -- a local filesystem read, never a network call.
package authoring

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
// through unchanged; a relative "./..." local source (isLocalPackageSource's
// own prefix check) is resolved to an absolute path against the process's
// current working directory via filepath.Abs, then checked against that
// same cwd as a traversal boundary (BLOCKING 1 below), so `set --ref` on one
// still reaches a real, existing git repository instead of being misread as
// an OWNER/REPO shorthand; every other (non-"./") string is an OWNER/REPO or
// HOST/OWNER/REPO shorthand, expanded against github.com, mirroring
// internal/marketplace/source.go's defaultSourceHost default.
//
// The `strings.HasPrefix(source, "git@")` branch immediately below is, like
// the `filepath.IsAbs(source)` branch a few lines down, UNREACHABLE for any
// source that has actually passed manifest.ValidateMarketplaceSource as of
// BLOCKING 1 (external audit round 5, 2026-07-30): that validator's grammar
// rewrite mirrors upstream's SOURCE_RE exactly (four accepted shapes --
// 'owner/repo', 'host.tld/owner/repo', 'https://host.tld/owner/repo[.git]',
// './path' -- nothing else), which structurally rejects any SCP-style
// "git@host:path" string before it ever reaches this function in a
// production call. It is kept here (not removed) for the same reason the
// `filepath.IsAbs` branch is kept, below: this package's own
// TestResolveCloneURL directly calls this function with an unvalidated
// "git@..." string as one of its fixtures, bypassing the validator on
// purpose (this file's own long-standing test convention, not a round-5
// addition).
//
// The `filepath.IsAbs(source)` branch below is UNREACHABLE for any source
// that has actually passed manifest.ValidateMarketplaceSource: BLOCKING 1
// (external audit round 4, 2026-07-30) found that validator accepted an
// absolute or UNC filesystem path outright (it fell through to the
// "shorthand form -- accepted" branch there, since it is neither
// "."-prefixed nor "://"-schemed), and this function then returned that
// path unchanged with NO boundary check at all -- a path-traversal bypass
// needing no ".." segment whatsoever. The validator now rejects every
// absolute/UNC source, which closes this for every production path that
// reaches this function: this function's own only direct caller is
// gitRefLister.ListRefs (this file, below), which manifest.go's
// validateMarketplaceBlock, schema.go's LoadAuthoringConfig, and editor.go's
// AddPackage all sit upstream of, by way of that same validator -- MINOR
// (external audit round 5, 2026-07-30): those three are production callers
// of manifest.ValidateMarketplaceSource, not of this function directly; the
// wording here used to conflate the two.
// This branch is deliberately left as-is (not removed, not boundary-checked)
// rather than tightened further: this package's own tests
// (TestResolveCloneURL's "absolute filesystem path passes through
// unchanged" case, and refcheck_test.go/editor_test.go's many CheckPackages/
// OutdatedPackages/AddPackage/SetPackage unit tests that build an
// AuthoringConfig's PackageEntry.Source directly in memory, bypassing
// ValidateMarketplaceSource on purpose) rely on this function accepting an
// absolute path unchanged as a stand-in "real remote" fixture. Tightening
// this branch too (e.g. reusing pathWithinRoot below) would need converting
// dozens of those in-memory-struct unit tests, for no additional
// production-reachable protection: the validator is the only path any
// caller has to reach PackageEntry.Source or a CLI source argument at all.
// If a future caller ever constructs an AuthoringConfig/PackageEntry without
// going through LoadAuthoringConfig (or calls AddPackage/SetPackage with an
// unvalidated source some other way), this residual gap would apply to it
// too -- see this function's own BLOCKING 1 (round 3) doc below for why the
// relative-path branch gets an independent boundary check instead: that
// check was cheap to add because it does not require changing what "valid"
// looks like for the branch's own callers.
//
// MAJOR 2 (external audit round 2, 2026-07-30): before this fix, a relative
// local source fell through to the OWNER/REPO branch untouched --
// resolveCloneURL("./x") produced "https://github.com/./x.git" -- so
// `package set NAME --ref <mutable ref>` on any local package (a local
// package's source is normally a relative "./..." path) failed with a bogus
// GitHub 404 rather than resolving against the real local repository.
// Reading install-marketplace-contracts.md:87 ("set always resolves a given
// ref (no --no-verify escape hatch on set)", no local-source exemption)
// together with BLOCKING 2's fix (SetPackage must resolve a local source's
// ref, not short-circuit it): the contract requires this to succeed, not
// fail closed. See TestGitRefLister_ListRefs_RelativeLocalSource_ProductionLister
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
		return validateLocalSourceWithinRoot(source)
	}
	// v0.28.0 (PR #2439): a host-prefixed shorthand routes to ITS host, not
	// github.com -- upstream splits the host out (split_host_from_source)
	// and hands it to RefResolver(host=...) for both `package add`'s
	// verify/resolve and `check`'s per-entry resolution.
	if host, repoPath := SplitHostFromSource(source); host != "" {
		return "https://" + host + "/" + repoPath + ".git", nil
	}
	return "https://github.com/" + source + ".git", nil
}

// hostPrefixedSourceRe matches the `host.tld/owner/repo` shorthand (exactly
// three segments, first FQDN-ish), mirroring upstream's
// _HOST_PREFIXED_SOURCE_RE (yml_schema.py:112-113).
var hostPrefixedSourceRe = regexp.MustCompile(
	`^((?:[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?\.)+[A-Za-z][A-Za-z0-9-]*)/([A-Za-z0-9._-]+/[A-Za-z0-9._-]+)$`)

// httpsURLSourceRe matches `https://host.tld/path/to/repo[.git]` with a
// nested (2+ segment) repository path, mirroring upstream's
// _HTTPS_URL_SOURCE_RE as widened by v0.28.0's PR #2439.
var httpsURLSourceRe = regexp.MustCompile(
	`^https://((?:[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?\.)+[A-Za-z][A-Za-z0-9-]*)/([A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)+?)(?:\.git)?$`)

// SplitHostFromSource splits a host-qualified marketplace source into
// (host, repository path), mirroring upstream's split_host_from_source
// (yml_schema.py:125-140): the https form's trailing ".git" is stripped;
// the bare owner/repo shorthand and "./..." local paths return ("", source).
func SplitHostFromSource(source string) (host, repoPath string) {
	if m := httpsURLSourceRe.FindStringSubmatch(source); m != nil {
		return m[1], strings.TrimSuffix(m[2], ".git")
	}
	if m := hostPrefixedSourceRe.FindStringSubmatch(source); m != nil {
		return m[1], m[2]
	}
	return "", source
}

// validateLocalSourceWithinRoot implements mkt-046's local-source path-
// containment check for resolveCloneURL: resolves a "./"-prefixed source
// against the process's cwd (resolveCloneURL has no directory parameter of
// its own -- every production caller that reaches it, via gitRefLister.
// ListRefs, relies on the CLI's own dir="." convention, cmd/apm-go/
// marketplace_package.go) and rejects it if the resolved path escapes that
// root. Delegates to resolveLocalSourceAgainstRoot, below, for the actual
// containment logic -- see that function's doc comment for why this exists
// as a separate, root-parameterized function rather than folding cwd lookup
// directly into the shared logic.
func validateLocalSourceWithinRoot(source string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve local source %q: %w", source, err)
	}
	return resolveLocalSourceAgainstRoot(cwd, source)
}

// resolveLocalSourceAgainstRoot resolves a "./"-prefixed local source
// against an explicit project root and rejects it if the resolved path
// escapes that root, via pathWithinRoot's three layers (lexical, real
// symlink, Windows junction/reparse point). It performs NO network I/O,
// preserving mkt-046's "a local source never touches the network" contract
// -- this is a pure filesystem/path check.
//
// root is a parameter (rather than always being the process's cwd, as
// validateLocalSourceWithinRoot above assumes) so verifyPackageSource
// (editor.go) can check a local source against AddPackage's own dir
// parameter directly, instead of relying on the caller having already
// os.Chdir-ed into it -- AddPackage/SetPackage's unit tests construct a
// fixture directory via t.TempDir() and pass it as dir without chdir-ing the
// test process into it, and dir is the actual, correct project root for
// that call regardless of the process's cwd.
//
// Shared by resolveCloneURL (above, via validateLocalSourceWithinRoot) and
// verifyPackageSource (editor.go), rather than each hand-rolling its own
// copy (the same "one decision function" principle classifyRefResolution
// already applies to ref resolution): BLOCKING 2 (2026-07-31 follow-up, live
// end-to-end reproduction) found that verifyPackageSource's local branch
// used to unconditionally `return nil` with NO path check at all.
// resolveRef's mkt-046 short-circuit (classifyRefResolution's
// skipLocalSource branch) ALSO never resolves or validates a local source's
// path for `add` -- so `package add ./linked`, where "linked" is a
// privilege-free junction (or symlink) pointing outside the project root,
// was accepted outright with no rejection anywhere in AddPackage, and a
// subsequent `pack` faithfully read the escaping target's apm.yml contents
// into the marketplace.json output. See
// TestVerifyPackageSource_LocalSourceEscapingRoot_Rejected and
// TestMarketplacePackageAdd_LocalSourceEscapingRoot_Rejected.
// ResolveLocalSourceAgainstRoot exports resolveLocalSourceAgainstRoot for use
// by other marketplace packages that need this same containment check
// performed again at their own point in time, rather than trusting that a
// check `package add` already ran once still holds true (B-BLOCKING-1,
// external audit round 6, 2026-07-31 follow-up): mkt-046 lets `add` reference
// a source before its directory exists on disk, so nothing stops that path
// from later becoming a symlink or Windows junction pointing outside root --
// entirely valid on disk, invisible to a string-only ".." check -- before a
// subsequent read (e.g. `apm pack`'s local-package metadata enrichment,
// internal/marketplace/build/metadata.go's localApmYMLPath) resolves whatever
// that escaping target actually points to. Every caller must re-run this at
// its own read time instead of caching an earlier call's result.
func ResolveLocalSourceAgainstRoot(root, source string) (string, error) {
	return resolveLocalSourceAgainstRoot(root, source)
}

func resolveLocalSourceAgainstRoot(root, source string) (string, error) {
	// Normalize backslashes so "..\..\" traversal is caught on all platforms.
	source = strings.ReplaceAll(source, `\`, "/")

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve project root %q: %w", root, err)
	}
	abs, err := filepath.Abs(filepath.Join(root, source))
	if err != nil {
		return "", fmt.Errorf("resolve local source %q: %w", source, err)
	}
	if !pathWithinRoot(absRoot, abs) {
		return "", fmt.Errorf("local marketplace source %q resolves outside the project root", source)
	}
	return abs, nil
}

// pathWithinRoot reports whether target is root itself or a descendant of
// it. Used by resolveCloneURL's layer-2 traversal guard (BLOCKING 1, above):
// filepath.Rel's own ".." prefix on its result is the standard way to detect
// an escape, since target has already been filepath.Clean-ed by
// filepath.Abs by the time this is called.
//
// BLOCKING 2 (external audit round 4, 2026-07-30): the check above is purely
// LEXICAL -- it never touches the filesystem. A directory symlink (or
// Windows junction) physically located inside root but pointing at a real
// directory outside it produces a target string containing no ".."
// whatsoever (e.g. root="/project", target="/project/linked", and
// "/project/linked" is a symlink to "/somewhere/else"): filepath.Abs/Rel
// both operate on the string only, so the lexical check above reports
// "within root" -- but `git ls-remote` (or any real filesystem access)
// follows the symlink at the OS level and actually reaches "/somewhere/
// else". pathWithinRootLexical alone is therefore necessary but not
// sufficient. This resolves both root and target through
// filepath.EvalSymlinks (which walks every path component, following any
// symlink it finds) and re-checks containment on that resolved pair too --
// closing the gap for an EXISTING symlink target (the only kind `git
// ls-remote` could ever reach anything real through). See
// TestResolveCloneURL's "relative local source escaping cwd via a directory
// symlink is rejected" subtest.
//
// MAJOR 1 (external audit round 5, 2026-07-30): this used to fall back to
// the lexical result (i.e. ACCEPT) on ANY EvalSymlinks error, not just "the
// leaf does not exist yet". That is unsafe even for the "doesn't exist yet"
// case: EvalSymlinks fails as soon as it cannot stat the target's FINAL
// component, but every component BEFORE the final one has already been
// resolved by then -- an existing symlinked parent directory that already
// points outside root (e.g. target="<root>/linked-parent/not-yet-created",
// where "linked-parent" is a symlink to somewhere outside root) makes
// EvalSymlinks fail on the missing leaf while the parent's escape has
// already happened. Falling back to "accept" here left a TOCTOU window: a
// second process could create the missing leaf between this check and the
// subsequent `git ls-remote` invocation, which would then genuinely follow
// the already-escaping parent symlink out of root. Since resolveCloneURL's
// only production callers ever reach a local source that is expected to be
// a real, existing git repository (ValidateMarketplaceSource/isLocalPackage
// Source having already classified it as local), there is no legitimate
// case where target does not exist at this point either -- so any
// EvalSymlinks error (missing path, permission denied, or otherwise) now
// rejects outright instead of falling back to the lexical result.
//
// Third layer, Windows-junction-aware (2026-07-31 follow-up): the
// EvalSymlinks-based layer above is itself blind to a Windows directory
// junction. As of Go 1.23, os.Lstat no longer reports ModeSymlink for a
// junction by default, and filepath.EvalSymlinks decides whether to follow a
// path component solely by that bit -- so it silently returns a path through
// a junction UNCHANGED, with a nil error, instead of resolving or rejecting
// it. A junction physically inside root but pointing outside it therefore
// used to slip past both layers above with no error at all: the lexical
// check sees no ".." segment, and EvalSymlinks reports "no change" rather
// than following it out of root. Creating a junction needs no special
// Windows privilege (unlike a real symlink), making this a LOWER-privilege
// bypass than the plain symlink escape the second layer defends against.
//
// B-MAJOR-1 (external audit round 6, 2026-07-31 follow-up): an earlier
// version of this third layer (hasUnresolvableReparsePoint) rejected ANY
// existing reparse point along the path from root to target outright,
// without ever checking where it actually pointed. That has two distinct
// bugs, not one:
//  1. A junction physically inside root that points to somewhere ELSE also
//     inside root (a perfectly ordinary, non-escaping local package layout --
//     e.g. a monorepo using junctions/symlinks to share a package directory)
//     was rejected as a false positive, since the old code never resolved the
//     junction's target to check it.
//  2. root ITSELF being reached through a reparse point (e.g. the project
//     checkout sits behind a Windows Dev Drive mapping, `subst`, or a parent
//     directory symlink) made isUnresolvableReparsePoint(root) unconditionally
//     true, rejecting EVERY local source under that project -- root's own
//     path shape has nothing to do with whether target escapes it.
//
// resolveRealPathJunctionAware (below) replaces both the EvalSymlinks calls
// above and hasUnresolvableReparsePoint: it walks every path component of
// both root and target from the volume root down, and whenever the path
// built so far names an existing reparse point of ANY kind (a real symlink
// OR a junction), replaces that prefix with its actual resolved target via
// os.Readlink (which -- unlike filepath.EvalSymlinks -- DOES resolve a
// junction on this Go toolchain, since it reads the reparse point's target
// buffer directly rather than gating on Lstat's ModeSymlink bit) and keeps
// walking from there. This closes both directions at once: a junction
// resolving within root is accepted (bug 1, fixed), root itself being behind
// a junction no longer matters because root gets the exact same resolution
// treatment as target before they are compared (bug 2, fixed), and a
// junction resolving outside root is still rejected exactly as before (no
// regression on the escape case BLOCKING 2/MAJOR 1 already covered). See
// TestResolveCloneURL_JunctionEscape_Rejected (escape, still rejected),
// TestResolveCloneURL_JunctionWithinRoot_Accepted (bug 1),
// TestResolveCloneURL_ProjectRootBehindJunction_LocalSourceWithinRoot_Accepted
// (bug 2).
//
// Fourth refinement, missing-leaf tolerant (2026-07-31, `package add`
// follow-up): resolveCloneURL's own callers only ever reach an already-
// existing local git repository (MAJOR 1's reasoning above), but this
// function is also now used by verifyPackageSource (editor.go) to
// containment-check a NOT-YET-CREATED `package add` local source -- mkt-046
// explicitly lets `add` reference a source before its directory exists (see
// e.g. TestAddPackage_LocalSource_NoFlags_NeverTouchesNetwork, which never
// creates "./pkgs/tool" on disk). Requiring the full path to already exist
// (as layers 2/3 above did pre-2026-07-31, correctly, for resolveCloneURL's
// own "must already be a real repo" callers) would reject every such
// legitimate add. Instead, resolveRealPathJunctionAware below is run against
// longestExistingAncestor(target) rather than target itself: a path
// component that does not exist cannot itself be a symlink or junction an
// attacker could have used to escape root (there is nothing there to escape
// through), while an EXISTING ancestor that already escapes root -- e.g.
// MAJOR 1's own "symlinked parent, dangling/not-yet-created leaf" TOCTOU
// scenario -- is still rejected exactly as before, since that ancestor is
// still what gets checked. See
// TestVerifyPackageSource_LocalSourceEscapingRoot_Rejected and
// TestMarketplacePackageAdd_LocalSourceEscapingRoot_Rejected.
//
// B-BLOCKING-1 (external audit round 7, 2026-07-31 follow-up, "SECRET-NESTED"
// repro): resolveRealPathJunctionAware's PRE-round-7 version walked only the
// ORIGINAL path's own components; each time it substituted a resolved
// reparse-point target in for "current", it re-checked whether that whole
// substituted STRING was itself a reparse point (via a second os.Lstat on
// the full "current" path) before advancing to the original path's next
// component. That re-check can never observe a reparse point buried in one
// of the TARGET's own INTERMEDIATE components: Windows itself transparently
// resolves an intermediate junction as an ordinary part of parsing any
// longer path string handed to Lstat/CreateFile, so a target like
// "<root>/inner/pkg" -- where "inner" (not the final component "pkg") is
// itself a junction pointing outside root -- reports as an ordinary,
// non-reparse directory when Lstat-ed as a whole string, even though the OS
// silently followed "inner" out of root to get there. Concretely: a junction
// "<root>/outer" pointing at "<root>/inner/pkg", where "<root>/inner" is
// itself a SEPARATE junction pointing outside root, used to resolve as
// "<root>/inner/pkg" (lexically still under root) with no error at all --
// the true, OS-followed location (outside root, via "inner") was never
// checked, only the fact that the SUBSTITUTED STRING happened to look
// contained. This is fixed by never comparing "is the whole substituted
// string a reparse point" again: every reparse-point target, the moment it
// is read via os.Readlink, is immediately decomposed back into its own
// volume + path components and pushed onto the SAME pending-component queue
// the rest of the walk consumes from -- so "inner" (an intermediate
// component of a just-substituted target) gets its own, independent
// Lstat/reparse check exactly like any other component, at the point the
// walk actually reaches it, regardless of whether it came from the original
// path or a substitution. See
// TestResolveLocalSourceAgainstRoot_NestedJunctionEscape_Rejected (the
// nested-junction-escape case above) and its "cycle A->B->A fails closed"
// subtest (two junctions/symlinks each pointing at the other, guarded by
// maxReparseResolutionHops below, which counts substitutions the same way
// regardless of this rewrite).
func pathWithinRoot(root, target string) bool {
	if !pathWithinRootLexical(root, target) {
		return false
	}
	existing, ancestorErr := longestExistingAncestor(target)
	if ancestorErr != nil || existing == "" {
		return false
	}
	realRoot, rootErr := resolveRealPathJunctionAware(root)
	if rootErr != nil {
		return false
	}
	realExisting, existingErr := resolveRealPathJunctionAware(existing)
	if existingErr != nil {
		return false
	}
	// B-MINOR-1 (external audit round 8, 2026-07-31 follow-up): the same
	// directory can be spelled multiple distinct ways on Windows (an 8.3
	// short name like "C:\PROGRA~1", a "\\?\Volume{GUID}\..." path, a UNC
	// loopback path) that a plain string comparison cannot be trusted to
	// line up against root's own (possibly differently-spelled) string.
	// canonicalizeRealPathFn resolves each side to Windows' own single
	// canonical form first (a no-op on non-Windows); any failure to do so
	// fails closed rather than falling back to comparing the
	// un-canonicalized strings.
	canonicalRoot, rootCanonErr := canonicalizeRealPathFn(realRoot)
	if rootCanonErr != nil {
		return false
	}
	canonicalExisting, existingCanonErr := canonicalizeRealPathFn(realExisting)
	if existingCanonErr != nil {
		return false
	}
	return pathWithinRootLexical(canonicalRoot, canonicalExisting)
}

// canonicalizeRealPathFn is canonicalizeRealPath behind a package-level var
// so tests can substitute a fake (e.g. one that maps a known 8.3 short-name
// string to its long-name equivalent) without depending on an 8.3-short-name-
// enabled NTFS volume being available in the test environment -- see
// canonicalize_windows.go/canonicalize_other.go for the real implementation
// and TestPathWithinRoot_8dot3AliasedTarget_StillDetectedAsContained /
// TestPathWithinRoot_CanonicalizationFailure_FailsClosed for the fake-backed
// unit tests, and TestPathWithinRoot_Real8dot3ShortName_StillDetectedAsContained
// (Windows-only, visibly t.Skip when the temp volume has 8.3 name generation
// disabled) for a live repro.
var canonicalizeRealPathFn = canonicalizeRealPath

// longestExistingAncestor returns the longest prefix of path (path itself
// included) that currently exists on disk (per os.Lstat, so a symlink or
// junction counts as existing without following it), or "" if not even the
// path's root/volume prefix does.
//
// B-MAJOR-1 (external audit round 8, 2026-07-31 follow-up): an os.Lstat
// failure that is NOT "this component doesn't exist" (an ACL/permission
// error, or any other I/O failure) used to be treated identically to "does
// not exist" -- silently walking up to the parent and potentially skipping
// straight past the very component that could not be inspected (e.g. an
// ACL-protected reparse point this process lacks permission to Lstat), never
// positively determining what is actually there. That case is now returned
// as an error instead, so callers fail closed (reject the path as unverified
// rather than silently walking past it and treating the shorter,
// successfully-Lstat-able ancestor as if it were the whole story).
func longestExistingAncestor(path string) (string, error) {
	cur := path
	for {
		_, err := osLstat(cur)
		if err == nil {
			return cur, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("stat %q: %w", cur, err)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", nil
		}
		cur = parent
	}
}

// osLstat is os.Lstat behind a package-level var so tests can substitute a
// fake that returns a non-fs.ErrNotExist error (e.g. an ACL/permission
// denial), without requiring a live, environment-dependent ACL setup for
// every test that exercises B-MAJOR-1's fail-closed branch (external audit
// round 8, 2026-07-31 follow-up) -- see TestLongestExistingAncestor_
// NonNotExistLstatError_FailsClosed and TestResolveRealPathJunctionAware_
// NonNotExistLstatError_FailsClosed. A real, live ACL-denial repro also
// exists (TestPathWithinRoot_ACLDeniedComponent_FailsClosed, Windows-only,
// visibly t.Skip when this process cannot manipulate its own ACLs) for
// end-to-end confidence beyond the injected-fake unit tests.
var osLstat = os.Lstat

// maxReparseResolutionHops bounds resolveRealPathJunctionAware's symlink/
// junction-following loop, guarding against a reparse-point cycle (e.g. two
// junctions pointing at each other) that would otherwise loop forever.
const maxReparseResolutionHops = 40

// isReparsePoint reports whether the file at path is ANY kind of reparse
// point -- a real symlink (fi.Mode()&os.ModeSymlink, true on every OS) or a
// Windows directory junction/other reparse kind Go's ModeSymlink bit does
// not cover (isJunctionOrUnknownReparsePoint, reparse_windows.go/
// reparse_other.go), deliberately including "cannot positively rule it out"
// as true (fail closed) -- see that function's own doc comment. path is
// passed alongside fi (rather than relying on fi alone) because
// isJunctionOrUnknownReparsePoint's B-MINOR-1 tag-reading (reparse_windows.go)
// needs to open the file by name; os.FileInfo's Sys() (a
// syscall.Win32FileAttributeData, populated by Lstat) carries no reparse tag
// of its own.
func isReparsePoint(path string, fi os.FileInfo) bool {
	return fi.Mode()&os.ModeSymlink != 0 || isJunctionOrUnknownReparsePoint(path, fi)
}

// resolveReparsePointTarget resolves the single reparse point at path (a
// real symlink or a Windows junction) to its target via os.Readlink --
// unlike filepath.EvalSymlinks, which only follows a path component when
// Lstat reports ModeSymlink, os.Readlink reads the reparse point's target
// buffer directly and resolves a junction just as well as a symlink on this
// Go toolchain (verified directly, see reparse_windows.go). A relative
// target (as a symlink's target commonly is; a junction's is normally
// already absolute) is resolved against path's own parent directory,
// mirroring how the OS itself would interpret it. ok is false when Readlink
// fails -- path is not a reparse point Go can resolve this way, or some
// other read error -- callers must treat that as "cannot positively
// resolve," i.e. fail closed.
func resolveReparsePointTarget(path string) (target string, ok bool) {
	linked, err := os.Readlink(path)
	if err != nil {
		return "", false
	}
	if !filepath.IsAbs(linked) {
		linked = filepath.Join(filepath.Dir(path), linked)
	}
	return filepath.Clean(linked), true
}

// resolveRealPathJunctionAware resolves path the way filepath.EvalSymlinks
// does -- following every real symlink component -- but ALSO follows a
// Windows directory junction the same way, which filepath.EvalSymlinks does
// not (isReparsePoint's doc comment above). It walks path one component at a
// time from the volume root down, and each time the prefix built so far
// names an EXISTING reparse point of any kind, replaces that prefix with
// resolveReparsePointTarget's resolved target and keeps resolving from
// there (a junction may itself point at another junction or symlink, so
// this loops until an ordinary, non-reparse-point component is reached, up
// to maxReparseResolutionHops times). A component that does not exist yet
// stops the walk there rather than erroring: callers only ever invoke this
// on an existing path (pathWithinRoot passes root and
// longestExistingAncestor's result, never a raw possibly-nonexistent
// target), so this only matters for a component that vanishes mid-walk
// (TOCTOU) -- treated as "nothing further to resolve," not a caller-visible
// error, mirroring filepath.EvalSymlinks' own not-found short-circuit.
// Returns an error -- fail closed -- only when an existing reparse point's
// target cannot be read (resolveReparsePointTarget failure) or the walk
// exceeds maxReparseResolutionHops (only a reparse-point cycle could ever
// trigger that).
func resolveRealPathJunctionAware(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", path, err)
	}
	vol, pending := splitAbsPathComponents(abs)

	current := vol + string(filepath.Separator)
	hops := 0
	for len(pending) > 0 {
		part := pending[0]
		pending = pending[1:]
		if part == "" || part == "." {
			continue
		}
		candidate := filepath.Join(current, part)
		fi, statErr := osLstat(candidate)
		if statErr != nil {
			if errors.Is(statErr, fs.ErrNotExist) {
				// Doesn't exist (yet, or vanished mid-walk) -- nothing left
				// to resolve at or below this point. Any components still
				// pending (whether from the original path or a
				// just-substituted reparse-point target) are simply
				// subdirectories of a location that does not exist, so
				// they cannot themselves be a symlink or junction to
				// resolve -- dropping them here does not change
				// pathWithinRootLexical's later verdict either way, since
				// containment is prefix-based (a subdirectory of a
				// location inside root is inside root; a subdirectory of a
				// location outside root is outside root).
				return candidate, nil
			}
			// B-MAJOR-1 (external audit round 8, 2026-07-31 follow-up):
			// any OTHER Lstat failure (ACL/permission denied, a locked
			// file, or any other I/O error) means this component's true
			// nature -- ordinary directory, symlink, or junction -- could
			// not be positively determined. Silently treating that the
			// same as "doesn't exist" (the pre-round-8 behavior) would let
			// an ACL-protected reparse point slip past this walk
			// undetected, on the theory that "we couldn't see it, so
			// nothing more to resolve." Fail closed instead.
			return "", fmt.Errorf("resolve %q: cannot stat %q: %w", path, candidate, statErr)
		}
		if !isReparsePoint(candidate, fi) {
			current = candidate
			continue
		}
		hops++
		if hops > maxReparseResolutionHops {
			return "", fmt.Errorf("resolve %q: too many reparse-point hops (possible cycle)", path)
		}
		target, ok := resolveReparsePointTarget(candidate)
		if !ok {
			return "", fmt.Errorf("resolve %q: cannot read reparse point target at %q", path, candidate)
		}
		// B-BLOCKING-1 (external audit round 7, 2026-07-31 follow-up): push
		// the resolved target's OWN components onto the SAME pending queue
		// -- rather than substituting "current" wholesale and only
		// re-checking the resulting string as a whole -- so a reparse point
		// buried in one of the target's own intermediate components (e.g.
		// target="<root>/inner/pkg", where "inner" is itself a junction
		// pointing outside root) gets its own independent Lstat/reparse
		// check when the walk reaches it, exactly like any other component.
		// See this function's own "SECRET-NESTED" doc comment above
		// (pathWithinRoot) and TestResolveLocalSourceAgainstRoot_
		// NestedJunctionEscape_Rejected.
		targetVol, targetParts := splitAbsPathComponents(target)
		current = targetVol + string(filepath.Separator)
		pending = append(append([]string{}, targetParts...), pending...)
	}
	return current, nil
}

// splitAbsPathComponents splits an already-filepath.Abs-ed path into its
// volume name (e.g. "C:" on Windows, "" elsewhere) and its remaining path
// components, in walk order. Shared by resolveRealPathJunctionAware for both
// the path it is originally called with and every reparse-point target it
// substitutes in along the way (B-BLOCKING-1, external audit round 7,
// 2026-07-31 follow-up), so a target's own intermediate components are
// walked and reparse-checked exactly the same way the original path's are.
func splitAbsPathComponents(abs string) (vol string, parts []string) {
	vol = filepath.VolumeName(abs)
	rest := strings.TrimPrefix(abs[len(vol):], string(filepath.Separator))
	if rest == "" {
		return vol, nil
	}
	return vol, strings.Split(rest, string(filepath.Separator))
}

func pathWithinRootLexical(root, target string) bool {
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
// Reachable/VersionFound/RefOK mirror Python check.py's _CheckResult
// columns (Entry Health Check table): Reachable is false only when the
// remote itself could not be listed (network/offline failure), VersionFound
// reports whether any tag matched the entry's tag pattern at all, and RefOK
// is the overall pass/fail (always false when Err is non-nil).
type CheckResult struct {
	Package      PackageEntry
	Err          error
	Reachable    bool
	VersionFound bool
	RefOK        bool
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
// remote via lister.ListRefs. Local packages never touch the network -- a
// caller's lister that panics on any call proves zero network I/O for an
// all-local marketplace (or for remote packages with nothing pinned to
// verify) -- but ARE now resolved and stat-ed against dir (ticket 20 AC4,
// see checkPackage's own doc comment). offline=true fails every remote
// package that has a Ref or Version pinned to verify: this MVP keeps no
// on-disk ref cache to fall back on (design.md: "無快取可用視為失敗,不寬
// 貸"). dir is the project root a local ("./...") package's Source is
// resolved against -- CheckPackages' only caller (marketplaceCheckCmd)
// passes ".", matching the LoadAuthoringConfig(".") call that produced cfg.
func CheckPackages(dir string, cfg *AuthoringConfig, lister RefLister, offline bool) []CheckResult {
	results := make([]CheckResult, 0, len(cfg.Packages))
	for _, pkg := range cfg.Packages {
		results = append(results, checkPackage(dir, cfg, pkg, lister, offline))
	}
	return results
}

func checkPackage(dir string, cfg *AuthoringConfig, pkg PackageEntry, lister RefLister, offline bool) CheckResult {
	pass := CheckResult{Package: pkg, Reachable: true, VersionFound: true, RefOK: true}
	fail := func(reachable, versionFound bool, err error) CheckResult {
		return CheckResult{Package: pkg, Err: err, Reachable: reachable, VersionFound: versionFound}
	}

	// Ticket 20 AC4 (user-reported, 2026-08-25): this branch used to
	// `return pass` unconditionally, the same unconditional local-pass shape
	// as the Oracle's own commands/marketplace/check.py:121-134
	// (`entry.is_local` -> `_CheckResult(reachable=True, ...)` with no stat
	// at all) -- so `check` reported REACHABLE for a package whose local
	// directory did not exist (ticket 20's reproducer: `./llm-wiki\`, a
	// trailing-separator artifact `package add` itself now also refuses,
	// but any local path deleted or renamed after being added would hit
	// this same gap). This is a DELIBERATE apm-go-only hardening beyond the
	// Oracle: `check`'s whole purpose is to catch a pin that no longer
	// resolves, and a local path is exactly as "pin" as a remote ref is.
	// Reuses AddPackage's own resolveLocalSourceAgainstRoot +
	// verifyLocalSourceExists (editor.go) rather than a second check --
	// ResolveLocalSourceAgainstRoot's own doc comment already documents why
	// every reader of a local source must re-run this at its own read time
	// instead of trusting `add` having checked it once (B-BLOCKING-1,
	// external audit round 6, 2026-07-31 follow-up).
	if isLocalPackageSource(pkg.Source) {
		abs, err := resolveLocalSourceAgainstRoot(dir, pkg.Source)
		if err != nil {
			return fail(false, false, fmt.Errorf("package %q: %w", pkg.Name, err))
		}
		if err := verifyLocalSourceExists(pkg.Source, abs); err != nil {
			return fail(false, false, fmt.Errorf("package %q: %w", pkg.Name, err))
		}
		return pass
	}
	if pkg.Ref == "" && pkg.Version == "" {
		return pass
	}
	if offline {
		return fail(false, false, fmt.Errorf("package %q: --offline has no cached refs to verify %q against", pkg.Name, pkg.Source))
	}

	refs, err := lister.ListRefs(pkg.Source)
	if err != nil {
		return fail(false, false, fmt.Errorf("package %q: %w", pkg.Name, err))
	}

	if pkg.Ref != "" {
		if !hasRefNamed(refs, pkg.Ref) {
			return fail(true, false, fmt.Errorf("package %q: pinned ref %q not found on %q", pkg.Name, pkg.Ref, pkg.Source))
		}
		return pass
	}

	pattern := pkg.TagPattern
	if pattern == "" {
		pattern = cfg.Build.TagPattern
	}
	versionTags := tagpattern.FilterTags(refs, pattern, pkg.Name)
	_, ok, err := semver.MaxSatisfying(versionTags, pkg.Version)
	if err != nil {
		return fail(true, len(versionTags) > 0, fmt.Errorf("package %q: %w", pkg.Name, err))
	}
	if !ok {
		return fail(true, len(versionTags) > 0, fmt.Errorf("package %q: no tag on %q matches version range %q", pkg.Name, pkg.Source, pkg.Version))
	}
	return pass
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
