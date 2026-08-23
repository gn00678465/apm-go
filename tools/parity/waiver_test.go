//go:build unix

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWaivers_MissingFileReturnsEmptyNotError(t *testing.T) {
	waivers, err := loadWaivers(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("loadWaivers: %v", err)
	}
	if len(waivers) != 0 {
		t.Errorf("waivers = %v, want none", waivers)
	}
}

func TestLoadWaivers_ParsesEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "waivers.json")
	writeJSON(t, path, `[{"id":"version","fields":["stdout"],"taxonomy":"negative-control","oracle_commit":"abc","reason":"r","owner":"o","eval_plan_ref":"ref"}]`)

	waivers, err := loadWaivers(path)
	if err != nil {
		t.Fatalf("loadWaivers: %v", err)
	}
	if len(waivers) != 1 {
		t.Fatalf("waivers = %v, want 1 entry", waivers)
	}
	w := waivers[0]
	if w.ID != "version" || w.OracleCommit != "abc" || w.Reason != "r" || w.Owner != "o" || w.EvalPlanRef != "ref" {
		t.Errorf("waiver = %+v", w)
	}
	if !fieldsEqual(w.Fields, []string{"stdout"}) {
		t.Errorf("Fields = %v, want [stdout]", w.Fields)
	}
}

func writeJSON(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func TestValidateWaivers_UnknownIDFailsExit2(t *testing.T) {
	err := validateWaivers([]Waiver{{ID: "ghost", Fields: []string{"stdout"}, Reason: "r", OracleCommit: "pin"}},
		map[string]bool{"version": true}, "pin")
	assertWaiverValidationError(t, err)
}

func TestValidateWaivers_EmptyReasonFailsExit2(t *testing.T) {
	err := validateWaivers([]Waiver{{ID: "version", Fields: []string{"stdout"}, Reason: "", OracleCommit: "pin"}},
		map[string]bool{"version": true}, "pin")
	assertWaiverValidationError(t, err)
}

func TestValidateWaivers_EmptyFieldsFailsExit2(t *testing.T) {
	err := validateWaivers([]Waiver{{ID: "version", Fields: nil, Reason: "r", OracleCommit: "pin"}},
		map[string]bool{"version": true}, "pin")
	assertWaiverValidationError(t, err)
}

func TestValidateWaivers_WildcardFieldFailsExit2(t *testing.T) {
	err := validateWaivers([]Waiver{{ID: "version", Fields: []string{"*"}, Reason: "r", OracleCommit: "pin"}},
		map[string]bool{"version": true}, "pin")
	assertWaiverValidationError(t, err)
}

func TestValidateWaivers_OracleCommitMismatchFailsExit2(t *testing.T) {
	err := validateWaivers([]Waiver{{ID: "version", Fields: []string{"stdout"}, Reason: "r", OracleCommit: "wrong-pin"}},
		map[string]bool{"version": true}, "pin")
	assertWaiverValidationError(t, err)
}

func TestValidateWaivers_ValidPasses(t *testing.T) {
	err := validateWaivers([]Waiver{{ID: "version", Fields: []string{"stdout"}, Reason: "r", OracleCommit: "pin"}},
		map[string]bool{"version": true}, "pin")
	if err != nil {
		t.Errorf("validateWaivers: %v, want nil", err)
	}
}

func assertWaiverValidationError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("validateWaivers: expected an error, got nil")
	}
	var we *waiverValidationError
	if !errors.As(err, &we) {
		t.Fatalf("error %v is not a *waiverValidationError, so main() would not exit 2", err)
	}
	if we.ExitCode() != 2 {
		t.Errorf("ExitCode() = %d, want 2", we.ExitCode())
	}
}

// TestLoadAndValidateWaivers_ReadsOracleCommitFromRunJSON proves the
// validator checks waivers.json against what THIS run actually wrote to
// run.json, not some other in-memory value the caller might have passed
// instead.
func TestLoadAndValidateWaivers_ReadsOracleCommitFromRunJSON(t *testing.T) {
	outDir := t.TempDir()
	header := runHeader{Preflight: Preflight{OracleCommit: "from-run-json"}}
	data, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "run.json"), data, 0o644); err != nil {
		t.Fatalf("writing run.json: %v", err)
	}

	casesDir := filepath.Join(t.TempDir(), "cases")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatalf("mkdir cases: %v", err)
	}
	waiversPath := filepath.Join(filepath.Dir(casesDir), "waivers.json")
	writeJSON(t, waiversPath, `[{"id":"version","fields":["stdout"],"reason":"r","oracle_commit":"from-run-json"}]`)

	waivers, err := loadAndValidateWaivers(outDir, casesDir, "", map[string]bool{"version": true})
	if err != nil {
		t.Fatalf("loadAndValidateWaivers: %v", err)
	}
	if len(waivers) != 1 {
		t.Fatalf("waivers = %v, want 1", waivers)
	}

	// A waiver pinned to a DIFFERENT commit than run.json's must fail --
	// proves the check is against run.json, not just "any commit value".
	writeJSON(t, waiversPath, `[{"id":"version","fields":["stdout"],"reason":"r","oracle_commit":"stale-commit"}]`)
	_, err = loadAndValidateWaivers(outDir, casesDir, "", map[string]bool{"version": true})
	assertWaiverValidationError(t, err)
}

func TestLoadAndValidateWaivers_ExplicitPathOverridesDefault(t *testing.T) {
	outDir := t.TempDir()
	header := runHeader{Preflight: Preflight{OracleCommit: "pin"}}
	data, _ := json.Marshal(header)
	if err := os.WriteFile(filepath.Join(outDir, "run.json"), data, 0o644); err != nil {
		t.Fatalf("writing run.json: %v", err)
	}

	explicit := filepath.Join(t.TempDir(), "custom-waivers.json")
	writeJSON(t, explicit, `[{"id":"version","fields":["stdout"],"reason":"r","oracle_commit":"pin"}]`)

	waivers, err := loadAndValidateWaivers(outDir, "/nonexistent/cases", explicit, map[string]bool{"version": true})
	if err != nil {
		t.Fatalf("loadAndValidateWaivers: %v", err)
	}
	if len(waivers) != 1 {
		t.Errorf("waivers = %v, want 1", waivers)
	}
}
