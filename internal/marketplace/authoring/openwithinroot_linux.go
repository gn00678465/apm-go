//go:build linux

package authoring

import (
	"fmt"
	"os"
	"strconv"
)

// canonicalFilePath resolves an ALREADY-OPEN *os.File's real path via
// readlink on Linux's /proc/self/fd/<n> magic symlink -- the kernel's own
// authority on "what does this open file descriptor actually point at,"
// immune to any path-string swap (rename, symlink retarget, or a
// delete+recreate at the same path) that happens after the file was opened.
// See OpenLocalFileWithinRoot's doc comment (openwithinroot.go) for why this
// handle/descriptor-based check, rather than a second path-based lookup, is
// required. os.Readlink neither reads f's content nor moves its position.
func canonicalFilePath(f *os.File) (string, error) {
	fdPath := "/proc/self/fd/" + strconv.Itoa(int(f.Fd()))
	real, err := os.Readlink(fdPath)
	if err != nil {
		return "", fmt.Errorf("readlink %s (fd for %q): %w", fdPath, f.Name(), err)
	}
	return real, nil
}
