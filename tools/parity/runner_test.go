//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// TestRunCaseSide_PathExclusive_HidesRealBinary proves case.path_exclusive
// (ticket 08's doctor-git-missing mechanism) replaces PATH entirely with
// PathPrepend's directory, instead of merely leading it: a `git` reachable
// via the runner's own inherited PATH must NOT be found by the subprocess
// when path_exclusive is set and the fixture directory itself has no `git`.
func TestRunCaseSide_PathExclusive_HidesRealBinary(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `
if command -v git >/dev/null 2>&1; then
  echo "git found: $(command -v git)"
else
  echo "git not found"
fi
`)

	// A real `git` reachable via PATH, standing in for the runner host's
	// actual git -- path_exclusive must hide this even though it would
	// otherwise be found (a plain path_prepend does not: see
	// TestRunCaseSide_PathPrependShadowsRealBinary above, which relies on
	// exactly the opposite behaviour).
	realGitDir := t.TempDir()
	writeStubScript(t, realGitDir, "git", `echo "git version 1.0.0 (real)"`)

	caseDir := t.TempDir()
	emptyPathDir := filepath.Join(caseDir, "path")
	if err := os.MkdirAll(emptyPathDir, 0o755); err != nil {
		t.Fatalf("mkdir empty path fixture dir: %v", err)
	}

	c := Case{ID: "path-exclusive", Argv: []string{}, PathPrepend: "path", PathExclusive: true, Dir: caseDir}

	t.Setenv("PATH", realGitDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	outDir := t.TempDir()
	rec, err := runCaseSide([]string{stub}, c, outDir, "target", defaultTimeout)
	if err != nil {
		t.Fatalf("runCaseSide: %v", err)
	}
	if rec.Stdout == nil || strings.TrimSpace(*rec.Stdout) != "git not found" {
		t.Errorf("stdout = %v, want \"git not found\" (path_exclusive must hide every other PATH entry)", rec.Stdout)
	}
}

// TestRunCaseSide_TimeoutS_OverridesPerCase proves Case.TimeoutS (ticket
// 08) is honored as the per-run kill-timeout passed to runCaseSide's
// caller, independent of the default the caller would otherwise use --
// exercised here by passing a short duration derived from TimeoutS
// directly, the same way main.go's runCaseAllSides computes it.
func TestRunCaseSide_TimeoutS_OverridesPerCase(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `sleep 5; echo "should not print"`)

	c := Case{ID: "timeout-s", Argv: []string{}, TimeoutS: 1}

	outDir := t.TempDir()
	timeout := time.Duration(c.TimeoutS) * time.Second
	start := time.Now()
	rec, err := runCaseSide([]string{stub}, c, outDir, "target", timeout)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("runCaseSide: %v", err)
	}
	if !rec.TimedOut {
		t.Fatalf("TimedOut = false, want true (a 1s timeout must kill a 5s sleep)")
	}
	if elapsed >= 4*time.Second {
		t.Errorf("elapsed = %v, want well under the stub's 5s sleep (TimeoutS=1 must have applied)", elapsed)
	}
}

// TestCheckForbiddenSubstrings_DetectsInStdout proves a forbidden
// substring appearing in stdout fails the case closed with a clear error,
// even though the case's own exit code and everything else about the run
// was otherwise unremarkable.
func TestCheckForbiddenSubstrings_DetectsInStdout(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `echo "token=SECRET-VALUE-123"`)

	c := Case{ID: "leak-stdout", Argv: []string{}, ForbidSubstrings: []string{"SECRET-VALUE-123"}}

	outDir := t.TempDir()
	_, err := runCaseSide([]string{stub}, c, outDir, "target", defaultTimeout)
	if err == nil {
		t.Fatal("runCaseSide: expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "SECRET-VALUE-123") || !strings.Contains(err.Error(), "stdout") {
		t.Errorf("error = %q, want it to name the substring and stdout", err.Error())
	}
}

// TestCheckForbiddenSubstrings_DetectsInStderr mirrors the stdout case for
// stderr -- both streams are raw-output, either can leak a secret.
func TestCheckForbiddenSubstrings_DetectsInStderr(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `echo "token=SECRET-VALUE-123" >&2`)

	c := Case{ID: "leak-stderr", Argv: []string{}, ForbidSubstrings: []string{"SECRET-VALUE-123"}}

	outDir := t.TempDir()
	_, err := runCaseSide([]string{stub}, c, outDir, "target", defaultTimeout)
	if err == nil {
		t.Fatal("runCaseSide: expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "SECRET-VALUE-123") || !strings.Contains(err.Error(), "stderr") {
		t.Errorf("error = %q, want it to name the substring and stderr", err.Error())
	}
}

// TestCheckForbiddenSubstrings_DetectsInFsFile proves the scan also covers
// on-disk tree evidence (cwd/home), not just the two output streams --
// ticket 08's "fs" in "NEITHER side's stdout/stderr/fs".
func TestCheckForbiddenSubstrings_DetectsInFsFile(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `echo "SECRET-VALUE-123" > leaked.txt`)

	c := Case{ID: "leak-fs", Argv: []string{}, ForbidSubstrings: []string{"SECRET-VALUE-123"}}

	outDir := t.TempDir()
	_, err := runCaseSide([]string{stub}, c, outDir, "target", defaultTimeout)
	if err == nil {
		t.Fatal("runCaseSide: expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "SECRET-VALUE-123") || !strings.Contains(err.Error(), "leaked.txt") {
		t.Errorf("error = %q, want it to name the substring and the leaking fs path", err.Error())
	}
}

// TestCheckForbiddenSubstrings_CleanRunPasses proves the gate does not
// false-positive: a run whose output/fs never mentions the forbidden
// substring must succeed normally.
func TestCheckForbiddenSubstrings_CleanRunPasses(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `echo "all clear"`)

	c := Case{ID: "leak-none", Argv: []string{}, ForbidSubstrings: []string{"SECRET-VALUE-123"}}

	outDir := t.TempDir()
	rec, err := runCaseSide([]string{stub}, c, outDir, "target", defaultTimeout)
	if err != nil {
		t.Fatalf("runCaseSide: %v", err)
	}
	if rec.Stdout == nil || strings.TrimSpace(*rec.Stdout) != "all clear" {
		t.Errorf("stdout = %v, want \"all clear\"", rec.Stdout)
	}
}

// TestCheckForbiddenSubstrings_UnreadableTreeFileFailsClosed is ticket 08
// eval attempt 2's finding 1 reproducer, verbatim: a "file" tree entry
// naming a path that does not actually exist (or is otherwise unreadable)
// used to `continue` past it silently, treating an unscannable file as if
// it had been scanned clean. The gate must fail closed instead -- an
// unscannable file is exactly the case where a leak could be hiding.
func TestCheckForbiddenSubstrings_UnreadableTreeFileFailsClosed(t *testing.T) {
	err := checkForbiddenSubstrings(
		Case{ID: "unreadable", ForbidSubstrings: []string{"SECRET"}},
		"target", nil, nil,
		[]TreeEntry{{Path: "cwd/missing.txt", Kind: "file"}},
		map[string]string{"cwd": t.TempDir()},
	)
	if err == nil {
		t.Fatal("checkForbiddenSubstrings: expected a contextual error for an unreadable tree file, got nil (fail-open regression)")
	}
	if !strings.Contains(err.Error(), "missing.txt") {
		t.Errorf("error = %q, want it to name the unreadable path", err.Error())
	}
}

// TestCheckForbiddenSubstrings_EmptySubstringIsRejected is ticket 08 eval
// attempt 2's finding 1, second half: an empty forbid_substrings entry
// used to `continue` past itself, silently disabling the whole gate for
// that entry instead of erroring on what is almost certainly a
// misconfigured case.json.
func TestCheckForbiddenSubstrings_EmptySubstringIsRejected(t *testing.T) {
	err := checkForbiddenSubstrings(
		Case{ID: "empty-substring", ForbidSubstrings: []string{""}},
		"target", []byte("anything"), nil, nil, nil,
	)
	if err == nil {
		t.Fatal("checkForbiddenSubstrings: expected an error for an empty forbid_substrings entry, got nil")
	}
}

// TestRunCaseSide_ForbidSubstrings_RedactsEnvDeltaInWrittenRecord is ticket
// 08 eval attempt 2's finding 2 reproducer: checkForbiddenSubstrings only
// ever scanned stdout/stderr/fs, never env_delta, so a case's own secret
// literal (e.g. a fixture GITHUB_TOKEN) was written into record.json
// unconditionally -- the runner's non-leak claim never actually covered
// its own captured evidence. redactEnvDelta must replace it before
// writeRecordJSON ever sees it.
func TestRunCaseSide_ForbidSubstrings_RedactsEnvDeltaInWrittenRecord(t *testing.T) {
	const secret = "ghp-parity-fixture-DO-NOT-LEAK-0123456789"
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `echo "all clear"`)

	c := Case{
		ID:               "redact-env",
		Argv:             []string{},
		Env:              map[string]string{"GITHUB_TOKEN": secret},
		ForbidSubstrings: []string{secret},
	}

	outDir := t.TempDir()
	rec, err := runCaseSide([]string{stub}, c, outDir, "target", defaultTimeout)
	if err != nil {
		t.Fatalf("runCaseSide: %v", err)
	}
	if rec.EnvDelta["GITHUB_TOKEN"] == secret {
		t.Fatalf("EnvDelta[GITHUB_TOKEN] = %q, want it redacted (in-memory Record)", rec.EnvDelta["GITHUB_TOKEN"])
	}
	if strings.Contains(rec.EnvDelta["GITHUB_TOKEN"], secret) {
		t.Fatalf("EnvDelta[GITHUB_TOKEN] = %q, still contains the secret", rec.EnvDelta["GITHUB_TOKEN"])
	}

	recordPath := filepath.Join(outDir, "target", "redact-env", "record.json")
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("reading %s: %v", recordPath, err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatalf("record.json at %s contains the literal secret:\n%s", recordPath, data)
	}
	if !strings.Contains(string(data), "REDACTED") {
		t.Errorf("record.json = %s, want a REDACTED placeholder for GITHUB_TOKEN", data)
	}
}

// TestRedactEnvDelta_NoOpWhenNoForbiddenSubstrings proves every case
// without a secret to protect keeps its exact env_delta -- HOME,
// APM_CONFIG_DIR, etc. must never be touched by a case that sets no
// ForbidSubstrings, since diff.go's sandboxPathsFromEnvDelta and several
// tests read those values directly out of the record.
func TestRedactEnvDelta_NoOpWhenNoForbiddenSubstrings(t *testing.T) {
	env := map[string]string{"HOME": "/sandbox/home", "PATH": "/usr/bin"}
	got := redactEnvDelta(env, nil)
	if got["HOME"] != "/sandbox/home" || got["PATH"] != "/usr/bin" {
		t.Errorf("redactEnvDelta(env, nil) = %v, want env unchanged", got)
	}
}

// TestRedactEnvDelta_RedactsOnlyMatchingValues proves a value that happens
// to CONTAIN a forbidden substring under a non-sensitive-looking key is
// still replaced (the value-based path, independent of key name), while
// every unrelated value passes through untouched.
func TestRedactEnvDelta_RedactsOnlyMatchingValues(t *testing.T) {
	env := map[string]string{
		"WEIRDLY_NAMED_VAR": "ghp-secret-abc",
		"HOME":              "/sandbox/home",
	}
	got := redactEnvDelta(env, []string{"ghp-secret-abc"})
	if got["WEIRDLY_NAMED_VAR"] != "REDACTED" {
		t.Errorf("WEIRDLY_NAMED_VAR = %q, want REDACTED", got["WEIRDLY_NAMED_VAR"])
	}
	if got["HOME"] != "/sandbox/home" {
		t.Errorf("HOME = %q, want unchanged", got["HOME"])
	}
}

// TestRedactEnvDelta_RedactsSensitiveKeyEvenWithoutForbidSubstrings proves
// the "whole captured corpus" fix directly: a GITHUB_TOKEN-shaped key gets
// redacted unconditionally, with no forbidden substrings declared at all --
// this is what actually covers every doctor-* case that sets GITHUB_TOKEN
// purely to skip the Oracle's `gh auth token` fallback (per their own
// waivers.json entries), not just doctor-token-present (the one case that
// ALSO declares ForbidSubstrings to test the non-leak property itself).
func TestRedactEnvDelta_RedactsSensitiveKeyEvenWithoutForbidSubstrings(t *testing.T) {
	env := map[string]string{"GITHUB_TOKEN": "ghp-some-fixture-value", "HOME": "/sandbox/home"}
	got := redactEnvDelta(env, nil)
	if got["GITHUB_TOKEN"] != "REDACTED" {
		t.Errorf("GITHUB_TOKEN = %q, want REDACTED even with no forbid_substrings declared", got["GITHUB_TOKEN"])
	}
	if got["HOME"] != "/sandbox/home" {
		t.Errorf("HOME = %q, want unchanged", got["HOME"])
	}
}

// TestRunCaseSide_GithubTokenRedactedInRecordEvenWithoutForbidSubstrings is
// the end-to-end reproduction of the actual attempt-2 gap: a case that sets
// GITHUB_TOKEN but does NOT declare ForbidSubstrings (as nearly every
// doctor-* fixture in this corpus does, since ForbidSubstrings is only
// ever declared by doctor-token-present) must still never write the
// literal into its written record.json.
func TestRunCaseSide_GithubTokenRedactedInRecordEvenWithoutForbidSubstrings(t *testing.T) {
	const secret = "ghp-parity-fixture-DO-NOT-LEAK-0123456789"
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `echo "all clear"`)

	c := Case{
		ID:   "no-forbid-declared",
		Argv: []string{},
		Env:  map[string]string{"GITHUB_TOKEN": secret},
	}

	outDir := t.TempDir()
	rec, err := runCaseSide([]string{stub}, c, outDir, "target", defaultTimeout)
	if err != nil {
		t.Fatalf("runCaseSide: %v", err)
	}
	if rec.EnvDelta["GITHUB_TOKEN"] == secret {
		t.Fatalf("EnvDelta[GITHUB_TOKEN] = %q, want it redacted even without ForbidSubstrings declared", rec.EnvDelta["GITHUB_TOKEN"])
	}

	recordPath := filepath.Join(outDir, "target", "no-forbid-declared", "record.json")
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("reading %s: %v", recordPath, err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatalf("record.json at %s contains the literal secret despite no ForbidSubstrings declared:\n%s", recordPath, data)
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
