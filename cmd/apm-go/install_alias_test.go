package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The expected paths and lock lines are Oracle 0.33.0's for the same
// apm.yml (measured: issue151/r3-or/alias, aliaslocal, issue151-impl/al1-or,
// al2-or): an alias names the package's directory below apm_modules and its
// root skill, and the lock records it beside the unchanged repo_url.

func aliasInstall(t *testing.T, frozen bool) error {
	t.Helper()
	return subdirInstall(t, subdirPathLoader{subdirRealLoader()}, frozen)
}

func mustExist(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}

func mustNotExist(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("%s exists", p)
		}
	}
}

func TestInstall_AliasNamesDirectoryAndRootSkill(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	subdirGitRepo(t, filepath.Join(project, "acme", "skill"), map[string]string{"SKILL.md": gitVersionSkill})
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - git: acme/skill\n      alias: custom\n")
	if err := aliasInstall(t, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	mustExist(t, "apm_modules/custom/SKILL.md", ".claude/skills/custom/SKILL.md")
	mustNotExist(t, "apm_modules/acme", ".claude/skills/skill")
	entry := subdirLockEntry(t, project, "acme/skill")
	if entry.Alias != "custom" {
		t.Errorf("alias = %q, want custom", entry.Alias)
	}
	lock := globalStaleRead(t, filepath.Join(project, "apm.lock.yaml"))
	if !strings.Contains(lock, "- repo_url: acme/skill\n  alias: custom\n") {
		t.Errorf("lock has no alias after repo_url:\n%s", lock)
	}

	// A fresh clone restores from the lock into the alias's directory.
	aside := t.TempDir()
	for _, p := range []string{"apm_modules", ".claude"} {
		if err := os.Rename(p, filepath.Join(aside, p)); err != nil {
			t.Fatal(err)
		}
	}
	if err := aliasInstall(t, true); err != nil {
		t.Fatalf("frozen install: %v", err)
	}
	mustExist(t, "apm_modules/custom/SKILL.md", ".claude/skills/custom/SKILL.md")
	if got := globalStaleRead(t, filepath.Join(project, "apm.lock.yaml")); got != lock {
		t.Errorf("frozen install changed the lock:\n%s", got)
	}

	// commands/_helpers.py:404-412: update takes the alias for the package.
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: subdirPathLoader{subdirRealLoader()}}
	var err error
	captureUninstallStdout(t, func() { err = runUpdate(deps, false, false, "custom", false, "") })
	if err != nil {
		t.Errorf("update custom: %v", err)
	}
}

// Taking the alias away moves the skill back to the package's own name and
// leaves the alias's directory in apm_modules, as Oracle 0.33.0 does
// (issue151-impl/al2-or/aliaschg i4).
func TestInstall_AliasRemovedMovesSkillBack(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	subdirGitRepo(t, filepath.Join(project, "acme", "skill"), map[string]string{"SKILL.md": gitVersionSkill})
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - git: acme/skill\n      alias: bb\n")
	if err := aliasInstall(t, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - git: acme/skill\n")
	if err := aliasInstall(t, false); err != nil {
		t.Fatalf("install without alias: %v", err)
	}
	mustExist(t, ".claude/skills/skill/SKILL.md", "apm_modules/acme/skill/SKILL.md", "apm_modules/bb/SKILL.md")
	mustNotExist(t, ".claude/skills/bb")
	if entry := subdirLockEntry(t, project, "acme/skill"); entry.Alias != "" {
		t.Errorf("alias = %q, want none", entry.Alias)
	}
}

func TestInstall_AliasLocalPackage(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	writeGlobalStaleFile(t, filepath.Join(project, "dep", "SKILL.md"), "---\nname: dep\ndescription: d\n---\nx\n")
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - path: ./dep\n      alias: ldep\n")
	if err := aliasInstall(t, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	mustExist(t, "apm_modules/ldep/SKILL.md", ".claude/skills/ldep/SKILL.md")
	mustNotExist(t, "apm_modules/_local")
	if entry := subdirLockEntry(t, project, "_local/dep"); entry.Alias != "ldep" {
		t.Errorf("alias = %q, want ldep", entry.Alias)
	}
}

// models/dependency/materialization.py:117-129: an alias whose directory
// resolves to apm_modules itself, or out of it, is refused before anything
// is written there.
func TestInstall_AliasDirectoryRefused(t *testing.T) {
	cases := []struct {
		name, link, want string
	}{
		{"apm_modules itself", ".", "Invalid dependency alias 'custom': destination resolves to apm_modules itself. Choose a separate package directory and rerun apm-go install."},
		{"outside", "", "Invalid dependency alias 'custom': unsafe destination "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APM_CONFIG_DIR", t.TempDir())
			project := chdirTemp(t)
			subdirGitRepo(t, filepath.Join(project, "acme", "skill"), map[string]string{"SKILL.md": gitVersionSkill})
			if err := os.MkdirAll(filepath.Join(project, "apm_modules"), 0o755); err != nil {
				t.Fatal(err)
			}
			outside, link := t.TempDir(), tc.link
			if link == "" {
				link = outside
			}
			if err := os.Symlink(link, filepath.Join(project, "apm_modules", "custom")); err != nil {
				t.Skip("cannot make a symlink:", err)
			}
			writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - git: acme/skill\n      alias: custom\n")
			err := aliasInstall(t, false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			mustNotExist(t, "apm.lock.yaml", ".claude", filepath.Join(outside, "SKILL.md"))
		})
	}
}

// A lock an apm-go before issue #151 wrote records no alias although apm.yml
// declares one, and its package is at apm_modules/<key>: a frozen install of
// a fresh clone restores what that lock records, as before the upgrade.
func TestFrozenInstall_LockWithoutAliasRestoresItsDirectory(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	subdirGitRepo(t, filepath.Join(project, "acme", "skill"), map[string]string{"SKILL.md": gitVersionSkill})
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - git: acme/skill\n")
	if err := aliasInstall(t, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	lock := globalStaleRead(t, filepath.Join(project, "apm.lock.yaml"))
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - git: acme/skill\n      alias: custom\n")
	aside := t.TempDir()
	for _, p := range []string{"apm_modules", ".claude"} {
		if err := os.Rename(p, filepath.Join(aside, p)); err != nil {
			t.Fatal(err)
		}
	}
	if err := aliasInstall(t, true); err != nil {
		t.Fatalf("frozen install: %v", err)
	}
	mustExist(t, "apm_modules/acme/skill/SKILL.md", ".claude/skills/skill/SKILL.md")
	mustNotExist(t, "apm_modules/custom", ".claude/skills/custom")
	if got := globalStaleRead(t, filepath.Join(project, "apm.lock.yaml")); got != lock {
		t.Errorf("frozen install changed the lock:\n%s", got)
	}
}

// treeHashes is every file below dir, .git included, with its bytes.
func treeHashes(t *testing.T, dir string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		got[p] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// A lock whose alias names another package's directory is refused before
// anything is fetched or written, as Oracle 0.33.0 refuses it
// (deps/lockfile.py:286-296; measured: issue151-impl/r14-la-or/v1), so the
// other package's checkout stays as it was.
func TestFrozenInstall_LockAliasOfAnotherPackageTouchesNothing(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	subdirGitRepo(t, filepath.Join(project, "acme", "pkg"), map[string]string{"apm.yml": "name: pkg\nversion: 1.0.0\n", ".apm/agents/a.agent.md": "---\nname: a\ndescription: d\n---\nx\n"})
	subdirGitRepo(t, filepath.Join(project, "acme", "skill"), map[string]string{"SKILL.md": gitVersionSkill})
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - git: acme/pkg\n    - git: acme/skill\n")
	if err := aliasInstall(t, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	lockPath := filepath.Join(project, "apm.lock.yaml")
	lock := globalStaleRead(t, lockPath)
	tampered := strings.Replace(lock, "- repo_url: acme/pkg\n", "- repo_url: acme/pkg\n  alias: acme/skill\n", 1)
	if tampered == lock {
		t.Fatalf("no acme/pkg entry:\n%s", lock)
	}
	writeGlobalStaleFile(t, lockPath, tampered)
	before := treeHashes(t, "apm_modules/acme/skill")
	err := aliasInstall(t, true)
	want := "validate apm.lock.yaml: Invalid lockfile dependency alias 'acme/skill'. Restore a known-good apm.lock.yaml from version control or correct this entry's alias to its intended placement. Do not delete content at a rejected alias destination."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	after := treeHashes(t, "apm_modules/acme/skill")
	if len(after) != len(before) {
		t.Errorf("apm_modules/acme/skill had %d files, has %d", len(before), len(after))
	}
	for p, b := range before {
		if after[p] != b {
			t.Errorf("%s changed", p)
		}
	}
	if globalStaleRead(t, lockPath) != tampered {
		t.Error("the lock was written")
	}
}

// uninstall removes a direct package from the directory its apm.yml entry
// places it in, and a lock alias with whitespace around it is written back
// stripped (commands/uninstall/engine.py:750-751,834-835; measured:
// issue151-impl/r14-la-or/v7/uninstall, r14-la2-or).
func TestUninstall_DirectPackageDirectoryFromApmYml(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	subdirGitRepo(t, filepath.Join(project, "acme", "pkg"), map[string]string{"apm.yml": "name: pkg\nversion: 1.0.0\n", ".apm/agents/a.agent.md": "---\nname: a\ndescription: d\n---\nx\n"})
	subdirGitRepo(t, filepath.Join(project, "acme", "skill"), map[string]string{"SKILL.md": gitVersionSkill})
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - git: acme/pkg\n    - git: acme/skill\n      alias: x\n")
	if err := aliasInstall(t, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	lockPath := filepath.Join(project, "apm.lock.yaml")
	lock := globalStaleRead(t, lockPath)
	tampered := strings.Replace(strings.Replace(lock, "  alias: x\n", "  alias: '  x  '\n", 1), "- repo_url: acme/pkg\n", "- repo_url: acme/pkg\n  alias: elsewhere\n", 1)
	writeGlobalStaleFile(t, lockPath, tampered)
	var err error
	captureUninstallStdout(t, func() { err = runUninstall([]string{"acme/pkg"}, uninstallOptions{}) })
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	mustNotExist(t, "apm_modules/acme/pkg")
	mustExist(t, "apm_modules/x/SKILL.md")
	if got := globalStaleRead(t, lockPath); !strings.Contains(got, "  alias: x\n") {
		t.Errorf("lock does not record alias x:\n%s", got)
	}
}
