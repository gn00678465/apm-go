package authoring

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apm-go/apm/internal/semver"
)

// SPEC marketplace-check-outdated §B: `check` on SHA pins (SC-B1..B6),
// full ref names and HEAD (SC-B7/B8), tag-pattern inference (SC-B9),
// prerelease opt-in (SC-B12) and the manifest-version comparison
// (SC-B13..B19). Every git operation targets a t.TempDir() repository.

// ── fakes ────────────────────────────────────────────────────────────────

type panicProber struct{}

func (panicProber) HasCommit(source, sha string) (bool, error) {
	panic("HasCommit must not be called: the SHA was already listed by ls-remote")
}

type panicManifestFetcher struct{}

func (panicManifestFetcher) FetchManifestVersion(source, ref, subdir string) (string, error) {
	panic("FetchManifestVersion must not be called for this entry")
}

type errProber struct{ err error }

func (p errProber) HasCommit(string, string) (bool, error) { return false, p.err }

type errManifestFetcher struct{ err error }

func (f errManifestFetcher) FetchManifestVersion(string, string, string) (string, error) {
	return "", f.err
}

// realDeps runs every seam against the local fixture repository.
func realDeps() CheckDeps {
	return CheckDeps{Lister: gitRefLister{}, Prober: gitCommitProber{}, Manifest: gitManifestVersionFetcher{}}
}

func noProbeDeps() CheckDeps {
	return CheckDeps{Lister: gitRefLister{}, Prober: panicProber{}, Manifest: panicManifestFetcher{}}
}

func addCommit(t *testing.T, dir, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, msg+".txt"), []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", msg)
	return gitCmd(t, dir, "rev-parse", "HEAD")
}

func writeManifest(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func singleResult(t *testing.T, dir string, pkg PackageEntry, deps CheckDeps) CheckResult {
	t.Helper()
	cfg := &AuthoringConfig{Packages: []PackageEntry{pkg}}
	results := CheckPackagesWith(dir, cfg, deps, false)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	return results[0]
}

func wantPass(t *testing.T, r CheckResult) {
	t.Helper()
	if r.Err != nil || !r.Reachable || !r.VersionFound || !r.RefOK {
		t.Fatalf("result = %+v, want a clean pass", r)
	}
}

// ── SC-B1 ────────────────────────────────────────────────────────────────

func TestCheckPackages_ShaPin_MatchesListedCommit_NoProbe(t *testing.T) {
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")
	tagSHA := gitCmd(t, dir, "rev-parse", "v1.0.0^{commit}")

	wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: tagSHA}, noProbeDeps()))

	t.Run("BranchTipWithoutTag", func(t *testing.T) {
		gitCmd(t, dir, "checkout", "-q", "-b", "feature")
		tipSHA := addCommit(t, dir, "feature-work")
		wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: tipSHA}, noProbeDeps()))
	})
}

// ── SC-B2 ────────────────────────────────────────────────────────────────

func TestCheckPackages_ShaPin_NotAtAnyRef_ProbeSucceeds(t *testing.T) {
	dir := t.TempDir()
	initGitRepoWithTags(t, dir)
	first := gitCmd(t, dir, "rev-parse", "HEAD")
	addCommit(t, dir, "second")

	wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: first}, realDeps()))
}

func TestNewProbeFetchCmd_ShapeAndSecureEnv(t *testing.T) {
	cmd := newProbeFetchCmd(context.Background(), t.TempDir(), "https://example.invalid/o/r.git", strings.Repeat("a", 40))
	want := []string{"fetch", "--depth", "1", "--", "https://example.invalid/o/r.git", strings.Repeat("a", 40)}
	if len(cmd.Args) < len(want) {
		t.Fatalf("args = %v, want suffix %v", cmd.Args, want)
	}
	got := cmd.Args[len(cmd.Args)-len(want):]
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want suffix %v", cmd.Args, want)
		}
	}
	for _, e := range []string{"GIT_TERMINAL_PROMPT=0"} {
		found := false
		for _, env := range cmd.Env {
			if env == e {
				found = true
			}
		}
		if !found {
			t.Errorf("probe cmd env missing %q", e)
		}
	}
}

// ── SC-B3 ────────────────────────────────────────────────────────────────

func TestCheckPackages_ShaPin_Missing_ProbeFails_NotFound(t *testing.T) {
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")
	missing := strings.Repeat("0", 39) + "1"

	r := singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: missing}, realDeps())

	if r.Err == nil || r.Err.Error() != "Ref '"+missing+"' not found" {
		t.Fatalf("Err = %v, want the Oracle's \"Ref '<ref>' not found\"", r.Err)
	}
	if !r.Reachable || r.VersionFound || r.RefOK {
		t.Errorf("flags = reachable:%v versionFound:%v refOK:%v, want true/false/false", r.Reachable, r.VersionFound, r.RefOK)
	}
}

// ── SC-B4 ────────────────────────────────────────────────────────────────

func TestCheckPackages_ShaProbe_TempDirAlwaysRemoved(t *testing.T) {
	scratch := t.TempDir()
	orig := scratchTempRoot
	scratchTempRoot = scratch
	t.Cleanup(func() { scratchTempRoot = orig })

	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")
	first := gitCmd(t, dir, "rev-parse", "HEAD")
	addCommit(t, dir, "second")
	writeManifest(t, dir, ".claude-plugin/plugin.json", `{"version":"1.0.0"}`)
	third := addCommit(t, dir, "manifest")

	cfg := &AuthoringConfig{Packages: []PackageEntry{
		{Name: "probe-ok", Source: dir, Ref: first},
		{Name: "probe-missing", Source: dir, Ref: strings.Repeat("0", 39) + "1"},
		{Name: "manifest-ok", Source: dir, Ref: third, Version: "1.0.0"},
		{Name: "manifest-fail", Source: filepath.Join(t.TempDir(), "no-such-repo"), Ref: third, Version: "1.0.0"},
	}}
	CheckPackagesWith(dir, cfg, realDeps(), false)

	entries, err := os.ReadDir(scratch)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("temp dirs left behind after probes: %v", names)
	}
}

// ── SC-B5 ────────────────────────────────────────────────────────────────

func TestCheckPackages_ShaProbe_NetworkError_Unreachable(t *testing.T) {
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")
	sha := strings.Repeat("0", 39) + "1"

	r := singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: sha}, CheckDeps{
		Lister: gitRefLister{}, Prober: errProber{errors.New("git fetch: connection reset")}, Manifest: panicManifestFetcher{},
	})

	if r.Err == nil || !strings.Contains(r.Err.Error(), "connection reset") {
		t.Fatalf("Err = %v, want the probe's error surfaced", r.Err)
	}
	if r.Reachable || r.RefOK {
		t.Errorf("a probe transport failure must be Reachable=false, got %+v", r)
	}
}

func TestGitCommitProber_SanitizesTokenInError(t *testing.T) {
	fakeGitDir := buildFakeGit(t)
	t.Setenv("PATH", fakeGitDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKEGIT_FAIL_STDERR", "fatal: unable to access 'https://x-access-token:ghp_supersecret@example.com/owner/repo.git/': The requested URL returned error: 403")

	_, err := gitCommitProber{}.HasCommit("https://x-access-token:ghp_supersecret@example.com/owner/repo.git", strings.Repeat("a", 40))

	if err == nil {
		t.Fatal("expected the fake git failure to surface as an error")
	}
	if strings.Contains(err.Error(), "ghp_supersecret") {
		t.Errorf("token leaked into probe error: %q", err.Error())
	}
}

// ── SC-B6 ────────────────────────────────────────────────────────────────

func TestCheckPackages_ShaProbe_TimesOut(t *testing.T) {
	fakeGitDir := buildFakeGit(t)
	t.Setenv("PATH", fakeGitDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKEGIT_SLEEP_MS", "5000")
	orig := listRefsTimeout
	listRefsTimeout = 200 * time.Millisecond
	t.Cleanup(func() { listRefsTimeout = orig })

	start := time.Now()
	_, err := gitCommitProber{}.HasCommit("https://example.invalid/o/r.git", strings.Repeat("a", 40))
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("probe err = %v, want a timed-out error", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("probe took %s; the timeout did not fire", time.Since(start))
	}

	t.Run("ManifestFetch", func(t *testing.T) {
		start := time.Now()
		_, err := gitManifestVersionFetcher{}.FetchManifestVersion("https://example.invalid/o/r.git", "v1.0.0", "")
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("manifest fetch err = %v, want a timed-out error", err)
		}
		if time.Since(start) > 3*time.Second {
			t.Errorf("manifest fetch took %s; the timeout did not fire", time.Since(start))
		}
	})
}

// ── SC-B7 ────────────────────────────────────────────────────────────────

func TestCheckPackages_FullRefName_Accepted(t *testing.T) {
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")
	branch := gitCmd(t, dir, "rev-parse", "--abbrev-ref", "HEAD")

	wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: "refs/tags/v1.0.0"}, noProbeDeps()))
	wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: "refs/heads/" + branch}, noProbeDeps()))
}

// ── SC-B8 ────────────────────────────────────────────────────────────────

func TestCheckPackages_HeadRef_Rejected(t *testing.T) {
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")

	r := singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: "HEAD"}, noProbeDeps())

	want := "Ref 'HEAD' not found (run 'apm-go marketplace package set tool --ref HEAD' to pin a SHA)"
	if r.Err == nil || r.Err.Error() != want {
		t.Fatalf("Err = %v, want %q", r.Err, want)
	}
	if r.RefOK || !r.Reachable {
		t.Errorf("flags = %+v, want Reachable=true RefOK=false", r)
	}
	// The synthetic HEAD entry itself must survive: `package set --ref HEAD`
	// resolves through it (TestGitRefLister_ListRefs_IncludesHEAD).
	refs, err := gitRefLister{}.ListRefs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRefNamed(refs, "HEAD") {
		t.Error("ListRefs no longer advertises the synthetic HEAD entry")
	}
}

// ── SC-B9 ────────────────────────────────────────────────────────────────

func TestCheckPackages_VersionRange_TagPatternFallback(t *testing.T) {
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "tool_v1.2.0")
	gitCmd(t, dir, "branch", "v9.9.9")
	cfg := &AuthoringConfig{Packages: []PackageEntry{{Name: "tool", Source: dir, Version: "^1.0.0"}}}

	results := CheckPackagesWith(dir, cfg, noProbeDeps(), false)
	wantPass(t, results[0])

	t.Run("BranchNamedLikeAVersionIsNotATag", func(t *testing.T) {
		cfg := &AuthoringConfig{Packages: []PackageEntry{{Name: "tool", Source: dir, Version: "^9.0.0"}}}
		r := CheckPackagesWith(dir, cfg, noProbeDeps(), false)[0]
		if r.Err == nil || r.Err.Error() != "No tag matching '^9.0.0'" {
			t.Fatalf("Err = %v, want \"No tag matching '^9.0.0'\" (branch v9.9.9 must not count)", r.Err)
		}
	})
}

// ── SC-B12 ───────────────────────────────────────────────────────────────

func TestCheckPackages_VersionRange_ExcludesPrerelease_UnlessOptIn(t *testing.T) {
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0-beta.1")

	r := singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Version: "1.0.0-beta.1"}, noProbeDeps())
	if r.Err == nil || r.Err.Error() != "No tag matching '1.0.0-beta.1'" {
		t.Fatalf("without include_prerelease Err = %v, want the prerelease tag excluded", r.Err)
	}

	wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Version: "1.0.0-beta.1", IncludePrerelease: true}, noProbeDeps()))
}

// ── SC-B13..B19: manifest version comparison ─────────────────────────────

func manifestRepo(t *testing.T, pluginJSON, apmYML string) (dir, sha string) {
	t.Helper()
	dir = t.TempDir()
	initGitRepoWithTags(t, dir)
	if pluginJSON != "" {
		writeManifest(t, dir, ".claude-plugin/plugin.json", pluginJSON)
	}
	if apmYML != "" {
		writeManifest(t, dir, "apm.yml", apmYML)
	}
	sha = addCommit(t, dir, "manifest")
	gitCmd(t, dir, "tag", "v1.0.0")
	return dir, sha
}

func TestCheckPackages_RefWithDisplayVersion_ManifestMatches_OK(t *testing.T) {
	dir, sha := manifestRepo(t, `{"name":"tool","version":"1.0.0"}`, "")

	wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: sha, Version: "1.0.0"}, realDeps()))

	t.Run("TagNameRef", func(t *testing.T) {
		wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: "v1.0.0", Version: "1.0.0"}, realDeps()))
	})
}

func TestCheckPackages_RefWithDisplayVersion_ManifestMismatch_Fails(t *testing.T) {
	dir, sha := manifestRepo(t, `{"name":"tool","version":"1.1.0"}`, "")

	r := singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: sha, Version: "1.0.0"}, realDeps())

	want := "Version '1.0.0' does not match plugin manifest version '1.1.0' at ref '" + sha + "'"
	if r.Err == nil || r.Err.Error() != want {
		t.Fatalf("Err = %v, want %q", r.Err, want)
	}
	if !r.Reachable || !r.VersionFound || r.RefOK {
		t.Errorf("flags = %+v, want Reachable=true VersionFound=true RefOK=false", r)
	}
}

func TestCheckPackages_RefWithDisplayVersion_FallsBackToApmYML(t *testing.T) {
	dir, sha := manifestRepo(t, "", "name: tool\nversion: 1.0.0\n")
	wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: sha, Version: "1.0.0"}, realDeps()))

	t.Run("Mismatch", func(t *testing.T) {
		dir, sha := manifestRepo(t, "", "name: tool\nversion: 2.0.0\n")
		r := singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: sha, Version: "1.0.0"}, realDeps())
		want := "Version '1.0.0' does not match plugin manifest version '2.0.0' at ref '" + sha + "'"
		if r.Err == nil || r.Err.Error() != want {
			t.Fatalf("Err = %v, want %q", r.Err, want)
		}
	})
}

func TestCheckPackages_RefWithDisplayVersion_NoManifest_SkipsComparison(t *testing.T) {
	dir, sha := manifestRepo(t, "", "")
	wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: sha, Version: "1.0.0"}, realDeps()))
}

func TestCheckPackages_RefWithDisplayVersion_SubdirRespected(t *testing.T) {
	dir := t.TempDir()
	initGitRepoWithTags(t, dir)
	writeManifest(t, dir, "plugins/x/.claude-plugin/plugin.json", `{"version":"1.0.0"}`)
	writeManifest(t, dir, ".claude-plugin/plugin.json", `{"version":"9.9.9"}`)
	sha := addCommit(t, dir, "monorepo")

	wantPass(t, singleResult(t, dir, PackageEntry{Name: "x", Source: dir, Ref: sha, Version: "1.0.0", Subdir: "plugins/x"}, realDeps()))
}

func TestCheckPackages_RefWithRangeVersion_NoManifestFetch(t *testing.T) {
	dir, sha := manifestRepo(t, `{"version":"9.9.9"}`, "")

	wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: sha, Version: "^1.0.0"}, noProbeDeps()))

	t.Run("NoRefNoFetch", func(t *testing.T) {
		wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Version: "1.0.0"}, noProbeDeps()))
	})
}

func TestCheckPackages_ManifestFetch_CloneFailure_Unreachable(t *testing.T) {
	dir, sha := manifestRepo(t, `{"version":"1.0.0"}`, "")

	r := singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: sha, Version: "1.0.0"}, CheckDeps{
		Lister: gitRefLister{}, Prober: panicProber{}, Manifest: errManifestFetcher{errors.New("git fetch: could not read from remote")},
	})

	if r.Err == nil || !strings.Contains(r.Err.Error(), "could not read from remote") {
		t.Fatalf("Err = %v, want the fetch error surfaced", r.Err)
	}
	if r.Reachable || r.RefOK {
		t.Errorf("a manifest fetch transport failure must be Reachable=false, got %+v", r)
	}
}

// semver.TagInfo.Ref carries the full ref name so callers can tell a tag
// from a branch head (SC-B7, SC-B9); fakes that leave it empty are treated
// as tags for backward compatibility.
func TestParseRefsOutput_KeepsFullRefName(t *testing.T) {
	refs := parseRefsOutput(strings.Repeat("a", 40) + "\tHEAD\n" +
		strings.Repeat("b", 40) + "\trefs/heads/main\n" +
		strings.Repeat("c", 40) + "\trefs/tags/v1.0.0\n")
	want := map[string]string{"HEAD": "HEAD", "main": "refs/heads/main", "v1.0.0": "refs/tags/v1.0.0"}
	for _, r := range refs {
		if want[r.Name] != r.Ref {
			t.Errorf("Name %q: Ref = %q, want %q", r.Name, r.Ref, want[r.Name])
		}
	}
	if len(refs) != 3 {
		t.Errorf("len = %d, want 3", len(refs))
	}
	_ = semver.TagInfo{}
}
