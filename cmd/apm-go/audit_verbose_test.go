// Tests for R12c (prd.md/design.md §3, implement.md Phase 4 step 24):
// `apm-go audit` (bare, no --content) gains a --verbose flag that lists
// every deployed file path a successful run just re-verified. The default
// (non-verbose) output must stay exactly what it already was -- these
// tests lock that down alongside the new --verbose behavior.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAuditVerboseFixture lays down a lockfile recording one dependency
// deployed file plus one project-local deployed file, both hash-clean on
// disk, so bare audit succeeds (exit 0) with a non-trivial file set for
// --verbose to list.
func writeAuditVerboseFixture(t *testing.T) string {
	t.Helper()
	dir := chTemp(t)

	depFile := filepath.Join(dir, "apm_modules", "dep", "skill", "SKILL.md")
	localFile := filepath.Join(dir, "instructions", "local.md")
	if err := os.MkdirAll(filepath.Dir(depFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(localFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(depFile, []byte("dep content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localFile, []byte("local content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	depHash := sha256Envelope(t, depFile)
	localHash := sha256Envelope(t, localFile)

	lockYAML := `lockfile_version: "2"
dependencies:
  - repo_url: https://example.com/org/dep.git
    source: git
    deployed_file_hashes:
      apm_modules/dep/skill/SKILL.md: ` + depHash + `
local_deployed_file_hashes:
  instructions/local.md: ` + localHash + `
`
	if err := os.WriteFile(filepath.Join(dir, "apm.lock.yaml"), []byte(lockYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAudit_Verbose_ListsDeployedFiles(t *testing.T) {
	writeAuditVerboseFixture(t)

	stdout, _, exitCode := runAuditCmd(t, "--verbose")
	if exitCode != 0 {
		t.Fatalf("verbose bare audit should still succeed, got exit %d\nstdout=%q", exitCode, stdout)
	}
	if !strings.Contains(stdout, "audit: 2 deployed files verified") {
		t.Errorf("--verbose must keep the unchanged summary line, got %q", stdout)
	}
	for _, want := range []string{
		"https://example.com/org/dep.git",
		"apm_modules/dep/skill/SKILL.md",
		"(project)",
		"instructions/local.md",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--verbose stdout missing %q, got %q", want, stdout)
		}
	}
}

// TestAudit_DefaultOutputStaysSummaryOnly proves that WITHOUT --verbose,
// bare audit's success output is unchanged: only the summary line, none of
// the per-file/per-dependency detail --verbose now prints.
func TestAudit_DefaultOutputStaysSummaryOnly(t *testing.T) {
	writeAuditVerboseFixture(t)

	stdout, _, exitCode := runAuditCmd(t)
	if exitCode != 0 {
		t.Fatalf("bare audit should succeed, got exit %d\nstdout=%q", exitCode, stdout)
	}
	if !strings.Contains(stdout, "audit: 2 deployed files verified") {
		t.Errorf("default stdout missing summary line, got %q", stdout)
	}
	for _, unwanted := range []string{
		"https://example.com/org/dep.git",
		"apm_modules/dep/skill/SKILL.md",
		"(project)",
		"instructions/local.md",
	} {
		if strings.Contains(stdout, unwanted) {
			t.Errorf("default (non-verbose) stdout must not contain detail %q, got %q", unwanted, stdout)
		}
	}
}
