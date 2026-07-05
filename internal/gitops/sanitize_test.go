package gitops

import "testing"

func TestSanitizeGitOutput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "https url with PAT-style token userinfo",
			in:   "fatal: unable to access 'https://x-access-token:ghp_abc123@github.com/owner/repo.git/': The requested URL returned error: 403",
			want: "fatal: unable to access 'https://github.com/owner/repo.git/': The requested URL returned error: 403",
		},
		{
			name: "https url with user:pass",
			in:   "https://user:secret@example.com/repo.git",
			want: "https://example.com/repo.git",
		},
		{
			name: "no embedded credentials left unchanged",
			in:   "fatal: repository 'https://github.com/owner/repo.git/' not found",
			want: "fatal: repository 'https://github.com/owner/repo.git/' not found",
		},
		{
			name: "scp-style ssh remote unaffected (no scheme)",
			in:   "git@github.com:owner/repo.git",
			want: "git@github.com:owner/repo.git",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
		{
			name: "multiple credentialed urls in one message",
			in:   "https://a:b@host1/x.git and https://c:d@host2/y.git",
			want: "https://host1/x.git and https://host2/y.git",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeGitOutput(tt.in); got != tt.want {
				t.Errorf("SanitizeGitOutput(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
