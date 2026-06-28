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

// ── init tests (non-interactive via --yes) ──

func TestInitCmd_YesMode(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := initCmd()
	cmd.SetArgs([]string{"--yes", "--target", "claude"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init --yes failed: %v", err)
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
	if m.Name == "" {
		t.Error("name should not be empty")
	}
	if m.Version != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", m.Version)
	}
	if m.Description == "" {
		t.Error("description should not be empty")
	}
	if m.Author == "" {
		t.Error("author should not be empty")
	}

	content := string(data)
	if strings.Contains(content, "minimal") {
		t.Error("init output must not contain 'minimal'")
	}
	if strings.Contains(content, "targets:") {
		t.Error("init output must not contain 'targets:' (plural)")
	}
}

func TestInitCmd_GeneratedFieldsComplete(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := initCmd()
	cmd.SetArgs([]string{"--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init --yes failed: %v", err)
	}

	data, _ := os.ReadFile("apm.yml")
	content := string(data)

	requiredFields := []string{"name:", "version:", "description:", "author:", "dependencies:", "includes:"}
	for _, f := range requiredFields {
		if !strings.Contains(content, f) {
			t.Errorf("init output missing field %q", f)
		}
	}
}

func TestInitCmd_ExistingYmlYesOverwrites(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("existing"), 0644)

	cmd := initCmd()
	cmd.SetArgs([]string{"--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init --yes should overwrite: %v", err)
	}

	data, _ := os.ReadFile("apm.yml")
	if string(data) == "existing" {
		t.Error("apm.yml should have been overwritten")
	}
}

func TestInitCmd_TargetFlagValidation(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := initCmd()
	cmd.SetArgs([]string{"--yes", "--target", "notarealtool"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid target")
	}
}

func TestInitCmd_OutputPassesSelfValidation(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := initCmd()
	cmd.SetArgs([]string{"--yes", "--target", "claude,copilot"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile("apm.yml")
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatalf("init output fails SafeLoad: %v", err)
	}
	_, _, err = manifest.ParseManifest(node)
	if err != nil {
		t.Fatalf("init output fails ParseManifest: %v", err)
	}
}

func TestInitCmd_ProjectNameArg(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := initCmd()
	cmd.SetArgs([]string{"my-new-project", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init with project name failed: %v", err)
	}

	// Should have created the directory and apm.yml inside it
	if _, err := os.Stat(filepath.Join(dir, "my-new-project", "apm.yml")); err != nil {
		t.Error("apm.yml should be in the new project directory")
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

// ── helper tests ──

func TestParseToggleInput(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  []int
	}{
		{"1", 5, []int{0}},
		{"3", 5, []int{2}},
		{"1,3,5", 5, []int{0, 2, 4}},
		{"1-3", 5, []int{0, 1, 2}},
		{"1,3-5", 5, []int{0, 2, 3, 4}},
		{"", 5, nil},
		{"0", 5, nil},
		{"6", 5, nil},
		{"abc", 5, nil},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseToggleInput(tt.input, tt.max)
			if len(got) != len(tt.want) {
				t.Errorf("parseToggleInput(%q, %d) = %v, want %v", tt.input, tt.max, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("index %d: got %d, want %d", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBuildManifestData(t *testing.T) {
	data := buildManifestData("test", "1.0.0", "desc", "author", []string{"claude"})
	if data["name"] != "test" {
		t.Error("name mismatch")
	}
	if data["includes"] != "auto" {
		t.Error("includes should be 'auto'")
	}
	deps, ok := data["dependencies"].(map[string]any)
	if !ok {
		t.Fatal("dependencies should be a map")
	}
	if _, ok := deps["apm"]; !ok {
		t.Error("dependencies.apm missing")
	}
	if _, ok := deps["mcp"]; !ok {
		t.Error("dependencies.mcp missing")
	}
}

func TestReadExistingTargets(t *testing.T) {
	t.Run("sequence form", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		os.WriteFile("apm.yml", []byte("name: p\nversion: \"1.0.0\"\ntarget:\n  - claude\n  - copilot\n"), 0644)
		targets := readExistingTargets()
		if len(targets) != 2 || targets[0] != "claude" || targets[1] != "copilot" {
			t.Errorf("got %v, want [claude copilot]", targets)
		}
	})

	t.Run("scalar form", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		os.WriteFile("apm.yml", []byte("name: p\nversion: \"1.0.0\"\ntarget: claude\n"), 0644)
		targets := readExistingTargets()
		if len(targets) != 1 || targets[0] != "claude" {
			t.Errorf("got %v, want [claude]", targets)
		}
	})

	t.Run("no file", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		targets := readExistingTargets()
		if targets != nil {
			t.Errorf("got %v, want nil", targets)
		}
	})
}

func TestInitCmd_NonInitTargetRejected(t *testing.T) {
	for _, bad := range []string{"gemini", "cursor", "windsurf", "agent-skills"} {
		t.Run(bad, func(t *testing.T) {
			dir := t.TempDir()
			origDir, _ := os.Getwd()
			os.Chdir(dir)
			defer os.Chdir(origDir)

			cmd := initCmd()
			cmd.SetArgs([]string{"--yes", "--target", bad})
			if err := cmd.Execute(); err == nil {
				t.Errorf("--target %s should be rejected by init", bad)
			}
		})
	}
}

func TestInitCmd_ProjectNameWithDotDotRejected(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := initCmd()
	cmd.SetArgs([]string{"..", "--yes"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for project name '..'")
	}
}

func TestInitCmd_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("existing"), 0644)

	cmd := initCmd()
	cmd.SetArgs([]string{"--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init --force should succeed: %v", err)
	}

	data, _ := os.ReadFile("apm.yml")
	if string(data) == "existing" {
		t.Error("apm.yml should have been overwritten")
	}
}

func TestBuildManifestData_NoTargets(t *testing.T) {
	data := buildManifestData("test", "1.0.0", "desc", "author", nil)
	if _, ok := data["target"]; ok {
		t.Error("target should not be present when no targets selected")
	}
}
