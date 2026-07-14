package build

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseRemoteRefs(t *testing.T) {
	// Arrange
	output := "abc123\trefs/tags/v1.0.0\n" +
		"def456\trefs/heads/main\n" +
		"\n"

	// Act
	refs := parseRemoteRefs(output)

	// Assert
	if len(refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2", len(refs))
	}
	if refs[0].Name != "refs/tags/v1.0.0" || refs[0].Commit != "abc123" {
		t.Errorf("refs[0] = %+v", refs[0])
	}
	if refs[1].Name != "refs/heads/main" || refs[1].Commit != "def456" {
		t.Errorf("refs[1] = %+v", refs[1])
	}
}

func TestParseRemoteRefs_Empty(t *testing.T) {
	if refs := parseRemoteRefs(""); len(refs) != 0 {
		t.Errorf("parseRemoteRefs(\"\") = %v, want empty", refs)
	}
}

func TestResolveCloneURL(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{"full https URL passes through", "https://github.com/owner/repo"},
		{"scp-style ssh passes through", "git@github.com:owner/repo.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveCloneURL(tt.source); got != tt.source {
				t.Errorf("resolveCloneURL(%q) = %q, want unchanged", tt.source, got)
			}
		})
	}

	t.Run("absolute filesystem path passes through", func(t *testing.T) {
		abs := filepath.Join(t.TempDir(), "repo")
		if got := resolveCloneURL(abs); got != abs {
			t.Errorf("resolveCloneURL(%q) = %q, want unchanged", abs, got)
		}
	})

	t.Run("owner/repo shorthand expands against github.com", func(t *testing.T) {
		want := "https://github.com/owner/repo.git"
		if got := resolveCloneURL("owner/repo"); got != want {
			t.Errorf("resolveCloneURL(owner/repo) = %q, want %q", got, want)
		}
	})

	t.Run("host-prefixed shorthand expands against that host", func(t *testing.T) {
		want := "https://git.example.com/owner/repo.git"
		if got := resolveCloneURL("git.example.com/owner/repo"); got != want {
			t.Errorf("resolveCloneURL(git.example.com/owner/repo) = %q, want %q", got, want)
		}
	})
}

// TestNewListRefsCmd_AppliesSecureGitEnv proves ListRemoteRefs' subprocess
// is wired through gitops.SecureGitEnv by construction, without spawning a
// subprocess.
func TestNewListRefsCmd_AppliesSecureGitEnv(t *testing.T) {
	// Act
	cmd := newListRefsCmd(context.Background(), "https://example.invalid/owner/repo.git")

	// Assert
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "GCM_INTERACTIVE=never"} {
		found := false
		for _, e := range cmd.Env {
			if e == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("newListRefsCmd().Env missing %q; got %v", want, cmd.Env)
		}
	}
}

// buildFakeGit compiles internal/gitops/testdata/fakegit into a fresh temp
// dir under the platform's expected "git" executable name, returning that
// dir so the caller can prepend it to PATH. Mirrors
// internal/marketplace/authoring/refcheck_test.go's helper of the same
// name/purpose.
func buildFakeGit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	name := "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", out, "../../gitops/testdata/fakegit/main.go")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build fakegit: %v\n%s", err, output)
	}
	return dir
}

// TestGitRefLister_ListRemoteRefs_TimesOutOnSlowRemote proves
// ListRemoteRefs never hangs indefinitely: a "git" that sleeps far longer
// than listRefsTimeout must still cause ListRemoteRefs to return promptly
// with an error once the context deadline fires.
func TestGitRefLister_ListRemoteRefs_TimesOutOnSlowRemote(t *testing.T) {
	// Arrange
	fakeGitDir := buildFakeGit(t)
	t.Setenv("PATH", fakeGitDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKEGIT_SLEEP_MS", "5000")

	orig := listRefsTimeout
	listRefsTimeout = 200 * time.Millisecond
	t.Cleanup(func() { listRefsTimeout = orig })

	// Act
	start := time.Now()
	_, err := (gitRefLister{}).ListRemoteRefs("https://example.invalid/owner/repo.git")
	elapsed := time.Since(start)

	// Assert
	if err == nil {
		t.Fatal("expected ListRemoteRefs to fail once the timeout fires")
	}
	if elapsed > 3*time.Second {
		t.Errorf("ListRemoteRefs took %v, want it to return promptly once the context deadline fires", elapsed)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %v, want it to mention the timeout", err)
	}
}

// TestGitRefLister_ListRemoteRefs_SanitizesCredentialsInErrorMessage proves
// a failing git subprocess's stderr (which can echo the clone URL,
// credentials and all) never leaks a token into ListRemoteRefs' returned
// error (design.md's "ls-remote 失敗訊息不回顯憑證" rule).
func TestGitRefLister_ListRemoteRefs_SanitizesCredentialsInErrorMessage(t *testing.T) {
	// Arrange
	fakeGitDir := buildFakeGit(t)
	t.Setenv("PATH", fakeGitDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKEGIT_FAIL_STDERR", "fatal: unable to access 'https://x-access-token:ghp_supersecret@example.com/owner/repo.git/': The requested URL returned error: 403")

	// Act
	_, err := (gitRefLister{}).ListRemoteRefs("https://x-access-token:ghp_supersecret@example.com/owner/repo.git")

	// Assert
	if err == nil {
		t.Fatal("expected ListRemoteRefs to fail")
	}
	if strings.Contains(err.Error(), "ghp_supersecret") {
		t.Errorf("ListRemoteRefs error leaked a credential: %v", err)
	}
	if !strings.Contains(err.Error(), "example.com") {
		t.Errorf("ListRemoteRefs error lost the host entirely: %v", err)
	}
}
