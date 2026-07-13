package localbundle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectLocalBundle_Directory(t *testing.T) {
	bundleDir := buildTestBundle(t)

	info, err := DetectLocalBundle(bundleDir)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected a detected bundle")
	}
	if info.SourceDir != bundleDir {
		t.Errorf("SourceDir = %s, want %s", info.SourceDir, bundleDir)
	}
	if info.TempDir != "" {
		t.Errorf("TempDir = %q, want empty for a directory bundle", info.TempDir)
	}
	if !info.HasLockfile || !info.HasPackMeta {
		t.Errorf("HasLockfile=%v HasPackMeta=%v, want both true", info.HasLockfile, info.HasPackMeta)
	}
	if len(info.PackMeta.BundleFiles) == 0 {
		t.Error("expected a non-empty bundle_files manifest")
	}
	if len(info.PackTargets) != 1 || info.PackTargets[0] != "claude" {
		t.Errorf("PackTargets = %v, want [claude]", info.PackTargets)
	}
}

// TestDetectLocalBundle_DirectoryWithoutPluginJSON_ReturnsNil covers the
// "looks like a local dependency (has files, has apm.yml) but has no
// plugin.json" case: DetectLocalBundle must return (nil, nil) so the
// caller falls through to the ordinary local-path dependency install,
// never mistaking one for the other.
func TestDetectLocalBundle_DirectoryWithoutPluginJSON_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "apm.yml"), "name: x\nversion: \"1.0.0\"\n")

	info, err := DetectLocalBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Errorf("expected nil for a directory without plugin.json, got %+v", info)
	}
}

func TestDetectLocalBundle_NonexistentPath_ReturnsNil(t *testing.T) {
	info, err := DetectLocalBundle(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Error("expected nil for a nonexistent path")
	}
}

func TestDetectLocalBundle_Zip(t *testing.T) {
	bundleDir := buildTestBundle(t)
	zipPath := zipDir(t, bundleDir)

	info, err := DetectLocalBundle(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected a detected bundle from a .zip archive")
	}
	defer info.Cleanup()

	if info.TempDir == "" {
		t.Error("expected a non-empty TempDir for an archive bundle")
	}
	if !info.HasPackMeta {
		t.Error("expected pack metadata to be present")
	}
	if _, statErr := os.Stat(filepath.Join(info.SourceDir, "plugin.json")); statErr != nil {
		t.Errorf("expected plugin.json in the extracted bundle: %v", statErr)
	}
	if _, statErr := os.Stat(info.TempDir); statErr != nil {
		t.Fatalf("TempDir should exist before Cleanup: %v", statErr)
	}
}

func TestDetectLocalBundle_TarGz(t *testing.T) {
	bundleDir := buildTestBundle(t)
	tarGzPath := tarGzDir(t, bundleDir)

	info, err := DetectLocalBundle(tarGzPath)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected a detected bundle from a .tar.gz archive")
	}
	defer info.Cleanup()

	if _, statErr := os.Stat(filepath.Join(info.SourceDir, "plugin.json")); statErr != nil {
		t.Errorf("expected plugin.json in the extracted bundle: %v", statErr)
	}
}

func TestDetectLocalBundle_Cleanup_RemovesTempDir(t *testing.T) {
	bundleDir := buildTestBundle(t)
	zipPath := zipDir(t, bundleDir)

	info, err := DetectLocalBundle(zipPath)
	if err != nil || info == nil {
		t.Fatalf("detect failed: info=%v err=%v", info, err)
	}
	tempDir := info.TempDir
	info.Cleanup()
	if _, statErr := os.Stat(tempDir); !os.IsNotExist(statErr) {
		t.Errorf("expected TempDir %s to be removed after Cleanup", tempDir)
	}
}

func TestDetectLocalBundle_ArchiveWithoutPluginJSON_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "README.md"), "not a bundle")
	zipPath := zipDir(t, dir)

	info, err := DetectLocalBundle(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		info.Cleanup()
		t.Error("expected nil for an archive with no plugin.json")
	}
}

func TestDetectLocalBundle_CorruptArchive_Errors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bogus.zip")
	if err := os.WriteFile(path, []byte("not a zip file"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := DetectLocalBundle(path)
	if err == nil {
		t.Fatal("expected an error for a corrupt .zip archive")
	}
	if info != nil {
		t.Error("expected nil info alongside the error")
	}
}
