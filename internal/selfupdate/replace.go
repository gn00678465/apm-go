package selfupdate

import (
	"fmt"
	"os"
	"runtime"
)

var osRename = os.Rename

// replaceBinary replaces the binary at dst with the one at src.
//
// On Unix, os.Rename over the running binary works because the OS
// keeps the old inode alive until the process exits. The rename succeeds
// only because src is in the directory of dst, so both are on one file
// system; across file systems it fails with EXDEV.
//
// On Windows, the running binary is locked. The strategy: rename the
// running binary to dst+".old", copy the new binary to dst, then
// attempt to remove the .old file (may fail if still locked; cleaned
// up on next run or by the user).
func replaceBinary(dst, src string) error {
	if runtime.GOOS == "windows" {
		return replaceWindows(dst, src)
	}
	return replaceUnix(dst, src)
}

func replaceUnix(dst, src string) error {
	// os.CreateTemp made src 0600; a release binary is 0755.
	if err := os.Chmod(src, 0o755); err != nil {
		return fmt.Errorf("chmod new binary: %w", err)
	}

	if err := osRename(src, dst); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}
	return nil
}

func replaceWindows(dst, src string) error {
	oldPath := dst + oldBinarySuffix

	// Clean up leftover .old from a previous update.
	os.Remove(oldPath)

	if err := os.Rename(dst, oldPath); err != nil {
		return fmt.Errorf("rename current binary: %w", err)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		// Restore the original binary on failure.
		os.Rename(oldPath, dst)
		return fmt.Errorf("read new binary: %w", err)
	}

	if err := os.WriteFile(dst, data, 0o755); err != nil {
		os.Rename(oldPath, dst)
		return fmt.Errorf("write new binary: %w", err)
	}

	// Best-effort cleanup. The .old file may still be locked.
	os.Remove(oldPath)
	return nil
}
