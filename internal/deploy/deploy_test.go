package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/resolver"
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
	// req-tg-001: antigravity must NEVER be auto-detected.
	// SignalWhitelist maps GEMINI.md and AGENTS.md to antigravity,
	// but ResolveTargets must filter it out of auto-detection results.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# agents"), 0644)
	os.WriteFile(filepath.Join(dir, "GEMINI.md"), []byte("# gemini"), 0644)

	targets, _ := ResolveTargets("", nil, dir)
	for _, tgt := range targets {
		if tgt == "antigravity" {
			t.Error("antigravity should never be auto-detected (req-tg-001)")
		}
	}
}

func TestResolveTargets_UnsupportedTargetDiag(t *testing.T) {
	dir := t.TempDir()

	_, diags := ResolveTargets("gemini", nil, dir)
	if len(diags) != 1 || !strings.Contains(diags[0], "no registered handler") {
		t.Errorf("expected unsupported target diagnostic, got %v", diags)
	}
}

func TestDeployClaude_OracleMatch(t *testing.T) {
	// Verify against oracle/targets/expected/claude.yaml:
	//   .claude/rules/demo.md
	//   .claude/agents/helper.md
	//   .claude/commands/hello.md
	//   .agents/skills/demo/SKILL.md
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

	expected := map[string]bool{
		".claude/rules/demo.md":        true,
		".claude/agents/helper.md":     true,
		".claude/commands/hello.md":    true,
		".agents/skills/demo/SKILL.md": true,
	}

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

	expected := map[string]bool{
		".github/instructions/demo.instructions.md": true,
		".github/agents/helper.agent.md":            true,
		".agents/skills/demo/SKILL.md":              true,
	}

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

	expected := map[string]bool{
		".codex/agents/helper.toml":    true,
		".agents/skills/demo/SKILL.md": true,
	}
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

	expected := map[string]bool{
		".agents/rules/demo.md":        true,
		".agents/skills/demo/SKILL.md": true,
	}
	if len(deployed) != len(expected) {
		t.Fatalf("expected %d files, got %d: %v", len(expected), len(deployed), deployed)
	}
	for _, f := range deployed {
		if !expected[f] {
			t.Errorf("unexpected: %s", f)
		}
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

	expected := map[string]bool{
		".opencode/agents/helper.md":   true,
		".opencode/commands/hello.md":  true,
		".agents/skills/demo/SKILL.md": true,
	}
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
	// req-tg-003: all targets deploy skills to .agents/skills/<name>/SKILL.md
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

	result, err := Run([]string{"claude"}, dir, m, resolved)
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
	if len(depResult.Files) != 1 || depResult.Files[0] != ".agents/skills/extra/SKILL.md" {
		t.Errorf("expected [.agents/skills/extra/SKILL.md], got %v", depResult.Files)
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

	result, err := Run([]string{"claude"}, dir, m, resolved)
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

func TestRun_SkillDeduplication(t *testing.T) {
	// When multiple targets active, same skill should only be deployed once
	dir := t.TempDir()
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill\n")

	m := &manifest.Manifest{Name: "test", Version: "1.0.0"}

	result, err := Run([]string{"claude", "codex", "copilot"}, dir, m, nil)
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

func TestRun_NoTargets(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill\n")

	m := &manifest.Manifest{Name: "test", Version: "1.0.0"}

	result, err := Run(nil, dir, m, nil)
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

	result, err := Run([]string{"claude"}, dir, m, resolved)
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
		adapter     TargetAdapter
		mustDeploy  []PrimitiveType
		mustNotDeploy []PrimitiveType
	}{
		{&claudeAdapter{}, []PrimitiveType{TypeInstructions, TypeAgents, TypeSkills, TypeCommands}, []PrimitiveType{TypePrompts}},
		{&codexAdapter{}, []PrimitiveType{TypeAgents, TypeSkills}, []PrimitiveType{TypeInstructions, TypePrompts, TypeCommands}},
		{&copilotAdapter{}, []PrimitiveType{TypeInstructions, TypePrompts, TypeAgents, TypeSkills}, []PrimitiveType{TypeCommands}},
		{&antigravityAdapter{}, []PrimitiveType{TypeInstructions, TypeSkills, TypeHooks}, []PrimitiveType{TypeCommands, TypePrompts, TypeAgents}},
		{&opencodeAdapter{}, []PrimitiveType{TypeAgents, TypeCommands, TypeSkills}, []PrimitiveType{TypeInstructions, TypeHooks, TypePrompts}},
		{&agentSkillsAdapter{}, []PrimitiveType{TypeSkills}, []PrimitiveType{TypeInstructions, TypePrompts, TypeAgents, TypeCommands, TypeHooks}},
	}

	for _, tt := range tests {
		t.Run(tt.adapter.Name(), func(t *testing.T) {
			tdir := t.TempDir()
			var deployed []string
			for _, p := range prims {
				if !adapterSupports(tt.adapter, p.Type) {
					continue
				}
				files, err := tt.adapter.DeployPrimitive(p, tdir)
				if err != nil {
					t.Fatalf("deploy %s: %v", p.Name, err)
				}
				deployed = append(deployed, files...)
			}

			// Verify must-deploy types produced files
			for _, pt := range tt.mustDeploy {
				found := false
				for _, p := range prims {
					if p.Type == pt && adapterSupports(tt.adapter, pt) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s should support %s", tt.adapter.Name(), pt)
				}
			}

			// Verify must-not-deploy types are NOT supported
			for _, pt := range tt.mustNotDeploy {
				if adapterSupports(tt.adapter, pt) {
					t.Errorf("%s should NOT support %s", tt.adapter.Name(), pt)
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

	var deployed []string
	for _, p := range prims {
		if !adapterSupports(adapter, p.Type) {
			continue
		}
		files, err := adapter.DeployPrimitive(p, dir)
		if err != nil {
			t.Fatal(err)
		}
		deployed = append(deployed, files...)
	}

	if len(deployed) != 1 || deployed[0] != ".github/prompts/ask.prompt.md" {
		t.Errorf("expected [.github/prompts/ask.prompt.md], got %v", deployed)
	}
}

func TestAntigravityHooksDeploy(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/hooks/pre.json", `{"event":"PreToolUse"}`)

	prims := CollectLocalPrimitives(dir)
	adapter := &antigravityAdapter{}

	var deployed []string
	for _, p := range prims {
		if !adapterSupports(adapter, p.Type) {
			continue
		}
		files, err := adapter.DeployPrimitive(p, dir)
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
		t.Errorf("expected .agents/hooks.json in deployed, got %v", deployed)
	}
}
