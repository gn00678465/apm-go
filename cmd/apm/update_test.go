package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/semver"
)

func TestRunUpdate_Full_ReResolvesToNewestAndRewritesLock(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/a#^1.0.0\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte("lockfile_version: \"1\"\ndependencies:\n  - repo_url: acme/a\n    source: git\n    constraint: \"^1.0.0\"\n    resolved_tag: v1.2.0\n    depth: 1\n"), 0644)

	deps := &installDeps{
		tags: &mockInstallTagLister{tags: map[string][]semver.TagInfo{
			"acme/a": {{Name: "v1.2.0"}, {Name: "v1.5.0"}, {Name: "v1.9.0"}},
		}},
		loader: &mockInstallLoader{packages: map[string]*manifest.Manifest{
			"acme/a@v1.9.0": {Name: "a", Version: "1.9.0"},
		}},
	}

	if err := runUpdate(deps, false, false, ""); err != nil {
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

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/a#^1.0.0\n    - acme/b#^2.0.0\n"), 0644)
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

	if err := runUpdate(deps, false, false, "acme/a"); err != nil {
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

	err := runUpdate(deps, true, false, "acme/a")
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

	err := runUpdate(deps, false, false, "acme/a")
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

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/a#^1.0.0\n"), 0644)
	os.WriteFile("apm.lock.yaml", []byte("lockfile_version: \"1\"\ndependencies:\n  - repo_url: acme/a\n    source: git\n    constraint: \"^1.0.0\"\n    resolved_tag: v1.2.0\n    depth: 1\n"), 0644)

	deps := &installDeps{
		tags: &mockInstallTagLister{tags: map[string][]semver.TagInfo{
			"acme/a": {{Name: "v1.2.0"}, {Name: "v1.9.0"}},
		}},
		loader: &mockInstallLoader{packages: map[string]*manifest.Manifest{
			"acme/a@v1.9.0": {Name: "a", Version: "1.9.0"},
		}},
	}

	if err := runUpdate(deps, false, true, "acme/a"); err != nil {
		t.Fatalf("--no-frozen should override CI auto-frozen detection: %v", err)
	}
}

func TestRunUpdate_GitSemver_InstallPathClearedEvenWhenTagUnchanged(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/a#^1.0.0\n"), 0644)
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

	if err := runUpdate(deps, false, false, ""); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}

	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Errorf("expected apm_modules/acme/a to be cleared before re-resolution (req-lk-010), marker still present (stat err: %v)", err)
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

	err := runUpdate(deps, false, false, "")
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

	err := runUpdate(deps, false, false, "")
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

func TestRunUpdate_NoManifest(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runUpdate(deps, false, false, "")
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
	err := runUpdate(deps, false, false, "")
	if err == nil {
		t.Fatal("expected error when apm.lock.yaml is missing")
	}
	if !strings.Contains(err.Error(), "apm.lock.yaml") {
		t.Errorf("error should mention apm.lock.yaml: %v", err)
	}
}
