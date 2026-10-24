package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
)

// ComputePackageHash is the Oracle's compute_package_hash
// (utils/content_hash.py:25-86), the content_hash of a dependency's lock
// entry: SHA-256 over each regular file of the package directory dir, in the
// order of its slash-separated path, the path's bytes and then the file's,
// leaving out every path with a .git or __pycache__ component and a root
// .apm-pin. A package with no such file hashes as the empty string.
//
// The Oracle hashes a tree it copied with every symlink resolved, so a
// symlink here counts as the file or directory it points at, below the
// symlink's own path; a symlink loop is entered once. The install has
// refused a symlink that leaves the package before the lock is written.
func ComputePackageHash(dir string) (string, error) {
	files := map[string]string{}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "sha256:" + hex.EncodeToString(sha256.New().Sum(nil)), nil
	}
	if err := collectPackageFiles(dir, "", map[string]bool{}, files); err != nil {
		return "", err
	}
	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	h := sha256.New()
	for _, rel := range rels {
		data, err := os.ReadFile(files[rel])
		if err != nil {
			return "", err
		}
		h.Write([]byte(rel))
		h.Write(data)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// collectPackageFiles adds to files each regular file below dir, keyed by its
// path below the package root, prefix being dir's own path there. entered
// holds the real directories on the way down, so a loop stops.
func collectPackageFiles(dir, prefix string, entered map[string]bool, files map[string]string) error {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	if entered[real] {
		return nil
	}
	entered[real] = true
	defer delete(entered, real)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if name == ".git" || name == "__pycache__" {
			continue
		}
		p := filepath.Join(dir, name)
		rel := path.Join(prefix, name)
		mode := e.Type()
		if mode&fs.ModeSymlink != 0 {
			info, err := os.Stat(p)
			if err != nil {
				continue
			}
			mode = info.Mode().Type()
		}
		switch {
		case mode.IsDir():
			if err := collectPackageFiles(p, rel, entered, files); err != nil {
				return err
			}
		case mode.IsRegular() && rel != ".apm-pin":
			files[rel] = p
		}
	}
	return nil
}
