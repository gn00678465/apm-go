package gitops

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s\n%s", args, err, out)
	}
	return string(bytes.TrimSpace(out))
}

func initRepoWithTag(t *testing.T, dir, content, tag string) string {
	t.Helper()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "commit "+content)
	if tag != "" {
		gitCmd(t, dir, "tag", tag)
	}
	return gitCmd(t, dir, "rev-parse", "HEAD")
}

func TestCheckoutMatchesRef_TrueWhenHeadMatchesTag(t *testing.T) {
	dir := t.TempDir()
	initRepoWithTag(t, dir, "v1", "v1.0.0")

	if !checkoutMatchesRef(dir, "v1.0.0") {
		t.Error("expected checkout to match its own tag")
	}
}

func TestCheckoutMatchesRef_FalseWhenRefNotFoundLocally(t *testing.T) {
	dir := t.TempDir()
	initRepoWithTag(t, dir, "v1", "v1.0.0")

	if checkoutMatchesRef(dir, "v2.0.0") {
		t.Error("expected mismatch: v2.0.0 was never fetched into this checkout")
	}
}

func TestCheckoutMatchesRef_FalseWhenNotAGitRepo(t *testing.T) {
	dir := t.TempDir()
	if checkoutMatchesRef(dir, "v1.0.0") {
		t.Error("expected mismatch for a non-git directory")
	}
}

func TestCheckoutMatchesRef_FalseWhenRefEmpty(t *testing.T) {
	dir := t.TempDir()
	initRepoWithTag(t, dir, "v1", "v1.0.0")
	if checkoutMatchesRef(dir, "") {
		t.Error("expected mismatch for an empty ref")
	}
}

func TestCheckoutMatchesRef_FalseWhenWorktreeDirty(t *testing.T) {
	// A dirty worktree at the right commit still diverges from what a fresh
	// clone would produce (req-lk-007: skip must not change the observable
	// outcome).
	dir := t.TempDir()
	initRepoWithTag(t, dir, "v1", "v1.0.0")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("modified"), 0644); err != nil {
		t.Fatal(err)
	}

	if checkoutMatchesRef(dir, "v1.0.0") {
		t.Error("expected mismatch for a dirty worktree")
	}
}

func TestCheckoutMatchesRef_FalseWhenUntrackedFilePresent(t *testing.T) {
	dir := t.TempDir()
	initRepoWithTag(t, dir, "v1", "v1.0.0")
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}

	if checkoutMatchesRef(dir, "v1.0.0") {
		t.Error("expected mismatch for an untracked file")
	}
}

func TestCheckoutMatchesRef_FalseWhenIgnoredFilePresent(t *testing.T) {
	// A fresh clone never contains an ignored file (nothing generates them at
	// clone time), so one being present means this checkout diverges from
	// what a fresh clone would produce -- plain `git status --porcelain`
	// omits ignored files, so this needs the --ignored flag to catch it.
	// HEAD must still equal the tag (the .gitignore itself is part of the
	// tagged commit) so the mismatch is attributable to the ignored file
	// alone, not to HEAD having moved past the tag.
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("generated.txt\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "v1 with gitignore")
	gitCmd(t, dir, "tag", "v1.0.0")

	if err := os.WriteFile(filepath.Join(dir, "generated.txt"), []byte("build output"), 0644); err != nil {
		t.Fatal(err)
	}

	if checkoutMatchesRef(dir, "v1.0.0") {
		t.Error("expected mismatch for an ignored file present in the checkout")
	}
}

func TestCheckoutMatchesRef_TrueForAnnotatedTag(t *testing.T) {
	// An annotated tag's rev-parse resolves to the TAG OBJECT's own SHA, not
	// the commit it points at, unless peeled with ^{commit} -- without the
	// peel this would always report a false mismatch (safe but wasteful).
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "commit v1")
	gitCmd(t, dir, "tag", "-a", "v1.0.0", "-m", "release v1.0.0")

	if !checkoutMatchesRef(dir, "v1.0.0") {
		t.Error("expected checkout to match its own annotated tag")
	}
}

// TestLoadPackage_SkipsCloneWhenCheckoutMatchesRef proves the skip path
// never invokes clone: point RepoURL at a remote that doesn't exist, so a
// real clone attempt would fail, then pre-seed installDir to already match
// resolvedRef and confirm LoadPackage succeeds anyway (req-lk-007).
func TestLoadPackage_SkipsCloneWhenCheckoutMatchesRef(t *testing.T) {
	modulesDir := t.TempDir()
	r := &RealPackageLoader{ModulesDir: modulesDir}
	ref := &manifest.DependencyReference{RepoURL: "acme/pkg", Owner: "acme", Repo: "pkg", Scheme: "https", Host: "does-not-exist.invalid"}

	installDir, pathErr := r.installPath(ref)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatal(err)
	}
	initRepoWithTag(t, installDir, "v1", "v1.0.0")

	if _, err := r.LoadPackage(ref, "v1.0.0"); err != nil {
		t.Fatalf("LoadPackage should skip clone and succeed: %v", err)
	}
}

// chdirToFakeRemote sets CWD to a fresh tempdir containing a "remote"
// subdirectory (an initialized git repo), and returns a DependencyReference
// using the relative local-path clone form (git: ./path). A relative
// RepoURL is required here: installPath() naively string-joins RepoURL into
// the install path, which mishandles an absolute Windows path (drive
// letter) passed as a non-first filepath.Join argument -- a pre-existing,
// unrelated rough edge this test works around rather than exercises.
func chdirToFakeRemote(t *testing.T) (ref *manifest.DependencyReference, remoteDir string) {
	t.Helper()
	base := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origWD) })
	if err := os.Chdir(base); err != nil {
		t.Fatal(err)
	}
	remoteDir = filepath.Join(base, "remote")
	if err := os.MkdirAll(remoteDir, 0755); err != nil {
		t.Fatal(err)
	}
	return &manifest.DependencyReference{RepoURL: "remote"}, remoteDir
}

// TestLoadPackage_ReClonesWhenCheckoutStale proves a mismatched checkout is
// wiped and replaced, not silently kept (req-lk-007's core requirement).
func TestLoadPackage_ReClonesWhenCheckoutStale(t *testing.T) {
	ref, remoteDir := chdirToFakeRemote(t)
	initRepoWithTag(t, remoteDir, "correct", "v1.0.0")

	modulesDir := t.TempDir()
	r := &RealPackageLoader{ModulesDir: modulesDir}

	installDir, pathErr := r.installPath(ref)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Stale checkout: real git repo, but at a different tag/content.
	initRepoWithTag(t, installDir, "stale", "v0.9.0")

	if _, err := r.LoadPackage(ref, "v1.0.0"); err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(installDir, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "correct" {
		t.Errorf("expected stale checkout to be replaced with correct content, got %q", data)
	}
}

// TestLoadPackage_ClonesWhenDirMissing is the pre-existing baseline
// behavior (fresh install), unaffected by the req-lk-007 fix.
func TestLoadPackage_ClonesWhenDirMissing(t *testing.T) {
	ref, remoteDir := chdirToFakeRemote(t)
	initRepoWithTag(t, remoteDir, "hello", "v1.0.0")

	modulesDir := t.TempDir()
	r := &RealPackageLoader{ModulesDir: modulesDir}

	if _, err := r.LoadPackage(ref, "v1.0.0"); err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}

	installDir, pathErr := r.installPath(ref)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if _, err := os.Stat(filepath.Join(installDir, "file.txt")); err != nil {
		t.Errorf("expected file.txt to be cloned: %v", err)
	}
}

// TestLoadPackage_ClonesByRawCommitSHA is a from-scratch (apm_modules
// missing) clone where resolvedRef is a raw commit SHA rather than a
// branch/tag name -- e.g. the frozen-install path, which passes
// dep.ResolvedCommit. `git clone --depth 1 --branch <ref>` rejects a raw SHA
// with "Remote branch <sha> not found in upstream origin", so this proves
// the isCommitSHA fallback in cloneRepo actually gets exercised end-to-end
// through LoadPackage, not just at the cloneRepoAtCommit unit level.
func TestLoadPackage_ClonesByRawCommitSHA(t *testing.T) {
	ref, remoteDir := chdirToFakeRemote(t)
	sha := initRepoWithTag(t, remoteDir, "hello", "")

	modulesDir := t.TempDir()
	r := &RealPackageLoader{ModulesDir: modulesDir}

	if _, err := r.LoadPackage(ref, sha); err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}

	installDir, pathErr := r.installPath(ref)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if _, err := os.Stat(filepath.Join(installDir, "file.txt")); err != nil {
		t.Errorf("expected file.txt to be cloned: %v", err)
	}
	if head, err := ResolveCommit(installDir); err != nil || head != sha {
		t.Errorf("expected HEAD %s, got %s (err: %v)", sha, head, err)
	}
}

func TestIsCommitSHA(t *testing.T) {
	cases := map[string]bool{
		"27e04a371c29b4a714ddefca617faaef9cb8c38f": true,
		"27E04A371C29B4A714DDEFCA617FAAEF9CB8C38F": true,
		"v1.0.0": false,
		"main":   false,
		"":       false,
		"27e04a371c29b4a714ddefca617faaef9cb8c38":   false, // 39 chars
		"27e04a371c29b4a714ddefca617faaef9cb8c38ff": false, // 41 chars
		"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz":  false, // right length, non-hex
	}
	for ref, want := range cases {
		if got := isCommitSHA(ref); got != want {
			t.Errorf("isCommitSHA(%q) = %v, want %v", ref, got, want)
		}
	}
}

// TestLoadPackage_RefusesVirtualPathEscapingModulesDir guards installPath:
// RepoURL/VirtualPath are only charset-validated at manifest-parse time and
// do not reject ".." segments, so a crafted VirtualPath could otherwise
// resolve installDir outside ModulesDir (or onto an unrelated sibling
// directory still technically inside it) before LoadPackage's req-lk-007
// stale-checkout repair does an os.RemoveAll on it. A found-in-review
// regression: this was fixed in cmd/apm-go/update.go's purge path first, then
// found to also be missing here in the actual shared LoadPackage, which is
// reachable from every install (not just apm update).
func TestLoadPackage_RefusesVirtualPathEscapingModulesDir(t *testing.T) {
	modulesDir := t.TempDir()
	r := &RealPackageLoader{ModulesDir: modulesDir}

	// A sibling package that must survive: RepoURL "acme/a" + VirtualPath
	// ".." resolves to modulesDir/acme, wiping out anything there.
	siblingMarker := filepath.Join(modulesDir, "acme", "other-package", "marker.txt")
	if err := os.MkdirAll(filepath.Dir(siblingMarker), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(siblingMarker, []byte("unrelated package, must survive"), 0644); err != nil {
		t.Fatal(err)
	}

	ref := &manifest.DependencyReference{RepoURL: "acme/a", VirtualPath: "..", Owner: "acme", Repo: "a"}
	if _, err := r.LoadPackage(ref, "v1.0.0"); err == nil {
		t.Fatal("expected LoadPackage to refuse a VirtualPath containing \"..\"")
	}
	if _, statErr := os.Stat(siblingMarker); statErr != nil {
		t.Errorf("sibling package under ModulesDir must survive: %v", statErr)
	}
}

// TestMaterializeLocalCopy_CopiesTreeUnderModulesDir proves the happy path of
// the F1 fix's loader change: a local (non-git) directory dependency carrying
// LocalSourcePath is COPIED into apm_modules under its "_local/<name>" key,
// with its content readable there afterwards, and its apm.yml parsed.
func TestMaterializeLocalCopy_CopiesTreeUnderModulesDir(t *testing.T) {
	modulesDir := t.TempDir()
	r := &RealPackageLoader{ModulesDir: modulesDir}

	// A plain source directory (no .git) with an apm.yml and a nested file.
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "apm.yml"), []byte("name: dep\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(src, "skills", "hello")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "SKILL.md"), []byte("# hello"), 0644); err != nil {
		t.Fatal(err)
	}

	ref := &manifest.DependencyReference{RepoURL: "_local/hello-abc123", Source: "git", LocalSourcePath: src}
	m, err := r.LoadPackage(ref, "")
	if err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}
	if m == nil || m.Name != "dep" {
		t.Fatalf("expected parsed sub-manifest name=dep, got %+v", m)
	}
	copied := filepath.Join(modulesDir, "_local", "hello-abc123", "skills", "hello", "SKILL.md")
	if _, err := os.Stat(copied); err != nil {
		t.Errorf("expected source tree copied under ModulesDir: %v", err)
	}
}

// TestMaterializeLocalCopy_RefusesKeyEscapingModulesDir proves the copy
// destination is guarded by the SAME archive.ContainedKey check as every other
// dependency: a crafted "_local" key containing ".." must be refused, and an
// unrelated sibling under ModulesDir must survive untouched (no copy, no
// RemoveAll outside the intended key).
func TestMaterializeLocalCopy_RefusesKeyEscapingModulesDir(t *testing.T) {
	modulesDir := t.TempDir()
	r := &RealPackageLoader{ModulesDir: modulesDir}

	const canary = "must survive"
	siblingDir := filepath.Join(modulesDir, "victim")
	siblingMarker := filepath.Join(siblingDir, "marker.txt")
	if err := os.MkdirAll(siblingDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(siblingMarker, []byte(canary), 0644); err != nil {
		t.Fatal(err)
	}

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "apm.yml"), []byte("name: dep\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// RepoURL key contains ".." -> ContainedKey (via installPath) must refuse.
	ref := &manifest.DependencyReference{RepoURL: "_local/../victim", Source: "git", LocalSourcePath: src}
	_, err := r.LoadPackage(ref, "")
	if err == nil {
		t.Fatal("expected materializeLocalCopy to refuse a key containing \"..\"")
	}
	if !strings.Contains(err.Error(), "refusing") && !strings.Contains(err.Error(), "outside") {
		t.Errorf(`expected the refusal error to contain "refusing" or "outside", got %q`, err.Error())
	}

	// The would-be destination (the "_local" directory the crafted key
	// starts with) must never have been created at all -- nothing partial
	// leaked out before the ContainedKey guard fired.
	if _, statErr := os.Stat(filepath.Join(modulesDir, "_local")); !os.IsNotExist(statErr) {
		t.Errorf("expected no materialization destination to be created at all, stat err=%v", statErr)
	}

	entries, err := os.ReadDir(siblingDir)
	if err != nil {
		t.Fatalf("sibling directory under ModulesDir must survive a refused local copy: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "marker.txt" {
		t.Errorf("sibling directory must contain only its original marker.txt (no leaked/copied content), got %v", entries)
	}
	got, err := os.ReadFile(siblingMarker)
	if err != nil {
		t.Fatalf("sibling marker under ModulesDir must survive a refused local copy: %v", err)
	}
	if string(got) != canary {
		t.Errorf("sibling marker bytes changed unexpectedly: got %q want %q", got, canary)
	}
}

// TestCopyTreeNoSymlinks_SkipsSymlinks proves the security-relevant copy rule:
// a symlink under the source tree (which could point outside it) is never
// followed or copied into apm_modules; only the package's own in-tree regular
// files land there.
func TestCopyTreeNoSymlinks_SkipsSymlinks(t *testing.T) {
	src := t.TempDir()
	const realContent = "in-tree"
	if err := os.WriteFile(filepath.Join(src, "real.txt"), []byte(realContent), 0644); err != nil {
		t.Fatal(err)
	}
	// A secret the symlink would leak if followed.
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	const secretContent = "must not be copied"
	if err := os.WriteFile(secret, []byte(secretContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(src, "leak.txt")); err != nil {
		t.Skipf("symlink unsupported on this platform/CI: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "out")
	if err := copyTreeNoSymlinks(src, dst); err != nil {
		t.Fatalf("copyTreeNoSymlinks: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "real.txt"))
	if err != nil {
		t.Fatalf("regular file should be copied: %v", err)
	}
	if string(got) != realContent {
		t.Errorf("copied regular file bytes changed unexpectedly: got %q want %q", got, realContent)
	}
	if _, err := os.Lstat(filepath.Join(dst, "leak.txt")); err == nil {
		t.Error("symlink must NOT be copied (dereferenced or otherwise)")
	}
	// The secret the symlink pointed at must never have been read/altered by
	// the copy attempt.
	gotSecret, err := os.ReadFile(secret)
	if err != nil {
		t.Fatalf("secret outside src must survive: %v", err)
	}
	if string(gotSecret) != secretContent {
		t.Errorf("secret bytes changed unexpectedly: got %q want %q", gotSecret, secretContent)
	}
}
