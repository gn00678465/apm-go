//go:build unix

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRun_EndToEndWithStubBinaries wires LoadCases, runCaseSide, and the
// JSONL/run.json writers together the same way main() does, using stub
// shell scripts as both the Oracle and the Target so go test never invokes
// the real Oracle.
func TestRun_EndToEndWithStubBinaries(t *testing.T) {
	scriptDir := t.TempDir()
	oracle := writeStubScript(t, scriptDir, "oracle.sh", `
if [ "$1" = "--version" ]; then echo "oracle 1.2.3"; exit 0; fi
echo "oracle argv: $*"
exit 0
`)
	target := writeStubScript(t, scriptDir, "target.sh", `
if [ "$1" = "--version" ]; then echo "target 9.9.9"; exit 0; fi
echo "target argv: $*"
exit 3
`)

	casesDir := t.TempDir()
	writeCase(t, casesDir, "echo-case", `{"id": "echo-case", "argv": ["hello", "world"]}`)

	outDir := t.TempDir()
	cfg := Config{
		CasesDir:  casesDir,
		OutDir:    outDir,
		OracleCmd: []string{oracle},
		TargetBin: []string{target},
		Timeout:   defaultTimeout,
	}

	// captureRun (not runCases/Run) so this test exercises LoadCases/
	// runCaseSide/the JSONL/run.json writers without also driving ticket
	// 02's diff/waiver-gate stage -- this test's oracle/target stubs
	// deliberately differ, which runCases would now (correctly) fail on --
	// or the Oracle/Target pin preflight (ticket 03, preflight_test.go),
	// which needs a real git checkout to resolve APM_ORACLE_CMD's
	// --project argument against.
	preflight := Preflight{
		OracleVersion: getVersion(cfg.OracleCmd, cfg.Timeout),
		TargetVersion: getVersion(cfg.TargetBin, cfg.Timeout),
	}
	if _, err := captureRun(cfg, preflight); err != nil {
		t.Fatalf("captureRun: %v", err)
	}

	// run.json header.
	headerData, err := os.ReadFile(filepath.Join(outDir, "run.json"))
	if err != nil {
		t.Fatalf("reading run.json: %v", err)
	}
	var header runHeader
	if err := json.Unmarshal(headerData, &header); err != nil {
		t.Fatalf("unmarshalling run.json: %v", err)
	}
	if header.OracleVersion != "oracle 1.2.3" {
		t.Errorf("OracleVersion = %q", header.OracleVersion)
	}
	if header.TargetVersion != "target 9.9.9" {
		t.Errorf("TargetVersion = %q", header.TargetVersion)
	}
	if _, err := time.Parse(time.RFC3339, header.Timestamp); err != nil {
		t.Errorf("Timestamp = %q not RFC3339: %v", header.Timestamp, err)
	}

	// oracle.jsonl / target.jsonl: one line, matching each side's own exit
	// code (a case exiting non-zero is captured, not a runner failure).
	oracleRec := readSoleJSONLRecord(t, filepath.Join(outDir, "oracle.jsonl"))
	if oracleRec.ID != "echo-case" || oracleRec.ExitCode != 0 {
		t.Errorf("oracle record = %+v, want id=echo-case exit_code=0", oracleRec)
	}
	if oracleRec.Stdout == nil || !strings.Contains(*oracleRec.Stdout, "hello world") {
		t.Errorf("oracle stdout = %v, want it to contain the argv tail", oracleRec.Stdout)
	}

	targetRec := readSoleJSONLRecord(t, filepath.Join(outDir, "target.jsonl"))
	if targetRec.ID != "echo-case" || targetRec.ExitCode != 3 {
		t.Errorf("target record = %+v, want id=echo-case exit_code=3", targetRec)
	}

	// record.json exists per side per case with full evidence.
	for _, side := range []string{"oracle", "target"} {
		recordPath := filepath.Join(outDir, side, "echo-case", "record.json")
		if _, err := os.Stat(recordPath); err != nil {
			t.Errorf("%s: %v", recordPath, err)
		}
	}
}

// TestRun_EndToEndCapturesNonUTF8BytesExactly is the ticket's required
// reproducer: a stub emitting the invalid-UTF-8 byte 0xff must come through
// byte-exact in stdout.bin, with record.json's sha256 matching, rather than
// being mangled to U+FFFD by JSON string encoding.
func TestRun_EndToEndCapturesNonUTF8BytesExactly(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `
if [ "$1" = "--version" ]; then echo "stub 1.0"; exit 0; fi
printf '\377\n'
`)

	casesDir := t.TempDir()
	writeCase(t, casesDir, "raw-bytes", `{"id": "raw-bytes", "argv": []}`)

	outDir := t.TempDir()
	cfg := Config{
		CasesDir:  casesDir,
		OutDir:    outDir,
		OracleCmd: []string{stub},
		TargetBin: []string{stub},
		Timeout:   defaultTimeout,
	}

	if err := runCases(cfg, Preflight{}); err != nil {
		t.Fatalf("runCases: %v", err)
	}

	wantBytes := []byte{0xff, '\n'}

	for _, side := range []string{"oracle", "target"} {
		caseOutDir := filepath.Join(outDir, side, "raw-bytes")

		binData, err := os.ReadFile(filepath.Join(caseOutDir, "stdout.bin"))
		if err != nil {
			t.Fatalf("%s: reading stdout.bin: %v", side, err)
		}
		if !bytes.Equal(binData, wantBytes) {
			t.Errorf("%s: stdout.bin = % x, want % x", side, binData, wantBytes)
		}

		recData, err := os.ReadFile(filepath.Join(caseOutDir, "record.json"))
		if err != nil {
			t.Fatalf("%s: reading record.json: %v", side, err)
		}
		var rec Record
		if err := json.Unmarshal(recData, &rec); err != nil {
			t.Fatalf("%s: unmarshalling record.json: %v", side, err)
		}
		if rec.Stdout != nil {
			t.Errorf("%s: record.json inlined non-UTF-8 stdout as %q, want field omitted", side, *rec.Stdout)
		}
		if rec.StdoutBytes != len(wantBytes) {
			t.Errorf("%s: StdoutBytes = %d, want %d", side, rec.StdoutBytes, len(wantBytes))
		}
		if want := sha256Hex(wantBytes); rec.StdoutSHA256 != want {
			t.Errorf("%s: StdoutSHA256 = %q, want %q", side, rec.StdoutSHA256, want)
		}
	}
}

func readSoleJSONLRecord(t *testing.T, path string) jsonlRecord {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) != 1 {
		t.Fatalf("%s: expected exactly 1 line, got %d", path, len(lines))
	}

	var rec jsonlRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("unmarshalling %s: %v", path, err)
	}
	return rec
}

// TestRunCaseAllSides_TimeoutSOverridesConfigTimeout proves Case.TimeoutS
// (ticket 08) is what actually reaches runCaseSide via runCaseAllSides,
// not just cfg.Timeout: cfg.Timeout here is generously long (30s), so a
// stub sleeping 5s only gets killed early if the case's own timeout_s (1s)
// is the one actually applied.
func TestRunCaseAllSides_TimeoutSOverridesConfigTimeout(t *testing.T) {
	scriptDir := t.TempDir()
	oracle := writeStubScript(t, scriptDir, "oracle.sh", `sleep 5; echo "should not print"`)
	target := writeStubScript(t, scriptDir, "target.sh", `sleep 5; echo "should not print"`)

	c := Case{ID: "timeout-s-wiring", Argv: []string{}, TimeoutS: 1}

	outDir := t.TempDir()
	cfg := Config{
		OutDir:    outDir,
		OracleCmd: []string{oracle},
		TargetBin: []string{target},
		Timeout:   30 * time.Second,
	}

	start := time.Now()
	pair, err := runCaseAllSides(cfg, c)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("runCaseAllSides: %v", err)
	}
	if !pair.Oracle.TimedOut || !pair.Target.TimedOut {
		t.Fatalf("TimedOut = oracle:%v target:%v, want both true", pair.Oracle.TimedOut, pair.Target.TimedOut)
	}
	if elapsed >= 20*time.Second {
		t.Errorf("elapsed = %v, want well under cfg.Timeout (case's own timeout_s=1 must have applied)", elapsed)
	}
}

func TestResolveCmd_DefaultsAndEnvOverride(t *testing.T) {
	os.Unsetenv("APM_PARITY_TEST_CMD")
	got := resolveCmd("APM_PARITY_TEST_CMD", "./bin/apm-go")
	if len(got) != 1 || got[0] != "./bin/apm-go" {
		t.Errorf("resolveCmd default = %v", got)
	}

	// A whitespace-only override must fall back to the default rather than
	// resolving to an empty argv, which would later panic on argv[0].
	t.Setenv("APM_PARITY_TEST_CMD", "   ")
	got = resolveCmd("APM_PARITY_TEST_CMD", "./bin/apm-go")
	if len(got) != 1 || got[0] != "./bin/apm-go" {
		t.Errorf("resolveCmd with whitespace-only override = %v, want default", got)
	}

	t.Setenv("APM_PARITY_TEST_CMD", "uv run --project /x apm")
	got = resolveCmd("APM_PARITY_TEST_CMD", "./bin/apm-go")
	want := []string{"uv", "run", "--project", "/x", "apm"}
	if len(got) != len(want) {
		t.Fatalf("resolveCmd = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("resolveCmd[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRunCases_UnwaivedDiffFailsGate proves ticket 02's core wiring: two
// stubs that genuinely disagree, with no waiver in play, must fail
// runCases with a plain error (main() falls through to exit 1 -- no
// *preflightError/*waiverValidationError/*selfTestError involved).
func TestRunCases_UnwaivedDiffFailsGate(t *testing.T) {
	scriptDir := t.TempDir()
	oracle := writeStubScript(t, scriptDir, "oracle.sh", `echo "oracle output"; exit 0`)
	target := writeStubScript(t, scriptDir, "target.sh", `echo "target output"; exit 0`)

	casesDir := t.TempDir()
	writeCase(t, casesDir, "c1", `{"id": "c1", "argv": []}`)

	cfg := Config{
		CasesDir:    casesDir,
		OutDir:      t.TempDir(),
		OracleCmd:   []string{oracle},
		TargetBin:   []string{target},
		Timeout:     defaultTimeout,
		WaiversPath: filepath.Join(t.TempDir(), "no-such-waivers.json"),
	}

	err := runCases(cfg, Preflight{OracleCommit: "pin"})
	if err == nil {
		t.Fatal("runCases: expected an unwaived-diff error, got nil")
	}
	var ec interface{ ExitCode() int }
	if errors.As(err, &ec) {
		t.Errorf("runCases error %v unexpectedly carries ExitCode() = %d, want a plain error (exit 1)", err, ec.ExitCode())
	}

	diffData, readErr := os.ReadFile(filepath.Join(cfg.OutDir, "diff.jsonl"))
	if readErr != nil {
		t.Fatalf("reading diff.jsonl: %v", readErr)
	}
	if !strings.Contains(string(diffData), `"stdout"`) {
		t.Errorf("diff.jsonl = %s, want it to mention the stdout diff", diffData)
	}
}

// TestRunCases_WaivedDiffPasses proves a fully-covering waiver both
// silences the gate (runCases returns nil) AND still shows up as
// waived:true in diff.jsonl (acceptance: "waivers are visible, not
// hidden").
func TestRunCases_WaivedDiffPasses(t *testing.T) {
	scriptDir := t.TempDir()
	oracle := writeStubScript(t, scriptDir, "oracle.sh", `echo "oracle output"; exit 0`)
	target := writeStubScript(t, scriptDir, "target.sh", `echo "target output"; exit 0`)

	casesDir := t.TempDir()
	writeCase(t, casesDir, "c1", `{"id": "c1", "argv": []}`)

	waiversPath := filepath.Join(t.TempDir(), "waivers.json")
	if err := os.WriteFile(waiversPath, []byte(`[{"id":"c1","fields":["stdout"],"reason":"known diff","oracle_commit":"pin"}]`), 0o644); err != nil {
		t.Fatalf("writing waivers.json: %v", err)
	}

	cfg := Config{
		CasesDir:    casesDir,
		OutDir:      t.TempDir(),
		OracleCmd:   []string{oracle},
		TargetBin:   []string{target},
		Timeout:     defaultTimeout,
		WaiversPath: waiversPath,
	}

	if err := runCases(cfg, Preflight{OracleCommit: "pin"}); err != nil {
		t.Fatalf("runCases: %v, want nil (fully-covering waiver)", err)
	}

	diffData, err := os.ReadFile(filepath.Join(cfg.OutDir, "diff.jsonl"))
	if err != nil {
		t.Fatalf("reading diff.jsonl: %v", err)
	}
	var cd CaseDiff
	if err := json.Unmarshal(bytes.TrimSpace(diffData), &cd); err != nil {
		t.Fatalf("unmarshalling diff.jsonl: %v\n%s", err, diffData)
	}
	if !cd.Waived {
		t.Errorf("diff.jsonl entry Waived = false, want true")
	}
	if !fieldsEqual(cd.Fields, []string{"stdout"}) {
		t.Errorf("diff.jsonl entry Fields = %v, want [stdout] (waived, not hidden)", cd.Fields)
	}
}

// TestRunCases_UnknownWaiverIDFailsExit2 proves a waivers.json referencing
// a case id that was never loaded fails closed before any real diff logic
// runs, via *waiverValidationError (main() exit 2).
func TestRunCases_UnknownWaiverIDFailsExit2(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `exit 0`)

	casesDir := t.TempDir()
	writeCase(t, casesDir, "c1", `{"id": "c1", "argv": []}`)

	waiversPath := filepath.Join(t.TempDir(), "waivers.json")
	if err := os.WriteFile(waiversPath, []byte(`[{"id":"ghost","fields":["stdout"],"reason":"r","oracle_commit":"pin"}]`), 0o644); err != nil {
		t.Fatalf("writing waivers.json: %v", err)
	}

	cfg := Config{
		CasesDir:    casesDir,
		OutDir:      t.TempDir(),
		OracleCmd:   []string{stub},
		TargetBin:   []string{stub},
		Timeout:     defaultTimeout,
		WaiversPath: waiversPath,
	}

	err := runCases(cfg, Preflight{OracleCommit: "pin"})
	var we *waiverValidationError
	if !errors.As(err, &we) {
		t.Fatalf("runCases error %v is not a *waiverValidationError, so main() would not exit 2", err)
	}
}

// TestRunCases_InvalidWaiverInvokesNeitherOracleNorTarget proves ticket 02
// attempt 2's W2 fix: waivers.json is validated BEFORE any real case
// executes, not merely before the gate decision at the end. The stub
// scripts each touch a marker file the moment they're invoked; if
// validation genuinely ran first, those markers must never appear.
func TestRunCases_InvalidWaiverInvokesNeitherOracleNorTarget(t *testing.T) {
	scriptDir := t.TempDir()
	oracleMarker := filepath.Join(scriptDir, "oracle-invoked")
	targetMarker := filepath.Join(scriptDir, "target-invoked")
	oracle := writeStubScript(t, scriptDir, "oracle.sh", `touch `+oracleMarker+`; exit 0`)
	target := writeStubScript(t, scriptDir, "target.sh", `touch `+targetMarker+`; exit 0`)

	casesDir := t.TempDir()
	writeCase(t, casesDir, "c1", `{"id": "c1", "argv": []}`)

	waiversPath := filepath.Join(t.TempDir(), "waivers.json")
	if err := os.WriteFile(waiversPath, []byte(`[{"id":"ghost","fields":["stdout"],"reason":"r","oracle_commit":"pin"}]`), 0o644); err != nil {
		t.Fatalf("writing waivers.json: %v", err)
	}

	cfg := Config{
		CasesDir:    casesDir,
		OutDir:      filepath.Join(t.TempDir(), "out"),
		OracleCmd:   []string{oracle},
		TargetBin:   []string{target},
		Timeout:     defaultTimeout,
		WaiversPath: waiversPath,
	}

	err := runCases(cfg, Preflight{OracleCommit: "pin"})
	var we *waiverValidationError
	if !errors.As(err, &we) {
		t.Fatalf("runCases error %v is not a *waiverValidationError, so main() would not exit 2", err)
	}

	if _, statErr := os.Stat(oracleMarker); !os.IsNotExist(statErr) {
		t.Error("oracle stub was invoked despite an invalid waivers.json -- validation must run before any real case")
	}
	if _, statErr := os.Stat(targetMarker); !os.IsNotExist(statErr) {
		t.Error("target stub was invoked despite an invalid waivers.json -- validation must run before any real case")
	}
	if _, statErr := os.Stat(cfg.OutDir); !os.IsNotExist(statErr) {
		t.Errorf("%s exists after a waiver validation failure, want no output directory created", cfg.OutDir)
	}
}

// TestRunCases_NegativeControlWaiverOnProductCaseFailsExit2 proves ticket 02
// attempt 2's W3 fix through the full runCases flow: a case whose manifest
// doesn't declare expected_taxonomy ["negative-control"] can't be waived
// with that reserved taxonomy, even though the waiver otherwise fully
// covers the diff and would exit 0 if accepted (eval-ticket-02.md's W3
// reproducer, which previously got exit 0).
func TestRunCases_NegativeControlWaiverOnProductCaseFailsExit2(t *testing.T) {
	scriptDir := t.TempDir()
	oracle := writeStubScript(t, scriptDir, "oracle.sh", `echo "oracle output"; exit 0`)
	target := writeStubScript(t, scriptDir, "target.sh", `echo "target output"; exit 1`)

	casesDir := t.TempDir()
	// No expected_taxonomy: an ordinary product case, not a declared
	// negative control.
	writeCase(t, casesDir, "product", `{"id": "product", "argv": []}`)

	waiversPath := filepath.Join(t.TempDir(), "waivers.json")
	waiver := `[{"id":"product","fields":["stdout","exit_code"],"taxonomy":"negative-control","reason":"product negative-control probe","oracle_commit":"pin"}]`
	if err := os.WriteFile(waiversPath, []byte(waiver), 0o644); err != nil {
		t.Fatalf("writing waivers.json: %v", err)
	}

	cfg := Config{
		CasesDir:    casesDir,
		OutDir:      filepath.Join(t.TempDir(), "out"),
		OracleCmd:   []string{oracle},
		TargetBin:   []string{target},
		Timeout:     defaultTimeout,
		WaiversPath: waiversPath,
	}

	err := runCases(cfg, Preflight{OracleCommit: "pin"})
	var we *waiverValidationError
	if !errors.As(err, &we) {
		t.Fatalf("runCases error %v is not a *waiverValidationError, so main() would not exit 2 (negative-control must be rejected for a product case)", err)
	}
}

// TestRealCases_FixtureDirLoads is a regression check on the two product
// case fixtures ticket 02 restores (acceptance: "The two product cases
// from 01 (--version, doctor --help) remain").
func TestRealCases_FixtureDirLoads(t *testing.T) {
	cases, err := LoadCases("cases")
	if err != nil {
		t.Fatalf("LoadCases(\"cases\"): %v", err)
	}

	byID := make(map[string]Case, len(cases))
	for _, c := range cases {
		byID[c.ID] = c
	}

	version, ok := byID["version"]
	if !ok {
		t.Fatal(`cases/: missing the "version" case`)
	}
	if !fieldsEqual(version.Argv, []string{"--version"}) {
		t.Errorf("version.Argv = %v, want [--version]", version.Argv)
	}
	if !fieldsEqual(version.ExpectedTaxonomy, []string{"negative-control"}) {
		t.Errorf("version.ExpectedTaxonomy = %v, want [negative-control]", version.ExpectedTaxonomy)
	}

	doctorHelp, ok := byID["doctor-help"]
	if !ok {
		t.Fatal(`cases/: missing the "doctor-help" case`)
	}
	if !fieldsEqual(doctorHelp.Argv, []string{"doctor", "--help"}) {
		t.Errorf("doctor-help.Argv = %v, want [doctor --help]", doctorHelp.Argv)
	}
}

// TestRealWaiversJSON_ValidatesAgainstPin is a regression check that the
// checked-in waivers.json is internally consistent: every entry's
// oracle_commit matches the embedded oracle.pin, the reserved
// negative-control taxonomy is only ever used on a case that itself
// declares expected_taxonomy ["negative-control"], and (ticket 02 attempt
// 2: "No bulk waivers") it contains EXACTLY the version, doctor-help,
// (ticket 07: pack --format agent-plugin/apm refuse the unimplemented
// exporters -- .review/ticket-review.md §D authorizes exactly these two
// field-precise waivers) pack-refuse-agent-plugin/pack-refuse-apm, and
// doctor-healthy (attempt 2: box-drawing/padding `stdout` + the Oracle-only
// `gh` device-id `tree` path) entries, plus (ticket 15: the runner no
// longer forces APM_CONFIG_DIR, so browse-unknown-marketplace/list-empty's
// registry-location `tree` waivers no longer apply and are dropped)
// registry-explicit-config-dir, the one case that exercises apm-go's
// APM_CONFIG_DIR override as a path-precise `tree` waiver, plus (ticket 10
// attempt 3: the [>]/[i] prefix and Case.Dir fixes made this case's `stdout`
// diff legitimate -- box-drawing/padding only, same shape as
// doctor-healthy's) search-basic-hit's `stdout`-only entry (its `tree` diff,
// ticket 05's open file:// vs bare-path registry-serialization gap, stays
// unwaived on purpose), plus (ticket 08: fault-injection evidence backfill
// for doctor -- git-missing/nonzero, the four network-failure-kind cases,
// network-timeout, token-present/absent, and the seven marketplace-config
// cases) each of that ticket's own doctor-* waivers, plus (ticket 11: the
// missing Structure check + help drift) validate-help/-checkrefs-off/
// -checkrefs-on/-structure-fail's own `stdout`-only (`stderr`/`error_body`
// too for -structure-fail) waivers -- every one field-precise per its own
// reason -- no other case ever earns a bulk/wildcard waiver here.
func TestRealWaiversJSON_ValidatesAgainstPin(t *testing.T) {
	pin, err := pinnedOracleCommit()
	if err != nil {
		t.Fatalf("pinnedOracleCommit: %v", err)
	}

	waivers, err := loadWaivers("waivers.json")
	if err != nil {
		t.Fatalf("loadWaivers: %v", err)
	}

	cases, err := LoadCases("cases")
	if err != nil {
		t.Fatalf("LoadCases(\"cases\"): %v", err)
	}
	byID := make(map[string]Case, len(cases))
	for _, c := range cases {
		byID[c.ID] = c
	}
	if err := validateWaivers(waivers, byID, pin); err != nil {
		t.Errorf("validateWaivers: %v", err)
	}

	gotIDs := make([]string, 0, len(waivers))
	for _, w := range waivers {
		gotIDs = append(gotIDs, w.ID)
	}
	wantIDs := []string{
		"version", "doctor-healthy", "doctor-help", "pack-refuse-agent-plugin", "pack-refuse-apm",
		"registry-explicit-config-dir", "search-basic-hit",
		"doctor-git-missing", "doctor-git-nonzero",
		"doctor-network-dns-fail", "doctor-network-auth-fail", "doctor-network-not-found", "doctor-network-tls-fail", "doctor-network-timeout",
		"doctor-token-present", "doctor-token-absent",
		"doctor-config-none", "doctor-config-apmyml-valid", "doctor-config-legacy", "doctor-config-both",
		"doctor-config-apmyml-malformed", "doctor-config-legacy-malformed", "doctor-config-duplicate-names",
		"validate-help", "validate-checkrefs-off", "validate-checkrefs-on", "validate-structure-fail",
		"pack-no-flag", "pack-claude-plugin-flag", "pack-format-claude", "pack-format-claude-plugin", "pack-format-plugin",
		"pack-format-conflict", "pack-format-empty", "pack-format-unknown",
		"browse-unknown-marketplace", "list-empty", "search-unknown-marketplace",
		"plugin-init-no-flag", "plugin-init-format-plugin", "plugin-init-format-claude", "plugin-init-format-claude-plugin",
		"plugin-init-claude-plugin-flag", "plugin-init-format-agent-plugin",
		"plugin-init-existing-apmyml-yes", "plugin-init-existing-pluginjson-only-yes", "plugin-init-existing-mcpjson-agent-yes",
		"plugin-init-unicode-author", "plugin-init-normalise-upper", "plugin-init-normalise-underscore", "plugin-init-normalise-space",
		"plugin-init-conflict", "plugin-init-empty", "plugin-init-unknown", "plugin-init-format-apm",
		"plugin-init-existing-pluginjson-no-yes", "plugin-init-help",
		"search-missing-at", "search-empty-query", "search-empty-marketplace",
		"search-zero-results", "search-last-at-split", "search-limit-1", "search-tag-hit",
		"search-description-truncation", "search-help",
		"pack-archive", "pack-legacy-skill-paths",
	}
	if !fieldsEqual(gotIDs, wantIDs) {
		t.Errorf("waivers.json ids = %v, want exactly %v (ticket 02 attempt 2: no bulk waivers)", gotIDs, wantIDs)
	}
}
