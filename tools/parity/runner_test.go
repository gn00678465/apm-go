//go:build unix

package main

import (
	"os"
	"path/filepath"
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

// TestRunCaseSide_CapturesFilesWrittenUnderHome proves postRunTree's HOME
// coverage (ticket 02 attempt 3, amending ticket 01 AC5): a stub that writes
// under $HOME/.apm/ -- exactly what the real Oracle does with its
// marketplace registry, ignoring APM_CONFIG_DIR entirely -- must show up in
// the record's tree under the "home/" label, and its evidence bytes must be
// copied out before the sandbox is torn down.
func TestRunCaseSide_CapturesFilesWrittenUnderHome(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `
mkdir -p "$HOME/.apm"
echo '{"marketplaces":[]}' > "$HOME/.apm/x.json"
exit 0
`)

	c := Case{ID: "writes-home", Argv: []string{"seed"}}

	outDir := t.TempDir()
	rec, err := runCaseSide([]string{stub}, c, outDir, "target", defaultTimeout)
	if err != nil {
		t.Fatalf("runCaseSide: %v", err)
	}

	found := false
	for _, e := range rec.Tree {
		if e.Path == "home/.apm/x.json" {
			found = true
			if e.Kind != "file" {
				t.Errorf("home/.apm/x.json kind = %q, want file", e.Kind)
			}
		}
	}
	if !found {
		t.Fatalf("tree = %+v, want an entry for home/.apm/x.json", rec.Tree)
	}

	fsPath := filepath.Join(outDir, "target", "writes-home", "fs", "home", ".apm", "x.json")
	data, err := os.ReadFile(fsPath)
	if err != nil {
		t.Fatalf("evidence file %s was not copied out: %v", fsPath, err)
	}
	if string(data) != `{"marketplaces":[]}`+"\n" {
		t.Errorf("evidence file contents = %q", data)
	}
}

// TestRunCaseSide_ExpandsTMPInCaseEnv proves a case.env value containing
// "<TMP>" is expanded to the run's own sandbox cwd before the subprocess
// sees it (ticket 15: the registry-explicit-config-dir case needs this to
// point APM_CONFIG_DIR at a path inside its own cwd).
func TestRunCaseSide_ExpandsTMPInCaseEnv(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `echo "$APM_CONFIG_DIR"`)

	c := Case{ID: "tmp-expansion", Argv: []string{}, Env: map[string]string{"APM_CONFIG_DIR": "<TMP>/altcfg"}}

	outDir := t.TempDir()
	rec, err := runCaseSide([]string{stub}, c, outDir, "target", defaultTimeout)
	if err != nil {
		t.Fatalf("runCaseSide: %v", err)
	}

	wantCwd := sandboxCwdFromHome(rec.EnvDelta["HOME"])
	if wantCwd == "" {
		t.Fatalf("could not derive cwd from EnvDelta[HOME] = %q", rec.EnvDelta["HOME"])
	}
	want := wantCwd + "/altcfg"
	if rec.EnvDelta["APM_CONFIG_DIR"] != want {
		t.Errorf("EnvDelta[APM_CONFIG_DIR] = %q, want %q", rec.EnvDelta["APM_CONFIG_DIR"], want)
	}
	if rec.Stdout == nil || strings.TrimSpace(*rec.Stdout) != want {
		t.Errorf("stdout = %v, want the subprocess to see the expanded path %q", rec.Stdout, want)
	}
}

// TestRunCaseSide_LoadCasesRelativeDir_PathPrependStillResolves is (b) of
// ticket 10 attempt 3's regression pair (eval-ticket-10-r2.md §4): a case
// loaded via a RELATIVE -cases flag (LoadCases, not a hand-built Case struct
// like TestRunCaseSide_PathPrependShadowsRealBinary above) must still have
// PathPrepend's fixture binary shadow the real one, even when the runner's
// own cwd changes before the case actually runs -- exactly what happens
// between LoadCases (relative to the invoking shell's cwd) and runCaseSide
// (which chdirs the subprocess into its own sandbox). Before the fix,
// Case.Dir stayed relative, runner.go's PATH join resolved it against
// whatever the process's cwd happened to be at run time, and the fixture
// "path/git" was never found -- the real host git answered instead.
func TestRunCaseSide_LoadCasesRelativeDir_PathPrependStillResolves(t *testing.T) {
	parent := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() {
		if err := os.Chdir(origWD); err != nil {
			t.Fatalf("restoring cwd: %v", err)
		}
	}()
	if err := os.Chdir(parent); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	writeCase(t, "cases", "path-prepend-relative", `{"id": "path-prepend-relative", "argv": [], "path_prepend": "path"}`)
	pathDir := filepath.Join(parent, "cases", "path-prepend-relative", "path")
	if err := os.MkdirAll(pathDir, 0o755); err != nil {
		t.Fatalf("mkdir path fixture dir: %v", err)
	}
	writeStubScript(t, pathDir, "git", `echo "git version 9.9.9 (fixture)"`)

	cases, err := LoadCases("cases")
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(cases))
	}
	c := cases[0]

	// Move the process's cwd elsewhere BEFORE running the case -- if
	// Case.Dir were still relative, PathPrepend would now resolve against
	// this unrelated directory instead of the case's own.
	elsewhere := t.TempDir()
	if err := os.Chdir(elsewhere); err != nil {
		t.Fatalf("Chdir elsewhere: %v", err)
	}

	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `git --version`)

	outDir := t.TempDir()
	rec, err := runCaseSide([]string{stub}, c, outDir, "target", defaultTimeout)
	if err != nil {
		t.Fatalf("runCaseSide: %v", err)
	}
	if rec.Stdout == nil || !strings.Contains(*rec.Stdout, "git version 9.9.9 (fixture)") {
		t.Errorf("stdout = %v, want the fixture git's output (a relative -cases flag must not break path_prepend)", rec.Stdout)
	}
}

// TestRunCaseSide_PathPrependShadowsRealBinary proves case.path_prepend
// (ticket 08's fault-injection mechanism, added here per ticket 10 attempt
// 2 for doctor-healthy) puts its case-relative directory at the FRONT of
// PATH: a fixture `git` shell script there is what the case's own binary
// finds, not whatever real `git` (if any) is on the runner host's PATH.
func TestRunCaseSide_PathPrependShadowsRealBinary(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `git --version`)

	caseDir := t.TempDir()
	pathDir := filepath.Join(caseDir, "path")
	if err := os.MkdirAll(pathDir, 0o755); err != nil {
		t.Fatalf("mkdir path fixture dir: %v", err)
	}
	writeStubScript(t, pathDir, "git", `echo "git version 9.9.9 (fixture)"`)

	c := Case{ID: "path-prepend", Argv: []string{}, PathPrepend: "path", Dir: caseDir}

	outDir := t.TempDir()
	rec, err := runCaseSide([]string{stub}, c, outDir, "target", defaultTimeout)
	if err != nil {
		t.Fatalf("runCaseSide: %v", err)
	}
	if rec.Stdout == nil || !strings.Contains(*rec.Stdout, "git version 9.9.9 (fixture)") {
		t.Errorf("stdout = %v, want the fixture git's output (path_prepend must shadow the real git)", rec.Stdout)
	}
}
