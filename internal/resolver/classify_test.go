package resolver

import (
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

func TestClassifyReference(t *testing.T) {
	tests := []struct {
		name string
		ref  manifest.DependencyReference
		want ReferenceKind
	}{
		{
			name: "local path",
			ref:  manifest.DependencyReference{IsLocal: true, LocalPath: "./packages/foo", Source: "local"},
			want: KindLocal,
		},
		{
			name: "registry entry",
			ref:  manifest.DependencyReference{RepoURL: "owner/repo", Source: "registry"},
			want: KindRegistry,
		},
		{
			name: "git-semver caret",
			ref:  manifest.DependencyReference{RepoURL: "owner/repo", Reference: "^1.2.0", Source: "git"},
			want: KindGitSemver,
		},
		{
			name: "git-semver tilde",
			ref:  manifest.DependencyReference{RepoURL: "owner/repo", Reference: "~2.0", Source: "git"},
			want: KindGitSemver,
		},
		{
			name: "git-semver range",
			ref:  manifest.DependencyReference{RepoURL: "owner/repo", Reference: ">=1.0.0 <2.0.0", Source: "git"},
			want: KindGitSemver,
		},
		{
			name: "git-literal SHA",
			ref:  manifest.DependencyReference{RepoURL: "owner/repo", Reference: "abc123def456", Source: "git"},
			want: KindGitLiteral,
		},
		{
			name: "git-literal branch",
			ref:  manifest.DependencyReference{RepoURL: "owner/repo", Reference: "main", Source: "git"},
			want: KindGitLiteral,
		},
		{
			name: "git-literal tag",
			ref:  manifest.DependencyReference{RepoURL: "owner/repo", Reference: "v1.0.0", Source: "git"},
			want: KindGitLiteral,
		},
		{
			name: "git no ref",
			ref:  manifest.DependencyReference{RepoURL: "owner/repo", Source: "git"},
			want: KindGitLiteral,
		},
		{
			name: "marketplace",
			ref:  manifest.DependencyReference{RepoURL: "plugin-name", Source: "marketplace"},
			want: KindMarketplace,
		},
		{
			name: "shorthand with semver ref",
			ref:  manifest.DependencyReference{RepoURL: "owner/repo", Reference: "^1 || ^2", Source: "git"},
			want: KindGitSemver,
		},
		{
			name: "local takes priority over other fields",
			ref:  manifest.DependencyReference{IsLocal: true, LocalPath: "./foo", Source: "local", Reference: "^1.0.0"},
			want: KindLocal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyReference(&tt.ref)
			if got != tt.want {
				t.Errorf("ClassifyReference() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyRef(t *testing.T) {
	tests := []struct {
		ref  string
		want RefType
	}{
		{"", RefNone},
		{"^1.2.3", RefSemver},
		{"~1.2.0", RefSemver},
		{">=1.0.0 <2.0.0", RefSemver},
		{"*", RefSemver},
		{"main", RefLiteral},
		{"develop", RefLiteral},
		{"v1.0.0", RefLiteral},
		{"abc123", RefLiteral},
		{"refs/heads/main", RefLiteral},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			got := ClassifyRef(tt.ref)
			if got != tt.want {
				t.Errorf("ClassifyRef(%q) = %v, want %v", tt.ref, got, tt.want)
			}
		})
	}
}
