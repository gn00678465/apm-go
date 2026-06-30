// Package archive provides safe extraction of registry tar.gz archives,
// enforcing the OpenAPM v0.1 security controls req-sc-002 (path traversal /
// symlink escape) and req-sc-004 (container type, size and entry-count caps).
package archive

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Default resource caps (req-sc-004).
const (
	DefaultMaxBytes   int64 = 100 << 20 // 100 MB uncompressed
	DefaultMaxEntries int   = 10000
)

// Limits bounds extraction. Zero fields take the spec defaults.
type Limits struct {
	MaxBytes   int64 // uncompressed total
	MaxEntries int
}

func (l Limits) normalize() Limits {
	if l.MaxBytes <= 0 {
		l.MaxBytes = DefaultMaxBytes
	}
	if l.MaxEntries <= 0 {
		l.MaxEntries = DefaultMaxEntries
	}
	return l
}

// Extracted reports what SafeExtract wrote (relative paths, slash-separated).
type Extracted struct {
	Files []string
}

// SafeExtract reads a gzip(tar) stream from r, enforces lim, and extracts into
// a fresh staging directory which is renamed to dest on success. On ANY error
// the staging directory is removed so no partial extraction is left behind
// (req-sc-002 cleanup). The required diagnostic substrings are kept literal:
// ".." for path traversal, "link" for sym/hard links, "application/zip" for a
// wrong container.
func SafeExtract(r io.Reader, dest string, lim Limits) (*Extracted, error) {
	lim = lim.normalize()

	// Container check (req-sc-004a): peek the magic without consuming it.
	br := bufio.NewReader(r)
	magic, _ := br.Peek(2)
	if len(magic) >= 2 {
		switch {
		case magic[0] == 0x50 && magic[1] == 0x4b: // "PK"
			return nil, fmt.Errorf("archive: rejected application/zip container; v0.1 requires application/gzip (tar.gz)")
		case magic[0] == 0x1f && magic[1] == 0x8b: // gzip
			// ok
		default:
			return nil, fmt.Errorf("archive: unrecognized container; v0.1 requires application/gzip (tar.gz)")
		}
	}

	gz, err := gzip.NewReader(br)
	if err != nil {
		return nil, fmt.Errorf("archive: gzip: %w", err)
	}
	defer gz.Close()

	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("archive: prepare dest: %w", err)
	}
	stage := dest + ".apmtmp"
	os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return nil, fmt.Errorf("archive: prepare staging: %w", err)
	}

	files, err := extractInto(tar.NewReader(gz), stage, lim)
	if err != nil {
		os.RemoveAll(stage) // partial-extraction cleanup (req-sc-002)
		return nil, err
	}

	os.RemoveAll(dest)
	if err := os.Rename(stage, dest); err != nil {
		os.RemoveAll(stage)
		return nil, fmt.Errorf("archive: commit: %w", err)
	}
	return &Extracted{Files: files}, nil
}

func extractInto(tr *tar.Reader, stage string, lim Limits) ([]string, error) {
	var files []string
	var total int64
	entries := 0

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("archive: read entry: %w", err)
		}

		entries++
		if entries > lim.MaxEntries {
			return nil, fmt.Errorf("archive: entry count exceeds limit of %d", lim.MaxEntries)
		}

		// Link guard FIRST (req-sc-002): a symlink/hardlink entry whose name
		// itself contains ".." must still report "link", not "..".
		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
			return nil, fmt.Errorf("archive: entry %q is a symbolic or hard link; links are rejected", hdr.Name)
		}

		// Path guard (req-sc-002).
		clean := path.Clean(hdr.Name)
		if path.IsAbs(clean) || filepath.IsAbs(hdr.Name) || hasVolume(hdr.Name) {
			return nil, fmt.Errorf("archive: entry %q has an absolute path; rejected", hdr.Name)
		}
		if clean == ".." || strings.HasPrefix(clean, "../") || containsDotDot(clean) {
			return nil, fmt.Errorf("archive: entry %q escapes extraction root (contains \"..\"); rejected", hdr.Name)
		}
		target := filepath.Join(stage, filepath.FromSlash(clean))
		if !withinRoot(stage, target) {
			return nil, fmt.Errorf("archive: entry %q escapes extraction root (contains \"..\"); rejected", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return nil, fmt.Errorf("archive: mkdir %q: %w", clean, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return nil, fmt.Errorf("archive: mkdir parent %q: %w", clean, err)
			}
			n, err := copyCapped(target, tr, lim.MaxBytes-total)
			if err != nil {
				return nil, err
			}
			total += n
			if total > lim.MaxBytes {
				return nil, fmt.Errorf("archive: uncompressed size exceeds limit of %d bytes", lim.MaxBytes)
			}
			files = append(files, clean)
		default:
			return nil, fmt.Errorf("archive: entry %q has unsupported type %v; rejected", hdr.Name, hdr.Typeflag)
		}
	}
	return files, nil
}

// copyCapped writes at most remaining+1 bytes from tr into target so the caller
// can detect an over-limit archive (anti gzip-bomb). Returns bytes written.
func copyCapped(target string, tr io.Reader, remaining int64) (int64, error) {
	f, err := os.Create(target)
	if err != nil {
		return 0, fmt.Errorf("archive: create %q: %w", target, err)
	}
	defer f.Close()
	if remaining < 0 {
		remaining = 0
	}
	n, err := io.Copy(f, io.LimitReader(tr, remaining+1))
	if err != nil {
		return n, fmt.Errorf("archive: write %q: %w", target, err)
	}
	return n, nil
}

func containsDotDot(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

func hasVolume(p string) bool {
	return filepath.VolumeName(filepath.FromSlash(p)) != ""
}

func withinRoot(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if target == root {
		return true
	}
	return strings.HasPrefix(target, root+string(os.PathSeparator))
}

// Contained reports whether target resolves inside root (after cleaning and
// absolutizing). Callers use it as a defense-in-depth guard before choosing an
// extraction destination derived from untrusted input (e.g. a lockfile repo_url).
func Contained(root, target string) bool {
	absRoot, err1 := filepath.Abs(root)
	absTarget, err2 := filepath.Abs(target)
	if err1 != nil || err2 != nil {
		return false
	}
	return withinRoot(absRoot, absTarget)
}
