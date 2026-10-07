package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
)

// `update` shares deployAndFinalize with `install`, so it must clean stale
// deployed files the same way (issue #30).

// updateStaleCleanupProject creates a claude project with one local-path
// dependency (skill "demo") and one local skill ("own"), installs it, and
// returns the dependency's lock key.
func updateStaleCleanupProject(t *testing.T) string {
	t.Helper()
	manifestYAML := "name: p\nversion: 1.0.0\ntargets:\n  - claude\ndependencies:\n  apm:\n    - ./vendor/dep\n  mcp: []\n"
	staleCleanupProject(t, manifestYAML, map[string]string{
		"vendor/dep/apm.yml":                          "name: dep\nversion: 1.0.0\n",
		"vendor/dep/.apm/skills/demo/SKILL.md":        staleCleanupSkillMD,
		"vendor/dep/.apm/skills/demo/extra/keep.md":   "one\n",
		"vendor/dep/.apm/skills/demo/extra/remove.md": "two\n",
		".apm/skills/own/SKILL.md":                    "---\nname: own\ndescription: own skill\n---\n\n# Own\n",
		".apm/skills/own/extra/keep.md":               "three\n",
		".apm/skills/own/extra/remove.md":             "four\n",
	})
	staleCleanupInstall(t, "")
	assertStaleCleanupExists(t, true,
		".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra/keep.md", ".claude/skills/demo/extra/remove.md",
		".claude/skills/own/SKILL.md", ".claude/skills/own/extra/keep.md", ".claude/skills/own/extra/remove.md")
	return localModulesKey(resolveLocalSourceAbs("./vendor/dep"))
}

// staleCleanupUpdate runs `update` and returns its stdout, which also carries
// ux.Warn output.
func staleCleanupUpdate(t *testing.T) string {
	t.Helper()
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	return captureUninstallStdout(t, func() {
		if err := runUpdate(deps, false, true, "", false); err != nil {
			t.Fatalf("update: %v", err)
		}
	})
}

func TestRunUpdate_StaleCleanup_DependencyFileRemovedFromSource(t *testing.T) {
	depKey := updateStaleCleanupProject(t)
	const deployed = ".claude/skills/demo/extra/remove.md"
	assertDepLockLists(t, depKey, deployed)

	staleCleanupRemove(t, "vendor/dep/.apm/skills/demo/extra/remove.md")
	stdout := staleCleanupUpdate(t)

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
	if !strings.Contains(stdout, "Cleaned 1 stale file from "+depKey+"\n  - "+deployed+"\n") {
		t.Errorf("stdout must report the cleanup of %s and list %s, got:\n%s", depKey, deployed, stdout)
	}
}

func TestRunUpdate_StaleCleanup_LocalFileRemovedFromSource(t *testing.T) {
	updateStaleCleanupProject(t)
	const deployed = ".claude/skills/own/extra/remove.md"
	if lock := readLockfile(t); !staleCleanupContains(lock.LocalDeployedFiles, deployed) {
		t.Fatalf("install: lock must list %s, got %v", deployed, lock.LocalDeployedFiles)
	}

	staleCleanupRemove(t, ".apm/skills/own/extra/remove.md")
	stdout := staleCleanupUpdate(t)

	assertStaleCleanupExists(t, false, deployed)
	assertStaleCleanupExists(t, true, ".claude/skills/own/SKILL.md", ".claude/skills/own/extra/keep.md")
	lock := readLockfile(t)
	if staleCleanupContains(lock.LocalDeployedFiles, deployed) {
		t.Errorf("local_deployed_files still lists %s", deployed)
	}
	if _, ok := lock.LocalDeployedHashes[deployed]; ok {
		t.Errorf("local_deployed_file_hashes still lists %s", deployed)
	}
	if !strings.Contains(stdout, "Cleaned 1 stale file from <local .apm/>\n  - "+deployed+"\n") {
		t.Errorf("stdout must report the cleanup and list %s, got:\n%s", deployed, stdout)
	}
}

func TestRunUpdate_StaleCleanup_KeepsUserEditedCopy(t *testing.T) {
	depKey := updateStaleCleanupProject(t)
	const deployed = ".claude/skills/demo/extra/remove.md"
	const edited = "edited by the user\n"
	writeStaleCleanupFile(t, deployed, edited)

	staleCleanupRemove(t, "vendor/dep/.apm/skills/demo/extra/remove.md")
	stdout := staleCleanupUpdate(t)

	data, err := os.ReadFile(filepath.FromSlash(deployed))
	if err != nil {
		t.Fatalf("user-edited copy must be kept: %v", err)
	}
	if string(data) != edited {
		t.Errorf("user-edited copy content = %q, want %q", data, edited)
	}
	assertDepLockLists(t, depKey, deployed)
	if !strings.Contains(stdout, `keeping ".claude/skills/demo/extra/remove.md": modified since deploy (hash mismatch)`) {
		t.Errorf("stdout must warn about the kept file, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "Cleaned") {
		t.Errorf("stdout must not report a cleanup, got:\n%s", stdout)
	}
}
