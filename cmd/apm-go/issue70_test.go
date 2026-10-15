package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The expected transcripts below are what Oracle v0.33.0 printed for the same
// projects, with apm-go's status symbols and without the Oracle's line break
// at column 80.

// uninstallLocalTranscript is TestUninstallLocal_StdoutVerbatim's transcript:
// the Oracle prints it word for word for `uninstall _local/dep` too.
func uninstallLocalTranscript(manifestPath, section string) string {
	return " > Uninstalling 1 package(s)...\n" +
		" + _local/dep - found in apm.yml\n" +
		" i Removed _local/dep from apm_modules/\n" +
		" i Cleaned 1 stale file from ./dep\n" +
		" i Removed _local/dep from " + section + " in apm.yml\n" +
		" + Updated " + manifestPath + " (removed 1 package(s))\n" +
		" + Uninstall complete: Removed 1 package(s) from apm.yml, Removed 1 package(s) from apm_modules/\n"
}

// installedLocal70 copies the uninstall-local fixture, declares ./dep through
// deps (the dependency block of apm.yml), installs it, and returns the
// project directory and the absolute path of its apm.yml.
func installedLocal70(t *testing.T, deps string) (dir, manifestPath string) {
	t.Helper()
	installParityFixture(t, "uninstall-local")
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	writeCompileFile(t, dir, "apm.yml", "name: uninstall-fixture\nversion: 1.0.0\ntarget: claude\n"+deps)
	mustRunCLI(t, "install")
	return dir, filepath.Join(dir, "apm.yml")
}

const deps70 = "dependencies:\n  apm:\n  - ./dep\n"

// Each form of the declaration: the alias removes what the declared path
// removes, and the Oracle prints the same transcript for both.
func TestUninstall_LocalKeyAlias_SameAsDeclaredPath(t *testing.T) {
	tests := []struct{ name, deps, section string }{
		{"string form", deps70, "dependencies.apm"},
		{"object form", "dependencies:\n  apm:\n  - path: ./dep\n", "dependencies.apm"},
		{"devDependencies", "devDependencies:\n  apm:\n  - ./dep\n", "devDependencies.apm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var wantTree map[string]string
			t.Run("by path", func(t *testing.T) {
				dir, _ := installedLocal70(t, tt.deps)
				mustRunCLI(t, "uninstall", "./dep")
				wantTree = tree86(t, dir)
			})

			dir, manifestPath := installedLocal70(t, tt.deps)
			stdout, stderr, exitCode := runCompileCLI(t, "uninstall", "_local/dep")

			if exitCode != 0 || stderr != "" {
				t.Errorf("exit = %d, stderr = %q", exitCode, stderr)
			}
			if want := uninstallLocalTranscript(manifestPath, tt.section); stdout != want {
				t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
			}
			if got := tree86(t, dir); !reflect.DeepEqual(got, wantTree) {
				t.Errorf("tree after uninstall _local/dep:\n%v\nwant (uninstall ./dep):\n%v", got, wantTree)
			}
			for _, gone := range []string{"apm.lock.yaml", "apm_modules/_local/dep", ".claude"} {
				if _, err := os.Lstat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
					t.Errorf("%s still exists (err %v)", gone, err)
				}
			}
		})
	}
}

func TestUninstall_LocalKeyAlias_DryRun(t *testing.T) {
	dir, _ := installedLocal70(t, deps70)
	before := tree86(t, dir)

	stdout, stderr, exitCode := runCompileCLI(t, "uninstall", "--dry-run", "_local/dep")

	want := " > Uninstalling 1 package(s)...\n" +
		" + _local/dep - found in apm.yml\n" +
		" i Dry run: Would remove 1 package(s):\n" +
		" i   - _local/dep from apm.yml\n" +
		" i   - _local/dep from apm_modules/\n" +
		" + Dry run complete - no changes made\n"
	if exitCode != 0 || stderr != "" {
		t.Errorf("exit = %d, stderr = %q", exitCode, stderr)
	}
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
	assertTree86(t, dir, before)
}

// The Oracle accepts the alias in the user scope too, where a local package
// can only be declared by its absolute path.
func TestUninstallGlobal_LocalKeyAlias(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	src := filepath.Join(root, "src", "my-local-skill")
	writeCompileFile(t, src, "apm.yml", "name: my-local-skill\nversion: 1.0.0\n")
	writeCompileFile(t, src, "SKILL.md", "---\nname: my-local-skill\ndescription: d\n---\nx\n")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(home)
	mustRunCLI(t, "install", "--global", "--target", "claude", src)

	stdout, stderr, exitCode := runCompileCLI(t, "uninstall", "--global", "_local/my-local-skill")

	want := " > Uninstalling 1 package(s)...\n" +
		" + _local/my-local-skill - found in apm.yml\n" +
		" i Removed _local/my-local-skill from apm_modules/\n" +
		" i Cleaned 3 stale files from " + src + "\n" +
		" i Removed _local/my-local-skill from dependencies.apm in apm.yml\n" +
		" + Updated " + filepath.Join(home, ".apm", "apm.yml") + " (removed 1 package(s))\n" +
		" + Uninstall complete: Removed 1 package(s) from apm.yml, Removed 1 package(s) from apm_modules/\n"
	if exitCode != 0 || stderr != "" {
		t.Errorf("exit = %d, stderr = %q", exitCode, stderr)
	}
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
	for _, gone := range []string{".apm/apm_modules/_local/my-local-skill", ".claude/skills/my-local-skill"} {
		if _, err := os.Lstat(filepath.Join(home, gone)); !os.IsNotExist(err) {
			t.Errorf("%s still exists (err %v)", gone, err)
		}
	}
}

// The alias stands only through a lock entry with source local whose path is
// declared in apm.yml. Anything else is the ruling's not-found warning with
// exit 0, and no file changes.
func TestUninstall_LocalKeyAlias_NotMapped(t *testing.T) {
	tests := []struct {
		name  string
		arg   string
		setup func(t *testing.T, dir string)
	}{
		{"unknown name", "_local/nope", func(*testing.T, string) {}},
		{"no lock", "_local/dep", func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, "apm.lock.yaml")); err != nil {
				t.Fatal(err)
			}
		}},
		{"not declared in apm.yml", "_local/dep", func(t *testing.T, dir string) {
			writeCompileFile(t, dir, "apm.yml", "name: uninstall-fixture\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm: []\n")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, _ := installedLocal70(t, deps70)
			tt.setup(t, dir)
			before := tree86(t, dir)

			stdout, stderr, exitCode := runCompileCLI(t, "uninstall", tt.arg)

			want := " > Uninstalling 1 package(s)...\n" +
				" ! \"" + tt.arg + "\": not found in apm.yml\n" +
				" i No packages found in apm.yml to remove\n"
			if exitCode != 0 || stderr != "" {
				t.Errorf("exit = %d, stderr = %q", exitCode, stderr)
			}
			if stdout != want {
				t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
			}
			assertTree86(t, dir, before)
		})
	}
}

// A package that several arguments select is found once per argument and
// removed and counted once, as the Oracle does (commands/uninstall/
// engine.py:630-731).
func TestUninstall_SamePackageTwice_RemovedAndCountedOnce(t *testing.T) {
	for _, args := range [][]string{{"_local/dep", "./dep"}, {"./dep", "./dep"}} {
		t.Run(args[0]+" "+args[1], func(t *testing.T) {
			t.Run("dry run", func(t *testing.T) {
				dir, _ := installedLocal70(t, deps70)
				before := tree86(t, dir)

				stdout, stderr, exitCode := runCompileCLI(t, append([]string{"uninstall", "--dry-run"}, args...)...)

				want := " > Uninstalling 2 package(s)...\n" +
					" + _local/dep - found in apm.yml\n" +
					" + _local/dep - found in apm.yml\n" +
					" i Dry run: Would remove 1 package(s):\n" +
					" i   - _local/dep from apm.yml\n" +
					" i   - _local/dep from apm_modules/\n" +
					" + Dry run complete - no changes made\n"
				if exitCode != 0 || stderr != "" {
					t.Errorf("exit = %d, stderr = %q", exitCode, stderr)
				}
				if stdout != want {
					t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
				}
				assertTree86(t, dir, before)
			})

			var wantTree map[string]string
			t.Run("one argument", func(t *testing.T) {
				dir, _ := installedLocal70(t, deps70)
				mustRunCLI(t, "uninstall", "./dep")
				wantTree = tree86(t, dir)
			})

			dir, manifestPath := installedLocal70(t, deps70)
			stdout, stderr, exitCode := runCompileCLI(t, append([]string{"uninstall"}, args...)...)

			want := " > Uninstalling 2 package(s)...\n" +
				" + _local/dep - found in apm.yml\n" +
				" + _local/dep - found in apm.yml\n" +
				" i Removed _local/dep from apm_modules/\n" +
				" i Cleaned 1 stale file from ./dep\n" +
				" i Removed _local/dep from dependencies.apm in apm.yml\n" +
				" + Updated " + manifestPath + " (removed 1 package(s))\n" +
				" + Uninstall complete: Removed 1 package(s) from apm.yml, Removed 1 package(s) from apm_modules/\n"
			if exitCode != 0 || stderr != "" {
				t.Errorf("exit = %d, stderr = %q", exitCode, stderr)
			}
			if stdout != want {
				t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
			}
			if got := tree86(t, dir); !reflect.DeepEqual(got, wantTree) {
				t.Errorf("tree:\n%v\nwant (uninstall ./dep):\n%v", got, wantTree)
			}
		})
	}
}

// The same rule for an MCP server name, which only apm-go uninstalls.
func TestUninstall_SameMCPServerTwice_RemovedAndCountedOnce(t *testing.T) {
	twoServerProject(t)
	manifestPath := mcpCoverManifestPath(t)

	wantDryRun := " > Uninstalling 2 package(s)...\n" +
		" + one - found in apm.yml\n" +
		" + one - found in apm.yml\n" +
		" i Dry run: Would remove 1 package(s):\n" +
		" i   - one from apm.yml\n" +
		" + Dry run complete - no changes made\n"
	if got := mustRunCLI(t, "uninstall", "--dry-run", "one", "one"); got != wantDryRun {
		t.Errorf("uninstall --dry-run one one:\n%s\nwant:\n%s", got, wantDryRun)
	}

	want := " > Uninstalling 2 package(s)...\n" +
		" + one - found in apm.yml\n" +
		" + one - found in apm.yml\n" +
		" i Removed one from dependencies.mcp in apm.yml\n" +
		" + Updated " + manifestPath + " (removed 1 package(s))\n" +
		" + Uninstall complete: Removed 1 package(s) from apm.yml\n"
	if got := mustRunCLI(t, "uninstall", "one", "one"); got != want {
		t.Errorf("uninstall one one:\n%s\nwant:\n%s", got, want)
	}
}
