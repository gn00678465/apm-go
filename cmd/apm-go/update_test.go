package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/semver"
)

// TestUpdateCmd_DryRunFlag locks P0 #5's CLI wiring (register §2/§3.3
// D-1/§5): --dry-run must exist and its rendered --help line (including
// cobra's column alignment) must promise both a plan/preview and no
// mutation, so wording drift is caught immediately.
func TestUpdateCmd_DryRunFlag(t *testing.T) {
	cmd := updateCmd()
	if cmd.Flags().Lookup("dry-run") == nil {
		t.Fatal("update is missing --dry-run")
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("update --help returned error: %v", err)
	}
	out := buf.String()

	const dryRunHelpLine = "--dry-run     preview the update plan without applying it: no apm.lock.yaml write, no apm_modules/ mutation, no target deploy"
	if !strings.Contains(out, dryRunHelpLine) {
		t.Errorf("update --help output missing the --dry-run flag line %q:\n%s", dryRunHelpLine, out)
	}
}

func TestRunUpdate_Full_ReResolvesToNewestAndRewritesLock(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// target: claude (07-11-update-local-deps C2): this test's apm.yml has
	// deps but, before this task, no target: field and no auto-detected
	// harness signal dir -- runUpdate's new zero-target gate would otherwise
	// exit 2 here instead of exercising the git-semver re-resolution this
	// test actually targets.
	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ntarget:\n  - claude\ndependencies:\n  apm:\n    - acme/a#^1.0.0\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte("lockfile_version: \"1\"\ndependencies:\n  - repo_url: acme/a\n    source: git\n    constraint: \"^1.0.0\"\n    resolved_tag: v1.2.0\n    depth: 1\n"), 0644)

	deps := &installDeps{
		tags: &mockInstallTagLister{tags: map[string][]semver.TagInfo{
			"acme/a": {{Name: "v1.2.0"}, {Name: "v1.5.0"}, {Name: "v1.9.0"}},
		}},
		loader: &mockInstallLoader{packages: map[string]*manifest.Manifest{
			"acme/a@v1.9.0": {Name: "a", Version: "1.9.0"},
		}},
	}

	if err := runUpdate(deps, false, false, "", false); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}

	data, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "v1.9.0") {
		t.Errorf("expected lockfile to be rewritten with v1.9.0, got:\n%s", data)
	}
	if strings.Contains(string(data), "v1.2.0") {
		t.Errorf("expected old pin v1.2.0 to be gone, got:\n%s", data)
	}
}

func TestRunUpdate_Scoped_OnlyNamedPackageChanges(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// target: claude (07-11-update-local-deps C2): see the identical comment
	// in TestRunUpdate_Full_ReResolvesToNewestAndRewritesLock above.
	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ntarget:\n  - claude\ndependencies:\n  apm:\n    - acme/a#^1.0.0\n    - acme/b#^2.0.0\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte(
		"lockfile_version: \"1\"\ndependencies:\n"+
			"  - repo_url: acme/a\n    source: git\n    constraint: \"^1.0.0\"\n    resolved_tag: v1.2.0\n    depth: 1\n"+
			"  - repo_url: acme/b\n    source: git\n    constraint: \"^2.0.0\"\n    resolved_tag: v2.0.0\n    depth: 1\n"), 0644)

	deps := &installDeps{
		tags: &mockInstallTagLister{tags: map[string][]semver.TagInfo{
			"acme/a": {{Name: "v1.2.0"}, {Name: "v1.5.0"}, {Name: "v1.9.0"}},
			"acme/b": {{Name: "v2.0.0"}, {Name: "v2.5.0"}},
		}},
		loader: &mockInstallLoader{packages: map[string]*manifest.Manifest{
			"acme/a@v1.9.0": {Name: "a", Version: "1.9.0"},
			"acme/b@v2.0.0": {Name: "b", Version: "2.0.0"},
		}},
	}

	if err := runUpdate(deps, false, false, "acme/a", false); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}

	data, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "v1.9.0") {
		t.Errorf("expected acme/a to be re-resolved to v1.9.0, got:\n%s", content)
	}
	// req-rs-012: everything outside the named package's scope must hold its
	// original pin -- acme/b must NOT have moved to v2.5.0.
	if !strings.Contains(content, "v2.0.0") {
		t.Errorf("expected acme/b to keep its original pin v2.0.0, got:\n%s", content)
	}
	if strings.Contains(content, "v2.5.0") {
		t.Errorf("expected acme/b NOT to be re-resolved to v2.5.0 (out of scope), got:\n%s", content)
	}
}

func TestRunUpdate_Scoped_FrozenRefusedWithoutOverride(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/a#^1.0.0\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte("lockfile_version: \"1\"\ndependencies:\n  - repo_url: acme/a\n    source: git\n    constraint: \"^1.0.0\"\n    resolved_tag: v1.2.0\n    depth: 1\n"), 0644)

	// A pre-existing checkout marker: the frozen refusal must happen before
	// any apm_modules clearing, so a refused update leaves disk untouched.
	markerPath := filepath.Join("apm_modules", "acme", "a", "marker.txt")
	os.MkdirAll(filepath.Dir(markerPath), 0755)
	os.WriteFile(markerPath, []byte("must survive a refused update"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	err := runUpdate(deps, true, false, "acme/a", false)
	if err == nil {
		t.Fatal("expected error for scoped update against a frozen install without override")
	}
	if !strings.Contains(err.Error(), "frozen") {
		t.Errorf("error should mention frozen: %v", err)
	}
	if _, statErr := os.Stat(markerPath); statErr != nil {
		t.Errorf("refused update must not touch apm_modules: marker gone: %v", statErr)
	}
}

func TestRunUpdate_Scoped_CIAutoFrozenRefused(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	t.Setenv("CI", "true")

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/a#^1.0.0\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte("lockfile_version: \"1\"\ndependencies:\n  - repo_url: acme/a\n    source: git\n    constraint: \"^1.0.0\"\n    resolved_tag: v1.2.0\n    depth: 1\n"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	err := runUpdate(deps, false, false, "acme/a", false)
	if err == nil {
		t.Fatal("expected CI environment to auto-refuse a scoped update without --no-frozen")
	}
	if !strings.Contains(err.Error(), "frozen") {
		t.Errorf("error should mention frozen: %v", err)
	}
}

func TestRunUpdate_Scoped_NoFrozenOverridesCIAutoFrozen(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	t.Setenv("CI", "true")

	// target: claude (07-11-update-local-deps C2): see the identical comment
	// in TestRunUpdate_Full_ReResolvesToNewestAndRewritesLock above.
	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ntarget:\n  - claude\ndependencies:\n  apm:\n    - acme/a#^1.0.0\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte("lockfile_version: \"1\"\ndependencies:\n  - repo_url: acme/a\n    source: git\n    constraint: \"^1.0.0\"\n    resolved_tag: v1.2.0\n    depth: 1\n"), 0644)

	deps := &installDeps{
		tags: &mockInstallTagLister{tags: map[string][]semver.TagInfo{
			"acme/a": {{Name: "v1.2.0"}, {Name: "v1.9.0"}},
		}},
		loader: &mockInstallLoader{packages: map[string]*manifest.Manifest{
			"acme/a@v1.9.0": {Name: "a", Version: "1.9.0"},
		}},
	}

	if err := runUpdate(deps, false, true, "acme/a", false); err != nil {
		t.Fatalf("--no-frozen should override CI auto-frozen detection: %v", err)
	}
}

func TestRunUpdate_GitSemver_InstallPathClearedEvenWhenTagUnchanged(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// target: claude (07-11-update-local-deps C2): see the identical comment
	// in TestRunUpdate_Full_ReResolvesToNewestAndRewritesLock above.
	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ntarget:\n  - claude\ndependencies:\n  apm:\n    - acme/a#^1.0.0\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte("lockfile_version: \"1\"\ndependencies:\n  - repo_url: acme/a\n    source: git\n    constraint: \"^1.0.0\"\n    resolved_tag: v1.0.0\n    depth: 1\n"), 0644)

	// Only one tag exists, so the update re-resolves to the SAME tag --
	// req-lk-010 requires the download path to rerun anyway.
	deps := &installDeps{
		tags: &mockInstallTagLister{tags: map[string][]semver.TagInfo{
			"acme/a": {{Name: "v1.0.0"}},
		}},
		loader: &mockInstallLoader{},
	}

	markerPath := filepath.Join("apm_modules", "acme", "a", "marker.txt")
	os.MkdirAll(filepath.Dir(markerPath), 0755)
	os.WriteFile(markerPath, []byte("stale content from before"), 0644)

	if err := runUpdate(deps, false, false, "", false); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}

	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Errorf("expected apm_modules/acme/a to be cleared before re-resolution (req-lk-010), marker still present (stat err: %v)", err)
	}
}

// TestRunUpdate_GitSemver_DevDependency_InstallPathClearedEvenWhenTagUnchanged
// is the F3-adjacent decision test for `apm update` (bare/full): Python's
// `apm update` resolves against apm_deps + dev_apm_deps, so a
// devDependencies.apm git-semver entry must get the same req-lk-010
// cache-clear-before-resolve treatment as an ordinary dependencies.apm entry
// -- not silently skipped because directGitSemverUpdateScope only walked
// m.ParsedDeps.
func TestRunUpdate_GitSemver_DevDependency_InstallPathClearedEvenWhenTagUnchanged(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// target: claude (07-11-update-local-deps C2): see the identical comment
	// in TestRunUpdate_Full_ReResolvesToNewestAndRewritesLock above.
	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ntarget:\n  - claude\ndevDependencies:\n  apm:\n    - acme/a#^1.0.0\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte("lockfile_version: \"1\"\ndependencies:\n  - repo_url: acme/a\n    source: git\n    constraint: \"^1.0.0\"\n    resolved_tag: v1.0.0\n    depth: 1\n"), 0644)

	// Only one tag exists, so the update re-resolves to the SAME tag --
	// req-lk-010 requires the download path to rerun anyway.
	deps := &installDeps{
		tags: &mockInstallTagLister{tags: map[string][]semver.TagInfo{
			"acme/a": {{Name: "v1.0.0"}},
		}},
		loader: &mockInstallLoader{},
	}

	markerPath := filepath.Join("apm_modules", "acme", "a", "marker.txt")
	os.MkdirAll(filepath.Dir(markerPath), 0755)
	os.WriteFile(markerPath, []byte("stale content from before"), 0644)

	if err := runUpdate(deps, false, false, "", false); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}

	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Errorf("expected apm_modules/acme/a (dev dependency) to be cleared before re-resolution (req-lk-010), marker still present (stat err: %v)", err)
	}
}

// TestRunUpdate_RefusesVirtualPathEscapingApmModules guards the purge path
// in directGitSemverUpdateScope/runUpdate: virtual_path is only charset
// validated at parse time (unlike local-path deps, which reject ".."
// explicitly), so a crafted "path: ../../../evil" could otherwise resolve
// os.RemoveAll's target outside apm_modules entirely.
func TestRunUpdate_RefusesVirtualPathEscapingApmModules(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// manifest.validateVirtualPath only charset-checks each path segment (it
	// does not reject ".." the way lockfile.validatePathComponent does), so
	// apm.yml is the vulnerable input surface here -- the lockfile entry
	// deliberately stays benign since directGitSemverUpdateScope reads from
	// m.ParsedDeps (apm.yml), not the lockfile.
	os.WriteFile("apm.yml", []byte(
		"name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n"+
			"    - git: acme/a\n      ref: \"^1.0.0\"\n      path: \"../../../evil\"\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte("lockfile_version: \"1\"\ndependencies:\n  - repo_url: acme/a\n    source: git\n    constraint: \"^1.0.0\"\n    resolved_tag: v1.2.0\n    depth: 1\n"), 0644)

	// Canary file OUTSIDE apm_modules, at the escape target -- must survive.
	os.MkdirAll("evil", 0755)
	canary := filepath.Join("evil", "marker.txt")
	os.WriteFile(canary, []byte("must not be deleted"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	err := runUpdate(deps, false, false, "", false)
	if err == nil {
		t.Fatal("expected an error refusing to clear a path outside apm_modules")
	}
	if !strings.Contains(err.Error(), "apm_modules") {
		t.Errorf("error should mention apm_modules: %v", err)
	}
	if _, statErr := os.Stat(canary); statErr != nil {
		t.Errorf("canary file outside apm_modules must survive: %v", statErr)
	}
}

// TestRunUpdate_RefusesParentSegmentStayingInsideApmModules covers the
// narrower case archive.Contained alone misses: a ".." that resolves to a
// DIFFERENT, unrelated directory that is still technically inside
// apm_modules (e.g. a sibling package, or apm_modules itself), rather than
// escaping the root entirely. Contained("apm_modules", target) reports true
// for anything still under apm_modules, so this must be rejected earlier,
// before filepath.Join/Clean resolves the ".." away.
func TestRunUpdate_RefusesParentSegmentStayingInsideApmModules(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// "acme/a" + path ".." cleans to "apm_modules/acme" -- a real sibling
	// directory inside apm_modules, not the intended "acme/a" checkout.
	os.WriteFile("apm.yml", []byte(
		"name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n"+
			"    - git: acme/a\n      ref: \"^1.0.0\"\n      path: \"..\"\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte("lockfile_version: \"1\"\ndependencies:\n  - repo_url: acme/a\n    source: git\n    constraint: \"^1.0.0\"\n    resolved_tag: v1.2.0\n    depth: 1\n"), 0644)

	// A sibling package under apm_modules/acme that must NOT be swept up by
	// an over-broad RemoveAll("apm_modules/acme").
	siblingMarker := filepath.Join("apm_modules", "acme", "other-package", "marker.txt")
	os.MkdirAll(filepath.Dir(siblingMarker), 0755)
	os.WriteFile(siblingMarker, []byte("unrelated package, must survive"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	err := runUpdate(deps, false, false, "", false)
	if err == nil {
		t.Fatal("expected an error refusing to clear a \"..\"-containing path")
	}
	if !strings.Contains(err.Error(), "..") {
		t.Errorf("error should mention the \"..\" segment: %v", err)
	}
	if _, statErr := os.Stat(siblingMarker); statErr != nil {
		t.Errorf("sibling package under apm_modules must survive: %v", statErr)
	}
}

// TestRunUpdate_RegistryDevDependency_RequiresExperimentalFlag proves the
// registries-experimental gate in runUpdate (which explicitly documents
// itself as "mirrors runInstall's gate") also scans devDependencies.apm, not
// just dependencies.apm -- a registry-sourced dev dependency must still be
// refused pre-network when the "registries" experimental flag isn't
// enabled, exactly like a registry-sourced prod dependency.
func TestRunUpdate_RegistryDevDependency_RequiresExperimentalFlag(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Isolated config dir, guaranteed to have the "registries" experimental
	// flag disabled (default off) -- proves the gate actually runs, rather
	// than relying on the ambient environment.
	t.Setenv("APM_CONFIG_DIR", t.TempDir())

	os.WriteFile("apm.yml", []byte(
		"name: test\nversion: \"1.0.0\"\nregistries:\n  local:\n    url: http://127.0.0.1:1\n  default: local\n"+
			"devDependencies:\n  apm:\n    - id: acme/sample\n      version: 1.0.0\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte("lockfile_version: \"2\"\ndependencies:\n  - repo_url: acme/sample\n    source: registry\n    version: 1.0.0\n    depth: 1\n"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runUpdate(deps, false, false, "", false)
	if err == nil {
		t.Fatal("expected an error for a registry-sourced devDependencies.apm entry without the registries experimental flag enabled")
	}
	if !strings.Contains(err.Error(), "experimental feature") {
		t.Errorf("expected the registries-experimental-gate error (gate likely skipped the dev dependency and hit a different failure instead), got: %v", err)
	}
}

func TestRunUpdate_NoManifest(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runUpdate(deps, false, false, "", false)
	if err == nil {
		t.Fatal("expected error when apm.yml is missing")
	}
	if !strings.Contains(err.Error(), "apm.yml") {
		t.Errorf("error should mention apm.yml: %v", err)
	}
}

func TestRunUpdate_NoLockfile(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/a#^1.0.0\n"), 0644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runUpdate(deps, false, false, "", false)
	if err == nil {
		t.Fatal("expected error when apm.lock.yaml is missing")
	}
	if !strings.Contains(err.Error(), "apm.lock.yaml") {
		t.Errorf("error should mention apm.lock.yaml: %v", err)
	}
}

// TestUpdate_RespectsSkillSubset is BUG-2's `apm update` regression
// (prd.md AC-B2-5, design.md §1.2c): `update` has no --skill flag and no
// per-call requested-package concept, but it MUST still respect a
// dependency's already-persisted apm.yml skills: subset when it re-deploys
// -- before this fix, `update`'s buildLockfile/deployAndFinalize calls were
// always passed a nil skillSubset/requestedKeys with no substitute
// (effectiveSkillSubsets), so a bare `apm update` silently redeployed every
// skill in the bundle, forgetting the subset an earlier `apm install
// --skill` call had persisted.
func TestUpdate_RespectsSkillSubset(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	remotesDir := filepath.Join(dir, "remotes")
	if err := os.MkdirAll(remotesDir, 0755); err != nil {
		t.Fatal(err)
	}
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

	if err := runInstall(deps, false, true, "claude", []string{"skillX"}, []string{repoR}); err != nil {
		t.Fatalf("install repo-r --skill skillX: %v", err)
	}
	for _, p := range expectedSkillDeployPaths("skillX", []string{"SKILL.md", "notes.md"}) {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
			t.Fatalf("after install: expected %s to exist: %v", p, err)
		}
	}
	for _, p := range expectedSkillDeployPaths("skillY", []string{"SKILL.md", "notes.md"}) {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err == nil {
			t.Fatalf("after install: %s must NOT exist (skillY was never selected)", p)
		}
	}

	if err := runUpdate(deps, false, false, "", false); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}

	// The regression check: a bare `apm update` must not forget the
	// persisted --skill skillX subset.
	for _, p := range expectedSkillDeployPaths("skillX", []string{"SKILL.md", "notes.md"}) {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
			t.Errorf("after update: expected %s to still exist: %v", p, err)
		}
	}
	for _, p := range expectedSkillDeployPaths("skillY", []string{"SKILL.md", "notes.md"}) {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err == nil {
			t.Errorf("POLLUTION (BUG-2, update path): %s must NOT exist -- `apm update` must not forget the persisted --skill skillX subset", p)
		}
	}

	lock := readLockfile(t)
	if len(lock.Dependencies) != 1 {
		t.Fatalf("expected exactly ONE lockfile dependency after update, got %d", len(lock.Dependencies))
	}
	if got := lock.Dependencies[0].SkillSubset; len(got) != 1 || got[0] != "skillX" {
		t.Errorf("apm.lock.yaml skill_subset after update = %v, want [skillX]", got)
	}
}
