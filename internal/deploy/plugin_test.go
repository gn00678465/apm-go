package deploy

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// mattpocockSkillLeafNames mirrors the 20 skill leaf names apm install
// mattpocock/skills must deploy (parity with the Python oracle), spread
// across two nested category directories the way the real plugin.json does
// (e.g. "./skills/engineering/code-review", "./skills/productivity/grill-me").
var mattpocockSkillLeafNames = []string{
	"ask-matt", "code-review", "codebase-design", "diagnosing-bugs",
	"domain-modeling", "grill-me", "grill-with-docs", "grilling", "handoff",
	"implement", "improve-codebase-architecture", "prototype", "research",
	"setup-matt-pocock-skills", "tdd", "teach", "to-issues", "to-prd",
	"triage", "writing-great-skills",
}

func writePluginJSON(t *testing.T, modDir, body string) {
	t.Helper()
	pluginDir := filepath.Join(modDir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCollectDependencyPrimitives_PluginJSON_NestedSkills(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "mattpocock/skills")

	skillsArray := `["./skills/engineering/code-review", "./skills/productivity/grill-me"]`
	writePluginJSON(t, modDir, `{"name": "skills", "skills": `+skillsArray+`}`)

	mkFile(t, modDir, "skills/engineering/code-review/SKILL.md", "code review skill")
	mkFile(t, modDir, "skills/productivity/grill-me/SKILL.md", "grill me skill")

	prims := CollectDependencyPrimitives("mattpocock/skills", modDir)

	skills := onlyType(prims, TypeSkills)
	if len(skills) != 2 {
		t.Fatalf("expected 2 skill primitives, got %d: %+v", len(skills), skills)
	}

	names := namesOf(skills)
	sort.Strings(names)
	want := []string{"code-review", "grill-me"}
	if !equalStrings(names, want) {
		t.Errorf("expected leaf names %v, got %v", want, names)
	}

	for _, p := range skills {
		if p.Source != "dependency:mattpocock/skills" {
			t.Errorf("expected source 'dependency:mattpocock/skills', got %q", p.Source)
		}
		if p.DepKey != "mattpocock/skills" {
			t.Errorf("expected depKey 'mattpocock/skills', got %q", p.DepKey)
		}
		if !filepath.IsAbs(p.SrcPath) {
			t.Errorf("expected absolute SrcPath, got %q", p.SrcPath)
		}
	}
}

// TestCollectDependencyPrimitives_PluginJSON_MattpocockShape reproduces the
// empirical parity target: mattpocock/skills' plugin.json declares exactly
// 20 skill directories nested two levels deep (skills/<category>/<name>),
// which the legacy single-level skills/<name>/SKILL.md scan finds nothing
// in. CollectDependencyPrimitives must emit exactly these 20 skills by leaf
// name, order-independent.
func TestCollectDependencyPrimitives_PluginJSON_MattpocockShape(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "mattpocock/skills")

	categories := []string{"engineering", "productivity"}
	var declared []string
	for i, name := range mattpocockSkillLeafNames {
		cat := categories[i%len(categories)]
		rel := "./skills/" + cat + "/" + name
		declared = append(declared, `"`+rel+`"`)
		mkFile(t, modDir, "skills/"+cat+"/"+name+"/SKILL.md", "# "+name)
	}

	manifest := `{"name": "skills", "skills": [` + joinQuoted(declared) + `]}`
	writePluginJSON(t, modDir, manifest)

	prims := CollectDependencyPrimitives("mattpocock/skills", modDir)
	skills := onlyType(prims, TypeSkills)
	if len(skills) != len(mattpocockSkillLeafNames) {
		t.Fatalf("expected %d skill primitives, got %d: %+v", len(mattpocockSkillLeafNames), len(skills), skills)
	}

	got := namesOf(skills)
	sort.Strings(got)
	want := append([]string(nil), mattpocockSkillLeafNames...)
	sort.Strings(want)
	if !equalStrings(got, want) {
		t.Errorf("leaf names mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestCollectDependencyPrimitives_PluginJSON_EscapingSkillPathSkipped(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/evil")

	// A sibling directory outside modDir that a malicious plugin.json tries
	// to reach via "..".
	outside := filepath.Join(dir, "apm_modules", "acme")
	mkFile(t, outside, "secret/SKILL.md", "should never be reachable")

	mkFile(t, modDir, "skills/good/SKILL.md", "good skill")

	writePluginJSON(t, modDir, `{"name": "evil", "skills": ["../secret", "./skills/good"]}`)

	prims := CollectDependencyPrimitives("acme/evil", modDir)
	skills := onlyType(prims, TypeSkills)

	if len(skills) != 1 {
		t.Fatalf("expected exactly 1 skill primitive (escaping entry skipped), got %d: %+v", len(skills), skills)
	}
	if skills[0].Name != "good" {
		t.Errorf("expected surviving skill 'good', got %q", skills[0].Name)
	}
}

func TestCollectDependencyPrimitives_PluginJSON_AbsolutePathSkillSkipped(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/evil2")

	mkFile(t, modDir, "skills/good/SKILL.md", "good skill")

	abs := filepath.ToSlash(filepath.Join(dir, "outside"))
	manifest := `{"name": "evil2", "skills": ["` + abs + `", "./skills/good"]}`
	writePluginJSON(t, modDir, manifest)

	prims := CollectDependencyPrimitives("acme/evil2", modDir)
	skills := onlyType(prims, TypeSkills)

	if len(skills) != 1 || skills[0].Name != "good" {
		t.Fatalf("expected exactly 1 surviving skill 'good', got %+v", skills)
	}
}

func TestCollectDependencyPrimitives_PluginJSON_SymlinkedSkillSkipped(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/symlinked")
	outsideDir := filepath.Join(dir, "outside-skill")

	mkFile(t, outsideDir, "SKILL.md", "outside skill")
	mkFile(t, modDir, "skills/good/SKILL.md", "good skill")

	linkPath := filepath.Join(modDir, "skills", "linked")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Skipf("symlink creation not permitted in this environment: %v", err)
	}

	writePluginJSON(t, modDir, `{"name": "symlinked", "skills": ["./skills/linked", "./skills/good"]}`)

	prims := CollectDependencyPrimitives("acme/symlinked", modDir)
	skills := onlyType(prims, TypeSkills)

	if len(skills) != 1 || skills[0].Name != "good" {
		t.Fatalf("expected exactly 1 surviving skill 'good' (symlinked entry skipped), got %+v", skills)
	}
}

func TestCollectDependencyPrimitives_PluginJSON_MissingNameFallsBackToLegacyScan(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/noname")

	// plugin.json without "name" is not a valid manifest per spec -- ignored
	// entirely, legacy single-level skills/<name>/SKILL.md scan still runs.
	writePluginJSON(t, modDir, `{"skills": ["./skills/should-be-ignored"]}`)
	mkFile(t, modDir, "skills/legacy/SKILL.md", "legacy skill")

	prims := CollectDependencyPrimitives("acme/noname", modDir)
	skills := onlyType(prims, TypeSkills)

	if len(skills) != 1 || skills[0].Name != "legacy" {
		t.Fatalf("expected legacy scan fallback to find 'legacy', got %+v", skills)
	}
}

func TestCollectDependencyPrimitives_PluginJSON_NoSkillsKeyFallsBackToLegacyScan(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/agentsonly")

	// Valid plugin.json (has name) but no "skills" key at all -- doesn't
	// declare anything about skills, so the legacy single-level scan still
	// applies (only an explicitly-present "skills" array is authoritative).
	writePluginJSON(t, modDir, `{"name": "agentsonly"}`)
	mkFile(t, modDir, "skills/legacy/SKILL.md", "legacy skill")

	prims := CollectDependencyPrimitives("acme/agentsonly", modDir)
	skills := onlyType(prims, TypeSkills)

	if len(skills) != 1 || skills[0].Name != "legacy" {
		t.Fatalf("expected legacy scan fallback to find 'legacy', got %+v", skills)
	}
}

func TestCollectDependencyPrimitives_PluginJSON_EmptySkillsArrayIsAuthoritative(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/emptyskills")

	// plugin.json explicitly declares an empty skills array -- authoritative
	// (intentionally zero skills), legacy scan must NOT run.
	writePluginJSON(t, modDir, `{"name": "emptyskills", "skills": []}`)
	mkFile(t, modDir, "skills/legacy/SKILL.md", "legacy skill")

	prims := CollectDependencyPrimitives("acme/emptyskills", modDir)
	skills := onlyType(prims, TypeSkills)

	if len(skills) != 0 {
		t.Fatalf("expected 0 skills (explicit empty array is authoritative), got %+v", skills)
	}
}

func TestCollectDependencyPrimitives_PluginJSON_InvalidJSONFallsBackToLegacyScan(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/badjson")

	writePluginJSON(t, modDir, `{not valid json`)
	mkFile(t, modDir, "skills/legacy/SKILL.md", "legacy skill")

	prims := CollectDependencyPrimitives("acme/badjson", modDir)
	skills := onlyType(prims, TypeSkills)

	if len(skills) != 1 || skills[0].Name != "legacy" {
		t.Fatalf("expected legacy scan fallback to find 'legacy', got %+v", skills)
	}
}

func TestCollectDependencyPrimitives_PluginJSON_SkillMissingSKILLmdSkipped(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/nomd")

	// Directory exists but has no SKILL.md -- not a valid skill, skipped.
	if err := os.MkdirAll(filepath.Join(modDir, "skills", "empty"), 0755); err != nil {
		t.Fatal(err)
	}
	mkFile(t, modDir, "skills/good/SKILL.md", "good skill")

	writePluginJSON(t, modDir, `{"name": "nomd", "skills": ["./skills/empty", "./skills/good"]}`)

	prims := CollectDependencyPrimitives("acme/nomd", modDir)
	skills := onlyType(prims, TypeSkills)

	if len(skills) != 1 || skills[0].Name != "good" {
		t.Fatalf("expected exactly 1 surviving skill 'good', got %+v", skills)
	}
}

func TestCollectDependencyPrimitives_PluginJSON_Agents(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/withagents")

	mkFile(t, modDir, "agents/helper.md", "helper agent body")
	mkFile(t, modDir, "agents/reviewer.md", "reviewer agent body")

	writePluginJSON(t, modDir, `{"name": "withagents", "agents": ["./agents"]}`)

	prims := CollectDependencyPrimitives("acme/withagents", modDir)
	agents := onlyType(prims, TypeAgents)

	if len(agents) != 2 {
		t.Fatalf("expected 2 agent primitives, got %d: %+v", len(agents), agents)
	}
	names := namesOf(agents)
	sort.Strings(names)
	if !equalStrings(names, []string{"helper", "reviewer"}) {
		t.Errorf("expected [helper reviewer], got %v", names)
	}
}

func TestCollectDependencyPrimitives_PluginJSON_AgentsSingleFile(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/singleagent")

	mkFile(t, modDir, "custom/solo.md", "solo agent body")

	writePluginJSON(t, modDir, `{"name": "singleagent", "agents": ["./custom/solo.md"]}`)

	prims := CollectDependencyPrimitives("acme/singleagent", modDir)
	agents := onlyType(prims, TypeAgents)

	if len(agents) != 1 || agents[0].Name != "solo" {
		t.Fatalf("expected 1 agent 'solo', got %+v", agents)
	}
}

func TestCollectDependencyPrimitives_PluginJSON_Commands(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/withcommands")

	mkFile(t, modDir, "commands/deploy.md", "deploy command body")

	writePluginJSON(t, modDir, `{"name": "withcommands", "commands": ["./commands"]}`)

	prims := CollectDependencyPrimitives("acme/withcommands", modDir)
	commands := onlyType(prims, TypeCommands)

	if len(commands) != 1 || commands[0].Name != "deploy" {
		t.Fatalf("expected 1 command 'deploy', got %+v", commands)
	}
}

// TestCollectDependencyPrimitives_PluginJSON_SkillsAsSingleString covers the
// spec's alternate shape for a component field: a single path string
// instead of an array (mirrors Python's _resolve_sources string branch).
func TestCollectDependencyPrimitives_PluginJSON_SkillsAsSingleString(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/singleskillstring")

	mkFile(t, modDir, "skills/solo/SKILL.md", "solo skill")
	writePluginJSON(t, modDir, `{"name": "singleskillstring", "skills": "./skills/solo"}`)

	prims := CollectDependencyPrimitives("acme/singleskillstring", modDir)
	skills := onlyType(prims, TypeSkills)

	if len(skills) != 1 || skills[0].Name != "solo" {
		t.Fatalf("expected 1 skill 'solo', got %+v", skills)
	}
}

// TestCollectDependencyPrimitives_PluginJSON_AgentsDirSkipsSymlinkedEntry
// covers the defense-in-depth guard on individual files listed inside a
// component directory (not just the manifest-declared directory path
// itself), consistent with "the path or any component is a symlink".
func TestCollectDependencyPrimitives_PluginJSON_AgentsDirSkipsSymlinkedEntry(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/symlinkedagentfile")
	outsideFile := filepath.Join(dir, "outside-agent.md")

	mkFile(t, outsideFile, "", "outside agent body")
	mkFile(t, modDir, "agents/real.md", "real agent body")

	linkPath := filepath.Join(modDir, "agents", "linked.md")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlink creation not permitted in this environment: %v", err)
	}

	writePluginJSON(t, modDir, `{"name": "symlinkedagentfile", "agents": ["./agents"]}`)

	prims := CollectDependencyPrimitives("acme/symlinkedagentfile", modDir)
	agents := onlyType(prims, TypeAgents)

	if len(agents) != 1 || agents[0].Name != "real" {
		t.Fatalf("expected exactly 1 surviving agent 'real', got %+v", agents)
	}
}

// TestCollectDependencyPrimitives_PluginJSON_AgentsSingleFileUnsupportedExtSkipped
// covers the single-file (non-directory) entry branch of
// collectPluginFlatFiles when the file doesn't match the primitive's naming
// convention (e.g. no .md suffix) -- skipped, no primitive emitted.
func TestCollectDependencyPrimitives_PluginJSON_AgentsSingleFileUnsupportedExtSkipped(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/badext")

	mkFile(t, modDir, "custom/notes.txt", "not a markdown agent file")
	writePluginJSON(t, modDir, `{"name": "badext", "agents": ["./custom/notes.txt"]}`)

	prims := CollectDependencyPrimitives("acme/badext", modDir)
	agents := onlyType(prims, TypeAgents)

	if len(agents) != 0 {
		t.Fatalf("expected 0 agents (unsupported extension), got %+v", agents)
	}
}

func onlyType(prims []Primitive, t PrimitiveType) []Primitive {
	var out []Primitive
	for _, p := range prims {
		if p.Type == t {
			out = append(out, p)
		}
	}
	return out
}

func namesOf(prims []Primitive) []string {
	names := make([]string, len(prims))
	for i, p := range prims {
		names[i] = p.Name
	}
	return names
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func joinQuoted(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
