package resolver

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/semver"
)

// ── Test doubles ──

type mockTagLister struct {
	tags map[string][]semver.TagInfo
}

func (m *mockTagLister) ListTags(repoURL string) ([]semver.TagInfo, error) {
	return m.tags[repoURL], nil
}

// mockPackageLoader returns canned manifests keyed by (repoURL, resolvedRef).
type mockPackageLoader struct {
	packages map[string]*manifest.Manifest // key: "repoURL@ref"
}

func (m *mockPackageLoader) LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error) {
	key := ref.RepoURL + "@" + resolvedRef
	if pkg, ok := m.packages[key]; ok {
		return pkg, nil
	}
	return nil, nil
}

// ── Helpers ──

func makeDep(repo, ref string) *manifest.DependencyReference {
	return &manifest.DependencyReference{
		RepoURL:   repo,
		Owner:     strings.Split(repo, "/")[0],
		Repo:      strings.Split(repo, "/")[1],
		Reference: ref,
		Source:    "git",
	}
}

func makeManifest(name string, deps ...*manifest.DependencyReference) *manifest.Manifest {
	return &manifest.Manifest{
		Name:       name,
		Version:    "1.0.0",
		ParsedDeps: deps,
	}
}

func makeTags(names ...string) []semver.TagInfo {
	tags := make([]semver.TagInfo, len(names))
	for i, n := range names {
		tags[i] = semver.TagInfo{Name: n, Commit: "sha-" + n}
	}
	return tags
}

// ── Tests ──

func TestResolve_LinearThreeLevel(t *testing.T) {
	// root -> A@^1.0.0 -> B@^2.0.0 -> C@^3.0.0
	tags := &mockTagLister{tags: map[string][]semver.TagInfo{
		"acme/a": makeTags("v1.0.0", "v1.5.0", "v1.9.0"),
		"acme/b": makeTags("v2.0.0", "v2.1.0"),
		"acme/c": makeTags("v3.0.0", "v3.1.0"),
	}}
	loader := &mockPackageLoader{packages: map[string]*manifest.Manifest{
		"acme/a@v1.9.0": makeManifest("a", makeDep("acme/b", "^2.0.0")),
		"acme/b@v2.1.0": makeManifest("b", makeDep("acme/c", "^3.0.0")),
		"acme/c@v3.1.0": makeManifest("c"),
	}}

	root := makeManifest("root", makeDep("acme/a", "^1.0.0"))
	result, err := Resolve(root, nil, tags, loader, ResolverConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Deps) != 3 {
		t.Fatalf("deps count = %d, want 3", len(result.Deps))
	}

	// Check BFS order: A, B, C
	wantOrder := []string{"acme/a", "acme/b", "acme/c"}
	for i, want := range wantOrder {
		if result.Deps[i].Key != want {
			t.Errorf("deps[%d].Key = %q, want %q", i, result.Deps[i].Key, want)
		}
	}

	// Check resolved tags
	if result.Deps[0].ResolvedTag != "v1.9.0" {
		t.Errorf("A resolved to %q, want v1.9.0", result.Deps[0].ResolvedTag)
	}
	if result.Deps[1].ResolvedTag != "v2.1.0" {
		t.Errorf("B resolved to %q, want v2.1.0", result.Deps[1].ResolvedTag)
	}
	if result.Deps[2].ResolvedTag != "v3.1.0" {
		t.Errorf("C resolved to %q, want v3.1.0", result.Deps[2].ResolvedTag)
	}

	// Check depths
	if result.Deps[0].Depth != 1 {
		t.Errorf("A depth = %d, want 1", result.Deps[0].Depth)
	}
	if result.Deps[1].Depth != 2 {
		t.Errorf("B depth = %d, want 2", result.Deps[1].Depth)
	}
	if result.Deps[2].Depth != 3 {
		t.Errorf("C depth = %d, want 3", result.Deps[2].Depth)
	}
}

func TestResolve_DiamondIntersectionPick(t *testing.T) {
	// root -> A@^1.2.0 (direct)
	// root -> B@^2.0.0 -> A@^1.5.0 (transitive)
	// Intersection of ^1.2.0 and ^1.5.0 = [>=1.5.0, <2.0.0], pick highest
	tags := &mockTagLister{tags: map[string][]semver.TagInfo{
		"acme/a": makeTags("v1.2.0", "v1.5.0", "v1.7.0", "v1.9.0"),
		"acme/b": makeTags("v2.0.0", "v2.1.0"),
	}}
	loader := &mockPackageLoader{packages: map[string]*manifest.Manifest{
		"acme/a@v1.9.0": makeManifest("a"),
		"acme/b@v2.1.0": makeManifest("b", makeDep("acme/a", "^1.5.0")),
	}}

	root := makeManifest("root",
		makeDep("acme/a", "^1.2.0"),
		makeDep("acme/b", "^2.0.0"),
	)
	result, err := Resolve(root, nil, tags, loader, ResolverConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A should resolve to v1.9.0 (highest in intersection)
	var aDep *ResolvedDep
	for i := range result.Deps {
		if result.Deps[i].Key == "acme/a" {
			aDep = &result.Deps[i]
			break
		}
	}
	if aDep == nil {
		t.Fatal("acme/a not found in result")
	}
	if aDep.ResolvedTag != "v1.9.0" {
		t.Errorf("A resolved to %q, want v1.9.0 (highest in intersection)", aDep.ResolvedTag)
	}
}

func TestResolve_DiamondFailClosed(t *testing.T) {
	// root -> A@^1.0.0 (direct)
	// root -> B@^2.0.0 -> A@^2.0.0 (transitive)
	// Intersection of ^1.0.0 and ^2.0.0 = empty → fail
	tags := &mockTagLister{tags: map[string][]semver.TagInfo{
		"acme/a": makeTags("v1.0.0", "v1.5.0", "v2.0.0", "v2.1.0"),
		"acme/b": makeTags("v2.0.0"),
	}}
	loader := &mockPackageLoader{packages: map[string]*manifest.Manifest{
		"acme/a@v1.5.0": makeManifest("a"),
		"acme/b@v2.0.0": makeManifest("b", makeDep("acme/a", "^2.0.0")),
	}}

	root := makeManifest("root",
		makeDep("acme/a", "^1.0.0"),
		makeDep("acme/b", "^2.0.0"),
	)
	_, err := Resolve(root, nil, tags, loader, ResolverConfig{})
	if err == nil {
		t.Fatal("expected error for empty intersection diamond conflict")
	}
	// req-rs-010: diagnostic should contain chain format
	if !strings.Contains(err.Error(), "acme/a") {
		t.Errorf("error should mention acme/a: %v", err)
	}
	if !strings.Contains(err.Error(), "chain") {
		t.Errorf("error should contain chain info: %v", err)
	}
}

func TestResolve_FixpointReExpansion(t *testing.T) {
	// Discriminating test per advisor guidance:
	// root -> A@^1.0.0 → first-see resolves A to v1.9.0, which has child X@^2.0.0
	// root -> B@^1.0.0 → B has child A@~1.3.0
	// Intersection of ^1.0.0 and ~1.3.0 = [>=1.3.0, <1.4.0], so A re-pins to v1.3.0
	// A@v1.3.0 has child Y@^1.0.0 (different from v1.9.0's X@^2.0.0)
	// Final graph should have Y, NOT X.
	tags := &mockTagLister{tags: map[string][]semver.TagInfo{
		"acme/a": makeTags("v1.0.0", "v1.3.0", "v1.9.0"),
		"acme/b": makeTags("v1.0.0"),
		"acme/x": makeTags("v2.0.0"),
		"acme/y": makeTags("v1.0.0"),
	}}
	loader := &mockPackageLoader{packages: map[string]*manifest.Manifest{
		"acme/a@v1.9.0": makeManifest("a-1.9", makeDep("acme/x", "^2.0.0")),
		"acme/a@v1.3.0": makeManifest("a-1.3", makeDep("acme/y", "^1.0.0")),
		"acme/b@v1.0.0": makeManifest("b", makeDep("acme/a", "~1.3.0")),
		"acme/x@v2.0.0": makeManifest("x"),
		"acme/y@v1.0.0": makeManifest("y"),
	}}

	root := makeManifest("root",
		makeDep("acme/a", "^1.0.0"),
		makeDep("acme/b", "^1.0.0"),
	)
	result, err := Resolve(root, nil, tags, loader, ResolverConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A should be re-pinned to v1.3.0
	var aDep *ResolvedDep
	for i := range result.Deps {
		if result.Deps[i].Key == "acme/a" {
			aDep = &result.Deps[i]
		}
	}
	if aDep == nil {
		t.Fatal("acme/a not found")
	}
	if aDep.ResolvedTag != "v1.3.0" {
		t.Errorf("A resolved to %q, want v1.3.0 (re-pinned by intersection)", aDep.ResolvedTag)
	}

	// Y should be in the graph (from A@v1.3.0), NOT X (from A@v1.9.0)
	hasY := false
	hasX := false
	for _, dep := range result.Deps {
		if dep.Key == "acme/y" {
			hasY = true
		}
		if dep.Key == "acme/x" {
			hasX = true
		}
	}
	if !hasY {
		t.Error("acme/y should be in graph (child of A@v1.3.0)")
	}
	if hasX {
		t.Error("acme/x should NOT be in graph (stale child of A@v1.9.0)")
	}
}

func TestResolve_DepthLimit(t *testing.T) {
	// Chain: A -> B -> C -> D (depth 4), with maxDepth=3
	tags := &mockTagLister{tags: map[string][]semver.TagInfo{
		"acme/a": makeTags("v1.0.0"),
		"acme/b": makeTags("v1.0.0"),
		"acme/c": makeTags("v1.0.0"),
		"acme/d": makeTags("v1.0.0"),
	}}
	loader := &mockPackageLoader{packages: map[string]*manifest.Manifest{
		"acme/a@v1.0.0": makeManifest("a", makeDep("acme/b", "^1.0.0")),
		"acme/b@v1.0.0": makeManifest("b", makeDep("acme/c", "^1.0.0")),
		"acme/c@v1.0.0": makeManifest("c", makeDep("acme/d", "^1.0.0")),
		"acme/d@v1.0.0": makeManifest("d"),
	}}

	root := makeManifest("root", makeDep("acme/a", "^1.0.0"))
	_, err := Resolve(root, nil, tags, loader, ResolverConfig{MaxDepth: 3})
	if err == nil {
		t.Fatal("expected error for depth limit")
	}
	if !strings.Contains(err.Error(), "depth limit") {
		t.Errorf("error should mention depth limit: %v", err)
	}
}

func TestResolve_NestRejection(t *testing.T) {
	root := &manifest.Manifest{
		Name:               "test",
		Version:            "1.0.0",
		ConflictResolution: "nest",
	}
	_, err := Resolve(root, nil, &mockTagLister{}, &mockPackageLoader{}, ResolverConfig{})
	if err == nil {
		t.Fatal("expected error for conflict_resolution: nest")
	}
	if !strings.Contains(err.Error(), "nest") || !strings.Contains(err.Error(), "v0.2") {
		t.Errorf("error should mention nest and v0.2: %v", err)
	}
}

func TestResolve_DeclarationOrder(t *testing.T) {
	// root -> C, B, A (in this order) — result should preserve declaration order
	tags := &mockTagLister{tags: map[string][]semver.TagInfo{
		"acme/c": makeTags("v1.0.0"),
		"acme/b": makeTags("v1.0.0"),
		"acme/a": makeTags("v1.0.0"),
	}}
	loader := &mockPackageLoader{packages: map[string]*manifest.Manifest{
		"acme/c@v1.0.0": makeManifest("c"),
		"acme/b@v1.0.0": makeManifest("b"),
		"acme/a@v1.0.0": makeManifest("a"),
	}}

	root := makeManifest("root",
		makeDep("acme/c", "^1.0.0"),
		makeDep("acme/b", "^1.0.0"),
		makeDep("acme/a", "^1.0.0"),
	)
	result, err := Resolve(root, nil, tags, loader, ResolverConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantOrder := []string{"acme/c", "acme/b", "acme/a"}
	for i, want := range wantOrder {
		if result.Deps[i].Key != want {
			t.Errorf("deps[%d].Key = %q, want %q", i, result.Deps[i].Key, want)
		}
	}
}

func TestResolve_EmptyDeps(t *testing.T) {
	root := makeManifest("root")
	result, err := Resolve(root, nil, &mockTagLister{}, &mockPackageLoader{}, ResolverConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deps) != 0 {
		t.Errorf("deps count = %d, want 0", len(result.Deps))
	}
}

func TestResolve_LockReplay(t *testing.T) {
	// Lock has A pinned to v1.2.0 with constraint ^1.0.0.
	// Remote has v1.9.0 available. With lock replay, resolver should
	// return the locked v1.2.0 (no network call needed).
	tags := &mockTagLister{tags: map[string][]semver.TagInfo{
		"acme/a": makeTags("v1.2.0", "v1.9.0"),
	}}
	loader := &mockPackageLoader{packages: map[string]*manifest.Manifest{
		"acme/a@v1.2.0": makeManifest("a"),
	}}
	lock := &lockfile.Lockfile{
		Version: "1",
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/a", Constraint: "^1.0.0", ResolvedTag: "v1.2.0", ResolvedCommit: "sha-locked"},
		},
	}

	root := makeManifest("root", makeDep("acme/a", "^1.0.0"))
	result, err := Resolve(root, lock, tags, loader, ResolverConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deps) != 1 {
		t.Fatalf("deps count = %d", len(result.Deps))
	}
	// Should replay locked tag, not resolve to newest
	if result.Deps[0].ResolvedTag != "v1.2.0" {
		t.Errorf("expected locked v1.2.0, got %q", result.Deps[0].ResolvedTag)
	}
	if result.Deps[0].Commit != "sha-locked" {
		t.Errorf("expected locked commit sha-locked, got %q", result.Deps[0].Commit)
	}
}

func TestResolve_LockReplay_ConstraintChanged(t *testing.T) {
	// Manifest constraint changed from ^1.0.0 to ^1.5.0 — lock should NOT replay
	tags := &mockTagLister{tags: map[string][]semver.TagInfo{
		"acme/a": makeTags("v1.5.0", "v1.9.0"),
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

	root := makeManifest("root", makeDep("acme/a", "^1.5.0"))
	result, err := Resolve(root, lock, tags, loader, ResolverConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Constraint changed, so should re-resolve to newest
	if result.Deps[0].ResolvedTag != "v1.9.0" {
		t.Errorf("expected re-resolved v1.9.0, got %q", result.Deps[0].ResolvedTag)
	}
}

func TestResolve_ResolvedByIsParentKey(t *testing.T) {
	// root -> A@^1.2.0 (direct, depth 1)
	// root -> B@^2.0.0 -> A@~1.7.0 (transitive, depth 2, tighter)
	// ResolvedBy for A should be the parent key "acme/b" (not a chain string)
	tags := &mockTagLister{tags: map[string][]semver.TagInfo{
		"acme/a": makeTags("v1.2.0", "v1.7.0", "v1.7.4", "v1.9.0"),
		"acme/b": makeTags("v2.0.0"),
	}}
	loader := &mockPackageLoader{packages: map[string]*manifest.Manifest{
		"acme/a@v1.7.4": makeManifest("a"),
		"acme/b@v2.0.0": makeManifest("b", makeDep("acme/a", "~1.7.0")),
	}}

	root := makeManifest("root",
		makeDep("acme/a", "^1.2.0"),
		makeDep("acme/b", "^2.0.0"),
	)
	result, err := Resolve(root, nil, tags, loader, ResolverConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var aDep *ResolvedDep
	for i := range result.Deps {
		if result.Deps[i].Key == "acme/a" {
			aDep = &result.Deps[i]
		}
	}
	if aDep == nil {
		t.Fatal("acme/a not found")
	}
	// ~1.7.0 has higher lower bound (1.7.0) than ^1.2.0 (1.2.0), so B is tighter
	if aDep.ResolvedBy != "acme/b" {
		t.Errorf("ResolvedBy = %q, want %q (parent key, not chain string)", aDep.ResolvedBy, "acme/b")
	}
	// Should resolve to v1.7.4 (highest in intersection of ^1.2.0 AND ~1.7.0)
	if aDep.ResolvedTag != "v1.7.4" {
		t.Errorf("ResolvedTag = %q, want v1.7.4", aDep.ResolvedTag)
	}
}
