package gitops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

// commitRefRemote makes remoteDir a repository whose default branch main has
// two commits, file.txt "v1" then "v2", and pkg/file.txt with the same
// content. It returns both commits.
func commitRefRemote(t *testing.T, remoteDir string) (c1, c2 string) {
	t.Helper()
	gitCmd(t, remoteDir, "init", "-b", "main")
	gitCmd(t, remoteDir, "config", "core.autocrlf", "false")
	if err := os.MkdirAll(filepath.Join(remoteDir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	commit := func(content string) string {
		for _, f := range []string{"file.txt", filepath.Join("pkg", "file.txt")} {
			if err := os.WriteFile(filepath.Join(remoteDir, f), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		gitCmd(t, remoteDir, "add", ".")
		gitCmd(t, remoteDir, "commit", "-m", content)
		return gitCmd(t, remoteDir, "rev-parse", "HEAD")
	}
	return commit("v1"), commit("v2")
}

func loadedCommit(t *testing.T, r *RealPackageLoader, ref *manifest.DependencyReference) string {
	t.Helper()
	dir, err := r.installPath(ref)
	if err != nil {
		t.Fatal(err)
	}
	head, err := ResolveCommit(dir)
	if err != nil {
		t.Fatalf("installed checkout: %v", err)
	}
	return head
}

// The expected commits are the ones Oracle 0.33.0 installed for the same
// refs (issue #124 investigation, scenarios short7, short12, upper, tag, br,
// hextag): a name in refs comes before a commit prefix, but a branch other
// than the default one is not found by its short name.
func TestLoadPackage_CommitRefs(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, remote, c1, c2 string) (ref string)
		want  func(c1, c2 string) string
	}{
		{"7 hex characters", func(t *testing.T, _, c1, _ string) string { return c1[:7] }, first},
		{"12 hex characters", func(t *testing.T, _, c1, _ string) string { return c1[:12] }, first},
		{"upper case", func(t *testing.T, _, c1, _ string) string { return strings.ToUpper(c1[:7]) }, first},
		{"full commit", func(t *testing.T, _, c1, _ string) string { return c1 }, first},
		{"tag named like a commit prefix", func(t *testing.T, remote, c1, _ string) string {
			gitCmd(t, remote, "tag", c1[:7])
			return c1[:7]
		}, second},
		{"branch named like a commit prefix", func(t *testing.T, remote, c1, _ string) string {
			gitCmd(t, remote, "branch", c1[:7])
			return c1[:7]
		}, first},
		{"hex tag that is no commit prefix", func(t *testing.T, remote, c1, _ string) string {
			gitCmd(t, remote, "tag", "cafe1234", c1)
			return "cafe1234"
		}, first},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, remoteDir := chdirToFakeRemote(t)
			c1, c2 := commitRefRemote(t, remoteDir)
			pin := tt.setup(t, remoteDir, c1, c2)
			r := &RealPackageLoader{ModulesDir: t.TempDir()}

			if _, err := r.LoadPackage(ref, pin); err != nil {
				t.Fatalf("LoadPackage(%q): %v", pin, err)
			}

			want := tt.want(c1, c2)
			if got := loadedCommit(t, r, ref); got != want {
				t.Fatalf("installed %s, want %s", got, want)
			}
			dir, _ := r.installPath(ref)
			if branch := CheckedOutBranch(dir); branch != "" {
				t.Errorf("HEAD is on branch %q, want detached", branch)
			}
			if out := gitCmd(t, dir, "branch", "--list", pin); out != "" {
				t.Errorf("checkout made a local branch %q", out)
			}

			// A marker in .git survives only when the checkout is reused.
			marker := filepath.Join(dir, ".git", "reused-marker")
			if err := os.WriteFile(marker, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := r.LoadPackage(ref, pin); err != nil {
				t.Fatalf("second LoadPackage(%q): %v", pin, err)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Errorf("second LoadPackage cloned again: %v", err)
			}
			if got := loadedCommit(t, r, ref); got != want {
				t.Errorf("second LoadPackage: %s, want %s", got, want)
			}
		})
	}
}

func first(c1, _ string) string  { return c1 }
func second(_, c2 string) string { return c2 }

// Oracle 0.33.0 (scenario hex): Failed to download dependency o/hex: Could
// not resolve commit 'deadbeef' in repository o/hex: fatal: Needed a single
// revision. It is not tried as a branch.
func TestLoadPackage_HexBranchThatIsNoCommitFails(t *testing.T) {
	ref, remoteDir := chdirToFakeRemote(t)
	c1, _ := commitRefRemote(t, remoteDir)
	gitCmd(t, remoteDir, "branch", "deadbeef", c1)
	r := &RealPackageLoader{ModulesDir: t.TempDir()}

	_, err := r.LoadPackage(ref, "deadbeef")

	want := "clone remote: Could not resolve commit 'deadbeef' in repository remote: fatal: Needed a single revision"
	if err == nil || err.Error() != want {
		t.Fatalf("LoadPackage error = %v, want %q", err, want)
	}
	dir, _ := r.installPath(ref)
	if _, statErr := os.Lstat(dir); !os.IsNotExist(statErr) {
		t.Errorf("%s left behind (stat: %v)", dir, statErr)
	}
}

// apm-go before issue #124 cloned a hex ref that is not 40 characters as a
// branch, so an old checkout is on a branch of that name. Its commit is not
// what the ref names now, and it is cloned again.
func TestLoadPackage_ReClonesBranchCheckoutOfCommitRef(t *testing.T) {
	ref, remoteDir := chdirToFakeRemote(t)
	c1, _ := commitRefRemote(t, remoteDir)
	gitCmd(t, remoteDir, "branch", c1[:7])
	r := &RealPackageLoader{ModulesDir: t.TempDir()}
	dir, err := r.installPath(ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, filepath.Dir(dir), "-c", autocrlfOff, "clone", "-c", autocrlfOff, "--depth", "1",
		"--branch", c1[:7], "file://"+remoteDir, filepath.Base(dir))

	if _, err := r.LoadPackage(ref, c1[:7]); err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}

	if got := loadedCommit(t, r, ref); got != c1 {
		t.Errorf("installed %s, want %s", got, c1)
	}
}

// A frozen install loads a package by the commit of its lock. A checkout of
// a branch ref sits on that branch, and is reused as main reused it.
func TestLoadPackage_ReusesBranchCheckoutForItsCommit(t *testing.T) {
	ref, remoteDir := chdirToFakeRemote(t)
	_, c2 := commitRefRemote(t, remoteDir)
	r := &RealPackageLoader{ModulesDir: t.TempDir()}
	if _, err := r.LoadPackage(ref, "main"); err != nil {
		t.Fatalf("LoadPackage(main): %v", err)
	}
	dir, _ := r.installPath(ref)
	if branch := CheckedOutBranch(dir); branch != "main" {
		t.Fatalf("branch clone is on %q, want main", branch)
	}
	marker := filepath.Join(dir, ".git", "reused-marker")
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := r.LoadPackage(ref, c2); err != nil {
		t.Fatalf("LoadPackage(%s): %v", c2, err)
	}

	if _, err := os.Stat(marker); err != nil {
		t.Errorf("checkout of main was cloned again for its own commit: %v", err)
	}
}

// Oracle 0.33.0 (scenario sub): a subdirectory package pinned to a short
// commit is taken from that commit.
func TestLoadPackage_SubdirectoryAtShortCommit(t *testing.T) {
	_, remoteDir := chdirToFakeRemote(t)
	c1, _ := commitRefRemote(t, remoteDir)
	ref := &manifest.DependencyReference{RepoURL: "remote", VirtualPath: "pkg", VirtualType: "subdirectory"}
	r := &RealPackageLoader{ModulesDir: t.TempDir()}

	if _, err := r.LoadPackage(ref, strings.ToUpper(c1[:7])); err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}

	dir, _ := r.installPath(ref)
	data, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil || string(data) != "v1" {
		t.Errorf("pkg/file.txt = %q (%v), want v1", data, err)
	}
	if got := r.SubdirCommit("remote/pkg"); got != c1 {
		t.Errorf("SubdirCommit = %q, want %s", got, c1)
	}
}
