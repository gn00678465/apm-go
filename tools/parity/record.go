//go:build unix

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// inlineThreshold is the inline-body cap (acceptance: 64 KiB). Raw bytes
// beyond this, or bytes that are not valid UTF-8, are never put in a JSON
// string field — encoding/json would silently replace invalid UTF-8 with
// U+FFFD, corrupting the evidence (see eval-ticket-01.md finding "非 UTF-8
// raw output 遺失"). The full raw bytes always live in the sibling
// stdout.bin/stderr.bin files regardless of this cap.
const inlineThreshold = 64 * 1024

// Record is the full per-case-per-side evidence: <out>/<side>/<id>/record.json.
// Stdout/Stderr are populated only when the captured bytes are valid UTF-8
// and small enough to inline; otherwise the field is omitted and a consumer
// reads the byte-exact <id>/stdout.bin or <id>/stderr.bin instead. SHA256 and
// byte-count fields are always present so a reviewer can verify the .bin
// against the record without re-hashing large files by hand.
type Record struct {
	ID           string            `json:"id"`
	Argv         []string          `json:"argv"`
	EnvDelta     map[string]string `json:"env_delta"`
	ExitCode     int               `json:"exit_code"`
	TimedOut     bool              `json:"timed_out"`
	Stdout       *string           `json:"stdout,omitempty"`
	StdoutSHA256 string            `json:"stdout_sha256"`
	StdoutBytes  int               `json:"stdout_bytes"`
	Stderr       *string           `json:"stderr,omitempty"`
	StderrSHA256 string            `json:"stderr_sha256"`
	StderrBytes  int               `json:"stderr_bytes"`
	Tree         []TreeEntry       `json:"tree"`

	// stdoutRaw/stderrRaw are the exact captured bytes, unexported so they
	// never round-trip through JSON (Stdout/Stderr above are the JSON view,
	// which lossily omits itself for large or non-UTF-8 bodies). writeRawBodies
	// is what actually persists them.
	stdoutRaw []byte
	stderrRaw []byte
}

// NewRecord builds a Record from the exact bytes a subprocess wrote, deriving
// the sha256/byte-count evidence and the (possibly absent) inline string view
// for each of stdout/stderr.
func NewRecord(id string, argv []string, envDelta map[string]string, exitCode int, timedOut bool, stdoutRaw, stderrRaw []byte, tree []TreeEntry) Record {
	return Record{
		ID:           id,
		Argv:         argv,
		EnvDelta:     envDelta,
		ExitCode:     exitCode,
		TimedOut:     timedOut,
		Stdout:       inlineBody(stdoutRaw),
		StdoutSHA256: sha256Hex(stdoutRaw),
		StdoutBytes:  len(stdoutRaw),
		Stderr:       inlineBody(stderrRaw),
		StderrSHA256: sha256Hex(stderrRaw),
		StderrBytes:  len(stderrRaw),
		Tree:         tree,
		stdoutRaw:    stdoutRaw,
		stderrRaw:    stderrRaw,
	}
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// inlineBody returns a pointer to raw decoded as a string when it is valid
// UTF-8 and no larger than inlineThreshold; otherwise nil, which the caller's
// `json:"...,omitempty"` tag turns into an absent field. This is the only
// gate that matters for "lose nothing": the raw bytes are never lost, only
// this convenience view of them is sometimes unavailable.
func inlineBody(raw []byte) *string {
	if len(raw) > inlineThreshold || !utf8.Valid(raw) {
		return nil
	}
	s := string(raw)
	return &s
}

// jsonlRecord is one line of <out>/<side>.jsonl: identical to Record's JSON
// view, except when a body isn't inlined it also carries a path to the
// case's <side>/<id>/stdout.bin (or stderr.bin), so a reader never has to
// guess where the real bytes are.
type jsonlRecord struct {
	ID           string            `json:"id"`
	Argv         []string          `json:"argv"`
	EnvDelta     map[string]string `json:"env_delta"`
	ExitCode     int               `json:"exit_code"`
	TimedOut     bool              `json:"timed_out"`
	Stdout       *string           `json:"stdout,omitempty"`
	StdoutSHA256 string            `json:"stdout_sha256"`
	StdoutBytes  int               `json:"stdout_bytes"`
	StdoutPath   *string           `json:"stdout_path,omitempty"`
	Stderr       *string           `json:"stderr,omitempty"`
	StderrSHA256 string            `json:"stderr_sha256"`
	StderrBytes  int               `json:"stderr_bytes"`
	StderrPath   *string           `json:"stderr_path,omitempty"`
	Tree         []TreeEntry       `json:"tree"`
}

// toJSONLRecord builds the JSONL line for r. caseDir is r's own evidence
// directory (e.g. "oracle/some-case"), relative to the report root, used to
// point at stdout.bin/stderr.bin when a body isn't inlined.
func toJSONLRecord(r Record, caseDir string) jsonlRecord {
	out := jsonlRecord{
		ID:           r.ID,
		Argv:         r.Argv,
		EnvDelta:     r.EnvDelta,
		ExitCode:     r.ExitCode,
		TimedOut:     r.TimedOut,
		Stdout:       r.Stdout,
		StdoutSHA256: r.StdoutSHA256,
		StdoutBytes:  r.StdoutBytes,
		Stderr:       r.Stderr,
		StderrSHA256: r.StderrSHA256,
		StderrBytes:  r.StderrBytes,
		Tree:         r.Tree,
	}

	if out.Stdout == nil {
		p := caseDir + "/stdout.bin"
		out.StdoutPath = &p
	}
	if out.Stderr == nil {
		p := caseDir + "/stderr.bin"
		out.StderrPath = &p
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

// writeRawBodies writes <caseOutDir>/stdout.bin and stderr.bin with the exact
// captured bytes, always — regardless of size or UTF-8 validity, and even
// when empty. This is the byte-exact ground truth that record.json's inline
// Stdout/Stderr strings are only a lossy, size-capped view of.
func writeRawBodies(caseOutDir string, stdoutRaw, stderrRaw []byte) error {
	if err := os.MkdirAll(caseOutDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(caseOutDir, "stdout.bin"), stdoutRaw, 0o644); err != nil {
		return fmt.Errorf("writing stdout.bin: %w", err)
	}
	if err := os.WriteFile(filepath.Join(caseOutDir, "stderr.bin"), stderrRaw, 0o644); err != nil {
		return fmt.Errorf("writing stderr.bin: %w", err)
	}
	return nil
}

// appendJSONLRecord appends one line to the side's JSONL report file.
// caseDir is r's evidence directory relative to the report root (see
// toJSONLRecord).
func appendJSONLRecord(path string, r Record, caseDir string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(toJSONLRecord(r, caseDir))
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
