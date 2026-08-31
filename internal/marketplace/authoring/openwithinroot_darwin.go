//go:build darwin

package authoring

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// darwinMaxPathLen is Darwin's MAXPATHLEN (<sys/param.h>) -- fcntl(2)'s own
// documented minimum buffer size for F_GETPATH ("the buffer must be large
// enough to hold the full pathname, MAXPATHLEN in size").
const darwinMaxPathLen = 1024

// canonicalFilePath resolves an ALREADY-OPEN *os.File's real path via
// fcntl(F_GETPATH) called directly on that file's OWN descriptor -- macOS's
// own authority on "what path does this open descriptor actually resolve
// to," immune to any path-string swap that happens after the file was
// opened (see OpenLocalFileWithinRoot's doc comment, openwithinroot.go).
//
// Implemented via the standard library's own syscall.Syscall (SYS_FCNTL is
// part of the stable, libSystem-backed syscall surface Go's syscall package
// already exposes on darwin, via syscall.Syscall's own asm trampoline) --
// deliberately not golang.org/x/sys/unix, which is already an indirect
// module dependency (go.mod) but would become a direct one the moment any
// package imports it, a go.mod change this task's constraints avoid.
func canonicalFilePath(f *os.File) (string, error) {
	buf := make([]byte, darwinMaxPathLen)
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), uintptr(syscall.F_GETPATH), uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return "", fmt.Errorf("fcntl(F_GETPATH) on %q: %w", f.Name(), errno)
	}
	return string(buf[:cStringLen(buf)]), nil
}

// cStringLen returns the index of the first NUL byte in buf (F_GETPATH
// fills a NUL-terminated C string into its buffer), or len(buf) if none is
// found.
func cStringLen(buf []byte) int {
	for i, c := range buf {
		if c == 0 {
			return i
		}
	}
	return len(buf)
}
