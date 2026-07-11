package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/semver"
)

// setupLocalDepUpdateFixture is the shared TDD fixture for
// 07-11-update-local-deps (F1 gap): a root project with an explicit
// target: claude and a single local dependency "./dep-pkg" carrying one
// instructions file and one agent file, installed once via the REAL loader
// (gitops.RealPackageLoader) so apm_modules/_local/... materialization and
// .claude/ deployment are genuine filesystem effects, not mocked -- update's
// content-refresh behavior can only be proven against real bytes on disk.
// Returns dir and the hashed local module key (_local/dep-pkg-<sha8>) the
// baseline install produced.
func setupLocalDepUpdateFixture(t *testing.T) (dir, localKey string) {
	t.Helper()
	dir = chdirTemp(t)

	writeLocalDepContent(t, "dep-pkg", "v1")

	if err := os.WriteFile("apm.yml", []byte(
		"name: test\nversion: \"1.0.0\"\ntarget:\n  - claude\ndependencies:\n  apm:\n    - ./dep-pkg\n",
	), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	if err := runInstall(deps, false, true, "", nil, nil); err != nil {
		t.Fatalf("baseline runInstall: %v", err)
	}

	localKey = localModulesKey(resolveLocalSourceAbs("./dep-pkg"))
	return dir, localKey
}

// writeLocalDepContent (re)writes relDir's own apm.yml, one instructions
// file and one agent file, content-tagged with token, so a test can prove
// materialized/deployed bytes actually refreshed across an update (rather
// than merely that the files still exist).
func writeLocalDepContent(t *testing.T, relDir, token string) {
	t.Helper()
	instrDir := filepath.Join(relDir, ".apm", "instructions")
	agentsDir := filepath.Join(relDir, ".apm", "agents")
	if err := os.MkdirAll(instrDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(relDir, "apm.yml"), []byte("name: dep-pkg\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instrDir, "style.instructions.md"), []byte("rule-"+token), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "helper.md"), []byte("agent-"+token), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestRunUpdate_LocalDep_MaterializesAndDeploys is this task's (F1 gap) core
// RED->GREEN case: before the fix, runUpdate never called normalizeLocalDep,
// so a local dep resolved in place (depKey == its bare LocalPath) and
// materializeLocalCopy never ran -- the module tree and .claude/ deployment
// silently kept their stale pre-update (v1) bytes. After the fix, `apm
// update` must materialize+deploy a changed local dep exactly like a fresh
// `apm install` would.
func TestRunUpdate_LocalDep_MaterializesAndDeploys(t *testing.T) {
	dir, localKey := setupLocalDepUpdateFixture(t)

	writeLocalDepContent(t, "dep-pkg", "v2")

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	if err := runUpdate(deps, false, true, ""); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}

	moduleAgentPath := filepath.Join(dir, "apm_modules", filepath.FromSlash(localKey), ".apm", "agents", "helper.md")
	moduleAgent, err := os.ReadFile(moduleAgentPath)
	if err != nil {
		t.Fatalf("expected %s to exist after update: %v", moduleAgentPath, err)
	}
	if !strings.Contains(string(moduleAgent), "agent-v2") {
		t.Errorf("materialized local module content not refreshed by update, got %q", moduleAgent)
	}

	deployedRule, err := os.ReadFile(filepath.Join(dir, ".claude", "rules", "style.md"))
	if err != nil {
		t.Fatalf("expected .claude/rules/style.md to exist after update: %v", err)
	}
	if !strings.Contains(string(deployedRule), "rule-v2") {
		t.Errorf("deployed rule content not refreshed by update, got %q", deployedRule)
	}

	deployedAgent, err := os.ReadFile(filepath.Join(dir, ".claude", "agents", "helper.md"))
	if err != nil {
		t.Fatalf("expected .claude/agents/helper.md to exist after update: %v", err)
	}
	if !strings.Contains(string(deployedAgent), "agent-v2") {
		t.Errorf("deployed agent content not refreshed by update, got %q", deployedAgent)
	}

	lock := readLockfile(t)
	entry := lock.FindByKey(localKey)
	if entry == nil {
		t.Fatalf("expected lockfile entry %q to survive update under its stable hashed key", localKey)
	}
	if len(entry.DeployedFiles) == 0 || len(entry.DeployedHashes) == 0 {
		t.Errorf("expected deployed_files/deployed_file_hashes to stay populated after update (findings C#2 regression), got Files=%v Hashes=%v", entry.DeployedFiles, entry.DeployedHashes)
	}
}

// TestRunUpdate_LocalDep_LockfileKeyStable_NoBarePathEntry guards against
// findings.md's C#2 breakage: pre-fix, PlanFullUpdate resolved the
// un-normalized local dep under its bare LocalPath ("./dep-pkg") key, and
// buildLockfile rewrote the existing "_local/dep-pkg-<sha8>" entry (with its
// deployed_files/deployed_file_hashes provenance) into a bare
// `repo_url: ./dep-pkg, source: local` entry -- destroying uninstall/frozen
// provenance. After the fix, the key space must stay "_local/..." across
// update, exactly like install produced it.
func TestRunUpdate_LocalDep_LockfileKeyStable_NoBarePathEntry(t *testing.T) {
	_, localKey := setupLocalDepUpdateFixture(t)

	writeLocalDepContent(t, "dep-pkg", "v2")

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	if err := runUpdate(deps, false, true, ""); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}

	raw, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	if strings.Contains(content, "repo_url: ./dep-pkg") {
		t.Errorf("update must not rewrite the local dep's lock entry into a bare relative-path repo_url, got:\n%s", content)
	}
	if strings.Contains(content, "source: local") {
		t.Errorf("update must not rewrite the local dep's lock entry into source: local, got:\n%s", content)
	}

	lock := readLockfile(t)
	if lock.FindByKey(localKey) == nil {
		t.Errorf("expected the stable hashed local key %q to remain in the lockfile, got:\n%s", localKey, content)
	}
}

// TestRunUpdate_LocalDep_MatchesFreshInstallDeployedBytes is this task's PRD
// AC ("update 後 local dep 部署結果與 install 一致"): a project that installs
// v1 then updates to v2 must deploy byte-identical output to a fresh project
// that installs v2 directly -- proving the update path reuses install's exact
// materialize/deploy pipeline rather than some update-specific approximation
// of it.
func TestRunUpdate_LocalDep_MatchesFreshInstallDeployedBytes(t *testing.T) {
	updateDir, _ := setupLocalDepUpdateFixture(t)
	writeLocalDepContent(t, "dep-pkg", "v2")
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	if err := runUpdate(deps, false, true, ""); err != nil {
		t.Fatalf("update-side runUpdate: %v", err)
	}

	installDir := chdirTemp(t)
	writeLocalDepContent(t, "dep-pkg", "v2")
	if err := os.WriteFile("apm.yml", []byte(
		"name: test\nversion: \"1.0.0\"\ntarget:\n  - claude\ndependencies:\n  apm:\n    - ./dep-pkg\n",
	), 0644); err != nil {
		t.Fatal(err)
	}
	freshDeps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	if err := runInstall(freshDeps, false, true, "", nil, nil); err != nil {
		t.Fatalf("fresh-install-side runInstall: %v", err)
	}

	for _, rel := range []string{filepath.Join(".claude", "rules", "style.md"), filepath.Join(".claude", "agents", "helper.md")} {
		updateBytes, err := os.ReadFile(filepath.Join(updateDir, rel))
		if err != nil {
			t.Fatalf("update side missing %s: %v", rel, err)
		}
		installBytes, err := os.ReadFile(filepath.Join(installDir, rel))
		if err != nil {
			t.Fatalf("fresh install side missing %s: %v", rel, err)
		}
		if string(updateBytes) != string(installBytes) {
			t.Errorf("%s differs between update and fresh install: update=%q install=%q", rel, updateBytes, installBytes)
		}
	}
}

// TestRunUpdate_Scoped_LocalPathToken_Matches is the C3 regression: once
// runUpdate normalizes local deps into their "_local/<base>-<sha8>" key
// space, a scoped `apm update ./dep-pkg` positional token (still shaped like
// the pre-normalization manifest form) must keep matching that dependency --
// PlanScopedUpdate's depKey(dep)==packageName lookup would otherwise report
// "package not found" for a dependency that IS present, unless the token
// itself is translated the same way normalizeLocalDep translated the dep.
func TestRunUpdate_Scoped_LocalPathToken_Matches(t *testing.T) {
	dir, localKey := setupLocalDepUpdateFixture(t)

	writeLocalDepContent(t, "dep-pkg", "v2")

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	if err := runUpdate(deps, false, true, "./dep-pkg"); err != nil {
		t.Fatalf("scoped update on a local-path token must not fail: %v", err)
	}

	deployedAgent, err := os.ReadFile(filepath.Join(dir, ".claude", "agents", "helper.md"))
	if err != nil {
		t.Fatalf("expected .claude/agents/helper.md to exist after scoped local update: %v", err)
	}
	if !strings.Contains(string(deployedAgent), "agent-v2") {
		t.Errorf("scoped local update did not refresh deployment, got %q", deployedAgent)
	}

	lock := readLockfile(t)
	if lock.FindByKey(localKey) == nil {
		t.Errorf("expected lockfile entry %q to survive a scoped local-path update", localKey)
	}
}

// TestRunUpdate_DepsPresentZeroTarget_ExitsWithTeachingMessage is the C2
// regression, mirroring TestRunInstall_DepsPresentZeroTarget_ExitsWithTeachingMessage
// (install_test.go): before the fix, an update with resolvable dependencies
// but no --target, no apm.yml target:, and nothing auto-detected silently
// exited 0 -- and, worse than install's equivalent gap, deployAndFinalize's
// semantic-equality no-op check still let it rewrite apm.lock.yaml with no
// deployment to show for it. The fix must exit 2 with the same teaching
// message runInstall uses, and must not touch apm.lock.yaml at all (zero
// partial writes).
func TestRunUpdate_DepsPresentZeroTarget_ExitsWithTeachingMessage(t *testing.T) {
	chdirTemp(t)

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/a#^1.0.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	lockContent := []byte("lockfile_version: \"1\"\ndependencies:\n  - repo_url: acme/a\n    source: git\n    constraint: \"^1.0.0\"\n    resolved_tag: v1.2.0\n    depth: 1\n")
	if err := os.WriteFile("apm.lock.yaml", lockContent, 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{
		tags: &mockInstallTagLister{tags: map[string][]semver.TagInfo{
			"acme/a": {{Name: "v1.2.0"}, {Name: "v1.9.0"}},
		}},
		loader: &mockInstallLoader{packages: map[string]*manifest.Manifest{
			"acme/a@v1.9.0": {Name: "a", Version: "1.9.0"},
		}},
	}

	var err error
	stdout := captureUninstallStdout(t, func() {
		err = runUpdate(deps, false, true, "")
	})
	if err == nil {
		t.Fatal("expected an error when dependencies are present but no deployment target resolves")
	}
	if !strings.Contains(err.Error(), "no deployment target detected") {
		t.Errorf("error should teach the user about the missing target, got: %v", err)
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", got)
	}
	// ULD-13 (07-11-update-local-deps): printUpdateSummary's "Update plan for
	// apm.yml" heading must still reach stdout before the zero-target gate
	// returns its teaching error -- codex's checklist re-verification found
	// this heading missing, matching the oracle's observed stdout ordering
	// (design.md C2 "排序修正": plan first, teaching error second).
	if !strings.Contains(stdout, "Update plan for apm.yml") {
		t.Errorf("expected the update plan heading to print before the zero-target gate, got stdout: %q", stdout)
	}

	after, readErr := os.ReadFile("apm.lock.yaml")
	if readErr != nil {
		t.Fatalf("apm.lock.yaml should still exist (untouched), read error: %v", readErr)
	}
	if string(after) != string(lockContent) {
		t.Errorf("apm.lock.yaml must not be rewritten when update fails closed on zero targets\nbefore:\n%s\nafter:\n%s", lockContent, after)
	}
}
