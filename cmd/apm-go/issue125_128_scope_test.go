package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// c47-3: with no target, install ends before any package is fetched or
// copied, as Oracle 0.33.0 ends it: no Resolving line, no apm_modules/, and
// for install <pkg> the line that apm.yml was put back (measured with Oracle
// 0.33.0, apm-go-oracle-ref/d/c47-3/before1 A-D and before2 E, H).
func TestIssue125_NoTargetEndsBeforeFetch(t *testing.T) {
	declared := func(s i125Scope) { s.manifest(t, "", s.dep) }
	empty := func(s i125Scope) {
		writeCompileFile(t, s.proj, "apm.yml", "name: proj\nversion: 1.0.0\ndependencies:\n  apm: []\n")
	}
	cases := []struct {
		name     string
		setup    func(i125Scope)
		pkg      bool   // install <dep> instead of install
		want     string // a run of lines stdout holds
		restored bool
	}{
		{"declared package", declared, false, " > Installing dependencies from apm.yml...\n x no deployment target detected", false},
		{"new package", empty, true, " + Updated apm.yml with 1 new package(s)\n > Installing 1 new package...\n i apm.yml restored to its previous state.\n x no deployment target detected", true},
		{"no apm.yml", func(i125Scope) {}, true, " + Updated apm.yml with 1 new package(s)\n > Installing 1 new package...\n i Removed apm.yml created by the failed install.\n x no deployment target detected", false},
		{"package already declared", declared, true, " + Updated apm.yml dependency entries\n > Installing 1 new package...\n x no deployment target detected", false},
		{"declared package that is missing", func(s i125Scope) { s.manifest(t, "", filepath.Join(s.root, "missing")) }, false, " > Installing dependencies from apm.yml...\n x no deployment target detected", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newI125Scope(t)
			c.setup(s)
			before, _ := os.ReadFile(filepath.Join(s.proj, "apm.yml"))
			args := []string{"install"}
			if c.pkg {
				args = append(args, s.dep)
			}
			stdout, _, code := run125(t, args...)
			if code != 2 || !strings.Contains(stdout, c.want) || strings.Contains(stdout, "Resolving") {
				t.Errorf("exit %d, want 2 and %q, stdout:\n%s", code, c.want, stdout)
			}
			if c.restored != strings.Contains(stdout, "restored") {
				t.Errorf("restored line: want %v, stdout:\n%s", c.restored, stdout)
			}
			if i125Exists(filepath.Join(s.proj, "apm_modules")) {
				t.Error("apm_modules/ was made")
			}
			after, _ := os.ReadFile(filepath.Join(s.proj, "apm.yml"))
			if string(after) != string(before) {
				t.Errorf("apm.yml changed:\n%s", after)
			}
		})
	}
}

// install <path> of a local path that does not exist fails its validation, as
// in Oracle 0.33.0 (install/validation.py:155-164; measured, before2 H).
func TestIssue125_MissingLocalPathFailsValidation(t *testing.T) {
	s := newI125Scope(t)
	s.mkdirs(t, s.proj, ".claude")
	writeCompileFile(t, s.proj, "apm.yml", "name: proj\nversion: 1.0.0\n")
	stdout, _, code := run125(t, "install", "../missing")
	want := " + Validating 1 package...\n x ../missing -- path does not exist\nAll packages failed validation. Nothing to install.\n"
	if code != 1 || stdout != want {
		t.Errorf("exit %d, stdout:\n%s\nwant:\n%s", code, stdout, want)
	}
}

// 46: uninstall -g starts with the Oracle's line
// (commands/uninstall/cli.py:285-286; measured, apm-go-oracle-ref/d/46/before1).
func TestIssue125_UninstallGlobalScopeLine(t *testing.T) {
	s := newI125Scope(t)
	s.mkdirs(t, s.home, ".claude")
	if stdout, _, code := run125(t, "install", "-g", s.dep); code != 0 {
		t.Fatalf("install -g: exit %d, stdout:\n%s", code, stdout)
	}
	stdout, _, code := run125(t, "uninstall", "-g", s.dep)
	if code != 0 || !strings.HasPrefix(stdout, " i Uninstalling from user scope (~/.apm/)\n > Uninstalling 1 package(s)...\n") {
		t.Errorf("exit %d, stdout:\n%s", code, stdout)
	}
	if stdout, _, _ := run125(t, "uninstall", s.dep); strings.Contains(stdout, "user scope") {
		t.Errorf("a project uninstall names the user scope:\n%s", stdout)
	}
}

const i128Reason = "relative local paths are not supported at user scope (--global). Use an absolute path or a remote reference (owner/repo) instead"

// 128 c49-4: a relative local path on the command line of install -g is
// refused, as Oracle 0.33.0 refuses it (install/package_resolution.py:113-137;
// measured, apm-go-oracle-ref/d/128/before1 P, P2, Q).
func TestIssue128_UserScopeRelativePathOnCommandLine(t *testing.T) {
	for _, arg := range []string{"./dep", "../../proj/dep"} {
		t.Run(arg, func(t *testing.T) {
			s := newI125Scope(t)
			s.mkdirs(t, s.home, ".claude")
			i125Package(t, filepath.Join(s.proj, "dep"), "dep")
			stdout, _, code := run125(t, "install", "-g", arg)
			want := " + Validating 1 package...\n x " + arg + " -- " + i128Reason + "\nAll packages failed validation. Nothing to install.\n i Removed apm.yml created by the failed install.\n"
			if code != 1 || !strings.HasSuffix(stdout, want) {
				t.Errorf("exit %d, stdout:\n%s\nwant suffix:\n%s", code, stdout, want)
			}
			for _, p := range []string{".apm/apm.yml", ".apm/apm_modules", ".claude/agents"} {
				if i125Exists(filepath.Join(s.home, p)) {
					t.Errorf("%s was written", p)
				}
			}
		})
	}
}

// 128 c43-1: a relative local path in ~/.apm/apm.yml is refused before
// anything is fetched or written, by install -g, a frozen one and update -g.
// Oracle 0.33.0 prints its Resolving line, skips the package and ends with
// "No changes" (measured, 128/before1 R, U); apm-go refuses it as the command
// line refuses it. A path below ~ stays absolute (measured, S).
func TestIssue128_UserScopeRelativePathInManifest(t *testing.T) {
	for _, entry := range []string{"- ./m", "- path: ./m", "- git: ./m"} {
		for _, args := range [][]string{{"install", "-g"}, {"install", "-g", "--frozen"}, {"update", "-g"}} {
			t.Run(entry+" "+strings.Join(args, " "), func(t *testing.T) {
				s := newI125Scope(t)
				s.mkdirs(t, s.home, ".claude")
				i125Package(t, filepath.Join(s.home, ".apm", "m"), "m")
				writeCompileFile(t, s.home, ".apm/apm.yml", "name: home\nversion: 1.0.0\ndependencies:\n  apm:\n    "+entry+"\n")
				stdout, _, code := run125(t, args...)
				if code != 1 || !strings.Contains(stdout, ` x validate apm.yml: local dependency "./m": `+i128Reason+"\n") {
					t.Errorf("exit %d, stdout:\n%s", code, stdout)
				}
				for _, p := range []string{".apm/apm.lock.yaml", ".apm/apm_modules", ".claude/agents"} {
					if i125Exists(filepath.Join(s.home, p)) {
						t.Errorf("%s was written", p)
					}
				}
			})
		}
	}
	t.Run("~/m", func(t *testing.T) {
		s := newI125Scope(t)
		s.mkdirs(t, s.home, ".claude")
		i125Package(t, filepath.Join(s.home, "m"), "m")
		writeCompileFile(t, s.home, ".apm/apm.yml", "name: home\nversion: 1.0.0\ndependencies:\n  apm:\n    - ~/m\n")
		if stdout, _, code := run125(t, "install", "-g"); code != 0 || !i125Exists(filepath.Join(s.home, ".claude/agents/ag-m.md")) {
			t.Errorf("exit %d, stdout:\n%s", code, stdout)
		}
	})
}
