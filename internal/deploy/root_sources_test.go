package deploy

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
)

func namesOfType(prims []Primitive, pt PrimitiveType) []string {
	var names []string
	for _, p := range prims {
		if p.Type == pt {
			names = append(names, p.Name)
		}
	}
	return names
}

func deployedFiles(t *testing.T, dir, rel string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// A package's root agents are its *.agent.md files, not recursive, read
// before .apm/agents/ (find_agent_files, integration/agent_integrator.py:
// 71-113). Oracle 0.33.0 deploys rootag and nofm of the root and not
// cm.chatmode.md, plain.md or sub/deep.agent.md (agent/s1).
func TestCollect_RootAgents(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "apm.yml", "name: dep\nversion: 1.0.0\n")
	mkFile(t, dir, ".apm/agents/inner.agent.md", "---\ndescription: inner agent\n---\nInner body\n")
	mkFile(t, dir, "rootag.agent.md", "---\ndescription: root agent\n---\nRoot body\n")
	mkFile(t, dir, "nofm.agent.md", "no frontmatter body\n")
	mkFile(t, dir, "cm.chatmode.md", "---\ndescription: chat\n---\nChat body\n")
	mkFile(t, dir, "plain.md", "---\ndescription: plain\n---\nPlain body\n")
	mkFile(t, dir, "sub/deep.agent.md", "---\ndescription: deep\n---\nDeep body\n")

	prims, err := CollectDependencyPrimitives("acme/dep", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := namesOfType(prims, TypeAgents), []string{"nofm", "rootag", "inner"}; !slices.Equal(got, want) {
		t.Errorf("dependency agents = %v, want %v", got, want)
	}
}

// The project's own root agents are read only when it has a .apm/
// directory, an empty one too (agent/s4b, s4c), and not for --global
// (agent/s5b).
func TestCollect_ProjectRootAgentsNeedAPMDir(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "projag.agent.md", "---\ndescription: proj root agent\n---\nProj root body\n")
	if got := namesOfType(CollectLocalPrimitives(dir), TypeAgents); len(got) != 0 {
		t.Errorf("agents without .apm/ = %v, want none", got)
	}
	mkdir(t, filepath.Join(dir, ".apm"))
	if got, want := namesOfType(CollectLocalPrimitives(dir), TypeAgents), []string{"projag"}; !slices.Equal(got, want) {
		t.Errorf("agents with .apm/ = %v, want %v", got, want)
	}
	if got := namesOfType(CollectScopeLocalPrimitives(dir, true), TypeAgents); len(got) != 0 {
		t.Errorf("--global agents = %v, want none", got)
	}
}

// A root x.agent.md and .apm/agents/x.agent.md of one package deploy the
// root one, as Oracle 0.33.0's first install does (agent/s2). Deviation 3(4)
// of issue #126 comment 6267289737 keeps it on later installs too.
func TestRun_RootAgentWinsOverAPMAgent(t *testing.T) {
	dir := t.TempDir()
	mod := filepath.Join(dir, "apm_modules", "acme", "dep")
	mkFile(t, mod, "apm.yml", "name: dep\nversion: 1.0.0\n")
	mkFile(t, mod, ".apm/agents/x.agent.md", "---\ndescription: apm x\n---\nAPM x body\n")
	mkFile(t, mod, "x.agent.md", "---\ndescription: root x\n---\nRoot x body\n")
	m, resolved := depManifest("acme/dep")
	for range 2 {
		if _, err := Run([]string{"claude"}, dir, m, resolved, nil, "", false, nil); err != nil {
			t.Fatal(err)
		}
		if got, want := readDeployed(t, dir, ".claude/agents/x.md"), "---\ndescription: root x\n---\nRoot x body\n"; got != want {
			t.Errorf(".claude/agents/x.md = %q, want %q", got, want)
		}
	}
}

// A Claude plugin's root agents are not staged and come before its staged
// agents (agent/s3: Oracle 0.33.0's first install writes the root x).
func TestRun_PluginRootAgentsBeforeStagedAgents(t *testing.T) {
	dir := t.TempDir()
	mod := filepath.Join(dir, "apm_modules", "acme", "plug")
	mkFile(t, mod, ".claude-plugin/plugin.json", `{"name": "plug", "version": "1.0.0"}`+"\n")
	mkFile(t, mod, "agents/a.md", "---\ndescription: plugin a\n---\nA body\n")
	mkFile(t, mod, "agents/x.md", "---\ndescription: plugin x\n---\nPlugin x body\n")
	mkFile(t, mod, "rootag.agent.md", "---\ndescription: root agent\n---\nRoot body\n")
	mkFile(t, mod, "x.agent.md", "---\ndescription: root x\n---\nRoot x body\n")
	m, resolved := depManifest("acme/plug")
	if _, err := Run([]string{"claude"}, dir, m, resolved, nil, "", false, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := deployedFiles(t, dir, ".claude/agents"), []string{"a.md", "rootag.md", "x.md"}; !slices.Equal(got, want) {
		t.Errorf(".claude/agents = %v, want %v", got, want)
	}
	if got, want := readDeployed(t, dir, ".claude/agents/x.md"), "---\ndescription: root x\n---\nRoot x body\n"; got != want {
		t.Errorf(".claude/agents/x.md = %q, want %q", got, want)
	}
}

// A package's hooks are the *.json of .apm/hooks/, then of hooks/, each in
// name order and not recursive, one per stem in any case, .apm/hooks/ first
// (find_hook_files, integration/hook_integrator.py:449-488). Oracle 0.33.0
// deploys H.json of .apm/hooks/ and naked.json of hooks/ and not hooks/h.json
// (hook/n-or), and not hooks/sub/c.json or hooks/notes.txt (hook/a-copilot-or).
func TestCollect_RootHooks(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "apm.yml", "name: dep\nversion: 1.0.0\n")
	mkFile(t, dir, ".apm/hooks/H.json", `{"hooks":{}}`)
	mkFile(t, dir, "hooks/h.json", `{"hooks":{}}`)
	mkFile(t, dir, "hooks/naked.json", `{"hooks":{}}`)
	mkFile(t, dir, "hooks/sub/c.json", `{"hooks":{}}`)
	mkFile(t, dir, "hooks/notes.txt", "x\n")
	prims, err := CollectDependencyPrimitives("_local/dep", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := namesOfType(prims, TypeHooks), []string{"H", "naked"}; !slices.Equal(got, want) {
		t.Errorf("dependency hooks = %v, want %v", got, want)
	}
}

// The project's root hooks/ is read only when it has a .apm/ directory
// (install/phases/local_content.py:44-56; hook/l-rootapm-or, l-rootonly-or),
// and --global reads ~/.apm/.apm/hooks/ alone (#88 item 68).
func TestCollect_ProjectRootHooksNeedAPMDir(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "hooks/r.json", `{"hooks":{}}`)
	if got := namesOfType(CollectLocalPrimitives(dir), TypeHooks); len(got) != 0 {
		t.Errorf("hooks without .apm/ = %v, want none", got)
	}
	mkFile(t, dir, ".apm/hooks/x.json", `{"hooks":{}}`)
	if got, want := namesOfType(CollectLocalPrimitives(dir), TypeHooks), []string{"x", "r"}; !slices.Equal(got, want) {
		t.Errorf("hooks with .apm/ = %v, want %v", got, want)
	}
	if got, want := namesOfType(CollectScopeLocalPrimitives(dir, true), TypeHooks), []string{"x"}; !slices.Equal(got, want) {
		t.Errorf("--global hooks = %v, want %v", got, want)
	}
}

// _safe_source_name (integration/hook_integrator.py:844-855); "" is its
// fallback. "My Proj!" is measured (hook/n-or: My-Proj-l.json).
func TestSafeSourceName(t *testing.T) {
	for in, want := range map[string]string{
		"My Proj!":   "My-Proj",
		"myproj":     "myproj",
		"  my.proj ": "my.proj",
		"a..b":       "a.b",
		"--x__":      "x",
		"_local":     "local",
		"...":        "",
		"!!!":        "",
		"":           "",
	} {
		if got := safeSourceName(in); got != want {
			t.Errorf("safeSourceName(%q) = %q, want %q", in, got, want)
		}
	}
}

// Each copilot hook is .github/hooks/<package>-<stem>.json
// (integration/hook_integrator.py:1065-1067): a dependency's package is its
// directory below apm_modules, the project's the name its apm.yml gives,
// made safe (hook/a-copilot-or, n-or, l-two-or, g1run/g3-or), "local" with
// no apm.yml (hook/glob-copilot-or).
func TestRun_CopilotHookFileNames(t *testing.T) {
	for _, tt := range []struct {
		name, manifest string
		want           []string
	}{
		{"named project", "name: \"My Proj!\"\nversion: 1.0.0\n", []string{"My-Proj-l.json", "dep-a.json", "dep-b.json", "pkg-s.json"}},
		{"no apm.yml", "", []string{"dep-a.json", "dep-b.json", "local-l.json", "pkg-s.json"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.manifest != "" {
				mkFile(t, dir, "apm.yml", tt.manifest)
			}
			mkFile(t, dir, ".apm/hooks/l.json", `{"hooks":{"l":[]}}`)
			mod := filepath.Join(dir, "apm_modules", "acme", "dep")
			mkFile(t, mod, ".apm/hooks/a.json", `{"hooks":{"a":[]}}`)
			mkFile(t, mod, "hooks/b.json", `{"hooks":{"b":[]}}`)
			mkFile(t, filepath.Join(dir, "apm_modules", "acme", "tools", "sub", "pkg"), ".apm/hooks/s.json", `{"hooks":{"s":[]}}`)
			m, resolved := depManifest("acme/dep", "acme/tools/sub/pkg")
			if _, err := Run([]string{"copilot"}, dir, m, resolved, nil, "", false, nil); err != nil {
				t.Fatal(err)
			}
			got := deployedFiles(t, dir, ".github/hooks")
			sort.Strings(got)
			if !slices.Equal(got, tt.want) {
				t.Errorf(".github/hooks = %v, want %v", got, tt.want)
			}
		})
	}
}

// Hooks of one name from two packages are two copilot files and no conflict
// (hook/l-two-or). On codex, which writes every hook to .codex/hooks.json,
// they still conflict as before, with the same diagnostic.
func TestRun_CopilotHooksOfOneNameDoNotConflict(t *testing.T) {
	const shadowed = `hooks "h" from dependency:acme/z shadowed by dependency:acme/a (first-declared wins)`
	for _, tt := range []struct {
		targets  []string
		wantDiag bool
	}{
		{[]string{"copilot"}, false},
		{[]string{"copilot", "codex"}, true},
		{[]string{"codex"}, true},
	} {
		dir := t.TempDir()
		for _, p := range []string{"a", "z"} {
			mkFile(t, filepath.Join(dir, "apm_modules", "acme", p), ".apm/hooks/h.json", `{"hooks":{"`+p+`":[]}}`)
		}
		m, resolved := depManifest("acme/a", "acme/z")
		result, err := Run(tt.targets, dir, m, resolved, nil, "", false, nil)
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(tt.targets, "copilot") {
			if got, want := deployedFiles(t, dir, ".github/hooks"), []string{"a-h.json", "z-h.json"}; !slices.Equal(got, want) {
				t.Errorf("%v: .github/hooks = %v, want %v", tt.targets, got, want)
			}
			if got := readDeployed(t, dir, ".github/hooks/z-h.json"); got != `{"hooks":{"z":[]}}` {
				t.Errorf("%v: z-h.json = %q", tt.targets, got)
			}
		}
		if got := slices.Contains(result.Diags, shadowed); got != tt.wantDiag {
			t.Errorf("%v: diags %q, want the shadowed diagnostic: %v", tt.targets, result.Diags, tt.wantDiag)
		}
		if slices.Contains(tt.targets, "codex") {
			if got := readDeployed(t, dir, ".codex/hooks.json"); got != `{"hooks":{"a":[]}}` {
				t.Errorf("%v: .codex/hooks.json = %q, want a's", tt.targets, got)
			}
		}
	}
}

// Two packages whose directories have one name write one copilot file:
// the first in priority order keeps it (hook/g1run/g2-or).
func TestRun_CopilotHooksOfOnePackageName(t *testing.T) {
	dir := t.TempDir()
	for _, owner := range []string{"o1", "o2"} {
		mkFile(t, filepath.Join(dir, "apm_modules", owner, "tools"), ".apm/hooks/h.json", `{"hooks":{"`+owner+`":[]}}`)
	}
	m, resolved := depManifest("o1/tools", "o2/tools")
	result, err := Run([]string{"copilot"}, dir, m, resolved, nil, "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := readDeployed(t, dir, ".github/hooks/tools-h.json"); got != `{"hooks":{"o1":[]}}` {
		t.Errorf("tools-h.json = %q, want o1's", got)
	}
	if _, ok := result.PerDep["o2/tools"]; ok {
		t.Errorf("o2/tools recorded %v, want nothing", result.PerDep["o2/tools"].Files)
	}
}

// The project's hook and a dependency's both write .codex/hooks.json; the
// file holds the dependency's, so only the dependency records it (issue #126
// comment 6267289737, item 2). The overwrite warning is main's.
func TestRun_CodexHooksJSONRecordedWhereItStays(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/hooks/l.json", `{"hooks":{"l":[]}}`)
	mkFile(t, filepath.Join(dir, "apm_modules", "acme", "dep"), ".apm/hooks/a.json", `{"hooks":{"a":[]}}`)
	m, resolved := depManifest("acme/dep")
	result, err := Run([]string{"codex"}, dir, m, resolved, nil, "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	const path = ".codex/hooks.json"
	if want := `hooks "a" overwrites .codex/hooks.json already written by "l" (single-file target)`; !slices.Contains(result.Diags, want) {
		t.Errorf("diags = %q, want %q", result.Diags, want)
	}
	local := result.PerDep[""]
	if !slices.Contains(local.Files, path) {
		t.Errorf("local files = %v, want %s listed for the install block", local.Files, path)
	}
	if files, hashes := local.Recorded(); slices.Contains(files, path) || hashes[path] != "" {
		t.Errorf("local records %v %v, want no %s", files, hashes, path)
	}
	files, hashes := result.PerDep["acme/dep"].Recorded()
	want, err := lockfile.HashDeployedFile(filepath.Join(dir, path))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(files, path) || hashes[path] != want {
		t.Errorf("dependency records %v %v, want %s with the hash on disk %s", files, hashes, path, want)
	}
}
