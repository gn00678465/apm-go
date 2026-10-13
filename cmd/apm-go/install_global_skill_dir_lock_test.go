package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// Issue #50, item 2 of the owner's ruling: `install --global` lists each
// deployed skill's directory in the lock with no hash, as the Oracle does
// (measured on Oracle v0.33.0 with a local package: its user-scope lock has
// `.claude/skills/<name>` in deployed_files and a deployments row with
// `content_hash: null`).

func globalSkillDirLock(t *testing.T, home string) *lockfile.Lockfile {
	t.Helper()
	node, err := yamlcore.SafeLoad([]byte(globalStaleRead(t, filepath.Join(home, ".apm", "apm.lock.yaml"))))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lockfile.ParseLockfile(node)
	if err != nil {
		t.Fatal(err)
	}
	return lock
}

func TestInstallGlobal_LockListsSkillDirWithNoHash(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{
		".apm/skills/demo/SKILL.md":  staleCleanupSkillMD,
		".apm/skills/demo/extra.txt": "extra\n",
	})
	globalStaleInstall(t, "claude", src)

	dep := globalSkillDirLock(t, home).Dependencies[0]
	want := []string{".claude/skills/demo", ".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra.txt"}
	if strings.Join(dep.DeployedFiles, "\n") != strings.Join(want, "\n") {
		t.Errorf("deployed_files = %v, want %v", dep.DeployedFiles, want)
	}
	if h, ok := dep.DeployedHashes[".claude/skills/demo"]; ok {
		t.Errorf("the skill directory has a hash %q, want none", h)
	}
	raw := globalStaleRead(t, filepath.Join(home, ".apm", "apm.lock.yaml"))
	if !strings.Contains(raw, "  value: .claude/skills/demo\n  runtime: null\n  scope: project\n") ||
		!strings.Contains(raw, "  content_hash: null\n") {
		t.Errorf("no deployments row with a null hash for the skill directory:\n%s", raw)
	}
	if !globalStaleIsSymlink(t, filepath.Join(home, ".claude", "skills", "demo")) {
		t.Error(".claude/skills/demo is not the deploy's symlink")
	}
}

func TestInstallGlobal_SkillDirInLock_SecondInstallChangesNothing(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{".apm/skills/demo/SKILL.md": staleCleanupSkillMD})
	globalStaleInstall(t, "claude", src)
	lockPath := filepath.Join(home, ".apm", "apm.lock.yaml")
	before := globalStaleRead(t, lockPath)

	out := globalStaleInstall(t, "claude")

	if got := globalStaleRead(t, lockPath); got != before {
		t.Errorf("the lock changed on a second install:\n%s", got)
	}
	if strings.Contains(out, "keeping") || strings.Contains(out, "Cleaned") {
		t.Errorf("want no warning and no cleaned line:\n%s", out)
	}
}

// A lock written before this change has no entry for the skill directory.
func TestInstallGlobal_SkillDirAddedToOlderLock(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{".apm/skills/demo/SKILL.md": staleCleanupSkillMD})
	globalStaleInstall(t, "claude", src)
	lockPath := filepath.Join(home, ".apm", "apm.lock.yaml")
	current := globalStaleRead(t, lockPath)
	older := strings.Replace(current, "  - .claude/skills/demo\n", "", 1)
	if i := strings.Index(older, "- kind: project-relative\n  target: claude\n  value: .claude/skills/demo\n"); i >= 0 {
		j := strings.Index(older[i+1:], "- kind: ")
		if j < 0 {
			older = older[:i]
		} else {
			older = older[:i] + older[i+1+j:]
		}
	}
	if older == current || strings.Contains(older, "skills/demo\n") {
		t.Fatalf("precondition: could not build the older lock:\n%s", older)
	}
	if err := os.WriteFile(lockPath, []byte(older), 0o600); err != nil {
		t.Fatal(err)
	}

	out := globalStaleInstall(t, "claude")

	if got := globalStaleRead(t, lockPath); got != current {
		t.Errorf("lock after the install = \n%s\nwant\n%s", got, current)
	}
	if strings.Contains(out, "keeping") || strings.Contains(out, "Cleaned") {
		t.Errorf("want no warning and no cleaned line:\n%s", out)
	}
}

func TestInstallGlobal_Frozen_PassesWithSkillDirInLock(t *testing.T) {
	_, src := globalStaleScope(t, map[string]string{".apm/skills/demo/SKILL.md": staleCleanupSkillMD})
	globalStaleInstall(t, "claude", src)

	out, err := frozenRootGlobalInstall(t, true)

	if err != nil {
		t.Fatalf("install --global --frozen: %v\n%s", err, out)
	}
}

func TestUninstallGlobal_RemovesSkillSymlinkListedInLock(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{
		".apm/agents/keep.md":       globalStaleKeepAgent,
		".apm/skills/demo/SKILL.md": staleCleanupSkillMD,
	})
	globalStaleInstall(t, "claude", src)

	out := captureUninstallStdout(t, func() {
		if err := runUninstall([]string{src}, uninstallOptions{Global: true}); err != nil {
			t.Fatalf("uninstall --global: %v", err)
		}
	})

	if _, err := os.Lstat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Errorf(".claude still present after uninstall (Lstat err = %v)", err)
	}
	if strings.Contains(out, "keeping") || strings.Contains(out, "Kept") {
		t.Errorf("want no warning about a kept path:\n%s", out)
	}
	// The agent, the skill directory and its file: the Oracle counts the
	// directory too (measured on v0.33.0, `uninstall --global`).
	if !strings.Contains(out, "Cleaned 3 stale files from ") {
		t.Errorf("missing the cleaned line with 3 paths:\n%s", out)
	}
	if _, err := os.Lstat(filepath.Join(home, ".apm", "apm.lock.yaml")); !os.IsNotExist(err) {
		t.Errorf("the lock still exists after the only package is uninstalled (Lstat err = %v)", err)
	}
}

// The user pointed the skill symlink at a directory of their own and the
// skill left the package. The entry for the directory follows its files out
// of the lock, so the warning is printed once, not on every install.
func TestInstallGlobal_SkillDirEntryLeavesLockWhenUserTookTheSymlinkOver(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{
		".apm/agents/keep.md":       globalStaleKeepAgent,
		".apm/skills/demo/SKILL.md": staleCleanupSkillMD,
	})
	globalStaleInstall(t, "claude", src)
	own := filepath.Join(t.TempDir(), "own")
	writeGlobalStaleFile(t, filepath.Join(own, "README.md"), "mine\n")
	link := filepath.Join(home, ".claude", "skills", "demo")
	globalStaleRelink(t, own, link)
	if err := os.RemoveAll(filepath.Join(src, ".apm", "skills", "demo")); err != nil {
		t.Fatal(err)
	}

	first := globalStaleInstall(t, "claude")

	if strings.Count(first, `keeping ".claude/skills/demo"`) != 1 {
		t.Errorf("first install: want one warning for the kept symlink:\n%s", first)
	}
	assertGlobalStaleLock(t, home, false, ".claude/skills/demo")
	for i := 0; i < 2; i++ {
		again := globalStaleInstall(t, "claude")
		if strings.Contains(again, "keeping") {
			t.Errorf("later install %d: want no warning:\n%s", i+1, again)
		}
		assertGlobalStaleLock(t, home, false, ".claude/skills/demo")
	}
	assertGlobalStaleLinkTarget(t, link, own)
	if got := globalStaleRead(t, filepath.Join(own, "README.md")); got != "mine\n" {
		t.Errorf("the user's file = %q", got)
	}
}

// A real directory the deploy left to the user gets no lock entry, and gets
// its entries back when the user moves it away.
func TestInstallGlobal_KeptRealSkillDirHasNoDirEntryInLock(t *testing.T) {
	home, _, skill, _ := keptSkillInstalled(t, keptSkillUserFiles())
	assertGlobalStaleLock(t, home, false, ".claude/skills/demo")

	if err := os.RemoveAll(skill); err != nil {
		t.Fatal(err)
	}
	globalStaleInstall(t, "claude")

	assertGlobalStaleLock(t, home, true, "  - .claude/skills/demo\n")
	assertGlobalStaleLock(t, home, true, ".claude/skills/demo/SKILL.md")
}
