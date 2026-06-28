package resolver

import (
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
)

func TestShouldReplay(t *testing.T) {
	tests := []struct {
		name       string
		manifest   string
		locked     string
		wantReplay bool
	}{
		{"exact match", "^1.2.0", "^1.2.0", true},
		{"whitespace differs", "^1.2.0", "^ 1.2.0", false},
		{"range changed", "^1.3.0", "^1.2.0", false},
		{"both empty", "", "", true},
		{"semantically equal different string", ">=1.0.0, <2.0.0", ">=1.0.0,<2.0.0", false},
		{"leading space", " ^1.2.0", "^1.2.0", false},
		{"trailing space", "^1.2.0 ", "^1.2.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldReplay(tt.manifest, tt.locked)
			if got != tt.wantReplay {
				t.Errorf("ShouldReplay(%q, %q) = %v, want %v", tt.manifest, tt.locked, got, tt.wantReplay)
			}
		})
	}
}

func TestReplayDecision(t *testing.T) {
	lock := &lockfile.Lockfile{
		Version: "1",
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/foo", Constraint: "^1.2.0", ResolvedTag: "v1.9.0"},
		},
	}

	tests := []struct {
		name string
		ref  *manifest.DependencyReference
		lock *lockfile.Lockfile
		want ReplayAction
	}{
		{
			"replay: same constraint",
			&manifest.DependencyReference{RepoURL: "acme/foo", Reference: "^1.2.0", Source: "git"},
			lock, ReplayLocked,
		},
		{
			"re-resolve: constraint changed",
			&manifest.DependencyReference{RepoURL: "acme/foo", Reference: "^1.5.0", Source: "git"},
			lock, ReResolve,
		},
		{
			"new dep: not in lock",
			&manifest.DependencyReference{RepoURL: "acme/bar", Reference: "^1.0.0", Source: "git"},
			lock, NewDep,
		},
		{
			"new dep: no lockfile",
			&manifest.DependencyReference{RepoURL: "acme/foo", Reference: "^1.2.0", Source: "git"},
			nil, NewDep,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReplayDecision(tt.ref, tt.lock)
			if got != tt.want {
				t.Errorf("ReplayDecision() = %v, want %v", got, tt.want)
			}
		})
	}
}
