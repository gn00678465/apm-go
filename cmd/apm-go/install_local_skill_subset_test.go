package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// writeTwoSkillDep writes the local package dep/ with the skills sk and b, and
// the project apm.yml with the given dependencies.apm entry.
func writeTwoSkillDep(t *testing.T, entry string) {
	t.Helper()
	for _, name := range []string{"sk", "b"} {
		dir := filepath.Join("dep", ".apm", "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + name + "\ndescription: skill " + name + "\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join("dep", "apm.yml"), []byte("name: dep\nversion: 1.0.0\n"), 0o644); err != nil {
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
	writeTwoSkillDep(t, "  - ./dep\n")

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

// A reset must leave an apm.yml the next install can read: `path: dep` as the
// plain string `dep` does not parse.
func TestRunInstall_ResetKeepsPathEntryWithoutLocalPrefix(t *testing.T) {
	chdirTemp(t)
	writeTwoSkillDep(t, "    - path: dep\n")

	installLocalStep(t, "install", nil, nil)
	installLocalStep(t, "install ./dep --skill sk", []string{"sk"}, []string{"./dep"})
	installLocalStep(t, "install ./dep --skill '*'", []string{"*"}, []string{"./dep"})
	installLocalStep(t, "plain install after the reset", nil, nil)

	if names := deployedClaudeSkills(t); len(names) != 2 || names[0] != "b" || names[1] != "sk" {
		t.Errorf(".claude/skills = %v, want [b sk]", names)
	}
	const want = "name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n    - path: dep\n"
	if got, err := os.ReadFile("apm.yml"); err != nil || string(got) != want {
		t.Errorf("apm.yml (err %v):\n%s\nwant:\n%s", err, got, want)
	}
}
