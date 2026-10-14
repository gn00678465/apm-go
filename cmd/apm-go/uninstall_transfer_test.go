package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// Issue #74: two local packages deploy the same path, the first-declared one
// owns it, and `uninstall` of the owner hands the path to the package that
// stays instead of deleting it.

const transferRule = "---\ndescription: shared\n---\nshared rule\n"

// transferManifest declares ./a and ./b, in that order.
const transferManifest = "name: app\nversion: 1.0.0\ndependencies:\n  apm:\n    - ./a\n    - ./b\n"

// transferProject writes apm.yml and the local packages a and b under dir;
// files maps "<pkg>/<path below the package>" to content.
func transferProject(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	writeGlobalStaleFile(t, filepath.Join(dir, "apm.yml"), transferManifest)
	for _, p := range []string{"a", "b"} {
		writeGlobalStaleFile(t, filepath.Join(dir, p, "apm.yml"), "name: "+p+"\nversion: 1.0.0\n")
	}
	for rel, content := range files {
		writeGlobalStaleFile(t, filepath.Join(dir, filepath.FromSlash(rel)), content)
	}
}

func transferSHA(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func transferUninstall(t *testing.T, opts uninstallOptions, pkg string) string {
	t.Helper()
	return captureUninstallStdout(t, func() {
		if err := runUninstall([]string{pkg}, opts); err != nil {
			t.Fatalf("uninstall %s: %v", pkg, err)
		}
	})
}

func transferLockDep(t *testing.T, lockPath, key string) *lockfile.LockedDep {
	t.Helper()
	node, err := yamlcore.SafeLoad([]byte(globalStaleRead(t, lockPath)))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lockfile.ParseLockfile(node)
	if err != nil {
		t.Fatal(err)
	}
	dep := lock.FindByKey(key)
	if dep == nil {
		t.Fatalf("the lock has no entry %s", key)
	}
	return dep
}

func assertTransferRecord(t *testing.T, dep *lockfile.LockedDep, files []string, hashes map[string]string) {
	t.Helper()
	if strings.Join(dep.DeployedFiles, "\n") != strings.Join(files, "\n") {
		t.Errorf("%s deployed_files = %v, want %v", dep.RepoURL, dep.DeployedFiles, files)
	}
	if len(dep.DeployedHashes) != len(hashes) {
		t.Errorf("%s deployed_file_hashes = %v, want %v", dep.RepoURL, dep.DeployedHashes, hashes)
	}
	for f, h := range hashes {
		if dep.DeployedHashes[f] != h {
			t.Errorf("%s hash of %s = %q, want %q", dep.RepoURL, f, dep.DeployedHashes[f], h)
		}
	}
}

// assertTransferSteadyInstall checks the transcript of an install that has
// nothing to deploy for ./b: the rule is on disk with the hash the lock
// records (adopted). A local package is counted on every run, as the Oracle
// counts it, so the last line is "Installed", not "No changes". A skill is
// counted integrated on every run, so its line is passed in.
func assertTransferSteadyInstall(t *testing.T, out, lastBlockLine string) {
	t.Helper()
	want := " > Installing dependencies from apm.yml...\n" +
		" i Targets: claude  (source: --target flag)\n" +
		"   + ./b (local)\n" +
		"  |-- 1 rule(s) adopted -> .claude/rules/\n" +
		lastBlockLine +
		" + Installed 1 APM dependency in "
	if !strings.HasPrefix(out, want) || strings.Count(out, "\n") != strings.Count(want, "\n")+1 {
		t.Errorf("install after the uninstall =\n%s\nwant\n%s<elapsed>s.", out, want)
	}
}

func TestUninstall_SharedRuleOwner_HandsFileToSurvivor(t *testing.T) {
	dir := staleCleanupProject(t, transferManifest, nil)
	transferProject(t, dir, map[string]string{
		"a/.apm/instructions/shared.instructions.md": transferRule,
		"b/.apm/instructions/shared.instructions.md": transferRule,
	})
	staleCleanupInstall(t, "claude")

	out := transferUninstall(t, uninstallOptions{}, "./a")

	if got := globalStaleRead(t, filepath.Join(dir, ".claude", "rules", "shared.md")); got != "shared rule\n" {
		t.Errorf(".claude/rules/shared.md = %q, want %q", got, "shared rule\n")
	}
	assertTransferRecord(t, transferLockDep(t, filepath.Join(dir, "apm.lock.yaml"), "_local/b"),
		[]string{".claude/rules/shared.md"},
		map[string]string{".claude/rules/shared.md": "sha256:a5aa98439217de45641258de8f69aea33202a07829b4acf375b5484860ca05b8"})
	if strings.Contains(out, "Cleaned") {
		t.Errorf("the handed-over file is reported as cleaned:\n%s", out)
	}
}

func TestUninstall_SharedRuleOwner_SurvivorContentDiffers(t *testing.T) {
	dir := staleCleanupProject(t, transferManifest, nil)
	transferProject(t, dir, map[string]string{
		"a/.apm/instructions/shared.instructions.md": transferRule,
		"b/.apm/instructions/shared.instructions.md": "---\ndescription: shared\n---\nb rule\n",
	})
	staleCleanupInstall(t, "claude")

	out := transferUninstall(t, uninstallOptions{}, "./a")

	if got := globalStaleRead(t, filepath.Join(dir, ".claude", "rules", "shared.md")); got != "b rule\n" {
		t.Errorf(".claude/rules/shared.md = %q, want b's %q", got, "b rule\n")
	}
	assertTransferRecord(t, transferLockDep(t, filepath.Join(dir, "apm.lock.yaml"), "_local/b"),
		[]string{".claude/rules/shared.md"},
		map[string]string{".claude/rules/shared.md": transferSHA("b rule\n")})
	if strings.Contains(out, "Cleaned") {
		t.Errorf("the handed-over file is reported as cleaned:\n%s", out)
	}
}

func TestUninstall_SharedRuleOwner_UserEditKeptAndRecordedUnderSurvivor(t *testing.T) {
	dir := staleCleanupProject(t, transferManifest, nil)
	transferProject(t, dir, map[string]string{
		"a/.apm/instructions/shared.instructions.md": transferRule,
		"b/.apm/instructions/shared.instructions.md": "---\ndescription: shared\n---\nb rule\n",
	})
	staleCleanupInstall(t, "claude")
	rule := filepath.Join(dir, ".claude", "rules", "shared.md")
	writeGlobalStaleFile(t, rule, "mine\n")

	out := transferUninstall(t, uninstallOptions{}, "./a")

	if got := globalStaleRead(t, rule); got != "mine\n" {
		t.Errorf(".claude/rules/shared.md = %q, want the user's %q", got, "mine\n")
	}
	if !strings.Contains(out, ` ! keeping ".claude/rules/shared.md": modified since deploy (hash mismatch)`) ||
		!strings.Contains(out, " ! Kept 1 deployed file(s) that were modified or are shared") {
		t.Errorf("missing the keep warnings:\n%s", out)
	}
	assertTransferRecord(t, transferLockDep(t, filepath.Join(dir, "apm.lock.yaml"), "_local/b"),
		[]string{".claude/rules/shared.md"},
		map[string]string{".claude/rules/shared.md": transferSHA("b rule\n")})
}

func TestUninstall_SharedSkillOwner_HandsSkillToSurvivor(t *testing.T) {
	dir := staleCleanupProject(t, transferManifest, nil)
	transferProject(t, dir, map[string]string{
		"a/.apm/skills/demo/SKILL.md": staleCleanupSkillMD,
		"b/.apm/skills/demo/SKILL.md": staleCleanupSkillMD,
	})
	staleCleanupInstall(t, "claude")

	out := transferUninstall(t, uninstallOptions{}, "./a")

	if got := globalStaleRead(t, filepath.Join(dir, ".claude", "skills", "demo", "SKILL.md")); got != staleCleanupSkillMD {
		t.Errorf(".claude/skills/demo/SKILL.md = %q, want %q", got, staleCleanupSkillMD)
	}
	assertTransferRecord(t, transferLockDep(t, filepath.Join(dir, "apm.lock.yaml"), "_local/b"),
		[]string{".claude/skills/demo", ".claude/skills/demo/SKILL.md"},
		map[string]string{".claude/skills/demo/SKILL.md": transferSHA(staleCleanupSkillMD)})
	if strings.Contains(out, "Cleaned") {
		t.Errorf("the handed-over skill is reported as cleaned:\n%s", out)
	}
}

func TestUninstallGlobal_SharedRuleAndSkillOwner_HandsBothToSurvivor(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	home := t.TempDir()
	oldDir, oldDeploy := globalDirOverride, globalDeployDirOverride
	globalDirOverride, globalDeployDirOverride = filepath.Join(home, ".apm"), home
	t.Cleanup(func() { globalDirOverride, globalDeployDirOverride = oldDir, oldDeploy })
	transferProject(t, filepath.Join(home, ".apm"), map[string]string{
		"a/.apm/instructions/shared.instructions.md": transferRule,
		"b/.apm/instructions/shared.instructions.md": transferRule,
		"a/.apm/skills/demo/SKILL.md":                staleCleanupSkillMD,
		"b/.apm/skills/demo/SKILL.md":                staleCleanupSkillMD,
	})
	globalStaleInstall(t, "claude")
	lockPath := filepath.Join(home, ".apm", "apm.lock.yaml")

	out := transferUninstall(t, uninstallOptions{Global: true}, "./a")

	if got := globalStaleRead(t, filepath.Join(home, ".claude", "rules", "shared.md")); got != "shared rule\n" {
		t.Errorf("~/.claude/rules/shared.md = %q, want %q", got, "shared rule\n")
	}
	link := filepath.Join(home, ".claude", "skills", "demo")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("~/.claude/skills/demo is not a symlink: %v", err)
	}
	if want := filepath.Join(home, ".apm", "apm_modules", "_local", "b", ".apm", "skills", "demo"); target != want {
		t.Errorf("~/.claude/skills/demo -> %s, want %s", target, want)
	}
	assertTransferRecord(t, transferLockDep(t, lockPath, "_local/b"),
		[]string{".claude/rules/shared.md", ".claude/skills/demo", ".claude/skills/demo/SKILL.md"},
		map[string]string{
			".claude/rules/shared.md":      "sha256:a5aa98439217de45641258de8f69aea33202a07829b4acf375b5484860ca05b8",
			".claude/skills/demo/SKILL.md": transferSHA(staleCleanupSkillMD),
		})
	if strings.Contains(out, "Cleaned") {
		t.Errorf("the handed-over files are reported as cleaned:\n%s", out)
	}

	before := globalStaleRead(t, lockPath)
	again := globalStaleInstall(t, "claude")
	assertTransferSteadyInstall(t, again, "  |-- 1 skill(s) integrated -> .claude/skills/\n")
	if got := globalStaleRead(t, lockPath); got != before {
		t.Errorf("install after the uninstall rewrote the lock:\n%s\nwas\n%s", got, before)
	}
}

// Nothing is shared: the uninstall is the one it was before the hand-over.
func TestUninstall_NoSharedPath_Unchanged(t *testing.T) {
	dir := staleCleanupProject(t, transferManifest, nil)
	transferProject(t, dir, map[string]string{
		"a/.apm/instructions/only-a.instructions.md": "---\ndescription: a\n---\na rule\n",
		"b/.apm/instructions/only-b.instructions.md": "---\ndescription: b\n---\nb rule\n",
	})
	staleCleanupInstall(t, "claude")

	out := transferUninstall(t, uninstallOptions{}, "./a")

	wantOut := " > Uninstalling 1 package(s)...\n" +
		" + _local/a - found in apm.yml\n" +
		" i Removed _local/a from apm_modules/\n" +
		" i Cleaned 1 stale file from ./a\n" +
		" i Removed _local/a from dependencies.apm in apm.yml\n" +
		" + Updated " + filepath.Join(dir, "apm.yml") + " (removed 1 package(s))\n" +
		" + Uninstall complete: Removed 1 package(s) from apm.yml, Removed 1 package(s) from apm_modules/\n"
	if out != wantOut {
		t.Errorf("stdout =\n%s\nwant\n%s", out, wantOut)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".claude", "rules", "only-a.md")); !os.IsNotExist(err) {
		t.Errorf("a's rule is still there (Lstat err = %v)", err)
	}
	if got := globalStaleRead(t, filepath.Join(dir, ".claude", "rules", "only-b.md")); got != "b rule\n" {
		t.Errorf(".claude/rules/only-b.md = %q, want %q", got, "b rule\n")
	}
	wantLock := "lockfile_version: '1'\n" +
		"dependencies:\n" +
		"- repo_url: _local/b\n" +
		"  name: b\n" +
		"  version: 1.0.0\n" +
		"  package_type: apm_package\n" +
		"  deployed_files:\n" +
		"  - .claude/rules/only-b.md\n" +
		"  deployed_file_hashes:\n" +
		"    .claude/rules/only-b.md: " + transferSHA("b rule\n") + "\n" +
		"  source: local\n" +
		"  local_path: ./b\n" +
		"deployments:\n" +
		"- kind: project-relative\n" +
		"  target: claude\n" +
		"  value: .claude/rules/only-b.md\n" +
		"  runtime: null\n" +
		"  scope: project\n" +
		"  owners:\n" +
		"  - ./b\n" +
		"  active_owner: ./b\n" +
		"  content_hash: " + transferSHA("b rule\n") + "\n"
	if got := globalStaleRead(t, filepath.Join(dir, "apm.lock.yaml")); got != wantLock {
		t.Errorf("apm.lock.yaml =\n%s\nwant\n%s", got, wantLock)
	}
}

func TestUninstall_SharedRuleOwner_NextInstallChangesNothing(t *testing.T) {
	dir := staleCleanupProject(t, transferManifest, nil)
	transferProject(t, dir, map[string]string{
		"a/.apm/instructions/shared.instructions.md": transferRule,
		"b/.apm/instructions/shared.instructions.md": transferRule,
	})
	staleCleanupInstall(t, "claude")
	transferUninstall(t, uninstallOptions{}, "./a")
	lockPath := filepath.Join(dir, "apm.lock.yaml")
	before := globalStaleRead(t, lockPath)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	out := captureUninstallStdout(t, func() {
		if err := runInstall(deps, false, true, "claude", "", nil, nil); err != nil {
			t.Fatalf("install: %v", err)
		}
	})

	assertTransferSteadyInstall(t, out, "  |-- (files unchanged)\n")
	if got := globalStaleRead(t, lockPath); got != before {
		t.Errorf("install rewrote the lock:\n%s\nwas\n%s", got, before)
	}
}

// transferBRule is b's rule when it differs from a's; its deployed form.
const transferBRule = "---\ndescription: shared\n---\nb rule\n"

// The project's .claude is the user's symlink to another directory. Deploy
// and removal walk through it, so the hand-over does too.
func TestUninstall_SharedRuleOwner_ClaudeDirIsSymlink_HandsFileOver(t *testing.T) {
	dir := staleCleanupProject(t, transferManifest, nil)
	transferProject(t, dir, map[string]string{
		"a/.apm/instructions/shared.instructions.md": transferRule,
		"b/.apm/instructions/shared.instructions.md": transferBRule,
	})
	shared := filepath.Join(t.TempDir(), "shared-claude")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(dir, ".claude")); err != nil {
		t.Fatal(err)
	}
	staleCleanupInstall(t, "claude")

	out := transferUninstall(t, uninstallOptions{}, "./a")

	if got := globalStaleRead(t, filepath.Join(shared, "rules", "shared.md")); got != "b rule\n" {
		t.Errorf("shared-claude/rules/shared.md = %q, want b's %q", got, "b rule\n")
	}
	assertTransferRecord(t, transferLockDep(t, filepath.Join(dir, "apm.lock.yaml"), "_local/b"),
		[]string{".claude/rules/shared.md"},
		map[string]string{".claude/rules/shared.md": transferSHA("b rule\n")})
	if strings.Contains(out, "Cleaned") {
		t.Errorf("the handed-over file is reported as cleaned:\n%s", out)
	}
}

func TestUninstallGlobal_SharedRuleOwner_ClaudeDirIsSymlink_HandsFileOver(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	home := t.TempDir()
	oldDir, oldDeploy := globalDirOverride, globalDeployDirOverride
	globalDirOverride, globalDeployDirOverride = filepath.Join(home, ".apm"), home
	t.Cleanup(func() { globalDirOverride, globalDeployDirOverride = oldDir, oldDeploy })
	transferProject(t, filepath.Join(home, ".apm"), map[string]string{
		"a/.apm/instructions/shared.instructions.md": transferRule,
		"b/.apm/instructions/shared.instructions.md": transferBRule,
	})
	dotfiles := filepath.Join(t.TempDir(), "dotfiles", "claude")
	if err := os.MkdirAll(dotfiles, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dotfiles, filepath.Join(home, ".claude")); err != nil {
		t.Fatal(err)
	}
	globalStaleInstall(t, "claude")

	out := transferUninstall(t, uninstallOptions{Global: true}, "./a")

	if got := globalStaleRead(t, filepath.Join(dotfiles, "rules", "shared.md")); got != "b rule\n" {
		t.Errorf("dotfiles/claude/rules/shared.md = %q, want b's %q", got, "b rule\n")
	}
	assertTransferRecord(t, transferLockDep(t, filepath.Join(home, ".apm", "apm.lock.yaml"), "_local/b"),
		[]string{".claude/rules/shared.md"},
		map[string]string{".claude/rules/shared.md": transferSHA("b rule\n")})
	if strings.Contains(out, "Cleaned") {
		t.Errorf("the handed-over file is reported as cleaned:\n%s", out)
	}
}

// apm.yml declares one repository twice. Install warns about it; the trial
// deploy of the uninstall does not repeat the warning.
func TestUninstall_SharedRuleOwner_DuplicateDeclaration_NoExtraWarning(t *testing.T) {
	dir := staleCleanupProject(t, transferManifest, nil)
	transferProject(t, dir, map[string]string{
		"a/.apm/instructions/shared.instructions.md": transferRule,
		"b/.apm/instructions/shared.instructions.md": transferRule,
	})
	staleCleanupInstall(t, "claude")
	writeGlobalStaleFile(t, filepath.Join(dir, "apm.yml"),
		transferManifest+"    - acme/pkg\n    - Acme/pkg\n")

	out := transferUninstall(t, uninstallOptions{}, "./a")

	if strings.Contains(out, "more than once") {
		t.Errorf("the uninstall warns about the duplicate declaration:\n%s", out)
	}
	if got := globalStaleRead(t, filepath.Join(dir, ".claude", "rules", "shared.md")); got != "shared rule\n" {
		t.Errorf(".claude/rules/shared.md = %q, want %q", got, "shared rule\n")
	}
}

const (
	transferBSkillMD = "---\nname: demo\ndescription: b demo skill\n---\n\n# B demo\n"
	transferBScript  = "print('b')\n"
)

// writeTransferFiles writes files (slash path below dir -> content).
func writeTransferFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		writeGlobalStaleFile(t, filepath.Join(dir, filepath.FromSlash(rel)), content)
	}
}

// transferSkillFiles gives a and b the skill demo, each with a resource of
// its own.
var transferSkillFiles = map[string]string{
	"a/.apm/skills/demo/SKILL.md":     staleCleanupSkillMD,
	"a/.apm/skills/demo/a.txt":        "a\n",
	"b/.apm/skills/demo/SKILL.md":     transferBSkillMD,
	"b/.apm/skills/demo/scripts/b.py": transferBScript,
}

// A skill is handed over as a whole directory: b's own resource comes with
// it, and a's own resource goes.
func TestUninstall_SharedSkillOwner_HandsWholeSkillDirToSurvivor(t *testing.T) {
	dir := staleCleanupProject(t, transferManifest, nil)
	transferProject(t, dir, transferSkillFiles)
	staleCleanupInstall(t, "claude")

	transferUninstall(t, uninstallOptions{}, "./a")

	skill := filepath.Join(dir, ".claude", "skills", "demo")
	if got := globalStaleRead(t, filepath.Join(skill, "SKILL.md")); got != transferBSkillMD {
		t.Errorf("SKILL.md = %q, want b's %q", got, transferBSkillMD)
	}
	if got := globalStaleRead(t, filepath.Join(skill, "scripts", "b.py")); got != transferBScript {
		t.Errorf("scripts/b.py = %q, want %q", got, transferBScript)
	}
	if _, err := os.Lstat(filepath.Join(skill, "a.txt")); !os.IsNotExist(err) {
		t.Errorf("a's a.txt is still there (Lstat err = %v)", err)
	}
	lockPath := filepath.Join(dir, "apm.lock.yaml")
	assertTransferRecord(t, transferLockDep(t, lockPath, "_local/b"),
		[]string{".claude/skills/demo", ".claude/skills/demo/SKILL.md", ".claude/skills/demo/scripts/b.py"},
		map[string]string{
			".claude/skills/demo/SKILL.md":     transferSHA(transferBSkillMD),
			".claude/skills/demo/scripts/b.py": transferSHA(transferBScript),
		})

	before := globalStaleRead(t, lockPath)
	staleCleanupInstall(t, "claude")
	if got := globalStaleRead(t, lockPath); got != before {
		t.Errorf("install after the uninstall rewrote the lock:\n%s\nwas\n%s", got, before)
	}
}

func TestUninstallGlobal_SharedSkillOwner_RecordsSurvivorResources(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	home := t.TempDir()
	oldDir, oldDeploy := globalDirOverride, globalDeployDirOverride
	globalDirOverride, globalDeployDirOverride = filepath.Join(home, ".apm"), home
	t.Cleanup(func() { globalDirOverride, globalDeployDirOverride = oldDir, oldDeploy })
	transferProject(t, filepath.Join(home, ".apm"), transferSkillFiles)
	globalStaleInstall(t, "claude")
	lockPath := filepath.Join(home, ".apm", "apm.lock.yaml")

	transferUninstall(t, uninstallOptions{Global: true}, "./a")

	if got := globalStaleRead(t, filepath.Join(home, ".claude", "skills", "demo", "scripts", "b.py")); got != transferBScript {
		t.Errorf("~/.claude/skills/demo/scripts/b.py = %q, want %q", got, transferBScript)
	}
	assertTransferRecord(t, transferLockDep(t, lockPath, "_local/b"),
		[]string{".claude/skills/demo", ".claude/skills/demo/SKILL.md", ".claude/skills/demo/scripts/b.py"},
		map[string]string{
			".claude/skills/demo/SKILL.md":     transferSHA(transferBSkillMD),
			".claude/skills/demo/scripts/b.py": transferSHA(transferBScript),
		})

	before := globalStaleRead(t, lockPath)
	globalStaleInstall(t, "claude")
	if got := globalStaleRead(t, lockPath); got != before {
		t.Errorf("install after the uninstall rewrote the lock:\n%s\nwas\n%s", got, before)
	}
}

// The first surviving declaration takes the path, as install decides it,
// whatever the package names sort to.
func TestUninstall_SharedRuleOwner_FirstSurvivingDeclarationWins(t *testing.T) {
	const manifestYAML = "name: app\nversion: 1.0.0\ndependencies:\n  apm:\n    - ./a\n    - ./c\n    - ./b\n"
	dir := staleCleanupProject(t, manifestYAML, nil)
	files := map[string]string{}
	for _, p := range []string{"a", "c", "b"} {
		files[p+"/apm.yml"] = "name: " + p + "\nversion: 1.0.0\n"
		files[p+"/.apm/instructions/shared.instructions.md"] = "---\ndescription: shared\n---\n" + p + " rule\n"
	}
	writeTransferFiles(t, dir, files)
	staleCleanupInstall(t, "claude")

	transferUninstall(t, uninstallOptions{}, "./a")

	rule := filepath.Join(dir, ".claude", "rules", "shared.md")
	if got := globalStaleRead(t, rule); got != "c rule\n" {
		t.Errorf(".claude/rules/shared.md = %q, want c's %q", got, "c rule\n")
	}
	lockPath := filepath.Join(dir, "apm.lock.yaml")
	assertTransferRecord(t, transferLockDep(t, lockPath, "_local/c"),
		[]string{".claude/rules/shared.md"},
		map[string]string{".claude/rules/shared.md": transferSHA("c rule\n")})

	before := globalStaleRead(t, lockPath)
	staleCleanupInstall(t, "claude")
	if got := globalStaleRead(t, rule); got != "c rule\n" {
		t.Errorf("install after the uninstall changed the rule to %q", got)
	}
	if got := globalStaleRead(t, lockPath); got != before {
		t.Errorf("install after the uninstall rewrote the lock:\n%s\nwas\n%s", got, before)
	}
}

// b keeps only the skill its apm.yml entry names; the skill it shares with a
// is not handed to it.
func TestUninstall_SharedSkillOwner_SurvivorSkillSubsetHonored(t *testing.T) {
	const manifestYAML = "name: app\nversion: 1.0.0\ndependencies:\n  apm:\n    - ./a\n    - path: ./b\n      skills: [only-b]\n"
	dir := staleCleanupProject(t, manifestYAML, nil)
	onlyB := "---\nname: only-b\ndescription: only b\n---\n"
	writeTransferFiles(t, dir, map[string]string{
		"a/apm.yml":                     "name: a\nversion: 1.0.0\n",
		"b/apm.yml":                     "name: b\nversion: 1.0.0\n",
		"a/.apm/skills/demo/SKILL.md":   staleCleanupSkillMD,
		"b/.apm/skills/demo/SKILL.md":   transferBSkillMD,
		"b/.apm/skills/only-b/SKILL.md": onlyB,
	})
	staleCleanupInstall(t, "claude")

	transferUninstall(t, uninstallOptions{}, "./a")

	if _, err := os.Lstat(filepath.Join(dir, ".claude", "skills", "demo")); !os.IsNotExist(err) {
		t.Errorf(".claude/skills/demo is still there (Lstat err = %v)", err)
	}
	assertTransferRecord(t, transferLockDep(t, filepath.Join(dir, "apm.lock.yaml"), "_local/b"),
		[]string{".claude/skills/only-b", ".claude/skills/only-b/SKILL.md"},
		map[string]string{".claude/skills/only-b/SKILL.md": transferSHA(onlyB)})
}

// A lock written before issue #50 names a local package _local/<name>-<hash>,
// and the package is in that directory. The hand-over reads the packages
// under those keys, in the order apm.yml declares them, and records the file
// under c's key.
func TestUninstall_SharedRuleOwner_LegacyLockKeys(t *testing.T) {
	const manifestYAML = "name: app\nversion: 1.0.0\ndependencies:\n  apm:\n    - ./a\n    - ./c\n    - ./b\n"
	dir := staleCleanupProject(t, manifestYAML, nil)
	files := map[string]string{}
	for _, p := range []string{"a", "c", "b"} {
		files[p+"/apm.yml"] = "name: " + p + "\nversion: 1.0.0\n"
		files[p+"/.apm/instructions/shared.instructions.md"] = "---\ndescription: shared\n---\n" + p + " rule\n"
	}
	writeTransferFiles(t, dir, files)
	staleCleanupInstall(t, "claude")
	lockPath := filepath.Join(dir, "apm.lock.yaml")
	lock := globalStaleRead(t, lockPath)
	keys := map[string]string{}
	for _, p := range []string{"a", "c", "b"} {
		legacy := legacyLocalModulesKey(filepath.Join(dir, p))
		keys[p] = legacy
		if err := os.Rename(filepath.Join(dir, "apm_modules", "_local", p), filepath.Join(dir, "apm_modules", filepath.FromSlash(legacy))); err != nil {
			t.Fatal(err)
		}
		lock = strings.Replace(lock, "repo_url: _local/"+p+"\n", "repo_url: "+legacy+"\n", 1)
	}
	if err := os.WriteFile(lockPath, []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}

	transferUninstall(t, uninstallOptions{}, "./a")

	if got := globalStaleRead(t, filepath.Join(dir, ".claude", "rules", "shared.md")); got != "c rule\n" {
		t.Errorf(".claude/rules/shared.md = %q, want c's %q", got, "c rule\n")
	}
	assertTransferRecord(t, transferLockDep(t, lockPath, keys["c"]),
		[]string{".claude/rules/shared.md"},
		map[string]string{".claude/rules/shared.md": transferSHA("c rule\n")})
}

// transferBlockLines returns the stdout lines of an uninstall between the
// "Updated <apm.yml>" line and the "Uninstall complete" line.
func transferBlockLines(t *testing.T, out string) string {
	t.Helper()
	start := strings.Index(out, " + Updated ")
	end := strings.Index(out, " + Uninstall complete: ")
	if start < 0 || end < start {
		t.Fatalf("no Updated / Uninstall complete lines:\n%s", out)
	}
	after := out[start:end]
	return after[strings.Index(after, "\n")+1:]
}

// The lines are the Oracle's for the same steps (v0.33.0): the install block
// of each package that received a path, without its header.
func TestUninstall_SharedRuleOwner_PrintsSurvivorBlock(t *testing.T) {
	dir := staleCleanupProject(t, transferManifest, nil)
	transferProject(t, dir, map[string]string{
		"a/.apm/instructions/shared.instructions.md": transferRule,
		"b/.apm/instructions/shared.instructions.md": transferRule,
	})
	staleCleanupInstall(t, "claude")

	out := transferUninstall(t, uninstallOptions{}, "./a")

	want := "  |-- 1 rule(s) adopted -> .claude/rules/\n  |-- (files unchanged)\n"
	if got := transferBlockLines(t, out); got != want {
		t.Errorf("lines before Uninstall complete =\n%q\nwant\n%q\n%s", got, want, out)
	}
}

// The block counts every file the survivor has, not only the handed-over one.
func TestUninstall_SharedRuleOwner_BlockCountsSurvivorsOwnFiles(t *testing.T) {
	dir := staleCleanupProject(t, transferManifest, nil)
	transferProject(t, dir, map[string]string{
		"a/.apm/instructions/shared.instructions.md": transferRule,
		"b/.apm/instructions/shared.instructions.md": transferRule,
		"b/.apm/instructions/own.instructions.md":    "---\ndescription: own\n---\nb own\n",
	})
	staleCleanupInstall(t, "claude")

	out := transferUninstall(t, uninstallOptions{}, "./a")

	want := "  |-- 2 rule(s) adopted -> .claude/rules/\n  |-- (files unchanged)\n"
	if got := transferBlockLines(t, out); got != want {
		t.Errorf("lines before Uninstall complete =\n%q\nwant\n%q", got, want)
	}
}

// A skill is counted integrated on every run, so there is no
// "(files unchanged)" line.
func TestUninstall_SharedSkillOwner_PrintsSurvivorBlock(t *testing.T) {
	dir := staleCleanupProject(t, transferManifest, nil)
	transferProject(t, dir, map[string]string{
		"a/.apm/skills/demo/SKILL.md": staleCleanupSkillMD,
		"b/.apm/skills/demo/SKILL.md": staleCleanupSkillMD,
	})
	staleCleanupInstall(t, "claude")

	out := transferUninstall(t, uninstallOptions{}, "./a")

	want := "  |-- 1 skill(s) integrated -> .claude/skills/\n"
	if got := transferBlockLines(t, out); got != want {
		t.Errorf("lines before Uninstall complete =\n%q\nwant\n%q", got, want)
	}
}

func TestUninstallGlobal_SharedRuleAndSkillOwner_PrintsSurvivorBlock(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	home := t.TempDir()
	oldDir, oldDeploy := globalDirOverride, globalDeployDirOverride
	globalDirOverride, globalDeployDirOverride = filepath.Join(home, ".apm"), home
	t.Cleanup(func() { globalDirOverride, globalDeployDirOverride = oldDir, oldDeploy })
	transferProject(t, filepath.Join(home, ".apm"), map[string]string{
		"a/.apm/instructions/shared.instructions.md": transferRule,
		"b/.apm/instructions/shared.instructions.md": transferRule,
		"a/.apm/skills/demo/SKILL.md":                staleCleanupSkillMD,
		"b/.apm/skills/demo/SKILL.md":                staleCleanupSkillMD,
	})
	globalStaleInstall(t, "claude")

	out := transferUninstall(t, uninstallOptions{Global: true}, "./a")

	want := "  |-- 1 rule(s) adopted -> .claude/rules/\n  |-- 1 skill(s) integrated -> .claude/skills/\n"
	if got := transferBlockLines(t, out); got != want {
		t.Errorf("lines before Uninstall complete =\n%q\nwant\n%q", got, want)
	}
}
