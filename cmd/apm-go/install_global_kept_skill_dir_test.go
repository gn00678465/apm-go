package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Issue #43: `install --global` deploys a skill as a directory symlink. A
// real directory the user put in its place is deleted only when the package
// has every file in it with the same bytes.

const (
	keptSkillDetail  = "detail\n"
	keptSkillWarning = `keeping ".claude/skills/demo": the directory holds "user-notes.txt", which this package does not have with the same content; skill "demo" not deployed`
)

func keptSkillPackage() map[string]string {
	return map[string]string{
		".apm/agents/keep.md":            globalStaleKeepAgent,
		".apm/skills/demo/SKILL.md":      staleCleanupSkillMD,
		".apm/skills/demo/refs/notes.md": keptSkillDetail,
	}
}

// keptSkillReplaceWithDir replaces whatever is at full with a real directory
// holding files (slash-separated relative path -> content).
func keptSkillReplaceWithDir(t *testing.T, full string, files map[string]string) {
	t.Helper()
	if err := os.Remove(full); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(full, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		writeGlobalStaleFile(t, filepath.Join(full, filepath.FromSlash(rel)), content)
	}
}

// assertKeptSkillDir fails unless full is a real directory holding exactly
// want.
func assertKeptSkillDir(t *testing.T, full string, want map[string]string) {
	t.Helper()
	info, err := os.Lstat(full)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a real directory (mode %v)", full, info.Mode())
	}
	got := map[string]string{}
	err = filepath.WalkDir(full, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(full, p)
		if err != nil {
			return err
		}
		got[filepath.ToSlash(rel)] = globalStaleRead(t, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("files in %s = %v, want %v", full, got, want)
	}
}

// keptSkillInstalled installs keptSkillPackage, replaces the demo skill's
// symlink with a real directory holding userFiles, and installs again.
func keptSkillInstalled(t *testing.T, userFiles map[string]string) (home, src, skill, out string) {
	t.Helper()
	home, src = globalStaleScope(t, keptSkillPackage())
	globalStaleInstall(t, "claude", src)
	skill = filepath.Join(home, ".claude", "skills", "demo")
	if !globalStaleIsSymlink(t, skill) {
		t.Fatal("precondition: .claude/skills/demo is not a symlink after the first install")
	}
	assertGlobalStaleLock(t, home, true, ".claude/skills/demo/SKILL.md")
	keptSkillReplaceWithDir(t, skill, userFiles)
	return home, src, skill, globalStaleInstall(t, "claude")
}

func keptSkillUserFiles() map[string]string {
	return map[string]string{"SKILL.md": staleCleanupSkillMD, "user-notes.txt": "mine\n"}
}

func TestInstallGlobal_RealSkillDirWithSourceContentBecomesSymlinkAgain(t *testing.T) {
	_, _, skill, out := keptSkillInstalled(t, map[string]string{
		"SKILL.md":      staleCleanupSkillMD,
		"refs/notes.md": keptSkillDetail,
	})

	if !globalStaleIsSymlink(t, skill) {
		t.Error(".claude/skills/demo is not a symlink again")
	}
	if got := globalStaleRead(t, filepath.Join(skill, "refs", "notes.md")); got != keptSkillDetail {
		t.Errorf("refs/notes.md through the skill symlink = %q, want %q", got, keptSkillDetail)
	}
	if strings.Contains(out, "keeping") || strings.Contains(out, "Cleaned") {
		t.Errorf("want no warning and no cleaned line:\n%s", out)
	}
	if !strings.Contains(out, "Already up to date") {
		t.Errorf("want Already up to date:\n%s", out)
	}
}

func TestInstallGlobal_KeepsRealSkillDirWithExtraFile(t *testing.T) {
	home, _, skill, out := keptSkillInstalled(t, keptSkillUserFiles())

	assertKeptSkillDir(t, skill, keptSkillUserFiles())
	if strings.Count(out, keptSkillWarning) != 1 {
		t.Errorf("want the warning once:\n%s", out)
	}
	if strings.Contains(out, "Cleaned") {
		t.Errorf("nothing was deleted, want no cleaned line:\n%s", out)
	}
	assertGlobalStaleLock(t, home, false, ".claude/skills/demo")
	assertGlobalStaleLock(t, home, true, ".claude/agents/keep.md")
	if !globalStaleIsSymlink(t, filepath.Join(home, ".claude", "agents", "keep.md")) {
		t.Error(".claude/agents/keep.md is not a symlink")
	}

	out = globalStaleInstall(t, "claude")

	assertKeptSkillDir(t, skill, keptSkillUserFiles())
	if !strings.Contains(out, "Already up to date") || strings.Count(out, keptSkillWarning) != 1 {
		t.Errorf("want Already up to date and the warning once:\n%s", out)
	}
	assertGlobalStaleLock(t, home, false, ".claude/skills/demo")
}

func TestInstallGlobal_KeepsRealSkillDirWithEditedFile(t *testing.T) {
	userFiles := map[string]string{"SKILL.md": "MY OWN SKILL\n", "refs/notes.md": keptSkillDetail}
	home, _, skill, out := keptSkillInstalled(t, userFiles)

	assertKeptSkillDir(t, skill, userFiles)
	want := `keeping ".claude/skills/demo": the directory holds "SKILL.md", which this package does not have with the same content; skill "demo" not deployed`
	if strings.Count(out, want) != 1 {
		t.Errorf("want the warning naming SKILL.md once:\n%s", out)
	}
	if strings.Contains(out, "Cleaned") {
		t.Errorf("nothing was deleted, want no cleaned line:\n%s", out)
	}
	assertGlobalStaleLock(t, home, false, ".claude/skills/demo")
}

func TestInstallGlobal_KeptSkillDirDoesNotStopCleanupOfAnotherSkill(t *testing.T) {
	files := keptSkillPackage()
	files[".apm/skills/other/SKILL.md"] = "---\nname: other\ndescription: other skill\n---\n\n# Other\n"
	home, src := globalStaleScope(t, files)
	globalStaleInstall(t, "claude", src)
	skill := filepath.Join(home, ".claude", "skills", "demo")
	other := filepath.Join(home, ".claude", "skills", "other")
	if !globalStaleIsSymlink(t, other) {
		t.Fatal("precondition: .claude/skills/other is not a symlink after the first install")
	}
	keptSkillReplaceWithDir(t, skill, keptSkillUserFiles())
	if err := os.Remove(filepath.Join(src, ".apm", "skills", "other", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(src, ".apm", "skills", "other")); err != nil {
		t.Fatal(err)
	}

	out := globalStaleInstall(t, "claude")

	assertKeptSkillDir(t, skill, keptSkillUserFiles())
	if _, err := os.Lstat(other); !os.IsNotExist(err) {
		t.Errorf(".claude/skills/other still present (Lstat err = %v)", err)
	}
	if !strings.Contains(out, "Cleaned 1 stale file from _local/dep-") || strings.Count(out, ".claude/skills/other\n") != 1 {
		t.Errorf("want one cleaned line listing .claude/skills/other:\n%s", out)
	}
	if strings.Count(out, keptSkillWarning) != 1 {
		t.Errorf("want the warning once:\n%s", out)
	}
	assertGlobalStaleLock(t, home, false, ".claude/skills/demo")
	assertGlobalStaleLock(t, home, false, ".claude/skills/other")
}

func TestInstallGlobal_KeptSkillDirSurvivesRemovalOfTheSkillFromTheSource(t *testing.T) {
	home, src, skill, _ := keptSkillInstalled(t, keptSkillUserFiles())
	for _, rel := range []string{"refs/notes.md", "refs", "SKILL.md", ""} {
		if err := os.Remove(filepath.Join(src, ".apm", "skills", "demo", filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}

	out := globalStaleInstall(t, "claude")

	assertKeptSkillDir(t, skill, keptSkillUserFiles())
	if strings.Contains(out, "Cleaned") || strings.Contains(out, "keeping") {
		t.Errorf("want no cleaned line and no warning:\n%s", out)
	}
	assertGlobalStaleLock(t, home, false, ".claude/skills/demo")
}

func TestUninstallGlobal_KeptSkillDirSurvives(t *testing.T) {
	home, src, skill, _ := keptSkillInstalled(t, keptSkillUserFiles())

	captureUninstallStdout(t, func() {
		if err := runUninstall([]string{src}, uninstallOptions{Global: true}); err != nil {
			t.Fatalf("uninstall --global: %v", err)
		}
	})

	assertKeptSkillDir(t, skill, keptSkillUserFiles())
	if _, err := os.Lstat(filepath.Join(home, ".claude", "agents", "keep.md")); !os.IsNotExist(err) {
		t.Errorf("uninstall did not run: .claude/agents/keep.md still present (Lstat err = %v)", err)
	}
}

func TestInstallGlobal_KeepsSharedRealSkillDirOnceForThreeTargets(t *testing.T) {
	home, src := globalStaleScope(t, keptSkillPackage())
	globalStaleInstall(t, "codex,copilot,opencode", src)
	skill := filepath.Join(home, ".agents", "skills", "demo")
	if !globalStaleIsSymlink(t, skill) {
		t.Fatal("precondition: .agents/skills/demo is not a symlink after the first install")
	}
	keptSkillReplaceWithDir(t, skill, keptSkillUserFiles())

	out := globalStaleInstall(t, "codex,copilot,opencode")

	assertKeptSkillDir(t, skill, keptSkillUserFiles())
	want := `keeping ".agents/skills/demo": the directory holds "user-notes.txt", which this package does not have with the same content; skill "demo" not deployed`
	if strings.Count(out, want) != 1 || strings.Count(out, "keeping") != 1 {
		t.Errorf("want exactly one warning:\n%s", out)
	}
	assertGlobalStaleLock(t, home, false, ".agents/skills/demo")
}

func TestInstallGlobal_ReplacesRepointedSkillSymlinkAndRegularFile(t *testing.T) {
	tests := []struct {
		name    string
		replace func(t *testing.T, skill string)
	}{
		{"symlink the user pointed elsewhere", func(t *testing.T, skill string) {
			custom := t.TempDir()
			writeGlobalStaleFile(t, filepath.Join(custom, "SKILL.md"), "the user's own skill\n")
			globalStaleRelink(t, custom, skill)
		}},
		{"regular file", func(t *testing.T, skill string) {
			if err := os.Remove(skill); err != nil {
				t.Fatal(err)
			}
			writeGlobalStaleFile(t, skill, "MY OWN NOTES\n")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home, src := globalStaleScope(t, keptSkillPackage())
			globalStaleInstall(t, "claude", src)
			skill := filepath.Join(home, ".claude", "skills", "demo")
			tc.replace(t, skill)

			out := globalStaleInstall(t, "claude")

			if !globalStaleIsSymlink(t, skill) {
				t.Fatal(".claude/skills/demo is not a symlink again")
			}
			if got := globalStaleRead(t, filepath.Join(skill, "SKILL.md")); got != staleCleanupSkillMD {
				t.Errorf("SKILL.md through the skill symlink = %q, want %q", got, staleCleanupSkillMD)
			}
			if strings.Contains(out, "keeping") || !strings.Contains(out, "Already up to date") {
				t.Errorf("want Already up to date and no warning:\n%s", out)
			}
		})
	}
}

func TestInstallGlobal_KeptSkillDirMovedAwayIsDeployedAgain(t *testing.T) {
	home, _, skill, _ := keptSkillInstalled(t, keptSkillUserFiles())
	assertGlobalStaleLock(t, home, false, ".claude/skills/demo")
	if err := os.Rename(skill, filepath.Join(t.TempDir(), "demo-mine")); err != nil {
		t.Fatal(err)
	}

	out := globalStaleInstall(t, "claude")

	if !globalStaleIsSymlink(t, skill) {
		t.Fatal(".claude/skills/demo is not a symlink again")
	}
	if strings.Contains(out, "keeping") {
		t.Errorf("want no warning:\n%s", out)
	}
	assertGlobalStaleLock(t, home, true, ".claude/skills/demo/SKILL.md")
	assertGlobalStaleLock(t, home, true, ".claude/skills/demo/refs/notes.md")
}

func TestInstallGlobal_KeepsRealSkillDirOfLocalContent(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{".apm/agents/keep.md": globalStaleKeepAgent})
	writeGlobalStaleFile(t, filepath.Join(home, ".apm", ".apm", "skills", "demo", "SKILL.md"), staleCleanupSkillMD)
	globalStaleInstall(t, "claude", src)
	skill := filepath.Join(home, ".claude", "skills", "demo")
	if !globalStaleIsSymlink(t, skill) {
		t.Fatal("precondition: .claude/skills/demo is not a symlink after the first install")
	}
	assertGlobalStaleLock(t, home, true, ".claude/skills/demo/SKILL.md")
	keptSkillReplaceWithDir(t, skill, keptSkillUserFiles())

	out := globalStaleInstall(t, "claude")

	assertKeptSkillDir(t, skill, keptSkillUserFiles())
	if strings.Count(out, keptSkillWarning) != 1 || strings.Contains(out, "Cleaned") {
		t.Errorf("want the warning once and no cleaned line:\n%s", out)
	}
	assertGlobalStaleLock(t, home, false, ".claude/skills/demo")
}
