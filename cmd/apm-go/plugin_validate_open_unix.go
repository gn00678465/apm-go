//go:build !windows

package main

import (
	"os"
	"syscall"
)

// openManifestFileForPlatform opens path with O_NOFOLLOW|O_NONBLOCK (WP03
// round-2 review finding 1): O_NOFOLLOW makes the open fail outright if the
// final path component is a symlink at open time, regardless of what the
// pre-open Lstat in readAndValidate saw; O_NONBLOCK makes opening a FIFO
// with no writer attached return immediately instead of blocking forever.
// Guarantee on this platform: the open itself cannot follow a symlink or
// hang on a FIFO swapped into path after that Lstat. It does not by itself
// prove the opened file IS the one Lstat saw -- readAndValidate's Fstat +
// os.SameFile comparison after this call still does that, on every
// platform.
func openManifestFileForPlatform(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}
