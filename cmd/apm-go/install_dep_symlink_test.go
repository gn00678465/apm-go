package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
)

// Issue #89: a symlink in a dependency is followed only while it stays in the
// package. One that leaves it, dangles or loops fails the command with exit 1
// before anything is deployed; the project's own .apm/ is read as before.

const symlinkDepAgent = "---\nname: oracle\ndescription: d\n---\nagent body\n"

// symlinkDepProject changes to a new project directory, symlinks resolved,
// with the files outside every package that the fixtures point at.
func symlinkDepProject(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(chdirTemp(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	if err := os.Symlink("x", "probe"); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}
	if err := os.Remove("probe"); err != nil {
		t.Fatal(err)
	}
	for rel, content := range map[string]string{
		"outside/outer.agent.md":                        "SECRET-AGENT\n",
		"outside/outer.instructions.md":                 "SECRET-INSTR\n",
		"outside/outer.prompt.md":                       "SECRET-PROMPT\n",
		"outside/outer.md":                              "SECRET-CMD\n",
		"outside/outer.json":                            `{"hooks":{"SECRET":[]}}` + "\n",
		"outside/agentsdir/d.agent.md":                  "SECRET-DIR\n",
		"outside/apmdir/agents/a.agent.md":              "SECRET-APM\n",
		"outside/apmdir/instructions/c.instructions.md": "SECRET-COMPILE\n",
		"outside/secret.txt":                            "SECRET-SKILLFILE\n",
		"outside/plugin.json":                           `{"name":"evil","agents":["./agents"]}` + "\n",
		"outside/apm.yml":                               "name: dep\nversion: 1.0.0\nmcp:\n  - name: secret-mcp\n    registry: false\n    transport: stdio\n    command: SECRET-MCP-CMD\n",
		"outside/pkgdir/.apm/agents/p.agent.md":         "SECRET-SUBDIR\n",
	} {
		writeGlobalStaleFile(t, filepath.Join(dir, filepath.FromSlash(rel)), content)
	}
	return dir
}

// symlinkDepRepo makes dir a git repository whose one commit holds files and
// symlinks (path -> link text), and returns the commit.
func symlinkDepRepo(t *testing.T, dir string, files, links map[string]string) string {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "apm.yml")); os.IsNotExist(err) && links["apm.yml"] == "" && files["apm.yml"] == "" {
		writeGlobalStaleFile(t, filepath.Join(dir, "apm.yml"), "name: dep\nversion: 1.0.0\n")
	}
	for rel, content := range files {
		writeGlobalStaleFile(t, filepath.Join(dir, filepath.FromSlash(rel)), content)
	}
	for rel, target := range links {
		link := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		gitCmd(t, dir, "init")
	}
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "commit")
	return gitCmd(t, dir, "rev-parse", "HEAD")
}

func writeSymlinkDepManifest(t *testing.T, deps string) {
	t.Helper()
	writeGlobalStaleFile(t, "apm.yml", "name: e\nversion: 1.0.0\ndependencies:\n  apm:\n"+deps)
}

// writeSymlinkGitDep declares the repository in dir, below the working
// directory, as the git dependency acme/<dir>, cloned from there through the
// link acme/<dir> (useLocalGitRemotes).
func writeSymlinkGitDep(t *testing.T, dir string) {
	t.Helper()
	linkGitRemote(t, ".", dir)
	writeSymlinkDepManifest(t, "    - acme/"+dir+"\n")
}

// writeGlobalGitDep declares the repository <root>/.apm/repo in
// <root>/.apm/apm.yml as the git dependency acme/repo.
func writeGlobalGitDep(t *testing.T, root string) {
	t.Helper()
	linkGitRemote(t, filepath.Join(root, ".apm"), "repo")
	writeGlobalStaleFile(t, filepath.Join(root, ".apm", "apm.yml"), "name: global\nversion: 0.0.0\ndependencies:\n  apm:\n    - acme/repo\n")
}

// linkGitRemote makes <base>/acme/<dir> a link to the repository <base>/<dir>,
// the path subdirPathLoader clones acme/<dir> from.
func linkGitRemote(t *testing.T, base, dir string) {
	t.Helper()
	useLocalGitRemotes(t)
	link := filepath.Join(base, "acme", dir)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); err == nil {
		return
	}
	if err := os.Symlink(filepath.Join("..", dir), link); err != nil {
		t.Fatal(err)
	}
}

// treeOutsideModules records every path below dir except apm_modules/ (also
// ~/.apm/apm_modules/ of a HOME): a
// file's content hash, a symlink's link text, a directory.
func treeOutsideModules(t *testing.T, dir string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.IsDir() && d.Name() == "apm_modules":
			return filepath.SkipDir
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			tree[rel] = "-> " + target
			return err
		case d.IsDir():
			tree[rel] = "dir"
		default:
			data, err := os.ReadFile(p)
			sum := sha256.Sum256(data)
			tree[rel] = hex.EncodeToString(sum[:])
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func assertSameTree(t *testing.T, before, after map[string]string) {
	t.Helper()
	for p, v := range after {
		if before[p] != v {
			t.Errorf("%s: before %q, after %q", p, before[p], v)
		}
	}
	for p, v := range before {
		if _, ok := after[p]; !ok {
			t.Errorf("%s: before %q, gone after", p, v)
		}
	}
}

// assertFailsBeforeWriting runs apm-go args and wants exit 1 with wantLine as
// the last line of stdout, and no change outside apm_modules/ below root.
func assertFailsBeforeWriting(t *testing.T, root, wantLine string, args ...string) {
	t.Helper()
	before := treeOutsideModules(t, root)
	stdout, stderr, code := runCompileCLI(t, args...)
	if code != 1 {
		t.Errorf("exit code %d, want 1\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if got := lines[len(lines)-1]; got != wantLine {
		t.Errorf("last line\n got %q\nwant %q\nstdout:\n%s", got, wantLine, stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want none", stderr)
	}
	assertSameTree(t, before, treeOutsideModules(t, root))
}

func mustRun(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, code := runCompileCLI(t, args...)
	if code != 0 {
		t.Fatalf("apm-go %s: exit %d\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), code, stdout, stderr)
	}
	return stdout
}

func readText(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func sha256Of(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Test 1, investigation table A: a file symlink out of the package in each
// .apm/<type> directory.
func TestInstall_DependencyFileSymlinkOutsideFails(t *testing.T) {
	for _, tt := range []struct{ subdir, name string }{
		{"agents", "outer.agent.md"},
		{"instructions", "outer.instructions.md"},
		{"prompts", "outer.prompt.md"},
		{"commands", "outer.md"},
		{"hooks", "outer.json"},
	} {
		t.Run(tt.subdir, func(t *testing.T) {
			dir := symlinkDepProject(t)
			outer := filepath.Join(dir, "outside", tt.name)
			symlinkDepRepo(t, "repo", map[string]string{"agents/oracle.agent.md": symlinkDepAgent},
				map[string]string{".apm/" + tt.subdir + "/" + tt.name: outer})
			writeSymlinkGitDep(t, "repo")

			assertFailsBeforeWriting(t, dir,
				" x deploy: dependency acme/repo: symlink .apm/"+tt.subdir+"/"+tt.name+" resolves to "+outer+", outside the package",
				"install", "--target", "claude,copilot")
		})
	}
}

// Test 2, tables B and C: .apm/agents or .apm itself is a directory symlink
// out of the package.
func TestInstall_DependencyDirSymlinkOutsideFails(t *testing.T) {
	for _, tt := range []struct{ link, target string }{
		{".apm/agents", "agentsdir"},
		{".apm", "apmdir"},
	} {
		t.Run(tt.link, func(t *testing.T) {
			dir := symlinkDepProject(t)
			target := filepath.Join(dir, "outside", tt.target)
			symlinkDepRepo(t, "repo", map[string]string{}, map[string]string{tt.link: target})
			writeSymlinkGitDep(t, "repo")

			assertFailsBeforeWriting(t, dir,
				" x deploy: dependency acme/repo: symlink "+tt.link+" resolves to "+target+", outside the package",
				"install", "--target", "claude")
		})
	}
}

// Test 3: a dangling symlink, a symlink loop, and a directory symlink back to
// the directory it is in.
func TestInstall_DependencyDanglingOrLoopingSymlinkFails(t *testing.T) {
	for _, tt := range []struct {
		name  string
		links map[string]string
		files map[string]string
		want  string
	}{
		{
			name:  "dangling",
			links: map[string]string{".apm/agents/gone.agent.md": "../../missing.agent.md"},
			want:  "symlink .apm/agents/gone.agent.md -> ../../missing.agent.md does not resolve: no such file or directory",
		},
		{
			name:  "loop",
			links: map[string]string{".apm/agents/a.agent.md": "b.agent.md", ".apm/agents/b.agent.md": "a.agent.md"},
			want:  "symlink .apm/agents/a.agent.md is a symlink loop",
		},
		{
			name:  "directory cycle",
			files: map[string]string{".apm/skills/s/SKILL.md": "---\nname: s\ndescription: d\n---\ns\n"},
			links: map[string]string{".apm/skills/s/self": "."},
			want:  "symlink .apm/skills/s/self resolves to <modules>/.apm/skills/s, a directory it is in (symlink loop)",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := symlinkDepProject(t)
			files := tt.files
			if files == nil {
				files = map[string]string{}
			}
			symlinkDepRepo(t, "repo", files, tt.links)
			writeSymlinkGitDep(t, "repo")

			want := strings.ReplaceAll(tt.want, "<modules>", filepath.Join(dir, "apm_modules", "acme", "repo"))
			assertFailsBeforeWriting(t, dir, " x deploy: dependency acme/repo: "+want, "install", "--target", "claude")
		})
	}
}

// Test 4: a file symlink and a directory symlink that stay in the package are
// deployed as what they point at, under the symlink's own name, also to codex.
func TestInstall_DependencyInPackageSymlinksAreDeployed(t *testing.T) {
	t.Run("file", func(t *testing.T) {
		symlinkDepProject(t)
		symlinkDepRepo(t, "repo", map[string]string{"agents/oracle.agent.md": symlinkDepAgent},
			map[string]string{".apm/agents/inpkg.agent.md": "../../agents/oracle.agent.md"})
		writeSymlinkGitDep(t, "repo")

		mustRun(t, "install", "--target", "claude,codex")

		if got := readText(t, ".claude/agents/inpkg.md"); got != symlinkDepAgent {
			t.Errorf(".claude/agents/inpkg.md = %q, want %q", got, symlinkDepAgent)
		}
		const wantTOML = "name = 'oracle'\ndescription = 'd'\ndeveloper_instructions = 'agent body'\n"
		if got := readText(t, ".codex/agents/inpkg.toml"); got != wantTOML {
			t.Errorf(".codex/agents/inpkg.toml = %q, want %q", got, wantTOML)
		}
		dep := subdirLockEntry(t, ".", "acme/repo")
		if got := dep.DeployedHashes[".claude/agents/inpkg.md"]; got != sha256Of(symlinkDepAgent) {
			t.Errorf("lock hash of .claude/agents/inpkg.md = %q, want %q", got, sha256Of(symlinkDepAgent))
		}
		if got := dep.DeployedHashes[".codex/agents/inpkg.toml"]; got != sha256Of(wantTOML) {
			t.Errorf("lock hash of .codex/agents/inpkg.toml = %q, want %q", got, sha256Of(wantTOML))
		}
	})
	t.Run("directory", func(t *testing.T) {
		symlinkDepProject(t)
		symlinkDepRepo(t, "repo", map[string]string{"agents/oracle.agent.md": symlinkDepAgent},
			map[string]string{".apm/agents": "../agents"})
		writeSymlinkGitDep(t, "repo")

		mustRun(t, "install", "--target", "claude")

		if got := readText(t, ".claude/agents/oracle.md"); got != symlinkDepAgent {
			t.Errorf(".claude/agents/oracle.md = %q, want %q", got, symlinkDepAgent)
		}
		dep := subdirLockEntry(t, ".", "acme/repo")
		if got := dep.DeployedHashes[".claude/agents/oracle.md"]; got != sha256Of(symlinkDepAgent) {
			t.Errorf("lock hash of .claude/agents/oracle.md = %q, want %q", got, sha256Of(symlinkDepAgent))
		}
	})
}

const symlinkDepSkill = "---\nname: linked\ndescription: d\n---\nskill\n"

// symlinkDepSkillRepo is a package with a skill directory that is a symlink
// within it, and a skill holding a file symlink within it.
func symlinkDepSkillRepo(t *testing.T, dir string) {
	t.Helper()
	symlinkDepRepo(t, dir, map[string]string{
		"realskills/linked/SKILL.md": symlinkDepSkill,
		"skills/s/SKILL.md":          "---\nname: s\ndescription: d\n---\ns\n",
		"docs/ref.md":                "reference\n",
	}, map[string]string{
		"skills/linked":   "../realskills/linked",
		"skills/s/ref.md": "../../docs/ref.md",
	})
}

// Test 5: skills, copied to the project and linked by --global.
func TestInstall_DependencySkillSymlinks(t *testing.T) {
	t.Run("copy", func(t *testing.T) {
		symlinkDepProject(t)
		symlinkDepSkillRepo(t, "repo")
		writeSymlinkGitDep(t, "repo")

		mustRun(t, "install", "--target", "claude")

		for p, want := range map[string]string{
			".claude/skills/linked/SKILL.md": symlinkDepSkill,
			".claude/skills/s/ref.md":        "reference\n",
		} {
			if got := readText(t, p); got != want {
				t.Errorf("%s = %q, want %q", p, got, want)
			}
			if info, err := os.Lstat(p); err != nil || !info.Mode().IsRegular() {
				t.Errorf("%s is not a regular file (%v)", p, err)
			}
		}
		dep := subdirLockEntry(t, ".", "acme/repo")
		if got := dep.DeployedHashes[".claude/skills/s/ref.md"]; got != sha256Of("reference\n") {
			t.Errorf("lock hash of .claude/skills/s/ref.md = %q", got)
		}
	})
	t.Run("global", func(t *testing.T) {
		home := symlinkDepGlobal(t)
		symlinkDepSkillRepo(t, filepath.Join(home, ".apm", "repo"))
		writeGlobalGitDep(t, home)

		mustRun(t, "install", "--global", "--target", "claude")

		modules := filepath.Join(home, ".apm", "apm_modules", "acme", "repo")
		for link, want := range map[string]string{
			".claude/skills/linked": filepath.Join(modules, "realskills", "linked"),
			".claude/skills/s":      filepath.Join(modules, "skills", "s"),
		} {
			if got, err := os.Readlink(filepath.Join(home, link)); err != nil || got != want {
				t.Errorf("%s -> %q (%v), want -> %q", link, got, err, want)
			}
		}
		if got := readText(t, filepath.Join(home, ".claude/skills/s/ref.md")); got != "reference\n" {
			t.Errorf(".claude/skills/s/ref.md = %q", got)
		}
		dep := subdirLockEntry(t, filepath.Join(home, ".apm"), "acme/repo")
		want := map[string]string{
			".claude/skills/linked/SKILL.md": sha256Of(symlinkDepSkill),
			".claude/skills/s/SKILL.md":      sha256Of("---\nname: s\ndescription: d\n---\ns\n"),
			".claude/skills/s/ref.md":        sha256Of("reference\n"),
		}
		for p, h := range want {
			if dep.DeployedHashes[p] != h {
				t.Errorf("lock hash of %s = %q, want %q", p, dep.DeployedHashes[p], h)
			}
		}
		if len(dep.DeployedHashes) != len(want) {
			t.Errorf("lock hashes %v, want the files of %v", dep.DeployedHashes, want)
		}
	})
	for _, global := range []bool{false, true} {
		name := "outside copy"
		if global {
			name = "outside global"
		}
		t.Run(name, func(t *testing.T) {
			dir := symlinkDepProject(t)
			root, repo, args := dir, "repo", []string{"install", "--target", "claude"}
			if global {
				root = symlinkDepGlobalIn(t, dir)
				repo = filepath.Join(root, ".apm", "repo")
				writeGlobalGitDep(t, root)
				args = []string{"install", "--global", "--target", "claude"}
			} else {
				writeSymlinkGitDep(t, "repo")
			}
			secret := filepath.Join(dir, "outside", "secret.txt")
			symlinkDepRepo(t, repo, map[string]string{"skills/real/SKILL.md": "---\nname: real\ndescription: d\n---\nr\n"},
				map[string]string{"skills/real/secret.txt": secret})

			assertFailsBeforeWriting(t, root,
				" x deploy: dependency acme/repo: symlink skills/real/secret.txt resolves to "+secret+", outside the package", args...)
		})
	}
}

// symlinkDepGlobal makes a project like symlinkDepProject and a HOME for
// --global inside it, and returns HOME.
func symlinkDepGlobal(t *testing.T) string {
	t.Helper()
	return symlinkDepGlobalIn(t, symlinkDepProject(t))
}

func symlinkDepGlobalIn(t *testing.T, dir string) string {
	t.Helper()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, ".apm"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	return home
}

// Test 6: --global, investigation table A, and an in-package agent symlink,
// whose deployed symlink leads to the file it points at.
func TestInstall_GlobalDependencySymlinks(t *testing.T) {
	t.Run("outside", func(t *testing.T) {
		dir := symlinkDepProject(t)
		home := symlinkDepGlobalIn(t, dir)
		outer := filepath.Join(dir, "outside", "outer.agent.md")
		symlinkDepRepo(t, filepath.Join(home, ".apm", "repo"), map[string]string{"agents/oracle.agent.md": symlinkDepAgent},
			map[string]string{".apm/agents/outer.agent.md": outer})
		writeGlobalGitDep(t, home)

		assertFailsBeforeWriting(t, home,
			" x deploy: dependency acme/repo: symlink .apm/agents/outer.agent.md resolves to "+outer+", outside the package",
			"install", "--global", "--target", "claude")
	})
	t.Run("in package", func(t *testing.T) {
		home := symlinkDepGlobal(t)
		symlinkDepRepo(t, filepath.Join(home, ".apm", "repo"), map[string]string{"agents/oracle.agent.md": symlinkDepAgent},
			map[string]string{".apm/agents/inpkg.agent.md": "../../agents/oracle.agent.md"})
		writeGlobalGitDep(t, home)

		mustRun(t, "install", "--global", "--target", "claude")

		want := filepath.Join(home, ".apm", "apm_modules", "acme", "repo", "agents", "oracle.agent.md")
		if got, err := os.Readlink(filepath.Join(home, ".claude", "agents", "inpkg.md")); err != nil || got != want {
			t.Errorf(".claude/agents/inpkg.md -> %q (%v), want -> %q", got, err, want)
		}
	})
}

// Test 7: a subdirectory package (acme/mono with path:).
func TestInstall_SubdirectoryPackageSymlinks(t *testing.T) {
	t.Run("file outside", func(t *testing.T) {
		dir := symlinkDepProject(t)
		outer := filepath.Join(dir, "outside", "outer.agent.md")
		symlinkDepRepo(t, "mono", map[string]string{"pkg/apm.yml": "name: pkg\nversion: 1.0.0\n"},
			map[string]string{"pkg/.apm/agents/outer.agent.md": outer})
		linkGitRemote(t, ".", "mono")
		writeSymlinkDepManifest(t, "    - git: acme/mono\n      path: pkg\n")

		assertFailsBeforeWriting(t, dir,
			` x resolve: loading package acme/mono/pkg: subdirectory "pkg": symlink pkg/.apm/agents/outer.agent.md resolves to `+outer+", outside the package",
			"install", "--target", "claude")
		assertNoModule(t, "apm_modules/acme/mono")
	})
	t.Run("path through a symlink outside", func(t *testing.T) {
		dir := symlinkDepProject(t)
		target := filepath.Join(dir, "outside")
		symlinkDepRepo(t, "mono", map[string]string{"README.md": "mono\n"}, map[string]string{"pkgroot": target})
		linkGitRemote(t, ".", "mono")
		writeSymlinkDepManifest(t, "    - git: acme/mono\n      path: pkgroot/pkgdir\n")

		assertFailsBeforeWriting(t, dir,
			` x resolve: loading package acme/mono/pkgroot/pkgdir: subdirectory "pkgroot/pkgdir": symlink pkgroot resolves to `+target+", outside the package",
			"install", "--target", "claude")
		assertNoModule(t, "apm_modules/acme/mono")
	})
	t.Run("in repository", func(t *testing.T) {
		symlinkDepProject(t)
		symlinkDepRepo(t, "mono", map[string]string{
			"pkg/apm.yml":            "name: pkg\nversion: 1.0.0\n",
			"shared/shared.agent.md": symlinkDepAgent,
		}, map[string]string{"pkg/.apm/agents/shared.agent.md": "../../../shared/shared.agent.md"})
		linkGitRemote(t, ".", "mono")
		writeSymlinkDepManifest(t, "    - git: acme/mono\n      path: pkg\n")

		mustRun(t, "install", "--target", "claude")

		copied := filepath.Join("apm_modules", "acme", "mono", "pkg", ".apm", "agents", "shared.agent.md")
		if info, err := os.Lstat(copied); err != nil || !info.Mode().IsRegular() {
			t.Errorf("%s is not a regular file (%v)", copied, err)
		}
		if got := readText(t, ".claude/agents/shared.md"); got != symlinkDepAgent {
			t.Errorf(".claude/agents/shared.md = %q, want %q", got, symlinkDepAgent)
		}
	})
}

func assertNoModule(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Errorf("%s is left behind (%v)", p, err)
	}
}

// Test 8: the dependency's apm.yml is a symlink out of the package. Install
// fails while it resolves; uninstall of a package whose vendored apm.yml is
// such a symlink still removes it.
func TestInstall_DependencyManifestSymlinkOutside(t *testing.T) {
	t.Run("install", func(t *testing.T) {
		dir := symlinkDepProject(t)
		outer := filepath.Join(dir, "outside", "apm.yml")
		symlinkDepRepo(t, "repo", map[string]string{}, map[string]string{"apm.yml": outer})
		writeSymlinkGitDep(t, "repo")

		assertFailsBeforeWriting(t, dir,
			" x resolve: loading package acme/repo: symlink apm.yml resolves to "+outer+", outside the package",
			"install", "--target", "claude")
	})
	t.Run("uninstall", func(t *testing.T) {
		dir := symlinkDepProject(t)
		symlinkDepRepo(t, "acme/repo", map[string]string{".apm/agents/oracle.agent.md": symlinkDepAgent}, nil)
		writeSymlinkDepManifest(t, "    - acme/repo\n")
		if err := subdirInstall(t, subdirPathLoader{subdirRealLoader()}, false); err != nil {
			t.Fatal(err)
		}
		vendored := filepath.Join("apm_modules", "acme", "repo", "apm.yml")
		if err := os.Remove(vendored); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(dir, "outside", "apm.yml"), vendored); err != nil {
			t.Fatal(err)
		}

		var err error
		captureUninstallStdout(t, func() { err = runUninstall([]string{"acme/repo"}, uninstallOptions{}) })
		if err != nil {
			t.Fatalf("uninstall: %v", err)
		}

		if got := readText(t, "apm.yml"); got != "name: e\nversion: 1.0.0\ndependencies:\n  apm: []\n" {
			t.Errorf("apm.yml after uninstall = %q", got)
		}
		for _, p := range []string{".claude/agents/oracle.md", ".mcp.json", "apm_modules/acme/repo"} {
			assertNoModule(t, p)
		}
	})
}

// Test 9: .claude-plugin/plugin.json is a symlink out of the package.
func TestInstall_DependencyPluginManifestSymlinkOutsideFails(t *testing.T) {
	dir := symlinkDepProject(t)
	outer := filepath.Join(dir, "outside", "plugin.json")
	symlinkDepRepo(t, "repo", map[string]string{"agents/plain.md": symlinkDepAgent},
		map[string]string{".claude-plugin/plugin.json": outer})
	writeSymlinkGitDep(t, "repo")

	assertFailsBeforeWriting(t, dir,
		" x deploy: dependency acme/repo: symlink .claude-plugin/plugin.json resolves to "+outer+", outside the package",
		"install", "--target", "claude")
}

// Test 10: compile reads a dependency's instructions by the same rule.
func TestCompile_DependencySymlinks(t *testing.T) {
	t.Run("outside", func(t *testing.T) {
		dir := symlinkDepProject(t)
		writeSymlinkGitDep(t, "repo")
		target := filepath.Join(dir, "outside", "apmdir")
		writeGlobalStaleFile(t, "apm_modules/acme/repo/apm.yml", "name: dep\nversion: 1.0.0\n")
		if err := os.Symlink(target, filepath.Join("apm_modules", "acme", "repo", ".apm")); err != nil {
			t.Fatal(err)
		}

		assertFailsBeforeWriting(t, dir,
			" x compile: dependency acme/repo: symlink .apm resolves to "+target+", outside the package",
			"compile", "--target", "codex")
		assertNoModule(t, "AGENTS.md")
	})
	t.Run("in package", func(t *testing.T) {
		symlinkDepProject(t)
		writeSymlinkGitDep(t, "repo")
		writeGlobalStaleFile(t, "apm_modules/acme/repo/apm.yml", "name: dep\nversion: 1.0.0\n")
		writeGlobalStaleFile(t, "apm_modules/acme/repo/docs/c.instructions.md", "IN-PACKAGE-INSTRUCTION\n")
		if err := os.MkdirAll(filepath.Join("apm_modules", "acme", "repo", ".apm", "instructions"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../../docs/c.instructions.md", filepath.Join("apm_modules", "acme", "repo", ".apm", "instructions", "c.instructions.md")); err != nil {
			t.Fatal(err)
		}

		mustRun(t, "compile", "--target", "codex")

		if got := readText(t, "AGENTS.md"); !strings.Contains(got, "\nIN-PACKAGE-INSTRUCTION\n") {
			t.Errorf("AGENTS.md lacks the in-package instruction:\n%s", got)
		}
	})
}

// Test 11: a frozen install of a locked commit that has a symlink out of the
// package fails with nothing written, the lock included. The frozen install
// fetches the locked commit again, so the symlink is in that commit.
func TestInstall_FrozenDependencySymlinkOutsideFails(t *testing.T) {
	dir := symlinkDepProject(t)
	clean := symlinkDepRepo(t, "acme/repo", map[string]string{".apm/agents/plain.agent.md": symlinkDepAgent}, nil)
	writeSymlinkDepManifest(t, "    - acme/repo\n")
	loader := subdirPathLoader{subdirRealLoader()}
	if err := subdirInstall(t, loader, false); err != nil {
		t.Fatal(err)
	}

	outer := filepath.Join(dir, "outside", "outer.agent.md")
	escaping := symlinkDepRepo(t, "acme/repo", nil, map[string]string{".apm/agents/outer.agent.md": outer})
	lock := readText(t, "apm.lock.yaml")
	if !strings.Contains(lock, clean) {
		t.Fatalf("lock does not pin %s:\n%s", clean, lock)
	}
	cleanTree, err := lockfile.ComputeTreeSHA256("acme/repo", clean)
	if err != nil {
		t.Fatal(err)
	}
	escapingTree, err := lockfile.ComputeTreeSHA256("acme/repo", escaping)
	if err != nil {
		t.Fatal(err)
	}
	lock = strings.ReplaceAll(lock, clean, escaping)
	lock = strings.ReplaceAll(lock, cleanTree, escapingTree)
	lock = strings.Replace(lock, "  deployed_files:\n", "  deployed_files:\n  - .claude/agents/outer.md\n", 1)
	lock = strings.Replace(lock, "  deployed_file_hashes:\n", "  deployed_file_hashes:\n    .claude/agents/outer.md: sha256:"+strings.Repeat("0", 64)+"\n", 1)
	writeGlobalStaleFile(t, "apm.lock.yaml", lock)
	if err := os.Remove(filepath.Join(".claude", "agents", "plain.md")); err != nil {
		t.Fatal(err)
	}
	before := treeOutsideModules(t, dir)

	err = subdirInstall(t, loader, true)

	want := "frozen install: deploy: dependency acme/repo: symlink .apm/agents/outer.agent.md resolves to " + outer + ", outside the package"
	if err == nil || err.Error() != want || exitCodeOf(err) != 1 {
		t.Errorf("frozen install: error %v (exit %d)\nwant %s (exit 1)", err, exitCodeOf(err), want)
	}
	assertSameTree(t, before, treeOutsideModules(t, dir))
}

// Test 12: the project's own .apm/ keeps its behavior: a symlink in it is
// deployed wherever it leads, and compile leaves a symlinked instruction out.
func TestInstall_LocalContentSymlinksUnchanged(t *testing.T) {
	dir := symlinkDepProject(t)
	writeGlobalStaleFile(t, "apm.yml", "name: e\nversion: 1.0.0\n")
	for name, target := range map[string]string{
		"outer.agent.md":        filepath.Join(dir, "outside", "outer.agent.md"),
		"outer.instructions.md": filepath.Join(dir, "outside", "outer.instructions.md"),
	} {
		dirName := map[bool]string{true: "agents", false: "instructions"}[strings.HasSuffix(name, ".agent.md")]
		link := filepath.Join(".apm", dirName, name)
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	writeGlobalStaleFile(t, ".apm/instructions/own.instructions.md", "OWN-INSTRUCTION\n")

	mustRun(t, "install", "--target", "claude")
	if got := readText(t, ".claude/agents/outer.md"); got != "SECRET-AGENT\n" {
		t.Errorf(".claude/agents/outer.md = %q, want the project's own symlink deployed", got)
	}

	mustRun(t, "compile", "--target", "codex")
	agents := readText(t, "AGENTS.md")
	if !strings.Contains(agents, "OWN-INSTRUCTION") || strings.Contains(agents, "SECRET-INSTR") {
		t.Errorf("AGENTS.md should hold the project's own instruction and not its symlinked one:\n%s", agents)
	}
}

// Test 13: with several symlinks out of their packages, the one reported is
// the first the deploy collects: dependencies in the order apm.yml declares
// them, entries of one directory by name.
func TestInstall_DependencySymlinkErrorOrder(t *testing.T) {
	dir := symlinkDepProject(t)
	outer := filepath.Join(dir, "outside", "outer.agent.md")
	links := map[string]string{".apm/agents/b.agent.md": outer, ".apm/agents/a.agent.md": outer}
	symlinkDepRepo(t, "first", map[string]string{}, links)
	symlinkDepRepo(t, "second", map[string]string{}, links)
	linkGitRemote(t, ".", "second")
	linkGitRemote(t, ".", "first")
	writeSymlinkDepManifest(t, "    - acme/second\n    - acme/first\n")

	assertFailsBeforeWriting(t, dir,
		" x deploy: dependency acme/second: symlink .apm/agents/a.agent.md resolves to "+outer+", outside the package",
		"install", "--target", "claude")
}

// Review round 1, finding 1: the SKILL.md that makes a directory a skill is
// resolved before it is looked at, so one that dangles or loops fails the
// collection instead of hiding the directory from it.
func TestInstall_DependencySkillMarkerSymlinkFails(t *testing.T) {
	sibling := map[string]string{".apm/instructions/sib.instructions.md": "SIBLING\n"}
	for _, tt := range []struct {
		name  string
		files map[string]string
		links map[string]string
		want  string
	}{
		{
			name:  "skills dangling",
			links: map[string]string{"skills/s/SKILL.md": "../../nope.md"},
			want:  "symlink skills/s/SKILL.md -> ../../nope.md does not resolve: no such file or directory",
		},
		{
			name:  ".apm/skills dangling",
			links: map[string]string{".apm/skills/s/SKILL.md": "../../../nope.md"},
			want:  "symlink .apm/skills/s/SKILL.md -> ../../../nope.md does not resolve: no such file or directory",
		},
		{
			name:  "loop",
			links: map[string]string{"skills/s/SKILL.md": "other.md", "skills/s/other.md": "SKILL.md"},
			want:  "symlink skills/s/SKILL.md is a symlink loop",
		},
		{
			name:  "plugin skill dangling",
			files: map[string]string{".claude-plugin/plugin.json": `{"name":"p","skills":["./custom/s"]}` + "\n"},
			links: map[string]string{"custom/s/SKILL.md": "nope.md"},
			want:  "symlink custom/s/SKILL.md -> nope.md does not resolve: no such file or directory",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := symlinkDepProject(t)
			files := map[string]string{}
			for k, v := range sibling {
				files[k] = v
			}
			for k, v := range tt.files {
				files[k] = v
			}
			symlinkDepRepo(t, "repo", files, tt.links)
			writeSymlinkGitDep(t, "repo")

			assertFailsBeforeWriting(t, dir, " x deploy: dependency acme/repo: "+tt.want, "install", "--target", "claude")
			for _, p := range []string{"apm.lock.yaml", ".gitignore", ".claude"} {
				assertNoModule(t, p)
			}
		})
	}
}

// A directory with no SKILL.md at all is still not a skill, and no error.
func TestInstall_DependencyDirectoryWithoutSkillMarkerIsSkipped(t *testing.T) {
	symlinkDepProject(t)
	symlinkDepRepo(t, "repo", map[string]string{
		"skills/notes/readme.md":     "notes\n",
		".apm/agents/plain.agent.md": symlinkDepAgent,
	}, nil)
	writeSymlinkGitDep(t, "repo")

	mustRun(t, "install", "--target", "claude")

	assertNoModule(t, ".claude/skills/notes")
	if got := readText(t, ".claude/agents/plain.md"); got != symlinkDepAgent {
		t.Errorf(".claude/agents/plain.md = %q", got)
	}
}

// Review round 1, finding 2: the package root is compared in its absolute,
// resolved form, so an absolute symlink into the checkout is inside the
// package although the modules directory is the relative apm_modules.
func TestInstall_DependencyManifestSymlinkIntoCheckout(t *testing.T) {
	const metadata = "name: dep\nversion: 1.0.0\ndependencies:\n  mcp:\n    - name: from-metadata\n      registry: false\n      transport: stdio\n      command: metadata-cmd\n"
	for _, absolute := range []bool{true, false} {
		name := "relative"
		if absolute {
			name = "absolute"
		}
		t.Run(name, func(t *testing.T) {
			dir := symlinkDepProject(t)
			target := "metadata.yml"
			if absolute {
				target = filepath.Join(dir, "apm_modules", "acme", "repo", "metadata.yml")
			}
			symlinkDepRepo(t, "repo", map[string]string{"metadata.yml": metadata}, map[string]string{"apm.yml": target})
			writeSymlinkGitDep(t, "repo")

			mustRun(t, "install", "--target", "claude")

			if got := readText(t, ".mcp.json"); !strings.Contains(got, `"from-metadata"`) || !strings.Contains(got, `"metadata-cmd"`) {
				t.Errorf(".mcp.json does not hold the server metadata.yml declares:\n%s", got)
			}
		})
	}
}

// Review round 1, finding 2: with a relative TMPDIR the clone a subdirectory
// package is taken from has a relative path, and an absolute symlink into the
// clone is still inside it. The clone's directory name is random, so the
// symlink leads there through <project>/clone, made as soon as the
// temporary directory appears, while git is still cloning.
func TestInstall_SubdirectoryAbsoluteSymlinkWithRelativeTMPDIR(t *testing.T) {
	dir := symlinkDepProject(t)
	if err := os.Mkdir("tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", "tmp")
	symlinkDepRepo(t, "mono", map[string]string{
		"pkg/apm.yml":            "name: pkg\nversion: 1.0.0\n",
		"shared/shared.agent.md": symlinkDepAgent,
	}, map[string]string{"pkg/.apm/agents/shared.agent.md": filepath.Join(dir, "clone", "repo", "shared", "shared.agent.md")})
	linkGitRemote(t, ".", "mono")
	writeSymlinkDepManifest(t, "    - git: acme/mono\n      path: pkg\n")

	done := make(chan struct{})
	linked := make(chan error, 1)
	go func() {
		for {
			select {
			case <-done:
				linked <- nil
				return
			default:
			}
			matches, _ := filepath.Glob(filepath.Join(dir, "tmp", "apm-subdir-*"))
			if len(matches) == 1 {
				linked <- os.Symlink(matches[0], filepath.Join(dir, "clone"))
				return
			}
		}
	}()
	stdout, stderr, code := runCompileCLI(t, "install", "--target", "claude")
	close(done)
	if err := <-linked; err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("install: exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	copied := filepath.Join("apm_modules", "acme", "mono", "pkg", ".apm", "agents", "shared.agent.md")
	if info, err := os.Lstat(copied); err != nil || !info.Mode().IsRegular() {
		t.Errorf("%s is not a regular file (%v)", copied, err)
	}
	if got := readText(t, ".claude/agents/shared.md"); got != symlinkDepAgent {
		t.Errorf(".claude/agents/shared.md = %q, want %q", got, symlinkDepAgent)
	}
}
