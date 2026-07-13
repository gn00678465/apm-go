package archive

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// SafeExtractZip reads the zip archive at archivePath, enforces lim, and
// extracts into a fresh staging directory which is renamed to dest on
// success -- the same staged-extraction-then-rename pattern as SafeExtract
// (tar.gz), so on ANY error the staging directory is removed and dest is
// left untouched. Applies the identical security posture (req-sc-002/004):
// an entry-count cap, a running uncompressed-bytes cap enforced against
// bytes actually copied (not the archive's own, attacker-controlled
// UncompressedSize64 metadata), absolute-path/".." traversal rejection, and
// symlink rejection (detected via the Unix permission bits zip stores in
// each entry's external file attributes -- mirrors Python's
// safe_extract_zip: "(external_attr >> 16) & 0xF000 == 0xA000").
//
// Needed because SafeExtract (extract.go) deliberately REJECTS a zip
// container outright ("rejected application/zip container; v0.1 requires
// application/gzip") -- that guard exists for the registry tar.gz-only
// download path and must stay as-is; this is a separate entry point for
// local-bundle install's ".zip" shape (research/pack-parity-findings.md §6),
// which Python's own local-bundle installer supports alongside tar.gz.
func SafeExtractZip(archivePath, dest string, lim Limits) (*Extracted, error) {
	lim = lim.normalize()

	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("archive: open zip: %w", err)
	}
	defer zr.Close()

	if len(zr.File) > lim.MaxEntries {
		return nil, fmt.Errorf("archive: entry count exceeds limit of %d", lim.MaxEntries)
	}

	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("archive: prepare dest: %w", err)
	}
	stage := dest + ".apmtmp"
	os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return nil, fmt.Errorf("archive: prepare staging: %w", err)
	}

	files, err := extractZipInto(zr, stage, lim)
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

func extractZipInto(zr *zip.ReadCloser, stage string, lim Limits) ([]string, error) {
	var files []string
	var total int64

	for _, zf := range zr.File {
		// Link guard FIRST (req-sc-002), mirroring SafeExtract's tar.gz
		// ordering: a symlink entry (recorded via the Unix mode bits in the
		// upper 16 bits of ExternalAttrs -- archive/zip's FileHeader.Mode()
		// only honors those bits when the entry's CreatorVersion indicates a
		// Unix-family creator, matching Python's own check) is rejected
		// outright, even if its name also happens to contain "..".
		if zf.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("archive: entry %q is a symbolic link; links are rejected", zf.Name)
		}

		// Path guard (req-sc-002) -- identical logic to SafeExtract's tar.gz
		// path.
		clean := path.Clean(zf.Name)
		if path.IsAbs(clean) || filepath.IsAbs(zf.Name) || hasVolume(zf.Name) {
			return nil, fmt.Errorf("archive: entry %q has an absolute path; rejected", zf.Name)
		}
		if clean == ".." || strings.HasPrefix(clean, "../") || containsDotDot(clean) {
			return nil, fmt.Errorf("archive: entry %q escapes extraction root (contains \"..\"); rejected", zf.Name)
		}
		target := filepath.Join(stage, filepath.FromSlash(clean))
		if !withinRoot(stage, target) {
			return nil, fmt.Errorf("archive: entry %q escapes extraction root (contains \"..\"); rejected", zf.Name)
		}

		if zf.FileInfo().IsDir() || strings.HasSuffix(zf.Name, "/") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return nil, fmt.Errorf("archive: mkdir %q: %w", clean, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, fmt.Errorf("archive: mkdir parent %q: %w", clean, err)
		}
		n, err := copyCappedZipEntry(target, zf, lim.MaxBytes-total)
		if err != nil {
			return nil, err
		}
		total += n
		if total > lim.MaxBytes {
			return nil, fmt.Errorf("archive: uncompressed size exceeds limit of %d bytes", lim.MaxBytes)
		}
		files = append(files, clean)
	}
	return files, nil
}

// copyCappedZipEntry mirrors copyCapped (extract.go), reading from a zip
// entry's own reader instead of a shared tar.Reader stream.
func copyCappedZipEntry(target string, zf *zip.File, remaining int64) (int64, error) {
	rc, err := zf.Open()
	if err != nil {
		return 0, fmt.Errorf("archive: open entry %q: %w", zf.Name, err)
	}
	defer rc.Close()

	f, err := os.Create(target)
	if err != nil {
		return 0, fmt.Errorf("archive: create %q: %w", target, err)
	}
	defer f.Close()
	if remaining < 0 {
		remaining = 0
	}
	n, err := io.Copy(f, io.LimitReader(rc, remaining+1))
	if err != nil {
		return n, fmt.Errorf("archive: write %q: %w", target, err)
	}
	return n, nil
}
