//go:build !windows

package gitops

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// realpathStrict is Python 3.12's os.path.realpath(path, strict=True) for an
// absolute POSIX path (posixpath.py:420-496). It resolves one component at a
// time, so an error names the component Python's names, and a symlink met
// again while it is being resolved is ELOOP on that symlink; filepath.
// EvalSymlinks gives up after 255 links and names no path. Every error is an
// *os.PathError.
func realpathStrict(path string) (string, error) {
	return joinRealpath("", path, map[string]*string{})
}

// joinRealpath is posixpath._joinrealpath with strict=True. seen maps a
// symlink to its resolved path, or to nil while it is being resolved.
func joinRealpath(path, rest string, seen map[string]*string) (string, error) {
	if strings.HasPrefix(rest, "/") {
		rest, path = rest[1:], "/"
	}
	for rest != "" {
		var name string
		name, rest, _ = strings.Cut(rest, "/")
		switch name {
		case "", ".":
			continue
		case "..":
			path = filepath.Dir(path)
			continue
		}
		newpath := filepath.Join(path, name)
		info, err := os.Lstat(newpath)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			path = newpath
			continue
		}
		if resolved, ok := seen[newpath]; ok {
			if resolved != nil {
				path = *resolved
				continue
			}
			if _, err := os.Stat(newpath); err != nil {
				return "", err
			}
			return "", &os.PathError{Op: "stat", Path: newpath, Err: syscall.ELOOP}
		}
		seen[newpath] = nil
		target, err := os.Readlink(newpath)
		if err != nil {
			return "", err
		}
		if path, err = joinRealpath(path, target, seen); err != nil {
			return "", err
		}
		resolved := path
		seen[newpath] = &resolved
	}
	return path, nil
}
