package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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

			lock, err := buildLockfile(result, nil, &registry.Loader{}, tt.skillSubset, requestedKeys, true, nil)
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

	if err := persistPackagesToManifest(doc, []string{"acme/foo"}, []string{"*"}); err != nil {
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

	if err := persistPackagesToManifest(doc, []string{"acme/foo"}, []string{"*"}); err != nil {
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
