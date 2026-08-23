//go:build unix

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCases_ParsesManifestAndSortsByDirName(t *testing.T) {
	casesDir := t.TempDir()

	writeCase(t, casesDir, "b-case", `{
		"id": "b-case",
		"argv": ["search", "foo@bar"],
		"stdin": "hello",
		"env": {"FOO": "bar"},
		"rewrite_binary_name": true,
		"expected_taxonomy": ["F08"],
		"waiver": "known-diff"
	}`)
	writeCase(t, casesDir, "a-case", `{"id": "a-case", "argv": ["--version"]}`)

	// A directory without case.json must be skipped, not error.
	if err := os.MkdirAll(filepath.Join(casesDir, "not-a-case"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cases, err := LoadCases(casesDir)
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(cases))
	}

	if cases[0].ID != "a-case" || cases[1].ID != "b-case" {
		t.Fatalf("expected sorted order [a-case, b-case], got [%s, %s]", cases[0].ID, cases[1].ID)
	}

	b := cases[1]
	if len(b.Argv) != 2 || b.Argv[0] != "search" || b.Argv[1] != "foo@bar" {
		t.Errorf("argv = %v, want [search foo@bar]", b.Argv)
	}
	if b.Stdin != "hello" {
		t.Errorf("stdin = %q, want %q", b.Stdin, "hello")
	}
	if b.Env["FOO"] != "bar" {
		t.Errorf("env[FOO] = %q, want %q", b.Env["FOO"], "bar")
	}
	if !b.RewriteBinaryName {
		t.Error("rewrite_binary_name = false, want true")
	}
	if len(b.ExpectedTaxonomy) != 1 || b.ExpectedTaxonomy[0] != "F08" {
		t.Errorf("expected_taxonomy = %v, want [F08]", b.ExpectedTaxonomy)
	}
	if b.Waiver != "known-diff" {
		t.Errorf("waiver = %q, want %q", b.Waiver, "known-diff")
	}
}

func TestLoadCases_MissingIDIsError(t *testing.T) {
	casesDir := t.TempDir()
	writeCase(t, casesDir, "broken", `{"argv": ["--version"]}`)

	if _, err := LoadCases(casesDir); err == nil {
		t.Fatal("expected error for case.json missing id, got nil")
	}
}

func TestCase_FixtureDir(t *testing.T) {
	casesDir := t.TempDir()
	writeCase(t, casesDir, "with-fixture", `{"id": "with-fixture", "argv": []}`)
	fixtureDir := filepath.Join(casesDir, "with-fixture", "fixture")
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	writeCase(t, casesDir, "without-fixture", `{"id": "without-fixture", "argv": []}`)

	cases, err := LoadCases(casesDir)
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}

	byID := map[string]Case{}
	for _, c := range cases {
		byID[c.ID] = c
	}

	if got := byID["with-fixture"].FixtureDir(); got != fixtureDir {
		t.Errorf("FixtureDir() = %q, want %q", got, fixtureDir)
	}
	if got := byID["without-fixture"].FixtureDir(); got != "" {
		t.Errorf("FixtureDir() = %q, want empty", got)
	}
}

func writeCase(t *testing.T, casesDir, id, manifest string) {
	t.Helper()
	dir := filepath.Join(casesDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir case dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "case.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("writing case.json: %v", err)
	}
}
