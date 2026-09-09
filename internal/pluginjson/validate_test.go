// Table-driven scenarios for Validate (T007). Each subtest is named after
// the spec.md (mission plugin-manifest-validate-01M21E5Q) acceptance
// scenario or edge case it pins (SC-002). Scenarios that require CLI-layer
// behavior Validate([]byte) Report cannot express -- manifest *location*
// (US1 AS7/AS8, multiple-candidate note), the --strict/-v flags themselves,
// and positional-argument usage errors -- are WP03's concern and are not
// covered here; see this WP's Activity Log for the explicit list.
package pluginjson

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertReport(t *testing.T, got Report, wantStructureFailed bool, want []Finding) {
	t.Helper()
	if got.StructureFailed != wantStructureFailed {
		t.Errorf("StructureFailed = %v, want %v", got.StructureFailed, wantStructureFailed)
	}
	if len(got.Findings) != len(want) {
		t.Fatalf("Findings = %+v, want %+v", got.Findings, want)
	}
	for i, f := range got.Findings {
		if f != want[i] {
			t.Errorf("Findings[%d] = %+v, want %+v", i, f, want[i])
		}
	}
}

func assertStructureError(t *testing.T, got Report, wantPrefix string) {
	t.Helper()
	if !got.StructureFailed {
		t.Errorf("StructureFailed = false, want true")
	}
	if len(got.Findings) != 1 {
		t.Fatalf("Findings = %+v, want exactly one Structure error", got.Findings)
	}
	f := got.Findings[0]
	if f.Check != Structure || f.Level != LevelError {
		t.Errorf("Findings[0] = %+v, want Check=Structure Level=LevelError", f)
	}
	if !strings.HasPrefix(f.Message, wantPrefix) {
		t.Errorf("Findings[0].Message = %q, want prefix %q", f.Message, wantPrefix)
	}
}

func TestValidate(t *testing.T) {
	// -- US1: pre-pack / pre-publish manifest checks --

	t.Run("US1-AS1-scaffold-claude-zero-findings", func(t *testing.T) {
		dir := t.TempDir()
		if err := Scaffold(dir, "demo-plugin", "1.0.0", "demo plugin", "Demo Author"); err != nil {
			t.Fatalf("Scaffold: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		assertReport(t, Validate(data), false, nil)
	})

	t.Run("US1-AS2-scaffold-agent-plugin-zero-findings", func(t *testing.T) {
		dir := t.TempDir()
		if err := ScaffoldAgent(dir, "demo-plugin", "1.0.0", "demo plugin", "Demo Author"); err != nil {
			t.Fatalf("ScaffoldAgent: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		assertReport(t, Validate(data), false, nil)
	})

	t.Run("US1-AS3-missing-name", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{}`)), false, []Finding{
			{Name, LevelError, "missing required field 'name'"},
		})
	})

	t.Run("US1-AS4-keywords-not-array", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","keywords":"a"}`)), false, []Finding{
			{Fields, LevelError, "'keywords' must be an array of strings"},
		})
	})

	t.Run("US1-AS5-skills-missing-dot-slash-prefix", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","skills":"skills/"}`)), false, []Finding{
			{Paths, LevelError, "'skills' must start with './'"},
		})
	})

	t.Run("US1-AS6a-commands-relative-escape-fails-prefix", func(t *testing.T) {
		// "../outside/x.md" does not match the schema's ^\./ pattern either
		// (it starts with ".." not "./"), so it is caught by the same
		// missing-prefix check as AS5, not a distinct traversal message.
		assertReport(t, Validate([]byte(`{"name":"x","commands":"../outside/x.md"}`)), false, []Finding{
			{Paths, LevelError, "'commands' must start with './'"},
		})
	})

	t.Run("US1-AS6b-commands-absolute-path", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","commands":"/outside/x.md"}`)), false, []Finding{
			{Paths, LevelError, "'commands' must not be an absolute path"},
		})
	})

	// -- US2: CI --strict catches typos and stray non-object fields --

	t.Run("US2-AS1-descripton-did-you-mean", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","descripton":"d"}`)), false, []Finding{
			{Unrecognized, LevelWarning, "unrecognized field 'descripton' (did you mean 'description'?)"},
		})
	})

	t.Run("US2-AS2-strict-does-not-change-report", func(t *testing.T) {
		// Validate has no --strict concept (FR-008: the printed counts do
		// not change under --strict, only the exit code does) -- this pins
		// that invariant WP03's CLI layer depends on: the same input
		// produces the identical Report as AS1.
		data := []byte(`{"name":"x","descripton":"d"}`)
		assertReport(t, Validate(data), false, []Finding{
			{Unrecognized, LevelWarning, "unrecognized field 'descripton' (did you mean 'description'?)"},
		})
	})

	t.Run("US2-AS3-publisher-no-suggestion", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","publisher":"p"}`)), false, []Finding{
			{Unrecognized, LevelWarning, "unrecognized field 'publisher'"},
		})
	})

	t.Run("US2-AS4-metadata-and-experimental-non-object", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","metadata":"a","experimental":[]}`)), false, []Finding{
			{Fields, LevelWarning, "'metadata' should be an object; Claude Code ignores other values"},
			{Fields, LevelWarning, "'experimental' should be an object; Claude Code ignores other values"},
		})
	})

	t.Run("US2-AS5-experimental-themes-type-error-and-foo-unrecognized", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","experimental":{"themes":1,"foo":1}}`)), false, []Finding{
			{Fields, LevelError, "'experimental.themes' must be a string or array"},
			{Unrecognized, LevelWarning, "unrecognized field 'experimental.foo'"},
		})
	})

	// -- US3: hostile/broken input gets one diagnosis, never a crash --

	t.Run("US3-AS1-invalid-json-unbalanced-brace", func(t *testing.T) {
		assertStructureError(t, Validate([]byte(`{`)), "invalid JSON: ")
	})

	t.Run("US3-AS2-top-level-not-object", func(t *testing.T) {
		assertReport(t, Validate([]byte(`[]`)), true, []Finding{
			{Structure, LevelError, "top-level value must be an object"},
		})
	})

	t.Run("US3-AS3-oversize-6MiB", func(t *testing.T) {
		data := bytes.Repeat([]byte("a"), 6*1024*1024)
		assertReport(t, Validate(data), true, []Finding{
			{Structure, LevelError, fmt.Sprintf("file exceeds 5 MiB cap (%d bytes)", len(data))},
		})
	})

	t.Run("US3-AS4-duplicate-key-last-value-wins", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"a","name":"b"}`)), false, []Finding{
			{Structure, LevelWarning, "duplicate key 'name' (last value wins)"},
		})
	})

	// -- Edge cases --

	t.Run("EdgeCase-name-not-a-string", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":123}`)), false, []Finding{
			{Name, LevelError, "'name' must be a string"},
		})
	})

	t.Run("EdgeCase-name-empty", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":""}`)), false, []Finding{
			{Name, LevelError, "'name' must not be empty"},
		})
	})

	t.Run("EdgeCase-name-contains-space", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"my plugin"}`)), false, []Finding{
			{Name, LevelError, "'name' must not contain spaces"},
		})
	})

	t.Run("EdgeCase-name-contains-control-char", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"my\u0001plugin"}`)), false, []Finding{
			{Name, LevelError, "'name' must not contain control characters"},
		})
	})

	t.Run("EdgeCase-name-contains-bidi-char", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"my`+"‮"+`plugin"}`)), false, []Finding{
			{Name, LevelError, "'name' must not contain bidirectional formatting characters"},
		})
	})

	t.Run("EdgeCase-name-not-kebab-case", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"My_Plugin"}`)), false, []Finding{
			{Name, LevelWarning, "'name' is not kebab-case"},
		})
	})

	t.Run("EdgeCase-dependencies-element-number", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","dependencies":[1]}`)), false, []Finding{
			{Fields, LevelError, "'dependencies[0]' must be a string or object"},
		})
	})

	t.Run("EdgeCase-dependencies-element-missing-name", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","dependencies":[{}]}`)), false, []Finding{
			{Fields, LevelError, "'dependencies[0]' is missing 'name'"},
		})
	})

	t.Run("EdgeCase-dependencies-version-not-string", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","dependencies":[{"name":"foo","version":1}]}`)), false, []Finding{
			{Fields, LevelError, "'dependencies[0].version' must be a string"},
		})
	})

	t.Run("EdgeCase-author-name-not-string", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","author":{"name":123}}`)), false, []Finding{
			{Fields, LevelError, "'author.name' must be a string"},
		})
	})

	t.Run("EdgeCase-hooks-mcpServers-lspServers-inline-object-legal", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","hooks":{"a":1},"mcpServers":{"b":2},"lspServers":{"c":3}}`)), false, nil)
	})

	t.Run("EdgeCase-top-level-themes-belongs-under-experimental", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","themes":["dark"]}`)), false, []Finding{
			{Unrecognized, LevelWarning, "'themes' belongs under 'experimental'"},
		})
	})

	t.Run("EdgeCase-top-level-monitors-belongs-under-experimental", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","monitors":["cpu"]}`)), false, []Finding{
			{Unrecognized, LevelWarning, "'monitors' belongs under 'experimental'"},
		})
	})

	t.Run("EdgeCase-known-fields-present-file-order", func(t *testing.T) {
		report := Validate([]byte(`{"description":"d","name":"x","foo":1,"version":"1.0.0"}`))
		want := []string{"description", "name", "version"}
		if len(report.KnownFieldsPresent) != len(want) {
			t.Fatalf("KnownFieldsPresent = %v, want %v", report.KnownFieldsPresent, want)
		}
		for i, k := range report.KnownFieldsPresent {
			if k != want[i] {
				t.Errorf("KnownFieldsPresent[%d] = %q, want %q", i, k, want[i])
			}
		}
	})

	t.Run("EdgeCase-fields-error-before-warning-in-same-check", func(t *testing.T) {
		// metadata (warning-tier) appears before keywords (error-tier) in
		// the manifest text; the Fields check must still emit the error
		// first (data-model.md: errors precede warnings within a check).
		assertReport(t, Validate([]byte(`{"name":"x","metadata":"z","keywords":"a"}`)), false, []Finding{
			{Fields, LevelError, "'keywords' must be an array of strings"},
			{Fields, LevelWarning, "'metadata' should be an object; Claude Code ignores other values"},
		})
	})

	t.Run("EdgeCase-invalid-utf8", func(t *testing.T) {
		assertReport(t, Validate([]byte{0xFF}), true, []Finding{
			{Structure, LevelError, "invalid UTF-8"},
		})
	})

	t.Run("EdgeCase-empty-file", func(t *testing.T) {
		assertStructureError(t, Validate([]byte("")), "invalid JSON: ")
	})

	t.Run("EdgeCase-deep-nesting-1MiB", func(t *testing.T) {
		data := bytes.Repeat([]byte("["), 1024*1024)
		assertStructureError(t, Validate(data), "invalid JSON: ")
	})

	t.Run("EdgeCase-paths-dotdot-segment", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","commands":"./sub/../secret.md"}`)), false, []Finding{
			{Paths, LevelError, "'commands' must not contain '..'"},
		})
	})

	t.Run("EdgeCase-paths-array-index-field", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","skills":["./a.md","bad"]}`)), false, []Finding{
			{Paths, LevelError, "'skills[1]' must start with './'"},
		})
	})
}
