package main

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// writeTwoSkillDep writes the local package at dir with the skills sk and b,
// and the project apm.yml with the given dependencies.apm entry.
func writeTwoSkillDep(t *testing.T, dir, entry string) {
	t.Helper()
	for _, name := range []string{"sk", "b"} {
		skillDir := filepath.Join(dir, ".apm", "skills", name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + name + "\ndescription: skill " + name + "\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "apm.yml"), []byte("name: dep\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("apm.yml", []byte("name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n"+entry), 0o644); err != nil {
		t.Fatal(err)
	}
}

func installLocalStep(t *testing.T, step string, skills, packages []string) {
	t.Helper()
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	if err := runInstall(deps, false, false, "", "", skills, packages); err != nil {
		t.Fatalf("%s: %v", step, err)
	}
}

func deployedClaudeSkills(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(".claude", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// Issue #61, the reported sequence: install, install ./dep --skill sk, install.
func TestRunInstall_LocalSkillSubsetSurvivesNextInstall(t *testing.T) {
	chdirTemp(t)
	writeTwoSkillDep(t, "dep", "  - ./dep\n")

	installLocalStep(t, "install", nil, nil)
	installLocalStep(t, "install ./dep --skill sk", []string{"sk"}, []string{"./dep"})

	const wantManifest = "name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n    - path: ./dep\n      skills:\n        - sk\n"
	got, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != wantManifest {
		t.Fatalf("apm.yml after --skill sk:\n%s\nwant:\n%s", got, wantManifest)
	}

	installLocalStep(t, "second plain install", nil, nil)

	if names := deployedClaudeSkills(t); len(names) != 1 || names[0] != "sk" {
		t.Errorf(".claude/skills = %v, want [sk]", names)
	}
	if got, err := os.ReadFile("apm.yml"); err != nil || string(got) != wantManifest {
		t.Errorf("apm.yml changed by the plain install (err %v):\n%s", err, got)
	}
	lockBytes, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	lockDoc, err := yamlcore.SafeLoad(lockBytes)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lockfile.ParseLockfile(lockDoc)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Dependencies) != 1 {
		t.Fatalf("lock has %d dependencies, want 1", len(lock.Dependencies))
	}
	dep := lock.Dependencies[0]
	if dep.Source != "local" || dep.LocalPath != "./dep" || len(dep.SkillSubset) != 1 || dep.SkillSubset[0] != "sk" {
		t.Errorf("lock entry: source %q, local_path %q, skill_subset %v; want local, ./dep, [sk]", dep.Source, dep.LocalPath, dep.SkillSubset)
	}
}

// A `path:` value with no local prefix is refused as the Oracle v0.33.0
// refuses it (models/dependency/reference.py:837-842), by install and by
// uninstall, before anything is written. The reason text is the Oracle's.
func TestPathEntryWithoutLocalPrefixIsRefused(t *testing.T) {
	const reason = "Object-style dependency must have a 'git' field, or 'path' must be a local filesystem path (starting with './', '../', '/', or '~')"
	tests := []struct {
		name, dir, entry, pkg, want string
	}{
		{"bare name", "dep", "    - path: dep\n", "./dep", `validate apm.yml: dependency entry 0 (path: "dep"): ` + reason},
		{"two segments", filepath.Join("sub", "dep"), "    - path: sub/dep\n", "./sub/dep", `validate apm.yml: dependency entry 0 (path: "sub/dep"): ` + reason},
	}
	install := func(skills, packages []string) error {
		deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
		return runInstall(deps, false, false, "", "", skills, packages)
	}
	for _, tt := range tests {
		steps := []struct {
			name string
			run  func() error
		}{
			{"install", func() error { return install(nil, nil) }},
			{"install <path> --skill sk", func() error { return install([]string{"sk"}, []string{tt.pkg}) }},
			{"install <path> --skill '*'", func() error { return install([]string{"*"}, []string{tt.pkg}) }},
			{"uninstall <path>", func() error { return runUninstall([]string{tt.pkg}, uninstallOptions{}) }},
		}
		t.Run(tt.name, func(t *testing.T) {
			chdirTemp(t)
			writeTwoSkillDep(t, tt.dir, tt.entry)
			before, err := os.ReadFile("apm.yml")
			if err != nil {
				t.Fatal(err)
			}
			for _, step := range steps {
				err := step.run()
				if err == nil || err.Error() != tt.want {
					t.Errorf("%s: error %v, want %s", step.name, err, tt.want)
				}
				if code := exitCodeOf(err); code != 1 {
					t.Errorf("%s: exit code %d, want 1", step.name, code)
				}
				if after, err := os.ReadFile("apm.yml"); err != nil || string(after) != string(before) {
					t.Errorf("%s: apm.yml changed (err %v):\n%s", step.name, err, after)
				}
				for _, p := range []string{"apm.lock.yaml", "apm_modules", ".claude"} {
					if _, err := os.Lstat(p); !os.IsNotExist(err) {
						t.Errorf("%s: %s exists (err %v)", step.name, p, err)
					}
				}
			}
		})
	}
}

// A path that starts with two slashes is not a local path in either form, as
// in the Oracle (models/dependency/reference.py:328-348, :1829-1832); every
// command stops before it writes anything.
func TestDoubleSlashPathIsRefused(t *testing.T) {
	const notLocal = "Object-style dependency must have a 'git' field, or 'path' must be a local filesystem path (starting with './', '../', '/', or '~')"
	tests := []struct {
		name, entry string
		want        func(pkg string) string
	}{
		{"string form", "    - %s\n", func(string) string {
			return "validate apm.yml: Protocol-relative URLs are not supported\n\nInvalid Git host: '//...'.\n"
		}},
		{"path form", "    - path: %s\n", func(pkg string) string {
			return `validate apm.yml: dependency entry 0 (path: "` + pkg + `"): ` + notLocal
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chdirTemp(t)
			actual := filepath.Join(t.TempDir(), "dep")
			pkg := "//" + strings.TrimPrefix(filepath.ToSlash(actual), "/")
			writeTwoSkillDep(t, actual, strings.Replace(tt.entry, "%s", pkg, 1))
			before, err := os.ReadFile("apm.yml")
			if err != nil {
				t.Fatal(err)
			}
			install := func(skills, packages []string) error {
				deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
				return runInstall(deps, false, false, "", "", skills, packages)
			}
			steps := []struct {
				name string
				run  func() error
			}{
				{"install", func() error { return install(nil, nil) }},
				{"install <path> --skill sk", func() error { return install([]string{"sk"}, []string{pkg}) }},
				{"uninstall <path>", func() error { return runUninstall([]string{pkg}, uninstallOptions{}) }},
			}
			for _, step := range steps {
				err := step.run()
				if err == nil || !strings.HasPrefix(err.Error(), tt.want(pkg)) {
					t.Errorf("%s: error %v, want it to start with %s", step.name, err, tt.want(pkg))
				}
				if code := exitCodeOf(err); code != 1 {
					t.Errorf("%s: exit code %d, want 1", step.name, code)
				}
				if after, err := os.ReadFile("apm.yml"); err != nil || string(after) != string(before) {
					t.Errorf("%s: apm.yml changed (err %v):\n%s", step.name, err, after)
				}
				for _, p := range []string{"apm.lock.yaml", "apm_modules", ".claude"} {
					if _, err := os.Lstat(p); !os.IsNotExist(err) {
						t.Errorf("%s: %s exists (err %v)", step.name, p, err)
					}
				}
			}
		})
	}
}

// Whitespace around a path is not part of it in either form, as in the
// Oracle (models/dependency/reference.py:1812-1814): `./dep ` is ./dep, also
// with a directory `dep ` next to it, and `install ./dep --skill` updates
// that entry instead of adding a second one and writes the path back without
// the whitespace, the bytes the Oracle (18c4c43) wrote.
func TestRunInstall_PaddedPathEntryIsTheUnpaddedPackage(t *testing.T) {
	chdirTemp(t)
	writeSkill := func(pkg, name string) {
		t.Helper()
		dir := filepath.Join(pkg, ".apm", "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		text := "---\nname: " + name + "\ndescription: skill " + name + "\n---\nfrom [" + pkg + "]\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	packages := []string{"dep"}
	if runtime.GOOS != "windows" {
		packages = append(packages, "dep ")
	}
	for _, pkg := range packages {
		writeSkill(pkg, "sk")
		if err := os.WriteFile(filepath.Join(pkg, "apm.yml"), []byte("name: dep\nversion: 1.0.0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill("dep", "b")
	const head = "name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n"
	const plain = head + "    - './dep '\n"
	const subset = head + "    - path: ./dep\n      skills:\n        - sk\n"
	const reset = head + "    - ./dep\n"
	if err := os.WriteFile("apm.yml", []byte(plain), 0o644); err != nil {
		t.Fatal(err)
	}
	after := func(step, wantManifest string, wantSkills ...string) {
		t.Helper()
		got, err := os.ReadFile("apm.yml")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != wantManifest {
			t.Fatalf("apm.yml after %s:\n%s\nwant:\n%s", step, got, wantManifest)
		}
		if names := deployedClaudeSkills(t); strings.Join(names, ",") != strings.Join(wantSkills, ",") {
			t.Errorf("%s: .claude/skills = %v, want %v", step, names, wantSkills)
		}
		sk, err := os.ReadFile(filepath.Join(".claude", "skills", "sk", "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(sk), "from [dep]") {
			t.Fatalf("%s: deployed sk is not the one of `dep`:\n%s", step, sk)
		}
	}

	installLocalStep(t, "install", nil, nil)
	after("install", plain, "b", "sk")
	installLocalStep(t, "install ./dep --skill sk", []string{"sk"}, []string{"./dep"})
	after("--skill sk", subset, "sk")
	installLocalStep(t, "install after --skill sk", nil, nil)
	after("install after --skill sk", subset, "sk")
	installLocalStep(t, "install ./dep --skill '*'", []string{"*"}, []string{"./dep"})
	after("--skill '*'", reset, "b", "sk")
	installLocalStep(t, "install after the reset", nil, nil)
	after("install after the reset", reset, "b", "sk")
}
