package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Expected values in this file are Oracle 0.33.0's, measured for the second
// review of PR #171 (runs below /home/madao/.cache/apm-go-oracle-ref/B/r2-*),
// unless a comment names a deviation. Deviation for every refresh: apm-go
// does not print the Oracle's `[+] Cleaned up <N> integrated <kind>` lines.

// The deployed bytes of ./b/dep's primitives and their lock hashes, as Oracle
// 0.33.0's install writes them for claude, before and after the source
// changes (" b" to " NEW", "old asset" to "NEW asset").
var (
	refreshOld = map[string]string{
		".claude/agents/rb.md":       "4f8f6347c801191f6709955c4360fd7d2291b8f285a73a792960fe404cfb5b4f",
		".claude/commands/cb.md":     "adf632dc5c85daf680eefb19cae93edbbbfd29cb2d44deac0fcf9f83ee76bde3",
		".claude/rules/ib.md":        "3363e61749012b9deccd3473eb4ffcf08098fd96b8a51df3607315fa46531d3b",
		".claude/skills/sb/SKILL.md": "a5ffeb45fd924ad03f2ce64c9e6bd1fcb62f7b2bfde19067c6fac0f67619fe97",
		".claude/skills/sb/old.txt":  "cc2f663c3d8891fe283325c7ca3f8f8dbad078f0b7154ed5d125e92130ac745b",
	}
	refreshNew = map[string]string{
		".claude/agents/rb.md":       "cfa23f89bd18c248c87eef7b9805890ca798f8f2ee7606c6687922eebd40aba1",
		".claude/commands/cb.md":     "47651ef4fef693e53ccfc36409dc5eb4116989ab512cff90c541094dd6d020ed",
		".claude/rules/ib.md":        "431524ec995c7ed617185957def8b62220d38e9ca22f16d6cd748a47d52af167",
		".claude/skills/sb/SKILL.md": "fb946a73d6b5f01213ebf405e60e8165be3d36776329cffe1f8edde4b48d8dce",
		".claude/skills/sb/old.txt":  "968192db06b31446f23eefb87a4dc7a8961e4794110526e0ae5873628dc63006",
	}
	refreshSources = map[string]string{
		".apm/skills/sb/SKILL.md":              sharedSkillB,
		".apm/skills/sb/old.txt":               "old asset\n",
		".apm/agents/rb.agent.md":              "---\nname: rb\ndescription: d\n---\nagent b\n",
		".apm/instructions/ib.instructions.md": "---\ndescription: d\n---\ninstruction b\n",
		".apm/prompts/cb.prompt.md":            "---\ndescription: d\n---\nprompt b\n",
	}
	refreshDeployed = map[string]string{
		".claude/skills/sb/SKILL.md": sharedSkillB,
		".claude/skills/sb/old.txt":  "old asset\n",
		".claude/agents/rb.md":       "---\nname: rb\ndescription: d\n---\nagent b\n",
		".claude/rules/ib.md":        "instruction b\n",
		".claude/commands/cb.md":     "---\ndescription: d\n---\n\nprompt b",
	}
)

const (
	refreshAgents   = "  |-- 1 agents integrated -> .claude/agents/\n"
	refreshCommands = "  |-- 1 commands integrated -> .claude/commands/\n"
	refreshRules    = "  |-- 1 rule(s) integrated -> .claude/rules/\n"
	refreshSkills   = "  |-- 1 skill(s) integrated -> .claude/skills/\n"
)

// refreshState is the state of r2probe.py: Oracle 0.33.0's install of ./b/dep
// alone (a skill sb with old.txt, an agent, an instruction and a prompt, for
// claude), then ./a/dep, which shares b's directory below apm_modules, added
// before it to apm.yml and the lock.
type refreshState struct {
	deployRoot, manifestDir string
	a, b                    string // the declared paths
	global                  bool
}

func newRefreshState(t *testing.T, global, alias bool) *refreshState {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	t.Setenv("APM_CONFIG_DIR", filepath.Join(root, "config"))
	s := &refreshState{deployRoot: filepath.Join(root, "proj"), global: global}
	s.manifestDir = s.deployRoot
	if global {
		s.deployRoot, s.manifestDir = home, filepath.Join(home, ".apm")
		oldDir, oldDeploy := globalDirOverride, globalDeployDirOverride
		globalDirOverride, globalDeployDirOverride = s.manifestDir, home
		t.Cleanup(func() { globalDirOverride, globalDeployDirOverride = oldDir, oldDeploy })
	}
	srcRoot := s.manifestDir
	if global {
		srcRoot = filepath.Join(root, "src")
		s.a, s.b = filepath.Join(srcRoot, "a", "dep"), filepath.Join(srcRoot, "b", "dep")
	} else {
		s.a, s.b = "./a/dep", "./b/dep"
	}
	writeCompileFile(t, srcRoot, "a/dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	writeCompileFile(t, srcRoot, "a/dep/.apm/skills/sa/SKILL.md", sharedSkillA)
	writeCompileFile(t, srcRoot, "b/dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	dir := "_local/dep"
	if alias {
		dir = "AAA"
	}
	writeCompileFile(t, s.manifestDir, "apm_modules/"+dir+"/apm.yml", "name: dep\nversion: 1.0.0\n")
	for rel, body := range refreshSources {
		writeCompileFile(t, srcRoot, "b/dep/"+rel, body)
		writeCompileFile(t, s.manifestDir, "apm_modules/"+dir+"/"+rel, body)
	}
	for rel, body := range refreshDeployed {
		writeCompileFile(t, s.deployRoot, rel, body)
	}
	entry, aliasLine := func(p string) string { return "    - '" + p + "'\n" }, ""
	if alias {
		entry = func(p string) string { return "    - path: '" + p + "'\n      alias: AAA\n" }
		aliasLine = "  alias: AAA\n"
	}
	writeCompileFile(t, s.manifestDir, "apm.yml", "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n"+entry(s.a)+entry(s.b))
	lock := "lockfile_version: '1'\napm_version: 0.33.0\ndependencies:\n" +
		"- repo_url: _local/dep\n" + aliasLine + "  source: local\n  local_path: '" + s.a + "'\n" +
		"- repo_url: _local/dep\n" + aliasLine + "  name: dep\n  version: 1.0.0\n  package_type: apm_package\n" +
		refreshLockFiles(refreshOld) + "  source: local\n  local_path: '" + s.b + "'\n"
	writeCompileFile(t, s.manifestDir, "apm.lock.yaml", lock)
	if global {
		chdirTemp(t)
	} else {
		t.Chdir(s.deployRoot)
	}
	return s
}

// refreshLockFiles is the deployed_files and deployed_file_hashes of a lock
// entry that records hashes, the skill's directory with no hash.
func refreshLockFiles(hashes map[string]string) string {
	files := []string{}
	for p := range hashes {
		files = append(files, p)
		if dir := filepath.ToSlash(filepath.Dir(p)); strings.HasSuffix(filepath.ToSlash(filepath.Dir(dir)), "/skills") && !slices.Contains(files, dir) {
			files = append(files, dir)
		}
	}
	slices.Sort(files)
	out := "  deployed_files:\n"
	for _, p := range files {
		out += "  - " + p + "\n"
	}
	out += "  deployed_file_hashes:\n"
	for _, p := range files {
		if h, ok := hashes[p]; ok {
			out += "    " + p + ": sha256:" + h + "\n"
		}
	}
	return out
}

func (s *refreshState) src(rel string) string {
	if s.global {
		return filepath.Join(filepath.Dir(filepath.Dir(s.b)), "b", "dep", filepath.FromSlash(rel))
	}
	return filepath.Join(s.deployRoot, "b", "dep", filepath.FromSlash(rel))
}

// changeSources gives every source of ./b/dep new content.
func (s *refreshState) changeSources(t *testing.T) {
	t.Helper()
	for rel := range refreshSources {
		p := s.src(rel)
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		body := strings.ReplaceAll(strings.ReplaceAll(string(data), " b\n", " NEW\n"), "old asset", "NEW asset")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// relink moves the deploy root's rel to target and puts a relative symlink to
// it in its place.
func (s *refreshState) relink(t *testing.T, rel, target string) {
	t.Helper()
	from, to := filepath.Join(s.deployRoot, filepath.FromSlash(rel)), filepath.Join(s.deployRoot, filepath.FromSlash(target))
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
	link, err := filepath.Rel(filepath.Dir(from), to)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(link, from); err != nil {
		t.Skipf("symlink: %v", err)
	}
}

func (s *refreshState) uninstall(t *testing.T, blocks string) {
	t.Helper()
	args, want := []string{"uninstall", s.a}, ""
	if s.global {
		args, want = []string{"uninstall", "-g", s.a}, " i Uninstalling from user scope (~/.apm/)\n"
	}
	want += " > Uninstalling 1 package(s)...\n + _local/dep - found in apm.yml\n" +
		" i Refreshed _local/dep in apm_modules/ for 1 surviving declaration(s)\n" +
		" i Removed _local/dep from dependencies.apm in apm.yml\n" +
		" + Updated " + filepath.Join(s.manifestDir, "apm.yml") + " (removed 1 package(s))\n" + blocks +
		" + Uninstall complete: Removed 1 package(s) from apm.yml\n"
	if got := mustRunCLI(t, args...); got != want {
		t.Errorf("uninstall:\n%s\nwant:\n%s", got, want)
	}
}

// check compares the lock's record of ./b/dep and the files at the deploy
// root (read through any symlink) with want, path to hash, checks that no
// other path of the old record is there, and runs a frozen install.
func (s *refreshState) check(t *testing.T, want map[string]string) {
	t.Helper()
	lock := readFileString(t, filepath.Join(s.manifestDir, "apm.lock.yaml"))
	if record := refreshLockFiles(want) + "  source: local\n  local_path: '" + s.b + "'\n"; !strings.Contains(strings.ReplaceAll(lock, "local_path: "+s.b+"\n", "local_path: '"+s.b+"'\n"), record) {
		t.Errorf("apm.lock.yaml does not record\n%s\nfor ./b/dep:\n%s", record, lock)
	}
	for p := range refreshOld {
		full := filepath.Join(s.deployRoot, filepath.FromSlash(p))
		h, ok := want[p]
		if !ok {
			if _, err := os.Lstat(full); !os.IsNotExist(err) {
				t.Errorf("%s is still there (%v)", p, err)
			}
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			t.Errorf("read %s: %v", p, err)
			continue
		}
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != h {
			t.Errorf("%s = %q, want the content of hash %s", p, data, h)
		}
	}
	if s.global {
		mustRunCLI(t, "install", "-g", "--frozen")
	} else {
		mustRunCLI(t, "install", "--frozen")
	}
}

func without(m map[string]string, drop ...string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if !slices.Contains(drop, k) {
			out[k] = v
		}
	}
	return out
}

// A path the old copy of the package that stays deployed and its new copy
// does not is removed, a file the user edited included, and its lock record
// is made again from what the new copy deploys (commands/uninstall/cli.py:516-524,
// commands/uninstall/lockfile_state.py:69-82; measured for each case, at
// project and user scope, and with an alias).
func TestUninstall_SharedLocalSlot_RemovesWhatTheSourceDropped(t *testing.T) {
	cases := []struct {
		name   string
		change func(t *testing.T, s *refreshState)
		blocks string
		want   map[string]string
	}{
		{"skill file", func(t *testing.T, s *refreshState) { mustRemove(t, s.src(".apm/skills/sb/old.txt")) },
			refreshAgents + refreshCommands + refreshRules + refreshSkills, without(refreshOld, ".claude/skills/sb/old.txt")},
		{"skill", func(t *testing.T, s *refreshState) { mustRemoveAll(t, s.src(".apm/skills/sb")) },
			refreshAgents + refreshCommands + refreshRules, without(refreshOld, ".claude/skills/sb/SKILL.md", ".claude/skills/sb/old.txt")},
		{"agent", func(t *testing.T, s *refreshState) { mustRemove(t, s.src(".apm/agents/rb.agent.md")) },
			refreshCommands + refreshRules + refreshSkills, without(refreshOld, ".claude/agents/rb.md")},
		{"instruction", func(t *testing.T, s *refreshState) { mustRemove(t, s.src(".apm/instructions/ib.instructions.md")) },
			refreshAgents + refreshCommands + refreshSkills, without(refreshOld, ".claude/rules/ib.md")},
		{"command", func(t *testing.T, s *refreshState) { mustRemove(t, s.src(".apm/prompts/cb.prompt.md")) },
			refreshAgents + refreshRules + refreshSkills, without(refreshOld, ".claude/commands/cb.md")},
		{"agent the user edited", func(t *testing.T, s *refreshState) {
			mustRemove(t, s.src(".apm/agents/rb.agent.md"))
			writeCompileFile(t, s.deployRoot, ".claude/agents/rb.md", "user edit\n")
		}, refreshCommands + refreshRules + refreshSkills, without(refreshOld, ".claude/agents/rb.md")},
		{"skill file the user edited", func(t *testing.T, s *refreshState) {
			mustRemove(t, s.src(".apm/skills/sb/old.txt"))
			writeCompileFile(t, s.deployRoot, ".claude/skills/sb/old.txt", "user edit\n")
		}, refreshAgents + refreshCommands + refreshRules + refreshSkills, without(refreshOld, ".claude/skills/sb/old.txt")},
	}
	for _, scope := range []struct {
		name          string
		global, alias bool
	}{{"project", false, false}, {"alias", false, true}, {"global", true, false}} {
		for _, tc := range cases {
			t.Run(scope.name+"/"+tc.name, func(t *testing.T) {
				s := newRefreshState(t, scope.global, scope.alias)
				tc.change(t, s)
				c := s.settled(t)
				s.uninstall(t, tc.blocks)
				s.check(t, tc.want)
				c.check(t)
			})
		}
	}
}

// Links below the deploy root, every source changed. A target root linked
// elsewhere and a directory linked to another place in its target root are
// written through, as deploy.CheckDeployPath bounds a path. A skill's
// directory the user replaced by a symlink is replaced by the skill, and the
// directory it pointed at is left as it was, as install replaces it. A
// deployed file the user replaced by a symlink is the user's: it is not
// written, not counted, and keeps its old lock hash, which the file there
// still has; install refuses the rule there (the item on deployed paths), so
// that case has no install to compare with. Deviations (each measured):
// Oracle 0.33.0 refuses a project target root that is a symlink (`Refusing
// deployment through symlinked target root: .claude`, exit 1), skips the
// agent and the command below a linked directory as files it does not
// manage, replaces a user's leaf symlink by the file, and deletes the content
// of a linked skill directory.
func TestUninstall_SharedLocalSlot_Symlinks(t *testing.T) {
	all := refreshAgents + refreshCommands + refreshRules + refreshSkills
	cases := []struct {
		name   string
		global bool
		links  func(t *testing.T, s *refreshState)
		blocks string
		want   map[string]string
	}{
		{"target root", false, func(t *testing.T, s *refreshState) { s.relink(t, ".claude", "dotclaude") }, all, refreshNew},
		{"global target root", true, func(t *testing.T, s *refreshState) { s.relink(t, ".claude", "dotclaude") }, all, refreshNew},
		{"directories in the target root", false, func(t *testing.T, s *refreshState) {
			for _, d := range []string{"agents", "rules", "commands"} {
				s.relink(t, ".claude/"+d, ".claude/"+d+"real")
			}
		}, all, refreshNew},
		{"deployed files", false, func(t *testing.T, s *refreshState) {
			for _, rel := range []string{"agents/rb.md", "rules/ib.md", "commands/cb.md"} {
				s.relink(t, ".claude/"+rel, "user-"+strings.ReplaceAll(rel, "/", "-"))
			}
		}, refreshSkills, map[string]string{
			".claude/agents/rb.md":       refreshOld[".claude/agents/rb.md"],
			".claude/commands/cb.md":     refreshOld[".claude/commands/cb.md"],
			".claude/rules/ib.md":        refreshOld[".claude/rules/ib.md"],
			".claude/skills/sb/SKILL.md": refreshNew[".claude/skills/sb/SKILL.md"],
			".claude/skills/sb/old.txt":  refreshNew[".claude/skills/sb/old.txt"],
		}},
		{"skill directory", false, func(t *testing.T, s *refreshState) { s.relink(t, ".claude/skills/sb", "userskill") }, all, refreshNew},
		{"global skill directory", true, func(t *testing.T, s *refreshState) { s.relink(t, ".claude/skills/sb", "userskill") }, all, refreshNew},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newRefreshState(t, tc.global, false)
			tc.links(t, s)
			s.changeSources(t)
			c := s.settled(t)
			s.uninstall(t, tc.blocks)
			s.check(t, tc.want)
			if strings.Contains(tc.name, "skill directory") {
				for rel, body := range map[string]string{"userskill/SKILL.md": sharedSkillB, "userskill/old.txt": "old asset\n"} {
					if got := readFileString(t, filepath.Join(s.deployRoot, rel)); got != body {
						t.Errorf("%s = %q, want %q", rel, got, body)
					}
				}
			}
			if tc.name != "deployed files" {
				c.check(t)
			}
		})
	}
}

// The staged copy of the package that stays is validated as the Oracle's
// validate_apm_package validates it (commands/uninstall/engine.py:235-243),
// so a source whose marker does not load ends the uninstall with nothing
// changed, and one that loads is refreshed (measured for each source, at
// project and user scope, and with an alias).
func TestUninstall_SharedLocalSlot_ValidatesStagedCopy(t *testing.T) {
	cases := []struct {
		name  string
		files []string // path, content, ...
		ok    bool
	}{
		{"plugin.json invalid", []string{"plugin.json", "{invalid json"}, false},
		{"plugin.json with no name", []string{"plugin.json", "{}"}, false},
		{"plugin.json with a name", []string{"plugin.json", `{"name": "dep"}`}, true},
		{".claude-plugin/plugin.json invalid", []string{".claude-plugin/plugin.json", "{invalid json"}, false},
		{".claude-plugin/plugin.json empty", []string{".claude-plugin/plugin.json", "{}"}, true},
		{"apm.yml alone", []string{"apm.yml", "name: dep\nversion: 1.0.0\n"}, false},
		{"apm.yml with no version", []string{"apm.yml", "name: dep\n"}, false},
		{"SKILL.md that does not load", []string{"SKILL.md", "---\nname: [unclosed\n---\nbody\n"}, false},
		{"SKILL.md", []string{"SKILL.md", "---\nname: dep\ndescription: d\n---\nbody\n"}, true},
		{"apm.yml and a skill bundle", []string{"apm.yml", "name: dep\nversion: 1.0.0\n", "skills/sx/SKILL.md", "---\nname: sx\ndescription: d\n---\nbody\n"}, true},
		{"apm.yml and hooks", []string{"apm.yml", "name: dep\nversion: 1.0.0\n", "hooks/h.json", "{}"}, false},
		{"apm.yml and a SKILL.md that does not load", []string{"apm.yml", "name: dep\nversion: 1.0.0\n", "SKILL.md", "---\nname: [unclosed\n---\nbody\n"}, true},
		{"apm package and an invalid plugin.json", []string{"apm.yml", "name: dep\nversion: 1.0.0\n", ".apm/skills/sb/SKILL.md", "---\nname: sb\ndescription: d\n---\nbody\n", "plugin.json", "{invalid json"}, true},
	}
	for _, scope := range []struct {
		name          string
		global, alias bool
	}{{"project", false, false}, {"alias", false, true}, {"global", true, false}} {
		for _, tc := range cases {
			t.Run(scope.name+"/"+tc.name, func(t *testing.T) {
				s := newRefreshState(t, scope.global, scope.alias)
				mustRemoveAll(t, s.src(""))
				for i := 0; i < len(tc.files); i += 2 {
					writeCompileFile(t, s.src(""), tc.files[i], tc.files[i+1])
				}
				args := []string{"uninstall", s.a}
				if scope.global {
					args = []string{"uninstall", "-g", s.a}
				}
				before := treeSnapshot(t, filepath.Dir(s.manifestDir))
				stdout, stderr, exitCode := runCompileCLI(t, args...)
				if tc.ok {
					if exitCode != 0 {
						t.Errorf("exit = %d, stderr = %q, stdout:\n%s", exitCode, stderr, stdout)
					}
					return
				}
				want := " > Uninstalling 1 package(s)...\n + _local/dep - found in apm.yml\n" +
					" x Failed to refresh _local/dep from its declared local path. Check the exact path in apm.yml, run 'apm-go install', and retry.\n" +
					" x Error uninstalling packages: A surviving local dependency could not be staged. No manifest changes were made.\n"
				if scope.global {
					want = " i Uninstalling from user scope (~/.apm/)\n" + want
				}
				if exitCode != 1 || stderr != "" || stdout != want {
					t.Errorf("exit = %d, stderr = %q, stdout:\n%s\nwant:\n%s", exitCode, stderr, stdout, want)
				}
				assertTreeUnchanged(t, filepath.Dir(s.manifestDir), before)
			})
		}
	}
}

func mustRemove(t *testing.T, p string) {
	t.Helper()
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
}

func mustRemoveAll(t *testing.T, p string) {
	t.Helper()
	if err := os.RemoveAll(p); err != nil {
		t.Fatal(err)
	}
}

func (s *refreshState) settled(t *testing.T) *settled {
	t.Helper()
	c := &settled{deployRoot: s.deployRoot, manifestDir: s.manifestDir, global: s.global}
	c.start(t)
	return c
}
