package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/manifest"
)

// Issue #45: a frozen install verifies deployed files under the deploy root
// and never sends a local-path lock entry to the package loader.

const (
	frozenRootAgent    = "---\nname: a\ndescription: d\n---\nbody\n"
	frozenRootTampered = "---\nname: a\ndescription: d\n---\ntampered\n"
	frozenRootSuccess  = "Frozen install: all dependencies pinned and verified"
)

var frozenRootPackage = map[string]string{
	".apm/agents/a.md":        frozenRootAgent,
	".apm/skills/s1/SKILL.md": "---\nname: s1\ndescription: d\n---\nskill\n",
	".apm/skills/s2/SKILL.md": "---\nname: s2\ndescription: d\n---\nskill\n",
}

var frozenRootDeployed = []string{
	".claude/agents/a.md",
	".claude/skills/s1/SKILL.md",
	".claude/skills/s2/SKILL.md",
}

// frozenRootLoader fails the test when a frozen install asks it for a package:
// the real loader would delete the local copy and then clone from the network.
type frozenRootLoader struct{ t *testing.T }

func (l frozenRootLoader) LoadPackage(ref *manifest.DependencyReference, _ string) (*manifest.Manifest, error) {
	l.t.Errorf("frozen install sent %s to the package loader", ref.RepoURL)
	return nil, errors.New("loader must not run")
}

func frozenRootGlobalInstall(t *testing.T, frozen bool) (string, error) {
	t.Helper()
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: frozenRootLoader{t}}
	var runErr error
	out := captureUninstallStdout(t, func() {
		deployDir, err := enterGlobalScope()
		if err != nil {
			t.Fatal(err)
		}
		runErr = runInstall(deps, frozen, true, "claude", deployDir, nil, nil)
	})
	return out, runErr
}

func frozenRootLocalModule(t *testing.T, projectDir string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(projectDir, "apm_modules", "_local"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("apm_modules/_local has %d entries, want 1", len(entries))
	}
	return filepath.Join(projectDir, "apm_modules", "_local", entries[0].Name())
}

func assertFrozenRootIntact(t *testing.T, home string) {
	t.Helper()
	module := frozenRootLocalModule(t, filepath.Join(home, ".apm"))
	if got := globalStaleRead(t, filepath.Join(module, ".apm", "agents", "a.md")); got != frozenRootAgent {
		t.Errorf("module copy of a.md = %q, want %q", got, frozenRootAgent)
	}
	for _, rel := range frozenRootDeployed {
		want := frozenRootPackage[".apm/"+strings.TrimPrefix(rel, ".claude/")]
		if got := globalStaleRead(t, filepath.Join(home, filepath.FromSlash(rel))); got != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
}

func TestInstallGlobal_Frozen_UnchangedInstallPasses(t *testing.T) {
	home, src := globalStaleScope(t, frozenRootPackage)
	globalStaleInstall(t, "claude", src)

	out, err := frozenRootGlobalInstall(t, true)

	if err != nil {
		t.Fatalf("frozen install: %v", err)
	}
	if !strings.Contains(out, frozenRootSuccess) {
		t.Errorf("missing success line:\n%s", out)
	}
	assertFrozenRootIntact(t, home)
}

func TestInstallGlobal_CIDefaultsToFrozen_UnchangedInstallPasses(t *testing.T) {
	home, src := globalStaleScope(t, frozenRootPackage)
	globalStaleInstall(t, "claude", src)
	t.Setenv("CI", "1")

	out, err := frozenRootGlobalInstall(t, false)

	if err != nil {
		t.Fatalf("install under CI: %v", err)
	}
	if !strings.Contains(out, "CI environment detected, defaulting to frozen install") {
		t.Errorf("missing CI line:\n%s", out)
	}
	if !strings.Contains(out, frozenRootSuccess) {
		t.Errorf("missing success line:\n%s", out)
	}
	assertFrozenRootIntact(t, home)
}

func TestInstallGlobal_Frozen_ReportsObservedHashOfChangedContent(t *testing.T) {
	home, src := globalStaleScope(t, frozenRootPackage)
	globalStaleInstall(t, "claude", src)
	module := frozenRootLocalModule(t, filepath.Join(home, ".apm"))
	writeGlobalStaleFile(t, filepath.Join(module, ".apm", "agents", "a.md"), frozenRootTampered)

	_, err := frozenRootGlobalInstall(t, true)

	const want = "frozen install: content-integrity violation: .claude/agents/a.md" +
		" expected sha256:d69d5d87e63020276ead84a8093adbf3a8c6cb6fff5b5f2eb5fb2b0174eb20ce," +
		" observed sha256:b042cbaf6f9a8618d9637956a2ef782316a118fc383a3ce5f0690d6919117f9d"
	if err == nil || err.Error() != want {
		t.Fatalf("frozen install error = %v\nwant %s", err, want)
	}
}

func TestInstallGlobal_Frozen_ReportsFirstPathInSortedOrder(t *testing.T) {
	home, src := globalStaleScope(t, frozenRootPackage)
	globalStaleInstall(t, "claude", src)
	for _, rel := range []string{".claude/skills/s2", ".claude/skills/s1", ".claude/agents/a.md"} {
		if err := os.Remove(filepath.Join(home, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}

	const want = "frozen install: content-integrity violation: .claude/agents/a.md" +
		" expected sha256:d69d5d87e63020276ead84a8093adbf3a8c6cb6fff5b5f2eb5fb2b0174eb20ce," +
		" observed <missing>"
	for i := 0; i < 20; i++ {
		_, err := frozenRootGlobalInstall(t, true)
		if err == nil || err.Error() != want {
			t.Fatalf("run %d: frozen install error = %v\nwant %s", i, err, want)
		}
	}
}

func TestInstall_Frozen_ProjectScopeKeepsLocalModule(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: proj\nversion: 0.0.0\n")
	src := filepath.Join(t.TempDir(), "dep")
	writeGlobalStaleFile(t, filepath.Join(src, "apm.yml"), "name: dep\nversion: 1.0.0\n")
	for rel, content := range frozenRootPackage {
		writeGlobalStaleFile(t, filepath.Join(src, filepath.FromSlash(rel)), content)
	}
	install := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	captureUninstallStdout(t, func() {
		if err := runInstall(install, false, true, "claude", "", nil, []string{src}); err != nil {
			t.Fatalf("install: %v", err)
		}
	})

	frozen := &installDeps{tags: &mockInstallTagLister{}, loader: frozenRootLoader{t}}
	var err error
	out := captureUninstallStdout(t, func() {
		err = runInstall(frozen, true, true, "claude", "", nil, nil)
	})

	if err != nil {
		t.Fatalf("frozen install: %v", err)
	}
	if !strings.Contains(out, frozenRootSuccess) {
		t.Errorf("missing success line:\n%s", out)
	}
	module := frozenRootLocalModule(t, project)
	if got := globalStaleRead(t, filepath.Join(module, ".apm", "agents", "a.md")); got != frozenRootAgent {
		t.Errorf("module copy of a.md = %q, want %q", got, frozenRootAgent)
	}
}
