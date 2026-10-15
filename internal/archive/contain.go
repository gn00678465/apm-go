package archive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// maxDanglingLinks bounds RealPath's walk through dangling symlinks, the one
// part of the resolution filepath.EvalSymlinks does not do and so does not
// bound.
const maxDanglingLinks = 255

// ContainmentError is a path below a base directory that, with every symlink
// resolved, is not below that base. Its text is the Oracle's
// (utils/path_security.py:205-221): a PathTraversalError wrapped once more by
// the except clause around it.
type ContainmentError struct {
	// Path is the absolute path, its symlinks not resolved.
	Path string
	// Base is the base directory, symlinks resolved.
	Base string
	// Resolved is Path with every symlink resolved, the last one included.
	Resolved string
}

func (e *ContainmentError) Error() string {
	return fmt.Sprintf("Cannot verify containment of '%s' within '%s': Path '%s' resolves to '%s' which is outside the allowed base directory '%s'",
		e.Path, e.Base, e.Path, e.Resolved, e.Base)
}

// CheckWithin refuses target unless, with every symlink on it resolved, the
// last component included, it is strictly below base with base's own
// symlinks resolved. A symlinked base passes, since both sides are compared
// resolved. It is the Oracle's ensure_path_within, run before a package
// directory below apm_modules is removed or written: os.RemoveAll follows a
// symlinked parent, so a string check alone lets it delete outside base.
func CheckWithin(base, target string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	realBase, err := RealPath(base)
	if err != nil {
		return fmt.Errorf("Cannot verify containment of '%s' within '%s': %w", abs, base, err)
	}
	resolved, err := RealPath(abs)
	if err != nil {
		return fmt.Errorf("Cannot verify containment of '%s' within '%s': %w", abs, realBase, err)
	}
	if resolved == realBase || !Within(realBase, resolved) {
		return &ContainmentError{Path: abs, Base: realBase, Resolved: resolved}
	}
	return nil
}

// RealPath is p made absolute with every symlink resolved, as Python's
// Path.resolve(strict=False) resolves it: the part of p that does not exist
// yet (apm_modules before the first install) is joined to the real path of
// the nearest directory above it that does, and a dangling symlink is
// replaced by its target.
func RealPath(p string) (string, error) {
	return realPath(p, 0)
}

func realPath(p string, links int) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved, nil
	}
	parent := filepath.Dir(abs)
	if !(os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR)) || parent == abs {
		return "", err
	}
	realParent, err := realPath(parent, links)
	if err != nil {
		return "", err
	}
	name := filepath.Join(realParent, filepath.Base(abs))
	link, err := os.Readlink(name)
	if err != nil {
		return name, nil
	}
	if links >= maxDanglingLinks {
		return "", fmt.Errorf("symlink %s: too many levels of symbolic links", name)
	}
	if !filepath.IsAbs(link) {
		link = filepath.Join(realParent, link)
	}
	return realPath(link, links+1)
}

// Within reports whether path is dir or below it.
func Within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
