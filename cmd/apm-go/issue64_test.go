package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #64: local paths above the project root, paths that name no
// directory, and sources that hold the project.

// upDepProject makes root/proj, with apm.yml declaring deps, the working
// directory, and puts a package with one skill at root/dep. It returns root.
func upDepProject(t *testing.T, deps string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range map[string]string{
		"proj/apm.yml":                "name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n" + deps,
		"dep/apm.yml":                 "name: dep\nversion: 1.0.0\n",
		"dep/.apm/skills/sk/SKILL.md": "---\nname: sk\ndescription: a skill\n---\nbody\n",
	} {
		writeCompileFile(t, root, rel, content)
	}
	t.Chdir(filepath.Join(root, "proj"))
	return root
}

func assertLockRecordsUpDep(t *testing.T) {
	t.Helper()
	lock, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"repo_url: _local/dep\n", "source: local\n", "local_path: ../dep\n"} {
		if !strings.Contains(string(lock), line) {
			t.Errorf("apm.lock.yaml has no %q:\n%s", line, lock)
		}
	}
	assertIssue50Exists(t, true, "apm_modules/_local/dep/apm.yml", ".claude/skills/sk/SKILL.md")
}

// Oracle v0.33.0 installs a sibling of the project declared with `../`, in
// both forms, and records it as a local package.
func TestInstall_ParentDirLocalPackage(t *testing.T) {
	for _, entry := range []string{"    - ../dep\n", "    - path: ../dep\n"} {
		t.Run(strings.TrimSpace(entry), func(t *testing.T) {
			upDepProject(t, "  apm:\n"+entry)
			mustRunCLI(t, "install")
			assertLockRecordsUpDep(t)
		})
	}
}

// Oracle v0.33.0: `install ../dep` writes `- ../dep`, and `uninstall ../dep`
// returns apm.yml to `apm: []` and removes the copy and the deployed skill.
func TestInstallUninstall_ParentDirLocalPackageFromCLI(t *testing.T) {
	upDepProject(t, "  apm: []\n")

	mustRunCLI(t, "install", "../dep")
	manifestBytes, err := os.ReadFile("apm.yml")
	if err != nil || !strings.HasSuffix(string(manifestBytes), "  apm:\n    - ../dep\n") {
		t.Errorf("apm.yml after install (err %v):\n%s", err, manifestBytes)
	}
	assertLockRecordsUpDep(t)

	mustRunCLI(t, "uninstall", "../dep")
	manifestBytes, err = os.ReadFile("apm.yml")
	if err != nil || !strings.HasSuffix(string(manifestBytes), "  apm: []\n") {
		t.Errorf("apm.yml after uninstall (err %v):\n%s", err, manifestBytes)
	}
	assertIssue50Exists(t, false, "apm_modules/_local/dep", ".claude/skills/sk")
}

// Oracle v0.33.0 refuses a local path whose last component is not a name
// before anything is copied.
func TestInstall_LocalPathNamingNoDirectory_Refused(t *testing.T) {
	tests := []struct{ entry, path string }{
		{"- path: ./.", "./."},
		{"- ./", "./"},
		{"- path: /", "/"},
	}
	for _, tt := range tests {
		t.Run(tt.entry, func(t *testing.T) {
			upDepProject(t, "  apm:\n    "+tt.entry+"\n")
			stdout, _, exitCode := runCompileCLI(t, "install")
			want := "Local path '" + tt.path + "' does not resolve to a named directory. " +
				"Use a path that ends with a directory name (e.g., './my-package' instead of './').\n"
			if exitCode != 1 || !strings.HasSuffix(stdout, want) {
				t.Errorf("exit = %d; stdout:\n%s\nwant exit 1 and it to end with:\n%s", exitCode, stdout, want)
			}
			assertIssue50Exists(t, false, "apm_modules")
		})
	}
}

// A source that is the project directory or holds it is refused before any
// write: no copy, no lock, no deploy, no .gitignore, apm.yml as it was.
// Deliberate deviation: the Oracle copies the project into itself until the
// path is too long, exits 1 and leaves .claude/ behind.
func TestInstall_SourceHoldingTheProject_RefusedBeforeAnyWrite(t *testing.T) {
	const refusal = " is the project directory or one of its parent directories; it cannot be copied into apm_modules/\n"
	tests := []struct {
		name     string
		manifest func(root string) string
		args     func(root string) []string
		declared func(root string) string
	}{
		{
			name:     "path to the project by name",
			manifest: func(string) string { return "  apm:\n    - path: ../proj\n" },
			args:     func(string) []string { return []string{"install"} },
			declared: func(string) string { return "../proj" },
		},
		{
			name:     "absolute path of the project",
			manifest: func(root string) string { return "  apm:\n    - path: " + filepath.Join(root, "proj") + "\n" },
			args:     func(string) []string { return []string{"install"} },
			declared: func(root string) string { return filepath.Join(root, "proj") },
		},
		{
			name:     "named parent of the project",
			manifest: func(root string) string { return "  apm:\n    - path: ../../" + filepath.Base(root) + "\n" },
			args:     func(string) []string { return []string{"install"} },
			declared: func(root string) string { return "../../" + filepath.Base(root) },
		},
		{
			name:     "install with the absolute path of the project",
			manifest: func(string) string { return "  apm: []\n" },
			args:     func(root string) []string { return []string{"install", filepath.Join(root, "proj")} },
			declared: func(root string) string { return filepath.Join(root, "proj") },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			manifestYAML := "name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n" + tt.manifest(root)
			writeCompileFile(t, root, "proj/apm.yml", manifestYAML)
			t.Chdir(filepath.Join(root, "proj"))

			stdout, _, exitCode := runCompileCLI(t, tt.args(root)...)

			want := "local dependency " + tt.declared(root) + refusal
			if exitCode != 1 || !strings.HasSuffix(stdout, want) {
				t.Errorf("exit = %d; stdout:\n%s\nwant exit 1 and it to end with:\n%s", exitCode, stdout, want)
			}
			assertIssue50Exists(t, false, "apm_modules", "apm.lock.yaml", ".claude", ".gitignore")
			if got, err := os.ReadFile("apm.yml"); err != nil || string(got) != manifestYAML {
				t.Errorf("apm.yml = %q (err %v), want it unchanged", got, err)
			}
		})
	}
}

// review of PR for issue #64, repro destination-symlink: apm_modules/_local
// links to the directory that holds the source, so the copy's destination is
// the source itself. The source must survive and install must fail.
func TestInstall_DestinationResolvingToTheSource_Refused(t *testing.T) {
	root := upDepProject(t, "  apm:\n    - ../dep\n")
	writeCompileFile(t, root, "dep/important.txt", "SOURCE MUST SURVIVE\n")
	if err := os.MkdirAll("apm_modules", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join("apm_modules", "_local")); err != nil {
		t.Fatal(err)
	}

	stdout, _, exitCode := runCompileCLI(t, "install")

	if exitCode != 1 {
		t.Errorf("exit = %d, want 1\n%s", exitCode, stdout)
	}
	if got, err := os.ReadFile(filepath.Join(root, "dep", "important.txt")); err != nil || string(got) != "SOURCE MUST SURVIVE\n" {
		t.Errorf("the source's important.txt = %q (err %v), want it untouched", got, err)
	}
}

// lateRefusalProject installs ../dep, changes its source, and declares the
// project itself after it. It returns the bytes of the copy, apm.yml and the
// lock that a refused run must leave as they are.
func lateRefusalProject(t *testing.T) (root string, copied, manifestBytes, lock []byte) {
	t.Helper()
	root = upDepProject(t, "  apm:\n    - ../dep\n")
	writeCompileFile(t, root, "dep/important.txt", "FIRST\n")
	mustRunCLI(t, "install")
	writeCompileFile(t, root, "dep/important.txt", "CHANGED SOURCE\n")
	f, err := os.OpenFile("apm.yml", os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("    - path: ../proj\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	read := func(p string) []byte {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	return root, read("apm_modules/_local/dep/important.txt"), read("apm.yml"), read("apm.lock.yaml")
}

func assertLateRefusal(t *testing.T, args []string, copied, manifestBytes, lock []byte) {
	t.Helper()
	stdout, _, exitCode := runCompileCLI(t, args...)
	want := "local dependency ../proj is the project directory or one of its parent directories; it cannot be copied into apm_modules/\n"
	if exitCode != 1 || !strings.HasSuffix(stdout, want) {
		t.Errorf("exit = %d; stdout:\n%s\nwant exit 1 and it to end with:\n%s", exitCode, stdout, want)
	}
	for p, want := range map[string][]byte{"apm_modules/_local/dep/important.txt": copied, "apm.yml": manifestBytes, "apm.lock.yaml": lock} {
		if got, err := os.ReadFile(p); err != nil || string(got) != string(want) {
			t.Errorf("%s = %q (err %v), want it unchanged: %q", p, got, err, want)
		}
	}
}

// review of PR for issue #64, repro late-refusal: a local dependency that is
// refused must not let an earlier one be copied again first.
func TestInstall_LateRefusal_LeavesEarlierCopy(t *testing.T) {
	_, copied, manifestBytes, lock := lateRefusalProject(t)
	assertLateRefusal(t, []string{"install"}, copied, manifestBytes, lock)
}

func TestUpdate_LateRefusal_LeavesEarlierCopy(t *testing.T) {
	_, copied, manifestBytes, lock := lateRefusalProject(t)
	assertLateRefusal(t, []string{"update"}, copied, manifestBytes, lock)
}

// review of PR for issue #64, second round: after an install of ../dep, the
// copy is replaced by a link to the source and apm.yml names the source
// through that link. Issue #86 changed the outcome: the link leads out of
// apm_modules, so the owner's ruling on #86 refuses it as Oracle 0.33.0 does,
// and the source is left as it was.
func TestInstall_SourceThroughItsDestinationLink_Refused(t *testing.T) {
	root := upDepProject(t, "  apm:\n    - ../dep\n")
	writeCompileFile(t, root, "dep/important.txt", "original\n")
	mustRunCLI(t, "install")
	if err := os.RemoveAll(filepath.Join("apm_modules", "_local", "dep")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "dep"), filepath.Join("apm_modules", "_local", "dep")); err != nil {
		t.Fatal(err)
	}
	writeCompileFile(t, root, "proj/apm.yml", "name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n    - path: ./apm_modules/_local/dep\n")
	before := tree86(t, root)

	stdout, _, code := runCompileCLI(t, "install")

	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	want := " x local dependency ./apm_modules/_local/dep would be copied to apm_modules/_local/dep, which resolves to " +
		filepath.Join(real, "dep") + " outside apm_modules/; refusing to replace it\n"
	if code != 1 || stdout != want {
		t.Errorf("exit %d, stdout:\n%s\nwant exit 1, stdout:\n%s", code, stdout, want)
	}
	assertTree86(t, root, before)
}
