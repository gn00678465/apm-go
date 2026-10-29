package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// i125Package is the package of the card's scripts (issue125 scripts/lib.sh
// mkpkg): an agent, an instruction, a prompt and a skill, plus a hook.
func i125Package(t *testing.T, dir, name string) {
	t.Helper()
	writeCompileFile(t, dir, "apm.yml", "name: "+name+"\nversion: 1.0.0\n")
	writeCompileFile(t, dir, ".apm/agents/ag-"+name+".agent.md", "---\ndescription: a\n---\nagent "+name+"\n")
	writeCompileFile(t, dir, ".apm/instructions/r-"+name+".instructions.md", "---\napplyTo: \"**/*.go\"\n---\nrule "+name+"\n")
	writeCompileFile(t, dir, ".apm/prompts/p-"+name+".prompt.md", "---\ndescription: p\n---\nprompt "+name+"\n")
	writeCompileFile(t, dir, ".apm/skills/sk-"+name+"/SKILL.md", "---\nname: sk-"+name+"\ndescription: d\n---\nskill "+name+"\n")
	writeCompileFile(t, dir, "hooks/h.json", `{"hooks":{"PreToolUse":[{"matcher":"x","hooks":[{"type":"command","command":"echo"}]}]}}`+"\n")
}

// i125Scope is a project and a HOME with the packages dep and dep2 beside
// them; the working directory is the project.
type i125Scope struct{ root, proj, home, dep, dep2 string }

func newI125Scope(t *testing.T) i125Scope {
	t.Helper()
	root := t.TempDir()
	s := i125Scope{root: root, proj: filepath.Join(root, "proj"), home: filepath.Join(root, "home"),
		dep: filepath.Join(root, "pkgs", "dep"), dep2: filepath.Join(root, "pkgs", "dep2")}
	i125Package(t, s.dep, "dep")
	i125Package(t, s.dep2, "dep2")
	writeCompileFile(t, root, "gitconfig", "")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	setTestHome(t, s.home)
	for _, d := range []string{s.proj, s.home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(s.proj)
	return s
}

func (s i125Scope) mkdirs(t *testing.T, base string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(base, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func (s i125Scope) manifest(t *testing.T, targets string, deps ...string) {
	t.Helper()
	m := "name: proj\nversion: 1.0.0\n"
	if targets != "" {
		m += "targets: [" + targets + "]\n"
	}
	if len(deps) > 0 {
		m += "dependencies:\n  apm:\n"
		for _, d := range deps {
			m += "    - " + d + "\n"
		}
	}
	writeCompileFile(t, s.proj, "apm.yml", m)
}

// i125Deployed is every deployed path a lock at path records, by entry
// ("<local>" for the project's own content), sorted.
func i125Deployed(t *testing.T, path string) map[string][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	lf, err := lockfile.ParseLockfile(node)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, d := range lf.Dependencies {
		out[d.UniqueKey()] = slices.Sorted(slices.Values(d.DeployedFiles))
	}
	if len(lf.LocalDeployedFiles) > 0 {
		out["<local>"] = slices.Sorted(slices.Values(lf.LocalDeployedFiles))
	}
	return out
}

func i125Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func run125(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	stdout, stderr, code := runCompileCLI(t, args...)
	return elapsed86.ReplaceAllString(stdout, "in <t>s."), stderr, code
}

// c47-2: a target named twice deploys once, and the Targets line is sorted,
// as Oracle 0.33.0's resolve_targets sorts (measured, issue125 proj2-or/t1,
// t2, t5).
func TestIssue125_TargetsDedupedAndSorted(t *testing.T) {
	t.Run("copilot,vscode", func(t *testing.T) {
		s := newI125Scope(t)
		stdout, _, code := run125(t, "install", s.dep, "--target", "copilot,vscode")
		if code != 0 || !strings.Contains(stdout, " i Targets: copilot  (source: --target flag)\n") ||
			!strings.Contains(stdout, "  |-- 1 prompts integrated -> .github/prompts/\n") || strings.Contains(stdout, "adopted") {
			t.Fatalf("exit %d, stdout:\n%s", code, stdout)
		}
		stdout, _, _ = run125(t, "install", "--target", "copilot,vscode")
		if !strings.Contains(stdout, "  |-- 1 prompts adopted -> .github/prompts/\n") {
			t.Errorf("second install, stdout:\n%s", stdout)
		}
	})
	t.Run("codex,claude", func(t *testing.T) {
		s := newI125Scope(t)
		stdout, _, code := run125(t, "install", s.dep, "--target", "codex,claude")
		if code != 0 || !strings.Contains(stdout, " i Targets: claude, codex  (source: --target flag)\n") {
			t.Errorf("exit %d, stdout:\n%s", code, stdout)
		}
	})
	t.Run("targets: [codex, claude, claude]", func(t *testing.T) {
		s := newI125Scope(t)
		s.manifest(t, "codex, claude, claude", s.dep)
		stdout, _, code := run125(t, "install")
		if code != 0 || !strings.Contains(stdout, " i Targets: claude, codex  (source: apm.yml)\n") ||
			!strings.Contains(stdout, "  |-- 2 agents integrated -> .claude/agents/, .codex/agents/\n") {
			t.Errorf("exit %d, stdout:\n%s", code, stdout)
		}
	})
}

// c62-3: two detected harnesses and no target stop the command with exit 2
// before anything is written (measured, issue125 proj2-or/m1-m6,
// amb-or/loc, amb-or/mc, ambf-or/f).
func TestIssue125_MultipleHarnessesRefused(t *testing.T) {
	cases := []struct {
		name, want string
		dirs       []string
		files      map[string]string
		deps, args []string
		created    bool // install <pkg> created apm.yml
		ci         bool
		noManifest bool
	}{
		{name: "install", want: "claude, codex", dirs: []string{".claude", ".codex"}, deps: []string{"dep"}, args: []string{"install"}},
		{name: "adapterless gemini", want: "claude, gemini", dirs: []string{".claude", ".gemini"}, deps: []string{"dep"}, args: []string{"install"}},
		{name: "claude and .github/prompts", want: "claude, copilot", dirs: []string{".claude", ".github/prompts"}, files: map[string]string{"CLAUDE.md": ""}, deps: []string{"dep"}, args: []string{"install"}},
		{name: "install pkg", want: "claude, codex", dirs: []string{".claude", ".codex"}, args: []string{"install", "dep"}, created: true, noManifest: true},
		{name: "update", want: "claude, codex", dirs: []string{".claude", ".codex"}, deps: []string{"dep"}, args: []string{"update"}},
		{name: "own content only", want: "claude, codex", dirs: []string{".claude", ".codex"}, files: map[string]string{".apm/agents/l.agent.md": "---\ndescription: l\n---\nl\n"}, args: []string{"install"}},
		{name: "install --mcp", want: "claude, codex", dirs: []string{".claude", ".codex"}, args: []string{"install", "--mcp", "srv", "--", "echo", "hi"}},
		{name: "frozen", want: "claude, codex", dirs: []string{".claude", ".codex"}, deps: []string{"dep"}, args: []string{"install", "--frozen"}},
		{name: "frozen under CI", want: "claude, codex", dirs: []string{".claude", ".codex"}, deps: []string{"dep"}, args: []string{"install"}, ci: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newI125Scope(t)
			s.mkdirs(t, s.proj, c.dirs...)
			for rel, content := range c.files {
				writeCompileFile(t, s.proj, rel, content)
			}
			var deps []string
			for _, d := range c.deps {
				deps = append(deps, filepath.Join(s.root, "pkgs", d))
			}
			if !c.noManifest {
				s.manifest(t, "", deps...)
			}
			if c.ci {
				t.Setenv("CI", "1")
			}
			args := slices.Clone(c.args)
			if args[len(args)-1] == "dep" {
				args[len(args)-1] = s.dep
			}
			stdout, stderr, code := run125(t, args...)
			want := " x Multiple harnesses detected: " + c.want
			if code != 2 || !strings.Contains(stdout, want) || strings.Contains(stderr, "Usage:") ||
				!strings.Contains(stdout, "  1. Pass --target <name> (e.g. --target claude)") {
				t.Fatalf("exit %d, stdout:\n%s\nstderr:\n%s\nwant exit 2 and %q", code, stdout, stderr, want)
			}
			for _, rel := range []string{"apm.lock.yaml", "apm_modules", ".gitignore", ".claude/agents", ".mcp.json"} {
				assertNoFile(t, filepath.Join(s.proj, rel))
			}
			if c.created {
				assertNoFile(t, filepath.Join(s.proj, "apm.yml"))
			}
		})
	}
}

// What two harnesses do not stop (measured, issue125 proj2-or/m5, m7,
// amb-or/c, amb-or/un, glob1-or/d1), and one detected signal of the Oracle's
// table that apm-go did not read before.
func TestIssue125_MultipleHarnessesNotRefused(t *testing.T) {
	t.Run("--target", func(t *testing.T) {
		s := newI125Scope(t)
		s.mkdirs(t, s.proj, ".claude", ".opencode")
		s.manifest(t, "", s.dep)
		if stdout, _, code := run125(t, "install", "--target", "claude"); code != 0 || !strings.Contains(stdout, " i Targets: claude  (source: --target flag)") {
			t.Errorf("exit %d, stdout:\n%s", code, stdout)
		}
	})
	t.Run("nothing to integrate", func(t *testing.T) {
		s := newI125Scope(t)
		s.mkdirs(t, s.proj, ".claude", ".codex")
		s.manifest(t, "")
		if stdout, _, code := run125(t, "install"); code != 0 {
			t.Errorf("exit %d, stdout:\n%s", code, stdout)
		}
	})
	t.Run("compile", func(t *testing.T) {
		s := newI125Scope(t)
		s.mkdirs(t, s.proj, ".claude", ".codex")
		s.manifest(t, "")
		writeCompileFile(t, s.proj, ".apm/instructions/i.instructions.md", "---\napplyTo: \"**\"\n---\nx\n")
		if stdout, _, code := run125(t, "compile"); code != 0 || strings.Contains(stdout, "Multiple harnesses") {
			t.Errorf("exit %d, stdout:\n%s", code, stdout)
		}
	})
	t.Run("uninstall", func(t *testing.T) {
		s := newI125Scope(t)
		s.manifest(t, "claude", s.dep)
		if _, _, code := run125(t, "install"); code != 0 {
			t.Fatal("install failed")
		}
		s.manifest(t, "", s.dep)
		s.mkdirs(t, s.proj, ".codex")
		if stdout, _, code := run125(t, "uninstall", s.dep); code != 0 {
			t.Errorf("exit %d, stdout:\n%s", code, stdout)
		}
	})
	t.Run("--global", func(t *testing.T) {
		s := newI125Scope(t)
		s.mkdirs(t, s.home, ".claude", ".codex")
		if stdout, _, code := run125(t, "install", "-g", s.dep); code != 0 {
			t.Errorf("exit %d, stdout:\n%s", code, stdout)
		}
	})
	t.Run(".github/prompts/ alone detects copilot", func(t *testing.T) {
		s := newI125Scope(t)
		s.mkdirs(t, s.proj, ".github/prompts")
		s.manifest(t, "", s.dep)
		if stdout, _, code := run125(t, "install"); code != 0 || !strings.Contains(stdout, " i Targets: copilot  (source: auto-detect from .github/prompts/)\n") {
			t.Errorf("exit %d, stdout:\n%s", code, stdout)
		}
	})
	t.Run("one adapterless harness is no target", func(t *testing.T) {
		s := newI125Scope(t)
		s.mkdirs(t, s.proj, ".gemini")
		s.manifest(t, "", s.dep)
		if stdout, _, code := run125(t, "install"); code != 2 || !strings.Contains(stdout, " x no deployment target detected") {
			t.Errorf("exit %d, stdout:\n%s", code, stdout)
		}
	})
}

// The user scope detects its targets by the user-scope roots of HOME, else
// copilot (measured, issue125 glob1-or/d1-d8).
func TestIssue125_UserScopeDetection(t *testing.T) {
	cases := []struct {
		name     string
		homeDirs []string
		projDirs []string
		want     []string // the target roots of the lock's paths, sorted
	}{
		{"claude and codex", []string{".claude", ".codex"}, nil, []string{".agents", ".claude", ".codex"}},
		{"claude", []string{".claude"}, nil, []string{".claude"}},
		{"marker in the current directory", nil, []string{".claude"}, []string{".agents", ".copilot"}},
		{"nothing", nil, nil, []string{".agents", ".copilot"}},
		{"copilot and opencode", []string{".copilot", ".config/opencode"}, nil, []string{".agents", ".config", ".copilot"}},
		{"old roots", []string{".github", ".opencode"}, nil, []string{".agents", ".copilot"}},
		{"claude and gemini", []string{".claude", ".gemini"}, nil, []string{".claude"}},
		{"~/.apm/.claude", []string{".apm/.claude"}, nil, []string{".agents", ".copilot"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newI125Scope(t)
			s.mkdirs(t, s.home, c.homeDirs...)
			s.mkdirs(t, s.proj, c.projDirs...)
			if stdout, _, code := run125(t, "install", "-g", s.dep); code != 0 {
				t.Fatalf("exit %d, stdout:\n%s", code, stdout)
			}
			roots := map[string]bool{}
			for _, f := range i125Deployed(t, filepath.Join(s.home, ".apm", "apm.lock.yaml"))["_local/dep"] {
				roots[strings.SplitN(f, "/", 2)[0]] = true
			}
			if got := slices.Sorted(maps.Keys(roots)); !slices.Equal(got, c.want) {
				t.Errorf("target roots %v, want %v", got, c.want)
			}
		})
	}
}

// The user-scope paths of copilot and opencode (measured, issue125
// glob1-or/g68/1.glock); a project keeps its paths (proj2-or/t1).
func TestIssue125_UserScopePaths(t *testing.T) {
	s := newI125Scope(t)
	if stdout, _, code := run125(t, "install", "-g", s.dep, "--target", "claude,opencode,copilot"); code != 0 {
		t.Fatalf("exit %d, stdout:\n%s", code, stdout)
	}
	want := []string{
		".agents/skills/sk-dep", ".agents/skills/sk-dep/SKILL.md",
		".claude/agents/ag-dep.md", ".claude/commands/p-dep.md", ".claude/rules/r-dep.md", ".claude/skills/sk-dep", ".claude/skills/sk-dep/SKILL.md",
		".config/opencode/agents/ag-dep.md", ".config/opencode/commands/p-dep.md", ".config/opencode/skills/sk-dep", ".config/opencode/skills/sk-dep/SKILL.md",
		".copilot/agents/ag-dep.agent.md", ".copilot/hooks/dep-h.json", ".copilot/instructions/r-dep.instructions.md", ".copilot/prompts/p-dep.prompt.md",
	}
	if got := i125Deployed(t, filepath.Join(s.home, ".apm", "apm.lock.yaml"))["_local/dep"]; !slices.Equal(got, want) {
		t.Errorf("deployed_files:\n%v\nwant:\n%v", got, want)
	}
	for _, rel := range []string{".github", ".opencode"} {
		assertNoFile(t, filepath.Join(s.home, rel))
	}

	p := newI125Scope(t)
	if _, _, code := run125(t, "install", p.dep, "--target", "copilot,opencode"); code != 0 {
		t.Fatal("project install failed")
	}
	for _, rel := range []string{".github/agents/ag-dep.agent.md", ".opencode/agents/ag-dep.md", ".agents/skills/sk-dep/SKILL.md"} {
		if !i125Exists(filepath.Join(p.proj, filepath.FromSlash(rel))) {
			t.Errorf("project %s not deployed", rel)
		}
	}
}

// legacyGlobalLock turns the --global lock and files of s into what an
// apm-go before issue #125 wrote: copilot's below ~/.github, opencode's
// below ~/.opencode and its skill in ~/.agents/skills, each a symlink into
// apm_modules as the symlink deploy makes them.
func legacyGlobalLock(t *testing.T, s i125Scope) {
	t.Helper()
	lockPath := filepath.Join(s.home, ".apm", "apm.lock.yaml")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	lock := strings.NewReplacer(".copilot/", ".github/", ".config/opencode/skills/", "", ".config/opencode/", ".opencode/").Replace(string(data))
	// The opencode skill lines became "sk-dep" lines; drop them, its
	// .agents/skills record is already there.
	var kept []string
	for _, l := range strings.Split(lock, "\n") {
		if !strings.Contains(l, " sk-dep") || strings.Contains(l, "skills/sk-dep") {
			kept = append(kept, l)
		}
	}
	if err := os.WriteFile(lockPath, []byte(strings.Join(kept, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".copilot", ".config"} {
		if err := os.RemoveAll(filepath.Join(s.home, rel)); err != nil {
			t.Fatal(err)
		}
	}
	src := filepath.Join(s.home, ".apm", "apm_modules", "_local", "dep")
	for link, target := range map[string]string{
		".github/agents/ag-dep.agent.md":             ".apm/agents/ag-dep.agent.md",
		".github/instructions/r-dep.instructions.md": ".apm/instructions/r-dep.instructions.md",
		".github/prompts/p-dep.prompt.md":            ".apm/prompts/p-dep.prompt.md",
		".github/hooks/dep-h.json":                   "hooks/h.json",
		".opencode/agents/ag-dep.md":                 ".apm/agents/ag-dep.agent.md",
	} {
		at := filepath.Join(s.home, filepath.FromSlash(link))
		if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(src, filepath.FromSlash(target)), at); err != nil {
			t.Fatal(err)
		}
	}
	writeCompileFile(t, s.home, ".opencode/commands/p-dep.md", readFileString(t, filepath.Join(s.home, ".claude", "commands", "p-dep.md")))
}

// The first install -g after the upgrade removes what an earlier apm-go
// deployed below ~/.github and ~/.opencode (measured with main and v0.4.1
// locks, issue125 new1/upg-main and upg-v041 g1, g3, g5, g6).
func TestIssue125_UserScopeUpgrade(t *testing.T) {
	setup := func(t *testing.T) i125Scope {
		s := newI125Scope(t)
		setSymlinkProbe(t, true)
		if stdout, _, code := run125(t, "install", "-g", s.dep, "--target", "claude,opencode,copilot"); code != 0 {
			t.Fatalf("exit %d, stdout:\n%s", code, stdout)
		}
		legacyGlobalLock(t, s)
		return s
	}
	legacy := func(files []string) []string {
		var out []string
		for _, f := range files {
			if strings.HasPrefix(f, ".github/") || strings.HasPrefix(f, ".opencode/") {
				out = append(out, f)
			}
		}
		return out
	}
	t.Run("same targets", func(t *testing.T) {
		s := setup(t)
		stdout, _, code := run125(t, "install", "-g")
		if code != 0 || !strings.Contains(stdout, " i Cleaned 6 stale files from ") {
			t.Fatalf("exit %d, stdout:\n%s", code, stdout)
		}
		for _, rel := range []string{".github", ".opencode"} {
			assertNoFile(t, filepath.Join(s.home, rel))
		}
		files := i125Deployed(t, filepath.Join(s.home, ".apm", "apm.lock.yaml"))["_local/dep"]
		if l := legacy(files); len(l) > 0 || !slices.Contains(files, ".copilot/agents/ag-dep.agent.md") {
			t.Errorf("deployed_files %v", files)
		}
		if _, _, code := run125(t, "install", "-g", "--frozen"); code != 0 {
			t.Errorf("frozen after the upgrade: exit %d", code)
		}
	})
	t.Run("copilot and opencode taken out", func(t *testing.T) {
		s := setup(t)
		writeCompileFile(t, s.home, ".apm/apm.yml", strings.NewReplacer("  - copilot\n", "", "  - opencode\n", "").Replace(readFileString(t, filepath.Join(s.home, ".apm", "apm.yml"))))
		if stdout, _, code := run125(t, "install", "-g"); code != 0 {
			t.Fatalf("exit %d, stdout:\n%s", code, stdout)
		}
		for _, rel := range []string{".github", ".opencode", ".copilot"} {
			assertNoFile(t, filepath.Join(s.home, rel))
		}
	})
	t.Run("a file the user changed stays", func(t *testing.T) {
		s := setup(t)
		at := filepath.Join(s.home, ".github", "agents", "ag-dep.agent.md")
		if err := os.Remove(at); err != nil {
			t.Fatal(err)
		}
		writeCompileFile(t, s.home, ".github/agents/ag-dep.agent.md", "mine\n")
		stdout, stderr, code := run125(t, "install", "-g")
		if code != 0 || !strings.Contains(stdout+stderr, `keeping ".github/agents/ag-dep.agent.md": modified since deploy`) {
			t.Fatalf("exit %d, stdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		if readFileString(t, at) != "mine\n" {
			t.Error("the user's file changed")
		}
	})
	t.Run("frozen with the old files gone", func(t *testing.T) {
		s := setup(t)
		for _, rel := range []string{".github", ".opencode"} {
			if err := os.RemoveAll(filepath.Join(s.home, rel)); err != nil {
				t.Fatal(err)
			}
		}
		stdout, _, code := run125(t, "install", "-g", "--frozen")
		if code != 1 || !strings.Contains(stdout, "observed <missing>") {
			t.Errorf("exit %d, stdout:\n%s\nwant exit 1 and a <missing> violation", code, stdout)
		}
	})
}

// User-scope MCP (measured, issue125 gmcp-or).
func TestIssue125_UserScopeMCP(t *testing.T) {
	mcpArgs := func(target string) []string {
		return []string{"install", "-g", "--mcp", "srv", "--target", target, "--", "echo", "hi"}
	}
	t.Run("copilot", func(t *testing.T) {
		s := newI125Scope(t)
		const old = "{\"mcpServers\":{\"old\":{\"type\":\"local\",\"command\":\"x\"}}}\n"
		writeCompileFile(t, s.home, ".github/mcp-config.json", old)
		if stdout, _, code := run125(t, mcpArgs("copilot")...); code != 0 {
			t.Fatalf("exit %d, stdout:\n%s", code, stdout)
		}
		if got := readFileString(t, filepath.Join(s.home, ".copilot", "mcp-config.json")); !strings.Contains(got, `"srv"`) {
			t.Errorf("~/.copilot/mcp-config.json:\n%s", got)
		}
		if got := readFileString(t, filepath.Join(s.home, ".github", "mcp-config.json")); got != old {
			t.Errorf("~/.github/mcp-config.json changed:\n%s", got)
		}
	})
	for _, target := range []string{"opencode", "agent-skills"} {
		t.Run(target+" refused", func(t *testing.T) {
			s := newI125Scope(t)
			stdout, stderr, code := run125(t, mcpArgs(target)...)
			want := "Usage: apm-go install [packages...] [flags]\nTry 'apm-go install --help' for help.\n\nError: Selected targets are unavailable for user-scope MCP installation (" +
				target + "; source: --target flag); choose a global-capable --target or omit --global\n"
			if code != 2 || stderr != want || stdout != "" {
				t.Errorf("exit %d, stdout:\n%s\nstderr:\n%s\nwant exit 2 and stderr:\n%s", code, stdout, stderr, want)
			}
			assertNoFile(t, filepath.Join(s.home, ".apm", "apm.yml"))
		})
	}
	t.Run("claude,opencode", func(t *testing.T) {
		s := newI125Scope(t)
		stdout, _, code := run125(t, mcpArgs("claude,opencode")...)
		if code != 0 || !strings.HasPrefix(stdout, " ! Skipped workspace-only runtimes at user scope: opencode -- omit --global to install these\n") ||
			!strings.Contains(stdout, " i Targeting specific runtime: claude\n") {
			t.Fatalf("exit %d, stdout:\n%s", code, stdout)
		}
		if m := readFileString(t, filepath.Join(s.home, ".apm", "apm.yml")); !strings.Contains(m, "targets:\n- claude\ndependencies:") {
			t.Errorf("~/.apm/apm.yml:\n%s", m)
		}
		assertNoFile(t, filepath.Join(s.home, "opencode.json"))
	})
	t.Run("all", func(t *testing.T) {
		newI125Scope(t)
		stdout, _, code := run125(t, mcpArgs("all")...)
		if code != 0 || !strings.Contains(stdout, " ! Skipped workspace-only runtimes at user scope: opencode, vscode -- omit --global to install these\n") ||
			!strings.Contains(stdout, " i Targeting specific runtimes: claude, codex\n") {
			t.Errorf("exit %d, stdout:\n%s", code, stdout)
		}
	})
	t.Run("targets of apm.yml", func(t *testing.T) {
		s := newI125Scope(t)
		writeCompileFile(t, s.home, ".apm/apm.yml", "name: g\nversion: 1.0.0\ntargets: [claude, opencode]\ndependencies:\n  mcp:\n    - name: srv\n      registry: false\n      transport: stdio\n      command: echo\n      args: [hi]\n")
		stdout, _, code := run125(t, "install", "-g")
		if code != 0 || !strings.Contains(stdout, " i Targeting declared targets from apm.yml: claude, opencode\n ! Skipped workspace-only runtimes at user scope: opencode -- omit --global to install these\n") ||
			!strings.Contains(stdout, "|     +- Configuring for Claude...\n") {
			t.Errorf("exit %d, stdout:\n%s", code, stdout)
		}
	})
	t.Run("project opencode unchanged", func(t *testing.T) {
		s := newI125Scope(t)
		s.manifest(t, "")
		if stdout, _, code := run125(t, "install", "--mcp", "srv", "--target", "opencode", "--", "echo", "hi"); code != 0 {
			t.Fatalf("exit %d, stdout:\n%s", code, stdout)
		}
		if !i125Exists(filepath.Join(s.proj, "opencode.json")) {
			t.Error("opencode.json not written")
		}
	})
}

// Item 29: files of a target taken out of targets: are removed; a one-off
// --target and a project with no targets: keep them (measured, issue125
// proj2-or/p29a, p29b, p29d, v29-or/b, v29-or/d, only2-or step 3).
func TestIssue125_StaleFilesOfRemovedTargets(t *testing.T) {
	copilotFiles := []string{".github/agents/ag-dep.agent.md", ".github/instructions/r-dep.instructions.md", ".github/prompts/p-dep.prompt.md", ".agents/skills/sk-dep"}
	present := func(t *testing.T, s i125Scope, want bool) {
		t.Helper()
		for _, rel := range copilotFiles {
			if got := i125Exists(filepath.Join(s.proj, filepath.FromSlash(rel))); got != want {
				t.Errorf("%s present = %v, want %v", rel, got, want)
			}
		}
		recorded := i125Deployed(t, filepath.Join(s.proj, "apm.lock.yaml"))["_local/dep"]
		if got := slices.Contains(recorded, copilotFiles[0]); got != want {
			t.Errorf("lock records %s = %v, want %v: %v", copilotFiles[0], got, want, recorded)
		}
	}
	install := func(t *testing.T, args ...string) string {
		t.Helper()
		stdout, _, code := run125(t, args...)
		if code != 0 {
			t.Fatalf("%v: exit %d, stdout:\n%s", args, code, stdout)
		}
		return stdout
	}
	t.Run("targets shrink", func(t *testing.T) {
		s := newI125Scope(t)
		s.manifest(t, "claude, copilot", s.dep)
		install(t, "install")
		s.manifest(t, "claude", s.dep)
		if out := install(t, "install"); !strings.Contains(out, " i Cleaned 6 stale files from "+s.dep) {
			t.Errorf("stdout:\n%s", out)
		}
		present(t, s, false)
	})
	t.Run("one-off --target narrower than targets:", func(t *testing.T) {
		s := newI125Scope(t)
		s.manifest(t, "claude, copilot", s.dep)
		install(t, "install")
		install(t, "install", "--target", "claude")
		present(t, s, true)
	})
	t.Run("one-off --target wider than targets:", func(t *testing.T) {
		s := newI125Scope(t)
		s.manifest(t, "claude", s.dep)
		install(t, "install")
		install(t, "install", "--target", "claude,copilot")
		install(t, "install")
		present(t, s, false)
	})
	t.Run("no targets:", func(t *testing.T) {
		s := newI125Scope(t)
		s.manifest(t, "", s.dep)
		install(t, "install", "--target", "claude,copilot")
		install(t, "install", "--target", "claude")
		present(t, s, true)
	})
	t.Run("copilot replaced by codex", func(t *testing.T) {
		s := newI125Scope(t)
		s.manifest(t, "claude, copilot", s.dep)
		install(t, "install")
		s.manifest(t, "claude, codex", s.dep)
		install(t, "install")
		for _, rel := range copilotFiles[:3] {
			assertNoFile(t, filepath.Join(s.proj, filepath.FromSlash(rel)))
		}
		for _, rel := range []string{".codex/agents/ag-dep.toml", ".agents/skills/sk-dep/SKILL.md"} {
			if !i125Exists(filepath.Join(s.proj, filepath.FromSlash(rel))) {
				t.Errorf("%s missing", rel)
			}
		}
	})
	t.Run("the project's own content", func(t *testing.T) {
		s := newI125Scope(t)
		writeCompileFile(t, s.proj, ".apm/agents/loc.agent.md", "---\ndescription: l\n---\nlocal\n")
		s.manifest(t, "claude", s.dep)
		install(t, "install")
		install(t, "install", s.dep2, "--target", "claude,copilot")
		if out := install(t, "install"); !strings.Contains(out, " i Cleaned 1 stale file from <local .apm/>\n") {
			t.Errorf("stdout:\n%s", out)
		}
		assertNoFile(t, filepath.Join(s.proj, ".github", "agents", "loc.agent.md"))
	})
}

// install <pkg> integrates the named package alone (measured, issue125
// only-or, impl1/same1).
func TestIssue125_InstallPackageDeploysItAlone(t *testing.T) {
	t.Run("another package's changed source is not deployed", func(t *testing.T) {
		s := newI125Scope(t)
		s.manifest(t, "claude", s.dep)
		if _, _, code := run125(t, "install"); code != 0 {
			t.Fatal("install failed")
		}
		before := i125Deployed(t, "apm.lock.yaml")["_local/dep"]
		writeCompileFile(t, s.dep, ".apm/agents/ag-dep.agent.md", "---\ndescription: a\n---\nagent dep CHANGED\n")
		if _, _, code := run125(t, "install", s.dep2); code != 0 {
			t.Fatal("install dep2 failed")
		}
		if got := readFileString(t, filepath.Join(s.proj, ".claude", "agents", "ag-dep.md")); strings.Contains(got, "CHANGED") {
			t.Errorf("dep's agent was deployed again:\n%s", got)
		}
		if got := i125Deployed(t, "apm.lock.yaml")["_local/dep"]; !slices.Equal(got, before) {
			t.Errorf("dep's record %v, want %v", got, before)
		}
		if _, _, code := run125(t, "install"); code != 0 {
			t.Fatal("install failed")
		}
		if got := readFileString(t, filepath.Join(s.proj, ".claude", "agents", "ag-dep.md")); !strings.Contains(got, "CHANGED") {
			t.Errorf("full install did not deploy dep's agent:\n%s", got)
		}
	})
	t.Run("a same-named agent goes to the named package", func(t *testing.T) {
		s := newI125Scope(t)
		a, b := filepath.Join(s.root, "pkgs", "a"), filepath.Join(s.root, "pkgs", "b")
		for name, dir := range map[string]string{"a": a, "b": b} {
			writeCompileFile(t, dir, "apm.yml", "name: "+name+"\nversion: 1.0.0\n")
			writeCompileFile(t, dir, ".apm/agents/x.agent.md", "---\ndescription: x\n---\nagent from "+name+"\n")
		}
		s.manifest(t, "claude", a)
		if _, _, code := run125(t, "install"); code != 0 {
			t.Fatal("install failed")
		}
		if _, _, code := run125(t, "install", b); code != 0 {
			t.Fatal("install b failed")
		}
		if got := readFileString(t, filepath.Join(s.proj, ".claude", "agents", "x.md")); !strings.Contains(got, "agent from b") {
			t.Errorf("x.md:\n%s\nwant b's agent", got)
		}
		lock := i125Deployed(t, "apm.lock.yaml")
		if len(lock["_local/a"]) != 0 || !slices.Equal(lock["_local/b"], []string{".claude/agents/x.md"}) {
			t.Errorf("lock %v, want x.md under _local/b alone", lock)
		}
	})
}

// A kept directory that leaves a cached git or registry package's lock entry
// without paths is counted in the last line (issue #125 comment 6230746037;
// issue125 keptgit-go, keptreg-go).
func TestIssue125_KeptDirectoryLastLine(t *testing.T) {
	keep := func(t *testing.T, s i125Scope, skill string) {
		t.Helper()
		at := filepath.Join(s.home, ".claude", "skills", skill)
		if err := os.Remove(at); err != nil {
			t.Fatal(err)
		}
		writeCompileFile(t, at, "SKILL.md", "mine\n")
	}
	check := func(t *testing.T) {
		t.Helper()
		if stdout, _, _ := run125(t, "install", "-g"); !strings.HasSuffix(stdout, " + Installed 1 APM dependency in <t>s.\n") {
			t.Errorf("second install -g, stdout:\n%s", stdout)
		}
		if stdout, _, _ := run125(t, "install", "-g"); !strings.HasSuffix(stdout, " i No changes -- install state already up to date in <t>s.\n") {
			t.Errorf("third install -g, stdout:\n%s", stdout)
		}
	}
	t.Run("git", func(t *testing.T) {
		s := newI125Scope(t)
		setSymlinkProbe(t, true)
		useLocalGitRemotes(t)
		summaryGitRepo(t, filepath.Join(s.home, ".apm", "o", "gpkg"), map[string]string{
			"apm.yml": "name: gpkg\nversion: 1.0.0\n", ".apm/skills/sk-gpkg/SKILL.md": "---\nname: sk-gpkg\ndescription: d\n---\nskill\n",
			".apm/agents/ag.agent.md": "---\ndescription: a\n---\nagent\n"}, "v1.0.0")
		writeCompileFile(t, s.home, ".apm/apm.yml", "name: g\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - o/gpkg#v1.0.0\n")
		if stdout, _, code := run125(t, "install", "-g"); code != 0 {
			t.Fatalf("exit %d, stdout:\n%s", code, stdout)
		}
		keep(t, s, "sk-gpkg")
		check(t)
	})
	t.Run("registry", func(t *testing.T) {
		s := newI125Scope(t)
		setSymlinkProbe(t, true)
		enableRegistries(t)
		archive := summaryTarGz(t, map[string]string{"apm.yml": "name: sample\nversion: 1.0.0\n",
			".apm/skills/probe/SKILL.md": "---\nname: probe\ndescription: d\n---\nprobe\n", ".apm/agents/ag.agent.md": "---\ndescription: a\n---\nagent\n"})
		sum := sha256.Sum256(archive)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/versions"):
				fmt.Fprintf(w, `{"package":"acme/sample","versions":[{"version":"1.0.0","digest":"sha256:%s","published_at":"2026-01-01T00:00:00Z"}]}`, hex.EncodeToString(sum[:]))
			case strings.HasSuffix(r.URL.Path, "/download"):
				w.Write(archive)
			default:
				http.Error(w, "nf", http.StatusNotFound)
			}
		}))
		t.Cleanup(srv.Close)
		writeCompileFile(t, s.home, ".apm/apm.yml", "name: g\nversion: 1.0.0\ntargets: [claude]\nregistries:\n  local:\n    url: "+srv.URL+
			"\n    insecure: true\n  default: local\ndependencies:\n  apm:\n    - id: acme/sample\n      version: 1.0.0\n")
		if stdout, _, code := run125(t, "install", "-g"); code != 0 {
			t.Fatalf("exit %d, stdout:\n%s", code, stdout)
		}
		keep(t, s, "probe")
		check(t)
	})
}

// ~/.config/opencode is a target root and ~/.config is not: the cleanups and
// uninstall -g leave ~/.config and what other tools keep there, also when it
// is empty, and write through a ~/.config/opencode linked into HOME or out
// of it (owner ruling, issue #125, step 11).
func TestIssue125_ConfigDirIsTheUsers(t *testing.T) {
	others := []string{".config/other/settings.json", ".config/opencode/opencode.json"}
	for _, link := range []string{"", "inside", "outside"} {
		t.Run("opencode root "+cmp125(link, "a directory"), func(t *testing.T) {
			s := newI125Scope(t)
			for _, rel := range others {
				writeCompileFile(t, s.home, rel, "user\n")
			}
			s.mkdirs(t, s.home, ".config/empty-tool")
			real := filepath.Join(s.home, ".config", "opencode")
			if link != "" {
				real = filepath.Join(s.home, "dotfiles", "opencode")
				if link == "outside" {
					real = filepath.Join(s.root, "elsewhere", "opencode")
				}
				if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(filepath.Join(s.home, ".config", "opencode"), real); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(real, filepath.Join(s.home, ".config", "opencode")); err != nil {
					t.Fatal(err)
				}
			}
			setSymlinkProbe(t, true)
			if stdout, _, code := run125(t, "install", "-g", s.dep, "--target", "opencode,claude"); code != 0 {
				t.Fatalf("install -g: exit %d, stdout:\n%s", code, stdout)
			}
			if !i125Exists(filepath.Join(real, "agents", "ag-dep.md")) {
				t.Fatalf("agent not deployed into %s", real)
			}
			// The stale cleanup of opencode's files once targets: drops it.
			writeCompileFile(t, s.home, ".apm/apm.yml", strings.Replace(readFileString(t, filepath.Join(s.home, ".apm", "apm.yml")), "  - opencode\n", "", 1))
			if stdout, _, code := run125(t, "install", "-g"); code != 0 {
				t.Fatalf("second install -g: exit %d, stdout:\n%s", code, stdout)
			}
			assertNoFile(t, filepath.Join(real, "agents", "ag-dep.md"))
			if stdout, _, code := run125(t, "uninstall", "-g", s.dep); code != 0 {
				t.Fatalf("uninstall -g: exit %d, stdout:\n%s", code, stdout)
			}
			for _, rel := range []string{others[0], ".config/empty-tool", ".config"} {
				if !i125Exists(filepath.Join(s.home, filepath.FromSlash(rel))) {
					t.Errorf("%s removed", rel)
				}
			}
			if got := readFileString(t, filepath.Join(real, "opencode.json")); got != "user\n" {
				t.Errorf("opencode.json = %q", got)
			}
			if link != "" {
				if fi, err := os.Lstat(filepath.Join(s.home, ".config", "opencode")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
					t.Errorf("~/.config/opencode is no longer the user's symlink (err %v)", err)
				}
			}
		})
	}
	t.Run("an empty ~/.config stays", func(t *testing.T) {
		s := newI125Scope(t)
		s.mkdirs(t, s.home, ".config")
		if _, _, code := run125(t, "install", "-g", s.dep, "--target", "opencode"); code != 0 {
			t.Fatal("install -g failed")
		}
		if _, _, code := run125(t, "uninstall", "-g", s.dep); code != 0 {
			t.Fatal("uninstall -g failed")
		}
		if !i125Exists(filepath.Join(s.home, ".config")) {
			t.Error("~/.config removed")
		}
	})
}

func cmp125(a, b string) string {
	if a == "" {
		return b
	}
	return "linked " + a
}

// An install at user scope whose MCP servers no target takes there fails
// with exit 1 after it deploys and writes the lock, as Oracle 0.33.0's
// RequiredIntegrationError (install/service_integration.py:128,
// integration/mcp_integrator_install.py:1199-1204; measured, issue125
// r15c/yo, yodep, tflagdep).
func TestIssue125_UserScopeMCPNoTarget(t *testing.T) {
	const mcp = "  mcp:\n    - name: srv\n      registry: false\n      transport: stdio\n      command: echo\n      args: [hi]\n"
	const tail = " ! Skipped workspace-only runtimes at user scope: opencode -- omit --global to install these\n" +
		" ! No runtimes support user-scope MCP installation (supported: Copilot CLI, Claude Code, Codex CLI, Gemini CLI, Antigravity CLI, Hermes, Kiro, Windsurf, JetBrains Copilot)\n" +
		" x MCP dependencies are declared, but no effective target can accept their configuration. Choose a supported --target and retry.\n" +
		" x Install failed after <t>s.\n"
	cases := []struct {
		name, targets, targeting string
		withDep                  bool
		args                     []string
	}{
		{"targets of apm.yml", "targets: [opencode]\n", " i Targeting declared target from apm.yml: opencode\n", false, []string{"install", "-g"}},
		{"targets of apm.yml and a package", "targets: [opencode]\n", " i Targeting declared target from apm.yml: opencode\n", true, []string{"install", "-g"}},
		{"--target and a package", "", " i Targeting specific runtime: opencode\n", true, []string{"install", "-g", "--target", "opencode"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newI125Scope(t)
			deps := "dependencies:\n"
			if c.withDep {
				deps += "  apm:\n    - " + s.dep + "\n"
			}
			manifest := "name: g\nversion: 1.0.0\n" + c.targets + deps + mcp
			writeCompileFile(t, s.home, ".apm/apm.yml", manifest)
			stdout, _, code := run125(t, c.args...)
			stdout = regexp.MustCompile(`after [0-9]+\.[0-9]+s\.`).ReplaceAllString(stdout, "after <t>s.")
			// The compile hint is apm-go's own line, not this item's.
			stdout = regexp.MustCompile(`(?m)^ i Instructions installed for .*\n`).ReplaceAllString(stdout, "")
			if code != 1 || !strings.HasSuffix(stdout, "+- MCP Servers (1)\n"+c.targeting+tail) {
				t.Fatalf("exit %d, stdout:\n%s\nwant exit 1, ending:\n+- MCP Servers (1)\n%s%s", code, stdout, c.targeting, tail)
			}
			if got := readFileString(t, filepath.Join(s.home, ".apm", "apm.yml")); got != manifest {
				t.Errorf("~/.apm/apm.yml changed:\n%s", got)
			}
			lockPath := filepath.Join(s.home, ".apm", "apm.lock.yaml")
			if !c.withDep {
				assertNoFile(t, lockPath)
				return
			}
			want := []string{".config/opencode/agents/ag-dep.md", ".config/opencode/commands/p-dep.md", ".config/opencode/skills/sk-dep", ".config/opencode/skills/sk-dep/SKILL.md"}
			if got := i125Deployed(t, lockPath)["_local/dep"]; !slices.Equal(got, want) {
				t.Errorf("deployed_files %v, want %v", got, want)
			}
			if strings.Contains(readFileString(t, lockPath), "mcp_servers") {
				t.Error("the lock records the MCP server")
			}
		})
	}
}
