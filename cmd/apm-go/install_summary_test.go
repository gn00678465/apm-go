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

	"github.com/apm-go/apm/internal/deploy"
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
	setTestHome(t, filepath.Join(root, "home"))
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

// A primitive that lost its name to another package's is adopted when what
// the Oracle compares is on disk: what it deploys, an agent read with its
// CRLF as LF, or for a command its prompt's own text, which a converted
// command never has (integration/base_integrator.py:378-405,429-495,
// command_integrator.py:581). A same skill is integrated. A path the lock
// records is written over, and the disk keeps what it has when the loser
// deploys that (install/pipeline.py:723-728). Each want is Oracle 0.33.0's
// stdout for the case of the same name under the card's
// review122/probes/, rerun as issue122-s17/runs1/ and runs2/ (the opencode
// and codex cases) and issue122-s25/runs1/ (the skill-*, crflagent cases);
// a key that ends in "/" is an empty directory. The Oracle's Resolving
// order changes from run to run, apm-go's is the order of apm.yml.
func TestInstallSummary_ShadowedOutcomes(t *testing.T) {
	for _, tt := range shadowedCases() {
		t.Run(tt.name, func(t *testing.T) { runShadowedCase(t, tt, nil) })
	}
}

// The comparison writes no file, so a temporary directory that is missing or
// cannot be written changes nothing (issue122-s25/tmp-missing, tmp-ro).
func TestInstallSummary_ShadowedOutcomesWithoutTempDir(t *testing.T) {
	for _, tmp := range []string{"missing", "readonly"} {
		for _, tt := range shadowedCases() {
			if tt.name != "sameskill" && tt.name != "crlfagent" && tt.name != "sameprompt" {
				continue
			}
			t.Run(tmp+"/"+tt.name, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), tmp)
				if tmp == "readonly" {
					if err := os.Mkdir(dir, 0o500); err != nil {
						t.Fatal(err)
					}
				}
				runShadowedCase(t, tt, func() {
					for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
						t.Setenv(key, dir)
					}
				})
			})
		}
	}
}

type shadowedCase struct {
	name, target, i1, i2 string
	a, z                 map[string]string
}

func shadowedCases() []shadowedCase {
	agent := "---\nname: ag\ndescription: d\n---\nbody\n"
	prompt := "---\ndescription: d\n---\nhello $ARGUMENTS\n"
	rule := "---\napplyTo: \"**/*.go\"\n---\nhello\n"
	skill := "---\nname: sk\ndescription: d\n---\nhello\n"
	head := func(targets string, fetched bool) string {
		h := " > Installing dependencies from apm.yml...\n"
		if fetched {
			h += " > Resolving ./a...\n > Resolving ./z...\n"
		}
		return h + " i Targets: " + targets + "  (source: --target flag)\n   + ./a (local)\n   + ./z (local)\n"
	}
	const gitignore = " i Added apm_modules/ to .gitignore\n"
	const skipped = "   ! 1 file skipped -- local files exist, not managed by APM\n"
	const last = " + Installed 2 APM dependencies in <t>s.\n"
	return []shadowedCase{
		{"crlfagent", "claude",
			head("claude", true) + "  |-- 1 agents integrated -> .claude/agents/\n  |-- 1 agents adopted -> .claude/agents/\n  |-- (files unchanged)\n" + gitignore + "\n" + last,
			head("claude", false) + "  |-- 1 agents adopted -> .claude/agents/\n  |-- (files unchanged)\n  |-- 1 agents adopted -> .claude/agents/\n  |-- (files unchanged)\n\n" + last,
			map[string]string{".apm/agents/ag.agent.md": agent}, map[string]string{".apm/agents/ag.agent.md": strings.ReplaceAll(agent, "\n", "\r\n")}},
		{"sameprompt", "claude",
			head("claude", true) + "  |-- 1 commands integrated -> .claude/commands/\n  |-- (files unchanged)\n" + gitignore + skipped + last,
			head("claude", false) + "  |-- 1 commands integrated -> .claude/commands/\n  |-- 1 commands integrated -> .claude/commands/\n\n" + last,
			map[string]string{".apm/prompts/p.prompt.md": prompt}, map[string]string{".apm/prompts/p.prompt.md": prompt}},
		{"samepromptmulti", "claude,copilot",
			head("claude, copilot", true) + "  |-- 1 prompts integrated -> .github/prompts/\n  |-- 1 commands integrated -> .claude/commands/\n  |-- 1 prompts adopted -> .github/prompts/\n  |-- (files unchanged)\n" + gitignore + skipped + last,
			head("claude, copilot", false) + "  |-- 1 prompts adopted -> .github/prompts/\n  |-- 1 commands integrated -> .claude/commands/\n  |-- 1 prompts adopted -> .github/prompts/\n  |-- 1 commands integrated -> .claude/commands/\n\n" + last,
			map[string]string{".apm/prompts/p.prompt.md": prompt}, map[string]string{".apm/prompts/p.prompt.md": prompt}},
		{"sameskill", "claude",
			head("claude", true) + "  |-- 1 skill(s) integrated -> .claude/skills/\n  |-- 1 skill(s) integrated -> .claude/skills/\n" + gitignore + "\n" + last,
			head("claude", false) + "  |-- 1 skill(s) integrated -> .claude/skills/\n  |-- 1 skill(s) integrated -> .claude/skills/\n\n" + last,
			map[string]string{".apm/skills/sk/SKILL.md": skill}, map[string]string{".apm/skills/sk/SKILL.md": skill}},
		{"samerule", "claude",
			head("claude", true) + "  |-- 1 rule(s) integrated -> .claude/rules/\n  |-- 1 rule(s) adopted -> .claude/rules/\n  |-- (files unchanged)\n" + gitignore + "\n" + last,
			head("claude", false) + "  |-- 1 rule(s) adopted -> .claude/rules/\n  |-- (files unchanged)\n  |-- 1 rule(s) adopted -> .claude/rules/\n  |-- (files unchanged)\n\n" + last,
			map[string]string{".apm/instructions/ru.instructions.md": rule}, map[string]string{".apm/instructions/ru.instructions.md": rule}},
		{"samerulemulti", "claude,copilot",
			head("claude, copilot", true) + "  |-- 2 instruction(s) integrated -> .claude/rules/, .github/instructions/\n  |-- 2 instruction(s) adopted -> .claude/rules/, .github/instructions/\n  |-- (files unchanged)\n" + gitignore + "\n" + last,
			head("claude, copilot", false) + "  |-- 2 instruction(s) adopted -> .claude/rules/, .github/instructions/\n  |-- (files unchanged)\n  |-- 2 instruction(s) adopted -> .claude/rules/, .github/instructions/\n  |-- (files unchanged)\n\n" + last,
			map[string]string{".apm/instructions/ru.instructions.md": rule}, map[string]string{".apm/instructions/ru.instructions.md": rule}},
		{"crlfagentopencode", "opencode",
			head("opencode", true) + "  |-- 1 agents integrated -> .opencode/agents/\n  |-- 1 agents adopted -> .opencode/agents/\n  |-- (files unchanged)\n" + gitignore + "\n" + last,
			head("opencode", false) + "  |-- 1 agents adopted -> .opencode/agents/\n  |-- (files unchanged)\n  |-- 1 agents adopted -> .opencode/agents/\n  |-- (files unchanged)\n\n" + last,
			map[string]string{".apm/agents/ag.agent.md": agent}, map[string]string{".apm/agents/ag.agent.md": strings.ReplaceAll(agent, "\n", "\r\n")}},
		// codex writes the agent as TOML, which never has the source's text.
		{"sameagentcodex", "codex",
			head("codex", true) + "  |-- 1 agents integrated -> .codex/agents/\n  |-- (files unchanged)\n" + gitignore + skipped + last,
			head("codex", false) + "  |-- 1 agents integrated -> .codex/agents/\n  |-- 1 agents integrated -> .codex/agents/\n\n" + last,
			map[string]string{".apm/agents/ag.agent.md": agent}, map[string]string{".apm/agents/ag.agent.md": agent}},
		// A skill of other bytes is one skipped file. On the rerun the Oracle
		// writes the second package's skill over the first's and prints
		// "1 skill(s) integrated" and "1 skill replaced by a different
		// package (last installed wins)"; apm-go keeps the first package's,
		// the owner-pick deviation of the uninstall item of PRODUCT.md.
		{"skilldiff", "claude",
			head("claude", true) + "  |-- 1 skill(s) integrated -> .claude/skills/\n  |-- (files unchanged)\n" + gitignore + skipped + last,
			head("claude", false) + "  |-- 1 skill(s) integrated -> .claude/skills/\n  |-- (files unchanged)\n\n" + last,
			map[string]string{".apm/skills/sk/SKILL.md": skill}, map[string]string{".apm/skills/sk/SKILL.md": skill + "z\n"}},
		// A name on one side alone, an empty directory included, makes two
		// skills differ, as filecmp.dircmp compares them
		// (integration/skill_integrator.py:531-550). The deploy makes no empty
		// directory (install/deployable_source_plan.py:284-297), so on the
		// rerun what the second skill deploys is on disk, and it is
		// integrated, as the Oracle, which writes it over the first, prints.
		{"skill-empty-dir", "claude",
			head("claude", true) + "  |-- 1 skill(s) integrated -> .claude/skills/\n  |-- (files unchanged)\n" + gitignore + skipped + last,
			head("claude", false) + "  |-- 1 skill(s) integrated -> .claude/skills/\n  |-- 1 skill(s) integrated -> .claude/skills/\n\n" + last,
			map[string]string{".apm/skills/sk/SKILL.md": skill}, map[string]string{".apm/skills/sk/SKILL.md": skill, ".apm/skills/sk/data/": ""}},
		{"skill-onefile", "claude",
			head("claude", true) + "  |-- 1 skill(s) integrated -> .claude/skills/\n  |-- (files unchanged)\n" + gitignore + skipped + last,
			head("claude", false) + "  |-- 1 skill(s) integrated -> .claude/skills/\n  |-- (files unchanged)\n\n" + last,
			map[string]string{".apm/skills/sk/SKILL.md": skill}, map[string]string{".apm/skills/sk/SKILL.md": skill, ".apm/skills/sk/notes.txt": "n\n"}},
		// The names of filecmp.DEFAULT_IGNORES are not compared.
		{"skill-pycache", "claude",
			head("claude", true) + "  |-- 1 skill(s) integrated -> .claude/skills/\n  |-- 1 skill(s) integrated -> .claude/skills/\n" + gitignore + "\n" + last,
			head("claude", false) + "  |-- 1 skill(s) integrated -> .claude/skills/\n  |-- 1 skill(s) integrated -> .claude/skills/\n\n" + last,
			map[string]string{".apm/skills/sk/SKILL.md": skill}, map[string]string{".apm/skills/sk/SKILL.md": skill, ".apm/skills/sk/__pycache__/c.pyc": "x"}},
		{"skill-gitdir", "claude",
			head("claude", true) + "  |-- 1 skill(s) integrated -> .claude/skills/\n  |-- 1 skill(s) integrated -> .claude/skills/\n" + gitignore + "\n" + last,
			head("claude", false) + "  |-- 1 skill(s) integrated -> .claude/skills/\n  |-- 1 skill(s) integrated -> .claude/skills/\n\n" + last,
			map[string]string{".apm/skills/sk/SKILL.md": skill}, map[string]string{".apm/skills/sk/SKILL.md": skill, ".apm/skills/sk/.git/HEAD": "ref: refs/heads/main\n"}},
		// A name that is a file on one side and a directory on the other is
		// dircmp's common_funny, which the Oracle does not look at.
		{"skill-funny", "claude",
			head("claude", true) + "  |-- 1 skill(s) integrated -> .claude/skills/\n  |-- 1 skill(s) integrated -> .claude/skills/\n" + gitignore + "\n" + last,
			head("claude", false) + "  |-- 1 skill(s) integrated -> .claude/skills/\n  |-- 1 skill(s) integrated -> .claude/skills/\n\n" + last,
			map[string]string{".apm/skills/sk/SKILL.md": skill, ".apm/skills/sk/x": "file\n"}, map[string]string{".apm/skills/sk/SKILL.md": skill, ".apm/skills/sk/x/f": "in dir\n"}},
		// The Oracle turns only a CRLF into LF before it compares
		// (utils/atomic_io.py:28-37), so an agent with bare CRs differs from
		// the LF file it writes; on the rerun the path is the lock's, and the
		// agent deploys what is on disk.
		{"crflagent", "claude",
			head("claude", true) + "  |-- 1 agents integrated -> .claude/agents/\n  |-- (files unchanged)\n" + gitignore + skipped + last,
			head("claude", false) + "  |-- 1 agents adopted -> .claude/agents/\n  |-- (files unchanged)\n  |-- 1 agents integrated -> .claude/agents/\n\n" + last,
			map[string]string{".apm/agents/ag.agent.md": agent}, map[string]string{".apm/agents/ag.agent.md": strings.ReplaceAll(agent, "\n", "\r")}},
	}
}

// At user scope the Oracle copies an agent with its line ends written as
// LF; apm-go links it, or copies it with a bare CR kept when it cannot link.
// What the Oracle compares is then its copy, and the summary is the same in
// both modes. Each want is Oracle 0.33.0's for issue122-s28/runs-or2/g-crlf
// and g-cr; it prints a warning about which targets support user scope,
// which apm-go leaves out (issue #122 comment 6232580233 item 2).
func TestInstallSummary_GlobalShadowed(t *testing.T) {
	agent := "---\nname: ag\ndescription: d\n---\nbody\n"
	for _, tt := range []struct {
		name, body string
		i1, i2     func(root string) string
	}{
		{"crlf", strings.ReplaceAll(agent, "\n", "\r\n"),
			func(root string) string {
				return globalHead(root, true) + "  |-- 1 agents integrated -> .claude/agents/\n  |-- 1 agents adopted -> .claude/agents/\n  |-- (files unchanged)\n" +
					"   + <project root> (local)\n  |-- (files unchanged)\n\n + Installed 2 APM dependencies in <t>s.\n"
			},
			func(root string) string {
				return globalHead(root, false) + "  |-- 1 agents adopted -> .claude/agents/\n  |-- (files unchanged)\n  |-- 1 agents adopted -> .claude/agents/\n  |-- (files unchanged)\n" +
					"   + <project root> (local)\n  |-- (files unchanged)\n\n + Installed 2 APM dependencies in <t>s.\n"
			}},
		{"cr", strings.ReplaceAll(agent, "\n", "\r"),
			func(root string) string {
				return globalHead(root, true) + "  |-- 1 agents integrated -> .claude/agents/\n  |-- (files unchanged)\n" +
					"   + <project root> (local)\n  |-- (files unchanged)\n   ! 1 file skipped -- local files exist, not managed by APM\n + Installed 2 APM dependencies in <t>s.\n"
			},
			func(root string) string {
				return globalHead(root, false) + "  |-- 1 agents integrated -> .claude/agents/\n  |-- 1 agents integrated -> .claude/agents/\n" +
					"   + <project root> (local)\n  |-- (files unchanged)\n\n + Installed 2 APM dependencies in <t>s.\n"
			}},
	} {
		for _, link := range []bool{true, false} {
			mode := map[bool]string{true: "link", false: "copy"}[link]
			t.Run(tt.name+"/"+mode, func(t *testing.T) {
				if link {
					if err := os.Symlink("x", filepath.Join(t.TempDir(), "l")); err != nil {
						t.Skipf("this account cannot make a symlink: %v", err)
					}
				}
				t.Cleanup(deploy.SetSymlinkProbeForTest(func() bool { return link }))
				root := summaryProject(t, map[string]string{
					"pkg/apm.yml": "name: pkg\nversion: 1.0.0\n", "pkg/.apm/agents/ag.agent.md": tt.body,
					"z/apm.yml": "name: z\nversion: 1.0.0\n", "z/.apm/agents/ag.agent.md": tt.body,
				})
				proj := filepath.Join(root, "proj")
				writeCompileFile(t, filepath.Join(root, "home"), ".apm/apm.yml", "name: g\nversion: 1.0.0\ndependencies:\n  apm:\n    - "+
					filepath.Join(proj, "pkg")+"\n    - "+filepath.Join(proj, "z")+"\n")
				args := []string{"install", "-g", "--target", "claude"}
				runSummary(t, []summaryRun{{args, tt.i1(proj)}, {args, tt.i2(proj)}})
			})
		}
	}
}

func globalHead(proj string, fetched bool) string {
	h := " i Installing to user scope (~/.apm/)\n > Installing dependencies from apm.yml...\n"
	if fetched {
		h += " > Resolving " + filepath.Join(proj, "pkg") + "...\n > Resolving " + filepath.Join(proj, "z") + "...\n"
	}
	return h + "   + " + filepath.Join(proj, "pkg") + " (local)\n   + " + filepath.Join(proj, "z") + " (local)\n"
}

// A package's own agent is adopted when the file on disk before the install
// is what the Oracle compares: the agent with each CRLF as LF, a bare CR
// kept, against the bytes on disk as they are
// (integration/base_integrator.py:337-374,429-495). So an agent with a bare
// CR is integrated on every run, an agent the user turned to CRLF is
// integrated, and a file that was there with the same bytes is adopted
// before any lock records it. A claude rule is compared as converted
// (integration/instruction_integrator.py:450-485), so one the user turned to
// CRLF is integrated too. Each want is Oracle 0.33.0's for
// issue122-s28/runs-w1/w-cr, w-crlfdisk, w-pre and runs-r1/r-crlfdisk.
func TestInstallSummary_AdoptedAsOracleCompares(t *testing.T) {
	agent := "---\nname: ag\ndescription: d\n---\nbody\n"
	head := func(fetched bool) string {
		h := " > Installing dependencies from apm.yml...\n"
		if fetched {
			h += " > Resolving ./a...\n"
		}
		return h + " i Targets: claude  (source: --target flag)\n   + ./a (local)\n"
	}
	const gitignore = " i Added apm_modules/ to .gitignore\n"
	const last = "\n + Installed 1 APM dependency in <t>s.\n"
	integrated := "  |-- 1 agents integrated -> .claude/agents/\n"
	adopted := "  |-- 1 agents adopted -> .claude/agents/\n  |-- (files unchanged)\n"
	toCRLF := func(rel string) func() {
		return func() {
			data, err := os.ReadFile(rel)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(rel, bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n")), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tt := range []struct {
		name, src, body string
		pre, before     func()
		i1, i2          string
	}{
		{"barecr", "agents/ag.agent.md", strings.ReplaceAll(agent, "\n", "\r"), nil, nil,
			head(true) + integrated + gitignore + last, head(false) + integrated + last},
		{"crlfdisk", "agents/ag.agent.md", agent, nil, toCRLF(filepath.Join(".claude", "agents", "ag.md")),
			head(true) + integrated + gitignore + last, head(false) + integrated + last},
		{"preexisting", "agents/ag.agent.md", agent, func() { writeCompileFile(t, ".", ".claude/agents/ag.md", agent) }, nil,
			head(true) + adopted + gitignore + last, head(false) + adopted + last},
		{"rulecrlfdisk", "instructions/ru.instructions.md", "---\napplyTo: \"**/*.go\"\n---\nhello\n", nil, toCRLF(filepath.Join(".claude", "rules", "ru.md")),
			head(true) + "  |-- 1 rule(s) integrated -> .claude/rules/\n" + gitignore + last, head(false) + "  |-- 1 rule(s) integrated -> .claude/rules/\n" + last},
	} {
		t.Run(tt.name, func(t *testing.T) {
			onePkgProject(t, "    - ./a\n", map[string]string{"a/apm.yml": "name: a\nversion: 1.0.0\n", "a/.apm/" + tt.src: tt.body})
			if tt.pre != nil {
				tt.pre()
			}
			args := []string{"install", "--target", "claude"}
			runSummary(t, []summaryRun{{args, tt.i1}})
			if tt.before != nil {
				tt.before()
			}
			runSummary(t, []summaryRun{{args, tt.i2}})
		})
	}
}

// When the user replaced the winner's agent by a symlink to another file of
// its directory, what the Oracle compares is never there
// (integration/base_integrator.py:342-343), and it looks the path up in its
// managed set with the symlink resolved (utils/paths.py:14-29), so the loser
// is a skipped file. The want is Oracle 0.33.0's for
// issue122-s28/runs-or2/p-symlink but for the first package: apm-go writes
// its agent through the symlink, which CheckDeployWrite allows, and prints
// it integrated, where the Oracle skips it as a second skipped file and
// warns that it kept the file the lock records under the second package
// (the owner-pick deviation of the uninstall item of PRODUCT.md).
func TestInstallSummary_ShadowedSymlinkOnDisk(t *testing.T) {
	agent := "---\nname: ag\ndescription: d\n---\nbody\n"
	onePkgProject(t, "    - ./a\n    - ./z\n", map[string]string{
		"a/apm.yml": "name: a\nversion: 1.0.0\n", "a/.apm/agents/ag.agent.md": agent,
		"z/apm.yml": "name: z\nversion: 1.0.0\n", "z/.apm/agents/ag.agent.md": agent,
	})
	args := []string{"install", "--target", "claude"}
	runCLI86(t, args...)
	dest := filepath.Join(".claude", "agents", "ag.md")
	writeCompileFile(t, ".", ".claude/agents/other.md", agent)
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other.md", dest); err != nil {
		t.Skipf("symlink: %v", err)
	}
	want := " > Installing dependencies from apm.yml...\n" +
		" i Targets: claude  (source: --target flag)\n" +
		"   + ./a (local)\n" +
		"   + ./z (local)\n" +
		"  |-- 1 agents integrated -> .claude/agents/\n" +
		"  |-- (files unchanged)\n" +
		"   ! 1 file skipped -- local files exist, not managed by APM\n" +
		" + Installed 2 APM dependencies in <t>s.\n"
	runSummary(t, []summaryRun{{args, want}, {args, want}})
}

// runShadowedCase installs ./a and ./z, each with its files, twice; before
// runs it sets up what the environment of the runs needs.
func runShadowedCase(t *testing.T, tt shadowedCase, before func()) {
	files := map[string]string{"a/apm.yml": "name: a\nversion: 1.0.0\n", "z/apm.yml": "name: z\nversion: 1.0.0\n"}
	var dirs []string
	for pkg, pkgFiles := range map[string]map[string]string{"a": tt.a, "z": tt.z} {
		for rel, content := range pkgFiles {
			if strings.HasSuffix(rel, "/") {
				dirs = append(dirs, pkg+"/"+rel)
				continue
			}
			files[pkg+"/"+rel] = content
		}
	}
	onePkgProject(t, "    - ./a\n    - ./z\n", files)
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if before != nil {
		before()
	}
	args := []string{"install", "--target", tt.target}
	runSummary(t, []summaryRun{{args, tt.i1}, {args, tt.i2}})
}

// A path the lock records for the project's own content is managed too, as
// the Oracle reads that content as a dependency of its own
// (deps/lockfile.py:951-963): a package that loses it is no skipped file.
// The want is Oracle 0.33.0's for review122/local-owner, rerun as
// issue122-s17/runs1/localowner, but for the second package: the Oracle
// writes its agent over the first's and prints it integrated, with two
// warnings that it kept the project's old file, where apm-go keeps the
// first package's agent (the owner-pick deviation of the uninstall item of
// PRODUCT.md) and so prints the empty line of a run without diagnostics.
func TestInstallSummary_ShadowedLocalOwner(t *testing.T) {
	agent := func(body string) string { return "---\nname: ag\ndescription: d\n---\n" + body + "\n" }
	summaryProject(t, map[string]string{
		"apm.yml":                 "name: proj\nversion: 1.0.0\n",
		".apm/agents/ag.agent.md": agent("local"),
	})
	runSummary(t, []summaryRun{{[]string{"install", "--target", "claude"}, " > Installing dependencies from apm.yml...\n" +
		" i Targets: claude  (source: --target flag)\n" +
		"   + <project root> (local)\n" +
		"  |-- 1 agents integrated -> .claude/agents/\n" +
		" i Added apm_modules/ to .gitignore\n" +
		"\n" +
		" + Installed 1 APM dependency in <t>s.\n"}})
	if err := os.Remove(filepath.Join(".apm", "agents", "ag.agent.md")); err != nil {
		t.Fatal(err)
	}
	for rel, content := range map[string]string{
		"apm.yml":                   "name: proj\nversion: 1.0.0\ndependencies:\n  apm:\n    - ./a\n    - ./z\n",
		"a/apm.yml":                 "name: a\nversion: 1.0.0\n",
		"a/.apm/agents/ag.agent.md": agent("a"),
		"z/apm.yml":                 "name: z\nversion: 1.0.0\n",
		"z/.apm/agents/ag.agent.md": agent("z"),
	} {
		writeCompileFile(t, ".", rel, content)
	}
	runSummary(t, []summaryRun{{[]string{"install", "--target", "claude"}, " > Installing dependencies from apm.yml...\n" +
		" > Resolving ./a...\n" +
		" > Resolving ./z...\n" +
		" i Targets: claude  (source: --target flag)\n" +
		"   + ./a (local)\n" +
		"   + ./z (local)\n" +
		"  |-- 1 agents integrated -> .claude/agents/\n" +
		"  |-- (files unchanged)\n" +
		"   + <project root> (local)\n" +
		"  |-- (files unchanged)\n" +
		"\n" +
		" + Installed 2 APM dependencies in <t>s.\n"}})
}

// A package whose root is a skill prints its skill alone: the Oracle counts
// sub-skills only below .github/skills/, which no target writes
// (integration/skill_integrator.py:1241-1245; review122/probes/rootskill,
// rerun as issue122-s17/runs1/rootskill).
func TestInstallSummary_RootSkillHidesSubSkills(t *testing.T) {
	onePkgProject(t, "    - ./a\n", map[string]string{
		"a/apm.yml":                    "name: a\nversion: 1.0.0\n",
		"a/SKILL.md":                   "---\nname: sk\ndescription: d\n---\nhello\n",
		"a/.apm/skills/other/SKILL.md": "---\nname: other\ndescription: d\n---\nhello\n",
	})
	args := []string{"install", "--target", "claude,copilot"}
	runSummary(t, []summaryRun{
		{args, " > Installing dependencies from apm.yml...\n" +
			" > Resolving ./a...\n" +
			" i Targets: claude, copilot  (source: --target flag)\n" +
			"   + ./a (local)\n" +
			"  |-- Skill integrated -> .agents/skills/, .claude/skills/\n" +
			" i Added apm_modules/ to .gitignore\n" +
			"\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
		{args, " > Installing dependencies from apm.yml...\n" +
			" i Targets: claude, copilot  (source: --target flag)\n" +
			"   + ./a (local)\n" +
			"  |-- (files unchanged)\n" +
			"\n" +
			" + Installed 1 APM dependency in <t>s.\n"},
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
