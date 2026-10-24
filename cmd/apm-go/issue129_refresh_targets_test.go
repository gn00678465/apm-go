package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// sharedSlotPair writes ./a/<name> and ./b/<name> below srcRoot, each with a
// skill s<a|b><name>, as the review's multi.py and tflag.py write them.
func sharedSlotPair(t *testing.T, srcRoot, name string) {
	t.Helper()
	for _, k := range []string{"a", "b"} {
		writeCompileFile(t, srcRoot, k+"/"+name+"/apm.yml", "name: "+name+"\nversion: 1.0.0\n")
		writeCompileFile(t, srcRoot, k+"/"+name+"/.apm/skills/s"+k+name+"/SKILL.md", "---\nname: s"+k+name+"\ndescription: d\n---\nfrom "+k+name+"\n")
	}
}

// addFirstDeclarations declares each of paths before the installed ones in
// apm.yml and the lock, as a second declaration of an installed package's
// directory below apm_modules.
func addFirstDeclarations(t *testing.T, manifestDir, head string, first, installed []string) {
	t.Helper()
	entries := head
	for _, p := range append(append([]string(nil), first...), installed...) {
		entries += "    - '" + p + "'\n"
	}
	writeCompileFile(t, manifestDir, "apm.yml", entries)
	extra := ""
	for _, p := range first {
		extra += "- repo_url: _local/" + filepath.Base(p) + "\n  source: local\n  local_path: '" + p + "'\n"
	}
	lock := readFileString(t, filepath.Join(manifestDir, "apm.lock.yaml"))
	writeCompileFile(t, manifestDir, "apm.lock.yaml", strings.Replace(lock, "dependencies:\n", "dependencies:\n"+extra, 1))
}

// Two shared slots refreshed in one uninstall: ./b/dep's new copy drops the
// agent rb and ./b/x's gains it, so the path goes to ./b/x, with its content,
// recorded under it alone, as the next install gives it (review run multi).
func TestUninstall_SharedLocalSlot_TwoSlotsRefreshed(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(scopeName(global), func(t *testing.T) {
			s := newOtherScope(t, global, true)
			sharedSlotPair(t, s.srcRoot, "dep")
			sharedSlotPair(t, s.srcRoot, "x")
			writeCompileFile(t, s.srcRoot, "b/dep/.apm/agents/rb.agent.md", "---\nname: rb\ndescription: d\n---\nagent bdep\n")
			ref := func(k, n string) string {
				if global {
					return filepath.Join(s.srcRoot, k, n)
				}
				return "./" + k + "/" + n
			}
			head := "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n"
			installed := []string{ref("b", "dep"), ref("b", "x")}
			writeCompileFile(t, s.manifestDir, "apm.yml", head+"    - '"+installed[0]+"'\n    - '"+installed[1]+"'\n")
			if global {
				chdirTemp(t)
			} else {
				t.Chdir(s.deployRoot)
			}
			s.run(t, "install")
			addFirstDeclarations(t, s.manifestDir, head, []string{ref("a", "dep"), ref("a", "x")}, installed)
			mustRemove(t, filepath.Join(s.srcRoot, "b", "dep", ".apm", "agents", "rb.agent.md"))
			writeCompileFile(t, s.srcRoot, "b/x/.apm/agents/rb.agent.md", "---\nname: rb\ndescription: d\n---\nagent bx\n")
			writeCompileFile(t, s.srcRoot, "b/x/.apm/skills/sbx/SKILL.md", "---\nname: sbx\ndescription: d\n---\nNEW bx\n")
			c := &settled{deployRoot: s.deployRoot, manifestDir: s.manifestDir, global: global}
			c.start(t)
			s.run(t, "uninstall", ref("a", "dep"), ref("a", "x"))
			for rel, body := range map[string]string{
				".claude/agents/rb.md":        "---\nname: rb\ndescription: d\n---\nagent bx\n",
				".claude/skills/sbx/SKILL.md": "---\nname: sbx\ndescription: d\n---\nNEW bx\n",
			} {
				if got := readFileString(t, filepath.Join(s.deployRoot, filepath.FromSlash(rel))); got != body {
					t.Errorf("%s = %q, want %q", rel, got, body)
				}
			}
			lock := readFileString(t, filepath.Join(s.manifestDir, "apm.lock.yaml"))
			node, err := yamlcore.SafeLoad([]byte(lock))
			if err != nil {
				t.Fatal(err)
			}
			lf, err := lockfile.ParseLockfile(node)
			if err != nil {
				t.Fatal(err)
			}
			dep, x := lf.FindByKey("_local/dep"), lf.FindByKey("_local/x")
			if dep == nil || x == nil || slices.Contains(dep.DeployedFiles, ".claude/agents/rb.md") || !slices.Contains(x.DeployedFiles, ".claude/agents/rb.md") {
				t.Errorf("want .claude/agents/rb.md recorded under ./b/x alone:\n%s", lock)
			}
			c.check(t)
		})
	}
}

// A project with no targets:, installed with --target claude,copilot, where
// the signals name several harnesses (or none): the next install with no
// --target is refused, so the refresh takes the targets from the paths the
// lock records that one target alone governs, and deploys nothing for codex
// or opencode, which govern .agents/skills too (review runs tflag, tflag2).
// With no agent, the paths of the lock name claude alone, and the skill copilot
// recorded below .agents/skills is refreshed as a path of the old record.
func TestUninstall_SharedLocalSlot_TargetFlag(t *testing.T) {
	for _, tc := range []struct {
		signal string
		agent  bool
	}{{"none", true}, {"CLAUDE.md", true}, {"none", false}} {
		signal := tc.signal
		t.Run(signal+"/"+map[bool]string{true: "agent", false: "skill"}[tc.agent], func(t *testing.T) {
			s := newOtherScope(t, false, true)
			sharedSlotPair(t, s.srcRoot, "dep")
			if tc.agent {
				writeCompileFile(t, s.srcRoot, "b/dep/.apm/agents/rb.agent.md", "---\nname: rb\ndescription: d\n---\nagent b\n")
			}
			if signal != "none" {
				writeCompileFile(t, s.deployRoot, signal, "# user file\n")
			}
			head := "name: proj\nversion: 1.0.0\ndependencies:\n  apm:\n"
			writeCompileFile(t, s.manifestDir, "apm.yml", head+"    - ./b/dep\n")
			t.Chdir(s.deployRoot)
			mustRunCLI(t, "install", "--target", "claude,copilot")
			addFirstDeclarations(t, s.manifestDir, head, []string{"./a/dep"}, []string{"./b/dep"})
			writeCompileFile(t, s.srcRoot, "b/dep/.apm/skills/sbdep/SKILL.md", "---\nname: sbdep\ndescription: d\n---\nNEW b\n")
			c := &settled{deployRoot: s.deployRoot, manifestDir: s.manifestDir, args: []string{"--target", "claude,copilot"},
				roots: []string{".claude/", ".github/", ".agents/skills/"}}
			c.start(t)
			mustRunCLI(t, "uninstall", "./a/dep")
			for _, rel := range []string{".claude/skills/sbdep/SKILL.md", ".agents/skills/sbdep/SKILL.md"} {
				if got := readFileString(t, filepath.Join(s.deployRoot, filepath.FromSlash(rel))); !strings.HasSuffix(got, "NEW b\n") {
					t.Errorf("%s = %q, want the new copy's skill", rel, got)
				}
			}
			for _, d := range []string{".codex", ".opencode"} {
				if _, err := os.Lstat(filepath.Join(s.deployRoot, d)); !os.IsNotExist(err) {
					t.Errorf("uninstall made %s (%v)", d, err)
				}
			}
			c.check(t)
		})
	}
}

// ./b/dep's new copy gains a skill sz where the user has a directory of their
// own, or a symlink to one, that no lock records (review runs
// g-ugainskill, g-ugainskilllink): the refresh leaves it, and the lock records
// no .claude/skills/sz for ./b/dep with no file below it, as install drops
// such an entry (withoutOrphanSkillDirs), so the next install changes no lock
// and the next uninstall warns about nothing.
func TestUninstall_SharedLocalSlot_GainedSkillOverUserDir(t *testing.T) {
	for _, tc := range []struct {
		global, link bool
	}{{false, false}, {false, true}, {true, false}, {true, true}} {
		global := tc.global
		t.Run(scopeName(global)+map[bool]string{false: "/dir", true: "/link"}[tc.link], func(t *testing.T) {
			s := newOtherScope(t, global, true)
			writeCompileFile(t, s.srcRoot, "b/dep/.apm/skills/sb/SKILL.md", "---\nname: sb\ndescription: d\n---\nfrom b\n")
			head := "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n"
			writeCompileFile(t, s.manifestDir, "apm.yml", head+"    - '"+s.b+"'\n")
			if global {
				chdirTemp(t)
			} else {
				t.Chdir(s.deployRoot)
			}
			s.run(t, "install")
			addFirstDeclarations(t, s.manifestDir, head, []string{s.a}, []string{s.b})
			writeCompileFile(t, s.srcRoot, "b/dep/.apm/skills/sz/SKILL.md", "---\nname: sz\ndescription: d\n---\nfrom b\n")
			if tc.link {
				writeCompileFile(t, s.deployRoot, "uz/SKILL.md", "user skill\n")
				if err := os.Symlink("../../uz", filepath.Join(s.deployRoot, ".claude", "skills", "sz")); err != nil {
					t.Fatal(err)
				}
			} else {
				writeCompileFile(t, s.deployRoot, ".claude/skills/sz/SKILL.md", "user skill\n")
			}
			c := &settled{deployRoot: s.deployRoot, manifestDir: s.manifestDir, global: global, user: []string{".claude/skills/sz"}, noFrozen: global && !tc.link}
			c.start(t)
			stdout := s.run(t, "uninstall", s.a)
			if got := readFileString(t, filepath.Join(s.deployRoot, ".claude", "skills", "sz", "SKILL.md")); got != "user skill\n" {
				t.Errorf("user skill = %q, want it kept", got)
			}
			lock := readFileString(t, filepath.Join(s.manifestDir, "apm.lock.yaml"))
			if strings.Contains(lock, ".claude/skills/sz") || strings.Contains(stdout, "keeping") {
				t.Errorf("lock records the user's skill directory, or the uninstall warns:\n%s\n%s", lock, stdout)
			}
			if !global || !tc.link {
				c.check(t)
				return
			}
			// install -g replaces the user's symlink by its own (#43), which the
			// refresh, like a project deploy, leaves (PRODUCT.md, the exception
			// for --global); the next uninstall still warns about nothing.
			if out := s.run(t, "uninstall", s.b); strings.Contains(out, "keeping") {
				t.Errorf("uninstall of ./b/dep warns:\n%s", out)
			}
		})
	}
}
