package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Issue #122: the install transcript is Oracle 0.33.0's. Each want below is
// the Oracle's stdout for the same project and commands (the directory
// named with it, under the card's evidence), with apm-go's status symbols,
// apm-go in hint text, and without the lines the rulings drop: the two
// experimental-resolver lines (issue #50 ruling R10) and the collision
// warning's "Use 'apm install --force' to overwrite" (issue #122 comment
// 6232421913 item 2).

const (
	summaryAgent = "---\nname: %s\ndescription: d\n---\nbody %s\n"
)

func summaryAgentFile(name, body string) string {
	return strings.ReplaceAll(strings.Replace(summaryAgent, "%s", name, 1), "%s", body)
}

// summaryProject writes files below <root>/proj, makes it the working
// directory and returns root.
func summaryProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeCompileFile(t, root, "gitconfig", "")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	// Windows takes the home directory from USERPROFILE.
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	for rel, content := range files {
		writeCompileFile(t, root, "proj/"+rel, content)
	}
	t.Chdir(filepath.Join(root, "proj"))
	return root
}

type summaryRun struct {
	args []string
	want string
}

func runSummary(t *testing.T, runs []summaryRun) {
	t.Helper()
	for i, r := range runs {
		stdout, code := runCLI86(t, r.args...)
		if code != 0 || stdout != r.want {
			t.Errorf("run %d %v: exit %d, stdout:\n%s\nwant exit 0, stdout:\n%s", i+1, r.args, code, stdout, r.want)
		}
	}
}

func onePkgProject(t *testing.T, deps string, files map[string]string) {
	t.Helper()
	files["apm.yml"] = "name: proj\nversion: 1.0.0\ndependencies:\n  apm:\n" + deps
	summaryProject(t, files)
}

// Four targets collapse to "4 targets"; the counts of every target add up,
// adopted apart, and a codex agent, which is converted, is integrated on
// every run (m2-or/ord4; install/services.py:440-600,
// install/deployed_paths.py:13-24). The Targets line is given in name order:
// the order of --target is #125's.
func TestInstallSummary_FourTargets(t *testing.T) {
	onePkgProject(t, "    - ./pkg\n", map[string]string{
		"pkg/apm.yml":                              "name: pkg\nversion: 1.0.0\n",
		"pkg/.apm/agents/ag.agent.md":              summaryAgentFile("ag", "ag"),
		"pkg/.apm/instructions/ru.instructions.md": "---\napplyTo: \"**/*.go\"\n---\nrule ru\n",
		"pkg/.apm/skills/sk/SKILL.md":              "---\nname: sk\ndescription: d\n---\nskill sk\n",
		"pkg/.apm/prompts/pr.prompt.md":            "---\ndescription: d\n---\nprompt pr\n",
	})
	args := []string{"install", "--target", "claude,codex,copilot,opencode"}
	runSummary(t, []summaryRun{
		{args, " > Installing dependencies from apm.yml...\n" +
			" > Resolving ./pkg...\n" +
			" i Targets: claude, codex, copilot, opencode  (source: --target flag)\n" +
			"   + ./pkg (local)\n" +
			"  |-- 1 prompts integrated -> .github/prompts/\n" +
			"  |-- 4 agents integrated -> 4 targets\n" +
			"  |-- 2 commands integrated -> .claude/commands/, .opencode/commands/\n" +
			"  |-- 2 instruction(s) integrated -> .claude/rules/, .github/instructions/\n" +
			"  |-- 1 skill(s) integrated -> .agents/skills/, .claude/skills/\n" +
			" i Added apm_modules/ to .gitignore\n" +
			" i Instructions installed for codex, opencode. Run 'apm-go compile' to update AGENTS.md.\n" +
			"\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
		{args, " > Installing dependencies from apm.yml...\n" +
			" i Targets: claude, codex, copilot, opencode  (source: --target flag)\n" +
			"   + ./pkg (local)\n" +
			"  |-- 1 prompts adopted -> .github/prompts/\n" +
			"  |-- 1 agents integrated (3 adopted) -> 4 targets\n" +
			"  |-- 2 commands integrated -> .claude/commands/, .opencode/commands/\n" +
			"  |-- 2 instruction(s) adopted -> .claude/rules/, .github/instructions/\n" +
			"  |-- 1 skill(s) integrated -> .agents/skills/, .claude/skills/\n" +
			" i Instructions installed for codex, opencode. Run 'apm-go compile' to update AGENTS.md.\n" +
			"\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
	})
}

// --verbose lists the directories of a line with two targets one per line
// (m2-or/verbose; install/deployed_paths.py:13-24). The Oracle's other
// verbose lines are not ported.
func TestInstallSummary_VerboseExpandsTargets(t *testing.T) {
	onePkgProject(t, "    - ./pkg\n", map[string]string{
		"pkg/apm.yml":                              "name: pkg\nversion: 1.0.0\n",
		"pkg/.apm/agents/ag.agent.md":              summaryAgentFile("ag", "ag"),
		"pkg/.apm/instructions/ru.instructions.md": "---\napplyTo: \"**/*.go\"\n---\nrule ru\n",
		"pkg/.apm/skills/sk/SKILL.md":              "---\nname: sk\ndescription: d\n---\nskill sk\n",
		"pkg/.apm/prompts/pr.prompt.md":            "---\ndescription: d\n---\nprompt pr\n",
	})
	stdout, code := runCLI86(t, "install", "--target", "claude,copilot", "--verbose")
	block := "   + ./pkg (local)\n" +
		"  |-- 1 prompts integrated -> .github/prompts/\n" +
		"  |-- 2 agents integrated:\n" +
		"  |     -> .claude/agents/\n" +
		"  |     -> .github/agents/\n" +
		"  |-- 1 commands integrated -> .claude/commands/\n" +
		"  |-- 2 instruction(s) integrated:\n" +
		"  |     -> .claude/rules/\n" +
		"  |     -> .github/instructions/\n" +
		"  |-- 1 skill(s) integrated:\n" +
		"  |     -> .agents/skills/\n" +
		"  |     -> .claude/skills/\n"
	if code != 0 || !strings.Contains(stdout, block) {
		t.Errorf("exit %d, stdout:\n%s\nwant exit 0 and the block:\n%s", code, stdout, block)
	}
}

// Two packages with an agent of one name: every header first, then the
// details of each package; the second package's agents are skipped files
// (m2-or/colmix/i1.out; utils/diagnostics.py:455-463), and its own agent
// is counted.
func TestInstallSummary_AgentCollision(t *testing.T) {
	onePkgProject(t, "    - ./m\n    - ./z\n", map[string]string{
		"m/apm.yml":                   "name: m\nversion: 1.0.0\n",
		"m/.apm/agents/ag.agent.md":   summaryAgentFile("ag", "ag") + "m\n",
		"z/apm.yml":                   "name: z\nversion: 1.0.0\n",
		"z/.apm/agents/ag.agent.md":   summaryAgentFile("ag", "ag") + "z\n",
		"z/.apm/agents/only.agent.md": summaryAgentFile("only", "only"),
	})
	runSummary(t, []summaryRun{
		{[]string{"install", "--target", "claude,copilot"}, " > Installing dependencies from apm.yml...\n" +
			" > Resolving ./m...\n" +
			" > Resolving ./z...\n" +
			" i Targets: claude, copilot  (source: --target flag)\n" +
			"   + ./m (local)\n" +
			"   + ./z (local)\n" +
			"  |-- 2 agents integrated -> .claude/agents/, .github/agents/\n" +
			"  |-- 2 agents integrated -> .claude/agents/, .github/agents/\n" +
			" i Added apm_modules/ to .gitignore\n" +
			"   ! 2 files skipped -- local files exist, not managed by APM\n" +
			" + Installed 2 APM dependencies in <t>s.\n"},
	})
}

// A skill two packages have is one skipped file, whatever the number of
// skill roots (m2-or/skcol-claude, m2-or/skcol-claudecodex).
func TestInstallSummary_SkillCollision(t *testing.T) {
	for _, tt := range []struct{ target, roots string }{
		{"claude", ".claude/skills/"},
		{"claude,codex", ".agents/skills/, .claude/skills/"},
	} {
		t.Run(tt.target, func(t *testing.T) {
			onePkgProject(t, "    - ./m\n    - ./z\n", map[string]string{
				"m/apm.yml":                 "name: m\nversion: 1.0.0\n",
				"m/.apm/skills/sk/SKILL.md": "---\nname: sk\ndescription: d\n---\nskill sk\nm\n",
				"z/apm.yml":                 "name: z\nversion: 1.0.0\n",
				"z/.apm/skills/sk/SKILL.md": "---\nname: sk\ndescription: d\n---\nskill sk\nz\n",
			})
			runSummary(t, []summaryRun{
				{[]string{"install", "--target", tt.target}, " > Installing dependencies from apm.yml...\n" +
					" > Resolving ./m...\n" +
					" > Resolving ./z...\n" +
					" i Targets: " + strings.ReplaceAll(tt.target, ",", ", ") + "  (source: --target flag)\n" +
					"   + ./m (local)\n" +
					"   + ./z (local)\n" +
					"  |-- 1 skill(s) integrated -> " + tt.roots + "\n" +
					"  |-- (files unchanged)\n" +
					" i Added apm_modules/ to .gitignore\n" +
					"   ! 1 file skipped -- local files exist, not managed by APM\n" +
					" + Installed 2 APM dependencies in <t>s.\n"},
			})
		})
	}
}

// An empty .apm/ still gives a <project root> block (r14-or/emptyapm,
// m2-or/emptyapm; install/phases/local_content.py:45-56). It integrated
// nothing, so it is not counted.
func TestInstallSummary_EmptyAPMDirHasProjectBlock(t *testing.T) {
	onePkgProject(t, "    - ./dep\n", map[string]string{
		"dep/apm.yml":                 "name: dep\nversion: 1.0.0\n",
		"dep/.apm/agents/ag.agent.md": summaryAgentFile("ag", "ag"),
	})
	if err := os.Mkdir(".apm", 0o755); err != nil {
		t.Fatal(err)
	}
	runSummary(t, []summaryRun{
		{[]string{"install", "--target", "claude"}, " > Installing dependencies from apm.yml...\n" +
			" > Resolving ./dep...\n" +
			" i Targets: claude  (source: --target flag)\n" +
			"   + ./dep (local)\n" +
			"  |-- 1 agents integrated -> .claude/agents/\n" +
			"   + <project root> (local)\n" +
			"  |-- (files unchanged)\n" +
			" i Added apm_modules/ to .gitignore\n" +
			"\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
	})
}

// The empty line before the last one marks a run with no diagnostics. A
// targets: field in apm.yml gives the Oracle two diagnostics when anything
// is integrated, which apm-go does not print (R10), so there is no empty
// line (m2-or/blanktg); an install of MCP servers alone has none of them
// and keeps the line (r10probe1/mcp, measured with Oracle 0.33.0).
func TestInstallSummary_BlankLineWithManifestTargets(t *testing.T) {
	summaryProject(t, map[string]string{
		"apm.yml":                     "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - ./dep\n",
		"dep/apm.yml":                 "name: dep\nversion: 1.0.0\n",
		"dep/.apm/agents/ag.agent.md": summaryAgentFile("ag", "ag"),
	})
	runSummary(t, []summaryRun{
		{[]string{"install"}, " > Installing dependencies from apm.yml...\n" +
			" > Resolving ./dep...\n" +
			" i Targets: claude  (source: apm.yml)\n" +
			"   + ./dep (local)\n" +
			"  |-- 1 agents integrated -> .claude/agents/\n" +
			" i Added apm_modules/ to .gitignore\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
	})
}

// A package whose root is a skill says "Skill integrated" only when the
// skill's directory was new (r2-or/rootskill).
func TestInstallSummary_RootSkillRerun(t *testing.T) {
	summaryProject(t, map[string]string{
		"apm.yml":      "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - ./pkg\n",
		"pkg/SKILL.md": "---\nname: pkg\ndescription: d\n---\nroot skill\n",
	})
	runSummary(t, []summaryRun{
		{[]string{"install"}, " > Installing dependencies from apm.yml...\n" +
			" > Resolving ./pkg...\n" +
			" i Targets: claude  (source: apm.yml)\n" +
			"   + ./pkg (local)\n" +
			"  |-- Skill integrated -> .claude/skills/\n" +
			" i Added apm_modules/ to .gitignore\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
		{[]string{"install"}, " > Installing dependencies from apm.yml...\n" +
			" i Targets: claude  (source: apm.yml)\n" +
			"   + ./pkg (local)\n" +
			"  |-- (files unchanged)\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
	})
}

// install <pkg> lists the named package alone (r2-or/scoped).
func TestInstallSummary_ScopedListsNamedPackage(t *testing.T) {
	summaryProject(t, map[string]string{
		"apm.yml":                   "name: proj\nversion: 1.0.0\ntargets: [claude]\n",
		"a/apm.yml":                 "name: a\nversion: 1.0.0\n",
		"a/.apm/skills/sa/SKILL.md": "---\nname: sa\ndescription: d\n---\na\n",
		"b/apm.yml":                 "name: b\nversion: 1.0.0\n",
		"b/.apm/skills/sb/SKILL.md": "---\nname: sb\ndescription: d\n---\nb\n",
	})
	runSummary(t, []summaryRun{
		{[]string{"install", "./a"}, " + Validating 1 package...\n" +
			" + ./a\n" +
			" + Updated apm.yml with 1 new package(s)\n" +
			" > Installing 1 new package...\n" +
			" > Resolving ./a...\n" +
			" i Targets: claude  (source: apm.yml)\n" +
			"   + ./a (local)\n" +
			"  |-- 1 skill(s) integrated -> .claude/skills/\n" +
			" i Added apm_modules/ to .gitignore\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
		{[]string{"install", "./b"}, " + Validating 1 package...\n" +
			" + ./b\n" +
			" + Updated apm.yml with 1 new package(s)\n" +
			" > Installing 1 new package...\n" +
			" > Resolving ./b...\n" +
			" i Targets: claude  (source: apm.yml)\n" +
			"   + ./b (local)\n" +
			"  |-- 1 skill(s) integrated -> .claude/skills/\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
	})
}

// A hook is integrated on every run, and so is a codex agent (m2-or/hooks;
// install/services.py:534-537). The Oracle's per-hook lines describe the
// hook file it rewrites; apm-go copies the file as it is and prints none.
func TestInstallSummary_HooksAndCodexAgentIntegratedOnRerun(t *testing.T) {
	onePkgProject(t, "    - ./pkg\n", map[string]string{
		"pkg/apm.yml":                 "name: pkg\nversion: 1.0.0\n",
		"pkg/.apm/agents/ag.agent.md": summaryAgentFile("ag", "ag"),
		"pkg/.apm/hooks/h.json":       `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo hi"}]}]}}` + "\n",
	})
	args := []string{"install", "--target", "codex,copilot"}
	runCLI86(t, args...)
	runSummary(t, []summaryRun{
		{args, " > Installing dependencies from apm.yml...\n" +
			" i Targets: codex, copilot  (source: --target flag)\n" +
			"   + ./pkg (local)\n" +
			"  |-- 1 agents integrated (1 adopted) -> .codex/agents/, .github/agents/\n" +
			"  |-- 2 hook(s) integrated -> .codex/hooks.json, .github/hooks/\n" +
			"\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
	})
}

// The Targets line names the signals auto-detection found
// (r2-or/autodet; core/target_detection.py:1070-1080,1134-1151).
func TestInstallSummary_AutoDetectSource(t *testing.T) {
	onePkgProject(t, "    - ./dep\n", map[string]string{
		"dep/apm.yml":                 "name: dep\nversion: 1.0.0\n",
		"dep/.apm/agents/ag.agent.md": summaryAgentFile("ag", "ag"),
	})
	if err := os.Mkdir(".claude", 0o755); err != nil {
		t.Fatal(err)
	}
	stdout, code := runCLI86(t, "install")
	if want := " i Targets: claude  (source: auto-detect from .claude/)\n"; code != 0 || !strings.Contains(stdout, want) {
		t.Errorf("exit %d, stdout:\n%s\nwant the line %q", code, stdout, want)
	}
}

// A user-scope install writes no .gitignore: the Oracle's global install
// has no such step (r4-or/global2).
func TestInstallSummary_GlobalWritesNoGitignore(t *testing.T) {
	root := summaryProject(t, map[string]string{
		"dep/apm.yml":                 "name: dep\nversion: 1.0.0\n",
		"dep/.apm/agents/ag.agent.md": summaryAgentFile("ag", "ag"),
	})
	home := filepath.Join(root, "home")
	writeCompileFile(t, home, ".apm/apm.yml", "name: g\nversion: 1.0.0\ndependencies:\n  apm:\n    - "+filepath.Join(root, "proj", "dep")+"\n")
	stdout, code := runCLI86(t, "install", "-g", "--target", "claude")
	if code != 0 || strings.Contains(stdout, ".gitignore") {
		t.Errorf("exit %d, stdout:\n%s\nwant exit 0 and no .gitignore line", code, stdout)
	}
	for _, p := range []string{filepath.Join(home, ".apm", ".gitignore"), filepath.Join(root, "proj", ".gitignore")} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s exists (Lstat err = %v)", p, err)
		}
	}
}

// The count of skipped files on the first install is the Oracle's for each
// collision (r2-or/collide, r2-or/shadow, r6-or/colmulti, r6-or/colproj,
// issue107/col2/or-c7 for the plugin with two staged agents of one name).
// Which package wins and what its block shows is apm-go's own (issue #50
// ruling R1), so the blocks are not compared. A rerun has no skipped file,
// since the Oracle's lock then manages every path.
func TestInstallSummary_CollisionCounts(t *testing.T) {
	rule := func(body string) string { return "---\napplyTo: \"**/*.go\"\n---\nrule " + body + "\n" }
	for _, tt := range []struct {
		name, deps, target, want string
		files                    map[string]string
	}{
		{"collide", "    - ./m\n    - ./a\n    - ./z\n", "claude", "1 file skipped", map[string]string{
			"m/apm.yml": "name: m\nversion: 1.0.0\n", "m/.apm/agents/shag.agent.md": summaryAgentFile("shag", "m"),
			"m/.apm/instructions/shared.instructions.md": rule("m"),
			"a/apm.yml": "name: a\nversion: 1.0.0\n", "a/.apm/instructions/shared.instructions.md": rule("a"),
			"z/apm.yml": "name: z\nversion: 1.0.0\n", "z/.apm/agents/shag.agent.md": summaryAgentFile("shag", "z"),
		}},
		{"shadow", "    - ./m\n    - ./z\n", "claude", "1 file skipped", map[string]string{
			"m/apm.yml": "name: m\nversion: 1.0.0\n", "m/.apm/agents/shag.agent.md": summaryAgentFile("shag", "m"),
			"z/apm.yml": "name: z\nversion: 1.0.0\n", "z/.apm/agents/shag.agent.md": summaryAgentFile("shag", "z"),
		}},
		{"colmulti", "    - ./m\n    - ./z\n", "claude,copilot", "6 files skipped", map[string]string{
			"m/apm.yml": "name: m\nversion: 1.0.0\n", "m/.apm/agents/ag.agent.md": summaryAgentFile("ag", "m"),
			"m/.apm/instructions/ru.instructions.md": rule("m"),
			"m/.apm/prompts/pr.prompt.md":            "---\ndescription: d\n---\nprompt m\n",
			"m/.apm/skills/sk/SKILL.md":              "---\nname: sk\ndescription: d\n---\nskill m\n",
			"z/apm.yml":                              "name: z\nversion: 1.0.0\n", "z/.apm/agents/ag.agent.md": summaryAgentFile("ag", "z"),
			"z/.apm/instructions/ru.instructions.md": rule("z"),
			"z/.apm/prompts/pr.prompt.md":            "---\ndescription: d\n---\nprompt z\n",
			"z/.apm/skills/sk/SKILL.md":              "---\nname: sk\ndescription: d\n---\nskill z\n",
		}},
		{"colproj", "    - ./m\n", "claude", "", map[string]string{
			"m/apm.yml": "name: m\nversion: 1.0.0\n", "m/.apm/agents/ag.agent.md": summaryAgentFile("ag", "m"),
			".apm/agents/ag.agent.md": summaryAgentFile("ag", "proj"),
		}},
		{"plugin", "    - ./plug\n", "claude,copilot", "2 files skipped", map[string]string{
			"plug/.claude-plugin/plugin.json": `{"name":"plug"}` + "\n",
			"plug/agents/rev.md":              "---\nname: rev\ndescription: d\n---\nA\n",
			"plug/.apm/agents/rev.agent.md":   "---\nname: rev\ndescription: d\n---\nB\n",
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			onePkgProject(t, tt.deps, tt.files)
			args := []string{"install", "--target", tt.target}
			first, code := runCLI86(t, args...)
			line := "   ! " + tt.want + " -- local files exist, not managed by APM\n"
			if code != 0 || (tt.want != "" && !strings.Contains(first, line)) || (tt.want == "" && strings.Contains(first, "skipped --")) {
				t.Errorf("first install: exit %d, stdout:\n%s\nwant the line %q", code, first, line)
			}
			if again, code := runCLI86(t, args...); code != 0 || strings.Contains(again, "skipped --") {
				t.Errorf("rerun: exit %d, stdout:\n%s\nwant no skipped files", code, again)
			}
		})
	}
}

// Two packages with an agent of the same bytes: the second one's is adopted,
// not skipped (r6-or/colsame/i1.out; integration/base_integrator.py:378-405).
// The Oracle's Resolving order changes from run to run; apm-go's is the
// order of apm.yml. The targets come from --target in place of the case's
// targets:, so the empty line comes too (r13).
func TestInstallSummary_SameBytesAdopted(t *testing.T) {
	onePkgProject(t, "    - ./m\n    - ./z\n", map[string]string{
		"m/apm.yml": "name: m\nversion: 1.0.0\n", "m/.apm/agents/ag.agent.md": summaryAgentFile("ag", "same"),
		"z/apm.yml": "name: z\nversion: 1.0.0\n", "z/.apm/agents/ag.agent.md": summaryAgentFile("ag", "same"),
	})
	runSummary(t, []summaryRun{
		{[]string{"install", "--target", "claude"}, " > Installing dependencies from apm.yml...\n" +
			" > Resolving ./m...\n" +
			" > Resolving ./z...\n" +
			" i Targets: claude  (source: --target flag)\n" +
			"   + ./m (local)\n" +
			"   + ./z (local)\n" +
			"  |-- 1 agents integrated -> .claude/agents/\n" +
			"  |-- 1 agents adopted -> .claude/agents/\n" +
			"  |-- (files unchanged)\n" +
			" i Added apm_modules/ to .gitignore\n" +
			"\n" +
			" + Installed 2 APM dependencies in <t>s.\n"},
	})
}

// summaryGitRepo makes dir, below the working directory, a git repository on
// branch main with files in one commit, tags it with each of tags, and
// returns the commit.
func summaryGitRepo(t *testing.T, dir string, files map[string]string, tags ...string) string {
	t.Helper()
	for rel, content := range files {
		writeCompileFile(t, dir, rel, content)
	}
	gitCmd(t, dir, "init", "-b", "main")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "init")
	for _, tag := range tags {
		gitCmd(t, dir, "tag", tag)
	}
	return gitCmd(t, dir, "rev-parse", "HEAD")
}

// A git package's header names the ref and commit the run fetched, and on
// a rerun the ones the lock recorded with "(cached)"; with no ref the
// fetched ref is the default branch and a rerun names none
// (r2-or/gitmulti, r4-or/gitpin2, issue122-impl/m1-or/tag;
// install/sources.py:360-400,700-745). Only a fetched package, its
// transitive one included, gets a Resolving line (install/phases/
// resolve.py:430-481), and only a fetched one is counted in the last line.
func TestInstallSummary_GitHeaders(t *testing.T) {
	summaryProject(t, map[string]string{"apm.yml": "name: proj\nversion: 1.0.0\n"})
	useLocalGitRemotes(t)
	agentRepo := func(name, deps string) map[string]string {
		return map[string]string{
			"apm.yml":                           "name: " + name + "\nversion: 1.0.0\n" + deps,
			".apm/agents/" + name + ".agent.md": summaryAgentFile(name, name),
		}
	}
	c := summaryGitRepo(t, "o/c", agentRepo("c", ""))
	a := summaryGitRepo(t, "o/a", agentRepo("a", "dependencies:\n  apm:\n    - o/c\n"))
	b := summaryGitRepo(t, "o/b", agentRepo("b", ""))
	tagged := summaryGitRepo(t, "o/t", agentRepo("t", ""), "v1.0.0")

	t.Run("no ref, with a transitive package", func(t *testing.T) {
		writeCompileFile(t, ".", "apm.yml", "name: proj\nversion: 1.0.0\ndependencies:\n  apm:\n    - o/a\n")
		args := []string{"install", "--target", "claude"}
		unpinned := "   ! 2 dependencies unpinned: o/a, o/c -- add #tag or #sha to prevent drift\n"
		runSummary(t, []summaryRun{
			{args, " > Installing dependencies from apm.yml...\n" +
				" > Resolving o/a...\n" +
				" > Resolving o/c...\n" +
				" i Targets: claude  (source: --target flag)\n" +
				"   + o/a #main @" + a[:8] + "\n" +
				"   + o/c #main @" + c[:8] + "\n" +
				"  |-- 1 agents integrated -> .claude/agents/\n" +
				"  |-- 1 agents integrated -> .claude/agents/\n" +
				" i Added apm_modules/ to .gitignore\n" +
				unpinned +
				" + Installed 2 APM dependencies in <t>s.\n"},
			{args, " > Installing dependencies from apm.yml...\n" +
				" i Targets: claude  (source: --target flag)\n" +
				"   + o/a @" + a[:8] + " (cached)\n" +
				"   + o/c @" + c[:8] + " (cached)\n" +
				"  |-- 1 agents adopted -> .claude/agents/\n" +
				"  |-- (files unchanged)\n" +
				"  |-- 1 agents adopted -> .claude/agents/\n" +
				"  |-- (files unchanged)\n" +
				unpinned +
				" i No changes -- install state already up to date in <t>s.\n"},
		})
	})
	for _, tt := range []struct{ name, entry, ref, commit string }{
		{"full sha", "o/b#" + b, b, b},
		{"tag", "o/t#v1.0.0", "v1.0.0", tagged},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.RemoveAll("apm_modules"); err != nil {
				t.Fatal(err)
			}
			for _, f := range []string{"apm.lock.yaml", ".gitignore"} {
				if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			}
			if err := os.RemoveAll(".claude"); err != nil {
				t.Fatal(err)
			}
			writeCompileFile(t, ".", "apm.yml", "name: proj\nversion: 1.0.0\ndependencies:\n  apm:\n    - "+tt.entry+"\n")
			repo := strings.SplitN(tt.entry, "#", 2)[0]
			args := []string{"install", "--target", "claude"}
			runSummary(t, []summaryRun{
				{args, " > Installing dependencies from apm.yml...\n" +
					" > Resolving " + repo + "...\n" +
					" i Targets: claude  (source: --target flag)\n" +
					"   + " + repo + " #" + tt.ref + " @" + tt.commit[:8] + "\n" +
					"  |-- 1 agents integrated -> .claude/agents/\n" +
					" i Added apm_modules/ to .gitignore\n" +
					"\n" +
					" + Installed 1 APM dependency in <t>s.\n"},
				{args, " > Installing dependencies from apm.yml...\n" +
					" i Targets: claude  (source: --target flag)\n" +
					"   + " + repo + " #" + tt.ref + " @" + tt.commit[:8] + " (cached)\n" +
					"  |-- 1 agents adopted -> .claude/agents/\n" +
					"  |-- (files unchanged)\n" +
					"\n" +
					" i No changes -- install state already up to date in <t>s.\n"},
			})
		})
	}
}

// summaryTarGz is a tar.gz of files, in name order.
func summaryTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, n := range names {
		if err := tw.WriteHeader(&tar.Header{Name: n, Size: int64(len(files[n])), Mode: 0o644}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(files[n])); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A registry package and the one it declares, as the card's fake registry
// served them (issue69-registry-transitive): the header is the version the
// lock records, "(cached)" on a rerun, which fetches nothing and prints no
// Resolving line (r2-or/reg).
func TestInstallSummary_RegistryWithTransitive(t *testing.T) {
	enableRegistries(t)
	archives := map[string][]byte{
		"sample": summaryTarGz(t, map[string]string{"apm.yml": "name: sample\nversion: 1.0.0\ndependencies:\n  apm:\n" +
			"    - id: acme/child\n      registry: local\n      version: 1.0.0\n      targets: [copilot]\n"}),
		"child": summaryTarGz(t, map[string]string{"apm.yml": "name: child\nversion: 1.0.0\n",
			".apm/agents/child.agent.md": summaryAgentFile("child", "child")}),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "sample"
		if strings.Contains(r.URL.Path, "/child/") {
			key = "child"
		}
		sum := sha256.Sum256(archives[key])
		switch {
		case strings.HasSuffix(r.URL.Path, "/versions"):
			fmt.Fprintf(w, `{"package":"acme/%s","versions":[{"version":"1.0.0","digest":"sha256:%s","published_at":"2026-01-01T00:00:00Z"}]}`, key, hex.EncodeToString(sum[:]))
		case strings.HasSuffix(r.URL.Path, "/download"):
			w.Write(archives[key])
		default:
			http.Error(w, "nf", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	summaryProject(t, map[string]string{"apm.yml": "name: proj\nversion: 1.0.0\ntargets: [claude, copilot]\n" +
		"registries:\n  local:\n    url: " + srv.URL + "\n    insecure: true\n  default: local\n" +
		"dependencies:\n  apm:\n    - id: acme/sample\n      version: 1.0.0\n"})
	runSummary(t, []summaryRun{
		{[]string{"install"}, " > Installing dependencies from apm.yml...\n" +
			" > Resolving acme/sample...\n" +
			" > Resolving acme/child...\n" +
			" i Targets: claude, copilot  (source: apm.yml)\n" +
			"   + acme/sample #1.0.0\n" +
			"   + acme/child #1.0.0\n" +
			"  |-- (files unchanged)\n" +
			"  |-- 1 agents integrated -> .github/agents/\n" +
			" i Added apm_modules/ to .gitignore\n" +
			" + Installed 2 APM dependencies in <t>s.\n"},
		{[]string{"install"}, " > Installing dependencies from apm.yml...\n" +
			" i Targets: claude, copilot  (source: apm.yml)\n" +
			"   + acme/sample #1.0.0 (cached)\n" +
			"   + acme/child #1.0.0 (cached)\n" +
			"  |-- (files unchanged)\n" +
			"  |-- 1 agents adopted -> .github/agents/\n" +
			"  |-- (files unchanged)\n" +
			" i No changes -- install state already up to date in <t>s.\n"},
	})
}

// A git package whose apm_modules directory leads out of apm_modules ends
// the install with the Oracle's lines and no Resolving line for it
// (r4-or/gitrefuse/i1.out; commands/install.py:1948-1957). With --verbose
// the hint to rerun with it is left out (r4-or/regrefuse2/i1.out).
func TestInstallSummary_GitContainmentRefused(t *testing.T) {
	for _, verbose := range []bool{false, true} {
		t.Run(fmt.Sprintf("verbose=%v", verbose), func(t *testing.T) {
			root := summaryProject(t, map[string]string{"apm.yml": "name: proj\nversion: 1.0.0\ndependencies:\n  apm:\n    - o/b\n"})
			useLocalGitRemotes(t)
			summaryGitRepo(t, "o/b", map[string]string{"apm.yml": "name: b\nversion: 1.0.0\n"})
			if err := os.MkdirAll(filepath.Join(root, "outside", "o"), 0o755); err != nil {
				t.Fatal(err)
			}
			link86(t, filepath.Join(root, "outside", "o"), filepath.Join("apm_modules", "o"))
			args := []string{"install", "--target", "claude"}
			hint := " i Run with --verbose for detailed diagnostics\n"
			if verbose {
				args, hint = append(args, "--verbose"), ""
			}
			stdout, code := runCLI86(t, args...)
			real := realDir86(t, root)
			want := " > Installing dependencies from apm.yml...\n" +
				" x Failed to install APM dependencies: " + containment86(filepath.Join(cwd86(t), "apm_modules", "o", "b"),
				filepath.Join(real, "proj", "apm_modules"), filepath.Join(real, "outside", "o", "b")) + "\n" +
				hint
			if code != 1 || !strings.HasPrefix(stdout, want) || !regexp.MustCompile(`\n x Install failed after \d+\.\ds\.\n$`).MatchString(stdout) ||
				strings.Count(stdout, "\n") != strings.Count(want, "\n")+1 {
				t.Errorf("exit %d, stdout:\n%s\nwant exit 1, stdout:\n%s x Install failed after <t>s.", code, stdout, want)
			}
		})
	}
}
