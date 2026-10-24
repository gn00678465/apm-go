package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
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
// .apm-pin. A package with no such file, or no directory, hashes as the empty
// string.
//
// A symlink is left out, whether it points at a file or a directory, and a
// symlinked directory is not entered, as the Oracle's rglob skips each
// is_symlink() entry and Python 3.12's rglob does not follow one
// (content_hash.py:59-66).
func ComputePackageHash(dir string) (string, error) {
	h := sha256.New()
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
	}
	files := map[string]string{}
	if err := collectPackageFiles(dir, "", files); err != nil {
		return "", err
	}
	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
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
// path below the package root, prefix being dir's own path there.
func collectPackageFiles(dir, prefix string, files map[string]string) error {
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
		switch mode := e.Type(); {
		case mode.IsDir():
			if err := collectPackageFiles(p, rel, files); err != nil {
				return err
			}
		case mode.IsRegular() && rel != ".apm-pin":
			files[rel] = p
		}
	}
	return nil
}
