//go:build unix

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
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

	if err := Run(cfg); err != nil {
		t.Fatalf("Run: %v", err)
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

	if err := Run(cfg); err != nil {
		t.Fatalf("Run: %v", err)
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
