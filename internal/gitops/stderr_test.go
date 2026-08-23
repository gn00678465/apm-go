package gitops

import "testing"

// Expected values are upstream git_stderr.py's own tables and hint/summary
// strings (lines 56-92, 125-154).
func TestTranslateGitStderr(t *testing.T) {
	cases := []struct {
		name     string
		stderr   string
		exitCode int
		remote   string
		wantKind GitErrorKind
		wantSum  string
		wantHint string
	}{
		{
			name: "auth beats everything", stderr: "remote: Invalid username or password.\nfatal: Authentication failed for 'https://github.com/x/y.git/'\n",
			exitCode: 128, remote: "github.com", wantKind: GitErrorAuth,
			wantSum:  "Git authentication failed during ls-remote.",
			wantHint: "Check your GITHUB_TOKEN / gh auth / SSH key. Run 'apm-go doctor' to diagnose.",
		},
		{
			name: "404 is not-found with remote label", stderr: "remote: Repository not found.\nfatal: The requested URL returned error: 404\n",
			exitCode: 128, remote: "github.com", wantKind: GitErrorNotFound,
			wantSum:  "Git ref or repository not found during ls-remote.",
			wantHint: "Verify the remote 'github.com' exists and the ref is spelled correctly.",
		},
		{
			name: "not-found without remote", stderr: "fatal: couldn't find remote ref v9\n",
			exitCode: 2, wantKind: GitErrorNotFound,
			wantSum:  "Git ref or repository not found during ls-remote.",
			wantHint: "Verify the remote the remote exists and the ref is spelled correctly.",
		},
		{
			name: "could not resolve host is DNS/timeout not not-found", stderr: "fatal: unable to access 'https://github.com/git/git.git/': Could not resolve host: github.com\n",
			exitCode: 128, remote: "github.com", wantKind: GitErrorTimeout,
			wantSum:  "Git network timeout during ls-remote.",
			wantHint: "Network issue contacting the remote. Retry or check your connection.",
		},
		{
			name: "connection refused", stderr: "fatal: unable to access 'https://x/': Failed to connect to x port 443: Connection refused\n",
			exitCode: 128, wantKind: GitErrorTimeout,
			wantSum:  "Git network timeout during ls-remote.",
			wantHint: "Network issue contacting the remote. Retry or check your connection.",
		},
		{
			name: "unknown with exit code", stderr: "something odd\n", exitCode: 1, wantKind: GitErrorUnknown,
			wantSum:  "Git failed during ls-remote (exit 1).",
			wantHint: "Git failed during ls-remote. See raw stderr above.",
		},
		{
			name: "unknown without exit code", stderr: "", exitCode: -1, wantKind: GitErrorUnknown,
			wantSum:  "Git failed during ls-remote.",
			wantHint: "Git failed during ls-remote. See raw stderr above.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TranslateGitStderr(tc.stderr, tc.exitCode, "ls-remote", tc.remote)
			if got.Kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", got.Kind, tc.wantKind)
			}
			if got.Summary != tc.wantSum {
				t.Errorf("summary = %q, want %q", got.Summary, tc.wantSum)
			}
			if got.Hint != tc.wantHint {
				t.Errorf("hint = %q, want %q", got.Hint, tc.wantHint)
			}
			if got.Raw != tc.stderr {
				t.Errorf("raw = %q, want %q", got.Raw, tc.stderr)
			}
		})
	}
}

func TestTranslateGitStderr_RawTruncatedAt500(t *testing.T) {
	long := make([]byte, 600)
	for i := range long {
		long[i] = 'a'
	}
	got := TranslateGitStderr(string(long), 1, "clone", "")
	if len(got.Raw) != 500+len("... (truncated)") || got.Raw[500:] != "... (truncated)" {
		t.Errorf("raw = %d bytes, tail %q", len(got.Raw), got.Raw[len(got.Raw)-16:])
	}
}
