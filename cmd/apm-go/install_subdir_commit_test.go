package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/apm-go/apm/internal/semver"
	"github.com/apm-go/apm/internal/yamlcore"
)

// Issue #65: the lock pins a subdirectory package to the commit of the
// repository it was taken from. Its module directory has no .git, so git run
// there answers for the project's own repository.

const (
	subdirAlphaSkill = "---\nname: alpha\ndescription: d\n---\nalpha\n"
	subdirOwnSkill   = "---\nname: own\ndescription: d\n---\nown\n"
)

// subdirGitRepo makes dir a git repository with files in one commit and
// returns the commit.
func subdirGitRepo(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	for rel, content := range files {
		writeGlobalStaleFile(t, filepath.Join(dir, filepath.FromSlash(rel)), content)
	}
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	for rel := range files {
		gitCmd(t, dir, "add", "--", rel)
	}
	gitCmd(t, dir, "commit", "-m", "init")
	return gitCmd(t, dir, "rev-parse", "HEAD")
}

func subdirMonoRepo(t *testing.T, dir string) string {
	t.Helper()
	return subdirGitRepo(t, dir, map[string]string{
		"skills/alpha/SKILL.md": subdirAlphaSkill,
		"skills/beta/SKILL.md":  "---\nname: beta\ndescription: d\n---\nbeta\n",
	})
}

// subdirPathLoader is the real loader given a reference with no owner and
// repo, which it clones from "<owner>/<repo>" as a path under the working
// directory instead of from the network. Everything else, the subdirectory
// extraction and the commit record included, is the real loader's.
type subdirPathLoader struct{ *gitops.RealPackageLoader }

func (l subdirPathLoader) LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error) {
	local := *ref
	local.Owner, local.Repo = "", ""
	return l.RealPackageLoader.LoadPackage(&local, resolvedRef)
}

// subdirPathTagLister lists the tags of a git dependency from the same path
// subdirPathLoader clones it from.
type subdirPathTagLister struct{}

func (subdirPathTagLister) ListTags(repoURL string) ([]semver.TagInfo, error) {
	return subdirPathTagLister{}.ListTagsForRef(&manifest.DependencyReference{RepoURL: repoURL})
}

func (subdirPathTagLister) ListTagsForRef(ref *manifest.DependencyReference) ([]semver.TagInfo, error) {
	local := *ref
	local.Owner, local.Repo = "", ""
	return (&gitops.RealTagLister{}).ListTagsForRef(&local)
}

// useLocalGitRemotes makes the install and update commands load packages
// with subdirPathLoader for the rest of the test.
func useLocalGitRemotes(t *testing.T) {
	t.Helper()
	prev := newGitPackageLoader
	newGitPackageLoader = func(modulesDir string) resolver.PackageLoader {
		return subdirPathLoader{&gitops.RealPackageLoader{ModulesDir: modulesDir}}
	}
	t.Cleanup(func() { newGitPackageLoader = prev })
}

func subdirInstall(t *testing.T, loader interface {
	LoadPackage(*manifest.DependencyReference, string) (*manifest.Manifest, error)
}, frozen bool) error {
	t.Helper()
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: loader}
	var err error
	captureUninstallStdout(t, func() {
		err = runInstall(deps, frozen, true, "claude", "", nil, nil)
	})
	return err
}

func subdirRealLoader() *gitops.RealPackageLoader {
	return &gitops.RealPackageLoader{ModulesDir: "apm_modules"}
}

func subdirLockEntry(t *testing.T, project, key string) *lockfile.LockedDep {
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
	dep := lock.FindByKey(key)
	if dep == nil {
		t.Fatalf("lock has no entry %s:\n%s", key, data)
	}
	return dep
}

func assertSubdirPinned(t *testing.T, project, key, sourceCommit, projectCommit string) {
	t.Helper()
	dep := subdirLockEntry(t, project, key)
	if dep.ResolvedCommit != sourceCommit {
		t.Errorf("%s resolved_commit = %q, want the source repository's commit %q (the project's is %q)",
			key, dep.ResolvedCommit, sourceCommit, projectCommit)
	}
	if dep.TreeSHA256 != "" {
		t.Errorf("%s tree_sha256 = %q, want none", key, dep.TreeSHA256)
	}
}

// subdirClone starts a second project, itself a git repository, with the first
// project's apm.yml and lock and copies of the source repositories.
func subdirClone(t *testing.T, project string, sources ...string) string {
	t.Helper()
	clone := chdirTemp(t)
	for _, name := range sources {
		if err := os.CopyFS(filepath.Join(clone, filepath.FromSlash(name)), os.DirFS(filepath.Join(project, filepath.FromSlash(name)))); err != nil {
			t.Fatal(err)
		}
	}
	subdirGitRepo(t, clone, map[string]string{
		"apm.yml":       globalStaleRead(t, filepath.Join(project, "apm.yml")),
		"apm.lock.yaml": globalStaleRead(t, filepath.Join(project, "apm.lock.yaml")),
	})
	return clone
}

func TestInstall_SubdirectoryPackage_LockPinsSourceCommit(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	sourceCommit := subdirMonoRepo(t, filepath.Join(project, "acme", "mono"))
	projectCommit := subdirGitRepo(t, project, map[string]string{
		"apm.yml": "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - git: acme/mono\n      path: skills/alpha\n",
	})
	if sourceCommit == projectCommit {
		t.Fatal("the source and the project have the same commit")
	}

	if err := subdirInstall(t, subdirPathLoader{subdirRealLoader()}, false); err != nil {
		t.Fatalf("install: %v", err)
	}

	assertSubdirPinned(t, project, "acme/mono/skills/alpha", sourceCommit, projectCommit)
	if got := globalStaleRead(t, filepath.Join(project, ".claude", "skills", "alpha", "SKILL.md")); got != subdirAlphaSkill {
		t.Errorf("deployed alpha skill = %q, want %q", got, subdirAlphaSkill)
	}
	lock := globalStaleRead(t, filepath.Join(project, "apm.lock.yaml"))

	if err := subdirInstall(t, subdirPathLoader{subdirRealLoader()}, false); err != nil {
		t.Fatalf("second install: %v", err)
	}
	if got := globalStaleRead(t, filepath.Join(project, "apm.lock.yaml")); got != lock {
		t.Errorf("second install changed the lock:\n%s\nwant:\n%s", got, lock)
	}
}

func TestInstall_SubdirectoryPackage_FrozenInstallInAnotherRepository(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	sourceCommit := subdirMonoRepo(t, filepath.Join(project, "acme", "mono"))
	projectCommit := subdirGitRepo(t, project, map[string]string{
		"apm.yml": "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - acme/mono/skills/alpha\n",
	})
	if err := subdirInstall(t, subdirPathLoader{subdirRealLoader()}, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	assertSubdirPinned(t, project, "acme/mono/skills/alpha", sourceCommit, projectCommit)
	lock := globalStaleRead(t, filepath.Join(project, "apm.lock.yaml"))

	clone := subdirClone(t, project, "acme/mono")
	err := subdirInstall(t, subdirPathLoader{subdirRealLoader()}, true)

	if err != nil {
		t.Fatalf("frozen install in the clone: %v", err)
	}
	if got := globalStaleRead(t, filepath.Join(clone, ".claude", "skills", "alpha", "SKILL.md")); got != subdirAlphaSkill {
		t.Errorf("deployed alpha skill = %q, want %q", got, subdirAlphaSkill)
	}
	assertColdAbsent(t, clone, ".claude/skills/beta")
	if got := globalStaleRead(t, filepath.Join(clone, "apm.lock.yaml")); got != lock {
		t.Errorf("frozen install changed the lock:\n%s\nwant:\n%s", got, lock)
	}
}

func TestInstall_TransitiveSubdirectoryPackage_LockPinsSourceCommit(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	sourceCommit := subdirMonoRepo(t, filepath.Join(project, "acme", "mono"))
	pkgCommit := subdirGitRepo(t, filepath.Join(project, "acme", "pkg"), map[string]string{
		"apm.yml":                  "name: pkg\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/mono/skills/alpha\n",
		".apm/skills/own/SKILL.md": subdirOwnSkill,
	})
	projectCommit := subdirGitRepo(t, project, map[string]string{
		"apm.yml": "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - acme/pkg\n",
	})

	if err := subdirInstall(t, subdirPathLoader{subdirRealLoader()}, false); err != nil {
		t.Fatalf("install: %v", err)
	}

	assertSubdirPinned(t, project, "acme/mono/skills/alpha", sourceCommit, projectCommit)
	if dep := subdirLockEntry(t, project, "acme/mono/skills/alpha"); dep.Depth != 2 {
		t.Errorf("acme/mono/skills/alpha depth = %d, want 2", dep.Depth)
	}
	whole := subdirLockEntry(t, project, "acme/pkg")
	if whole.ResolvedCommit != pkgCommit || !strings.HasPrefix(whole.TreeSHA256, "sha256:") {
		t.Errorf("acme/pkg resolved_commit = %q, tree_sha256 = %q; want %q and a sha256 tree hash",
			whole.ResolvedCommit, whole.TreeSHA256, pkgCommit)
	}
	lock := globalStaleRead(t, filepath.Join(project, "apm.lock.yaml"))

	clone := subdirClone(t, project, "acme/mono", "acme/pkg")
	err := subdirInstall(t, subdirPathLoader{subdirRealLoader()}, true)

	if err != nil {
		t.Fatalf("frozen install in the clone: %v", err)
	}
	if got := globalStaleRead(t, filepath.Join(clone, ".claude", "skills", "alpha", "SKILL.md")); got != subdirAlphaSkill {
		t.Errorf("deployed alpha skill = %q, want %q", got, subdirAlphaSkill)
	}
	if got := globalStaleRead(t, filepath.Join(clone, ".claude", "skills", "own", "SKILL.md")); got != subdirOwnSkill {
		t.Errorf("deployed own skill = %q, want %q", got, subdirOwnSkill)
	}
	if got := globalStaleRead(t, filepath.Join(clone, "apm.lock.yaml")); got != lock {
		t.Errorf("frozen install changed the lock:\n%s\nwant:\n%s", got, lock)
	}
}
