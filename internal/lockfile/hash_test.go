package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashEnvelope(t *testing.T) {
	got := HashEnvelope("sha256", "abcdef1234567890")
	if got != "sha256:abcdef1234567890" {
		t.Errorf("got %q", got)
	}
}

func TestParseHashEnvelope(t *testing.T) {
	tests := []struct {
		input   string
		algo    string
		hex     string
		wantErr bool
	}{
		{"sha256:abcdef", "sha256", "abcdef", false},
		{"sha384:abcdef", "sha384", "abcdef", false},
		{"sha512:abcdef", "sha512", "abcdef", false},
		{"md5:abcdef", "", "", true},
		// Bare 64-char hex → sha256
		{"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", "sha256", "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", false},
		// Too short bare hex
		{"abcdef", "", "", true},
		{"", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			algo, hex, err := ParseHashEnvelope(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if algo != tt.algo || hex != tt.hex {
				t.Errorf("got (%q, %q), want (%q, %q)", algo, hex, tt.algo, tt.hex)
			}
		})
	}
}

func TestHashFileBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("test"), 0644)

	h, err := HashFileBytes(path)
	if err != nil {
		t.Fatal(err)
	}
	// SHA-256 of "test" = 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
	want := "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	if h != want {
		t.Errorf("got %q, want %q", h, want)
	}
}

func TestComputeDeployedFileHashes(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("world"), 0644)

	hashes, err := ComputeDeployedFileHashes([]string{"a.txt", "sub/b.txt", "sub/"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes) != 2 {
		t.Errorf("expected 2 hashes (dir skipped), got %d", len(hashes))
	}
	if _, ok := hashes["sub/"]; ok {
		t.Error("directory should not have a hash")
	}
	if hashes["a.txt"] == "" {
		t.Error("a.txt should have a hash")
	}
}

func TestVerifyDeployedHashes_Match(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("test"), 0644)

	hashes := map[string]string{
		"test.txt": "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
	}
	if err := VerifyDeployedHashes(hashes, dir); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVerifyDeployedHashes_Mismatch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("tampered"), 0644)

	hashes := map[string]string{
		"test.txt": "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
	}
	err := VerifyDeployedHashes(hashes, dir)
	if err == nil {
		t.Fatal("expected integrity violation error")
	}
	if !contains(err.Error(), "integrity violation") {
		t.Errorf("error should mention integrity violation: %v", err)
	}
}

func TestVerifyArchiveHash_Match(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.tar.gz")
	os.WriteFile(path, []byte("test"), 0644)

	err := VerifyArchiveHash(path, "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVerifyArchiveHash_Mismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.tar.gz")
	os.WriteFile(path, []byte("different"), 0644)

	err := VerifyArchiveHash(path, "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08")
	if err == nil {
		t.Fatal("expected archive integrity violation error")
	}
}

func TestVerifyDeployedHashes_BareHex(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("test"), 0644)

	hashes := map[string]string{
		"test.txt": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
	}
	if err := VerifyDeployedHashes(hashes, dir); err != nil {
		t.Errorf("bare hex should be tolerated: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
