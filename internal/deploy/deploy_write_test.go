package deploy

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/archive"
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
				return &archive.ContainmentError{Path: filepath.Join(root, "home/.claude/rules/r.md"), Base: filepath.Join(root, "home/.claude/rules"), Resolved: filepath.Join(root, "outside/precious.txt")}
			},
		},
		{
			name: "last component dangles outside the target root",
			rel:  ".claude/rules/r.md",
			setup: func(t *testing.T, root string) {
				deployPathLink(t, filepath.Join(root, "outside/new.txt"), filepath.Join(root, "home/.claude/rules/r.md"))
			},
			want: func(root string) error {
				return &archive.ContainmentError{Path: filepath.Join(root, "home/.claude/rules/r.md"), Base: filepath.Join(root, "home/.claude/rules"), Resolved: filepath.Join(root, "outside/new.txt")}
			},
		},
		{
			name: "last component links to another place in the target root",
			rel:  ".claude/rules/r.md",
			setup: func(t *testing.T, root string) {
				deployPathLink(t, "../shared/r.md", filepath.Join(root, "home/.claude/rules/r.md"))
			},
			want: func(root string) error {
				return &archive.ContainmentError{Path: filepath.Join(root, "home/.claude/rules/r.md"), Base: filepath.Join(root, "home/.claude/rules"), Resolved: filepath.Join(root, "home/.claude/shared/r.md")}
			},
		},
		{
			name: "last component links inside its own directory",
			rel:  ".claude/rules/r.md",
			setup: func(t *testing.T, root string) {
				deployPathLink(t, "other.md", filepath.Join(root, "home/.claude/rules/r.md"))
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
			name: "parent links to another place in the target root",
			rel:  ".claude/rules/r.md",
			setup: func(t *testing.T, root string) {
				deployPathLink(t, "shared", filepath.Join(root, "home/.claude/rules"))
			},
		},
		{
			name: "directory of a skill links out of the skill",
			rel:  ".claude/skills/demo/sub/b.md",
			setup: func(t *testing.T, root string) {
				deployPathLink(t, "../../shared", filepath.Join(root, "home/.claude/skills/demo/sub"))
			},
			want: func(root string) error {
				return &archive.ContainmentError{Path: filepath.Join(root, "home/.claude/skills/demo/sub/b.md"), Base: filepath.Join(root, "home/.claude/skills/demo"), Resolved: filepath.Join(root, "home/.claude/shared/b.md")}
			},
		},
		{
			name: "directory of a skill links inside the skill",
			rel:  ".claude/skills/demo/sub/b.md",
			setup: func(t *testing.T, root string) {
				deployPathLink(t, "other", filepath.Join(root, "home/.claude/skills/demo/sub"))
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

// Every place a copy deploy writes a file refuses a destination that is a
// symlink out of the primitive's directory, out of the target root or to
// another place in it, as the Oracle refuses it: the write would follow it and
// overwrite what it points at.
func TestDeploy_DestinationLinksOutside_NotWritten(t *testing.T) {
	const agent = "---\ndescription: reviewer\n---\nagent\n"
	tests := []struct {
		name, src, content, dest string
		deploy                   func(home, src string) error
	}{
		{name: "copied agent", src: "agents/r.md", content: agent, dest: ".claude/agents/r.md",
			deploy: func(home, src string) error {
				_, err := Adapters["claude"].DeployPrimitive(Primitive{Name: "r", Type: TypeAgents, DepKey: "o/dep", SrcPath: src}, home)
				return err
			}},
		{name: "copied command", src: "commands/c.md", content: "command\n", dest: ".opencode/commands/c.md",
			deploy: func(home, src string) error {
				_, err := Adapters["opencode"].DeployPrimitive(Primitive{Name: "c", Type: TypeCommands, DepKey: "o/dep", SrcPath: src}, home)
				return err
			}},
		{name: "codex agent TOML", src: "agents/r.md", content: agent, dest: ".codex/agents/r.toml",
			deploy: func(home, src string) error {
				_, err := Adapters["codex"].DeployPrimitive(Primitive{Name: "r", Type: TypeAgents, DepKey: "o/dep", SrcPath: src}, home)
				return err
			}},
		{name: "claude rule", src: "instructions/r.instructions.md", content: "---\napplyTo: \"**\"\n---\nrule\n", dest: ".claude/rules/r.md",
			deploy: func(home, src string) error {
				_, err := Adapters["claude"].DeployPrimitive(Primitive{Name: "r", Type: TypeInstructions, DepKey: "o/dep", SrcPath: src}, home)
				return err
			}},
		{name: "copilot prompt", src: "prompts/p.prompt.md", content: "prompt\n", dest: ".github/prompts/p.prompt.md",
			deploy: func(home, src string) error {
				_, err := Adapters["copilot"].DeployPrimitive(Primitive{Name: "p", Type: TypePrompts, DepKey: "o/dep", SrcPath: src}, home)
				return err
			}},
		{name: "antigravity plugin.json", dest: ".agents/plugins/dep/plugin.json",
			deploy: func(home, _ string) error {
				_, err := Adapters["antigravity"].(BundleTarget).FinalizeBundles([]string{"o/dep"}, home)
				return err
			}},
	}
	for _, tt := range tests {
		for _, target := range []string{"outside", "shared"} {
			t.Run(tt.name+" to "+target, func(t *testing.T) {
				root := deployPathRoot(t)
				home := filepath.Join(root, "home")
				src := filepath.Join(root, "src", filepath.FromSlash(tt.src))
				if tt.src != "" {
					writeDeployFile(t, src, tt.content)
				}
				first, _, _ := strings.Cut(tt.dest, "/")
				user := filepath.Join(root, "outside/precious.txt")
				if target == "shared" {
					user = filepath.Join(home, first, "shared/precious.txt")
				}
				writeDeployFile(t, user, "precious unrelated user content\n")
				dest := filepath.Join(home, filepath.FromSlash(tt.dest))
				deployPathLink(t, user, dest)
				before := deployTree(t, root)

				err := tt.deploy(home, src)

				want := &archive.ContainmentError{Path: dest, Base: filepath.Dir(dest), Resolved: user}
				var got *archive.ContainmentError
				if !errors.As(err, &got) || !reflect.DeepEqual(got, want) {
					t.Errorf("deploy = %v, want %v", err, want)
				}
				if after := deployTree(t, root); !reflect.DeepEqual(after, before) {
					t.Errorf("tree changed:\n%v\nwant\n%v", after, before)
				}
			})
		}
	}
}

// A destination that links to another file of its own directory is written
// there, as the Oracle writes it.
func TestDeploy_DestinationLinksInsideItsDirectory_Written(t *testing.T) {
	root := deployPathRoot(t)
	home := filepath.Join(root, "home")
	src := filepath.Join(root, "src/r.md")
	writeDeployFile(t, src, "agent\n")
	deployPathLink(t, "other.md", filepath.Join(home, ".claude/agents/r.md"))

	if _, err := Adapters["claude"].DeployPrimitive(Primitive{Name: "r", Type: TypeAgents, DepKey: "o/dep", SrcPath: src}, home); err != nil {
		t.Fatalf("deploy = %v", err)
	}
	if got := deployTree(t, filepath.Join(home, ".claude")); got["agents/other.md"] != "agent\n" || got["agents/r.md"] != "-> other.md" {
		t.Errorf(".claude = %v, want agents/r.md -> other.md holding agent", got)
	}
}

// A file of a skill the user replaced by a symlink is replaced by the copy,
// wherever the symlink points, and what it pointed at stays, as the Oracle
// replaces the skill's directory.
func TestDeploySkill_FileLinkReplaced(t *testing.T) {
	for _, target := range []string{"outside", "shared"} {
		t.Run(target, func(t *testing.T) {
			root := deployPathRoot(t)
			home := filepath.Join(root, "home")
			skill := filepath.Join(root, "src/skills/demo")
			writeDeployFile(t, filepath.Join(skill, "SKILL.md"), "---\nname: demo\ndescription: demo\n---\n")
			writeDeployFile(t, filepath.Join(skill, "data/precious.txt"), "precious\n")
			user := filepath.Join(root, "outside/precious.txt")
			if target == "shared" {
				user = filepath.Join(home, ".claude/shared/precious.txt")
			}
			writeDeployFile(t, user, "USER SHARED CONTENT\n")
			deployPathLink(t, user, filepath.Join(home, ".claude/skills/demo/data/precious.txt"))

			files, err := Adapters["claude"].DeployPrimitive(Primitive{Name: "demo", Type: TypeSkills, DepKey: "o/dep", SrcPath: skill}, home)
			if err != nil {
				t.Fatalf("deploy = %v", err)
			}
			if want := []string{".claude/skills/demo/SKILL.md", ".claude/skills/demo/data/precious.txt"}; !reflect.DeepEqual(files, want) {
				t.Errorf("files = %v, want %v", files, want)
			}
			got := deployTree(t, home)
			if got[".claude/skills/demo/data/precious.txt"] != "precious\n" {
				t.Errorf("skill file = %q, want the package's bytes in a regular file", got[".claude/skills/demo/data/precious.txt"])
			}
			if data, _ := os.ReadFile(user); string(data) != "USER SHARED CONTENT\n" {
				t.Errorf("user file = %q, want it unchanged", data)
			}
		})
	}
}

// A directory of a skill the user replaced by a symlink out of the skill's
// directory, also to another place in the target root, is refused before
// anything is written.
func TestDeploySkill_DirLinkOutOfSkill_Refused(t *testing.T) {
	root := deployPathRoot(t)
	home := filepath.Join(root, "home")
	skill := filepath.Join(root, "src/skills/demo")
	writeDeployFile(t, filepath.Join(skill, "SKILL.md"), "---\nname: demo\ndescription: demo\n---\n")
	writeDeployFile(t, filepath.Join(skill, "data/precious.txt"), "precious\n")
	writeDeployFile(t, filepath.Join(home, ".claude/shared/data/precious.txt"), "USER SHARED CONTENT\n")
	link := filepath.Join(home, ".claude/skills/demo/data")
	deployPathLink(t, "../../shared/data", link)
	before := deployTree(t, root)

	_, err := Adapters["claude"].DeployPrimitive(Primitive{Name: "demo", Type: TypeSkills, DepKey: "o/dep", SrcPath: skill}, home)

	want := &archive.ContainmentError{Path: link, Base: filepath.Join(home, ".claude/skills/demo"), Resolved: filepath.Join(home, ".claude/shared/data")}
	var got *archive.ContainmentError
	if !errors.As(err, &got) || !reflect.DeepEqual(got, want) {
		t.Errorf("deploy = %v, want %v", err, want)
	}
	if after := deployTree(t, root); !reflect.DeepEqual(after, before) {
		t.Errorf("tree changed:\n%v\nwant\n%v", after, before)
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
	old := filepath.Join(root, "src/apm_modules/o/dep/.apm/agents/r.agent.md")
	writeDeployFile(t, old, "old\n")
	deployPathLink(t, old, filepath.Join(home, ".claude/agents/r.md"))
	activeDeployMode.symlink, activeDeployMode.projectDir = true, filepath.Join(root, "src")
	defer func() { activeDeployMode.symlink, activeDeployMode.projectDir = false, "" }()

	if _, err := Adapters["claude"].DeployPrimitive(Primitive{Name: "r", Type: TypeAgents, DepKey: "o/dep", SrcPath: src}, home); err != nil {
		t.Fatalf("deploy = %v", err)
	}
	if got, _ := os.Readlink(filepath.Join(home, ".claude/agents/r.md")); got != src {
		t.Errorf(".claude/agents/r.md -> %q, want %q", got, src)
	}
	if data, _ := os.ReadFile(old); string(data) != "old\n" {
		t.Errorf("old source = %q, want it unchanged", data)
	}
}

// A symlink deploy keeps a symlink the user put at the destination, one that
// leads outside the bucket's source and out of the primitive's directory, as
// the Oracle keeps it (measured with install -g).
func TestDeploySymlink_UserLeafLink_Refused(t *testing.T) {
	root := deployPathRoot(t)
	home := filepath.Join(root, "home")
	src := filepath.Join(root, "src/apm_modules/o/dep/.apm/agents/r.agent.md")
	writeDeployFile(t, src, "agent\n")
	user := filepath.Join(home, ".claude/shared/r.md")
	writeDeployFile(t, user, "USER SHARED CONTENT\n")
	dest := filepath.Join(home, ".claude/agents/r.md")
	deployPathLink(t, "../shared/r.md", dest)
	activeDeployMode.symlink, activeDeployMode.projectDir = true, filepath.Join(root, "src")
	defer func() { activeDeployMode.symlink, activeDeployMode.projectDir = false, "" }()
	before := deployTree(t, root)

	_, err := Adapters["claude"].DeployPrimitive(Primitive{Name: "r", Type: TypeAgents, DepKey: "o/dep", SrcPath: src}, home)

	want := &archive.ContainmentError{Path: dest, Base: filepath.Dir(dest), Resolved: user}
	var got *archive.ContainmentError
	if !errors.As(err, &got) || !reflect.DeepEqual(got, want) {
		t.Errorf("deploy = %v, want %v", err, want)
	}
	if after := deployTree(t, root); !reflect.DeepEqual(after, before) {
		t.Errorf("tree changed:\n%v\nwant\n%v", after, before)
	}
}
