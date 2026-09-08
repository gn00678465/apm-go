//go:build windows

package authoring

import (
	"os"
	"syscall"
)

// fsctlGetReparsePoint is FSCTL_GET_REPARSE_POINT (winioctl.h): reads a
// reparse point's raw REPARSE_DATA_BUFFER. Every REPARSE_DATA_BUFFER layout
// (symbolic link, mount point, or a third party's own generic reparse
// buffer) begins with the same 4-byte ULONG ReparseTag field, so reading
// just that prefix is enough to classify the reparse point without parsing
// the rest of whichever tag-specific union follows it.
const fsctlGetReparsePoint = 0x000900A8

// maxReparseDataBufferSize is MAXIMUM_REPARSE_DATA_BUFFER_SIZE (winnt.h):
// the largest buffer FSCTL_GET_REPARSE_POINT can ever fill.
const maxReparseDataBufferSize = 16 * 1024

// isJunctionOrUnknownReparsePoint reports whether fi's file, at path, carries
// Windows' FILE_ATTRIBUTE_REPARSE_POINT flag AND that reparse point is a
// "name surrogate" (isNameSurrogateReparseTag, reparse_tags.go) -- i.e. a
// directory junction/mount point or another reparse kind that redirects path
// resolution elsewhere, the only kind isReparsePoint (refcheck.go) needs to
// treat as something requiring resolution and containment-checking.
//
// As of Go 1.23, os.Lstat no longer reports ModeSymlink for a junction by
// default (GODEBUG=winsymlink; see Go's os/types_windows.go, fileStat.Mode)
// -- and filepath.EvalSymlinks decides whether to follow a path component
// solely by that bit, so it silently stops at a junction and returns it
// unresolved instead of erroring. Verified directly: os.Lstat(junction).Mode()
// reports os.ModeIrregular (not ModeSymlink), and filepath.EvalSymlinks
// (pathThroughJunction) returns the path UNCHANGED with a nil error on this
// Go toolchain, while os.Readlink(junction) DOES correctly resolve it (see
// resolveReparsePointTarget, refcheck.go).
//
// B-MINOR-1 (external audit round 7, 2026-07-31 follow-up): see
// isNameSurrogateReparseTag's own doc comment (reparse_tags.go) for the full
// false-positive this fixes (OneDrive/Cloud Files placeholders and similar
// non-name-surrogate reparse points used to be rejected outright as
// unresolvable). Any file this cannot positively read a tag for -- a stat
// error, an unexpected Sys() type, or a failed DeviceIoControl -- is still
// treated as a name surrogate: fail closed, unchanged from before, since
// apm-go then has no way to prove the component does not escape root.
func isJunctionOrUnknownReparsePoint(path string, fi os.FileInfo) bool {
	sys, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return true
	}
	if sys.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return false
	}
	tag, ok := readReparseTag(path)
	if !ok {
		return true // fail closed: cannot positively classify this reparse point
	}
	return isNameSurrogateReparseTag(tag)
}

// readReparseTag opens path (without following any reparse point it may
// itself be, via FILE_FLAG_OPEN_REPARSE_POINT) and issues
// FSCTL_GET_REPARSE_POINT to read its raw REPARSE_DATA_BUFFER, returning
// just the leading 4-byte ReparseTag field. ok is false when the file cannot
// be opened this way, or the FSCTL itself fails or returns fewer than 4
// bytes -- callers must treat that as "cannot positively classify," i.e.
// fail closed (isJunctionOrUnknownReparsePoint, above).
//
// Residual gap (honestly disclosed, not "later"): this function's own
// DeviceIoControl plumbing has not been exercised against a REAL OneDrive/
// Cloud Files placeholder or AppExecLink in this codebase's test suite --
// doing so would require a registered Cloud Files sync provider (or an
// actual OneDrive-synced folder) on the machine running `go test`, which is
// not something a unit test can construct or assume present. What IS tested
// directly (TestIsNameSurrogateReparseTag, refcheck_test.go) is the pure bit
// arithmetic this function's result feeds into, against Microsoft's own
// documented tag values. Cost estimate for closing this specific residual
// gap: registering a minimal Cloud Files sync provider via the
// CfRegisterSyncRoot Win32 API purely for a test fixture is itself
// substantial new surface (a new syscall binding, administrative
// registration, and cleanup) for a single MINOR-severity false-positive this
// function already fails safe on (any DeviceIoControl error still rejects,
// unchanged from before) -- not undertaken here.
func readReparseTag(path string) (tag uint32, ok bool) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, false
	}
	h, err := syscall.CreateFile(
		p,
		0,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return 0, false
	}
	defer syscall.CloseHandle(h)

	buf := make([]byte, maxReparseDataBufferSize)
	var bytesReturned uint32
	if err := syscall.DeviceIoControl(h, fsctlGetReparsePoint, nil, 0, &buf[0], uint32(len(buf)), &bytesReturned, nil); err != nil {
		return 0, false
	}
	if bytesReturned < 4 {
		return 0, false
	}
	return uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16 | uint32(buf[3])<<24, true
}
