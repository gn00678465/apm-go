package deploy

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/resolver"
)

// Issue #69: a dependency's `targets:` limits it to the active targets it
// names. The files and their owners are what Oracle 0.33.0 deployed and
// recorded for the same two packages (~/.cache/apm-go-verify/issue69/t1-or,
// t2-or, a3-or).

const targetsAgent = "---\nname: dagent\ndescription: d\n---\nfrom %s\n"

// twoAgentProject vendors dep1 and dep2, each with the agent dagent, and
// declares them in that order with the given targets: lists.
func twoAgentProject(t *testing.T, dep1Targets, dep2Targets []string) (string, *manifest.Manifest) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"dep1", "dep2"} {
		mkFile(t, filepath.Join(dir, "apm_modules", "acme", name), ".apm/agents/dagent.agent.md", strings.Replace(targetsAgent, "%s", name, 1))
	}
	m := &manifest.Manifest{Name: "repro", Version: "1.0.0", ParsedDeps: []*manifest.DependencyReference{
		{RepoURL: "acme/dep1", Owner: "acme", Repo: "dep1", Source: "git", TargetSubset: dep1Targets},
		{RepoURL: "acme/dep2", Owner: "acme", Repo: "dep2", Source: "git", TargetSubset: dep2Targets},
	}}
	return dir, m
}

func assertDeployedFrom(t *testing.T, dir string, want map[string]string) {
	t.Helper()
	for rel, from := range want {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		if !strings.HasSuffix(string(data), "from "+from+"\n") {
			t.Errorf("%s = %q, want the agent of %s", rel, data, from)
		}
	}
}

func depFiles(r *DeployResult, key string) []string {
	if dr := r.PerDep[key]; dr != nil {
		return dr.Files
	}
	return nil
}

func TestRun_TargetSubset_ConflictDecidedPerTarget(t *testing.T) {
	dir, m := twoAgentProject(t, []string{"claude"}, nil)

	r, err := Run([]string{"claude", "copilot"}, dir, m, nil, nil, "", false, nil)
	if err != nil {
		t.Fatal(err)
	}

	assertDeployedFrom(t, dir, map[string]string{
		".claude/agents/dagent.md":       "dep1",
		".github/agents/dagent.agent.md": "dep2",
	})
	if got := depFiles(r, "acme/dep1"); !slices.Equal(got, []string{".claude/agents/dagent.md"}) {
		t.Errorf("acme/dep1 files = %q", got)
	}
	if got := depFiles(r, "acme/dep2"); !slices.Equal(got, []string{".github/agents/dagent.agent.md"}) {
		t.Errorf("acme/dep2 files = %q", got)
	}
	want := []string{`agents "dagent" from dependency:acme/dep2 shadowed by dependency:acme/dep1 (first-declared wins)`}
	if !slices.Equal(r.Diags, want) {
		t.Errorf("Diags = %q, want %q", r.Diags, want)
	}
	if len(r.SkippedDeps) != 0 {
		t.Errorf("SkippedDeps = %+v, want none", r.SkippedDeps)
	}
}

func TestRun_TargetSubset_ConflictOnEveryTargetReportedOnce(t *testing.T) {
	dir, m := twoAgentProject(t, nil, []string{"claude"})

	r, err := Run([]string{"claude", "copilot"}, dir, m, nil, nil, "", false, nil)
	if err != nil {
		t.Fatal(err)
	}

	assertDeployedFrom(t, dir, map[string]string{
		".claude/agents/dagent.md":       "dep1",
		".github/agents/dagent.agent.md": "dep1",
	})
	if got := depFiles(r, "acme/dep2"); got != nil {
		t.Errorf("acme/dep2 files = %q, want none", got)
	}
	want := []string{`agents "dagent" from dependency:acme/dep2 shadowed by dependency:acme/dep1 (first-declared wins)`}
	if !slices.Equal(r.Diags, want) {
		t.Errorf("Diags = %q, want %q", r.Diags, want)
	}
}

func TestRun_NoTargetSubset_DiagsUnchanged(t *testing.T) {
	dir, m := twoAgentProject(t, nil, nil)

	r, err := Run([]string{"claude", "copilot"}, dir, m, nil, nil, "", false, nil)
	if err != nil {
		t.Fatal(err)
	}

	assertDeployedFrom(t, dir, map[string]string{
		".claude/agents/dagent.md":       "dep1",
		".github/agents/dagent.agent.md": "dep1",
	})
	want := []string{`agents "dagent" from dependency:acme/dep2 shadowed by dependency:acme/dep1 (first-declared wins)`}
	if !slices.Equal(r.Diags, want) {
		t.Errorf("Diags = %q, want %q", r.Diags, want)
	}
}

func TestRun_TargetSubset_NoActiveTargetSkipsButKeepsMCP(t *testing.T) {
	dir := t.TempDir()
	module := filepath.Join(dir, "apm_modules", "acme", "dep")
	mkFile(t, module, ".apm/agents/dagent.agent.md", strings.Replace(targetsAgent, "%s", "dep", 1))
	mkFile(t, module, "apm.yml", "name: dep\nversion: 1.0.0\ndependencies:\n  mcp:\n    - name: depmcp\n      registry: false\n      transport: stdio\n      command: echo\n")
	m := &manifest.Manifest{Name: "repro", Version: "1.0.0", ParsedDeps: []*manifest.DependencyReference{
		{RepoURL: "acme/dep", Owner: "acme", Repo: "dep", Source: "git", TargetSubset: []string{"codex"}},
	}}

	r, err := Run([]string{"claude", "copilot"}, dir, m, nil, nil, "", false, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := depFiles(r, "acme/dep"); got != nil {
		t.Errorf("acme/dep files = %q, want none", got)
	}
	want := []SkippedDep{{Key: "acme/dep", Targets: []string{"codex"}}}
	if !slices.EqualFunc(r.SkippedDeps, want, func(a, b SkippedDep) bool { return a.Key == b.Key && slices.Equal(a.Targets, b.Targets) }) {
		t.Errorf("SkippedDeps = %+v, want %+v", r.SkippedDeps, want)
	}
	var mcpTargets []string
	for _, p := range r.MCPProvenance {
		if p.Server == "depmcp" {
			mcpTargets = append(mcpTargets, p.Target)
		}
	}
	if !slices.Equal(mcpTargets, []string{"claude", "copilot"}) {
		t.Errorf("depmcp configured for %q, want claude and copilot", mcpTargets)
	}
	for _, rel := range []string{".mcp.json", ".github/mcp-config.json"} {
		if data, err := os.ReadFile(filepath.Join(dir, rel)); err != nil || !strings.Contains(string(data), "depmcp") {
			t.Errorf("%s does not configure depmcp (err %v):\n%s", rel, err, data)
		}
	}
}

func TestRun_TargetSubset_TransitiveFromResolution(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, filepath.Join(dir, "apm_modules", "acme", "a"), ".apm/agents/aagent.agent.md", "---\nname: aagent\ndescription: d\n---\nA\n")
	mkFile(t, filepath.Join(dir, "apm_modules", "acme", "b"), ".apm/agents/bagent.agent.md", "---\nname: bagent\ndescription: d\n---\nB\n")
	m := &manifest.Manifest{Name: "repro", Version: "1.0.0", ParsedDeps: []*manifest.DependencyReference{
		{RepoURL: "acme/a", Owner: "acme", Repo: "a", Source: "git", TargetSubset: []string{"claude"}},
	}}
	resolved := &resolver.ResolutionResult{Deps: []resolver.ResolvedDep{
		{Key: "acme/a", RepoURL: "acme/a", Depth: 1},
		{Key: "acme/b", RepoURL: "acme/b", Depth: 2, TargetSubset: []string{"copilot"}},
	}}

	r, err := Run([]string{"claude", "copilot"}, dir, m, resolved, nil, "", false, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := depFiles(r, "acme/a"); !slices.Equal(got, []string{".claude/agents/aagent.md"}) {
		t.Errorf("acme/a files = %q", got)
	}
	if got := depFiles(r, "acme/b"); !slices.Equal(got, []string{".github/agents/bagent.agent.md"}) {
		t.Errorf("acme/b files = %q", got)
	}
}

// s1-or: copilot and codex both copy a skill to .agents/skills/<name>. dep1
// gives sk to copilot only, dep2 to both; the Oracle keeps dep1's copy and
// records it under dep1 alone.
func TestRun_TargetSubset_SharedSkillDirKeepsFirstWriter(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"dep1", "dep2"} {
		mkFile(t, filepath.Join(dir, "apm_modules", "acme", name), ".apm/skills/sk/SKILL.md", "---\nname: sk\ndescription: d\n---\nfrom "+name+"\n")
	}
	m := &manifest.Manifest{Name: "repro", Version: "1.0.0", ParsedDeps: []*manifest.DependencyReference{
		{RepoURL: "acme/dep1", Owner: "acme", Repo: "dep1", Source: "git", TargetSubset: []string{"copilot"}},
		{RepoURL: "acme/dep2", Owner: "acme", Repo: "dep2", Source: "git"},
	}}

	r, err := Run([]string{"copilot", "codex"}, dir, m, nil, nil, "", false, nil)
	if err != nil {
		t.Fatal(err)
	}

	assertDeployedFrom(t, dir, map[string]string{".agents/skills/sk/SKILL.md": "dep1"})
	if got := depFiles(r, "acme/dep1"); !slices.Equal(got, []string{".agents/skills/sk/SKILL.md"}) {
		t.Errorf("acme/dep1 files = %q", got)
	}
	if got := depFiles(r, "acme/dep2"); got != nil {
		t.Errorf("acme/dep2 files = %q, want none", got)
	}
	const wantHash = "sha256:bd5850cc21c98a0cb25805de652cc5692570d5c1be6381d417d92a3971a09c1f"
	if got := r.PerDep["acme/dep1"].Hashes[".agents/skills/sk/SKILL.md"]; got != wantHash {
		t.Errorf("recorded hash = %s, want the Oracle's %s", got, wantHash)
	}
}

// keepFirstSharedSkill finds a target's skill directory through skillRooter,
// so an adapter that deploys skills without it would escape the rule.
func TestAdapters_SkillTargetsNameTheirSkillRoot(t *testing.T) {
	for name, adapter := range Adapters {
		if !adapterSupports(adapter, TypeSkills) {
			continue
		}
		if _, ok := adapter.(skillRooter); !ok {
			t.Errorf("%s deploys skills but does not implement skillRooter", name)
		}
	}
}
