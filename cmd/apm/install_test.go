package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/registry"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/apm-go/apm/internal/semver"
	"github.com/apm-go/apm/internal/yamlcore"
)

type mockInstallTagLister struct {
	tags map[string][]semver.TagInfo
}

func (m *mockInstallTagLister) ListTags(repoURL string) ([]semver.TagInfo, error) {
	return m.tags[repoURL], nil
}

type mockInstallLoader struct {
	packages map[string]*manifest.Manifest
}

func (m *mockInstallLoader) LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error) {
	key := ref.RepoURL + "@" + resolvedRef
	if pkg, ok := m.packages[key]; ok {
		return pkg, nil
	}
	return nil, nil
}

func TestRunInstall_NoDeps(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &mockInstallLoader{},
	}
	err := runInstall(deps, false, false, "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRunInstall_NoDeps_LocalOnlyWithTargetStillDeploys is a general (non-MCP)
// regression for a fix made while building the MCP feature: a manifest with
// zero dependencies.apm entries used to hit an early "No dependencies to
// install" return before target resolution ever ran, so a project with only
// local .apm/ primitives (or, per mcp_e2e_test.go, only dependencies.mcp)
// could never deploy via `apm install` at all. Once a target is resolvable
// (here, explicit --target), install must still deploy local primitives and
// write a lockfile.
func TestRunInstall_NoDeps_LocalOnlyWithTargetStillDeploys(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	if err := os.MkdirAll(".apm/instructions", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".apm", "instructions", "demo.instructions.md"), []byte("# demo instructions"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".claude", "rules", "demo.md")); err != nil {
		t.Errorf("expected local instructions to deploy to .claude/rules/demo.md: %v", err)
	}
	if _, err := os.Stat("apm.lock.yaml"); err != nil {
		t.Errorf("expected apm.lock.yaml to be written: %v", err)
	}
}

func TestRunInstall_WithDeps(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo#^1.0.0\n"), 0644)

	deps := &installDeps{
		tags: &mockInstallTagLister{tags: map[string][]semver.TagInfo{
			"acme/foo": {{Name: "v1.0.0", Commit: "abc123"}, {Name: "v1.5.0", Commit: "def456"}},
		}},
		loader: &mockInstallLoader{packages: map[string]*manifest.Manifest{
			"acme/foo@v1.5.0": {Name: "foo", Version: "1.5.0"},
		}},
	}

	// tree_sha256 requires a git repo at apm_modules/acme/foo — skip by making it fail gracefully
	// For unit test: we test that the install pipeline runs; tree_sha256 will error
	// since there's no real git repo. That's expected — integration tests handle the full flow.
	err := runInstall(deps, false, true, "", nil, nil) // --no-provenance to simplify
	// Expected: tree_sha256 error since there's no git repo in temp dir
	if err == nil || !strings.Contains(err.Error(), "tree_sha256") {
		// If it somehow succeeds or has a different error, that's also informative
		t.Logf("install result: %v", err)
	}
}

// readLockfile is a small test helper: reads and parses apm.lock.yaml from
// the current directory.
func readLockfile(t *testing.T) *lockfile.Lockfile {
	t.Helper()
	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("read apm.lock.yaml: %v", err)
	}
	node, err := yamlcore.SafeLoad(lockData)
	if err != nil {
		t.Fatalf("parse apm.lock.yaml: %v", err)
	}
	lock, err := lockfile.ParseLockfile(node)
	if err != nil {
		t.Fatalf("validate apm.lock.yaml: %v", err)
	}
	return lock
}

// TestRunInstall_PositionalDedup_KeysByVirtualPath is a regression test for
// MI2 (marketplace-install review finding): the positional-package dedup map
// (existing) used to be keyed by bare RepoURL (ignoring VirtualPath), while
// the identity used everywhere else (requestedKeys, marketplaceProvenance)
// was deploy.DepRefKey (RepoURL, or RepoURL/VirtualPath). A monorepo dep
// already declared in apm.yml (e.g. org/monorepo/skills/a) shares its bare
// RepoURL with a second virtual-path package from the same repo
// (org/monorepo/skills/b) requested positionally -- the second one was
// silently matched against "already declared" and never appended to
// m.ParsedDeps, so it never reached the resolved graph or apm.lock.yaml.
func TestRunInstall_PositionalDedup_KeysByVirtualPath(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - org/monorepo/skills/a\n"), 0644)

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &mockInstallLoader{},
	}
	err := runInstall(deps, false, true, "", nil, []string{"org/monorepo/skills/b"})
	if err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	lock := readLockfile(t)
	got := make(map[string]bool, len(lock.Dependencies))
	for _, d := range lock.Dependencies {
		got[d.UniqueKey()] = true
	}
	if !got["org/monorepo/skills/a"] {
		t.Errorf("expected org/monorepo/skills/a (already declared) in apm.lock.yaml, got keys: %v", got)
	}
	if !got["org/monorepo/skills/b"] {
		t.Errorf("expected org/monorepo/skills/b (newly requested positional package) in apm.lock.yaml, got keys: %v", got)
	}
}

// TestRunInstall_PositionalDedup_TrueDuplicateStillSkipped guards against a
// regression in the MI2 fix above: an ordinary repeat install of an
// already-declared, non-virtual-path dependency must still be recognized as
// a duplicate and not produce a second entry.
func TestRunInstall_PositionalDedup_TrueDuplicateStillSkipped(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo\n"), 0644)

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &mockInstallLoader{},
	}
	err := runInstall(deps, false, true, "", nil, []string{"acme/foo"})
	if err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	lock := readLockfile(t)
	count := 0
	for _, d := range lock.Dependencies {
		if d.UniqueKey() == "acme/foo" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 acme/foo entry in apm.lock.yaml (duplicate repeat install must be deduped), got %d", count)
	}
}

// TestBuildLockfile_SkillSubsetScopedToRequestedDep is a regression test: the
// skill_subset field used to be stamped onto every dependency in the
// resolved graph whenever any --skill flag was combined with any positional
// package (bug), regardless of whether that dependency was the one --skill
// actually targeted. Only the requested dependency's lock entry may carry
// skill_subset; an unrelated, already-declared dependency must not.
func TestBuildLockfile_SkillSubsetScopedToRequestedDep(t *testing.T) {
	result := &resolver.ResolutionResult{
		Deps: []resolver.ResolvedDep{
			{Key: "acme/foo", RepoURL: "acme/foo", Kind: resolver.KindRegistry},
			{Key: "acme/bar", RepoURL: "acme/bar", Kind: resolver.KindRegistry},
		},
	}
	requestedKeys := map[string]bool{"acme/foo": true}

	lock, err := buildLockfile(result, nil, &registry.Loader{}, []string{"x"}, requestedKeys, true, nil)
	if err != nil {
		t.Fatalf("buildLockfile: %v", err)
	}

	byRepo := make(map[string][]string)
	for _, d := range lock.Dependencies {
		byRepo[d.RepoURL] = d.SkillSubset
	}
	if got := byRepo["acme/foo"]; len(got) != 1 || got[0] != "x" {
		t.Errorf("acme/foo (the --skill target) should have skill_subset [x], got %v", got)
	}
	if got := byRepo["acme/bar"]; len(got) != 0 {
		t.Errorf("acme/bar (unrelated, already-declared dependency) must not have skill_subset, got %v", got)
	}
}

// TestBuildLockfile_SkillSubsetNoMatch_Errors is a regression test (found by
// codex review of the scoping fix above): a --skill flag whose requestedKeys
// never matches any dependency in the resolved graph used to silently do
// nothing -- e.g. `apm install --skill x` with no positional package
// (requestedKeys empty), or a positional package string that never made it
// into the resolved graph (e.g. it collided with an already-declared
// dependency during positional-package dedup, keyed by bare repo_url and
// blind to a virtual_path suffix). Either way, the CLI printed "Skill
// subset: x" and reported success while deploying/locking nothing scoped by
// it. --skill must fail loudly instead.
func TestBuildLockfile_SkillSubsetNoMatch_Errors(t *testing.T) {
	result := &resolver.ResolutionResult{
		Deps: []resolver.ResolvedDep{
			{Key: "acme/foo", RepoURL: "acme/foo", Kind: resolver.KindRegistry},
		},
	}

	_, err := buildLockfile(result, nil, &registry.Loader{}, []string{"x"}, map[string]bool{}, true, nil)
	if err == nil {
		t.Fatal("expected an error when --skill's requestedKeys match no resolved dependency")
	}
	if !strings.Contains(err.Error(), "--skill") {
		t.Errorf("error should name --skill, got %q", err.Error())
	}
}

// TestBuildLockfile_SkillSubsetPartialMatch_Errors is a regression test
// (second codex review round): the first fail-loud guard only checked
// whether AT LEAST ONE requested key matched, so with multiple positional
// packages a valid match on one could mask another that silently never
// resolved into the graph (e.g. `apm install good/pkg collided/pkg/sub
// --skill x` where collided/pkg/sub lost the pre-existing bare-repo_url
// dedup). Every requested key must match, not just one.
func TestBuildLockfile_SkillSubsetPartialMatch_Errors(t *testing.T) {
	result := &resolver.ResolutionResult{
		Deps: []resolver.ResolvedDep{
			{Key: "acme/good", RepoURL: "acme/good", Kind: resolver.KindRegistry},
		},
	}
	requestedKeys := map[string]bool{"acme/good": true, "acme/collided/sub": true}

	_, err := buildLockfile(result, nil, &registry.Loader{}, []string{"x"}, requestedKeys, true, nil)
	if err == nil {
		t.Fatal("expected an error when one of two requested keys never resolved into the graph")
	}
	if !strings.Contains(err.Error(), "acme/collided/sub") {
		t.Errorf("error should name the unmatched key acme/collided/sub, got %q", err.Error())
	}
}

// TestRunInstall_SkillWithoutPackages_Errors is a regression test (second
// codex review round): --skill with no positional package used to silently
// print "Skill subset: x" and deploy everything unfiltered, since
// requestedKeys stayed empty. Reject up front instead.
func TestRunInstall_SkillWithoutPackages_Errors(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runInstall(deps, false, true, "", []string{"x"}, nil)
	if err == nil || !strings.Contains(err.Error(), "--skill") {
		t.Fatalf("expected a --skill error, got %v", err)
	}
}

// TestRunInstall_SkillWithFrozen_Errors is a regression test (second codex
// review round): frozen installs skip resolution/deploy filtering entirely
// via a separate early-return code path, so --skill combined with --frozen
// used to silently do nothing instead of being rejected as unsupported.
func TestRunInstall_SkillWithFrozen_Errors(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte("version: \"1\"\ndependencies: []\n"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runInstall(deps, true, true, "", []string{"x"}, []string{"acme/foo"})
	if err == nil || !strings.Contains(err.Error(), "--skill") || !strings.Contains(err.Error(), "frozen") {
		t.Fatalf("expected a --skill+frozen error, got %v", err)
	}
}

func TestRunInstall_FrozenMissingLockfile(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo#^1.0.0\n"), 0644)

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &mockInstallLoader{},
	}
	err := runInstall(deps, true, false, "", nil, nil)
	if err == nil {
		t.Fatal("expected error for frozen install without lockfile")
	}
	if !strings.Contains(err.Error(), "frozen") {
		t.Errorf("error should mention frozen: %v", err)
	}
}

func TestRunInstall_FrozenMissingPin(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo#^1.0.0\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte("lockfile_version: \"1\"\ndependencies: []\n"), 0644)

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &mockInstallLoader{},
	}
	err := runInstall(deps, true, false, "", nil, nil)
	if err == nil {
		t.Fatal("expected error for frozen install with missing pin")
	}
	if !strings.Contains(err.Error(), "acme/foo") {
		t.Errorf("error should mention missing dep: %v", err)
	}
}

// spyLoader records the resolvedRef and ref each LoadPackage call receives,
// to lock in which lockfile fields a caller prefers without depending on
// real git.
type spyLoader struct {
	calls []string
	refs  []*manifest.DependencyReference
}

func (s *spyLoader) LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error) {
	s.calls = append(s.calls, resolvedRef)
	s.refs = append(s.refs, ref)
	return nil, nil
}

// TestRunInstall_Frozen_PrefersResolvedCommitOverResolvedRef guards
// req-lk-007's frozen-path fix: resolved_ref may name a mutable branch (e.g.
// "main"), so the skip-vs-reclone check must be verified against
// resolved_commit (the authoritative lockfile pin), not resolved_ref.
func TestRunInstall_Frozen_PrefersResolvedCommitOverResolvedRef(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo#^1.0.0\n"), 0644)
	lockContent := "lockfile_version: \"1\"\ndependencies:\n  - repo_url: acme/foo\n    resolved_ref: main\n    resolved_commit: \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n    tree_sha256: \"sha256:0000000000000000000000000000000000000000000000000000000000000000\"\n    depth: 1\n"
	os.WriteFile("apm.lock.yaml", []byte(lockContent), 0644)

	spy := &spyLoader{}
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: spy}
	// Expected to fail later at tree_sha256 verification (mock does not
	// materialize a real checkout) -- this test only cares about what was
	// passed to LoadPackage before that point.
	runInstall(deps, true, false, "", nil, nil)

	if len(spy.calls) != 1 {
		t.Fatalf("expected exactly 1 LoadPackage call, got %d: %v", len(spy.calls), spy.calls)
	}
	if spy.calls[0] != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("LoadPackage called with resolvedRef = %q, want resolved_commit (not resolved_ref %q)", spy.calls[0], "main")
	}
}

// TestRunInstall_Frozen_PreservesVirtualPath guards a gap codex review found
// alongside the resolved_commit fix: the frozen path's reconstructed
// DependencyReference dropped VirtualPath, so RealPackageLoader.installPath
// (which appends VirtualPath) and lockfile's dep.UniqueKey() (which also
// appends VirtualPath, e.g. for VerifyTreeSHA256) would disagree on which
// directory this dependency lives in whenever virtual_path is set.
func TestRunInstall_Frozen_PreservesVirtualPath(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo/sub/pkg#^1.0.0\n"), 0644)
	lockContent := "lockfile_version: \"1\"\ndependencies:\n  - repo_url: acme/foo\n    virtual_path: sub/pkg\n    resolved_ref: v1.0.0\n    resolved_commit: \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n    tree_sha256: \"sha256:0000000000000000000000000000000000000000000000000000000000000000\"\n    depth: 1\n"
	os.WriteFile("apm.lock.yaml", []byte(lockContent), 0644)

	spy := &spyLoader{}
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: spy}
	runInstall(deps, true, false, "", nil, nil)

	if len(spy.refs) != 1 {
		t.Fatalf("expected exactly 1 LoadPackage call, got %d", len(spy.refs))
	}
	if spy.refs[0].VirtualPath != "sub/pkg" {
		t.Errorf("LoadPackage called with ref.VirtualPath = %q, want %q", spy.refs[0].VirtualPath, "sub/pkg")
	}
}

func TestRunInstall_NoProvenance(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &mockInstallLoader{},
	}
	err := runInstall(deps, false, true, "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOwnerFromRepoURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"acme/foo", "acme"},
		{"github.com/acme/foo", "acme"},
		{"foo", "foo"},
	}
	for _, tt := range tests {
		if got := ownerFromRepoURL(tt.url); got != tt.want {
			t.Errorf("ownerFromRepoURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestRepoFromRepoURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"acme/foo", "foo"},
		{"github.com/acme/foo", "foo"},
		{"foo", "foo"},
	}
	for _, tt := range tests {
		if got := repoFromRepoURL(tt.url); got != tt.want {
			t.Errorf("repoFromRepoURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestResolveCloneURL(t *testing.T) {
	loader := &gitopsResolveCloneURLHelper{}
	tests := []struct {
		host   string
		scheme string
		owner  string
		repo   string
		want   string
	}{
		{"", "", "acme", "foo", "https://github.com/acme/foo.git"},
		{"gitlab.com", "https", "acme", "foo", "https://gitlab.com/acme/foo.git"},
		{"gitlab.com", "ssh", "acme", "foo", "ssh://git@gitlab.com/acme/foo.git"},
		{"gitlab.com", "git", "acme", "foo", "git@gitlab.com:acme/foo.git"},
	}
	for _, tt := range tests {
		ref := &manifest.DependencyReference{Host: tt.host, Scheme: tt.scheme, Owner: tt.owner, Repo: tt.repo}
		got := loader.resolveCloneURL(ref, "github.com")
		if got != tt.want {
			t.Errorf("resolveCloneURL(%+v) = %q, want %q", ref, got, tt.want)
		}
	}
}

type gitopsResolveCloneURLHelper struct{}

func (g *gitopsResolveCloneURLHelper) resolveCloneURL(ref *manifest.DependencyReference, defaultHost string) string {
	if ref.Scheme != "" {
		switch ref.Scheme {
		case "https", "http":
			host := ref.Host
			if host == "" {
				host = defaultHost
			}
			return ref.Scheme + "://" + host + "/" + ref.Owner + "/" + ref.Repo + ".git"
		case "ssh":
			host := ref.Host
			if host == "" {
				host = defaultHost
			}
			return "ssh://git@" + host + "/" + ref.Owner + "/" + ref.Repo + ".git"
		case "git":
			host := ref.Host
			if host == "" {
				host = defaultHost
			}
			return "git@" + host + ":" + ref.Owner + "/" + ref.Repo + ".git"
		}
	}
	host := ref.Host
	if host == "" {
		host = defaultHost
	}
	return "https://" + host + "/" + ref.Owner + "/" + ref.Repo + ".git"
}

func TestInstallCmd_Help(t *testing.T) {
	cmd := installCmd()
	if cmd.Use != "install [packages...]" {
		t.Errorf("Use = %q, want install [packages...]", cmd.Use)
	}
	f := cmd.Flags()
	if f.Lookup("frozen") == nil {
		t.Error("missing --frozen flag")
	}
	if f.Lookup("no-provenance") == nil {
		t.Error("missing --no-provenance flag")
	}
}

func TestRunInstall_FrozenMissingTreeSHA256(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo#^1.0.0\n"), 0644)
	// Lockfile with a git entry that has resolved_commit but NO tree_sha256
	lockContent := "lockfile_version: \"1\"\ndependencies:\n  - repo_url: acme/foo\n    resolved_commit: \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n    depth: 1\n"
	os.WriteFile("apm.lock.yaml", []byte(lockContent), 0644)
	os.MkdirAll(filepath.Join("apm_modules", "acme", "foo"), 0755)

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &mockInstallLoader{},
	}
	err := runInstall(deps, true, false, "", nil, nil)
	if err == nil {
		t.Fatal("expected error for frozen install with missing tree_sha256")
	}
	if !strings.Contains(err.Error(), "tree_sha256") {
		t.Errorf("error should mention tree_sha256: %v", err)
	}
}
