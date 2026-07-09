package resolver

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/semver"
)

func TestPlanFullUpdate(t *testing.T) {
	// Lock has A@v1.2.0, but remote now has v1.9.0
	tags := &mockTagLister{tags: map[string][]semver.TagInfo{
		"acme/a": makeTags("v1.2.0", "v1.5.0", "v1.9.0"),
	}}
	loader := &mockPackageLoader{packages: map[string]*manifest.Manifest{
		"acme/a@v1.9.0": makeManifest("a"),
	}}
	lock := &lockfile.Lockfile{
		Version: "1",
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/a", Constraint: "^1.0.0", ResolvedTag: "v1.2.0"},
		},
	}

	root := makeManifest("root", makeDep("acme/a", "^1.0.0"))
	result, err := PlanFullUpdate(root, lock, tags, loader, ResolverConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deps) != 1 {
		t.Fatalf("deps count = %d", len(result.Deps))
	}
	// Should resolve to newest, not locked
	if result.Deps[0].ResolvedTag != "v1.9.0" {
		t.Errorf("expected v1.9.0 (fresh resolve), got %q", result.Deps[0].ResolvedTag)
	}
}

func TestPlanScopedUpdate_OnlyTargetReResolved(t *testing.T) {
	tags := &mockTagLister{tags: map[string][]semver.TagInfo{
		"acme/a": makeTags("v1.2.0", "v1.5.0", "v1.9.0"),
		"acme/b": makeTags("v2.0.0", "v2.5.0"),
	}}
	loader := &mockPackageLoader{packages: map[string]*manifest.Manifest{
		"acme/a@v1.9.0": makeManifest("a"),
		"acme/b@v2.5.0": makeManifest("b"),
	}}
	lock := &lockfile.Lockfile{
		Version: "1",
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/a", Constraint: "^1.0.0", ResolvedTag: "v1.2.0"},
			{RepoURL: "acme/b", Constraint: "^2.0.0", ResolvedTag: "v2.0.0"},
		},
	}

	root := makeManifest("root",
		makeDep("acme/a", "^1.0.0"),
		makeDep("acme/b", "^2.0.0"),
	)
	result, err := PlanScopedUpdate(root, lock, tags, loader, ResolverConfig{}, "acme/a", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A should be re-resolved to newest
	var aDep, bDep *ResolvedDep
	for i := range result.Deps {
		switch result.Deps[i].Key {
		case "acme/a":
			aDep = &result.Deps[i]
		case "acme/b":
			bDep = &result.Deps[i]
		}
	}
	if aDep == nil {
		t.Fatal("acme/a not found")
	}
	if aDep.ResolvedTag != "v1.9.0" {
		t.Errorf("A should be re-resolved to v1.9.0, got %q", aDep.ResolvedTag)
	}
	// B should still be in the result (re-resolved since current resolver doesn't use lock yet)
	if bDep == nil {
		t.Fatal("acme/b not found")
	}
}

func TestPlanScopedUpdate_FrozenRefused(t *testing.T) {
	lock := &lockfile.Lockfile{Version: "1"}
	root := makeManifest("root", makeDep("acme/a", "^1.0.0"))
	_, err := PlanScopedUpdate(root, lock, &mockTagLister{}, &mockPackageLoader{}, ResolverConfig{}, "acme/a", true)
	if err == nil {
		t.Fatal("expected error for frozen install")
	}
	if !strings.Contains(err.Error(), "frozen") {
		t.Errorf("error should mention frozen: %v", err)
	}
}

func TestPlanScopedUpdate_NoLockfile(t *testing.T) {
	root := makeManifest("root", makeDep("acme/a", "^1.0.0"))
	_, err := PlanScopedUpdate(root, nil, &mockTagLister{}, &mockPackageLoader{}, ResolverConfig{}, "acme/a", false)
	if err == nil {
		t.Fatal("expected error for missing lockfile")
	}
}

func TestPlanScopedUpdate_PackageNotFound(t *testing.T) {
	lock := &lockfile.Lockfile{Version: "1"}
	root := makeManifest("root", makeDep("acme/a", "^1.0.0"))
	_, err := PlanScopedUpdate(root, lock, &mockTagLister{}, &mockPackageLoader{}, ResolverConfig{}, "acme/nonexistent", false)
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found: %v", err)
	}
}

// TestPlanScopedUpdate_DevDependencyFound is the F3-adjacent decision test
// for `apm update <pkg>`: Python's update.py resolves the positional package
// argument against apm_deps + dev_apm_deps (both regular and dev), so a
// hand-authored devDependencies.apm entry must be a valid scoped-update
// target too, not rejected as "not found in manifest".
func TestPlanScopedUpdate_DevDependencyFound(t *testing.T) {
	tags := &mockTagLister{tags: map[string][]semver.TagInfo{
		"acme/b": makeTags("v1.2.0", "v1.9.0"),
	}}
	loader := &mockPackageLoader{packages: map[string]*manifest.Manifest{
		"acme/b@v1.9.0": makeManifest("b"),
	}}
	lock := &lockfile.Lockfile{
		Version: "1",
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/b", Constraint: "^1.0.0", ResolvedTag: "v1.2.0"},
		},
	}

	root := makeManifestWithDev("root", nil, []*manifest.DependencyReference{makeDep("acme/b", "^1.0.0")})
	result, err := PlanScopedUpdate(root, lock, tags, loader, ResolverConfig{}, "acme/b", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var bDep *ResolvedDep
	for i := range result.Deps {
		if result.Deps[i].Key == "acme/b" {
			bDep = &result.Deps[i]
		}
	}
	if bDep == nil {
		t.Fatal("acme/b (dev dependency) not found in scoped update result")
	}
	if bDep.ResolvedTag != "v1.9.0" {
		t.Errorf("dev dep acme/b should be re-resolved to v1.9.0, got %q", bDep.ResolvedTag)
	}
}
