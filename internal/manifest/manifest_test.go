package manifest

import (
	"bytes"
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

func TestParseManifest_AgyAliasNormalization(t *testing.T) {
	// Explicit-only alignment (Codex H2 audit): apm.yml target: agy must
	// canonicalize to antigravity at parse time (parseTargetField ->
	// ValidateTarget), otherwise the raw alias token would flow into
	// deploy.ResolveTargets, where filterSupported silently drops it.
	for _, form := range []string{"target: agy\n", "target: [agy]\n"} {
		data := []byte("name: p\nversion: \"1.0.0\"\n" + form)
		node, _ := yamlcore.SafeLoad(data)
		m, _, err := ParseManifest(node)
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", form, err)
		}
		if len(m.Target) != 1 || m.Target[0] != "antigravity" {
			t.Errorf("%q: agy should normalize to antigravity, got %v", form, m.Target)
		}
	}
}

// ── target:/targets: decision tree (research/pack-parity-findings.md §2.1,
// mirroring apm_yml.py:47-108 parse_targets_field) ──

func TestParseManifest_TargetsPluralList(t *testing.T) {
	data := []byte("name: p\nversion: \"1.0.0\"\ntargets:\n  - claude\n  - copilot\n")
	node, _ := yamlcore.SafeLoad(data)
	m, _, err := ParseManifest(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Target) != 2 || m.Target[0] != "claude" || m.Target[1] != "copilot" {
		t.Errorf("targets: [claude, copilot] got %v", m.Target)
	}
}

func TestParseManifest_TargetsPluralScalarSingleElement(t *testing.T) {
	data := []byte("name: p\nversion: \"1.0.0\"\ntargets: claude\n")
	node, _ := yamlcore.SafeLoad(data)
	m, _, err := ParseManifest(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Target) != 1 || m.Target[0] != "claude" {
		t.Errorf("targets: claude (scalar) should become single-element list, got %v", m.Target)
	}
}

func TestParseManifest_TargetsEmptyListRejected(t *testing.T) {
	data := []byte("name: p\nversion: \"1.0.0\"\ntargets: []\n")
	node, _ := yamlcore.SafeLoad(data)
	_, _, err := ParseManifest(node)
	if err == nil {
		t.Fatal("expected error for empty targets: list")
	}
	if !strings.Contains(err.Error(), "targets") {
		t.Errorf("error %q should mention 'targets'", err.Error())
	}
}

func TestParseManifest_TargetsNullRejected(t *testing.T) {
	data := []byte("name: p\nversion: \"1.0.0\"\ntargets:\n")
	node, _ := yamlcore.SafeLoad(data)
	_, _, err := ParseManifest(node)
	if err == nil {
		t.Fatal("expected error for null targets:")
	}
	if !strings.Contains(err.Error(), "targets") {
		t.Errorf("error %q should mention 'targets'", err.Error())
	}
}

func TestParseManifest_TargetCSVSugar(t *testing.T) {
	data := []byte("name: p\nversion: \"1.0.0\"\ntarget: \"claude,copilot\"\n")
	node, _ := yamlcore.SafeLoad(data)
	m, _, err := ParseManifest(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Target) != 2 || m.Target[0] != "claude" || m.Target[1] != "copilot" {
		t.Errorf("target: \"claude,copilot\" (CSV sugar) got %v", m.Target)
	}
}

func TestParseManifest_TargetCSVSugarWithSpaces(t *testing.T) {
	data := []byte("name: p\nversion: \"1.0.0\"\ntarget: \"claude, copilot\"\n")
	node, _ := yamlcore.SafeLoad(data)
	m, _, err := ParseManifest(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Target) != 2 || m.Target[0] != "claude" || m.Target[1] != "copilot" {
		t.Errorf("target: \"claude, copilot\" (CSV sugar w/ spaces) got %v", m.Target)
	}
}

func TestParseManifest_TargetSingleUnchanged(t *testing.T) {
	// Existing single-target behavior must not regress under the CSV-sugar
	// change: a scalar with no comma is still a one-element list.
	data := []byte("name: p\nversion: \"1.0.0\"\ntarget: claude\n")
	node, _ := yamlcore.SafeLoad(data)
	m, _, err := ParseManifest(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Target) != 1 || m.Target[0] != "claude" {
		t.Errorf("target: claude got %v", m.Target)
	}
}

func TestParseManifest_TargetNullFallsThroughToAutoDetect(t *testing.T) {
	data := []byte("name: p\nversion: \"1.0.0\"\ntarget:\n")
	node, _ := yamlcore.SafeLoad(data)
	m, _, err := ParseManifest(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Target) != 0 {
		t.Errorf("target: (null) should be empty (auto-detect), got %v", m.Target)
	}
}

func TestParseManifest_TargetAndTargetsConflict(t *testing.T) {
	forms := []string{
		"name: p\nversion: \"1.0.0\"\ntarget: claude\ntargets:\n  - claude\n",
		"name: p\nversion: \"1.0.0\"\ntargets:\n  - claude\ntarget: claude\n",
	}
	for _, data := range forms {
		node, _ := yamlcore.SafeLoad([]byte(data))
		_, _, err := ParseManifest(node)
		if err == nil {
			t.Fatalf("expected conflict error for both keys present, form %q", data)
		}
	}
}

// wantConflictingTargetsError duplicates the mutex-conflict error text as an
// independent literal (not a reference to the string embedded in
// manifest.go) so wording drift breaks this test loudly, matching the
// allowExecutablesWarning verbatim-lock pattern above.
const wantConflictingTargetsError = "apm.yml must not define both 'target:' and 'targets:'; use only one"

// TestParseManifest_TargetAndTargetsConflict_InvalidFirstValueMasksNothing
// locks codex-verify-phase01.md FAIL 1: Python computes key presence and
// raises ConflictingTargetsError before reading either value
// (apm_yml.py:53-58). The prior Go implementation only noticed the conflict
// once it reached the second key, so an invalid or empty value under
// whichever key appeared first surfaced instead of the mutex error.
func TestParseManifest_TargetAndTargetsConflict_InvalidFirstValueMasksNothing(t *testing.T) {
	forms := map[string]string{
		"empty targets first, valid target second":   "name: p\nversion: \"1.0.0\"\ntargets: []\ntarget: claude\n",
		"invalid target first, valid targets second": "name: p\nversion: \"1.0.0\"\ntarget: bogus\ntargets:\n  - claude\n",
	}
	for name, data := range forms {
		t.Run(name, func(t *testing.T) {
			node, _ := yamlcore.SafeLoad([]byte(data))
			_, _, err := ParseManifest(node)
			if err == nil {
				t.Fatal("expected conflict error")
			}
			if err.Error() != wantConflictingTargetsError {
				t.Errorf("error = %q, want %q (conflict must be checked before either value is parsed)", err.Error(), wantConflictingTargetsError)
			}
		})
	}
}

// TestParseManifest_TargetListRejectsMappingElement locks
// codex-verify-phase01.md FAIL 2: Python stringifies every list element
// before filtering and canonical validation (apm_yml.py:82-84,94-98), so a
// mapping element becomes a non-empty string and is rejected as an unknown
// target. The prior Go implementation copied yaml.Node.Value without
// checking the element kind; a mapping node has an empty Value, so it was
// silently dropped as blank and the manifest parsed successfully.
func TestParseManifest_TargetListRejectsMappingElement(t *testing.T) {
	forms := map[string]string{
		"targets: [{foo: bar}]": "name: p\nversion: \"1.0.0\"\ntargets:\n  - foo: bar\n",
		"target: [{foo: bar}]":  "name: p\nversion: \"1.0.0\"\ntarget:\n  - foo: bar\n",
	}
	for name, data := range forms {
		t.Run(name, func(t *testing.T) {
			node, _ := yamlcore.SafeLoad([]byte(data))
			_, _, err := ParseManifest(node)
			if err == nil {
				t.Fatalf("expected error for mapping element inside target list, form %q", data)
			}
		})
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

func TestParseManifest_MarketplaceSourceValidation(t *testing.T) {
	t.Run("valid source", func(t *testing.T) {
		data := []byte("name: p\nversion: \"1.0.0\"\nmarketplace:\n  packages:\n    - source: ./packages/foo\n")
		node, _ := yamlcore.SafeLoad(data)
		_, _, err := ParseManifest(node)
		if err != nil {
			t.Errorf("valid marketplace source should accept: %v", err)
		}
	})
	t.Run("escape rejected", func(t *testing.T) {
		data := []byte("name: p\nversion: \"1.0.0\"\nmarketplace:\n  packages:\n    - source: ../escape\n")
		node, _ := yamlcore.SafeLoad(data)
		_, _, err := ParseManifest(node)
		if err == nil {
			t.Fatal("expected error for marketplace source with ..")
		}
	})
}

func TestParseManifest_MCPValidation(t *testing.T) {
	t.Run("valid self-defined", func(t *testing.T) {
		data := []byte("name: p\nversion: \"1.0.0\"\ndependencies:\n  mcp:\n    - registry: false\n      transport: stdio\n      command: my-server\n      args: []\n")
		node, _ := yamlcore.SafeLoad(data)
		_, _, err := ParseManifest(node)
		if err != nil {
			t.Errorf("valid MCP should accept: %v", err)
		}
	})
	t.Run("self-defined missing transport", func(t *testing.T) {
		data := []byte("name: p\nversion: \"1.0.0\"\ndependencies:\n  mcp:\n    - registry: false\n      command: my-server\n")
		node, _ := yamlcore.SafeLoad(data)
		_, _, err := ParseManifest(node)
		if err == nil {
			t.Fatal("expected error for self-defined MCP missing transport")
		}
	})
}

func TestParseManifest_DepStringValidation(t *testing.T) {
	t.Run("valid shorthand", func(t *testing.T) {
		data := []byte("name: p\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - owner/repo\n")
		node, _ := yamlcore.SafeLoad(data)
		_, _, err := ParseManifest(node)
		if err != nil {
			t.Errorf("valid shorthand dep should accept: %v", err)
		}
	})
	t.Run("invalid string dep", func(t *testing.T) {
		data := []byte("name: p\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - \"not valid string\"\n")
		node, _ := yamlcore.SafeLoad(data)
		_, _, err := ParseManifest(node)
		if err == nil {
			t.Fatal("expected error for invalid dep string")
		}
	})
}

func TestParseManifest_HttpLocalhostTextAccepted(t *testing.T) {
	data := []byte("name: p\nversion: \"1.0.0\"\nregistries:\n  local:\n    url: http://localhost/r\n")
	node, _ := yamlcore.SafeLoad(data)
	_, _, err := ParseManifest(node)
	if err != nil {
		t.Errorf("http with localhost should be accepted: %v", err)
	}
}

// wantAllowExecutablesWarning duplicates allowExecutablesWarning as an
// independent string literal -- not a reference to that const -- so a
// wording change to allowExecutablesWarning breaks this test with a red
// diff instead of both sides silently changing together (same
// verbatim-lock pattern as errNoDeployTarget's literal
// "no deployment target detected" check in cmd/apm-go/install_test.go).
const wantAllowExecutablesWarning = "[warn] apm.yml has an allowExecutables: block, but apm-go does not enforce it yet; this block is not effective in apm-go and every executable primitive (hooks, bin, MCP) is still deployed unconditionally"

// TestParseManifest_AllowExecutablesWarning locks P0 #4 (register §4.1/§5):
// an allowExecutables: block must produce a returned Diagnostic AND print
// directly to stderr (the only way this reaches `apm-go install`, which
// discards ParseManifest's returned diags -- see allowExecutablesWarning's
// doc comment), while never turning into a parse error.
func TestParseManifest_AllowExecutablesWarning(t *testing.T) {
	data := []byte("name: p\nversion: \"1.0.0\"\nallowExecutables: {}\n")
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}

	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	_, diags, parseErr := ParseManifest(node)
	w.Close()
	os.Stderr = orig
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	stderr := buf.String()

	if parseErr != nil {
		t.Fatalf("allowExecutables: {} must not fail parsing: %v", parseErr)
	}
	if !strings.Contains(stderr, wantAllowExecutablesWarning) {
		t.Errorf("stderr = %q, want the full allowExecutables warning %q", stderr, wantAllowExecutablesWarning)
	}
	found := false
	for _, d := range diags {
		if d.Message == wantAllowExecutablesWarning {
			found = true
		}
	}
	if !found {
		t.Errorf("diags = %+v, want the allowExecutables warning among returned diagnostics too", diags)
	}
}

func TestParseManifest_NoAllowExecutables_NoWarning(t *testing.T) {
	data := []byte("name: p\nversion: \"1.0.0\"\n")
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}

	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	_, _, parseErr := ParseManifest(node)
	w.Close()
	os.Stderr = orig
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}

	if parseErr != nil {
		t.Fatalf("unexpected error: %v", parseErr)
	}
	if buf.Len() != 0 {
		t.Errorf("stderr = %q, want no output when apm.yml has no allowExecutables: block", buf.String())
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
