package yamlcore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

// oraclePath returns the path to a conformance oracle fixture.
// Tests skip if the conformance-kit is not available (e.g. CI without submodule).
func oraclePath(t *testing.T, rel string) string {
	t.Helper()
	// Walk up from the package dir to find the project root.
	// internal/yamlcore/ → project root is ../../
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
	t.Skipf("oracle fixture not found: %s (conformance-kit not available)", rel)
	return ""
}

func readOracle(t *testing.T, rel string) []byte {
	t.Helper()
	p := oraclePath(t, rel)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// ── req-mf-020(b): reject anchors ──

func TestSafeLoad_RejectAnchor(t *testing.T) {
	tests := []struct {
		name  string
		input string
		errSS string // required substring in error
	}{
		{
			name:  "anchor on scalar",
			input: "name: &n p\nversion: \"1.0.0\"\n",
			errSS: "anchor",
		},
		{
			name:  "anchor on mapping",
			input: "top: &m\n  a: 1\n",
			errSS: "anchor",
		},
		{
			name:  "anchor on sequence",
			input: "items: &s\n  - a\n",
			errSS: "anchor",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SafeLoad([]byte(tt.input))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errSS) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errSS)
			}
		})
	}
}

// ── req-mf-020(b): reject aliases ──

func TestSafeLoad_RejectAlias(t *testing.T) {
	input := "name: &n p\nversion: \"1.0.0\"\ndescription: *n\n"
	_, err := SafeLoad([]byte(input))
	if err == nil {
		t.Fatal("expected error for alias, got nil")
	}
	if !strings.Contains(err.Error(), "anchor") && !strings.Contains(err.Error(), "alias") {
		t.Errorf("error %q should mention anchor or alias", err.Error())
	}
}

// ── req-mf-020(b): oracle fixture ──

func TestSafeLoad_RejectOracleAnchorAlias(t *testing.T) {
	data := readOracle(t, filepath.Join("manifest", "invalid-yaml-anchor-alias.yml"))
	_, err := SafeLoad(data)
	if err == nil {
		t.Fatal("expected error for oracle anchor/alias fixture, got nil")
	}
}

// ── req-mf-020(c): reject custom tags ──

func TestSafeLoad_RejectCustomTag(t *testing.T) {
	tests := []struct {
		name  string
		input string
		errSS string
	}{
		{
			name:  "local custom tag",
			input: "foo: !custom bar\n",
			errSS: "!custom",
		},
		{
			name:  "verbatim custom tag",
			input: "foo: !myapp/type val\n",
			errSS: "!myapp/type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SafeLoad([]byte(tt.input))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errSS) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errSS)
			}
		})
	}
}

// ── req-mf-020(c): standard !! tags are permitted ──

func TestSafeLoad_AcceptStandardTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"explicit !!int", "val: !!int 42\n"},
		{"explicit !!float", "val: !!float 3.14\n"},
		{"explicit !!bool", "val: !!bool true\n"},
		{"explicit !!str", "val: !!str 42\n"},
		{"explicit !!timestamp", "val: !!timestamp 2026-01-01\n"},
		{"explicit !!binary", "val: !!binary aGVsbG8=\n"},
		{"explicit !!null", "val: !!null\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SafeLoad([]byte(tt.input))
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// ── req-mf-020(a)/(d): Node-level property assertions ──
// Full behavioral enforcement is in Phase 1 typed accessors.

func TestSafeLoad_ImplicitTagProperties(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantValue  string
		wantTagged bool
	}{
		{"implicit int", "val: 42\n", "42", false},
		{"explicit !!int", "val: !!int 42\n", "42", true},
		{"implicit bool", "val: true\n", "true", false},
		{"octal-looking not coerced", "val: 0755\n", "0755", false},
		{"yaml 1.2 octal", "val: 0o755\n", "0o755", false},
		{"implicit float", "val: 3.14\n", "3.14", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := SafeLoad([]byte(tt.input))
			if err != nil {
				t.Fatal(err)
			}
			// doc -> mapping -> value is Content[0].Content[1]
			val := node.Content[0].Content[1]
			if val.Value != tt.wantValue {
				t.Errorf("Value = %q, want %q", val.Value, tt.wantValue)
			}
			tagged := val.Style&yaml.TaggedStyle != 0
			if tagged != tt.wantTagged {
				t.Errorf("TaggedStyle = %v, want %v", tagged, tt.wantTagged)
			}
		})
	}
}

// ── multi-document rejection ──

func TestSafeLoad_RejectMultiDoc(t *testing.T) {
	tests := []struct {
		name  string
		input string
		errSS string
	}{
		{
			name:  "two documents",
			input: "name: p\n---\nname: q\n",
			errSS: "multi-document",
		},
		{
			name:  "anchor hidden in second doc",
			input: "name: p\n---\nname: &n q\n",
			errSS: "multi-document",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SafeLoad([]byte(tt.input))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errSS) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errSS)
			}
		})
	}
}

// ── req-mf-020: acceptance ──

func TestSafeLoad_AcceptValid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"minimal", "name: p\nversion: \"1.0.0\"\n"},
		{"with x-* keys", "name: p\nx-acme: val\n"},
		{"empty doc", "{}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SafeLoad([]byte(tt.input))
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestSafeLoad_AcceptOracleFixtures(t *testing.T) {
	fixtures := []string{
		filepath.Join("manifest", "valid-minimal.yml"),
		filepath.Join("manifest", "x-extension-roundtrip.yml"),
		filepath.Join("lockfile", "round-trip-unknown-fields.yml"),
		filepath.Join("lockfile", "v1-git-only.yml"),
		filepath.Join("lockfile", "v2-with-registry.yml"),
	}
	for _, rel := range fixtures {
		t.Run(rel, func(t *testing.T) {
			data := readOracle(t, rel)
			_, err := SafeLoad(data)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// ── req-ext-001, req-mf-006, req-cf-001: round-trip byte-equivalence ──

func TestRoundTrip_ByteExact(t *testing.T) {
	fixtures := []string{
		filepath.Join("manifest", "x-extension-roundtrip.yml"),
		filepath.Join("manifest", "valid-minimal.yml"),
		filepath.Join("lockfile", "round-trip-unknown-fields.yml"),
		filepath.Join("lockfile", "v1-git-only.yml"),
		filepath.Join("lockfile", "v2-with-registry.yml"),
	}
	for _, rel := range fixtures {
		t.Run(rel, func(t *testing.T) {
			src := readOracle(t, rel)
			node, err := SafeLoad(src)
			if err != nil {
				t.Fatal(err)
			}
			out, err := SafeDump(node)
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != string(src) {
				t.Errorf("round-trip not byte-exact:\n  src len=%d\n  out len=%d", len(src), len(out))
				// Show first difference
				for i := 0; i < len(src) && i < len(out); i++ {
					if src[i] != out[i] {
						t.Errorf("  first diff at byte %d: src=%q out=%q", i, src[i], out[i])
						break
					}
				}
			}
		})
	}
}

// TestSafeDump_DoesNotWrapLongFlowContent guards against a real bug found
// against a hand-formatted apm.yml: yaml.NewEncoder applies WithV3Defaults(),
// which sets an 80-column line width, so re-dumping an untouched document
// containing flow-style entries wider than 80 columns split scalar values
// mid-token (e.g. a quoted string broken across two lines), corrupting
// content that was never touched by the caller's edit.
func TestSafeDump_DoesNotWrapLongFlowContent(t *testing.T) {
	src := []byte("dependencies:\n" +
		"  apm: [{git: \"https://github.com/getsentry/skills\", skills: [skill-writer]}, " +
		"{git: \"https://github.com/getsentry/skills\", skills: [skill-writer]}]\n")
	node, err := SafeLoad(src)
	if err != nil {
		t.Fatal(err)
	}
	out, err := SafeDump(node)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "\n      ") || strings.Contains(string(out), "\n        ") {
		t.Errorf("SafeDump wrapped long flow content onto a continuation line:\n%s", out)
	}
	roundTripped, err := SafeLoad(out)
	if err != nil {
		t.Fatalf("SafeDump output failed to re-parse: %v", err)
	}
	out2, err := SafeDump(roundTripped)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(out2) {
		t.Errorf("SafeDump is not idempotent on its own output")
	}
}

// Round-trip determinism: parse+dump twice must produce identical output.
func TestRoundTrip_Deterministic(t *testing.T) {
	src := readOracle(t, filepath.Join("manifest", "x-extension-roundtrip.yml"))

	node1, _ := SafeLoad(src)
	out1, _ := SafeDump(node1)

	node2, _ := SafeLoad(src)
	out2, _ := SafeDump(node2)

	if string(out1) != string(out2) {
		t.Error("round-trip is non-deterministic")
	}
}
