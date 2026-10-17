package deploy

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// canSymlink tries one symlink in a temporary directory. Windows refuses one
// with ERROR_PRIVILEGE_NOT_HELD to an account that is not an administrator
// and has no Developer Mode, the default; any other result keeps the symlink
// deploy, which then reports its own errors as before.
func canSymlink() bool {
	dir, err := os.MkdirTemp("", "apm-go-symlink-probe-")
	if err != nil {
		return true
	}
	defer os.RemoveAll(dir)
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		return true
	}
	err = os.Symlink(target, filepath.Join(dir, "link"))
	return !errors.Is(err, syscall.ERROR_PRIVILEGE_NOT_HELD)
}
