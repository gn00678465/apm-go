package archive

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// SymlinkError is a symlink in a package that a reader must not follow: it
// resolves outside the package root, does not resolve, or loops.
type SymlinkError struct {
	// Path is the symlink as the package names it: relative to the package
	// root, slash-separated, the components before it not resolved.
	Path string
	// Target is where it resolves to; "" when it does not resolve.
	Target string
	// Link is the symlink's own text, set when it does not resolve.
	Link string
	// Err is why it does not resolve.
	Err  error
	Loop bool
}

func (e *SymlinkError) Error() string {
	switch {
	case e.Loop && e.Target != "":
		return fmt.Sprintf("symlink %s resolves to %s, a directory it is in (symlink loop)", e.Path, e.Target)
	case e.Loop:
		return fmt.Sprintf("symlink %s is a symlink loop", e.Path)
	case e.Err != nil:
		return fmt.Sprintf("symlink %s -> %s does not resolve: %v", e.Path, e.Link, e.Err)
	default:
		return fmt.Sprintf("symlink %s resolves to %s, outside the package", e.Path, e.Target)
	}
}

// ResolveIn returns rel, a slash-separated path below root, with every symlink
// on it resolved, still relative to root. root must have no symlinks in it. A
// symlink that leaves root, dangles or loops is a *SymlinkError. A component
// that cannot be read ends the resolution with the rest of rel unresolved, so
// the caller's own read of the result fails as a read of rel would.
func ResolveIn(root, rel string) (string, error) {
	cur, named := root, ""
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return "", fmt.Errorf("path %s leaves the package", rel)
		}
		named = path.Join(named, part)
		next := filepath.Join(cur, part)
		info, err := os.Lstat(next)
		if err != nil {
			cur = filepath.Join(next, filepath.FromSlash(strings.Join(parts[i+1:], "/")))
			break
		}
		if info.Mode()&os.ModeSymlink == 0 {
			cur = next
			continue
		}
		if cur, err = resolveLink(root, next, named); err != nil {
			return "", err
		}
	}
	out, err := filepath.Rel(root, cur)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(out), nil
}

// CheckTree reports the first symlink below rel, in name order, that a copy
// reading symlinks as what they point at must not follow: one ResolveIn
// refuses, or a directory symlink back to a directory the copy is already in
// or has entered through another symlink. A copy that follows every symlink of
// a tree CheckTree passed stays inside root and ends.
func CheckTree(root, rel string) error {
	resolved, err := ResolveIn(root, rel)
	if err != nil {
		return err
	}
	return checkTree(root, filepath.Join(root, filepath.FromSlash(resolved)), path.Clean(rel), nil)
}

func checkTree(root, dir, named string, entered []string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		p, n := filepath.Join(dir, e.Name()), path.Join(named, e.Name())
		if e.Type()&os.ModeSymlink == 0 {
			if e.IsDir() {
				if err := checkTree(root, p, n, entered); err != nil {
					return err
				}
			}
			continue
		}
		target, err := resolveLink(root, p, n)
		if err != nil {
			return err
		}
		if info, err := os.Stat(target); err != nil || !info.IsDir() {
			continue
		}
		if withinRoot(target, dir) || slices.Contains(entered, target) {
			return &SymlinkError{Path: n, Target: target, Loop: true}
		}
		if err := checkTree(root, target, n, append(slices.Clip(entered), target)); err != nil {
			return err
		}
	}
	return nil
}

// resolveLink resolves the symlink at link, named named in the package.
func resolveLink(root, link, named string) (string, error) {
	target, err := filepath.EvalSymlinks(link)
	if err != nil {
		if _, statErr := os.Stat(link); errors.Is(statErr, syscall.ELOOP) {
			return "", &SymlinkError{Path: named, Loop: true}
		}
		text, _ := os.Readlink(link)
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			err = pathErr.Err
		}
		return "", &SymlinkError{Path: named, Link: text, Err: err}
	}
	if !withinRoot(root, target) {
		return "", &SymlinkError{Path: named, Target: target}
	}
	return target, nil
}
