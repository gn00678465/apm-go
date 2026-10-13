package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #50 follow-up (owner ruling of 2026-10-13): a skill directory the
// user replaced by a symlink to a directory of their own is not the deploy's
// any more. Nothing is deleted through it, also a file with the same bytes
// as the package's, and the symlink stays.

// userSkillDir makes a directory with a copy of the deployed SKILL.md and a
// file of the user's own, and puts a symlink to it at link.
func userSkillDir(t *testing.T, link, skillMD string) (own string) {
	t.Helper()
	own = filepath.Join(t.TempDir(), "own")
	writeGlobalStaleFile(t, filepath.Join(own, "SKILL.md"), skillMD)
	writeGlobalStaleFile(t, filepath.Join(own, "notes.md"), "mine\n")
	if err := os.RemoveAll(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(own, link); err != nil {
		t.Fatal(err)
	}
	return own
}

func assertUserSkillDirIntact(t *testing.T, link, own, skillMD string) {
	t.Helper()
	assertGlobalStaleLinkTarget(t, link, own)
	if got := globalStaleRead(t, filepath.Join(own, "SKILL.md")); got != skillMD {
		t.Errorf("the user's SKILL.md = %q, want %q", got, skillMD)
	}
	if got := globalStaleRead(t, filepath.Join(own, "notes.md")); got != "mine\n" {
		t.Errorf("the user's notes.md = %q", got)
	}
}

const localSkillMD = "---\nname: sk\ndescription: d\n---\nskill\n"

func TestUninstall_KeepsFilesBehindUserSkillSymlink(t *testing.T) {
	localSkillProject(t)
	mustRunCLI(t, "install")
	link := filepath.Join(".claude", "skills", "sk")
	own := userSkillDir(t, link, localSkillMD)

	out := mustRunCLI(t, "uninstall", "./dep")

	assertUserSkillDirIntact(t, link, own, localSkillMD)
	if strings.Count(out, `keeping ".claude/skills/sk": it is a symlink`) != 1 {
		t.Errorf("want one warning that names the symlink:\n%s", out)
	}
	if _, err := os.Lstat(filepath.Join(".claude", "rules", "dep.md")); !os.IsNotExist(err) {
		t.Errorf("uninstall did not run: .claude/rules/dep.md still present (Lstat err = %v)", err)
	}
}

func TestInstall_StaleCleanup_KeepsFilesBehindUserSkillSymlink(t *testing.T) {
	localSkillProject(t)
	mustRunCLI(t, "install")
	link := filepath.Join(".claude", "skills", "sk")
	own := userSkillDir(t, link, localSkillMD)
	if err := os.RemoveAll(filepath.Join("dep", ".apm", "skills")); err != nil {
		t.Fatal(err)
	}

	out := mustRunCLI(t, "install")

	assertUserSkillDirIntact(t, link, own, localSkillMD)
	if strings.Count(out, `keeping ".claude/skills/sk": it is a symlink`) != 1 {
		t.Errorf("want one warning that names the symlink:\n%s", out)
	}
	// The user's entry leaves the lock: the warning is not repeated, and a
	// frozen install does not check a file that is the user's.
	if lock := lockOnDisk(t); strings.Contains(lock, ".claude/skills/sk") {
		t.Errorf("the lock still records the user's entry:\n%s", lock)
	}
	if again := mustRunCLI(t, "install"); strings.Contains(again, "keeping") {
		t.Errorf("second install: want no warning:\n%s", again)
	}
	writeGlobalStaleFile(t, filepath.Join(own, "SKILL.md"), "edited by the user\n")
	if _, _, exitCode := runCompileCLI(t, "install", "--frozen"); exitCode != 0 {
		t.Errorf("install --frozen fails on the user's edited file: exit %d", exitCode)
	}
}

// The package still has the skill: the place is the deploy's. The user's
// symlink is replaced by the skill's own directory and nothing is written
// into the directory it led to.
func TestInstall_ReplacesUserSkillSymlinkWithoutWritingThroughIt(t *testing.T) {
	localSkillProject(t)
	mustRunCLI(t, "install")
	link := filepath.Join(".claude", "skills", "sk")
	own := filepath.Join(t.TempDir(), "own")
	writeGlobalStaleFile(t, filepath.Join(own, "SKILL.md"), "MY OWN SKILL\n")
	if err := os.RemoveAll(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(own, link); err != nil {
		t.Fatal(err)
	}

	mustRunCLI(t, "install")

	if globalStaleIsSymlink(t, link) {
		t.Error(".claude/skills/sk is still a symlink")
	}
	if got := globalStaleRead(t, filepath.Join(link, "SKILL.md")); got != localSkillMD {
		t.Errorf("deployed SKILL.md = %q, want the package's", got)
	}
	if got := globalStaleRead(t, filepath.Join(own, "SKILL.md")); got != "MY OWN SKILL\n" {
		t.Errorf("the user's SKILL.md = %q, want it untouched", got)
	}
	if _, err := os.Lstat(filepath.Join(own, "extra.txt")); !os.IsNotExist(err) {
		t.Errorf("the package's extra.txt was written into the user's directory (Lstat err = %v)", err)
	}
}

// The user's directory has one of the two files the lock records.
func TestUninstall_KeptCountsOnlyFilesThatExistBehindUserSkillSymlink(t *testing.T) {
	localSkillProject(t)
	mustRunCLI(t, "install")
	userSkillDir(t, filepath.Join(".claude", "skills", "sk"), localSkillMD)

	out := mustRunCLI(t, "uninstall", "./dep")

	if !strings.Contains(out, "Kept 1 deployed file(s)") {
		t.Errorf("want one kept file:\n%s", out)
	}
}

func TestInstallGlobal_UserRepointedSkillSymlinkLeavesLockAfterOneWarning(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{
		".apm/agents/keep.md":       globalStaleKeepAgent,
		".apm/skills/demo/SKILL.md": staleCleanupSkillMD,
	})
	globalStaleInstall(t, "claude", src)
	link := filepath.Join(home, ".claude", "skills", "demo")
	own := userSkillDir(t, link, staleCleanupSkillMD)
	if err := os.RemoveAll(filepath.Join(src, ".apm", "skills", "demo")); err != nil {
		t.Fatal(err)
	}

	first := globalStaleInstall(t, "claude")

	if strings.Count(first, `keeping ".claude/skills/demo"`) != 1 {
		t.Errorf("first install: want one warning:\n%s", first)
	}
	assertGlobalStaleLock(t, home, false, ".claude/skills/demo")
	if again := globalStaleInstall(t, "claude"); strings.Contains(again, "keeping") {
		t.Errorf("second install: want no warning:\n%s", again)
	}
	assertUserSkillDirIntact(t, link, own, staleCleanupSkillMD)
}

func TestUninstallGlobal_KeepsFilesBehindRepointedSkillSymlink(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{
		".apm/agents/keep.md":       globalStaleKeepAgent,
		".apm/skills/demo/SKILL.md": staleCleanupSkillMD,
	})
	globalStaleInstall(t, "claude", src)
	link := filepath.Join(home, ".claude", "skills", "demo")
	own := userSkillDir(t, link, staleCleanupSkillMD)

	out := captureUninstallStdout(t, func() {
		if err := runUninstall([]string{src}, uninstallOptions{Global: true}); err != nil {
			t.Fatalf("uninstall --global: %v", err)
		}
	})

	assertUserSkillDirIntact(t, link, own, staleCleanupSkillMD)
	if strings.Count(out, `keeping ".claude/skills/demo"`) != 1 {
		t.Errorf("want one warning that names the symlink:\n%s", out)
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "agents", "keep.md")); !os.IsNotExist(err) {
		t.Errorf("uninstall did not run: .claude/agents/keep.md still present (Lstat err = %v)", err)
	}
	if !strings.Contains(out, "Cleaned 1 stale file from ") {
		t.Errorf("want only the agent counted as cleaned:\n%s", out)
	}
}

// The agent symlink is one symlink per file; pointed at a file of the user's
// with the same bytes, it is the user's entry.
func TestUninstallGlobal_KeepsRepointedAgentSymlink(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{".apm/agents/keep.md": globalStaleKeepAgent})
	globalStaleInstall(t, "claude", src)
	link := filepath.Join(home, ".claude", "agents", "keep.md")
	own := filepath.Join(t.TempDir(), "keep.md")
	writeGlobalStaleFile(t, own, globalStaleKeepAgent)
	globalStaleRelink(t, own, link)

	out := captureUninstallStdout(t, func() {
		if err := runUninstall([]string{src}, uninstallOptions{Global: true}); err != nil {
			t.Fatalf("uninstall --global: %v", err)
		}
	})

	assertGlobalStaleLinkTarget(t, link, own)
	if got := globalStaleRead(t, own); got != globalStaleKeepAgent {
		t.Errorf("the user's file = %q", got)
	}
	if strings.Count(out, `keeping ".claude/agents/keep.md"`) != 1 {
		t.Errorf("want one warning that names the symlink:\n%s", out)
	}
}

// The package's own files are not deleted through the deploy's symlink: the
// symlink goes, and the module directory is removed as a whole after it.
func TestUninstallGlobal_RemovesDeploySymlinkNotThroughIt(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{".apm/skills/demo/SKILL.md": staleCleanupSkillMD})
	globalStaleInstall(t, "claude", src)
	link := filepath.Join(home, ".claude", "skills", "demo")
	moduleSkill, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	// Outside apm_modules the removal of the module directory cannot hide a
	// delete made through the symlink.
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(moduleSkill, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, moduleSkill); err != nil {
		t.Fatal(err)
	}

	captureUninstallStdout(t, func() {
		if err := runUninstall([]string{src}, uninstallOptions{Global: true}); err != nil {
			t.Fatalf("uninstall --global: %v", err)
		}
	})

	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("the deploy's symlink is still there (Lstat err = %v)", err)
	}
	if got := globalStaleRead(t, filepath.Join(moved, "SKILL.md")); got != staleCleanupSkillMD {
		t.Errorf("the file behind the symlink = %q", got)
	}
}

// A run that does not govern the target keeps the record: `install -t codex`
// neither warns about nor forgets what is under .claude.
func TestInstallGlobal_UserRepointedSkillSymlinkOfAnotherTargetStaysInLock(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{
		".apm/agents/keep.md":       globalStaleKeepAgent,
		".apm/skills/demo/SKILL.md": staleCleanupSkillMD,
	})
	globalStaleInstall(t, "claude", src)
	userSkillDir(t, filepath.Join(home, ".claude", "skills", "demo"), staleCleanupSkillMD)
	if err := os.RemoveAll(filepath.Join(src, ".apm", "skills", "demo")); err != nil {
		t.Fatal(err)
	}

	out := globalStaleInstall(t, "codex")

	if strings.Contains(out, "keeping") {
		t.Errorf("want no warning from a run that does not govern .claude:\n%s", out)
	}
	assertGlobalStaleLock(t, home, true, ".claude/skills/demo/SKILL.md")
}

func TestInstallGlobal_UserRepointedAgentSymlinkLeavesLockAfterOneWarning(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{
		".apm/agents/keep.md": globalStaleKeepAgent,
		".apm/agents/gone.md": globalStaleGoneAgent,
	})
	globalStaleInstall(t, "claude", src)
	link := filepath.Join(home, ".claude", "agents", "gone.md")
	own := filepath.Join(t.TempDir(), "gone.md")
	writeGlobalStaleFile(t, own, globalStaleGoneAgent)
	globalStaleRelink(t, own, link)
	if err := os.Remove(filepath.Join(src, ".apm", "agents", "gone.md")); err != nil {
		t.Fatal(err)
	}

	first := globalStaleInstall(t, "claude")

	if strings.Count(first, `keeping ".claude/agents/gone.md"`) != 1 {
		t.Errorf("first install: want one warning:\n%s", first)
	}
	assertGlobalStaleLock(t, home, false, ".claude/agents/gone.md")
	if again := globalStaleInstall(t, "claude"); strings.Contains(again, "keeping") {
		t.Errorf("second install: want no warning:\n%s", again)
	}
	assertGlobalStaleLinkTarget(t, link, own)
}

// codex, opencode and copilot share .agents/skills/<name>: the first deploy
// replaces the user's symlink, the others find the directory.
func TestInstall_ReplacesUserSkillSymlinkSharedByThreeTargets(t *testing.T) {
	localSkillProject(t)
	mustRunCLI(t, "install", "--target", "codex,opencode,copilot")
	link := filepath.Join(".agents", "skills", "sk")
	before := lockOnDisk(t)
	own := filepath.Join(t.TempDir(), "own")
	writeGlobalStaleFile(t, filepath.Join(own, "SKILL.md"), "MY OWN SKILL\n")
	if err := os.RemoveAll(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(own, link); err != nil {
		t.Fatal(err)
	}

	mustRunCLI(t, "install", "--target", "codex,opencode,copilot")

	if globalStaleIsSymlink(t, link) {
		t.Error(".agents/skills/sk is still a symlink")
	}
	if got := globalStaleRead(t, filepath.Join(own, "SKILL.md")); got != "MY OWN SKILL\n" {
		t.Errorf("the user's SKILL.md = %q, want it untouched", got)
	}
	if got := lockOnDisk(t); got != before {
		t.Errorf("the lock changed:\n%s\nwant\n%s", got, before)
	}
}
