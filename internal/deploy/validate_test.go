package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

// Whether each package is taken is what Oracle 0.33.0's uninstall did with
// it as the staged copy of a package that stays (issue #129, the r2 runs of
// PR #171's second review).
func TestPackageError(t *testing.T) {
	const yml = "name: dep\nversion: 1.0.0\n"
	const skill = "---\nname: s\ndescription: d\n---\nbody\n"
	const badSkill = "---\nname: [unclosed\n---\nbody\n"
	tests := []struct {
		name  string
		files []string // path, content, ...
		ok    bool
	}{
		{"apm package", []string{"apm.yml", yml, ".apm/skills/s/SKILL.md", skill}, true},
		{"apm package with an invalid plugin.json", []string{"apm.yml", yml, ".apm/skills/s/SKILL.md", skill, "plugin.json", "{invalid json"}, true},
		{"plugin.json invalid", []string{"plugin.json", "{invalid json"}, false},
		{"plugin.json with no name", []string{"plugin.json", "{}"}, false},
		{"plugin.json with a name", []string{"plugin.json", `{"name": "dep"}`}, true},
		{".claude-plugin/plugin.json invalid", []string{".claude-plugin/plugin.json", "{invalid json"}, false},
		{".claude-plugin/plugin.json empty", []string{".claude-plugin/plugin.json", "{}"}, true},
		{"apm.yml alone", []string{"apm.yml", yml}, false},
		{"apm.yml and hooks", []string{"apm.yml", yml, "hooks/h.json", "{}"}, false},
		{"apm.yml and a skill bundle", []string{"apm.yml", yml, "skills/sx/SKILL.md", skill}, true},
		{"apm.yml and a SKILL.md that does not load", []string{"apm.yml", yml, "SKILL.md", badSkill}, true},
		{"SKILL.md", []string{"SKILL.md", skill}, true},
		{"SKILL.md that does not load", []string{"SKILL.md", badSkill}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for i := 0; i < len(tt.files); i += 2 {
				p := filepath.Join(dir, filepath.FromSlash(tt.files[i]))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(tt.files[i+1]), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := PackageError(dir); (err == nil) != tt.ok {
				t.Errorf("PackageError = %v, want ok %v", err, tt.ok)
			}
		})
	}
}
