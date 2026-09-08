package pluginjson

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScaffold_MatchesUpstreamGolden pins R3.3.d against the real upstream
// production artifact (internal/pack/bundle/testdata/upstream-plugin-init.golden.json,
// captured 2026-07-31 -- see spec/conformance/agent-schema.md's
// "plugin.json" section): same field order, same 2-space indent, same
// trailing newline.
func TestScaffold_MatchesUpstreamGolden(t *testing.T) {
	dir := t.TempDir()

	if err := Scaffold(dir, "design-taste", "1.0.0", "APM project for design-taste", "Madao"); err != nil {
		t.Fatalf("Scaffold() err = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}

	want, err := os.ReadFile(filepath.Join("..", "pack", "bundle", "testdata", "upstream-plugin-init.golden.json"))
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}

	if string(got) != string(want) {
		t.Errorf("Scaffold() output does not match upstream golden:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestScaffold_EmptyFieldsStillWriteRequiredKeys covers the boundary the
// golden fixture doesn't: version/description/author empty. Unlike
// bundle.PluginManifest.ToJSONValue (which omits empty optional fields),
// plugin init's template always writes every key -- upstream's _helpers.py
// defaults version/description/author to "" rather than omitting them
// (research/upstream-marketplace-plugin.md:100-106).
func TestScaffold_EmptyFieldsStillWriteRequiredKeys(t *testing.T) {
	dir := t.TempDir()

	if err := Scaffold(dir, "bare-plugin", "", "", ""); err != nil {
		t.Fatalf("Scaffold() err = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}

	want := "{\n" +
		"  \"name\": \"bare-plugin\",\n" +
		"  \"version\": \"\",\n" +
		"  \"description\": \"\",\n" +
		"  \"author\": {\n" +
		"    \"name\": \"\"\n" +
		"  },\n" +
		"  \"license\": \"MIT\"\n" +
		"}\n"
	if string(got) != want {
		t.Errorf("Scaffold() = %q, want %q", got, want)
	}
}

// TestScaffold_WriteFileError covers the os.WriteFile failure path (a
// nonexistent parent directory), matching the "write plugin.json: %w"
// wrapped-error contract.
func TestScaffold_WriteFileError(t *testing.T) {
	err := Scaffold(filepath.Join(t.TempDir(), "does-not-exist"), "x", "1.0.0", "d", "a")
	if err == nil {
		t.Fatal("Scaffold() into a nonexistent directory: err = nil, want an error")
	}
}

// A failure on the SECOND staged file must roll the first one back out of
// the project root; the cmd-level test fails on the first file, so the
// committed-list rollback branch had no test until tools/gate.sh's mutant
// stage-rollback-skipped survived.
func TestStagedScaffold_RollsBackAlreadyCommittedFiles(t *testing.T) {
	root := t.TempDir()
	st, err := NewStagedScaffold(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Cleanup()
	for _, name := range []string{"first.json", "second.json"} {
		if err := os.WriteFile(st.Add(name), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	restore := SetCommitHookForTest(func(name string) error {
		if name == "second.json" {
			return os.ErrPermission
		}
		return nil
	})
	defer restore()
	if err := st.Commit(); err == nil {
		t.Fatal("Commit succeeded, want the injected failure on second.json")
	}
	for _, name := range []string{"first.json", "second.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("%s left in the project root after a failed commit (err=%v)", name, err)
		}
	}
}
