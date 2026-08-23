//go:build unix

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWalkTree_FilesDirsAndSymlink(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "hello")
	mustWriteFile(t, filepath.Join(root, "sub", "b.txt"), "world")
	if err := os.Symlink("a.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	entries, err := walkTree(root, "cwd")
	if err != nil {
		t.Fatalf("walkTree: %v", err)
	}

	byPath := map[string]TreeEntry{}
	for _, e := range entries {
		byPath[e.Path] = e
	}

	a, ok := byPath["cwd/a.txt"]
	if !ok || a.Kind != "file" || a.SHA256 == "" || a.Size != 5 {
		t.Errorf("cwd/a.txt entry = %+v, ok=%v", a, ok)
	}
	if sub, ok := byPath["cwd/sub"]; !ok || sub.Kind != "dir" {
		t.Errorf("cwd/sub entry = %+v, ok=%v", sub, ok)
	}
	if link, ok := byPath["cwd/link.txt"]; !ok || link.Kind != "symlink" {
		t.Errorf("cwd/link.txt entry = %+v, ok=%v", link, ok)
	}

	// Sorted by path.
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Path > entries[i].Path {
			t.Errorf("entries not sorted: %s before %s", entries[i-1].Path, entries[i].Path)
		}
	}
}

func TestWalkTree_MissingRootReturnsEmpty(t *testing.T) {
	entries, err := walkTree(filepath.Join(t.TempDir(), "does-not-exist"), "config")
	if err != nil {
		t.Fatalf("walkTree: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries for missing root, got %v", entries)
	}
}

func TestDiffDeleted_MarksMissingFilesOnly(t *testing.T) {
	before := []TreeEntry{
		{Path: "cwd/kept.txt", Kind: "file"},
		{Path: "cwd/removed.txt", Kind: "file"},
		{Path: "cwd/removed-link", Kind: "symlink"},
		{Path: "cwd/removed-dir", Kind: "dir"}, // dirs are excluded from deletion reporting
	}
	after := map[string]bool{"cwd/kept.txt": true}

	deleted := diffDeleted(before, after)

	byPath := map[string]TreeEntry{}
	for _, e := range deleted {
		byPath[e.Path] = e
	}
	if len(deleted) != 2 {
		t.Fatalf("expected 2 deleted entries, got %d: %v", len(deleted), deleted)
	}
	if e, ok := byPath["cwd/removed.txt"]; !ok || e.Kind != "deleted" {
		t.Errorf("cwd/removed.txt = %+v, ok=%v", e, ok)
	}
	if e, ok := byPath["cwd/removed-link"]; !ok || e.Kind != "deleted" {
		t.Errorf("cwd/removed-link = %+v, ok=%v", e, ok)
	}
	if _, ok := byPath["cwd/removed-dir"]; ok {
		t.Error("cwd/removed-dir should not be reported as deleted (dirs excluded)")
	}
}

// TestRunCaseSide_TreeMarksFixtureFileDeletedByStub is the integration proof
// the ticket asks for: a fixture file the stub binary deletes must show up
// in the case's evidence tree with kind "deleted", not just vanish silently.
func TestRunCaseSide_TreeMarksFixtureFileDeletedByStub(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `
rm -f "$(pwd)/gone.txt"
echo "kept" > "$(pwd)/new.txt"
exit 0
`)

	casesDir := t.TempDir()
	writeCase(t, casesDir, "delete-case", `{"id": "delete-case", "argv": []}`)
	fixtureDir := filepath.Join(casesDir, "delete-case", "fixture")
	mustWriteFile(t, filepath.Join(fixtureDir, "gone.txt"), "will be deleted")
	mustWriteFile(t, filepath.Join(fixtureDir, "stays.txt"), "will survive")

	cases, err := LoadCases(casesDir)
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}

	outDir := t.TempDir()
	rec, err := runCaseSide([]string{stub}, cases[0], outDir, "target", defaultTimeout)
	if err != nil {
		t.Fatalf("runCaseSide: %v", err)
	}
	if rec.ExitCode != 0 {
		t.Fatalf("stub exited %d, stderr=%q", rec.ExitCode, rec.Stderr)
	}

	byPath := map[string]TreeEntry{}
	for _, e := range rec.Tree {
		byPath[e.Path] = e
	}

	if e, ok := byPath["cwd/gone.txt"]; !ok || e.Kind != "deleted" {
		t.Errorf("cwd/gone.txt = %+v, ok=%v, want kind=deleted", e, ok)
	}
	if e, ok := byPath["cwd/stays.txt"]; !ok || e.Kind != "file" {
		t.Errorf("cwd/stays.txt = %+v, ok=%v, want kind=file", e, ok)
	}
	if e, ok := byPath["cwd/new.txt"]; !ok || e.Kind != "file" {
		t.Errorf("cwd/new.txt = %+v, ok=%v, want kind=file", e, ok)
	}
}
