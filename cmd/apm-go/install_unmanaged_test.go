package main

import (
	"os"
	"strings"
	"testing"
)

// Issue #144: a project install leaves a file or skill directory the user
// put where a primitive deploys, when the lock before the run does not
// record it, as Oracle 0.33.0's _check_adopt_or_skip does
// (integration/base_integrator.py:198-241,429-495). Each want is the
// Oracle's stdout for the same project (the card's evidence under
// ~/.cache/apm-go-verify/issue144/run{1,2,3}-or/<scenario>/), with the
// changes install_summary_test.go names, and, where the Oracle deletes the
// user's files below .apm/skills/, the deliberate deviation of issue #144
// comment 6234185763.

const (
	unmanagedSkill  = "---\nname: sk\ndescription: d\n---\nskill sk\n"
	unmanagedPrompt = "---\ndescription: d\n---\nprompt pr\n"
	unmanagedRule   = "---\napplyTo: \"**/*.go\"\n---\nrule ru\n"
)

// unmanagedPackage is the package m of the scenarios: an agent, an
// instruction, a skill and a prompt, for targets.
func unmanagedPackage(targets string, files map[string]string) {
	files["m/apm.yml"] = "name: m\nversion: 1.0.0\n"
	files["m/.apm/agents/ag.agent.md"] = summaryAgentFile("ag", "ag")
	files["m/.apm/instructions/ru.instructions.md"] = unmanagedRule
	files["m/.apm/skills/sk/SKILL.md"] = unmanagedSkill
	files["m/.apm/prompts/pr.prompt.md"] = unmanagedPrompt
	files["apm.yml"] = "name: proj\nversion: 1.0.0\ntargets: [" + targets + "]\ndependencies:\n  apm:\n    - ./m\n"
}

// userFiles are the issue's table: a file of the user's at each path the
// package deploys to with claude and copilot.
var userFiles = []string{
	".claude/agents/ag.md",
	".claude/rules/ru.md",
	".claude/skills/sk/SKILL.md",
	".claude/skills/sk/notes.md",
	".claude/commands/pr.md",
	".github/prompts/pr.prompt.md",
	".github/instructions/ru.instructions.md",
	".github/agents/ag.agent.md",
}

func userContent(rel string) string { return "USER " + rel + "\n" }

// assertUserFiles checks that each of kept still has the user's content.
func assertUserFiles(t *testing.T, step string, kept []string) {
	t.Helper()
	for _, rel := range kept {
		got, err := os.ReadFile(rel)
		if err != nil || string(got) != userContent(rel) {
			t.Errorf("%s: %s = %q, %v; want the user's file", step, rel, got, err)
		}
	}
}

// lockPaths is the deployed paths apm.lock.yaml lists, in its order.
func lockPaths(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, line := range strings.Split(string(data), "\n") {
		if p, ok := strings.CutPrefix(line, "  - "); ok && strings.HasPrefix(p, ".") && !strings.HasPrefix(p, "./") {
			paths = append(paths, p)
		}
	}
	return paths
}

func assertLockPaths(t *testing.T, step string, want ...string) {
	t.Helper()
	if got := lockPaths(t); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s: lock records %q, want %q", step, got, want)
	}
}

// The issue's table (run1-or/userfiles, run1-or/frozen, run1-or/uninstall):
// every file of the user's stays and the lock does not record it, but the
// claude rule, which the Oracle takes as its own and writes over. A second
// install keeps them (the Oracle deletes .claude/skills/sk there: the
// deviation), a frozen install and audit pass, also once a recorded file is
// gone, and uninstall leaves them.
func TestInstallUnmanaged_UserFilesKept(t *testing.T) {
	files := map[string]string{}
	unmanagedPackage("claude, copilot", files)
	for _, rel := range userFiles {
		files[rel] = userContent(rel)
	}
	summaryProject(t, files)
	kept := slicesWithout(userFiles, ".claude/rules/ru.md")
	lock := []string{".agents/skills/sk", ".agents/skills/sk/SKILL.md", ".claude/rules/ru.md"}

	runSummary(t, []summaryRun{
		{[]string{"install"}, " > Installing dependencies from apm.yml...\n" +
			" > Resolving ./m...\n" +
			" i Targets: claude, copilot  (source: apm.yml)\n" +
			"   + ./m (local)\n" +
			"  |-- 1 rule(s) integrated -> .claude/rules/\n" +
			" i Added apm_modules/ to .gitignore\n" +
			"   ! 6 files skipped -- local files exist, not managed by APM\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
	})
	assertUserFiles(t, "install", kept)
	assertLockPaths(t, "install", lock...)

	runSummary(t, []summaryRun{
		{[]string{"install"}, " > Installing dependencies from apm.yml...\n" +
			" i Targets: claude, copilot  (source: apm.yml)\n" +
			"   + ./m (local)\n" +
			"  |-- 1 rule(s) adopted -> .claude/rules/\n" +
			"  |-- (files unchanged)\n" +
			"   ! 6 files skipped -- local files exist, not managed by APM\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
	})
	assertUserFiles(t, "second install", kept)
	assertLockPaths(t, "second install", lock...)

	frozen := " + Frozen install: all dependencies pinned and verified\n"
	runSummary(t, []summaryRun{{[]string{"install", "--frozen"}, frozen}})
	if err := os.Remove(".claude/rules/ru.md"); err != nil {
		t.Fatal(err)
	}
	runSummary(t, []summaryRun{{[]string{"install", "--frozen"}, frozen}})
	if _, err := os.Stat(".claude/rules/ru.md"); err != nil {
		t.Errorf("frozen install did not restore the recorded rule: %v", err)
	}
	assertUserFiles(t, "frozen install", kept)
	assertLockPaths(t, "frozen install", lock...)

	runSummary(t, []summaryRun{{[]string{"audit"}, " > Scanning installed packages and deployed files...\n" +
		" + audit: 2 deployed files verified\n"}})

	stdout, code := runCLI86(t, "uninstall", "./m")
	if code != 0 || !strings.Contains(stdout, " i Cleaned 3 stale files from ./m\n") {
		t.Errorf("uninstall: exit %d, stdout:\n%s\nwant exit 0 and 3 files cleaned", code, stdout)
	}
	assertUserFiles(t, "uninstall", kept)
}

func slicesWithout(list []string, drop string) []string {
	var out []string
	for _, s := range list {
		if s != drop {
			out = append(out, s)
		}
	}
	return out
}

// Files that are what the Oracle compares are adopted and recorded, but a
// command: the Oracle compares it with the prompt's source, which it
// converts, so the command is a skipped file (run1-or/same).
func TestInstallUnmanaged_SameAdopted(t *testing.T) {
	files := map[string]string{}
	unmanagedPackage("claude, copilot", files)
	summaryProject(t, files)
	if _, code := runCLI86(t, "install"); code != 0 {
		t.Fatal("first install failed")
	}
	if err := os.Remove("apm.lock.yaml"); err != nil {
		t.Fatal(err)
	}
	runSummary(t, []summaryRun{
		{[]string{"install"}, " > Installing dependencies from apm.yml...\n" +
			" i Targets: claude, copilot  (source: apm.yml)\n" +
			"   + ./m (local)\n" +
			"  |-- 1 prompts adopted -> .github/prompts/\n" +
			"  |-- 2 agents adopted -> .claude/agents/, .github/agents/\n" +
			"  |-- 2 instruction(s) adopted -> .claude/rules/, .github/instructions/\n" +
			"  |-- 1 skill(s) integrated -> .agents/skills/, .claude/skills/\n" +
			"   ! 1 file skipped -- local files exist, not managed by APM\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
	})
	assertLockPaths(t, "install", ".agents/skills/sk", ".agents/skills/sk/SKILL.md",
		".claude/agents/ag.md", ".claude/rules/ru.md", ".claude/skills/sk", ".claude/skills/sk/SKILL.md",
		".github/agents/ag.agent.md", ".github/instructions/ru.instructions.md", ".github/prompts/pr.prompt.md")
}

// A skill directory that dircmp does not find equal to the source, one with
// the same SKILL.md and a file of the user's or an empty one, is left whole
// and is one skipped file (run1-or/skillextra, run1-or/skillempty).
func TestInstallUnmanaged_SkillDirNotTheSource(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra map[string]string
		left  int
	}{
		{"skillextra", map[string]string{".claude/skills/sk/SKILL.md": unmanagedSkill, ".claude/skills/sk/notes.md": "USER\n"}, 2},
		{"skillempty", map[string]string{".claude/skills/sk/.keep": ""}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{}
			unmanagedPackage("claude", files)
			for rel, content := range tc.extra {
				files[rel] = content
			}
			summaryProject(t, files)
			if tc.left == 0 {
				if err := os.Remove(".claude/skills/sk/.keep"); err != nil {
					t.Fatal(err)
				}
			}
			runSummary(t, []summaryRun{
				{[]string{"install"}, " > Installing dependencies from apm.yml...\n" +
					" > Resolving ./m...\n" +
					" i Targets: claude  (source: apm.yml)\n" +
					"   + ./m (local)\n" +
					"  |-- 1 agents integrated -> .claude/agents/\n" +
					"  |-- 1 commands integrated -> .claude/commands/\n" +
					"  |-- 1 rule(s) integrated -> .claude/rules/\n" +
					" i Added apm_modules/ to .gitignore\n" +
					"   ! 1 file skipped -- local files exist, not managed by APM\n" +
					" + Installed 1 APM dependency in <t>s.\n"},
			})
			if entries, err := os.ReadDir(".claude/skills/sk"); err != nil || len(entries) != tc.left {
				t.Errorf(".claude/skills/sk holds %v, %v; want it as the user left it", entries, err)
			}
			assertLockPaths(t, "install", ".claude/agents/ag.md", ".claude/commands/pr.md", ".claude/rules/ru.md")
		})
	}
}

// With a directory of the user's in both skill roots, each is left and
// counts once, on every install: the deviation, where the Oracle replaces
// .agents/skills/sk on the first and .claude/skills/sk on the second
// (run2-or/skill2).
func TestInstallUnmanaged_BothSkillRoots(t *testing.T) {
	files := map[string]string{
		".claude/skills/sk/SKILL.md": userContent(".claude/skills/sk/SKILL.md"),
		".agents/skills/sk/SKILL.md": userContent(".agents/skills/sk/SKILL.md"),
	}
	unmanagedPackage("claude, copilot", files)
	summaryProject(t, files)
	runSummary(t, []summaryRun{
		{[]string{"install"}, " > Installing dependencies from apm.yml...\n" +
			" > Resolving ./m...\n" +
			" i Targets: claude, copilot  (source: apm.yml)\n" +
			"   + ./m (local)\n" +
			"  |-- 1 prompts integrated -> .github/prompts/\n" +
			"  |-- 2 agents integrated -> .claude/agents/, .github/agents/\n" +
			"  |-- 1 commands integrated -> .claude/commands/\n" +
			"  |-- 2 instruction(s) integrated -> .claude/rules/, .github/instructions/\n" +
			" i Added apm_modules/ to .gitignore\n" +
			"   ! 2 files skipped -- local files exist, not managed by APM\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
		{[]string{"install"}, " > Installing dependencies from apm.yml...\n" +
			" i Targets: claude, copilot  (source: apm.yml)\n" +
			"   + ./m (local)\n" +
			"  |-- 1 prompts adopted -> .github/prompts/\n" +
			"  |-- 2 agents adopted -> .claude/agents/, .github/agents/\n" +
			"  |-- 1 commands integrated -> .claude/commands/\n" +
			"  |-- 2 instruction(s) adopted -> .claude/rules/, .github/instructions/\n" +
			"   ! 2 files skipped -- local files exist, not managed by APM\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
	})
	assertUserFiles(t, "install", []string{".claude/skills/sk/SKILL.md", ".agents/skills/sk/SKILL.md"})
}

// A package whose root is a skill leaves each skill directory of the user's,
// one skipped file each, on every install (run3-or/rootskill).
func TestInstallUnmanaged_RootSkill(t *testing.T) {
	files := map[string]string{
		"rs/SKILL.md":                "---\nname: rs\ndescription: d\n---\nroot skill\n",
		"apm.yml":                    "name: proj\nversion: 1.0.0\ntargets: [claude, copilot]\ndependencies:\n  apm:\n    - ./rs\n",
		".claude/skills/rs/SKILL.md": userContent(".claude/skills/rs/SKILL.md"),
		".claude/skills/rs/notes.md": userContent(".claude/skills/rs/notes.md"),
		".agents/skills/rs/SKILL.md": userContent(".agents/skills/rs/SKILL.md"),
	}
	summaryProject(t, files)
	runSummary(t, []summaryRun{
		{[]string{"install"}, " > Installing dependencies from apm.yml...\n" +
			" > Resolving ./rs...\n" +
			" i Targets: claude, copilot  (source: apm.yml)\n" +
			"   + ./rs (local)\n" +
			"  |-- (files unchanged)\n" +
			" i Added apm_modules/ to .gitignore\n" +
			"   ! 2 files skipped -- local files exist, not managed by APM\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
		{[]string{"install"}, " > Installing dependencies from apm.yml...\n" +
			" i Targets: claude, copilot  (source: apm.yml)\n" +
			"   + ./rs (local)\n" +
			"  |-- (files unchanged)\n" +
			"   ! 2 files skipped -- local files exist, not managed by APM\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
	})
	assertUserFiles(t, "install", []string{".claude/skills/rs/SKILL.md", ".claude/skills/rs/notes.md", ".agents/skills/rs/SKILL.md"})
}

// The project's own .apm/ content leaves a file of the user's the same way,
// and with nothing else to record the install writes no lock
// (run1-or/local).
func TestInstallUnmanaged_ProjectContent(t *testing.T) {
	summaryProject(t, map[string]string{
		".apm/agents/la.agent.md": summaryAgentFile("la", "la"),
		".claude/agents/la.md":    userContent(".claude/agents/la.md"),
		"apm.yml":                 "name: proj\nversion: 1.0.0\ntargets: [claude]\n",
	})
	runSummary(t, []summaryRun{
		{[]string{"install"}, " > Installing dependencies from apm.yml...\n" +
			" i Targets: claude  (source: apm.yml)\n" +
			"   + <project root> (local)\n" +
			"  |-- (files unchanged)\n" +
			" i Added apm_modules/ to .gitignore\n" +
			"   ! 1 file skipped -- local files exist, not managed by APM\n" +
			" i No changes -- install state already up to date in <t>s.\n"},
	})
	assertUserFiles(t, "install", []string{".claude/agents/la.md"})
	if _, err := os.Stat("apm.lock.yaml"); !os.IsNotExist(err) {
		t.Errorf("apm.lock.yaml: %v, want none", err)
	}
}

// opencode's agent and command, codex's TOML agent and the shared skill
// directory are left too (run1-or/oc). The Targets line keeps the order of
// apm.yml, an existing difference.
func TestInstallUnmanaged_OpencodeCodex(t *testing.T) {
	kept := []string{".opencode/agents/ag.md", ".opencode/commands/pr.md", ".codex/agents/ag.toml", ".agents/skills/sk/SKILL.md"}
	files := map[string]string{}
	unmanagedPackage("opencode, codex", files)
	for _, rel := range kept {
		files[rel] = userContent(rel)
	}
	summaryProject(t, files)
	runSummary(t, []summaryRun{
		{[]string{"install"}, " > Installing dependencies from apm.yml...\n" +
			" > Resolving ./m...\n" +
			" i Targets: opencode, codex  (source: apm.yml)\n" +
			"   + ./m (local)\n" +
			"  |-- (files unchanged)\n" +
			" i Added apm_modules/ to .gitignore\n" +
			" i Instructions installed for opencode, codex. Run 'apm-go compile' to update AGENTS.md.\n" +
			"   ! 4 files skipped -- local files exist, not managed by APM\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
	})
	assertUserFiles(t, "install", kept)
}

// A path another package's lock record holds is managed: when two packages
// that share a skill and an agent change places in apm.yml, the new first
// one takes both, as in Oracle 0.33.0 (runswap-or/swap: the files and the
// lock are the Oracle's).
func TestInstallUnmanaged_OwnerChange(t *testing.T) {
	files := map[string]string{}
	for _, p := range []string{"a", "b"} {
		files[p+"/apm.yml"] = "name: " + p + "\nversion: 1.0.0\n"
		files[p+"/.apm/skills/sk/SKILL.md"] = "---\nname: sk\ndescription: d\n---\nskill from " + p + "\n"
		files[p+"/.apm/agents/ag.agent.md"] = "---\nname: ag\ndescription: d\n---\nagent from " + p + "\n"
	}
	manifest := func(first, second string) string {
		return "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - ./" + first + "\n    - ./" + second + "\n"
	}
	files["apm.yml"] = manifest("a", "b")
	summaryProject(t, files)
	if _, code := runCLI86(t, "install"); code != 0 {
		t.Fatal("first install failed")
	}
	writeCompileFile(t, ".", "apm.yml", manifest("b", "a"))
	stdout, code := runCLI86(t, "install")
	if code != 0 || strings.Contains(stdout, "skipped") {
		t.Errorf("install after the swap: exit %d, stdout:\n%s", code, stdout)
	}
	for rel, want := range map[string]string{
		".claude/skills/sk/SKILL.md": "---\nname: sk\ndescription: d\n---\nskill from b\n",
		".claude/agents/ag.md":       "---\nname: ag\ndescription: d\n---\nagent from b\n",
	} {
		if got, err := os.ReadFile(rel); err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want b's", rel, got, err)
		}
	}
	data, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if b := strings.Index(string(data), "repo_url: _local/b"); b < 0 || !strings.Contains(string(data)[b:], ".claude/skills/sk/SKILL.md") {
		t.Errorf("lock does not record the skill under _local/b:\n%s", data)
	}
}

// A frozen install writes no lock, so a path the lock does not record that
// has come to hold what the package deploys is left as it is, also when a
// recorded file is gone: the Oracle's --frozen, an ordinary install, adopts
// it and exits 0 (~/.cache/apm-go-verify/issue144/rev4-or/frozenadopt).
func TestInstallUnmanaged_FrozenOverAdoptablePath(t *testing.T) {
	for _, tc := range []struct{ name, path, content string }{
		{"agent", ".claude/agents/ag.md", summaryAgentFile("ag", "ag")},
		{"skill", ".claude/skills/sk/SKILL.md", unmanagedSkill},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{tc.path: userContent(tc.path)}
			unmanagedPackage("claude", files)
			summaryProject(t, files)
			if _, code := runCLI86(t, "install"); code != 0 {
				t.Fatal("install failed")
			}
			before := lockPaths(t)
			writeCompileFile(t, ".", tc.path, tc.content)
			if err := os.Remove(".claude/rules/ru.md"); err != nil {
				t.Fatal(err)
			}
			runSummary(t, []summaryRun{{[]string{"install", "--frozen"}, " + Frozen install: all dependencies pinned and verified\n"}})
			if got, err := os.ReadFile(tc.path); err != nil || string(got) != tc.content {
				t.Errorf("%s = %q, %v; want it as the user left it", tc.path, got, err)
			}
			if _, err := os.Stat(".claude/rules/ru.md"); err != nil {
				t.Errorf("the recorded rule is not restored: %v", err)
			}
			if got := lockPaths(t); strings.Join(got, "\n") != strings.Join(before, "\n") {
				t.Errorf("lock records %q, want %q", got, before)
			}
		})
	}
}
