package gitops

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf16"

	"github.com/apm-go/apm/internal/manifest"
)

const (
	errorCantResolveFilename syscall.Errno = 1921
	// MAKELANGID(LANG_NEUTRAL, SUBLANG_DEFAULT): the system language.
	langSystemDefault = 0x0400
)

// realpathStrict is Python 3.12's os.path.realpath(path, strict=True) on
// Windows, which pathlib's resolve(strict=True) calls for the Oracle
// (install/phases/local_content.py:139-148). ntpath.realpath opens the whole
// path once (os._getfinalpathname: CreateFileW with
// FILE_FLAG_BACKUP_SEMANTICS, which follows every symlink and junction) and,
// with strict, raises that call's OSError as it is: the reason names path
// itself, never a component or the target. pathlib's check_eloop turns
// ERROR_CANT_RESOLVE_FILENAME into "Symlink loop from %r", so that error is
// returned as ELOOP, which copyTreeDereferencing reports that way. A path
// that opens is resolved with filepath.EvalSymlinks, so it is spelled as
// archive.RealPath spells the package root it is compared with.
func realpathStrict(path string) (string, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", &os.PathError{Op: "CreateFile", Path: path, Err: err}
	}
	h, err := syscall.CreateFile(p, 0, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		code, ok := err.(syscall.Errno)
		switch {
		case !ok:
			return "", &os.PathError{Op: "CreateFile", Path: path, Err: err}
		case code == errorCantResolveFilename:
			return "", &os.PathError{Op: "CreateFile", Path: path, Err: syscall.ELOOP}
		}
		return "", &winError{code: code, path: path}
	}
	syscall.CloseHandle(h)
	return filepath.EvalSymlinks(path)
}

// winError prints as Python 3.12's str(OSError) for a Windows error,
// "[WinError <n>] <message>: <repr(filename)>" (Objects/exceptions.c
// OSError_str). pyOSError passes it through as it is.
type winError struct {
	code syscall.Errno
	path string
}

func (e *winError) Error() string {
	return fmt.Sprintf("[WinError %d] %s: %s", uint32(e.code), winMessage(e.code), manifest.PyRepr(e.path))
}

// winMessage is the message Python/errors.c
// (PyErr_SetExcFromWindowsErrWithFilenameObjects) gives a Windows error code:
// FormatMessageW's text in the system language, with every trailing character
// up to ' ' and every trailing ASCII '.' removed. syscall.Errno.Error is the
// English text with its dot, which differs on a system in another language.
func winMessage(code syscall.Errno) string {
	buf := make([]uint16, 1<<15-1)
	n, err := syscall.FormatMessage(syscall.FORMAT_MESSAGE_FROM_SYSTEM|syscall.FORMAT_MESSAGE_IGNORE_INSERTS, 0, uint32(code), langSystemDefault, buf, nil)
	if err != nil || n == 0 {
		return fmt.Sprintf("Windows Error 0x%x", uint32(code))
	}
	for n > 0 && (buf[n-1] <= ' ' || buf[n-1] == '.') {
		n--
	}
	return string(utf16.Decode(buf[:n]))
}
