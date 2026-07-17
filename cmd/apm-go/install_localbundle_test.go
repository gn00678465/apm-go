package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/pack/bundle"
	"github.com/apm-go/apm/internal/yamlcore"
)

// buildInstallTestBundle produces a real plugin-format bundle (via the same
// bundle.Produce BundleProducer `apm-go pack` calls) with an embedded,
// integrity-checked apm.lock.yaml, for cmd/apm-go's own runInstall tests of
// the local-bundle consumption path (Phase 6).
func buildInstallTestBundle(t *testing.T) string {
	t.Helper()
	projectRoot := t.TempDir()
	writeInstallFixtureFile(t, filepath.Join(projectRoot, ".apm", "agents", "foo.md"), "# agent foo")
	writeInstallFixtureFile(t, filepath.Join(projectRoot, ".apm", "skills", "bar", "SKILL.md"), "# skill bar")

	doc, err := yamlcore.SafeLoad([]byte("name: demo\nversion: 1.0.0\n"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	result, err := bundle.Produce(&buf, bundle.ProduceOptions{
		ProjectRoot: projectRoot,
		OutputDir:   filepath.Join(projectRoot, "build"),
		PkgName:     "demo",
		PkgVersion:  "1.0.0",
		Target:      "claude",
		ApmYMLNode:  doc.Content[0],
		Lockfile:    &lockfile.Lockfile{Version: "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result.BundleDir
}

// buildInstallTestBundleWithNestedSkill mirrors buildInstallTestBundle but
// packs a two-level nested skill (skills/<category>/<name>/SKILL.md) -- the
// shape test1's real skills carry (e.g.
// .agents/skills/engineering/ask-matt/SKILL.md) -- rather than the flat
// skills/bar/SKILL.md every other fixture in this file uses. It exists
// solely so TestRunInstall_LocalBundle_NestedSkill_* has automated coverage
// for the deploy-verbatim-no-flattening guarantee Gate 6b's A3 finding only
// checked via a manual A/B script run (research/codex-verify-gate6b-fix.md).
func buildInstallTestBundleWithNestedSkill(t *testing.T) string {
	t.Helper()
	projectRoot := t.TempDir()
	writeInstallFixtureFile(t, filepath.Join(projectRoot, ".apm", "skills", "engineering", "ask-matt", "SKILL.md"), "# nested skill ask-matt")

	doc, err := yamlcore.SafeLoad([]byte("name: demo\nversion: 1.0.0\n"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	result, err := bundle.Produce(&buf, bundle.ProduceOptions{
		ProjectRoot: projectRoot,
		OutputDir:   filepath.Join(projectRoot, "build"),
		PkgName:     "demo",
		PkgVersion:  "1.0.0",
		Target:      "claude",
		ApmYMLNode:  doc.Content[0],
		Lockfile:    &lockfile.Lockfile{Version: "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result.BundleDir
}

// TestRunInstall_LocalBundle_NestedSkill_DeploysVerbatim locks in Gate 6b's
// A3 finding (codex-verify-gate6b-fix.md: 78 two-level nested skills found
// in a real pack->install run, path/hash diff both 0 against Python) as an
// automated regression: a skill nested two levels deep
// (skills/<category>/<name>/SKILL.md) must deploy VERBATIM under the
// target's deploy root -- never flattened to skills/<name>/SKILL.md.
func TestRunInstall_LocalBundle_NestedSkill_DeploysVerbatim(t *testing.T) {
	bundleDir := buildInstallTestBundleWithNestedSkill(t)
	chdirTemp(t)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	if err := runInstall(deps, false, false, "claude", nil, []string{bundleDir}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantPath := filepath.Join(".claude", "skills", "engineering", "ask-matt", "SKILL.md")
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("expected %s to be deployed (nested skill path preserved): %v", wantPath, err)
	}
	if string(data) != "# nested skill ask-matt" {
		t.Errorf("%s content = %q, want verbatim source copy", wantPath, data)
	}

	// The flattened (bug) shape must NOT exist either.
	if _, statErr := os.Stat(filepath.Join(".claude", "skills", "ask-matt", "SKILL.md")); statErr == nil {
		t.Error(".claude/skills/ask-matt/SKILL.md must not exist -- nested skill must not be flattened")
	}
}

// TestRunInstall_LocalBundle_NestedSkill_CopilotUsesAgentsRoot covers the
// same nested-skill verbatim-path guarantee for copilot's skills
// deploy_root override (.agents, not .github) -- a second target-routing
// branch integrate.go's targetRoutingTable selects independently of the
// nesting depth itself.
func TestRunInstall_LocalBundle_NestedSkill_CopilotUsesAgentsRoot(t *testing.T) {
	bundleDir := buildInstallTestBundleWithNestedSkill(t)
	chdirTemp(t)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	if err := runInstall(deps, false, false, "copilot", nil, []string{bundleDir}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantPath := filepath.Join(".agents", "skills", "engineering", "ask-matt", "SKILL.md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected %s to be deployed: %v", wantPath, err)
	}
}

func writeInstallFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// chdirTemp is defined in marketplace_authoring_test.go.

func TestRunInstall_LocalBundle_DeploysAndWritesLockfile(t *testing.T) {
	bundleDir := buildInstallTestBundle(t)
	chdirTemp(t)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	if err := runInstall(deps, false, false, "claude", nil, []string{bundleDir}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(".claude", "agents", "foo.md")); err != nil {
		t.Errorf("expected .claude/agents/foo.md to be deployed: %v", err)
	}
	// claude's skills primitive has no deploy_root override in Python's
	// KNOWN_TARGETS (targets.py:513) -- a claude-only install lands skills
	// under .claude/skills/, not the cross-tool .agents/skills/ apm-go's
	// REGULAR (non-bundle) deploy pipeline additionally writes to.
	if _, err := os.Stat(filepath.Join(".claude", "skills", "bar", "SKILL.md")); err != nil {
		t.Errorf("expected .claude/skills/bar/SKILL.md to be deployed: %v", err)
	}

	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("expected apm.lock.yaml to be written: %v", err)
	}
	lockText := string(lockData)
	if !strings.Contains(lockText, "local_deployed_files") {
		t.Errorf("apm.lock.yaml = %s, want local_deployed_files recorded", lockText)
	}
	if !strings.Contains(lockText, ".claude/agents/foo.md") {
		t.Errorf("apm.lock.yaml = %s, want deployed file path recorded", lockText)
	}
	// Imperative deploy: apm.yml must never be created/touched.
	if _, err := os.Stat("apm.yml"); !os.IsNotExist(err) {
		t.Errorf("apm.yml must not be created by a local bundle install (stat err = %v)", err)
	}
}

// TestRunInstall_LocalBundle_SummaryAggregatesDeployedFilesByKind is the
// R12b regression (prd.md/design.md §3): an ordinary `apm install` groups
// its deployed-files summary by primitive kind and target root
// (deployedFilesTree, R10b) -- local-bundle install used to only print the
// aggregate file count, dropping that same breakdown even though
// result.Files (fed to this same helper) already carries everything needed
// to produce it.
func TestRunInstall_LocalBundle_SummaryAggregatesDeployedFilesByKind(t *testing.T) {
	bundleDir := buildInstallTestBundle(t)
	chdirTemp(t)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	r, w, _ := os.Pipe()
	origStdout := os.Stdout
	os.Stdout = w
	err := runInstall(deps, false, false, "claude", nil, []string{bundleDir})
	os.Stdout = origStdout
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	stdout := buf.String()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "1 agent -> .claude/agents/") {
		t.Errorf("expected an aggregated agent summary line, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "1 skill -> .claude/skills/") {
		t.Errorf("expected an aggregated skill summary line, got:\n%s", stdout)
	}
}

func TestRunInstall_LocalBundle_TamperedFile_Errors(t *testing.T) {
	bundleDir := buildInstallTestBundle(t)
	if err := os.WriteFile(filepath.Join(bundleDir, "agents", "foo.md"), []byte("TAMPERED"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdirTemp(t)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runInstall(deps, false, false, "claude", nil, []string{bundleDir})
	if err == nil {
		t.Fatal("expected an integrity-check error for a tampered bundle")
	}
	if !strings.Contains(err.Error(), "integrity check failed") {
		t.Errorf("err = %v, want it to mention integrity check failure", err)
	}
	if _, statErr := os.Stat(".claude"); !os.IsNotExist(statErr) {
		t.Error("expected zero files deployed when integrity verification fails")
	}
	if _, statErr := os.Stat("apm.lock.yaml"); !os.IsNotExist(statErr) {
		t.Error("expected no apm.lock.yaml written when integrity verification fails")
	}
}

func TestRunInstall_LocalBundle_SkillFlagConflict_Errors(t *testing.T) {
	bundleDir := buildInstallTestBundle(t)
	chdirTemp(t)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runInstall(deps, false, false, "claude", []string{"bar"}, []string{bundleDir})
	if err == nil {
		t.Fatal("expected a flag-conflict usage error")
	}
	if !strings.Contains(err.Error(), "--skill") {
		t.Errorf("err = %v, want it to name --skill as the conflicting flag", err)
	}
}

func TestRunInstall_LocalBundle_AllowInsecureFlagConflict_Errors(t *testing.T) {
	bundleDir := buildInstallTestBundle(t)
	chdirTemp(t)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}, allowInsecure: true}
	err := runInstall(deps, false, false, "claude", nil, []string{bundleDir})
	if err == nil {
		t.Fatal("expected a flag-conflict usage error")
	}
	if !strings.Contains(err.Error(), "--allow-insecure") {
		t.Errorf("err = %v, want it to name --allow-insecure as the conflicting flag", err)
	}
}

func TestRunInstall_LocalBundle_ZeroTargets_NoOpNoError(t *testing.T) {
	bundleDir := buildInstallTestBundle(t)
	chdirTemp(t)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	// No --target, and the fresh temp consumer dir has no harness signal to
	// auto-detect from -- targets resolves empty.
	if err := runInstall(deps, false, false, "", nil, []string{bundleDir}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat("apm.lock.yaml"); !os.IsNotExist(statErr) {
		t.Error("expected no apm.lock.yaml written when zero targets resolve")
	}
}

// TestRunInstall_LocalBundle_TargetMismatch_WarnsButStillDeploys covers the
// check_target_mismatch contract: the bundle was packed for "claude" (see
// buildInstallTestBundle), but installing with --target codex must still
// deploy (codex supports agents/skills) rather than refuse -- the mismatch
// is advisory only, never a gate.
func TestRunInstall_LocalBundle_TargetMismatch_WarnsButStillDeploys(t *testing.T) {
	bundleDir := buildInstallTestBundle(t)
	chdirTemp(t)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	if err := runInstall(deps, false, false, "codex", nil, []string{bundleDir}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verbatim deploy: the bundle's "agents/foo.md" is copied byte-for-byte
	// under codex's default root, NOT re-derived into a deploy.Primitive and
	// reformatted into codex's own TOML agent convention (that transform only
	// applies to apm-go's REGULAR .apm/-source deploy pipeline).
	if _, err := os.Stat(filepath.Join(".codex", "agents", "foo.md")); err != nil {
		t.Errorf("expected .codex/agents/foo.md to be deployed verbatim despite the target mismatch: %v", err)
	}
}

func TestRunInstall_LocalBundle_ArchiveExtensionNotABundle_UsageError(t *testing.T) {
	consumerDir := chdirTemp(t)
	notABundle := filepath.Join(consumerDir, "fake.zip")
	if err := os.WriteFile(notABundle, []byte("PK\x03\x04not a real zip"), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runInstall(deps, false, false, "", nil, []string{notABundle})
	if err == nil {
		t.Fatal("expected an error for an invalid .zip archive")
	}
}

// TestRunInstall_LocalPathDependency_StillFallsThrough is the regression
// guard for a directory that LOOKS like it could be a local-bundle
// candidate (a single positional filesystem path) but has no plugin.json:
// it must fall through to the ordinary local-path dependency install,
// exactly as before this task (F1/normalizeLocalDep unaffected).
func TestRunInstall_LocalPathDependency_StillFallsThrough(t *testing.T) {
	pkgDir := t.TempDir()
	writeInstallFixtureFile(t, filepath.Join(pkgDir, "apm.yml"), "name: localpkg\nversion: \"1.0.0\"\n")
	writeInstallFixtureFile(t, filepath.Join(pkgDir, ".apm", "agents", "a.md"), "# a")

	consumerDir := chdirTemp(t)
	writeInstallFixtureFile(t, filepath.Join(consumerDir, "apm.yml"), "name: consumer\nversion: \"1.0.0\"\n")

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	if err := runInstall(deps, false, true, "claude", nil, []string{pkgDir}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("expected apm.lock.yaml to be written for an ordinary local-path dependency install: %v", err)
	}
	if !strings.Contains(string(lockData), "local") {
		t.Errorf("apm.lock.yaml = %s, want a local-source dependency entry", lockData)
	}
}
