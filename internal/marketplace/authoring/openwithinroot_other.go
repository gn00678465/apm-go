//go:build !windows && !linux && !darwin

package authoring

import (
	"errors"
	"os"
)

// canonicalFilePath has no implementation on this platform: neither
// Windows' GetFinalPathNameByHandleW, Linux's /proc/self/fd/N, nor Darwin's
// F_GETPATH exists here. Per this whole package's established fail-closed
// convention (pathWithinRoot's own doc comment, refcheck.go: "cannot
// positively verify" is rejected, never silently accepted as "assume it's
// fine"), OpenLocalFileWithinRoot must never treat an unsupported platform
// as a free pass -- this always errors instead of returning a path.
func canonicalFilePath(f *os.File) (string, error) {
	return "", errors.New("openwithinroot: handle-based path canonicalization is not implemented for this platform (fail-closed)")
}
