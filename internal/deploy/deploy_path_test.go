package deploy

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// deployPathRoot is a new directory, symlinks resolved, holding home/ (the
// deploy root) and outside/.
func deployPathRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"home/.claude/rules", "home/.claude/skills/demo", "outside/rules", "outside/dotfiles/rules"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("x", filepath.Join(root, "probe")); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}
	return root
}

func deployPathLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.RemoveAll(link); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestCheckDeployPath(t *testing.T) {
	tests := []struct {
		name  string
		rel   string
		setup func(t *testing.T, root string)
		want  func(root string) error
	}{
		{
			name: "parent below the target root links outside",
			rel:  ".claude/rules/r.md",
			setup: func(t *testing.T, root string) {
				deployPathLink(t, filepath.Join(root, "outside/rules"), filepath.Join(root, "home/.claude/rules"))
			},
			want: func(root string) error {
				return &DeployPathError{Path: ".claude/rules/r.md", Link: ".claude/rules", Resolved: filepath.Join(root, "outside/rules"), Root: filepath.Join(root, "home/.claude")}
			},
		},
		{
			name: "parent links to another directory in the target root",
			rel:  ".claude/rules/r.md",
			setup: func(t *testing.T, root string) {
				if err := os.MkdirAll(filepath.Join(root, "home/.claude/real-rules"), 0o755); err != nil {
					t.Fatal(err)
				}
				deployPathLink(t, "real-rules", filepath.Join(root, "home/.claude/rules"))
			},
		},
		{
			name: "target root is a symlink",
			rel:  ".claude/rules/r.md",
			setup: func(t *testing.T, root string) {
				deployPathLink(t, filepath.Join(root, "outside/dotfiles"), filepath.Join(root, "home/.claude"))
			},
		},
		{
			name: "path itself is a deploy symlink",
			rel:  ".claude/rules/r.md",
			setup: func(t *testing.T, root string) {
				deployPathLink(t, filepath.Join(root, "outside/rules/r.md"), filepath.Join(root, "home/.claude/rules/r.md"))
			},
		},
		{
			name: "skill directory is a symlink",
			rel:  ".claude/skills/demo/SKILL.md",
			setup: func(t *testing.T, root string) {
				deployPathLink(t, filepath.Join(root, "outside"), filepath.Join(root, "home/.claude/skills/demo"))
			},
		},
		{
			name: "directory inside a skill links outside",
			rel:  ".claude/skills/demo/data/precious.txt",
			setup: func(t *testing.T, root string) {
				deployPathLink(t, filepath.Join(root, "outside/rules"), filepath.Join(root, "home/.claude/skills/demo/data"))
			},
			want: func(root string) error {
				return &DeployPathError{Path: ".claude/skills/demo/data/precious.txt", Link: ".claude/skills/demo/data", Resolved: filepath.Join(root, "outside/rules"), Root: filepath.Join(root, "home/.claude")}
			},
		},
		{
			name:  "nothing exists yet",
			rel:   ".codex/agents/r.toml",
			setup: func(*testing.T, string) {},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := deployPathRoot(t)
			tt.setup(t, root)

			err := CheckDeployPath(filepath.Join(root, "home"), tt.rel)

			var want error
			if tt.want != nil {
				want = tt.want(root)
			}
			if !reflect.DeepEqual(err, want) {
				t.Errorf("CheckDeployPath(%q) = %v, want %v", tt.rel, err, want)
			}
		})
	}
}

func TestCheckDeployPath_DotDot(t *testing.T) {
	err := CheckDeployPath(t.TempDir(), ".claude/../etc/passwd")
	if err == nil || err.Error() != ".claude/../etc/passwd: path escapes the deploy root" {
		t.Errorf("CheckDeployPath = %v", err)
	}
}

func TestDeployPathError_Text(t *testing.T) {
	err := &DeployPathError{Path: ".claude/rules/r.md", Link: ".claude/rules", Resolved: "/o/rules", Root: "/h/.claude"}
	if want := ".claude/rules/r.md: symlink .claude/rules resolves to /o/rules, outside /h/.claude"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

// The paths an uninstall lists: each refused path and a recorded skill
// directory with one below it, sorted, as Oracle 0.33.0 lists them.
func TestOutsideDeployPaths(t *testing.T) {
	root := deployPathRoot(t)
	deployPathLink(t, filepath.Join(root, "outside/rules"), filepath.Join(root, "home/.claude/skills/demo/data"))
	files := []string{".claude/rules/r.md", ".claude/skills/demo", ".claude/skills/demo/SKILL.md", ".claude/skills/demo/data/precious.txt"}
	hashes := map[string]string{".claude/rules/r.md": "h", ".claude/skills/demo/SKILL.md": "h", ".claude/skills/demo/data/precious.txt": "h"}

	got := OutsideDeployPaths(filepath.Join(root, "home"), files, hashes)

	if want := []string{".claude/skills/demo", ".claude/skills/demo/data/precious.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("OutsideDeployPaths = %v, want %v", got, want)
	}
}

// A skill directory a removed path is in passes as a whole to a package that
// stays, so a symlink anywhere in its tree that leads out of the target root
// lists the skill directory; one that stays inside does not, and a skill
// directory that is itself a symlink is the user's and is not walked.
func TestOutsideDeployPaths_SkillTree(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, root string)
		want  []string
	}{
		{name: "symlink in the skill leads outside", want: []string{".claude/skills/demo"},
			setup: func(t *testing.T, root string) {
				deployPathLink(t, filepath.Join(root, "outside/rules"), filepath.Join(root, "home/.claude/skills/demo/sub"))
			}},
		{name: "dangling symlink in the skill leads outside", want: []string{".claude/skills/demo"},
			setup: func(t *testing.T, root string) {
				deployPathLink(t, filepath.Join(root, "outside/gone"), filepath.Join(root, "home/.claude/skills/demo/deep/sub"))
			}},
		{name: "symlink in the skill stays in the target root",
			setup: func(t *testing.T, root string) {
				deployPathLink(t, "../../rules", filepath.Join(root, "home/.claude/skills/demo/sub"))
			}},
		{name: "skill directory is a symlink",
			setup: func(t *testing.T, root string) {
				deployPathLink(t, filepath.Join(root, "outside"), filepath.Join(root, "home/.claude/skills/demo"))
				deployPathLink(t, filepath.Join(root, "outside/rules"), filepath.Join(root, "outside/sub"))
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := deployPathRoot(t)
			tt.setup(t, root)

			got := OutsideDeployPaths(filepath.Join(root, "home"), []string{".claude/skills/demo/SKILL.md"}, map[string]string{".claude/skills/demo/SKILL.md": "h"})

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("OutsideDeployPaths = %v, want %v", got, tt.want)
			}
		})
	}
}
