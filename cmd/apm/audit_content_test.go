// Tests for Phase 7 (07-12-p0-parity-quickwins): `apm-go audit --content`,
// the hidden-Unicode scan pillar of Python's bare `apm audit`. See
// audit_content.go and design.md's "audit 掃描接線" section.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAuditContentFixture lays down a lockfile that records relPath as a
// local_deployed_files entry, plus the file itself with the given content,
// under a fresh temp dir (which becomes the working directory).
func writeAuditContentFixture(t *testing.T, relPath, content string) string {
	t.Helper()
	dir := chTemp(t)

	abs := filepath.Join(dir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	lockYAML := "lockfile_version: \"2\"\nlocal_deployed_files:\n  - " + relPath + "\n"
	if err := os.WriteFile(filepath.Join(dir, "apm.lock.yaml"), []byte(lockYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runAuditCmd(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := auditCmd()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return outBuf.String(), errBuf.String(), exitCodeOf(err)
}

func TestAuditContent_ExitCodes(t *testing.T) {
	tests := []struct {
		name       string
		relPath    string
		content    string
		wantExit   int
		wantStderr []string
	}{
		{
			name:     "clean_ascii_exit_0",
			relPath:  "instructions/demo.md",
			content:  "hello world\nnothing suspicious here\n",
			wantExit: 0,
		},
		{
			// U+202E (RLO) ... U+202C (PDF): critical bidi-override range.
			name:     "critical_bidi_override_exit_1",
			relPath:  "instructions/critical.md",
			content:  "safe ‮hidden‬ text\n",
			wantExit: 1,
			wantStderr: []string{
				"critical", "bidi-override", "instructions/critical.md",
			},
		},
		{
			// U+200B: zero-width space, warning-level.
			name:     "warning_only_zero_width_exit_2",
			relPath:  "instructions/warn.md",
			content:  "safe​text\n",
			wantExit: 2,
			wantStderr: []string{
				"warning", "zero-width", "instructions/warn.md",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			writeAuditContentFixture(t, tc.relPath, tc.content)

			stdout, stderr, exitCode := runAuditCmd(t, "--content")
			if exitCode != tc.wantExit {
				t.Fatalf("exit code = %d, want %d\nstdout=%q\nstderr=%q", exitCode, tc.wantExit, stdout, stderr)
			}
			for _, want := range tc.wantStderr {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr missing %q, got %q", want, stderr)
				}
			}
		})
	}
}

// TestAuditContent_InfoOnlyStaysExit0 covers the third exit-code branch:
// info-level-only findings (no critical, no warning) must not escalate the
// exit code, matching Classify's severity ordering.
func TestAuditContent_InfoOnlyStaysExit0(t *testing.T) {
	// U+FEFF at byte 0 is classified info (ScanText's leading-BOM special case).
	writeAuditContentFixture(t, "instructions/info.md", "\uFEFFhello world\n")

	stdout, stderr, exitCode := runAuditCmd(t, "--content")
	if exitCode != 0 {
		t.Fatalf("info-only findings must not escalate exit code, got %d\nstdout=%q\nstderr=%q", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "info-level finding") {
		t.Errorf("stdout should report info-level finding count, got %q", stdout)
	}
}

// TestAuditContent_ScansDepsAndLocal proves the scan set is the union of
// every dependency's DeployedFiles and the project's LocalDeployedFiles
// (checklist A2) -- not just one of the two sources.
func TestAuditContent_ScansDepsAndLocal(t *testing.T) {
	dir := chTemp(t)

	depFile := filepath.Join(dir, "apm_modules", "dep.md")
	localFile := filepath.Join(dir, "instructions", "local.md")
	if err := os.MkdirAll(filepath.Dir(depFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(localFile), 0o755); err != nil {
		t.Fatal(err)
	}
	// Dependency-side finding: bidi override (critical).
	if err := os.WriteFile(depFile, []byte("dep ‮hidden‬\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Local-side finding: zero-width space (warning) -- would not surface
	// unless local_deployed_files is also scanned.
	if err := os.WriteFile(localFile, []byte("local​text\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lockYAML := `lockfile_version: "2"
dependencies:
  - repo_url: https://example.com/dep.git
    source: git
    deployed_files:
      - apm_modules/dep.md
local_deployed_files:
  - instructions/local.md
`
	if err := os.WriteFile(filepath.Join(dir, "apm.lock.yaml"), []byte(lockYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, exitCode := runAuditCmd(t, "--content")
	if exitCode != 1 {
		t.Fatalf("expected exit 1 (critical dep finding present), got %d\nstderr=%q", exitCode, stderr)
	}
	if !strings.Contains(stderr, "apm_modules/dep.md") {
		t.Errorf("stderr must include the dependency-side finding, got %q", stderr)
	}
	if !strings.Contains(stderr, "instructions/local.md") {
		t.Errorf("stderr must include the local-side finding (deps must not shadow local), got %q", stderr)
	}
}

// TestAuditContent_BareUnaffected pins that bare `audit` (no --content)
// keeps doing SHA-256 re-verification unchanged -- content findings in the
// deployed file must NOT surface without the flag (checklist A1/C1).
func TestAuditContent_BareUnaffected(t *testing.T) {
	dir := writeAuditContentFixture(t, "instructions/critical.md", "safe ‮hidden‬ text\n")

	// Compute the real SHA-256 so the bare path's hash re-verification
	// passes cleanly and doesn't mask the content-scan-vs-bare distinction
	// under test here.
	hash := sha256Envelope(t, filepath.Join(dir, "instructions", "critical.md"))

	lockYAML := "lockfile_version: \"2\"\nlocal_deployed_files:\n  - instructions/critical.md\n" +
		"local_deployed_file_hashes:\n  instructions/critical.md: " + hash + "\n"
	if err := os.WriteFile(filepath.Join(dir, "apm.lock.yaml"), []byte(lockYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, exitCode := runAuditCmd(t)
	if exitCode != 0 {
		t.Fatalf("bare audit must stay SHA-only and pass on a hash-clean file, got exit %d\nstdout=%q\nstderr=%q", exitCode, stdout, stderr)
	}
	if strings.Contains(stdout, "hidden") || strings.Contains(stderr, "critical") {
		t.Errorf("bare audit must not surface content-scan findings, got stdout=%q stderr=%q", stdout, stderr)
	}
}

// TestAuditContent_HelpLocksExclusionWording pins the exact "--content does
// NOT reproduce install-replay drift detection, nor Python's --ci/--policy/
// --external/--format/-o/--strip" wording in `apm-go audit --help`'s Long
// description (checklist D4). A prior codex adversarial pass flipped "does
// NOT reproduce" to "DOES reproduce" in the Long string and every existing
// audit test (including TestAuditCmd_HelpDocumentsSemanticDifference) still
// passed -- that flip silently tells users --content also performs
// install-replay drift detection, which it does not. These literals are
// independent of the product string (copy-checked, not referenced) so a
// future wording drift is caught here instead of being tautologically
// green.
func TestAuditContent_HelpLocksExclusionWording(t *testing.T) {
	cmd := auditCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("audit --help returned error: %v", err)
	}
	out := buf.String()

	const driftExclusion = "does NOT reproduce Python's default install-replay"
	if !strings.Contains(out, driftExclusion) {
		t.Errorf("audit --help output missing drift-exclusion wording %q:\n%s", driftExclusion, out)
	}

	const flagExclusionOpen = "nor any of Python's --ci, --policy, --external,"
	if !strings.Contains(out, flagExclusionOpen) {
		t.Errorf("audit --help output missing flag-exclusion wording %q:\n%s", flagExclusionOpen, out)
	}

	const flagExclusionClose = "--format, -o, or --strip flags"
	if !strings.Contains(out, flagExclusionClose) {
		t.Errorf("audit --help output missing flag-exclusion wording %q:\n%s", flagExclusionClose, out)
	}
}
