package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// inlineThreshold is the JSONL inlining cap (acceptance: 64 KiB). record.json
// always carries the full raw bytes regardless of size; only the JSONL line
// applies this threshold, referencing record.json by path once exceeded so
// nothing is ever silently truncated.
const inlineThreshold = 64 * 1024

// Record is the full per-case-per-side evidence: <out>/<side>/<id>/record.json.
// stdout/stderr are always the complete raw bytes here, never truncated.
type Record struct {
	ID       string            `json:"id"`
	Argv     []string          `json:"argv"`
	EnvDelta map[string]string `json:"env_delta"`
	ExitCode int               `json:"exit_code"`
	TimedOut bool              `json:"timed_out"`
	Stdout   string            `json:"stdout"`
	Stderr   string            `json:"stderr"`
	Tree     []TreeEntry       `json:"tree"`
}

// jsonlRecord is one line of <out>/<side>.jsonl: identical to Record, except
// a stdout/stderr body over inlineThreshold is replaced by a path reference
// into the case's record.json (which always has the full body).
type jsonlRecord struct {
	ID         string            `json:"id"`
	Argv       []string          `json:"argv"`
	EnvDelta   map[string]string `json:"env_delta"`
	ExitCode   int               `json:"exit_code"`
	TimedOut   bool              `json:"timed_out"`
	Stdout     *string           `json:"stdout,omitempty"`
	StdoutPath *string           `json:"stdout_path,omitempty"`
	Stderr     *string           `json:"stderr,omitempty"`
	StderrPath *string           `json:"stderr_path,omitempty"`
	Tree       []TreeEntry       `json:"tree"`
}

// toJSONLRecord builds the JSONL line for r. recordPath is the path to r's
// own record.json, relative to the report root, used as the reference when a
// body is too large to inline.
func toJSONLRecord(r Record, recordPath string) jsonlRecord {
	out := jsonlRecord{
		ID:       r.ID,
		Argv:     r.Argv,
		EnvDelta: r.EnvDelta,
		ExitCode: r.ExitCode,
		TimedOut: r.TimedOut,
		Tree:     r.Tree,
	}

	if len(r.Stdout) > inlineThreshold {
		out.StdoutPath = &recordPath
	} else {
		stdout := r.Stdout
		out.Stdout = &stdout
	}

	if len(r.Stderr) > inlineThreshold {
		out.StderrPath = &recordPath
	} else {
		stderr := r.Stderr
		out.Stderr = &stderr
	}

	return out
}

// writeRecordJSON writes r to <caseOutDir>/record.json.
func writeRecordJSON(caseOutDir string, r Record) error {
	if err := os.MkdirAll(caseOutDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling record for %s: %w", r.ID, err)
	}
	return os.WriteFile(filepath.Join(caseOutDir, "record.json"), data, 0o644)
}

// appendJSONLRecord appends one line to the side's JSONL report file.
func appendJSONLRecord(path string, r Record, recordPath string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(toJSONLRecord(r, recordPath))
	if err != nil {
		return fmt.Errorf("marshalling jsonl record for %s: %w", r.ID, err)
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// copyEvidenceFiles copies the raw bytes of every "file" tree entry into
// <caseOutDir>/fs/<path>, preserving the label/relative-path layout used in
// TreeEntry.Path. roots maps each label ("cwd", "config") to the sandbox
// directory it was walked from. Empty files are copied too — an empty source
// still produces an empty destination file via the O_CREATE open.
func copyEvidenceFiles(caseOutDir string, roots map[string]string, tree []TreeEntry) error {
	fsRoot := filepath.Join(caseOutDir, "fs")
	for _, e := range tree {
		if e.Kind != "file" {
			continue
		}
		label, rel, ok := splitLabel(e.Path)
		if !ok {
			continue
		}
		root, ok := roots[label]
		if !ok {
			continue
		}

		src := filepath.Join(root, filepath.FromSlash(rel))
		dst := filepath.Join(fsRoot, filepath.FromSlash(e.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := copyFileBytes(src, dst); err != nil {
			return fmt.Errorf("copying evidence file %s: %w", e.Path, err)
		}
	}
	return nil
}

func splitLabel(path string) (label, rel string, ok bool) {
	idx := strings.IndexByte(path, '/')
	if idx < 0 {
		return "", "", false
	}
	return path[:idx], path[idx+1:], true
}

func copyFileBytes(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
