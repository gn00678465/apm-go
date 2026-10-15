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
