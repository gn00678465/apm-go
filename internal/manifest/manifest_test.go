package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func loadFixture(t *testing.T, rel string) (*Manifest, []Diagnostic, error) {
	t.Helper()
	p := oraclePath(t, rel)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, nil, err
	}
	return ParseManifest(node)
}

// ── Oracle fixture acceptance tests ──

func TestParseManifest_ValidMinimal(t *testing.T) {
	m, diags, err := loadFixture(t, filepath.Join("manifest", "valid-minimal.yml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Name != "my-project" {
		t.Errorf("name = %q, want %q", m.Name, "my-project")
	}
	if m.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", m.Version, "1.0.0")
	}
	// No warnings expected for valid minimal
	for _, d := range diags {
		if d.Level == LevelError {
			t.Errorf("unexpected error diagnostic: %s", d.Message)
		}
	}
}

func TestParseManifest_ValidFull(t *testing.T) {
	m, _, err := loadFixture(t, filepath.Join("manifest", "valid-full.yml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Name != "contoso/security-baseline" {
		t.Errorf("name = %q", m.Name)
	}
	// Check antigravity is accepted
	hasAntigravity := false
	for _, tgt := range m.Target {
		if tgt == "antigravity" {
			hasAntigravity = true
		}
	}
	if !hasAntigravity {
		t.Errorf("target should contain antigravity, got %v", m.Target)
	}
}

func TestParseManifest_ValidWorkspacesReserved(t *testing.T) {
	_, diags, err := loadFixture(t, filepath.Join("manifest", "valid-workspaces-reserved.yml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "workspaces") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning containing 'workspaces'")
	}
}

// ── Oracle fixture rejection tests ──

func TestParseManifest_InvalidMissingName(t *testing.T) {
	_, _, err := loadFixture(t, filepath.Join("manifest", "invalid-missing-name.yml"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error %q should contain 'name'", err.Error())
	}
}

func TestParseManifest_InvalidTarget(t *testing.T) {
	_, _, err := loadFixture(t, filepath.Join("manifest", "invalid-target.yml"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "notarealtool") {
		t.Errorf("error %q should contain 'notarealtool'", err.Error())
	}
}

func TestParseManifest_InvalidBothIdGit(t *testing.T) {
	_, _, err := loadFixture(t, filepath.Join("manifest", "invalid-both-id-git.yml"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "id") || !strings.Contains(err.Error(), "git") {
		t.Errorf("error %q should contain 'id' and 'git'", err.Error())
	}
}

func TestParseManifest_InvalidNoSourceKey(t *testing.T) {
	_, _, err := loadFixture(t, filepath.Join("manifest", "invalid-no-source-key.yml"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "source") {
		t.Errorf("error %q should contain 'source'", err.Error())
	}
}

func TestParseManifest_InvalidRegistryScheme(t *testing.T) {
	_, _, err := loadFixture(t, filepath.Join("manifest", "invalid-registry-scheme.yml"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestParseManifest_InvalidRegistriesTypo(t *testing.T) {
	_, _, err := loadFixture(t, filepath.Join("manifest", "invalid-registries-typo.yml"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "urls") {
		t.Errorf("error %q should contain 'urls'", err.Error())
	}
}

func TestParseManifest_InvalidLocalpathEscape(t *testing.T) {
	_, _, err := loadFixture(t, filepath.Join("manifest", "invalid-localpath-escape.yml"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "escape") {
		t.Errorf("error %q should mention escape", err.Error())
	}
}

func TestParseManifest_InvalidHashAlgorithm(t *testing.T) {
	_, _, err := loadFixture(t, filepath.Join("manifest", "invalid-hash-algorithm.yml"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "md5") {
		t.Errorf("error %q should contain 'md5'", err.Error())
	}
}

// ── Lockfile content-sniff: should NOT fail manifest validation ──

func TestNodeHasKey_LockfileVersion(t *testing.T) {
	data := []byte("lockfile_version: \"1\"\ndependencies: []\n")
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	root := node.Content[0]
	if !NodeHasKey(root, "lockfile_version") {
		t.Error("expected lockfile_version key to be detected")
	}
}

// ── Inline unit tests ──

func TestParseManifest_NonMapping(t *testing.T) {
	data := []byte("- item1\n- item2\n")
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = ParseManifest(node)
	if err == nil {
		t.Fatal("expected error for non-mapping top-level")
	}
	if !strings.Contains(err.Error(), "mapping") {
		t.Errorf("error %q should mention mapping", err.Error())
	}
}

func TestParseManifest_TargetAllNoWarning(t *testing.T) {
	data := []byte("name: p\nversion: \"1.0.0\"\ntarget: all\n")
	node, _ := yamlcore.SafeLoad(data)
	_, diags, err := ParseManifest(node)
	if err != nil {
		t.Fatalf("target: all should be accepted: %v", err)
	}
	for _, d := range diags {
		if d.Req == "req-tg-004" {
			t.Errorf("target: all should not produce tg-004 warning, got: %s", d.Message)
		}
	}
}

func TestParseManifest_GeminiWarning(t *testing.T) {
	data := []byte("name: p\nversion: \"1.0.0\"\ntarget: gemini\n")
	node, _ := yamlcore.SafeLoad(data)
	_, diags, err := ParseManifest(node)
	if err != nil {
		t.Fatalf("target: gemini should be accepted: %v", err)
	}
	found := false
	for _, d := range diags {
		if d.Req == "req-tg-004" && strings.Contains(d.Message, "gemini") {
			found = true
		}
	}
	if !found {
		t.Error("expected tg-004 warning for gemini (no adapter)")
	}
}

func TestParseManifest_InsecureTrueVariants(t *testing.T) {
	for _, variant := range []string{"true", "True", "TRUE"} {
		t.Run(variant, func(t *testing.T) {
			data := []byte(fmt.Sprintf("name: p\nversion: \"1.0.0\"\nregistries:\n  local:\n    url: http://example.com/r\n    insecure: %s\n", variant))
			node, _ := yamlcore.SafeLoad(data)
			_, _, err := ParseManifest(node)
			if err != nil {
				t.Errorf("insecure: %s should be accepted: %v", variant, err)
			}
		})
	}
}

func TestParseManifest_EmptyName(t *testing.T) {
	data := []byte("name: \"\"\nversion: \"1.0.0\"\n")
	node, _ := yamlcore.SafeLoad(data)
	_, _, err := ParseManifest(node)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestParseManifest_NonSemverWarning(t *testing.T) {
	data := []byte("name: p\nversion: not-semver\n")
	node, _ := yamlcore.SafeLoad(data)
	_, diags, err := ParseManifest(node)
	if err != nil {
		t.Fatalf("non-semver should not be an error: %v", err)
	}
	found := false
	for _, d := range diags {
		if d.Req == "req-mf-004" {
			found = true
		}
	}
	if !found {
		t.Error("expected req-mf-004 warning for non-semver version")
	}
}

func TestParseManifest_MinimalTargetRejected(t *testing.T) {
	data := []byte("name: p\nversion: \"1.0.0\"\ntarget: minimal\n")
	node, _ := yamlcore.SafeLoad(data)
	_, _, err := ParseManifest(node)
	if err == nil {
		t.Fatal("expected error for target: minimal")
	}
	if !strings.Contains(err.Error(), "minimal") {
		t.Errorf("error should mention minimal: %v", err)
	}
}

func TestParseManifest_AliasNormalization(t *testing.T) {
	data := []byte("name: p\nversion: \"1.0.0\"\ntarget: vscode\n")
	node, _ := yamlcore.SafeLoad(data)
	m, _, err := ParseManifest(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Target) != 1 || m.Target[0] != "copilot" {
		t.Errorf("vscode should normalize to copilot, got %v", m.Target)
	}
}

func TestParseManifest_HttpInsecureAccepted(t *testing.T) {
	data := []byte("name: p\nversion: \"1.0.0\"\nregistries:\n  local:\n    url: http://192.168.1.1/r\n")
	node, _ := yamlcore.SafeLoad(data)
	_, _, err := ParseManifest(node)
	if err != nil {
		t.Errorf("http with private IP should be accepted: %v", err)
	}
}

func TestParseManifest_HttpLocalhostAccepted(t *testing.T) {
	data := []byte("name: p\nversion: \"1.0.0\"\nregistries:\n  local:\n    url: http://127.0.0.1/r\n")
	node, _ := yamlcore.SafeLoad(data)
	_, _, err := ParseManifest(node)
	if err != nil {
		t.Errorf("http with loopback should be accepted: %v", err)
	}
}

func TestContainsEscape(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"./packages/foo", false},
		{"../outside", true},
		{"../../etc/passwd", true},
		{"packages/../packages/foo", false},
		{"packages/../../outside", true},
		{"./foo/bar", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := containsEscape(tt.path); got != tt.want {
				t.Errorf("containsEscape(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
