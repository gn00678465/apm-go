package authoring

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/apm-go/apm/internal/marketplace/tagpattern"
	"github.com/apm-go/apm/internal/semver"
)

// Edge branches of the SPEC marketplace-check-outdated seams: every test
// here pins a failure mode a caller can actually hit (hostile subdir,
// malformed manifest, oversized manifest, missing scratch root, a remote
// that is not a repository) and the exact text or outcome it produces.

func TestGitManifestVersionFetcher_RefVanished_Errors(t *testing.T) {
	dir, _ := manifestRepo(t, `{"version":"1.0.0"}`, "")

	_, err := gitManifestVersionFetcher{}.FetchManifestVersion(dir, "v9.9.9", "")

	if err == nil || !strings.Contains(err.Error(), "v9.9.9") || !strings.Contains(err.Error(), "plugin manifest") {
		t.Fatalf("err = %v, want a ref-vanished error naming the ref", err)
	}
}

func TestGitManifestVersionFetcher_HostileSubdir_ErrorsInsteadOfEscaping(t *testing.T) {
	dir, sha := manifestRepo(t, `{"version":"1.0.0"}`, "")

	_, err := gitManifestVersionFetcher{}.FetchManifestVersion(dir, sha, "../outside")

	if err == nil || !strings.Contains(err.Error(), "git show") {
		t.Fatalf("err = %v, want git show's refusal surfaced as an error, never a silent skip", err)
	}
}

func TestGitManifestVersionFetcher_MalformedPluginJSON_Errors(t *testing.T) {
	dir, sha := manifestRepo(t, `{"version":`, "")

	_, err := gitManifestVersionFetcher{}.FetchManifestVersion(dir, sha, "")

	if err == nil || !strings.Contains(err.Error(), "parse plugin.json") {
		t.Fatalf("err = %v, want a parse plugin.json error", err)
	}
}

func TestGitManifestVersionFetcher_ApmYML_UnsafeYAML_Errors(t *testing.T) {
	dir, sha := manifestRepo(t, "", "base: &a 1\nversion: *a\n")

	_, err := gitManifestVersionFetcher{}.FetchManifestVersion(dir, sha, "")

	if err == nil || !strings.Contains(err.Error(), "parse apm.yml") {
		t.Fatalf("err = %v, want the safe-subset loader's rejection surfaced as a parse apm.yml error", err)
	}
}

func TestGitManifestVersionFetcher_ApmYML_NotAMapping_NoVersion(t *testing.T) {
	dir, sha := manifestRepo(t, "", "- just\n- a list\n")

	v, err := gitManifestVersionFetcher{}.FetchManifestVersion(dir, sha, "")

	if err != nil || v != "" {
		t.Fatalf("(%q, %v), want no version and no error for a non-mapping apm.yml", v, err)
	}
}

func TestGitManifestVersionFetcher_OversizedManifest_Errors(t *testing.T) {
	big := `{"version":"1.0.0","pad":"` + strings.Repeat("x", manifestReadMaxBytes) + `"}`
	dir, sha := manifestRepo(t, big, "")

	_, err := gitManifestVersionFetcher{}.FetchManifestVersion(dir, sha, "")

	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v, want the size cap to refuse the manifest", err)
	}
}

func TestGitCommitProber_RemoteNotARepository_Unreachable(t *testing.T) {
	notARepo := t.TempDir()

	found, err := gitCommitProber{}.HasCommit(notARepo, strings.Repeat("a", 40))

	if found || err == nil || !strings.Contains(err.Error(), "git fetch") {
		t.Fatalf("(%v, %v), want a git fetch transport error, not a not-found verdict", found, err)
	}
}

func TestGitCommitProber_LocalSourceOutsideRoot_Errors(t *testing.T) {
	_, err := gitCommitProber{}.HasCommit("./../escapes-the-root", strings.Repeat("a", 40))

	if err == nil {
		t.Fatal("a ./ source that escapes the root must error, not probe")
	}
	if strings.Contains(err.Error(), "git fetch") {
		t.Errorf("err = %v; the source must be refused before any git subprocess runs", err)
	}
}

func TestFetchRefIntoScratch_MissingScratchRoot_Errors(t *testing.T) {
	orig := scratchTempRoot
	scratchTempRoot = filepath.Join(t.TempDir(), "no-such-parent")
	t.Cleanup(func() { scratchTempRoot = orig })
	dir, _ := manifestRepo(t, "", "")

	_, cleanup, err := fetchRefIntoScratch(dir, "v1.0.0")
	cleanup()

	if err == nil || !strings.Contains(err.Error(), "create scratch repository") {
		t.Fatalf("err = %v, want a create scratch repository error", err)
	}
}

// An unresponsive remote (TCP accepts, never speaks) must trip the fetch
// deadline itself -- the scratch init succeeds locally, so this is the
// timeout branch a fake git that sleeps on every subcommand can never reach.
func TestFetchRefIntoScratch_UnresponsiveRemote_TimesOut(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()
	orig := listRefsTimeout
	listRefsTimeout = 300 * time.Millisecond
	t.Cleanup(func() { listRefsTimeout = orig })

	start := time.Now()
	_, cleanup, err := fetchRefIntoScratch("https://"+ln.Addr().String()+"/owner/repo.git", strings.Repeat("a", 40))
	cleanup()

	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want a timed-out error from the fetch deadline", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("fetch took %s; the deadline did not cut it short", time.Since(start))
	}
}

func TestGitFailureText_PrefersStderrThenExecError(t *testing.T) {
	if got := gitFailureText("  fatal: boom  \n", errors.New("exit status 1")); got != "fatal: boom" {
		t.Errorf("got %q, want trimmed stderr", got)
	}
	if got := gitFailureText("   ", errors.New("exit status 1")); got != "exit status 1" {
		t.Errorf("got %q, want the exec error when stderr is blank", got)
	}
}

func TestRemoveScratch_RetriesWhileAFileIsHeldOpen(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows refuses to delete an open file; elsewhere the first RemoveAll succeeds")
	}
	dir := filepath.Join(t.TempDir(), "scratch")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "held"))
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(120 * time.Millisecond)
		f.Close()
	}()

	start := time.Now()
	removeScratch(dir)

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("scratch dir still present after retries (stat err %v)", err)
	}
	if time.Since(start) < 100*time.Millisecond {
		t.Errorf("removal returned after %s; the retry loop never waited for the handle", time.Since(start))
	}
}

func TestTruncateRunes(t *testing.T) {
	cases := []struct{ in, want string }{
		{"short", "short"},
		{strings.Repeat("a", 60), strings.Repeat("a", 60)},
		{strings.Repeat("a", 61), strings.Repeat("a", 60)},
		{strings.Repeat("中", 70), strings.Repeat("中", 60)},
	}
	for _, c := range cases {
		if got := truncateRunes(c.in, 60); got != c.want {
			t.Errorf("truncateRunes(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCheckPackages_VersionRange_Unparsable_FailsWithParseError(t *testing.T) {
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")

	r := singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Version: "not a range!!"}, noProbeDeps())

	if r.Err == nil || r.RefOK || !r.Reachable || !r.VersionFound {
		t.Fatalf("result = %+v, want a reachable, version-found row that fails on the unparsable range", r)
	}
}

func TestInfer_WithoutPackageName_SkipsNameLayouts(t *testing.T) {
	// With no package name a "{name}" placeholder compiles to the empty
	// literal, so "{name}-v{version}" would claim the tag "-v1.0.0"; the
	// layouts carrying {name} must be skipped instead.
	odd := []semver.TagInfo{{Name: "-v1.0.0", Ref: "refs/tags/-v1.0.0"}}
	if got := tagpattern.Infer(odd, ""); got != "" {
		t.Errorf("Infer with no package name = %q, want \"\" (a {name} layout cannot be inferred without the name)", got)
	}
	named := []semver.TagInfo{{Name: "tool_v1.2.0", Ref: "refs/tags/tool_v1.2.0"}}
	if got := tagpattern.Infer(named, "tool"); got != "{name}_v{version}" {
		t.Errorf("Infer with the name = %q, want {name}_v{version}", got)
	}
}
