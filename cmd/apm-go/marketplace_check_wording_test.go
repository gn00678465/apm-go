package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SPEC marketplace-check-outdated SC-B10/B11 at the command layer: the
// Oracle's verbose lines (check.py:138-139, 158-159), Detail cells, summary
// lines and silent exit 1 (check.py:255-262), plus the SHA probe and
// manifest comparison wired through the CLI's default seams.

func writeCheckFixture(t *testing.T, packages string) {
	t.Helper()
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n" +
		"  packages:\n" + packages
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceCheck_Verbose_PrintsResolvingLines(t *testing.T) {
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	withFixtureRemoteLister(t, repoDir)
	if err := os.MkdirAll(filepath.Join("pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCheckFixture(t,
		"    - name: tool\n      source: owner/repo\n      ref: v1.0.0\n"+
			"    - name: local-a\n      source: ./pkgs/a\n"+
			"    - name: hosted\n      source: https://github.com/owner/hosted.git\n      ref: v1.0.0\n")

	out, err := runMarketplaceCmd(t, "check", "-v")

	if err != nil {
		t.Fatalf("check -v returned error: %v (output: %s)", err, out)
	}
	for _, want := range []string{
		"Resolving tool via default host: owner/repo",
		"Skipping local-a -- local path, no network check",
		"Resolving hosted via github.com: https://github.com/owner/hosted.git",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks verbose line %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "Resolving tool") > strings.Index(out, "REACHABLE") {
		t.Errorf("verbose lines must precede the table:\n%s", out)
	}

	t.Run("QuietWithoutFlag", func(t *testing.T) {
		out, _ := runMarketplaceCmd(t, "check")
		if strings.Contains(out, "Resolving") || strings.Contains(out, "Skipping") {
			t.Errorf("verbose lines printed without -v:\n%s", out)
		}
	})
}

func TestMarketplaceCheck_Offline_DetailWording(t *testing.T) {
	chdirTemp(t)
	writeCheckFixture(t, "    - name: tool\n      source: owner/repo\n      ref: "+strings.Repeat("a", 40)+"\n")

	out, err := runMarketplaceCmd(t, "check", "--offline")

	if !strings.Contains(out, "No cached refs (offline)") {
		t.Errorf("output = %q, want the Oracle's \"No cached refs (offline)\" detail (check.py:206)", out)
	}
	if !strings.Contains(out, "1 entries have issues") {
		t.Errorf("output = %q, want \"1 entries have issues\"", out)
	}
	if got := exitCodeOf(err); got != 1 || !isSilentExit(err) {
		t.Errorf("exit = %d silent=%v, want a silent exit 1", got, isSilentExit(err))
	}
}

func TestMarketplaceCheck_UnreachableDetail_TruncatedTo60(t *testing.T) {
	chdirTemp(t)
	withFixtureRemoteLister(t, filepath.Join(t.TempDir(), "definitely-not-a-repository-with-a-long-name"))
	writeCheckFixture(t, "    - name: tool\n      source: owner/repo\n      ref: v1.0.0\n")

	out, _ := runMarketplaceCmd(t, "check")

	// check.py:220-224 renders a transport failure as exc.summary_text[:60];
	// the fixture lister's error names the long repo path, so the untruncated
	// text can never fit in 60 characters.
	if strings.Contains(out, "definitely-not-a-repository-with-a-long-name") {
		t.Errorf("unreachable detail was not truncated to 60 characters:\n%s", out)
	}
	if !strings.Contains(out, "git ls-remote") {
		t.Errorf("unreachable detail missing entirely:\n%s", out)
	}
}

func TestMarketplaceCheck_ShaPin_VerifiedThroughDefaultProber(t *testing.T) {
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir)
	first := gitCmd(t, repoDir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(repoDir, "more.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repoDir, "add", ".")
	gitCmd(t, repoDir, "commit", "-m", "second")
	withFixtureRemoteLister(t, repoDir)
	writeCheckFixture(t, "    - name: tool\n      source: owner/repo\n      ref: "+first+"\n")

	out, err := runMarketplaceCmd(t, "check")

	if err != nil {
		t.Fatalf("a SHA that is no ref's tip must pass via the probe: %v\n%s", err, out)
	}
	if !strings.Contains(out, "All 1 entries OK") {
		t.Errorf("output = %q, want \"All 1 entries OK\"", out)
	}
}

func TestMarketplaceCheck_ManifestMismatch_ReportedInTable(t *testing.T) {
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir)
	if err := os.MkdirAll(filepath.Join(repoDir, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".claude-plugin", "plugin.json"), []byte(`{"version":"2.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repoDir, "add", ".")
	gitCmd(t, repoDir, "commit", "-m", "manifest")
	gitCmd(t, repoDir, "tag", "v1.0.0")
	withFixtureRemoteLister(t, repoDir)
	writeCheckFixture(t, "    - name: tool\n      source: owner/repo\n      ref: v1.0.0\n      version: 1.0.0\n")

	out, err := runMarketplaceCmd(t, "check")

	want := "Version '1.0.0' does not match plugin manifest version '2.0.0' at ref 'v1.0.0'"
	if !strings.Contains(out, want) {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got := exitCodeOf(err); got != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", got)
	}
}
