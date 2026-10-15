package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/semver"
)

// Issue #86: a directory below apm_modules that leads out of apm_modules
// through a symlink is neither removed nor written, as in Oracle 0.33.0.

const issue86Rule = "---\napplyTo: \"**\"\n---\nrule\n"

// tree86 lists every path under dir with its content or link target, without
// following symlinks.
func tree86(t *testing.T, dir string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			got[rel] = "-> " + target
			return err
		case d.IsDir():
			got[rel] = "dir"
		default:
			data, err := os.ReadFile(p)
			got[rel] = string(data)
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func assertTree86(t *testing.T, dir string, before map[string]string) {
	t.Helper()
	if got := tree86(t, dir); !reflect.DeepEqual(got, before) {
		t.Errorf("%s changed:\n%v\nwant\n%v", dir, got, before)
	}
}

func link86(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}
}

func realDir86(t *testing.T, p string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func cwd86(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

// containment86 is the Oracle's reason for apm_modules/<key>
// (utils/path_security.py:205-221).
func containment86(abs, base, resolved string) string {
	return fmt.Sprintf("Cannot verify containment of '%s' within '%s': Path '%s' resolves to '%s' which is outside the allowed base directory '%s'",
		abs, base, abs, resolved, base)
}

func uninstallRefused86(label, reason, recovery string) string {
	return " x Refusing to remove " + label + ": " + reason + "\n" +
		" x Uninstall incomplete: package deletion failed. apm.yml and lockfile ownership were retained; some package files may already have been removed. Resolve the unsafe package path and retry the same uninstall command, or run '" + recovery + "' to restore the declared packages. Run with --verbose for detailed diagnostics.\n"
}

// installedLocal86 installs ../dep, which deploys a rule and a skill with a
// data directory, and returns the directory holding proj/, dep/ and outside/.
func installedLocal86(t *testing.T) string {
	t.Helper()
	root := upDepProject(t, "  apm:\n    - ../dep\n")
	writeCompileFile(t, root, "dep/.apm/instructions/r.instructions.md", issue86Rule)
	writeCompileFile(t, root, "dep/.apm/skills/sk/data/precious.txt", "precious\n")
	writeCompileFile(t, root, "outside/other.txt", "other\n")
	mustRunCLI(t, "install")
	assertIssue50Exists(t, true, ".claude/rules/r.md", "apm_modules/_local/dep/apm.yml")
	return root
}

// moveLocalOutside86 moves the copy of ../dep to outside/dep, adds a file
// the package does not have, and links what moved: apm_modules/_local (L1)
// or apm_modules/_local/dep (L2).
func moveLocalOutside86(t *testing.T, root string, parent bool) {
	t.Helper()
	outside := filepath.Join(root, "outside")
	if err := os.Rename(filepath.Join("apm_modules", "_local", "dep"), filepath.Join(outside, "dep")); err != nil {
		t.Fatal(err)
	}
	writeCompileFile(t, root, "outside/dep/precious.txt", "precious\n")
	if parent {
		if err := os.Remove(filepath.Join("apm_modules", "_local")); err != nil {
			t.Fatal(err)
		}
		link86(t, outside, filepath.Join("apm_modules", "_local"))
		return
	}
	link86(t, filepath.Join(outside, "dep"), filepath.Join("apm_modules", "_local", "dep"))
}

// Test 2: L1, L2, L3. Every path is checked before the first write, so the
// deployed rule, apm.yml and the lock stay too.
func TestUninstall_ModuleDirOutsideApmModules_Refused(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, root string)
		resolved string
	}{
		{name: "L1 parent link", setup: func(t *testing.T, root string) { moveLocalOutside86(t, root, true) }, resolved: "outside/dep"},
		{name: "L2 leaf link", setup: func(t *testing.T, root string) { moveLocalOutside86(t, root, false) }, resolved: "outside/dep"},
		{name: "L3 dangling leaf link", setup: func(t *testing.T, root string) {
			if err := os.RemoveAll(filepath.Join("apm_modules", "_local", "dep")); err != nil {
				t.Fatal(err)
			}
			link86(t, filepath.Join(root, "outside", "gone"), filepath.Join("apm_modules", "_local", "dep"))
		}, resolved: "outside/gone"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := installedLocal86(t)
			tt.setup(t, root)
			before := tree86(t, root)

			stdout, _, code := runCompileCLI(t, "uninstall", "../dep")

			real := realDir86(t, root)
			reason := containment86(filepath.Join(cwd86(t), "apm_modules", "_local", "dep"),
				filepath.Join(real, "proj", "apm_modules"), filepath.Join(real, filepath.FromSlash(tt.resolved)))
			want := " > Uninstalling 1 package(s)...\n + _local/dep - found in apm.yml\n" + uninstallRefused86("_local/dep", reason, "apm-go install")
			if code != 1 || stdout != want {
				t.Errorf("exit %d, stdout:\n%s\nwant exit 1, stdout:\n%s", code, stdout, want)
			}
			assertTree86(t, root, before)
			assertIssue50Exists(t, true, ".claude/rules/r.md")
		})
	}
}

// Test 3: a transitive package whose parent directory leads outside. The
// direct package's directory and deployed file stay: the Oracle has removed
// the direct directory by then, apm-go checks every key first.
func TestUninstall_TransitiveModuleDirOutsideApmModules_Refused(t *testing.T) {
	root := t.TempDir()
	writeCompileFile(t, root, "proj/apm.yml", "name: test\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n    - acme/foo\n")
	writeCompileFile(t, root, "proj/apm_modules/acme/foo/apm.yml", "name: foo\nversion: 1.0.0\ndependencies:\n  apm:\n    - other/child\n")
	writeCompileFile(t, root, "proj/.claude/rules/foo.md", "foo\n")
	writeCompileFile(t, root, "outside/other/child/precious.txt", "precious\n")
	t.Chdir(filepath.Join(root, "proj"))
	link86(t, filepath.Join(root, "outside", "other"), filepath.Join("apm_modules", "other"))
	sum := sha256.Sum256([]byte("foo\n"))
	writeUninstallLockfileFixture(t, &lockfile.Lockfile{Dependencies: []lockfile.LockedDep{
		{RepoURL: "acme/foo", Source: "git", DeployedFiles: []string{".claude/rules/foo.md"},
			DeployedHashes: map[string]string{".claude/rules/foo.md": "sha256:" + hex.EncodeToString(sum[:])}},
		{RepoURL: "other/child", Source: "git", ResolvedBy: "acme/foo", Depth: 2},
	}})
	before := tree86(t, root)

	stdout, _, code := runCompileCLI(t, "uninstall", "acme/foo")

	real := realDir86(t, root)
	reason := containment86(filepath.Join(cwd86(t), "apm_modules", "other", "child"),
		filepath.Join(real, "proj", "apm_modules"), filepath.Join(real, "outside", "other", "child"))
	want := " > Uninstalling 1 package(s)...\n + acme/foo - found in apm.yml\n" + uninstallRefused86("transitive dep other/child", reason, "apm-go install")
	if code != 1 || stdout != want {
		t.Errorf("exit %d, stdout:\n%s\nwant exit 1, stdout:\n%s", code, stdout, want)
	}
	assertTree86(t, root, before)
}

// Test 4: L1 under ~/.apm.
func TestUninstallGlobal_ModuleDirOutsideApmModules_Refused(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	writeCompileFile(t, root, "home/.apm/apm.yml", "name: global\nversion: 0.0.0\ntarget: claude\ndependencies:\n  apm:\n    - ../dep\n")
	writeCompileFile(t, root, "home/dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	writeCompileFile(t, root, "home/dep/.apm/instructions/r.instructions.md", issue86Rule)
	writeCompileFile(t, root, "outside/other.txt", "other\n")
	t.Chdir(root)
	mustRunCLI(t, "install", "--global")
	mods := filepath.Join(home, ".apm", "apm_modules")
	if err := os.Rename(filepath.Join(mods, "_local", "dep"), filepath.Join(root, "outside", "dep")); err != nil {
		t.Fatal(err)
	}
	writeCompileFile(t, root, "outside/dep/precious.txt", "precious\n")
	if err := os.Remove(filepath.Join(mods, "_local")); err != nil {
		t.Fatal(err)
	}
	link86(t, filepath.Join(root, "outside"), filepath.Join(mods, "_local"))
	before := tree86(t, root)

	stdout, _, code := runCompileCLI(t, "uninstall", "--global", "../dep")

	real := realDir86(t, root)
	reason := containment86(filepath.Join(home, ".apm", "apm_modules", "_local", "dep"),
		filepath.Join(real, "home", ".apm", "apm_modules"), filepath.Join(real, "outside", "dep"))
	want := " > Uninstalling 1 package(s)...\n + _local/dep - found in apm.yml\n" + uninstallRefused86("_local/dep", reason, "apm-go install --global")
	if code != 1 || stdout != want {
		t.Errorf("exit %d, stdout:\n%s\nwant exit 1, stdout:\n%s", code, stdout, want)
	}
	assertTree86(t, root, before)
}

// Test 5: L1 dry run. The Oracle prints no "from apm_modules/" line for a
// directory it would refuse (commands/uninstall/engine.py:751-767).
func TestUninstallDryRun_ModuleDirOutsideApmModules(t *testing.T) {
	root := installedLocal86(t)
	moveLocalOutside86(t, root, true)
	before := tree86(t, root)

	stdout, _, code := runCompileCLI(t, "uninstall", "--dry-run", "../dep")

	want := " > Uninstalling 1 package(s)...\n + _local/dep - found in apm.yml\n" +
		" i Dry run: Would remove 1 package(s):\n i   - _local/dep from apm.yml\n + Dry run complete - no changes made\n"
	if code != 0 || stdout != want {
		t.Errorf("exit %d, stdout:\n%s\nwant exit 0, stdout:\n%s", code, stdout, want)
	}
	assertTree86(t, root, before)
}

// Test 6: apm_modules itself linked outside the project is compared resolved
// on both sides, so uninstall runs as it does without the link.
func TestUninstall_SymlinkedApmModules_Removes(t *testing.T) {
	installedLocal86(t)
	plain, _, plainCode := runCompileCLI(t, "uninstall", "../dep")
	plain = strings.ReplaceAll(plain, cwd86(t), "<proj>")
	plainTree := tree86(t, filepath.Join(cwd86(t), "apm_modules"))

	root := installedLocal86(t)
	moved := filepath.Join(root, "outside", "modules")
	if err := os.Rename("apm_modules", moved); err != nil {
		t.Fatal(err)
	}
	link86(t, moved, "apm_modules")

	stdout, _, code := runCompileCLI(t, "uninstall", "../dep")
	stdout = strings.ReplaceAll(stdout, cwd86(t), "<proj>")

	if code != plainCode || code != 0 || stdout != plain {
		t.Errorf("exit %d, stdout:\n%s\nwant exit %d, stdout:\n%s", code, stdout, plainCode, plain)
	}
	if got := tree86(t, moved); !reflect.DeepEqual(got, plainTree) {
		t.Errorf("outside/modules = %v, want %v", got, plainTree)
	}
	assertIssue50Exists(t, false, "apm.lock.yaml", ".claude/rules/r.md")
}

// Test 8: the req-lk-010 clear of a git semver dependency before update.
func TestUpdate_ClearSemverModuleDirOutside_Refused(t *testing.T) {
	root := t.TempDir()
	writeCompileFile(t, root, "proj/apm.yml", "name: test\nversion: \"1.0.0\"\ntarget:\n  - claude\ndependencies:\n  apm:\n    - acme/a#^1.0.0\n")
	writeCompileFile(t, root, "proj/apm.lock.yaml", "lockfile_version: \"1\"\ndependencies:\n  - repo_url: acme/a\n    source: git\n    constraint: \"^1.0.0\"\n    resolved_tag: v1.2.0\n    depth: 1\n")
	writeCompileFile(t, root, "outside/acme/a/precious.txt", "precious\n")
	t.Chdir(filepath.Join(root, "proj"))
	link86(t, filepath.Join(root, "outside", "acme"), filepath.Join("apm_modules", "acme"))
	before := tree86(t, root)
	deps := &installDeps{
		tags:   &mockInstallTagLister{tags: map[string][]semver.TagInfo{"acme/a": {{Name: "v1.2.0"}, {Name: "v1.9.0"}}}},
		loader: &mockInstallLoader{packages: map[string]*manifest.Manifest{"acme/a@v1.9.0": {Name: "a", Version: "1.9.0"}}},
	}

	err := runUpdate(deps, false, false, "", false)

	real := realDir86(t, root)
	want := `refusing to clear "acme/a": ` + containment86(filepath.Join(cwd86(t), "apm_modules", "acme", "a"),
		filepath.Join(real, "proj", "apm_modules"), filepath.Join(real, "outside", "acme", "a"))
	if err == nil || err.Error() != want || exitCodeOf(err) != 1 {
		t.Errorf("runUpdate: %v (exit %d)\nwant exit 1: %s", err, exitCodeOf(err), want)
	}
	assertTree86(t, root, before)
}

// Test 9: a registry package, extracted by registry.Loader on install and by
// the frozen replay, below a parent linked outside.
func TestRegistry_ModuleDirOutsideApmModules_Refused(t *testing.T) {
	for _, frozen := range []bool{false, true} {
		t.Run(fmt.Sprintf("frozen=%v", frozen), func(t *testing.T) {
			enableRegistries(t)
			fx := buildRegistryFixture(t)
			sum := sha256.Sum256(fx)
			digest := "sha256:" + hex.EncodeToString(sum[:])
			srv := startE2EServer(t, fx, digest)
			root := t.TempDir()
			writeCompileFile(t, root, "proj/apm.yml", fmt.Sprintf("name: consumer\nversion: 0.1.0\ntarget: claude\nregistries:\n  local:\n    url: %s\n    insecure: true\n  default: local\ndependencies:\n  apm:\n    - id: acme/sample\n      version: 1.0.0\n", srv.URL))
			if frozen {
				writeCompileFile(t, root, "proj/apm.lock.yaml", fmt.Sprintf("lockfile_version: \"2\"\ndependencies:\n"+
					"  - repo_url: acme/sample\n    source: registry\n"+
					"    resolved_url: %s/v1/packages/acme/sample/versions/1.0.0/download\n"+
					"    resolved_hash: %s\n    version: \"1.0.0\"\n    depth: 1\n", srv.URL, digest))
			}
			writeCompileFile(t, root, "outside/acme/sample/precious.txt", "precious\n")
			t.Chdir(filepath.Join(root, "proj"))
			link86(t, filepath.Join(root, "outside", "acme"), filepath.Join("apm_modules", "acme"))
			before := tree86(t, root)

			deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
			err := runInstall(deps, frozen, false, "", "", nil, nil)

			real := realDir86(t, root)
			reason := containment86(filepath.Join(cwd86(t), "apm_modules", "acme", "sample"),
				filepath.Join(real, "proj", "apm_modules"), filepath.Join(real, "outside", "acme", "sample"))
			want := `resolve: loading package registry:"":"acme/sample": ` + reason
			if frozen {
				want = `frozen install: refusing to extract "acme/sample": ` + reason
			}
			if err == nil || err.Error() != want || exitCodeOf(err) != 1 {
				t.Errorf("runInstall: %v (exit %d)\nwant exit 1: %s", err, exitCodeOf(err), want)
			}
			assertTree86(t, root, before)
		})
	}
}

// Test 10: L5, apm_modules/_local/dep linked outside: install and update
// refuse with checkLocalCopy's text and leave the link and its target.
func TestLocalCopy_DestinationLinkOutside_Refused(t *testing.T) {
	for _, cmd := range []string{"install", "update"} {
		t.Run(cmd, func(t *testing.T) {
			root := installedLocal86(t)
			moveLocalOutside86(t, root, false)
			before := tree86(t, root)

			stdout, _, code := runCompileCLI(t, cmd)

			want := " x local dependency ../dep would be copied to apm_modules/_local/dep, which resolves to " +
				filepath.Join(realDir86(t, root), "outside", "dep") + " outside apm_modules/; refusing to replace it\n"
			if code != 1 || stdout != want {
				t.Errorf("exit %d, stdout:\n%s\nwant exit 1, stdout:\n%s", code, stdout, want)
			}
			assertTree86(t, root, before)
		})
	}
}

// Test 10, the other half: a link that stays inside apm_modules is replaced
// by the copy, as before.
func TestLocalCopy_DestinationLinkInside_Copies(t *testing.T) {
	for _, cmd := range []string{"install", "update"} {
		t.Run(cmd, func(t *testing.T) {
			root := installedLocal86(t)
			other := filepath.Join(cwd86(t), "apm_modules", "other")
			if err := os.Rename(filepath.Join("apm_modules", "_local", "dep"), other); err != nil {
				t.Fatal(err)
			}
			link86(t, other, filepath.Join("apm_modules", "_local", "dep"))
			otherBefore := tree86(t, other)

			mustRunCLI(t, cmd)

			if info, err := os.Lstat(filepath.Join("apm_modules", "_local", "dep")); err != nil || !info.IsDir() {
				t.Errorf("apm_modules/_local/dep is not a directory (lstat %v, error %v)", info, err)
			}
			assertTree86(t, other, otherBefore)
			if got, want := tree86(t, filepath.Join("apm_modules", "_local", "dep")), tree86(t, filepath.Join(root, "dep")); !reflect.DeepEqual(got, want) {
				t.Errorf("apm_modules/_local/dep = %v, want the source %v", got, want)
			}
		})
	}
}
