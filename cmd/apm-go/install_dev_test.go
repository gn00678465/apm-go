package main

import (
	"os"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

// TestRunInstall_Dev_WritesDevDependenciesNotDependencies is the AC42
// end-to-end regression: `install --dev owner/repo` must persist the
// package into devDependencies.apm, never dependencies.apm, and the
// resulting apm.lock.yaml entry must carry package_type (AC45).
func TestRunInstall_Dev_WritesDevDependenciesNotDependencies(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &mockInstallLoader{},
		dev:    true,
	}
	if err := runInstall(deps, false, true, "claude", nil, []string{"acme/foo"}); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	manifestBytes, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	node, err := yamlcore.SafeLoad(manifestBytes)
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if len(m.ParsedDeps) != 0 {
		t.Errorf("expected zero dependencies.apm entries, got %d: %+v", len(m.ParsedDeps), m.ParsedDeps)
	}
	if len(m.ParsedDevDeps) != 1 || m.ParsedDevDeps[0].RepoURL != "acme/foo" {
		t.Errorf("expected exactly one devDependencies.apm entry acme/foo, got %+v", m.ParsedDevDeps)
	}

	lock := readLockfile(t)
	var found *lockfile.LockedDep
	for i := range lock.Dependencies {
		if lock.Dependencies[i].UniqueKey() == "acme/foo" {
			found = &lock.Dependencies[i]
		}
	}
	if found == nil {
		t.Fatalf("expected acme/foo in apm.lock.yaml, got: %+v", lock.Dependencies)
	}
	if found.PackageType != lockfile.PackageTypeMarketplacePlugin {
		t.Errorf("package_type = %q, want %q", found.PackageType, lockfile.PackageTypeMarketplacePlugin)
	}
}

// TestRunInstall_NoDev_StillWritesDependenciesNotDevDependencies is AC44's
// regression gate: without --dev, positional-package persistence stays
// dependencies.apm (never devDependencies.apm) and the lockfile entry's
// package_type stays empty, exactly as before --dev existed.
func TestRunInstall_NoDev_StillWritesDependenciesNotDevDependencies(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &mockInstallLoader{},
	}
	if err := runInstall(deps, false, true, "claude", nil, []string{"acme/foo"}); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	manifestBytes, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	got := string(manifestBytes)
	if strings.Contains(got, "devDependencies:") {
		t.Errorf("expected NO devDependencies: section without --dev, got:\n%s", got)
	}
	if !strings.Contains(got, "dependencies:") || !strings.Contains(got, "acme/foo") {
		t.Errorf("expected acme/foo under dependencies:, got:\n%s", got)
	}

	lock := readLockfile(t)
	if len(lock.Dependencies) != 1 {
		t.Fatalf("expected 1 locked dependency, got %d", len(lock.Dependencies))
	}
	if got := lock.Dependencies[0].PackageType; got != "" {
		t.Errorf("expected empty package_type for a non-dev dependency, got %q", got)
	}
}

// TestPersistPackagesToManifest_DevSection_InsertsBetweenIncludesAndScripts
// is AC43's key-order regression: a freshly-created devDependencies section
// must land between includes: and scripts: (07-29-targets-init-shape's R2
// key-order contract, design.md §11's "鍵序約束"), not appended at the
// document's end. Uses the same apm.yml shape as
// 07-29-install-dev/verify.ps1's probe fixture.
func TestPersistPackagesToManifest_DevSection_InsertsBetweenIncludesAndScripts(t *testing.T) {
	src := "name: dev-probe\nversion: 1.0.0\ndescription: probe\nauthor: t\ntargets:\n  - claude\n" +
		"dependencies:\n  apm: []\n  mcp: []\nincludes: auto\nscripts: {}\n"
	doc, err := yamlcore.SafeLoad([]byte(src))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	if err := persistPackagesToManifest(doc, []string{"pbakaus/impeccable"}, nil, true); err != nil {
		t.Fatalf("persistPackagesToManifest: %v", err)
	}
	out, err := yamlcore.SafeDump(doc)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	got := string(out)

	incIdx := strings.Index(got, "includes:")
	devIdx := strings.Index(got, "devDependencies:")
	scrIdx := strings.Index(got, "scripts:")
	if incIdx < 0 || devIdx < 0 || scrIdx < 0 {
		t.Fatalf("expected includes/devDependencies/scripts all present; got:\n%s", got)
	}
	if !(incIdx < devIdx && devIdx < scrIdx) {
		t.Errorf("expected devDependencies between includes and scripts (includes=%d dev=%d scripts=%d); got:\n%s", incIdx, devIdx, scrIdx, got)
	}
	if !strings.Contains(got, "apm:\n    - pbakaus/impeccable") {
		t.Errorf("expected package entry under devDependencies.apm; got:\n%s", got)
	}
	if strings.Contains(got, "dependencies:\n  apm:\n    - pbakaus/impeccable") {
		t.Errorf("must not write into dependencies.apm; got:\n%s", got)
	}
}

// TestPersistPackagesToManifest_DevSection_ReusesExistingKey proves a
// pre-existing devDependencies: key is reused in place (no duplicate key,
// no reformatting of unrelated keys) when adding a second dev package.
func TestPersistPackagesToManifest_DevSection_ReusesExistingKey(t *testing.T) {
	src := "name: d\nversion: 1.0.0\nincludes: auto\ndevDependencies:\n  apm:\n    - acme/existing\nscripts: {}\n"
	doc, err := yamlcore.SafeLoad([]byte(src))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	if err := persistPackagesToManifest(doc, []string{"acme/new"}, nil, true); err != nil {
		t.Fatalf("persistPackagesToManifest: %v", err)
	}
	out, err := yamlcore.SafeDump(doc)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	got := string(out)

	if n := strings.Count(got, "devDependencies:"); n != 1 {
		t.Errorf("devDependencies: key appears %d times, want 1; got:\n%s", n, got)
	}
	if !strings.Contains(got, "- acme/existing") {
		t.Errorf("expected pre-existing entry to survive; got:\n%s", got)
	}
	if !strings.Contains(got, "- acme/new") {
		t.Errorf("expected new entry to be added; got:\n%s", got)
	}
}

// TestRunInstall_Dev_ExistingDevDependency_BareInstall_MovesToNonDev is the
// scenario-A regression from the external Tier-2 audit finding: install.go's
// existing/existingByIdentity dedup (built at "1b. Add positional packages")
// previously scanned ONLY m.ParsedDeps, never m.ParsedDevDeps, so
// re-installing an already-declared devDependencies.apm entry WITHOUT --dev
// silently appended a SECOND, duplicate entry into dependencies.apm instead
// of recognizing it as already declared. Per the user's 2026-07-30 decision
// (npm `npm i -D X` / `npm i X` parity), the correct behavior is not "leave
// alone" but MOVE: this call's flag (no --dev here) is the final word on
// section ownership, so the entry relocates from devDependencies.apm to
// dependencies.apm, never leaving a duplicate in both.
func TestRunInstall_Dev_ExistingDevDependency_BareInstall_MovesToNonDev(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndevDependencies:\n  apm:\n    - acme/foo\n"), 0644)

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &mockInstallLoader{},
	}
	if err := runInstall(deps, false, true, "claude", nil, []string{"acme/foo"}); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	manifestBytes, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	node, err := yamlcore.SafeLoad(manifestBytes)
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if len(m.ParsedDevDeps) != 0 {
		t.Errorf("expected zero devDependencies.apm entries after move (moved out, not duplicated), got %d: %+v", len(m.ParsedDevDeps), m.ParsedDevDeps)
	}
	if len(m.ParsedDeps) != 1 || m.ParsedDeps[0].RepoURL != "acme/foo" {
		t.Errorf("expected exactly one dependencies.apm entry acme/foo (moved, not duplicated), got %+v", m.ParsedDeps)
	}

	lock := readLockfile(t)
	if len(lock.Dependencies) != 1 {
		t.Fatalf("expected exactly 1 locked dependency (no duplicate), got %d: %+v", len(lock.Dependencies), lock.Dependencies)
	}
	if got := lock.Dependencies[0].PackageType; got != "" {
		t.Errorf("package_type = %q after moving out of devDependencies.apm, want \"\"", got)
	}
}

// TestRunInstall_Dev_ExistingDependency_DevInstall_MovesToDev is the
// scenario-B regression: `install --dev owner/repo` against a package
// already declared (non-dev) in dependencies.apm previously left `existing`
// (built only from m.ParsedDeps) reporting it as already-declared, so the
// early `continue` at the positional-package loop skipped adding it to
// m.ParsedDevDeps entirely -- meaning it was never added to devDependencies
// while persistPackagesToManifest (indexed only against the target section's
// OWN apm sequence) independently wrote a SECOND entry there, and the
// lockfile's package_type marking loop (keyed off m.ParsedDevDeps) never saw
// it, leaving package_type="" instead of "marketplace_plugin". Fixed
// behavior: MOVE the entry from dependencies.apm to devDependencies.apm.
func TestRunInstall_Dev_ExistingDependency_DevInstall_MovesToDev(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo\n"), 0644)

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &mockInstallLoader{},
		dev:    true,
	}
	if err := runInstall(deps, false, true, "claude", nil, []string{"acme/foo"}); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	manifestBytes, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	node, err := yamlcore.SafeLoad(manifestBytes)
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if len(m.ParsedDeps) != 0 {
		t.Errorf("expected zero dependencies.apm entries after move (moved out, not duplicated), got %d: %+v", len(m.ParsedDeps), m.ParsedDeps)
	}
	if len(m.ParsedDevDeps) != 1 || m.ParsedDevDeps[0].RepoURL != "acme/foo" {
		t.Errorf("expected exactly one devDependencies.apm entry acme/foo (moved, not duplicated), got %+v", m.ParsedDevDeps)
	}

	lock := readLockfile(t)
	if len(lock.Dependencies) != 1 {
		t.Fatalf("expected exactly 1 locked dependency (no duplicate), got %d: %+v", len(lock.Dependencies), lock.Dependencies)
	}
	if got := lock.Dependencies[0].PackageType; got != lockfile.PackageTypeMarketplacePlugin {
		t.Errorf("package_type = %q after moving into devDependencies.apm, want %q", got, lockfile.PackageTypeMarketplacePlugin)
	}
}

// TestPersistPackagesToManifest_DevSection_NoIncludesKey_AppendsAtEnd covers
// the fallback branch: a document with no includes: key at all still gets a
// devDependencies section (at the end), instead of erroring or silently
// dropping the package.
func TestPersistPackagesToManifest_DevSection_NoIncludesKey_AppendsAtEnd(t *testing.T) {
	src := "name: d\nversion: 1.0.0\n"
	doc, err := yamlcore.SafeLoad([]byte(src))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	if err := persistPackagesToManifest(doc, []string{"acme/foo"}, nil, true); err != nil {
		t.Fatalf("persistPackagesToManifest: %v", err)
	}
	out, err := yamlcore.SafeDump(doc)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "devDependencies:\n  apm:\n    - acme/foo") {
		t.Errorf("expected devDependencies.apm entry; got:\n%s", got)
	}
}

// TestRunInstall_Dev_CrossSectionMove_PreservesEntryMetadata is the
// 2026-07-30 Tier-2 regression: moving an already-declared dependency across
// dependencies.apm/devDependencies.apm previously rebuilt a FRESH
// {git, skills}/scalar entry from just the bare pkg string (removeMatchingEntry
// only ever returned whether something was removed, discarding the removed
// node itself), silently dropping every other persisted field the original
// entry carried -- e.g. `{git: acme/foo, ref: stable}` became a bare
// `acme/foo` scalar after `install --dev acme/foo`. The fix transplants the
// original entry node instead of rebuilding one.
func TestRunInstall_Dev_CrossSectionMove_PreservesEntryMetadata(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - git: acme/foo\n      ref: stable\n"), 0644)

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &mockInstallLoader{},
		dev:    true,
	}
	if err := runInstall(deps, false, true, "claude", nil, []string{"acme/foo"}); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	manifestBytes, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	got := string(manifestBytes)
	if !strings.Contains(got, "ref: stable") {
		t.Errorf("expected ref: stable to survive the cross-section move, got:\n%s", got)
	}

	node, err := yamlcore.SafeLoad(manifestBytes)
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if len(m.ParsedDeps) != 0 {
		t.Errorf("expected zero dependencies.apm entries after move, got %+v", m.ParsedDeps)
	}
	if len(m.ParsedDevDeps) != 1 || m.ParsedDevDeps[0].RepoURL != "acme/foo" || m.ParsedDevDeps[0].Reference != "stable" {
		t.Errorf("expected exactly one devDependencies.apm entry acme/foo@stable, got %+v", m.ParsedDevDeps)
	}
}

// TestRunInstall_Dev_MoveOutOfDevWithLegacyLock_StillPersistsManifestMove is
// the 2026-07-30 Tier-2 regression for the OTHER half of the same finding:
// deployAndFinalize's no-op check (lockfile.IsSemanticEqual alone) can be
// fooled by a LEGACY lockfile whose package_type was never populated by an
// apm-go build predating R9.4 -- moving a dependency OUT of devDependencies
// recomputes package_type as "", which happens to equal the legacy lock's
// (incorrectly blank) existing value too, so IsSemanticEqual reports "no
// change" even though the manifest section move genuinely needs to be
// written. manifestSectionMoved must gate the no-op check independently of
// lockfile equality.
func TestRunInstall_Dev_MoveOutOfDevWithLegacyLock_StillPersistsManifestMove(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndevDependencies:\n  apm:\n    - acme/foo\n"), 0644)

	// Seed a REAL lockfile via a --dev install, so its content (resolved
	// ref, deployed files/hashes, etc.) is exactly what a second, identical
	// resolve/deploy will reproduce -- the only field this test then
	// deliberately corrupts is package_type, to simulate a pre-R9.4 binary
	// that never wrote it at all.
	seedDeps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &mockInstallLoader{},
		dev:    true,
	}
	if err := runInstall(seedDeps, false, true, "claude", nil, []string{"acme/foo"}); err != nil {
		t.Fatalf("seed runInstall: %v", err)
	}

	lockBytes, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.Replace(string(lockBytes), "    package_type: marketplace_plugin\n", "", 1)
	if legacy == string(lockBytes) {
		t.Fatalf("fixture setup: expected package_type: marketplace_plugin in seeded apm.lock.yaml, got:\n%s", string(lockBytes))
	}
	if err := os.WriteFile("apm.lock.yaml", []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	// Second run, WITHOUT --dev, on the SAME already-declared dependency:
	// existingIsDev[key]=true (apm.yml still says devDependencies) !=
	// deps.dev=false triggers the section move.
	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &mockInstallLoader{},
	}
	if err := runInstall(deps, false, true, "claude", nil, []string{"acme/foo"}); err != nil {
		t.Fatalf("second runInstall: %v", err)
	}

	manifestBytes, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	node, err := yamlcore.SafeLoad(manifestBytes)
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	// The 2026-07-30 decision (install.go's persistPackagesToManifest) keeps
	// an emptied-out section as a normal `devDependencies: {apm: []}`
	// skeleton rather than deleting the key -- assert on the PARSED
	// structure, not string absence of "devDependencies:".
	if len(m.ParsedDevDeps) != 0 {
		t.Errorf("expected zero devDependencies.apm entries after the move, got %+v", m.ParsedDevDeps)
	}
	if len(m.ParsedDeps) != 1 || m.ParsedDeps[0].RepoURL != "acme/foo" {
		t.Errorf("expected exactly one dependencies.apm entry acme/foo (the persisted move), got %+v", m.ParsedDeps)
	}
}

// TestRunInstall_DevWithFrozen_Errors is the 2026-07-30 Tier-2 regression
// mirroring TestRunInstall_SkillWithFrozen_Errors: frozen installs return
// from the separate "3. Frozen install" branch well before
// persistPackagesToManifest is ever reached, so `--frozen --dev X` against
// an already-declared dependency previously printed a misleading "moved
// from ... to devDependencies.apm" message (moveDependencyBetweenSections'
// pure in-memory mutation, made before frozen mode is checked) and then
// silently discarded it.
func TestRunInstall_DevWithFrozen_Errors(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte("version: \"1\"\ndependencies: []\n"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}, dev: true}
	err := runInstall(deps, true, true, "", nil, []string{"acme/foo"})
	if err == nil || !strings.Contains(err.Error(), "--dev") || !strings.Contains(err.Error(), "frozen") {
		t.Fatalf("expected a --dev+frozen error, got %v", err)
	}
}

// TestRunInstall_DevWithoutPackages_Errors mirrors
// TestRunInstall_SkillWildcardWithoutPackages_Errors: --dev alone (no
// positional package to route into devDependencies.apm) is rejected up
// front rather than silently no-op-ing.
func TestRunInstall_DevWithoutPackages_Errors(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}, dev: true}
	err := runInstall(deps, false, true, "", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "--dev") {
		t.Fatalf("expected a --dev error for --dev with no positional package, got %v", err)
	}
}
