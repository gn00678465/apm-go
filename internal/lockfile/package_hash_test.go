package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// Each want is the content_hash Oracle 0.33.0 wrote to its lock for a package
// with these files (~/.cache/apm-go-oracle-ref/A/124-82/r3 for the
// subdirectory package mono/pkg and the repository whole, A/124-36/r1 for the
// registry package acme/sample). A .git, a __pycache__ and a root .apm-pin
// are left out; a symlink counts as what it points at, as the Oracle's copy
// of a package resolves it.
func TestComputePackageHash(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		links map[string]string
		want  string
	}{
		{
			name: "subdirectory package",
			files: map[string]string{
				"apm.yml":                             "name: monopkg\nversion: 3.1.0\n",
				".apm/instructions/m.instructions.md": "---\napplyTo: \"**\"\n---\n\nRule m.\n",
				".apm-pin":                            "pin\n",
			},
			want: "sha256:1451ccba8759ac8fb7ca4eb239debbd84d992723e50a1bbcbf00e64bca7a965d",
		},
		{
			name: "repository with its .git",
			files: map[string]string{
				"apm.yml":                             "name: wholepkg\nversion: 2.0.0\n",
				".apm/instructions/w.instructions.md": "---\napplyTo: \"**\"\n---\n\nRule w.\n",
				".git/HEAD":                           "ref: refs/heads/main\n",
				"sub/__pycache__/x.pyc":               "x",
			},
			want: "sha256:50c2ea3b55758c682c81a9d45c0b0ecea070e86f5183c6a3dd2bbc32173d4127",
		},
		{
			name: "registry package",
			files: map[string]string{
				"apm.yml":                             "name: sample\nversion: 1.0.0\n",
				".apm/instructions/x.instructions.md": "---\napplyTo: \"**\"\n---\n\nRule x.\n",
			},
			want: "sha256:4d8d086aea16a848f1728963a002093ab458d204c0bcf3ad614e6825d5122261",
		},
		{
			name: "registry package through symlinks",
			files: map[string]string{
				"apm.yml":                "name: sample\nversion: 1.0.0\n",
				"real/x.instructions.md": "---\napplyTo: \"**\"\n---\n\nRule x.\n",
			},
			links: map[string]string{".apm/instructions": "../real"},
			// The Oracle's copy of this package holds both files.
			want: "sha256:" + sha256Hex(".apm/instructions/x.instructions.md---\napplyTo: \"**\"\n---\n\nRule x.\n"+
				"apm.ymlname: sample\nversion: 1.0.0\n"+
				"real/x.instructions.md---\napplyTo: \"**\"\n---\n\nRule x.\n"),
		},
		{name: "empty", want: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, body := range tt.files {
				p := filepath.Join(dir, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for rel, target := range tt.links {
				p := filepath.Join(dir, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, p); err != nil {
					t.Skipf("symlink: %v", err)
				}
			}
			got, err := ComputePackageHash(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("ComputePackageHash = %s, want %s", got, tt.want)
			}
		})
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
