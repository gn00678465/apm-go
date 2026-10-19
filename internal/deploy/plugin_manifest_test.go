package deploy

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A Claude plugin's manifest is the first of the Oracle's find_plugin_json
// places that exists (utils/helpers.py:105-129), read whole. The expected
// files and package names are what Oracle 0.33.0 deployed for the same local
// plugin (~/.cache/apm-go-verify/issue126/plug/r4.txt, r5.txt, r3/, r6/):
// agents/rev.md, commands/hello.md and skills/s1/, with agents2/rev2.md when
// the manifest lists ./agents2. A manifest with no name is named after the
// package directory. The cases the Oracle refuses with exit 1 keep what
// apm-go deployed before (main 7984723, the same files): a root plugin.json
// that is an Agent Plugin's, has no name or is not valid JSON is passed over,
// and any other invalid first manifest leaves the package no plugin.
func TestRun_PluginManifestLocations(t *testing.T) {
	const (
		agentPluginSchema = `"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"`
		pluginFiles       = ".claude/agents/rev.md .claude/commands/hello.md .claude/skills/s1/SKILL.md"
		pluginFiles2      = ".claude/agents/rev2.md .claude/commands/hello.md .claude/skills/s1/SKILL.md"
		skillOnly         = ".claude/skills/s1/SKILL.md"
	)
	tests := []struct {
		name      string
		manifests map[string]string
		apmYML    string
		want      string
		pkg       string // the [<package>] of the command notes, "" for none
	}{
		{"github plugin", map[string]string{".github/plugin/plugin.json": `{"name":"plug","version":"1.0.0"}`}, "", pluginFiles, "plug"},
		{"github plugin with no name", map[string]string{".github/plugin/plugin.json": `{"version":"1.0.0"}`}, "", pluginFiles, "plug"},
		{"cursor plugin", map[string]string{".cursor-plugin/plugin.json": `{"name":"plug","version":"1.0.0"}`}, "", pluginFiles, "plug"},
		{"root plugin.json", map[string]string{"plugin.json": `{"name":"plug","version":"1.0.0"}`}, "", pluginFiles, "plug"},
		{"root plugin.json with an unknown $schema", map[string]string{"plugin.json": `{"$schema":"https://example.com/x.json","name":"plug","version":"1.0.0"}`}, "", pluginFiles, "plug"},
		{"github before claude-plugin", map[string]string{
			".github/plugin/plugin.json": `{"name":"ghname","version":"1.0.0","agents":["./agents2"]}`,
			".claude-plugin/plugin.json": `{"name":"cpname","version":"2.0.0"}`,
		}, "", pluginFiles2, "ghname"},
		{"root before claude-plugin", map[string]string{
			"plugin.json":                `{"name":"rootname","version":"1.0.0","agents":["./agents2"]}`,
			".claude-plugin/plugin.json": `{"name":"cpname","version":"2.0.0"}`,
		}, "", pluginFiles2, "rootname"},
		{"claude-plugin before cursor", map[string]string{
			".claude-plugin/plugin.json": `{"name":"cpname","version":"1.0.0","agents":["./agents2"]}`,
			".cursor-plugin/plugin.json": `{"name":"cuname","version":"2.0.0"}`,
		}, "", pluginFiles2, "cpname"},
		{"first manifest with no name is not merged", map[string]string{
			".github/plugin/plugin.json": `{"version":"1.0.0","agents":["./agents2"]}`,
			".claude-plugin/plugin.json": `{"name":"cpname","version":"2.0.0"}`,
		}, "", pluginFiles2, "plug"},
		{"claude-plugin with no name", map[string]string{".claude-plugin/plugin.json": `{"version":"1.0.0"}`}, "", pluginFiles, "plug"},
		{"claude-plugin with an empty name", map[string]string{".claude-plugin/plugin.json": `{"name":"","version":"1.0.0"}`}, "", pluginFiles, "plug"},
		{"claude-plugin with a null name", map[string]string{".claude-plugin/plugin.json": `{"name":null,"version":"1.0.0"}`}, "", pluginFiles, "plug"},
		{"no name beside a metadata-only apm.yml", map[string]string{".claude-plugin/plugin.json": `{"version":"1.0.0"}`}, "name: metaname\nversion: 2.0.0\n", pluginFiles, "plug"},
		{"name that is no string", map[string]string{".claude-plugin/plugin.json": `{"name":123,"version":"1.0.0"}`}, "", skillOnly, ""},
		{"root plugin.json with no name", map[string]string{"plugin.json": `{"version":"1.0.0"}`}, "", skillOnly, ""},
		{"root Agent Plugin", map[string]string{"plugin.json": `{` + agentPluginSchema + `,"name":"plug","version":"1.0.0"}`}, "", skillOnly, ""},
		{"root plugin.json with no name before claude-plugin", map[string]string{
			"plugin.json":                `{"version":"1.0.0"}`,
			".claude-plugin/plugin.json": `{"name":"cpname","version":"2.0.0"}`,
		}, "", pluginFiles, "cpname"},
		{"invalid first manifest", map[string]string{
			".github/plugin/plugin.json": `{not json`,
			".claude-plugin/plugin.json": `{"name":"cpname","version":"2.0.0"}`,
		}, "", skillOnly, ""},
		{"invalid root plugin.json", map[string]string{"plugin.json": `{not json`}, "", skillOnly, ""},
		{"invalid root plugin.json before claude-plugin", map[string]string{
			"plugin.json":                `{not json`,
			".claude-plugin/plugin.json": `{"name":"cpname","version":"2.0.0"}`,
		}, "", pluginFiles, "cpname"},
		{"claude-plugin directory with no manifest", map[string]string{".claude-plugin/README": "x"}, "", skillOnly, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			mod := filepath.Join(dir, "apm_modules", "_local", "plug")
			for rel, body := range tt.manifests {
				mkFile(t, mod, rel, body+"\n")
			}
			if tt.apmYML != "" {
				mkFile(t, mod, "apm.yml", tt.apmYML)
			}
			mkFile(t, mod, "agents/rev.md", "---\nname: rev\ndescription: reviewer\n---\nReview.\n")
			mkFile(t, mod, "agents2/rev2.md", "---\nname: rev2\ndescription: reviewer2\n---\nReview2.\n")
			mkFile(t, mod, "commands/hello.md", "---\ndescription: hello cmd\nname: hello\n---\nSay hi\n")
			mkFile(t, mod, "skills/s1/SKILL.md", "---\nname: s1\ndescription: s\n---\nS\n")
			m, resolved := depManifest("_local/plug")

			result, err := Run([]string{"claude"}, dir, m, resolved, nil, "", false, nil)
			if err != nil {
				t.Fatal(err)
			}

			if got := strings.Join(filesBelow(t, dir, ".claude"), " "); got != tt.want {
				t.Errorf("deployed %s, want %s", got, tt.want)
			}
			var pkgs []string
			for _, n := range result.CommandNotes {
				pkgs = append(pkgs, n.Package)
			}
			if want := []string{tt.pkg}; tt.pkg == "" && len(pkgs) != 0 || tt.pkg != "" && !slices.Equal(pkgs, want) {
				t.Errorf("command note packages = %q, want %q", pkgs, tt.pkg)
			}
		})
	}
}

// An APM package reads .claude-plugin/plugin.json alone, as before: the
// Oracle reads no plugin.json of an APM package, and apm-go keeps deploying
// what that file lists (#88 item 75).
func TestCollectDependencyPrimitives_APMPackageReadsClaudePluginManifestOnly(t *testing.T) {
	tests := []struct {
		name      string
		manifests map[string]string
		want      []string
	}{
		{"github plugin", map[string]string{".github/plugin/plugin.json": `{"name":"plug","agents":["./agents/rev.md"]}`}, nil},
		{"root plugin.json", map[string]string{"plugin.json": `{"name":"plug","agents":["./agents/rev.md"]}`}, nil},
		{"claude-plugin", map[string]string{".claude-plugin/plugin.json": `{"name":"plug","agents":["./agents/rev.md"]}`}, []string{"rev"}},
		{"claude-plugin with no name", map[string]string{".claude-plugin/plugin.json": `{"agents":["./agents/rev.md"]}`}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod := filepath.Join(t.TempDir(), "apm_modules", "acme", "pkg")
			for rel, body := range tt.manifests {
				mkFile(t, mod, rel, body)
			}
			mkFile(t, mod, "apm.yml", "name: pkg\nversion: 1.0.0\n")
			mkFile(t, mod, ".apm/instructions/i.instructions.md", "i\n")
			mkFile(t, mod, "agents/rev.md", "rev\n")

			got := namesOf(onlyType(collectDep(t, "acme/pkg", mod), TypeAgents))

			if !slices.Equal(got, tt.want) {
				t.Errorf("agents = %v, want %v", got, tt.want)
			}
		})
	}
}

// A plugin.json that lists the package's root skill ("./") or one skill
// directory twice names one skill. The Oracle deploys it once; apm-go
// collected it twice, so update and a frozen install that restored a file
// warned that the skill shadowed itself (issue #126 item 新-113-5,
// ~/.cache/apm-go-verify/issue126/plug/c1/, r8/).
func TestRun_PluginSkillListedTwiceDoesNotShadowItself(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		files    map[string]string
		want     string
	}{
		{"root skill", `{"name":"plugskill","skills":["./"]}`,
			map[string]string{"SKILL.md": "---\nname: plugskill\ndescription: d\n---\nBody\n"},
			".claude/skills/plugskill/.claude-plugin/plugin.json .claude/skills/plugskill/SKILL.md"},
		{"one directory twice", `{"name":"plug","skills":["./skills/s1","skills/s1/"]}`,
			map[string]string{"skills/s1/SKILL.md": "---\nname: s1\ndescription: s\n---\nS\n"},
			".claude/skills/s1/SKILL.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			mod := filepath.Join(dir, "apm_modules", "_local", "plugskill")
			writePluginJSON(t, mod, tt.manifest)
			for rel, body := range tt.files {
				mkFile(t, mod, rel, body)
			}
			m, resolved := depManifest("_local/plugskill")

			result, err := Run([]string{"claude"}, dir, m, resolved, nil, "", false, nil)
			if err != nil {
				t.Fatal(err)
			}

			for _, d := range result.Diags {
				if strings.Contains(d, "shadowed") {
					t.Errorf("diagnostic %q, want none", d)
				}
			}
			if got := strings.Join(filesBelow(t, dir, ".claude"), " "); got != tt.want {
				t.Errorf("deployed %s, want %s", got, tt.want)
			}
		})
	}
}

// filesBelow lists the regular files below dir/rel, slash-separated relative
// to dir, sorted.
func filesBelow(t *testing.T, dir, rel string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(filepath.Join(dir, rel), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.Type().IsRegular() {
			r, _ := filepath.Rel(dir, p)
			files = append(files, filepath.ToSlash(r))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	return files
}
