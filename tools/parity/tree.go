//go:build unix

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// TreeEntry is one path under a run's cwd or APM_CONFIG_DIR, before or after
// normalisation. Kind "deleted" marks a fixture path that existed before the
// run and is gone afterward; it carries no Size/SHA256.
type TreeEntry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"` // file | dir | symlink | deleted
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

// walkTree walks root and returns a path-sorted listing of everything under
// it, each path prefixed "label/" so entries from cwd and APM_CONFIG_DIR
// stay unambiguous once merged into one tree. root itself is not listed.
func walkTree(root, label string) ([]TreeEntry, error) {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}

	var entries []TreeEntry
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		path := label + "/" + filepath.ToSlash(rel)

		switch {
		case d.Type()&fs.ModeSymlink != 0:
			info, err := d.Info()
			if err != nil {
				return err
			}
			entries = append(entries, TreeEntry{Path: path, Kind: "symlink", Size: info.Size()})
		case d.IsDir():
			entries = append(entries, TreeEntry{Path: path, Kind: "dir"})
		default:
			info, err := d.Info()
			if err != nil {
				return err
			}
			sum, err := sha256File(p)
			if err != nil {
				return err
			}
			entries = append(entries, TreeEntry{Path: path, Kind: "file", Size: info.Size(), SHA256: sum})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", root, err)
	}

	sortTreeEntries(entries)
	return entries, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sortTreeEntries(entries []TreeEntry) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
}

// diffDeleted compares a pre-run snapshot of file/symlink paths against the
// set of paths present after the run, and returns a "deleted" entry for each
// one the run removed. Directories are excluded: a removed directory shows
// up as its files disappearing, and reporting the directory itself too would
// be redundant.
func diffDeleted(before []TreeEntry, afterPaths map[string]bool) []TreeEntry {
	var deleted []TreeEntry
	for _, e := range before {
		if e.Kind == "dir" {
			continue
		}
		if !afterPaths[e.Path] {
			deleted = append(deleted, TreeEntry{Path: e.Path, Kind: "deleted"})
		}
	}
	return deleted
}
