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

	prims := collectDep(t, "mattpocock/skills", modDir)

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

	prims := collectDep(t, "mattpocock/skills", modDir)
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

	prims := collectDep(t, "acme/evil", modDir)
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

	prims := collectDep(t, "acme/evil2", modDir)
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

	prims := collectDep(t, "acme/symlinked", modDir)
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

	prims := collectDep(t, "acme/noname", modDir)
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

	prims := collectDep(t, "acme/agentsonly", modDir)
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

	prims := collectDep(t, "acme/emptyskills", modDir)
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

	prims := collectDep(t, "acme/badjson", modDir)
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

	prims := collectDep(t, "acme/nomd", modDir)
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

	prims := collectDep(t, "acme/withagents", modDir)
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

	prims := collectDep(t, "acme/singleagent", modDir)
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

	prims := collectDep(t, "acme/withcommands", modDir)

	if commands := onlyType(prims, TypeCommands); len(commands) != 0 {
		t.Errorf("commands = %+v, want none: a plugin command is a prompt", commands)
	}
	prompts := onlyType(prims, TypePrompts)
	if len(prompts) != 1 || prompts[0].Name != "deploy" {
		t.Fatalf("expected 1 prompt 'deploy', got %+v", prompts)
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

	prims := collectDep(t, "acme/singleskillstring", modDir)
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

	prims := collectDep(t, "acme/symlinkedagentfile", modDir)
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

	prims := collectDep(t, "acme/badext", modDir)
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

// A plugin.json without an "agents" key deploys the plugin's agents/
// directory, as the Oracle's _resolve_sources falls back to it
// (deps/plugin_parser.py:1180-1214).
func TestCollectDependencyPrimitives_PluginJSON_DefaultAgentsDir(t *testing.T) {
	modDir := filepath.Join(t.TempDir(), "apm_modules", "_local/plug")
	writePluginJSON(t, modDir, `{"name":"plug","version":"1.0.0"}`)
	mkFile(t, modDir, "agents/rev.md", "rev agent\n")

	agents := onlyType(collectDep(t, "_local/plug", modDir), TypeAgents)

	if len(agents) != 1 || agents[0].Name != "rev" || agents[0].SrcPath != filepath.Join(modDir, "agents", "rev.md") {
		t.Fatalf("agents = %+v, want rev from agents/rev.md", agents)
	}
}

// The default directories are a Claude plugin's alone: the Oracle takes a
// package whose apm.yml has a .apm/ or declares a dependency for an APM
// package and reads no plugin.json there (models/format_detection.py:451-460).
// An apm.yml with metadata alone does not make one.
func TestCollectDependencyPrimitives_PluginJSON_DefaultDirsNeedNoEligibleAPMYML(t *testing.T) {
	tests := []struct {
		name     string
		apmYML   string // "" for none
		apmDir   bool
		wantDefs bool
	}{
		{name: "no apm.yml", wantDefs: true},
		{name: "metadata-only apm.yml", apmYML: "name: m\nversion: 2.0.0\n", wantDefs: true},
		{name: "apm.yml with .apm/", apmYML: "name: m\nversion: 2.0.0\n", apmDir: true},
		{name: "apm.yml declaring an apm dependency", apmYML: "name: m\ndependencies:\n  apm:\n    - acme/x\n"},
		{name: "apm.yml declaring a dev mcp dependency", apmYML: "name: m\ndevDependencies:\n  mcp:\n    - name: s\n"},
		{name: "apm.yml with an empty dependency list", apmYML: "name: m\ndependencies:\n  apm: []\n", wantDefs: true},
		{name: "apm.yml that does not parse", apmYML: "name: [\n", wantDefs: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modDir := filepath.Join(t.TempDir(), "apm_modules", "_local/plug")
			writePluginJSON(t, modDir, `{"name":"plug"}`)
			mkFile(t, modDir, "agents/rev.md", "rev agent\n")
			if tt.apmYML != "" {
				mkFile(t, modDir, "apm.yml", tt.apmYML)
			}
			if tt.apmDir {
				if err := os.MkdirAll(filepath.Join(modDir, ".apm"), 0o755); err != nil {
					t.Fatal(err)
				}
			}

			agents := onlyType(collectDep(t, "_local/plug", modDir), TypeAgents)

			if got := len(agents) == 1; got != tt.wantDefs {
				t.Fatalf("agents = %+v, want the default agents/ read: %v", agents, tt.wantDefs)
			}
		})
	}
}

// A plugin command is the prompt the Oracle copies it to: <name>.md and
// <name>.prompt.md become .apm/prompts/<name>.prompt.md
// (bundle/plugin_layout.py:44-50, deps/plugin_parser.py:1264-1290), found
// as a prompt is, at the top level only and without a Copilot App workflow.
func TestCollectDependencyPrimitives_PluginJSON_CommandsArePrompts(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     []string
	}{
		{"default commands directory", `{"name":"plug"}`, []string{"hello", "three"}},
		{"declared commands directory", `{"name":"plug","commands":"./commands"}`, []string{"hello", "three"}},
		{"declared command file", `{"name":"plug","commands":["./commands/three.prompt.md"]}`, []string{"three"}},
		{"declared workflow file", `{"name":"plug","commands":["./commands/two.md"]}`, nil},
		{"empty list", `{"name":"plug","commands":[]}`, nil},
		{"value that is neither a list nor a string", `{"name":"plug","commands":7}`, []string{"hello", "three"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modDir := filepath.Join(t.TempDir(), "apm_modules", "_local/plug")
			writePluginJSON(t, modDir, tt.manifest)
			mkFile(t, modDir, "commands/hello.md", "---\ndescription: hi\n---\nHello\n")
			mkFile(t, modDir, "commands/two.md", "---\ninterval: 1h\n---\nTwo\n")
			mkFile(t, modDir, "commands/three.prompt.md", "Three\n")
			mkFile(t, modDir, "commands/notes.txt", "notes\n")
			mkFile(t, modDir, "commands/sub/deep.md", "Deep\n")

			prims := collectDep(t, "_local/plug", modDir)

			if commands := onlyType(prims, TypeCommands); len(commands) != 0 {
				t.Errorf("commands = %+v, want none", commands)
			}
			got := namesOf(onlyType(prims, TypePrompts))
			sort.Strings(got)
			if !equalStrings(got, tt.want) {
				t.Errorf("prompts = %v, want %v", got, tt.want)
			}
		})
	}
}

// The Oracle names a Claude plugin by the apm.yml it builds from plugin.json
// (deps/plugin_parser.py:1370), so the warning about its command carries the
// name of plugin.json; an APM package keeps the name of its own apm.yml.
func TestRun_PluginCommandNotesNameThePlugin(t *testing.T) {
	tests := []struct {
		name   string
		apmYML string
		apmDir bool
		want   string
	}{
		{name: "no apm.yml", want: "other"},
		{name: "metadata-only apm.yml", apmYML: "name: meta\nversion: 2.0.0\n", want: "other"},
		{name: "APM package", apmYML: "name: meta\nversion: 2.0.0\n", apmDir: true, want: "meta"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			mod := filepath.Join(dir, "apm_modules", "acme/pkg")
			writePluginJSON(t, mod, `{"name":"other","commands":"./commands"}`)
			mkFile(t, mod, "commands/hello.md", "---\ndescription: hi\nname: hello\n---\nHello\n")
			if tt.apmYML != "" {
				mkFile(t, mod, "apm.yml", tt.apmYML)
			}
			if tt.apmDir {
				mkFile(t, mod, ".apm/instructions/a.instructions.md", "a\n")
			}
			m, resolved := depManifest("acme/pkg")

			result, err := Run([]string{"claude"}, dir, m, resolved, nil, "", false)
			if err != nil {
				t.Fatal(err)
			}

			want := CommandNote{DepKey: "acme/pkg", Package: tt.want, Text: "Claude command hello: frontmatter keys not supported for claude commands and were dropped: name. Supported keys: allowed-tools, argument-hint, description, input, model."}
			if len(result.CommandNotes) != 1 || result.CommandNotes[0] != want {
				t.Errorf("notes = %+v, want [%+v]", result.CommandNotes, want)
			}
		})
	}
}

// A default directory that is a symlink is not read, also one that stays in
// the plugin: the Oracle's _resolve_sources refuses any symlink on the way
// (deps/plugin_parser.py:1208-1215).
func TestCollectDependencyPrimitives_PluginJSON_SymlinkedDefaultDirSkipped(t *testing.T) {
	modDir := filepath.Join(t.TempDir(), "apm_modules", "_local/plug")
	writePluginJSON(t, modDir, `{"name":"plug"}`)
	mkFile(t, modDir, "real-agents/rev.md", "rev agent\n")
	mkFile(t, modDir, "real-commands/hello.md", "Hello\n")
	for _, link := range []string{"agents", "commands"} {
		if err := os.Symlink("real-"+link, filepath.Join(modDir, link)); err != nil {
			t.Skipf("symlink: %v", err)
		}
	}

	prims := collectDep(t, "_local/plug", modDir)

	if got := append(onlyType(prims, TypeAgents), onlyType(prims, TypePrompts)...); len(got) != 0 {
		t.Errorf("primitives = %+v, want none from a symlinked default directory", got)
	}
}

// Telling a Claude plugin from an APM package reads no apm.yml that leaves
// the package, and is no error either: the collection compile does stays as it
// was, and install refuses that apm.yml where it reads the package's manifest.
func TestCollectDependencyPrimitives_PluginJSON_APMYMLOutsideIsNotRead(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/plug")
	writePluginJSON(t, modDir, `{"name":"plug"}`)
	mkFile(t, modDir, "agents/rev.md", "rev agent\n")
	mkFile(t, dir, "outside.yml", "name: x\ndependencies:\n  apm:\n    - acme/x\n")
	if err := os.Symlink(filepath.Join("..", "..", "..", "outside.yml"), filepath.Join(modDir, "apm.yml")); err != nil {
		t.Skipf("symlink: %v", err)
	}

	agents := onlyType(collectDep(t, "acme/plug", modDir), TypeAgents)

	if len(agents) != 1 || agents[0].Name != "rev" {
		t.Fatalf("agents = %+v, want rev from the default agents/", agents)
	}
}
