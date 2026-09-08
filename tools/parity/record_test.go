//go:build unix

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestToJSONLRecord_InlinesSmallBodies(t *testing.T) {
	r := NewRecord("small", nil, nil, 0, false, []byte("hello"), []byte("world"), nil)
	out := toJSONLRecord(r, "target/small")

	if out.Stdout == nil || *out.Stdout != "hello" {
		t.Errorf("Stdout = %v, want inlined %q", out.Stdout, "hello")
	}
	if out.StdoutPath != nil {
		t.Errorf("StdoutPath = %v, want nil for a small body", out.StdoutPath)
	}
	if out.Stderr == nil || *out.Stderr != "world" {
		t.Errorf("Stderr = %v, want inlined %q", out.Stderr, "world")
	}
}

func TestToJSONLRecord_ReferencesLargeBodiesByBinPath(t *testing.T) {
	big := bytes.Repeat([]byte("x"), inlineThreshold+1)
	r := NewRecord("big", nil, nil, 0, false, big, []byte("small"), nil)
	out := toJSONLRecord(r, "oracle/big")

	if out.Stdout != nil {
		t.Errorf("Stdout = %v, want nil (referenced instead)", out.Stdout)
	}
	if out.StdoutPath == nil || *out.StdoutPath != "oracle/big/stdout.bin" {
		t.Errorf("StdoutPath = %v, want %q", out.StdoutPath, "oracle/big/stdout.bin")
	}
	if out.StdoutBytes != len(big) {
		t.Errorf("StdoutBytes = %d, want %d", out.StdoutBytes, len(big))
	}
	// Stderr stays inlined: the threshold applies per-field, not per-record.
	if out.Stderr == nil || *out.Stderr != "small" {
		t.Errorf("Stderr = %v, want inlined %q", out.Stderr, "small")
	}
	if out.StderrPath != nil {
		t.Errorf("StderrPath = %v, want nil", out.StderrPath)
	}
}

func TestToJSONLRecord_ReferencesNonUTF8BodyByBinPath(t *testing.T) {
	raw := []byte{0xff, '\n'}
	r := NewRecord("bin", nil, nil, 0, false, raw, nil, nil)
	out := toJSONLRecord(r, "oracle/bin")

	if out.Stdout != nil {
		t.Errorf("Stdout = %v, want nil for non-UTF-8 bytes", *out.Stdout)
	}
	if out.StdoutPath == nil || *out.StdoutPath != "oracle/bin/stdout.bin" {
		t.Errorf("StdoutPath = %v, want %q", out.StdoutPath, "oracle/bin/stdout.bin")
	}
}

func TestAppendJSONLRecord_OneLinePerCase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.jsonl")

	r1 := NewRecord("c1", nil, nil, 0, false, []byte("a"), nil, nil)
	r2 := NewRecord("c2", nil, nil, 0, false, []byte("b"), nil, nil)
	if err := appendJSONLRecord(path, r1, "target/c1"); err != nil {
		t.Fatalf("appendJSONLRecord: %v", err)
	}
	if err := appendJSONLRecord(path, r2, "target/c2"); err != nil {
		t.Fatalf("appendJSONLRecord: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening jsonl: %v", err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	var first jsonlRecord
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("unmarshalling first line: %v", err)
	}
	if first.ID != "c1" {
		t.Errorf("first.ID = %q, want c1", first.ID)
	}
}

func TestNewRecord_NonUTF8BytesOmitInlineButKeepShaAndBytes(t *testing.T) {
	raw := []byte{0xff, '\n'}
	r := NewRecord("bin", nil, nil, 0, false, raw, nil, nil)

	if r.Stdout != nil {
		t.Errorf("Stdout = %q, want nil for non-UTF-8 bytes", *r.Stdout)
	}
	if r.StdoutBytes != len(raw) {
		t.Errorf("StdoutBytes = %d, want %d", r.StdoutBytes, len(raw))
	}
	if want := sha256Hex(raw); r.StdoutSHA256 != want {
		t.Errorf("StdoutSHA256 = %q, want %q", r.StdoutSHA256, want)
	}
}

func TestWriteRecordJSON_OmitsInlineOverThresholdButKeepsShaAndBytes(t *testing.T) {
	dir := t.TempDir()
	big := bytes.Repeat([]byte("y"), inlineThreshold+100)
	r := NewRecord("full", nil, nil, 0, false, big, nil, nil)

	if err := writeRecordJSON(dir, r); err != nil {
		t.Fatalf("writeRecordJSON: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "record.json"))
	if err != nil {
		t.Fatalf("reading record.json: %v", err)
	}
	var got Record
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshalling record.json: %v", err)
	}
	if got.Stdout != nil {
		t.Errorf("record.json inlined stdout despite exceeding the threshold: %d bytes", len(*got.Stdout))
	}
	if got.StdoutBytes != len(big) {
		t.Errorf("StdoutBytes = %d, want %d", got.StdoutBytes, len(big))
	}
	if want := sha256Hex(big); got.StdoutSHA256 != want {
		t.Errorf("StdoutSHA256 = %q, want %q", got.StdoutSHA256, want)
	}
}

func TestWriteRawBodies_PreservesNonUTF8BytesExactly(t *testing.T) {
	dir := t.TempDir()
	stdout := []byte{0xff, '\n'}
	stderr := []byte("ok")

	if err := writeRawBodies(dir, stdout, stderr); err != nil {
		t.Fatalf("writeRawBodies: %v", err)
	}

	gotStdout, err := os.ReadFile(filepath.Join(dir, "stdout.bin"))
	if err != nil {
		t.Fatalf("reading stdout.bin: %v", err)
	}
	if !bytes.Equal(gotStdout, stdout) {
		t.Errorf("stdout.bin = % x, want % x", gotStdout, stdout)
	}

	gotStderr, err := os.ReadFile(filepath.Join(dir, "stderr.bin"))
	if err != nil {
		t.Fatalf("reading stderr.bin: %v", err)
	}
	if !bytes.Equal(gotStderr, stderr) {
		t.Errorf("stderr.bin = % x, want % x", gotStderr, stderr)
	}
}

func TestWriteRawBodies_WritesEmptyFilesForEmptyOutput(t *testing.T) {
	dir := t.TempDir()
	if err := writeRawBodies(dir, nil, nil); err != nil {
		t.Fatalf("writeRawBodies: %v", err)
	}

	for _, name := range []string{"stdout.bin", "stderr.bin"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
		if info.Size() != 0 {
			t.Errorf("%s size = %d, want 0", name, info.Size())
		}
	}
}

func TestCopyEvidenceFiles_CopiesBytesIncludingEmptyFiles(t *testing.T) {
	cwdRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(cwdRoot, "content.txt"), "some bytes")
	mustWriteFile(t, filepath.Join(cwdRoot, "empty.txt"), "")

	tree, err := walkTree(cwdRoot, "cwd")
	if err != nil {
		t.Fatalf("walkTree: %v", err)
	}

	caseOutDir := t.TempDir()
	roots := map[string]string{"cwd": cwdRoot}
	if err := copyEvidenceFiles(caseOutDir, roots, tree); err != nil {
		t.Fatalf("copyEvidenceFiles: %v", err)
	}

	assertFileContent(t, filepath.Join(caseOutDir, "fs", "cwd", "content.txt"), "some bytes")

	info, err := os.Stat(filepath.Join(caseOutDir, "fs", "cwd", "empty.txt"))
	if err != nil {
		t.Fatalf("expected empty.txt to be copied: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("empty.txt size = %d, want 0", info.Size())
	}
}
