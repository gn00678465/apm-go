package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const oracleIntegrity = "../../conformance/conformance-kit/oracle/integrity"

func openOracle(t *testing.T, name string) *os.File {
	t.Helper()
	p := filepath.Join(oracleIntegrity, name)
	f, err := os.Open(p)
	if err != nil {
		t.Skipf("oracle not present (%s); run from full checkout", p)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// noLeak asserts the staging dir and dest were fully cleaned after a failure.
func noLeak(t *testing.T, dest string) {
	t.Helper()
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("dest %q should not exist after fail-closed, stat err=%v", dest, err)
	}
	if _, err := os.Stat(dest + ".apmtmp"); !os.IsNotExist(err) {
		t.Errorf("staging dir leaked after fail-closed")
	}
}

func TestSafeExtract_ZipSlip(t *testing.T) {
	f := openOracle(t, "zip-slip.tar.gz")
	dest := filepath.Join(t.TempDir(), "out")
	_, err := SafeExtract(f, dest, Limits{})
	if err == nil {
		t.Fatal("expected fail-closed on zip-slip, got nil")
	}
	if !strings.Contains(err.Error(), "..") {
		t.Errorf("diagnostic must contain \"..\", got %q", err.Error())
	}
	noLeak(t, dest)
}

// writeCanary drops a canary file as a sibling of dest's parent temp dir
// (i.e. outside dest and outside dest's ".apmtmp" staging dir) and returns a
// closure that fails the test if the canary's bytes ever changed -- proof
// that a fail-closed extraction touched nothing outside its own staging
// area.
func writeCanary(t *testing.T, parent string) func() {
	t.Helper()
	canary := filepath.Join(parent, "canary.txt")
	const content = "must not change"
	if err := os.WriteFile(canary, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		got, err := os.ReadFile(canary)
		if err != nil || string(got) != content {
			t.Errorf("canary outside dest changed unexpectedly: got=%q err=%v", got, err)
		}
	}
}

func TestSafeExtract_SymlinkEscape(t *testing.T) {
	f := openOracle(t, "symlink-escape.tar.gz")
	parent := t.TempDir()
	dest := filepath.Join(parent, "out")
	checkCanary := writeCanary(t, parent)
	_, err := SafeExtract(f, dest, Limits{})
	if err == nil {
		t.Fatal("expected fail-closed on symlink-escape, got nil")
	}
	if !strings.Contains(err.Error(), "link") {
		t.Errorf("diagnostic must contain \"link\", got %q", err.Error())
	}
	noLeak(t, dest)
	checkCanary()
}

// TestSafeExtract_HardlinkEscape is SEC-03's hardlink counterpart to
// TestSafeExtract_SymlinkEscape: extract.go rejects tar.TypeLink identically
// to tar.TypeSymlink (see the combined guard in extractInto), but until now
// only the symlink shape had a covering test. Built synthetically via tgz
// (like the other non-oracle cases below) since no committed oracle fixture
// contains a hardlink entry.
func TestSafeExtract_HardlinkEscape(t *testing.T) {
	data := tgz(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Name: "evil-hardlink", Typeflag: tar.TypeLink, Linkname: "../../etc/passwd", Mode: 0o644})
	})
	parent := t.TempDir()
	dest := filepath.Join(parent, "out")
	checkCanary := writeCanary(t, parent)
	_, err := SafeExtract(bytes.NewReader(data), dest, Limits{})
	if err == nil {
		t.Fatal("expected fail-closed on hardlink entry, got nil")
	}
	if !strings.Contains(err.Error(), "link") {
		t.Errorf("diagnostic must contain \"link\", got %q", err.Error())
	}
	noLeak(t, dest)
	checkCanary()
}

func TestSafeExtract_FourEntryCap(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out")
	f := openOracle(t, "four-entry.tar.gz")
	_, err := SafeExtract(f, dest, Limits{MaxEntries: 3})
	if err == nil {
		t.Fatal("expected fail-closed at entry-count cap, got nil")
	}
	if !strings.Contains(err.Error(), "entry count") {
		t.Errorf("expected entry-count diagnostic, got %q", err.Error())
	}
	noLeak(t, dest)

	// Default cap (10000) admits all four entries.
	f2 := openOracle(t, "four-entry.tar.gz")
	dest2 := filepath.Join(t.TempDir(), "out2")
	ex, err := SafeExtract(f2, dest2, Limits{})
	if err != nil {
		t.Fatalf("default cap should extract four-entry: %v", err)
	}
	if len(ex.Files) != 4 {
		t.Errorf("expected 4 files, got %d (%v)", len(ex.Files), ex.Files)
	}
}

func TestSafeExtract_Good(t *testing.T) {
	f := openOracle(t, "good.tar.gz")
	dest := filepath.Join(t.TempDir(), "out")
	ex, err := SafeExtract(f, dest, Limits{})
	if err != nil {
		t.Fatalf("good.tar.gz should extract: %v", err)
	}
	if len(ex.Files) != 1 || ex.Files[0] != "skill/SKILL.md" {
		t.Fatalf("expected [skill/SKILL.md], got %v", ex.Files)
	}
	if _, err := os.Stat(filepath.Join(dest, "skill", "SKILL.md")); err != nil {
		t.Errorf("extracted file missing on disk: %v", err)
	}
}

// --- synthetic archives for cases without an oracle fixture ---

func tgz(t *testing.T, build func(*tar.Writer)) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	build(tw)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeReg(tw *tar.Writer, name string, data []byte) {
	tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Size: int64(len(data)), Mode: 0o644})
	tw.Write(data)
}

func TestSafeExtract_RejectsZipContainer(t *testing.T) {
	// minimal zip local-file-header magic "PK\x03\x04"
	data := []byte{0x50, 0x4b, 0x03, 0x04, 0x00, 0x00}
	dest := filepath.Join(t.TempDir(), "out")
	_, err := SafeExtract(bytes.NewReader(data), dest, Limits{})
	if err == nil || !strings.Contains(err.Error(), "application/zip") {
		t.Fatalf("expected application/zip rejection, got %v", err)
	}
	noLeak(t, dest)
}

func TestSafeExtract_RejectsAbsolutePath(t *testing.T) {
	data := tgz(t, func(tw *tar.Writer) { writeReg(tw, "/etc/evil", []byte("x")) })
	dest := filepath.Join(t.TempDir(), "out")
	_, err := SafeExtract(bytes.NewReader(data), dest, Limits{})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("expected absolute-path rejection, got %v", err)
	}
	noLeak(t, dest)
}

func TestSafeExtract_SizeCap(t *testing.T) {
	data := tgz(t, func(tw *tar.Writer) { writeReg(tw, "big.bin", bytes.Repeat([]byte("A"), 1024)) })
	dest := filepath.Join(t.TempDir(), "out")
	_, err := SafeExtract(bytes.NewReader(data), dest, Limits{MaxBytes: 512})
	if err == nil || !strings.Contains(err.Error(), "size exceeds") {
		t.Fatalf("expected size-cap rejection, got %v", err)
	}
	noLeak(t, dest)
}

func TestSafeExtract_NestedDotDotRejected(t *testing.T) {
	data := tgz(t, func(tw *tar.Writer) { writeReg(tw, "a/../../escape.txt", []byte("x")) })
	dest := filepath.Join(t.TempDir(), "out")
	_, err := SafeExtract(bytes.NewReader(data), dest, Limits{})
	if err == nil || !strings.Contains(err.Error(), "..") {
		t.Fatalf("expected .. rejection, got %v", err)
	}
	noLeak(t, dest)
}

func TestSafeExtract_DirAndNestedFile(t *testing.T) {
	data := tgz(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Name: "pkg/", Typeflag: tar.TypeDir, Mode: 0o755})
		writeReg(tw, "pkg/file.txt", []byte("hi"))
	})
	dest := filepath.Join(t.TempDir(), "out")
	ex, err := SafeExtract(bytes.NewReader(data), dest, Limits{})
	if err != nil {
		t.Fatalf("valid dir+file archive should extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "pkg", "file.txt")); err != nil {
		t.Errorf("nested file missing: %v", err)
	}
	if len(ex.Files) != 1 {
		t.Errorf("expected 1 regular file recorded, got %v", ex.Files)
	}
}

func TestSafeExtract_RejectsUnsupportedType(t *testing.T) {
	data := tgz(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Name: "dev", Typeflag: tar.TypeFifo, Mode: 0o644})
	})
	dest := filepath.Join(t.TempDir(), "out")
	_, err := SafeExtract(bytes.NewReader(data), dest, Limits{})
	if err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("expected unsupported-type rejection, got %v", err)
	}
	noLeak(t, dest)
}

func TestContained(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		target string
		want   bool
	}{
		{filepath.Join(root, "apm_modules", "x", "repo"), true},
		{root, true},
		{filepath.Join(root, "..", "escape"), false},
		{filepath.Join(root, "apm_modules", "..", "..", "escape"), false},
	}
	for _, tc := range cases {
		if got := Contained(root, tc.target); got != tc.want {
			t.Errorf("Contained(%q, %q) = %v want %v", root, tc.target, got, tc.want)
		}
	}
}

func TestContainedKey(t *testing.T) {
	root := "apm_modules"
	cases := []struct {
		key  string
		want bool
	}{
		{"acme/foo", true},
		{"acme/foo/sub/pkg", true},
		{"../../../evil", false},                // escapes root entirely
		{"..", false},                           // resolves to root itself
		{"acme/..", false},                      // resolves to root itself
		{"acme/../other", false},                // resolves to a sibling, still "inside" root but wrong
		{"acme/foo/../../../etc/passwd", false}, // deep escape
		{"acme\\..\\other", false},              // backslash-separated ".." must also be caught
	}
	for _, tc := range cases {
		if got := ContainedKey(root, tc.key); got != tc.want {
			t.Errorf("ContainedKey(%q, %q) = %v, want %v", root, tc.key, got, tc.want)
		}
	}
}

func TestSafeExtract_GzipMagicButInvalid(t *testing.T) {
	// gzip magic 1f 8b but not a valid gzip stream -> gzip.NewReader errors.
	data := []byte{0x1f, 0x8b, 0x00, 0x00, 0x00}
	dest := filepath.Join(t.TempDir(), "out")
	_, err := SafeExtract(bytes.NewReader(data), dest, Limits{})
	if err == nil || !strings.Contains(err.Error(), "gzip") {
		t.Fatalf("expected gzip error, got %v", err)
	}
}
