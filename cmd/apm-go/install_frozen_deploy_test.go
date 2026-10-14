package main

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

// Issue #65: a frozen install deploys the locked files that are not on disk,
// never writes the lock or apm.yml, and fails when the deploy does not
// reproduce the lock.

const (
	coldSkillMD      = "---\nname: style\ndescription: fixture\n---\nBODY\n"
	coldPackageYML   = "name: style\nversion: 0.1.0\n"
	coldProjectYML   = "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - ./vendor/style\n"
	coldSkillPath    = ".claude/skills/style/SKILL.md"
	coldPackagePath  = ".claude/skills/style/apm.yml"
	coldSkillHash    = "sha256:4b0bd57babf059f4499fb1084669da94d683824baaa7bbf4be7b7a69bc5e6894"
	coldSkillMissing = "frozen install: content-integrity violation: " + coldSkillPath +
		" expected " + coldSkillHash + ", observed <missing>"
	coldCILine = "CI environment detected, defaulting to frozen install"
)

// coldProject installs ./vendor/style for claude in a new project directory
// and returns the directory. The lock is the one that install wrote.
func coldProject(t *testing.T) string {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	writeGlobalStaleFile(t, filepath.Join(project, "vendor", "style", "apm.yml"), coldPackageYML)
	writeGlobalStaleFile(t, filepath.Join(project, "vendor", "style", "SKILL.md"), coldSkillMD)
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), coldProjectYML)
	coldInstall(t, &gitops.RealPackageLoader{ModulesDir: "apm_modules"})
	return project
}

func coldInstall(t *testing.T, loader interface {
	LoadPackage(*manifest.DependencyReference, string) (*manifest.Manifest, error)
}) {
	t.Helper()
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: loader}
	captureUninstallStdout(t, func() {
		if err := runInstall(deps, false, true, "claude", "", nil, nil); err != nil {
			t.Fatalf("install: %v", err)
		}
	})
}

// coldStrip leaves what a clone of the project has: apm.yml, the lock and the
// package sources.
func coldStrip(t *testing.T, project string) {
	t.Helper()
	for _, name := range []string{".claude", "apm_modules", ".gitignore"} {
		if err := os.RemoveAll(filepath.Join(project, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func coldFrozen(t *testing.T, frozen bool, target string) (string, error) {
	t.Helper()
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	var runErr error
	out := captureUninstallStdout(t, func() {
		runErr = runInstall(deps, frozen, true, target, "", nil, nil)
	})
	return out, runErr
}

// coldUnchanged returns a check that apm.yml and the lock have the bytes they
// have now.
func coldUnchanged(t *testing.T, project string) func() {
	t.Helper()
	before := map[string]string{}
	for _, name := range []string{"apm.yml", "apm.lock.yaml"} {
		if data, err := os.ReadFile(filepath.Join(project, name)); err == nil {
			before[name] = string(data)
		}
	}
	return func() {
		t.Helper()
		for name, want := range before {
			if got := globalStaleRead(t, filepath.Join(project, name)); got != want {
				t.Errorf("%s changed:\n%s\nwant:\n%s", name, got, want)
			}
		}
	}
}

func assertColdAbsent(t *testing.T, project string, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := os.Lstat(filepath.Join(project, filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Errorf("%s exists (lstat error %v), want absent", name, err)
		}
	}
}

func assertColdDeployed(t *testing.T, project string) {
	t.Helper()
	if got := globalStaleRead(t, filepath.Join(project, filepath.FromSlash(coldSkillPath))); got != coldSkillMD {
		t.Errorf("%s = %q, want %q", coldSkillPath, got, coldSkillMD)
	}
	if got := globalStaleRead(t, filepath.Join(project, filepath.FromSlash(coldPackagePath))); got != coldPackageYML {
		t.Errorf("%s = %q, want %q", coldPackagePath, got, coldPackageYML)
	}
}

func TestInstallFrozen_Cold_DeploysLockedFiles(t *testing.T) {
	project := coldProject(t)
	coldStrip(t, project)
	unchanged := coldUnchanged(t, project)

	out, err := coldFrozen(t, true, "claude")

	if err != nil {
		t.Fatalf("frozen install: %v", err)
	}
	if strings.TrimSpace(out) != "+ "+frozenRootSuccess {
		t.Errorf("stdout = %q, want only the success line", out)
	}
	assertColdDeployed(t, project)
	unchanged()
	assertColdAbsent(t, project, ".gitignore")
}

func TestInstallFrozen_Cold_CIDefaultDeploysLockedFiles(t *testing.T) {
	project := coldProject(t)
	coldStrip(t, project)
	unchanged := coldUnchanged(t, project)
	t.Setenv("CI", "1")

	out, err := coldFrozen(t, false, "claude")

	if err != nil {
		t.Fatalf("install under CI: %v", err)
	}
	if !strings.Contains(out, coldCILine) {
		t.Errorf("missing CI line:\n%s", out)
	}
	if !strings.Contains(out, frozenRootSuccess) {
		t.Errorf("missing success line:\n%s", out)
	}
	assertColdDeployed(t, project)
	unchanged()
	assertColdAbsent(t, project, ".gitignore")
}

func TestInstallFrozen_OneFileMissing_RestoresIt(t *testing.T) {
	project := coldProject(t)
	if err := os.Remove(filepath.Join(project, filepath.FromSlash(coldPackagePath))); err != nil {
		t.Fatal(err)
	}
	unchanged := coldUnchanged(t, project)

	// The copy in apm_modules/ is there, so the loader must not run.
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: frozenRootLoader{t}}
	var err error
	out := captureUninstallStdout(t, func() {
		err = runInstall(deps, true, true, "claude", "", nil, nil)
	})

	if err != nil {
		t.Fatalf("frozen install: %v", err)
	}
	if !strings.Contains(out, frozenRootSuccess) {
		t.Errorf("missing success line:\n%s", out)
	}
	assertColdDeployed(t, project)
	unchanged()
}

func TestInstallFrozen_ProjectContentMissing_DeploysIt(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: proj\nversion: 0.0.0\n")
	writeGlobalStaleFile(t, filepath.Join(project, ".apm", "agents", "a.md"), frozenRootAgent)
	coldInstall(t, frozenRootLoader{t})
	coldStrip(t, project)
	unchanged := coldUnchanged(t, project)

	out, err := coldFrozen(t, true, "claude")

	if err != nil {
		t.Fatalf("frozen install: %v", err)
	}
	if !strings.Contains(out, frozenRootSuccess) {
		t.Errorf("missing success line:\n%s", out)
	}
	if got := globalStaleRead(t, filepath.Join(project, ".claude", "agents", "a.md")); got != frozenRootAgent {
		t.Errorf(".claude/agents/a.md = %q, want %q", got, frozenRootAgent)
	}
	unchanged()
}

func TestInstallFrozen_Cold_SourceChangedAfterLock_Fails(t *testing.T) {
	project := coldProject(t)
	coldStrip(t, project)
	writeGlobalStaleFile(t, filepath.Join(project, "vendor", "style", "SKILL.md"), coldSkillMD+"extra\n")
	before := coldTree(t, project)

	_, err := coldFrozen(t, true, "claude")

	const want = "frozen install: content-integrity violation: " + coldSkillPath +
		" expected " + coldSkillHash +
		", observed sha256:cea6a79859de08569f3fc85847ca7e8429e6df1c013134ed751890853119ef27"
	if err == nil || err.Error() != want {
		t.Fatalf("frozen install error = %v\nwant %s", err, want)
	}
	if code := exitCodeOf(err); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	assertColdTree(t, project, before)
}

func TestInstallFrozen_Cold_SourceHasUnlockedSkill_Fails(t *testing.T) {
	project := coldProject(t)
	coldStrip(t, project)
	writeGlobalStaleFile(t, filepath.Join(project, "vendor", "style", ".apm", "skills", "extra", "SKILL.md"),
		"---\nname: extra\ndescription: d\n---\nextra skill\n")
	before := coldTree(t, project)

	_, err := coldFrozen(t, true, "claude")

	const want = "frozen install: content-integrity violation: .claude/skills/extra/SKILL.md" +
		" expected <not in lockfile>" +
		", observed sha256:d36250725c2cf1e3003b19abe6328e3c7d40d553a92b4cf79398f1245c4592a1"
	if err == nil || err.Error() != want {
		t.Fatalf("frozen install error = %v\nwant %s", err, want)
	}
	assertColdTree(t, project, before)
}

func TestInstallFrozen_MissingAndChanged_ReportsChangedAndWritesNothing(t *testing.T) {
	project := coldProject(t)
	if err := os.Remove(filepath.Join(project, filepath.FromSlash(coldPackagePath))); err != nil {
		t.Fatal(err)
	}
	writeGlobalStaleFile(t, filepath.Join(project, filepath.FromSlash(coldSkillPath)), coldSkillMD+"extra\n")
	unchanged := coldUnchanged(t, project)

	_, err := coldFrozen(t, true, "claude")

	const want = "frozen install: content-integrity violation: " + coldSkillPath +
		" expected " + coldSkillHash +
		", observed sha256:cea6a79859de08569f3fc85847ca7e8429e6df1c013134ed751890853119ef27"
	if err == nil || err.Error() != want {
		t.Fatalf("frozen install error = %v\nwant %s", err, want)
	}
	assertColdAbsent(t, project, coldPackagePath)
	if got := globalStaleRead(t, filepath.Join(project, filepath.FromSlash(coldSkillPath))); got != coldSkillMD+"extra\n" {
		t.Errorf("%s = %q, want the changed content kept", coldSkillPath, got)
	}
	unchanged()
}

func TestInstallFrozen_Cold_NoTarget_ExitsTwoAndWritesNothing(t *testing.T) {
	project := coldProject(t)
	coldStrip(t, project)
	unchanged := coldUnchanged(t, project)

	_, err := coldFrozen(t, true, "")

	if err == nil || err.Error() != errNoDeployTarget().Error() {
		t.Fatalf("frozen install error = %v\nwant the no-target error", err)
	}
	if !strings.HasPrefix(err.Error(), "no deployment target detected\n") {
		t.Errorf("error = %q, want it to start with the no-target line", err)
	}
	if code := exitCodeOf(err); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	assertColdAbsent(t, project, ".claude", ".agents", "apm_modules")
	unchanged()
}

func TestInstallFrozen_Cold_TargetDoesNotGovernLockedFiles_Fails(t *testing.T) {
	project := coldProject(t)
	coldStrip(t, project)
	unchanged := coldUnchanged(t, project)

	_, err := coldFrozen(t, true, "codex")

	if err == nil || err.Error() != coldSkillMissing {
		t.Fatalf("frozen install error = %v\nwant %s", err, coldSkillMissing)
	}
	assertColdAbsent(t, project, ".claude", ".agents", ".codex", "apm_modules")
	unchanged()
}

func TestInstallFrozen_Cold_NoManifest_Fails(t *testing.T) {
	project := coldProject(t)
	coldStrip(t, project)
	if err := os.Remove(filepath.Join(project, "apm.yml")); err != nil {
		t.Fatal(err)
	}
	unchanged := coldUnchanged(t, project)

	_, err := coldFrozen(t, true, "claude")

	if err == nil || err.Error() != coldSkillMissing {
		t.Fatalf("frozen install error = %v\nwant %s", err, coldSkillMissing)
	}
	assertColdAbsent(t, project, ".claude", "apm_modules", "apm.yml")
	unchanged()
}

// coldRewriteLock replaces one string in the lock, which has to be there once.
func coldRewriteLock(t *testing.T, project, old, replacement string) {
	t.Helper()
	lockPath := filepath.Join(project, "apm.lock.yaml")
	lock := globalStaleRead(t, lockPath)
	if n := strings.Count(lock, old); n != 1 {
		t.Fatalf("lock has %q %d times, want 1:\n%s", old, n, lock)
	}
	writeGlobalStaleFile(t, lockPath, strings.Replace(lock, old, replacement, 1))
}

// coldBareHexLock drops the sha256: prefix of both deployed_file_hashes
// values, the other form req-lk-016 lets a lock record a digest in.
func coldBareHexLock(t *testing.T, project string) {
	t.Helper()
	coldRewriteLock(t, project, coldSkillPath+": sha256:", coldSkillPath+": ")
	coldRewriteLock(t, project, coldPackagePath+": sha256:", coldPackagePath+": ")
}

func TestInstallFrozen_Cold_BareHexLock_DeploysLockedFiles(t *testing.T) {
	project := coldProject(t)
	coldStrip(t, project)
	coldBareHexLock(t, project)
	unchanged := coldUnchanged(t, project)

	out, err := coldFrozen(t, true, "claude")

	if err != nil {
		t.Fatalf("frozen install: %v", err)
	}
	if !strings.Contains(out, frozenRootSuccess) {
		t.Errorf("missing success line:\n%s", out)
	}
	assertColdDeployed(t, project)
	unchanged()
}

func TestInstallFrozen_Cold_BareHexLock_SourceChangedAfterLock_Fails(t *testing.T) {
	project := coldProject(t)
	coldStrip(t, project)
	coldBareHexLock(t, project)
	writeGlobalStaleFile(t, filepath.Join(project, "vendor", "style", "SKILL.md"), coldSkillMD+"extra\n")
	before := coldTree(t, project)

	_, err := coldFrozen(t, true, "claude")

	const want = "frozen install: content-integrity violation: " + coldSkillPath +
		" expected 4b0bd57babf059f4499fb1084669da94d683824baaa7bbf4be7b7a69bc5e6894" +
		", observed sha256:cea6a79859de08569f3fc85847ca7e8429e6df1c013134ed751890853119ef27"
	if err == nil || err.Error() != want {
		t.Fatalf("frozen install error = %v\nwant %s", err, want)
	}
	if code := exitCodeOf(err); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	assertColdTree(t, project, before)
}

func TestInstallFrozen_Cold_NonSHA256LockHash_Fails(t *testing.T) {
	project := coldProject(t)
	coldStrip(t, project)
	coldRewriteLock(t, project, coldSkillPath+": "+coldSkillHash, coldSkillPath+": sha512:abcdef")
	before := coldTree(t, project)

	_, err := coldFrozen(t, true, "claude")

	const want = "frozen install: content-integrity violation: " + coldSkillPath +
		" expected sha512:abcdef, observed " + coldSkillHash
	if err == nil || err.Error() != want {
		t.Fatalf("frozen install error = %v\nwant %s", err, want)
	}
	if code := exitCodeOf(err); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	assertColdTree(t, project, before)
}

// coldGitLoader puts a copy of one git checkout at apm_modules/<repo URL>, as
// the real loader does by cloning.
type coldGitLoader struct {
	t        *testing.T
	checkout string
	calls    int
}

func (l *coldGitLoader) LoadPackage(ref *manifest.DependencyReference, _ string) (*manifest.Manifest, error) {
	l.calls++
	dest := filepath.Join("apm_modules", filepath.FromSlash(ref.RepoURL))
	if _, err := os.Stat(dest); err == nil {
		return &manifest.Manifest{Name: "foo", Version: "1.0.0"}, nil
	}
	if err := os.CopyFS(dest, os.DirFS(l.checkout)); err != nil {
		l.t.Fatal(err)
	}
	return &manifest.Manifest{Name: "foo", Version: "1.0.0"}, nil
}

func TestInstallFrozen_Cold_GitDependency_DeploysLockedFiles(t *testing.T) {
	const skill = "---\nname: foo\ndescription: d\n---\ngit skill\n"
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	checkout := t.TempDir()
	writeGlobalStaleFile(t, filepath.Join(checkout, "apm.yml"), "name: foo\nversion: 1.0.0\n")
	writeGlobalStaleFile(t, filepath.Join(checkout, ".apm", "skills", "foo", "SKILL.md"), skill)
	gitCmd(t, checkout, "init")
	gitCmd(t, checkout, "config", "user.name", "test")
	gitCmd(t, checkout, "config", "user.email", "test@test.com")
	gitCmd(t, checkout, "add", ".")
	gitCmd(t, checkout, "commit", "-m", "init")

	project := chdirTemp(t)
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"),
		"name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - acme/foo#main\n")
	loader := &coldGitLoader{t: t, checkout: checkout}
	coldInstall(t, loader)
	lock := globalStaleRead(t, filepath.Join(project, "apm.lock.yaml"))
	if !strings.Contains(lock, "tree_sha256:") || !strings.Contains(lock, ".claude/skills/foo/SKILL.md") {
		t.Fatalf("lock does not pin the git dependency's tree and skill:\n%s", lock)
	}
	coldStrip(t, project)
	unchanged := coldUnchanged(t, project)
	loader.calls = 0

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: loader}
	var err error
	out := captureUninstallStdout(t, func() {
		err = runInstall(deps, true, true, "claude", "", nil, nil)
	})

	if err != nil {
		t.Fatalf("frozen install: %v", err)
	}
	if !strings.Contains(out, frozenRootSuccess) {
		t.Errorf("missing success line:\n%s", out)
	}
	if loader.calls != 1 {
		t.Errorf("loader calls = %d, want 1", loader.calls)
	}
	if got := globalStaleRead(t, filepath.Join(project, ".claude", "skills", "foo", "SKILL.md")); got != skill {
		t.Errorf(".claude/skills/foo/SKILL.md = %q, want %q", got, skill)
	}
	unchanged()
}

func TestInstallGlobal_Frozen_RemovedSkillSymlink_ComesBack(t *testing.T) {
	home, src := globalStaleScope(t, frozenRootPackage)
	globalStaleInstall(t, "claude", src)
	link := filepath.Join(home, ".claude", "skills", "s1")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(home, ".apm", "apm.lock.yaml")
	lockBefore := globalStaleRead(t, lockPath)

	out, err := frozenRootGlobalInstall(t, true)

	if err != nil {
		t.Fatalf("frozen install: %v", err)
	}
	if !strings.Contains(out, frozenRootSuccess) {
		t.Errorf("missing success line:\n%s", out)
	}
	info, statErr := os.Lstat(link)
	if statErr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s: lstat = %v, %v; want a symlink", link, info, statErr)
	}
	assertFrozenRootIntact(t, home)
	if got := globalStaleRead(t, lockPath); got != lockBefore {
		t.Errorf("lock changed:\n%s\nwant:\n%s", got, lockBefore)
	}
}

// coldTree maps every path under root that is not in an apm_modules directory
// to its content: the bytes of a file, the target of a symlink, "" for a
// directory.
func coldTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "apm_modules" {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			tree[rel] = ""
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			tree[rel] = "-> " + target
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			tree[rel] = string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func assertColdTree(t *testing.T, root string, want map[string]string) {
	t.Helper()
	got := coldTree(t, root)
	for _, path := range slices.Sorted(maps.Keys(got)) {
		if before, ok := want[path]; !ok {
			t.Errorf("%s was created", path)
		} else if before != got[path] {
			t.Errorf("%s changed: %q, want %q", path, got[path], before)
		}
	}
	for _, path := range slices.Sorted(maps.Keys(want)) {
		if _, ok := got[path]; !ok {
			t.Errorf("%s was removed", path)
		}
	}
}

// coldRepoLoader fills apm_modules/<key> from in-memory repositories the way
// the real loader does: the contents of the virtual path for a subdirectory
// reference, the whole repository for any other.
type coldRepoLoader struct {
	t     *testing.T
	repos map[string]map[string]string
	refs  []manifest.DependencyReference
}

func (l *coldRepoLoader) LoadPackage(ref *manifest.DependencyReference, _ string) (*manifest.Manifest, error) {
	l.refs = append(l.refs, *ref)
	dest := filepath.Join("apm_modules", filepath.FromSlash(ref.RepoURL), filepath.FromSlash(ref.VirtualPath))
	if err := os.RemoveAll(dest); err != nil {
		l.t.Fatal(err)
	}
	prefix := ""
	if ref.VirtualPath != "" && ref.VirtualType == "subdirectory" {
		prefix = ref.VirtualPath + "/"
	}
	for rel, content := range l.repos[ref.RepoURL] {
		if strings.HasPrefix(rel, prefix) {
			writeGlobalStaleFile(l.t, filepath.Join(dest, filepath.FromSlash(strings.TrimPrefix(rel, prefix))), content)
		}
	}
	data, err := os.ReadFile(filepath.Join(dest, "apm.yml"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, err
	}
	m, _, err := manifest.ParseManifest(node)
	return m, err
}

func (l *coldRepoLoader) virtualTypes(repoURL string) []string {
	var types []string
	for _, ref := range l.refs {
		if ref.RepoURL == repoURL {
			types = append(types, ref.VirtualType)
		}
	}
	return types
}

const (
	coldAlphaSkill = "---\nname: alpha\ndescription: d\n---\nalpha\n"
	coldBetaSkill  = "---\nname: beta\ndescription: d\n---\nbeta\n"
	coldOwnSkill   = "---\nname: own\ndescription: d\n---\nown\n"
)

var coldMonoRepo = map[string]string{
	"skills/alpha/SKILL.md": coldAlphaSkill,
	"skills/beta/SKILL.md":  coldBetaSkill,
}

// coldFrozenWith runs a frozen install for claude with loader.
func coldFrozenWith(t *testing.T, loader *coldRepoLoader) (string, error) {
	t.Helper()
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: loader}
	var err error
	out := captureUninstallStdout(t, func() {
		err = runInstall(deps, true, true, "claude", "", nil, nil)
	})
	return out, err
}

func TestInstallFrozen_Cold_VirtualPathDependency_DeploysOnlyThatDirectory(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"),
		"name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - acme/mono/skills/alpha\n")
	loader := &coldRepoLoader{t: t, repos: map[string]map[string]string{"acme/mono": coldMonoRepo}}
	coldInstall(t, loader)
	want := coldTree(t, project)
	if want[filepath.FromSlash(".claude/skills/alpha/SKILL.md")] != coldAlphaSkill {
		t.Fatalf("install did not deploy the alpha skill: %v", slices.Sorted(maps.Keys(want)))
	}
	if _, ok := want[filepath.FromSlash(".claude/skills/beta")]; ok {
		t.Fatalf("install deployed the beta skill: %v", slices.Sorted(maps.Keys(want)))
	}
	coldStrip(t, project)
	delete(want, ".gitignore")
	loader.refs = nil

	out, err := coldFrozenWith(t, loader)

	if err != nil {
		t.Fatalf("frozen install: %v", err)
	}
	if !strings.Contains(out, frozenRootSuccess) {
		t.Errorf("missing success line:\n%s", out)
	}
	assertColdTree(t, project, want)
	if got := loader.virtualTypes("acme/mono"); !slices.Equal(got, []string{"subdirectory"}) {
		t.Errorf("loader got virtual types %q for acme/mono, want one subdirectory reference", got)
	}
}

func TestInstallFrozen_Cold_TransitiveVirtualPathDependency_DeploysLockedFiles(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"),
		"name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - acme/pkg\n")
	loader := &coldRepoLoader{t: t, repos: map[string]map[string]string{
		"acme/mono": coldMonoRepo,
		"acme/pkg": {
			"apm.yml":                  "name: pkg\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/mono/skills/alpha\n",
			".apm/skills/own/SKILL.md": coldOwnSkill,
		},
	}}
	coldInstall(t, loader)
	lock := globalStaleRead(t, filepath.Join(project, "apm.lock.yaml"))
	if !strings.Contains(lock, "virtual_path: skills/alpha") || !strings.Contains(lock, "depth: 2") ||
		strings.Contains(lock, "resolved_commit") {
		t.Fatalf("lock is not a depth-2 virtual path entry with no resolved_commit:\n%s", lock)
	}
	want := coldTree(t, project)
	if want[filepath.FromSlash(".claude/skills/alpha/SKILL.md")] != coldAlphaSkill ||
		want[filepath.FromSlash(".claude/skills/own/SKILL.md")] != coldOwnSkill {
		t.Fatalf("install did not deploy both skills: %v", slices.Sorted(maps.Keys(want)))
	}
	coldStrip(t, project)
	delete(want, ".gitignore")
	loader.refs = nil

	out, err := coldFrozenWith(t, loader)

	if err != nil {
		t.Fatalf("frozen install: %v", err)
	}
	if !strings.Contains(out, frozenRootSuccess) {
		t.Errorf("missing success line:\n%s", out)
	}
	assertColdTree(t, project, want)
	if got := loader.virtualTypes("acme/mono"); !slices.Equal(got, []string{"subdirectory"}) {
		t.Errorf("loader got virtual types %q for acme/mono, want one subdirectory reference", got)
	}
}

func TestInstallGlobal_Frozen_ChangedSourceAndRemovedSymlink_FailsAndWritesNothing(t *testing.T) {
	home, src := globalStaleScope(t, frozenRootPackage)
	globalStaleInstall(t, "claude", src)
	module := frozenRootLocalModule(t, filepath.Join(home, ".apm"))
	writeGlobalStaleFile(t, filepath.Join(module, ".apm", "skills", "s1", "SKILL.md"), frozenRootTampered)
	if err := os.Remove(filepath.Join(home, ".claude", "skills", "s1")); err != nil {
		t.Fatal(err)
	}
	before := coldTree(t, home)

	_, err := frozenRootGlobalInstall(t, true)

	const want = "frozen install: content-integrity violation: .claude/skills/s1/SKILL.md" +
		" expected sha256:be6d6557ab9085f5bc1eac70e27943eb1ed44aa4b0eaa14f97b388dd1fdd0e19," +
		" observed sha256:b042cbaf6f9a8618d9637956a2ef782316a118fc383a3ce5f0690d6919117f9d"
	if err == nil || err.Error() != want {
		t.Fatalf("frozen install error = %v\nwant %s", err, want)
	}
	assertColdTree(t, home, before)
}

func TestInstallGlobal_Frozen_SkillWithInnerSymlink_RestoresDirectorySymlink(t *testing.T) {
	const (
		skillMD = "---\nname: style\ndescription: d\n---\nskill\n"
		notes   = "safe local notes\n"
	)
	home, _ := globalStaleScope(t, nil)
	source := filepath.Join(home, ".apm", ".apm", "skills", "style")
	writeGlobalStaleFile(t, filepath.Join(source, "SKILL.md"), skillMD)
	writeGlobalStaleFile(t, filepath.Join(source, "notes.md"), notes)
	if err := os.Symlink("notes.md", filepath.Join(source, "linked.md")); err != nil {
		t.Fatal(err)
	}
	globalStaleInstall(t, "claude")
	lockPath := filepath.Join(home, ".apm", "apm.lock.yaml")
	lockBefore := globalStaleRead(t, lockPath)
	if !strings.Contains(lockBefore, ".claude/skills/style/linked.md: sha256:") {
		t.Fatalf("lock does not record the inner symlink:\n%s", lockBefore)
	}
	deployed := filepath.Join(home, ".claude", "skills", "style")
	if err := os.Remove(deployed); err != nil {
		t.Fatal(err)
	}
	sourceBefore := coldTree(t, source)

	out, err := frozenRootGlobalInstall(t, true)

	if err != nil {
		t.Fatalf("frozen install: %v", err)
	}
	if !strings.Contains(out, frozenRootSuccess) {
		t.Errorf("missing success line:\n%s", out)
	}
	if info, statErr := os.Lstat(deployed); statErr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s: lstat = %v, %v; want a symlink", deployed, info, statErr)
	}
	if got := globalStaleRead(t, filepath.Join(deployed, "linked.md")); got != notes {
		t.Errorf("linked.md through the deployed symlink = %q, want %q", got, notes)
	}
	if got := globalStaleRead(t, lockPath); got != lockBefore {
		t.Errorf("lock changed:\n%s\nwant:\n%s", got, lockBefore)
	}
	// Removing the trial deploy directory must remove its links, not what
	// they point to.
	wantSource := map[string]string{".": "", "SKILL.md": skillMD, "notes.md": notes, "linked.md": "-> notes.md"}
	if !maps.Equal(sourceBefore, wantSource) {
		t.Fatalf("source before the frozen install = %v, want %v", sourceBefore, wantSource)
	}
	assertColdTree(t, source, wantSource)
}
