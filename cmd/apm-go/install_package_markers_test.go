package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #128 items 2 and c50-5: a local directory with no apm.yml, SKILL.md
// or plugin manifest is no package, also one that holds only skills/ or
// hooks/. The lines are Oracle 0.33.0's (install/validation.py:155-263;
// measured, A/128-2/r2, A/c50/r1-bundle, r1-hook), with apm-go's status
// symbols, and the apm.yml the run created is removed.
func TestInstall_LocalDirectoryWithoutMarkerIsRefused(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name: "empty",
			want: " + Created apm.yml\n" +
				" i Targets set: claude (persisted to apm.yml)\n" +
				" + Validating 1 package...\n" +
				" x ./pkg -- no apm.yml, SKILL.md, or plugin.json found\n" +
				"All packages failed validation. Nothing to install.\n" +
				" i Removed apm.yml created by the failed install.\n",
		},
		{
			name:  "skills only",
			files: map[string]string{"pkg/skills/b1/SKILL.md": "---\nname: b1\ndescription: d\n---\nbody\n"},
			want: " + Created apm.yml\n" +
				" i Targets set: claude (persisted to apm.yml)\n" +
				" + Validating 1 package...\n" +
				" i   Found installable package(s) inside this directory:\n" +
				" x ./pkg -- no apm.yml, SKILL.md, or plugin.json found\n" +
				"All packages failed validation. Nothing to install.\n" +
				" i Removed apm.yml created by the failed install.\n",
		},
		{
			name:  "hooks only",
			files: map[string]string{"pkg/hooks/h.json": "{\"hooks\": {}}\n"},
			want: " + Created apm.yml\n" +
				" i Targets set: claude (persisted to apm.yml)\n" +
				" + Validating 1 package...\n" +
				" x ./pkg -- no apm.yml, SKILL.md, or plugin.json found\n" +
				"All packages failed validation. Nothing to install.\n" +
				" i Removed apm.yml created by the failed install.\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := chdirTemp(t)
			if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
				t.Fatal(err)
			}
			for rel, content := range tc.files {
				writeCompileFile(t, dir, rel, content)
			}
			stdout, _, code := runCompileCLI(t, "install", "./pkg", "--target", "claude")
			if code != 1 {
				t.Errorf("exit = %d, want 1", code)
			}
			if stdout != tc.want {
				t.Errorf("stdout:\n%s\nwant:\n%s", stdout, tc.want)
			}
			for _, rel := range []string{"apm.yml", "apm.lock.yaml", ".claude", "apm_modules"} {
				if _, err := os.Lstat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
					t.Errorf("%s: err = %v, want it absent", rel, err)
				}
			}
		})
	}
}

// With --verbose the hint names each package found below the directory, as
// the Oracle names it (measured, A/after1/c-bundle-v).
func TestInstall_LocalDirectoryWithoutMarker_VerboseNamesInnerPackages(t *testing.T) {
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "pkg/skills/b1/SKILL.md", "---\nname: b1\ndescription: d\n---\nbody\n")
	stdout, _, code := runCompileCLI(t, "install", "./pkg", "--target", "claude", "--verbose")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := " i   Found installable package(s) inside this directory:\n" +
		"      apm-go install " + filepath.Join(real, "pkg", "skills", "b1") + "\n" +
		" x ./pkg -- no apm.yml, SKILL.md, or plugin.json found\n"
	if !strings.Contains(stdout, want) && !strings.Contains(stdout, strings.Replace(want, real, dir, 1)) {
		t.Errorf("stdout:\n%s\nwant it to hold:\n%s", stdout, want)
	}
}

// A directory apm.yml declares that is no package fails the install with
// the Oracle's reason (measured, A/128-2/r2), and no lock is written.
func TestInstall_DeclaredLocalDirectoryWithoutMarkerFails(t *testing.T) {
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n  - ./empty\n")
	if err := os.MkdirAll(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := runCompileCLI(t, "install")
	if code != 1 {
		t.Errorf("exit = %d, want 1\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "Local package is not a valid APM package (no apm.yml, SKILL.md, or plugin.json): ./empty\n") {
		t.Errorf("stdout has no refusal:\n%s", stdout)
	}
	for _, rel := range []string{"apm.lock.yaml", ".claude", "apm_modules/_local/empty"} {
		if _, err := os.Lstat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
			t.Errorf("%s: err = %v, want it absent", rel, err)
		}
	}
}

// Issue #126 item 78: a component path plugin.json declares that is empty,
// leaves the plugin or is not there fails the install before anything is
// deployed, with the Oracle's reason (deps/plugin_parser.py:516-551;
// measured, A/78/missing-r1). Hooks given as an object declare no path.
func TestInstall_PluginDeclaredComponentPath(t *testing.T) {
	cases := []struct {
		name, manifest, want string
	}{
		{"missing", `{"name": "plug", "agents": "./nope"}`,
			"Plugin 'plug' declares missing 'agents' component path './nope' in plugin root '<root>'. Add the component or remove the declaration from plugin.json, then reinstall."},
		{"missing in a list", `{"name": "plug", "commands": ["./commands/hello.md", "./commands/gone.md"]}`,
			"Plugin 'plug' declares missing 'commands' component path './commands/gone.md' in plugin root '<root>'. Add the component or remove the declaration from plugin.json, then reinstall."},
		{"empty", `{"name": "plug", "skills": ""}`,
			"Plugin 'plug' declares an empty 'skills' component path in plugin root '<root>'. Remove the empty declaration from plugin.json, then reinstall."},
		{"outside", `{"name": "plug", "agents": "../elsewhere"}`,
			"Plugin 'plug' declares an invalid 'agents' component path '../elsewhere' outside plugin root '<root>'. Move the component inside the plugin root or remove the declaration from plugin.json, then reinstall."},
		{"hooks object", `{"name": "plug", "hooks": {"PreToolUse": []}}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := chdirTemp(t)
			writeCompileFile(t, dir, "plug/.claude-plugin/plugin.json", tc.manifest+"\n")
			writeCompileFile(t, dir, "plug/commands/hello.md", "---\ndescription: Say hello\n---\n\nSay hello.\n")
			writeCompileFile(t, dir, "elsewhere/a.md", "# a\n")
			stdout, _, code := runCompileCLI(t, "install", "./plug", "--target", "claude")
			if tc.want == "" {
				if code != 0 {
					t.Fatalf("exit = %d, want 0\n%s", code, stdout)
				}
				return
			}
			if code != 1 {
				t.Errorf("exit = %d, want 1\n%s", code, stdout)
			}
			real, err := filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(real, "apm_modules", "_local", "plug")
			if want := strings.ReplaceAll(tc.want, "<root>", root); !strings.Contains(stdout, want) &&
				!strings.Contains(stdout, strings.ReplaceAll(tc.want, "<root>", filepath.Join(dir, "apm_modules", "_local", "plug"))) {
				t.Errorf("stdout:\n%s\nwant it to hold:\n%s", stdout, want)
			}
			for _, rel := range []string{"apm.yml", "apm.lock.yaml", ".claude"} {
				if _, err := os.Lstat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
					t.Errorf("%s: err = %v, want it absent", rel, err)
				}
			}
		})
	}
}
