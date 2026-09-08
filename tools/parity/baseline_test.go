//go:build unix

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBaseline_MissingFileReturnsEmptyNotError(t *testing.T) {
	entries, err := loadBaseline(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("loadBaseline: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %v, want none", entries)
	}
}

func TestLoadBaseline_ParsesEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")
	writeJSON(t, path, `[{"path":"home/.apm/config.json","reason":"r","oracle_ref":"config.py:15"}]`)

	entries, err := loadBaseline(path)
	if err != nil {
		t.Fatalf("loadBaseline: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %v, want 1 entry", entries)
	}
	e := entries[0]
	if e.Path != "home/.apm/config.json" || e.Reason != "r" || e.OracleRef != "config.py:15" {
		t.Errorf("entry = %+v", e)
	}
}

func assertBaselineValidationError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("validateBaseline: expected an error, got nil")
	}
	var be *baselineValidationError
	if !errors.As(err, &be) {
		t.Fatalf("error %v is not a *baselineValidationError, so main() would not exit 2", err)
	}
	if be.ExitCode() != 2 {
		t.Errorf("ExitCode() = %d, want 2", be.ExitCode())
	}
}

func TestValidateBaseline_EmptyPathFailsExit2(t *testing.T) {
	err := validateBaseline([]BaselineEntry{{Path: "", Reason: "r"}})
	assertBaselineValidationError(t, err)
}

func TestValidateBaseline_GlobPathFailsExit2(t *testing.T) {
	err := validateBaseline([]BaselineEntry{{Path: "home/*", Reason: "r"}})
	assertBaselineValidationError(t, err)
}

func TestValidateBaseline_EmptyReasonFailsExit2(t *testing.T) {
	err := validateBaseline([]BaselineEntry{{Path: "home/.apm/config.json", Reason: ""}})
	assertBaselineValidationError(t, err)
}

func TestValidateBaseline_DuplicatePathFailsExit2(t *testing.T) {
	err := validateBaseline([]BaselineEntry{
		{Path: "home/.apm/config.json", Reason: "r1"},
		{Path: "home/.apm/config.json", Reason: "r2"},
	})
	assertBaselineValidationError(t, err)
}

func TestValidateBaseline_ValidPasses(t *testing.T) {
	err := validateBaseline([]BaselineEntry{{Path: "home/.apm/config.json", Reason: "r", OracleRef: "config.py:15"}})
	if err != nil {
		t.Errorf("validateBaseline: %v, want nil", err)
	}
}

func TestLoadAndValidateBaseline_ExplicitPathOverridesDefault(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "custom-baseline.json")
	writeJSON(t, explicit, `[{"path":"home/.apm/config.json","reason":"r","oracle_ref":"config.py:15"}]`)

	entries, err := loadAndValidateBaseline("/nonexistent/cases", explicit)
	if err != nil {
		t.Fatalf("loadAndValidateBaseline: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("entries = %v, want 1", entries)
	}
}

func TestLoadAndValidateBaseline_DefaultSiblingOfCasesDir(t *testing.T) {
	casesDir := filepath.Join(t.TempDir(), "cases")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatalf("mkdir cases: %v", err)
	}
	baselinePath := filepath.Join(filepath.Dir(casesDir), "baseline.json")
	writeJSON(t, baselinePath, `[{"path":"home/.apm/config.json","reason":"r","oracle_ref":"config.py:15"}]`)

	entries, err := loadAndValidateBaseline(casesDir, "")
	if err != nil {
		t.Fatalf("loadAndValidateBaseline: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("entries = %v, want 1", entries)
	}
}

func TestLoadAndValidateBaseline_InvalidEntryFailsExit2(t *testing.T) {
	casesDir := filepath.Join(t.TempDir(), "cases")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatalf("mkdir cases: %v", err)
	}
	baselinePath := filepath.Join(filepath.Dir(casesDir), "baseline.json")
	writeJSON(t, baselinePath, `[{"path":"home/*","reason":"r"}]`)

	_, err := loadAndValidateBaseline(casesDir, "")
	assertBaselineValidationError(t, err)
}

func TestBaselinePathSet_IndexesByPath(t *testing.T) {
	set := baselinePathSet([]BaselineEntry{
		{Path: "home/.apm/config.json", Reason: "r"},
		{Path: "home/.cache/apm/last_version_check", Reason: "r"},
	})
	if !set["home/.apm/config.json"] || !set["home/.cache/apm/last_version_check"] {
		t.Errorf("set = %v, missing expected paths", set)
	}
	if set["home/.apm/marketplaces.json"] {
		t.Errorf("set unexpectedly contains an unrelated path")
	}
}
