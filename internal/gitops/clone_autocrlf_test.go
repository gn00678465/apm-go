package gitops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

// Issue #119: a package's checkout has the bytes the repository holds, LF
// included, also under core.autocrlf=true, git for Windows' default, as the
// Oracle's git cache checks out (cache/git_cache.py:63-127). autocrlf=true
// converts on checkout on Linux too, so the test runs everywhere.

const autocrlfFile = "line one\nline two\n"

func autocrlfTrueGlobal(t *testing.T) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte("[core]\n\tautocrlf = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func readCheckoutFile(t *testing.T, r *RealPackageLoader, ref *manifest.DependencyReference) string {
	t.Helper()
	installDir, err := r.installPath(ref)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(installDir, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLoadPackage_CheckoutKeepsLFUnderAutocrlfTrue(t *testing.T) {
	for _, byCommit := range []bool{false, true} {
		t.Run(map[bool]string{false: "tag", true: "commit"}[byCommit], func(t *testing.T) {
			ref, remoteDir := chdirToFakeRemote(t)
			autocrlfTrueGlobal(t)
			resolved := initRepoWithTag(t, remoteDir, autocrlfFile, "v1.0.0")
			if !byCommit {
				resolved = "v1.0.0"
			}
			r := &RealPackageLoader{ModulesDir: t.TempDir()}
			if _, err := r.LoadPackage(ref, resolved); err != nil {
				t.Fatalf("LoadPackage: %v", err)
			}
			if got := readCheckoutFile(t, r, ref); got != autocrlfFile {
				t.Errorf("checkout file.txt = %q, want %q", got, autocrlfFile)
			}
			installDir, _ := r.installPath(ref)
			if got := gitCmd(t, installDir, "config", "--local", "--get", "core.autocrlf"); got != "false" {
				t.Errorf("checkout core.autocrlf = %q, want false", got)
			}
		})
	}
}

// A checkout an earlier apm-go cloned under core.autocrlf=true has CRLF in
// its working tree and git status still calls it clean: it is cloned again.
func TestLoadPackage_ReClonesCheckoutWithoutAutocrlfPin(t *testing.T) {
	ref, remoteDir := chdirToFakeRemote(t)
	autocrlfTrueGlobal(t)
	initRepoWithTag(t, remoteDir, autocrlfFile, "v1.0.0")
	r := &RealPackageLoader{ModulesDir: t.TempDir()}
	installDir, err := r.installPath(ref)
	if err != nil {
		t.Fatal(err)
	}
	gitCmd(t, ".", "clone", "--branch", "v1.0.0", "--", "remote", installDir)
	if got := readCheckoutFile(t, r, ref); got != "line one\r\nline two\r\n" {
		t.Fatalf("precondition: an unpinned clone under autocrlf=true has file.txt = %q, want CRLF", got)
	}
	if !worktreeClean(installDir) {
		t.Fatal("precondition: git status does not call the CRLF checkout clean")
	}

	if _, err := r.LoadPackage(ref, "v1.0.0"); err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}
	if got := readCheckoutFile(t, r, ref); got != autocrlfFile {
		t.Errorf("checkout file.txt = %q, want %q", got, autocrlfFile)
	}
}
