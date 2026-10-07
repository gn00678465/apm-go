package main

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/manifest"
)

// Issue #30: a file deleted from the source must lose its deployed copy and
// its lock record on the next install. The expected "Cleaned ..." sentences
// are the oracle's (src/apm_cli/core/command_logger.py:482-496).

const staleCleanupSkillMD = "---\nname: demo\ndescription: demo skill\n---\n\n# Demo\n"

// staleCleanupProject creates a project in a fresh temp directory with the
// given apm.yml and files (slash-separated relative path -> content).
func staleCleanupProject(t *testing.T, manifestYAML string, files map[string]string) string {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	dir := chdirTemp(t)
	writeStaleCleanupFile(t, "apm.yml", manifestYAML)
	for rel, content := range files {
		writeStaleCleanupFile(t, rel, content)
	}
	return dir
}

func writeStaleCleanupFile(t *testing.T, rel, content string) {
	t.Helper()
	full := filepath.FromSlash(rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// staleCleanupInstall runs `install` (with -t target when target is not
// empty) and returns its stdout, which also carries ux.Warn output.
func staleCleanupInstall(t *testing.T, target string) string {
	t.Helper()
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	return captureUninstallStdout(t, func() {
		if err := runInstall(deps, false, true, target, "", nil, nil); err != nil {
			t.Fatalf("install: %v", err)
		}
	})
}

func staleCleanupExists(rel string) bool {
	_, err := os.Stat(filepath.FromSlash(rel))
	return err == nil
}

func staleCleanupRemove(t *testing.T, rel string) {
	t.Helper()
	if err := os.Remove(filepath.FromSlash(rel)); err != nil {
		t.Fatal(err)
	}
}

func assertStaleCleanupExists(t *testing.T, want bool, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		if got := staleCleanupExists(rel); got != want {
			t.Errorf("%s exists = %v, want %v", rel, got, want)
		}
	}
}

func staleCleanupContains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func localSkillFiles() map[string]string {
	return map[string]string{
		".apm/skills/demo/SKILL.md":        staleCleanupSkillMD,
		".apm/skills/demo/extra/keep.md":   "one\n",
		".apm/skills/demo/extra/remove.md": "two\n",
	}
}

const staleCleanupClaudeManifest = "name: repro\nversion: 1.0.0\ntargets:\n  - claude\ndependencies:\n  apm: []\n  mcp: []\nincludes: auto\n"

func TestInstall_StaleCleanup_LocalSkillFileRemovedFromSource(t *testing.T) {
	staleCleanupProject(t, staleCleanupClaudeManifest, localSkillFiles())

	staleCleanupInstall(t, "")
	const deployed = ".claude/skills/demo/extra/remove.md"
	assertStaleCleanupExists(t, true, deployed)
	if lock := readLockfile(t); !staleCleanupContains(lock.LocalDeployedFiles, deployed) {
		t.Fatalf("first install: lock must list %s, got %v", deployed, lock.LocalDeployedFiles)
	}

	staleCleanupRemove(t, ".apm/skills/demo/extra/remove.md")
	stdout := staleCleanupInstall(t, "")

	assertStaleCleanupExists(t, false, deployed)
	assertStaleCleanupExists(t, true, ".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra/keep.md")
	lock := readLockfile(t)
	if staleCleanupContains(lock.LocalDeployedFiles, deployed) {
		t.Errorf("local_deployed_files still lists %s", deployed)
	}
	if _, ok := lock.LocalDeployedHashes[deployed]; ok {
		t.Errorf("local_deployed_file_hashes still lists %s", deployed)
	}
	if !strings.Contains(stdout, "Cleaned 1 stale file from <local .apm/>") {
		t.Errorf("stdout must report the cleanup, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Cleaned 1 stale file from <local .apm/>\n  - .claude/skills/demo/extra/remove.md\n") {
		t.Errorf("stdout must list the removed path under the Cleaned line, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "Already up to date") {
		t.Errorf("stdout must not say Already up to date, got:\n%s", stdout)
	}
}

func TestInstall_StaleCleanup_RemovesCopyFromEveryTarget(t *testing.T) {
	manifestYAML := "name: repro\nversion: 1.0.0\ntargets:\n  - claude\n  - codex\ndependencies:\n  apm: []\n  mcp: []\nincludes: auto\n"
	staleCleanupProject(t, manifestYAML, localSkillFiles())

	staleCleanupInstall(t, "")
	copies := []string{".claude/skills/demo/extra/remove.md", ".agents/skills/demo/extra/remove.md"}
	assertStaleCleanupExists(t, true, copies...)

	staleCleanupRemove(t, ".apm/skills/demo/extra/remove.md")
	stdout := staleCleanupInstall(t, "")

	assertStaleCleanupExists(t, false, copies...)
	assertStaleCleanupExists(t, true, ".claude/skills/demo/extra/keep.md", ".agents/skills/demo/extra/keep.md")
	lock := readLockfile(t)
	for _, p := range copies {
		if staleCleanupContains(lock.LocalDeployedFiles, p) {
			t.Errorf("local_deployed_files still lists %s", p)
		}
	}
	if !strings.Contains(stdout, "Cleaned 2 stale files from <local .apm/>") {
		t.Errorf("stdout must report both removed copies, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Cleaned 2 stale files from <local .apm/>\n  - .agents/skills/demo/extra/remove.md\n  - .claude/skills/demo/extra/remove.md\n") {
		t.Errorf("stdout must list both removed paths in sorted order under the Cleaned line, got:\n%s", stdout)
	}
}

func TestInstall_StaleCleanup_DependencyFileRemovedFromSource(t *testing.T) {
	manifestYAML := "name: repro\nversion: 1.0.0\ntargets:\n  - claude\ndependencies:\n  apm:\n    - ./vendor/dep\n"
	staleCleanupProject(t, manifestYAML, map[string]string{
		"vendor/dep/apm.yml":                          "name: dep\nversion: 1.0.0\n",
		"vendor/dep/.apm/skills/demo/SKILL.md":        staleCleanupSkillMD,
		"vendor/dep/.apm/skills/demo/extra/keep.md":   "one\n",
		"vendor/dep/.apm/skills/demo/extra/remove.md": "two\n",
	})
	depKey := localModulesKey(resolveLocalSourceAbs("./vendor/dep"))

	staleCleanupInstall(t, "")
	const deployed = ".claude/skills/demo/extra/remove.md"
	assertStaleCleanupExists(t, true, deployed)
	old := readLockfile(t).FindByKey(depKey)
	if old == nil || !staleCleanupContains(old.DeployedFiles, deployed) {
		t.Fatalf("first install: lock entry %s must list %s, got %+v", depKey, deployed, old)
	}

	staleCleanupRemove(t, "vendor/dep/.apm/skills/demo/extra/remove.md")
	stdout := staleCleanupInstall(t, "")

	assertStaleCleanupExists(t, false, deployed)
	assertStaleCleanupExists(t, true, ".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra/keep.md")
	dep := readLockfile(t).FindByKey(depKey)
	if dep == nil {
		t.Fatalf("lock entry %s is gone", depKey)
	}
	if staleCleanupContains(dep.DeployedFiles, deployed) {
		t.Errorf("deployed_files still lists %s", deployed)
	}
	if _, ok := dep.DeployedHashes[deployed]; ok {
		t.Errorf("deployed_file_hashes still lists %s", deployed)
	}
	if !strings.Contains(stdout, "Cleaned 1 stale file from "+depKey) {
		t.Errorf("stdout must report the cleanup for %s, got:\n%s", depKey, stdout)
	}
	if strings.Contains(stdout, "Already up to date") {
		t.Errorf("stdout must not say Already up to date, got:\n%s", stdout)
	}
}

func TestInstall_StaleCleanup_KeepsUserEditedCopy(t *testing.T) {
	staleCleanupProject(t, staleCleanupClaudeManifest, localSkillFiles())
	staleCleanupInstall(t, "")

	const deployed = ".claude/skills/demo/extra/remove.md"
	const edited = "edited by the user\n"
	writeStaleCleanupFile(t, deployed, edited)
	staleCleanupRemove(t, ".apm/skills/demo/extra/remove.md")
	stdout := staleCleanupInstall(t, "")

	data, err := os.ReadFile(filepath.FromSlash(deployed))
	if err != nil {
		t.Fatalf("user-edited copy must be kept: %v", err)
	}
	if string(data) != edited {
		t.Errorf("user-edited copy content = %q, want %q", data, edited)
	}
	lock := readLockfile(t)
	if !staleCleanupContains(lock.LocalDeployedFiles, deployed) {
		t.Errorf("lock must still list the kept file %s, got %v", deployed, lock.LocalDeployedFiles)
	}
	if !strings.Contains(stdout, `keeping ".claude/skills/demo/extra/remove.md": modified since deploy (hash mismatch)`) {
		t.Errorf("stdout must warn about the kept file, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "Cleaned") {
		t.Errorf("stdout must not report a cleanup, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "  - "+deployed) {
		t.Errorf("stdout must not list the kept file as removed, got:\n%s", stdout)
	}
}

func TestInstall_StaleCleanup_KeepsFileTheLockDoesNotRecord(t *testing.T) {
	staleCleanupProject(t, staleCleanupClaudeManifest, localSkillFiles())
	staleCleanupInstall(t, "")

	const own = ".claude/skills/demo/extra/mine.md"
	writeStaleCleanupFile(t, own, "the user's own file\n")
	staleCleanupRemove(t, ".apm/skills/demo/extra/remove.md")
	staleCleanupInstall(t, "")

	assertStaleCleanupExists(t, true, own)
	assertStaleCleanupExists(t, false, ".claude/skills/demo/extra/remove.md")
}

func TestInstall_StaleCleanup_TargetChangeKeepsOtherTargetFiles(t *testing.T) {
	manifestYAML := "name: repro\nversion: 1.0.0\ndependencies:\n  apm: []\n  mcp: []\nincludes: auto\n"
	staleCleanupProject(t, manifestYAML, localSkillFiles())
	staleCleanupInstall(t, "claude")

	stdout := staleCleanupInstall(t, "codex")

	claudeFiles := []string{".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra/keep.md", ".claude/skills/demo/extra/remove.md"}
	assertStaleCleanupExists(t, true, claudeFiles...)
	assertStaleCleanupExists(t, true, ".agents/skills/demo/SKILL.md")
	lock := readLockfile(t)
	for _, p := range claudeFiles {
		if !staleCleanupContains(lock.LocalDeployedFiles, p) {
			t.Errorf("lock must still list %s after a target change", p)
		}
		if _, ok := lock.LocalDeployedHashes[p]; !ok {
			t.Errorf("lock must still record the hash of %s after a target change", p)
		}
	}
	if strings.Contains(stdout, "Cleaned") {
		t.Errorf("a target change must not clean anything, got:\n%s", stdout)
	}
}

func TestInstall_StaleCleanup_UnchangedSourceIsUpToDate(t *testing.T) {
	staleCleanupProject(t, staleCleanupClaudeManifest, localSkillFiles())
	staleCleanupInstall(t, "")
	before, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}

	stdout := staleCleanupInstall(t, "")

	if !strings.Contains(stdout, "Already up to date") {
		t.Errorf("stdout must say Already up to date, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "Cleaned") {
		t.Errorf("stdout must not report a cleanup, got:\n%s", stdout)
	}
	after, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("apm.lock.yaml changed on a no-op install:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// The MCP config file of a target this run governs is recorded in the old
// lock and, once the last server is gone from apm.yml, no longer claimed by
// the new one. It holds the user's own entries too and must survive.
func TestInstall_StaleCleanup_KeepsMCPConfigFiles(t *testing.T) {
	const withServer = "name: repro\nversion: 1.0.0\ntargets:\n  - claude\n  - codex\ndependencies:\n  mcp:\n    - name: e2e-server\n      registry: false\n      transport: stdio\n      command: my-mcp-server\n"
	const withoutServer = "name: repro\nversion: 1.0.0\ntargets:\n  - claude\n  - codex\ndependencies:\n  mcp: []\n"
	staleCleanupProject(t, withServer, localSkillFiles())
	staleCleanupInstall(t, "")
	mcpFiles := []string{".mcp.json", ".codex/config.toml"}
	assertStaleCleanupExists(t, true, mcpFiles...)
	lock := readLockfile(t)
	for _, p := range mcpFiles {
		if !staleCleanupContains(lock.LocalDeployedFiles, p) {
			t.Fatalf("first install: lock must list %s, got %v", p, lock.LocalDeployedFiles)
		}
	}

	stdout := staleCleanupInstall(t, "")
	assertStaleCleanupExists(t, true, mcpFiles...)
	if strings.Contains(stdout, "Cleaned") {
		t.Errorf("a re-install must not clean anything, got:\n%s", stdout)
	}

	writeStaleCleanupFile(t, "apm.yml", withoutServer)
	stdout = staleCleanupInstall(t, "")
	assertStaleCleanupExists(t, true, mcpFiles...)
	if strings.Contains(stdout, "Cleaned") {
		t.Errorf("removing the MCP server must not clean its config file, got:\n%s", stdout)
	}
}

// depSkillProject creates a project whose only content is the local-path
// dependency ./vendor/dep with one skill, and returns the dependency's lock
// key. files are relative to the skill directory.
func depSkillProject(t *testing.T, skill string, files map[string]string) string {
	t.Helper()
	tree := map[string]string{"vendor/dep/apm.yml": "name: dep\nversion: 1.0.0\n"}
	for rel, content := range files {
		tree["vendor/dep/.apm/skills/"+skill+"/"+rel] = content
	}
	staleCleanupProject(t, "name: p\nversion: 1.0.0\ndependencies:\n  apm:\n    - ./vendor/dep\n  mcp: []\n", tree)
	return localModulesKey(resolveLocalSourceAbs("./vendor/dep"))
}

func assertDepLockLists(t *testing.T, depKey string, rels ...string) {
	t.Helper()
	dep := readLockfile(t).FindByKey(depKey)
	if dep == nil {
		t.Fatalf("lock entry %s is gone", depKey)
	}
	for _, rel := range rels {
		if !staleCleanupContains(dep.DeployedFiles, rel) {
			t.Errorf("deployed_files of %s must list %s, got %v", depKey, rel, dep.DeployedFiles)
		}
		if _, ok := dep.DeployedHashes[rel]; !ok {
			t.Errorf("deployed_file_hashes of %s must list %s", depKey, rel)
		}
	}
}

// antigravity writes a dependency only under .agents/plugins/<pkg>/, so the
// dependency's .agents/skills/ copy belongs to codex and must survive.
func TestInstall_StaleCleanup_AntigravityKeepsCodexDependencySkill(t *testing.T) {
	depKey := depSkillProject(t, "ds", map[string]string{"SKILL.md": "---\nname: ds\ndescription: d\n---\n# D\n"})
	const codexCopy = ".agents/skills/ds/SKILL.md"
	staleCleanupInstall(t, "codex")
	assertStaleCleanupExists(t, true, codexCopy)

	stdout := staleCleanupInstall(t, "antigravity")

	assertStaleCleanupExists(t, true, codexCopy)
	assertDepLockLists(t, depKey, codexCopy)
	if strings.Contains(stdout, "Cleaned") {
		t.Errorf("a target change must not clean anything, got:\n%s", stdout)
	}
}

func TestInstall_StaleCleanup_CodexKeepsAntigravityDependencyBundle(t *testing.T) {
	depKey := depSkillProject(t, "ds", map[string]string{"SKILL.md": "---\nname: ds\ndescription: d\n---\n# D\n"})
	bundle := ".agents/plugins/" + path.Base(depKey)
	bundleFiles := []string{bundle + "/plugin.json", bundle + "/skills/ds/SKILL.md"}
	staleCleanupInstall(t, "antigravity")
	assertStaleCleanupExists(t, true, bundleFiles...)

	stdout := staleCleanupInstall(t, "codex")

	assertStaleCleanupExists(t, true, bundleFiles...)
	assertStaleCleanupExists(t, true, ".agents/skills/ds/SKILL.md")
	assertDepLockLists(t, depKey, bundleFiles...)
	if strings.Contains(stdout, "Cleaned") {
		t.Errorf("a target change must not clean anything, got:\n%s", stdout)
	}
}

func TestInstall_StaleCleanup_AntigravityDependencyFileRemovedFromSource(t *testing.T) {
	depKey := depSkillProject(t, "demo", map[string]string{
		"SKILL.md":        staleCleanupSkillMD,
		"extra/keep.md":   "one\n",
		"extra/remove.md": "two\n",
	})
	bundleSkill := ".agents/plugins/" + path.Base(depKey) + "/skills/demo"
	deployed := bundleSkill + "/extra/remove.md"
	staleCleanupInstall(t, "antigravity")
	assertStaleCleanupExists(t, true, deployed)

	staleCleanupRemove(t, "vendor/dep/.apm/skills/demo/extra/remove.md")
	stdout := staleCleanupInstall(t, "antigravity")

	assertStaleCleanupExists(t, false, deployed)
	assertStaleCleanupExists(t, true, bundleSkill+"/SKILL.md", bundleSkill+"/extra/keep.md")
	if dep := readLockfile(t).FindByKey(depKey); dep == nil || staleCleanupContains(dep.DeployedFiles, deployed) {
		t.Errorf("deployed_files of %s must not list %s, got %+v", depKey, deployed, dep)
	}
	if !strings.Contains(stdout, "Cleaned 1 stale file from "+depKey) {
		t.Errorf("stdout must report the cleanup for %s, got:\n%s", depKey, stdout)
	}
}

const staleCleanupDepSkillMD = "---\nname: demo\ndescription: d\n---\n# D\n"

func removeDepSkill(t *testing.T, skill string) {
	t.Helper()
	if err := os.RemoveAll(filepath.FromSlash("vendor/dep/.apm/skills/" + skill)); err != nil {
		t.Fatal(err)
	}
}

// A dependency whose last skill is deleted deploys 0 files; the oracle still
// records it (install/template.py:318) and cleans all of its old files.
func TestInstall_StaleCleanup_DependencyDeployingNothingIsCleaned(t *testing.T) {
	depKey := depSkillProject(t, "demo", map[string]string{"SKILL.md": staleCleanupDepSkillMD})
	const deployed = ".claude/skills/demo/SKILL.md"
	staleCleanupInstall(t, "claude")
	assertStaleCleanupExists(t, true, deployed)
	assertDepLockLists(t, depKey, deployed)

	removeDepSkill(t, "demo")
	stdout := staleCleanupInstall(t, "claude")

	assertStaleCleanupExists(t, false, deployed)
	dep := readLockfile(t).FindByKey(depKey)
	if dep == nil {
		t.Fatalf("lock entry %s is gone", depKey)
	}
	if staleCleanupContains(dep.DeployedFiles, deployed) {
		t.Errorf("deployed_files still lists %s", deployed)
	}
	if _, ok := dep.DeployedHashes[deployed]; ok {
		t.Errorf("deployed_file_hashes still lists %s", deployed)
	}
	if !strings.Contains(stdout, "Cleaned 1 stale file from "+depKey+"\n  - "+deployed+"\n") {
		t.Errorf("stdout must report the cleanup of %s and list %s, got:\n%s", depKey, deployed, stdout)
	}
	if strings.Contains(stdout, "Already up to date") {
		t.Errorf("stdout must not say Already up to date, got:\n%s", stdout)
	}
}

func TestInstall_StaleCleanup_DependencyDeployingNothingKeepsUserEditedCopy(t *testing.T) {
	depKey := depSkillProject(t, "demo", map[string]string{"SKILL.md": staleCleanupDepSkillMD})
	const deployed = ".claude/skills/demo/SKILL.md"
	staleCleanupInstall(t, "claude")

	const edited = "edited by the user\n"
	writeStaleCleanupFile(t, deployed, edited)
	removeDepSkill(t, "demo")
	stdout := staleCleanupInstall(t, "claude")

	data, err := os.ReadFile(filepath.FromSlash(deployed))
	if err != nil {
		t.Fatalf("user-edited copy must be kept: %v", err)
	}
	if string(data) != edited {
		t.Errorf("user-edited copy content = %q, want %q", data, edited)
	}
	assertDepLockLists(t, depKey, deployed)
	if !strings.Contains(stdout, `keeping ".claude/skills/demo/SKILL.md": modified since deploy (hash mismatch)`) {
		t.Errorf("stdout must warn about the kept file, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "Cleaned") {
		t.Errorf("stdout must not report a cleanup, got:\n%s", stdout)
	}
}

func TestInstall_StaleCleanup_DependencyDeployingNothingKeepsOtherTargetFiles(t *testing.T) {
	depKey := depSkillProject(t, "demo", map[string]string{"SKILL.md": staleCleanupDepSkillMD})
	const deployed = ".claude/skills/demo/SKILL.md"
	staleCleanupInstall(t, "claude")

	removeDepSkill(t, "demo")
	stdout := staleCleanupInstall(t, "codex")

	assertStaleCleanupExists(t, true, deployed)
	assertDepLockLists(t, depKey, deployed)
	if strings.Contains(stdout, "Cleaned") {
		t.Errorf("a different target must not clean claude's files, got:\n%s", stdout)
	}
}

// blockDeployOf makes the next deploy of rel fail inside DeployPrimitive: a
// directory already sits at the destination, so the file cannot be written.
func blockDeployOf(t *testing.T, rel string) {
	t.Helper()
	if err := os.MkdirAll(filepath.FromSlash(rel), 0o755); err != nil {
		t.Fatal(err)
	}
}

// A primitive that fails to re-deploy is missing from this run's file list
// and would look stale (oracle: install/phases/post_deps_local.py:60-62).
func TestInstall_StaleCleanup_LocalDeployFailureKeepsDeployedFiles(t *testing.T) {
	staleCleanupProject(t, staleCleanupClaudeManifest, map[string]string{
		".apm/skills/demo/SKILL.md":   staleCleanupSkillMD,
		".apm/skills/demo/extra/a.md": "one\n",
	})
	staleCleanupInstall(t, "")
	deployed := []string{".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra/a.md"}
	assertStaleCleanupExists(t, true, deployed...)

	writeStaleCleanupFile(t, ".apm/skills/demo/extra/new.md", "two\n")
	blockDeployOf(t, ".claude/skills/demo/extra/new.md")
	stdout := staleCleanupInstall(t, "")

	assertStaleCleanupExists(t, true, deployed...)
	lock := readLockfile(t)
	for _, p := range deployed {
		if !staleCleanupContains(lock.LocalDeployedFiles, p) {
			t.Errorf("local_deployed_files must still list %s, got %v", p, lock.LocalDeployedFiles)
		}
	}
	if strings.Contains(stdout, "Cleaned") {
		t.Errorf("a failed deploy must not clean anything, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "deploy demo to claude failed") {
		t.Errorf("stdout must warn about the failed deploy, got:\n%s", stdout)
	}
}

// Oracle: install/phases/cleanup.py:148-152.
func TestInstall_StaleCleanup_DependencyDeployFailureKeepsDeployedFiles(t *testing.T) {
	depKey := depSkillProject(t, "demo", map[string]string{"SKILL.md": staleCleanupDepSkillMD, "extra/a.md": "one\n"})
	staleCleanupInstall(t, "claude")
	deployed := []string{".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra/a.md"}
	assertStaleCleanupExists(t, true, deployed...)

	writeStaleCleanupFile(t, "vendor/dep/.apm/skills/demo/extra/new.md", "two\n")
	blockDeployOf(t, ".claude/skills/demo/extra/new.md")
	stdout := staleCleanupInstall(t, "claude")

	assertStaleCleanupExists(t, true, deployed...)
	assertDepLockLists(t, depKey, deployed...)
	if strings.Contains(stdout, "Cleaned") {
		t.Errorf("a failed deploy must not clean anything, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "deploy demo to claude failed") {
		t.Errorf("stdout must warn about the failed deploy, got:\n%s", stdout)
	}
}

func TestInstall_StaleCleanup_DeployFailureInOneBucketDoesNotStopAnother(t *testing.T) {
	manifestYAML := "name: p\nversion: 1.0.0\ntargets:\n  - claude\ndependencies:\n  apm:\n    - ./vendor/dep\n  mcp: []\n"
	staleCleanupProject(t, manifestYAML, map[string]string{
		".apm/skills/demo/SKILL.md":                 staleCleanupSkillMD,
		".apm/skills/demo/extra/a.md":               "one\n",
		"vendor/dep/apm.yml":                        "name: dep\nversion: 1.0.0\n",
		"vendor/dep/.apm/skills/ds/SKILL.md":        "---\nname: ds\ndescription: d\n---\n# D\n",
		"vendor/dep/.apm/skills/ds/extra/remove.md": "two\n",
	})
	depKey := localModulesKey(resolveLocalSourceAbs("./vendor/dep"))
	staleCleanupInstall(t, "")
	local := []string{".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra/a.md"}
	const depStale = ".claude/skills/ds/extra/remove.md"
	assertStaleCleanupExists(t, true, append(local, depStale)...)

	writeStaleCleanupFile(t, ".apm/skills/demo/extra/new.md", "three\n")
	blockDeployOf(t, ".claude/skills/demo/extra/new.md")
	staleCleanupRemove(t, "vendor/dep/.apm/skills/ds/extra/remove.md")
	stdout := staleCleanupInstall(t, "")

	assertStaleCleanupExists(t, true, local...)
	assertStaleCleanupExists(t, false, depStale)
	if !strings.Contains(stdout, "Cleaned 1 stale file from "+depKey+"\n  - "+depStale+"\n") {
		t.Errorf("stdout must report the dependency's cleanup, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "<local .apm/>") {
		t.Errorf("the failed local bucket must not be cleaned, got:\n%s", stdout)
	}
}

// makeUnreadable removes every permission from rel until the test ends.
// root ignores permission bits, so the test is skipped for root.
func makeUnreadable(t *testing.T, rel string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permission bits do not restrict root")
	}
	full := filepath.FromSlash(rel)
	if err := os.Chmod(full, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(full, 0o755) })
}

// An unreadable source directory yields no primitives, exactly like a deleted
// one; its deployed copies must not be taken for stale.
func TestInstall_StaleCleanup_UnreadableLocalSourceKeepsDeployedFiles(t *testing.T) {
	staleCleanupProject(t, staleCleanupClaudeManifest, map[string]string{
		".apm/skills/demo/SKILL.md":      staleCleanupSkillMD,
		".apm/skills/demo/extra/keep.md": "k\n",
	})
	staleCleanupInstall(t, "")
	deployed := []string{".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra/keep.md"}
	assertStaleCleanupExists(t, true, deployed...)

	makeUnreadable(t, ".apm/skills")
	stdout := staleCleanupInstall(t, "")

	assertStaleCleanupExists(t, true, deployed...)
	lock := readLockfile(t)
	for _, p := range deployed {
		if !staleCleanupContains(lock.LocalDeployedFiles, p) {
			t.Errorf("local_deployed_files must still list %s, got %v", p, lock.LocalDeployedFiles)
		}
	}
	if strings.Contains(stdout, "Cleaned") {
		t.Errorf("an unreadable source must not clean anything, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "<local .apm/>: read .apm/skills failed: permission denied") {
		t.Errorf("stdout must warn about the unreadable source, got:\n%s", stdout)
	}
}

// loadOnceLoader loads each package through inner once and then reuses its
// module directory, as a loader does for a checkout it already has. The real
// loader copies a local-path dependency again on every install, and that copy
// aborts the install when the module directory is unreadable.
type loadOnceLoader struct {
	inner  *gitops.RealPackageLoader
	loaded map[string]*manifest.Manifest
}

func (l *loadOnceLoader) LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error) {
	if m, ok := l.loaded[ref.RepoURL]; ok {
		return m, nil
	}
	m, err := l.inner.LoadPackage(ref, resolvedRef)
	if err == nil {
		l.loaded[ref.RepoURL] = m
	}
	return m, err
}

func TestInstall_StaleCleanup_UnreadableDependencySourceKeepsDeployedFiles(t *testing.T) {
	depKey := depSkillProject(t, "demo", map[string]string{"SKILL.md": staleCleanupDepSkillMD, "extra/keep.md": "k\n"})
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &loadOnceLoader{
		inner:  &gitops.RealPackageLoader{ModulesDir: "apm_modules"},
		loaded: map[string]*manifest.Manifest{},
	}}
	install := func() string {
		return captureUninstallStdout(t, func() {
			if err := runInstall(deps, false, true, "claude", "", nil, nil); err != nil {
				t.Fatalf("install: %v", err)
			}
		})
	}
	install()
	deployed := []string{".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra/keep.md"}
	assertStaleCleanupExists(t, true, deployed...)

	makeUnreadable(t, "apm_modules/"+depKey+"/.apm/skills")
	stdout := install()

	assertStaleCleanupExists(t, true, deployed...)
	assertDepLockLists(t, depKey, deployed...)
	if strings.Contains(stdout, "Cleaned") {
		t.Errorf("an unreadable source must not clean anything, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, depKey+": read apm_modules/"+depKey+"/.apm/skills failed: permission denied") {
		t.Errorf("stdout must warn about the unreadable source of %s, got:\n%s", depKey, stdout)
	}
}

func TestInstall_StaleCleanup_MissingLocalSourceDirectoryIsCleaned(t *testing.T) {
	staleCleanupProject(t, staleCleanupClaudeManifest, map[string]string{
		".apm/skills/demo/SKILL.md":      staleCleanupSkillMD,
		".apm/skills/demo/extra/keep.md": "k\n",
	})
	staleCleanupInstall(t, "")
	deployed := []string{".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra/keep.md"}
	assertStaleCleanupExists(t, true, deployed...)

	if err := os.RemoveAll(filepath.FromSlash(".apm/skills")); err != nil {
		t.Fatal(err)
	}
	stdout := staleCleanupInstall(t, "")

	assertStaleCleanupExists(t, false, deployed...)
	if !strings.Contains(stdout, "Cleaned 2 stale files from <local .apm/>\n  - .claude/skills/demo/SKILL.md\n  - .claude/skills/demo/extra/keep.md\n") {
		t.Errorf("stdout must report the cleanup, got:\n%s", stdout)
	}
	if strings.Contains(stdout, " failed: ") {
		t.Errorf("a missing source directory is not a read failure, got:\n%s", stdout)
	}
}
