package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Issue #50: the install and uninstall contract for local packages.

const (
	legacyDepRule = "# Dep rule\n\nFrom the dependency.\n"
	legacyOwnRule = "# Own rule\n\nFrom the project.\n"
	// legacyFixtureKey is the key in testdata/legacy_lock/apm.lock.yaml.
	legacyFixtureKey = "_local/dep-493bc424"
)

// legacyLockProject builds, in a new working directory, the state apm-go
// before issue #50 left after `install ./dep`: the lock from testdata, the
// package copied to apm_modules/_local/dep-<hash>, and both deployed rules.
// It returns the old key.
func legacyLockProject(t *testing.T) string {
	t.Helper()
	lock, err := os.ReadFile(filepath.Join("testdata", "legacy_lock", "apm.lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := chdirTemp(t)
	key := legacyLocalModulesKey(filepath.Join(dir, "dep"))
	for rel, content := range map[string]string{
		"apm.yml":     "name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n    - ./dep\n",
		"dep/apm.yml": "name: dep\nversion: 1.0.0\n",
		"dep/.apm/instructions/dep.instructions.md":                     legacyDepRule,
		".apm/instructions/own.instructions.md":                         legacyOwnRule,
		"apm_modules/" + key + "/apm.yml":                               "name: dep\nversion: 1.0.0\n",
		"apm_modules/" + key + "/.apm/instructions/dep.instructions.md": legacyDepRule,
		".claude/rules/dep.md":                                          legacyDepRule,
		".claude/rules/own.md":                                          legacyOwnRule,
		"apm.lock.yaml":                                                 strings.ReplaceAll(string(lock), legacyFixtureKey, key),
	} {
		writeCompileFile(t, dir, rel, content)
	}
	return key
}

func assertIssue50Exists(t *testing.T, want bool, paths ...string) {
	t.Helper()
	for _, p := range paths {
		_, err := os.Lstat(p)
		if want && err != nil {
			t.Errorf("%s: want it present, got %v", p, err)
		}
		if !want && !os.IsNotExist(err) {
			t.Errorf("%s: want it absent, Lstat err = %v", p, err)
		}
	}
}

// The lock of legacyLockProject in the current format. The layout is the
// Oracle's (parity case install-local-multi-block compares the same shape
// byte for byte); apm_version is this build's.
const legacyLockConverted = `lockfile_version: '1'
apm_version: dev
dependencies:
- repo_url: _local/dep
  name: dep
  version: 1.0.0
  package_type: apm_package
  deployed_files:
  - .claude/rules/dep.md
  deployed_file_hashes:
    .claude/rules/dep.md: sha256:b65d2cf2095c072332c85702c9cdcade660bba79de9a67ea5bb12c1878a7217a
  source: local
  local_path: ./dep
deployments:
- kind: project-relative
  target: claude
  value: .claude/rules/dep.md
  runtime: null
  scope: project
  owners:
  - ./dep
  active_owner: ./dep
  content_hash: sha256:b65d2cf2095c072332c85702c9cdcade660bba79de9a67ea5bb12c1878a7217a
- kind: project-relative
  target: claude
  value: .claude/rules/own.md
  runtime: null
  scope: project
  owners:
  - .
  active_owner: .
  content_hash: sha256:ad3790093c985e844a6541e159782c829227aa2d98fda86ef24eb673af37fe33
local_deployed_files:
- .claude/rules/own.md
local_deployed_file_hashes:
  .claude/rules/own.md: sha256:ad3790093c985e844a6541e159782c829227aa2d98fda86ef24eb673af37fe33
`

// Ruling R16: an install that is not frozen writes the whole lock in the
// current format, moves the package to apm_modules/_local/<name>, and deletes
// the directory the old lock names -- that one only.
func TestInstall_LegacyLock_ConvertedInPlace(t *testing.T) {
	oldKey := legacyLockProject(t)
	// Not named by the lock: a pattern match on _local/ would delete it.
	writeCompileFile(t, ".", "apm_modules/_local/dep-ffffffff/keep.txt", "user\n")

	stdout, stderr, exitCode := runCompileCLI(t, "install")

	if exitCode != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q\n%s", exitCode, stderr, stdout)
	}
	lock, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(lock) != legacyLockConverted {
		t.Errorf("apm.lock.yaml:\n%s\nwant:\n%s", lock, legacyLockConverted)
	}
	assertIssue50Exists(t, false, "apm_modules/"+oldKey)
	assertIssue50Exists(t, true, "apm_modules/_local/dep/apm.yml", "apm_modules/_local/dep-ffffffff/keep.txt")
	// The deployed rule of the old key is adopted, not cleaned as stale and
	// deployed again.
	if got, err := os.ReadFile(".claude/rules/dep.md"); err != nil || string(got) != legacyDepRule {
		t.Errorf(".claude/rules/dep.md = %q (err %v)", got, err)
	}
	if strings.Contains(stdout, "Cleaned") || !strings.Contains(stdout, "  |-- 1 rule(s) adopted -> .claude/rules/\n  |-- (files unchanged)\n") {
		t.Errorf("want both packages adopted and nothing cleaned:\n%s", stdout)
	}
}

// Ruling R16: a frozen install passes on a lock in the old format and changes
// no file.
func TestInstallFrozen_LegacyLock_PassesAndChangesNothing(t *testing.T) {
	legacyLockProject(t)
	before := treeSnapshot(t, ".")

	stdout, stderr, exitCode := runCompileCLI(t, "install", "--frozen")

	if exitCode != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q\n%s", exitCode, stderr, stdout)
	}
	if !strings.Contains(stdout, " + Frozen install: all dependencies pinned and verified\n") {
		t.Errorf("stdout:\n%s", stdout)
	}
	after := treeSnapshot(t, ".")
	for path, entry := range before {
		if after[path] != entry {
			t.Errorf("%s changed", path)
		}
	}
	if len(after) != len(before) {
		t.Errorf("%d entries after, %d before", len(after), len(before))
	}
}

// Ruling R16: uninstall finds a package the old lock names by its old key,
// and keeps the lock for the project's own deployed files.
func TestUninstall_LegacyLock_RemovesEntryAndDirectory(t *testing.T) {
	oldKey := legacyLockProject(t)

	stdout, stderr, exitCode := runCompileCLI(t, "uninstall", "./dep")

	if exitCode != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q\n%s", exitCode, stderr, stdout)
	}
	assertIssue50Exists(t, false, "apm_modules/"+oldKey, ".claude/rules/dep.md")
	assertIssue50Exists(t, true, ".claude/rules/own.md", "dep/apm.yml")
	lock := readLockfile(t)
	if len(lock.Dependencies) != 0 {
		t.Errorf("lock still has dependencies: %+v", lock.Dependencies)
	}
	if len(lock.LocalDeployedFiles) != 1 || lock.LocalDeployedFiles[0] != ".claude/rules/own.md" {
		t.Errorf("local_deployed_files = %v, want [.claude/rules/own.md]", lock.LocalDeployedFiles)
	}
	manifestBytes, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifestBytes), "./dep") {
		t.Errorf("apm.yml still lists ./dep:\n%s", manifestBytes)
	}
}

// Ruling R6: two local packages with one directory name would share
// apm_modules/_local/<name>. install refuses them before it writes anything.
func TestInstall_LocalNameCollision_RefusedBeforeAnyWrite(t *testing.T) {
	const manifestYAML = "name: proj\nversion: 1.0.0\ntarget: claude\n"
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", manifestYAML)
	writeCompileFile(t, dir, "a/dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	writeCompileFile(t, dir, "b/dep/apm.yml", "name: dep\nversion: 1.0.0\n")

	stdout, stderr, exitCode := runCompileCLI(t, "install", "./a/dep", "./b/dep")

	if exitCode != 1 || stderr != "" {
		t.Errorf("exit = %d, stderr = %q; want 1 and no stderr", exitCode, stderr)
	}
	want := " x local packages " + filepath.Join(dir, "a", "dep") + " and " + filepath.Join(dir, "b", "dep") +
		` have the same directory name "dep"; a local package is installed to apm_modules/_local/dep, so the two would overwrite each other -- rename one of the directories` + "\n"
	if !strings.HasSuffix(stdout, want) {
		t.Errorf("stdout:\n%s\nwant it to end with:\n%s", stdout, want)
	}
	assertIssue50Exists(t, false, "apm_modules", "apm.lock.yaml", ".claude")
	if got, err := os.ReadFile("apm.yml"); err != nil || string(got) != manifestYAML {
		t.Errorf("apm.yml = %q (err %v), want it unchanged", got, err)
	}
}

// Ruling R20: the loader copies a local package before the resolver refuses
// it for declaring a local package of its own. The copy must not stay.
func TestInstall_RefusedLocalPackage_LeavesNoCopy(t *testing.T) {
	const manifestYAML = "name: proj\nversion: 1.0.0\ntarget: claude\n"
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", manifestYAML)
	writeCompileFile(t, dir, "dep/apm.yml", "name: dep\nversion: 1.0.0\ndependencies:\n  apm:\n    - ./inner\n")
	writeCompileFile(t, dir, "dep/inner/apm.yml", "name: inner\nversion: 1.0.0\n")

	stdout, _, exitCode := runCompileCLI(t, "install", "./dep")

	if exitCode != 1 || !strings.Contains(stdout, `transitive dependency of "_local/dep" resolves to a local filesystem path ("./inner")`) {
		t.Fatalf("exit = %d; want the resolver's refusal:\n%s", exitCode, stdout)
	}
	assertIssue50Exists(t, false, "apm_modules", "apm.lock.yaml")
	if got, err := os.ReadFile("apm.yml"); err != nil || string(got) != manifestYAML {
		t.Errorf("apm.yml = %q (err %v), want it unchanged", got, err)
	}
}

var issue50Duration = regexp.MustCompile(` in \d+\.\d+s\.\n`)

// installParityFixture copies the fixture of a parity case into a new working
// directory.
func installParityFixture(t *testing.T, caseID string) {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("..", "..", "tools", "parity", "cases", caseID, "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	dir := chdirTemp(t)
	if err := os.CopyFS(dir, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
}

// Ruling R10: the install transcript is the Oracle's. Each expected text is
// what Oracle v0.33.0 printed for the parity case of the same name, with
// apm-go's status symbols and without the Oracle's two "Skipped inactive
// experimental resolver" lines, which the ruling leaves out (the waiver of
// those cases covers exactly that).
func TestInstall_LocalPackageTranscripts(t *testing.T) {
	tests := []struct {
		caseID string
		setup  bool
		args   []string
		want   string
	}{
		{
			caseID: "install-local-path-dep",
			args:   []string{"install", "./dep", "--target", "claude"},
			want: " + Validating 1 package...\n" +
				" + ./dep\n" +
				" + Updated apm.yml with 1 new package(s)\n" +
				" > Installing 1 new package...\n" +
				" > Resolving ./dep...\n" +
				" i Targets: claude  (source: --target flag)\n" +
				"   + ./dep (local)\n" +
				"  |-- 1 rule(s) integrated -> .claude/rules/\n" +
				" i Added apm_modules/ to .gitignore\n" +
				" + Installed 1 APM dependency in <DUR>.\n",
		},
		{
			caseID: "install-local-multi-block",
			args:   []string{"install"},
			want: " > Installing dependencies from apm.yml...\n" +
				" > Resolving ./dep...\n" +
				" i Targets: claude  (source: apm.yml)\n" +
				"   + ./dep (local)\n" +
				"  |-- 3 rule(s) integrated -> .claude/rules/\n" +
				"   + <project root> (local)\n" +
				"  |-- 3 rule(s) integrated -> .claude/rules/\n" +
				" i Added apm_modules/ to .gitignore\n" +
				" + Installed 2 APM dependencies in <DUR>.\n",
		},
		{
			caseID: "install-local-repeat",
			setup:  true,
			args:   []string{"install"},
			want: " > Installing dependencies from apm.yml...\n" +
				" i Targets: claude  (source: apm.yml)\n" +
				"   + ./dep (local)\n" +
				"  |-- 1 rule(s) adopted -> .claude/rules/\n" +
				"  |-- (files unchanged)\n" +
				"   + <project root> (local)\n" +
				"  |-- 1 rule(s) adopted -> .claude/rules/\n" +
				"  |-- (files unchanged)\n" +
				" + Installed 1 APM dependency in <DUR>.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.caseID, func(t *testing.T) {
			installParityFixture(t, tt.caseID)
			if tt.setup {
				if _, _, exitCode := runCompileCLI(t, "install"); exitCode != 0 {
					t.Fatalf("setup install: exit %d", exitCode)
				}
			}

			stdout, stderr, exitCode := runCompileCLI(t, tt.args...)

			if exitCode != 0 || stderr != "" {
				t.Errorf("exit = %d, stderr = %q", exitCode, stderr)
			}
			if got := issue50Duration.ReplaceAllString(stdout, " in <DUR>.\n"); got != tt.want {
				t.Errorf("stdout:\n%s\nwant:\n%s", got, tt.want)
			}
			info, err := os.Stat("apm.lock.yaml")
			if err != nil {
				t.Fatal(err)
			}
			// Ruling R19.
			if info.Mode().Perm() != 0o600 {
				t.Errorf("apm.lock.yaml mode = %v, want 0600", info.Mode().Perm())
			}
		})
	}
}

// The two cases below have a stdout waiver in tools/parity (owner ruling of
// 2026-10-12), so the gate does not compare their stdout. These tests do, for
// the same fixture.

// The Oracle prints the same words and breaks the last line at column 80;
// that break is the waived difference.
func TestUninstallLocal_StdoutVerbatim(t *testing.T) {
	installParityFixture(t, "uninstall-local")
	if _, _, exitCode := runCompileCLI(t, "install", "./dep", "--target", "claude"); exitCode != 0 {
		t.Fatalf("setup install: exit %d", exitCode)
	}
	manifestPath, err := filepath.Abs("apm.yml")
	if err != nil {
		t.Fatal(err)
	}

	stdout, stderr, exitCode := runCompileCLI(t, "uninstall", "./dep")

	want := " > Uninstalling 1 package(s)...\n" +
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
}

// testdata/issue50/README.md says how the expected text follows from the
// Oracle's.
func TestMarketplaceInit_StdoutVerbatim(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "issue50", "marketplace-init.stdout"))
	if err != nil {
		t.Fatal(err)
	}
	chdirTemp(t)

	stdout, stderr, exitCode := runCompileCLI(t, "marketplace", "init", "--name", "demo", "--no-gitignore-check")

	if exitCode != 0 || stderr != "" {
		t.Errorf("exit = %d, stderr = %q", exitCode, stderr)
	}
	if stdout != string(want) {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
}

// localSkillProject builds the project the Oracle's lock
// internal/lockfile/testdata/oracle/local-skill.lock.yaml was recorded for.
func localSkillProject(t *testing.T) {
	t.Helper()
	dir := chdirTemp(t)
	for rel, content := range map[string]string{
		"apm.yml":     "name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n  - ./dep\n",
		"dep/apm.yml": "name: dep\nversion: 1.0.0\n",
		"dep/.apm/instructions/dep.instructions.md": legacyDepRule,
		"dep/.apm/skills/sk/SKILL.md":               "---\nname: sk\ndescription: d\n---\nskill\n",
		"dep/.apm/skills/sk/extra.txt":              "extra\n",
	} {
		writeCompileFile(t, dir, rel, content)
	}
}

func mustRunCLI(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, exitCode := runCompileCLI(t, args...)
	if exitCode != 0 || stderr != "" {
		t.Fatalf("%v: exit = %d, stderr = %q\n%s", args, exitCode, stderr, stdout)
	}
	return issue50Duration.ReplaceAllString(stdout, " in <DUR>.\n")
}

var issue50APMVersion = regexp.MustCompile(`(?m)^apm_version: .*$`)

// A skill's directory is a deployed path of its own in the Oracle's lock,
// with no hash. The lock, the repeat install and the uninstall below are
// what Oracle v0.33.0 wrote and printed for this project, with apm-go's
// status symbols and without the two experimental-resolver lines (R10).
func TestInstall_LocalPackageWithSkill_LockAndTranscripts(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "..", "internal", "lockfile", "testdata", "oracle", "local-skill.lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	localSkillProject(t)

	mustRunCLI(t, "install")

	lock, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	const version = "apm_version: X"
	if got, want := issue50APMVersion.ReplaceAllString(string(lock), version), issue50APMVersion.ReplaceAllString(string(want), version); got != want {
		t.Errorf("apm.lock.yaml:\n%s\nwant the Oracle's:\n%s", got, want)
	}

	wantRepeat := " > Installing dependencies from apm.yml...\n" +
		" i Targets: claude  (source: apm.yml)\n" +
		"   + ./dep (local)\n" +
		"  |-- 1 rule(s) adopted -> .claude/rules/\n" +
		"  |-- 1 skill(s) integrated -> .claude/skills/\n" +
		" + Installed 1 APM dependency in <DUR>.\n"
	if got := mustRunCLI(t, "install"); got != wantRepeat {
		t.Errorf("repeat install:\n%s\nwant:\n%s", got, wantRepeat)
	}
	if again, err := os.ReadFile("apm.lock.yaml"); err != nil || string(again) != string(lock) {
		t.Errorf("the repeat install changed apm.lock.yaml (err %v):\n%s", err, again)
	}

	// The directory entry has no hash: frozen install and audit verify the
	// three files and are not stopped by it.
	if got := mustRunCLI(t, "install", "--frozen"); got != " + Frozen install: all dependencies pinned and verified\n" {
		t.Errorf("frozen install:\n%s", got)
	}
	if got := mustRunCLI(t, "audit"); !strings.HasSuffix(got, " + audit: 3 deployed files verified\n") {
		t.Errorf("audit:\n%s", got)
	}

	manifestPath, err := filepath.Abs("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	wantUninstall := " > Uninstalling 1 package(s)...\n" +
		" + _local/dep - found in apm.yml\n" +
		" i Removed _local/dep from apm_modules/\n" +
		" i Cleaned 4 stale files from ./dep\n" +
		" i Removed _local/dep from dependencies.apm in apm.yml\n" +
		" + Updated " + manifestPath + " (removed 1 package(s))\n" +
		" + Uninstall complete: Removed 1 package(s) from apm.yml, Removed 1 package(s) from apm_modules/\n"
	if got := mustRunCLI(t, "uninstall", "./dep"); got != wantUninstall {
		t.Errorf("uninstall:\n%s\nwant:\n%s", got, wantUninstall)
	}
	assertIssue50Exists(t, false, ".claude", "apm.lock.yaml")
}

// A skill removed from the package is stale: its files and its directory
// entry leave the lock and the disk, with no warning for the entry that has
// no hash. The lock then equals the one a fresh install without the skill
// writes.
func TestInstall_LocalPackageSkillRemoved_CleansDirectoryEntry(t *testing.T) {
	localSkillProject(t)
	mustRunCLI(t, "install")
	if err := os.RemoveAll(filepath.Join("dep", ".apm", "skills")); err != nil {
		t.Fatal(err)
	}

	got := mustRunCLI(t, "install")

	if !strings.Contains(got, " i Cleaned 3 stale files from ") || strings.Contains(got, " ! ") {
		t.Errorf("want three paths cleaned and no warning:\n%s", got)
	}
	assertIssue50Exists(t, false, ".claude/skills")
	assertIssue50Exists(t, true, ".claude/rules/dep.md")
	lock := readLockfile(t)
	if files := lock.Dependencies[0].DeployedFiles; len(files) != 1 || files[0] != ".claude/rules/dep.md" {
		t.Errorf("deployed_files = %v, want [.claude/rules/dep.md]", files)
	}
}

// A file the user put into a deployed skill directory keeps the directory:
// uninstall removes the package's files, leaves the user's, and does not
// count or warn about the directory entry.
func TestUninstall_SkillDirectoryWithUserFile_Stays(t *testing.T) {
	localSkillProject(t)
	mustRunCLI(t, "install")
	writeCompileFile(t, ".", ".claude/skills/sk/notes.txt", "mine\n")

	got := mustRunCLI(t, "uninstall", "./dep")

	if !strings.Contains(got, " i Cleaned 3 stale files from ./dep\n") || strings.Contains(got, " ! ") {
		t.Errorf("want the three files cleaned and no warning:\n%s", got)
	}
	assertIssue50Exists(t, true, ".claude/skills/sk/notes.txt")
	assertIssue50Exists(t, false, ".claude/skills/sk/SKILL.md", ".claude/rules/dep.md")
}

// The per-kind lines for an agent, a command and a rule, first install and
// repeat, are what Oracle v0.33.0 printed for a local package with those
// three in .claude/. The Oracle makes its command from .apm/prompts/, apm-go
// from .apm/commands/ (a difference in what is deployed, recorded in ticket
// 32); the line is the same. On the repeat run the Oracle says "integrated"
// for the command and "adopted" for the other two.
func TestInstall_LocalPackageKindLines(t *testing.T) {
	dir := chdirTemp(t)
	for rel, content := range map[string]string{
		"apm.yml":     "name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n  - ./dep\n",
		"dep/apm.yml": "name: dep\nversion: 1.0.0\n",
		"dep/.apm/instructions/dep.instructions.md": legacyDepRule,
		"dep/.apm/agents/helper.agent.md":           "---\nname: helper\ndescription: d\n---\nagent body\n",
		"dep/.apm/commands/go.md":                   "---\ndescription: d\n---\ncommand body\n",
	} {
		writeCompileFile(t, dir, rel, content)
	}

	wantFirst := " > Installing dependencies from apm.yml...\n" +
		" > Resolving ./dep...\n" +
		" i Targets: claude  (source: apm.yml)\n" +
		"   + ./dep (local)\n" +
		"  |-- 1 agents integrated -> .claude/agents/\n" +
		"  |-- 1 commands integrated -> .claude/commands/\n" +
		"  |-- 1 rule(s) integrated -> .claude/rules/\n" +
		" i Added apm_modules/ to .gitignore\n" +
		" + Installed 1 APM dependency in <DUR>.\n"
	if got := mustRunCLI(t, "install"); got != wantFirst {
		t.Errorf("first install:\n%s\nwant:\n%s", got, wantFirst)
	}

	wantRepeat := " > Installing dependencies from apm.yml...\n" +
		" i Targets: claude  (source: apm.yml)\n" +
		"   + ./dep (local)\n" +
		"  |-- 1 agents adopted -> .claude/agents/\n" +
		"  |-- 1 commands integrated -> .claude/commands/\n" +
		"  |-- 1 rule(s) adopted -> .claude/rules/\n" +
		" + Installed 1 APM dependency in <DUR>.\n"
	if got := mustRunCLI(t, "install"); got != wantRepeat {
		t.Errorf("repeat install:\n%s\nwant:\n%s", got, wantRepeat)
	}

	if got := mustRunCLI(t, "uninstall", "./dep"); !strings.Contains(got, " i Cleaned 3 stale files from ./dep\n") {
		t.Errorf("uninstall:\n%s", got)
	}
}

// --env and --header keep the order they were given in. The expected files
// are what Oracle v0.33.0 wrote for the same command lines (apm.yml and
// .mcp.json whole, the lock from mcp_configs on); a later plain install must
// not reorder them.
func TestInstallMCP_EnvAndHeaderKeepInputOrder(t *testing.T) {
	tests := []struct {
		name                            string
		args                            []string
		wantManifest, wantMCP, wantLock string
	}{
		{
			name: "env",
			args: []string{"install", "--mcp", "two", "--target", "claude", "--env", "K=V", "--env", "A=B", "--", "npx", "-y", "foo"},
			wantManifest: "name: p\nversion: 1.0.0\ndependencies:\n  mcp:\n  - name: two\n    registry: false\n    transport: stdio\n    command: npx\n" +
				"    args:\n    - -y\n    - foo\n    env:\n      K: V\n      A: B\n",
			wantMCP: "{\n  \"mcpServers\": {\n    \"two\": {\n      \"type\": \"stdio\",\n      \"command\": \"npx\",\n      \"env\": {\n        \"K\": \"V\",\n        \"A\": \"B\"\n      },\n" +
				"      \"args\": [\n        \"-y\",\n        \"foo\"\n      ]\n    }\n  }\n}\n",
			wantLock: "mcp_configs:\n  two:\n    name: two\n    transport: stdio\n    env:\n      K: V\n      A: B\n    args:\n    - -y\n    - foo\n    registry: false\n    command: npx\n" +
				"mcp_target_servers:\n  claude:\n  - two\n",
		},
		{
			name: "headers",
			args: []string{"install", "--mcp", "api", "--target", "claude", "--transport", "http", "--url", "https://example.com/mcp", "--header", "X-K=V", "--header", "A-H=B"},
			wantManifest: "name: p\nversion: 1.0.0\ndependencies:\n  mcp:\n  - name: api\n    registry: false\n    transport: http\n    url: https://example.com/mcp\n" +
				"    headers:\n      X-K: V\n      A-H: B\n",
			wantMCP: "{\n  \"mcpServers\": {\n    \"api\": {\n      \"type\": \"http\",\n      \"url\": \"https://example.com/mcp\",\n      \"headers\": {\n        \"X-K\": \"V\",\n        \"A-H\": \"B\"\n      }\n    }\n  }\n}\n",
			wantLock: "mcp_configs:\n  api:\n    name: api\n    transport: http\n    registry: false\n    headers:\n      X-K: V\n      A-H: B\n    url: https://example.com/mcp\n" +
				"mcp_target_servers:\n  claude:\n  - api\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APM_CONFIG_DIR", t.TempDir())
			dir := chdirTemp(t)
			writeCompileFile(t, dir, "apm.yml", "name: p\nversion: 1.0.0\n")

			mustRunCLI(t, tt.args...)

			check := func(when string) {
				t.Helper()
				for file, want := range map[string]string{"apm.yml": tt.wantManifest, ".mcp.json": tt.wantMCP} {
					if got, err := os.ReadFile(file); err != nil || string(got) != want {
						t.Errorf("%s, %s (err %v):\n%s\nwant:\n%s", when, file, err, got, want)
					}
				}
				lock, err := os.ReadFile("apm.lock.yaml")
				if err != nil {
					t.Fatal(err)
				}
				if !strings.HasSuffix(string(lock), tt.wantLock) {
					t.Errorf("%s, apm.lock.yaml:\n%s\nwant it to end with:\n%s", when, lock, tt.wantLock)
				}
			}
			check("after install --mcp")
			mustRunCLI(t, "install", "--target", "claude")
			check("after a plain install")
		})
	}
}

// Ruling R16 for update: it writes the lock in the current format, so it has
// to delete the directory the old lock named, as install does. Its text is
// not changed.
func TestUpdate_LegacyLock_RemovesOldDirectory(t *testing.T) {
	oldKey := legacyLockProject(t)
	writeCompileFile(t, ".", "apm_modules/_local/dep-ffffffff/keep.txt", "user\n")

	stdout, stderr, exitCode := runCompileCLI(t, "update")

	if exitCode != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q\n%s", exitCode, stderr, stdout)
	}
	assertIssue50Exists(t, false, "apm_modules/"+oldKey)
	assertIssue50Exists(t, true, "apm_modules/_local/dep/apm.yml", "apm_modules/_local/dep-ffffffff/keep.txt", ".claude/rules/dep.md")
	lock := readLockfile(t)
	if len(lock.Dependencies) != 1 || lock.Dependencies[0].RepoURL != "_local/dep" || lock.Dependencies[0].Source != "local" {
		t.Errorf("lock dependencies = %+v, want one _local/dep with source local", lock.Dependencies)
	}
}

// An install that fails before apm.yml is written does not say it updated
// apm.yml, and a local path that does not exist gets no check mark. The
// error line is the resolver's.
func TestInstall_FailedRunDoesNotClaimManifestUpdate(t *testing.T) {
	const manifestYAML = "name: p\nversion: 1.0.0\ntarget: claude\n"
	tests := []struct {
		name      string
		files     map[string]string
		args      []string
		wantLines string
		wantError string
	}{
		{
			name: "local path that does not exist",
			args: []string{"install", "./nope"},
			wantLines: " + Validating 1 package...\n" +
				" > Installing 1 new package...\n" +
				" > Resolving ./nope...\n",
			wantError: " x resolve: loading package _local/nope: local dependency source ",
		},
		{
			name: "local package that declares a local package",
			files: map[string]string{
				"dep/apm.yml":     "name: dep\nversion: 1.0.0\ndependencies:\n  apm:\n    - ./sub\n",
				"dep/sub/apm.yml": "name: sub\nversion: 1.0.0\n",
			},
			args: []string{"install", "./dep"},
			wantLines: " + Validating 1 package...\n" +
				" + ./dep\n" +
				" > Installing 1 new package...\n" +
				" > Resolving ./dep...\n",
			wantError: ` x resolve: transitive dependency of "_local/dep" resolves to a local filesystem path ("./sub"); `,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := chdirTemp(t)
			writeCompileFile(t, dir, "apm.yml", manifestYAML)
			for rel, content := range tt.files {
				writeCompileFile(t, dir, rel, content)
			}

			stdout, stderr, exitCode := runCompileCLI(t, tt.args...)

			if exitCode != 1 || stderr != "" {
				t.Errorf("exit = %d, stderr = %q; want 1 and no stderr", exitCode, stderr)
			}
			rest, ok := strings.CutPrefix(stdout, tt.wantLines)
			if !ok || !strings.HasPrefix(rest, tt.wantError) || strings.Count(rest, "\n") != 1 {
				t.Errorf("stdout:\n%s\nwant exactly:\n%sthen one line starting %q", stdout, tt.wantLines, tt.wantError)
			}
			if got, err := os.ReadFile("apm.yml"); err != nil || string(got) != manifestYAML {
				t.Errorf("apm.yml = %q (err %v), want it unchanged", got, err)
			}
		})
	}
}
