package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// Issue #113: a git or registry package whose root is a skill deploys the
// skill without the checkout's .git, and the lock records none of it, as
// Oracle 0.33.0 deploys it (measured: .claude/skills/myskill/ holds SKILL.md
// alone). A local package keeps its .git, as the Oracle copies it.

// checkout113 writes files into dir and makes dir a git checkout of all it
// holds in one commit, as the loader leaves apm_modules/<key>.
func checkout113(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		writeGlobalStaleFile(t, filepath.Join(dir, filepath.FromSlash(rel)), content)
	}
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "init")
}

func project113(t *testing.T, apmDeps string) string {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n"+apmDeps)
	return project
}

// install113 runs `install -t target`, with global `install -g`, and returns
// its stdout. A local package needs the real loader, which copies it.
func install113(t *testing.T, target string, global, local bool) string {
	t.Helper()
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	if local {
		deps.loader = subdirRealLoader()
	}
	return captureUninstallStdout(t, func() {
		deployDir := ""
		if global {
			var err error
			if deployDir, err = enterGlobalScope(); err != nil {
				t.Fatal(err)
			}
		}
		if err := runInstall(deps, false, true, target, deployDir, nil, nil); err != nil {
			t.Fatalf("install: %v", err)
		}
	})
}

func readLock113(t *testing.T, lockPath string) *lockfile.Lockfile {
	t.Helper()
	node, err := yamlcore.SafeLoad([]byte(globalStaleRead(t, lockPath)))
	if err != nil {
		t.Fatal(err)
	}
	lf, err := lockfile.ParseLockfile(node)
	if err != nil {
		t.Fatal(err)
	}
	return lf
}

// gitRecords113 returns the paths of the lock text, deployed files, hashes and
// deployments alike, that are a .git or below one.
func gitRecords113(lockText string) []string {
	var out []string
	for _, line := range strings.Split(lockText, "\n") {
		if strings.Contains(line, "/.git/") || strings.HasSuffix(strings.TrimRight(line, ":"), "/.git") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

func assertExists113(t *testing.T, full string, want bool) {
	t.Helper()
	_, err := os.Lstat(full)
	if got := err == nil; got != want {
		t.Errorf("%s exists = %v, want %v", full, got, want)
	}
}

// treeFiles113 returns every regular file below dir, by slash-separated path.
func treeFiles113(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || !info.Mode().IsRegular() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		data, err := os.ReadFile(p)
		files[filepath.ToSlash(rel)] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestInstall113_GitRootSkill_DeploysNoGit(t *testing.T) {
	cases := []struct {
		name, key string
		files     map[string]string
		skill     string
	}{
		{"root SKILL.md", "acme/myskill", map[string]string{"SKILL.md": gitVersionSkill}, "myskill"},
		// The plugin's "./" skill is the root SKILL.md's: it is shadowed by
		// it, and neither brings the .git.
		{"plugin.json skills ./", "acme/plug", map[string]string{
			"SKILL.md":                   gitVersionSkill,
			".claude-plugin/plugin.json": `{"name":"plug","skills":["./"]}`,
		}, "plug"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			project := project113(t, "    - "+c.key+"\n")
			module := filepath.Join(project, "apm_modules", filepath.FromSlash(c.key))
			checkout113(t, module, c.files)
			before := treeFiles113(t, filepath.Join(module, ".git"))

			install113(t, "claude,copilot", false, false)

			for _, root := range []string{".claude/skills", ".agents/skills"} {
				skill := filepath.Join(project, filepath.FromSlash(root), c.skill)
				assertExists113(t, filepath.Join(skill, "SKILL.md"), true)
				assertExists113(t, filepath.Join(skill, ".git"), false)
			}
			if got := gitRecords113(globalStaleRead(t, filepath.Join(project, "apm.lock.yaml"))); len(got) != 0 {
				t.Errorf("lock records .git paths: %q", got)
			}
			if got := treeFiles113(t, filepath.Join(module, ".git")); len(got) != len(before) {
				t.Errorf("apm_modules .git has %d files, had %d", len(got), len(before))
			}
			if st := gitCmd(t, module, "status", "--porcelain"); st != "" {
				t.Errorf("git status of the checkout: %q", st)
			}
		})
	}
}

// Only the root's own .git is the checkout's; one deeper down is the skill's.
func TestInstall113_GitRootSkill_KeepsNestedGit(t *testing.T) {
	project := project113(t, "    - acme/myskill\n")
	module := filepath.Join(project, "apm_modules", "acme", "myskill")
	checkout113(t, module, map[string]string{"SKILL.md": gitVersionSkill})
	writeGlobalStaleFile(t, filepath.Join(module, "sub", ".git"), "gitdir: ../elsewhere\n")

	install113(t, "claude", false, false)

	assertExists113(t, filepath.Join(project, ".claude", "skills", "myskill", "sub", ".git"), true)
	assertExists113(t, filepath.Join(project, ".claude", "skills", "myskill", ".git"), false)
	lock := globalStaleRead(t, filepath.Join(project, "apm.lock.yaml"))
	if !strings.Contains(lock, ".claude/skills/myskill/sub/.git:") {
		t.Errorf("lock does not record sub/.git:\n%s", lock)
	}
}

// A local package is copied with its .git, as the Oracle copies it.
func TestInstall113_LocalRootSkill_KeepsGit(t *testing.T) {
	project := project113(t, "    - ./dep\n")
	checkout113(t, filepath.Join(project, "dep"), map[string]string{"SKILL.md": gitVersionSkill})

	install113(t, "claude", false, true)

	assertExists113(t, filepath.Join(project, ".claude", "skills", "dep", ".git", "HEAD"), true)
	lock := globalStaleRead(t, filepath.Join(project, "apm.lock.yaml"))
	if !strings.Contains(lock, ".claude/skills/dep/.git/HEAD") {
		t.Errorf("lock does not record .git/HEAD of the local package:\n%s", lock)
	}
}

// addOldGitRecords gives the lock entry of key a record, with the hash read
// at deployRoot, of each file of module's .git below skillDir, as apm-go
// wrote the lock before #113. The returned paths are the lock paths.
func addOldGitRecords(t *testing.T, lockPath, key, module, deployRoot, skillDir string) []string {
	t.Helper()
	node, err := yamlcore.SafeLoad([]byte(globalStaleRead(t, lockPath)))
	if err != nil {
		t.Fatal(err)
	}
	lf := readLock113(t, lockPath)
	dep := lf.FindByKey(key)
	if dep == nil {
		t.Fatalf("no lock entry %s", key)
	}
	var paths []string
	for rel := range treeFiles113(t, filepath.Join(module, ".git")) {
		p := skillDir + "/.git/" + rel
		hash, err := lockfile.HashFileBytes(filepath.Join(deployRoot, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		dep.DeployedFiles = append(dep.DeployedFiles, p)
		dep.DeployedHashes[p] = hash
		paths = append(paths, p)
	}
	slices.Sort(dep.DeployedFiles)
	out, err := lockfile.WriteLockfile(lf, node)
	if err != nil {
		t.Fatal(err)
	}
	writeGlobalStaleFile(t, lockPath, string(out))
	return paths
}

// A project lock written before #113 records the copied .git: the stale
// cleanup removes each file that still has its hash and keeps, with a
// warning and its record, the one that does not.
func TestInstall113_Project_OldLockGitCleaned(t *testing.T) {
	project := project113(t, "    - acme/myskill\n")
	module := filepath.Join(project, "apm_modules", "acme", "myskill")
	checkout113(t, module, map[string]string{"SKILL.md": gitVersionSkill})
	install113(t, "claude", false, false)
	skillDir := ".claude/skills/myskill"
	for rel, content := range treeFiles113(t, filepath.Join(module, ".git")) {
		writeGlobalStaleFile(t, filepath.Join(project, filepath.FromSlash(skillDir), ".git", filepath.FromSlash(rel)), content)
	}
	lockPath := filepath.Join(project, "apm.lock.yaml")
	paths := addOldGitRecords(t, lockPath, "acme/myskill", module, project, skillDir)
	edited := skillDir + "/.git/description"
	writeGlobalStaleFile(t, filepath.Join(project, filepath.FromSlash(edited)), "edited by the user\n")

	out := install113(t, "claude", false, false)

	for _, p := range paths {
		assertExists113(t, filepath.Join(project, filepath.FromSlash(p)), p == edited)
	}
	if want := `keeping "` + edited + `": modified since deploy (hash mismatch)`; !strings.Contains(out, want) {
		t.Errorf("stdout lacks %q:\n%s", want, out)
	}
	lock := readLock113(t, lockPath)
	var git []string
	for _, f := range lock.FindByKey("acme/myskill").DeployedFiles {
		if strings.Contains(f, "/.git/") {
			git = append(git, f)
		}
	}
	if !slices.Equal(git, []string{edited}) {
		t.Errorf("lock .git records = %q, want [%s]", git, edited)
	}
	if st := gitCmd(t, module, "status", "--porcelain"); st != "" {
		t.Errorf("git status of the checkout: %q", st)
	}
}

// global113 makes ~/.apm declare acme/myskill, a git checkout of a root skill,
// and returns HOME and the checkout.
func global113(t *testing.T) (home, module string) {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	home = t.TempDir()
	oldDir, oldDeploy := globalDirOverride, globalDeployDirOverride
	globalDirOverride, globalDeployDirOverride = filepath.Join(home, ".apm"), home
	t.Cleanup(func() { globalDirOverride, globalDeployDirOverride = oldDir, oldDeploy })
	writeGlobalStaleFile(t, filepath.Join(home, ".apm", "apm.yml"), "name: g\nversion: 0.1.0\ndependencies:\n  apm:\n    - acme/myskill\n")
	module = filepath.Join(home, ".apm", "apm_modules", "acme", "myskill")
	checkout113(t, module, map[string]string{"SKILL.md": gitVersionSkill})
	return home, module
}

func TestInstallGlobal113_GitRootSkill_LockHasNoGit(t *testing.T) {
	home, module := global113(t)

	install113(t, "claude", true, false)

	link := filepath.Join(home, ".claude", "skills", "myskill")
	if got, _ := os.Readlink(link); !sameFile112(t, got, module) {
		t.Errorf("%s -> %q, want -> %s", link, got, module)
	}
	lock := globalStaleRead(t, filepath.Join(home, ".apm", "apm.lock.yaml"))
	if got := gitRecords113(lock); len(got) != 0 {
		t.Errorf("lock records .git paths: %q", got)
	}
	if !strings.Contains(lock, ".claude/skills/myskill/SKILL.md") {
		t.Errorf("lock does not record SKILL.md:\n%s", lock)
	}
}

// A --global lock written before #113 records the checkout's .git through the
// skill symlink: the records leave the lock and nothing is removed through
// the link.
func TestInstallGlobal113_OldLockGitRecordsDropped(t *testing.T) {
	home, module := global113(t)
	install113(t, "claude", true, false)
	lockPath := filepath.Join(home, ".apm", "apm.lock.yaml")
	addOldGitRecords(t, lockPath, "acme/myskill", module, home, ".claude/skills/myskill")
	before := treeFiles113(t, filepath.Join(module, ".git"))

	install113(t, "claude", true, false)

	if got := gitRecords113(globalStaleRead(t, lockPath)); len(got) != 0 {
		t.Errorf("lock records .git paths: %q", got)
	}
	if got := treeFiles113(t, filepath.Join(module, ".git")); !mapsEqual113(got, before) {
		t.Errorf("apm_modules/acme/myskill/.git changed: %d files, had %d", len(got), len(before))
	}
	if st := gitCmd(t, module, "status", "--porcelain"); st != "" {
		t.Errorf("git status of the checkout: %q", st)
	}
}

func mapsEqual113(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// A local package keeps its .git records under --global, also on the next
// install.
func TestInstallGlobal113_LocalRootSkill_KeepsGitRecords(t *testing.T) {
	home, src := globalStaleScope(t, nil)
	checkout113(t, src, map[string]string{"SKILL.md": gitVersionSkill})
	globalStaleInstall(t, "claude", src)
	assertGlobalStaleLock(t, home, true, ".claude/skills/dep/.git/HEAD")

	globalStaleInstall(t, "claude")

	assertGlobalStaleLock(t, home, true, ".claude/skills/dep/.git/HEAD")
}
