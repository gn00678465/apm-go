package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
)

// Issue #32: `install --global` deploys symlinks from the deploy root into
// the global project directory. A source that is gone must lose its deployed
// entry, and the delete must never go through a symlink.

const (
	globalStaleKeepAgent = "---\nname: keep\ndescription: keep\n---\nkeep\n"
	globalStaleGoneAgent = "---\nname: gone\ndescription: gone\n---\ngone\n"
)

// globalStaleScope points the global project directory at <home>/.apm and the
// deploy root at <home>, both in a temp directory, and writes a local package
// at the returned src path (slash-separated relative path -> content).
func globalStaleScope(t *testing.T, files map[string]string) (home, src string) {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	home = t.TempDir()
	oldDir, oldDeploy := globalDirOverride, globalDeployDirOverride
	globalDirOverride, globalDeployDirOverride = filepath.Join(home, ".apm"), home
	t.Cleanup(func() { globalDirOverride, globalDeployDirOverride = oldDir, oldDeploy })

	src = filepath.Join(t.TempDir(), "dep")
	writeGlobalStaleFile(t, filepath.Join(src, "apm.yml"), "name: dep\nversion: 1.0.0\n")
	for rel, content := range files {
		writeGlobalStaleFile(t, filepath.Join(src, filepath.FromSlash(rel)), content)
	}
	return home, src
}

func writeGlobalStaleFile(t *testing.T, full, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// globalStaleInstall runs `install --global -t target [packages]` and returns
// its stdout, which also carries ux.Warn output.
func globalStaleInstall(t *testing.T, target string, packages ...string) string {
	t.Helper()
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	return captureUninstallStdout(t, func() {
		deployDir, err := enterGlobalScope()
		if err != nil {
			t.Fatal(err)
		}
		if err := ensureGlobalManifest(); err != nil {
			t.Fatal(err)
		}
		if err := runInstall(deps, false, true, target, deployDir, nil, packages); err != nil {
			t.Fatalf("install --global: %v", err)
		}
	})
}

func globalStaleIsSymlink(t *testing.T, full string) bool {
	t.Helper()
	info, err := os.Lstat(full)
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()&os.ModeSymlink != 0
}

func globalStaleRead(t *testing.T, full string) string {
	t.Helper()
	data, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertGlobalStaleLock(t *testing.T, home string, want bool, rel string) {
	t.Helper()
	lock := globalStaleRead(t, filepath.Join(home, ".apm", "apm.lock.yaml"))
	if got := strings.Contains(lock, rel); got != want {
		t.Errorf("lock records %s = %v, want %v\n%s", rel, got, want, lock)
	}
}

// globalStaleModuleFiles returns every regular file under the global
// apm_modules directory (slash-separated relative path -> content).
func globalStaleModuleFiles(t *testing.T, home string) map[string]string {
	t.Helper()
	root := filepath.Join(home, ".apm", "apm_modules")
	files := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || !info.Mode().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		// The module key carries a hash of the temp source path.
		files[strings.Join(parts[2:], "/")] = globalStaleRead(t, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestInstallGlobal_StaleCleanup_RemovesAgentSymlink(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{
		".apm/agents/keep.md": globalStaleKeepAgent,
		".apm/agents/gone.md": globalStaleGoneAgent,
	})
	globalStaleInstall(t, "claude", src)
	gone := filepath.Join(home, ".claude", "agents", "gone.md")
	if !globalStaleIsSymlink(t, gone) {
		t.Fatal("precondition: .claude/agents/gone.md is not a symlink after the first install")
	}

	if err := os.Remove(filepath.Join(src, ".apm", "agents", "gone.md")); err != nil {
		t.Fatal(err)
	}
	out := globalStaleInstall(t, "claude")

	if _, err := os.Lstat(gone); !os.IsNotExist(err) {
		t.Errorf(".claude/agents/gone.md still present (Lstat err = %v)", err)
	}
	if got := globalStaleRead(t, filepath.Join(home, ".claude", "agents", "keep.md")); got != globalStaleKeepAgent {
		t.Errorf("keep.md through its symlink = %q, want %q", got, globalStaleKeepAgent)
	}
	if !strings.Contains(out, "Cleaned 1 stale file from _local/dep-") {
		t.Errorf("missing cleaned line:\n%s", out)
	}
	if strings.Count(out, ".claude/agents/gone.md\n") != 1 {
		t.Errorf("want .claude/agents/gone.md listed once:\n%s", out)
	}
	assertGlobalStaleLock(t, home, false, ".claude/agents/gone.md")
	assertGlobalStaleLock(t, home, true, ".claude/agents/keep.md")

	want := map[string]string{"apm.yml": "name: dep\nversion: 1.0.0\n", ".apm/agents/keep.md": globalStaleKeepAgent}
	got := globalStaleModuleFiles(t, home)
	if len(got) != len(want) {
		t.Errorf("apm_modules files = %v, want %v", got, want)
	}
	for rel, content := range want {
		if got[rel] != content {
			t.Errorf("apm_modules %s = %q, want %q", rel, got[rel], content)
		}
	}
}

func TestInstallGlobal_StaleCleanup_RemovesWholeSkillSymlink(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{
		".apm/agents/keep.md":       globalStaleKeepAgent,
		".apm/skills/demo/SKILL.md": staleCleanupSkillMD,
	})
	globalStaleInstall(t, "claude", src)
	skill := filepath.Join(home, ".claude", "skills", "demo")
	if !globalStaleIsSymlink(t, skill) {
		t.Fatal("precondition: .claude/skills/demo is not a symlink after the first install")
	}
	assertGlobalStaleLock(t, home, true, ".claude/skills/demo/SKILL.md")

	if err := os.RemoveAll(filepath.Join(src, ".apm", "skills", "demo")); err != nil {
		t.Fatal(err)
	}
	out := globalStaleInstall(t, "claude")

	if _, err := os.Lstat(skill); !os.IsNotExist(err) {
		t.Errorf(".claude/skills/demo still present (Lstat err = %v)", err)
	}
	if _, err := os.Lstat(filepath.Dir(skill)); !os.IsNotExist(err) {
		t.Errorf("empty .claude/skills still present (Lstat err = %v)", err)
	}
	if !strings.Contains(out, "Cleaned 1 stale file from _local/dep-") {
		t.Errorf("missing cleaned line:\n%s", out)
	}
	if strings.Count(out, ".claude/skills/demo\n") != 1 || strings.Contains(out, ".claude/skills/demo/SKILL.md") {
		t.Errorf("want .claude/skills/demo listed once and no file below it:\n%s", out)
	}
	assertGlobalStaleLock(t, home, false, ".claude/skills/demo")
	if got := globalStaleRead(t, filepath.Join(home, ".claude", "agents", "keep.md")); got != globalStaleKeepAgent {
		t.Errorf("keep.md through its symlink = %q, want %q", got, globalStaleKeepAgent)
	}
}

func TestInstallGlobal_StaleCleanup_KeepsSkillSymlinkWhenOneFileRemains(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{
		".apm/skills/demo/SKILL.md": staleCleanupSkillMD,
		".apm/skills/demo/extra.md": "extra\n",
	})
	globalStaleInstall(t, "claude", src)

	if err := os.Remove(filepath.Join(src, ".apm", "skills", "demo", "extra.md")); err != nil {
		t.Fatal(err)
	}
	out := globalStaleInstall(t, "claude")

	skill := filepath.Join(home, ".claude", "skills", "demo")
	if !globalStaleIsSymlink(t, skill) {
		t.Error(".claude/skills/demo is no longer a symlink")
	}
	if got := globalStaleRead(t, filepath.Join(skill, "SKILL.md")); got != staleCleanupSkillMD {
		t.Errorf("SKILL.md through the skill symlink = %q, want %q", got, staleCleanupSkillMD)
	}
	if strings.Contains(out, "Cleaned") {
		t.Errorf("nothing was deleted, want no cleaned line:\n%s", out)
	}
	got := globalStaleModuleFiles(t, home)
	if len(got) != 2 || got[".apm/skills/demo/SKILL.md"] != staleCleanupSkillMD || got["apm.yml"] != "name: dep\nversion: 1.0.0\n" {
		t.Errorf("apm_modules files = %v, want apm.yml and .apm/skills/demo/SKILL.md unchanged", got)
	}
	assertGlobalStaleLock(t, home, true, ".claude/skills/demo/SKILL.md")
	assertGlobalStaleLock(t, home, false, ".claude/skills/demo/extra.md")
}

func TestInstallGlobal_StaleCleanup_RemovesGeneratedRegularFile(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{
		".apm/agents/keep.md": globalStaleKeepAgent,
		".apm/agents/gone.md": globalStaleGoneAgent,
	})
	globalStaleInstall(t, "codex", src)
	gone := filepath.Join(home, ".codex", "agents", "gone.toml")
	if info, err := os.Lstat(gone); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("precondition: .codex/agents/gone.toml is not a regular file (info = %v, err = %v)", info, err)
	}

	if err := os.Remove(filepath.Join(src, ".apm", "agents", "gone.md")); err != nil {
		t.Fatal(err)
	}
	out := globalStaleInstall(t, "codex")

	if _, err := os.Lstat(gone); !os.IsNotExist(err) {
		t.Errorf(".codex/agents/gone.toml still present (Lstat err = %v)", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".codex", "agents", "keep.toml")); err != nil {
		t.Errorf(".codex/agents/keep.toml: %v", err)
	}
	if !strings.Contains(out, "Cleaned 1 stale file from _local/dep-") || strings.Count(out, ".codex/agents/gone.toml\n") != 1 {
		t.Errorf("want one cleaned line listing .codex/agents/gone.toml:\n%s", out)
	}
	assertGlobalStaleLock(t, home, false, ".codex/agents/gone.toml")
}

func TestInstallGlobal_StaleCleanup_KeepsUserEditedRegularFile(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{
		".apm/agents/keep.md": globalStaleKeepAgent,
		".apm/agents/gone.md": globalStaleGoneAgent,
	})
	globalStaleInstall(t, "codex", src)
	gone := filepath.Join(home, ".codex", "agents", "gone.toml")
	writeGlobalStaleFile(t, gone, "edited by the user\n")

	if err := os.Remove(filepath.Join(src, ".apm", "agents", "gone.md")); err != nil {
		t.Fatal(err)
	}
	out := globalStaleInstall(t, "codex")

	if got := globalStaleRead(t, gone); got != "edited by the user\n" {
		t.Errorf(".codex/agents/gone.toml = %q, want the user's edit", got)
	}
	if !strings.Contains(out, `keeping ".codex/agents/gone.toml": modified since deploy (hash mismatch)`) {
		t.Errorf("missing hash-mismatch warning:\n%s", out)
	}
	if strings.Contains(out, "Cleaned") {
		t.Errorf("nothing was deleted, want no cleaned line:\n%s", out)
	}
}

func TestInstallGlobal_StaleCleanup_KeepsSymlinkPointingOutsideGlobalDir(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{
		".apm/agents/keep.md": globalStaleKeepAgent,
		".apm/agents/gone.md": globalStaleGoneAgent,
	})
	globalStaleInstall(t, "claude", src)

	// The user replaced the deployed symlink with their own.
	own := filepath.Join(t.TempDir(), "mine.md")
	writeGlobalStaleFile(t, own, "the user's own agent\n")
	gone := filepath.Join(home, ".claude", "agents", "gone.md")
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(own, gone); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(src, ".apm", "agents", "gone.md")); err != nil {
		t.Fatal(err)
	}
	out := globalStaleInstall(t, "claude")

	if !globalStaleIsSymlink(t, gone) {
		t.Error("the user's symlink was removed")
	}
	if got := globalStaleRead(t, own); got != "the user's own agent\n" {
		t.Errorf("symlink target = %q, want it unchanged", got)
	}
	if !strings.Contains(out, `keeping ".claude/agents/gone.md": symlink target`) {
		t.Errorf("missing warning for the kept symlink:\n%s", out)
	}
	if strings.Contains(out, "Cleaned") {
		t.Errorf("nothing was deleted, want no cleaned line:\n%s", out)
	}
}

// The brief's reproduction: one agent and one whole skill deleted together,
// then a second run with nothing left to clean.
func TestInstallGlobal_StaleCleanup_AgentAndSkillThenUpToDate(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{
		".apm/agents/keep.md":       globalStaleKeepAgent,
		".apm/agents/gone.md":       globalStaleGoneAgent,
		".apm/skills/demo/SKILL.md": staleCleanupSkillMD,
	})
	globalStaleInstall(t, "claude", src)

	if err := os.Remove(filepath.Join(src, ".apm", "agents", "gone.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(src, ".apm", "skills", "demo")); err != nil {
		t.Fatal(err)
	}
	out := globalStaleInstall(t, "claude")

	if !strings.Contains(out, "Cleaned 2 stale files from _local/dep-") {
		t.Errorf("missing cleaned line:\n%s", out)
	}
	if strings.Count(out, ".claude/agents/gone.md\n") != 1 || strings.Count(out, ".claude/skills/demo\n") != 1 {
		t.Errorf("want each removed symlink listed once:\n%s", out)
	}
	got := globalStaleModuleFiles(t, home)
	if len(got) != 2 || got[".apm/agents/keep.md"] != globalStaleKeepAgent || got["apm.yml"] != "name: dep\nversion: 1.0.0\n" {
		t.Errorf("apm_modules files = %v, want apm.yml and .apm/agents/keep.md unchanged", got)
	}

	again := globalStaleInstall(t, "claude")
	if !strings.Contains(again, "Already up to date") || strings.Contains(again, "Cleaned") {
		t.Errorf("second run: want Already up to date and no cleaned line:\n%s", again)
	}
}

func TestInstallGlobal_StaleCleanup_DeployDirIsUsersOwnSymlink(t *testing.T) {
	home, src := globalStaleScope(t, map[string]string{
		".apm/agents/keep.md": globalStaleKeepAgent,
		".apm/agents/gone.md": globalStaleGoneAgent,
	})
	dotfiles := filepath.Join(t.TempDir(), "dotfiles", "claude")
	if err := os.MkdirAll(dotfiles, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dotfiles, filepath.Join(home, ".claude")); err != nil {
		t.Fatal(err)
	}
	globalStaleInstall(t, "claude", src)
	if !globalStaleIsSymlink(t, filepath.Join(dotfiles, "agents", "gone.md")) {
		t.Fatal("precondition: agents/gone.md is not a symlink behind the user's .claude symlink")
	}

	if err := os.Remove(filepath.Join(src, ".apm", "agents", "gone.md")); err != nil {
		t.Fatal(err)
	}
	out := globalStaleInstall(t, "claude")

	if _, err := os.Lstat(filepath.Join(dotfiles, "agents", "gone.md")); !os.IsNotExist(err) {
		t.Errorf("agents/gone.md still present (Lstat err = %v)", err)
	}
	if !strings.Contains(out, "Cleaned 1 stale file from _local/dep-") || strings.Count(out, ".claude/agents/gone.md\n") != 1 {
		t.Errorf("want one cleaned line listing .claude/agents/gone.md:\n%s", out)
	}
	if got := globalStaleRead(t, filepath.Join(home, ".claude", "agents", "keep.md")); got != globalStaleKeepAgent {
		t.Errorf("keep.md through its symlink = %q, want %q", got, globalStaleKeepAgent)
	}
	if !globalStaleIsSymlink(t, filepath.Join(home, ".claude")) {
		t.Error(".claude is no longer the user's symlink")
	}
	assertGlobalStaleLock(t, home, false, ".claude/agents/gone.md")
	assertGlobalStaleLock(t, home, true, ".claude/agents/keep.md")
}
