package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/marketplace"
)

// writeLocalMarketplacePlugin lays out, under root, a local marketplace whose
// single plugin "hello" sources a plain (non-git) directory containing a
// skill. Returns the marketplace directory (the one passed to `marketplace
// add`). The plugin directory is a plain folder -- NOT a git repo -- which is
// exactly why the fix must COPY it into apm_modules (Python's local-dependency
// model) rather than `git clone` it.
func writeLocalMarketplacePlugin(t *testing.T, mktDir string) {
	t.Helper()
	if err := os.MkdirAll(mktDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mktDir, "marketplace.json"),
		[]byte(`{"name": "acme", "plugins": [{"name": "hello", "source": "./hello-pkg"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(mktDir, "hello-pkg", "skills", "hello")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# hello skill"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRunInstall_LocalMarketplacePlugin_E2E_InTree is the NON-MOCKED end-to-end
// proof of the F1 fix: it drives the real cmd/apm-go -> resolver ->
// RealPackageLoader -> deploy pipeline (no mock loader) for
// `install hello@<local-marketplace>` where the marketplace lives INSIDE the
// project tree. The mocked unit tests could not catch the gap this covers:
// the real loader must materialize an absolute-path local dependency into a
// VALID, CONTAINED apm_modules subdir (previously it built
// `apm_modules\C:\Users\...` and `git clone` failed with "could not create
// leading directories"). Asserts the skill actually DEPLOYS to
// .claude/skills/hello/SKILL.md and apm.lock.yaml is written, then that a
// second bare `apm install` re-loads and re-deploys without error.
func TestRunInstall_LocalMarketplacePlugin_E2E_InTree(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)
	t.Setenv("APM_CONFIG_DIR", t.TempDir())

	mktDir := filepath.Join(dir, "vendor", "mkt")
	writeLocalMarketplacePlugin(t, mktDir)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: mktDir}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"},
	}

	// (a) first install: real loader must copy the plugin dir into apm_modules
	// and deploy the skill.
	if err := runInstall(deps, false, true, "claude", nil, []string{"hello@acme"}); err != nil {
		t.Fatalf("(a) runInstall: %v", err)
	}
	deployed := filepath.Join(dir, ".claude", "skills", "hello", "SKILL.md")
	if _, err := os.Stat(deployed); err != nil {
		t.Fatalf("(a) expected skill deployed to .claude/skills/hello/SKILL.md: %v", err)
	}
	if _, err := os.Stat("apm.lock.yaml"); err != nil {
		t.Fatalf("(a) expected apm.lock.yaml written: %v", err)
	}

	// (b) round-trip: bare install re-reads the persisted relative apm.yml path
	// and re-deploys with no error.
	if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
		t.Fatalf("(b) bare runInstall (round-trip): %v", err)
	}
	if _, err := os.Stat(deployed); err != nil {
		t.Fatalf("(b) expected skill still deployed after round-trip: %v", err)
	}
}

// TestRunInstall_LocalMarketplacePlugin_E2E_OutOfTree is the same non-mocked
// e2e proof for a marketplace OUTSIDE the project tree, where apm.yml persists
// an ABSOLUTE dependency path. Proves both the absolute-path persist and its
// materialization + deploy + round-trip end to end.
func TestRunInstall_LocalMarketplacePlugin_E2E_OutOfTree(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)
	t.Setenv("APM_CONFIG_DIR", t.TempDir())

	// Sibling temp dir -> outside the project tree -> apm.yml persists absolute.
	mktDir := t.TempDir()
	writeLocalMarketplacePlugin(t, mktDir)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: mktDir}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"},
	}

	if err := runInstall(deps, false, true, "claude", nil, []string{"hello@acme"}); err != nil {
		t.Fatalf("(a) runInstall: %v", err)
	}
	deployed := filepath.Join(dir, ".claude", "skills", "hello", "SKILL.md")
	if _, err := os.Stat(deployed); err != nil {
		t.Fatalf("(a) expected skill deployed to .claude/skills/hello/SKILL.md: %v", err)
	}
	if _, err := os.Stat("apm.lock.yaml"); err != nil {
		t.Fatalf("(a) expected apm.lock.yaml written: %v", err)
	}

	// Round-trip re-reads the persisted absolute apm.yml path.
	if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
		t.Fatalf("(b) bare runInstall (round-trip): %v", err)
	}
	if _, err := os.Stat(deployed); err != nil {
		t.Fatalf("(b) expected skill still deployed after round-trip: %v", err)
	}
}
