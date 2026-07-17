// Tests for R12d (prd.md/design.md §3, implement.md Phase 4 step 24):
// `apm-go install --frozen` gains a --verbose flag (installDeps.verbose)
// that lists every dependency the run just pinned+verified. The default
// (non-verbose) success line must stay exactly what it already was --
// these tests lock that down alongside the new --verbose behavior.
package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInstall_Frozen_VerboseListsDependencies(t *testing.T) {
	lock := integrityFixture(t, "good.frozen.yaml")
	good := integrityFixture(t, "good.tar.gz")
	dir := chTemp(t)

	copyInto(t, lock, filepath.Join(dir, "apm.lock.yaml"))
	copyInto(t, good, filepath.Join(dir, "good.tar.gz"))

	deps := newDeps()
	deps.verbose = true

	out := captureInstallStdout(t, func() {
		if err := runInstall(deps, true, false, "", nil, nil); err != nil {
			t.Fatalf("verbose frozen install should succeed: %v", err)
		}
	})

	if !strings.Contains(out, "Frozen install: all dependencies pinned and verified") {
		t.Errorf("--verbose must keep the unchanged summary line, got %q", out)
	}
	if !strings.Contains(out, "registry.example.com/demo/good") {
		t.Errorf("--verbose output should list the pinned dependency, got %q", out)
	}
}

// TestRunInstall_Frozen_DefaultOutputStaysSummaryOnly proves that WITHOUT
// --verbose (installDeps.verbose left at its zero value), a successful
// --frozen install's stdout is unchanged: just the summary line, none of
// the per-dependency listing --verbose now prints.
func TestRunInstall_Frozen_DefaultOutputStaysSummaryOnly(t *testing.T) {
	lock := integrityFixture(t, "good.frozen.yaml")
	good := integrityFixture(t, "good.tar.gz")
	dir := chTemp(t)

	copyInto(t, lock, filepath.Join(dir, "apm.lock.yaml"))
	copyInto(t, good, filepath.Join(dir, "good.tar.gz"))

	out := captureInstallStdout(t, func() {
		if err := runInstall(newDeps(), true, false, "", nil, nil); err != nil {
			t.Fatalf("frozen install should succeed: %v", err)
		}
	})

	if !strings.Contains(out, "Frozen install: all dependencies pinned and verified") {
		t.Errorf("default stdout missing summary line, got %q", out)
	}
	if strings.Contains(out, "registry.example.com/demo/good") {
		t.Errorf("default (non-verbose) stdout must not list the dependency, got %q", out)
	}
}
