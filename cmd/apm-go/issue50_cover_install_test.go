package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	yamllib "go.yaml.in/yaml/v4"
)

// Ruling R6 for packages apm.yml already declares: the refusal is the one the
// command-line path prints (TestInstall_LocalNameCollision_RefusedBeforeAnyWrite).
func TestInstall_LocalNameCollisionInManifest_RefusedBeforeAnyWrite(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
	}{
		{
			name:     "both in dependencies",
			manifest: "name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n  - ./a/dep\n  - ./b/dep\n",
		},
		{
			name:     "one in devDependencies",
			manifest: "name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n  - ./a/dep\ndevDependencies:\n  apm:\n  - ./b/dep\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := chdirTemp(t)
			writeCompileFile(t, dir, "apm.yml", tt.manifest)
			writeCompileFile(t, dir, "a/dep/apm.yml", "name: dep\nversion: 1.0.0\n")
			writeCompileFile(t, dir, "b/dep/apm.yml", "name: dep\nversion: 1.0.0\n")

			stdout, stderr, exitCode := runCompileCLI(t, "install")

			if exitCode != 1 || stderr != "" {
				t.Errorf("exit = %d, stderr = %q; want 1 and no stderr", exitCode, stderr)
			}
			want := " x local packages " + filepath.Join(dir, "a", "dep") + " and " + filepath.Join(dir, "b", "dep") +
				` have the same directory name "dep"; a local package is installed to apm_modules/_local/dep, so the two would overwrite each other -- rename one of the directories` + "\n"
			if stdout != want {
				t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
			}
			assertIssue50Exists(t, false, "apm_modules", "apm.lock.yaml", ".claude")
			if got, err := os.ReadFile("apm.yml"); err != nil || string(got) != tt.manifest {
				t.Errorf("apm.yml = %q (err %v), want it unchanged", got, err)
			}
		})
	}
}

// Ruling R13: update keeps its own summary, with its list of the MCP servers
// it configured.
func TestUpdate_ListsConfiguredMCPServers(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", "name: p\nversion: 1.0.0\ntarget: claude\ndependencies:\n  mcp:\n"+
		"  - name: two\n    registry: false\n    transport: stdio\n    command: npx\n"+
		"  - name: one\n    registry: false\n    transport: stdio\n    command: npx\n")
	mustRunCLI(t, "install")

	got := mustRunCLI(t, "update")

	want := " i Targets: claude  (source: apm.yml)\n" +
		" i MCP servers configured:\n" +
		"  - one -> claude\n" +
		"  - two -> claude\n" +
		" i Already up to date\n"
	if got != want {
		t.Errorf("update:\n%s\nwant:\n%s", got, want)
	}
}

// The package_type of a local package with no apm.yml of its own, and of one
// that has apm.yml beside SKILL.md. hybrid, marketplace_plugin and
// claude_skill are what Oracle v0.33.0 wrote to the lock for these three
// directories; hook_package is the Oracle's name for the type
// (models/validation.py:29), which the Oracle itself refuses as a local
// package.
func TestInstall_LocalPackageTypesInLock(t *testing.T) {
	const skill = "---\nname: s\ndescription: d\n---\nbody\n"
	dir := chdirTemp(t)
	for rel, content := range map[string]string{
		"apm.yml":             "name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n  - ./hy\n  - ./pl\n  - ./sk\n  - ./hk\n",
		"hy/apm.yml":          "name: hy\nversion: 1.0.0\n",
		"hy/SKILL.md":         skill,
		"pl/plugin.json":      "{\"name\":\"pl\",\"version\":\"1.0.0\"}\n",
		"sk/SKILL.md":         skill,
		"hk/hooks/hooks.json": "{\"hooks\":{}}\n",
	} {
		writeCompileFile(t, dir, rel, content)
	}

	mustRunCLI(t, "install")

	want := map[string]string{
		"_local/hy": "hybrid",
		"_local/pl": "marketplace_plugin",
		"_local/sk": "claude_skill",
		"_local/hk": "hook_package",
	}
	lock := readLockfile(t)
	if len(lock.Dependencies) != len(want) {
		t.Fatalf("lock has %d dependencies, want %d", len(lock.Dependencies), len(want))
	}
	for _, dep := range lock.Dependencies {
		if dep.PackageType != want[dep.RepoURL] {
			t.Errorf("%s: package_type = %q, want %q", dep.RepoURL, dep.PackageType, want[dep.RepoURL])
		}
	}
}

// One rule of a package changed and one did not: the transcript is what
// Oracle v0.33.0 printed for the second install, with apm-go's status symbols
// and without the two experimental-resolver lines (R10).
func TestInstall_ChangedRuleBesideUnchangedRule(t *testing.T) {
	dir := chdirTemp(t)
	for rel, content := range map[string]string{
		"apm.yml":     "name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n  - ./dep\n",
		"dep/apm.yml": "name: dep\nversion: 1.0.0\n",
		"dep/.apm/instructions/a.instructions.md": "# A\n\nA rule.\n",
		"dep/.apm/instructions/b.instructions.md": "# B\n\nB rule.\n",
	} {
		writeCompileFile(t, dir, rel, content)
	}
	mustRunCLI(t, "install")
	writeCompileFile(t, dir, "dep/.apm/instructions/b.instructions.md", "# B\n\nB rule changed.\n")

	got := mustRunCLI(t, "install")

	want := " > Installing dependencies from apm.yml...\n" +
		" i Targets: claude  (source: apm.yml)\n" +
		"   + ./dep (local)\n" +
		"  |-- 1 rule(s) integrated (1 adopted) -> .claude/rules/\n" +
		" + Installed 1 APM dependency in <DUR>.\n"
	if got != want {
		t.Errorf("second install:\n%s\nwant:\n%s", got, want)
	}
}

// A dependency brought instructions and the target reads them from
// AGENTS.md: the Oracle's hint (install/phases/finalize.py:147), which it
// printed for this project, with apm-go's command name.
func TestInstall_InstructionsForAgentsFamilyTarget_PrintsCompileHint(t *testing.T) {
	dir := chdirTemp(t)
	for rel, content := range map[string]string{
		"apm.yml":     "name: proj\nversion: 1.0.0\ntarget: codex\ndependencies:\n  apm:\n  - ./dep\n",
		"dep/apm.yml": "name: dep\nversion: 1.0.0\n",
		"dep/.apm/instructions/a.instructions.md": "# A\n\nA rule.\n",
	} {
		writeCompileFile(t, dir, rel, content)
	}

	got := mustRunCLI(t, "install")

	want := " i Instructions installed for codex. Run 'apm-go compile' to update AGENTS.md.\n" +
		" + Installed 1 APM dependency in <DUR>.\n"
	if !strings.HasSuffix(got, want) {
		t.Errorf("install:\n%s\nwant it to end with:\n%s", got, want)
	}
}

// A lock of the project's own content alone is not stamped with a version
// and keeps the one it has, as Oracle v0.33.0 does for the same two installs.
func TestInstall_ProjectContentOnly_KeepsLockVersion(t *testing.T) {
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntarget: claude\n")
	writeCompileFile(t, dir, ".apm/instructions/a.instructions.md", "# A\n\nA rule.\n")
	mustRunCLI(t, "install")
	lock, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	const head = "lockfile_version: '1'\n"
	rest, ok := strings.CutPrefix(string(lock), head+"dependencies: []\n")
	if !ok {
		t.Fatalf("first lock has a version line:\n%s", lock)
	}
	writeCompileFile(t, dir, "apm.lock.yaml", head+"apm_version: 0.1.2\ndependencies: []\n"+rest)
	writeCompileFile(t, dir, ".apm/instructions/b.instructions.md", "# B\n\nB rule.\n")

	mustRunCLI(t, "install")

	lock, err = os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(lock), head+"apm_version: 0.1.2\ndependencies: []\n") {
		t.Errorf("second lock:\n%s", lock)
	}
	if !strings.Contains(string(lock), ".claude/rules/b.md") {
		t.Errorf("second lock does not record the new rule:\n%s", lock)
	}
}

// readOnlyWorkDir takes the write permission from the working directory, so
// no file can be created in or renamed into it; what is in it stays writable.
func readOnlyWorkDir(t *testing.T, dir string) {
	t.Helper()
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
}

// A lock that cannot be written fails the command with one error line; the
// lock on disk is not replaced by a partial file.
func TestLockWriteFailure_IsOneErrorLine(t *testing.T) {
	const wantError = " x write apm.lock.yaml: create temp file for apm.lock.yaml: "
	ownRuleProject := func(t *testing.T) string {
		dir := chdirTemp(t)
		writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntarget: claude\n")
		writeCompileFile(t, dir, ".apm/instructions/a.instructions.md", "# A\n\nA rule.\n")
		writeCompileFile(t, dir, ".gitignore", "apm_modules/\n")
		return dir
	}
	tests := []struct {
		name  string
		setup func(t *testing.T) (args []string, wantLock string)
	}{
		{
			name: "install",
			setup: func(t *testing.T) ([]string, string) {
				dir := ownRuleProject(t)
				if err := os.MkdirAll(filepath.Join(dir, ".claude", "rules"), 0o755); err != nil {
					t.Fatal(err)
				}
				return []string{"install"}, ""
			},
		},
		{
			name: "update",
			setup: func(t *testing.T) ([]string, string) {
				dir := ownRuleProject(t)
				mustRunCLI(t, "install")
				lock, err := os.ReadFile("apm.lock.yaml")
				if err != nil {
					t.Fatal(err)
				}
				writeCompileFile(t, dir, ".apm/instructions/b.instructions.md", "# B\n\nB rule.\n")
				return []string{"update"}, string(lock)
			},
		},
		{
			name: "install of a local bundle",
			setup: func(t *testing.T) ([]string, string) {
				bundleDir := buildInstallTestBundle(t)
				dir := chdirTemp(t)
				if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
					t.Fatal(err)
				}
				return []string{"install", bundleDir, "--target", "claude"}, ""
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, wantLock := tt.setup(t)
			dir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			readOnlyWorkDir(t, dir)

			stdout, stderr, exitCode := runCompileCLI(t, args...)

			if exitCode != 1 || stderr != "" {
				t.Errorf("exit = %d, stderr = %q; want 1 and no stderr", exitCode, stderr)
			}
			lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
			if last := lines[len(lines)-1]; !strings.HasPrefix(last, wantError) {
				t.Errorf("last line = %q, want it to start with %q\n%s", last, wantError, stdout)
			}
			if wantLock == "" {
				assertIssue50Exists(t, false, "apm.lock.yaml")
			} else if got, err := os.ReadFile("apm.lock.yaml"); err != nil || string(got) != wantLock {
				t.Errorf("apm.lock.yaml changed (err %v):\n%s", err, got)
			}
			entries, err := os.ReadDir(".")
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if strings.Contains(e.Name(), ".tmp") {
					t.Errorf("temporary file left behind: %s", e.Name())
				}
			}
		})
	}
}

// apm.yml is written after the lock. When it cannot be written the command
// fails with one error line and apm.yml is as it was.
func TestInstall_ManifestNotWritable_IsOneErrorLine(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	const manifestYAML = "name: proj\nversion: 1.0.0\ntarget: claude\n"
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", manifestYAML)
	writeCompileFile(t, dir, "dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	writeCompileFile(t, dir, "dep/.apm/instructions/dep.instructions.md", legacyDepRule)
	if err := os.Chmod("apm.yml", 0o400); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, exitCode := runCompileCLI(t, "install", "./dep")

	if exitCode != 1 || stderr != "" {
		t.Errorf("exit = %d, stderr = %q; want 1 and no stderr", exitCode, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if want := " x write apm.yml: "; !strings.HasPrefix(lines[len(lines)-1], want) {
		t.Errorf("stdout:\n%s\nwant its last line to start with %q", stdout, want)
	}
	if got, err := os.ReadFile("apm.yml"); err != nil || string(got) != manifestYAML {
		t.Errorf("apm.yml = %q (err %v), want it unchanged", got, err)
	}
}

// lockedDir makes dir/ro/file with ro read-only, so dir cannot be removed.
func lockedDir(t *testing.T, dir string) {
	t.Helper()
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	ro := filepath.Join(dir, "ro")
	writeCompileFile(t, ro, "file", "x\n")
	if err := os.Chmod(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(ro, 0o700) })
}

// Ruling R16: the directory an old lock names is deleted best-effort. One
// that cannot be deleted is a warning; the lock is converted all the same.
func TestInstall_LegacyLock_UnremovableOldDirectoryIsAWarning(t *testing.T) {
	oldKey := legacyLockProject(t)
	lockedDir(t, filepath.Join("apm_modules", filepath.FromSlash(oldKey)))

	stdout, stderr, exitCode := runCompileCLI(t, "install")

	if exitCode != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q\n%s", exitCode, stderr, stdout)
	}
	if want := " ! could not remove apm_modules/" + oldKey + ": "; !strings.Contains(stdout, want) {
		t.Errorf("stdout:\n%s\nwant a line starting:\n%s", stdout, want)
	}
	if lock, err := os.ReadFile("apm.lock.yaml"); err != nil || string(lock) != legacyLockConverted {
		t.Errorf("apm.lock.yaml (err %v):\n%s\nwant:\n%s", err, lock, legacyLockConverted)
	}
}

// refusingLoader stands in for the loader of a local package that fails
// after it made the copy.
type refusingLoader struct {
	afterCopy func(dir string)
}

func (l *refusingLoader) LoadPackage(ref *manifest.DependencyReference, _ string) (*manifest.Manifest, error) {
	l.afterCopy(filepath.Join("apm_modules", filepath.FromSlash(ref.RepoURL)))
	return nil, errors.New("refused")
}

// Ruling R20: the copy of a refused local package is deleted best-effort.
// One that cannot be deleted is a warning beside the error of the resolve.
func TestInstall_RefusedLocalPackage_UnremovableCopyIsAWarning(t *testing.T) {
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n  - ./dep\n")
	writeCompileFile(t, dir, "dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &refusingLoader{afterCopy: func(copyDir string) { lockedDir(t, copyDir) }},
	}

	stdout, err := runInstallCapturingStdout(t, deps, false, false, "", nil, nil)

	if err == nil || !strings.Contains(err.Error(), "loading package _local/dep: refused") {
		t.Errorf("err = %v, want the loader's refusal", err)
	}
	if want := " ! could not remove apm_modules/_local/dep: "; !strings.Contains(stdout, want) {
		t.Errorf("stdout:\n%s\nwant a line starting:\n%s", stdout, want)
	}
	assertIssue50Exists(t, false, "apm.lock.yaml")
}

// A backslash is a legal character of a directory name on Unix and a path
// separator in a lock read on Windows, so it does not reach the apm_modules
// key. The path apm.yml and the lock's local_path record is the user's.
func TestInstall_LocalDirectoryNameWithBackslash_KeyHasNoSeparator(t *testing.T) {
	if filepath.Separator == '\\' {
		t.Skip("a backslash cannot be part of a directory name here")
	}
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntarget: claude\n")
	writeCompileFile(t, dir, `we\ird/apm.yml`, "name: dep\nversion: 1.0.0\n")
	writeCompileFile(t, dir, `we\ird/.apm/instructions/a.instructions.md`, "# A\n\nA rule.\n")

	mustRunCLI(t, "install", `./we\ird`)

	lock := readLockfile(t)
	if len(lock.Dependencies) != 1 || lock.Dependencies[0].RepoURL != "_local/we_ird" || lock.Dependencies[0].LocalPath != `./we\ird` {
		t.Errorf("lock dependencies = %+v, want one _local/we_ird with local_path ./we\\ird", lock.Dependencies)
	}
	assertIssue50Exists(t, true, "apm_modules/_local/we_ird/apm.yml", ".claude/rules/a.md")
}

// finish reports a lock or an apm.yml it cannot serialize. No apm.yml or lock
// that parsed gets here in such a state, so the outcome is built by hand.
func TestInstallRunFinish_SerializeFailures(t *testing.T) {
	scalar := func(v string) *yamllib.Node {
		return &yamllib.Node{Kind: yamllib.ScalarNode, Tag: "!!str", Value: v}
	}
	// A document is not a value: the YAML encoder refuses it inside a mapping.
	misplaced := &yamllib.Node{Kind: yamllib.DocumentNode}
	tests := []struct {
		name    string
		outcome installOutcome
		want    string
	}{
		{
			name: "lock",
			outcome: installOutcome{newLock: &lockfile.Lockfile{
				Version:    "1",
				MCPServers: []string{"s"},
				MCPConfigs: map[string]*yamllib.Node{"s": misplaced},
			}},
			want: "serialize lockfile: YAML encode error: ",
		},
		{
			name: "apm.yml root is not a mapping",
			outcome: installOutcome{
				newLock:  &lockfile.Lockfile{Version: "1"},
				node:     scalar("x"),
				packages: []string{"acme/pkg"},
			},
			want: "update apm.yml: manifest root is not a mapping",
		},
		{
			name: "apm.yml",
			outcome: installOutcome{
				newLock: &lockfile.Lockfile{Version: "1"},
				node: &yamllib.Node{Kind: yamllib.MappingNode, Tag: "!!map", Content: []*yamllib.Node{
					scalar("name"), misplaced,
				}},
				packages: []string{"acme/pkg"},
			},
			want: "serialize apm.yml: YAML encode error: ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chdirTemp(t)
			run := &installRun{}

			var err error
			captureStdout(t, func() { err = run.finish(tt.outcome) })

			if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to start with %q", err, tt.want)
			}
			assertIssue50Exists(t, false, "apm.lock.yaml", "apm.yml")
		})
	}
}
