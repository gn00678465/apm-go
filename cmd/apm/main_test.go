package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

func oraclePath(t *testing.T, rel string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(wd, "..", "..")
	candidates := []string{
		filepath.Join(root, "conformance-kit", "oracle", rel),
		filepath.Join(root, "..", "conformance-kit", "oracle", rel),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skipf("oracle fixture not found: %s", rel)
	return ""
}

// ── init tests ──

func TestInitCmd_BasicCreateAndValidate(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := initCmd()
	cmd.SetArgs([]string{"--name", "test-project", "--version", "1.0.0", "--target", "claude"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if err != nil {
		t.Fatal(err)
	}

	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatalf("SafeLoad failed: %v", err)
	}
	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if m.Name != "test-project" {
		t.Errorf("name = %q, want %q", m.Name, "test-project")
	}
	if m.Version != "1.0.0" {
		t.Errorf("version = %q", m.Version)
	}

	content := string(data)
	if strings.Contains(content, "minimal") {
		t.Error("init output must not contain 'minimal'")
	}
	if strings.Contains(content, "targets:") {
		t.Error("init output must not contain 'targets:'")
	}
}

func TestInitCmd_SpecialCharName(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := initCmd()
	cmd.SetArgs([]string{"--name", "foo #bar", "--version", "1.0.0"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "apm.yml"))
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "foo #bar" {
		t.Errorf("name = %q, want %q", m.Name, "foo #bar")
	}
}

func TestInitCmd_NumericName(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := initCmd()
	cmd.SetArgs([]string{"--name", "123", "--version", "1.0.0"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "apm.yml"))
	node, _ := yamlcore.SafeLoad(data)
	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "123" {
		t.Errorf("name = %q, want %q", m.Name, "123")
	}
}

func TestInitCmd_UnsupportedTarget(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := initCmd()
	cmd.SetArgs([]string{"--name", "p", "--target", "gemini"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for unsupported target")
	}
}

func TestInitCmd_ExistingFileNoForce(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("existing"), 0644)

	cmd := initCmd()
	cmd.SetArgs([]string{"--name", "p"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when apm.yml exists without --force")
	}
}

func TestInitCmd_ForceOverwrite(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("existing"), 0644)

	cmd := initCmd()
	cmd.SetArgs([]string{"--name", "p", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init --force should succeed: %v", err)
	}
}

// ── validate dispatch tests ──

func TestValidateCmd_LockfileBypass(t *testing.T) {
	p := oraclePath(t, filepath.Join("lockfile", "v1-git-only.yml"))
	cmd := validateCmd()
	cmd.SetArgs([]string{p})
	if err := cmd.Execute(); err != nil {
		t.Errorf("lockfile should be accepted via content-sniff: %v", err)
	}
}

func TestValidateCmd_InvalidManifest(t *testing.T) {
	p := oraclePath(t, filepath.Join("manifest", "invalid-missing-name.yml"))
	cmd := validateCmd()
	cmd.SetArgs([]string{p})
	if err := cmd.Execute(); err == nil {
		t.Error("expected error for invalid manifest")
	}
}

func TestValidateCmd_ValidManifest(t *testing.T) {
	p := oraclePath(t, filepath.Join("manifest", "valid-minimal.yml"))
	cmd := validateCmd()
	cmd.SetArgs([]string{p})
	if err := cmd.Execute(); err != nil {
		t.Errorf("valid manifest should be accepted: %v", err)
	}
}
