package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

// Issue #114: a `git:` entry whose value is a local path is the local
// package that path names, as in Oracle 0.33.0. The expected locks are the
// Oracle's (~/.cache/apm-go-verify/issue114/v1, v2, v3), less the apm_version
// line, which an install with no provenance does not write.

const (
	gitLocalRule     = "---\napplyTo: \"**\"\n---\nrule\n"
	gitLocalDeployed = "---\npaths:\n  - \"**\"\n---\n\nrule\n"
)

const gitLocalOracleLock = `lockfile_version: '1'
dependencies:
- repo_url: _local/repo
  name: pkg
  version: 1.0.0
  package_type: apm_package
  deployed_files:
  - .claude/rules/r.md
  deployed_file_hashes:
    .claude/rules/r.md: sha256:e4d2c9159f7bdc6ee826e9260d631da669d35a4ff909b6f3d58180401a577bed
  source: local
  local_path: ./repo
deployments:
- kind: project-relative
  target: claude
  value: .claude/rules/r.md
  runtime: null
  scope: project
  owners:
  - ./repo
  active_owner: ./repo
  content_hash: sha256:e4d2c9159f7bdc6ee826e9260d631da669d35a4ff909b6f3d58180401a577bed
`

// gitLocalOldLock is the lock apm-go wrote for `git: ./repo` before issue
// #114 (v1/Ag).
const gitLocalOldLock = `lockfile_version: '1'
apm_version: dev
dependencies:
- repo_url: ./repo
  resolved_commit: bea94927c104789f78a76d783397379f4c040a44
  version: 1.0.0
  deployed_files:
  - .claude/rules/r.md
  deployed_file_hashes:
    .claude/rules/r.md: sha256:e4d2c9159f7bdc6ee826e9260d631da669d35a4ff909b6f3d58180401a577bed
  source: git
  tree_sha256: sha256:38e003310a0ab672c9f5d0b55d7c31d19645a28f5598f2a58eef118b29ed5ae9
deployments:
- kind: project-relative
  target: claude
  value: .claude/rules/r.md
  runtime: null
  scope: project
  owners:
  - ./repo
  active_owner: ./repo
  content_hash: sha256:e4d2c9159f7bdc6ee826e9260d631da669d35a4ff909b6f3d58180401a577bed
`

// localOnlyLoader fails the test when it is asked for anything but the copy
// of a local package.
type localOnlyLoader struct{ t *testing.T }

func (l localOnlyLoader) LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error) {
	if ref.LocalSourcePath == "" {
		l.t.Errorf("loader asked to fetch %q at %q", ref.RepoURL, resolvedRef)
		return nil, errors.New("not a local package")
	}
	return subdirRealLoader().LoadPackage(ref, resolvedRef)
}

// gitLocalProject makes the project of the issue: a git repository repo/
// with one rule, tagged v1.0.0, declared by the given apm.yml entry.
func gitLocalProject(t *testing.T, entry string) string {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	subdirGitRepo(t, filepath.Join(project, "repo"), map[string]string{
		"apm.yml":                             "name: pkg\nversion: 1.0.0\n",
		".apm/instructions/r.instructions.md": gitLocalRule,
	})
	gitCmd(t, filepath.Join(project, "repo"), "tag", "v1.0.0")
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: proj\nversion: 1.0.0\ndependencies:\n  apm:\n"+entry)
	return project
}

// noGitCommands puts a git first on PATH that records its arguments and
// fails, and returns the record.
func noGitCommands(t *testing.T) func() string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return func() string { return "" }
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "calls")
	writeGlobalStaleFile(t, filepath.Join(bin, "git"), "#!/bin/sh\necho \"$*\" >> '"+log+"'\nexit 1\n")
	if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() string {
		data, _ := os.ReadFile(log)
		return string(data)
	}
}

func assertLockText(t *testing.T, project, want string) {
	t.Helper()
	if got := globalStaleRead(t, filepath.Join(project, "apm.lock.yaml")); got != want {
		t.Errorf("apm.lock.yaml:\n%s\nwant:\n%s", got, want)
	}
}

func TestInstall_GitLocalPath_FreshCloneFrozen(t *testing.T) {
	for _, tc := range []struct {
		name string
		ci   bool
	}{{"--frozen", false}, {"CI=1", true}} {
		t.Run(tc.name, func(t *testing.T) {
			project := gitLocalProject(t, "    - git: ./repo\n")
			if err := subdirInstall(t, localOnlyLoader{t}, false); err != nil {
				t.Fatalf("install: %v", err)
			}
			assertLockText(t, project, gitLocalOracleLock)
			if err := os.RemoveAll(filepath.Join(project, "apm_modules")); err != nil {
				t.Fatal(err)
			}
			gitCalls := noGitCommands(t)
			frozen := !tc.ci
			if tc.ci {
				t.Setenv("CI", "1")
			}

			var err error
			out := captureUninstallStdout(t, func() {
				err = runInstall(&installDeps{tags: &mockInstallTagLister{}, loader: localOnlyLoader{t}}, frozen, true, "claude", "", nil, nil)
			})

			if err != nil {
				t.Fatalf("frozen install: %v\n%s", err, out)
			}
			assertLockText(t, project, gitLocalOracleLock)
			if got := globalStaleRead(t, filepath.Join(project, ".claude", "rules", "r.md")); got != gitLocalDeployed {
				t.Errorf(".claude/rules/r.md = %q", got)
			}
			if calls := gitCalls(); calls != "" {
				t.Errorf("frozen install ran git:\n%s", calls)
			}
		})
	}
}

// A fresh clone without the deployed files deploys them from the local path.
func TestInstall_GitLocalPath_FrozenDeploysMissingFiles(t *testing.T) {
	project := gitLocalProject(t, "    - git: ./repo\n")
	if err := subdirInstall(t, localOnlyLoader{t}, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	for _, dir := range []string{"apm_modules", ".claude"} {
		if err := os.RemoveAll(filepath.Join(project, dir)); err != nil {
			t.Fatal(err)
		}
	}
	gitCalls := noGitCommands(t)

	if err := subdirInstall(t, localOnlyLoader{t}, true); err != nil {
		t.Fatalf("frozen install: %v", err)
	}

	if got := globalStaleRead(t, filepath.Join(project, ".claude", "rules", "r.md")); got != gitLocalDeployed {
		t.Errorf(".claude/rules/r.md = %q", got)
	}
	assertLockText(t, project, gitLocalOracleLock)
	if calls := gitCalls(); calls != "" {
		t.Errorf("frozen install ran git:\n%s", calls)
	}
}

// The ref is recorded as resolved_ref and checks nothing out: the working
// tree is the content, as the Oracle copies it.
func TestInstall_GitLocalPath_RefIsRecordedOnly(t *testing.T) {
	for _, ref := range []string{"v1.0.0", "^1.0.0"} {
		t.Run(ref, func(t *testing.T) {
			project := gitLocalProject(t, "    - git: ./repo\n      ref: \""+ref+"\"\n")
			writeGlobalStaleFile(t, filepath.Join(project, "repo", ".apm", "instructions", "r.instructions.md"), "---\napplyTo: \"**\"\n---\nuncommitted\n")
			gitCalls := noGitCommands(t)

			if err := subdirInstall(t, localOnlyLoader{t}, false); err != nil {
				t.Fatalf("install: %v", err)
			}

			if got := globalStaleRead(t, filepath.Join(project, ".claude", "rules", "r.md")); got != "---\npaths:\n  - \"**\"\n---\n\nuncommitted\n" {
				t.Errorf(".claude/rules/r.md = %q, want the working tree's", got)
			}
			if dep := subdirLockEntry(t, project, "_local/repo"); dep.ResolvedRef != ref || dep.Source != "local" || dep.ResolvedCommit != "" {
				t.Errorf("resolved_ref %q, source %q, resolved_commit %q; want %q, local, none", dep.ResolvedRef, dep.Source, dep.ResolvedCommit, ref)
			}
			if !strings.Contains(globalStaleRead(t, filepath.Join(project, "apm.lock.yaml")), "  package_type: apm_package\n") {
				t.Error("lock lost the local package fields")
			}
			if calls := gitCalls(); calls != "" {
				t.Errorf("install ran git:\n%s", calls)
			}
		})
	}
}

// With `path:` the Oracle copies the whole directory and deploys its root
// (v2/G2o). Its virtual_path and is_virtual lines are not written.
func TestInstall_GitLocalPath_PathDoesNotSelectContent(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	subdirGitRepo(t, filepath.Join(project, "mono"), map[string]string{
		"apm.yml":                                "name: mono\nversion: 2.0.0\n",
		".apm/instructions/root.instructions.md": "---\napplyTo: \"**\"\n---\nroot\n",
		"pkg/apm.yml":                            "name: pkg\nversion: 1.0.0\n",
		"pkg/.apm/instructions/sub.instructions.md": "---\napplyTo: \"**\"\n---\nsub\n",
	})
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: proj\nversion: 1.0.0\ndependencies:\n  apm:\n    - git: ./mono\n      path: pkg\n")

	if err := subdirInstall(t, localOnlyLoader{t}, false); err != nil {
		t.Fatalf("install: %v", err)
	}

	assertColdAbsent(t, project, ".claude/rules/sub.md")
	assertLockText(t, project, `lockfile_version: '1'
dependencies:
- repo_url: _local/mono
  name: mono
  version: 2.0.0
  package_type: apm_package
  deployed_files:
  - .claude/rules/root.md
  deployed_file_hashes:
    .claude/rules/root.md: sha256:43fbf03ac6826ccb03ae367d587068d4aa4bff08ccbb13d6b77a48e2f642feaf
  source: local
  local_path: ./mono
deployments:
- kind: project-relative
  target: claude
  value: .claude/rules/root.md
  runtime: null
  scope: project
  owners:
  - ./mono
  active_owner: ./mono
  content_hash: sha256:43fbf03ac6826ccb03ae367d587068d4aa4bff08ccbb13d6b77a48e2f642feaf
`)
}

// gitLocalOldProject is the project apm-go installed `git: ./repo` into
// before issue #114: the old lock, the deployed rule, the clone
// apm_modules/repo, and another directory below apm_modules/.
func gitLocalOldProject(t *testing.T) string {
	t.Helper()
	project := gitLocalProject(t, "    - git: ./repo\n")
	writeGlobalStaleFile(t, filepath.Join(project, "apm.lock.yaml"), gitLocalOldLock)
	writeGlobalStaleFile(t, filepath.Join(project, ".claude", "rules", "r.md"), gitLocalDeployed)
	writeGlobalStaleFile(t, filepath.Join(project, "apm_modules", "repo", "apm.yml"), "name: pkg\nversion: 1.0.0\n")
	writeGlobalStaleFile(t, filepath.Join(project, "apm_modules", "keep", "marker"), "k\n")
	return project
}

func TestInstall_GitLocalPath_OldLockIsRewritten(t *testing.T) {
	project := gitLocalOldProject(t)

	var err error
	out := captureUninstallStdout(t, func() {
		err = runInstall(&installDeps{tags: &mockInstallTagLister{}, loader: localOnlyLoader{t}}, false, true, "claude", "", nil, nil)
	})

	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	assertLockText(t, project, gitLocalOracleLock)
	// The Oracle's lines for the same old lock (v3/on).
	if !strings.Contains(out, "  |-- 1 rule(s) adopted -> .claude/rules/\n  |-- (files unchanged)\n") {
		t.Errorf("output does not adopt the rule:\n%s", out)
	}
	assertColdAbsent(t, project, "apm_modules/repo")
	if got := globalStaleRead(t, filepath.Join(project, "apm_modules", "keep", "marker")); got != "k\n" {
		t.Errorf("apm_modules/keep/marker = %q", got)
	}
	if got := globalStaleRead(t, filepath.Join(project, ".claude", "rules", "r.md")); got != gitLocalDeployed {
		t.Errorf(".claude/rules/r.md = %q", got)
	}
}

// A file the old entry records and the package no longer deploys is a stale
// file of the package, as the Oracle reports it (v3/st: "Cleaned 1 stale file
// from ./repo"), not a file of a package no longer in apm.yml.
func TestInstall_GitLocalPath_OldLockStaleFile(t *testing.T) {
	project := gitLocalOldProject(t)
	writeGlobalStaleFile(t, filepath.Join(project, ".claude", "rules", "gone.md"), "gone\n")
	old := strings.Replace(gitLocalOldLock, "  - .claude/rules/r.md\n", "  - .claude/rules/gone.md\n  - .claude/rules/r.md\n", 1)
	old = strings.Replace(old, "  deployed_file_hashes:\n", "  deployed_file_hashes:\n    .claude/rules/gone.md: "+sha256Of("gone\n")+"\n", 1)
	writeGlobalStaleFile(t, filepath.Join(project, "apm.lock.yaml"), old)

	var err error
	out := captureUninstallStdout(t, func() {
		err = runInstall(&installDeps{tags: &mockInstallTagLister{}, loader: localOnlyLoader{t}}, false, true, "claude", "", nil, nil)
	})

	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if !strings.Contains(out, " i Cleaned 1 stale file from ") || strings.Contains(out, "no longer in apm.yml") {
		t.Errorf("output does not clean gone.md as a stale file of the package:\n%s", out)
	}
	assertColdAbsent(t, project, ".claude/rules/gone.md")
	assertLockText(t, project, gitLocalOracleLock)
}

func TestInstall_GitLocalPath_OldLockFrozen(t *testing.T) {
	for _, tc := range []struct {
		name string
		ci   bool
	}{{"--frozen", false}, {"CI=1", true}} {
		t.Run(tc.name, func(t *testing.T) {
			project := gitLocalOldProject(t)
			gitCalls := noGitCommands(t)
			if tc.ci {
				t.Setenv("CI", "1")
			}

			var err error
			out := captureUninstallStdout(t, func() {
				err = runInstall(&installDeps{tags: &mockInstallTagLister{}, loader: localOnlyLoader{t}}, !tc.ci, true, "claude", "", nil, nil)
			})

			if err != nil {
				t.Fatalf("frozen install: %v\n%s", err, out)
			}
			if !strings.Contains(out, "Frozen install: all dependencies pinned and verified") {
				t.Errorf("output:\n%s", out)
			}
			assertLockText(t, project, gitLocalOldLock)
			if calls := gitCalls(); calls != "" {
				t.Errorf("frozen install ran git:\n%s", calls)
			}
		})
	}
}

func TestUninstall_GitLocalPath_OldLock(t *testing.T) {
	project := gitLocalOldProject(t)

	var err error
	out := captureUninstallStdout(t, func() { err = runUninstall([]string{"./repo"}, uninstallOptions{}) })

	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	// The Oracle's result for the same old lock (v3/un).
	if !strings.Contains(out, " i Cleaned 1 stale file from ./repo\n") {
		t.Errorf("output does not clean the rule:\n%s", out)
	}
	assertColdAbsent(t, project, ".claude/rules/r.md", "apm.lock.yaml")
	if got := globalStaleRead(t, filepath.Join(project, "apm.yml")); got != "name: proj\nversion: 1.0.0\ndependencies:\n  apm: []\n" {
		t.Errorf("apm.yml = %q", got)
	}
}

// The declared ref of a local package does not change, so update --dry-run
// plans nothing for it, as for a local package with no ref.
func TestUpdate_GitLocalPath_DryRunPlansNoRefChange(t *testing.T) {
	project := gitLocalProject(t, "    - git: ./repo\n      ref: \"^1.0.0\"\n")
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n    - git: ./repo\n      ref: \"^1.0.0\"\n")
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: localOnlyLoader{t}}
	if err := subdirInstall(t, deps.loader, false); err != nil {
		t.Fatalf("install: %v", err)
	}

	var err error
	out := captureUninstallStdout(t, func() { err = runUpdate(deps, false, false, "", true, "") })

	if err != nil {
		t.Fatalf("update --dry-run: %v\n%s", err, out)
	}
	if strings.Contains(out, "_local/repo") {
		t.Errorf("update --dry-run plans a change for the local package:\n%s", out)
	}
}
