package deploy

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCheckDeployWrite(t *testing.T) {
	tests := []struct {
		name  string
		rel   string
		setup func(t *testing.T, root string)
		want  func(root string) error
	}{
		{
			name: "last component links outside the target root",
			rel:  ".claude/rules/r.md",
			setup: func(t *testing.T, root string) {
				deployPathLink(t, filepath.Join(root, "outside/precious.txt"), filepath.Join(root, "home/.claude/rules/r.md"))
			},
			want: func(root string) error {
				return &DeployPathError{Path: ".claude/rules/r.md", Link: ".claude/rules/r.md", Resolved: filepath.Join(root, "outside/precious.txt"), Root: filepath.Join(root, "home/.claude")}
			},
		},
		{
			name: "last component dangles outside the target root",
			rel:  ".claude/rules/r.md",
			setup: func(t *testing.T, root string) {
				deployPathLink(t, filepath.Join(root, "outside/new.txt"), filepath.Join(root, "home/.claude/rules/r.md"))
			},
			want: func(root string) error {
				return &DeployPathError{Path: ".claude/rules/r.md", Link: ".claude/rules/r.md", Resolved: filepath.Join(root, "outside/new.txt"), Root: filepath.Join(root, "home/.claude")}
			},
		},
		{
			name: "last component links inside the target root",
			rel:  ".claude/rules/r.md",
			setup: func(t *testing.T, root string) {
				deployPathLink(t, "../shared/r.md", filepath.Join(root, "home/.claude/rules/r.md"))
			},
		},
		{
			name: "parent links outside the target root",
			rel:  ".claude/rules/r.md",
			setup: func(t *testing.T, root string) {
				deployPathLink(t, filepath.Join(root, "outside/rules"), filepath.Join(root, "home/.claude/rules"))
			},
			want: func(root string) error {
				return &DeployPathError{Path: ".claude/rules/r.md", Link: ".claude/rules", Resolved: filepath.Join(root, "outside/rules"), Root: filepath.Join(root, "home/.claude")}
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
			outsideBefore := deployTree(t, filepath.Join(root, "outside"))

			err := CheckDeployWrite(filepath.Join(root, "home"), tt.rel)

			var want error
			if tt.want != nil {
				want = tt.want(root)
			}
			if !reflect.DeepEqual(err, want) {
				t.Errorf("CheckDeployWrite(%q) = %v, want %v", tt.rel, err, want)
			}
			if got := deployTree(t, filepath.Join(root, "outside")); !reflect.DeepEqual(got, outsideBefore) {
				t.Errorf("outside changed: %v, want %v", got, outsideBefore)
			}
		})
	}
}

// A symlink a cleanup removes, or a symlink deploy replaces, is not followed.
func TestCheckDeployPath_LastComponentLinksOutside(t *testing.T) {
	root := deployPathRoot(t)
	deployPathLink(t, filepath.Join(root, "outside/precious.txt"), filepath.Join(root, "home/.claude/rules/r.md"))

	if err := CheckDeployPath(filepath.Join(root, "home"), ".claude/rules/r.md"); err != nil {
		t.Errorf("CheckDeployPath = %v, want nil", err)
	}
}

// deployTree lists every path under dir with its content or link target,
// without following symlinks.
func deployTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(p)
			got[rel] = "-> " + target
			return err
		case info.IsDir():
			got[rel] = "dir"
		default:
			data, err := os.ReadFile(p)
			got[rel] = string(data)
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func writeDeployFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Every place a copy deploy writes content refuses a destination that is a
// symlink out of its target root: the write would follow it and overwrite
// what it points at.
func TestDeploy_DestinationLinksOutside_NotWritten(t *testing.T) {
	const agent = "---\ndescription: reviewer\n---\nagent\n"
	tests := []struct {
		name, src, content, dest, root string
		more                           map[string]string
		deploy                         func(home, src string) error
	}{
		{name: "copied agent", src: "agents/r.md", content: agent, dest: ".claude/agents/r.md", root: ".claude",
			deploy: func(home, src string) error {
				_, err := Adapters["claude"].DeployPrimitive(Primitive{Name: "r", Type: TypeAgents, DepKey: "o/dep", SrcPath: src}, home)
				return err
			}},
		{name: "copied command", src: "commands/c.md", content: "command\n", dest: ".opencode/commands/c.md", root: ".opencode",
			deploy: func(home, src string) error {
				_, err := Adapters["opencode"].DeployPrimitive(Primitive{Name: "c", Type: TypeCommands, DepKey: "o/dep", SrcPath: src}, home)
				return err
			}},
		{name: "codex agent TOML", src: "agents/r.md", content: agent, dest: ".codex/agents/r.toml", root: ".codex",
			deploy: func(home, src string) error {
				_, err := Adapters["codex"].DeployPrimitive(Primitive{Name: "r", Type: TypeAgents, DepKey: "o/dep", SrcPath: src}, home)
				return err
			}},
		{name: "claude rule", src: "instructions/r.instructions.md", content: "---\napplyTo: \"**\"\n---\nrule\n", dest: ".claude/rules/r.md", root: ".claude",
			deploy: func(home, src string) error {
				_, err := Adapters["claude"].DeployPrimitive(Primitive{Name: "r", Type: TypeInstructions, DepKey: "o/dep", SrcPath: src}, home)
				return err
			}},
		{name: "file in a skill", src: "skills/demo/data/precious.txt", content: "precious\n", dest: ".claude/skills/demo/data/precious.txt", root: ".claude",
			more: map[string]string{"skills/demo/SKILL.md": "---\nname: demo\ndescription: demo\n---\n"},
			deploy: func(home, src string) error {
				skill := filepath.Dir(filepath.Dir(src))
				_, err := Adapters["claude"].DeployPrimitive(Primitive{Name: "demo", Type: TypeSkills, DepKey: "o/dep", SrcPath: skill}, home)
				return err
			}},
		{name: "antigravity plugin.json", dest: ".agents/plugins/dep/plugin.json", root: ".agents",
			deploy: func(home, _ string) error {
				_, err := Adapters["antigravity"].(BundleTarget).FinalizeBundles([]string{"o/dep"}, home)
				return err
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := deployPathRoot(t)
			home := filepath.Join(root, "home")
			src := filepath.Join(root, "src", filepath.FromSlash(tt.src))
			if tt.src != "" {
				writeDeployFile(t, src, tt.content)
			}
			for rel, content := range tt.more {
				writeDeployFile(t, filepath.Join(root, "src", filepath.FromSlash(rel)), content)
			}
			writeDeployFile(t, filepath.Join(root, "outside/precious.txt"), "precious unrelated user content\n")
			deployPathLink(t, filepath.Join(root, "outside/precious.txt"), filepath.Join(home, filepath.FromSlash(tt.dest)))
			before := deployTree(t, root)

			err := tt.deploy(home, src)

			want := &DeployPathError{Path: tt.dest, Link: tt.dest, Resolved: filepath.Join(root, "outside/precious.txt"), Root: filepath.Join(home, tt.root)}
			var got *DeployPathError
			if !errors.As(err, &got) || !reflect.DeepEqual(got, want) {
				t.Errorf("deploy = %v, want %v", err, want)
			}
			if after := deployTree(t, root); !reflect.DeepEqual(after, before) {
				t.Errorf("tree changed:\n%v\nwant\n%v", after, before)
			}
		})
	}
}

// A destination that links to another place in its target root is written
// there, as it was.
func TestDeploy_DestinationLinksInsideTargetRoot_Written(t *testing.T) {
	root := deployPathRoot(t)
	home := filepath.Join(root, "home")
	src := filepath.Join(root, "src/r.md")
	writeDeployFile(t, src, "agent\n")
	deployPathLink(t, "../shared/r.md", filepath.Join(home, ".claude/agents/r.md"))
	if err := os.MkdirAll(filepath.Join(home, ".claude/shared"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Adapters["claude"].DeployPrimitive(Primitive{Name: "r", Type: TypeAgents, DepKey: "o/dep", SrcPath: src}, home); err != nil {
		t.Fatalf("deploy = %v", err)
	}
	if got := deployTree(t, filepath.Join(home, ".claude")); got["shared/r.md"] != "agent\n" || got["agents/r.md"] != "-> ../shared/r.md" {
		t.Errorf(".claude = %v, want agents/r.md -> ../shared/r.md holding agent", got)
	}
}

// Feedback test 4: a symlink deploy replaces a symlink at the destination,
// its own from an earlier run, which points out of the target root into
// apm_modules; the file it pointed at is not touched.
func TestDeploySymlink_ReplacesLeafLink(t *testing.T) {
	root := deployPathRoot(t)
	home := filepath.Join(root, "home")
	src := filepath.Join(root, "src/r.md")
	writeDeployFile(t, src, "agent\n")
	writeDeployFile(t, filepath.Join(root, "outside/old.md"), "old\n")
	deployPathLink(t, filepath.Join(root, "outside/old.md"), filepath.Join(home, ".claude/agents/r.md"))
	activeDeployMode.symlink = true
	defer func() { activeDeployMode.symlink = false }()

	if _, err := Adapters["claude"].DeployPrimitive(Primitive{Name: "r", Type: TypeAgents, DepKey: "o/dep", SrcPath: src}, home); err != nil {
		t.Fatalf("deploy = %v", err)
	}
	if got, _ := os.Readlink(filepath.Join(home, ".claude/agents/r.md")); got != src {
		t.Errorf(".claude/agents/r.md -> %q, want %q", got, src)
	}
	if got := deployTree(t, filepath.Join(root, "outside")); got["old.md"] != "old\n" {
		t.Errorf("outside = %v, want old.md unchanged", got)
	}
}
