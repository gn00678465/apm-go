package main

import (
	"bytes"
	"os"
	"testing"
)

// Expected lines are the Oracle's (v0.33.0 commands/audit.py:955,986), with
// apm-go's status symbols.
func runAuditIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	t.Chdir(dir)
	cmd := auditCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("audit %v: %v", args, err)
	}
	return buf.String()
}

func TestAudit_NoLockfileIsAnEmptyScan(t *testing.T) {
	got := runAuditIn(t, t.TempDir())
	want := " > Scanning installed packages and deployed files...\n" +
		" i No recognized deployed primitives found -- nothing to scan\n"
	if got != want {
		t.Errorf("audit with no lockfile:\n got %q\nwant %q", got, want)
	}
}

func TestAudit_LockfileWithoutDeployedFilesIsAnEmptyScan(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/apm.lock.yaml", []byte("lockfile_version: '1'\ndependencies: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := runAuditIn(t, dir)
	want := " > Scanning installed packages and deployed files...\n" +
		" i No recognized deployed primitives found -- nothing to scan\n"
	if got != want {
		t.Errorf("audit with an empty lockfile:\n got %q\nwant %q", got, want)
	}
}

func TestAudit_ContentWithNoLockfileKeepsItsOwnLine(t *testing.T) {
	got := runAuditIn(t, t.TempDir(), "--content")
	want := " i No apm.lock.yaml found -- nothing to scan. Use --file to scan a specific file.\n"
	if got != want {
		t.Errorf("audit --content with no lockfile:\n got %q\nwant %q", got, want)
	}
}
