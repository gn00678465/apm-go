package deploy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
)

// A copy deploy writes deployText's bytes where a --global symlink deploy
// shows the source's, and one lock has to restore under both (issue #118), so
// the two hash alike.
func TestDeployText_HashesAsSource(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"crlf", "a\r\nb\r\n", "a\nb\n"},
		{"lf only", "a\nb\n", "a\nb\n"},
		{"bare cr", "a\rb\r\n", "a\rb\n"},
		{"cr before crlf", "a\r\r\nb\n", "a\r\r\nb\n"},
		{"nul", "a\x00\r\nb\r\n", "a\x00\r\nb\r\n"},
		{"not utf-8", "a\xff\r\nb\r\n", "a\xff\r\nb\r\n"},
		{"mixed", "a\r\nb\nc\rd\r\n", "a\nb\nc\rd\n"},
	}
	dir := t.TempDir()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deployText([]byte(tc.src))
			if string(got) != tc.want {
				t.Errorf("deployText(%q) = %q, want %q", tc.src, got, tc.want)
			}
			src := filepath.Join(dir, tc.name+".src")
			dst := filepath.Join(dir, tc.name+".dst")
			if err := os.WriteFile(src, []byte(tc.src), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dst, got, 0o644); err != nil {
				t.Fatal(err)
			}
			srcHash, err := lockfile.HashDeployedFile(src)
			if err != nil {
				t.Fatal(err)
			}
			dstHash, err := lockfile.HashDeployedFile(dst)
			if err != nil {
				t.Fatal(err)
			}
			if srcHash != dstHash {
				t.Errorf("hash of written %q = %s, of source %q = %s", got, dstHash, tc.src, srcHash)
			}
		})
	}
}
