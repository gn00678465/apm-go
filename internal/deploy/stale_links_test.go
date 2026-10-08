package deploy

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// sha256sum of the three source files staleLinkRoots and the tests write:
// "gone\n", "demo\n" and "extra\n".
const (
	staleLinkGoneHash  = "sha256:4b9f2c32577beb1ebc8ab2a1e226faaa9176a81cd4eedbaa22f8a0db919972b5"
	staleLinkDemoHash  = "sha256:eb9c26baee47f19e4993a77bca936d0ff09e355a82d3db79bf154ebff1a80604"
	staleLinkExtraHash = "sha256:65110ea3b8b62b0c09742c368bf1527f0978b06dff7a1371ef7b4c98e244d91a"
)

// staleLinkRoots returns a deploy root and a source root in one temp
// directory. The source root holds agents/keep.md, agents/gone.md and
// skills/demo/SKILL.md.
func staleLinkRoots(t *testing.T) (deployRoot, sourceRoot string) {
	t.Helper()
	base := t.TempDir()
	deployRoot = filepath.Join(base, "home")
	sourceRoot = filepath.Join(base, "home", ".apm")
	for rel, content := range map[string]string{
		"agents/keep.md":       "keep\n",
		"agents/gone.md":       "gone\n",
		"skills/demo/SKILL.md": "demo\n",
	} {
		writeStaleLinkFile(t, filepath.Join(sourceRoot, filepath.FromSlash(rel)), content)
	}
	return deployRoot, sourceRoot
}

func writeStaleLinkFile(t *testing.T, full, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func staleLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func assertStaleLinkGone(t *testing.T, full string) {
	t.Helper()
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Errorf("%s still present (Lstat err = %v)", full, err)
	}
}

func assertStaleLinkContent(t *testing.T, full, want string) {
	t.Helper()
	got, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s = %q, want %q", full, got, want)
	}
}

func assertStaleLinkResult(t *testing.T, removed, wantRemoved, diags []string, wantDiags int) {
	t.Helper()
	if !reflect.DeepEqual(removed, wantRemoved) {
		t.Errorf("removed = %v, want %v", removed, wantRemoved)
	}
	if len(diags) != wantDiags {
		t.Errorf("diags = %v, want %d", diags, wantDiags)
	}
}

func TestRemoveStaleLinkedFiles_PathIsSymlink(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	link := filepath.Join(deployRoot, ".claude", "agents", "gone.md")
	staleLink(t, filepath.Join(sourceRoot, "agents", "gone.md"), link)

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/agents/gone.md"}, map[string]string{".claude/agents/gone.md": staleLinkGoneHash}, nil)

	assertStaleLinkResult(t, removed, []string{".claude/agents/gone.md"}, diags, 0)
	assertStaleLinkGone(t, link)
	assertStaleLinkGone(t, filepath.Join(deployRoot, ".claude"))
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "agents", "gone.md"), "gone\n")
}

func TestRemoveStaleLinkedFiles_DanglingSymlinkWithRelativeTarget(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	link := filepath.Join(deployRoot, ".claude", "agents", "gone.md")
	staleLink(t, filepath.Join("..", "..", ".apm", "agents", "deleted.md"), link)
	staleLink(t, filepath.Join(sourceRoot, "agents", "keep.md"), filepath.Join(deployRoot, ".claude", "agents", "keep.md"))

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/agents/gone.md"}, nil,
		map[string]bool{".claude/agents/keep.md": true})

	assertStaleLinkResult(t, removed, []string{".claude/agents/gone.md"}, diags, 0)
	assertStaleLinkGone(t, link)
	assertStaleLinkContent(t, filepath.Join(deployRoot, ".claude", "agents", "keep.md"), "keep\n")
}

func TestRemoveStaleLinkedFiles_ParentSymlinkWithNothingClaimedBelow(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	link := filepath.Join(deployRoot, ".claude", "skills", "demo")
	staleLink(t, filepath.Join(sourceRoot, "skills", "demo"), link)
	writeStaleLinkFile(t, filepath.Join(sourceRoot, "skills", "demo", "extra.md"), "extra\n")
	// A sibling whose name starts with the link's name is not below it.
	claimed := map[string]bool{".claude/skills/demo-two/SKILL.md": true}
	writeStaleLinkFile(t, filepath.Join(deployRoot, ".claude", "skills", "demo-two", "SKILL.md"), "two\n")

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot,
		[]string{".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra.md"},
		map[string]string{".claude/skills/demo/SKILL.md": staleLinkDemoHash, ".claude/skills/demo/extra.md": staleLinkExtraHash}, claimed)

	assertStaleLinkResult(t, removed, []string{".claude/skills/demo"}, diags, 0)
	assertStaleLinkGone(t, link)
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "skills", "demo", "SKILL.md"), "demo\n")
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "skills", "demo", "extra.md"), "extra\n")
	assertStaleLinkContent(t, filepath.Join(deployRoot, ".claude", "skills", "demo-two", "SKILL.md"), "two\n")
}

func TestRemoveStaleLinkedFiles_ParentSymlinkWithClaimedPathBelow(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	link := filepath.Join(deployRoot, ".claude", "skills", "demo")
	staleLink(t, filepath.Join(sourceRoot, "skills", "demo"), link)
	writeStaleLinkFile(t, filepath.Join(sourceRoot, "skills", "demo", "extra.md"), "extra\n")

	// The recorded hash (sha256sum of "extra\n") matches the file behind the
	// symlink: a delete through the symlink would pass the hash check.
	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/skills/demo/extra.md"},
		map[string]string{".claude/skills/demo/extra.md": "sha256:65110ea3b8b62b0c09742c368bf1527f0978b06dff7a1371ef7b4c98e244d91a"},
		map[string]bool{".claude/skills/demo/SKILL.md": true})

	assertStaleLinkResult(t, removed, nil, diags, 0)
	assertStaleLinkContent(t, filepath.Join(link, "SKILL.md"), "demo\n")
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "skills", "demo", "extra.md"), "extra\n")
}

func TestRemoveStaleLinkedFiles_NoSymlinkUsesHashCheck(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	same := filepath.Join(deployRoot, ".codex", "agents", "gone.toml")
	edited := filepath.Join(deployRoot, ".codex", "agents", "edited.toml")
	writeStaleLinkFile(t, same, "name = \"gone\"\n")
	writeStaleLinkFile(t, edited, "edited by the user\n")
	hashes := map[string]string{
		// sha256sum of the content written above.
		".codex/agents/gone.toml":   "sha256:8201da4eee960804250182dd55436de61f94c2298cdbae62dbb71825cabf3eff",
		".codex/agents/edited.toml": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot,
		[]string{".codex/agents/gone.toml", ".codex/agents/edited.toml"}, hashes, nil)

	assertStaleLinkResult(t, removed, []string{".codex/agents/gone.toml"}, diags, 1)
	if len(diags) == 1 && diags[0] != `keeping ".codex/agents/edited.toml": modified since deploy (hash mismatch)` {
		t.Errorf("diag = %q", diags[0])
	}
	assertStaleLinkGone(t, same)
	assertStaleLinkContent(t, edited, "edited by the user\n")
}

func TestRemoveStaleLinkedFiles_MissingPath(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	writeStaleLinkFile(t, filepath.Join(deployRoot, ".claude", "agents", "other.md"), "other\n")

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot,
		[]string{".claude/agents/gone.md", ".claude/skills/demo/SKILL.md"}, nil, nil)

	assertStaleLinkResult(t, removed, nil, diags, 0)
	assertStaleLinkContent(t, filepath.Join(deployRoot, ".claude", "agents", "other.md"), "other\n")
}

func TestRemoveStaleLinkedFiles_PathIsSymlinkWithTargetOutsideSourceRoot(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	outside := filepath.Join(t.TempDir(), "mine")
	writeStaleLinkFile(t, filepath.Join(outside, "agent.md"), "mine\n")
	leaf := filepath.Join(deployRoot, ".claude", "agents", "gone.md")
	staleLink(t, filepath.Join(outside, "agent.md"), leaf)

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/agents/gone.md"}, nil, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	want := `keeping ".claude/agents/gone.md": symlink target "` + filepath.Join(outside, "agent.md") + `" is not in the source of this package`
	if len(diags) == 1 && diags[0] != want {
		t.Errorf("diag = %q, want %q", diags[0], want)
	}
	assertStaleLinkIsSymlink(t, leaf)
	assertStaleLinkContent(t, leaf, "mine\n")
}

// A skill directory the user took over is kept whole and reported once, by
// its own lock path.
func TestRemoveStaleLinkedFiles_UserParentSymlink_KeepsFileWithOtherHash(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	outside := filepath.Join(t.TempDir(), "mine")
	writeStaleLinkFile(t, filepath.Join(outside, "skill", "SKILL.md"), "my skill\n")
	parent := filepath.Join(deployRoot, ".claude", "skills", "demo")
	staleLink(t, filepath.Join(outside, "skill"), parent)

	// The recorded hash is the sha256sum of "demo\n", the deployed content.
	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot,
		[]string{".claude/skills/demo/SKILL.md", ".claude/skills/demo/more.md"},
		map[string]string{".claude/skills/demo/SKILL.md": "sha256:eb9c26baee47f19e4993a77bca936d0ff09e355a82d3db79bf154ebff1a80604"}, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	want := `keeping ".claude/skills/demo": symlink target "` + filepath.Join(outside, "skill") + `" is not in the source of this package`
	if len(diags) == 1 && diags[0] != want {
		t.Errorf("diag = %q, want %q", diags[0], want)
	}
	assertStaleLinkIsSymlink(t, parent)
	assertStaleLinkContent(t, filepath.Join(parent, "SKILL.md"), "my skill\n")
}

func TestRemoveStaleLinkedFiles_KeepsDeployRootAndNonEmptyParents(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	staleLink(t, filepath.Join(sourceRoot, "agents", "gone.md"), filepath.Join(deployRoot, ".claude", "agents", "gone.md"))
	writeStaleLinkFile(t, filepath.Join(deployRoot, ".claude", "settings.json"), "{}\n")

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/agents/gone.md"}, map[string]string{".claude/agents/gone.md": staleLinkGoneHash}, nil)

	assertStaleLinkResult(t, removed, []string{".claude/agents/gone.md"}, diags, 0)
	assertStaleLinkGone(t, filepath.Join(deployRoot, ".claude", "agents"))
	assertStaleLinkContent(t, filepath.Join(deployRoot, ".claude", "settings.json"), "{}\n")
}

func TestRemoveStaleLinkedFiles_PathEscapingDeployRoot(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	victim := filepath.Join(filepath.Dir(deployRoot), "victim.md")
	writeStaleLinkFile(t, victim, "victim\n")

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{"../victim.md"}, nil, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	if len(diags) == 1 && diags[0] != `refusing to remove "../victim.md": path escapes project directory` {
		t.Errorf("diag = %q", diags[0])
	}
	assertStaleLinkContent(t, victim, "victim\n")
}

// staleLinkUserDir makes deployRoot/.claude the user's own symlink to a
// directory outside sourceRoot and returns that directory.
func staleLinkUserDir(t *testing.T, deployRoot string) string {
	t.Helper()
	dotfiles := filepath.Join(t.TempDir(), "dotfiles", "claude")
	if err := os.MkdirAll(dotfiles, 0o755); err != nil {
		t.Fatal(err)
	}
	staleLink(t, dotfiles, filepath.Join(deployRoot, ".claude"))
	return dotfiles
}

func assertStaleLinkIsSymlink(t *testing.T, full string) {
	t.Helper()
	info, err := os.Lstat(full)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s is not a symlink", full)
	}
}

func TestRemoveStaleLinkedFiles_UserParentSymlink_RemovesDanglingLeaf(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	dotfiles := staleLinkUserDir(t, deployRoot)
	staleLink(t, filepath.Join(sourceRoot, "agents", "deleted.md"), filepath.Join(dotfiles, "agents", "gone.md"))
	staleLink(t, filepath.Join(sourceRoot, "agents", "keep.md"), filepath.Join(dotfiles, "agents", "keep.md"))

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/agents/gone.md"}, nil,
		map[string]bool{".claude/agents/keep.md": true})

	assertStaleLinkResult(t, removed, []string{".claude/agents/gone.md"}, diags, 0)
	assertStaleLinkGone(t, filepath.Join(dotfiles, "agents", "gone.md"))
	assertStaleLinkIsSymlink(t, filepath.Join(deployRoot, ".claude"))
	assertStaleLinkContent(t, filepath.Join(deployRoot, ".claude", "agents", "keep.md"), "keep\n")
}

func TestRemoveStaleLinkedFiles_UserParentSymlink_RemovesSkillSymlinkOnly(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	dotfiles := staleLinkUserDir(t, deployRoot)
	staleLink(t, filepath.Join(sourceRoot, "skills", "demo"), filepath.Join(dotfiles, "skills", "demo"))
	staleLink(t, filepath.Join(sourceRoot, "agents", "keep.md"), filepath.Join(dotfiles, "agents", "keep.md"))

	// The recorded hash (sha256sum of "demo\n") matches the file behind the
	// skill symlink: a delete through the symlink would pass the hash check.
	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/skills/demo/SKILL.md"},
		map[string]string{".claude/skills/demo/SKILL.md": "sha256:eb9c26baee47f19e4993a77bca936d0ff09e355a82d3db79bf154ebff1a80604"},
		map[string]bool{".claude/agents/keep.md": true})

	assertStaleLinkResult(t, removed, []string{".claude/skills/demo"}, diags, 0)
	assertStaleLinkGone(t, filepath.Join(dotfiles, "skills"))
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "skills", "demo", "SKILL.md"), "demo\n")
	assertStaleLinkIsSymlink(t, filepath.Join(deployRoot, ".claude"))
}

func TestRemoveStaleLinkedFiles_UserParentSymlink_RegularFileUsesHashCheck(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	dotfiles := staleLinkUserDir(t, deployRoot)
	writeStaleLinkFile(t, filepath.Join(dotfiles, "agents", "gone.toml"), "name = \"gone\"\n")
	writeStaleLinkFile(t, filepath.Join(dotfiles, "rules", "edited.md"), "edited by the user\n")
	hashes := map[string]string{
		// sha256sum of the content written above.
		".claude/agents/gone.toml": "sha256:8201da4eee960804250182dd55436de61f94c2298cdbae62dbb71825cabf3eff",
		".claude/rules/edited.md":  "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot,
		[]string{".claude/agents/gone.toml", ".claude/rules/edited.md", ".claude/rules/unrecorded.md"}, hashes, nil)

	assertStaleLinkResult(t, removed, []string{".claude/agents/gone.toml"}, diags, 1)
	if len(diags) == 1 && diags[0] != `keeping ".claude/rules/edited.md": modified since deploy (hash mismatch)` {
		t.Errorf("diag = %q", diags[0])
	}
	assertStaleLinkGone(t, filepath.Join(dotfiles, "agents"))
	assertStaleLinkContent(t, filepath.Join(dotfiles, "rules", "edited.md"), "edited by the user\n")
	assertStaleLinkIsSymlink(t, filepath.Join(deployRoot, ".claude"))
}

func TestRemoveStaleLinkedFiles_UserParentSymlink_SurvivesWhenEmptied(t *testing.T) {
	for name, deployed := range map[string]func(t *testing.T, sourceRoot, full string){
		"symlink": func(t *testing.T, sourceRoot, full string) {
			staleLink(t, filepath.Join(sourceRoot, "agents", "gone.md"), full)
		},
		"regular file": func(t *testing.T, _, full string) {
			writeStaleLinkFile(t, full, "gone\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			deployRoot, sourceRoot := staleLinkRoots(t)
			dotfiles := staleLinkUserDir(t, deployRoot)
			deployed(t, sourceRoot, filepath.Join(dotfiles, "agents", "gone.md"))

			removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/agents/gone.md"},
				map[string]string{".claude/agents/gone.md": staleLinkGoneHash}, nil)

			assertStaleLinkResult(t, removed, []string{".claude/agents/gone.md"}, diags, 0)
			assertStaleLinkGone(t, filepath.Join(dotfiles, "agents"))
			assertStaleLinkIsSymlink(t, filepath.Join(deployRoot, ".claude"))
			if info, err := os.Stat(dotfiles); err != nil || !info.IsDir() {
				t.Errorf("the directory behind the user's symlink is gone (err = %v)", err)
			}
		})
	}
}

func TestRemoveStaleLinkedFiles_ReportsSymlinkThatCannotBeRemovedOnce(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory that refuses a delete: Windows ignores the write bit of a directory and root is not bound by it")
	}
	deployRoot, sourceRoot := staleLinkRoots(t)
	writeStaleLinkFile(t, filepath.Join(sourceRoot, "skills", "demo", "extra.md"), "extra\n")
	skills := filepath.Join(deployRoot, ".claude", "skills")
	staleLink(t, filepath.Join(sourceRoot, "skills", "demo"), filepath.Join(skills, "demo"))
	if err := os.Chmod(skills, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(skills, 0o755) })

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot,
		[]string{".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra.md"},
		map[string]string{".claude/skills/demo/SKILL.md": staleLinkDemoHash, ".claude/skills/demo/extra.md": staleLinkExtraHash}, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	if len(diags) == 1 && !strings.HasPrefix(diags[0], `keeping ".claude/skills/demo": failed to remove: `) {
		t.Errorf("diag = %q", diags[0])
	}
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "skills", "demo", "extra.md"), "extra\n")
}

// The target of the parent symlink is written through another name of
// sourceRoot, so by its text it is outside sourceRoot. The file behind it is
// still source content and its hash matches the lock.
func TestRemoveStaleLinkedFiles_KeepsSourceFileBehindSymlinkWrittenThroughAnotherName(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	alias := filepath.Join(t.TempDir(), "alias")
	staleLink(t, sourceRoot, alias)
	parent := filepath.Join(deployRoot, ".claude", "skills", "demo")
	staleLink(t, filepath.Join(alias, "skills", "demo"), parent)

	// sha256sum of "demo\n".
	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/skills/demo/SKILL.md"},
		map[string]string{".claude/skills/demo/SKILL.md": "sha256:eb9c26baee47f19e4993a77bca936d0ff09e355a82d3db79bf154ebff1a80604"}, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	want := `keeping ".claude/skills/demo": symlink target "` + filepath.Join(alias, "skills", "demo") + `" is not in the source of this package`
	if len(diags) == 1 && diags[0] != want {
		t.Errorf("diag = %q, want %q", diags[0], want)
	}
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "skills", "demo", "SKILL.md"), "demo\n")
	assertStaleLinkIsSymlink(t, parent)
}

func TestRemoveStaleLinkedFiles_KeepsPathItCannotInspect(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory that cannot be read: Windows ignores the permission bits and root is not bound by them")
	}
	deployRoot, sourceRoot := staleLinkRoots(t)
	agents := filepath.Join(deployRoot, ".claude", "agents")
	staleLink(t, filepath.Join(sourceRoot, "agents", "gone.md"), filepath.Join(agents, "gone.md"))
	if err := os.Chmod(agents, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(agents, 0o755) })

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/agents/gone.md"}, nil, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	if len(diags) == 1 && (!strings.HasPrefix(diags[0], `keeping ".claude/agents/gone.md": `) || !strings.HasSuffix(diags[0], "permission denied")) {
		t.Errorf("diag = %q", diags[0])
	}
	if err := os.Chmod(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	assertStaleLinkIsSymlink(t, filepath.Join(agents, "gone.md"))
}

func TestRemoveStaleLinkedFiles_KeepsSymlinkThatIsItselfClaimed(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	link := filepath.Join(deployRoot, ".claude", "skills", "demo")
	staleLink(t, filepath.Join(sourceRoot, "skills", "demo"), link)

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/skills/demo/SKILL.md"}, nil,
		map[string]bool{".claude/skills/demo": true})

	assertStaleLinkResult(t, removed, nil, diags, 0)
	assertStaleLinkContent(t, filepath.Join(link, "SKILL.md"), "demo\n")
}

func TestRemoveStaleLinkedFiles_UserParentSymlink_KeepsFileWhenSourceRootCannotBeResolved(t *testing.T) {
	deployRoot, _ := staleLinkRoots(t)
	dotfiles := staleLinkUserDir(t, deployRoot)
	writeStaleLinkFile(t, filepath.Join(dotfiles, "agents", "gone.toml"), "name = \"gone\"\n")

	// sha256sum of the content written above: the hash check would pass.
	missing := filepath.Join(t.TempDir(), "missing")
	removed, diags := RemoveStaleLinkedFiles(deployRoot, missing, missing, []string{".claude/agents/gone.toml"},
		map[string]string{".claude/agents/gone.toml": "sha256:8201da4eee960804250182dd55436de61f94c2298cdbae62dbb71825cabf3eff"}, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	if len(diags) == 1 && diags[0] != `keeping ".claude/agents/gone.toml": it is a file in the apm project directory, reached through a symlink` {
		t.Errorf("diag = %q", diags[0])
	}
	assertStaleLinkContent(t, filepath.Join(dotfiles, "agents", "gone.toml"), "name = \"gone\"\n")
}

// The install command passes "." as the project directory. When the working
// directory was entered through a symlink, filepath.Abs(".") keeps that
// spelling, and a file reached with the real spelling must still be seen as
// inside the project directory.
func TestRemoveStaleLinkedFiles_KeepsSourceFileWhenWorkingDirIsEnteredThroughSymlink(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(base, "real", "apm", "agents", "x.md")
	writeStaleLinkFile(t, source, "demo\n")
	staleLink(t, filepath.Join(base, "real"), filepath.Join(base, "link"))
	t.Chdir(filepath.Join(base, "link", "apm"))
	if abs, err := filepath.Abs("."); err != nil || abs != filepath.Join(base, "link", "apm") {
		t.Skipf("filepath.Abs(\".\") = %q, %v: this platform does not keep the symlink spelling of the working directory", abs, err)
	}
	deployRoot := filepath.Join(base, "home")
	parent := filepath.Join(deployRoot, ".claude", "agents", "sub")
	staleLink(t, filepath.Join(base, "real", "apm", "agents"), parent)

	// sha256sum of "demo\n": the hash check would pass.
	removed, diags := RemoveStaleLinkedFiles(deployRoot, ".", ".", []string{".claude/agents/sub/x.md"},
		map[string]string{".claude/agents/sub/x.md": "sha256:eb9c26baee47f19e4993a77bca936d0ff09e355a82d3db79bf154ebff1a80604"}, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	if len(diags) == 1 && diags[0] != `keeping ".claude/agents/sub/x.md": it is a file in the apm project directory, reached through a symlink` {
		t.Errorf("diag = %q", diags[0])
	}
	assertStaleLinkContent(t, source, "demo\n")
}

// A target root is made by os.MkdirAll, never by a deploy symlink, so a
// symlink there is the user's also when it leads into sourceRoot.
func TestRemoveStaleLinkedFiles_TargetRootSymlinkIntoSourceRootIsUsers(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	dotfiles := filepath.Join(sourceRoot, "dotfiles", "claude")
	staleLink(t, filepath.Join(sourceRoot, "agents", "deleted.md"), filepath.Join(dotfiles, "agents", "gone.md"))
	staleLink(t, dotfiles, filepath.Join(deployRoot, ".claude"))

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/agents/gone.md"}, nil, nil)

	assertStaleLinkResult(t, removed, []string{".claude/agents/gone.md"}, diags, 0)
	assertStaleLinkIsSymlink(t, filepath.Join(deployRoot, ".claude"))
	assertStaleLinkGone(t, filepath.Join(dotfiles, "agents"))
	if info, err := os.Stat(dotfiles); err != nil || !info.IsDir() {
		t.Errorf("the directory behind the user's symlink is gone (err = %v)", err)
	}
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "agents", "keep.md"), "keep\n")
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "agents", "gone.md"), "gone\n")
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "skills", "demo", "SKILL.md"), "demo\n")
}

func TestRemoveStaleLinkedFiles_NestedTargetRootSymlinkIntoSourceRootIsUsers(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	shared := filepath.Join(sourceRoot, "dotfiles", "skills")
	staleLink(t, filepath.Join(sourceRoot, "skills", "demo"), filepath.Join(shared, "demo"))
	staleLink(t, shared, filepath.Join(deployRoot, ".agents", "skills"))

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".agents/skills/demo/SKILL.md"},
		map[string]string{".agents/skills/demo/SKILL.md": staleLinkDemoHash}, nil)

	assertStaleLinkResult(t, removed, []string{".agents/skills/demo"}, diags, 0)
	assertStaleLinkIsSymlink(t, filepath.Join(deployRoot, ".agents", "skills"))
	assertStaleLinkGone(t, filepath.Join(shared, "demo"))
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "skills", "demo", "SKILL.md"), "demo\n")
}

func TestRemoveStaleLinkedFiles_SkillRootSymlinkIntoSourceRootIsUsers(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	shared := filepath.Join(sourceRoot, "dotfiles", "skills")
	staleLink(t, filepath.Join(sourceRoot, "skills", "demo"), filepath.Join(shared, "demo"))
	staleLink(t, shared, filepath.Join(deployRoot, ".claude", "skills"))

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/skills/demo/SKILL.md"},
		map[string]string{".claude/skills/demo/SKILL.md": staleLinkDemoHash}, nil)

	assertStaleLinkResult(t, removed, []string{".claude/skills/demo"}, diags, 0)
	assertStaleLinkIsSymlink(t, filepath.Join(deployRoot, ".claude", "skills"))
	assertStaleLinkGone(t, filepath.Join(shared, "demo"))
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "skills", "demo", "SKILL.md"), "demo\n")
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "agents", "keep.md"), "keep\n")
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "agents", "gone.md"), "gone\n")
}

func TestRemoveStaleLinkedFiles_RemovesSkillSymlinkInBundle(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	link := filepath.Join(deployRoot, ".agents", "plugins", "pkg", "skills", "demo")
	staleLink(t, filepath.Join(sourceRoot, "skills", "demo"), link)

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".agents/plugins/pkg/skills/demo/SKILL.md"},
		map[string]string{".agents/plugins/pkg/skills/demo/SKILL.md": staleLinkDemoHash}, nil)

	assertStaleLinkResult(t, removed, []string{".agents/plugins/pkg/skills/demo"}, diags, 0)
	assertStaleLinkGone(t, link)
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "skills", "demo", "SKILL.md"), "demo\n")
}

// A parent symlink that is not a skill directory is the user's, also below a
// target root and with a target inside sourceRoot.
func TestRemoveStaleLinkedFiles_ParentSymlinkThatIsNotSkillDirIsUsers(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	sub := filepath.Join(deployRoot, ".claude", "agents", "sub")
	staleLink(t, filepath.Join(sourceRoot, "agents"), sub)

	// sha256sum of "gone\n": the hash check would pass.
	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/agents/sub/gone.md"},
		map[string]string{".claude/agents/sub/gone.md": "sha256:4b9f2c32577beb1ebc8ab2a1e226faaa9176a81cd4eedbaa22f8a0db919972b5"}, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	if len(diags) == 1 && diags[0] != `keeping ".claude/agents/sub/gone.md": it is a file in the apm project directory, reached through a symlink` {
		t.Errorf("diag = %q", diags[0])
	}
	assertStaleLinkIsSymlink(t, sub)
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "agents", "gone.md"), "gone\n")
}

// The symlink leads into the project directory, but into the source of
// another package: this bucket's deploy did not make it.
func TestRemoveStaleLinkedFiles_KeepsSymlinkIntoSourceOfAnotherBucket(t *testing.T) {
	deployRoot, projectDir := staleLinkRoots(t)
	other := filepath.Join(projectDir, "apm_modules", "other", ".apm", "agents", "gone.md")
	writeStaleLinkFile(t, other, "other\n")
	link := filepath.Join(deployRoot, ".claude", "agents", "gone.md")
	staleLink(t, other, link)

	removed, diags := RemoveStaleLinkedFiles(deployRoot, projectDir, filepath.Join(projectDir, "apm_modules", "dep"),
		[]string{".claude/agents/gone.md"}, nil, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	want := `keeping ".claude/agents/gone.md": symlink target "` + other + `" is not in the source of this package`
	if len(diags) == 1 && diags[0] != want {
		t.Errorf("diag = %q, want %q", diags[0], want)
	}
	assertStaleLinkIsSymlink(t, link)
	assertStaleLinkContent(t, other, "other\n")
}

func TestRemoveStaleLinkedFiles_LocalBucketRemovesSymlinkIntoItsSource(t *testing.T) {
	deployRoot, projectDir := staleLinkRoots(t)
	link := filepath.Join(deployRoot, ".claude", "agents", "gone.md")
	staleLink(t, filepath.Join(projectDir, ".apm", "agents", "deleted.md"), link)

	removed, diags := RemoveStaleLinkedFiles(deployRoot, projectDir, filepath.Join(projectDir, ".apm"),
		[]string{".claude/agents/gone.md"}, nil, nil)

	assertStaleLinkResult(t, removed, []string{".claude/agents/gone.md"}, diags, 0)
	assertStaleLinkGone(t, link)
}

// A file reached through the user's parent symlink is kept when it is
// anywhere inside the project directory, not only inside the bucket's source.
func TestRemoveStaleLinkedFiles_UserParentSymlinkIntoAnotherPlaceOfProjectDir_KeepsFile(t *testing.T) {
	deployRoot, projectDir := staleLinkRoots(t)
	custom := filepath.Join(projectDir, "custom")
	writeStaleLinkFile(t, filepath.Join(custom, "x.md"), "demo\n")
	link := filepath.Join(deployRoot, ".claude", "agents", "sub")
	staleLink(t, custom, link)
	bucketSource := filepath.Join(projectDir, "apm_modules", "dep")
	if err := os.MkdirAll(bucketSource, 0o755); err != nil {
		t.Fatal(err)
	}

	// sha256sum of "demo\n": the hash check would pass.
	removed, diags := RemoveStaleLinkedFiles(deployRoot, projectDir, bucketSource,
		[]string{".claude/agents/sub/x.md", ".claude/agents/sub/missing.md"},
		map[string]string{".claude/agents/sub/x.md": "sha256:eb9c26baee47f19e4993a77bca936d0ff09e355a82d3db79bf154ebff1a80604"}, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	if len(diags) == 1 && diags[0] != `keeping ".claude/agents/sub/x.md": it is a file in the apm project directory, reached through a symlink` {
		t.Errorf("diag = %q", diags[0])
	}
	assertStaleLinkIsSymlink(t, link)
	assertStaleLinkContent(t, filepath.Join(custom, "x.md"), "demo\n")
}

func TestRemoveStaleLinkedFiles_TakenOverSkillDirKeepsEveryFileAndReportsOnce(t *testing.T) {
	deployRoot, projectDir := staleLinkRoots(t)
	mine := filepath.Join(t.TempDir(), "my-skills", "demo")
	writeStaleLinkFile(t, filepath.Join(mine, "SKILL.md"), "demo\n")
	writeStaleLinkFile(t, filepath.Join(mine, "extra.md"), "extra\n")
	link := filepath.Join(deployRoot, ".claude", "skills", "demo")
	staleLink(t, mine, link)

	// Both hashes are the sha256sum of the content: the hash check would pass.
	removed, diags := RemoveStaleLinkedFiles(deployRoot, projectDir, filepath.Join(projectDir, "skills"),
		[]string{".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra.md"},
		map[string]string{
			".claude/skills/demo/SKILL.md": "sha256:eb9c26baee47f19e4993a77bca936d0ff09e355a82d3db79bf154ebff1a80604",
			".claude/skills/demo/extra.md": "sha256:65110ea3b8b62b0c09742c368bf1527f0978b06dff7a1371ef7b4c98e244d91a",
		}, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	want := `keeping ".claude/skills/demo": symlink target "` + mine + `" is not in the source of this package`
	if len(diags) == 1 && diags[0] != want {
		t.Errorf("diag = %q, want %q", diags[0], want)
	}
	assertStaleLinkIsSymlink(t, link)
	assertStaleLinkContent(t, filepath.Join(mine, "SKILL.md"), "demo\n")
	assertStaleLinkContent(t, filepath.Join(mine, "extra.md"), "extra\n")
}

func TestRemoveStaleLinkedFiles_TakenOverSkillDirThatDanglesIsKept(t *testing.T) {
	deployRoot, projectDir := staleLinkRoots(t)
	missing := filepath.Join(t.TempDir(), "my-skills", "demo")
	link := filepath.Join(deployRoot, ".claude", "skills", "demo")
	staleLink(t, missing, link)

	removed, diags := RemoveStaleLinkedFiles(deployRoot, projectDir, filepath.Join(projectDir, "skills"),
		[]string{".claude/skills/demo/SKILL.md"}, nil, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	want := `keeping ".claude/skills/demo": symlink target "` + missing + `" is not in the source of this package`
	if len(diags) == 1 && diags[0] != want {
		t.Errorf("diag = %q, want %q", diags[0], want)
	}
	assertStaleLinkIsSymlink(t, link)
}

// staleLinkDemoSkill links .claude/skills/demo to the demo skill in
// sourceRoot, which then holds SKILL.md ("demo\n") and, when withExtra,
// extra.md ("extra\n").
func staleLinkDemoSkill(t *testing.T, deployRoot, sourceRoot string, withExtra bool) (link string) {
	t.Helper()
	if withExtra {
		writeStaleLinkFile(t, filepath.Join(sourceRoot, "skills", "demo", "extra.md"), "extra\n")
	}
	link = filepath.Join(deployRoot, ".claude", "skills", "demo")
	staleLink(t, filepath.Join(sourceRoot, "skills", "demo"), link)
	return link
}

func assertStaleLinkNotDeployed(t *testing.T, diags []string, lockPath, target string) {
	t.Helper()
	want := `keeping "` + lockPath + `": symlink target "` + target + `" does not hold what this package deployed`
	if len(diags) != 1 || diags[0] != want {
		t.Errorf("diags = %q, want [%q]", diags, want)
	}
}

func TestRemoveStaleLinkedFiles_KeepsPathSymlinkWithNoRecordedHash(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	link := filepath.Join(deployRoot, ".claude", "agents", "gone.md")
	target := filepath.Join(sourceRoot, "agents", "gone.md")
	staleLink(t, target, link)

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/agents/gone.md"}, nil, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	assertStaleLinkNotDeployed(t, diags, ".claude/agents/gone.md", target)
	assertStaleLinkIsSymlink(t, link)
}

// The user pointed the entry at another file of the same package.
func TestRemoveStaleLinkedFiles_KeepsPathSymlinkThatReadsOtherContent(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	link := filepath.Join(deployRoot, ".claude", "agents", "gone.md")
	target := filepath.Join(sourceRoot, "agents", "keep.md")
	staleLink(t, target, link)

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/agents/gone.md"},
		map[string]string{".claude/agents/gone.md": staleLinkGoneHash}, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	assertStaleLinkNotDeployed(t, diags, ".claude/agents/gone.md", target)
	assertStaleLinkIsSymlink(t, link)
	assertStaleLinkContent(t, target, "keep\n")
}

func TestRemoveStaleLinkedFiles_RemovesSkillSymlinkWhenOneStalePathIsMissing(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	link := staleLinkDemoSkill(t, deployRoot, sourceRoot, false)

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot,
		[]string{".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra.md"},
		map[string]string{".claude/skills/demo/SKILL.md": staleLinkDemoHash, ".claude/skills/demo/extra.md": staleLinkExtraHash}, nil)

	assertStaleLinkResult(t, removed, []string{".claude/skills/demo"}, diags, 0)
	assertStaleLinkGone(t, link)
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "skills", "demo", "SKILL.md"), "demo\n")
}

func TestRemoveStaleLinkedFiles_KeepsSkillSymlinkWhenOneStalePathReadsOtherContent(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	link := staleLinkDemoSkill(t, deployRoot, sourceRoot, true)

	// extra.md is recorded with the hash of "demo\n", not of its content.
	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot,
		[]string{".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra.md"},
		map[string]string{".claude/skills/demo/SKILL.md": staleLinkDemoHash, ".claude/skills/demo/extra.md": staleLinkDemoHash}, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	assertStaleLinkNotDeployed(t, diags, ".claude/skills/demo", filepath.Join(sourceRoot, "skills", "demo"))
	assertStaleLinkIsSymlink(t, link)
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "skills", "demo", "SKILL.md"), "demo\n")
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "skills", "demo", "extra.md"), "extra\n")
}

func TestRemoveStaleLinkedFiles_KeepsSkillSymlinkWhenNoStalePathExists(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	link := staleLinkDemoSkill(t, deployRoot, sourceRoot, false)

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot,
		[]string{".claude/skills/demo/old.md", ".claude/skills/demo/older.md"},
		map[string]string{".claude/skills/demo/old.md": staleLinkDemoHash, ".claude/skills/demo/older.md": staleLinkDemoHash}, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	assertStaleLinkNotDeployed(t, diags, ".claude/skills/demo", filepath.Join(sourceRoot, "skills", "demo"))
	assertStaleLinkIsSymlink(t, link)
}

func TestRemoveStaleLinkedFiles_KeepsSymlinkWhoseTargetCannotBeResolved(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	loop := filepath.Join(sourceRoot, "agents", "loop.md")
	staleLink(t, loop, loop)
	link := filepath.Join(deployRoot, ".claude", "agents", "gone.md")
	staleLink(t, loop, link)

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/agents/gone.md"},
		map[string]string{".claude/agents/gone.md": staleLinkGoneHash}, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	if len(diags) == 1 && (!strings.HasPrefix(diags[0], `keeping ".claude/agents/gone.md": `) || strings.Contains(diags[0], "does not hold")) {
		t.Errorf("diag = %q, want the error of resolving the target", diags[0])
	}
	assertStaleLinkIsSymlink(t, link)
}

func TestRemoveStaleLinkedFiles_KeepsSymlinkWhoseContentCannotBeRead(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	link := filepath.Join(deployRoot, ".claude", "agents", "gone.md")
	staleLink(t, filepath.Join(sourceRoot, "agents"), link)

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot, []string{".claude/agents/gone.md"},
		map[string]string{".claude/agents/gone.md": staleLinkGoneHash}, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	if len(diags) == 1 && (!strings.HasPrefix(diags[0], `keeping ".claude/agents/gone.md": hash file `) || strings.Contains(diags[0], "does not hold")) {
		t.Errorf("diag = %q, want the error of reading the content", diags[0])
	}
	assertStaleLinkIsSymlink(t, link)
	assertStaleLinkContent(t, filepath.Join(sourceRoot, "agents", "keep.md"), "keep\n")
}

// A sibling whose name starts with the skill's name is not below the skill
// symlink: its content does not count in the proof.
func TestRemoveStaleLinkedFiles_ProofIgnoresSiblingWithSameNamePrefix(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	link := staleLinkDemoSkill(t, deployRoot, sourceRoot, false)
	sibling := filepath.Join(deployRoot, ".claude", "skills", "demo-two", "SKILL.md")
	writeStaleLinkFile(t, sibling, "edited by the user\n")

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, sourceRoot,
		[]string{".claude/skills/demo/SKILL.md", ".claude/skills/demo-two/SKILL.md"},
		map[string]string{".claude/skills/demo/SKILL.md": staleLinkDemoHash, ".claude/skills/demo-two/SKILL.md": staleLinkDemoHash}, nil)

	assertStaleLinkResult(t, removed, []string{".claude/skills/demo"}, diags, 1)
	if len(diags) == 1 && diags[0] != `keeping ".claude/skills/demo-two/SKILL.md": modified since deploy (hash mismatch)` {
		t.Errorf("diag = %q", diags[0])
	}
	assertStaleLinkGone(t, link)
	assertStaleLinkContent(t, sibling, "edited by the user\n")
}
