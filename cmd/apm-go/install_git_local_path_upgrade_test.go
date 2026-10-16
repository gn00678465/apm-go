package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// gitPathRule is the rule deployed from an instruction with body text.
func gitPathRule(text string) string {
	return "---\npaths:\n  - \"**\"\n---\n\n" + text + "\n"
}

// gitPathPackage is a package a lock before issue #114 records under its
// `git:` path: its instructions (name -> body) and the clone apm-go made of
// it below apm_modules/.
type gitPathPackage struct {
	path  string
	rules map[string]string
}

// gitPathOldProject makes a project whose apm.yml declares each package as
// `git: <path>` and whose lock, deployed rules and apm_modules/ are what
// apm-go wrote for it before issue #114. A rule two packages have is the
// first package's on disk and in the lock, as that install deployed it.
func gitPathOldProject(t *testing.T, pkgs ...gitPathPackage) string {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	var manifestEntries, lockEntries strings.Builder
	deployed := map[string]bool{}
	for _, p := range pkgs {
		files := map[string]string{"apm.yml": "name: " + filepath.Base(p.path) + "\nversion: 1.0.0\n"}
		var paths, hashes strings.Builder
		for name, body := range p.rules {
			files[".apm/instructions/"+name+".instructions.md"] = "---\napplyTo: \"**\"\n---\n" + body + "\n"
			rel := ".claude/rules/" + name + ".md"
			if deployed[rel] {
				continue
			}
			deployed[rel] = true
			writeGlobalStaleFile(t, filepath.Join(project, filepath.FromSlash(rel)), gitPathRule(body))
			paths.WriteString("  - " + rel + "\n")
			hashes.WriteString("    " + rel + ": " + sha256Of(gitPathRule(body)) + "\n")
		}
		commit := subdirGitRepo(t, filepath.Join(project, filepath.FromSlash(p.path)), files)
		for rel, content := range files {
			writeGlobalStaleFile(t, filepath.Join(project, "apm_modules", filepath.FromSlash(p.path), filepath.FromSlash(rel)), content)
		}
		manifestEntries.WriteString("    - git: " + p.path + "\n")
		fmt.Fprintf(&lockEntries, "- repo_url: %s\n  resolved_commit: %s\n  version: 1.0.0\n  deployed_files:\n%s  deployed_file_hashes:\n%s  source: git\n", p.path, commit, paths.String(), hashes.String())
	}
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n"+manifestEntries.String())
	writeGlobalStaleFile(t, filepath.Join(project, "apm.lock.yaml"), "lockfile_version: '1'\ndependencies:\n"+lockEntries.String())
	return project
}

func gitPathLock(t *testing.T, project string) *lockfile.Lockfile {
	t.Helper()
	node, err := yamlcore.SafeLoad([]byte(globalStaleRead(t, filepath.Join(project, "apm.lock.yaml"))))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lockfile.ParseLockfile(node)
	if err != nil {
		t.Fatal(err)
	}
	return lock
}

// Review of PR #121, finding 1: the clone a `git:` path names can be the
// directory its local copy now takes, or hold it. Install and update keep
// it, and what they deployed from it.
func TestInstall_GitLocalPath_OldCloneOverlapsLocalCopy(t *testing.T) {
	for _, tc := range []struct{ path, key string }{
		{"./_local/repo", "_local/repo"},
		{"./_local", "_local/_local"},
	} {
		for _, update := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s update=%v", tc.path, update), func(t *testing.T) {
				project := gitPathOldProject(t, gitPathPackage{path: tc.path, rules: map[string]string{"r": "rule"}})
				deps := &installDeps{tags: &mockInstallTagLister{}, loader: localOnlyLoader{t}}

				var err error
				out := captureUninstallStdout(t, func() {
					if update {
						err = runUpdate(deps, false, false, "", false)
					} else {
						err = runInstall(deps, false, true, "", "", nil, nil)
					}
				})

				if err != nil {
					t.Fatalf("%v\n%s", err, out)
				}
				copied := filepath.Join(project, "apm_modules", filepath.FromSlash(tc.key))
				if got := globalStaleRead(t, filepath.Join(copied, ".apm", "instructions", "r.instructions.md")); got != "---\napplyTo: \"**\"\n---\nrule\n" {
					t.Errorf("%s holds %q", copied, got)
				}
				if got := globalStaleRead(t, filepath.Join(project, ".claude", "rules", "r.md")); got != gitPathRule("rule") {
					t.Errorf(".claude/rules/r.md = %q", got)
				}
				if dep := subdirLockEntry(t, project, tc.key); dep.Source != "local" || dep.LocalPath != tc.path {
					t.Errorf("lock entry %s: source %q, local_path %q", tc.key, dep.Source, dep.LocalPath)
				}
			})
		}
	}
}

// The other directories removeLegacyLocalModules is not to remove: one an old
// key names that is, holds or lies in the directory of a package of the new
// lock under another key, also for a lock before issue #50.
func TestRemoveLegacyLocalModules_KeepsDirectoriesOfNewPackages(t *testing.T) {
	for _, tc := range []struct {
		name    string
		old     lockfile.LockedDep
		current lockfile.LockedDep
	}{
		{"same directory as a git package", lockfile.LockedDep{RepoURL: "./acme/repo", Source: "git"}, lockfile.LockedDep{RepoURL: "acme/repo", Source: "git"}},
		{"inside a git package", lockfile.LockedDep{RepoURL: "/home/u/x", Source: "git"}, lockfile.LockedDep{RepoURL: "home/u", Source: "git"}},
		{"holds a local copy", lockfile.LockedDep{RepoURL: "./_local", Source: "git"}, lockfile.LockedDep{RepoURL: "_local/_local", Source: "local", LocalPath: "./_local"}},
		{"before issue #50, holds a package", lockfile.LockedDep{RepoURL: "_local/dep-0123abcd", Source: "git"}, lockfile.LockedDep{RepoURL: "_local/dep-0123abcd/sub", Source: "git"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project := chdirTemp(t)
			markers := []string{
				filepath.Join(project, "apm_modules", filepath.FromSlash(tc.current.RepoURL), "marker"),
				filepath.Join(project, moduleDirOf(tc.old.RepoURL), "old-marker"),
			}
			for _, m := range markers {
				writeGlobalStaleFile(t, m, "keep\n")
			}
			newLock := &lockfile.Lockfile{Dependencies: []lockfile.LockedDep{tc.current, {RepoURL: "_local/x", Source: "local", LocalPath: tc.old.RepoURL}}}

			removeLegacyLocalModules(&lockfile.Lockfile{Dependencies: []lockfile.LockedDep{tc.old}}, newLock)

			for _, m := range markers {
				if got := globalStaleRead(t, m); got != "keep\n" {
					t.Errorf("%s = %q", m, got)
				}
			}
		})
	}
	t.Run("control", func(t *testing.T) {
		project := chdirTemp(t)
		writeGlobalStaleFile(t, filepath.Join(project, "apm_modules", "repo", "apm.yml"), "name: repo\n")
		newLock := &lockfile.Lockfile{Dependencies: []lockfile.LockedDep{{RepoURL: "_local/repo", Source: "local", LocalPath: "./repo"}}}

		removeLegacyLocalModules(&lockfile.Lockfile{Dependencies: []lockfile.LockedDep{{RepoURL: "./repo", Source: "git"}}}, newLock)

		assertColdAbsent(t, project, "apm_modules/repo")
	})
}

// Review of PR #121, finding 2: uninstall turns only the entry it removes. A
// package that stays keeps its old entry and clone, compile reads it, and a
// rule it shares with the removed package is handed over to it under its old
// key; the next install turns it.
func TestUninstall_GitLocalPath_SurvivorKeepsOldEntry(t *testing.T) {
	project := gitPathOldProject(t,
		gitPathPackage{path: "./first", rules: map[string]string{"r": "first-shared", "first": "first-own"}},
		gitPathPackage{path: "./second", rules: map[string]string{"r": "second-shared", "second": "second-own"}},
	)
	before := *gitPathLock(t, project).FindByKey("./second")

	var err error
	out := captureUninstallStdout(t, func() { err = runUninstall([]string{"./first"}, uninstallOptions{}) })

	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if got := globalStaleRead(t, filepath.Join(project, ".claude", "rules", "r.md")); got != gitPathRule("second-shared") {
		t.Errorf("shared rule = %q, want the version of ./second", got)
	}
	assertColdAbsent(t, project, ".claude/rules/first.md")
	lock := gitPathLock(t, project)
	if len(lock.Dependencies) != 1 {
		t.Fatalf("lock has %d entries, want ./second alone", len(lock.Dependencies))
	}
	after := lock.Dependencies[0]
	if after.RepoURL != "./second" || after.Source != "git" || after.ResolvedCommit != before.ResolvedCommit || after.LocalPath != "" {
		t.Errorf("./second entry is now repo_url %q, source %q, resolved_commit %q, local_path %q; want it as before", after.RepoURL, after.Source, after.ResolvedCommit, after.LocalPath)
	}
	if after.DeployedHashes[".claude/rules/r.md"] != sha256Of(gitPathRule("second-shared")) || after.DeployedHashes[".claude/rules/second.md"] != before.DeployedHashes[".claude/rules/second.md"] {
		t.Errorf("./second hashes = %v", after.DeployedHashes)
	}
	if got := globalStaleRead(t, filepath.Join(project, "apm_modules", "second", "apm.yml")); got != "name: second\nversion: 1.0.0\n" {
		t.Errorf("apm_modules/second/apm.yml = %q", got)
	}

	if _, stderr, code := runCompileCLI(t, "compile", "--target", "codex"); code != 0 {
		t.Fatalf("compile: exit %d\n%s", code, stderr)
	}
	if agents := globalStaleRead(t, filepath.Join(project, "AGENTS.md")); !strings.Contains(agents, "second-own") {
		t.Errorf("AGENTS.md lacks the instruction of ./second:\n%s", agents)
	}

	if err := subdirInstall(t, localOnlyLoader{t}, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	if dep := subdirLockEntry(t, project, "_local/second"); dep.Source != "local" || dep.LocalPath != "./second" {
		t.Errorf("_local/second: source %q, local_path %q", dep.Source, dep.LocalPath)
	}
	assertColdAbsent(t, project, "apm_modules/second")
	if got := globalStaleRead(t, filepath.Join(project, ".claude", "rules", "r.md")); got != gitPathRule("second-shared") {
		t.Errorf("shared rule after install = %q", got)
	}
}

// The rule handed over goes to the package that stays and is declared first,
// as install gives it, also when the lock sorts the old entries otherwise.
func TestUninstall_GitLocalPath_HandoverFollowsDeclarationOrder(t *testing.T) {
	project := gitPathOldProject(t,
		gitPathPackage{path: "./first", rules: map[string]string{"r": "first-shared"}},
		gitPathPackage{path: "./zeta", rules: map[string]string{"r": "zeta-shared"}},
		gitPathPackage{path: "./alpha", rules: map[string]string{"r": "alpha-shared"}},
	)

	var err error
	out := captureUninstallStdout(t, func() { err = runUninstall([]string{"./first"}, uninstallOptions{}) })

	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if got := globalStaleRead(t, filepath.Join(project, ".claude", "rules", "r.md")); got != gitPathRule("zeta-shared") {
		t.Errorf("shared rule = %q, want the version of ./zeta, declared before ./alpha", got)
	}
	if dep := gitPathLock(t, project).FindByKey("./zeta"); dep == nil || dep.DeployedHashes[".claude/rules/r.md"] != sha256Of(gitPathRule("zeta-shared")) {
		t.Errorf("./zeta does not record the shared rule: %+v", dep)
	}
}
