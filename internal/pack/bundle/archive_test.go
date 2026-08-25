package bundle

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeBundleFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// TestWriteZipArchive_EntriesPrefixedWithBundleName mirrors write_zip_archive
// (utils/archive.py): every entry's name is prefixed with the bundle
// directory's own base name.
func TestWriteZipArchive_EntriesPrefixedWithBundleName(t *testing.T) {
	dir := t.TempDir()
	bundleDir := filepath.Join(dir, "demo-1.0.0")
	writeBundleFile(t, filepath.Join(bundleDir, "plugin.json"), "{}", 0o644)
	writeBundleFile(t, filepath.Join(bundleDir, "skills", "hello", "SKILL.md"), "hello", 0o644)

	archivePath := filepath.Join(dir, "demo-1.0.0.zip")
	if err := writeZipArchive(bundleDir, archivePath); err != nil {
		t.Fatalf("writeZipArchive: %v", err)
	}

	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open produced zip: %v", err)
	}
	defer zr.Close()

	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"demo-1.0.0/plugin.json", "demo-1.0.0/skills/hello/SKILL.md"} {
		if !names[want] {
			t.Errorf("zip entries = %v, want %q", names, want)
		}
	}
}

// TestWriteZipArchive_SkipsSymlinks proves a symlink inside bundleDir
// (whether it points inside or outside the bundle) is never followed into
// the archive -- mirroring write_zip_archive's "if fp.is_symlink(): continue".
func TestWriteZipArchive_SkipsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on windows")
	}
	dir := t.TempDir()
	bundleDir := filepath.Join(dir, "demo-1.0.0")
	writeBundleFile(t, filepath.Join(bundleDir, "plugin.json"), "{}", 0o644)

	outsideSecret := filepath.Join(dir, "outside-secret.txt")
	if err := os.WriteFile(outsideSecret, []byte("do not leak"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideSecret, filepath.Join(bundleDir, "leak.txt")); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(dir, "demo-1.0.0.zip")
	if err := writeZipArchive(bundleDir, archivePath); err != nil {
		t.Fatalf("writeZipArchive: %v", err)
	}

	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open produced zip: %v", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == "demo-1.0.0/leak.txt" {
			t.Fatalf("archive must not contain a symlink entry, found %q", f.Name)
		}
	}
}

// TestWriteZipArchive_PreservesPermissionBits mirrors zipfile.ZipFile.write's
// default behavior of copying the source file's own os.stat() mode.
func TestWriteZipArchive_PreservesPermissionBits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("exact permission bits are not meaningful on windows")
	}
	dir := t.TempDir()
	bundleDir := filepath.Join(dir, "demo-1.0.0")
	writeBundleFile(t, filepath.Join(bundleDir, "run.sh"), "#!/bin/sh\n", 0o755)

	archivePath := filepath.Join(dir, "demo-1.0.0.zip")
	if err := writeZipArchive(bundleDir, archivePath); err != nil {
		t.Fatalf("writeZipArchive: %v", err)
	}

	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open produced zip: %v", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "demo-1.0.0/run.sh" {
			continue
		}
		if mode := f.Mode().Perm(); mode != 0o755 {
			t.Errorf("run.sh mode = %v, want 0755", mode)
		}
		return
	}
	t.Fatal("run.sh entry not found in archive")
}

// TestWriteTarGzArchive_EntriesAndSymlinkExclusion mirrors
// TestWriteZipArchive_EntriesPrefixedWithBundleName/SkipsSymlinks for the
// tar.gz format.
func TestWriteTarGzArchive_EntriesAndSymlinkExclusion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on windows")
	}
	dir := t.TempDir()
	bundleDir := filepath.Join(dir, "demo-1.0.0")
	writeBundleFile(t, filepath.Join(bundleDir, "plugin.json"), "{}", 0o644)

	outsideSecret := filepath.Join(dir, "outside-secret.txt")
	if err := os.WriteFile(outsideSecret, []byte("do not leak"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideSecret, filepath.Join(bundleDir, "leak.txt")); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(dir, "demo-1.0.0.tar.gz")
	if err := writeTarGzArchive(bundleDir, archivePath); err != nil {
		t.Fatalf("writeTarGzArchive: %v", err)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open produced tar.gz: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	found := false
	for {
		hdr, terr := tr.Next()
		if terr == io.EOF {
			break
		}
		if terr != nil {
			t.Fatalf("tar read: %v", terr)
		}
		if hdr.Name == "demo-1.0.0/leak.txt" {
			t.Fatalf("archive must not contain a symlink entry, found %q", hdr.Name)
		}
		if hdr.Name == "demo-1.0.0/plugin.json" {
			found = true
		}
	}
	if !found {
		t.Fatal("demo-1.0.0/plugin.json entry not found in tar.gz")
	}
}

// TestProjectedArchivePath mirrors projected_archive_path (utils/archive.py):
// outputDir joined with "<bundleName><suffix>", suffix depending on format.
func TestProjectedArchivePath(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{"zip", filepath.Join("out", "demo-1.0.0.zip")},
		{"tar.gz", filepath.Join("out", "demo-1.0.0.tar.gz")},
	}
	for _, tt := range tests {
		if got := projectedArchivePath("out", "demo-1.0.0", tt.format); got != tt.want {
			t.Errorf("projectedArchivePath(%q) = %q, want %q", tt.format, got, tt.want)
		}
	}
}
