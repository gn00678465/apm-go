//go:build unix

package main

import (
	"strings"
	"testing"
)

// TestRunCaseSide_RunsSetupArgvBeforeMainArgv proves setup_argv steps run,
// in the same sandbox cwd, before Argv: a setup step drops a marker file
// that Argv's own run then observes.
func TestRunCaseSide_RunsSetupArgvBeforeMainArgv(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `
if [ "$1" = "setup" ]; then
  touch marker.txt
  exit 0
fi
if [ -f marker.txt ]; then echo "marker: present"; else echo "marker: absent"; fi
`)

	c := Case{
		ID:        "setup-order",
		Argv:      []string{"check"},
		SetupArgv: [][]string{{"setup"}},
	}

	outDir := t.TempDir()
	rec, err := runCaseSide([]string{stub}, c, outDir, "target", defaultTimeout)
	if err != nil {
		t.Fatalf("runCaseSide: %v", err)
	}
	if rec.Stdout == nil || !strings.Contains(*rec.Stdout, "marker: present") {
		t.Errorf("stdout = %v, want it to report the setup-created marker present", rec.Stdout)
	}
}

// TestRunCaseSide_MultipleSetupArgvStepsRunInOrder proves several
// setup_argv entries run in the order they're listed, not just the first.
func TestRunCaseSide_MultipleSetupArgvStepsRunInOrder(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `
if [ "$1" = "append" ]; then
  echo "$2" >> log.txt
  exit 0
fi
cat log.txt
`)

	c := Case{
		ID:   "setup-multi",
		Argv: []string{"dump"},
		SetupArgv: [][]string{
			{"append", "one"},
			{"append", "two"},
		},
	}

	outDir := t.TempDir()
	rec, err := runCaseSide([]string{stub}, c, outDir, "target", defaultTimeout)
	if err != nil {
		t.Fatalf("runCaseSide: %v", err)
	}
	if rec.Stdout == nil || *rec.Stdout != "one\ntwo\n" {
		t.Errorf("stdout = %v, want \"one\\ntwo\\n\" (setup steps ran in listed order)", rec.Stdout)
	}
}

// TestRunCaseSide_SetupArgvFailureAbortsBeforeMainArgv proves a nonzero-exit
// setup step fails the case as a runner error, and Argv itself never runs.
func TestRunCaseSide_SetupArgvFailureAbortsBeforeMainArgv(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `
if [ "$1" = "fail-setup" ]; then exit 7; fi
touch should-not-run.marker
`)

	c := Case{
		ID:        "setup-fails",
		Argv:      []string{"main"},
		SetupArgv: [][]string{{"fail-setup"}},
	}

	outDir := t.TempDir()
	_, err := runCaseSide([]string{stub}, c, outDir, "target", defaultTimeout)
	if err == nil {
		t.Fatal("runCaseSide: expected an error from the failing setup step, got nil")
	}
	if !strings.Contains(err.Error(), "setup_argv[0]") || !strings.Contains(err.Error(), "exited 7") {
		t.Errorf("error = %q, want it to name setup_argv[0] and its exit code 7", err.Error())
	}
}
