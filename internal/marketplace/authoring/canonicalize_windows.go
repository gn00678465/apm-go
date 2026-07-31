//go:build windows

package authoring

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

// procGetFinalPathNameByHandleW is kernel32.dll's GetFinalPathNameByHandleW,
// invoked directly via syscall.NewLazyDLL/NewProc (standard library `syscall`
// only -- no golang.org/x/sys/windows or any other new dependency, mirroring
// reparse_windows.go's own syscall.CreateFile/DeviceIoControl calls).
var procGetFinalPathNameByHandleW = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFinalPathNameByHandleW")

// finalPathNameFlags requests Windows' own normalized, DOS-drive-letter
// canonical form (both are the zero value: FILE_NAME_NORMALIZED and
// VOLUME_NAME_DOS are the API's defaults, spelled out here for clarity since
// their names, not "0", are what winbase.h documents).
const finalPathNameFlags = 0 // FILE_NAME_NORMALIZED (0x0) | VOLUME_NAME_DOS (0x0)

// volumeNameGUIDFlag is Win32's VOLUME_NAME_GUID flag for
// GetFinalPathNameByHandleW (winbase.h/fileapi.h: VOLUME_NAME_DOS 0x0,
// VOLUME_NAME_GUID 0x1, VOLUME_NAME_NT 0x2, VOLUME_NAME_NONE 0x4). Spelled
// out numerically here -- not sourced from golang.org/x/sys/windows -- to
// keep this package's "standard syscall only, no new dependency" convention.
// canonicalizeRealPath falls back to this (B-MAJOR-1, external audit round 9,
// 2026-07-31 follow-up) when the default VOLUME_NAME_DOS request fails with
// ERROR_PATH_NOT_FOUND: that happens for a volume mounted WITHOUT an
// assigned DOS drive letter (e.g. mounted only at an NTFS mount point) --
// GetFinalPathNameByHandleW cannot produce a drive-letter spelling for it at
// all, even though the handle opened fine and the path unambiguously exists.
// VOLUME_NAME_GUID's `\\?\Volume{GUID}\...` spelling never depends on a
// drive letter existing, so it succeeds where VOLUME_NAME_DOS structurally
// cannot -- without it, canonicalizeRealPath's fail-closed contract
// (ok=false on ANY canonicalization failure) turned a legitimate,
// non-drive-lettered path into a false rejection.
const volumeNameGUIDFlag = 0x1

// getFinalPathNameByHandleFn is getFinalPathNameByHandle behind a swappable
// package-level var -- the same seam-injection pattern refcheck.go's
// canonicalizeRealPathFn uses -- so a test can simulate the
// ERROR_PATH_NOT_FOUND -> VOLUME_NAME_GUID retry below without depending on
// a real volume mounted without an assigned DOS drive letter (there is no
// portable, privilege-free way to create one for a single test's temp
// directory). Production code always uses the real getFinalPathNameByHandle.
var getFinalPathNameByHandleFn = getFinalPathNameByHandle

// canonicalizeRealPath resolves path -- which callers only ever pass as
// resolveRealPathJunctionAware's own output, itself only ever invoked against
// an ancestor longestExistingAncestor already confirmed exists -- to
// Windows' own canonical "final path" via GetFinalPathNameByHandleW.
//
// B-MINOR-1 (external audit round 8, 2026-07-31 follow-up): pathWithinRoot's
// final containment comparison (pathWithinRootLexical) is a plain string
// comparison over whatever spelling resolveRealPathJunctionAware happened to
// produce. Windows lets the SAME physical directory be spelled multiple
// distinct ways -- an 8.3 short name alias (`C:\PROGRA~1` for
// `C:\Program Files`), a `\\?\Volume{GUID}\...` volume-GUID path, or a UNC
// loopback path (`\\localhost\C$\...`) -- none of which a plain string
// comparison against root's own (differently-spelled) string can be trusted
// to line up with, one way or the other. GetFinalPathNameByHandleW is
// Windows' own authority on "what is this handle's one canonical path,"
// removing that ambiguity before the string comparison ever runs.
//
// ok is false (fail closed, per this whole task's established convention for
// "cannot positively verify") when the path cannot be opened, or the API
// call itself fails for any reason -- a caller must never treat a
// canonicalization failure as "assume it's fine, compare the un-canonicalized
// strings instead," since that's exactly the ambiguity this exists to close.
func canonicalizeRealPath(path string) (string, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", fmt.Errorf("canonicalize %q: %w", path, err)
	}
	h, err := syscall.CreateFile(
		p,
		0,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS, // required to open a directory handle
		0,
	)
	if err != nil {
		return "", fmt.Errorf("canonicalize %q: open: %w", path, err)
	}
	defer syscall.CloseHandle(h)

	result, callErr := getFinalPathNameByHandleFn(h, finalPathNameFlags)
	if callErr != nil {
		// B-MAJOR-1 (round 9): only retry with VOLUME_NAME_GUID for the
		// SPECIFIC failure mode a missing drive letter produces
		// (ERROR_PATH_NOT_FOUND) -- any other failure (e.g. the handle
		// itself somehow became invalid) still fails closed immediately,
		// matching this function's existing convention of never guessing
		// past an error it cannot positively explain.
		if !errors.Is(callErr, syscall.ERROR_PATH_NOT_FOUND) {
			return "", fmt.Errorf("canonicalize %q: GetFinalPathNameByHandleW: %w", path, callErr)
		}
		result, callErr = getFinalPathNameByHandleFn(h, volumeNameGUIDFlag)
		if callErr != nil {
			return "", fmt.Errorf("canonicalize %q: GetFinalPathNameByHandleW (VOLUME_NAME_GUID fallback): %w", path, callErr)
		}
	}

	return stripExtendedLengthPrefix(result), nil
}

// getFinalPathNameByHandle wraps one GetFinalPathNameByHandleW call for the
// given already-open handle and flags, returning the raw (still
// \\?\-prefixed) result string.
func getFinalPathNameByHandle(h syscall.Handle, flags uintptr) (string, error) {
	// 32K covers the longest path Windows can address even with the \\?\
	// extended-length prefix GetFinalPathNameByHandleW itself prepends.
	buf := make([]uint16, 32*1024)
	r1, _, callErr := procGetFinalPathNameByHandleW.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		flags,
	)
	if r1 == 0 {
		return "", callErr
	}
	if int(r1) >= len(buf) {
		return "", fmt.Errorf("result truncated")
	}
	return syscall.UTF16ToString(buf[:r1]), nil
}

// stripExtendedLengthPrefix undoes GetFinalPathNameByHandleW's always-present
// \\?\ extended-length prefix (and \\?\UNC\ for a UNC target), so downstream
// filepath.Rel/filepath.Clean callers in this package see the ordinary
// drive-letter or UNC spelling every other path here already works with,
// not an extended-length one.
//
// B-MAJOR-1 (external audit round 9, 2026-07-31 follow-up): a UNC target's
// ordinary spelling is `\\server\share` -- TWO leading backslashes, one of
// them the UNC prefix marker itself. The prior version prepended only ONE
// (`\` + rest after stripping the literal "UNC\" segment), producing
// `\server\share`, which is not a valid UNC path at all and would never
// round-trip through filepath.Rel/Clean the same way root's own
// `\\server\share` spelling does -- silently breaking containment
// comparisons for every UNC-rooted local source.
func stripExtendedLengthPrefix(result string) string {
	result = strings.TrimPrefix(result, `\\?\`)
	if rest, isUNC := strings.CutPrefix(result, `UNC\`); isUNC {
		result = `\\` + rest
	}
	return result
}
