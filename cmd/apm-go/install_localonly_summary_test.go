package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runInstallCapturingStdout runs runInstall with os.Stdout redirected to a
// pipe, returning its combined output. Local to this file so the R16 tests
// below don't need to duplicate the os.Pipe dance per test.
func runInstallCapturingStdout(t *testing.T, deps *installDeps, frozen, noProvenance bool, targetFlag string, skillSubset []string, packages []string) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	installErr := runInstall(deps, frozen, noProvenance, targetFlag, "", skillSubset, packages)
	os.Stdout = origStdout
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String(), installErr
}

// TestInstall_LocalOnlyProject_Success is the R16 regression (prd.md/
// design.md §3): a zero-dependency manifest with deployable .apm/ local
// primitives used to print the contradictory sequence "No dependencies to
// install" -> a deployed-files tree -> "Installed 0 dependencies". The
// up-front info line must be muted when a target resolves and local content
// WILL be deployed, and the closing summary counts the project's own content
// as one dependency, the Oracle's count (install/phases/integrate.py:385;
// issue #50 ruling R10) -- decided from deploy.Run's ACTUAL result (a file
// really landed under .claude/rules/), not a second directory scan or a
// hardcoded "0 dependencies" count that contradicts the block printed above
// it.
func TestInstall_LocalOnlyProject_Success(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)
	os.MkdirAll(filepath.Join(".apm", "instructions"), 0755)
	os.WriteFile(filepath.Join(".apm", "instructions", "x.instructions.md"), []byte("# x"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	stdout, err := runInstallCapturingStdout(t, deps, false, true, "claude", nil, nil)
	if err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	if strings.Contains(stdout, "No dependencies to install") {
		t.Errorf("expected the up-front info line to be muted when local content will deploy, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Installed 1 APM dependency in ") {
		t.Errorf(`expected "Installed 1 APM dependency" in the summary, got:\n%s`, stdout)
	}
	if strings.Contains(stdout, "Installed 0 dependencies") {
		t.Errorf("summary must not contradict the deployed-files tree with a 0-dependencies count, got:\n%s", stdout)
	}
}

// TestInstall_LocalOnlyProject_ZeroFilesDeployed is R16's negative case: a
// zero-dependency manifest with a .apm/ primitive that exists on disk but
// deploys ZERO files to the resolved target (codex has no registered
// handler for TypeInstructions -- adapterSupports filters it out). The
// summary must NOT count the project's content as installed just because a
// local primitive was found on disk; the decision must reflect deploy.Run's
// actual (empty) result, and the closing line is the Oracle's for a run with
// nothing to integrate.
func TestInstall_LocalOnlyProject_ZeroFilesDeployed(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)
	os.MkdirAll(filepath.Join(".apm", "instructions"), 0755)
	os.WriteFile(filepath.Join(".apm", "instructions", "x.instructions.md"), []byte("# x"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	// codex supports agents/skills/hooks only -- an instructions-only local
	// tree deploys zero files to it.
	stdout, err := runInstallCapturingStdout(t, deps, false, true, "codex", nil, nil)
	if err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	if strings.Contains(stdout, "Installed 1 APM dependency") {
		t.Errorf("must not count the project's content when zero files actually deployed, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "No changes -- install state already up to date in ") {
		t.Errorf(`expected the "No changes" line when nothing deployed, got:\n%s`, stdout)
	}
}
