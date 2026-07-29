package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestErrNoDeployTarget_TeachesPluralTargetsKey is AC4 (R1.3): the
// no-deployment-target teaching error must recommend the plural targets:
// key, and must not print the singular target: example anymore.
func TestErrNoDeployTarget_TeachesPluralTargetsKey(t *testing.T) {
	msg := errNoDeployTarget().Error()

	if !strings.Contains(msg, "targets:") {
		t.Errorf("errNoDeployTarget() = %q, want it to mention 'targets:' (plural)", msg)
	}
	if regexp.MustCompile(`(?m)^\s*target:\s*$`).MatchString(msg) {
		t.Errorf("errNoDeployTarget() = %q, must not print the singular 'target:' example", msg)
	}
}

// TestInstall_LegacySingularTargetKey_StillDeploys is AC29 (R1.4/C4): an
// apm.yml written with only the singular target: key (pre-existing project,
// never touched by this task's init changes) must still resolve a deploy
// target and deploy local primitives end to end -- proving
// internal/manifest's dual-key parsing on the deploy path was not broken by
// this task's changes elsewhere.
func TestInstall_LegacySingularTargetKey_StillDeploys(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	if err := os.MkdirAll(filepath.Join(".apm", "instructions"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".apm", "instructions", "demo.instructions.md"), []byte("# demo"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ntarget:\n  - claude\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	// No --target flag: the deploy target must resolve from the manifest's
	// singular target: key alone.
	if err := runInstall(deps, false, true, "", nil, nil); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".claude", "rules", "demo.md")); err != nil {
		t.Errorf("expected local instructions to deploy to .claude/rules/demo.md: %v", err)
	}
	if _, err := os.Stat("apm.lock.yaml"); err != nil {
		t.Errorf("expected apm.lock.yaml to be written: %v", err)
	}
}
