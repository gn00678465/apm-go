package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
)

// skillJunk is what the Oracle's skill copy leaves out of the skill sk, with
// the bytes main 7984723 deployed and recorded.
var skillJunk = map[string]string{".apm-pin": "pin\n", "__pycache__/c.pyc": "pyc\n"}

// recordOldSkillFiles adds to the lock, under bucket ("" for the project's
// own content), each file of junk below skillDir with the hash main recorded
// for it, as the lock of an install before issue #126 lists them.
func recordOldSkillFiles(t *testing.T, bucket, skillDir string, junk map[string]string) {
	t.Helper()
	lock := readLockfile(t)
	files, hashes := &lock.LocalDeployedFiles, lock.LocalDeployedHashes
	if bucket != "" {
		dep := lock.FindByKey(bucket)
		files, hashes = &dep.DeployedFiles, dep.DeployedHashes
	}
	for rel, body := range junk {
		path := skillDir + "/" + rel
		h, err := lockfile.HashDeployedFile(writeTemp(t, body))
		if err != nil {
			t.Fatal(err)
		}
		*files = append(*files, path)
		hashes[path] = h
	}
	slices.Sort(*files)
	data, err := lockfile.WriteLockfile(lock, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("apm.lock.yaml", data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// A project install over a lock that lists what the Oracle's skill copy
// leaves out cleans those files as stale, as Oracle 0.33.0's install over
// that lock does (skex/up1: Cleaned 8 stale files from ./ap), and a frozen
// install of the lock it writes passes.
func TestInstall_SkillFilesLeftOutAreStale(t *testing.T) {
	root := upDepProject(t, "  apm:\n    - ../dep\n")
	for rel, body := range skillJunk {
		writeCompileFile(t, root, "dep/.apm/skills/sk/"+rel, body)
	}
	mustRunCLI(t, "install")
	before := readLockfile(t)
	for rel, body := range skillJunk {
		writeCompileFile(t, filepath.Join(root, "proj"), ".claude/skills/sk/"+rel, body)
	}
	recordOldSkillFiles(t, "_local/dep", ".claude/skills/sk", skillJunk)

	stdout, code := runCLI86(t, "install")

	want := " i Cleaned 2 stale files from ../dep\n  - .claude/skills/sk/.apm-pin\n  - .claude/skills/sk/__pycache__/c.pyc\n"
	if code != 0 || !strings.Contains(stdout, want) {
		t.Errorf("exit %d, stdout:\n%s\nwant exit 0 and:\n%s", code, stdout, want)
	}
	for rel := range skillJunk {
		if _, err := os.Lstat(filepath.Join(".claude", "skills", "sk", filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf(".claude/skills/sk/%s is still there", rel)
		}
	}
	if got := readLockfile(t).FindByKey("_local/dep").DeployedFiles; !slices.Equal(got, before.FindByKey("_local/dep").DeployedFiles) {
		t.Errorf("lock records %v, want %v", got, before.FindByKey("_local/dep").DeployedFiles)
	}
	if out, code := runCLI86(t, "install", "--frozen"); code != 0 {
		t.Errorf("frozen install exit %d:\n%s", code, out)
	}
}

// A --global install links the skill directory, which still shows what the
// copy leaves out, so an old lock's records of it there are dropped without a
// word and nothing is removed through the link, for a dependency's skill and
// the user scope's own (issue #126 comment 6267289737 item 1).
func TestInstallGlobal_SkillFilesLeftOutLeaveTheLock(t *testing.T) {
	root := globalOwners112(t)
	home := filepath.Join(root, "home")
	writeCompileFile(t, root, "dep-a/.apm/skills/sk/SKILL.md", "---\nname: sk\ndescription: d\n---\nbody\n")
	ownDir := filepath.Join(home, ".apm", ".apm", "skills", "own")
	writeCompileFile(t, ownDir, "SKILL.md", "---\nname: own\ndescription: d\n---\nbody\n")
	for rel, body := range skillJunk {
		writeCompileFile(t, root, "dep-a/.apm/skills/sk/"+rel, body)
		writeCompileFile(t, ownDir, rel, body)
	}
	writeGlobalManifest112(t, root, "claude", "dep-a")
	mustRunCLI(t, "install", "-g")
	t.Chdir(filepath.Join(home, ".apm"))
	before := readLockfile(t)
	recordOldSkillFiles(t, "_local/dep-a", ".claude/skills/sk", skillJunk)
	recordOldSkillFiles(t, "", ".claude/skills/own", skillJunk)

	stdout, code := runCLI86(t, "install", "-g")

	if code != 0 || strings.Contains(stdout, "Cleaned") || strings.Contains(stdout, "keeping") {
		t.Errorf("exit %d, stdout:\n%s\nwant exit 0 and no cleanup line", code, stdout)
	}
	t.Chdir(filepath.Join(home, ".apm"))
	after := readLockfile(t)
	if !slices.Equal(after.FindByKey("_local/dep-a").DeployedFiles, before.FindByKey("_local/dep-a").DeployedFiles) ||
		!slices.Equal(after.LocalDeployedFiles, before.LocalDeployedFiles) {
		t.Errorf("lock records %v and %v, want %v and %v", after.FindByKey("_local/dep-a").DeployedFiles, after.LocalDeployedFiles,
			before.FindByKey("_local/dep-a").DeployedFiles, before.LocalDeployedFiles)
	}
	for _, p := range []string{".claude/skills/sk/.apm-pin", ".claude/skills/own/.apm-pin"} {
		if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(p))); err != nil {
			t.Errorf("%s is not seen through the link: %v", p, err)
		}
	}
	if out, code := runCLI86(t, "install", "-g", "--frozen"); code != 0 {
		t.Errorf("frozen install exit %d:\n%s", code, out)
	}
}
