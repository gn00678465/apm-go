package lockfile

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

func TestIsTruthyCI(t *testing.T) {
	tests := []struct {
		val  string
		want bool
	}{
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"1", true},
		{"yes", true},
		{"anything", true},
		{"", false},
		{"0", false},
		{"false", false},
		{"FALSE", false},
		{"False", false},
	}
	for _, tt := range tests {
		t.Run(tt.val, func(t *testing.T) {
			if got := IsTruthyCI(tt.val); got != tt.want {
				t.Errorf("IsTruthyCI(%q) = %v, want %v", tt.val, got, tt.want)
			}
		})
	}
}

func TestCheckFrozenInstall_AllPresent(t *testing.T) {
	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: "acme/foo", Source: "git"},
		},
	}
	lock := &Lockfile{
		Version:      "1",
		Dependencies: []LockedDep{{RepoURL: "acme/foo"}},
	}
	if err := CheckFrozenInstall(m, lock); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckFrozenInstall_MissingPin(t *testing.T) {
	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: "acme/foo", Source: "git"},
			{RepoURL: "acme/bar", Source: "git"},
		},
	}
	lock := &Lockfile{
		Version:      "1",
		Dependencies: []LockedDep{{RepoURL: "acme/foo"}},
	}
	err := CheckFrozenInstall(m, lock)
	if err == nil {
		t.Fatal("expected error for missing pin")
	}
	if !strings.Contains(err.Error(), "acme/bar") {
		t.Errorf("error should mention acme/bar: %v", err)
	}
}

func TestCheckFrozenInstall_NoLockfile(t *testing.T) {
	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: "acme/foo", Source: "git"},
		},
	}
	err := CheckFrozenInstall(m, nil)
	if err == nil {
		t.Fatal("expected error for nil lockfile")
	}
}
