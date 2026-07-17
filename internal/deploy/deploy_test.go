package deploy

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/apm-go/apm/internal/yamlcore"
)

func TestResolveTargets_FlagOverrides(t *testing.T) {
	dir := t.TempDir()
	// Create claude signal
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	// Flag should override detection
	targets, _ := ResolveTargets("codex", []string{"claude"}, dir)
	if len(targets) != 1 || targets[0] != "codex" {
		t.Errorf("expected [codex], got %v", targets)
	}
}

func TestResolveTargets_FlagAllExcludesAntigravity(t *testing.T) {
	// Explicit-only alignment (user decision 2026-07-05, matching Python
	// EXPLICIT_ONLY_TARGETS={"agent-skills","antigravity"}): neither
	// agent-skills nor antigravity may ride along on --target all.
	dir := t.TempDir()
	targets, _ := ResolveTargets("all", nil, dir)
	for _, tgt := range targets {
		if tgt == "antigravity" {
			t.Error("antigravity must not be included by --target all (explicit-only)")
		}
		if tgt == "agent-skills" {
			t.Error("agent-skills must not be included by --target all")
		}
	}
}

func TestResolveTargets_ManifestAllExcludesAntigravity(t *testing.T) {
	// The apm.yml target: [all] expansion path must apply the same
	// explicit-only exclusion as the --target all flag path.
	dir := t.TempDir()
	targets, _ := ResolveTargets("", []string{"all"}, dir)
	for _, tgt := range targets {
		if tgt == "antigravity" {
			t.Error("antigravity must not be included by manifest target: [all] (explicit-only)")
		}
		if tgt == "agent-skills" {
			t.Error("agent-skills must not be included by manifest target: [all]")
		}
	}
}

func TestResolveTargets_ManifestTargets(t *testing.T) {
	dir := t.TempDir()

	targets, _ := ResolveTargets("", []string{"claude", "copilot"}, dir)
	if len(targets) != 2 {
		t.Errorf("expected 2 targets, got %v", targets)
	}
}

func TestResolveTargets_AutoDetect(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	targets, _ := ResolveTargets("", nil, dir)
	if len(targets) != 1 || targets[0] != "claude" {
		t.Errorf("expected [claude], got %v", targets)
	}
}

func TestResolveTargets_NoSignal(t *testing.T) {
	dir := t.TempDir()

	targets, _ := ResolveTargets("", nil, dir)
	if len(targets) != 0 {
		t.Errorf("expected empty, got %v", targets)
	}
}

func TestResolveTargets_AgentSkillsNotAutoDetected(t *testing.T) {
	// req-tg-001: agent-skills NEVER auto-detected
	dir := t.TempDir()
	// Even with .agents/ directory, agent-skills should not be auto-detected
	os.MkdirAll(filepath.Join(dir, ".agents"), 0755)

	targets, _ := ResolveTargets("", nil, dir)
	for _, t2 := range targets {
		if t2 == "agent-skills" {
			t.Error("agent-skills should never be auto-detected")
		}
	}
}

func TestResolveTargets_AntigravityNotAutoDetected(t *testing.T) {
	// Explicit-only alignment (user decision 2026-07-05, matching Python
	// EXPLICIT_ONLY_TARGETS={"agent-skills","antigravity"}): GEMINI.md and
	// AGENTS.md are cross-tool files (also read by opencode/agent-skills
	// tooling), so their presence must NOT auto-enable antigravity -- and
	// with no other signals present the resolution must be empty. This
	// flips the earlier auto-detect behavior.
	tests := []struct {
		name  string
		files []string
	}{
		{"GEMINI.md alone", []string{"GEMINI.md"}},
		{"AGENTS.md alone", []string{"AGENTS.md"}},
		{"both GEMINI.md and AGENTS.md", []string{"GEMINI.md", "AGENTS.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("# marker"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			targets, _ := ResolveTargets("", nil, dir)
			if len(targets) != 0 {
				t.Errorf("expected no targets with only %v present, got %v", tt.files, targets)
			}
		})
	}
}

func TestResolveTargets_AntigravityExplicitSelection(t *testing.T) {
	// Explicit selection matrix (Codex H1): both the --target flag path and
	// the apm.yml target: path must activate antigravity, for the canonical
	// name and the agy alias alike. The flag path canonicalizes inside
	// SplitTargetFlag; the manifest path canonicalizes at apm.yml parse time
	// (parseTargetField -> ValidateTarget), so ResolveTargets sees canonical
	// tokens either way -- the manifest subtests parse a real apm.yml
	// snippet to prove the alias survives the full path (Codex H2).
	dir := t.TempDir()

	flagCases := []struct {
		name string
		flag string
	}{
		{"flag antigravity", "antigravity"},
		{"flag agy alias", "agy"},
	}
	for _, tt := range flagCases {
		t.Run(tt.name, func(t *testing.T) {
			targets, diags := ResolveTargets(tt.flag, nil, dir)
			if len(diags) != 0 {
				t.Errorf("expected no diagnostics, got %v", diags)
			}
			if len(targets) != 1 || targets[0] != "antigravity" {
				t.Errorf("expected [antigravity], got %v", targets)
			}
		})
	}

	manifestCases := []struct {
		name string
		yml  string
	}{
		{"manifest target antigravity", "name: p\nversion: \"1.0.0\"\ntarget: [antigravity]\n"},
		{"manifest target agy alias", "name: p\nversion: \"1.0.0\"\ntarget: [agy]\n"},
	}
	for _, tt := range manifestCases {
		t.Run(tt.name, func(t *testing.T) {
			node, err := yamlcore.SafeLoad([]byte(tt.yml))
			if err != nil {
				t.Fatal(err)
			}
			m, _, err := manifest.ParseManifest(node)
			if err != nil {
				t.Fatal(err)
			}
			targets, diags := ResolveTargets("", m.Target, dir)
			if len(diags) != 0 {
				t.Errorf("expected no diagnostics, got %v", diags)
			}
			if len(targets) != 1 || targets[0] != "antigravity" {
				t.Errorf("expected [antigravity], got %v", targets)
			}
		})
	}
}

func TestResolveTargets_UnsupportedTargetDiag(t *testing.T) {
	dir := t.TempDir()

	_, diags := ResolveTargets("gemini", nil, dir)
	if len(diags) != 1 || !strings.Contains(diags[0], "no registered handler") {
		t.Errorf("expected unsupported target diagnostic, got %v", diags)
	}
}

// TestResolveTargets_FlagCommaSplit is the F2 regression: `--target
// claude,codex` used to be treated as one literal (unknown) target string
// (targets := []string{flagTarget}, no split), silently resolving to zero
// targets. It must split into both claude and codex.
func TestResolveTargets_FlagCommaSplit(t *testing.T) {
	dir := t.TempDir()

	targets, diags := ResolveTargets("claude,codex", nil, dir)
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics, got %v", diags)
	}
	want := map[string]bool{"claude": true, "codex": true}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %v", targets)
	}
	for _, tgt := range targets {
		if !want[tgt] {
			t.Errorf("unexpected target %q, want one of claude/codex", tgt)
		}
	}
}

// TestResolveTargets_FlagCommaSplit_TrimsSpaces proves whitespace around
// comma-separated tokens is tolerated (" claude, codex ").
func TestResolveTargets_FlagCommaSplit_TrimsSpaces(t *testing.T) {
	dir := t.TempDir()

	targets, _ := ResolveTargets(" claude, codex ", nil, dir)
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %v", targets)
	}
}

// TestSplitTargetFlag_UnknownTokenRejected is the F2/mf-005 regression: a
// CLI --target token that is neither canonical, a known alias, nor an
// x-<vendor>-<name> extension used to silently resolve to zero targets
// (checkUnsupported found no adapter, diag-only, filterSupported dropped
// it) instead of being rejected. It must now be a hard error naming the
// offending token.
func TestSplitTargetFlag_UnknownTokenRejected(t *testing.T) {
	_, err := SplitTargetFlag("bogus")
	if err == nil {
		t.Fatal("expected an error for an unknown --target token, got nil")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error should name the offending token, got: %v", err)
	}
}

// TestSplitTargetFlag_KnownButAdapterlessAccepted proves req-tg-004's
// contract survives F2: cursor/gemini/windsurf are canonical (real)
// vocabulary without a registered adapter -- SplitTargetFlag (the
// validation layer) must accept them; ResolveTargets's checkUnsupported is
// what reports the separate non-fatal "no registered handler" diagnostic,
// not a hard rejection here.
func TestSplitTargetFlag_KnownButAdapterlessAccepted(t *testing.T) {
	for _, tgt := range []string{"cursor", "gemini", "windsurf"} {
		if _, err := SplitTargetFlag(tgt); err != nil {
			t.Errorf("SplitTargetFlag(%q) should be accepted (known vocabulary, no adapter), got error: %v", tgt, err)
		}
	}
}

// TestSplitTargetFlag_AllAndVendorTokensAccepted proves the two other
// accepted shapes besides plain canonical names: "all" and the
// x-<vendor>-<name> extension pattern (req-tg-004/req-mf-005).
func TestSplitTargetFlag_AllAndVendorTokensAccepted(t *testing.T) {
	for _, tgt := range []string{"all", "x-acme-tool"} {
		if _, err := SplitTargetFlag(tgt); err != nil {
			t.Errorf("SplitTargetFlag(%q) should be accepted, got error: %v", tgt, err)
		}
	}
}

// TestResolveTargets_CommaListWithUnknownToken proves an unknown token
// combined with a valid one in a comma list still fails closed as a
// diagnostic-only zero-target result from ResolveTargets (the CLI-level
// hard error is install.go's job, via deploy.SplitTargetFlag called
// up front -- ResolveTargets itself keeps its existing no-crash,
// diagnostics-only contract for every other caller, e.g. install --mcp).
func TestResolveTargets_CommaListWithUnknownToken(t *testing.T) {
	dir := t.TempDir()

	targets, diags := ResolveTargets("claude,bogus", nil, dir)
	if len(targets) != 0 {
		t.Errorf("expected zero targets when the flag contains an unknown token, got %v", targets)
	}
	if len(diags) != 1 || !strings.Contains(diags[0], "bogus") {
		t.Errorf("expected a diagnostic naming the unknown token, got %v", diags)
	}
}

func TestDeployClaude_OracleMatch(t *testing.T) {
	// Verify against oracle/targets/expected/claude.yaml:
	//   .claude/rules/demo.md
	//   .claude/agents/helper.md
	//   .claude/commands/hello.md
	//   .agents/skills/demo/SKILL.md
	// Plus a Go-specific addition beyond the Python oracle: claude also
	// copies skills to .claude/skills/<name>/ because Claude Code does not
	// discover skills from the cross-tool .agents/skills/ canonical path.
	dir := t.TempDir()

	// Create .apm/ structure matching oracle _input
	mkFile(t, dir, ".apm/instructions/demo.instructions.md", "# demo instructions")
	mkFile(t, dir, ".apm/agents/helper.agent.md", "helper agent")
	mkFile(t, dir, ".apm/commands/hello.md", "# hello command")
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill body")

	prims := CollectLocalPrimitives(dir)
	adapter := &claudeAdapter{}

	var deployed []string
	for _, p := range prims {
		if !adapterSupports(adapter, p.Type) {
			continue
		}
		files, err := adapter.DeployPrimitive(p, dir)
		if err != nil {
			t.Fatalf("deploy %s: %v", p.Name, err)
		}
		deployed = append(deployed, files...)
	}

	expected := oracleFileSet(loadOracle(t, "claude"))
	const extraSkillCopy = ".claude/skills/demo/SKILL.md"
	expected[extraSkillCopy] = true

	if len(deployed) != len(expected) {
		t.Fatalf("expected %d files, got %d: %v", len(expected), len(deployed), deployed)
	}
	for _, f := range deployed {
		if !expected[f] {
			t.Errorf("unexpected deployed file: %s", f)
		}
		// Verify file exists on disk
		abs := filepath.Join(dir, filepath.FromSlash(f))
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			t.Errorf("deployed file does not exist: %s", abs)
		}
	}
}

func TestDeployCopilot_OracleMatch(t *testing.T) {
	// oracle/targets/expected/copilot.yaml:
	//   .github/instructions/demo.instructions.md
	//   .github/agents/helper.agent.md
	//   .agents/skills/demo/SKILL.md
	dir := t.TempDir()

	mkFile(t, dir, ".apm/instructions/demo.instructions.md", "# demo")
	mkFile(t, dir, ".apm/agents/helper.agent.md", "helper")
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill")

	prims := CollectLocalPrimitives(dir)
	adapter := &copilotAdapter{}

	var deployed []string
	for _, p := range prims {
		if !adapterSupports(adapter, p.Type) {
			continue
		}
		files, err := adapter.DeployPrimitive(p, dir)
		if err != nil {
			t.Fatalf("deploy %s: %v", p.Name, err)
		}
		deployed = append(deployed, files...)
	}

	expected := oracleFileSet(loadOracle(t, "copilot"))

	if len(deployed) != len(expected) {
		t.Fatalf("expected %d, got %d: %v", len(expected), len(deployed), deployed)
	}
	for _, f := range deployed {
		if !expected[f] {
			t.Errorf("unexpected: %s", f)
		}
	}
}

func TestDeployCodex_OracleMatch(t *testing.T) {
	// oracle: .codex/agents/helper.toml, .agents/skills/demo/SKILL.md
	dir := t.TempDir()

	mkFile(t, dir, ".apm/agents/helper.agent.md", "helper")
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill")

	prims := CollectLocalPrimitives(dir)
	adapter := &codexAdapter{}

	var deployed []string
	for _, p := range prims {
		if !adapterSupports(adapter, p.Type) {
			continue
		}
		files, err := adapter.DeployPrimitive(p, dir)
		if err != nil {
			t.Fatalf("deploy: %v", err)
		}
		deployed = append(deployed, files...)
	}

	expected := oracleFileSet(loadOracle(t, "codex"))
	if len(deployed) != len(expected) {
		t.Fatalf("expected %d files, got %d: %v", len(expected), len(deployed), deployed)
	}
	for _, f := range deployed {
		if !expected[f] {
			t.Errorf("unexpected: %s", f)
		}
	}
}

func TestDeployAntigravity_OracleMatch(t *testing.T) {
	// oracle: .agents/rules/demo.md, .agents/skills/demo/SKILL.md (no agents per oracle)
	dir := t.TempDir()

	mkFile(t, dir, ".apm/instructions/demo.instructions.md", "# demo")
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill")

	prims := CollectLocalPrimitives(dir)
	adapter := &antigravityAdapter{}

	var deployed []string
	for _, p := range prims {
		if !adapterSupports(adapter, p.Type) {
			continue
		}
		files, err := adapter.DeployPrimitive(p, dir)
		if err != nil {
			t.Fatalf("deploy: %v", err)
		}
		deployed = append(deployed, files...)
	}

	expected := oracleFileSet(loadOracle(t, "antigravity"))
	if len(deployed) != len(expected) {
		t.Fatalf("expected %d files, got %d: %v", len(expected), len(deployed), deployed)
	}
	for _, f := range deployed {
		if !expected[f] {
			t.Errorf("unexpected: %s", f)
		}
	}
}

// TestDeployAntigravity_AgentsPerAgentDirectory locks the antigravity agents
// primitive mapping: .agents/agents/<name>/agent.md -- the static agent.md
// format Antigravity CLI >=1.0.16 scans (research/cli-subagents.md). Unlike
// claude's flat .claude/agents/<name>.md, each agent gets its own directory
// named after the agent, with the file inside always called agent.md. The
// content is a byte-copy of the source (no frontmatter transform), matching
// the adapter-wide convention. This mapping is an apm-go documented extension
// ahead of the Python upstream (prd.md decision 2026-07-10).
func TestDeployAntigravity_AgentsPerAgentDirectory(t *testing.T) {
	dir := t.TempDir()
	const src = "---\nname: reviewer\ndescription: Reviews diffs.\n---\nYou are a reviewer.\n"
	mkFile(t, dir, ".apm/agents/reviewer.agent.md", src)

	prims := CollectLocalPrimitives(dir)
	agent := findByType(prims, TypeAgents)
	if agent == nil {
		t.Fatal("no agents primitive collected")
	}

	adapter := &antigravityAdapter{}
	files, err := adapter.DeployPrimitive(*agent, dir)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}

	want := ".agents/agents/reviewer/agent.md"
	if len(files) != 1 || files[0] != want {
		t.Fatalf("expected [%s], got %v", want, files)
	}
	got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(want)))
	if err != nil {
		t.Fatalf("deployed file missing: %v", err)
	}
	if string(got) != src {
		t.Errorf("deployed content not byte-identical to source:\n got: %q\nwant: %q", got, src)
	}
}

// TestRun_AgentSameNameCollision_FirstDeclaredWins locks the same-name agent
// collision semantics for both per-name-path targets: two packages deploying
// an identically-named agent are resolved BEFORE any adapter runs, by
// ResolvePrimitives (conflict.go) -- same source class, first-declared wins
// (req-pr-003) with a "shadowed by" diagnostic; the loser never reaches
// DeployPrimitive, so exactly one write happens and the deployed file carries
// the first-declared dependency's bytes. antigravity's dependency agent path
// now lands inside the winning dependency's plugin bundle
// (.agents/plugins/<pkg>/agents/<name>/agent.md, task
// 07-11-antigravity-plugins-bundle) rather than the flat
// .agents/agents/<name>/agent.md local primitives still use; it must match
// claude's flat path (.claude/agents/<name>.md) collision semantics exactly.
func TestRun_AgentSameNameCollision_FirstDeclaredWins(t *testing.T) {
	tests := []struct {
		target string
		path   string
	}{
		{"claude", ".claude/agents/reviewer.md"},
		{"antigravity", ".agents/plugins/first/agents/reviewer/agent.md"},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			dir := t.TempDir()
			const firstContent = "first-declared agent body\n"
			const secondContent = "second-declared agent body\n"
			mkFile(t, filepath.Join(dir, "apm_modules", "acme", "first"),
				".apm/agents/reviewer.agent.md", firstContent)
			mkFile(t, filepath.Join(dir, "apm_modules", "acme", "second"),
				".apm/agents/reviewer.agent.md", secondContent)

			m := &manifest.Manifest{
				Name:    "test",
				Version: "1.0.0",
				ParsedDeps: []*manifest.DependencyReference{
					{RepoURL: "acme/first", Owner: "acme", Repo: "first", Source: "git"},
					{RepoURL: "acme/second", Owner: "acme", Repo: "second", Source: "git"},
				},
			}

			result, err := Run([]string{tt.target}, dir, m, nil, nil)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}

			got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(tt.path)))
			if err != nil {
				t.Fatalf("expected deployed agent at %s: %v", tt.path, err)
			}
			if string(got) != firstContent {
				t.Errorf("expected first-declared dependency's content at %s, got %q", tt.path, got)
			}

			// The deployed path is recorded against the FIRST dep only, so
			// lockfile provenance (deployed_files) attributes it to the winner.
			if dr := result.PerDep["acme/first"]; dr == nil || !slices.Contains(dr.Files, tt.path) {
				t.Errorf("expected %s recorded under acme/first, got %+v", tt.path, result.PerDep["acme/first"])
			}
			if dr := result.PerDep["acme/second"]; dr != nil && slices.Contains(dr.Files, tt.path) {
				t.Errorf("loser acme/second must not record %s, got %+v", tt.path, dr)
			}

			found := false
			for _, d := range result.Diags {
				if strings.Contains(d, `"reviewer"`) && strings.Contains(d, "first-declared wins") {
					found = true
				}
			}
			if !found {
				t.Errorf("expected first-declared-wins diagnostic for agent %q, got %v", "reviewer", result.Diags)
			}
		})
	}
}

func TestDeployOpenCode_OracleMatch(t *testing.T) {
	// oracle: .opencode/agents/helper.md, .opencode/commands/hello.md, .agents/skills/demo/SKILL.md
	dir := t.TempDir()

	mkFile(t, dir, ".apm/agents/helper.agent.md", "helper")
	mkFile(t, dir, ".apm/commands/hello.md", "# hello")
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill")

	prims := CollectLocalPrimitives(dir)
	adapter := &opencodeAdapter{}

	var deployed []string
	for _, p := range prims {
		if !adapterSupports(adapter, p.Type) {
			continue
		}
		files, err := adapter.DeployPrimitive(p, dir)
		if err != nil {
			t.Fatalf("deploy: %v", err)
		}
		deployed = append(deployed, files...)
	}

	expected := oracleFileSet(loadOracle(t, "opencode"))
	if len(deployed) != len(expected) {
		t.Fatalf("expected %d files, got %d: %v", len(expected), len(deployed), deployed)
	}
	for _, f := range deployed {
		if !expected[f] {
			t.Errorf("unexpected: %s", f)
		}
	}
}

func TestDeployAgentSkills_SkillsOnly(t *testing.T) {
	dir := t.TempDir()

	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill")
	mkFile(t, dir, ".apm/instructions/demo.instructions.md", "# demo")

	prims := CollectLocalPrimitives(dir)
	adapter := &agentSkillsAdapter{}

	var deployed []string
	for _, p := range prims {
		if !adapterSupports(adapter, p.Type) {
			continue
		}
		files, err := adapter.DeployPrimitive(p, dir)
		if err != nil {
			t.Fatalf("deploy: %v", err)
		}
		deployed = append(deployed, files...)
	}

	if len(deployed) != 1 {
		t.Fatalf("expected 1 (skills only), got %d: %v", len(deployed), deployed)
	}
	if deployed[0] != ".agents/skills/demo/SKILL.md" {
		t.Errorf("expected .agents/skills/demo/SKILL.md, got %s", deployed[0])
	}
}

func TestDeployRootConstraint(t *testing.T) {
	// req-tg-002: verify each adapter only writes under its registered roots
	adapters := map[string]TargetAdapter{
		"claude":       &claudeAdapter{},
		"codex":        &codexAdapter{},
		"copilot":      &copilotAdapter{},
		"antigravity":  &antigravityAdapter{},
		"opencode":     &opencodeAdapter{},
		"agent-skills": &agentSkillsAdapter{},
	}

	dir := t.TempDir()
	mkFile(t, dir, ".apm/instructions/demo.instructions.md", "demo")
	mkFile(t, dir, ".apm/agents/helper.agent.md", "helper")
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill")
	mkFile(t, dir, ".apm/commands/hello.md", "hello")

	prims := CollectLocalPrimitives(dir)

	for name, adapter := range adapters {
		roots := adapter.DeployRoots()

		for _, p := range prims {
			if !adapterSupports(adapter, p.Type) {
				continue
			}
			files, err := adapter.DeployPrimitive(p, dir)
			if err != nil {
				t.Fatalf("%s deploy %s: %v", name, p.Name, err)
			}
			for _, f := range files {
				validRoot := false
				for _, root := range roots {
					if strings.HasPrefix(f, root) {
						validRoot = true
						break
					}
				}
				if !validRoot {
					t.Errorf("req-tg-002: %s wrote %s outside registered roots %v", name, f, roots)
				}
			}
		}
	}
}

func TestSkillConvergence(t *testing.T) {
	// req-tg-003: all targets deploy skills to .agents/skills/<name>/SKILL.md.
	// claude additionally deploys to .claude/skills/<name>/SKILL.md because
	// Claude Code only discovers skills from .claude/skills, not
	// .agents/skills -- the canonical path alone is invisible to it.
	dir := t.TempDir()
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill body")

	prims := CollectLocalPrimitives(dir)
	skillPrim := findByType(prims, TypeSkills)
	if skillPrim == nil {
		t.Fatal("no skill primitive found")
	}

	allAdapters := []TargetAdapter{
		&claudeAdapter{},
		&codexAdapter{},
		&copilotAdapter{},
		&antigravityAdapter{},
		&opencodeAdapter{},
		&agentSkillsAdapter{},
	}

	for _, adapter := range allAdapters {
		if !adapterSupports(adapter, TypeSkills) {
			continue
		}
		files, err := adapter.DeployPrimitive(*skillPrim, dir)
		if err != nil {
			t.Fatalf("%s: %v", adapter.Name(), err)
		}

		if adapter.Name() == "claude" {
			want := map[string]bool{
				".agents/skills/demo/SKILL.md": true,
				".claude/skills/demo/SKILL.md": true,
			}
			if len(files) != len(want) {
				t.Errorf("claude should deploy %d files (canonical + Claude Code compat copy), got %v", len(want), files)
			}
			for _, f := range files {
				if !want[f] {
					t.Errorf("claude: unexpected deployed file %s", f)
				}
			}
			continue
		}

		if len(files) != 1 || files[0] != ".agents/skills/demo/SKILL.md" {
			t.Errorf("req-tg-003: %s should deploy to .agents/skills/demo/SKILL.md, got %v",
				adapter.Name(), files)
		}
	}
}

func TestDeploySkill_BundleWithSiblings(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill body")
	mkFile(t, dir, ".apm/skills/demo/scripts/run.sh", "#!/bin/sh\necho hi")
	mkFile(t, dir, ".apm/skills/demo/references/guide.md", "# guide")

	prims := CollectLocalPrimitives(dir)
	skillPrim := findByType(prims, TypeSkills)
	if skillPrim == nil {
		t.Fatal("no skill primitive")
	}

	files, err := deploySkill(*skillPrim, dir)
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]bool{
		".agents/skills/demo/SKILL.md":            true,
		".agents/skills/demo/scripts/run.sh":      true,
		".agents/skills/demo/references/guide.md": true,
	}
	if len(files) != len(expected) {
		t.Fatalf("expected %d files, got %d: %v", len(expected), len(files), files)
	}
	for _, f := range files {
		if !expected[f] {
			t.Errorf("unexpected: %s", f)
		}
	}
}

func TestRun_FullPipeline(t *testing.T) {
	dir := t.TempDir()

	// Local primitives
	mkFile(t, dir, ".apm/instructions/demo.instructions.md", "# demo instructions\n")
	mkFile(t, dir, ".apm/agents/helper.agent.md", "helper agent\n")
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill body\n")
	mkFile(t, dir, ".apm/commands/hello.md", "# hello\n")

	// Simulate a resolved dependency with a skill
	depKey := "acme/foo"
	modDir := filepath.Join(dir, "apm_modules", depKey)
	mkFile(t, modDir, ".apm/skills/extra/SKILL.md", "extra skill\n")

	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: depKey, Owner: "acme", Repo: "foo", Source: "git"},
		},
	}
	resolved := &resolver.ResolutionResult{
		Deps: []resolver.ResolvedDep{
			{Key: depKey, RepoURL: depKey, Kind: resolver.KindGitSemver, Depth: 1},
		},
	}

	result, err := Run([]string{"claude"}, dir, m, resolved, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Local primitives should be deployed
	localResult := result.PerDep[""]
	if localResult == nil {
		t.Fatal("expected local deploy result")
	}
	localExpected := map[string]bool{
		".claude/rules/demo.md":        true,
		".claude/agents/helper.md":     true,
		".claude/commands/hello.md":    true,
		".agents/skills/demo/SKILL.md": true,
		".claude/skills/demo/SKILL.md": true, // Claude Code compat copy (req-tg-003 note)
	}
	for _, f := range localResult.Files {
		if !localExpected[f] {
			t.Errorf("unexpected local file: %s", f)
		}
	}
	if len(localResult.Files) != len(localExpected) {
		t.Errorf("expected %d local files, got %d: %v", len(localExpected), len(localResult.Files), localResult.Files)
	}

	// Dep primitives should be deployed
	depResult := result.PerDep[depKey]
	if depResult == nil {
		t.Fatal("expected dep deploy result")
	}
	depExpected := map[string]bool{
		".agents/skills/extra/SKILL.md": true,
		".claude/skills/extra/SKILL.md": true, // Claude Code compat copy (req-tg-003 note)
	}
	if len(depResult.Files) != len(depExpected) {
		t.Errorf("expected %d dep files, got %d: %v", len(depExpected), len(depResult.Files), depResult.Files)
	}
	for _, f := range depResult.Files {
		if !depExpected[f] {
			t.Errorf("unexpected dep file: %s", f)
		}
	}

	// Hashes should be computed
	for _, f := range localResult.Files {
		if _, ok := localResult.Hashes[f]; !ok {
			t.Errorf("missing hash for %s", f)
		}
	}
}

func TestRun_ConflictResolution(t *testing.T) {
	dir := t.TempDir()

	// Local has a skill named "demo"
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "local version\n")

	// Dependency also has a skill named "demo"
	depKey := "acme/foo"
	modDir := filepath.Join(dir, "apm_modules", depKey)
	mkFile(t, modDir, ".apm/skills/demo/SKILL.md", "dep version\n")

	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: depKey, Owner: "acme", Repo: "foo", Source: "git"},
		},
	}
	resolved := &resolver.ResolutionResult{
		Deps: []resolver.ResolvedDep{
			{Key: depKey, RepoURL: depKey, Kind: resolver.KindGitSemver, Depth: 1},
		},
	}

	result, err := Run([]string{"claude"}, dir, m, resolved, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should have conflict diagnostic (req-pr-002)
	if len(result.Diags) == 0 {
		t.Error("expected conflict diagnostic")
	}

	// Local should win — verify content
	deployed := filepath.Join(dir, ".agents/skills/demo/SKILL.md")
	content, _ := os.ReadFile(deployed)
	if string(content) != "local version\n" {
		t.Errorf("expected local version, got %q", string(content))
	}
}

// TestRun_SkillFilterScopedToDepKey is a regression test: --skill used to be
// applied to every TypeSkills primitive project-wide regardless of source
// (bug), silently suppressing deployment of local skills and other
// already-declared dependencies' skills whenever any --skill filter was
// active. The filter must only affect the dependency (or dependencies) it
// was requested for.
func TestRun_SkillFilterScopedToDepKey(t *testing.T) {
	dir := t.TempDir()

	// Local skill, unrelated to the --skill flag below.
	mkFile(t, dir, ".apm/skills/loc/SKILL.md", "local\n")

	// depA is the --skill target: has two skills, only one selected.
	depA := "acme/foo"
	mkFile(t, filepath.Join(dir, "apm_modules", depA), ".apm/skills/a1/SKILL.md", "a1\n")
	mkFile(t, filepath.Join(dir, "apm_modules", depA), ".apm/skills/a2/SKILL.md", "a2\n")

	// depB is NOT the --skill target: its skill must be unaffected.
	depB := "acme/bar"
	mkFile(t, filepath.Join(dir, "apm_modules", depB), ".apm/skills/b1/SKILL.md", "b1\n")

	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: depA, Owner: "acme", Repo: "foo", Source: "git"},
			{RepoURL: depB, Owner: "acme", Repo: "bar", Source: "git"},
		},
	}
	resolved := &resolver.ResolutionResult{
		Deps: []resolver.ResolvedDep{
			{Key: depA, RepoURL: depA, Kind: resolver.KindGitSemver, Depth: 1},
			{Key: depB, RepoURL: depB, Kind: resolver.KindGitSemver, Depth: 1},
		},
	}

	_, err := Run([]string{"claude"}, dir, m, resolved, &SkillFilter{Subsets: map[string][]string{depA: {"a1"}}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".agents/skills/loc/SKILL.md")); err != nil {
		t.Errorf("local skill must be unaffected by --skill scoped to %s: %v", depA, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents/skills/b1/SKILL.md")); err != nil {
		t.Errorf("unrelated dependency %s's skill must be unaffected by --skill scoped to %s: %v", depB, depA, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents/skills/a1/SKILL.md")); err != nil {
		t.Errorf("selected skill a1 (in %s) should deploy: %v", depA, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents/skills/a2/SKILL.md")); err == nil {
		t.Errorf("unselected skill a2 (in %s, the --skill target) should not deploy", depA)
	}
}

// TestRun_SkillFilterAbsentKeyDeploysAll is a regression test for the
// documented `--skill '*'` RESET sentinel (install.md: "--skill '*' resets
// to install all skills"): the RESET semantics now live entirely above
// deploy.Run (cmd/apm-go's effectiveSkillSubsets deletes the dependency's
// entry from the Subsets map on a wildcard), so SkillFilter's own contract
// is simply "a dependency absent from Subsets deploys every skill" (H6: "no
// entry" is the ONLY representation of "deploy all" -- a value is never an
// empty slice). This covers both a SkillFilter with no Subsets at all and
// one where OTHER dependencies are scoped but depA is not, proving the
// omission -- not a special-cased wildcard value -- is what deploys
// everything for depA.
func TestRun_SkillFilterAbsentKeyDeploysAll(t *testing.T) {
	tests := []struct {
		name    string
		subsets map[string][]string
	}{
		{"nil Subsets", nil},
		{"empty Subsets", map[string][]string{}},
		{"other dependency scoped, depA absent", map[string][]string{"acme/other": {"x"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			depA := "acme/foo"
			mkFile(t, filepath.Join(dir, "apm_modules", depA), ".apm/skills/a1/SKILL.md", "a1\n")
			mkFile(t, filepath.Join(dir, "apm_modules", depA), ".apm/skills/a2/SKILL.md", "a2\n")

			m := &manifest.Manifest{
				Name:    "test",
				Version: "1.0.0",
				ParsedDeps: []*manifest.DependencyReference{
					{RepoURL: depA, Owner: "acme", Repo: "foo", Source: "git"},
				},
			}
			resolved := &resolver.ResolutionResult{
				Deps: []resolver.ResolvedDep{
					{Key: depA, RepoURL: depA, Kind: resolver.KindGitSemver, Depth: 1},
				},
			}

			_, err := Run([]string{"claude"}, dir, m, resolved, &SkillFilter{Subsets: tt.subsets})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}

			if _, err := os.Stat(filepath.Join(dir, ".agents/skills/a1/SKILL.md")); err != nil {
				t.Errorf("a1 should deploy when depA is absent from Subsets (%v): %v", tt.subsets, err)
			}
			if _, err := os.Stat(filepath.Join(dir, ".agents/skills/a2/SKILL.md")); err != nil {
				t.Errorf("a2 should also deploy when depA is absent from Subsets (%v): %v", tt.subsets, err)
			}
		})
	}
}

func TestRun_SkillDeduplication(t *testing.T) {
	// When multiple targets active, same skill should only be deployed once
	dir := t.TempDir()
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill\n")

	m := &manifest.Manifest{Name: "test", Version: "1.0.0"}

	result, err := Run([]string{"claude", "codex", "copilot"}, dir, m, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// .agents/skills/demo/SKILL.md should appear only once in local results
	localResult := result.PerDep[""]
	if localResult == nil {
		t.Fatal("expected local result")
	}
	count := 0
	for _, f := range localResult.Files {
		if f == ".agents/skills/demo/SKILL.md" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("skill should be deployed once, found %d times", count)
	}
}

// TestRun_SkillDeduplication_ClaudeExtraCopySurvivesTargetOrder is a
// regression test for a bug where the cross-target skill dedup in Run
// skipped calling claudeAdapter.DeployPrimitive entirely whenever another
// skill-supporting target (e.g. codex) had already deployed the canonical
// .agents/skills/<name>/ path first. That `continue` meant claude's
// target-specific .claude/skills/<name>/ copy (needed because Claude Code
// does not discover skills from .agents/skills) was silently dropped
// whenever claude wasn't the first skill-supporting target to run.
func TestRun_SkillDeduplication_ClaudeExtraCopySurvivesTargetOrder(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill\n")

	m := &manifest.Manifest{Name: "test", Version: "1.0.0"}

	// codex runs before claude -- this ordering used to trigger the bug.
	result, err := Run([]string{"codex", "claude"}, dir, m, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	localResult := result.PerDep[""]
	if localResult == nil {
		t.Fatal("expected local result")
	}

	counts := map[string]int{}
	for _, f := range localResult.Files {
		counts[f]++
	}

	if counts[".agents/skills/demo/SKILL.md"] != 1 {
		t.Errorf(".agents/skills/demo/SKILL.md should appear exactly once, got %d (files: %v)",
			counts[".agents/skills/demo/SKILL.md"], localResult.Files)
	}
	if counts[".claude/skills/demo/SKILL.md"] != 1 {
		t.Errorf(".claude/skills/demo/SKILL.md should appear exactly once even when codex "+
			"deploys first, got %d (files: %v)", counts[".claude/skills/demo/SKILL.md"], localResult.Files)
	}

	if _, err := os.Stat(filepath.Join(dir, ".claude/skills/demo/SKILL.md")); err != nil {
		t.Errorf("expected .claude/skills/demo/SKILL.md on disk: %v", err)
	}
}

// TestDeploySkillClaude_BundleWithSiblings verifies the .claude/skills/ extra
// copy carries the whole skill bundle (not just SKILL.md) -- a skill with
// scripts/ or references/ siblings must not be truncated in the Claude Code
// compat copy.
func TestDeploySkillClaude_BundleWithSiblings(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill body")
	mkFile(t, dir, ".apm/skills/demo/scripts/run.sh", "#!/bin/sh\necho hi")
	mkFile(t, dir, ".apm/skills/demo/references/guide.md", "# guide")

	prims := CollectLocalPrimitives(dir)
	skillPrim := findByType(prims, TypeSkills)
	if skillPrim == nil {
		t.Fatal("no skill primitive")
	}

	adapter := &claudeAdapter{}
	files, err := adapter.DeployPrimitive(*skillPrim, dir)
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]bool{
		".agents/skills/demo/SKILL.md":            true,
		".agents/skills/demo/scripts/run.sh":      true,
		".agents/skills/demo/references/guide.md": true,
		".claude/skills/demo/SKILL.md":            true,
		".claude/skills/demo/scripts/run.sh":      true,
		".claude/skills/demo/references/guide.md": true,
	}
	if len(files) != len(expected) {
		t.Fatalf("expected %d files, got %d: %v", len(expected), len(files), files)
	}
	for _, f := range files {
		if !expected[f] {
			t.Errorf("unexpected: %s", f)
		}
		abs := filepath.Join(dir, filepath.FromSlash(f))
		content, err := os.ReadFile(abs)
		if err != nil {
			t.Errorf("deployed file does not exist: %s: %v", abs, err)
			continue
		}
		if len(content) == 0 {
			t.Errorf("deployed file is empty: %s", abs)
		}
	}
}

func TestRun_NoTargets(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill\n")

	m := &manifest.Manifest{Name: "test", Version: "1.0.0"}

	result, err := Run(nil, dir, m, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// No targets → nothing deployed
	if len(result.PerDep) != 0 {
		t.Errorf("expected empty, got %v", result.PerDep)
	}
}

func TestRun_DeployedFilesKeyMatch(t *testing.T) {
	// Acceptance #8: DeployedFiles/DeployedHashes populated per-entry in lockfile.
	// Verifies that deploy.Run's PerDep keys match lockfile.LockedDep.UniqueKey(),
	// including the virtual-path case where key divergence would silently break.
	dir := t.TempDir()

	// Local primitives
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "local skill\n")

	// Direct dep (plain key, no virtual path)
	depKeyPlain := "acme/foo"
	mkFile(t, filepath.Join(dir, "apm_modules", depKeyPlain),
		".apm/skills/extra/SKILL.md", "dep skill\n")

	// Direct dep with virtual path
	vpRepoURL := "org/monorepo"
	vpVirtPath := "packages/bar"
	vpKey := vpRepoURL + "/" + vpVirtPath
	mkFile(t, filepath.Join(dir, "apm_modules", vpKey),
		".apm/agents/helper.agent.md", "vp agent\n")

	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: depKeyPlain, Owner: "acme", Repo: "foo", Source: "git"},
			{RepoURL: vpRepoURL, VirtualPath: vpVirtPath, Owner: "org", Repo: "monorepo", Source: "git"},
		},
	}
	resolved := &resolver.ResolutionResult{
		Deps: []resolver.ResolvedDep{
			{Key: depKeyPlain, RepoURL: depKeyPlain, Kind: resolver.KindGitSemver, Depth: 1},
			{Key: vpKey, RepoURL: vpRepoURL, VirtualPath: vpVirtPath, Kind: resolver.KindGitSemver, Depth: 1},
		},
	}

	result, err := Run([]string{"claude"}, dir, m, resolved, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Simulate install.go's lockfile population loop: build LockedDep, call UniqueKey(), look up PerDep.
	lockDeps := []struct {
		repoURL     string
		virtualPath string
	}{
		{depKeyPlain, ""},
		{vpRepoURL, vpVirtPath},
	}

	for _, ld := range lockDeps {
		key := ld.repoURL
		if ld.virtualPath != "" {
			key = ld.repoURL + "/" + ld.virtualPath
		}
		dr, ok := result.PerDep[key]
		if !ok {
			t.Errorf("PerDep missing key %q (simulated UniqueKey)", key)
			continue
		}
		if len(dr.Files) == 0 {
			t.Errorf("key %q: DeployedFiles is empty", key)
		}
		if len(dr.Hashes) == 0 {
			t.Errorf("key %q: DeployedHashes is empty", key)
		}
	}

	// Also verify local entry
	localDR, ok := result.PerDep[""]
	if !ok {
		t.Fatal("PerDep missing local entry (key=\"\")")
	}
	if len(localDR.Files) == 0 {
		t.Error("local DeployedFiles is empty")
	}
	if len(localDR.Hashes) == 0 {
		t.Error("local DeployedHashes is empty")
	}
}

func findByType(prims []Primitive, pt PrimitiveType) *Primitive {
	for i := range prims {
		if prims[i].Type == pt {
			return &prims[i]
		}
	}
	return nil
}

// --- Phase 4-T: not_deployed negative tests ---

func TestRun_MultipleHooksOverwriteDiagnostic(t *testing.T) {
	// S-003: two hook files collapse to .agents/hooks.json -> warn
	dir := t.TempDir()
	mkFile(t, dir, ".apm/hooks/pre.json", `{"event":"pre"}`)
	mkFile(t, dir, ".apm/hooks/post.json", `{"event":"post"}`)

	m := &manifest.Manifest{Name: "test", Version: "1.0.0"}

	result, err := Run([]string{"antigravity"}, dir, m, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	found := false
	for _, d := range result.Diags {
		if strings.Contains(d, "overwrites") && strings.Contains(d, "hooks.json") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected overwrite diagnostic for multiple hooks, got %v", result.Diags)
	}
}

func TestNotDeployed_PerTarget(t *testing.T) {
	dir := t.TempDir()
	// Create ALL primitive types
	mkFile(t, dir, ".apm/instructions/demo.instructions.md", "inst")
	mkFile(t, dir, ".apm/agents/helper.agent.md", "agent")
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill")
	mkFile(t, dir, ".apm/commands/hello.md", "cmd")
	mkFile(t, dir, ".apm/hooks/pre.json", `{"event":"pre"}`)
	mkFile(t, dir, ".apm/prompts/ask.prompt.md", "prompt")

	prims := CollectLocalPrimitives(dir)

	tests := []struct {
		adapter       TargetAdapter
		mustDeploy    []PrimitiveType
		mustNotDeploy []PrimitiveType
	}{
		{&claudeAdapter{}, []PrimitiveType{TypeInstructions, TypeAgents, TypeSkills, TypeCommands}, []PrimitiveType{TypePrompts, TypeHooks}},
		{&codexAdapter{}, []PrimitiveType{TypeAgents, TypeSkills, TypeHooks}, []PrimitiveType{TypeInstructions, TypePrompts, TypeCommands}},
		{&copilotAdapter{}, []PrimitiveType{TypeInstructions, TypePrompts, TypeAgents, TypeSkills, TypeHooks}, []PrimitiveType{TypeCommands}},
		{&antigravityAdapter{}, []PrimitiveType{TypeInstructions, TypeSkills, TypeHooks, TypeAgents}, []PrimitiveType{TypeCommands, TypePrompts}},
		{&opencodeAdapter{}, []PrimitiveType{TypeAgents, TypeCommands, TypeSkills}, []PrimitiveType{TypeInstructions, TypeHooks, TypePrompts}},
		{&agentSkillsAdapter{}, []PrimitiveType{TypeSkills}, []PrimitiveType{TypeInstructions, TypePrompts, TypeAgents, TypeCommands, TypeHooks}},
	}

	// Per-primitive unique names so we can detect them in deploy paths.
	nameByType := map[PrimitiveType]string{
		TypeInstructions: "demo",
		TypeAgents:       "helper",
		TypeSkills:       "demo",
		TypeCommands:     "hello",
		TypeHooks:        "pre",
		TypePrompts:      "ask",
	}

	for _, tt := range tests {
		t.Run(tt.adapter.Name(), func(t *testing.T) {
			tdir := t.TempDir()
			deployedByType := map[PrimitiveType][]string{}
			var deployed []string
			for _, p := range prims {
				if !adapterSupports(tt.adapter, p.Type) {
					continue
				}
				files, err := tt.adapter.DeployPrimitive(p, tdir)
				if err != nil {
					t.Fatalf("deploy %s: %v", p.Name, err)
				}
				deployedByType[p.Type] = append(deployedByType[p.Type], files...)
				deployed = append(deployed, files...)
			}

			// must-deploy: actually produced output files on disk
			for _, pt := range tt.mustDeploy {
				files := deployedByType[pt]
				if len(files) == 0 {
					t.Errorf("%s: %s must deploy at least one file, got none", tt.adapter.Name(), pt)
					continue
				}
				for _, f := range files {
					abs := filepath.Join(tdir, filepath.FromSlash(f))
					if _, err := os.Stat(abs); os.IsNotExist(err) {
						t.Errorf("%s: deployed file does not exist on disk: %s", tt.adapter.Name(), abs)
					}
				}
			}

			// must-not-deploy: no deployed path references the unsupported primitive's name
			for _, pt := range tt.mustNotDeploy {
				if adapterSupports(tt.adapter, pt) {
					t.Errorf("%s should NOT support %s", tt.adapter.Name(), pt)
				}
				marker := nameByType[pt]
				for _, f := range deployed {
					// skills and instructions share the name "demo"; skip cross-type false positives
					if pt == TypeInstructions && strings.Contains(f, "/skills/") {
						continue
					}
					if marker != "" && strings.Contains(f, "/"+marker+".") {
						t.Errorf("%s: must-not-deploy %s leaked into %s", tt.adapter.Name(), pt, f)
					}
				}
			}
		})
	}
}

func TestUnsupportedTarget_CursorWindsurf(t *testing.T) {
	dir := t.TempDir()
	for _, target := range []string{"cursor", "windsurf"} {
		t.Run(target, func(t *testing.T) {
			_, diags := ResolveTargets(target, nil, dir)
			found := false
			for _, d := range diags {
				if strings.Contains(d, "no registered handler") {
					found = true
				}
			}
			if !found {
				t.Errorf("--%s should emit 'no registered handler' diagnostic, got %v", target, diags)
			}
		})
	}
}

func TestCopilotPrompts(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/prompts/ask.prompt.md", "# ask prompt")

	prims := CollectLocalPrimitives(dir)
	adapter := &copilotAdapter{}

	tdir := t.TempDir() // deploy target separate from source
	var deployed []string
	for _, p := range prims {
		if !adapterSupports(adapter, p.Type) {
			continue
		}
		files, err := adapter.DeployPrimitive(p, tdir)
		if err != nil {
			t.Fatal(err)
		}
		deployed = append(deployed, files...)
	}

	if len(deployed) != 1 || deployed[0] != ".github/prompts/ask.prompt.md" {
		t.Errorf("expected [.github/prompts/ask.prompt.md], got %v", deployed)
	}
}

func TestCopilotHooksDeploy(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/hooks/pre.json", `{"event":"pre"}`)

	prims := CollectLocalPrimitives(dir)
	adapter := &copilotAdapter{}

	tdir := t.TempDir()
	hook := findByType(prims, TypeHooks)
	if hook == nil {
		t.Fatal("no hook primitive")
	}
	files, err := adapter.DeployPrimitive(*hook, tdir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != ".github/hooks/pre.json" {
		t.Errorf("expected [.github/hooks/pre.json], got %v", files)
	}
}

func TestCodexHooksDeploy(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/hooks/pre.json", `{"event":"pre"}`)

	prims := CollectLocalPrimitives(dir)
	adapter := &codexAdapter{}

	tdir := t.TempDir()
	hook := findByType(prims, TypeHooks)
	if hook == nil {
		t.Fatal("no hook primitive")
	}
	files, err := adapter.DeployPrimitive(*hook, tdir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != ".codex/hooks.json" {
		t.Errorf("expected [.codex/hooks.json], got %v", files)
	}
}

func TestAntigravityHooksDeploy(t *testing.T) {
	dir := t.TempDir()
	const hookContent = `{"event":"PreToolUse"}`
	mkFile(t, dir, ".apm/hooks/pre.json", hookContent)

	prims := CollectLocalPrimitives(dir)
	adapter := &antigravityAdapter{}

	tdir := t.TempDir()
	var deployed []string
	for _, p := range prims {
		if !adapterSupports(adapter, p.Type) {
			continue
		}
		files, err := adapter.DeployPrimitive(p, tdir)
		if err != nil {
			t.Fatal(err)
		}
		deployed = append(deployed, files...)
	}

	found := false
	for _, f := range deployed {
		if f == ".agents/hooks.json" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected .agents/hooks.json in deployed, got %v", deployed)
	}

	// Verify on-disk content matches source (S-004)
	got, err := os.ReadFile(filepath.Join(tdir, ".agents", "hooks.json"))
	if err != nil {
		t.Fatalf("read deployed hooks.json: %v", err)
	}
	if string(got) != hookContent {
		t.Errorf("hooks.json content = %q, want %q", string(got), hookContent)
	}
}
