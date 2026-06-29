package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectLocalPrimitives(t *testing.T) {
	dir := t.TempDir()

	// Create .apm/ structure matching oracle input
	mkFile(t, dir, ".apm/instructions/demo.instructions.md", "# demo")
	mkFile(t, dir, ".apm/agents/helper.agent.md", "helper agent")
	mkFile(t, dir, ".apm/skills/demo/SKILL.md", "skill body")
	mkFile(t, dir, ".apm/commands/hello.md", "# hello")

	prims := CollectLocalPrimitives(dir)
	if len(prims) != 4 {
		t.Fatalf("expected 4 primitives, got %d", len(prims))
	}

	byType := groupByType(prims)

	// req-pr-001: source attribution
	for _, p := range prims {
		if p.Source != "local" {
			t.Errorf("expected source 'local', got %q", p.Source)
		}
	}

	if inst := byType[TypeInstructions]; len(inst) != 1 || inst[0].Name != "demo" {
		t.Errorf("instructions: expected [demo], got %v", inst)
	}
	if ag := byType[TypeAgents]; len(ag) != 1 || ag[0].Name != "helper" {
		t.Errorf("agents: expected [helper], got %v", ag)
	}
	if sk := byType[TypeSkills]; len(sk) != 1 || sk[0].Name != "demo" {
		t.Errorf("skills: expected [demo], got %v", sk)
	}
	if cmd := byType[TypeCommands]; len(cmd) != 1 || cmd[0].Name != "hello" {
		t.Errorf("commands: expected [hello], got %v", cmd)
	}
}

func TestCollectDependencyPrimitives(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/foo")

	mkFile(t, modDir, ".apm/skills/bar/SKILL.md", "bar skill")
	mkFile(t, modDir, ".apm/instructions/setup.md", "setup")

	prims := CollectDependencyPrimitives("acme/foo", modDir)

	if len(prims) != 2 {
		t.Fatalf("expected 2 primitives, got %d", len(prims))
	}

	for _, p := range prims {
		if p.Source != "dependency:acme/foo" {
			t.Errorf("expected source 'dependency:acme/foo', got %q", p.Source)
		}
		if p.DepKey != "acme/foo" {
			t.Errorf("expected depKey 'acme/foo', got %q", p.DepKey)
		}
	}
}

func TestCollectDependencyPrimitives_SkillBundle(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/skill-pkg")

	// Skill bundle: SKILL.md at root
	mkFile(t, modDir, "SKILL.md", "root skill")

	prims := CollectDependencyPrimitives("acme/skill-pkg", modDir)

	if len(prims) != 1 {
		t.Fatalf("expected 1, got %d", len(prims))
	}
	if prims[0].Type != TypeSkills {
		t.Errorf("expected skills type")
	}
	if prims[0].Name != "skill-pkg" {
		t.Errorf("expected name 'skill-pkg', got %q", prims[0].Name)
	}
}

func TestCollectDependencyPrimitives_SkillCollection(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/multi")

	// Skill collection: skills/<name>/SKILL.md
	mkFile(t, modDir, "skills/alpha/SKILL.md", "alpha")
	mkFile(t, modDir, "skills/beta/SKILL.md", "beta")

	prims := CollectDependencyPrimitives("acme/multi", modDir)

	if len(prims) != 2 {
		t.Fatalf("expected 2, got %d", len(prims))
	}
	names := map[string]bool{}
	for _, p := range prims {
		names[p.Name] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("expected alpha and beta, got %v", names)
	}
}

func TestExtractInstructionName(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"demo.instructions.md", "demo"},
		{"setup.md", "setup"},
		{"readme.txt", ""},
	}
	for _, tt := range tests {
		got := extractInstructionName(tt.input)
		if got != tt.expected {
			t.Errorf("extractInstructionName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestExtractAgentName(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"helper.agent.md", "helper"},
		{"assistant.md", "assistant"},
		{"data.json", ""},
	}
	for _, tt := range tests {
		got := extractAgentName(tt.input)
		if got != tt.expected {
			t.Errorf("extractAgentName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func mkFile(t *testing.T, base, rel, content string) {
	t.Helper()
	p := filepath.Join(base, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(p), 0755)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func groupByType(prims []Primitive) map[PrimitiveType][]Primitive {
	m := make(map[PrimitiveType][]Primitive)
	for _, p := range prims {
		m[p.Type] = append(m[p.Type], p)
	}
	return m
}
