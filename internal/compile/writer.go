package compile

import (
	"errors"
	"os"
	"path/filepath"
)

// errSimulatedAtomicWriteFailure is returned by test doubles that swap
// atomicWrite to exercise WriteAGENTSMD's failure path (IO-03).
var errSimulatedAtomicWriteFailure = errors.New("compile: simulated atomic write failure")

// atomicWrite persists content to path via a temp-file-then-rename swap
// (design.md §6; oracle: compilation/output_writer.py). It is a package
// variable rather than a hardcoded call so tests can inject a failing stub
// to verify WriteAGENTSMD never corrupts a pre-existing AGENTS.md when the
// underlying write fails, without relying on OS-specific permission tricks.
var atomicWrite = defaultAtomicWrite

func defaultAtomicWrite(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".agents-md-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	_, writeErr := tmp.WriteString(content)
	closeErr := tmp.Close()
	if writeErr != nil {
		os.Remove(tmpPath)
		return writeErr
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return closeErr
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// WriteAGENTSMD writes content to projectDir/AGENTS.md. If the file already
// exists with byte-identical content, it is left completely untouched
// (wrote=false) -- design.md §6's idempotency contract, mirroring the
// oracle's "No changes detected; preserving existing AGENTS.md for
// idempotency" observable behavior. Otherwise the write is atomic
// (temp file + rename), so a mid-write failure can never leave a
// pre-existing AGENTS.md partially overwritten.
func WriteAGENTSMD(projectDir, content string) (wrote bool, err error) {
	path := filepath.Join(projectDir, "AGENTS.md")

	existing, readErr := os.ReadFile(path)
	if readErr == nil {
		if string(existing) == content {
			return false, nil
		}
	} else if !os.IsNotExist(readErr) {
		return false, readErr
	}

	if err := atomicWrite(path, content); err != nil {
		return false, err
	}
	return true, nil
}
