package marketplace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
)

const (
	rewriteInspectHint = `inspect matching rules with 'git config --show-origin --get-regexp ^url\..*\.insteadOf$'`
	rewriteRetryHint   = "Correct the matching Git configuration, then run 'apm-go marketplace update acme' to retry."
)

// gitConfigWithRule is a gitops.ConfigProbeFunc whose git configuration has
// one insteadOf rule and nothing else.
func gitConfigWithRule(replacement, prefix string) gitops.ConfigProbeFunc {
	return func(context.Context, gitops.ConfigProbe) ([]byte, int, error) {
		return []byte("global\x00url." + replacement + ".insteadOf\n" + prefix + "\x00"), 0, nil
	}
}

// recordGitInvocations puts the recording fake git first on PATH and returns
// the file it writes when it is run.
func recordGitInvocations(t *testing.T) string {
	t.Helper()
	record := filepath.Join(t.TempDir(), "git-invocations")
	t.Setenv("PATH", buildFakeGit(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKEGIT_RECORD_FILE", record)
	return record
}

func assertGitNotRun(t *testing.T, record string) {
	t.Helper()
	if invoked, err := os.ReadFile(record); err == nil {
		t.Fatalf("git was run: %s", invoked)
	}
}

// Expected sentences: Oracle v0.33.0 marketplace/client.py:580-603 inside
// marketplace/errors.py:60, with apm-go in the retry hint.
func TestFetch_Git_RewriteCheckFailure(t *testing.T) {
	const remote = "https://git.example.com/team/plugins.git"
	tests := []struct {
		name       string
		probe      gitops.ConfigProbeFunc
		remote     string
		wantReason string
	}{
		{"configuration unreadable", gitops.FailingGitConfigProbe, remote,
			"Unable to verify Git URL rewrite safety (Git config probe failed); check Git configuration and retry; " + rewriteInspectHint},
		{"credentials", gitConfigWithRule("https://user:pw@git.example.com/", "https://git.example.com/"), remote,
			"Git URL rewrite contains credentials; " + rewriteInspectHint + " and remove the unsafe rule"},
		{"credential-origin", gitConfigWithRule("https://git.example.com/other/", "https://token@git.example.com/team/"), "https://token@git.example.com/team/plugins.git",
			"authenticated Git URL rewrite changes the remote origin; " + rewriteInspectHint + " and remove the unsafe rule"},
		{"https-downgrade", gitConfigWithRule("http://git.example.com/", "https://git.example.com/"), remote,
			"HTTPS Git remote was rejected because Git configuration rewrites it to insecure HTTP; " + rewriteInspectHint + " and remove the unsafe rule"},
		{"insecure-transport", gitConfigWithRule("git://git.example.com/", "https://git.example.com/"), remote,
			"HTTPS Git URL rewrite selects an insecure transport; " + rewriteInspectHint + " and remove the unsafe rule"},
		{"cross-host has no wording of its own", gitConfigWithRule("ssh://git@mirror.example.com/", "https://git.example.com/"), remote,
			"unsafe Git URL rewrite; " + rewriteInspectHint + " and remove the unsafe rule"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			record := recordGitInvocations(t)
			t.Cleanup(gitops.SetConfigProbeForTest(tc.probe))
			t.Setenv("GIT_HTTP_EXTRAHEADER", "")
			src := &MarketplaceSource{Name: "acme", URL: tc.remote, Ref: "main"}
			if src.Kind() != KindGit {
				t.Fatalf("fixture is %s, want a generic git source", src.Kind())
			}

			_, err := Fetch(context.Background(), src)

			want := "Failed to fetch marketplace 'acme': " + tc.wantReason + ". " + rewriteRetryHint
			if err == nil || err.Error() != want {
				t.Fatalf("error\n got %v\nwant %s", err, want)
			}
			assertGitNotRun(t, record)
		})
	}
}

// `marketplace add` fetches before the source is named.
func TestFetch_Git_RewriteCheckFailure_UnnamedSource(t *testing.T) {
	record := recordGitInvocations(t)
	t.Cleanup(gitops.SetConfigProbeForTest(gitops.FailingGitConfigProbe))
	reason := "Unable to verify Git URL rewrite safety (Git config probe failed); check Git configuration and retry; " + rewriteInspectHint
	tests := []struct {
		repo, wantName string
	}{
		{"plugins", "plugins"},
		{"", "marketplace"},
	}
	for _, tc := range tests {
		_, err := Fetch(context.Background(), &MarketplaceSource{URL: "https://git.example.com/team/plugins.git", Ref: "main", Repo: tc.repo})

		want := "Failed to fetch marketplace '" + tc.wantName + "': " + reason +
			". Correct the matching Git configuration, then run 'apm-go marketplace update " + tc.wantName + "' to retry."
		if err == nil || err.Error() != want {
			t.Errorf("repo %q\n got %v\nwant %s", tc.repo, err, want)
		}
	}
	assertGitNotRun(t, record)
}

// The commit-pinned path clones without --branch; it takes the same check.
func TestFetch_Git_RewriteCheckFailure_CommitRef(t *testing.T) {
	record := recordGitInvocations(t)
	t.Cleanup(gitops.SetConfigProbeForTest(gitops.FailingGitConfigProbe))

	_, err := Fetch(context.Background(), &MarketplaceSource{
		Name: "acme", URL: "https://git.example.com/team/plugins.git", Ref: "0123456789abcdef0123456789abcdef01234567",
	})

	if err == nil {
		t.Fatal("want the check to fail the fetch")
	}
	assertGitNotRun(t, record)
}

// An invalid ref is still reported first, as in the Oracle
// (marketplace/client.py:571 runs _validate_ref before the policy).
func TestFetch_Git_InvalidRefBeforeRewriteCheck(t *testing.T) {
	probed := false
	t.Cleanup(gitops.SetConfigProbeForTest(func(context.Context, gitops.ConfigProbe) ([]byte, int, error) {
		probed = true
		return nil, 1, nil
	}))

	_, err := Fetch(context.Background(), &MarketplaceSource{Name: "acme", URL: "https://git.example.com/team/plugins.git", Ref: "--upload-pack=x"})

	if err == nil || probed {
		t.Fatalf("want the ref rejected before any probe; err=%v probed=%v", err, probed)
	}
}
