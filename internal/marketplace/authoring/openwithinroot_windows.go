//go:build windows

package authoring

import (
	"os"
	"syscall"
)

// canonicalFilePath resolves an ALREADY-OPEN *os.File's real path via
// GetFinalPathNameByHandleW called directly on that file's OWN handle
// (f.Fd()) -- not a fresh, path-based re-open like canonicalizeRealPath
// (canonicalize_windows.go) performs -- so the canonical path
// OpenLocalFileWithinRoot compares against root (openwithinroot.go) is
// guaranteed to describe the exact same open file that will actually be
// read, closing the TOCTOU window a check-then-open pattern leaves (see
// OpenLocalFileWithinRoot's own doc comment). f.Fd() is Windows' own HANDLE
// value cast to uintptr (os.File's documented contract: "It is valid to
// call Fd and pass the returned handle to functions in package syscall");
// GetFinalPathNameByHandleW is a pure metadata query and never moves f's
// read position, so the caller can go on to read f normally afterwards.
func canonicalFilePath(f *os.File) (string, error) {
	return canonicalPathFromHandle(f.Name(), syscall.Handle(f.Fd()))
}
