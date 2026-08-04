// Tests for this sub-task's (07-29-agent-schema-spec) R2/R3, plugin.json
// family: apm-go's own self-authored JSON Schemas for the claude and
// copilot plugin.json output shapes (testdata/apm-plugin-claude.schema.json,
// testdata/apm-plugin-copilot.schema.json), sharing the same anti-drift
// design as internal/marketplace/build/schema_sync_test.go -- see that
// file's doc comment for the SchemaGolden/SchemaReject/SchemaDrift/
// SchemaSync naming contract this file also follows.
//
// PluginManifest (pluginjson.go) is ONE Go type shared by both ecosystems;
// mcpServers is only ever populated for claude (pluginmanifest/producer.go's
// Produce, `if ecosystem == "claude"`). So the struct-vs-schema drift check
// runs against the claude schema (the full/superset field list); the
// copilot schema's deliberate omission of mcpServers is locked down
// separately by TestSchemaDrift_CopilotSchemaIsClaudeMinusMCPServers AND (a
// Tier-2 audit finding: static golden files can't catch a broken serializer)
// by TestSchemaGolden_LiveOutput_* below, which calls the REAL
// PluginManifest.ToJSONValue rather than reading a static fixture.
//
// codex audit round 7 (BLOCKING-2) found this package's mcpServers node
// (the one free-form/map-typed schema node in either plugin.json schema,
// mirroring the marketplace package's "metadata"/"author" exceptions) had no
// structural protection at all -- TestSchemaDrift_SchemaStructuralInvariants
// below (mirroring internal/marketplace/build/schema_sync_test.go's
// identical-in-spirit walker) closes that.
package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

func compileApmSchema(t *testing.T, path string) *jsonschema.Schema {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(path, strings.NewReader(string(data))); err != nil {
		t.Fatalf("AddResource(%s): %v", path, err)
	}
	schema, err := compiler.Compile(path)
	if err != nil {
		t.Fatalf("Compile(%s): %v", path, err)
	}
	return schema
}

func validateJSONFile(t *testing.T, schema *jsonschema.Schema, path string) error {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return schema.Validate(v)
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod above %s", dir)
		}
		dir = parent
	}
}

// fieldInfo is one exported struct field's json-tag-derived contract --
// mirrors internal/marketplace/build/schema_sync_test.go's identical type
// (kept as a separate copy since these are two different packages' test
// files with no shared internal test-helper package).
type fieldInfo struct {
	name     string
	required bool
	kind     reflect.Kind
}

func structFieldInfos(t *testing.T, typ reflect.Type) []fieldInfo {
	t.Helper()
	var out []fieldInfo
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.PkgPath != "" {
			continue // unexported
		}
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			t.Fatalf("field %s.%s has no usable json tag (got %q)", typ.Name(), f.Name, tag)
		}
		parts := strings.Split(tag, ",")
		name := parts[0]
		omitempty := false
		for _, p := range parts[1:] {
			if p == "omitempty" {
				omitempty = true
			}
		}
		out = append(out, fieldInfo{name: name, required: !omitempty, kind: f.Type.Kind()})
	}
	return out
}

// expectedSchemaType maps a Go reflect.Kind to the JSON Schema "type" a
// field of that kind should declare. PluginManifest/Author have no
// interface{}-typed fields, so unlike the marketplace package's identical
// helper, ok=false here always indicates an unhandled/undocumented Kind.
func expectedSchemaType(k reflect.Kind) (schemaType string, ok bool) {
	switch k {
	case reflect.String:
		return "string", true
	case reflect.Slice, reflect.Array:
		return "array", true
	case reflect.Map, reflect.Struct, reflect.Ptr:
		return "object", true
	default:
		return "", false
	}
}

func schemaDoc(t *testing.T, schemaPath string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read %s: %v", schemaPath, err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("unmarshal %s: %v", schemaPath, err)
	}
	return root
}

func schemaNode(t *testing.T, schemaPath string, path []string) map[string]any {
	t.Helper()
	node := schemaDoc(t, schemaPath)
	for _, p := range path {
		next, ok := node[p].(map[string]any)
		if !ok {
			t.Fatalf("schema %s: path %v: no object at %q", schemaPath, path, p)
		}
		node = next
	}
	return node
}

func resolveRefNode(root map[string]any, ref string) map[string]any {
	name := strings.TrimPrefix(ref, "#/$defs/")
	defs, _ := root["$defs"].(map[string]any)
	node, _ := defs[name].(map[string]any)
	return node
}

func schemaPropsAndRequired(node map[string]any) (props, required []string) {
	if propsRaw, ok := node["properties"].(map[string]any); ok {
		for k := range propsRaw {
			props = append(props, k)
		}
	}
	if reqRaw, ok := node["required"].([]any); ok {
		for _, r := range reqRaw {
			if s, ok := r.(string); ok {
				required = append(required, s)
			}
		}
	}
	return props, required
}

// schemaPropertyType resolves the JSON Schema "type" declared for property
// propName on node ($ref values resolved one level against root). Neither
// PluginManifest nor Author's schema nodes are oneOf-shaped, unlike the
// marketplace package's remoteSource, so no oneOf-branch search is needed
// here.
func schemaPropertyType(root, node map[string]any, propName string) (string, bool) {
	propsRaw, ok := node["properties"].(map[string]any)
	if !ok {
		return "", false
	}
	propNode, ok := propsRaw[propName].(map[string]any)
	if !ok {
		return "", false
	}
	if ref, ok := propNode["$ref"].(string); ok {
		target := resolveRefNode(root, ref)
		if target == nil {
			return "", false
		}
		if typ, ok := target["type"].(string); ok {
			return typ, true
		}
		return "", false
	}
	if typ, ok := propNode["type"].(string); ok {
		return typ, true
	}
	return "", false
}

func assertFieldSetsEqual(t *testing.T, label string, a, b []string) {
	t.Helper()
	as := map[string]bool{}
	for _, x := range a {
		as[x] = true
	}
	bs := map[string]bool{}
	for _, x := range b {
		bs[x] = true
	}
	var onlyA, onlyB []string
	for k := range as {
		if !bs[k] {
			onlyA = append(onlyA, k)
		}
	}
	for k := range bs {
		if !as[k] {
			onlyB = append(onlyB, k)
		}
	}
	if len(onlyA) == 0 && len(onlyB) == 0 {
		return
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	t.Errorf("%s: field sets differ -- only in first set: %v; only in second set: %v", label, onlyA, onlyB)
}

// specRow, tableRowRe, specTableRows, specTypeCategory, specRequiredness,
// specSchemaCase, and assertSpecTableMatchesSchema are this file's own copy
// of internal/marketplace/build/schema_sync_test.go's identical machinery
// (Tier-2 audit fix: SchemaSync originally only compared field NAMES; this
// extends it to the 型別/必填選填 columns too) -- duplicated rather than
// shared since these are two separate packages with no common internal
// test-helper package (same rationale as compileApmSchema/schemaDoc/etc.
// already being duplicated per-package in this file).
type specRow struct {
	name        string
	typeRaw     string
	requiredRaw string
}

var tableRowRe = regexp.MustCompile("(?m)^\\|\\s*`([A-Za-z][A-Za-z0-9]*)`\\s*\\|\\s*([^|]*?)\\s*\\|\\s*([^|]*?)\\s*\\|")

// discoveredTable, discoverFieldTables, headingStartOffsets,
// specTableRowsAt, bindSpecSchemaCase: see internal/marketplace/build/
// schema_sync_test.go's identical-in-spirit copies for the full rationale
// (Tier-8 audit fix: a plain strings.Index substring search for a
// sub-heading only ever finds the FIRST occurrence, so a duplicate/bogus
// table sharing the same heading prefix as a real one silently had its own
// content never parsed or compared against anything).
type discoveredTable struct {
	heading       string
	headingOffset int
	headerOffset  int
}

func discoverFieldTables(content string) []discoveredTable {
	headerMatches := fieldTableHeaderRe.FindAllStringIndex(content, -1)
	headingMatches := anyHeadingRe.FindAllStringIndex(content, -1)
	var out []discoveredTable
	for _, hm := range headerMatches {
		headerStart := hm[0]
		heading := ""
		headingOffset := -1
		for _, hd := range headingMatches {
			if hd[0] >= headerStart {
				break
			}
			heading = strings.TrimSpace(content[hd[0]:hd[1]])
			headingOffset = hd[0]
		}
		out = append(out, discoveredTable{heading: heading, headingOffset: headingOffset, headerOffset: headerStart})
	}
	return out
}

func headingStartOffsets(content string) []int {
	headingMatches := anyHeadingRe.FindAllStringIndex(content, -1)
	out := make([]int, len(headingMatches))
	for i, hd := range headingMatches {
		out[i] = hd[0]
	}
	return out
}

func specTableRowsAt(content string, headingOffset int, headingStarts []int) []specRow {
	end := len(content)
	for _, off := range headingStarts {
		if off > headingOffset {
			end = off
			break
		}
	}
	segment := content[headingOffset:end]
	var rows []specRow
	for _, m := range tableRowRe.FindAllStringSubmatch(segment, -1) {
		rows = append(rows, specRow{name: m[1], typeRaw: strings.TrimSpace(m[2]), requiredRaw: strings.TrimSpace(m[3])})
	}
	return rows
}

func bindSpecSchemaCase(t *testing.T, tables []discoveredTable, c specSchemaCase) discoveredTable {
	t.Helper()
	var matches []discoveredTable
	for _, tbl := range tables {
		if strings.HasPrefix(tbl.heading, c.subHeading) {
			matches = append(matches, tbl)
		}
	}
	if len(matches) != 1 {
		var offsets []int
		for _, m := range matches {
			offsets = append(offsets, m.headingOffset)
		}
		t.Fatalf("specSchemaCases entry %q matched %d field tables (want exactly 1); heading byte offsets: %v", c.subHeading, len(matches), offsets)
	}
	return matches[0]
}

// allowedTypeLiterals is a CLOSED, EXACT-STRING vocabulary of every 型別
// column value that currently appears in agent-schema.md's plugin.json
// section (Tier-3 audit fix: the previous strings.HasPrefix(raw, "string（")/
// "object（" check accepted ANY parenthetical suffix -- e.g.
// "string（garbage-not-a-type）" -- as a valid string/object column instead
// of failing closed). Transcribed byte-for-byte from the two plugin.json
// sub-tables (`grep -oE` over their 型別 columns) -- no enum-bearing field
// exists in this family (unlike the marketplace package's identical-in-
// spirit copy), and no oneOf-typed field either, so this map is simpler:
// just the plain category, no enumValues/skip machinery needed.
var allowedTypeLiterals = map[string]string{
	"string":        "string",
	"object":        "object",
	"array[string]": "array",
}

// specTypeCategory looks up raw in the closed allowedTypeLiterals vocabulary
// (exact string match, not a prefix). ok=false for anything else
// (fail-closed).
func specTypeCategory(raw string) (category string, skip bool, ok bool) {
	cat, found := allowedTypeLiterals[raw]
	if !found {
		return "", false, false
	}
	return cat, false, true
}

func specRequiredness(raw string) (required, ok bool) {
	switch {
	case strings.HasPrefix(raw, "必填"):
		return true, true
	case strings.HasPrefix(raw, "選填"):
		return false, true
	default:
		return false, false
	}
}

// specSchemaCase pairs one spec sub-table (identified by its "### " heading
// text) with the schema file/path it documents. Neither PluginManifest's
// root nor Author's $defs node is oneOf-shaped (unlike the marketplace
// package's remoteSource), so unlike that package's version, plain
// schemaPropsAndRequired (not an oneOf-aware variant) is enough here --
// required-ness is therefore a straight equality check, no
// intersection/subset nuance needed.
type specSchemaCase struct {
	subHeading string
	schemaFile string
	path       []string
}

var specSchemaCases = []specSchemaCase{
	{"### 欄位（`PluginManifest`", "testdata/apm-plugin-claude.schema.json", nil},
	{"### author（`Author`", "testdata/apm-plugin-claude.schema.json", []string{"$defs", "author"}},
}

func assertSpecTableMatchesSchema(t *testing.T, rows []specRow, c specSchemaCase) {
	t.Helper()
	if len(rows) == 0 {
		t.Fatalf("sub-heading %q: no table rows found", c.subHeading)
	}
	root := schemaDoc(t, c.schemaFile)
	node := schemaNode(t, c.schemaFile, c.path)
	_, schemaRequired := schemaPropsAndRequired(node)
	schemaRequiredSet := map[string]bool{}
	for _, r := range schemaRequired {
		schemaRequiredSet[r] = true
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			category, skip, ok := specTypeCategory(row.typeRaw)
			if !ok {
				t.Errorf("field %q: unrecognized 型別 column %q -- add a case to specTypeCategory or document it as residual", row.name, row.typeRaw)
			} else if !skip {
				actual, found := schemaPropertyType(root, node, row.name)
				if !found {
					t.Errorf("field %q: schema has no resolvable type to compare against spec's %q", row.name, row.typeRaw)
				} else if actual != category {
					t.Errorf("field %q: spec 型別 %q implies schema type %q, schema declares %q", row.name, row.typeRaw, category, actual)
				}
			}

			required, ok := specRequiredness(row.requiredRaw)
			if !ok {
				t.Errorf("field %q: unrecognized 必填/選填 column %q -- add a case to specRequiredness", row.name, row.requiredRaw)
				return
			}
			if required != schemaRequiredSet[row.name] {
				t.Errorf("field %q: spec says required=%v (%q), schema required list says %v", row.name, required, row.requiredRaw, schemaRequiredSet[row.name])
			}
		})
	}
}

func TestSchemaSync_SpecMatchesSchemaTypesAndRequiredness(t *testing.T) {
	specPath := filepath.Join(findRepoRoot(t), ".trellis", "spec", "conformance", "agent-schema.md")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}
	content := string(data)
	tables := discoverFieldTables(content)
	headingStarts := headingStartOffsets(content)
	for _, c := range specSchemaCases {
		t.Run(c.subHeading, func(t *testing.T) {
			bound := bindSpecSchemaCase(t, tables, c)
			rows := specTableRowsAt(content, bound.headingOffset, headingStarts)
			assertSpecTableMatchesSchema(t, rows, c)
		})
	}
}

func fieldSet(names ...string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		out[n] = true
	}
	return out
}

func assertFieldMapsEqual(t *testing.T, label string, got, want map[string]bool) {
	t.Helper()
	var onlyGot, onlyWant []string
	for k := range got {
		if !want[k] {
			onlyGot = append(onlyGot, k)
		}
	}
	for k := range want {
		if !got[k] {
			onlyWant = append(onlyWant, k)
		}
	}
	if len(onlyGot) == 0 && len(onlyWant) == 0 {
		return
	}
	sort.Strings(onlyGot)
	sort.Strings(onlyWant)
	t.Errorf("%s: field sets differ -- only in spec: %v; only in schema: %v", label, onlyGot, onlyWant)
}

// ── SchemaGolden (static fixtures) ───────────────────────────────────────

func TestSchemaGolden_PluginClaude_ValidatesAgainstApmSchema(t *testing.T) {
	schema := compileApmSchema(t, "testdata/apm-plugin-claude.schema.json")
	if err := validateJSONFile(t, schema, "testdata/apm-plugin-claude.golden.json"); err != nil {
		t.Errorf("golden claude plugin.json failed validation: %v", err)
	}
}

func TestSchemaGolden_PluginCopilot_ValidatesAgainstApmSchema(t *testing.T) {
	schema := compileApmSchema(t, "testdata/apm-plugin-copilot.schema.json")
	if err := validateJSONFile(t, schema, "testdata/apm-plugin-copilot.golden.json"); err != nil {
		t.Errorf("golden copilot plugin.json failed validation: %v", err)
	}
}

// TestSchemaGolden_UpstreamPluginInit_ValidatesAgainstBothSchemas and
// TestSchemaGolden_UpstreamPluginPack_ValidatesAgainstBothSchemas validate
// VERBATIM upstream (apm 0.26.0, 2026-07-28 eval run) plugin.json artifacts --
// AS4's literal "把 research/ 裡的上游實跑產物餵進去要通過" requirement, now actually
// satisfied for the plugin.json family too (an earlier round of this file used
// "derived-plugin-*.golden.json" fixtures reconstructed from research's FIELD-
// SET description alone, since research/eval-real-run-20260728.md itself has
// no verbatim JSON fence for either plugin.json shape; that gap is closed
// here). Transcribed 2026-07-31 by the main session directly from the eval
// working directory still on disk at
// D:/Projects/apm-dev/evals/apm-20260728T140015Z-1-001/apm-plugin-verify/single-plugin-repo/
// (2026-07-28 apm 0.26.0 run): upstream-plugin-init.golden.json is
// `plugin.json` (repo root, `apm plugin init` output); upstream-plugin-pack.golden.json
// is `.claude-plugin/plugin.json` (`apm pack` output, no license -- apm.yml has
// no license: field, matching research/eval-real-run-20260728.md:307-308's
// field-set description, now backed by the actual file contents).
//
// Both fixtures are expected to validate against BOTH ecosystem schemas: the
// init fixture's license is an optional field in both schemas (only
// mcpServers differs between claude/copilot), and the pack fixture has
// neither license nor mcpServers, so it trivially satisfies both.
func TestSchemaGolden_UpstreamPluginInit_ValidatesAgainstBothSchemas(t *testing.T) {
	for _, schemaFile := range []string{"testdata/apm-plugin-claude.schema.json", "testdata/apm-plugin-copilot.schema.json"} {
		schema := compileApmSchema(t, schemaFile)
		if err := validateJSONFile(t, schema, "testdata/upstream-plugin-init.golden.json"); err != nil {
			t.Errorf("upstream `apm plugin init` plugin.json failed validation against %s: %v", schemaFile, err)
		}
	}
}

func TestSchemaGolden_UpstreamPluginPack_ValidatesAgainstBothSchemas(t *testing.T) {
	for _, schemaFile := range []string{"testdata/apm-plugin-claude.schema.json", "testdata/apm-plugin-copilot.schema.json"} {
		schema := compileApmSchema(t, schemaFile)
		if err := validateJSONFile(t, schema, "testdata/upstream-plugin-pack.golden.json"); err != nil {
			t.Errorf("upstream `apm pack` plugin.json failed validation against %s: %v", schemaFile, err)
		}
	}
}

// ── SchemaGolden (live output -- exercises the real serializer) ─────────

// buildFullPluginManifest returns a PluginManifest with every field
// populated (a real *Author, a real mcpServers JSONValue) -- used by
// TestSchemaGolden_LiveOutput_* to exercise the actual ToJSONValue/
// authorValue serialization path end-to-end, rather than reading a static
// fixture off disk. includeMCPServers controls only whether MCPServers is
// set, mirroring producer.go's `if ecosystem == "claude"` gate.
func buildFullPluginManifest(includeMCPServers bool) *PluginManifest {
	m := &PluginManifest{
		Name:        "demo-plugin",
		Version:     "1.0.0",
		Description: "A demo plugin for schema testing.",
		Author:      &Author{Name: "Jane Doe", Email: "jane@example.com", URL: "https://example.com/jane"},
		License:     "MIT",
		Homepage:    "https://example.com/demo-plugin",
		Repository:  "https://github.com/acme/demo-plugin",
		Keywords:    []string{"demo", "schema"},
	}
	if includeMCPServers {
		mcp := ObjectValue(JSONField{Key: "demo", Val: ObjectValue(
			JSONField{Key: "command", Val: StringValue("npx")},
			JSONField{Key: "args", Val: ArrayOfStrings([]string{"-y", "demo-mcp-server"})},
		)})
		m.MCPServers = &mcp
	}
	return m
}

// decodeJSON unmarshals data into a generic `any` tree (objects become
// map[string]any, arrays []any) -- the normalized shape reflect.DeepEqual
// compares in validateLiveOutputAgainstSchema below.
func decodeJSON(t *testing.T, data []byte, label string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("unmarshal %s: %v\ndata: %s", label, err, data)
	}
	return v
}

// TestSchemaGolden_LiveOutput_PluginClaude and
// TestSchemaGolden_LiveOutput_PluginCopilot are this file's Tier-2-audit fix:
// every other test in this package either reads a STATIC fixture file or
// checks documentation-only json tags via reflection -- neither actually
// calls PluginManifest.ToJSONValue()/authorValue() with real data. A mutation
// that breaks authorValue (e.g. dropping the Email/URL appends, leaving only
// {"name": ...}) would sail through both of those unnoticed -- the schema
// only requires "name" on author, and the top-level key SET is unaffected by
// a field going missing one level down inside author. These two tests close
// that gap with a whole-tree comparison, not just a top-level-key-set one:
// build a fully-populated manifest (buildFullPluginManifest, matching
// exactly what committed testdata/apm-plugin-{claude,copilot}.golden.json
// were generated from), call the REAL serializer, and assert (a)
// schema.Validate() passes and (b) the live output, decoded to a generic
// `any` tree, is reflect.DeepEqual to the committed golden file decoded the
// same way -- a nested field going missing/wrong (author.email, author.url,
// or anything else at any depth) is now caught by (b) even when it wouldn't
// change the top-level key set or violate the schema's own (deliberately
// loose, name-only-required) author sub-schema.
func validateLiveOutputAgainstSchema(t *testing.T, m *PluginManifest, schemaFile, goldenFile, label string) {
	t.Helper()
	raw := MarshalIndent(m.ToJSONValue())
	live := decodeJSON(t, raw, "live "+label+" output")

	schema := compileApmSchema(t, schemaFile)
	if err := schema.Validate(live); err != nil {
		t.Errorf("live %s output failed validation: %v\noutput: %s", label, err, raw)
	}

	goldenRaw, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatalf("read %s: %v", goldenFile, err)
	}
	golden := decodeJSON(t, goldenRaw, goldenFile)

	if !reflect.DeepEqual(live, golden) {
		t.Errorf("live %s output does not match committed golden %s (whole-tree compare)\nlive:\n%s\ngolden:\n%s", label, goldenFile, raw, goldenRaw)
	}
}

func TestSchemaGolden_LiveOutput_PluginClaude(t *testing.T) {
	validateLiveOutputAgainstSchema(t, buildFullPluginManifest(true), "testdata/apm-plugin-claude.schema.json", "testdata/apm-plugin-claude.golden.json", "claude plugin.json")
}

func TestSchemaGolden_LiveOutput_PluginCopilot(t *testing.T) {
	validateLiveOutputAgainstSchema(t, buildFullPluginManifest(false), "testdata/apm-plugin-copilot.schema.json", "testdata/apm-plugin-copilot.golden.json", "copilot plugin.json")
}

// ── SchemaReject ─────────────────────────────────────────────────────────

func TestSchemaReject_PluginAuthorAsString(t *testing.T) {
	schema := compileApmSchema(t, "testdata/apm-plugin-claude.schema.json")
	invalid := map[string]any{
		"name":   "demo-plugin",
		"author": "Jane Doe", // must be {"name": ...}, not a bare string.
	}
	if err := schema.Validate(invalid); err == nil {
		t.Fatal("expected validation error for plugin.json 'author' given as a string")
	}
}

// TestSchemaReject_ClaudeGoldenWithMCPServers_FailsCopilotSchema is the
// negative-side confirmation that the claude/copilot schemas' one deliberate
// difference (mcpServers) is actually enforced, not just documented: the
// claude golden (which DOES contain mcpServers) must fail the copilot schema.
func TestSchemaReject_ClaudeGoldenWithMCPServers_FailsCopilotSchema(t *testing.T) {
	schema := compileApmSchema(t, "testdata/apm-plugin-copilot.schema.json")
	if err := validateJSONFile(t, schema, "testdata/apm-plugin-claude.golden.json"); err == nil {
		t.Fatal("expected the claude golden (which includes mcpServers) to fail validation against the copilot schema")
	}
}

// ── SchemaDrift ──────────────────────────────────────────────────────────

func TestSchemaDrift_GoTypesMatchSchemaProperties(t *testing.T) {
	type driftCase struct {
		name       string
		goType     reflect.Type
		schemaFile string
		path       []string
	}
	cases := []driftCase{
		{name: "PluginManifest", goType: reflect.TypeOf(PluginManifest{}), schemaFile: "testdata/apm-plugin-claude.schema.json"},
		{name: "Author", goType: reflect.TypeOf(Author{}), schemaFile: "testdata/apm-plugin-claude.schema.json", path: []string{"$defs", "author"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := schemaDoc(t, c.schemaFile)
			node := schemaNode(t, c.schemaFile, c.path)
			fields := structFieldInfos(t, c.goType)
			var goAll, goRequired []string
			for _, f := range fields {
				goAll = append(goAll, f.name)
				if f.required {
					goRequired = append(goRequired, f.name)
				}
			}
			schemaProps, schemaRequired := schemaPropsAndRequired(node)
			assertFieldSetsEqual(t, c.name+" properties", goAll, schemaProps)
			assertFieldSetsEqual(t, c.name+" required", goRequired, schemaRequired)

			for _, f := range fields {
				expected, ok := expectedSchemaType(f.kind)
				if !ok {
					t.Fatalf("%s: unhandled Go reflect.Kind %v for field %q -- add a case to expectedSchemaType", c.name, f.kind, f.name)
				}
				actual, found := schemaPropertyType(root, node, f.name)
				if !found {
					t.Errorf("%s: schema property %q has no resolvable \"type\" (expected %q)", c.name, f.name, expected)
					continue
				}
				if actual != expected {
					t.Errorf("%s: field %q type mismatch -- Go Kind %v implies schema type %q, schema declares %q", c.name, f.name, f.kind, expected, actual)
				}
			}
		})
	}
}

// TestSchemaDrift_CopilotSchemaIsClaudeMinusMCPServers locks down the one
// deliberate difference between the two plugin.json schema files: copilot's
// property set must equal claude's minus exactly "mcpServers" (PluginManifest
// is one shared Go type -- see this file's doc comment -- so this is the
// only place that difference is ever checked).
// schemaShapeNode is a canonical, comparable projection of a JSON Schema
// object node: its "type", its properties (recursively projected, $ref
// resolved), its "required" list (sorted), its "additionalProperties" flag
// (bool form) or nested schema (object form -- e.g. ClaudePlugin.author's
// map[string]string shape, not used by this package's own schemas today but
// projected defensively), its "enum" values (sorted), its array "items"
// (recursively projected), and its "oneOf" branches (recursively projected,
// in document order) -- everything structurally load-bearing, deliberately
// excluding free-prose keys ("description", "$id", "title", "$schema") that
// are expected to differ between the two files. A Tier-3 audit fix: an
// earlier version of this projection silently ignored "items"/"oneOf"
// entirely (so e.g. a nested `"oneOf":[{"enum":[...]}]` smuggled onto a
// property, or `keywords.items` being loosened to `{}`, changed nothing this
// projection could see) and only resolved "$ref" for the top-level node
// passed in, not recursively for every property -- see schemaShape's
// unknownSchemaKeys fail-closed check below, which additionally guards
// against a FUTURE unhandled keyword (e.g. "const", "pattern") silently
// falling through unprojected the same way.
type schemaShapeNode struct {
	Type                       string
	Properties                 map[string]*schemaShapeNode
	Required                   []string
	AdditionalProperties       *bool
	AdditionalPropertiesSchema *schemaShapeNode
	Enum                       []string
	Items                      *schemaShapeNode
	OneOf                      []*schemaShapeNode
}

func resolveRefOrSelf(root, node map[string]any) map[string]any {
	if ref, ok := node["$ref"].(string); ok {
		if resolved := resolveRefNode(root, ref); resolved != nil {
			return resolved
		}
	}
	return node
}

// schemaShapeHandledKeys is schemaShape's fail-closed keyword allowlist:
// every key here is either structurally projected (into a schemaShapeNode
// field) or explicitly ignored as free prose. Any OTHER top-level key on a
// schema object node -- a typo, or a real JSON Schema keyword this
// projection was never taught (e.g. "const", "pattern", "minLength") -- is
// a hard test failure, not a silent pass-through, so this file's deep-
// compare can never be quietly bypassed by using a keyword it doesn't know
// to look at.
var schemaShapeHandledKeys = map[string]bool{
	"type": true, "properties": true, "required": true, "additionalProperties": true,
	"enum": true, "items": true, "oneOf": true, "$ref": true,
	// Neutral: "$defs" is the document-level definitions container -- its
	// entries are only ever structurally relevant once dereferenced via a
	// "$ref" elsewhere (already projected then), so the container key
	// itself carries no independent shape to compare.
	"description": true, "title": true, "$schema": true, "$id": true, "$defs": true,
}

func schemaShape(t *testing.T, root, node map[string]any) *schemaShapeNode {
	t.Helper()
	node = resolveRefOrSelf(root, node)
	for k := range node {
		if !schemaShapeHandledKeys[k] {
			t.Fatalf("schemaShape: unrecognized schema keyword %q on node %v -- add explicit projection (or add to the neutral-key allowlist) before this deep-compare can be trusted", k, node)
		}
	}
	s := &schemaShapeNode{}
	// Fix 4 (Tier-6 audit): "type" must be a single string, never a JSON
	// array (a union-type escape hatch these schemas never need -- multi-
	// shape fields are always modeled via "oneOf", already projected above).
	// A silent `.(string)` type-assertion failure here would leave s.Type as
	// the empty string, indistinguishable from "no type declared at all" --
	// fail closed instead.
	if typRaw, exists := node["type"]; exists {
		typ, ok := typRaw.(string)
		if !ok {
			t.Fatalf("schemaShape: node %v has \"type\" = %#v, not a single string -- use \"oneOf\" for multi-shape fields instead of a JSON Schema type array", node, typRaw)
		}
		s.Type = typ
	}
	if reqRaw, ok := node["required"].([]any); ok {
		for _, r := range reqRaw {
			if str, ok := r.(string); ok {
				s.Required = append(s.Required, str)
			}
		}
		sort.Strings(s.Required)
	}
	switch ap := node["additionalProperties"].(type) {
	case bool:
		s.AdditionalProperties = &ap
	case map[string]any:
		s.AdditionalPropertiesSchema = schemaShape(t, root, ap)
	}
	if enumRaw, ok := node["enum"].([]any); ok {
		for _, e := range enumRaw {
			if str, ok := e.(string); ok {
				s.Enum = append(s.Enum, str)
			}
		}
		sort.Strings(s.Enum)
	}
	if propsRaw, ok := node["properties"].(map[string]any); ok {
		s.Properties = map[string]*schemaShapeNode{}
		for k, v := range propsRaw {
			vNode, _ := v.(map[string]any)
			s.Properties[k] = schemaShape(t, root, vNode)
		}
	}
	if itemsRaw, ok := node["items"].(map[string]any); ok {
		s.Items = schemaShape(t, root, itemsRaw)
	}
	if oneOfRaw, ok := node["oneOf"].([]any); ok {
		for _, br := range oneOfRaw {
			brNode, _ := br.(map[string]any)
			s.OneOf = append(s.OneOf, schemaShape(t, root, brNode))
		}
	}
	return s
}

// TestSchemaDrift_CopilotSchemaIsClaudeMinusMCPServers is this file's Tier-3
// audit fix: the copilot schema was previously checked ONLY by a flat
// field-NAME-set comparison (allSchemaProperties, which flattens every
// $defs entry's properties into one big set) -- copilot's own required list,
// each property's type, and additionalProperties were never independently
// checked against anything (a mutation like tightening copilot's root
// "required" to ["name","version"] would sail through green, since the
// claude-side driftCase/specSchemaCase machinery only ever points AT the
// claude schema file). This single anchored check replaces that with a
// recursive DEEP comparison: copilot's schema shape (type/properties,
// recursing into $defs like author/required/additionalProperties/enum) must
// equal claude's shape with exactly "mcpServers" removed from its top-level
// properties. Because copilot's correctness is now defined as "claude minus
// mcpServers, structurally," it automatically inherits every drift
// protection already anchored on the claude schema file (TestSchemaDrift_
// GoTypesMatchSchemaProperties, TestSchemaSync_SpecMatchesSchema*) without
// a second, independently-maintained copy of those checks -- which is also
// why agent-schema.md's plugin.json section can stay a single shared table
// rather than duplicating one for each ecosystem.
func TestSchemaDrift_CopilotSchemaIsClaudeMinusMCPServers(t *testing.T) {
	claudeRoot := schemaDoc(t, "testdata/apm-plugin-claude.schema.json")
	copilotRoot := schemaDoc(t, "testdata/apm-plugin-copilot.schema.json")

	claudeShape := schemaShape(t, claudeRoot, claudeRoot)
	copilotShape := schemaShape(t, copilotRoot, copilotRoot)

	if claudeShape.Properties == nil || claudeShape.Properties["mcpServers"] == nil {
		t.Fatal("claude schema must declare mcpServers")
	}
	if copilotShape.Properties != nil && copilotShape.Properties["mcpServers"] != nil {
		t.Fatal("copilot schema must never declare mcpServers")
	}
	delete(claudeShape.Properties, "mcpServers")

	if !reflect.DeepEqual(claudeShape, copilotShape) {
		t.Errorf("copilot schema is not exactly claude-minus-mcpServers (recursive type/properties/required/additionalProperties/enum compare)\nclaude (minus mcpServers): %#v\ncopilot: %#v", claudeShape, copilotShape)
	}
}

// ── SchemaDrift: schema tree structural invariants (codex round-7 BLOCKING-2 fix) ─

// bundleApException mirrors internal/marketplace/build/schema_sync_test.go's
// identical-in-spirit apException: the EXACT expected shape of an
// additionalProperties exception. present records whether
// "additionalProperties" must appear on the node AT ALL (false for
// "mcpServers", which has no additionalProperties key whatsoever, same style
// as the marketplace package's "metadata"); value, when present is true, is
// the EXACT expected JSON value.
type bundleApException struct {
	present bool
	value   any
}

// bundleAdditionalPropertiesExceptions is this package's mirror of the
// marketplace package's additionalPropertiesExceptions. mcpServers is the
// ONLY free-form/map-typed node in either plugin.json schema (Go's
// PluginManifest.MCPServers is *JSONValue, an arbitrary passthrough object
// with no fixed key set -- pluginjson.go's doc comment on the MCPServers
// field) -- the claude schema declares it bare `{"type":"object"}` (no
// additionalProperties key at all); the copilot schema never declares
// mcpServers at all (already locked by TestSchemaDrift_
// CopilotSchemaIsClaudeMinusMCPServers), so its own exceptions map is empty.
// Confirmed exhaustive by reading both schema files in full before writing
// this map.
var bundleAdditionalPropertiesExceptions = map[string]map[string]bundleApException{
	"testdata/apm-plugin-claude.schema.json":  {"root.properties.mcpServers": {present: false}},
	"testdata/apm-plugin-copilot.schema.json": {},
}

// bundleSchemaTreeHandledKeys mirrors schemaShapeHandledKeys/the marketplace
// package's schemaTreeHandledKeys: every key here is either structurally
// walked or explicitly ignored as free prose; any other top-level schema
// keyword is a hard failure, not a silent pass-through.
var bundleSchemaTreeHandledKeys = map[string]bool{
	"type": true, "properties": true, "required": true, "additionalProperties": true,
	"enum": true, "items": true, "oneOf": true, "$ref": true,
	"description": true, "title": true, "$schema": true, "$id": true, "$defs": true,
}

// isPureRefNode reports whether node is a bare `{"$ref": "..."}` wrapper
// (nothing else) -- walkBundleSchemaTreeNode skips recursing into these,
// since the $defs entry they point at is already walked separately (once)
// by bundleSchemaTreeInvariants' own top-level $defs iteration.
func isPureRefNode(node map[string]any) bool {
	if node == nil {
		return false
	}
	_, hasRef := node["$ref"]
	return hasRef && len(node) == 1
}

// checkBundleAdditionalPropertiesInvariant mirrors the marketplace package's
// checkAdditionalPropertiesInvariant, including its BLOCKING-2 fix
// (codex round 7): additionalProperties:false is always fine (closed, no
// exception needed); anything else must match a
// bundleAdditionalPropertiesExceptions entry exactly (both its presence and,
// when present, its literal value) -- AND, since an exception node
// represents a free-form/map-typed Go value with no fixed key set
// (PluginManifest.MCPServers, a *JSONValue), it must not ALSO declare
// "properties" or "required". Concrete repro this closes: adding
// "required":["demo"] to $defs... no -- to root.properties.mcpServers
// (mcpServers has no $defs entry of its own, it's declared inline) would go
// completely unnoticed by every other check in this file, since none of them
// inspect mcpServers' own nested shape (TestSchemaDrift_
// GoTypesMatchSchemaProperties never recurses into it -- MCPServers is a
// *JSONValue, not a Go struct with its own json tags to compare -- and
// TestSchemaDrift_CopilotSchemaIsClaudeMinusMCPServers deletes the
// mcpServers property entirely before comparing), yet mcpjson.go's
// SanitizeServers accepts arbitrary server names, so requiring one specific
// name would reject legitimate output.
func checkBundleAdditionalPropertiesInvariant(t *testing.T, node map[string]any, path string, exceptions map[string]bundleApException) {
	t.Helper()
	apRaw, hasAP := node["additionalProperties"]
	if b, ok := apRaw.(bool); ok && !b {
		return // additionalProperties:false -- closed, correct, no exception needed.
	}
	exc, isException := exceptions[path]
	if !isException {
		reason := "no additionalProperties declaration at all (implicitly open)"
		if hasAP {
			reason = fmt.Sprintf("additionalProperties = %#v (not `false`)", apRaw)
		}
		t.Errorf("%s: type:object node is not closed (%s), and is not in bundleAdditionalPropertiesExceptions -- either set additionalProperties:false, or add+justify a whitelist entry", path, reason)
		return
	}
	if exc.present != hasAP {
		t.Errorf("%s: additionalProperties presence mismatch against the whitelisted exception -- exception expects present=%v, actual present=%v (%#v)", path, exc.present, hasAP, apRaw)
		return
	}
	if exc.present && !reflect.DeepEqual(exc.value, apRaw) {
		t.Errorf("%s: additionalProperties = %#v does not exactly match the whitelisted exception's expected shape %#v", path, apRaw, exc.value)
	}
	if _, hasProps := node["properties"]; hasProps {
		t.Errorf("%s: free-form/map-typed node (additionalProperties exception) must not declare \"properties\" -- it backs a Go map/JSONValue with no fixed key set", path)
	}
	if _, hasReq := node["required"]; hasReq {
		t.Errorf("%s: free-form/map-typed node (additionalProperties exception) must not declare \"required\" -- it backs a Go map/JSONValue with no fixed key set", path)
	}
}

// checkBundleArrayItemsInvariant mirrors the marketplace package's
// checkArrayItemsInvariant: every type:array node must have a resolvable
// non-empty items.type.
func checkBundleArrayItemsInvariant(t *testing.T, root, node map[string]any, path string) {
	t.Helper()
	itemsRaw, ok := node["items"].(map[string]any)
	if !ok {
		t.Errorf("%s: type:array node has no \"items\" schema", path)
		return
	}
	resolved := resolveRefOrSelf(root, itemsRaw)
	typ, ok := resolved["type"].(string)
	if !ok || typ == "" {
		t.Errorf("%s: type:array node's items has no resolvable non-empty \"type\" (items=%#v)", path, itemsRaw)
	}
}

// walkBundleSchemaTreeNode mirrors the marketplace package's
// walkSchemaTreeNode: fail-closed keyword check, single-string "type" check,
// then additionalProperties/array-items invariants, then recurses into every
// properties/items/oneOf-branch/additionalProperties sub-schema node
// (skipping pure $ref wrappers).
func walkBundleSchemaTreeNode(t *testing.T, root, node map[string]any, path string, exceptions map[string]bundleApException) {
	t.Helper()
	if node == nil {
		return
	}
	for k := range node {
		if !bundleSchemaTreeHandledKeys[k] {
			t.Errorf("%s: unrecognized schema keyword %q -- add explicit handling before this structural check can be trusted", path, k)
		}
	}

	if typRaw, exists := node["type"]; exists {
		if _, ok := typRaw.(string); !ok {
			t.Errorf("%s: \"type\" is %#v, not a single string -- use \"oneOf\" for multi-shape fields instead of a JSON Schema type array", path, typRaw)
		}
	}

	if typ, _ := node["type"].(string); typ == "object" {
		checkBundleAdditionalPropertiesInvariant(t, node, path, exceptions)
	}
	if typ, _ := node["type"].(string); typ == "array" {
		checkBundleArrayItemsInvariant(t, root, node, path)
	}

	if propsRaw, ok := node["properties"].(map[string]any); ok {
		names := make([]string, 0, len(propsRaw))
		for name := range propsRaw {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			v, _ := propsRaw[name].(map[string]any)
			if isPureRefNode(v) {
				continue
			}
			walkBundleSchemaTreeNode(t, root, v, path+".properties."+name, exceptions)
		}
	}
	if itemsRaw, ok := node["items"].(map[string]any); ok && !isPureRefNode(itemsRaw) {
		walkBundleSchemaTreeNode(t, root, itemsRaw, path+".items", exceptions)
	}
	if oneOfRaw, ok := node["oneOf"].([]any); ok {
		for i, br := range oneOfRaw {
			brNode, _ := br.(map[string]any)
			if isPureRefNode(brNode) {
				continue
			}
			walkBundleSchemaTreeNode(t, root, brNode, fmt.Sprintf("%s.oneOf[%d]", path, i), exceptions)
		}
	}
	if apRaw, ok := node["additionalProperties"].(map[string]any); ok {
		walkBundleSchemaTreeNode(t, root, apRaw, path+".additionalProperties", exceptions)
	}
}

// bundleSchemaTreeInvariants walks every object/array node reachable from
// schemaFile's document root (the root node itself, plus every $defs entry)
// and enforces the invariants above.
func bundleSchemaTreeInvariants(t *testing.T, schemaFile string) {
	t.Helper()
	root := schemaDoc(t, schemaFile)
	exceptions := bundleAdditionalPropertiesExceptions[schemaFile]
	walkBundleSchemaTreeNode(t, root, root, "root", exceptions)
	if defs, ok := root["$defs"].(map[string]any); ok {
		names := make([]string, 0, len(defs))
		for name := range defs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			defNode, _ := defs[name].(map[string]any)
			walkBundleSchemaTreeNode(t, root, defNode, "$defs."+name, exceptions)
		}
	}
}

func TestSchemaDrift_SchemaStructuralInvariants(t *testing.T) {
	for _, schemaFile := range []string{
		"testdata/apm-plugin-claude.schema.json",
		"testdata/apm-plugin-copilot.schema.json",
	} {
		t.Run(schemaFile, func(t *testing.T) {
			bundleSchemaTreeInvariants(t, schemaFile)
		})
	}
}

// ── SchemaSync: spec doc field tables <-> schema property (per-subtable) ─

// specSubTableFieldNames is a []specRow's field-NAME-only projection.
func specSubTableFieldNames(rows []specRow) map[string]bool {
	out := map[string]bool{}
	for _, row := range rows {
		out[row.name] = true
	}
	return out
}

// TestSchemaSync_SpecMatchesSchemaFieldSet compares each spec sub-table's
// field-name set against its OWN mapped schema node (specSchemaCases),
// never a family-wide flattened union (Tier-5 audit fix -- see
// internal/marketplace/build/schema_sync_test.go's identical-in-spirit copy
// for the full "why a union comparison is blind to a single table losing a
// row" rationale; PluginManifest/Author's own root/$defs.author schema
// nodes are used directly, no oneOf-union handling needed here). Each
// table's rows are read from its bindSpecSchemaCase-bound occurrence
// (Tier-8 audit fix), not a first-match substring search.
func TestSchemaSync_SpecMatchesSchemaFieldSet(t *testing.T) {
	specPath := filepath.Join(findRepoRoot(t), ".trellis", "spec", "conformance", "agent-schema.md")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}
	content := string(data)
	tables := discoverFieldTables(content)
	headingStarts := headingStartOffsets(content)
	for _, c := range specSchemaCases {
		t.Run(c.subHeading, func(t *testing.T) {
			bound := bindSpecSchemaCase(t, tables, c)
			rows := specTableRowsAt(content, bound.headingOffset, headingStarts)
			spec := specSubTableFieldNames(rows)
			node := schemaNode(t, c.schemaFile, c.path)
			props, _ := schemaPropsAndRequired(node)
			assertFieldMapsEqual(t, c.subHeading, spec, fieldSet(props...))
		})
	}
}

// fieldTableHeaderRe, anyHeadingRe: see internal/marketplace/build/
// schema_sync_test.go's identical-in-spirit copies for the full rationale
// (Tier-7 audit fix: a family-section-scoped scan misses a bogus field
// table pasted outside every recognized "## " section entirely, e.g. at the
// very end of the file).
var fieldTableHeaderRe = regexp.MustCompile(`(?m)^\| 欄位 \| 型別 \| 必填/選填 \| 預設值 \| 上游出處 \|\s*$`)
var anyHeadingRe = regexp.MustCompile(`(?m)^#{2,3} .+$`)

// foreignSubHeadings lists sub-headings this package's specSchemaCases does
// NOT itself map, but that legitimately belong to the marketplace package's
// own specSchemaCases (internal/marketplace/build/schema_sync_test.go,
// which carries the exact mirror-image list pointing back at this
// package's 2 plugin.json sub-headings).
var foreignSubHeadings = []string{
	"### 文件層（`ClaudeDocument`",
	"### owner（`ClaudeOwner`",
	"### plugins[]（`ClaudePlugin`",
	"### source（`RemoteSource`, `mapper.go:72`",
	"### 文件層（`CodexDocument`",
	"### interface（`CodexInterface`",
	"### plugins[]（`CodexPlugin`",
	"### policy（`CodexPolicy`",
	"### local source（`CodexLocalSource`",
	"### remote source（`RemoteSource`，與 Claude 共用同一個 Go 型別",
}

// firstBacktickTokenRe, subtestLabel: see internal/marketplace/build/
// schema_sync_test.go's identical-in-spirit copies for the full rationale
// (Tier-7 audit fix: CJK + fullwidth-punctuation test names, embedded in
// every `go test -json` event's "Test" field, were observed to break
// verify.ps1's line-by-line JSON parser on Windows/PowerShell).
var firstBacktickTokenRe = regexp.MustCompile("`([^`]+)`")

func subtestLabel(heading string, index int) string {
	m := firstBacktickTokenRe.FindStringSubmatch(heading)
	if m != nil {
		token := m[1]
		asciiOnly := true
		for _, r := range token {
			if r > 127 {
				asciiOnly = false
				break
			}
		}
		if asciiOnly && token != "" {
			return fmt.Sprintf("%02d_%s", index, token)
		}
	}
	return fmt.Sprintf("table_%02d", index)
}

// TestSchemaSync_AllFieldTablesAreMapped is this package's copy of
// internal/marketplace/build/schema_sync_test.go's identical-in-spirit
// whole-file scan: it finds every field-table header (fieldTableHeaderRe)
// anywhere in the WHOLE spec document (not scoped to the "## plugin.json"
// section), identifies its owning heading (nearest preceding "## "/"### "
// line by byte offset), asserts that heading is a prefix-match for some
// specSchemaCases entry (this package's own) or foreignSubHeadings entry
// (acknowledged as belonging to the marketplace package), and (Tier-8 audit
// fix) separately asserts each specSchemaCases entry matches EXACTLY ONE
// discovered table -- see build package's identical-in-spirit copy for the
// full "duplicate heading prefix" rationale.
func TestSchemaSync_AllFieldTablesAreMapped(t *testing.T) {
	specPath := filepath.Join(findRepoRoot(t), ".trellis", "spec", "conformance", "agent-schema.md")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}
	content := string(data)
	tables := discoverFieldTables(content)
	if len(tables) == 0 {
		t.Fatal("found zero field-table headers in the whole spec file -- fieldTableHeaderRe may be stale")
	}

	for i, tbl := range tables {
		t.Run(subtestLabel(tbl.heading, i), func(t *testing.T) {
			if tbl.heading == "" {
				t.Fatalf("field-table header at byte offset %d has no preceding ##/### heading to identify it", tbl.headerOffset)
			}
			mapped := false
			for _, c := range specSchemaCases {
				if strings.HasPrefix(tbl.heading, c.subHeading) {
					mapped = true
					break
				}
			}
			if !mapped {
				for _, fh := range foreignSubHeadings {
					if strings.HasPrefix(tbl.heading, fh) {
						mapped = true
						break
					}
				}
			}
			if !mapped {
				t.Errorf("field table under heading %q is not mapped in specSchemaCases (this package) or foreignSubHeadings (sibling package) -- fail-closed: add an entry, or this table silently escapes every drift check", tbl.heading)
			}
		})
	}

	for i, c := range specSchemaCases {
		t.Run(fmt.Sprintf("unique_%02d_%s", i, subtestLabel(c.subHeading, i)), func(t *testing.T) {
			bindSpecSchemaCase(t, tables, c)
		})
	}
}

// ── SchemaSync: schema-file hash seal (coordinator-mandated primary R3.1.2 mechanism) ──
//
// Mirrors internal/marketplace/build/schema_sync_test.go's identical-in-
// spirit copy -- see that file's section header comment for the full
// rationale (a byte-for-byte SHA-256 seal, independent of any semantic
// projection's necessarily bounded depth). own/foreign are swapped: this
// package verifies ITS OWN two rows (apm-plugin-claude/copilot) and
// acknowledges (without verifying) the marketplace package's own two rows.

// schemaFileHashRowRe/validSHA256HexRe/specSchemaFileHash/
// parseSchemaFileHashTable mirror internal/marketplace/build/
// schema_sync_test.go's identical-in-spirit copies -- see that file's doc
// comments for the full rationale (codex round-8 audit fix: row-detection is
// now based solely on the row's second column being a .schema.json path, NOT
// on its hash column already looking like valid hex -- the previous version
// made a malformed-hash row completely invisible to the parser, so a
// duplicate row with a 63-character "hash" silently escaped both the
// duplicate-row check and the mismatch check).
var schemaFileHashRowRe = regexp.MustCompile("^\\|[^|]*\\|\\s*`([^`]+\\.schema\\.json)`\\s*\\|.*`([^`]*)`\\s*\\|\\s*$")

var validSHA256HexRe = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

type specSchemaFileHash struct {
	specPath string
	hashRaw  string // literal hash-column text, UNVALIDATED -- may be malformed; see validSHA256HexRe
}

func parseSchemaFileHashTable(t *testing.T, content string) []specSchemaFileHash {
	t.Helper()
	const headingText = "## 可執行 schema 對照表"
	idx := strings.Index(content, headingText)
	if idx < 0 {
		t.Fatalf("spec is missing the %q heading", headingText)
	}
	rest := content[idx+len(headingText):]
	end := len(rest)
	if loc := regexp.MustCompile(`(?m)^## `).FindStringIndex(rest); loc != nil {
		end = loc[0]
	}
	segment := rest[:end]

	var out []specSchemaFileHash
	for _, line := range strings.Split(segment, "\n") {
		m := schemaFileHashRowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out = append(out, specSchemaFileHash{specPath: m[1], hashRaw: m[2]})
	}
	return out
}

// ownSchemaFileHashPaths is this package's own two schema files' full
// spec-table paths -- TestSchemaSync_SchemaFileHashesMatchSpec requires
// EXACTLY one row per entry here, with a SHA-256 matching the file's actual
// current bytes.
var ownSchemaFileHashPaths = []string{
	"internal/pack/bundle/testdata/apm-plugin-claude.schema.json",
	"internal/pack/bundle/testdata/apm-plugin-copilot.schema.json",
}

// foreignSchemaFileHashPaths acknowledges the marketplace package's
// (internal/marketplace/build) own two schema-file hash rows -- present in
// the same shared table, intentionally NOT verified by this package's own
// test (build's own TestSchemaSync_SchemaFileHashesMatchSpec verifies them
// instead) -- mirrors foreignSubHeadings' cross-acknowledgment pattern.
var foreignSchemaFileHashPaths = []string{
	"internal/marketplace/build/testdata/apm-claude-marketplace.schema.json",
	"internal/marketplace/build/testdata/apm-codex-marketplace.schema.json",
}

// sha256HexFile normalizes CRLF to LF before hashing -- see the twin in
// internal/marketplace/build/schema_sync_test.go for why (core.autocrlf makes a
// raw-byte hash platform-dependent, so a seal recorded from an LF working copy
// fails on a CRLF checkout of the same commit).
func sha256HexFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256([]byte(strings.ReplaceAll(string(data), "\r\n", "\n")))
	return hex.EncodeToString(sum[:])
}

// TestSchemaSync_SchemaFileHashesMatchSpec is the coordinator-mandated
// primary R3.1.2 mechanism -- see this section's header comment and
// internal/marketplace/build/schema_sync_test.go's identical-in-spirit copy
// for the full rationale. Fails closed on: a missing row for either of this
// package's own two schema files, more than one row for either (including a
// duplicate whose hash column is malformed -- a codex round-8 audit fix, see
// schemaFileHashRowRe's doc comment), a row whose hash column isn't a
// well-formed 64-hex string, a row whose SHA-256 doesn't match the file's
// actual current bytes, or an unrecognized schema-file hash row (neither
// this package's own nor the sibling's acknowledged one).
func TestSchemaSync_SchemaFileHashesMatchSpec(t *testing.T) {
	specPath := filepath.Join(findRepoRoot(t), ".trellis", "spec", "conformance", "agent-schema.md")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}
	rows := parseSchemaFileHashTable(t, string(data))
	if len(rows) == 0 {
		t.Fatal("found zero schema-file hash rows in \"## 可執行 schema 對照表\" -- schemaFileHashRowRe may be stale")
	}

	byPath := map[string][]specSchemaFileHash{}
	for _, r := range rows {
		byPath[r.specPath] = append(byPath[r.specPath], r)
	}

	recognized := map[string]bool{}
	for _, p := range ownSchemaFileHashPaths {
		recognized[p] = true
	}
	for _, p := range foreignSchemaFileHashPaths {
		recognized[p] = true
	}
	for _, r := range rows {
		if !recognized[r.specPath] {
			t.Errorf("unrecognized schema-file hash row for %q -- neither in ownSchemaFileHashPaths (this package) nor foreignSchemaFileHashPaths (sibling package) -- fail-closed: add an entry, or this row silently escapes the hash seal", r.specPath)
		}
	}

	repoRoot := findRepoRoot(t)
	for _, want := range ownSchemaFileHashPaths {
		t.Run(want, func(t *testing.T) {
			got := byPath[want]
			if len(got) == 0 {
				t.Fatalf("spec table has no SHA-256 row for %q", want)
			}
			if len(got) > 1 {
				var raws []string
				for _, r := range got {
					raws = append(raws, fmt.Sprintf("%q", r.hashRaw))
				}
				t.Fatalf("spec table has %d rows for %q, want exactly 1 -- row hash-column contents: [%s]", len(got), want, strings.Join(raws, ", "))
			}
			hashRaw := got[0].hashRaw
			if !validSHA256HexRe.MatchString(hashRaw) {
				t.Fatalf("malformed SHA-256 in spec table row for %q: %q (want exactly 64 lowercase hex characters)", want, hashRaw)
			}
			specHash := strings.ToLower(hashRaw)
			actualHash := sha256HexFile(t, filepath.Join(repoRoot, filepath.FromSlash(want)))
			if specHash != actualHash {
				t.Errorf("schema 檔已變更但 spec 對照表 SHA-256 未更新——先確認 spec 的欄位表/型別/enum 描述是否需同步，再更新 hash (%s: spec=%s, actual=%s)", want, specHash, actualHash)
			}
		})
	}
}
