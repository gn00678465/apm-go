package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRunInstall_StaleCheckoutIsRepaired is the CLI-level req-lk-007
// regression: it exercises the real cmd/apm-go -> resolver -> RealPackageLoader
// path, cloning acme/remote from a local repository (subdirPathLoader), so it
// never contacts a network host.
func TestRunInstall_StaleCheckoutIsRepaired(t *testing.T) {
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

	remoteDir := filepath.Join(dir, "acme", "remote")
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
	pinnedHead := headOf(remoteDir)

	// A second, unrelated commit exists on the remote but is NOT what's
	// pinned -- the manifest ref stays fixed at the v1.0.0 tag throughout,
	// so "correct" has one unambiguous meaning for the whole test.
	os.WriteFile(filepath.Join(remoteDir, "extra.txt"), []byte("extra"), 0644)
	git(remoteDir, "add", ".")
	git(remoteDir, "commit", "-m", "v2, unrelated to what's pinned")
	unrelatedCommit := headOf(remoteDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - git: acme/remote\n      ref: v1.0.0\n"), 0644)

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: subdirPathLoader{subdirRealLoader()},
	}
	// --target claude only satisfies the "dependencies present but no
	// deployment target" exit-2 guard (F2); this test's subject is stale
	// checkout repair, not deploy.
	if err := runInstall(deps, false, true, "claude", "", nil, nil); err != nil {
		t.Fatalf("first runInstall: %v", err)
	}

	installDir := filepath.Join("apm_modules", "acme", "remote")
	if headOf(installDir) != pinnedHead {
		t.Fatalf("fresh install HEAD mismatch: got %s, want %s", headOf(installDir), pinnedHead)
	}

	// Stale-ify: move the checkout to the unrelated commit, simulating a
	// tampered/corrupted apm_modules directory that no longer matches the
	// pinned v1.0.0 tag.
	git(installDir, "fetch", remoteDir, unrelatedCommit)
	git(installDir, "checkout", unrelatedCommit)
	if headOf(installDir) != unrelatedCommit {
		t.Fatal("test setup failed: checkout should now be at the unrelated commit")
	}

	// --target claude only satisfies the "dependencies present but no
	// deployment target" exit-2 guard (F2); this test's subject is stale
	// checkout repair, not deploy.
	if err := runInstall(deps, false, true, "claude", "", nil, nil); err != nil {
		t.Fatalf("second runInstall: %v", err)
	}

	if got := headOf(installDir); got != pinnedHead {
		t.Errorf("expected stale checkout to be repaired back to pinned %s, got %s (req-lk-007 regression)", pinnedHead, got)
	}
}
