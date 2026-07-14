package archive

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// buildZip writes a zip archive with the given (name, content) entries to a
// temp file and returns its path. A content of "" for a name ending in "/"
// writes a directory entry.
func buildZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "test.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// buildZipWithSymlink writes a zip archive containing one regular entry and
// one symlink entry (Unix mode bits set in the external file attributes),
// mirroring how a genuine Unix zip tool encodes a symlink.
func buildZipWithSymlink(t *testing.T, linkName, linkTarget string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	hdr := &zip.FileHeader{Name: linkName, Method: zip.Store}
	hdr.SetMode(os.ModeSymlink | 0777)
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(linkTarget)); err != nil {
		t.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "test.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSafeExtractZip_HappyPath(t *testing.T) {
	src := buildZip(t, map[string]string{
		"plugin.json":         `{"name":"demo"}`,
		"agents/foo.md":       "agent content",
		"skills/bar/SKILL.md": "skill content",
	})
	dest := filepath.Join(t.TempDir(), "out")
	extracted, err := SafeExtractZip(src, dest, Limits{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(extracted.Files) != 3 {
		t.Errorf("Files = %v, want 3 entries", extracted.Files)
	}
	data, rerr := os.ReadFile(filepath.Join(dest, "plugin.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(data) != `{"name":"demo"}` {
		t.Errorf("plugin.json content = %q", data)
	}
	if _, err := os.Stat(filepath.Join(dest, "skills", "bar", "SKILL.md")); err != nil {
		t.Errorf("expected nested file to be extracted: %v", err)
	}
}

func TestSafeExtractZip_PathTraversal_FailsClosed(t *testing.T) {
	src := buildZip(t, map[string]string{
		"../../etc/passwd": "pwned",
	})
	dest := filepath.Join(t.TempDir(), "out")
	_, err := SafeExtractZip(src, dest, Limits{})
	if err == nil {
		t.Fatal("expected fail-closed on path traversal, got nil")
	}
	noLeak(t, dest)
}

func TestSafeExtractZip_Symlink_Rejected(t *testing.T) {
	src := buildZipWithSymlink(t, "evil-link", "/etc/passwd")
	dest := filepath.Join(t.TempDir(), "out")
	_, err := SafeExtractZip(src, dest, Limits{})
	if err == nil {
		t.Fatal("expected fail-closed on symlink entry, got nil")
	}
	noLeak(t, dest)
}

func TestSafeExtractZip_EntryCountExceeded(t *testing.T) {
	entries := map[string]string{}
	for i := 0; i < 5; i++ {
		entries[filepath.Base(t.TempDir())+string(rune('a'+i))+".txt"] = "x"
	}
	src := buildZip(t, entries)
	dest := filepath.Join(t.TempDir(), "out")
	_, err := SafeExtractZip(src, dest, Limits{MaxEntries: 2})
	if err == nil {
		t.Fatal("expected entry-count-exceeded error, got nil")
	}
	noLeak(t, dest)
}

func TestSafeExtractZip_UncompressedSizeExceeded(t *testing.T) {
	src := buildZip(t, map[string]string{"big.txt": "0123456789"})
	dest := filepath.Join(t.TempDir(), "out")
	_, err := SafeExtractZip(src, dest, Limits{MaxBytes: 4})
	if err == nil {
		t.Fatal("expected size-exceeded error, got nil")
	}
	noLeak(t, dest)
}

func TestSafeExtractZip_DirEntry(t *testing.T) {
	src := buildZip(t, map[string]string{
		"agents/":       "",
		"agents/foo.md": "content",
	})
	dest := filepath.Join(t.TempDir(), "out")
	_, err := SafeExtractZip(src, dest, Limits{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info, err := os.Stat(filepath.Join(dest, "agents")); err != nil || !info.IsDir() {
		t.Errorf("expected agents/ directory to be created")
	}
}

func TestSafeExtractZip_NotAZip_Errors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bogus.zip")
	if err := os.WriteFile(path, []byte("not a zip"), 0644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "out")
	_, err := SafeExtractZip(path, dest, Limits{})
	if err == nil {
		t.Fatal("expected error opening a non-zip file")
	}
}
