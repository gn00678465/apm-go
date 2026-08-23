//go:build unix

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeBinFiles writes <outDir>/<side>/<id>/stdout.bin and stderr.bin --
// diffCase always reads these from disk (record.go's writeRawBodies writes
// them unconditionally in production, regardless of size or UTF-8
// validity), so tests must provide them too.
func writeBinFiles(t *testing.T, outDir, side, id, stdout, stderr string) {
	t.Helper()
	dir := filepath.Join(outDir, side, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stdout.bin"), []byte(stdout), 0o644); err != nil {
		t.Fatalf("write stdout.bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stderr.bin"), []byte(stderr), 0o644); err != nil {
		t.Fatalf("write stderr.bin: %v", err)
	}
}

func mustDiffCase(t *testing.T, outDir string, c Case, oracleRec, targetRec Record) (CaseDiff, diffDetail) {
	t.Helper()
	cd, detail, err := diffCase(outDir, c, oracleRec, targetRec)
	if err != nil {
		t.Fatalf("diffCase: %v", err)
	}
	return cd, detail
}

func TestDiffCase_NoDifference(t *testing.T) {
	outDir := t.TempDir()
	c := Case{ID: "c1", ExpectedTaxonomy: []string{"F00"}}
	writeBinFiles(t, outDir, "oracle", "c1", "hi", "")
	writeBinFiles(t, outDir, "target", "c1", "hi", "")

	cd, _ := mustDiffCase(t, outDir, c, Record{ExitCode: 0}, Record{ExitCode: 0})
	if len(cd.Fields) != 0 {
		t.Errorf("Fields = %v, want none", cd.Fields)
	}
	if cd.ID != "c1" {
		t.Errorf("ID = %q, want c1", cd.ID)
	}
	if len(cd.Taxonomy.Expected) != 1 || cd.Taxonomy.Expected[0] != "F00" {
		t.Errorf("Taxonomy.Expected = %v, want [F00] (pass-through from case)", cd.Taxonomy.Expected)
	}
}

func TestDiffCase_ExitCodeDiffers(t *testing.T) {
	outDir := t.TempDir()
	c := Case{ID: "c1"}
	writeBinFiles(t, outDir, "oracle", "c1", "same", "")
	writeBinFiles(t, outDir, "target", "c1", "same", "")

	cd, detail := mustDiffCase(t, outDir, c, Record{ExitCode: 0}, Record{ExitCode: 1})
	if !fieldsEqual(cd.Fields, []string{"exit_code"}) {
		t.Fatalf("Fields = %v, want [exit_code]", cd.Fields)
	}
	if detail.ExitCode == nil || detail.ExitCode.Old != 0 || detail.ExitCode.New != 1 {
		t.Errorf("detail.ExitCode = %+v, want {Old:0 New:1}", detail.ExitCode)
	}
	if !containsStr(cd.Taxonomy.Heuristic, "F08") {
		t.Errorf("Heuristic = %v, want to contain F08", cd.Taxonomy.Heuristic)
	}
}

func TestDiffCase_StdoutDiffersUsesNormalizedValue(t *testing.T) {
	outDir := t.TempDir()
	c := Case{ID: "c1"}
	oracleHome := "/tmp/apm-parity-oracle/home"
	targetHome := "/tmp/apm-parity-target/home"
	writeBinFiles(t, outDir, "oracle", "c1", "path: /tmp/apm-parity-oracle/cwd/f", "")
	writeBinFiles(t, outDir, "target", "c1", "path: /tmp/apm-parity-target/cwd/f", "")

	oracle := Record{ExitCode: 0, EnvDelta: map[string]string{"HOME": oracleHome}}
	target := Record{ExitCode: 0, EnvDelta: map[string]string{"HOME": targetHome}}

	cd, detail := mustDiffCase(t, outDir, c, oracle, target)
	// Both sides' own sandbox cwd normalizes to <TMP>, so after
	// normalization the strings are identical -- no diff.
	if len(cd.Fields) != 0 {
		t.Errorf("Fields = %v, want none (both sides normalize to the same <TMP> path)", cd.Fields)
	}
	if detail.Stdout != nil {
		t.Errorf("detail.Stdout = %+v, want nil", detail.Stdout)
	}
}

func TestDiffCase_StdoutGenuinelyDiffers(t *testing.T) {
	outDir := t.TempDir()
	c := Case{ID: "c1", Argv: []string{"--help"}}
	writeBinFiles(t, outDir, "oracle", "c1", "usage: apm [OPTIONS]", "")
	writeBinFiles(t, outDir, "target", "c1", "usage: apm-go [OPTIONS]", "")

	cd, detail := mustDiffCase(t, outDir, c, Record{}, Record{})
	if !fieldsEqual(cd.Fields, []string{"stdout"}) {
		t.Fatalf("Fields = %v, want [stdout]", cd.Fields)
	}
	if detail.Stdout == nil || detail.Stdout.Raw.Old != "usage: apm [OPTIONS]" || detail.Stdout.Raw.New != "usage: apm-go [OPTIONS]" {
		t.Errorf("detail.Stdout.Raw = %+v", detail.Stdout)
	}
	if detail.Stdout == nil || detail.Stdout.Normalized.Old != "usage: apm [OPTIONS]" || detail.Stdout.Normalized.New != "usage: apm-go [OPTIONS]" {
		t.Errorf("detail.Stdout.Normalized = %+v, want unchanged (no sandbox paths present)", detail.Stdout)
	}
	if !containsStr(cd.Taxonomy.Heuristic, "F01") {
		t.Errorf("Heuristic = %v, want to contain F01 for a --help case", cd.Taxonomy.Heuristic)
	}
}

// TestDiffCase_DiffDetailKeepsRawAndNormalizedOldNew proves ticket 02
// attempt 2's D2 fix: diff/<id>.json's stdout detail keeps the untouched
// raw old/new (as actually printed, sandbox paths and all) ALONGSIDE the
// normalized old/new used to decide the field differs -- not the
// normalized value in place of the raw one (eval-ticket-02.md's D2
// finding: the previous attempt stored only the normalized value, so a raw
// hex commit like "c8d6cdec" had already been rewritten to "<SHA>" in the
// evidence a reviewer would read).
func TestDiffCase_DiffDetailKeepsRawAndNormalizedOldNew(t *testing.T) {
	outDir := t.TempDir()
	c := Case{ID: "c1"}
	oracleHome := "/tmp/apm-parity-oracle/home"
	targetHome := "/tmp/apm-parity-target/home"
	writeBinFiles(t, outDir, "oracle", "c1", "path: /tmp/apm-parity-oracle/cwd/f exit 0", "")
	writeBinFiles(t, outDir, "target", "c1", "path: /tmp/apm-parity-target/cwd/f exit 1", "")

	oracle := Record{EnvDelta: map[string]string{"HOME": oracleHome}}
	target := Record{EnvDelta: map[string]string{"HOME": targetHome}}

	cd, detail := mustDiffCase(t, outDir, c, oracle, target)
	if !fieldsEqual(cd.Fields, []string{"stdout"}) {
		t.Fatalf("Fields = %v, want [stdout]", cd.Fields)
	}
	if detail.Stdout == nil {
		t.Fatal("detail.Stdout = nil")
	}
	if detail.Stdout.Raw.Old != "path: /tmp/apm-parity-oracle/cwd/f exit 0" {
		t.Errorf("Raw.Old = %q, want the untouched raw bytes", detail.Stdout.Raw.Old)
	}
	if detail.Stdout.Raw.New != "path: /tmp/apm-parity-target/cwd/f exit 1" {
		t.Errorf("Raw.New = %q, want the untouched raw bytes", detail.Stdout.Raw.New)
	}
	if detail.Stdout.Normalized.Old != "path: <TMP>/f exit 0" {
		t.Errorf("Normalized.Old = %q, want the sandbox path normalized away", detail.Stdout.Normalized.Old)
	}
	if detail.Stdout.Normalized.New != "path: <TMP>/f exit 1" {
		t.Errorf("Normalized.New = %q, want the sandbox path normalized away", detail.Stdout.Normalized.New)
	}
}

func TestDiffCase_RewriteBinaryNameMakesBinaryOnlyStdoutMatch(t *testing.T) {
	outDir := t.TempDir()
	c := Case{ID: "c1", RewriteBinaryName: true}
	writeBinFiles(t, outDir, "oracle", "c1", "usage: apm doctor", "")
	writeBinFiles(t, outDir, "target", "c1", "usage: apm-go doctor", "")

	cd, _ := mustDiffCase(t, outDir, c, Record{}, Record{})
	if len(cd.Fields) != 0 {
		t.Errorf("Fields = %v, want none once apm-go<->apm is normalized away", cd.Fields)
	}
}

// TestDiffCase_NonUTF8StdoutIsStillNormalized proves the fix for reading
// straight from stdout.bin: a body that record.go's inlineBody would have
// omitted (non-UTF-8) still gets sandbox-path normalization, rather than
// falling back to comparing un-normalized raw bytes that always differ
// purely because each side's sandbox got its own unique temp path.
func TestDiffCase_NonUTF8StdoutIsStillNormalized(t *testing.T) {
	outDir := t.TempDir()
	c := Case{ID: "c1"}
	oracleHome := "/tmp/apm-parity-oracle/home"
	targetHome := "/tmp/apm-parity-target/home"
	writeBinFiles(t, outDir, "oracle", "c1", "\xffpath: /tmp/apm-parity-oracle/cwd/f", "")
	writeBinFiles(t, outDir, "target", "c1", "\xffpath: /tmp/apm-parity-target/cwd/f", "")

	oracle := Record{EnvDelta: map[string]string{"HOME": oracleHome}, StdoutSHA256: "would-differ-if-compared-raw-1"}
	target := Record{EnvDelta: map[string]string{"HOME": targetHome}, StdoutSHA256: "would-differ-if-compared-raw-2"}

	cd, _ := mustDiffCase(t, outDir, c, oracle, target)
	if len(cd.Fields) != 0 {
		t.Errorf("Fields = %v, want none: non-UTF-8 bodies must still normalize by sandbox path, not fall back to raw sha256", cd.Fields)
	}
}

func TestDiffCase_TreeAddedFile(t *testing.T) {
	outDir := t.TempDir()
	c := Case{ID: "c1"}
	writeBinFiles(t, outDir, "oracle", "c1", "", "")
	writeBinFiles(t, outDir, "target", "c1", "", "")
	oracle := Record{Tree: nil}
	target := Record{Tree: []TreeEntry{{Path: "cwd/extra.txt", Kind: "file", Size: 3, SHA256: "abc"}}}

	cd, detail := mustDiffCase(t, outDir, c, oracle, target)
	if !fieldsEqual(cd.Fields, []string{"tree"}) {
		t.Fatalf("Fields = %v, want [tree]", cd.Fields)
	}
	if detail.Tree == nil || len(detail.Tree.Added) != 1 || detail.Tree.Added[0].Path != "cwd/extra.txt" {
		t.Errorf("detail.Tree = %+v", detail.Tree)
	}
	for _, f := range []string{"F09", "F03"} {
		if !containsStr(cd.Taxonomy.Heuristic, f) {
			t.Errorf("Heuristic = %v, want to contain %q", cd.Taxonomy.Heuristic, f)
		}
	}
}

func TestDiffCase_TreeRemovedFile(t *testing.T) {
	outDir := t.TempDir()
	c := Case{ID: "c1"}
	writeBinFiles(t, outDir, "oracle", "c1", "", "")
	writeBinFiles(t, outDir, "target", "c1", "", "")
	oracle := Record{Tree: []TreeEntry{{Path: "cwd/gone.txt", Kind: "file", Size: 1, SHA256: "x"}}}
	target := Record{Tree: nil}

	cd, detail := mustDiffCase(t, outDir, c, oracle, target)
	if !fieldsEqual(cd.Fields, []string{"tree"}) {
		t.Fatalf("Fields = %v, want [tree]", cd.Fields)
	}
	if detail.Tree == nil || len(detail.Tree.Removed) != 1 || detail.Tree.Removed[0].Path != "cwd/gone.txt" {
		t.Errorf("detail.Tree = %+v", detail.Tree)
	}
}

// TestDiffCase_SameShaButDifferentRawBytesIsATreeDiff proves the tree
// comparison doesn't just trust TreeEntry.SHA256 equality (acceptance:
// "raw bytes of every file present on both sides") -- it reads the actual
// copied fs/ evidence back and compares bytes directly.
func TestDiffCase_SameShaButDifferentRawBytesIsATreeDiff(t *testing.T) {
	outDir := t.TempDir()
	c := Case{ID: "c1"}
	writeBinFiles(t, outDir, "oracle", "c1", "", "")
	writeBinFiles(t, outDir, "target", "c1", "", "")
	entry := TreeEntry{Path: "cwd/f.txt", Kind: "file", Size: 5, SHA256: "same-sha-recorded-by-both-sides"}
	oracle := Record{Tree: []TreeEntry{entry}}
	target := Record{Tree: []TreeEntry{entry}}

	writeFSFile(t, outDir, "oracle", "c1", "cwd/f.txt", "aaaaa")
	writeFSFile(t, outDir, "target", "c1", "cwd/f.txt", "bbbbb")

	cd, detail := mustDiffCase(t, outDir, c, oracle, target)
	if !fieldsEqual(cd.Fields, []string{"tree"}) {
		t.Fatalf("Fields = %v, want [tree] even though recorded sha256 matched on both sides", cd.Fields)
	}
	if detail.Tree == nil || len(detail.Tree.Changed) != 1 {
		t.Errorf("detail.Tree = %+v, want one changed entry", detail.Tree)
	}
}

func TestDiffCase_SameShaAndSameRawBytesIsNotATreeDiff(t *testing.T) {
	outDir := t.TempDir()
	c := Case{ID: "c1"}
	writeBinFiles(t, outDir, "oracle", "c1", "", "")
	writeBinFiles(t, outDir, "target", "c1", "", "")
	entry := TreeEntry{Path: "cwd/f.txt", Kind: "file", Size: 5, SHA256: "sha"}
	oracle := Record{Tree: []TreeEntry{entry}}
	target := Record{Tree: []TreeEntry{entry}}

	writeFSFile(t, outDir, "oracle", "c1", "cwd/f.txt", "aaaaa")
	writeFSFile(t, outDir, "target", "c1", "cwd/f.txt", "aaaaa")

	cd, _ := mustDiffCase(t, outDir, c, oracle, target)
	if len(cd.Fields) != 0 {
		t.Errorf("Fields = %v, want none", cd.Fields)
	}
}

func TestDiffCase_MissingStdoutBinIsAnError(t *testing.T) {
	outDir := t.TempDir()
	c := Case{ID: "c1"}
	// Neither side's stdout.bin/stderr.bin was written.

	_, _, err := diffCase(outDir, c, Record{}, Record{})
	if err == nil {
		t.Fatal("diffCase: expected an error when evidence files are missing, got nil")
	}
}

func writeFSFile(t *testing.T, outDir, side, id, relPath, content string) {
	t.Helper()
	writeFSFileBytes(t, outDir, side, id, relPath, []byte(content))
}

func writeFSFileBytes(t *testing.T, outDir, side, id, relPath string, content []byte) {
	t.Helper()
	full := filepath.Join(outDir, side, id, "fs", filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestDiffCase_TreeFileNormalizesSandboxPathBeforeByteCompare proves ticket
// 02 attempt 2's bytes-normalisation fix: a text file that merely stores
// its own sandbox's absolute cwd (e.g. a registry manifest recording the
// fixture dir it was seeded from) must NOT show as a tree diff purely
// because each side's sandbox got a different-length temp path -- even
// though that gives the two sides genuinely different raw bytes, size, and
// sha256 at capture time.
func TestDiffCase_TreeFileNormalizesSandboxPathBeforeByteCompare(t *testing.T) {
	outDir := t.TempDir()
	c := Case{ID: "c1"}
	writeBinFiles(t, outDir, "oracle", "c1", "", "")
	writeBinFiles(t, outDir, "target", "c1", "", "")

	oracleHome := "/tmp/apm-parity-oracle/home"
	targetHome := "/tmp/apm-parity-target/home"
	oracle := Record{
		Tree:     []TreeEntry{{Path: "config/registry.json", Kind: "file", Size: 55, SHA256: "oracle-sha-differs-because-cwd-length-differs"}},
		EnvDelta: map[string]string{"HOME": oracleHome},
	}
	target := Record{
		Tree:     []TreeEntry{{Path: "config/registry.json", Kind: "file", Size: 55, SHA256: "target-sha-differs-because-cwd-length-differs"}},
		EnvDelta: map[string]string{"HOME": targetHome},
	}

	writeFSFile(t, outDir, "oracle", "c1", "config/registry.json", `{"fixture":"/tmp/apm-parity-oracle/cwd/fixture"}`)
	writeFSFile(t, outDir, "target", "c1", "config/registry.json", `{"fixture":"/tmp/apm-parity-target/cwd/fixture"}`)

	cd, _ := mustDiffCase(t, outDir, c, oracle, target)
	if len(cd.Fields) != 0 {
		t.Errorf("Fields = %v, want none: registry file differs only by each side's own sandbox cwd", cd.Fields)
	}
}

// TestDiffCase_TreeFileGenuineTextDriftStillDetected proves the
// normalisation fix doesn't paper over a real difference: two text files
// whose content differs for a reason OTHER than the sandbox path must still
// surface as a tree diff.
func TestDiffCase_TreeFileGenuineTextDriftStillDetected(t *testing.T) {
	outDir := t.TempDir()
	c := Case{ID: "c1"}
	writeBinFiles(t, outDir, "oracle", "c1", "", "")
	writeBinFiles(t, outDir, "target", "c1", "", "")
	entry := TreeEntry{Path: "config/registry.json", Kind: "file", Size: 10, SHA256: "sha"}
	oracle := Record{Tree: []TreeEntry{entry}}
	target := Record{Tree: []TreeEntry{entry}}

	writeFSFile(t, outDir, "oracle", "c1", "config/registry.json", `{"n":1}`)
	writeFSFile(t, outDir, "target", "c1", "config/registry.json", `{"n":2}`)

	cd, detail := mustDiffCase(t, outDir, c, oracle, target)
	if !fieldsEqual(cd.Fields, []string{"tree"}) {
		t.Fatalf("Fields = %v, want [tree]: genuine content drift, not sandbox-path noise", cd.Fields)
	}
	if detail.Tree == nil || len(detail.Tree.Changed) != 1 {
		t.Errorf("detail.Tree = %+v, want one changed entry", detail.Tree)
	}
}

// TestDiffCase_TreeFileNonUTF8ComparedRaw proves binary tree files are
// compared as raw bytes, never run through normalizeString/UTF-8 decoding
// (acceptance: "binary files compare raw").
func TestDiffCase_TreeFileNonUTF8ComparedRaw(t *testing.T) {
	outDir := t.TempDir()
	c := Case{ID: "c1"}
	writeBinFiles(t, outDir, "oracle", "c1", "", "")
	writeBinFiles(t, outDir, "target", "c1", "", "")
	entry := TreeEntry{Path: "cwd/f.bin", Kind: "file", Size: 2, SHA256: "sha"}
	oracle := Record{Tree: []TreeEntry{entry}}
	target := Record{Tree: []TreeEntry{entry}}

	writeFSFileBytes(t, outDir, "oracle", "c1", "cwd/f.bin", []byte{0xff, 0x00})
	writeFSFileBytes(t, outDir, "target", "c1", "cwd/f.bin", []byte{0xff, 0x01})

	cd, _ := mustDiffCase(t, outDir, c, oracle, target)
	if !fieldsEqual(cd.Fields, []string{"tree"}) {
		t.Errorf("Fields = %v, want [tree]: genuinely different non-UTF-8 bytes", cd.Fields)
	}
}

func TestApplyWaiver_FullCoverageWaives(t *testing.T) {
	cd := CaseDiff{ID: "c1", Fields: []string{"stdout", "exit_code"}}
	waivers := []Waiver{{ID: "c1", Fields: []string{"stdout", "exit_code"}, Reason: "known gap"}}

	got := applyWaiver(cd, nil, waivers)
	if !got.Waived {
		t.Error("Waived = false, want true (waiver covers every differing field)")
	}
	if got.WaiverReason != "known gap" {
		t.Errorf("WaiverReason = %q, want %q", got.WaiverReason, "known gap")
	}
}

func TestApplyWaiver_PartialCoverageDoesNotWaive(t *testing.T) {
	cd := CaseDiff{ID: "c1", Fields: []string{"stdout", "exit_code"}}
	waivers := []Waiver{{ID: "c1", Fields: []string{"stdout"}, Reason: "known gap"}}

	got := applyWaiver(cd, nil, waivers)
	if got.Waived {
		t.Error("Waived = true, want false: waiver only lists stdout, diff also has exit_code")
	}
}

func TestApplyWaiver_WrongIDDoesNotMatch(t *testing.T) {
	cd := CaseDiff{ID: "c1", Fields: []string{"stdout"}}
	waivers := []Waiver{{ID: "other", Fields: []string{"stdout"}, Reason: "x"}}

	got := applyWaiver(cd, nil, waivers)
	if got.Waived {
		t.Error("Waived = true, want false: no waiver for this id")
	}
}

func TestApplyWaiver_EmptyDiffNeverWaived(t *testing.T) {
	cd := CaseDiff{ID: "c1"}
	got := applyWaiver(cd, nil, []Waiver{{ID: "c1", Fields: []string{"stdout"}, Reason: "x"}})
	if got.Waived {
		t.Error("Waived = true, want false: nothing differed, nothing to waive")
	}
}

func TestCountUnwaived(t *testing.T) {
	diffs := []CaseDiff{
		{ID: "a"}, // empty diff
		{ID: "b", Fields: []string{"stdout"}, Waived: true},
		{ID: "c", Fields: []string{"stdout"}, Waived: false},
	}
	if got := countUnwaived(diffs); got != 1 {
		t.Errorf("countUnwaived = %d, want 1", got)
	}
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Ticket 02 attempt 5 (eval-ticket-02-r3.md): a `tree` waiver must name the
// exact paths it covers. A tree diff is waived only when EVERY differing
// path is listed in the waiver's tree_paths; a waiver whose scope lives only
// in its reason text waives nothing.
func TestApplyWaiver_TreeRequiresPathPreciseCoverage(t *testing.T) {
	cd := CaseDiff{ID: "c1", Fields: []string{"tree"}}
	paths := []string{"home/.apm/config.json"}

	// Field-only waiver, no tree_paths -> NOT waived.
	got := applyWaiver(cd, paths, []Waiver{{ID: "c1", Fields: []string{"tree"}, Reason: "x"}})
	if got.Waived {
		t.Fatal("tree waiver without tree_paths must not waive")
	}

	// Exact path listed -> waived.
	got = applyWaiver(cd, paths, []Waiver{{ID: "c1", Fields: []string{"tree"}, TreePaths: []string{"home/.apm/config.json"}, Reason: "x"}})
	if !got.Waived {
		t.Fatal("tree waiver naming the exact path must waive")
	}

	// A second, unlisted path appears -> NOT waived (this is the
	// last_version_check case the evaluator caught).
	got = applyWaiver(cd, []string{"home/.apm/config.json", "home/.cache/apm/last_version_check"},
		[]Waiver{{ID: "c1", Fields: []string{"tree"}, TreePaths: []string{"home/.apm/config.json"}, Reason: "x"}})
	if got.Waived {
		t.Fatal("an unlisted tree path must break the waiver")
	}

	// stdout-only diff with a waiver that has tree_paths but no stdout coverage -> NOT waived.
	got = applyWaiver(CaseDiff{ID: "c1", Fields: []string{"stdout"}}, nil,
		[]Waiver{{ID: "c1", Fields: []string{"tree"}, TreePaths: []string{"a"}, Reason: "x"}})
	if got.Waived {
		t.Fatal("tree_paths must not widen field coverage")
	}
}
