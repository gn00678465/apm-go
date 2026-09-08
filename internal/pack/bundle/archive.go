package bundle

import (
	"archive/tar"
	"archive/zip"
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/apm-go/apm/internal/archive"
	"github.com/apm-go/apm/internal/rootfs"
)

// SupportedArchiveFormats mirrors utils/archive.py's
// SUPPORTED_ARCHIVE_FORMATS (ticket 17 phase 2). apm-go's own Choice-flag
// validation (cmd/apm-go/pack.go's --archive-format) already rejects
// anything else before Produce is ever called; this list exists so
// writeArchive's format switch has one place to fall back to an explicit
// error instead of silently defaulting, mirroring validate_archive_format's
// own defense-in-depth (never actually user-reachable through the CLI,
// same conclusion the Oracle's own code reaches -- Click's Choice always
// validates first).
var SupportedArchiveFormats = map[string]bool{"zip": true, "tar.gz": true}

// archiveEntry is one regular file to add to an archive: its real
// filesystem path, its POSIX-style name inside the archive, and the
// os.FileInfo used to preserve permission bits.
type archiveEntry struct {
	rel     string
	arcName string
	info    fs.FileInfo
}

// collectArchiveEntries walks bundleDir the same way write_zip_archive/
// write_tar_archive do (utils/archive.py): sorted(bundle_dir.rglob("*")),
// skipping symlinks and anything that is not a regular file -- so real
// subdirectories get no entry of their own; both zip and tar readers infer
// directories from their members' path prefixes. Every arcName is prefixed
// with bundleDir's own base name, matching
// f"{bundle_dir.name}/{fp.relative_to(bundle_dir).as_posix()}".
//
// Each computed name is re-validated via archive.ValidatedRelPath -- the
// SAME invariant internal/archive's extraction side enforces on read
// (extract.go/zip.go) -- as a defense-in-depth check: a walk over a
// directory apm-go itself just built should never produce a hostile name,
// but the write side must not simply assume that (ticket 17 phase 2's
// explicit instruction: mirror the existing invariant set, don't invent a
// second one).
// The walk goes through bundleRW's directory handle rather than the bundle's
// path string. The result becomes the archive's contents, so a link planted
// mid-walk must not be able to get an outside file packed in -- the same
// reason the lockfile's integrity walk uses the handle.
func collectArchiveEntries(bundleRW *rootfs.RootWriter, base string) ([]archiveEntry, error) {
	var entries []archiveEntry
	err := fs.WalkDir(bundleRW.FS(), ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || d.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		relSlash := p
		if _, err := archive.ValidatedRelPath(relSlash); err != nil {
			return err
		}
		entries = append(entries, archiveEntry{
			rel:     relSlash,
			arcName: base + "/" + relSlash,
			info:    info,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].arcName < entries[j].arcName })
	return entries, nil
}

// writeArchive dispatches to writeZipArchive/writeTarGzArchive, mirroring
// plugin_exporter.py's own dispatch (~lines 1055-1061): "tar.gz" ->
// write_tar_archive, anything else -> write_zip_archive (only reachable
// with "zip" here, since the CLI's own Choice flag already restricts the
// value).
func writeArchive(bundleRW, outRW *rootfs.RootWriter, base, archiveRel, format string) error {
	switch format {
	case "tar.gz":
		return writeTarGzArchive(bundleRW, outRW, base, archiveRel)
	case "zip":
		return writeZipArchive(bundleRW, outRW, base, archiveRel)
	default:
		return fmt.Errorf("archive: unknown archive_format %q; must be 'zip' or 'tar.gz'", format)
	}
}

// writeZipArchive mirrors write_zip_archive (utils/archive.py): a
// ZIP_DEFLATED archive at compresslevel=9 (Go's flate.BestCompression is
// the same numeric level), one entry per regular file in bundleDir
// (symlinks and directories excluded), each entry's name prefixed with
// bundleDir's own base name, and each entry's permission bits preserved via
// zip.FileInfoHeader (mirrors Python zipfile.ZipFile.write's default
// behavior of copying the source file's os.stat() mode when no explicit
// ZipInfo is given).
func writeZipArchive(bundleRW, outRW *rootfs.RootWriter, base, archiveRel string) error {
	entries, err := collectArchiveEntries(bundleRW, base)
	if err != nil {
		return err
	}

	// Atomic through outRW's handle: os.Create on a path string both leaves
	// the boundary and truncates in place, so a hard link planted at the
	// archive name would have its other name destroyed before a byte of the
	// archive was written.
	f, err := outRW.CreateAtomic(archiveRel)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.BestCompression)
	})

	for _, e := range entries {
		hdr, err := zip.FileInfoHeader(e.info)
		if err != nil {
			return fmt.Errorf("archive: header for %q: %w", e.arcName, err)
		}
		hdr.Name = e.arcName
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return fmt.Errorf("archive: add %q: %w", e.arcName, err)
		}
		if err := copyEntryContent(w, bundleRW, e.rel); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("archive: finalize %q: %w", outRW.Path(archiveRel), err)
	}
	return f.Commit()
}

// writeTarGzArchive mirrors write_tar_archive (utils/archive.py): a
// "w:gz" tarball, same entry set/naming/skip rules as writeZipArchive, each
// entry's permission bits preserved via tar.FileInfoHeader (mirrors
// tarfile.add's default mode-preservation behavior).
func writeTarGzArchive(bundleRW, outRW *rootfs.RootWriter, base, archiveRel string) error {
	entries, err := collectArchiveEntries(bundleRW, base)
	if err != nil {
		return err
	}

	// Atomic through outRW's handle: os.Create on a path string both leaves
	// the boundary and truncates in place, so a hard link planted at the
	// archive name would have its other name destroyed before a byte of the
	// archive was written.
	f, err := outRW.CreateAtomic(archiveRel)
	if err != nil {
		return err
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	for _, e := range entries {
		hdr, err := tar.FileInfoHeader(e.info, "")
		if err != nil {
			return fmt.Errorf("archive: header for %q: %w", e.arcName, err)
		}
		hdr.Name = e.arcName
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("archive: add %q: %w", e.arcName, err)
		}
		if err := copyEntryContent(tw, bundleRW, e.rel); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("archive: finalize tar stream for %q: %w", outRW.Path(archiveRel), err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("archive: finalize gzip stream for %q: %w", outRW.Path(archiveRel), err)
	}
	return f.Commit()
}

// copyEntryContent streams one file's content into an already-created archive
// entry writer, opening it through the bundle's own directory handle. Re-opens
// by name rather than reusing a descriptor captured during the walk, since
// collectArchiveEntries only records fs.FileInfo.
func copyEntryContent(w io.Writer, bundleRW *rootfs.RootWriter, rel string) error {
	src, err := bundleRW.FS().Open(rel)
	if err != nil {
		return fmt.Errorf("archive: open %q: %w", bundleRW.Path(rel), err)
	}
	defer src.Close()
	if _, err := io.Copy(w, src); err != nil {
		return fmt.Errorf("archive: write %q: %w", bundleRW.Path(rel), err)
	}
	return nil
}

// archiveRelName is projectedArchivePath's boundary-relative form: the same
// "<bundleName><suffix>" leaf, without the OutputDir prefix, for callers that
// address the file through a handle instead of a path string.
func archiveRelName(bundleName, format string) string {
	if format == "tar.gz" {
		return bundleName + ".tar.gz"
	}
	return bundleName + ".zip"
}

// projectedArchivePath mirrors projected_archive_path (utils/archive.py):
// outputDir / f"{bundleName}{suffix}" where suffix is ".zip" or ".tar.gz" --
// confirming -o/--output is the DIRECTORY the archive is placed into, not
// the archive file's own path.
func projectedArchivePath(outputDir, bundleName, format string) string {
	suffix := ".zip"
	if format == "tar.gz" {
		suffix = ".tar.gz"
	}
	return filepath.Join(outputDir, bundleName+suffix)
}
