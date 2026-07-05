package authoring

import (
	"bytes"
	"context"
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
			if got := resolveCloneURL(tt.source); got != tt.source {
				t.Errorf("resolveCloneURL(%q) = %q, want unchanged", tt.source, got)
			}
		})
	}

	t.Run("absolute filesystem path passes through", func(t *testing.T) {
		abs := filepath.Join(t.TempDir(), "repo")
		if got := resolveCloneURL(abs); got != abs {
			t.Errorf("resolveCloneURL(%q) = %q, want unchanged", abs, got)
		}
	})

	t.Run("owner/repo shorthand expands against github.com", func(t *testing.T) {
		want := "https://github.com/owner/repo.git"
		if got := resolveCloneURL("owner/repo"); got != want {
			t.Errorf("resolveCloneURL(owner/repo) = %q, want %q", got, want)
		}
	})
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
