package authoring

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/apm-go/apm/internal/semver"
)

// ── test helpers: a real local git repo fixture ─────────────────────────

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s\n%s", args, err, out)
	}
	return string(bytes.TrimSpace(out))
}

// initGitRepoWithTags creates a real git repository in dir with a single
// commit and every tag in tags, so refcheck's production gitRefLister can
// run a genuine `git ls-remote` against it without any network access
// (mkt-041's "本地 git repo fixture" test requirement).
func initGitRepoWithTags(t *testing.T, dir string, tags ...string) {
	t.Helper()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "init")
	for _, tag := range tags {
		gitCmd(t, dir, "tag", tag)
	}
}

// panicLister is a RefLister fake that panics if ever called -- used to
// prove a code path takes zero network/subprocess action (mkt-041/mkt-046's
// "本地跳過網路,fake lister panic 斷言" convention).
type panicLister struct{}

func (panicLister) ListRefs(source string) ([]semver.TagInfo, error) {
	panic("ListRefs must not be called: this package should never touch the network")
}

// ── local packages never touch the network ──────────────────────────────

func TestCheckPackages_LocalSource_NeverCallsLister(t *testing.T) {
	// Arrange
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "local-a", Source: "./pkgs/a", Version: "^1.0.0"},
		{Name: "local-b", Source: "./pkgs/b", Ref: "v1.0.0"},
		{Name: "local-c", Source: "./pkgs/c"},
	}}

	// Act
	results := CheckPackages(cfg, panicLister{}, false)

	// Assert
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("package %q: unexpected error: %v", r.Package.Name, r.Err)
		}
	}
}

func TestCheckPackages_UnpinnedRemotePackage_NothingToVerify(t *testing.T) {
	// Arrange: a remote source with neither Ref nor Version pinned has
	// nothing for `check` to verify, so it must not touch the network
	// either.
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "unpinned", Source: "owner/repo"},
	}}

	// Act
	results := CheckPackages(cfg, panicLister{}, false)

	// Assert
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("results = %+v, want a single passing result", results)
	}
}

// ── --offline: no cache, so a pinned remote package always fails ────────

func TestCheckPackages_Offline_FailsPinnedRemotePackageWithoutNetwork(t *testing.T) {
	// Arrange
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "remote-tool", Source: "owner/repo", Version: "^1.0.0"},
	}}

	// Act: panicLister proves --offline never reaches the lister at all.
	results := CheckPackages(cfg, panicLister{}, true)

	// Assert
	if len(results) != 1 || results[0].Err == nil {
		t.Fatalf("results = %+v, want --offline to fail the pinned remote package", results)
	}
}

func TestCheckPackages_Offline_LocalPackageStillPasses(t *testing.T) {
	// Arrange
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "local-tool", Source: "./pkgs/a", Version: "^1.0.0"},
	}}

	// Act
	results := CheckPackages(cfg, panicLister{}, true)

	// Assert
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("results = %+v, want --offline to leave a local package unaffected", results)
	}
}

// ── remote packages: real git ls-remote against a local repo fixture ────

func TestCheckPackages_RemoteRef_FoundOnRealGitRepo(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0", "v1.1.0")
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "tool", Source: dir, Ref: "v1.1.0"},
	}}

	// Act
	results := CheckPackages(cfg, gitRefLister{}, false)

	// Assert
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("results = %+v, want the pinned ref to be found on the real repo", results)
	}
}

// TestGitRefLister_ListRefs_IncludesHEAD is the F4 regression: `newListRefsCmd`
// used to run `git ls-remote --tags --heads`, whose refs/tags/ and
// refs/heads/ namespace filters can never surface a "HEAD" line -- so
// editor.go's resolveRef, searching this same list for an exact `r.Name ==
// "HEAD"` match, could never resolve `package add/set --ref HEAD` (silently
// always failing with "ref \"HEAD\" not found", contradicting
// marketplace.md's documented "Mutable refs (HEAD, branches) are
// auto-resolved to a concrete SHA at write time" promise). Proven here
// against a real local git repo fixture (no network).
func TestGitRefLister_ListRefs_IncludesHEAD(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")
	wantSHA := gitCmd(t, dir, "rev-parse", "HEAD")

	// Act
	refs, err := gitRefLister{}.ListRefs(dir)

	// Assert
	if err != nil {
		t.Fatalf("ListRefs returned error: %v", err)
	}
	var headSHA string
	for _, r := range refs {
		if r.Name == "HEAD" {
			headSHA = r.Commit
		}
	}
	if headSHA == "" {
		t.Fatalf("ListRefs = %+v, want a \"HEAD\" entry", refs)
	}
	if headSHA != wantSHA {
		t.Errorf("HEAD entry commit = %q, want %q (the repo's actual HEAD SHA)", headSHA, wantSHA)
	}
	// The tag must still be present and correctly named -- proving the
	// explicit refspec patterns didn't regress ordinary tag/branch listing.
	foundTag := false
	for _, r := range refs {
		if r.Name == "v1.0.0" {
			foundTag = true
		}
	}
	if !foundTag {
		t.Errorf("ListRefs = %+v, want tag \"v1.0.0\" still present", refs)
	}
}

func TestCheckPackages_RemoteRef_MissingFailsCheck(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "tool", Source: dir, Ref: "v9.9.9"},
	}}

	// Act
	results := CheckPackages(cfg, gitRefLister{}, false)

	// Assert
	if len(results) != 1 || results[0].Err == nil {
		t.Fatal("expected a missing pinned ref to fail check")
	}
	if !strings.Contains(results[0].Err.Error(), "v9.9.9") {
		t.Errorf("error = %v, want it to name the missing ref", results[0].Err)
	}
}

func TestCheckPackages_RemoteVersionRange_MatchesRealTag(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0", "v1.2.0", "v2.0.0")
	cfg := &AuthoringConfig{
		Build: Build{TagPattern: "v{version}"},
		Packages: []PackageEntry{
			{Name: "tool", Source: dir, Version: "^1.0.0"},
		},
	}

	// Act
	results := CheckPackages(cfg, gitRefLister{}, false)

	// Assert
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("results = %+v, want ^1.0.0 satisfied by v1.2.0", results)
	}
}

func TestCheckPackages_RemoteVersionRange_NoMatchFailsCheck(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "tool", Source: dir, Version: "^2.0.0"},
	}}

	// Act
	results := CheckPackages(cfg, gitRefLister{}, false)

	// Assert
	if len(results) != 1 || results[0].Err == nil {
		t.Fatal("expected ^2.0.0 with no matching tag to fail check")
	}
}

func TestCheckPackages_RemoteVersionRange_UsesPackageTagPatternOverBuildDefault(t *testing.T) {
	// Arrange: a monorepo-style tag scheme ("tool-a-v1.2.0") that would
	// never match the plain "v{version}" build default.
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "tool-a-v1.0.0", "tool-a-v1.5.0")
	cfg := &AuthoringConfig{
		Build: Build{TagPattern: "v{version}"},
		Packages: []PackageEntry{
			{Name: "tool-a", Source: dir, Version: "^1.0.0", TagPattern: "{name}-v{version}"},
		},
	}

	// Act
	results := CheckPackages(cfg, gitRefLister{}, false)

	// Assert
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("results = %+v, want the package-level tagPattern to be used", results)
	}
}

func TestCheckPackages_RemoteSource_LsRemoteFailureFailsCheck(t *testing.T) {
	// Arrange: a path that is neither a local ("./") source nor a real git
	// repo -- git ls-remote against it must fail, and that failure must
	// surface as a check failure, not a panic/crash.
	dir := t.TempDir()
	notARepo := filepath.Join(dir, "not-a-repo")
	if err := os.MkdirAll(notARepo, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "tool", Source: notARepo, Ref: "v1.0.0"},
	}}

	// Act
	results := CheckPackages(cfg, gitRefLister{}, false)

	// Assert
	if len(results) != 1 || results[0].Err == nil {
		t.Fatal("expected git ls-remote against a non-repo path to fail check")
	}
}

// ── aggregation: multiple packages, some failing ─────────────────────────

func TestCheckPackages_AggregatesEveryPackageIndependently(t *testing.T) {
	// Arrange
	goodDir := t.TempDir()
	initGitRepoWithTags(t, goodDir, "v1.0.0")
	badDir := t.TempDir()
	initGitRepoWithTags(t, badDir, "v1.0.0")

	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "local", Source: "./pkgs/local"},
		{Name: "good-remote", Source: goodDir, Ref: "v1.0.0"},
		{Name: "bad-remote", Source: badDir, Ref: "v9.9.9"},
	}}

	// Act
	results := CheckPackages(cfg, gitRefLister{}, false)

	// Assert
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}
	if results[0].Err != nil {
		t.Errorf("local package failed unexpectedly: %v", results[0].Err)
	}
	if results[1].Err != nil {
		t.Errorf("good-remote package failed unexpectedly: %v", results[1].Err)
	}
	if results[2].Err == nil {
		t.Error("bad-remote package should have failed check")
	}
}

// ── duplicate package name warning (C6, non-fatal) ───────────────────────
//
// Mirrors Python's commands/marketplace/check.py:_warn_duplicate_names
// (__init__.py:170-184): two packages whose names collide
// case-insensitively is a WARNING, never a hard error -- `check`'s exit
// code is still driven solely by resolvable/unresolvable refs.

func TestDuplicatePackageNames_CaseDifferingCollision_ReturnsWarning(t *testing.T) {
	// Arrange
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "Foo-Tool", Source: "./pkgs/a"},
		{Name: "foo-tool", Source: "./pkgs/b"},
	}}

	// Act
	warnings := DuplicatePackageNames(cfg)

	// Assert
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly 1", warnings)
	}
	// Names the *later* (colliding) occurrence's own name, per Python's
	// f"Duplicate package name '{entry.name}' ..." (entry is packages[idx],
	// not the earlier packages[seen[lower]]).
	if !strings.Contains(warnings[0], "foo-tool") {
		t.Errorf("warning = %q, want it to name the colliding occurrence %q", warnings[0], "foo-tool")
	}
	if !strings.Contains(warnings[0], "packages[0]") || !strings.Contains(warnings[0], "packages[1]") {
		t.Errorf("warning = %q, want it to reference both package indices", warnings[0])
	}
}

func TestDuplicatePackageNames_UniqueNames_NoWarnings(t *testing.T) {
	// Arrange
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "tool-a", Source: "./pkgs/a"},
		{Name: "tool-b", Source: "./pkgs/b"},
	}}

	// Act
	warnings := DuplicatePackageNames(cfg)

	// Assert
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none for unique names", warnings)
	}
}

func TestDuplicatePackageNames_ThreeWayCollision_ReportsEachAdditionalOccurrence(t *testing.T) {
	// Arrange: mirrors Python's per-occurrence reporting (each repeat after
	// the first names the earlier index it collided with).
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "dup", Source: "./pkgs/a"},
		{Name: "dup", Source: "./pkgs/b"},
		{Name: "dup", Source: "./pkgs/c"},
	}}

	// Act
	warnings := DuplicatePackageNames(cfg)

	// Assert
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v, want 2 (one per additional occurrence)", warnings)
	}
}

// ── resolveCloneURL / parseRefsOutput unit coverage ──────────────────────

func TestResolveCloneURL(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{"full https URL passes through", "https://github.com/owner/repo"},
		{"scp-style ssh passes through", "git@github.com:owner/repo.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveCloneURL(tt.source)
			if err != nil {
				t.Fatalf("resolveCloneURL(%q) returned error: %v", tt.source, err)
			}
			if got != tt.source {
				t.Errorf("resolveCloneURL(%q) = %q, want unchanged", tt.source, got)
			}
		})
	}

	t.Run("absolute filesystem path passes through", func(t *testing.T) {
		abs := filepath.Join(t.TempDir(), "repo")
		got, err := resolveCloneURL(abs)
		if err != nil {
			t.Fatalf("resolveCloneURL(%q) returned error: %v", abs, err)
		}
		if got != abs {
			t.Errorf("resolveCloneURL(%q) = %q, want unchanged", abs, got)
		}
	})

	t.Run("owner/repo shorthand expands against github.com", func(t *testing.T) {
		want := "https://github.com/owner/repo.git"
		got, err := resolveCloneURL("owner/repo")
		if err != nil {
			t.Fatalf("resolveCloneURL(owner/repo) returned error: %v", err)
		}
		if got != want {
			t.Errorf("resolveCloneURL(owner/repo) = %q, want %q", got, want)
		}
	})

	// MAJOR 2 (external audit round 2, 2026-07-30): a relative "./..."
	// local source must resolve against the process's cwd, not fall
	// through to the OWNER/REPO shorthand branch (which used to produce
	// the bogus "https://github.com/./repo.git").
	t.Run("relative local source resolves against cwd, not as OWNER/REPO shorthand", func(t *testing.T) {
		parent := t.TempDir()
		chdirTo(t, parent)
		want := filepath.Join(parent, "repo")
		// MAJOR 1 (external audit round 5, 2026-07-30): pathWithinRoot's
		// boundary check now calls filepath.EvalSymlinks on target and
		// rejects on ANY error (including "does not exist"), so this fixture
		// must be a real, existing directory -- exactly like every
		// production caller: resolveCloneURL's local-source branch is only
		// ever reached for a package a caller expects to be a real git repo.
		if err := os.Mkdir(want, 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := resolveCloneURL("./repo")
		if err != nil {
			t.Fatalf("resolveCloneURL(./repo) returned error: %v", err)
		}
		if got != want {
			t.Errorf("resolveCloneURL(./repo) = %q, want %q (the local cwd-relative path, not a GitHub shorthand expansion)", got, want)
		}
	})

	// BLOCKING 1 (external audit round 3, 2026-07-30): a relative local
	// source whose "." segments resolve to a path outside the project
	// root (cwd) must be rejected here too -- layer 2's defense-in-depth
	// partner to manifest.ValidateMarketplaceSource's segment-level "..'
	// reject (mcp.go). Regression-tests the exact Windows-style bypass
	// the audit reproduced: "./..\\..\\outside" used to slip past a
	// forward-slash-only ".." split and then resolve to a real,
	// escaping path here.
	t.Run("relative local source escaping cwd via backslash traversal is rejected", func(t *testing.T) {
		parent := t.TempDir()
		project := filepath.Join(parent, "project")
		if err := os.Mkdir(project, 0o755); err != nil {
			t.Fatal(err)
		}
		chdirTo(t, project)
		if _, err := resolveCloneURL(`./..\..\outside`); err == nil {
			t.Fatal("resolveCloneURL(`./..\\..\\outside`) = nil error, want a rejection (path escapes the project root)")
		}
	})

	t.Run("relative local source escaping cwd via forward-slash traversal is rejected", func(t *testing.T) {
		parent := t.TempDir()
		project := filepath.Join(parent, "project")
		if err := os.Mkdir(project, 0o755); err != nil {
			t.Fatal(err)
		}
		chdirTo(t, project)
		if _, err := resolveCloneURL("./../../outside"); err == nil {
			t.Fatal("resolveCloneURL(./../../outside) = nil error, want a rejection (path escapes the project root)")
		}
	})

	// BLOCKING 2 (external audit round 4, 2026-07-30): a directory symlink
	// physically located inside the project root but pointing at a real
	// directory outside it must be rejected too -- the string
	// "<project>/linked" contains no ".." segment at all, so the purely
	// lexical filepath.Rel check pathWithinRoot used to rely on exclusively
	// reports it as "within root", while `git ls-remote` (or any real
	// filesystem access) follows the symlink at the OS level and actually
	// reaches the outside directory. Skipped (visibly, not silently) when
	// this process cannot create a directory symlink -- e.g. Windows
	// without Developer Mode or SeCreateSymbolicLinkPrivilege -- since that
	// is an environment limitation, not a test failure.
	t.Run("relative local source escaping cwd via a directory symlink is rejected", func(t *testing.T) {
		parent := t.TempDir()
		project := filepath.Join(parent, "project")
		outside := filepath.Join(parent, "outside")
		if err := os.Mkdir(project, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(outside, 0o755); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(project, "linked")
		if ok, symErr := createDirSymlinkOrJunction(t, outside, link); !ok {
			t.Skipf("SKIPPED: cannot create a directory symlink or junction in this environment (%v); BLOCKING 2's symlink-escape guard is untested by this run", symErr)
		}
		chdirTo(t, project)
		if _, err := resolveCloneURL("./linked"); err == nil {
			t.Fatal("resolveCloneURL(./linked) = nil error, want a rejection (symlink resolves outside the project root)")
		}
	})

	t.Run("relative local source staying within cwd is accepted", func(t *testing.T) {
		parent := t.TempDir()
		chdirTo(t, parent)
		want := filepath.Join(parent, "normal")
		// MAJOR 1 (external audit round 5, 2026-07-30): see the "resolves
		// against cwd" subtest above for why this must now be a real,
		// existing directory.
		if err := os.Mkdir(want, 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := resolveCloneURL("./normal")
		if err != nil {
			t.Fatalf("resolveCloneURL(./normal) returned error: %v", err)
		}
		if got != want {
			t.Errorf("resolveCloneURL(./normal) = %q, want %q", got, want)
		}
	})

	// MAJOR 1 (external audit round 5, 2026-07-30): a dangling leaf under an
	// EXISTING symlinked parent that already points outside the project
	// root -- the leaf itself does not exist, so filepath.EvalSymlinks fails
	// with an IsNotExist-classified error while the parent's escape has
	// already happened. The pre-round-5 code fell back to the lexical result
	// (which reports "within root", since the string "<project>/linked-
	// parent/not-yet-created" contains no ".." segment) on ANY EvalSymlinks
	// error, accepting this case -- a TOCTOU window: a second process could
	// create the missing leaf between this check and the subsequent `git
	// ls-remote` invocation, which would then genuinely follow the
	// already-escaping parent symlink out of root. Skipped (visibly, not
	// silently) when this process cannot create a directory symlink, same as
	// the sibling symlink subtest above.
	t.Run("relative local source with a dangling leaf under an escaping symlinked parent is rejected", func(t *testing.T) {
		parent := t.TempDir()
		project := filepath.Join(parent, "project")
		outside := filepath.Join(parent, "outside")
		if err := os.Mkdir(project, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(outside, 0o755); err != nil {
			t.Fatal(err)
		}
		linkedParent := filepath.Join(project, "linked-parent")
		if ok, symErr := createDirSymlinkOrJunction(t, outside, linkedParent); !ok {
			t.Skipf("SKIPPED: cannot create a directory symlink or junction in this environment (%v); MAJOR 1's dangling-leaf guard is untested by this run", symErr)
		}
		chdirTo(t, project)
		// Deliberately do NOT create "not-yet-created": that is the point of
		// this test -- the leaf must not exist yet.
		if _, err := resolveCloneURL("./linked-parent/not-yet-created"); err == nil {
			t.Fatal("resolveCloneURL(./linked-parent/not-yet-created) = nil error, want a rejection (parent symlink already resolves outside the project root, even though the leaf itself doesn't exist yet)")
		}
	})
}

// TestResolveCloneURL_JunctionEscape_Rejected is a dedicated regression test
// (2026-07-31 follow-up) that forces the junction branch directly -- via
// `mklink /J`, bypassing os.Symlink entirely -- rather than relying on
// os.Symlink failing first (as the fallback in TestResolveCloneURL's own
// symlink subtests does). This matters because a real symlink and a
// junction are NOT equivalent from pathWithinRoot's point of view: as of Go
// 1.23, os.Lstat no longer reports ModeSymlink for a junction by default, so
// filepath.EvalSymlinks (which decides whether to follow a path component
// solely by that bit) silently returns a path through a junction UNCHANGED
// instead of resolving or rejecting it -- verified directly against this Go
// toolchain (a prior version of this test, run against the pre-fix
// pathWithinRoot with only the lexical + EvalSymlinks layers, failed with
// "resolveCloneURL(./linked) via a junction = nil error, want rejection").
// A machine where os.Symlink itself happens to succeed (e.g. Developer Mode
// enabled) would never exercise the junction branch at all through the
// fallback alone, silently leaving this gap uncovered -- hence a test that
// always uses a junction regardless of symlink privilege.
func TestResolveCloneURL_JunctionEscape_Rejected(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("directory junctions are a Windows-only concept")
	}
	parent := t.TempDir()
	project := filepath.Join(parent, "project")
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, "linked")
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, outside)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("SKIPPED: cannot create a directory junction in this environment (%v: %s); the junction-escape guard is untested by this run", err, out)
	}
	chdirTo(t, project)
	if _, err := resolveCloneURL("./linked"); err == nil {
		t.Fatal("resolveCloneURL(./linked) via a junction = nil error, want a rejection (junction resolves outside the project root)")
	}
}

// TestResolveCloneURL_JunctionWithinRoot_Accepted is B-MAJOR-1's first named
// regression (external audit round 6, 2026-07-31 follow-up): a junction
// physically inside the project root that points to somewhere ELSE also
// inside the project root (an ordinary, non-escaping layout -- e.g. a
// monorepo sharing one real package directory under two names) must be
// ACCEPTED, not rejected. A prior version of pathWithinRoot's third layer
// (hasUnresolvableReparsePoint) rejected ANY existing reparse point along
// the path unconditionally, without ever checking where it actually
// pointed -- a false positive this test would have caught turning red.
func TestResolveCloneURL_JunctionWithinRoot_Accepted(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("directory junctions are a Windows-only concept")
	}
	project := t.TempDir()
	actual := filepath.Join(project, "actual-target")
	if err := os.Mkdir(actual, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, "linked")
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, actual)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("SKIPPED: cannot create a directory junction in this environment (%v: %s); this regression is untested by this run", err, out)
	}
	chdirTo(t, project)
	// resolveCloneURL returns the literal (unresolved) joined path -- the OS
	// itself transparently follows the junction when git actually opens it,
	// the same way it would for any other reparse point; the junction
	// resolution above is used only internally, to verify containment.
	want := link
	got, err := resolveCloneURL("./linked")
	if err != nil {
		t.Fatalf("resolveCloneURL(./linked) via a junction pointing WITHIN the project root returned error: %v, want acceptance", err)
	}
	if got != want {
		t.Errorf("resolveCloneURL(./linked) = %q, want the literal joined path %q", got, want)
	}
}

// TestResolveCloneURL_ProjectRootBehindJunction_LocalSourceWithinRoot_Accepted
// is B-MAJOR-1's second named regression: the project root itself (cwd)
// being reached through a junction (e.g. the checkout sits behind a Windows
// Dev Drive mapping, `subst`, or a parent-directory junction) must not, by
// itself, cause every local source under that project to be rejected. A
// prior version of hasUnresolvableReparsePoint special-cased
// isUnresolvableReparsePoint(root) as an automatic, unconditional rejection
// -- root's own path shape has nothing to do with whether target escapes
// it, and this test's local source (a perfectly ordinary subdirectory of the
// SAME real project) would have been wrongly rejected by that bug.
func TestResolveCloneURL_ProjectRootBehindJunction_LocalSourceWithinRoot_Accepted(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("directory junctions are a Windows-only concept")
	}
	parent := t.TempDir()
	realProject := filepath.Join(parent, "real-project")
	pkgDir := filepath.Join(realProject, "pkgs", "tool")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	aliasProject := filepath.Join(parent, "alias-project")
	cmd := exec.Command("cmd", "/c", "mklink", "/J", aliasProject, realProject)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("SKIPPED: cannot create a directory junction in this environment (%v: %s); this regression is untested by this run", err, out)
	}
	chdirTo(t, aliasProject)
	// resolveCloneURL returns the literal (unresolved) joined path, exactly
	// like TestResolveCloneURL_JunctionWithinRoot_Accepted above -- the
	// junction resolution is used only internally, to verify containment.
	want := filepath.Join(aliasProject, "pkgs", "tool")
	got, err := resolveCloneURL("./pkgs/tool")
	if err != nil {
		t.Fatalf("resolveCloneURL(./pkgs/tool) with a project root reached through a junction returned error: %v, want acceptance (the local source itself never escapes the real project)", err)
	}
	if got != want {
		t.Errorf("resolveCloneURL(./pkgs/tool) = %q, want the literal joined path %q", got, want)
	}
}

// TestResolveLocalSourceAgainstRoot_NestedJunctionEscape_Rejected is
// B-BLOCKING-1's named regression (external audit round 7, 2026-07-31
// follow-up, "SECRET-NESTED" repro): a junction physically inside the
// project root ("outer") that points at a path ("<root>/inner/pkg") whose
// OWN intermediate component ("inner") is a SEPARATE junction pointing
// outside root must be rejected -- not accepted just because the literal
// target string "<root>/inner/pkg" happens to look contained. Uses the
// exported ResolveLocalSourceAgainstRoot directly (rather than
// resolveCloneURL) since that is the entry point other marketplace packages
// (internal/marketplace/build/metadata.go) call to re-run this same
// containment check at their own read time.
func TestResolveLocalSourceAgainstRoot_NestedJunctionEscape_Rejected(t *testing.T) {
	t.Run("nested junction chain escapes through an intermediate component", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("directory junctions are a Windows-only concept")
		}
		parent := t.TempDir()
		root := filepath.Join(parent, "proj")
		outside := filepath.Join(parent, "outside")
		if err := os.MkdirAll(filepath.Join(outside, "pkg"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(root, 0o755); err != nil {
			t.Fatal(err)
		}

		// "inner" is a junction pointing entirely outside root.
		inner := filepath.Join(root, "inner")
		mklinkInner := exec.Command("cmd", "/c", "mklink", "/J", inner, outside)
		if out, err := mklinkInner.CombinedOutput(); err != nil {
			t.Skipf("SKIPPED: cannot create a directory junction in this environment (%v: %s); the nested-junction-escape guard is untested by this run", err, out)
		}

		// "outer" is a SECOND junction, physically inside root, whose target
		// is "<root>/inner/pkg" -- a path that is only reachable at all
		// because the OS transparently follows "inner" (itself a junction)
		// while validating this second mklink's target.
		outer := filepath.Join(root, "outer")
		mklinkOuter := exec.Command("cmd", "/c", "mklink", "/J", outer, filepath.Join(root, "inner", "pkg"))
		if out, err := mklinkOuter.CombinedOutput(); err != nil {
			t.Skipf("SKIPPED: cannot create the second (nested-target) directory junction in this environment (%v: %s); the nested-junction-escape guard is untested by this run", err, out)
		}

		if _, err := ResolveLocalSourceAgainstRoot(root, "./outer"); err == nil {
			t.Fatal("ResolveLocalSourceAgainstRoot(root, ./outer) = nil error, want a rejection: \"outer\" resolves (via \"inner\", itself a junction) to a real location OUTSIDE root, even though the literal target string \"<root>/inner/pkg\" looks contained")
		}
	})

	// B-BLOCKING-1's own fail-closed requirement: a reparse-point cycle
	// (two directory entries each pointing at the other) must be rejected
	// outright rather than looping forever or silently accepting one side.
	// Uses real directory symlinks (not junctions): a Windows junction's
	// target must already exist at mklink time, which makes a genuine
	// two-node cycle impossible to construct via mklink at all (whichever
	// side is created second would need the first side's target -- itself
	// dangling until the second side exists -- to already resolve).
	// Symlinks carry no such existence requirement, so they can form a real
	// cycle, and pathWithinRoot's isReparsePoint treats a real symlink and a
	// junction identically (fi.Mode()&os.ModeSymlink, true for either
	// reparse kind once resolveReparsePointTarget reads it via
	// os.Readlink).
	t.Run("cycle A->B->A fails closed", func(t *testing.T) {
		root := t.TempDir()
		a := filepath.Join(root, "a")
		b := filepath.Join(root, "b")
		if err := os.Symlink(b, a); err != nil {
			t.Skipf("SKIPPED: cannot create a directory symlink in this environment (%v); the reparse-point-cycle fail-closed guard is untested by this run", err)
		}
		if err := os.Symlink(a, b); err != nil {
			t.Skipf("SKIPPED: cannot create the second directory symlink completing the cycle in this environment (%v); the reparse-point-cycle fail-closed guard is untested by this run", err)
		}

		done := make(chan struct{})
		var err error
		go func() {
			defer close(done)
			_, err = ResolveLocalSourceAgainstRoot(root, "./a")
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("ResolveLocalSourceAgainstRoot(root, ./a) did not return within 5s against a genuine A->B->A reparse-point cycle -- maxReparseResolutionHops did not bound the walk")
		}
		if err == nil {
			t.Fatal("ResolveLocalSourceAgainstRoot(root, ./a) = nil error, want a fail-closed rejection of the A->B->A cycle (possible-cycle hop limit)")
		}
	})
}

// TestIsNameSurrogateReparseTag is B-MINOR-1's pure-logic regression
// (external audit round 7, 2026-07-31 follow-up): isJunctionOrUnknownReparse
// Point (reparse_windows.go) must resolve/reject only "name surrogate"
// reparse points (a real symlink or directory junction/mount point -- the
// only kinds that can make a path actually resolve somewhere other than its
// literal string), and must NOT treat a non-name-surrogate reparse point
// (e.g. a OneDrive/Cloud Files placeholder, NTFS deduplication, or an
// AppExecLink) the same way -- those attach alternate data at the SAME
// name, and rejecting them outright (the pre-round-7 behavior: any reparse
// point that os.Readlink could not resolve was treated as fail-closed)
// falsely broke every ordinary file inside a OneDrive-synced project
// directory. Runs on every platform (isNameSurrogateReparseTag has no OS
// dependency) so this exact bit-test is verified against Microsoft's own
// documented tag catalog even where a live Windows reparse point cannot be
// constructed.
func TestIsNameSurrogateReparseTag(t *testing.T) {
	tests := []struct {
		name string
		tag  uint32
		want bool
	}{
		{"IO_REPARSE_TAG_SYMLINK", 0xA000000C, true},
		{"IO_REPARSE_TAG_MOUNT_POINT", 0xA0000003, true},
		{"IO_REPARSE_TAG_CLOUD (OneDrive/Cloud Files placeholder)", 0x9000001A, false},
		{"IO_REPARSE_TAG_APPEXECLINK", 0x8000001B, false},
		{"IO_REPARSE_TAG_DEDUP", 0x80000013, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNameSurrogateReparseTag(tt.tag); got != tt.want {
				t.Errorf("isNameSurrogateReparseTag(0x%08X) = %v, want %v", tt.tag, got, tt.want)
			}
		})
	}
}

// chdirTo temporarily changes the process's working directory to dir for
// the duration of the test, restoring the original directory in cleanup.
// Needed for MAJOR 2's relative-local-source tests: resolveCloneURL's (and
// gitRefLister's) filepath.Abs resolution of a "./..." source depends on
// cwd, same as a real `apm marketplace package set` invocation running from
// the project root.
func chdirTo(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
}

// createDirSymlinkOrJunction creates a directory symlink at link pointing to
// target, falling back to a Windows directory junction (`mklink /J`) when a
// real symlink cannot be created (2026-07-31, external audit follow-up: a
// plain Windows account without Developer Mode or
// SeCreateSymbolicLinkPrivilege can never create a directory symlink, so
// BLOCKING 2's and MAJOR 1's symlink-escape guards were silently t.Skip-ped
// on every such machine -- an untested-in-practice regression gate, not a
// passing one). A junction needs no special privilege on Windows, and Go's
// filepath.EvalSymlinks (which pathWithinRoot relies on) follows a junction
// the same way it follows a symlink, so the fallback exercises the exact
// same production code path. Returns ok=false only when both mechanisms
// fail, so the caller can still visibly t.Skip as a last resort.
func createDirSymlinkOrJunction(t *testing.T, target, link string) (ok bool, lastErr error) {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return true, nil
	} else {
		lastErr = err
	}
	if runtime.GOOS != "windows" {
		return false, lastErr
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("mklink /J fallback also failed: %w: %s", err, bytes.TrimSpace(out))
	}
	return true, nil
}

// TestGitRefLister_ListRefs_RelativeLocalSource_ProductionLister is MAJOR
// 2's real-repo, production-lister proof (external audit round 2,
// 2026-07-30): the audit correctly pointed out that REGR-B2's tests only
// ever injected mapRefLister, so they proved "SetPackage no longer silently
// clears a local source's ref" but never proved "a *relative* local repo's
// ref actually resolves" through the real `git ls-remote` codepath (a
// relative source is the only shape a real local package's source ever
// takes -- see manifest.ValidateMarketplaceSource). This runs gitRefLister{}
// (the production RefLister, no fake) against a real git repo reached via a
// relative "./..." path.
func TestGitRefLister_ListRefs_RelativeLocalSource_ProductionLister(t *testing.T) {
	// Arrange
	parent := t.TempDir()
	repoDir := filepath.Join(parent, "repo")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	wantSHA := gitCmd(t, repoDir, "rev-parse", "v1.0.0")
	chdirTo(t, parent)

	// Act
	refs, err := gitRefLister{}.ListRefs("./repo")

	// Assert
	if err != nil {
		t.Fatalf("ListRefs(./repo) returned error: %v", err)
	}
	found := false
	for _, r := range refs {
		if r.Name == "v1.0.0" {
			found = true
			if r.Commit != wantSHA {
				t.Errorf("tag v1.0.0 commit = %q, want %q", r.Commit, wantSHA)
			}
		}
	}
	if !found {
		t.Errorf("refs = %+v, want tag v1.0.0 present (a relative local source must resolve against cwd, not be misread as an OWNER/REPO shorthand)", refs)
	}
}

func TestParseRefsOutput(t *testing.T) {
	// Arrange
	output := "abc123\trefs/tags/v1.0.0\n" +
		"def456\trefs/heads/main\n" +
		"\n"

	// Act
	refs := parseRefsOutput(output)

	// Assert
	if len(refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2", len(refs))
	}
	if refs[0].Name != "v1.0.0" || refs[0].Commit != "abc123" {
		t.Errorf("refs[0] = %+v", refs[0])
	}
	if refs[1].Name != "main" || refs[1].Commit != "def456" {
		t.Errorf("refs[1] = %+v", refs[1])
	}
}

func TestParseRefsOutput_Empty(t *testing.T) {
	if refs := parseRefsOutput(""); len(refs) != 0 {
		t.Errorf("parseRefsOutput(\"\") = %v, want empty", refs)
	}
}

// ── OutdatedPackages (mkt-042 修訂版): five status icons ─────────────────
//
// design.md's icon table:
//
//	[+] current == latest-in-range
//	[!] range 內有可升級(計入 exit 1)-- 同圖示也用於「no matching tags」(不計入)
//	[*] latest overall != latest in range (range 外任何更新,不限 major)
//	[i] 已 pin ref 或無 range,略過
//	[x] 遠端抓取失敗,不影響 exit code
//
// "current" is whatever a prior `apm pack` run last published a package at
// (see OutdatedPackages's doc comment); these tests supply it directly via
// the `current` map rather than through any file, since this sub-task has no
// `apm pack` (mkt-050+, a separate not-yet-landed sub-task) to produce one.

func TestOutdatedPackages_IconPlus_CurrentMatchesLatestInRangeAndOverall(t *testing.T) {
	// Arrange: nothing outside ^1.0.0 exists either, so there is nothing to
	// override [+] to [*].
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0", "v1.1.0")
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "tool", Source: dir, Version: "^1.0.0"},
	}}

	// Act
	rows := OutdatedPackages(cfg, gitRefLister{}, false, false, map[string]string{"tool": "v1.1.0"})

	// Assert
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.Status != "[+]" {
		t.Errorf("Status = %q, want [+]", r.Status)
	}
	if r.Upgradable {
		t.Error("Upgradable = true, want false for an up-to-date package")
	}
	if r.LatestInRange != "v1.1.0" || r.LatestOverall != "v1.1.0" {
		t.Errorf("LatestInRange/LatestOverall = %q/%q, want v1.1.0/v1.1.0", r.LatestInRange, r.LatestOverall)
	}
}

func TestOutdatedPackages_IconBang_UpgradableWithinRange_CountsTowardExit1(t *testing.T) {
	// Arrange: current is stale (v1.0.0) but a newer in-range tag (v1.1.0)
	// exists, and nothing outside the range beats it either.
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0", "v1.1.0")
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "tool", Source: dir, Version: "^1.0.0"},
	}}

	// Act
	rows := OutdatedPackages(cfg, gitRefLister{}, false, false, map[string]string{"tool": "v1.0.0"})

	// Assert
	if len(rows) != 1 || rows[0].Status != "[!]" {
		t.Fatalf("rows = %+v, want a single [!] row", rows)
	}
	if !rows[0].Upgradable {
		t.Error("Upgradable = false, want true: mkt-042's exit 1 must count this row")
	}
}

func TestOutdatedPackages_IconBang_NoMatchingTagsFound_DoesNotCountTowardExit1(t *testing.T) {
	// Arrange: the repo's only tag does not match the "v{version}" pattern
	// at all.
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "release-1")
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "tool", Source: dir, Version: "^1.0.0"},
	}}

	// Act
	rows := OutdatedPackages(cfg, gitRefLister{}, false, false, nil)

	// Assert
	if len(rows) != 1 || rows[0].Status != "[!]" {
		t.Fatalf("rows = %+v, want a single [!] row", rows)
	}
	if rows[0].Upgradable {
		t.Error("Upgradable = true, want false: \"no matching tags found\" must NOT count toward exit 1 (mkt-042 修訂版)")
	}
	if !strings.Contains(rows[0].Note, "no matching tags") {
		t.Errorf("Note = %q, want it to mention no matching tags", rows[0].Note)
	}
}

func TestOutdatedPackages_IconStar_OverridesPlus_LatestOverallOutsideRange(t *testing.T) {
	// Arrange: current already matches the range's ceiling (v1.1.0), which
	// alone would be [+], but v2.0.0 exists outside the ^1.0.0 range -- so
	// the final status must be overridden to [*]. Because the pre-override
	// branch was [+] (up to date within the range), this row must NOT count
	// toward exit 1 -- mkt-042's "exit 1 僅由 upgradable 計數驅動", not by the
	// displayed [*] status.
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0", "v1.1.0", "v2.0.0")
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "tool", Source: dir, Version: "^1.0.0"},
	}}

	// Act
	rows := OutdatedPackages(cfg, gitRefLister{}, false, false, map[string]string{"tool": "v1.1.0"})

	// Assert
	if len(rows) != 1 || rows[0].Status != "[*]" {
		t.Fatalf("rows = %+v, want a single [*] row", rows)
	}
	if rows[0].Upgradable {
		t.Error("Upgradable = true, want false: this [*] row's pre-override branch was [+]")
	}
	if rows[0].LatestOverall != "v2.0.0" {
		t.Errorf("LatestOverall = %q, want v2.0.0", rows[0].LatestOverall)
	}
}

func TestOutdatedPackages_IconStar_OverridesBang_StillCountsTowardExit1(t *testing.T) {
	// Arrange: same tags as above, but current is stale (v1.0.0) so the
	// pre-override branch is [!] (counted); the final displayed status is
	// still overridden to [*] by the out-of-range v2.0.0, but the
	// upgradable count set by the earlier [!] branch must survive the
	// override, per outdated.py:116-128's own counter semantics.
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0", "v1.1.0", "v2.0.0")
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "tool", Source: dir, Version: "^1.0.0"},
	}}

	// Act
	rows := OutdatedPackages(cfg, gitRefLister{}, false, false, map[string]string{"tool": "v1.0.0"})

	// Assert
	if len(rows) != 1 || rows[0].Status != "[*]" {
		t.Fatalf("rows = %+v, want a single [*] row", rows)
	}
	if !rows[0].Upgradable {
		t.Error("Upgradable = false, want true: the pre-override [!] branch's count must survive the [*] override")
	}
}

func TestOutdatedPackages_IconI_PinnedRefLocalOrNoRange_NeverTouchesNetwork(t *testing.T) {
	// Arrange
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "pinned", Source: "owner/repo", Ref: "v1.0.0"},
		{Name: "no-range", Source: "owner/repo"},
		{Name: "local", Source: "./pkgs/a", Version: "^1.0.0"},
	}}

	// Act: panicLister proves none of these ever touch the network.
	rows := OutdatedPackages(cfg, panicLister{}, false, false, nil)

	// Assert
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	for _, r := range rows {
		if r.Status != "[i]" {
			t.Errorf("package %q: Status = %q, want [i]", r.Package.Name, r.Status)
		}
		if r.Upgradable {
			t.Errorf("package %q: Upgradable = true, want false for a skipped package", r.Package.Name)
		}
	}
}

func TestOutdatedPackages_IconX_FetchFailure_DoesNotCountTowardExit1(t *testing.T) {
	// Arrange: not a real git repo, so `git ls-remote` fails.
	dir := t.TempDir()
	notARepo := filepath.Join(dir, "not-a-repo")
	if err := os.MkdirAll(notARepo, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "tool", Source: notARepo, Version: "^1.0.0"},
	}}

	// Act
	rows := OutdatedPackages(cfg, gitRefLister{}, false, false, nil)

	// Assert
	if len(rows) != 1 || rows[0].Status != "[x]" {
		t.Fatalf("rows = %+v, want a single [x] row", rows)
	}
	if rows[0].Upgradable {
		t.Error("Upgradable = true, want false: mkt-042's [x] must not affect exit code")
	}
}

func TestOutdatedPackages_Offline_NeverTouchesNetworkAndReportsIconX(t *testing.T) {
	// Arrange
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "tool", Source: "owner/repo", Version: "^1.0.0"},
	}}

	// Act: panicLister proves --offline short-circuits before ever calling
	// ListRefs.
	rows := OutdatedPackages(cfg, panicLister{}, true, false, nil)

	// Assert
	if len(rows) != 1 || rows[0].Status != "[x]" {
		t.Fatalf("rows = %+v, want a single [x] row", rows)
	}
	if rows[0].Upgradable {
		t.Error("Upgradable = true, want false: --offline's [x] must not affect exit code")
	}
}

func TestOutdatedPackages_IncludePrerelease_RevealsNewerPrereleaseOutsideRange(t *testing.T) {
	// Arrange: v1.1.0-beta.1 is numerically newer than v1.0.0 but is a
	// prerelease, and it does not satisfy ^1.0.0 under npm semver rules
	// (only a same-[major,minor,patch] prerelease range would match it) --
	// so its only visible effect is on LatestOverall/[*], not on which tag
	// satisfies the range.
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0", "v1.1.0-beta.1")
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "tool", Source: dir, Version: "^1.0.0"},
	}}
	current := map[string]string{"tool": "v1.0.0"}

	t.Run("without --include-prerelease, the prerelease tag is invisible", func(t *testing.T) {
		rows := OutdatedPackages(cfg, gitRefLister{}, false, false, current)
		if len(rows) != 1 {
			t.Fatalf("len(rows) = %d, want 1", len(rows))
		}
		if rows[0].Status != "[+]" {
			t.Errorf("Status = %q, want [+] (v1.0.0 is the only visible tag, and current matches it)", rows[0].Status)
		}
		if rows[0].LatestOverall != "v1.0.0" {
			t.Errorf("LatestOverall = %q, want v1.0.0 with prereleases excluded", rows[0].LatestOverall)
		}
	})

	t.Run("with --include-prerelease, the newer prerelease becomes LatestOverall", func(t *testing.T) {
		rows := OutdatedPackages(cfg, gitRefLister{}, false, true, current)
		if len(rows) != 1 {
			t.Fatalf("len(rows) = %d, want 1", len(rows))
		}
		if rows[0].LatestOverall != "v1.1.0-beta.1" {
			t.Errorf("LatestOverall = %q, want v1.1.0-beta.1 with --include-prerelease", rows[0].LatestOverall)
		}
		if rows[0].Status != "[*]" {
			t.Errorf("Status = %q, want [*] once the prerelease is visible outside the range's satisfying set", rows[0].Status)
		}
	})
}

func TestOutdatedPackages_PerPackageIncludePrereleaseOverridesGlobalFlag(t *testing.T) {
	// Arrange: the global --include-prerelease flag is false, but the
	// package entry's own IncludePrerelease is true (mkt-042/mkt-045's "or
	// entry.include_prerelease" rule, ported from Python's
	// _extract_tag_versions).
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0", "v1.1.0-beta.1")
	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "tool", Source: dir, Version: "^1.0.0", IncludePrerelease: true},
	}}

	// Act
	rows := OutdatedPackages(cfg, gitRefLister{}, false, false, nil)

	// Assert
	if len(rows) != 1 || rows[0].LatestOverall != "v1.1.0-beta.1" {
		t.Fatalf("rows = %+v, want the package's own IncludePrerelease to reveal v1.1.0-beta.1", rows)
	}
}

func TestOutdatedPackages_AggregatesUpgradableCountAcrossPackages(t *testing.T) {
	// Arrange: three packages exercising three different final statuses,
	// proving exit-1 aggregation is driven by the Upgradable field, not by
	// scanning Status strings (a [*] row can be either counted or not).
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0", "v1.1.0")

	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "up-to-date", Source: dir, Version: "^1.0.0"},    // current below == [+]
		{Name: "needs-upgrade", Source: dir, Version: "^1.0.0"}, // current stale == [!], counted
		{Name: "pinned", Source: dir, Ref: "v1.0.0"},            // [i], never counted
	}}
	current := map[string]string{"up-to-date": "v1.1.0", "needs-upgrade": "v1.0.0"}

	// Act
	rows := OutdatedPackages(cfg, gitRefLister{}, false, false, current)

	// Assert
	upgradable := 0
	for _, r := range rows {
		if r.Upgradable {
			upgradable++
		}
	}
	if upgradable != 1 {
		t.Fatalf("upgradable count = %d, want 1 (only \"needs-upgrade\")", upgradable)
	}
}

// ── credential-prompt hardening / timeout (修正組 G) ──────────────────────

// buildFakeGit compiles internal/gitops/testdata/fakegit into a fresh temp
// dir under the platform's expected "git" executable name, returning that
// dir so the caller can prepend it to PATH. The fake binary's behavior is
// controlled via FAKEGIT_SLEEP_MS/FAKEGIT_FAIL_STDERR env vars (see that
// package's doc comment).
func buildFakeGit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	name := "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", out, "../../gitops/testdata/fakegit/main.go")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build fakegit: %v\n%s", err, output)
	}
	return dir
}

// TestNewListRefsCmd_AppliesSecureGitEnv proves ListRefs's subprocess is
// wired through gitops.SecureGitEnv by construction, without spawning a
// subprocess.
func TestNewListRefsCmd_AppliesSecureGitEnv(t *testing.T) {
	// Act
	cmd := newListRefsCmd(context.Background(), "https://example.invalid/owner/repo.git")

	// Assert
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "GCM_INTERACTIVE=never"} {
		found := false
		for _, e := range cmd.Env {
			if e == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("newListRefsCmd().Env missing %q; got %v", want, cmd.Env)
		}
	}
}

// TestGitRefLister_ListRefs_TimesOutOnSlowRemote proves ListRefs never
// hangs indefinitely: a "git" that sleeps far longer than listRefsTimeout
// must still cause ListRefs to return promptly with an error once the
// context deadline fires (review finding F3 HIGH).
func TestGitRefLister_ListRefs_TimesOutOnSlowRemote(t *testing.T) {
	// Arrange
	fakeGitDir := buildFakeGit(t)
	t.Setenv("PATH", fakeGitDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKEGIT_SLEEP_MS", "5000")

	orig := listRefsTimeout
	listRefsTimeout = 200 * time.Millisecond
	t.Cleanup(func() { listRefsTimeout = orig })

	// Act
	start := time.Now()
	_, err := (gitRefLister{}).ListRefs("https://example.invalid/owner/repo.git")
	elapsed := time.Since(start)

	// Assert
	if err == nil {
		t.Fatal("expected ListRefs to fail once the timeout fires")
	}
	if elapsed > 3*time.Second {
		t.Errorf("ListRefs took %v, want it to return promptly once the context deadline fires", elapsed)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %v, want it to mention the timeout", err)
	}
}

// TestGitRefLister_ListRefs_SanitizesCredentialsInErrorMessage proves a
// failing git subprocess's stderr (which can echo the clone URL,
// credentials and all) never leaks a token into ListRefs's returned error.
func TestGitRefLister_ListRefs_SanitizesCredentialsInErrorMessage(t *testing.T) {
	// Arrange
	fakeGitDir := buildFakeGit(t)
	t.Setenv("PATH", fakeGitDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKEGIT_FAIL_STDERR", "fatal: unable to access 'https://x-access-token:ghp_supersecret@example.com/owner/repo.git/': The requested URL returned error: 403")

	// Act
	_, err := (gitRefLister{}).ListRefs("https://x-access-token:ghp_supersecret@example.com/owner/repo.git")

	// Assert
	if err == nil {
		t.Fatal("expected ListRefs to fail")
	}
	if strings.Contains(err.Error(), "ghp_supersecret") {
		t.Errorf("ListRefs error leaked a credential: %v", err)
	}
	if !strings.Contains(err.Error(), "example.com") {
		t.Errorf("ListRefs error lost the host entirely: %v", err)
	}
}

// TestSplitHostFromSource mirrors upstream split_host_from_source
// (yml_schema.py:125-140, nested-https widened by v0.28.0 PR #2439).
func TestSplitHostFromSource(t *testing.T) {
	tests := []struct {
		source   string
		wantHost string
		wantRepo string
	}{
		{"owner/repo", "", "owner/repo"},
		{"./pkgs/tool", "", "./pkgs/tool"},
		{"gitlab.com/owner/repo", "gitlab.com", "owner/repo"},
		{"https://gitlab.com/owner/repo", "gitlab.com", "owner/repo"},
		{"https://gitlab.com/owner/repo.git", "gitlab.com", "owner/repo"},
		{"https://gitlab.com/group/subgroup/repo", "gitlab.com", "group/subgroup/repo"},
		{"https://gitlab.com/group/subgroup/repo.git", "gitlab.com", "group/subgroup/repo"},
	}
	for _, tt := range tests {
		host, repo := SplitHostFromSource(tt.source)
		if host != tt.wantHost || repo != tt.wantRepo {
			t.Errorf("SplitHostFromSource(%q) = (%q, %q), want (%q, %q)", tt.source, host, repo, tt.wantHost, tt.wantRepo)
		}
	}
}

// TestResolveCloneURL_HostPrefixedShorthand_RoutesToItsHost covers v0.28.0
// PR #2439: a host-qualified shorthand must clone from ITS host, not be
// concatenated onto github.com.
func TestResolveCloneURL_HostPrefixedShorthand_RoutesToItsHost(t *testing.T) {
	got, err := resolveCloneURL("gitlab.com/owner/repo")
	if err != nil {
		t.Fatalf("resolveCloneURL returned error: %v", err)
	}
	if got != "https://gitlab.com/owner/repo.git" {
		t.Errorf("resolveCloneURL(gitlab.com/owner/repo) = %q, want https://gitlab.com/owner/repo.git", got)
	}
}
