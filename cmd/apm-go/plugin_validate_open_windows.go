//go:build windows

package main

import "os"

// openManifestFileForPlatform opens path with a plain os.Open (WP03 round-2
// review finding 1): windows has no O_NOFOLLOW/O_NONBLOCK equivalent and no
// FIFO type, so this open's guarantee is strictly weaker than the unix
// build's. A symlink (reparse point) swapped into path after the pre-open
// Lstat in readAndValidate IS followed by this call; there is no
// FIFO-blocking risk because windows has none. The only guard against the
// substituted-target case on this platform is the Fstat + os.SameFile
// comparison readAndValidate performs against that pre-open Lstat, after
// this open has already returned.
func openManifestFileForPlatform(path string) (*os.File, error) {
	return os.Open(path)
}
