package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// Issue #119: a package whose files have CRLF line endings (git's autocrlf on
// Windows, or a file committed that way) deploys and records what the same
// package with LF line endings does, as Oracle 0.33.0 does: agents,
// instructions, prompts and commands are written with LF (utils/atomic_io.py:
// 28-48), skills and hooks are copied as they are, and the lock records each
// file's hash over its CRLF -> LF form (utils/content_hash.py:89-153).

const crlfTargets = "claude,copilot,codex"

// crlfPackageFiles is a package with one primitive of each kind, its lines
// ending with eol.
func crlfPackageFiles(eol string) map[string]string {
	files := map[string]string{
		".apm/agents/rev.agent.md":                "---\ndescription: reviews code\n---\nReview the code.\n",
		".apm/instructions/style.instructions.md": "---\napplyTo: \"**/*.go\"\n---\nUse gofmt.\n",
		".apm/prompts/hello.prompt.md":            "---\ndescription: say hello\n---\nSay hello.\n",
		".apm/commands/ship.md":                   "---\ndescription: ship it\n---\nShip it.\n",
		".apm/skills/sk/SKILL.md":                 "---\nname: sk\ndescription: a skill\n---\nSkill body.\n",
		".apm/hooks/h.json":                       "{\n  \"version\": 1\n}\n",
	}
	for k, v := range files {
		files[k] = strings.ReplaceAll(v, "\n", eol)
	}
	return files
}

// crlfDeployedPaths are the files the package deploys to crlfTargets; the
// ones copied as they are say so.
var crlfDeployedPaths = map[string]bool{
	".claude/agents/rev.md":                      false,
	".github/agents/rev.agent.md":                false,
	".codex/agents/rev.toml":                     false,
	".claude/rules/style.md":                     false,
	".github/instructions/style.instructions.md": false,
	".github/prompts/hello.prompt.md":            false,
	".claude/commands/hello.md":                  false,
	".claude/commands/ship.md":                   false,
	".claude/skills/sk/SKILL.md":                 true,
	".agents/skills/sk/SKILL.md":                 true,
	".github/hooks/{pkg}-h.json":                 true,
}

func writeCRLFFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		writeGlobalStaleFile(t, filepath.Join(dir, filepath.FromSlash(rel)), content)
	}
}

func crlfInstall(t *testing.T, frozen bool, packages ...string) (string, error) {
	t.Helper()
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: subdirPathLoader{subdirRealLoader()}}
	var err error
	out := captureUninstallStdout(t, func() {
		err = runInstall(deps, frozen, true, crlfTargets, "", nil, packages)
	})
	return out, err
}

func crlfLock(t *testing.T, project string) *lockfile.Lockfile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(project, "apm.lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lockfile.ParseLockfile(node)
	if err != nil {
		t.Fatal(err)
	}
	return lock
}

// crlfBucketHashes is the deployed_file_hashes of the one bucket a project
// records: the dependency's, or the project's own.
func crlfBucketHashes(t *testing.T, project string) map[string]string {
	t.Helper()
	lock := crlfLock(t, project)
	if len(lock.Dependencies) == 1 {
		return lock.Dependencies[0].DeployedHashes
	}
	if len(lock.Dependencies) == 0 {
		return lock.LocalDeployedHashes
	}
	t.Fatalf("lock has %d dependencies, want one", len(lock.Dependencies))
	return nil
}

// assertCRLFDeploy checks that crlfProject, whose package has CRLF line
// endings, deployed what lfProject, whose package has LF ones, deployed: the
// same bytes, CRLF kept in skills and hooks, and the same lock hashes.
// hookPkg is the package a copilot hook file is named after
// (integration/hook_integrator.py:1065-1067).
func assertCRLFDeploy(t *testing.T, lfProject, crlfProject, hookPkg string) {
	t.Helper()
	lfHashes := crlfBucketHashes(t, lfProject)
	crlfHashes := crlfBucketHashes(t, crlfProject)
	for _, key := range slices.Sorted(maps.Keys(crlfDeployedPaths)) {
		rel := strings.Replace(key, "{pkg}", hookPkg, 1)
		lf := globalStaleRead(t, filepath.Join(lfProject, filepath.FromSlash(rel)))
		got := globalStaleRead(t, filepath.Join(crlfProject, filepath.FromSlash(rel)))
		want := lf
		if crlfDeployedPaths[key] {
			want = strings.ReplaceAll(lf, "\n", "\r\n")
		}
		if got != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
		if strings.Contains(lf, "\r") {
			t.Errorf("%s from the LF package has a CR: %q", rel, lf)
		}
		if lfHashes[rel] != sha256Of(lf) {
			t.Errorf("LF lock records %s as %s, want the sha256 of its bytes %s", rel, lfHashes[rel], sha256Of(lf))
		}
		if crlfHashes[rel] != lfHashes[rel] {
			t.Errorf("CRLF lock records %s as %s, want the LF hash %s", rel, crlfHashes[rel], lfHashes[rel])
		}
	}
	if len(crlfHashes) != len(lfHashes) {
		t.Errorf("CRLF lock records %d hashes, the LF lock %d", len(crlfHashes), len(lfHashes))
	}
}

// crlfProjects runs setup in two new projects, with the package's lines
// ending in LF and in CRLF, installs each, and returns both projects.
func crlfProjects(t *testing.T, setup func(t *testing.T, project string, files map[string]string)) (lfProject, crlfProject string) {
	t.Helper()
	for _, eol := range []string{"\n", "\r\n"} {
		project := chdirTemp(t)
		setup(t, project, crlfPackageFiles(eol))
		if out, err := crlfInstall(t, false); err != nil {
			t.Fatalf("install (eol %q): %v\n%s", eol, err, out)
		}
		if eol == "\n" {
			lfProject = project
		} else {
			crlfProject = project
		}
	}
	return lfProject, crlfProject
}

// assertCRLFProjectStable checks that, in the current project, a frozen
// install passes on the CRLF files it deployed and an install writes the
// lock again with no byte changed.
func assertCRLFProjectStable(t *testing.T, project string) {
	t.Helper()
	lock := globalStaleRead(t, filepath.Join(project, "apm.lock.yaml"))
	if out, err := crlfInstall(t, true); err != nil {
		t.Fatalf("frozen install: %v\n%s", err, out)
	}
	if out, err := crlfInstall(t, false); err != nil {
		t.Fatalf("second install: %v\n%s", err, out)
	}
	if got := globalStaleRead(t, filepath.Join(project, "apm.lock.yaml")); got != lock {
		t.Errorf("second install changed the lock:\n%s\nwant:\n%s", got, lock)
	}
}

func TestInstall_CRLF_ProjectOwnContent(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	lfProject, crlfProject := crlfProjects(t, func(t *testing.T, project string, files map[string]string) {
		files["apm.yml"] = "name: own\nversion: 1.0.0\n"
		writeCRLFFiles(t, project, files)
	})
	assertCRLFDeploy(t, lfProject, crlfProject, "own")
	assertCRLFProjectStable(t, crlfProject)
}

func TestInstall_CRLF_LocalPathPackage(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	lfProject, crlfProject := crlfProjects(t, func(t *testing.T, project string, files map[string]string) {
		files["apm.yml"] = "name: crlfpkg\nversion: 1.0.0\n"
		writeCRLFFiles(t, filepath.Join(project, "pkg"), files)
		writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - ./pkg\n")
	})
	assertCRLFDeploy(t, lfProject, crlfProject, "pkg")
	assertCRLFProjectStable(t, crlfProject)
}

func TestInstall_CRLF_GitPackage(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	isolateGitConfig(t, "")
	lfProject, crlfProject := crlfProjects(t, func(t *testing.T, project string, files map[string]string) {
		files["apm.yml"] = "name: crlfpkg\nversion: 1.0.0\n"
		subdirGitRepo(t, filepath.Join(project, "acme", "pkg"), files)
		writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - git: acme/pkg\n")
	})
	if got := globalStaleRead(t, filepath.Join(crlfProject, "apm_modules", "acme", "pkg", ".apm", "agents", "rev.agent.md")); !strings.Contains(got, "\r\n") {
		t.Fatalf("the checkout of a package committed with CRLF has no CRLF: %q", got)
	}
	assertCRLFDeploy(t, lfProject, crlfProject, "pkg")
	assertCRLFProjectStable(t, crlfProject)
}

func crlfRegistryArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		data := []byte(files[name])
		if err := tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: 0o644}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstall_CRLF_RegistryPackage(t *testing.T) {
	enableRegistries(t)
	lfProject, crlfProject := crlfProjects(t, func(t *testing.T, project string, files map[string]string) {
		files["apm.yml"] = "name: sample\nversion: 1.0.0\n"
		archive := crlfRegistryArchive(t, files)
		srv := startE2EServer(t, archive, sha256Of(string(archive)))
		writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), fmt.Sprintf(
			"name: consumer\nversion: 0.1.0\nregistries:\n  local:\n    url: %s\n    insecure: true\n  default: local\ndependencies:\n  apm:\n    - id: acme/sample\n      version: 1.0.0\n", srv.URL))
	})
	assertCRLFDeploy(t, lfProject, crlfProject, "sample")
	assertCRLFProjectStable(t, crlfProject)
}

// crlfOwnProject installs the project's own LF content and returns the
// project.
func crlfOwnProject(t *testing.T) string {
	t.Helper()
	project := chdirTemp(t)
	files := crlfPackageFiles("\n")
	files["apm.yml"] = "name: own\nversion: 1.0.0\n"
	writeCRLFFiles(t, project, files)
	if out, err := crlfInstall(t, false); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	return project
}

const crlfAgentLF = "---\ndescription: reviews code\n---\nReview the code.\n"

func TestInstallFrozen_CRLF_DeployedFile(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := crlfOwnProject(t)
	agent := filepath.Join(project, ".claude", "agents", "rev.md")
	if got := globalStaleRead(t, agent); got != crlfAgentLF {
		t.Fatalf("deployed agent = %q, want %q", got, crlfAgentLF)
	}
	lockPath := filepath.Join(project, "apm.lock.yaml")
	lock := globalStaleRead(t, lockPath)

	writeGlobalStaleFile(t, agent, strings.ReplaceAll(crlfAgentLF, "\n", "\r\n"))
	if out, err := crlfInstall(t, true); err != nil {
		t.Fatalf("frozen install with the agent in CRLF: %v\n%s", err, out)
	}

	writeGlobalStaleFile(t, agent, strings.ReplaceAll(crlfAgentLF, "Review", "Rewrite"))
	_, err := crlfInstall(t, true)
	if err == nil || !strings.Contains(err.Error(), "content-integrity violation: .claude/agents/rev.md") {
		t.Fatalf("frozen install with other content: %v, want a content-integrity violation", err)
	}

	// A lock that recorded the raw hash of the CRLF bytes, as apm-go v0.4.0
	// did on Windows: frozen install accepts the normalized hash alone.
	writeGlobalStaleFile(t, agent, strings.ReplaceAll(crlfAgentLF, "\n", "\r\n"))
	writeGlobalStaleFile(t, lockPath, lock)
	crlfRecordRawAgent(t, lockPath)
	_, err = crlfInstall(t, true)
	if err == nil || !strings.Contains(err.Error(), "content-integrity violation: .claude/agents/rev.md") {
		t.Fatalf("frozen install on a lock with the raw CRLF hash: %v, want a content-integrity violation", err)
	}
}

// crlfOldLockPackage writes the local package dir of project, with the agent
// crlfAgentLF and one more.
func crlfOldLockPackage(t *testing.T, project, dir string) {
	t.Helper()
	writeCRLFFiles(t, filepath.Join(project, dir), map[string]string{
		"apm.yml":                   "name: " + dir + "\nversion: 1.0.0\n",
		".apm/agents/rev.agent.md":  crlfAgentLF,
		".apm/agents/keep.agent.md": "---\ndescription: kept\n---\nKept.\n",
	})
}

// crlfMakeOldLock turns the deployed claude agent and its lock record into
// what apm-go v0.4.0 wrote on Windows: the file in CRLF, recorded with the
// hash of those bytes.
func crlfMakeOldLock(t *testing.T, project string) {
	t.Helper()
	writeGlobalStaleFile(t, filepath.Join(project, ".claude", "agents", "rev.md"), strings.ReplaceAll(crlfAgentLF, "\n", "\r\n"))
	crlfRecordRawAgent(t, filepath.Join(project, "apm.lock.yaml"))
}

// crlfRecordRawAgent makes the lock at lockPath record .claude/agents/rev.md
// with the hash of crlfAgentLF in CRLF, where it recorded the LF hash.
func crlfRecordRawAgent(t *testing.T, lockPath string) {
	t.Helper()
	lock := globalStaleRead(t, lockPath)
	lf := ".claude/agents/rev.md: " + sha256Of(crlfAgentLF)
	if strings.Count(lock, lf) != 1 {
		t.Fatalf("lock does not record the agent's LF hash once:\n%s", lock)
	}
	raw := ".claude/agents/rev.md: " + sha256Of(strings.ReplaceAll(crlfAgentLF, "\n", "\r\n"))
	writeGlobalStaleFile(t, lockPath, strings.Replace(lock, lf, raw, 1))
}

// isolateGitConfig gives the git subprocesses of the test a global
// configuration with content alone and no system one.
func isolateGitConfig(t *testing.T, content string) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func assertCRLFOldFileRemoved(t *testing.T, project, out string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(project, ".claude", "agents", "rev.md")); !os.IsNotExist(err) {
		t.Errorf(".claude/agents/rev.md is still there (%v), want it removed\n%s", err, out)
	}
	if strings.Contains(out, "modified since deploy") {
		t.Errorf("cleanup kept a file as modified:\n%s", out)
	}
	lock, err := os.ReadFile(filepath.Join(project, "apm.lock.yaml"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if strings.Contains(string(lock), ".claude/agents/rev.md") {
		t.Error("the lock still records .claude/agents/rev.md")
	}
}

func crlfLocalSetup(t *testing.T, deps ...string) string {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	yml := "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n"
	for _, d := range deps {
		crlfOldLockPackage(t, project, d)
		yml += "    - ./" + d + "\n"
	}
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), yml)
	if out, err := crlfInstall(t, false); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	crlfMakeOldLock(t, project)
	return project
}

func TestInstall_OldWindowsLock_StaleFileRemoved(t *testing.T) {
	project := crlfLocalSetup(t, "pkg")
	if err := os.Remove(filepath.Join(project, "pkg", ".apm", "agents", "rev.agent.md")); err != nil {
		t.Fatal(err)
	}
	out, err := crlfInstall(t, false)
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	assertCRLFOldFileRemoved(t, project, out)
}

func TestInstall_OldWindowsLock_OrphanFileRemoved(t *testing.T) {
	project := crlfLocalSetup(t, "pkg")
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: caller\nversion: 0.1.0\ndependencies:\n  apm: []\n")
	out, err := crlfInstall(t, false)
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	assertCRLFOldFileRemoved(t, project, out)
}

func TestUninstall_OldWindowsLock_FileRemoved(t *testing.T) {
	project := crlfLocalSetup(t, "pkg")
	var err error
	out := captureUninstallStdout(t, func() { err = runUninstall([]string{"./pkg"}, uninstallOptions{}) })
	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	assertCRLFOldFileRemoved(t, project, out)
}

func TestUninstall_OldWindowsLock_HandsOverFile(t *testing.T) {
	project := crlfLocalSetup(t, "pkga", "pkgb")
	agent := filepath.Join(project, ".claude", "agents", "rev.md")
	var err error
	out := captureUninstallStdout(t, func() { err = runUninstall([]string{"./pkga"}, uninstallOptions{}) })
	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if strings.Contains(out, "modified since deploy") {
		t.Errorf("uninstall kept the file as modified:\n%s", out)
	}
	if got := globalStaleRead(t, agent); got != crlfAgentLF {
		t.Errorf(".claude/agents/rev.md = %q, want pkgb's %q", got, crlfAgentLF)
	}
	dep := crlfLock(t, project).FindByKey("_local/pkgb")
	if dep == nil || dep.DeployedHashes[".claude/agents/rev.md"] != sha256Of(crlfAgentLF) {
		t.Errorf("lock does not record .claude/agents/rev.md under _local/pkgb with the LF hash: %+v", dep)
	}
}

// A --global deploy links the agent to its CRLF source, and apm-go v0.4.0
// recorded the hash of those bytes: uninstall --global still knows the link
// as its own.
func TestUninstallGlobal_OldWindowsLock_LinkRemoved(t *testing.T) {
	crlfAgent := strings.ReplaceAll(crlfAgentLF, "\n", "\r\n")
	home, src := globalStaleScope(t, map[string]string{".apm/agents/rev.agent.md": crlfAgent})
	globalStaleInstall(t, "claude", src)
	link := filepath.Join(home, ".claude", "agents", "rev.md")
	if !globalStaleIsSymlink(t, link) || globalStaleRead(t, link) != crlfAgent {
		t.Fatal("precondition: .claude/agents/rev.md is not a symlink to the CRLF agent")
	}
	crlfRecordRawAgent(t, filepath.Join(home, ".apm", "apm.lock.yaml"))

	var err error
	out := captureUninstallStdout(t, func() { err = runUninstall([]string{src}, uninstallOptions{Global: true}) })
	if err != nil {
		t.Fatalf("uninstall --global: %v\n%s", err, out)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf(".claude/agents/rev.md is still there (%v), want it removed\n%s", err, out)
	}
	if strings.Contains(out, "keeping") {
		t.Errorf("uninstall --global kept a path:\n%s", out)
	}
}

// A lock committed on Linux is checked out with CRLF under git's
// core.autocrlf=true. It records the same state, so install leaves it as it
// is, as the Oracle writes the lock only when its content changed
// (install/phases/lockfile.py:554-562).
func TestInstall_LockCheckedOutWithCRLF_NotRewritten(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := crlfOwnProject(t)
	lockPath := filepath.Join(project, "apm.lock.yaml")
	lfOut, err := crlfInstall(t, false)
	if err != nil {
		t.Fatalf("install on the LF lock: %v\n%s", err, lfOut)
	}
	crlfLock := strings.ReplaceAll(globalStaleRead(t, lockPath), "\n", "\r\n")
	writeGlobalStaleFile(t, lockPath, crlfLock)

	out, err := crlfInstall(t, false)
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if got := globalStaleRead(t, lockPath); got != crlfLock {
		t.Errorf("install rewrote the CRLF lock:\n%q", got)
	}
	elapsed := regexp.MustCompile(`in [0-9.]+s\.`)
	if a, b := elapsed.ReplaceAllString(out, ""), elapsed.ReplaceAllString(lfOut, ""); a != b {
		t.Errorf("install on the CRLF lock printed:\n%s\nwant what it prints on the LF lock:\n%s", a, b)
	}
}

// A --global lock written by a symlink deploy restores under a copy deploy and
// the other way round (issue #118), also for sources whose bytes the copy has
// to keep: a bare CR, a CR before a CRLF, and a NUL.
func TestInstallGlobalFrozen_CRLFSourcesRestoreAcrossModes(t *testing.T) {
	// copied is what a copy deploy writes; a symlink shows the source.
	sources := map[string]struct{ content, copied string }{
		"bare cr":        {"---\ndescription: r\n---\r\nbody\rline\r\n", "---\ndescription: r\n---\nbody\rline\n"},
		"cr before crlf": {"---\ndescription: r\n---\r\nbody\r\r\nline\r\n", "---\ndescription: r\n---\r\nbody\r\r\nline\r\n"},
		"nul":            {"---\ndescription: r\n---\r\nbody\x00\r\nline\r\n", "---\ndescription: r\n---\r\nbody\x00\r\nline\r\n"},
	}
	for name, src := range sources {
		content := src.content
		for _, firstSymlink := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/first symlink %v", name, firstSymlink), func(t *testing.T) {
				home, srcDir := globalStaleScope(t, map[string]string{
					".apm/agents/rev.agent.md":            content,
					".apm/instructions/r.instructions.md": content,
					".apm/prompts/p.prompt.md":            content,
				})
				setSymlinkProbe(t, firstSymlink)
				if out, err := copyModeInstall(t, false, "claude,copilot", srcDir); err != nil {
					t.Fatalf("install --global: %v\n%s", err, out)
				}
				lockPath := filepath.Join(home, ".apm", "apm.lock.yaml")
				lockBefore := globalStaleRead(t, lockPath)
				for _, root := range []string{".claude", ".copilot"} {
					if err := os.RemoveAll(filepath.Join(home, root)); err != nil {
						t.Fatal(err)
					}
				}

				setSymlinkProbe(t, !firstSymlink)
				out, err := copyModeInstall(t, true, "claude,copilot")

				if err != nil {
					t.Fatalf("frozen install --global: %v\n%s", err, out)
				}
				want := src.copied
				if !firstSymlink {
					want = content
				}
				for _, rel := range []string{".claude/agents/rev.md", ".copilot/agents/rev.agent.md", ".copilot/instructions/r.instructions.md", ".copilot/prompts/p.prompt.md"} {
					if got := globalStaleRead(t, filepath.Join(home, filepath.FromSlash(rel))); got != want {
						t.Errorf("%s = %q, want %q", rel, got, want)
					}
				}
				if got := globalStaleRead(t, lockPath); got != lockBefore {
					t.Errorf("lock changed:\n%s\nwant:\n%s", got, lockBefore)
				}
			})
		}
	}
}

// oracleTextCases are bodies Oracle 0.33.0 reads with Path.read_text and
// writes with write_text_lf (measured), as src and the bytes it writes.
var oracleTextCases = map[string]struct{ src, oracle string }{
	"bare cr":        {"a\rb\r\n", "a\nb\n"},
	"cr before crlf": {"a\r\r\nb\r\n", "a\n\nb\n"},
	"nul":            {"a\x00\r\nb\r\n", "a\x00\nb\n"},
}

// oracleTextFiles is a package whose agent, instruction, prompt and command
// have body as their body.
func oracleTextFiles(body string) map[string]string {
	return map[string]string{
		".apm/agents/rev.agent.md":                "---\ndescription: r\n---\n" + body,
		".apm/instructions/style.instructions.md": "---\napplyTo: \"**/*.go\"\n---\n" + body,
		".apm/prompts/hello.prompt.md":            "---\ndescription: p\n---\n" + body,
		".apm/commands/ship.md":                   "---\ndescription: c\n---\n" + body,
	}
}

// A project deploy writes these files as the Oracle does: a bare CR, a CR
// before a CRLF and text with a NUL all become LF (issue #119 comment
// 6231670262). A claude rule and a codex agent are converted from that text,
// so they come out as the same package with the Oracle's LF body gives.
func TestInstall_OracleLineEndings_ProjectDeploy(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	for name, tc := range oracleTextCases {
		t.Run(name, func(t *testing.T) {
			install := func(body string) string {
				project := chdirTemp(t)
				files := oracleTextFiles(body)
				files["apm.yml"] = "name: own\nversion: 1.0.0\n"
				writeCRLFFiles(t, project, files)
				if out, err := crlfInstall(t, false); err != nil {
					t.Fatalf("install: %v\n%s", err, out)
				}
				return project
			}
			lfProject := install(tc.oracle)
			project := install(tc.src)

			copies := map[string]string{
				".claude/agents/rev.md":                      "---\ndescription: r\n---\n" + tc.oracle,
				".github/agents/rev.agent.md":                "---\ndescription: r\n---\n" + tc.oracle,
				".github/instructions/style.instructions.md": "---\napplyTo: \"**/*.go\"\n---\n" + tc.oracle,
				".github/prompts/hello.prompt.md":            "---\ndescription: p\n---\n" + tc.oracle,
				".claude/commands/ship.md":                   "---\ndescription: c\n---\n" + tc.oracle,
			}
			for rel, want := range copies {
				if got := globalStaleRead(t, filepath.Join(project, filepath.FromSlash(rel))); got != want {
					t.Errorf("%s = %q, want %q", rel, got, want)
				}
			}
			for _, rel := range []string{".claude/rules/style.md", ".codex/agents/rev.toml"} {
				want := globalStaleRead(t, filepath.Join(lfProject, filepath.FromSlash(rel)))
				if got := globalStaleRead(t, filepath.Join(project, filepath.FromSlash(rel))); got != want {
					t.Errorf("%s = %q, want %q", rel, got, want)
				}
			}
			hashes := crlfBucketHashes(t, project)
			for rel := range copies {
				if want := sha256Of(globalStaleRead(t, filepath.Join(project, filepath.FromSlash(rel)))); hashes[rel] != want {
					t.Errorf("lock records %s as %s, want the hash of its bytes %s", rel, hashes[rel], want)
				}
			}
		})
	}
}

// A claude rule and a codex agent are files in both --global modes, so both
// write the Oracle's LF text, as a project deploy does.
func TestInstallGlobal_OracleLineEndings_ConvertedFiles(t *testing.T) {
	for name, tc := range oracleTextCases {
		for _, symlink := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/symlink %v", name, symlink), func(t *testing.T) {
				install := func(body string) string {
					home, src := globalStaleScope(t, oracleTextFiles(body))
					setSymlinkProbe(t, symlink)
					if out, err := copyModeInstall(t, false, "claude,codex", src); err != nil {
						t.Fatalf("install --global: %v\n%s", err, out)
					}
					return home
				}
				lfHome := install(tc.oracle)
				home := install(tc.src)
				for _, rel := range []string{".claude/rules/style.md", ".codex/agents/rev.toml"} {
					want := globalStaleRead(t, filepath.Join(lfHome, filepath.FromSlash(rel)))
					if got := globalStaleRead(t, filepath.Join(home, filepath.FromSlash(rel))); got != want {
						t.Errorf("%s = %q, want %q", rel, got, want)
					}
				}
			})
		}
	}
}

// Content that is not UTF-8, which the Oracle cannot read, is copied as it is
// and recorded with the hash of its bytes (issue #119 comment 6231670262).
func TestInstall_NotUTF8Agent_CopiedAsItIs(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	const agent = "---\ndescription: r\n---\na\xff\r\nb\rc\r\n"
	project := chdirTemp(t)
	writeCRLFFiles(t, project, map[string]string{
		"apm.yml":                  "name: own\nversion: 1.0.0\n",
		".apm/agents/rev.agent.md": agent,
	})
	if out, err := crlfInstall(t, false); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	hashes := crlfBucketHashes(t, project)
	for _, rel := range []string{".claude/agents/rev.md", ".github/agents/rev.agent.md"} {
		if got := globalStaleRead(t, filepath.Join(project, filepath.FromSlash(rel))); got != agent {
			t.Errorf("%s = %q, want the source's bytes %q", rel, got, agent)
		}
		if hashes[rel] != sha256Of(agent) {
			t.Errorf("lock records %s as %s, want the hash of the raw bytes %s", rel, hashes[rel], sha256Of(agent))
		}
	}
}
