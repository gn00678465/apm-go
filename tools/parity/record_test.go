package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToJSONLRecord_InlinesSmallBodies(t *testing.T) {
	r := Record{ID: "small", Stdout: "hello", Stderr: "world"}
	out := toJSONLRecord(r, "target/small/record.json")

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

func TestToJSONLRecord_ReferencesLargeBodiesByPath(t *testing.T) {
	big := strings.Repeat("x", inlineThreshold+1)
	r := Record{ID: "big", Stdout: big, Stderr: "small"}
	out := toJSONLRecord(r, "oracle/big/record.json")

	if out.Stdout != nil {
		t.Errorf("Stdout = %v, want nil (referenced instead)", out.Stdout)
	}
	if out.StdoutPath == nil || *out.StdoutPath != "oracle/big/record.json" {
		t.Errorf("StdoutPath = %v, want %q", out.StdoutPath, "oracle/big/record.json")
	}
	// Stderr stays inlined: the threshold applies per-field, not per-record.
	if out.Stderr == nil || *out.Stderr != "small" {
		t.Errorf("Stderr = %v, want inlined %q", out.Stderr, "small")
	}
	if out.StderrPath != nil {
		t.Errorf("StderrPath = %v, want nil", out.StderrPath)
	}
}

func TestAppendJSONLRecord_OneLinePerCase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.jsonl")

	if err := appendJSONLRecord(path, Record{ID: "c1", Stdout: "a"}, "target/c1/record.json"); err != nil {
		t.Fatalf("appendJSONLRecord: %v", err)
	}
	if err := appendJSONLRecord(path, Record{ID: "c2", Stdout: "b"}, "target/c2/record.json"); err != nil {
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

func TestWriteRecordJSON_AlwaysInlinesFullBodyRegardlessOfSize(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("y", inlineThreshold+100)
	r := Record{ID: "full", Stdout: big}

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
	if got.Stdout != big {
		t.Errorf("record.json stdout truncated: got %d bytes, want %d", len(got.Stdout), len(big))
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
