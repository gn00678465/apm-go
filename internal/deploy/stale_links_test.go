package deploy

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
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

	// No hash is recorded: a symlink is removed without a hash comparison.
	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, []string{".claude/agents/gone.md"}, nil, nil)

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

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, []string{".claude/agents/gone.md"}, nil,
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

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot,
		[]string{".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra.md"}, nil, claimed)

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
	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, []string{".claude/skills/demo/extra.md"},
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

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot,
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

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot,
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

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, []string{".claude/agents/gone.md"}, nil, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	if len(diags) == 1 && !strings.HasPrefix(diags[0], `keeping ".claude/agents/gone.md": symlink target `) {
		t.Errorf("diag = %q", diags[0])
	}
	assertStaleLinkIsSymlink(t, leaf)
	assertStaleLinkContent(t, leaf, "mine\n")
}

// A parent symlink the user made is walked through, so a file behind it gets
// the hash check of any other regular file.
func TestRemoveStaleLinkedFiles_UserParentSymlink_KeepsFileWithOtherHash(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	outside := filepath.Join(t.TempDir(), "mine")
	writeStaleLinkFile(t, filepath.Join(outside, "skill", "SKILL.md"), "my skill\n")
	parent := filepath.Join(deployRoot, ".claude", "skills", "demo")
	staleLink(t, filepath.Join(outside, "skill"), parent)

	// The recorded hash is the sha256sum of "demo\n", the deployed content.
	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot,
		[]string{".claude/skills/demo/SKILL.md", ".claude/skills/demo/more.md"},
		map[string]string{".claude/skills/demo/SKILL.md": "sha256:eb9c26baee47f19e4993a77bca936d0ff09e355a82d3db79bf154ebff1a80604"}, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	if len(diags) == 1 && diags[0] != `keeping ".claude/skills/demo/SKILL.md": modified since deploy (hash mismatch)` {
		t.Errorf("diag = %q", diags[0])
	}
	assertStaleLinkIsSymlink(t, parent)
	assertStaleLinkContent(t, filepath.Join(parent, "SKILL.md"), "my skill\n")
}

func TestRemoveStaleLinkedFiles_KeepsDeployRootAndNonEmptyParents(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	staleLink(t, filepath.Join(sourceRoot, "agents", "gone.md"), filepath.Join(deployRoot, ".claude", "agents", "gone.md"))
	writeStaleLinkFile(t, filepath.Join(deployRoot, ".claude", "settings.json"), "{}\n")

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, []string{".claude/agents/gone.md"}, nil, nil)

	assertStaleLinkResult(t, removed, []string{".claude/agents/gone.md"}, diags, 0)
	assertStaleLinkGone(t, filepath.Join(deployRoot, ".claude", "agents"))
	assertStaleLinkContent(t, filepath.Join(deployRoot, ".claude", "settings.json"), "{}\n")
}

func TestRemoveStaleLinkedFiles_PathEscapingDeployRoot(t *testing.T) {
	deployRoot, sourceRoot := staleLinkRoots(t)
	victim := filepath.Join(filepath.Dir(deployRoot), "victim.md")
	writeStaleLinkFile(t, victim, "victim\n")

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, []string{"../victim.md"}, nil, nil)

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

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, []string{".claude/agents/gone.md"}, nil,
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
	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, []string{".claude/skills/demo/SKILL.md"},
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

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot,
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
			writeStaleLinkFile(t, full, "name = \"gone\"\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			deployRoot, sourceRoot := staleLinkRoots(t)
			dotfiles := staleLinkUserDir(t, deployRoot)
			deployed(t, sourceRoot, filepath.Join(dotfiles, "agents", "gone.md"))

			removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, []string{".claude/agents/gone.md"},
				map[string]string{".claude/agents/gone.md": "sha256:8201da4eee960804250182dd55436de61f94c2298cdbae62dbb71825cabf3eff"}, nil)

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

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot,
		[]string{".claude/skills/demo/SKILL.md", ".claude/skills/demo/extra.md"}, nil, nil)

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
	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, []string{".claude/skills/demo/SKILL.md"},
		map[string]string{".claude/skills/demo/SKILL.md": "sha256:eb9c26baee47f19e4993a77bca936d0ff09e355a82d3db79bf154ebff1a80604"}, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	if len(diags) == 1 && !strings.HasPrefix(diags[0], `keeping ".claude/skills/demo/SKILL.md": `) {
		t.Errorf("diag = %q", diags[0])
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

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, []string{".claude/agents/gone.md"}, nil, nil)

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

	removed, diags := RemoveStaleLinkedFiles(deployRoot, sourceRoot, []string{".claude/skills/demo/SKILL.md"}, nil,
		map[string]bool{".claude/skills/demo": true})

	assertStaleLinkResult(t, removed, nil, diags, 0)
	assertStaleLinkContent(t, filepath.Join(link, "SKILL.md"), "demo\n")
}

func TestRemoveStaleLinkedFiles_UserParentSymlink_KeepsFileWhenSourceRootCannotBeResolved(t *testing.T) {
	deployRoot, _ := staleLinkRoots(t)
	dotfiles := staleLinkUserDir(t, deployRoot)
	writeStaleLinkFile(t, filepath.Join(dotfiles, "agents", "gone.toml"), "name = \"gone\"\n")

	// sha256sum of the content written above: the hash check would pass.
	removed, diags := RemoveStaleLinkedFiles(deployRoot, filepath.Join(t.TempDir(), "missing"), []string{".claude/agents/gone.toml"},
		map[string]string{".claude/agents/gone.toml": "sha256:8201da4eee960804250182dd55436de61f94c2298cdbae62dbb71825cabf3eff"}, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	if len(diags) == 1 && diags[0] != `keeping ".claude/agents/gone.toml": it is a file in the apm project directory, reached through a symlink` {
		t.Errorf("diag = %q", diags[0])
	}
	assertStaleLinkContent(t, filepath.Join(dotfiles, "agents", "gone.toml"), "name = \"gone\"\n")
}

// The install command passes "." as sourceRoot. When the working directory
// was entered through a symlink, filepath.Abs(".") keeps that spelling, and a
// deploy symlink written with the real spelling looks like the user's.
func TestRemoveStaleLinkedFiles_KeepsSourceFileWhenWorkingDirIsEnteredThroughSymlink(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(base, "real", "apm", "skills", "demo", "SKILL.md")
	writeStaleLinkFile(t, source, "demo\n")
	staleLink(t, filepath.Join(base, "real"), filepath.Join(base, "link"))
	t.Chdir(filepath.Join(base, "link", "apm"))
	if abs, err := filepath.Abs("."); err != nil || abs != filepath.Join(base, "link", "apm") {
		t.Skipf("filepath.Abs(\".\") = %q, %v: this platform does not keep the symlink spelling of the working directory", abs, err)
	}
	deployRoot := filepath.Join(base, "home")
	parent := filepath.Join(deployRoot, ".claude", "skills", "demo")
	staleLink(t, filepath.Join(base, "real", "apm", "skills", "demo"), parent)

	// sha256sum of "demo\n": the hash check would pass.
	removed, diags := RemoveStaleLinkedFiles(deployRoot, ".", []string{".claude/skills/demo/SKILL.md"},
		map[string]string{".claude/skills/demo/SKILL.md": "sha256:eb9c26baee47f19e4993a77bca936d0ff09e355a82d3db79bf154ebff1a80604"}, nil)

	assertStaleLinkResult(t, removed, nil, diags, 1)
	if len(diags) == 1 && diags[0] != `keeping ".claude/skills/demo/SKILL.md": it is a file in the apm project directory, reached through a symlink` {
		t.Errorf("diag = %q", diags[0])
	}
	assertStaleLinkContent(t, source, "demo\n")
}
