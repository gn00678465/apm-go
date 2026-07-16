package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
)

// TestUninstall_Summary_NamesPackageAndReportsApmYMLPath is the R7
// regression (prd.md/design.md §3): a non-dry-run uninstall used to compress
// its closing summary into two bare counts ("Removed N package(s)" +
// "apm_modules: removed N director(ies)"), while --dry-run's own preview
// already named the matched package(s). The real run must at least match
// its own preview's information: name the removed package(s) and report the
// apm.yml path that was updated.
func TestUninstall_Summary_NamesPackageAndReportsApmYMLPath(t *testing.T) {
	dir := chdirTemp(t)

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	hash := writeUninstallDeployedFile(t, dir, ".claude/rules/foo.md", "foo rule")
	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/foo", Source: "git", DeployedFiles: []string{".claude/rules/foo.md"}, DeployedHashes: map[string]string{".claude/rules/foo.md": hash}},
		},
	}
	writeUninstallLockfileFixture(t, lock)

	stdout := captureUninstallStdout(t, func() {
		if err := runUninstall([]string{"acme/foo"}, uninstallOptions{}); err != nil {
			t.Fatalf("runUninstall: %v", err)
		}
	})

	if !strings.Contains(stdout, "acme/foo") {
		t.Errorf("expected the removed package name in the summary, got:\n%s", stdout)
	}
	wantPath, err := filepath.Abs("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, wantPath) {
		t.Errorf("expected the apm.yml absolute path %q in the summary, got:\n%s", wantPath, stdout)
	}
	if !strings.Contains(stdout, "cleaned 1 integrated file") {
		t.Errorf(`expected "cleaned 1 integrated file(s)" in the summary, got:\n%s`, stdout)
	}
	if strings.Contains(stdout, "kept") {
		t.Errorf("no file was kept in this scenario, summary should not mention kept files, got:\n%s", stdout)
	}
}

// TestUninstall_Summary_CountsReflectActualRemovalOutcome is the codex M1
// regression: the cleaned-file count must come from
// deploy.RemoveDeployedFiles's ACTUAL removed/kept results, not the
// lockfile's DeployedFiles length -- a missing file is silently skipped
// (neither removed nor kept), and a hand-modified file is kept (not
// removed), so a lockfile recording 3 deployed files must NOT be reported as
// "cleaned 3 integrated file(s)" when only 1 was actually deleted.
func TestUninstall_Summary_CountsReflectActualRemovalOutcome(t *testing.T) {
	dir := chdirTemp(t)

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// (1) a normal deployed file -> actually removed.
	okHash := writeUninstallDeployedFile(t, dir, ".claude/rules/ok.md", "ok content")
	// (2) a deployed file that was hand-edited since deploy -> hash mismatch,
	// kept with a warning, never deleted.
	modHash := writeUninstallDeployedFile(t, dir, ".claude/rules/modified.md", "original content")
	modifiedPath := filepath.Join(dir, ".claude", "rules", "modified.md")
	if err := os.WriteFile(modifiedPath, []byte("user edited content"), 0o644); err != nil {
		t.Fatal(err)
	}
	// (3) a deployed file recorded in the lockfile but already missing from
	// disk -> silently skipped, counted in neither removed nor kept.
	missingPath := ".claude/rules/missing.md"

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{
				RepoURL:       "acme/foo",
				Source:        "git",
				DeployedFiles: []string{".claude/rules/ok.md", ".claude/rules/modified.md", missingPath},
				DeployedHashes: map[string]string{
					".claude/rules/ok.md":       okHash,
					".claude/rules/modified.md": modHash,
					missingPath:                 "sha256:0000000000000000000000000000000000000000000000000000000000000000",
				},
			},
		},
	}
	writeUninstallLockfileFixture(t, lock)

	stdout := captureUninstallStdout(t, func() {
		if err := runUninstall([]string{"acme/foo"}, uninstallOptions{}); err != nil {
			t.Fatalf("runUninstall: %v", err)
		}
	})

	if !strings.Contains(stdout, "cleaned 1 integrated file(s) (1 kept -- modified or shared)") {
		t.Errorf("expected the cleanup count to reflect 1 actually-removed + 1 kept (not the lockfile's 3 recorded files), got:\n%s", stdout)
	}

	if _, err := os.Stat(filepath.Join(dir, ".claude", "rules", "ok.md")); !os.IsNotExist(err) {
		t.Errorf("expected ok.md to be removed, stat err=%v", err)
	}
	if got, err := os.ReadFile(modifiedPath); err != nil || string(got) != "user edited content" {
		t.Errorf("expected the hand-modified file to survive untouched, got=%q err=%v", got, err)
	}
}
