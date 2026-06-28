package resolver

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
)

func TestComputeWhy_DirectDep(t *testing.T) {
	lock := &lockfile.Lockfile{
		Version: "1",
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/foo", ResolvedRef: "v1.0.0", Depth: 1},
		},
	}
	paths, err := ComputeWhy(lock, "acme/foo")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths count = %d, want 1", len(paths))
	}
	if len(paths[0].Edges) != 1 {
		t.Errorf("edges count = %d, want 1", len(paths[0].Edges))
	}
	if paths[0].Edges[0].Key != "acme/foo" {
		t.Errorf("edge key = %q", paths[0].Edges[0].Key)
	}
}

func TestComputeWhy_TransitiveDep(t *testing.T) {
	lock := &lockfile.Lockfile{
		Version: "1",
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/foo", ResolvedRef: "v1.0.0", Depth: 1},
			{RepoURL: "acme/bar", ResolvedRef: "v2.0.0", ResolvedBy: "acme/foo", Depth: 2},
		},
	}
	paths, err := ComputeWhy(lock, "acme/bar")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths count = %d, want 1", len(paths))
	}
	// Chain: acme/foo -> acme/bar
	str := paths[0].String()
	if !strings.Contains(str, "acme/foo") || !strings.Contains(str, "acme/bar") {
		t.Errorf("path = %q, expected foo -> bar", str)
	}
	// First edge should be the root (foo), last should be target (bar)
	if paths[0].Edges[0].Key != "acme/foo" {
		t.Errorf("first edge = %q, want acme/foo", paths[0].Edges[0].Key)
	}
	if paths[0].Edges[len(paths[0].Edges)-1].Key != "acme/bar" {
		t.Errorf("last edge = %q, want acme/bar", paths[0].Edges[len(paths[0].Edges)-1].Key)
	}
}

func TestComputeWhy_CycleDetection(t *testing.T) {
	lock := &lockfile.Lockfile{
		Version: "1",
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/a", ResolvedRef: "v1.0.0", ResolvedBy: "acme/b", Depth: 1},
			{RepoURL: "acme/b", ResolvedRef: "v1.0.0", ResolvedBy: "acme/a", Depth: 2},
		},
	}
	paths, err := ComputeWhy(lock, "acme/a")
	if err != nil {
		t.Fatal(err)
	}
	// Should terminate (not infinite loop)
	if len(paths) == 0 {
		t.Error("expected at least one path even with cycle")
	}
}

func TestComputeWhy_NotFound(t *testing.T) {
	lock := &lockfile.Lockfile{
		Version: "1",
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/foo", Depth: 1},
		},
	}
	_, err := ComputeWhy(lock, "acme/nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found: %v", err)
	}
}

func TestComputeWhy_LexicographicOrder(t *testing.T) {
	lock := &lockfile.Lockfile{
		Version: "1",
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/a", Depth: 1},
			{RepoURL: "acme/b", Depth: 1},
			{RepoURL: "acme/target", ResolvedBy: "acme/b", Depth: 2},
		},
	}
	paths, err := ComputeWhy(lock, "acme/target")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths count = %d", len(paths))
	}
}

func TestComputeWhy_NilLockfile(t *testing.T) {
	_, err := ComputeWhy(nil, "acme/foo")
	if err == nil {
		t.Fatal("expected error for nil lockfile")
	}
}
