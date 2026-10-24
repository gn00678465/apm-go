package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

// Each want is the content_hash Oracle 0.33.0 wrote to its lock for a package
// with these files (~/.cache/apm-go-oracle-ref/A/124-82/r3 for the
// subdirectory package mono/pkg and the repository whole, A/124-36/r1 for the
// registry package acme/sample). A .git, a __pycache__ and a root .apm-pin
// are left out.
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
		// The cases below are hashed by Oracle 0.33.0's compute_package_hash
		// (A/p2-oracle-hash: reviewer, dirlink, dangling, loop), which leaves
		// every symlink out and enters no symlinked directory.
		{
			name: "file symlink",
			files: map[string]string{
				"apm.yml":                             "name: pkg\nversion: 1.0.0\n",
				".apm/instructions/x.instructions.md": "---\napplyTo: \"**\"\n---\n\nRULE\n",
				"data.txt":                            "some data\n",
			},
			links: map[string]string{"link.txt": "data.txt"},
			want:  "sha256:243d1badb991fd6b58dc26ef339b31e0b83c8deb38da60450ba75c3844eb1087",
		},
		{
			name: "directory symlink",
			files: map[string]string{
				"apm.yml":                "name: sample\nversion: 1.0.0\n",
				"real/x.instructions.md": "---\napplyTo: \"**\"\n---\n\nRule x.\n",
			},
			links: map[string]string{".apm/instructions": "../real"},
			want:  "sha256:2e329a8c5255ca28fe346e182f3438068be4c3aa6b3ee06fe051ab284367cfd3",
		},
		{
			name:  "dangling symlink",
			files: map[string]string{"apm.yml": "name: d\nversion: 1.0.0\n"},
			links: map[string]string{"gone.txt": "nothere"},
			want:  "sha256:054fb1dccfeb86626a0fb5978fc6c9771d8d8efc8dfa9103d77677f36ae9e330",
		},
		{
			name:  "symlink loop",
			files: map[string]string{"apm.yml": "name: l\nversion: 1.0.0\n"},
			links: map[string]string{"sub/self": "."},
			want:  "sha256:1ce62455ef5e8e16d29afc6589fdd18eada3f24f853c824e505c8083575a8a14",
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
