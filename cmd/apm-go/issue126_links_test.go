package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// Issue #126 item 64: relative links in a deployed file are rewritten as
// Oracle 0.33.0 rewrites them. Each fixture and wanted file under
// testdata/issue126_links is what the Oracle had and wrote
// (~/.cache/apm-go-verify/issue126/link/r2, r3, r4, r8, r13), and each
// wanted hash is the one its apm.lock.yaml records.

// issue126LinksRoot is taken when the test binary starts, in the package's
// directory, before a test enters its project.
var issue126LinksRoot, _ = filepath.Abs(filepath.Join("testdata", "issue126_links"))

func issue126LinksData(t *testing.T, rel string) string {
	t.Helper()
	return filepath.Join(issue126LinksRoot, filepath.FromSlash(rel))
}

// issue126Project copies the fixture of scenario into a new project
// directory, below a directory of its own, and enters the project.
func issue126Project(t *testing.T, scenario string) string {
	t.Helper()
	fixture := issue126LinksData(t, scenario+"/fixture")
	sandbox := chdirTemp(t)
	proj := filepath.Join(sandbox, "proj")
	if err := copyTreeSkipGit(fixture, proj); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	// Windows takes the home directory from USERPROFILE.
	setTestHome(t, filepath.Join(sandbox, "home"))
	return proj
}

// assertTreeAsWant fails unless each file below want is below root with the
// same bytes, and each file of root's dirs is below want.
func assertTreeAsWant(t *testing.T, want, root string, dirs ...string) {
	t.Helper()
	files := func(base string) []string {
		var out []string
		for _, d := range dirs {
			_ = filepath.WalkDir(filepath.Join(base, d), func(p string, e os.DirEntry, err error) error {
				if err == nil && !e.IsDir() {
					rel, _ := filepath.Rel(base, p)
					out = append(out, filepath.ToSlash(rel))
				}
				return nil
			})
		}
		slices.Sort(out)
		return out
	}
	wantFiles, gotFiles := files(want), files(root)
	if !slices.Equal(wantFiles, gotFiles) {
		t.Errorf("files:\n%s\nwant:\n%s", strings.Join(gotFiles, "\n"), strings.Join(wantFiles, "\n"))
	}
	for _, f := range wantFiles {
		w, _ := os.ReadFile(filepath.Join(want, f))
		g, err := os.ReadFile(filepath.Join(root, f))
		if err != nil || !bytes.Equal(g, w) {
			t.Errorf("%s:\n%s\nwant:\n%s", f, g, w)
		}
	}
}

var issue126HashLine = regexp.MustCompile(`(?m)^ +([^ ]+): (sha256:[0-9a-f]+)$`)

// assertIssue126LockHashes fails unless the deployed-file hashes of lock are
// the lines of the file wantHashes.
func assertIssue126LockHashes(t *testing.T, lock, wantHashes string) {
	t.Helper()
	data, err := os.ReadFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range issue126HashLine.FindAllStringSubmatch(string(data), -1) {
		if m[1] != "content_hash" {
			got = append(got, m[1]+": "+m[2])
		}
	}
	slices.Sort(got)
	want, err := os.ReadFile(wantHashes)
	if err != nil {
		t.Fatal(err)
	}
	if g := strings.Join(got, "\n") + "\n"; g != string(want) {
		t.Errorf("lock hashes:\n%s\nwant the Oracle's:\n%s", g, want)
	}
}

// link/r2: install ./dep --target <t>, then a frozen install on the files,
// and on a clone with no deployed file, whose trial deploy rewrites the links
// from the real project and so gives the lock's hashes.
func TestInstall_Issue126_LinksRewrittenAsTheOracleDoes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the want is Oracle 0.33.0's output on Linux; on Windows a \\ separates and a name is spelled as the disk spells it (TestRun_LinksOnWindows, link/win)")
	}
	for _, tc := range []struct {
		target string
		dirs   []string
	}{
		{"claude", []string{".claude"}},
		{"copilot", []string{".github", ".agents"}},
		{"opencode", []string{".opencode", ".agents"}},
	} {
		t.Run(tc.target, func(t *testing.T) {
			proj := issue126Project(t, "r2")
			mustRunCLI(t, "install", "./dep", "--target", tc.target)
			want := issue126LinksData(t, "r2/want/"+tc.target)
			assertTreeAsWant(t, want, proj, tc.dirs...)
			assertIssue126LockHashes(t, "apm.lock.yaml", want+".hashes")

			mustRunCLI(t, "install", "--frozen")
			for _, d := range tc.dirs {
				if err := os.RemoveAll(d); err != nil {
					t.Fatal(err)
				}
			}
			mustRunCLI(t, "install", "--frozen")
			assertTreeAsWant(t, want, proj, tc.dirs...)
		})
	}
}

// link/r5: a second install finds the prompt, the agents and the
// instruction as written, links rewritten, and adopts them, where Oracle
// 0.33.0 compares a prompt and an agent with their source and integrates
// them again (issue #126 comment 6270876048 item 1). A skill is integrated
// on every install.
func TestInstall_Issue126_RewrittenFilesSecondInstall(t *testing.T) {
	issue126Project(t, "r2")
	mustRunCLI(t, "install", "./dep", "--target", "copilot")
	want := " > Installing dependencies from apm.yml...\n" +
		" i Targets: copilot  (source: apm.yml)\n" +
		"   + ./dep (local)\n" +
		"  |-- 1 prompts adopted -> .github/prompts/\n" +
		"  |-- 2 agents adopted -> .github/agents/\n" +
		"  |-- 1 instruction(s) adopted -> .github/instructions/\n" +
		"  |-- 1 skill(s) integrated -> .agents/skills/\n" +
		" + Installed 1 APM dependency in <DUR>.\n"
	if got := mustRunCLI(t, "install"); got != want {
		t.Errorf("second install:\n%s\nwant:\n%s", got, want)
	}
}

// link/r12: a prompt, an instruction and a skill the user put there with
// the source's bytes. The Oracle keeps the prompt and the skill with their
// links as they are (1 prompts adopted, 1 skill(s) integrated, measured in
// impl/H-r12skill); apm-go leaves all three to the user, since neither is what the deploy writes, links
// rewritten, and a frozen install of a clean clone restores the lock (issue
// #126 comment 6270876048 item 1). The Oracle's "Use 'apm install --force'"
// line is not printed (issue #122 comment 6232421913 item 2).
func TestInstall_Issue126_UserFilesWithLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the want is Oracle 0.33.0's output on Linux; on Windows a \\ separates and a name is spelled as the disk spells it (TestRun_LinksOnWindows, link/win)")
	}
	proj := issue126Project(t, "r2")
	for _, f := range []string{"prompts/p.prompt.md", "instructions/i.instructions.md"} {
		data, err := os.ReadFile(filepath.Join("dep", ".apm", filepath.FromSlash(f)))
		if err != nil {
			t.Fatal(err)
		}
		writeCompileFile(t, proj, ".github/"+f, string(data))
	}
	if err := copyTreeSkipGit(filepath.Join("dep", ".apm", "skills", "s"), filepath.Join(proj, ".agents", "skills", "s")); err != nil {
		t.Fatal(err)
	}
	want := " + Created apm.yml\n" +
		" i Targets set: copilot (persisted to apm.yml)\n" +
		" + Validating 1 package...\n" +
		" + ./dep\n" +
		" + Updated apm.yml with 1 new package(s)\n" +
		" > Installing 1 new package...\n" +
		" > Resolving ./dep...\n" +
		" i Targets: copilot  (source: --target flag)\n" +
		"   + ./dep (local)\n" +
		"  |-- 2 agents integrated -> .github/agents/\n" +
		" i Added apm_modules/ to .gitignore\n" +
		"   ! 3 files skipped -- local files exist, not managed by APM\n" +
		" + Installed 1 APM dependency in <DUR>.\n"
	if got := mustRunCLI(t, "install", "./dep", "--target", "copilot"); got != want {
		t.Errorf("install:\n%s\nwant:\n%s", got, want)
	}
	lock, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lock), "    .github/agents/a.agent.md: sha256:590143e4868ace9f0161e2337d9ecb1c15b747cef3f6930cf3d7f6b9f118bcf5\n") {
		t.Errorf("apm.lock.yaml has no rewritten agent:\n%s", lock)
	}
	for _, f := range []string{".github/prompts/", ".github/instructions/", ".agents/skills/"} {
		if strings.Contains(string(lock), f) {
			t.Errorf("apm.lock.yaml records the user's file below %s:\n%s", f, lock)
		}
	}
	// A clean clone has the user's files and none of the deployed ones.
	for _, d := range []string{".github/agents"} {
		if err := os.RemoveAll(d); err != nil {
			t.Fatal(err)
		}
	}
	mustRunCLI(t, "install", "--frozen")
}

// impl/H2-nolock1: with the lock deleted, an install finds each file as it
// wrote it, links rewritten, adopts it and records it again, and a frozen
// install of a clean clone restores it (issue #126 comment 6270876048 item
// 1). With no lock the Oracle compares a prompt, an agent and a skill with
// their source and leaves them to the user (4 files skipped, measured in
// impl/H2-nolock1/or).
func TestInstall_Issue126_DeletedLockAdoptsRewrittenFiles(t *testing.T) {
	for _, tc := range []struct {
		kind, line, path string
	}{
		{"agent", "  |-- 2 agents adopted -> .github/agents/\n", ".github/agents/a.agent.md"},
		{"prompt", "  |-- 1 prompts adopted -> .github/prompts/\n", ".github/prompts/p.prompt.md"},
		{"skill", "  |-- 1 skill(s) integrated -> .agents/skills/\n", ".agents/skills/s/ref/r.md"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			issue126Project(t, "r2")
			mustRunCLI(t, "install", "./dep", "--target", "copilot")
			first, err := os.ReadFile("apm.lock.yaml")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove("apm.lock.yaml"); err != nil {
				t.Fatal(err)
			}
			got := mustRunCLI(t, "install")
			if !strings.Contains(got, tc.line) || strings.Contains(got, "skipped") {
				t.Errorf("install with the lock deleted:\n%s\nwant %q and no skipped file", got, tc.line)
			}
			lock, err := os.ReadFile("apm.lock.yaml")
			if err != nil {
				t.Fatal(err)
			}
			record := regexp.MustCompile(`(?m)^    ` + regexp.QuoteMeta(tc.path) + `: sha256:[0-9a-f]+$`)
			if want := record.Find(first); want == nil || !bytes.Contains(lock, want) {
				t.Errorf("apm.lock.yaml does not record %s as the first install did:\n%s", tc.path, lock)
			}
			for _, d := range []string{".github", ".agents"} {
				if err := os.RemoveAll(d); err != nil {
					t.Fatal(err)
				}
			}
			mustRunCLI(t, "install", "--frozen")
			if _, err := os.Stat(tc.path); err != nil {
				t.Errorf("frozen install of a clean clone did not restore %s: %v", tc.path, err)
			}
		})
	}
}

// impl/G-orup1: a path the old lock records holds the source with its links
// as they were. The Oracle keeps it and counts it adopted; apm-go writes the
// rewritten content and counts it integrated, so the lock holds what a
// frozen install of a clean clone writes (issue #126 comment 6270876048
// item 1).
func TestInstall_Issue126_RecordedFileWithLinksIsRewritten(t *testing.T) {
	proj := issue126Project(t, "r2")
	mustRunCLI(t, "install", "./dep", "--target", "copilot")
	written, err := os.ReadFile(".github/prompts/p.prompt.md")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join("dep", ".apm", "prompts", "p.prompt.md"))
	if err != nil {
		t.Fatal(err)
	}
	writeCompileFile(t, proj, ".github/prompts/p.prompt.md", string(src))
	got := mustRunCLI(t, "install")
	if !strings.Contains(got, "  |-- 1 prompts integrated -> .github/prompts/\n") || strings.Contains(got, "adopted -> .github/prompts/") {
		t.Errorf("install over the recorded prompt:\n%s", got)
	}
	if after, err := os.ReadFile(".github/prompts/p.prompt.md"); err != nil || !bytes.Equal(after, written) {
		t.Errorf("prompt after install:\n%s\nwant the rewritten content:\n%s", after, written)
	}
	for _, d := range []string{".github", ".agents"} {
		if err := os.RemoveAll(d); err != nil {
			t.Fatal(err)
		}
	}
	mustRunCLI(t, "install", "--frozen")
}

// link/r3: the project's own content, its package root the project.
func TestInstall_Issue126_OwnContentLinks(t *testing.T) {
	proj := issue126Project(t, "r3")
	data, err := os.ReadFile(issue126LinksData(t, "r3/outside.md"))
	if err != nil {
		t.Fatal(err)
	}
	writeCompileFile(t, filepath.Dir(proj), "outside.md", string(data))
	mustRunCLI(t, "install", "--target", "claude")
	assertTreeAsWant(t, issue126LinksData(t, "r3/want"), proj, ".claude")
	assertIssue126LockHashes(t, "apm.lock.yaml", issue126LinksData(t, "r3/want.hashes"))
}

// link/r8: a skill file whose links stay in the skill is copied as it is,
// CRLF kept; one with a link rewritten is written with LF; a command's body
// is rewritten after its CRLF is read as LF.
func TestInstall_Issue126_SkillFileWrittenOnlyWhenRewritten(t *testing.T) {
	proj := issue126Project(t, "r8")
	mustRunCLI(t, "install", "./dep", "--target", "claude")
	assertTreeAsWant(t, issue126LinksData(t, "r8/want"), proj, ".claude")
	assertIssue126LockHashes(t, "apm.lock.yaml", issue126LinksData(t, "r8/want.hashes"))
}

// link/r13: a Claude plugin's agent and command are read from where the
// Oracle stages them, .apm/agents/ and .apm/prompts/ of the package.
func TestInstall_Issue126_PluginLinksFromTheStagedPlace(t *testing.T) {
	proj := issue126Project(t, "r13")
	mustRunCLI(t, "install", "./plug", "--target", "claude,copilot")
	assertTreeAsWant(t, issue126LinksData(t, "r13/want"), proj, ".claude", ".github")
	assertIssue126LockHashes(t, "apm.lock.yaml", issue126LinksData(t, "r13/want.hashes"))
}

// link/r4: at --global a claude rule and a command are files and have their
// links rewritten as the Oracle's; an agent is linked, or copied where the
// deploy cannot link, as its source is (issue #126 comment 6267289737 item
// 1).
func TestInstallGlobal_Issue126_RuleAndCommandLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the want is Oracle 0.33.0's output on Linux; on Windows a \\ separates and a name is spelled as the disk spells it (TestRun_LinksOnWindows, link/win)")
	}
	proj := issue126Project(t, "r2")
	home := os.Getenv("HOME")
	mustRunCLI(t, "install", "-g", filepath.Join(proj, "dep"), "--target", "claude,copilot")
	want := issue126LinksData(t, "r4/want")
	for _, f := range []string{".claude/commands/p.md", ".claude/rules/i.md"} {
		w, _ := os.ReadFile(filepath.Join(want, f))
		g, err := os.ReadFile(filepath.Join(home, f))
		if err != nil || !bytes.Equal(g, w) {
			t.Errorf("%s:\n%s\nwant:\n%s", f, g, w)
		}
	}
	src, _ := os.ReadFile(filepath.Join(proj, "dep", ".apm", "agents", "a.agent.md"))
	if g, err := os.ReadFile(filepath.Join(home, ".claude", "agents", "a.md")); err != nil || !bytes.Equal(g, src) {
		t.Errorf(".claude/agents/a.md:\n%s\nwant the source:\n%s", g, src)
	}
	mustRunCLI(t, "install", "-g", "--frozen")

	// The copy a deploy makes when it cannot link records the same lock, so
	// a frozen install restores under either way.
	setSymlinkProbe(t, false)
	for _, d := range []string{".claude", ".github", ".agents"} {
		if err := os.RemoveAll(filepath.Join(home, d)); err != nil {
			t.Fatal(err)
		}
	}
	mustRunCLI(t, "install", "-g", "--frozen")
	if g, err := os.ReadFile(filepath.Join(home, ".claude", "agents", "a.md")); err != nil || !bytes.Equal(g, src) {
		t.Errorf("copied .claude/agents/a.md:\n%s\nwant the source:\n%s", g, src)
	}
}

// Issue #126 comment 6267289737 item 3(2): a skill's .md that is not UTF-8
// is copied as it is, where Oracle 0.33.0 ends install with exit 1
// (link/r9).
func TestInstall_Issue126_SkillMarkdownNotUTF8(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	home := t.TempDir()
	// Windows takes the home directory from USERPROFILE.
	setTestHome(t, home)
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	writeCompileFile(t, dir, "dep/docs/d.md", "# d\n")
	writeCompileFile(t, dir, "dep/.apm/skills/s/SKILL.md", "---\nname: s\ndescription: s\n---\n[d](../../../docs/d.md)\n")
	bad := "caf\xe9 [d](../../../docs/d.md)\r\n"
	writeCompileFile(t, dir, "dep/.apm/skills/s/latin1.md", bad)
	mustRunCLI(t, "install", "./dep", "--target", "claude")
	if g, err := os.ReadFile(".claude/skills/s/latin1.md"); err != nil || string(g) != bad {
		t.Errorf("latin1.md = %q, want the source %q", g, bad)
	}
	if g, _ := os.ReadFile(".claude/skills/s/SKILL.md"); !strings.Contains(string(g), "[d](../../../apm_modules/_local/dep/docs/d.md)") {
		t.Errorf("SKILL.md not rewritten:\n%s", g)
	}
}
