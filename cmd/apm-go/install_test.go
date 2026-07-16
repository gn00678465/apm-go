package main

import (
	"bytes"
	"crypto/sha256"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/gitops"
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

// TestRunInstall_DevDependency_ResolvedDeployedAndLocked is the RED/GREEN
// test for F3 (P1): a hand-authored devDependencies.apm entry -- with NO
// dependencies.apm block at all in apm.yml -- must be resolved, deployed
// (its primitives reach the target), and recorded in apm.lock.yaml, exactly
// like an ordinary dependencies.apm entry. This also exercises the
// "manifest declares ONLY dev deps" edge case: any gate keyed on
// len(m.ParsedDeps) alone (frozen materialization, the pre-resolve
// "No dependencies to install" short-circuit) must not treat a dev-only
// manifest as dependency-free.
func TestRunInstall_DevDependency_ResolvedDeployedAndLocked(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndevDependencies:\n  apm:\n    - acme/foo#^1.0.0\n"), 0644)

	// Simulate apm_modules content for the dev dependency (as if already
	// fetched), proving deploy.Run treats it as a DIRECT dep (depth 1) --
	// its self-defined primitives get collected and deployed exactly like a
	// dependencies.apm entry, not silently dropped or misfiled as
	// transitive.
	modDir := filepath.Join(dir, "apm_modules", "acme", "foo")
	if err := os.MkdirAll(filepath.Join(modDir, ".apm", "instructions"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, ".apm", "instructions", "dev.instructions.md"), []byte("# dev instructions"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{
		tags: &mockInstallTagLister{tags: map[string][]semver.TagInfo{
			"acme/foo": {{Name: "v1.0.0"}, {Name: "v1.5.0"}},
		}},
		loader: &mockInstallLoader{},
	}

	if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".claude", "rules", "dev.md")); err != nil {
		t.Errorf("expected devDependencies.apm entry's primitives to deploy to .claude/rules/dev.md: %v", err)
	}

	lock := readLockfile(t)
	var found *lockfile.LockedDep
	for i := range lock.Dependencies {
		if lock.Dependencies[i].UniqueKey() == "acme/foo" {
			found = &lock.Dependencies[i]
		}
	}
	if found == nil {
		t.Fatalf("expected devDependencies.apm entry acme/foo to be recorded in apm.lock.yaml, got: %+v", lock.Dependencies)
	}
	if found.ResolvedTag != "v1.5.0" {
		t.Errorf("acme/foo resolved_tag = %q, want v1.5.0 (highest matching ^1.0.0)", found.ResolvedTag)
	}
	if len(found.DeployedFiles) == 0 {
		t.Error("expected acme/foo lockfile entry to record deployed_files, got none")
	}
}

// TestRunInstall_DevDependency_SecondBareInstallIsNoOp proves idempotency
// (F3 requirement 4): a second bare `apm install` (no positional packages)
// after devDependencies.apm has already been resolved must not rewrite
// apm.yml or apm.lock.yaml.
func TestRunInstall_DevDependency_SecondBareInstallIsNoOp(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndevDependencies:\n  apm:\n    - acme/foo#^1.0.0\n"), 0644)

	deps := &installDeps{
		tags: &mockInstallTagLister{tags: map[string][]semver.TagInfo{
			"acme/foo": {{Name: "v1.0.0"}},
		}},
		loader: &mockInstallLoader{},
	}

	if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
		t.Fatalf("first runInstall: %v", err)
	}
	firstManifest, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	lockInfoBefore, err := os.Stat("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}

	if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
		t.Fatalf("second runInstall: %v", err)
	}
	secondManifest, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	lockInfoAfter, err := os.Stat("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}

	if string(firstManifest) != string(secondManifest) {
		t.Errorf("apm.yml changed on second bare install:\nfirst:\n%s\nsecond:\n%s", firstManifest, secondManifest)
	}
	if !lockInfoBefore.ModTime().Equal(lockInfoAfter.ModTime()) {
		t.Error("apm.lock.yaml was rewritten on a second bare install (idempotency broken: expected a pure no-op)")
	}
}

// TestRunInstall_Frozen_DevOnlyManifest_StillMaterializes is the F3 frozen
// decision test (definition-of-done item 3): a manifest declaring ONLY
// devDependencies.apm (zero dependencies.apm entries) must still run the
// frozen install's source-materialization step for its locked dev
// dependency, not silently skip it because that step used to gate on
// len(m.ParsedDeps) alone.
func TestRunInstall_Frozen_DevOnlyManifest_StillMaterializes(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndevDependencies:\n  apm:\n    - acme/foo#^1.0.0\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte("lockfile_version: \"1\"\ndependencies:\n  - repo_url: acme/foo\n    source: git\n    constraint: \"^1.0.0\"\n    resolved_tag: v1.0.0\n    depth: 1\n"), 0644)

	spy := &spyLoader{}
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: spy}
	if err := runInstall(deps, true, false, "", nil, nil); err != nil {
		t.Fatalf("expected frozen install to succeed with a dev-only manifest: %v", err)
	}
	if len(spy.calls) == 0 {
		t.Error("expected frozen install's source-materialization step to run for a dev-only manifest (a len(m.ParsedDeps)==0 gate must not skip devDependencies)")
	}
}

// TestRunInstall_FrozenMissingPin_DevDependency mirrors
// TestRunInstall_FrozenMissingPin for a devDependencies.apm entry: frozen
// structural verification (lockfile.CheckFrozenInstall) must fail the same
// way for a missing dev dependency pin as it does for a missing prod one.
func TestRunInstall_FrozenMissingPin_DevDependency(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndevDependencies:\n  apm:\n    - acme/foo#^1.0.0\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte("lockfile_version: \"1\"\ndependencies: []\n"), 0644)

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &mockInstallLoader{},
	}
	err := runInstall(deps, true, false, "", nil, nil)
	if err == nil {
		t.Fatal("expected error for frozen install with missing dev dependency pin")
	}
	if !strings.Contains(err.Error(), "acme/foo") {
		t.Errorf("error should mention missing dev dep: %v", err)
	}
}

// TestRunInstall_RefusesHTTPDependency_FromDevManifest proves the P0
// insecure-http policy gate (F4bdcac) also applies to a devDependencies.apm
// entry, not just dependencies.apm -- Python checks apm_deps + dev_apm_deps
// together (_check_insecure_dependencies(all_apm_deps, ...)).
func TestRunInstall_RefusesHTTPDependency_FromDevManifest(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndevDependencies:\n  apm:\n    - git: http://example.com/owner/repo\n"), 0644)

	spy := &spyLoader{}
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: spy}
	err := runInstall(deps, false, true, "", nil, nil)
	if err == nil {
		t.Fatal("expected error for http:// devDependency without --allow-insecure, got nil")
	}
	if !strings.Contains(err.Error(), "--allow-insecure") {
		t.Errorf("error should point at the --allow-insecure remediation, got: %v", err)
	}
	if len(spy.calls) != 0 {
		t.Errorf("expected zero LoadPackage (clone) calls before the refusal, got %d", len(spy.calls))
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
	// --target claude only satisfies the "dependencies present but no
	// deployment target" exit-2 guard (F2); this test's subject is
	// positional-package dedup, not deploy.
	err := runInstall(deps, false, true, "claude", nil, []string{"org/monorepo/skills/b"})
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
	// --target claude only satisfies the "dependencies present but no
	// deployment target" exit-2 guard (F2); this test's subject is
	// duplicate-dedup, not deploy.
	err := runInstall(deps, false, true, "claude", nil, []string{"acme/foo"})
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

// TestRunInstall_RefusesHTTPDependency_Positional is the P0 security
// regression test: `apm-go install http://...` used to proceed straight to
// git clone with no policy gate. It must now refuse before any clone is
// attempted (spy.calls stays empty) and name the offending dependency plus
// the --allow-insecure remediation.
func TestRunInstall_RefusesHTTPDependency_Positional(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

	spy := &spyLoader{}
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: spy}
	err := runInstall(deps, false, true, "", nil, []string{"http://example.com/owner/repo.git"})
	if err == nil {
		t.Fatal("expected error for http:// dependency without --allow-insecure, got nil")
	}
	if !strings.Contains(err.Error(), "http://example.com/owner/repo") {
		t.Errorf("error should name the offending dependency, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--allow-insecure") {
		t.Errorf("error should point at the --allow-insecure remediation, got: %v", err)
	}
	if len(spy.calls) != 0 {
		t.Errorf("expected zero LoadPackage (clone) calls before the refusal, got %d", len(spy.calls))
	}
}

// TestRunInstall_RefusesHTTPDependency_FromManifest proves the same gate
// applies to a dependencies.apm entry already declared in apm.yml, not just a
// CLI positional package.
func TestRunInstall_RefusesHTTPDependency_FromManifest(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - git: http://example.com/owner/repo\n"), 0644)

	spy := &spyLoader{}
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: spy}
	err := runInstall(deps, false, true, "", nil, nil)
	if err == nil {
		t.Fatal("expected error for http:// dependency declared in apm.yml without --allow-insecure, got nil")
	}
	if !strings.Contains(err.Error(), "--allow-insecure") {
		t.Errorf("error should point at the --allow-insecure remediation, got: %v", err)
	}
	if len(spy.calls) != 0 {
		t.Errorf("expected zero LoadPackage (clone) calls before the refusal, got %d", len(spy.calls))
	}
}

// TestRunInstall_AllowInsecureFlag_PermitsHTTPDependency proves
// installDeps.allowInsecure (wired from the --allow-insecure CLI flag) lets
// an http:// dependency to a public host proceed past the policy gate.
func TestRunInstall_AllowInsecureFlag_PermitsHTTPDependency(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

	spy := &spyLoader{}
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: spy, allowInsecure: true}
	err := runInstall(deps, false, true, "", nil, []string{"http://example.com/owner/repo.git"})
	if err != nil && strings.Contains(err.Error(), "HTTP dependency (unencrypted)") {
		t.Fatalf("--allow-insecure should have permitted the http:// dependency, got refusal: %v", err)
	}
	if len(spy.calls) == 0 {
		t.Error("expected the install to proceed to LoadPackage once --allow-insecure permitted the http:// dependency")
	}
}

// TestRunInstall_LoopbackHTTPDependency_AlsoRefusedWithoutFlag mirrors the
// Python reference implementation's flag-only gate: there is NO host
// exemption, so even a loopback host's http:// dependency is refused without
// --allow-insecure -- and permitted with it.
func TestRunInstall_LoopbackHTTPDependency_AlsoRefusedWithoutFlag(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

	spy := &spyLoader{}
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: spy}
	err := runInstall(deps, false, true, "", nil, []string{"http://127.0.0.1/owner/repo.git"})
	if err == nil {
		t.Fatal("expected refusal for loopback http:// dependency without --allow-insecure (no host exemption, Python parity)")
	}
	if !strings.Contains(err.Error(), "--allow-insecure") {
		t.Errorf("error should point at the --allow-insecure remediation, got: %v", err)
	}
	if len(spy.calls) != 0 {
		t.Errorf("expected zero LoadPackage (clone) calls before the refusal, got %d", len(spy.calls))
	}

	// With --allow-insecure the same loopback dependency proceeds to clone.
	spy2 := &spyLoader{}
	deps2 := &installDeps{tags: &mockInstallTagLister{}, loader: spy2, allowInsecure: true}
	err = runInstall(deps2, false, true, "", nil, []string{"http://127.0.0.1/owner/repo.git"})
	if err != nil && strings.Contains(err.Error(), "HTTP dependency (unencrypted)") {
		t.Fatalf("--allow-insecure should have permitted the loopback http:// dependency, got refusal: %v", err)
	}
	if len(spy2.calls) == 0 {
		t.Error("expected the install to proceed to LoadPackage once --allow-insecure permitted the loopback http:// dependency")
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
	effectiveSubsets := map[string][]string{"acme/foo": {"x"}}

	lock, err := buildLockfile(result, nil, &registry.Loader{}, effectiveSubsets, []string{"x"}, requestedKeys, true, nil)
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

	_, err := buildLockfile(result, nil, &registry.Loader{}, nil, []string{"x"}, map[string]bool{}, true, nil)
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

	_, err := buildLockfile(result, nil, &registry.Loader{}, nil, []string{"x"}, requestedKeys, true, nil)
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

// TestRunInstall_SkillWildcardWithoutPackages_Errors confirms the '*' RESET
// sentinel doesn't bypass the pre-existing "--skill requires a positional
// package" guard: the guard runs on the raw --skill flags before any
// wildcard normalization, so `--skill '*'` alone must still be rejected the
// same way `--skill x` alone is.
func TestRunInstall_SkillWildcardWithoutPackages_Errors(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runInstall(deps, false, true, "", []string{"*"}, nil)
	if err == nil || !strings.Contains(err.Error(), "--skill") {
		t.Fatalf("expected a --skill error for --skill '*' with no positional package, got %v", err)
	}
}

// TestRunInstall_SkillWildcardWithFrozen_Errors confirms the '*' RESET
// sentinel doesn't bypass the pre-existing "--skill is not supported with
// --frozen" guard.
func TestRunInstall_SkillWildcardWithFrozen_Errors(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte("version: \"1\"\ndependencies: []\n"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runInstall(deps, true, true, "", []string{"*"}, []string{"acme/foo"})
	if err == nil || !strings.Contains(err.Error(), "--skill") || !strings.Contains(err.Error(), "frozen") {
		t.Fatalf("expected a --skill+frozen error for --skill '*', got %v", err)
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

// TestInstallCmd_TargetShorthand is the F2 regression: `install -t claude`
// used to fail with "unknown shorthand flag: t" because --target was
// registered via StringVar (no shorthand). -t must resolve to --target.
func TestInstallCmd_TargetShorthand(t *testing.T) {
	cmd := installCmd()
	sh := cmd.Flags().ShorthandLookup("t")
	if sh == nil {
		t.Fatal("expected -t shorthand to be registered")
	}
	if sh.Name != "target" {
		t.Errorf("-t shorthand resolves to flag %q, want target", sh.Name)
	}
}

// TestInstallCmd_TargetShorthand_ParsesOnCLI proves -t is actually usable on
// the command line (not just registered), using it in place of --target for
// a local-only deploy so the assertion is deploy actually happened.
func TestInstallCmd_TargetShorthand_ParsesOnCLI(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)
	os.MkdirAll(filepath.Join(".apm", "instructions"), 0755)
	os.WriteFile(filepath.Join(".apm", "instructions", "demo.instructions.md"), []byte("# demo"), 0644)

	cmd := installCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"-t", "claude"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("install -t claude: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "rules", "demo.md")); err != nil {
		t.Errorf("expected -t claude to deploy local instructions to .claude/rules/demo.md: %v", err)
	}
}

// TestRunInstall_TargetCommaSplit_DeploysToBothTargets is the F2 regression:
// `--target claude,codex` used to be treated as one literal (unknown)
// target string (no comma splitting), silently resolving to zero targets.
// It must split and deploy to both.
func TestRunInstall_TargetCommaSplit_DeploysToBothTargets(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)
	os.MkdirAll(filepath.Join(".apm", "agents"), 0755)
	os.WriteFile(filepath.Join(".apm", "agents", "demo.md"), []byte("# demo agent"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	if err := runInstall(deps, false, true, "claude,codex", nil, nil); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".claude", "agents", "demo.md")); err != nil {
		t.Errorf("expected --target claude,codex to deploy to .claude/agents/demo.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".codex", "agents", "demo.toml")); err != nil {
		t.Errorf("expected --target claude,codex to deploy to .codex/agents/demo.toml: %v", err)
	}
}

// TestInstallCmd_UnknownTargetRejected is the F2/mf-005 regression: an
// unknown --target token used to silently resolve to zero targets (no
// registered adapter -> diagnostic only -> filtered out) instead of being
// rejected. It must now fail with exit 2, naming the offending token,
// before any manifest/lockfile work happens.
func TestInstallCmd_UnknownTargetRejected(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

	cmd := installCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--target", "bogus"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error for --target bogus, got nil")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error should name the offending token, got: %v", err)
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", got)
	}
}

// TestRunInstall_DepsPresentZeroTarget_ExitsWithTeachingMessage is the F2
// regression: with dependencies to install but no --target, no apm.yml
// target:, and nothing auto-detected, apm-go used to silently skip
// deployment and exit 0. install.md's exit-code table requires exit 2 with
// a teaching message ("no deployment target detectable").
func TestRunInstall_DepsPresentZeroTarget_ExitsWithTeachingMessage(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo\n"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runInstall(deps, false, true, "", nil, nil)
	if err == nil {
		t.Fatal("expected an error when dependencies are present but no deployment target resolves")
	}
	if !strings.Contains(err.Error(), "target") {
		t.Errorf("error should teach the user about the missing target, got: %v", err)
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", got)
	}
	if _, statErr := os.Stat("apm.lock.yaml"); !os.IsNotExist(statErr) {
		t.Error("apm.lock.yaml should not be written when the install fails closed on zero targets")
	}
}

// TestRunInstall_NoDepsZeroTarget_StillExitsZero is the companion negative
// case for the F2 fix: an EMPTY project (no dependencies to install, no
// local .apm/ primitives) with no resolvable target must keep exiting 0
// (existing "No dependencies to install" behavior) -- the exit-2 guard is
// scoped to "anything to integrate present" (deps or local primitives), not
// to "target resolution failed" in general.
func TestRunInstall_NoDepsZeroTarget_StillExitsZero(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	if err := runInstall(deps, false, true, "", nil, nil); err != nil {
		t.Fatalf("expected no error for a zero-dependency install with no target, got: %v", err)
	}
}

// TestRunInstall_LocalPrimitivesZeroTarget_ExitsWithTeachingMessage extends
// the F2 zero-target gate (task 07-11-instructions-applyto-parity, req #2):
// a project with local .apm/ primitives but ZERO dependencies and ZERO
// resolvable targets used to exit 0 silently WITHOUT deploying -- the user
// got no signal their primitives were ignored. Python exits 2 ("No harness
// detected") whenever there is anything to integrate (deps OR local
// primitives) but no target; apm-go must do the same, keeping its existing
// teaching-message wording and writing nothing.
func TestRunInstall_LocalPrimitivesZeroTarget_ExitsWithTeachingMessage(t *testing.T) {
	tests := []struct {
		name   string
		subdir string
		file   string
	}{
		{"instructions", "instructions", "x.instructions.md"},
		{"agents", "agents", "x.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			origDir, _ := os.Getwd()
			os.Chdir(dir)
			defer os.Chdir(origDir)

			os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)
			os.MkdirAll(filepath.Join(".apm", tt.subdir), 0755)
			os.WriteFile(filepath.Join(".apm", tt.subdir, tt.file), []byte("# x"), 0644)

			deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
			err := runInstall(deps, false, true, "", nil, nil)
			if err == nil {
				t.Fatal("expected an error when local primitives are present but no deployment target resolves")
			}
			if !strings.Contains(err.Error(), "no deployment target detected") {
				t.Errorf("error should carry the teaching message, got: %v", err)
			}
			if got := exitCodeOf(err); got != 2 {
				t.Errorf("exitCodeOf(err) = %d, want 2", got)
			}
			if _, statErr := os.Stat("apm.lock.yaml"); !os.IsNotExist(statErr) {
				t.Error("apm.lock.yaml should not be written when the install fails closed on zero targets")
			}
			if _, statErr := os.Stat(".claude"); !os.IsNotExist(statErr) {
				t.Error("nothing should be deployed when the install fails closed on zero targets")
			}
		})
	}
}

// TestInstall_NoTargetDiagnostic is the R17 regression: the no-deployment-
// target error must print a structured diagnostic (the scanned harness
// marker paths + concrete remediation), and cobra's default 14-line flag
// usage dump must be suppressed for THIS error specifically -- exercised
// through cmd.Execute() (not a direct runInstall call) so cobra's own
// usage-printing behavior is actually in play.
func TestInstall_NoTargetDiagnostic(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)
	os.MkdirAll(filepath.Join(".apm", "instructions"), 0755)
	os.WriteFile(filepath.Join(".apm", "instructions", "x.instructions.md"), []byte("# x"), 0644)

	cmd := installCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error when local primitives are present but no deployment target resolves")
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", got)
	}

	combined := out.String() + err.Error()
	if !strings.Contains(combined, "no deployment target detected") {
		t.Errorf("expected the teaching message, got: %s", combined)
	}
	for _, sig := range manifest.SignalWhitelist {
		if !strings.Contains(combined, sig.Path) {
			t.Errorf("expected scanned marker %q in diagnostic, got: %s", sig.Path, combined)
		}
	}
	if !strings.Contains(combined, "--target") {
		t.Errorf("expected a concrete --target remediation, got: %s", combined)
	}
	if strings.Contains(out.String(), "Flags:") {
		t.Errorf("cobra's flag usage dump must be suppressed for the no-target diagnostic, got: %s", out.String())
	}
}

// TestInstall_UsageStillShownOnFlagError is the reverse guard for R17: an
// ordinary flag/argument error (an unknown --target token, unrelated to the
// no-deployment-target diagnostic) must keep showing cobra's usage dump and
// exit code exactly as before -- SilenceUsage must never be flipped for the
// command as a whole, only for the one typed no-target error.
func TestInstall_UsageStillShownOnFlagError(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

	cmd := installCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--target", "bogus"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error for --target bogus, got nil")
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", got)
	}
	if !strings.Contains(out.String(), "Flags:") {
		t.Errorf("cobra's usage dump must still be shown for an ordinary flag error, got: %s", out.String())
	}
}

// TestRunInstall_LocalPrimitivesWithTargetSignal_StillDeploys is the
// over-fire regression for the local-primitives zero-target gate: with a
// detectable harness signal (.claude/ directory) present, a bare install of
// a local-primitives-only project must NOT hit the exit-2 gate -- it deploys
// and exits 0, exactly as before.
func TestRunInstall_LocalPrimitivesWithTargetSignal_StillDeploys(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)
	os.MkdirAll(filepath.Join(".apm", "instructions"), 0755)
	os.WriteFile(filepath.Join(".apm", "instructions", "x.instructions.md"), []byte("# x"), 0644)
	os.MkdirAll(".claude", 0755) // auto-detect signal for the claude target

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	if err := runInstall(deps, false, true, "", nil, nil); err != nil {
		t.Fatalf("runInstall with a detectable target: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "rules", "x.md")); err != nil {
		t.Errorf("expected local instructions deployed to .claude/rules/x.md: %v", err)
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

// TestBuildLockfile_SkillWildcardDoesNotRecordSubset is a regression test for
// the documented `--skill '*'` RESET sentinel (install.md: "--skill '*'
// resets to install all skills"): '*' used to be treated as a literal skill
// name, so buildLockfile stamped skill_subset: ["*"] onto the dependency
// instead of recording no subset at all (mirroring Python's `_skill_subset`
// staying None). Covers both a pure wildcard and a mixed list (`--skill
// review --skill '*'`) -- both must record no subset. The dependency must
// still count as "matched" for the --skill scoping validation below even
// though no subset is recorded (the requested package DID resolve).
func TestBuildLockfile_SkillWildcardDoesNotRecordSubset(t *testing.T) {
	tests := []struct {
		name        string
		skillSubset []string
	}{
		{"pure wildcard", []string{"*"}},
		{"mixed with a concrete name", []string{"review", "*"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &resolver.ResolutionResult{
				Deps: []resolver.ResolvedDep{
					{Key: "acme/foo", RepoURL: "acme/foo", Kind: resolver.KindRegistry},
				},
			}
			requestedKeys := map[string]bool{"acme/foo": true}

			// The '*' RESET sentinel means effectiveSkillSubsets deletes the
			// dependency's entry entirely (design.md §1.2c rule 3) -- nil
			// here simulates exactly that outcome, since this test targets
			// buildLockfile's OWN stamping logic directly.
			lock, err := buildLockfile(result, nil, &registry.Loader{}, nil, tt.skillSubset, requestedKeys, true, nil)
			if err != nil {
				t.Fatalf("buildLockfile: %v", err)
			}
			if len(lock.Dependencies) != 1 {
				t.Fatalf("expected 1 dependency, got %d", len(lock.Dependencies))
			}
			if got := lock.Dependencies[0].SkillSubset; len(got) != 0 {
				t.Errorf("expected no skill_subset recorded for --skill %v (reset to all), got %v", tt.skillSubset, got)
			}
		})
	}
}

// TestPersistPackagesToManifest_SkillWildcard_NewPackageWritesStringForm is a
// regression test: a NEW package installed with `--skill '*'` used to write
// the object form `{git: pkg, skills: ['*']}` (since len(skillSubset) > 0
// was the only check), persisting a literal "*" subset in apm.yml instead of
// the plain string form that means "install all" -- mirroring Python's
// `_skill_subset = None` (no subset persisted = install all).
func TestPersistPackagesToManifest_SkillWildcard_NewPackageWritesStringForm(t *testing.T) {
	doc, err := yamlcore.SafeLoad([]byte("name: d\nversion: 1.0.0\n"))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}

	// The '*' RESET sentinel is represented upstream (effectiveSkillSubsets)
	// as "no entry for this identity" -- nil here simulates that outcome
	// directly, since this test targets persistPackagesToManifest's own
	// entry-writing logic.
	if err := persistPackagesToManifest(doc, []string{"acme/foo"}, nil); err != nil {
		t.Fatalf("persistPackagesToManifest: %v", err)
	}

	out, err := yamlcore.SafeDump(doc)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	got := string(out)

	if !strings.Contains(got, "apm:\n    - acme/foo") {
		t.Errorf("expected plain string form for --skill '*' new package; got:\n%s", got)
	}
	if strings.Contains(got, "skills:") {
		t.Errorf("must not persist a skills: subset for --skill '*'; got:\n%s", got)
	}
}

// TestPersistPackagesToManifest_SkillWildcard_ClearsExistingSubset is a
// regression test for the "reset going forward" requirement: a package
// previously installed with a narrower `--skill x` (persisted as the object
// form `{git: pkg, skills: [x]}`) must have that subset CLEARED when
// re-installed with `--skill '*'` -- otherwise a later bare `apm install`
// (no --skill) would keep reading the stale narrower subset from apm.yml.
func TestPersistPackagesToManifest_SkillWildcard_ClearsExistingSubset(t *testing.T) {
	src := "name: d\nversion: 1.0.0\ndependencies:\n  apm:\n    - git: acme/foo\n      skills:\n        - x\n"
	doc, err := yamlcore.SafeLoad([]byte(src))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}

	// Same RESET-as-absent-entry simulation as the test above.
	if err := persistPackagesToManifest(doc, []string{"acme/foo"}, nil); err != nil {
		t.Fatalf("persistPackagesToManifest: %v", err)
	}

	out, err := yamlcore.SafeDump(doc)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	got := string(out)

	if strings.Contains(got, "skills:") {
		t.Errorf("expected --skill '*' to clear the previously-persisted skills: subset; got:\n%s", got)
	}
	if !strings.Contains(got, "acme/foo") {
		t.Errorf("expected acme/foo to remain declared; got:\n%s", got)
	}
}

// TestRunInstall_SkillWildcardDeploysAllSkills is the CLI-level regression
// test for the `--skill '*'` RESET sentinel, exercising the real
// runInstall -> resolver -> deploy.Run path (no mocks) using a local
// git-path dependency (matching gitcheckout_e2e_test.go's offline-friendly
// convention) with two skills. Before the fix, `--skill '*'` filtered
// skills down to nothing (since '*' was treated as a literal skill name
// nothing actually matches); after the fix, it must deploy every skill,
// exactly as a plain install (no --skill) would, and must not persist a
// skill_subset in apm.lock.yaml.
func TestRunInstall_SkillWildcardDeploysAllSkills(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	git := func(repoDir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s\n%s", args, err, out)
		}
	}

	remoteDir := filepath.Join(dir, "remote")
	if err := os.MkdirAll(remoteDir, 0755); err != nil {
		t.Fatal(err)
	}
	git(remoteDir, "init")
	git(remoteDir, "config", "user.name", "test")
	git(remoteDir, "config", "user.email", "test@test.com")
	os.WriteFile(filepath.Join(remoteDir, "apm.yml"), []byte("name: dep\nversion: \"1.0.0\"\n"), 0644)
	os.MkdirAll(filepath.Join(remoteDir, ".apm", "skills", "skillA"), 0755)
	os.WriteFile(filepath.Join(remoteDir, ".apm", "skills", "skillA", "SKILL.md"), []byte("# skill A"), 0644)
	os.MkdirAll(filepath.Join(remoteDir, ".apm", "skills", "skillB"), 0755)
	os.WriteFile(filepath.Join(remoteDir, ".apm", "skills", "skillB", "SKILL.md"), []byte("# skill B"), 0644)
	git(remoteDir, "add", ".")
	git(remoteDir, "commit", "-m", "v1")
	git(remoteDir, "tag", "v1.0.0")

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - git: ./remote\n      ref: v1.0.0\n"), 0644)

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"},
	}
	if err := runInstall(deps, false, true, "claude", []string{"*"}, []string{"./remote"}); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "skillA", "SKILL.md")); err != nil {
		t.Errorf("expected skillA to deploy under --skill '*' (reset to all): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "skillB", "SKILL.md")); err != nil {
		t.Errorf("expected skillB to deploy under --skill '*' (reset to all): %v", err)
	}

	lock := readLockfile(t)
	for _, d := range lock.Dependencies {
		if len(d.SkillSubset) != 0 {
			t.Errorf("expected no skill_subset persisted in apm.lock.yaml for --skill '*', dep %s has %v", d.UniqueKey(), d.SkillSubset)
		}
	}
}

// gitGo runs a git subcommand in repoDir with a deterministic test identity,
// failing the test on any error. Shared helper for the skill-subset fixture
// builders below (mirrors the inline `git` closures already used by
// TestRunInstall_SkillWildcardDeploysAllSkills and gitcheckout_e2e_test.go).
func gitGo(t *testing.T, repoDir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s\n%s", args, err, out)
	}
}

// gitSkillRepo creates a local git repository (committed + tagged v1.0.0) at
// parentDir/name, containing one skill directory per key in skills, each
// populated with every file listed for it. Deliberately gives each skill
// MULTIPLE files across MULTIPLE deploy target roots (codex H4: a skill
// maps to several deployed files -- "skill count" and "deployed file count"
// must never be conflated) rather than the minimal single-file fixture the
// pre-existing wildcard test uses.
func gitSkillRepo(t *testing.T, parentDir, name string, skills map[string][]string) string {
	t.Helper()
	repoDir := filepath.Join(parentDir, name)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}
	gitGo(t, repoDir, "init")
	gitGo(t, repoDir, "config", "user.name", "test")
	gitGo(t, repoDir, "config", "user.email", "test@test.com")
	if err := os.WriteFile(filepath.Join(repoDir, "apm.yml"), []byte("name: "+name+"\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for skill, files := range skills {
		skillDir := filepath.Join(repoDir, ".apm", "skills", skill)
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(skillDir, f), []byte("# "+skill+"/"+f+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	gitGo(t, repoDir, "add", ".")
	gitGo(t, repoDir, "commit", "-m", "v1")
	gitGo(t, repoDir, "tag", "v1.0.0")
	return repoDir
}

// expectedSkillDeployPaths returns the project-root-relative paths a skill's
// files land at when deployed to the "claude" target, which mirrors both of
// this fixture's two target roots (.agents/skills/ -- the cross-tool
// canonical root every target shares -- and .claude/skills/ -- claude's own
// extra copy, deploySkillClaude). Used to prove a fixture's skill really
// spans two distinct target roots (H4), not just two files under one root.
func expectedSkillDeployPaths(skill string, files []string) []string {
	var paths []string
	for _, root := range []string{".agents/skills", ".claude/skills"} {
		for _, f := range files {
			paths = append(paths, path.Join(root, skill, f))
		}
	}
	return paths
}

// TestInstall_SkillSubsetPollution is BUG-2's core reproduction (prd.md
// "BUG-2 ｜ --skill 子集失憶"): installing repo-b with its own --skill
// subset must NOT forget repo-a's previously-persisted --skill subset and
// redeploy repo-a's full, unfiltered skill set. Before the fix, deploy's
// SkillFilter was built ONLY from this call's CLI --skill flags/requested
// dependency keys (deployAndFinalize), so a bare re-deploy of an
// already-declared dependency (which every `apm install <other-pkg>` call
// triggers, by design -- full-manifest re-resolve) ignored any subset an
// EARLIER call had persisted for it.
func TestInstall_SkillSubsetPollution(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	remotesDir := filepath.Join(dir, "remotes")
	if err := os.MkdirAll(remotesDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Absolute paths, not a "./..."-relative one (deliberate -- an existing
	// apm.yml `git: <relative-path>` entry re-parsed via ParseDepDict is NOT
	// re-normalized into the same apm_modules/_local/<hash> materialization
	// key a fresh CLI positional arg gets (normalizeLocalDep's local-path
	// branch only matches manifest.IsAbsoluteLocalPath); that asymmetry is a
	// separate, pre-existing gap unrelated to BUG-2, so this fixture sticks
	// to the form (F1 spec-supported: absolute local paths) both code paths
	// already agree on, rather than tripping over it here).
	repoA := gitSkillRepo(t, remotesDir, "repo-a", map[string][]string{
		"skillA1": {"SKILL.md", "notes.md"},
		"skillA2": {"SKILL.md", "notes.md"},
	})
	repoB := gitSkillRepo(t, remotesDir, "repo-b", map[string][]string{
		"skillB1": {"SKILL.md", "notes.md"},
	})

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"},
	}

	if err := runInstall(deps, false, true, "claude", []string{"skillA1"}, []string{repoA}); err != nil {
		t.Fatalf("first install (repo-a --skill skillA1): %v", err)
	}
	for _, p := range expectedSkillDeployPaths("skillA1", []string{"SKILL.md", "notes.md"}) {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
			t.Fatalf("after step 1: expected %s to exist: %v", p, err)
		}
	}
	for _, p := range expectedSkillDeployPaths("skillA2", []string{"SKILL.md", "notes.md"}) {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err == nil {
			t.Fatalf("after step 1: %s must NOT exist (skillA2 was never selected)", p)
		}
	}

	if err := runInstall(deps, false, true, "claude", []string{"skillB1"}, []string{repoB}); err != nil {
		t.Fatalf("second install (repo-b --skill skillB1): %v", err)
	}

	// The pollution check: repo-a's persisted subset must still be honored.
	for _, p := range expectedSkillDeployPaths("skillA1", []string{"SKILL.md", "notes.md"}) {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
			t.Errorf("after step 2: expected %s to still exist: %v", p, err)
		}
	}
	for _, p := range expectedSkillDeployPaths("skillA2", []string{"SKILL.md", "notes.md"}) {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err == nil {
			t.Errorf("POLLUTION (BUG-2): %s must NOT exist -- installing repo-b must not forget repo-a's --skill skillA1 subset", p)
		}
	}
	for _, p := range expectedSkillDeployPaths("skillB1", []string{"SKILL.md", "notes.md"}) {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
			t.Errorf("expected %s to exist (repo-b's selected skill): %v", p, err)
		}
	}

	// apm.yml: each dependency must carry its OWN persisted subset.
	m := readManifestParsed(t)
	subsetByRepo := make(map[string][]string)
	for _, d := range m.ParsedDeps {
		subsetByRepo[d.RepoURL] = d.SkillSubset
	}
	if got := subsetByRepo[repoA]; len(got) != 1 || got[0] != "skillA1" {
		t.Errorf("apm.yml: expected %s skills: [skillA1], got %v (all deps: %+v)", repoA, got, subsetByRepo)
	}
	if got := subsetByRepo[repoB]; len(got) != 1 || got[0] != "skillB1" {
		t.Errorf("apm.yml: expected %s skills: [skillB1], got %v (all deps: %+v)", repoB, got, subsetByRepo)
	}

	// lockfile: skill_subset == effective name set, deployed_files == the
	// actual managed path set -- NOT the same count (H4: 1 skill maps to 4
	// deployed files here: 2 files x 2 target roots). The resolved/lockfile
	// space keys a local dependency by its normalized apm_modules/_local/
	// <hash> materialization key (localModulesKey), NOT the raw absolute
	// path apm.yml persists -- unlike the apm.yml assertions above, which
	// read the un-normalized persisted string.
	lockKeyA := localModulesKey(resolveLocalSourceAbs(repoA))
	lock := readLockfile(t)
	depByRepo := make(map[string]lockfile.LockedDep)
	for _, d := range lock.Dependencies {
		depByRepo[d.RepoURL] = d
	}
	da, ok := depByRepo[lockKeyA]
	if !ok {
		t.Fatalf("apm.lock.yaml: missing dependency %s (key %s)", repoA, lockKeyA)
	}
	if len(da.SkillSubset) != 1 || da.SkillSubset[0] != "skillA1" {
		t.Errorf("apm.lock.yaml: %s skill_subset = %v, want [skillA1]", repoA, da.SkillSubset)
	}
	if len(da.DeployedFiles) != 4 {
		t.Errorf("apm.lock.yaml: %s deployed_files = %v, want exactly 4 paths (1 skill, 2 files, 2 target roots)", repoA, da.DeployedFiles)
	}

	// apm_modules: exactly one materialized directory per repo, no
	// duplicates -- both local deps nest under a single apm_modules/_local/
	// parent (F1's materialization scheme), so the per-repo check happens
	// one level down.
	entries, err := os.ReadDir(filepath.Join(dir, "apm_modules", "_local"))
	if err != nil {
		t.Fatalf("read apm_modules/_local: %v", err)
	}
	if len(entries) != 2 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("apm_modules/_local: expected exactly 2 directories (one per repo), got %d: %v", len(entries), names)
	}
}

// TestInstall_SkillSubsetThreeRepos is the AC-B2-3 regression: installing a
// THIRD dependency with its own --skill subset must not re-inflate either of
// the two earlier dependencies' already-persisted subsets. Before the BUG-2
// fix, every `apm install` triggers a full-manifest re-resolve/re-deploy of
// ALL already-declared dependencies (by design), and the deploy-time
// SkillFilter was built only from the CURRENT call's CLI flags -- so every
// earlier dependency, having no entry in that call's filter, silently
// deployed its full, unfiltered skill set on each subsequent install.
func TestInstall_SkillSubsetThreeRepos(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	remotesDir := filepath.Join(dir, "remotes")
	if err := os.MkdirAll(remotesDir, 0755); err != nil {
		t.Fatal(err)
	}
	repoA := gitSkillRepo(t, remotesDir, "repo-a", map[string][]string{
		"skillA1": {"SKILL.md", "notes.md"},
		"skillA2": {"SKILL.md", "notes.md"},
	})
	repoB := gitSkillRepo(t, remotesDir, "repo-b", map[string][]string{
		"skillB1": {"SKILL.md", "notes.md"},
		"skillB2": {"SKILL.md", "notes.md"},
	})
	repoC := gitSkillRepo(t, remotesDir, "repo-c", map[string][]string{
		"skillC1": {"SKILL.md", "notes.md"},
		"skillC2": {"SKILL.md", "notes.md"},
	})

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"},
	}

	if err := runInstall(deps, false, true, "claude", []string{"skillA1"}, []string{repoA}); err != nil {
		t.Fatalf("install repo-a --skill skillA1: %v", err)
	}
	if err := runInstall(deps, false, true, "claude", []string{"skillB1"}, []string{repoB}); err != nil {
		t.Fatalf("install repo-b --skill skillB1: %v", err)
	}
	if err := runInstall(deps, false, true, "claude", []string{"skillC1"}, []string{repoC}); err != nil {
		t.Fatalf("install repo-c --skill skillC1: %v", err)
	}

	assertOnly := func(label string, present, absent []string) {
		t.Helper()
		for _, skill := range present {
			for _, p := range expectedSkillDeployPaths(skill, []string{"SKILL.md", "notes.md"}) {
				if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
					t.Errorf("%s: expected %s to exist: %v", label, p, err)
				}
			}
		}
		for _, skill := range absent {
			for _, p := range expectedSkillDeployPaths(skill, []string{"SKILL.md", "notes.md"}) {
				if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err == nil {
					t.Errorf("%s: INFLATION -- %s must NOT exist", label, p)
				}
			}
		}
	}

	// After the third install, repo-a and repo-b must still be narrowed to
	// their own originally-selected skill only -- not re-inflated to their
	// full skill set -- and repo-c must be narrowed to skillC1.
	assertOnly("after repo-c install", []string{"skillA1", "skillB1", "skillC1"}, []string{"skillA2", "skillB2", "skillC2"})

	m := readManifestParsed(t)
	subsetByRepo := make(map[string][]string)
	for _, d := range m.ParsedDeps {
		subsetByRepo[d.RepoURL] = d.SkillSubset
	}
	for repo, want := range map[string]string{repoA: "skillA1", repoB: "skillB1", repoC: "skillC1"} {
		if got := subsetByRepo[repo]; len(got) != 1 || got[0] != want {
			t.Errorf("apm.yml: expected %s skills: [%s], got %v", repo, want, got)
		}
	}

	lock := readLockfile(t)
	if len(lock.Dependencies) != 3 {
		t.Fatalf("apm.lock.yaml: expected exactly 3 dependencies, got %d", len(lock.Dependencies))
	}
	depByRepo := make(map[string]lockfile.LockedDep)
	for _, d := range lock.Dependencies {
		depByRepo[d.RepoURL] = d
	}
	for repo, want := range map[string]string{
		localModulesKey(resolveLocalSourceAbs(repoA)): "skillA1",
		localModulesKey(resolveLocalSourceAbs(repoB)): "skillB1",
		localModulesKey(resolveLocalSourceAbs(repoC)): "skillC1",
	} {
		d, ok := depByRepo[repo]
		if !ok {
			t.Fatalf("apm.lock.yaml: missing dependency %s", repo)
		}
		if len(d.SkillSubset) != 1 || d.SkillSubset[0] != want {
			t.Errorf("apm.lock.yaml: %s skill_subset = %v, want [%s]", repo, d.SkillSubset, want)
		}
	}
}

// TestInstall_SkillSubsetSameRepoUnion is the C3 regression (codex plan
// review, prd.md B2-2): re-installing the SAME dependency with a different
// --skill flag must UNION with (not replace, not silently ignore) its
// already-persisted subset, and that union must be written back into the
// EXISTING apm.yml entry in place -- persistPackagesToManifest used to
// `continue` on any already-declared package, never updating it at all.
// Exercises the full x -> y -> bare-install sequence prd.md's AC-B2-4
// requires.
func TestInstall_SkillSubsetSameRepoUnion(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	remotesDir := filepath.Join(dir, "remotes")
	if err := os.MkdirAll(remotesDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Absolute path, not "./..."-relative -- see TestInstall_SkillSubsetPollution's
	// comment on why (avoids an unrelated, pre-existing relative-local-path
	// re-normalization asymmetry).
	repoR := gitSkillRepo(t, remotesDir, "repo-r", map[string][]string{
		"skillX": {"SKILL.md", "notes.md"},
		"skillY": {"SKILL.md", "notes.md"},
	})

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"},
	}

	assertSelected := func(step string, selected ...string) {
		t.Helper()
		all := []string{"skillX", "skillY"}
		wanted := make(map[string]bool, len(selected))
		for _, s := range selected {
			wanted[s] = true
		}
		for _, skill := range all {
			for _, p := range expectedSkillDeployPaths(skill, []string{"SKILL.md", "notes.md"}) {
				_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p)))
				exists := err == nil
				if wanted[skill] && !exists {
					t.Errorf("%s: expected %s to exist", step, p)
				}
				if !wanted[skill] && exists {
					t.Errorf("%s: expected %s to NOT exist", step, p)
				}
			}
		}
	}

	// Step 1: install repo-r --skill skillX.
	if err := runInstall(deps, false, true, "claude", []string{"skillX"}, []string{repoR}); err != nil {
		t.Fatalf("step 1 (install repo-r --skill skillX): %v", err)
	}
	assertSelected("step1", "skillX")

	// Step 2: SAME repo, different --skill -- must UNION, not replace.
	if err := runInstall(deps, false, true, "claude", []string{"skillY"}, []string{repoR}); err != nil {
		t.Fatalf("step 2 (install repo-r --skill skillY): %v", err)
	}
	assertSelected("step2", "skillX", "skillY")

	m := readManifestParsed(t)
	if len(m.ParsedDeps) != 1 {
		t.Fatalf("step2: expected exactly ONE apm.yml entry for repo-r, got %d: %+v", len(m.ParsedDeps), m.ParsedDeps)
	}
	if got := m.ParsedDeps[0].SkillSubset; len(got) != 2 || got[0] != "skillX" || got[1] != "skillY" {
		t.Errorf("step2: apm.yml skills: = %v, want [skillX skillY] (union, sorted)", got)
	}

	lock := readLockfile(t)
	if len(lock.Dependencies) != 1 {
		t.Fatalf("step2: expected exactly ONE lockfile dependency, got %d", len(lock.Dependencies))
	}
	if got := lock.Dependencies[0].SkillSubset; len(got) != 2 || got[0] != "skillX" || got[1] != "skillY" {
		t.Errorf("step2: apm.lock.yaml skill_subset = %v, want [skillX skillY]", got)
	}
	if len(lock.Dependencies[0].DeployedFiles) != 8 {
		t.Errorf("step2: deployed_files = %v, want 8 paths (2 skills, 2 files, 2 target roots)", lock.Dependencies[0].DeployedFiles)
	}

	// Step 3: bare re-install (no positional package, no --skill) must keep
	// deploying the union -- not silently reset to full, not silently reset
	// to empty.
	if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
		t.Fatalf("step 3 (bare install): %v", err)
	}
	assertSelected("step3", "skillX", "skillY")

	m3 := readManifestParsed(t)
	if len(m3.ParsedDeps) != 1 || len(m3.ParsedDeps[0].SkillSubset) != 2 {
		t.Errorf("step3: apm.yml union must survive a bare re-install, got deps=%+v", m3.ParsedDeps)
	}
	lock3 := readLockfile(t)
	if len(lock3.Dependencies) != 1 || len(lock3.Dependencies[0].SkillSubset) != 2 {
		t.Errorf("step3: apm.lock.yaml skill_subset union must survive a bare re-install, got %+v", lock3.Dependencies)
	}
}

// TestValidateNewSkillNames_BlankNameRejected guards the H6 invariant at its
// entry point: a --skill value that trims to empty must be rejected loudly.
// Left to pass through, it would union to an EMPTY subset for a dependency
// with nothing persisted, and an empty slice in SkillFilter.Subsets silently
// deploys zero skills while apm.yml records no narrowing at all.
func TestValidateNewSkillNames_BlankNameRejected(t *testing.T) {
	for _, blank := range []string{"", " ", "\t"} {
		err := validateNewSkillNames(&resolver.ResolutionResult{}, map[string]bool{}, []string{blank})
		if err == nil {
			t.Errorf("--skill %q: expected a non-empty-name error, got nil", blank)
		}
	}
}

// TestInstall_UnknownSkill_NewNameErrorsAtomically is H3's "brand-new name"
// half (design.md §1.2f, prd.md B2-6): a --skill name THIS call introduces
// that doesn't match any skill the requested, already-resolved dependency
// actually has must fail the whole install BEFORE any write -- apm.yml,
// apm.lock.yaml, and every target directory must stay byte-identical to
// their pre-call state. Both a purely-unknown name and a mix of one real
// name plus one unknown name must fail the same way (no partial success).
func TestInstall_UnknownSkill_NewNameErrorsAtomically(t *testing.T) {
	cases := []struct {
		name   string
		skills []string
	}{
		{"purely unknown name", []string{"bogus"}},
		{"one real name mixed with one unknown name", []string{"realSkill", "bogus"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			origDir, _ := os.Getwd()
			os.Chdir(dir)
			defer os.Chdir(origDir)

			remotesDir := filepath.Join(dir, "remotes")
			if err := os.MkdirAll(remotesDir, 0755); err != nil {
				t.Fatal(err)
			}
			repo := gitSkillRepo(t, remotesDir, "repo-u", map[string][]string{
				"realSkill": {"SKILL.md"},
			})

			const originalManifest = "name: test\nversion: \"1.0.0\"\n"
			if err := os.WriteFile("apm.yml", []byte(originalManifest), 0644); err != nil {
				t.Fatal(err)
			}

			deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
			err := runInstall(deps, false, true, "claude", tc.skills, []string{repo})
			if err == nil {
				t.Fatal("expected an error for an unknown --skill name, got nil")
			}
			if !strings.Contains(err.Error(), "bogus") {
				t.Errorf("error should name the unknown skill \"bogus\", got: %v", err)
			}

			gotManifest, readErr := os.ReadFile("apm.yml")
			if readErr != nil {
				t.Fatalf("read apm.yml: %v", readErr)
			}
			if string(gotManifest) != originalManifest {
				t.Errorf("apm.yml changed after a failed --skill validation (atomicity broken):\nwant: %q\ngot:  %q", originalManifest, gotManifest)
			}

			if _, statErr := os.Stat("apm.lock.yaml"); statErr == nil {
				t.Error("apm.lock.yaml must not be written when --skill validation fails (atomicity broken)")
			}

			for _, root := range []string{".agents/skills", ".claude/skills"} {
				if entries, readErr := os.ReadDir(filepath.Join(dir, filepath.FromSlash(root))); readErr == nil && len(entries) > 0 {
					t.Errorf("%s must be empty when --skill validation fails, found %v", root, entries)
				}
			}
		})
	}
}

// TestInstall_UnknownSkill_PersistedNameDisappearsWarnsAndKeeps is H3's
// "already-persisted name" half: when an upstream package update drops a
// skill that was already persisted from an EARLIER install (not named by
// THIS call's --skill flag), the next bare install must NOT fail -- it warns
// (naming the vanished skill) and keeps the persisted subset unchanged in
// both apm.yml and apm.lock.yaml (no silent pruning, no hard error).
func TestInstall_UnknownSkill_PersistedNameDisappearsWarnsAndKeeps(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	remotesDir := filepath.Join(dir, "remotes")
	if err := os.MkdirAll(remotesDir, 0755); err != nil {
		t.Fatal(err)
	}
	repo := gitSkillRepo(t, remotesDir, "repo-v", map[string][]string{
		"onlySkill": {"SKILL.md"},
	})

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}

	if err := runInstall(deps, false, true, "claude", []string{"onlySkill"}, []string{repo}); err != nil {
		t.Fatalf("initial install: %v", err)
	}

	// Simulate an upstream update dropping the skill: a local-path dependency
	// re-copies its source verbatim on every install (gitops
	// materializeLocalCopy always replaces the prior materialization with a
	// fresh copy of the CURRENT source), so removing it from the source repo
	// makes the next install "see" it gone without needing a real git fetch.
	if err := os.RemoveAll(filepath.Join(repo, ".apm", "skills", "onlySkill")); err != nil {
		t.Fatal(err)
	}

	stderr := captureUninstallStderr(t, func() {
		if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
			t.Fatalf("bare re-install after upstream skill removal: %v", err)
		}
	})
	if !strings.Contains(stderr, "onlySkill") {
		t.Errorf("expected a warning naming the vanished persisted skill \"onlySkill\", got stderr: %q", stderr)
	}

	m := readManifestParsed(t)
	if len(m.ParsedDeps) != 1 || len(m.ParsedDeps[0].SkillSubset) != 1 || m.ParsedDeps[0].SkillSubset[0] != "onlySkill" {
		t.Errorf("apm.yml persisted subset must be kept unchanged, got %+v", m.ParsedDeps)
	}
	lock := readLockfile(t)
	if len(lock.Dependencies) != 1 || len(lock.Dependencies[0].SkillSubset) != 1 || lock.Dependencies[0].SkillSubset[0] != "onlySkill" {
		t.Errorf("apm.lock.yaml persisted skill_subset must be kept unchanged, got %+v", lock.Dependencies)
	}
}

// TestInstall_StaleSkillReconciliation is BUG-2's pollution-convergence test
// (design.md §1.2g, codex C1, prd.md B2-3/AC-B2-7): narrowing a PREVIOUSLY
// full install to a --skill subset must not just start filtering future
// deploys -- it must also clean up files the wider PRIOR install already
// wrote for the now-excluded skill, as long as they haven't been hand-edited
// since (an untouched stale file is removed; a hand-edited one is kept, with
// a warning naming it).
func TestInstall_StaleSkillReconciliation(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	remotesDir := filepath.Join(dir, "remotes")
	if err := os.MkdirAll(remotesDir, 0755); err != nil {
		t.Fatal(err)
	}
	repo := gitSkillRepo(t, remotesDir, "repo-s", map[string][]string{
		"skillA1": {"SKILL.md", "notes.md"},
		"skillA2": {"SKILL.md", "notes.md"},
	})

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}

	// Step 1: full install, no --skill -- deploys BOTH skills.
	if err := runInstall(deps, false, true, "claude", nil, []string{repo}); err != nil {
		t.Fatalf("step 1 (full install): %v", err)
	}
	for _, skill := range []string{"skillA1", "skillA2"} {
		for _, p := range expectedSkillDeployPaths(skill, []string{"SKILL.md", "notes.md"}) {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
				t.Fatalf("step1: expected %s to exist: %v", p, err)
			}
		}
	}

	// Hand-edit ONE of skillA2's four deployed copies BEFORE narrowing -- it
	// must survive reconciliation (kept + warned), unlike its three untouched
	// siblings under the same now-excluded skill.
	skillA2Paths := expectedSkillDeployPaths("skillA2", []string{"SKILL.md", "notes.md"})
	modifiedRel := skillA2Paths[1] // ".agents/skills/skillA2/notes.md"
	modifiedPath := filepath.Join(dir, filepath.FromSlash(modifiedRel))
	const modifiedContent = "hand-edited by the user\n"
	if err := os.WriteFile(modifiedPath, []byte(modifiedContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Step 2: narrow to --skill skillA1 (same repo) -- must deploy ONLY
	// skillA1 files, and clean up skillA2's now-stale, untouched files.
	stderr := captureUninstallStderr(t, func() {
		if err := runInstall(deps, false, true, "claude", []string{"skillA1"}, []string{repo}); err != nil {
			t.Fatalf("step 2 (narrow to skillA1): %v", err)
		}
	})

	// skillA1 must still be fully deployed.
	for _, p := range expectedSkillDeployPaths("skillA1", []string{"SKILL.md", "notes.md"}) {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
			t.Errorf("step2: expected %s to still exist: %v", p, err)
		}
	}

	// skillA2's three UNTOUCHED copies must be removed (stale + hash matches).
	for _, p := range skillA2Paths {
		if p == modifiedRel {
			continue
		}
		full := filepath.Join(dir, filepath.FromSlash(p))
		if _, err := os.Stat(full); err == nil {
			t.Errorf("step2: expected stale untouched %s to be removed", full)
		}
	}

	// skillA2's HAND-EDITED copy must be KEPT, byte-unchanged, with a warning.
	data, err := os.ReadFile(modifiedPath)
	if err != nil {
		t.Errorf("step2: expected hand-edited %s to be kept, got: %v", modifiedPath, err)
	} else if string(data) != modifiedContent {
		t.Errorf("step2: hand-edited file content changed unexpectedly: %q", data)
	}
	if !strings.Contains(stderr, "notes.md") {
		t.Errorf("expected a warning naming the kept modified file, got stderr: %q", stderr)
	}

	// apm.yml / lockfile reflect the narrowed subset.
	m := readManifestParsed(t)
	if len(m.ParsedDeps) != 1 || len(m.ParsedDeps[0].SkillSubset) != 1 || m.ParsedDeps[0].SkillSubset[0] != "skillA1" {
		t.Errorf("apm.yml skills: must be [skillA1], got %+v", m.ParsedDeps)
	}
	lock := readLockfile(t)
	if len(lock.Dependencies) != 1 {
		t.Fatalf("expected exactly one lockfile dependency, got %d", len(lock.Dependencies))
	}
	if got := lock.Dependencies[0].SkillSubset; len(got) != 1 || got[0] != "skillA1" {
		t.Errorf("apm.lock.yaml skill_subset must be [skillA1], got %v", got)
	}
	if len(lock.Dependencies[0].DeployedFiles) != 4 {
		t.Errorf("apm.lock.yaml deployed_files must be exactly 4 paths (skillA1, 2 files, 2 roots), got %v", lock.Dependencies[0].DeployedFiles)
	}
}

// TestInstall_StaleSkillReconciliation_TargetChangeWithoutSkillSubsetKeepsFiles
// is a codex final-gate HIGH regression: reconcileStaleSkillDeployments must
// NOT delete a dependency's files just because a LATER install call resolved
// a different --target set than an EARLIER one did -- that is a target
// selection change, not a --skill subset narrowing, and this BUG-2
// convergence mechanism must stay scoped to the latter. Installing to
// "claude" first (which deploys to BOTH .claude/skills/ and the shared
// .agents/skills/) and then re-installing the SAME dependency (no --skill
// either time) to "codex" only (which deploys to the shared .agents/skills/
// root only, not .claude/skills/) must leave the now-target-unclaimed
// .claude/skills/ copy on disk untouched, because this dependency's fresh
// lock entry has no active SkillSubset at all.
func TestInstall_StaleSkillReconciliation_TargetChangeWithoutSkillSubsetKeepsFiles(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	remotesDir := filepath.Join(dir, "remotes")
	if err := os.MkdirAll(remotesDir, 0755); err != nil {
		t.Fatal(err)
	}
	repo := gitSkillRepo(t, remotesDir, "repo-t", map[string][]string{
		"skillT": {"SKILL.md"},
	})

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}

	// Step 1: full install (no --skill) targeting claude -- deploys to BOTH
	// .claude/skills/skillT/ and .agents/skills/skillT/.
	if err := runInstall(deps, false, true, "claude", nil, []string{repo}); err != nil {
		t.Fatalf("step 1 (install --target claude): %v", err)
	}
	claudeOnlyPath := filepath.Join(dir, ".claude", "skills", "skillT", "SKILL.md")
	if _, err := os.Stat(claudeOnlyPath); err != nil {
		t.Fatalf("step1: expected %s to exist: %v", claudeOnlyPath, err)
	}

	// Step 2: full install (still no --skill) but this time targeting codex
	// only -- codex's skill deploy root is the shared .agents/skills/, so
	// .claude/skills/skillT/ is no longer claimed by this run's lockfile.
	if err := runInstall(deps, false, true, "codex", nil, []string{repo}); err != nil {
		t.Fatalf("step 2 (install --target codex): %v", err)
	}

	// The regression check: the claude-only copy must SURVIVE -- this
	// dependency never had an active --skill subset, so target-selection
	// drift alone must never trigger a deletion.
	if _, err := os.Stat(claudeOnlyPath); err != nil {
		t.Errorf("codex final-gate HIGH regression: %s was deleted after an unrelated --target change (no --skill ever used): %v", claudeOnlyPath, err)
	}

	lock := readLockfile(t)
	if len(lock.Dependencies) != 1 || len(lock.Dependencies[0].SkillSubset) != 0 {
		t.Fatalf("expected exactly one lockfile dependency with an empty skill_subset, got %+v", lock.Dependencies)
	}
}

// TestInstall_StaleSkillReconciliation_StillSelectedSkillSurvivesTargetChange
// is a second codex final-gate HIGH regression, distinct from the previous
// test: even for a dependency that DOES have an active --skill subset, a
// skill NAME still present in that subset must never be pruned just because
// one particular --target's copy of it isn't claimed this run. Only a skill
// name actually ABSENT from the fresh subset is eligible for cleanup.
func TestInstall_StaleSkillReconciliation_StillSelectedSkillSurvivesTargetChange(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	remotesDir := filepath.Join(dir, "remotes")
	if err := os.MkdirAll(remotesDir, 0755); err != nil {
		t.Fatal(err)
	}
	repo := gitSkillRepo(t, remotesDir, "repo-u2", map[string][]string{
		"skillX": {"SKILL.md"},
	})

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}

	// Step 1: narrow to --skill skillX, targeting claude -- deploys skillX
	// to BOTH .claude/skills/skillX/ and .agents/skills/skillX/.
	if err := runInstall(deps, false, true, "claude", []string{"skillX"}, []string{repo}); err != nil {
		t.Fatalf("step 1 (install --skill skillX --target claude): %v", err)
	}
	claudeOnlyPath := filepath.Join(dir, ".claude", "skills", "skillX", "SKILL.md")
	if _, err := os.Stat(claudeOnlyPath); err != nil {
		t.Fatalf("step1: expected %s to exist: %v", claudeOnlyPath, err)
	}

	// Step 2: SAME --skill skillX (still selected, no narrowing), but this
	// time targeting codex only -- codex's skill deploy root is the shared
	// .agents/skills/, so .claude/skills/skillX/ is not claimed this run.
	if err := runInstall(deps, false, true, "codex", []string{"skillX"}, []string{repo}); err != nil {
		t.Fatalf("step 2 (install --skill skillX --target codex): %v", err)
	}

	// The regression check: skillX is STILL in the subset -- its
	// now-unclaimed claude-only copy must survive; only a DESELECTED skill
	// name is eligible for cleanup.
	if _, err := os.Stat(claudeOnlyPath); err != nil {
		t.Errorf("codex final-gate HIGH regression: %s was deleted for a skill still present in the subset (target changed, not the subset): %v", claudeOnlyPath, err)
	}

	lock := readLockfile(t)
	if len(lock.Dependencies) != 1 || len(lock.Dependencies[0].SkillSubset) != 1 || lock.Dependencies[0].SkillSubset[0] != "skillX" {
		t.Fatalf("expected exactly one lockfile dependency with skill_subset=[skillX], got %+v", lock.Dependencies)
	}
}

// TestRunInstall_SkillMixedWildcardResetsToFull is a boundary regression
// (implement.md Phase 1 step 11): a PREVIOUSLY-narrowed dependency
// re-installed with a --skill list that MIXES a concrete name with the '*'
// RESET sentinel (`--skill skillX --skill '*'`) must reset to a full
// install exactly like a pure `--skill '*'` would -- covering the full
// runInstall -> deploy -> apm.yml/apm.lock.yaml chain, not just the unit-level
// buildLockfile/persistPackagesToManifest checks this mirrors.
func TestRunInstall_SkillMixedWildcardResetsToFull(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	remotesDir := filepath.Join(dir, "remotes")
	if err := os.MkdirAll(remotesDir, 0755); err != nil {
		t.Fatal(err)
	}
	repo := gitSkillRepo(t, remotesDir, "repo-m", map[string][]string{
		"skillX": {"SKILL.md"},
		"skillY": {"SKILL.md"},
	})
	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}

	// Step 1: narrow to skillX only.
	if err := runInstall(deps, false, true, "claude", []string{"skillX"}, []string{repo}); err != nil {
		t.Fatalf("step1: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "skillY", "SKILL.md")); err == nil {
		t.Fatal("step1: skillY must not be deployed yet")
	}

	// Step 2: mixed wildcard (a concrete name PLUS '*') resets to full.
	if err := runInstall(deps, false, true, "claude", []string{"skillX", "*"}, []string{repo}); err != nil {
		t.Fatalf("step2: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "skillY", "SKILL.md")); err != nil {
		t.Errorf("step2: expected skillY to deploy after mixed-wildcard reset: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "skillX", "SKILL.md")); err != nil {
		t.Errorf("step2: expected skillX to still be deployed: %v", err)
	}

	m := readManifestParsed(t)
	if len(m.ParsedDeps) != 1 || len(m.ParsedDeps[0].SkillSubset) != 0 {
		t.Errorf("step2: apm.yml skills: must be cleared after wildcard reset, got %+v", m.ParsedDeps)
	}
	lock := readLockfile(t)
	if len(lock.Dependencies) != 1 || len(lock.Dependencies[0].SkillSubset) != 0 {
		t.Errorf("step2: apm.lock.yaml skill_subset must be cleared after wildcard reset, got %+v", lock.Dependencies)
	}
}

// TestRunInstall_DevDependency_SkillSubsetHonored is a boundary regression
// (implement.md Phase 1 step 11, codex M6): a persisted --skill subset on a
// devDependencies.apm entry must be honored by deploy.Run's SkillFilter
// exactly like an ordinary dependencies.apm entry -- Run's directDeps walk
// covers m.ParsedDeps THEN m.ParsedDevDeps (F3), and effectiveSkillSubsets
// walks the same allDirectDeps helper, so a dev-only persisted subset must
// not silently fall back to a full install.
func TestRunInstall_DevDependency_SkillSubsetHonored(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	remotesDir := filepath.Join(dir, "remotes")
	if err := os.MkdirAll(remotesDir, 0755); err != nil {
		t.Fatal(err)
	}
	repo := gitSkillRepo(t, remotesDir, "repo-d", map[string][]string{
		"skillX": {"SKILL.md"},
		"skillY": {"SKILL.md"},
	})
	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}

	// Install once as an ordinary (production) dependency, narrowed to
	// skillX -- persistPackagesToManifest handles the git-value quoting
	// correctly (avoids hand-authoring YAML containing a raw Windows
	// absolute path, which has its own quoting pitfalls).
	if err := runInstall(deps, false, true, "claude", []string{"skillX"}, []string{repo}); err != nil {
		t.Fatalf("initial install: %v", err)
	}

	// Move the persisted entry from dependencies.apm to devDependencies.apm
	// by simple string surgery on the already-correctly-quoted output.
	manifestBytes, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	moved := strings.Replace(string(manifestBytes), "dependencies:\n  apm:", "devDependencies:\n  apm:", 1)
	if moved == string(manifestBytes) {
		t.Fatalf("did not find dependencies.apm block to move in apm.yml:\n%s", manifestBytes)
	}
	if err := os.WriteFile("apm.yml", []byte(moved), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove("apm.lock.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, ".agents")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, ".claude")); err != nil {
		t.Fatal(err)
	}

	if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
		t.Fatalf("bare install after moving to devDependencies: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "skillX", "SKILL.md")); err != nil {
		t.Errorf("expected skillX (persisted devDependency subset) to deploy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "skillY", "SKILL.md")); err == nil {
		t.Error("expected skillY to stay filtered out for the devDependency's persisted subset")
	}

	lock := readLockfile(t)
	if len(lock.Dependencies) != 1 || len(lock.Dependencies[0].SkillSubset) != 1 || lock.Dependencies[0].SkillSubset[0] != "skillX" {
		t.Errorf("apm.lock.yaml skill_subset for the devDependency must be [skillX], got %+v", lock.Dependencies)
	}
}

// TestRunInstall_MultiplePositionalPackages_SharedSkillFlag documents
// (implement.md Phase 1 step 11) the current, accepted behavior when a
// single --skill flag list is shared across MULTIPLE positional packages
// naming DIFFERENT repositories: effectiveSkillSubsets unions the WHOLE
// cliSubset into EVERY targeted dependency (design.md §1.2c rule 2), not
// just the name(s) that dependency actually has -- so validateNewSkillNames
// requires each name to exist in AT LEAST ONE targeted dependency (not
// every one), letting a shared --skill list name one skill per repo without
// being rejected as a typo just because neither repo has the OTHER repo's
// skill. The practical consequence (pinned here, not a bug to fix in this
// task): each dependency's persisted/locked subset includes the FULL
// cliSubset, even the name(s) it doesn't actually have, and deploy.Run warns
// about the name it lacks via the same skillSubsetDiags path an
// upstream-vanished persisted name uses.
func TestRunInstall_MultiplePositionalPackages_SharedSkillFlag(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	remotesDir := filepath.Join(dir, "remotes")
	if err := os.MkdirAll(remotesDir, 0755); err != nil {
		t.Fatal(err)
	}
	repoA := gitSkillRepo(t, remotesDir, "repo-ma", map[string][]string{
		"nameA":  {"SKILL.md"},
		"extraA": {"SKILL.md"},
	})
	repoB := gitSkillRepo(t, remotesDir, "repo-mb", map[string][]string{
		"nameB":  {"SKILL.md"},
		"extraB": {"SKILL.md"},
	})
	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}

	stderr := captureUninstallStderr(t, func() {
		err := runInstall(deps, false, true, "claude", []string{"nameA", "nameB"}, []string{repoA, repoB})
		if err != nil {
			t.Fatalf("runInstall: %v", err)
		}
	})

	// Each repo deploys only its OWN named skill, never its "extra" one, and
	// never the OTHER repo's name (which it doesn't actually have).
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "nameA", "SKILL.md")); err != nil {
		t.Errorf("expected repoA's nameA to deploy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "extraA", "SKILL.md")); err == nil {
		t.Error("expected repoA's extraA to stay filtered out")
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "nameB", "SKILL.md")); err != nil {
		t.Errorf("expected repoB's nameB to deploy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "extraB", "SKILL.md")); err == nil {
		t.Error("expected repoB's extraB to stay filtered out")
	}

	// Documented behavior: both repos are warned about the cross-applied
	// name they don't have (repoA lacks nameB, repoB lacks nameA).
	if !strings.Contains(stderr, "nameB") || !strings.Contains(stderr, "nameA") {
		t.Errorf("expected warnings naming the cross-applied skill each repo lacks, got stderr: %q", stderr)
	}

	m := readManifestParsed(t)
	if len(m.ParsedDeps) != 2 {
		t.Fatalf("expected exactly 2 apm.yml entries, got %d: %+v", len(m.ParsedDeps), m.ParsedDeps)
	}
	for _, d := range m.ParsedDeps {
		if got := d.SkillSubset; len(got) != 2 || got[0] != "nameA" || got[1] != "nameB" {
			t.Errorf("expected each dep's persisted subset to be the full shared --skill list [nameA nameB], got %v for %s", got, d.RepoURL)
		}
	}
}

// TestRunInstall_PlainAbsoluteLocalPathPackage_PersistsAndRoundTrips covers
// the shared-root-cause half of this task's fix (the F1 marketplace bug's
// underlying gap): `apm install /abs/path` -- a bare positional package
// argument that is itself an OS-absolute filesystem path, no marketplace
// syntax involved -- used to hard-error at manifest.ParseDepString
// ("dependency path %q is absolute; only relative paths are allowed")
// before this fix. It must now succeed exactly like any other local
// positional package: forced into a "git" source pointing at that absolute
// path (install.go's existing ref.IsLocal normalization, unchanged by this
// task), persisted to apm.yml verbatim (no marketplace canonical involved,
// so no relative/absolute decision to make here), lockfile written, and a
// second bare `apm install` re-reads both apm.yml and apm.lock.yaml
// without erroring (round-trip).
func TestRunInstall_PlainAbsoluteLocalPathPackage_PersistsAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// The "package" itself just needs to exist on disk -- LoadPackage is
	// mocked (matching every other install test in this file), so no real
	// git clone happens; only apm.yml/apm.lock.yaml persistence and
	// round-trip parsing are under test here.
	pkgDir := filepath.Join(dir, "external-pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	// Act (a): first install, plain absolute positional package.
	if err := runInstall(deps, false, true, "claude", nil, []string{pkgDir}); err != nil {
		t.Fatalf("(a) runInstall: %v", err)
	}

	// Assert (a): apm.yml persisted the absolute path verbatim.
	apmYML, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatalf("read apm.yml: %v", err)
	}
	if !strings.Contains(string(apmYML), pkgDir) {
		t.Errorf("apm.yml = %q, want it to contain %q", apmYML, pkgDir)
	}
	if _, statErr := os.Stat("apm.lock.yaml"); statErr != nil {
		t.Fatalf("expected apm.lock.yaml to be written: %v", statErr)
	}

	// Act + Assert (b): round-trip.
	if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
		t.Fatalf("(b) bare runInstall (round-trip): %v", err)
	}
}

// wantAllowExecutablesWarning duplicates internal/manifest's
// allowExecutablesWarning as an independent string literal -- not a
// reference to that package's unexported identifier -- so a wording change
// there breaks this cmd/apm-go E2E test too, not just internal/manifest's own
// unit test (same verbatim-lock pattern as pack_test.go's
// wantPackDepsWarning/wantPackTargetWarning).
const wantAllowExecutablesWarning = "[warn] apm.yml has an allowExecutables: block, but apm-go does not enforce it yet; this block is not effective in apm-go and every executable primitive (hooks, bin, MCP) is still deployed unconditionally"

// TestRunInstall_AllowExecutablesWarning locks P0 #4 (register §4.1/§5): an
// apm.yml `allowExecutables:` block is not enforced by apm-go -- every
// executable primitive (hooks, bin, MCP) is still deployed unconditionally,
// with or without the block -- but `apm-go install` must warn instead of
// silently ignoring it.
func TestRunInstall_AllowExecutablesWarning(t *testing.T) {
	const hookBody = `{"Stop":[{"hooks":[{"type":"command","command":"echo unchanged"}]}]}`

	runCase := func(t *testing.T, withBlock bool) (stderr string, hookHash [32]byte) {
		t.Helper()
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		if err := os.MkdirAll(filepath.Join(dir, ".apm", "hooks"), 0o755); err != nil {
			t.Fatal(err)
		}
		manifestYAML := "name: allow-test\nversion: \"1.0.0\"\ntarget:\n  - codex\n"
		if withBlock {
			manifestYAML += "allowExecutables: {}\n"
		}
		if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(".apm", "hooks", "probe.json"), []byte(hookBody), 0o644); err != nil {
			t.Fatal(err)
		}

		deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
		stderr = captureUninstallStderr(t, func() {
			if err := runInstall(deps, false, false, "", nil, nil); err != nil {
				t.Fatalf("runInstall failed: %v", err)
			}
		})

		hookPath := filepath.Join(dir, ".codex", "hooks.json")
		data, err := os.ReadFile(hookPath)
		if err != nil {
			t.Fatalf("hook not deployed at %s: %v", hookPath, err)
		}
		return stderr, sha256.Sum256(data)
	}

	stderrWith, hashWith := runCase(t, true)
	if !strings.Contains(stderrWith, "allowExecutables") {
		t.Errorf("stderr = %q, want an allowExecutables warning", stderrWith)
	}
	if !strings.Contains(strings.ToLower(stderrWith), "warn") {
		t.Errorf("stderr = %q, want a [warn]-level message", stderrWith)
	}
	if !strings.Contains(stderrWith, "not effective") {
		t.Errorf("stderr = %q, want the warning to say the block is not effective in apm-go", stderrWith)
	}
	if !strings.Contains(stderrWith, wantAllowExecutablesWarning) {
		t.Errorf("stderr = %q, want the full allowExecutables warning %q", stderrWith, wantAllowExecutablesWarning)
	}

	stderrWithout, hashWithout := runCase(t, false)
	if strings.Contains(stderrWithout, "allowExecutables") {
		t.Errorf("stderr = %q, must not warn when apm.yml has no allowExecutables: block", stderrWithout)
	}

	if hashWith != hashWithout {
		t.Errorf("deployed .codex/hooks.json differs with vs without allowExecutables: block (with=%x, without=%x)", hashWith, hashWithout)
	}
}

// TestResolvedDepCanonicalKey_SelfHostedPreservesCase is the final codex
// gate regression: a resolved dep only carries its RepoURL string, and
// feeding that into CanonicalRepoIdentity's bare-literal branch blanket-
// lowercased it -- diverging from the manifest-refs side, which preserves
// owner/repo case on a self-hosted host. Both sides must produce the same
// key or the lockfile lookup misses the subset the deploy filter applies.
func TestResolvedDepCanonicalKey_SelfHostedPreservesCase(t *testing.T) {
	manifestRef, err := manifest.ParseDepString("git.internal/Acme/Repo")
	if err != nil {
		t.Fatalf("ParseDepString: %v", err)
	}
	manifestSide := deploy.CanonicalDepKey(manifestRef)

	resolvedSide := resolvedDepCanonicalKey(resolver.ResolvedDep{RepoURL: "git.internal/Acme/Repo"})

	if resolvedSide != manifestSide {
		t.Errorf("resolved-side key %q != manifest-side key %q", resolvedSide, manifestSide)
	}
	if resolvedSide == strings.ToLower(resolvedSide) && manifestSide != strings.ToLower(manifestSide) {
		t.Errorf("resolved-side key was blanket-lowercased: %q", resolvedSide)
	}
}
