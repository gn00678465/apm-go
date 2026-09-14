package main

import (
	"os"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/ux"
)

// SPEC marketplace-check-outdated SC-C9 at the command layer: outdated.py:
// 143-159 prints "<N> package(s) can be updated" then sys.exit(1) with no
// further line; -v prints "    <N> upgradable entries" (four spaces, no
// symbol) via verbose_detail.

func writeOutdatedFixture(t *testing.T, packages string) {
	t.Helper()
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n" +
		"  packages:\n" + packages
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceOutdated_UpgradableExitsSilently(t *testing.T) {
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir, "v1.0.0", "v1.1.0")
	withFixtureRemoteLister(t, repoDir)
	writeOutdatedFixture(t, "    - name: tool\n      source: owner/repo\n      version: \"^1.0.0\"\n")

	out, err := runMarketplaceCmd(t, "outdated")

	if !strings.Contains(out, "1 package(s) can be updated") {
		t.Errorf("output = %q, want \"1 package(s) can be updated\"", out)
	}
	if strings.Contains(out, "outdated:") || strings.Contains(out, "available upgrade") {
		t.Errorf("output = %q, want no apm-go-only trailer after the Oracle's summary", out)
	}
	if got := exitCodeOf(err); got != 1 || !isSilentExit(err) {
		t.Errorf("exit = %d silent=%v, want a silent exit 1", got, isSilentExit(err))
	}

	t.Run("VerboseCount", func(t *testing.T) {
		out, _ := runMarketplaceCmd(t, "outdated", "-v")
		if !strings.Contains(out, "\n    1 upgradable entries\n") {
			t.Errorf("output = %q, want a four-space-indented \"1 upgradable entries\" line", out)
		}
		if strings.Contains(out, ux.SymbolList+"1 upgradable") || strings.Contains(out, "- 1 upgradable") {
			t.Errorf("output = %q, want no list symbol before the verbose count", out)
		}
	})
}

func TestMarketplaceOutdated_ShaPinWithVersion_RowThroughCLI(t *testing.T) {
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir, "v1.0.0")
	sha := gitCmd(t, repoDir, "rev-parse", "HEAD")
	if err := os.WriteFile(repoDir+"/next.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repoDir, "add", ".")
	gitCmd(t, repoDir, "commit", "-m", "next")
	gitCmd(t, repoDir, "tag", "v1.1.0")
	withFixtureRemoteLister(t, repoDir)
	writeOutdatedFixture(t, "    - name: tool\n      source: owner/repo\n      ref: "+sha+"\n      version: 1.0.0\n")

	out, err := runMarketplaceCmd(t, "outdated")

	for _, want := range []string{"v1.0.0", "v1.1.0", ux.SymbolWarn, "1 package(s) can be updated"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want %q", out, want)
		}
	}
	if strings.Contains(out, "Pinned to ref") || strings.Contains(out, "pinned to ref") {
		t.Errorf("a SHA pin with a display version must not be skipped:\n%s", out)
	}
	if got := exitCodeOf(err); got != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", got)
	}
}

func TestMarketplaceOutdated_NoteWording_MatchesOracle(t *testing.T) {
	chdirTemp(t)
	writeOutdatedFixture(t, "    - name: tool\n      source: owner/repo\n      ref: v1.0.0\n")

	out, err := runMarketplaceCmd(t, "outdated")

	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Pinned to ref; skipped") {
		t.Errorf("output = %q, want the Oracle's \"Pinned to ref; skipped\" note", out)
	}
}
