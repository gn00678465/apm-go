//go:build windows

package main

import "os"

// openManifestFileForPlatform opens path with a plain os.Open. Used only
// by readAndValidate's no-boundary path (a caller's own path argument,
// WP03 finding 2): the boundary path (a directory-probed candidate)
// resolves through os.OpenRoot instead (readManifestInRoot, WP03 round-3
// review), which closes the parent-directory containment race this
// function never addressed.
//
// Win32 does have an open-without-following capability --
// FILE_FLAG_OPEN_REPARSE_POINT, which os.Root's own Lstat equivalent uses
// (os/root_windows.go) -- but it opens the reparse point object itself,
// not the target's data, so it has no use for a call that must read the
// manifest's actual bytes; there is no windows analogue of O_NOFOLLOW for
// a data-read open. A symlink (reparse point) swapped into path after the
// pre-open Lstat in readAndValidate IS followed by this call; there is no
// FIFO-blocking risk because windows has no FIFO type. The only guard
// against the substituted-target case on this platform is the Fstat +
// os.SameFile comparison readAndValidate performs against that pre-open
// Lstat, after this open has already returned -- the same guard the unix
// build also relies on for that comparison, per its own comment.
func openManifestFileForPlatform(path string) (*os.File, error) {
	return os.Open(path)
}
