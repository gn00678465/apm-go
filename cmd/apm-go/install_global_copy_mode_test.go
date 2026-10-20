package main

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/gitops"
)

// Issue #118: on a Windows account that cannot make a symlink (not an
// administrator, no Developer Mode), `install --global` copies every
// primitive, as the Oracle's user scope always does. The probe is forced here
// so the copy deploy runs on any platform.

const (
	copyModeAgent       = "---\nname: ag\ndescription: agent\n---\nagent body\n"
	copyModeSkill       = "---\nname: sk\ndescription: skill\n---\nskill body\n"
	copyModeGoneSkill   = "---\nname: gone\ndescription: gone\n---\ngone body\n"
	copyModeInstruction = "---\napplyTo: \"**/*.go\"\n---\nuse gofmt\n"
	copyModePrompt      = "---\ndescription: prompt\n---\nprompt body\n"
)

var copyModePackage = map[string]string{
	".apm/agents/ag.agent.md":             copyModeAgent,
	".apm/skills/sk/SKILL.md":             copyModeSkill,
	".apm/skills/gone/SKILL.md":           copyModeGoneSkill,
	".apm/instructions/r.instructions.md": copyModeInstruction,
	".apm/prompts/p.prompt.md":            copyModePrompt,
}

// copyModeDeployed is every file the package deploys to claude and copilot,
// with the bytes a copy writes when it copies the source as it is ("" for a
// file the deploy converts: a claude rule and a command).
var copyModeDeployed = map[string]string{
	".claude/agents/ag.md":                    copyModeAgent,
	".claude/skills/sk/SKILL.md":              copyModeSkill,
	".claude/skills/gone/SKILL.md":            copyModeGoneSkill,
	".claude/rules/r.md":                      "",
	".claude/commands/p.md":                   "",
	".copilot/agents/ag.agent.md":             copyModeAgent,
	".copilot/instructions/r.instructions.md": copyModeInstruction,
	".copilot/prompts/p.prompt.md":            copyModePrompt,
	".agents/skills/sk/SKILL.md":              copyModeSkill,
	".agents/skills/gone/SKILL.md":            copyModeGoneSkill,
}

func setSymlinkProbe(t *testing.T, can bool) {
	t.Helper()
	t.Cleanup(deploy.SetSymlinkProbeForTest(func() bool { return can }))
}

func copyModeInstall(t *testing.T, frozen bool, target string, packages ...string) (string, error) {
	t.Helper()
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	var runErr error
	out := captureUninstallStdout(t, func() {
		deployDir, err := enterGlobalScope()
		if err != nil {
			t.Fatal(err)
		}
		runErr = runInstall(deps, frozen, true, target, deployDir, nil, packages)
	})
	return out, runErr
}

func copyModeUninstall(t *testing.T, pkg string) string {
	t.Helper()
	return captureUninstallStdout(t, func() {
		if err := runUninstall([]string{pkg}, uninstallOptions{Global: true}); err != nil {
			t.Fatalf("uninstall --global: %v", err)
		}
	})
}

// assertRegularFile fails unless full is a regular file, not a symlink, and,
// when want is not empty, has those bytes.
func assertRegularFile(t *testing.T, full, want string) {
	t.Helper()
	info, err := os.Lstat(full)
	if err != nil {
		t.Errorf("%s: %v", full, err)
		return
	}
	if !info.Mode().IsRegular() {
		t.Errorf("%s: mode %v, want a regular file", full, info.Mode())
		return
	}
	if want != "" {
		if got := globalStaleRead(t, full); got != want {
			t.Errorf("%s = %q, want %q", full, got, want)
		}
	}
}

// assertLockHashes fails unless the lock records each path with the hash of
// the bytes on disk.
func assertLockHashes(t *testing.T, home string, paths []string) {
	t.Helper()
	lock := globalStaleRead(t, filepath.Join(home, ".apm", "apm.lock.yaml"))
	for _, rel := range paths {
		want := rel + ": " + sha256Of(globalStaleRead(t, filepath.Join(home, filepath.FromSlash(rel))))
		if !strings.Contains(lock, want) {
			t.Errorf("lock does not record %q\n%s", want, lock)
		}
	}
}

func TestInstallGlobal_CopyMode_DeploysRegularFiles(t *testing.T) {
	setSymlinkProbe(t, false)
	home, src := globalStaleScope(t, copyModePackage)

	if out, err := copyModeInstall(t, false, "claude,copilot", src); err != nil {
		t.Fatalf("install --global: %v\n%s", err, out)
	}

	for rel, want := range copyModeDeployed {
		assertRegularFile(t, filepath.Join(home, filepath.FromSlash(rel)), want)
	}
	for _, dir := range []string{".claude/skills/sk", ".agents/skills/sk"} {
		if info, err := os.Lstat(filepath.Join(home, filepath.FromSlash(dir))); err != nil || !info.IsDir() {
			t.Errorf("%s: %v, %v; want a real directory", dir, info, err)
		}
		assertGlobalStaleLock(t, home, true, "- "+dir+"\n")
	}
	assertLockHashes(t, home, slices.Collect(maps.Keys(copyModeDeployed)))
}

func TestInstallGlobal_CopyMode_StaleCleanupAndUninstall(t *testing.T) {
	setSymlinkProbe(t, false)
	home, src := globalStaleScope(t, copyModePackage)
	if out, err := copyModeInstall(t, false, "claude,copilot", src); err != nil {
		t.Fatalf("install --global: %v\n%s", err, out)
	}
	for _, rel := range []string{".apm/agents/ag.agent.md", ".apm/skills/gone"} {
		if err := os.RemoveAll(filepath.Join(src, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}

	if out, err := copyModeInstall(t, false, "claude,copilot"); err != nil {
		t.Fatalf("second install --global: %v\n%s", err, out)
	}

	for _, rel := range []string{".claude/agents/ag.md", ".copilot/agents/ag.agent.md", ".claude/skills/gone", ".agents/skills/gone"} {
		if _, err := os.Lstat(filepath.Join(home, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("%s still present after the stale cleanup (Lstat err = %v)", rel, err)
		}
		assertGlobalStaleLock(t, home, false, rel)
	}
	assertRegularFile(t, filepath.Join(home, ".claude", "skills", "sk", "SKILL.md"), copyModeSkill)

	copyModeUninstall(t, src)

	for rel := range copyModeDeployed {
		if _, err := os.Lstat(filepath.Join(home, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("%s still present after uninstall --global (Lstat err = %v)", rel, err)
		}
	}
}

// A symlink an earlier symlink deploy left is replaced by a file, and nothing
// is written through it into the package source it points at.
func TestInstallGlobal_CopyMode_ReplacesEarlierDeployLinks(t *testing.T) {
	home, src := globalStaleScope(t, copyModePackage)
	setSymlinkProbe(t, true)
	if out, err := copyModeInstall(t, false, "claude,copilot", src); err != nil {
		t.Fatalf("symlink install --global: %v\n%s", err, out)
	}
	for _, rel := range []string{".claude/agents/ag.md", ".copilot/instructions/r.instructions.md", ".claude/skills/sk"} {
		if !globalStaleIsSymlink(t, filepath.Join(home, filepath.FromSlash(rel))) {
			t.Fatalf("precondition: %s is not a symlink after the symlink deploy", rel)
		}
	}
	modulesBefore := globalStaleModuleFiles(t, home)

	setSymlinkProbe(t, false)
	if out, err := copyModeInstall(t, false, "claude,copilot"); err != nil {
		t.Fatalf("copy install --global: %v\n%s", err, out)
	}

	for rel, want := range copyModeDeployed {
		assertRegularFile(t, filepath.Join(home, filepath.FromSlash(rel)), want)
	}
	assertLockHashes(t, home, slices.Collect(maps.Keys(copyModeDeployed)))
	if got := globalStaleModuleFiles(t, home); !maps.Equal(got, modulesBefore) {
		t.Errorf("apm_modules changed:\n got %v\nwant %v", got, modulesBefore)
	}
}

// Two packages have an agent of one name. After the symlink deploy the
// agent links to the first package's source; with the packages swapped, the
// copy writes the new owner's bytes and leaves the old owner's source as it
// was, which a write through the link would have overwritten.
func TestInstallGlobal_CopyMode_OwnerSwapKeepsOldOwnerSource(t *testing.T) {
	const (
		agentA = "---\nname: ag\ndescription: a\n---\nfrom a\n"
		agentB = "---\nname: ag\ndescription: b\n---\nfrom b\n"
	)
	home, srcA := globalStaleScope(t, map[string]string{".apm/agents/ag.agent.md": agentA})
	srcB := filepath.Join(filepath.Dir(srcA), "other")
	writeGlobalStaleFile(t, filepath.Join(srcB, "apm.yml"), "name: other\nversion: 1.0.0\n")
	writeGlobalStaleFile(t, filepath.Join(srcB, ".apm", "agents", "ag.agent.md"), agentB)
	setSymlinkProbe(t, true)
	if out, err := copyModeInstall(t, false, "claude", srcA, srcB); err != nil {
		t.Fatalf("symlink install --global: %v\n%s", err, out)
	}
	deployed := filepath.Join(home, ".claude", "agents", "ag.md")
	if !globalStaleIsSymlink(t, deployed) || globalStaleRead(t, deployed) != agentA {
		t.Fatal("precondition: .claude/agents/ag.md is not a symlink to the first package's agent")
	}
	manifest := filepath.Join(home, ".apm", "apm.yml")
	data := globalStaleRead(t, manifest)
	swapped := strings.NewReplacer("- "+srcA+"\n", "- "+srcB+"\n", "- "+srcB+"\n", "- "+srcA+"\n").Replace(data)
	if swapped == data {
		t.Fatalf("apm.yml does not list both packages:\n%s", data)
	}
	writeGlobalStaleFile(t, manifest, swapped)
	modulesBefore := globalStaleModuleFiles(t, home)

	setSymlinkProbe(t, false)
	if out, err := copyModeInstall(t, false, "claude"); err != nil {
		t.Fatalf("copy install --global: %v\n%s", err, out)
	}

	assertRegularFile(t, deployed, agentB)
	if got := globalStaleModuleFiles(t, home); !maps.Equal(got, modulesBefore) {
		t.Errorf("apm_modules changed:\n got %v\nwant %v", got, modulesBefore)
	}
}

// A symlink the user put at a destination, one that leads out of the
// primitive's directory and not into the global project directory, is
// refused and kept, as a project install keeps it.
func TestInstallGlobal_CopyMode_UserLinkRejected(t *testing.T) {
	setSymlinkProbe(t, false)
	home, src := globalStaleScope(t, map[string]string{".apm/agents/ag.agent.md": copyModeAgent})
	user := filepath.Join(home, "notes", "ag.md")
	writeGlobalStaleFile(t, user, "USER CONTENT\n")
	dest := filepath.Join(home, ".claude", "agents", "ag.md")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(user, dest); err != nil {
		t.Fatal(err)
	}

	out, err := copyModeInstall(t, false, "claude", src)

	if err != nil {
		t.Fatalf("install --global: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Rejected agent target path: Cannot verify containment of") {
		t.Errorf("missing Rejected line:\n%s", out)
	}
	if got, _ := os.Readlink(dest); got != user {
		t.Errorf(".claude/agents/ag.md -> %q, want the user's link to %q", got, user)
	}
	if got := globalStaleRead(t, user); got != "USER CONTENT\n" {
		t.Errorf("user file = %q, want it unchanged", got)
	}
}

func TestInstallGlobal_CopyMode_FrozenRestoresFiles(t *testing.T) {
	for _, firstSymlink := range []bool{false, true} {
		name := "after copy install"
		if firstSymlink {
			name = "after symlink install"
		}
		t.Run(name, func(t *testing.T) {
			home, src := globalStaleScope(t, copyModePackage)
			setSymlinkProbe(t, firstSymlink)
			if out, err := copyModeInstall(t, false, "claude,copilot", src); err != nil {
				t.Fatalf("install --global: %v\n%s", err, out)
			}
			lockPath := filepath.Join(home, ".apm", "apm.lock.yaml")
			lockBefore := globalStaleRead(t, lockPath)
			for _, root := range []string{".claude", ".copilot", ".agents"} {
				if err := os.RemoveAll(filepath.Join(home, root)); err != nil {
					t.Fatal(err)
				}
			}

			setSymlinkProbe(t, false)
			out, err := copyModeInstall(t, true, "claude,copilot")

			if err != nil {
				t.Fatalf("frozen install --global: %v\n%s", err, out)
			}
			for rel, want := range copyModeDeployed {
				assertRegularFile(t, filepath.Join(home, filepath.FromSlash(rel)), want)
			}
			if got := globalStaleRead(t, lockPath); got != lockBefore {
				t.Errorf("lock changed:\n%s\nwant:\n%s", got, lockBefore)
			}
		})
	}
}

// The package that stays receives the shared agent as a file of its own.
func TestInstallGlobal_CopyMode_UninstallHandsOverSharedFile(t *testing.T) {
	const (
		agentA = "---\nname: ag\ndescription: a\n---\nfrom a\n"
		agentB = "---\nname: ag\ndescription: b\n---\nfrom b\n"
	)
	setSymlinkProbe(t, false)
	home, srcA := globalStaleScope(t, map[string]string{".apm/agents/ag.agent.md": agentA})
	srcB := filepath.Join(filepath.Dir(srcA), "other")
	writeGlobalStaleFile(t, filepath.Join(srcB, "apm.yml"), "name: other\nversion: 1.0.0\n")
	writeGlobalStaleFile(t, filepath.Join(srcB, ".apm", "agents", "ag.agent.md"), agentB)
	if out, err := copyModeInstall(t, false, "claude", srcA, srcB); err != nil {
		t.Fatalf("install --global: %v\n%s", err, out)
	}
	deployed := filepath.Join(home, ".claude", "agents", "ag.md")
	assertRegularFile(t, deployed, agentA)

	out := copyModeUninstall(t, srcA)

	assertRegularFile(t, deployed, agentB)
	if !strings.Contains(out, "|-- 1 agents integrated -> .claude/agents/") {
		t.Errorf("missing hand-over line:\n%s", out)
	}
	assertLockHashes(t, home, []string{".claude/agents/ag.md"})
	lock := globalStaleRead(t, filepath.Join(home, ".apm", "apm.lock.yaml"))
	if strings.Contains(lock, "repo_url: _local/dep") {
		t.Errorf("lock still records the removed package:\n%s", lock)
	}
}

// The project's own skill holds a symlink to a file in it and one to a file
// outside it. A symlink deploy records both through its link to the skill
// directory; a copy deploy writes each as a file with its target's bytes, so
// a frozen install on an account that cannot make a symlink restores the
// lock a symlink deploy wrote, and an install leaves it as it is.
func TestInstallGlobal_CopyMode_OwnSkillFileLinksMatchSymlinkLock(t *testing.T) {
	const (
		data    = "UNCHANGED SOURCE\n"
		outside = "user notes outside the skill\n"
	)
	home, _ := globalStaleScope(t, nil)
	writeGlobalStaleFile(t, filepath.Join(home, ".apm", "apm.yml"), "name: global\nversion: 1.0.0\n")
	source := filepath.Join(home, ".apm", ".apm", "skills", "local")
	writeGlobalStaleFile(t, filepath.Join(source, "SKILL.md"), "---\nname: local\ndescription: d\n---\nbody\n")
	writeGlobalStaleFile(t, filepath.Join(source, "data.txt"), data)
	notes := filepath.Join(home, "notes", "o.txt")
	writeGlobalStaleFile(t, notes, outside)
	if err := os.Symlink("data.txt", filepath.Join(source, "alias.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(notes, filepath.Join(source, "outside.txt")); err != nil {
		t.Fatal(err)
	}
	setSymlinkProbe(t, true)
	if out, err := copyModeInstall(t, false, "claude"); err != nil {
		t.Fatalf("symlink install --global: %v\n%s", err, out)
	}
	lockPath := filepath.Join(home, ".apm", "apm.lock.yaml")
	lockBefore := globalStaleRead(t, lockPath)
	for _, rel := range []string{".claude/skills/local/alias.txt", ".claude/skills/local/outside.txt"} {
		if !strings.Contains(lockBefore, rel+": sha256:") {
			t.Fatalf("precondition: the symlink deploy's lock does not record %s:\n%s", rel, lockBefore)
		}
	}
	deployed := filepath.Join(home, ".claude", "skills", "local")
	if err := os.Remove(deployed); err != nil {
		t.Fatal(err)
	}

	setSymlinkProbe(t, false)
	out, err := copyModeInstall(t, true, "claude")

	if err != nil {
		t.Fatalf("frozen install --global: %v\n%s", err, out)
	}
	assertRegularFile(t, filepath.Join(deployed, "alias.txt"), data)
	assertRegularFile(t, filepath.Join(deployed, "outside.txt"), outside)
	assertRegularFile(t, filepath.Join(deployed, "data.txt"), data)
	if got := globalStaleRead(t, lockPath); got != lockBefore {
		t.Errorf("lock changed by the frozen install:\n%s\nwant:\n%s", got, lockBefore)
	}

	if out, err := copyModeInstall(t, false, "claude"); err != nil {
		t.Fatalf("copy install --global: %v\n%s", err, out)
	}
	if got := globalStaleRead(t, lockPath); got != lockBefore {
		t.Errorf("lock changed by the copy install:\n%s\nwant:\n%s", got, lockBefore)
	}
}

// A symlink in the project's own skill that leads to a directory, or
// dangles, is neither written nor recorded by a copy deploy.
func TestInstallGlobal_CopyMode_OwnSkillDirAndDanglingLinksSkipped(t *testing.T) {
	setSymlinkProbe(t, false)
	home, _ := globalStaleScope(t, nil)
	writeGlobalStaleFile(t, filepath.Join(home, ".apm", "apm.yml"), "name: global\nversion: 1.0.0\n")
	source := filepath.Join(home, ".apm", ".apm", "skills", "local")
	writeGlobalStaleFile(t, filepath.Join(source, "SKILL.md"), "---\nname: local\ndescription: d\n---\nbody\n")
	writeGlobalStaleFile(t, filepath.Join(home, "notes", "n.md"), "notes\n")
	if err := os.Symlink(filepath.Join(home, "notes"), filepath.Join(source, "dir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing.txt", filepath.Join(source, "dangling.txt")); err != nil {
		t.Fatal(err)
	}

	if out, err := copyModeInstall(t, false, "claude"); err != nil {
		t.Fatalf("install --global: %v\n%s", err, out)
	}

	deployed := filepath.Join(home, ".claude", "skills", "local")
	assertRegularFile(t, filepath.Join(deployed, "SKILL.md"), "")
	for _, name := range []string{"dir", "dangling.txt"} {
		if _, err := os.Lstat(filepath.Join(deployed, name)); !os.IsNotExist(err) {
			t.Errorf("%s deployed (Lstat err = %v)", name, err)
		}
		assertGlobalStaleLock(t, home, false, ".claude/skills/local/"+name)
	}
}
