package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/lockfile"
)

// Issue #75: a package that apm.yml no longer declares loses the files it
// deployed and its lock entry on a full install or update. Expected paths,
// counts and lock entries are what Oracle 0.33.0 printed and wrote for the
// same steps (~/.cache/apm-go-verify/issue75/, the s-*-o and o1 runs); the
// sentence is core/command_logger.py:563-576.

const (
	orphanManifestAB = "name: app\nversion: 1.0.0\ndependencies:\n  apm:\n    - ./a\n    - ./b\n"
	orphanManifestA  = "name: app\nversion: 1.0.0\ndependencies:\n  apm:\n    - ./a\n"
	orphanCleanedB   = "Cleaned 3 files from packages no longer in apm.yml\n  - .claude/rules/b.md\n  - .claude/skills/sb\n  - .claude/skills/sb/SKILL.md\n"
)

// orphanPackageFiles is one local package <p> with a rule and a skill s<p>,
// as the issue's steps write it.
func orphanPackageFiles(files map[string]string, root, p string) {
	files[root+p+"/apm.yml"] = "name: " + p + "\nversion: 1.0.0\n"
	files[root+p+"/.apm/instructions/"+p+".instructions.md"] = "---\ndescription: " + p + "\n---\nrule " + p + "\n"
	files[root+p+"/.apm/skills/s"+p+"/SKILL.md"] = "---\nname: s" + p + "\ndescription: d\n---\nx\n"
}

// orphanProject writes packages a, b and c and an apm.yml declaring a and b,
// then installs for target.
func orphanProject(t *testing.T, target string) {
	t.Helper()
	files := map[string]string{}
	for _, p := range []string{"a", "b", "c"} {
		orphanPackageFiles(files, "", p)
	}
	staleCleanupProject(t, orphanManifestAB, files)
	staleCleanupInstall(t, target)
	assertStaleCleanupExists(t, true, ".claude/rules/b.md", ".claude/skills/sb/SKILL.md")
}

func orphanInstallPackages(t *testing.T, target string, packages ...string) string {
	t.Helper()
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	return captureUninstallStdout(t, func() {
		if err := runInstall(deps, false, true, target, "", nil, packages); err != nil {
			t.Fatalf("install: %v", err)
		}
	})
}

func orphanUpdate(t *testing.T, pkg string) string {
	t.Helper()
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	return captureUninstallStdout(t, func() {
		if err := runUpdate(deps, false, true, pkg, false, ""); err != nil {
			t.Fatalf("update: %v", err)
		}
	})
}

func orphanLockKeys(t *testing.T) []string {
	t.Helper()
	var keys []string
	for _, d := range readLockfile(t).Dependencies {
		keys = append(keys, d.UniqueKey())
	}
	return keys
}

func assertOrphanLockKeys(t *testing.T, want ...string) {
	t.Helper()
	if got := orphanLockKeys(t); !slices.Equal(got, want) {
		t.Errorf("lock dependencies = %q, want %q", got, want)
	}
}

// rewriteOrphanLock applies edit to the lock on disk.
func rewriteOrphanLock(t *testing.T, edit func(*lockfile.Lockfile)) {
	t.Helper()
	lock := readLockfile(t)
	edit(lock)
	data, err := lockfile.WriteLockfile(lock, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("apm.lock.yaml", data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func orphanLockEntry(lock *lockfile.Lockfile, key string) *lockfile.LockedDep {
	for i := range lock.Dependencies {
		if lock.Dependencies[i].UniqueKey() == key {
			return &lock.Dependencies[i]
		}
	}
	return nil
}

func TestInstall_OrphanCleanup_RemovesFilesOfPackageNoLongerDeclared(t *testing.T) {
	orphanProject(t, "claude")

	writeStaleCleanupFile(t, "apm.yml", orphanManifestA)
	stdout := staleCleanupInstall(t, "claude")

	assertStaleCleanupExists(t, false, ".claude/rules/b.md", ".claude/skills/sb/SKILL.md", ".claude/skills/sb")
	assertStaleCleanupExists(t, true, ".claude/rules/a.md", ".claude/skills/sa/SKILL.md")
	assertOrphanLockKeys(t, "_local/a")
	if !strings.Contains(stdout, orphanCleanedB) {
		t.Errorf("stdout must report and list the three removed paths, got:\n%s", stdout)
	}
}

// s-edited: the rule was edited after the deploy. The Oracle keeps it, removes
// the skill, and keeps _local/b in the lock with the rule and its old hash.
func TestInstall_OrphanCleanup_KeepsEditedFileAndItsLockEntry(t *testing.T) {
	orphanProject(t, "claude")
	old := *orphanLockEntry(readLockfile(t), "_local/b")
	oldHash := old.DeployedHashes[".claude/rules/b.md"]

	const edited = "---\ndescription: b\n---\nrule b\nedited\n"
	writeStaleCleanupFile(t, ".claude/rules/b.md", edited)
	writeStaleCleanupFile(t, "apm.yml", orphanManifestA)
	stdout := staleCleanupInstall(t, "claude")

	if got, err := os.ReadFile(filepath.FromSlash(".claude/rules/b.md")); err != nil || string(got) != edited {
		t.Errorf("edited rule must stay as the user left it: %q, %v", got, err)
	}
	assertStaleCleanupExists(t, false, ".claude/skills/sb/SKILL.md", ".claude/skills/sb")
	if !strings.Contains(stdout, `keeping ".claude/rules/b.md": modified since deploy (hash mismatch)`) {
		t.Errorf("stdout must warn about the kept rule, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Cleaned 2 files from packages no longer in apm.yml\n  - .claude/skills/sb\n  - .claude/skills/sb/SKILL.md\n") {
		t.Errorf("stdout must report the two removed skill paths, got:\n%s", stdout)
	}

	assertOrphanLockKeys(t, "_local/a", "_local/b")
	b := orphanLockEntry(readLockfile(t), "_local/b")
	if b == nil {
		t.FailNow()
	}
	if !slices.Equal(b.DeployedFiles, []string{".claude/rules/b.md"}) {
		t.Errorf("_local/b deployed_files = %q, want only the kept rule", b.DeployedFiles)
	}
	if len(b.DeployedHashes) != 1 || b.DeployedHashes[".claude/rules/b.md"] != oldHash {
		t.Errorf("_local/b deployed_file_hashes = %v, want the rule's old hash %s", b.DeployedHashes, oldHash)
	}
	if b.Name != "b" || b.Version != "1.0.0" || b.Source != "local" || b.LocalPath != "./b" {
		t.Errorf("_local/b must keep its old fields, got name %q version %q source %q local_path %q", b.Name, b.Version, b.Source, b.LocalPath)
	}
}

// s-othertarget: the Oracle judges an orphan's paths against every target,
// not only this run's.
func TestInstall_OrphanCleanup_RemovesFilesOfTargetsNoLongerActive(t *testing.T) {
	orphanProject(t, "claude,copilot")
	assertStaleCleanupExists(t, true, ".github/instructions/b.instructions.md", ".agents/skills/sb/SKILL.md")

	writeStaleCleanupFile(t, "apm.yml", orphanManifestA)
	stdout := staleCleanupInstall(t, "claude")

	assertStaleCleanupExists(t, false,
		".claude/rules/b.md", ".claude/skills/sb", ".github/instructions/b.instructions.md", ".agents/skills/sb")
	assertStaleCleanupExists(t, true,
		".claude/rules/a.md", ".claude/skills/sa/SKILL.md", ".github/instructions/a.instructions.md", ".agents/skills/sa/SKILL.md")
	assertOrphanLockKeys(t, "_local/a")
	if !strings.Contains(stdout, "Cleaned 6 files from packages no longer in apm.yml\n") {
		t.Errorf("stdout must report six removed paths, got:\n%s", stdout)
	}
}

// A path the orphan recorded that a package still declared deploys in this
// run is that package's now.
func TestInstall_OrphanCleanup_KeepsPathAnotherPackageDeploys(t *testing.T) {
	const shared = "---\nname: shared\ndescription: d\n---\nshared\n"
	files := map[string]string{"b/.apm/skills/shared/SKILL.md": shared}
	for _, p := range []string{"a", "b"} {
		orphanPackageFiles(files, "", p)
	}
	staleCleanupProject(t, orphanManifestAB, files)
	staleCleanupInstall(t, "claude")
	if b := orphanLockEntry(readLockfile(t), "_local/b"); b == nil || !slices.Contains(b.DeployedFiles, ".claude/skills/shared/SKILL.md") {
		t.Fatalf("first install: _local/b must record the shared skill, got %+v", b)
	}

	writeStaleCleanupFile(t, "a/.apm/skills/shared/SKILL.md", shared)
	writeStaleCleanupFile(t, "apm.yml", orphanManifestA)
	stdout := staleCleanupInstall(t, "claude")

	assertStaleCleanupExists(t, true, ".claude/skills/shared/SKILL.md")
	assertStaleCleanupExists(t, false, ".claude/rules/b.md", ".claude/skills/sb")
	assertOrphanLockKeys(t, "_local/a")
	if a := orphanLockEntry(readLockfile(t), "_local/a"); !slices.Contains(a.DeployedFiles, ".claude/skills/shared/SKILL.md") {
		t.Errorf("_local/a must record the shared skill, got %q", a.DeployedFiles)
	}
	if !strings.Contains(stdout, orphanCleanedB) {
		t.Errorf("stdout must list only b's own paths, got:\n%s", stdout)
	}
}

// s-git: an orphan from a git source is cleaned the same way; no network is
// needed, because only the lock entry is read.
func TestInstall_OrphanCleanup_RemovesFilesOfGitOrphan(t *testing.T) {
	orphanProject(t, "claude")
	rewriteOrphanLock(t, func(lock *lockfile.Lockfile) {
		b := orphanLockEntry(lock, "_local/b")
		b.RepoURL, b.Source, b.LocalPath, b.Name, b.Version = "github.com/acme/pkgb", "git", "", "", ""
		b.ResolvedCommit = "0123456789abcdef0123456789abcdef01234567"
	})

	writeStaleCleanupFile(t, "apm.yml", orphanManifestA)
	stdout := staleCleanupInstall(t, "claude")

	assertStaleCleanupExists(t, false, ".claude/rules/b.md", ".claude/skills/sb")
	assertOrphanLockKeys(t, "_local/a")
	if !strings.Contains(stdout, orphanCleanedB) {
		t.Errorf("stdout must report and list the three removed paths, got:\n%s", stdout)
	}
}

// s-global: install --global removes the orphan's links, and nothing through
// them.
func TestInstallGlobal_OrphanCleanup_RemovesLinksOfPackageNoLongerDeclared(t *testing.T) {
	home, _ := globalStaleScope(t, nil)
	srcRoot := t.TempDir()
	files := map[string]string{}
	for _, p := range []string{"a", "b"} {
		orphanPackageFiles(files, "", p)
	}
	for rel, content := range files {
		writeGlobalStaleFile(t, filepath.Join(srcRoot, filepath.FromSlash(rel)), content)
	}
	manifestPath := filepath.Join(home, ".apm", "apm.yml")
	a, b := filepath.Join(srcRoot, "a"), filepath.Join(srcRoot, "b")
	writeGlobalStaleFile(t, manifestPath, "name: g\nversion: 1.0.0\ndependencies:\n  apm:\n    - "+a+"\n    - "+b+"\n")
	globalStaleInstall(t, "claude")
	if !globalStaleIsSymlink(t, filepath.Join(home, ".claude", "skills", "sb")) {
		t.Fatal("first install must link skill sb")
	}

	writeGlobalStaleFile(t, manifestPath, "name: g\nversion: 1.0.0\ndependencies:\n  apm:\n    - "+a+"\n")
	stdout := globalStaleInstall(t, "claude")

	for _, rel := range []string{".claude/rules/b.md", ".claude/skills/sb"} {
		if _, err := os.Lstat(filepath.Join(home, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("%s must be removed, Lstat err = %v", rel, err)
		}
	}
	if got := globalStaleRead(t, filepath.Join(b, ".apm", "skills", "sb", "SKILL.md")); got == "" {
		t.Error("the source of the removed skill link must stay")
	}
	if !globalStaleIsSymlink(t, filepath.Join(home, ".claude", "skills", "sa")) {
		t.Error("skill sa must stay linked")
	}
	assertGlobalStaleLock(t, home, false, "_local/b")
	assertGlobalStaleLock(t, home, true, "_local/a")
	if !strings.Contains(stdout, orphanCleanedB) {
		t.Errorf("stdout must report and list the three removed paths, got:\n%s", stdout)
	}
}

// s-update: update with no package cleans as install does.
func TestRunUpdate_OrphanCleanup_RemovesFilesOfPackageNoLongerDeclared(t *testing.T) {
	orphanProject(t, "claude")

	writeStaleCleanupFile(t, "apm.yml", orphanManifestA)
	stdout := orphanUpdate(t, "")

	assertStaleCleanupExists(t, false, ".claude/rules/b.md", ".claude/skills/sb")
	assertOrphanLockKeys(t, "_local/a")
	if !strings.Contains(stdout, orphanCleanedB) {
		t.Errorf("stdout must report and list the three removed paths, got:\n%s", stdout)
	}
}

// s-update3 and s-addpkg: with a package named on the command line the Oracle
// skips the cleanup and keeps the orphan's whole entry in the lock.
func TestOrphanCleanup_NamedPackageKeepsOrphanAndItsEntry(t *testing.T) {
	for name, run := range map[string]func(t *testing.T) string{
		"update ./a":  func(t *testing.T) string { return orphanUpdate(t, "./a") },
		"install ./c": func(t *testing.T) string { return orphanInstallPackages(t, "claude", "./c") },
	} {
		t.Run(name, func(t *testing.T) {
			orphanProject(t, "claude")
			old := *orphanLockEntry(readLockfile(t), "_local/b")

			writeStaleCleanupFile(t, "apm.yml", orphanManifestA)
			stdout := run(t)

			assertStaleCleanupExists(t, true, ".claude/rules/b.md", ".claude/skills/sb/SKILL.md")
			if strings.Contains(stdout, "no longer in apm.yml") {
				t.Errorf("stdout must not report an orphan cleanup, got:\n%s", stdout)
			}
			b := orphanLockEntry(readLockfile(t), "_local/b")
			if b == nil {
				t.Fatalf("lock must keep _local/b, got %q", orphanLockKeys(t))
			}
			if !slices.Equal(b.DeployedFiles, old.DeployedFiles) || len(b.DeployedHashes) != len(old.DeployedHashes) ||
				b.Name != old.Name || b.Version != old.Version || b.Source != old.Source || b.LocalPath != old.LocalPath {
				t.Errorf("_local/b entry changed:\n got %+v\nwant %+v", *b, old)
			}
			for f, h := range old.DeployedHashes {
				if b.DeployedHashes[f] != h {
					t.Errorf("_local/b hash of %s = %q, want %q", f, b.DeployedHashes[f], h)
				}
			}
		})
	}
}

// A path no apm-go target writes (the Oracle's cursor target) is neither
// removed nor carried over.
func TestInstall_OrphanCleanup_LeavesPathOfUnknownTarget(t *testing.T) {
	orphanProject(t, "claude")
	const cursorRule = ".cursor/rules/x.md"
	const content = "cursor rule\n"
	writeStaleCleanupFile(t, cursorRule, content)
	sum := sha256.Sum256([]byte(content))
	rewriteOrphanLock(t, func(lock *lockfile.Lockfile) {
		b := orphanLockEntry(lock, "_local/b")
		b.DeployedFiles = append(b.DeployedFiles, cursorRule)
		b.DeployedHashes[cursorRule] = "sha256:" + hex.EncodeToString(sum[:])
	})

	writeStaleCleanupFile(t, "apm.yml", orphanManifestA)
	stdout := staleCleanupInstall(t, "claude")

	assertStaleCleanupExists(t, true, cursorRule)
	assertStaleCleanupExists(t, false, ".claude/rules/b.md", ".claude/skills/sb")
	assertOrphanLockKeys(t, "_local/a")
	if !strings.Contains(stdout, orphanCleanedB) {
		t.Errorf("stdout must list only the paths under apm-go targets, got:\n%s", stdout)
	}
}

// A transitive package is an orphan once its parent stops declaring it.
func TestInstall_OrphanCleanup_RemovesFilesOfTransitivePackageNoLongerDeclared(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	setTestHome(t, t.TempDir())
	checkoutA := issue69Checkout(t, map[string]string{
		"apm.yml":                             "name: a\nversion: 1.0.0\ndependencies:\n  apm:\n    - git: acme/d\n      ref: main\n",
		".apm/instructions/a.instructions.md": "---\ndescription: a\n---\nrule a\n",
	})
	loader := &issue69GitLoader{t: t, checkouts: map[string]string{
		"acme/a": checkoutA,
		"acme/d": issue69Checkout(t, map[string]string{
			"apm.yml":                             "name: d\nversion: 1.0.0\n",
			".apm/instructions/d.instructions.md": "---\ndescription: d\n---\nrule d\n",
		}),
	}}
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", "name: app\nversion: 1.0.0\ndependencies:\n  apm:\n    - git: acme/a\n      ref: main\n")
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: loader}
	install := func() string {
		return captureUninstallStdout(t, func() {
			if err := runInstall(deps, false, true, "claude", "", nil, nil); err != nil {
				t.Fatalf("install: %v", err)
			}
		})
	}
	install()
	assertStaleCleanupExists(t, true, ".claude/rules/d.md")

	writeCompileFile(t, checkoutA, "apm.yml", "name: a\nversion: 1.0.0\n")
	if err := os.RemoveAll(filepath.Join(dir, "apm_modules", "acme", "a")); err != nil {
		t.Fatal(err)
	}
	stdout := install()

	assertStaleCleanupExists(t, false, ".claude/rules/d.md")
	assertStaleCleanupExists(t, true, ".claude/rules/a.md")
	if keys := orphanLockKeys(t); slices.Contains(keys, "acme/d") || !slices.Contains(keys, "acme/a") {
		t.Errorf("lock dependencies = %q, want acme/a and no acme/d", keys)
	}
	if !strings.Contains(stdout, "Cleaned 1 file from packages no longer in apm.yml\n  - .claude/rules/d.md\n") {
		t.Errorf("stdout must report and list the removed rule, got:\n%s", stdout)
	}
}

// legacyLockB rewrites _local/b in the lock to the format before issue #50:
// repo_url _local/b-<8 hex of the source's absolute path>, no source,
// local_path, name, version or package_type (review-step9/compare.py).
func legacyLockB(t *testing.T) string {
	t.Helper()
	legacy := legacyLocalModulesKey(resolveLocalSourceAbs("./b"))
	rewriteOrphanLock(t, func(lock *lockfile.Lockfile) {
		b := orphanLockEntry(lock, "_local/b")
		b.RepoURL, b.Source, b.LocalPath, b.Name, b.Version, b.PackageType = legacy, "", "", "", "", ""
	})
	if !slices.Contains(orphanLockKeys(t), legacy) {
		t.Fatalf("lock must record b under %s, got %q", legacy, orphanLockKeys(t))
	}
	return legacy
}

// review-step9 legacy: a package recorded in the old format and no longer
// declared is an orphan like any other; Oracle 0.33.0 removes its 3 paths.
func TestInstall_OrphanCleanup_RemovesFilesOfLegacyLocalEntry(t *testing.T) {
	orphanProject(t, "claude")
	legacyLockB(t)

	writeStaleCleanupFile(t, "apm.yml", orphanManifestA)
	stdout := staleCleanupInstall(t, "claude")

	assertStaleCleanupExists(t, false, ".claude/rules/b.md", ".claude/skills/sb/SKILL.md", ".claude/skills/sb")
	assertOrphanLockKeys(t, "_local/a")
	if !strings.Contains(stdout, orphanCleanedB) {
		t.Errorf("stdout must report and list the three removed paths, got:\n%s", stdout)
	}
}

// A package still declared whose old record is in the old format: its old
// record is the package's, so a skill gone from its source is cleaned and
// the lock records the package under its current key with the rule alone.
func TestInstall_StaleCleanup_LegacyLocalEntryOfDeclaredPackage(t *testing.T) {
	orphanProject(t, "claude")
	oldHash := orphanLockEntry(readLockfile(t), "_local/b").DeployedHashes[".claude/rules/b.md"]
	legacyLockB(t)

	if err := os.RemoveAll(filepath.FromSlash("b/.apm/skills/sb")); err != nil {
		t.Fatal(err)
	}
	stdout := staleCleanupInstall(t, "claude")

	assertStaleCleanupExists(t, false, ".claude/skills/sb/SKILL.md", ".claude/skills/sb")
	assertStaleCleanupExists(t, true, ".claude/rules/b.md")
	assertOrphanLockKeys(t, "_local/a", "_local/b")
	b := orphanLockEntry(readLockfile(t), "_local/b")
	if !slices.Equal(b.DeployedFiles, []string{".claude/rules/b.md"}) || b.DeployedHashes[".claude/rules/b.md"] != oldHash {
		t.Errorf("_local/b = %q %v, want only the rule with hash %s", b.DeployedFiles, b.DeployedHashes, oldHash)
	}
	if strings.Contains(stdout, "no longer in apm.yml") {
		t.Errorf("a declared package is no orphan, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Cleaned 2 stale files from ./b\n  - .claude/skills/sb\n  - .claude/skills/sb/SKILL.md\n") {
		t.Errorf("stdout must report the removed skill, got:\n%s", stdout)
	}
}

// unreadableDir makes rel unreadable until the test ends.
func unreadableDir(t *testing.T, rel string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory of mode 000")
	}
	dir := filepath.FromSlash(rel)
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if _, err := os.ReadDir(dir); err == nil {
		t.Skip("directory remains readable after chmod")
	}
}

// review-step9 compare-skill: a file that cannot be removed or read is still
// on disk, so the orphan keeps its record of it.
func TestInstall_OrphanCleanup_KeepsRecordOfUnreadableFile(t *testing.T) {
	orphanProject(t, "claude")
	oldHash := orphanLockEntry(readLockfile(t), "_local/b").DeployedHashes[".claude/skills/sb/SKILL.md"]

	writeStaleCleanupFile(t, "apm.yml", orphanManifestA)
	unreadableDir(t, ".claude/skills/sb")
	staleCleanupInstall(t, "claude")

	b := orphanLockEntry(readLockfile(t), "_local/b")
	if b == nil {
		t.Fatalf("lock must keep _local/b, got %q", orphanLockKeys(t))
	}
	if !slices.Contains(b.DeployedFiles, ".claude/skills/sb/SKILL.md") || b.DeployedHashes[".claude/skills/sb/SKILL.md"] != oldHash {
		t.Errorf("_local/b = %q %v, want .claude/skills/sb/SKILL.md with hash %s", b.DeployedFiles, b.DeployedHashes, oldHash)
	}
}

// The same holds for a stale file of a package that is still declared.
func TestInstall_StaleCleanup_KeepsRecordOfUnreadableFile(t *testing.T) {
	orphanProject(t, "claude")
	oldHash := orphanLockEntry(readLockfile(t), "_local/b").DeployedHashes[".claude/skills/sb/SKILL.md"]

	if err := os.RemoveAll(filepath.FromSlash("b/.apm/skills/sb")); err != nil {
		t.Fatal(err)
	}
	unreadableDir(t, ".claude/skills/sb")
	staleCleanupInstall(t, "claude")

	b := orphanLockEntry(readLockfile(t), "_local/b")
	if b == nil || !slices.Contains(b.DeployedFiles, ".claude/skills/sb/SKILL.md") || b.DeployedHashes[".claude/skills/sb/SKILL.md"] != oldHash {
		t.Errorf("_local/b = %+v, want .claude/skills/sb/SKILL.md with hash %s", b, oldHash)
	}
}

// A file the user deleted before the install is gone: the record leaves.
func TestInstall_OrphanCleanup_DropsRecordOfFileAlreadyGone(t *testing.T) {
	orphanProject(t, "claude")
	staleCleanupRemove(t, ".claude/rules/b.md")

	writeStaleCleanupFile(t, "apm.yml", orphanManifestA)
	stdout := staleCleanupInstall(t, "claude")

	assertStaleCleanupExists(t, false, ".claude/skills/sb")
	assertOrphanLockKeys(t, "_local/a")
	if !strings.Contains(stdout, "Cleaned 2 files from packages no longer in apm.yml\n  - .claude/skills/sb\n  - .claude/skills/sb/SKILL.md\n") {
		t.Errorf("stdout must report the two removed skill paths, got:\n%s", stdout)
	}
}
