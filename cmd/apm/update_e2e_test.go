package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
)

// TestRunUpdate_RealGitSemver_ResolvesToNewTag is the CLI-level end-to-end
// req-rs-011 regression: it exercises the real cmd/apm -> resolver ->
// RealTagLister -> RealPackageLoader path (no mocks) for a git-semver
// dependency, using a local git-path remote (git: ./remote) to stay
// offline-friendly (same rationale as TestRunInstall_StaleCheckoutIsRepaired).
// Mocked tests alone previously missed a real integration bug in this area
// (the req-lk-007 raw-SHA clone regression), so this proves the update
// command's wiring -- buildLockfile/deployAndFinalize extraction included --
// actually works against real git, not just mock loaders/tag listers.
func TestRunUpdate_RealGitSemver_ResolvesToNewTag(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	git := func(repoDir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s\n%s", args, err, out)
		}
	}
	headOf := func(repoDir string) string {
		t.Helper()
		cmd := exec.Command("git", "rev-parse", "HEAD")
		cmd.Dir = repoDir
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(bytes.TrimSpace(out))
	}

	remoteDir := filepath.Join(dir, "remote")
	if err := os.MkdirAll(remoteDir, 0755); err != nil {
		t.Fatal(err)
	}
	git(remoteDir, "init")
	git(remoteDir, "config", "user.name", "test")
	git(remoteDir, "config", "user.email", "test@test.com")
	os.WriteFile(filepath.Join(remoteDir, "apm.yml"), []byte("name: dep\nversion: \"1.0.0\"\n"), 0644)
	git(remoteDir, "add", ".")
	git(remoteDir, "commit", "-m", "v1")
	git(remoteDir, "tag", "v1.0.0")

	// target: claude in the manifest (rather than only --target on the
	// initial install) is required so runUpdate's own C2 zero-target gate
	// (07-11-update-local-deps) -- which has no --target flag of its own and
	// reads m.Target -- can resolve a target too; before that fix runUpdate
	// had no target gate at all, so this real-git resolution assertion
	// didn't need m.Target set for update to reach the lockfile write.
	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ntarget:\n  - claude\ndependencies:\n  apm:\n    - git: ./remote\n      ref: \"^1.0.0\"\n"), 0644)

	deps := &installDeps{
		tags:   &gitops.RealTagLister{},
		loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"},
	}
	if err := runInstall(deps, false, true, "", nil, nil); err != nil {
		t.Fatalf("initial runInstall: %v", err)
	}

	installDir := filepath.Join("apm_modules", "remote")
	v1Head := headOf(installDir)

	// A newer release appears on the remote, still within ^1.0.0.
	os.WriteFile(filepath.Join(remoteDir, "apm.yml"), []byte("name: dep\nversion: \"1.5.0\"\n"), 0644)
	git(remoteDir, "add", ".")
	git(remoteDir, "commit", "-m", "v1.5")
	git(remoteDir, "tag", "v1.5.0")
	v15Head := headOf(remoteDir)

	if err := runUpdate(deps, false, false, ""); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}

	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(lockData, []byte("v1.5.0")) {
		t.Errorf("expected lockfile to be updated to v1.5.0, got:\n%s", lockData)
	}

	got := headOf(installDir)
	if got != v15Head {
		t.Errorf("expected apm_modules/remote to be re-cloned at v1.5.0 (%s), got %s (was %s before update)", v15Head, got, v1Head)
	}
}

// TestRunUpdate_RealGitSemver_UnchangedTagStillRecloned is the real-git
// counterpart to TestRunUpdate_GitSemver_InstallPathClearedEvenWhenTagUnchanged:
// with only one tag on the remote, the update re-resolves to that SAME tag,
// yet req-lk-010 still requires apm_modules to be cleared and re-downloaded
// rather than silently trusting the existing checkout.
func TestRunUpdate_RealGitSemver_UnchangedTagStillRecloned(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	git := func(repoDir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s\n%s", args, err, out)
		}
	}

	remoteDir := filepath.Join(dir, "remote")
	if err := os.MkdirAll(remoteDir, 0755); err != nil {
		t.Fatal(err)
	}
	git(remoteDir, "init")
	git(remoteDir, "config", "user.name", "test")
	git(remoteDir, "config", "user.email", "test@test.com")
	os.WriteFile(filepath.Join(remoteDir, "apm.yml"), []byte("name: dep\nversion: \"1.0.0\"\n"), 0644)
	git(remoteDir, "add", ".")
	git(remoteDir, "commit", "-m", "v1")
	git(remoteDir, "tag", "v1.0.0")

	// target: claude in the manifest (see the identical comment in
	// TestRunUpdate_RealGitSemver_ResolvesToNewTag above) is required so
	// runUpdate's own C2 zero-target gate (07-11-update-local-deps) can
	// resolve a target too.
	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ntarget:\n  - claude\ndependencies:\n  apm:\n    - git: ./remote\n      ref: \"^1.0.0\"\n"), 0644)

	deps := &installDeps{
		tags:   &gitops.RealTagLister{},
		loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"},
	}
	if err := runInstall(deps, false, true, "", nil, nil); err != nil {
		t.Fatalf("initial runInstall: %v", err)
	}

	installDir := filepath.Join("apm_modules", "remote")
	markerPath := filepath.Join(installDir, "marker.txt")
	if err := os.WriteFile(markerPath, []byte("simulated tampering, untracked by git"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runUpdate(deps, false, false, ""); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}

	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Errorf("expected apm_modules/remote to be cleared and re-cloned even though the tag (v1.0.0) didn't change (req-lk-010); marker still present (stat err: %v)", err)
	}
}
