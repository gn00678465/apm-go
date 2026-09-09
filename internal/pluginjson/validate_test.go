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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// assertStructureError requires the exact decoder-detail message (not just a
// prefix): contracts/cli-plugin-validate.md's Structure row specifies
// `invalid JSON: <decoder message>` in full, and a prefix-only comparison
// would still pass if the decoder detail were dropped or corrupted.
func assertStructureError(t *testing.T, got Report, wantMessage string) {
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
	if f.Message != wantMessage {
		t.Errorf("Findings[0].Message = %q, want %q", f.Message, wantMessage)
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

	t.Run("EdgeCase-commands-object-form-legal", func(t *testing.T) {
		// Schema's third anyOf branch for "commands": an object mapping
		// command names to metadata (propertyNames string, additionalProperties
		// an object with "source"/"content"/etc) -- distinct from the string
		// and array-of-string path forms AS6a/AS6b exercise. kindStringOrArray
		// wrongly rejected this shape as a Fields error (defect 1).
		assertReport(t, Validate([]byte(`{"name":"x","commands":{"about":{"source":"./about.md"}}}`)), false, nil)
	})

	t.Run("EdgeCase-commands-wrong-type-rejected", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","commands":42}`)), false, []Finding{
			{Fields, LevelError, "'commands' must be a string, array, or object"},
		})
	})

	// -- US2: CI --strict catches typos and stray non-object fields --

	t.Run("US2-AS1-descripton-did-you-mean", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","descripton":"d"}`)), false, []Finding{
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

	t.Run("EdgeCase-experimental-monitors-object-array-legal", func(t *testing.T) {
		// Same anyOf shape as top-level "monitors" (kindStringOrArrayOfObject):
		// the Claude Code plugins-reference Monitors section allows the same
		// inline monitor object array directly under experimental.monitors,
		// not only at the top level -- unlike experimental.themes, which stays
		// on kindStringOrArray.
		assertReport(t, Validate([]byte(`{"name":"x","experimental":{"monitors":[{"name":"cpu","command":"echo cpu","description":"CPU monitor"}]}}`)), false, nil)
	})

	t.Run("EdgeCase-experimental-monitors-array-of-string-rejected", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","experimental":{"monitors":["cpu"]}}`)), false, []Finding{
			{Fields, LevelError, "'experimental.monitors' must be a string or array of objects"},
		})
	})

	// -- US3: hostile/broken input gets one diagnosis, never a crash --

	t.Run("US3-AS1-invalid-json-unbalanced-brace", func(t *testing.T) {
		assertStructureError(t, Validate([]byte(`{`)), "invalid JSON: unexpected end of JSON input")
	})

	t.Run("US3-AS2-top-level-not-object", func(t *testing.T) {
		assertReport(t, Validate([]byte(`[]`)), true, []Finding{
			{Structure, LevelError, "top-level value must be an object"},
		})
	})

	t.Run("EdgeCase-top-level-null", func(t *testing.T) {
		// Unlike every other non-object top level (array/number/string/bool),
		// which encoding/json rejects into a map with *json.UnmarshalTypeError,
		// a literal `null` decodes with err == nil and leaves the map nil --
		// the one case Validate must catch itself, not via the decode error.
		assertReport(t, Validate([]byte(`null`)), true, []Finding{
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
		// The second "name" value (123) is not a legal name value; only a
		// last-value-wins decode surfaces the resulting Name error, so this
		// (unlike two-legal-values) fails a first-value-wins implementation.
		assertReport(t, Validate([]byte(`{"name":"a","name":123}`)), false, []Finding{
			{Structure, LevelWarning, "duplicate key 'name' (last value wins)"},
			{Name, LevelError, "'name' must be a string"},
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

	t.Run("EdgeCase-dependencies-name-wrong-type", func(t *testing.T) {
		// {"name":123} has a present "name" key that is not a string -- this
		// is a type mismatch, not an absent key, so it must use the generic
		// "'<field>' must be a string" wording, not "is missing 'name'".
		assertReport(t, Validate([]byte(`{"name":"x","dependencies":[{"name":123}]}`)), false, []Finding{
			{Fields, LevelError, "'dependencies[0].name' must be a string"},
		})
	})

	t.Run("EdgeCase-dependencies-name-null", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","dependencies":[{"name":null}]}`)), false, []Finding{
			{Fields, LevelError, "'dependencies[0].name' must be a string"},
		})
	})

	t.Run("EdgeCase-dependencies-version-then-name-reverse-order", func(t *testing.T) {
		// "version" appears before "name" in the manifest text; same-level
		// findings must keep that file order (data-model.md), the same rule
		// checkAuthor's sub-object walk already applies to author.<k>.
		assertReport(t, Validate([]byte(`{"name":"x","dependencies":[{"version":1,"name":2}]}`)), false, []Finding{
			{Fields, LevelError, "'dependencies[0].version' must be a string"},
			{Fields, LevelError, "'dependencies[0].name' must be a string"},
		})
	})

	t.Run("EdgeCase-dependencies-string-entry-legal", func(t *testing.T) {
		// contracts/cli-plugin-validate.md's own message ("must be a string
		// or object") and Claude Code's documented dependency shape both
		// treat a bare string entry as legal; only a non-string,
		// non-object element is an error.
		assertReport(t, Validate([]byte(`{"name":"x","dependencies":["some-pkg"]}`)), false, nil)
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

	t.Run("EdgeCase-author-two-bad-keys-reverse-order", func(t *testing.T) {
		// "url" appears before "name" in the manifest text; same-level
		// findings must keep that file order, not a fixed name/email/url
		// sequence.
		assertReport(t, Validate([]byte(`{"name":"x","author":{"url":1,"name":2}}`)), false, []Finding{
			{Fields, LevelError, "'author.url' must be a string"},
			{Fields, LevelError, "'author.name' must be a string"},
		})
	})

	t.Run("EdgeCase-hooks-mcpServers-lspServers-inline-object-legal", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","hooks":{"a":1},"mcpServers":{"b":2},"lspServers":{"c":3}}`)), false, nil)
	})

	t.Run("EdgeCase-hooks-array-mixes-string-and-object-legal", func(t *testing.T) {
		// Pinned source: upstream b75a02b1's vendored schema's "hooks" array
		// branch has items anyOf a "./"-suffixed .json path string OR an
		// inline hooks object, in the very same array -- kindStringArrayOrObject
		// wrongly required every array element to be a string (defect,
		// WP02 finding 1).
		assertReport(t, Validate([]byte(`{"name":"x","hooks":["./hooks/extra.json",{"PreToolUse":[]}]}`)), false, nil)
	})

	t.Run("EdgeCase-mcpServers-array-mixes-string-and-object-legal", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","mcpServers":["./mcp/extra.json",{"demo":{"command":"foo"}}]}`)), false, nil)
	})

	t.Run("EdgeCase-lspServers-array-mixes-string-and-object-legal", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","lspServers":["./lsp/extra.json",{"ts":{"command":"tsserver","extensionToLanguage":{}}}]}`)), false, nil)
	})

	t.Run("EdgeCase-hooks-array-element-wrong-type-rejected", func(t *testing.T) {
		// A number is neither a path string nor an inline hooks object --
		// mixed-array support must not widen acceptance past string/object.
		assertReport(t, Validate([]byte(`{"name":"x","hooks":[42]}`)), false, []Finding{
			{Fields, LevelError, "'hooks' must be a string, array, or object"},
		})
	})

	t.Run("EdgeCase-hooks-mixed-array-checks-string-past-leading-object", func(t *testing.T) {
		// Review finding: pathValues required every array element to be a
		// string, so one object element (legal since defect fix WP02-1) made
		// the whole array's Paths check silently no-op. The offending string
		// is index 1, past the object at index 0; the message must keep that
		// ORIGINAL index, not renumber to 0 after the object is skipped.
		assertReport(t, Validate([]byte(`{"name":"x","hooks":[{},"/outside"]}`)), false, []Finding{
			{Paths, LevelError, "'hooks[1]' must not be an absolute path"},
		})
	})

	t.Run("EdgeCase-mcpServers-mixed-array-checks-string-past-leading-object", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","mcpServers":[{},"/outside"]}`)), false, []Finding{
			{Paths, LevelError, "'mcpServers[1]' must not be an absolute path"},
		})
	})

	t.Run("EdgeCase-lspServers-mixed-array-checks-string-past-leading-object", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","lspServers":[{},"/outside"]}`)), false, []Finding{
			{Paths, LevelError, "'lspServers[1]' must not be an absolute path"},
		})
	})

	t.Run("EdgeCase-channels-array-of-object-legal", func(t *testing.T) {
		// Pinned source: upstream b75a02b1's vendored
		// tests/fixtures/schemas/claude-code-plugin.schema.json declares
		// "channels": {"type":"array","items":{"type":"object",
		// "properties":{"server":{"type":"string",...}},"required":
		// ["server"]}} -- elements are objects, never bare strings.
		assertReport(t, Validate([]byte(`{"name":"x","channels":[{"server":"telegram"}]}`)), false, nil)
	})

	t.Run("EdgeCase-channels-array-of-string-rejected", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","channels":["telegram"]}`)), false, []Finding{
			{Fields, LevelError, "'channels' must be an array of objects"},
		})
	})

	t.Run("EdgeCase-experimental-subscan-survives-unrepresentable-number", func(t *testing.T) {
		// Mirrors EdgeCase-key-order-scan-survives-unrepresentable-number but
		// exercises checkExperimental's own scanObjectKeys call on the
		// nested experimental object, not just Validate's top-level scan. The
		// trailing value is an absolute path (not a legal "./t.md") so a scan
		// truncated right after "foo" (never reaching "themes") produces a
		// different result than a complete one -- a legal trailing value
		// made this test pass identically either way (round-3 review).
		assertReport(t, Validate([]byte(`{"name":"x","experimental":{"foo":{"n":1e1000},"themes":"/outside"}}`)), false, []Finding{
			{Paths, LevelError, "'experimental.themes' must not be an absolute path"},
			{Unrecognized, LevelWarning, "unrecognized field 'experimental.foo'"},
		})
	})

	t.Run("EdgeCase-top-level-themes-belongs-under-experimental", func(t *testing.T) {
		// "dark" is missing the schema's required "./" prefix. Top-level
		// "themes" carries the same ^\./ pattern as its experimental.themes
		// counterpart, so it must be path-checked too (defect 2) -- the
		// top-level-placement warning does not exempt it from Paths.
		assertReport(t, Validate([]byte(`{"name":"x","themes":["dark"]}`)), false, []Finding{
			{Paths, LevelError, "'themes[0]' must start with './'"},
			{Unrecognized, LevelWarning, "'themes' belongs under 'experimental'"},
		})
	})

	t.Run("EdgeCase-top-level-monitors-belongs-under-experimental", func(t *testing.T) {
		// Pinned source: upstream b75a02b1's vendored tests/fixtures/schemas/
		// claude-code-plugin.schema.json declares "monitors" as anyOf a
		// "./"-prefixed .json path string or an array of objects (each
		// requiring name/command/description) -- never a bare string array.
		// A legal monitor object still triggers the top-level-placement
		// warning this test pins (round-3 review: the previous fixture
		// ["cpu"] was invalid shape that happened to pass the old kind
		// check, so it pinned nothing about a legal monitors value).
		//
		// Also guards defect 2's fix: "monitors" now carries IsPath: true,
		// but the object-array branch has no "./" pattern in the schema, so
		// this legal object-array value must still produce no Paths finding
		// (pathValues skips every element here, since none is a string).
		assertReport(t, Validate([]byte(`{"name":"x","monitors":[{"name":"cpu","command":"echo cpu","description":"CPU monitor"}]}`)), false, []Finding{
			{Unrecognized, LevelWarning, "'monitors' belongs under 'experimental'"},
		})
	})

	t.Run("EdgeCase-top-level-monitors-array-of-string-rejected", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","monitors":["cpu"]}`)), false, []Finding{
			{Fields, LevelError, "'monitors' must be a string or array of objects"},
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
		// Two warning-tier and two error-tier Fields mismatches interleaved
		// in file order (metadata, keywords, experimental, license): the
		// Fields check must still emit both errors first (in their own file
		// order), then both warnings (in their own file order) -- a single
		// error/warning pair cannot distinguish "grouped by level" from
		// "wrong order preserved as-is".
		assertReport(t, Validate([]byte(`{"name":"x","metadata":"z","keywords":"a","experimental":[],"license":123}`)), false, []Finding{
			{Fields, LevelError, "'keywords' must be an array of strings"},
			{Fields, LevelError, "'license' must be a string"},
			{Fields, LevelWarning, "'metadata' should be an object; Claude Code ignores other values"},
			{Fields, LevelWarning, "'experimental' should be an object; Claude Code ignores other values"},
		})
	})

	t.Run("EdgeCase-invalid-utf8", func(t *testing.T) {
		assertReport(t, Validate([]byte{0xFF}), true, []Finding{
			{Structure, LevelError, "invalid UTF-8"},
		})
	})

	t.Run("EdgeCase-empty-file", func(t *testing.T) {
		assertStructureError(t, Validate([]byte("")), "invalid JSON: unexpected end of JSON input")
	})

	t.Run("EdgeCase-deep-nesting-1MiB", func(t *testing.T) {
		data := bytes.Repeat([]byte("["), 1024*1024)
		assertStructureError(t, Validate(data), "invalid JSON: exceeded max depth")
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

	t.Run("EdgeCase-key-order-scan-survives-unrepresentable-number", func(t *testing.T) {
		// metadata.n (1e1000) cannot be decoded as float64 by the default
		// json.Decoder token stream; the key-order scan must still reach
		// 'skills' afterwards instead of silently truncating the manifest's
		// known-field/duplicate-key scan at "metadata".
		assertReport(t, Validate([]byte(`{"name":"x","metadata":{"n":1e1000},"skills":"/outside"}`)), false, []Finding{
			{Paths, LevelError, "'skills' must not be an absolute path"},
		})
	})

	t.Run("EdgeCase-dependencies-whole-field-not-array", func(t *testing.T) {
		// contracts/cli-plugin-validate.md's Messages table has no dedicated
		// scenario for "dependencies" itself being non-array (only the
		// dependencies[i] element messages are listed); checkDependencies'
		// own doc comment picks "must be an array" to follow that table's
		// naming convention for the unlisted case.
		assertReport(t, Validate([]byte(`{"name":"x","dependencies":"oops"}`)), false, []Finding{
			{Fields, LevelError, "'dependencies' must be an array"},
		})
	})

	t.Run("EdgeCase-author-unknown-subfield-ignored", func(t *testing.T) {
		// checkAuthor only type-checks name/email/url; data-model.md's rule
		// table has no unrecognized-subfield check for nested author keys
		// (unlike experimental), so an extra key produces no finding at all.
		assertReport(t, Validate([]byte(`{"name":"x","author":{"foo":"bar"}}`)), false, nil)
	})

	t.Run("EdgeCase-experimental-subkey-did-you-mean", func(t *testing.T) {
		// 'theme' is a Damerau-Levenshtein distance of 1 from 'themes'
		// (contracts/cli-plugin-validate.md: suggestion when distance <= 2),
		// exercising unrecognizedExperimentalFinding's own suggestion branch
		// rather than the top-level unrecognizedFinding one US2-AS1 pins.
		assertReport(t, Validate([]byte(`{"name":"x","experimental":{"theme":1}}`)), false, []Finding{
			{Unrecognized, LevelWarning, "unrecognized field 'experimental.theme' (did you mean 'themes'?)"},
		})
	})

	t.Run("EdgeCase-keywords-element-not-string", func(t *testing.T) {
		// Distinct from US1-AS4 (keywords itself not an array): here the
		// field IS an array, but one element is not a string, so the
		// failure comes from decodeArrayOfStrings' element loop rather than
		// the top-level isJSONArray check.
		assertReport(t, Validate([]byte(`{"name":"x","keywords":[1]}`)), false, []Finding{
			{Fields, LevelError, "'keywords' must be an array of strings"},
		})
	})

	t.Run("EdgeCase-hooks-bare-scalar-rejected", func(t *testing.T) {
		// Neither a path string, an inline object, nor an array -- must fail
		// before ever reaching isArrayOfStringOrObject's element loop
		// (EdgeCase-hooks-array-element-wrong-type-rejected covers that).
		assertReport(t, Validate([]byte(`{"name":"x","hooks":42}`)), false, []Finding{
			{Fields, LevelError, "'hooks' must be a string, array, or object"},
		})
	})

	t.Run("EdgeCase-channels-bare-scalar-rejected", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","channels":42}`)), false, []Finding{
			{Fields, LevelError, "'channels' must be an array of objects"},
		})
	})

	t.Run("EdgeCase-defaultEnabled-wrong-type", func(t *testing.T) {
		assertReport(t, Validate([]byte(`{"name":"x","defaultEnabled":"yes"}`)), false, []Finding{
			{Fields, LevelError, "'defaultEnabled' must be a boolean"},
		})
	})
}

// TestCheckStringUnknown pins Check.String()'s fallback for a Check value
// outside the five declared constants -- unreachable through Validate()
// (which only ever constructs the five), so it is exercised directly on the
// exported type/method.
func TestCheckStringUnknown(t *testing.T) {
	if got := Check(99).String(); got != "Unknown" {
		t.Errorf("Check(99).String() = %q, want %q", got, "Unknown")
	}
}

// TestValidateInternalDefensiveBranches exercises the package's own
// defensive contract on its unexported helpers directly: never panic on
// malformed input, return the documented empty/zero result instead.
//
// Every raw value Validate() itself hands to these helpers is already a
// complete, syntactically-valid JSON fragment extracted by an earlier
// json.Unmarshal/json.Decoder pass over the same bytes -- re-decoding it can
// therefore never fail, which makes these branches unreachable through
// Validate([]byte) alone (confirmed empirically: re-unmarshaling identical
// bytes, including pathological ones like 1e1000, duplicate keys, and lone
// UTF-16 surrogates, never diverges from the first, successful parse). The
// only way to observe the guard is to feed the helper input a real parse
// could never produce, which is legitimate here because validate_test.go is
// `package pluginjson`, not an external test package.
func TestValidateInternalDefensiveBranches(t *testing.T) {
	t.Run("checkAuthor-malformed-object", func(t *testing.T) {
		if got := checkAuthor(json.RawMessage(`{"name":`)); got != nil {
			t.Errorf("checkAuthor(malformed) = %+v, want nil", got)
		}
	})

	t.Run("checkDependencies-malformed-array", func(t *testing.T) {
		if got := checkDependencies(json.RawMessage(`[1,`)); got != nil {
			t.Errorf("checkDependencies(malformed) = %+v, want nil", got)
		}
	})

	t.Run("checkDependencyElement-malformed-object", func(t *testing.T) {
		if got := checkDependencyElement(0, json.RawMessage(`{"name":`)); got != nil {
			t.Errorf("checkDependencyElement(malformed) = %+v, want nil", got)
		}
	})

	t.Run("checkExperimental-malformed-object", func(t *testing.T) {
		ee, ep, eu := checkExperimental(json.RawMessage(`{"themes":`))
		if ee != nil || ep != nil || eu != nil {
			t.Errorf("checkExperimental(malformed) = (%+v, %+v, %+v), want (nil, nil, nil)", ee, ep, eu)
		}
	})

	t.Run("rawKind-empty-matches-no-kind", func(t *testing.T) {
		if got := rawKind(json.RawMessage("")); got != 0 {
			t.Errorf("rawKind(empty) = %v, want 0", got)
		}
		if isJSONString(json.RawMessage("")) || isJSONObject(json.RawMessage("  ")) ||
			isJSONArray(json.RawMessage("")) || isJSONBool(json.RawMessage("")) {
			t.Error("empty/whitespace RawMessage must match no JSON kind")
		}
	})

	t.Run("decodeString-truncated", func(t *testing.T) {
		s, ok := decodeString(json.RawMessage(`"abc`))
		if ok || s != "" {
			t.Errorf("decodeString(truncated) = (%q, %v), want (\"\", false)", s, ok)
		}
	})

	t.Run("decodeArrayOfStrings-malformed", func(t *testing.T) {
		got, ok := decodeArrayOfStrings(json.RawMessage(`[1,`))
		if ok || got != nil {
			t.Errorf("decodeArrayOfStrings(malformed) = (%v, %v), want (nil, false)", got, ok)
		}
	})

	t.Run("checkKind-dependencyList-implemented-for-completeness", func(t *testing.T) {
		// checkKind's own doc comment: kindDependencyList is only reached
		// defensively today (Validate special-cases "dependencies" before
		// ever calling checkKind with it), since data-model.md's rule table
		// lists dependency-list as a legitimate Kind.
		if !checkKind(json.RawMessage(`[]`), kindDependencyList) {
			t.Error("checkKind([], kindDependencyList) = false, want true")
		}
		if checkKind(json.RawMessage(`{}`), kindDependencyList) {
			t.Error("checkKind({}, kindDependencyList) = true, want false")
		}
	})

	t.Run("checkKind-unknown-kind-rejects", func(t *testing.T) {
		if checkKind(json.RawMessage(`"x"`), ruleKind(99)) {
			t.Error("checkKind with an unknown ruleKind = true, want false")
		}
	})

	t.Run("isArrayOfStringOrObject-malformed-array", func(t *testing.T) {
		if isArrayOfStringOrObject(json.RawMessage(`[1,`)) {
			t.Error("isArrayOfStringOrObject(malformed) = true, want false")
		}
	})

	t.Run("isArrayOfObjects-malformed-array", func(t *testing.T) {
		if isArrayOfObjects(json.RawMessage(`[1,`)) {
			t.Error("isArrayOfObjects(malformed) = true, want false")
		}
	})

	t.Run("kindMismatchMessage-dependencyList", func(t *testing.T) {
		if got := kindMismatchMessage("dependencies", kindDependencyList); got != "'dependencies' must be an array" {
			t.Errorf("kindMismatchMessage(dependencyList) = %q, want %q", got, "'dependencies' must be an array")
		}
	})

	t.Run("kindMismatchMessage-unknown-kind", func(t *testing.T) {
		want := "'field' has an unrecognized type"
		if got := kindMismatchMessage("field", ruleKind(99)); got != want {
			t.Errorf("kindMismatchMessage(unknown) = %q, want %q", got, want)
		}
	})

	t.Run("pathValues-malformed-array", func(t *testing.T) {
		elems, isArray, ok := pathValues(json.RawMessage(`[1,`))
		if elems != nil || isArray || ok {
			t.Errorf("pathValues(malformed) = (%v, %v, %v), want (nil, false, false)", elems, isArray, ok)
		}
	})
}

// TestScanObjectKeysDefensive exercises scanObjectKeys' own error returns
// directly with byte sequences no Validate() call can produce (every caller
// hands it a value already known syntactically valid) -- see
// TestValidateInternalDefensiveBranches' doc comment for why direct calls
// are the only way to reach these.
func TestScanObjectKeysDefensive(t *testing.T) {
	assertEmpty := func(t *testing.T, label string, order []string, counts map[string]int) {
		t.Helper()
		if len(order) != 0 || len(counts) != 0 {
			t.Errorf("%s: scanObjectKeys = (%v, %v), want both empty", label, order, counts)
		}
	}

	t.Run("empty-input-first-token-eof", func(t *testing.T) {
		order, counts := scanObjectKeys([]byte(""))
		assertEmpty(t, "empty", order, counts)
	})

	t.Run("top-level-array-not-object", func(t *testing.T) {
		order, counts := scanObjectKeys([]byte("[1,2]"))
		assertEmpty(t, "array", order, counts)
	})

	t.Run("malformed-key-token", func(t *testing.T) {
		order, counts := scanObjectKeys([]byte(`{@`))
		assertEmpty(t, "malformed key", order, counts)
	})

	t.Run("malformed-value-token", func(t *testing.T) {
		order, counts := scanObjectKeys([]byte(`{"a":`))
		assertEmpty(t, "malformed value", order, counts)
	})
}
