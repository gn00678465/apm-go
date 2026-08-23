// Tests for this sub-task's (07-29-agent-schema-spec) R2/R3: apm-go's own
// self-authored JSON Schemas for the Claude and Codex marketplace.json
// output families (testdata/apm-claude-marketplace.schema.json,
// testdata/apm-codex-marketplace.schema.json -- NOT the upstream informational
// copy schema_test.go already validates against), plus the anti-drift
// machinery design.md's D3 requires:
//
//   - SchemaGolden: apm-go's own golden production output (testdata/
//     apm-*-marketplace.golden.json, generated directly from ClaudeMapper/
//     CodexMapper.Compose -- see this file's sibling testdata for how those
//     were produced) validates successfully against these schemas, AND (a
//     coupled-oracle fix, see this file's Upstream* tests) real upstream apm
//     0.26.0 artifacts also validate.
//   - SchemaReject: a deliberately broken variant (missing a required field)
//     fails validation.
//   - SchemaDrift: the Go struct's json-tag field set/required-ness AND
//     per-field JSON type are kept in a two-way (Go-extra AND schema-extra)
//     equality check, for every struct design.md's D3.1 lists. remoteSource
//     is oneOf-shaped (per-variant required fields, a Tier-2 audit finding --
//     see apm-claude-marketplace.schema.json/apm-codex-marketplace.schema.json's
//     doc comments), so its properties/required extraction is oneOf-aware
//     (union of branch properties, intersection of branch required).
//   - SchemaSync: the spec doc's (spec/conformance/agent-schema.md)
//     per-family markdown field tables are parsed and compared, as a flat
//     field-name set, against the union of that family's schema properties.
//
// codex audit round 7 closed three further oneOf/free-form escape routes,
// each a single-point-of-drift the checks above (by design) can't see:
// TestSchemaDrift_RemoteSourceBranchExactDiscriminatorEnum (a branch's OWN
// discriminator enum, as opposed to the union every other check compares
// against), the additionalProperties-exception "properties"/"required"
// guard in checkAdditionalPropertiesInvariant (a free-form/map-typed node
// gaining a fixed key requirement its Go map type can't actually enforce),
// and TestSchemaDrift_InterfaceFieldOneOfTopology (the exact branch LIST of
// an interface{}-typed field's oneOf, which the per-field type-check
// deliberately skips entirely via typeCheckSkip).
package build

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

	"github.com/apm-go/apm/internal/marketplace/authoring"
)

// ── shared helpers ──────────────────────────────────────────────────────

// compileApmSchema compiles one of this task's own self-authored schemas
// (as opposed to compileMarketplaceSchema in schema_test.go, which compiles
// the upstream informational copy).
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

// validateJSONFile loads path as a generic JSON value and validates it
// against schema, returning the validation error (nil on success).
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

// findRepoRoot walks up from the current working directory until it finds
// go.mod, mirroring design.md D3.2's "自 cwd 向上找 go.mod 定位".
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

// fieldInfo is one exported struct field's json-tag-derived contract: its
// wire name, whether it's unconditionally present (no "omitempty"), its Go
// Kind (used to infer the JSON type schema should declare for it), and --
// for reflect.Slice fields only -- the slice element's Kind (Fix 3b: cross-
// checking a Go []string field against the schema's own "items" type), and
// -- for reflect.Map fields only -- the map's VALUE Kind (Tier-8 audit Fix
// 3a: cross-checking a Go map[string]V field against the schema's own
// "additionalProperties.type").
type fieldInfo struct {
	name         string
	required     bool
	kind         reflect.Kind
	elemKind     reflect.Kind
	mapValueKind reflect.Kind
}

// structFieldInfos reflects over typ's exported fields.
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
		info := fieldInfo{name: name, required: !omitempty, kind: f.Type.Kind()}
		if info.kind == reflect.Slice || info.kind == reflect.Array {
			info.elemKind = f.Type.Elem().Kind()
		}
		if info.kind == reflect.Map {
			info.mapValueKind = f.Type.Elem().Kind()
		}
		out = append(out, info)
	}
	return out
}

// structJSONFields is structFieldInfos, projected down to just (all names,
// required names) -- the name-set/required-set comparisons that don't need
// per-field type info.
func structJSONFields(t *testing.T, typ reflect.Type) (all, required []string) {
	t.Helper()
	for _, f := range structFieldInfos(t, typ) {
		all = append(all, f.name)
		if f.required {
			required = append(required, f.name)
		}
	}
	return all, required
}

// expectedSchemaType maps a Go reflect.Kind to the JSON Schema "type" value
// schema_sync_test.go expects a field of that kind to declare. Returns
// ok=false for reflect.Interface (Go's `any`/interface{} -- e.g. a oneOf
// discriminated union like ClaudePlugin.Source/CodexPlugin.Source): callers
// must handle that case via an explicit per-driftCase skip list with a
// documented reason, never silently.
func expectedSchemaType(k reflect.Kind) (schemaType string, ok bool) {
	switch k {
	case reflect.String:
		return "string", true
	case reflect.Slice, reflect.Array:
		return "array", true
	case reflect.Map, reflect.Struct, reflect.Ptr:
		return "object", true
	case reflect.Interface:
		return "", false
	default:
		return "", false
	}
}

// schemaNode loads schemaPath and navigates into it via path (each element
// a JSON object key, e.g. []string{"$defs", "plugin"}); nil/empty path
// returns the document root.
func schemaNode(t *testing.T, schemaPath string, path []string) map[string]any {
	t.Helper()
	root := schemaDoc(t, schemaPath)
	node := root
	for _, p := range path {
		next, ok := node[p].(map[string]any)
		if !ok {
			t.Fatalf("schema %s: path %v: no object at %q", schemaPath, path, p)
		}
		node = next
	}
	return node
}

// schemaDoc reads and parses schemaPath as a generic JSON object (the
// document root, with $defs intact) -- used both directly and by schemaNode.
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

// resolveRefNode resolves a "#/$defs/<name>" ref string against root.
func resolveRefNode(root map[string]any, ref string) map[string]any {
	name := strings.TrimPrefix(ref, "#/$defs/")
	defs, _ := root["$defs"].(map[string]any)
	node, _ := defs[name].(map[string]any)
	return node
}

// resolveRefOrSelf returns node's $ref target (if it has one) or node itself.
func resolveRefOrSelf(root, node map[string]any) map[string]any {
	if ref, ok := node["$ref"].(string); ok {
		if resolved := resolveRefNode(root, ref); resolved != nil {
			return resolved
		}
	}
	return node
}

// schemaPropsAndRequired extracts a schema object node's "properties" keys
// and "required" array directly (non-oneOf case).
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

// schemaNodePropsAndRequired is schemaPropsAndRequired, but oneOf-aware: if
// node is `{"oneOf": [...]}` (each branch a $ref or inline object -- this
// schema's remoteSource shape, split into per-variant branches so each
// variant's actually-always-set fields can be its own "required" list --
// see apm-claude-marketplace.schema.json/apm-codex-marketplace.schema.json's
// $defs.remoteSource doc comments), properties is the UNION across every
// branch (a field is part of the type if any variant can carry it) and
// required is the INTERSECTION across every branch (a field is only
// unconditionally required if every variant demands it -- e.g. "source" for
// remoteSource, but not "repo"/"url"/"path", which are each specific to one
// or two variants only).
func schemaNodePropsAndRequired(root, node map[string]any) (props, required []string, isOneOf bool) {
	branchesRaw, ok := node["oneOf"].([]any)
	if !ok {
		p, r := schemaPropsAndRequired(node)
		return p, r, false
	}
	propSet := map[string]bool{}
	var requiredSets []map[string]bool
	for _, br := range branchesRaw {
		brNode, _ := br.(map[string]any)
		resolved := resolveRefOrSelf(root, brNode)
		p, r := schemaPropsAndRequired(resolved)
		for _, pp := range p {
			propSet[pp] = true
		}
		rs := map[string]bool{}
		for _, rr := range r {
			rs[rr] = true
		}
		requiredSets = append(requiredSets, rs)
	}
	for k := range propSet {
		props = append(props, k)
	}
	if len(requiredSets) > 0 {
		for k := range requiredSets[0] {
			inAll := true
			for _, s := range requiredSets[1:] {
				if !s[k] {
					inAll = false
					break
				}
			}
			if inAll {
				required = append(required, k)
			}
		}
	}
	return props, required, true
}

// schemaPropertyType resolves the JSON Schema "type" declared for property
// propName on node, oneOf-aware (searches every branch, since a oneOf-shaped
// node -- like remoteSource -- has no single top-level "properties" map).
// $ref values are resolved one level against root. Returns ok=false if no
// type can be determined (e.g. a property whose value is itself a oneOf,
// like ClaudePlugin's "source" -- callers must already be skip-listing that
// field via expectedSchemaType's ok=false path before reaching here).
func schemaPropertyType(root, node map[string]any, propName string) (string, bool) {
	if branchesRaw, ok := node["oneOf"].([]any); ok {
		for _, br := range branchesRaw {
			brNode, _ := br.(map[string]any)
			resolved := resolveRefOrSelf(root, brNode)
			if typ, ok := propertyTypeFromNode(root, resolved, propName); ok {
				return typ, true
			}
		}
		return "", false
	}
	return propertyTypeFromNode(root, node, propName)
}

func propertyTypeFromNode(root, node map[string]any, propName string) (string, bool) {
	propsRaw, ok := node["properties"].(map[string]any)
	if !ok {
		return "", false
	}
	propNode, ok := propsRaw[propName].(map[string]any)
	if !ok {
		return "", false
	}
	return resolveNodeType(root, propNode)
}

// schemaPropertyItemsType resolves property propName's array "items" schema
// type (Fix 3b's Go-cross-check half: for a Go []string field, the schema's
// declared items.type must be "string"). oneOf-aware the same way
// schemaPropertyType is; $ref on both the property itself and on its
// "items" value are each resolved one level.
func schemaPropertyItemsType(root, node map[string]any, propName string) (string, bool) {
	if branchesRaw, ok := node["oneOf"].([]any); ok {
		for _, br := range branchesRaw {
			brNode, _ := br.(map[string]any)
			resolved := resolveRefOrSelf(root, brNode)
			if typ, ok := propertyItemsTypeFromNode(root, resolved, propName); ok {
				return typ, true
			}
		}
		return "", false
	}
	return propertyItemsTypeFromNode(root, node, propName)
}

func propertyItemsTypeFromNode(root, node map[string]any, propName string) (string, bool) {
	propsRaw, ok := node["properties"].(map[string]any)
	if !ok {
		return "", false
	}
	propNode, ok := propsRaw[propName].(map[string]any)
	if !ok {
		return "", false
	}
	propNode = resolveRefOrSelf(root, propNode)
	itemsRaw, ok := propNode["items"].(map[string]any)
	if !ok {
		return "", false
	}
	return resolveNodeType(root, itemsRaw)
}

// schemaPropertyAdditionalPropertiesType resolves property propName's
// "additionalProperties" schema type (Tier-8 audit Fix 3a's Go-cross-check
// half: for a Go map[string]V field -- e.g. ClaudePlugin.Author, a
// map[string]string -- the schema's declared additionalProperties.type must
// equal V's own expected schema type). oneOf-aware and $ref-resolving the
// same way schemaPropertyItemsType is; ok=false if additionalProperties
// isn't itself a nested schema object (e.g. it's a plain `false`, which is
// the closed/no-map-values-allowed case and never applies to an actual Go
// map field).
func schemaPropertyAdditionalPropertiesType(root, node map[string]any, propName string) (string, bool) {
	if branchesRaw, ok := node["oneOf"].([]any); ok {
		for _, br := range branchesRaw {
			brNode, _ := br.(map[string]any)
			resolved := resolveRefOrSelf(root, brNode)
			if typ, ok := propertyAdditionalPropertiesTypeFromNode(root, resolved, propName); ok {
				return typ, true
			}
		}
		return "", false
	}
	return propertyAdditionalPropertiesTypeFromNode(root, node, propName)
}

func propertyAdditionalPropertiesTypeFromNode(root, node map[string]any, propName string) (string, bool) {
	propsRaw, ok := node["properties"].(map[string]any)
	if !ok {
		return "", false
	}
	propNode, ok := propsRaw[propName].(map[string]any)
	if !ok {
		return "", false
	}
	propNode = resolveRefOrSelf(root, propNode)
	apRaw, ok := propNode["additionalProperties"].(map[string]any)
	if !ok {
		return "", false
	}
	return resolveNodeType(root, apRaw)
}

// resolveNodeType resolves node's own JSON Schema "type" -- following one
// level of "$ref", and recursing into "oneOf" branches when every branch
// agrees on the same type (e.g. CodexPlugin's "source" property is itself
// `{"oneOf": [localSource, remoteSource]}` with no direct "type" key, but
// both branches ultimately resolve to "object", so the property as a whole
// is unambiguously object-typed). Returns ok=false if unresolvable OR if
// oneOf branches disagree (a genuinely mixed-type oneOf, like ClaudePlugin's
// "source" -- string for local, object for remote -- has no single type to
// report; callers must already be skip-listing such fields).
func resolveNodeType(root, node map[string]any) (string, bool) {
	if ref, ok := node["$ref"].(string); ok {
		target := resolveRefNode(root, ref)
		if target == nil {
			return "", false
		}
		return resolveNodeType(root, target)
	}
	if typ, ok := node["type"].(string); ok {
		return typ, true
	}
	if branchesRaw, ok := node["oneOf"].([]any); ok {
		var typ string
		for i, br := range branchesRaw {
			brNode, _ := br.(map[string]any)
			t, ok := resolveNodeType(root, brNode)
			if !ok {
				return "", false
			}
			if i == 0 {
				typ = t
			} else if t != typ {
				return "", false
			}
		}
		if typ != "" {
			return typ, true
		}
	}
	return "", false
}

// assertFieldSetsEqual reports every field present only in one of a/b, as
// a single test failure naming both label and the direction of the drift.
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

// assertSubset reports every element of small not present in big.
func assertSubset(t *testing.T, label string, small, big []string) {
	t.Helper()
	bigSet := map[string]bool{}
	for _, x := range big {
		bigSet[x] = true
	}
	var missing []string
	for _, x := range small {
		if !bigSet[x] {
			missing = append(missing, x)
		}
	}
	if len(missing) == 0 {
		return
	}
	sort.Strings(missing)
	t.Errorf("%s: fields unconditionally set in the Go type but not required by every schema oneOf branch: %v", label, missing)
}

func fieldSet(names ...string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		out[n] = true
	}
	return out
}

// specRow is one parsed markdown table row: field name plus the raw 型別
// (type) and 必填/選填 (required/optional) column text.
type specRow struct {
	name        string
	typeRaw     string
	requiredRaw string
}

// tableRowRe captures a table row's first THREE cells (欄位/型別/必填選填),
// extending tableFieldNameRe's first-cell-only match -- Tier-2 audit fix
// (D3.2's "欄位集合" sync was field-NAME only; this additionally lets
// TestSchemaSync_SpecMatchesSchemaTypesAndRequiredness compare each row's
// declared type/required-ness against the schema).
// The name class must allow "_": every field this file covered before
// v0.27.0 happened to be one word or camelCase (mcpServers), so the original
// [A-Za-z0-9]* silently could not express a snake_case field name. The first
// one -- tag_pattern -- made the spec row invisible to this parser, which
// surfaced as "only in schema: [tag_pattern]" rather than a parse error. That
// is fail-closed (an undocumentable field blocks the sync test instead of
// passing it), but it did make the real cause hard to see.
var tableRowRe = regexp.MustCompile("(?m)^\\|\\s*`([A-Za-z][A-Za-z0-9_]*)`\\s*\\|\\s*([^|]*?)\\s*\\|\\s*([^|]*?)\\s*\\|")

// discoveredTable is one field-table occurrence found anywhere in the spec
// document: its owning heading text, that heading's own byte offset (start
// of the "## "/"### " line), and the field-table header row's own byte
// offset (used only for naming duplicates in error messages).
type discoveredTable struct {
	heading       string
	headingOffset int
	headerOffset  int
}

// discoverFieldTables finds every field-table header (fieldTableHeaderRe)
// anywhere in content and its owning heading (nearest preceding "## "/
// "### " line, by byte offset).
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

// headingStartOffsets returns the byte offset of every "## "/"### " heading
// line in content (used by specTableRowsAt to find where a bound table's
// row-extraction segment ends).
func headingStartOffsets(content string) []int {
	headingMatches := anyHeadingRe.FindAllStringIndex(content, -1)
	out := make([]int, len(headingMatches))
	for i, hd := range headingMatches {
		out[i] = hd[0]
	}
	return out
}

// specTableRowsAt parses every markdown table row in the segment of content
// starting at headingOffset (a heading line's own, already-bound byte
// offset) up to the next heading in headingStarts (or EOF). Unlike an
// earlier version of this helper (specTableRows), this extracts from an
// EXPLICIT, already-bound byte offset rather than re-searching content for
// a subHeading substring -- see bindSpecSchemaCase's doc comment for why a
// substring re-search is unsafe when more than one table can share the same
// heading prefix.
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

// bindSpecSchemaCase finds the EXACTLY-ONE discovered field table whose
// owning heading matches c.subHeading (prefix match) -- Fix 1 (Tier-8
// audit): a duplicate table sharing the same heading PREFIX (e.g. a second,
// bogus "### owner（`ClaudeOwner`..." heading pasted elsewhere in the file)
// previously went undetected, because (a) the "is this table mapped by SOME
// specSchemaCases entry" check doesn't care how MANY tables match the same
// entry, and (b) row-extraction used to do a plain strings.Index substring
// search, which only ever finds the FIRST occurrence -- so a second (bogus)
// table's rows were silently never parsed or compared against anything.
// Fatals (not just Errors -- every other check in this file assumes a
// single well-defined occurrence to read from, so continuing would just
// cascade confusing failures) if the match count isn't exactly 1, naming
// every matching table's heading byte offset.
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

// oneOfLiteralAllowedAt is the ONLY (subHeading, fieldName) pair permitted to
// use the literal "string 或 object" 型別 text. ClaudePlugin's "source" is a
// real oneOf between a plain string (local) and an object (remote), so it
// alone has no single type to check.
const oneOfLiteralAllowedAt = "### plugins[]（`ClaudePlugin`\x00source"

// typeSpec is one entry of allowedTypeLiterals: the JSON Schema type
// category a 型別 column literal maps to, its enum value list (nil if the
// literal doesn't encode one), and whether it's the oneOf-skip literal.
type typeSpec struct {
	category   string
	enumValues []string
	skip       bool
}

// allowedTypeLiterals is a CLOSED, EXACT-STRING vocabulary of every 型別
// column value that currently appears in agent-schema.md (Tier-3 audit fix:
// the previous strings.HasPrefix(raw, "string（")/"object（" check accepted
// ANY parenthetical suffix -- e.g. "string（garbage-not-a-type）" -- as a
// valid string/object column instead of failing closed). Every entry here
// was transcribed byte-for-byte from the file (verified via `grep -oE` over
// every table's 型別 column before writing this map); adding a new type
// column value to the spec REQUIRES adding it here too, in the same change,
// or TestSchemaSync_SpecMatchesSchemaTypesAndRequiredness fails closed.
// enum-bearing entries additionally drive Fix-1b's enum-sync check below
// (schemaPropertyEnum): the value list must exactly equal the schema's own
// "enum" declaration for that field (oneOf branches unioned).
var allowedTypeLiterals = map[string]typeSpec{
	"string":                      {category: "string"},
	"object":                      {category: "object"},
	"array":                       {category: "array"},
	"array[string]":               {category: "array"},
	"object（`map[string]string`）": {category: "object"},
	"string 或 object":             {skip: true}, // permitted only at oneOfLiteralAllowedAt, checked separately
	"string（enum: `github`、`url`、`git-subdir`）": {category: "string", enumValues: []string{"github", "url", "git-subdir"}},
	"string（enum: `url`、`git-subdir`）":          {category: "string", enumValues: []string{"url", "git-subdir"}},
	"string（enum: `local`）":                     {category: "string", enumValues: []string{"local"}},
	"string（enum: `AVAILABLE`）":                 {category: "string", enumValues: []string{"AVAILABLE"}},
	"string（enum: `ON_INSTALL`）":                {category: "string", enumValues: []string{"ON_INSTALL"}},
}

// specTypeCategory looks up raw in the closed allowedTypeLiterals vocabulary
// (exact string match, not a prefix). loc identifies which (subHeading,
// fieldName) cell raw came from, so the "string 或 object" literal can be
// permitted at exactly one documented cell (oneOfLiteralAllowedAt) and
// rejected everywhere else. ok=false for anything outside this vocabulary
// (fail-closed: an unrecognized shape -- a typo, a new pattern nobody taught
// this function -- is a test FAILURE, never a silent skip or a best-effort
// guess).
func specTypeCategory(loc, raw string) (category string, skip bool, enumValues []string, ok bool) {
	spec, found := allowedTypeLiterals[raw]
	if !found {
		return "", false, nil, false
	}
	if spec.skip && loc != oneOfLiteralAllowedAt {
		return "", false, nil, false
	}
	return spec.category, spec.skip, spec.enumValues, true
}

// resolveNodeEnum resolves node's own "enum" declaration, following one
// level of "$ref" and recursing into "oneOf" branches (unioning every
// branch's enum values -- e.g. spec's single claude "source" row describes
// all three remoteSource variants' discriminator values at once, so the
// comparand must be their union, not any one branch's value alone).
// ok=false if no branch declares an enum at all.
func resolveNodeEnum(root, node map[string]any) ([]string, bool) {
	if ref, ok := node["$ref"].(string); ok {
		target := resolveRefNode(root, ref)
		if target == nil {
			return nil, false
		}
		return resolveNodeEnum(root, target)
	}
	if enumRaw, ok := node["enum"].([]any); ok {
		var vals []string
		for _, e := range enumRaw {
			if s, ok := e.(string); ok {
				vals = append(vals, s)
			}
		}
		sort.Strings(vals)
		return vals, true
	}
	if branchesRaw, ok := node["oneOf"].([]any); ok {
		union := map[string]bool{}
		found := false
		for _, br := range branchesRaw {
			brNode, _ := br.(map[string]any)
			vals, ok := resolveNodeEnum(root, brNode)
			if ok {
				found = true
				for _, v := range vals {
					union[v] = true
				}
			}
		}
		if !found {
			return nil, false
		}
		var out []string
		for v := range union {
			out = append(out, v)
		}
		sort.Strings(out)
		return out, true
	}
	return nil, false
}

// schemaPropertyEnum resolves property propName's "enum" declaration on
// node, oneOf-aware at the NODE level too (unlike resolveNodeEnum's
// property-value-level oneOf handling, this also searches every branch of a
// oneOf-shaped node -- like $defs.remoteSource -- for a branch that declares
// propName at all, mirroring schemaPropertyType's identical branch search).
func schemaPropertyEnum(root, node map[string]any, propName string) ([]string, bool) {
	if branchesRaw, ok := node["oneOf"].([]any); ok {
		union := map[string]bool{}
		found := false
		for _, br := range branchesRaw {
			brNode, _ := br.(map[string]any)
			resolved := resolveRefOrSelf(root, brNode)
			propsRaw, ok := resolved["properties"].(map[string]any)
			if !ok {
				continue
			}
			propNode, ok := propsRaw[propName].(map[string]any)
			if !ok {
				continue
			}
			vals, ok := resolveNodeEnum(root, propNode)
			if ok {
				found = true
				for _, v := range vals {
					union[v] = true
				}
			}
		}
		if !found {
			return nil, false
		}
		var out []string
		for v := range union {
			out = append(out, v)
		}
		sort.Strings(out)
		return out, true
	}
	propsRaw, ok := node["properties"].(map[string]any)
	if !ok {
		return nil, false
	}
	propNode, ok := propsRaw[propName].(map[string]any)
	if !ok {
		return nil, false
	}
	return resolveNodeEnum(root, propNode)
}

// specRequiredness maps a spec table's 必填/選填 column text to a bool (a
// leading "必填" -> true, a leading "選填" -> false). ok=false for anything
// else.
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
// text) with the schema file/path it documents -- the same schema location
// a driftCase in TestSchemaDrift_GoTypesMatchSchemaProperties points at, but
// this list exists independently (checking spec prose directly against the
// schema, not through the Go type) per this file's two-layer drift design.
type specSchemaCase struct {
	subHeading string
	schemaFile string
	path       []string
}

// specSchemaCases lists every family's sub-tables. "### 已知上游瑕疵..."
// and "### claude / copilot 差異" are deliberately absent: neither has a
// “ `fieldName` “ first-column table row (see agent-schema.md), so there
// is nothing for specTableRows to find there.
var specSchemaCases = []specSchemaCase{
	{"### 文件層（`ClaudeDocument`", "testdata/apm-claude-marketplace.schema.json", nil},
	{"### owner（`ClaudeOwner`", "testdata/apm-claude-marketplace.schema.json", []string{"$defs", "owner"}},
	{"### plugins[]（`ClaudePlugin`", "testdata/apm-claude-marketplace.schema.json", []string{"$defs", "plugin"}},
	{"### source（`RemoteSource`, `mapper.go:72`", "testdata/apm-claude-marketplace.schema.json", []string{"$defs", "remoteSource"}},
	{"### 文件層（`CodexDocument`", "testdata/apm-codex-marketplace.schema.json", nil},
	{"### interface（`CodexInterface`", "testdata/apm-codex-marketplace.schema.json", []string{"$defs", "interface"}},
	{"### plugins[]（`CodexPlugin`", "testdata/apm-codex-marketplace.schema.json", []string{"$defs", "plugin"}},
	{"### policy（`CodexPolicy`", "testdata/apm-codex-marketplace.schema.json", []string{"$defs", "policy"}},
	{"### local source（`CodexLocalSource`", "testdata/apm-codex-marketplace.schema.json", []string{"$defs", "localSource"}},
	{"### remote source（`RemoteSource`，與 Claude 共用同一個 Go 型別", "testdata/apm-codex-marketplace.schema.json", []string{"$defs", "remoteSource"}},
}

// assertSpecTableMatchesSchema is TestSchemaSync_SpecMatchesSchemaTypesAndRequiredness's
// per-table body. internal/pack/bundle/schema_sync_test.go has its own,
// separate copy for the plugin.json family's two sub-tables (no shared
// internal test-helper package between these two packages -- same pattern
// as compileApmSchema/schemaDoc/etc. already being duplicated per-package
// elsewhere in this file).
func assertSpecTableMatchesSchema(t *testing.T, rows []specRow, c specSchemaCase) {
	t.Helper()
	if len(rows) == 0 {
		t.Fatalf("sub-heading %q: no table rows found", c.subHeading)
	}
	root := schemaDoc(t, c.schemaFile)
	node := schemaNode(t, c.schemaFile, c.path)
	// isOneOf discarded: schemaNodePropsAndRequired's required list is
	// already the correct comparand for BOTH oneOf and flat defs (the
	// per-variant intersection in the oneOf case) -- see the strict-equality
	// comment below.
	_, schemaRequired, _ := schemaNodePropsAndRequired(root, node)
	schemaRequiredSet := map[string]bool{}
	for _, r := range schemaRequired {
		schemaRequiredSet[r] = true
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			loc := c.subHeading + "\x00" + row.name
			category, skip, specEnum, ok := specTypeCategory(loc, row.typeRaw)
			if !ok {
				t.Errorf("field %q: unrecognized 型別 column %q (fail-closed -- add an exact literal to allowedTypeLiterals, do not loosen it to a prefix match)", row.name, row.typeRaw)
			} else if !skip {
				actual, found := schemaPropertyType(root, node, row.name)
				if !found {
					t.Errorf("field %q: schema has no resolvable type to compare against spec's %q", row.name, row.typeRaw)
				} else if actual != category {
					t.Errorf("field %q: spec 型別 %q implies schema type %q, schema declares %q", row.name, row.typeRaw, category, actual)
				}

				// Fix 1b: enum sync -- spec's parenthetical enum value list
				// (if any) must exactly equal the schema's own "enum"
				// declaration for this field (oneOf branches unioned by
				// schemaPropertyEnum). Symmetric: a schema-side enum with no
				// spec-side claim is flagged too, not just the reverse.
				schemaEnum, schemaHasEnum := schemaPropertyEnum(root, node, row.name)
				specHasEnum := len(specEnum) > 0
				switch {
				case specHasEnum && !schemaHasEnum:
					t.Errorf("field %q: spec declares enum %v (%q) but schema has no enum for this field", row.name, specEnum, row.typeRaw)
				case !specHasEnum && schemaHasEnum:
					t.Errorf("field %q: schema declares enum %v but spec's 型別 column (%q) doesn't document it", row.name, schemaEnum, row.typeRaw)
				case specHasEnum && schemaHasEnum:
					wantEnum := append([]string(nil), specEnum...)
					sort.Strings(wantEnum)
					if !reflect.DeepEqual(wantEnum, schemaEnum) {
						t.Errorf("field %q: spec enum %v (%q) does not match schema enum %v", row.name, wantEnum, row.typeRaw, schemaEnum)
					}
				}
			}

			required, ok := specRequiredness(row.requiredRaw)
			if !ok {
				t.Errorf("field %q: unrecognized 必填/選填 column %q -- add a case to specRequiredness", row.name, row.requiredRaw)
				return
			}
			// isOneOf is no longer special-cased (Tier-2 audit fix 2c): for a
			// oneOf-shaped def, schemaRequiredSet is already the per-variant
			// INTERSECTION (schemaNodePropsAndRequired's doc comment) -- "必填"
			// means "in the intersection" (i.e. every branch requires it, like
			// "source" itself) and "選填" means "not in the intersection"
			// (like "repo", only required by the github branch). This is now
			// a strict equality in both directions: spec text must exactly
			// mirror the intersection, full stop. Per-variant "required by
			// SOME but not all branches" reality (e.g. "repo" only in
			// github's required) is separately locked down by
			// TestSchemaReject_RemoteSourceVariants and
			// TestSchemaGolden_RemoteSourceVariantsMinimal, not by this check.
			inSchemaRequired := schemaRequiredSet[row.name]
			if required != inSchemaRequired {
				t.Errorf("field %q: spec says required=%v (%q), schema required list says %v", row.name, required, row.requiredRaw, inSchemaRequired)
			}
		})
	}
}

func TestSchemaSync_SpecMatchesSchemaTypesAndRequiredness(t *testing.T) {
	specPath := filepath.Join(findRepoRoot(t), "spec", "conformance", "agent-schema.md")
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

// ── SchemaGolden: apm-go's own real output validates ────────────────────

func TestSchemaGolden_ClaudeMarketplace_ValidatesAgainstApmSchema(t *testing.T) {
	schema := compileApmSchema(t, "testdata/apm-claude-marketplace.schema.json")
	if err := validateJSONFile(t, schema, "testdata/apm-claude-marketplace.golden.json"); err != nil {
		t.Errorf("golden claude marketplace.json failed validation: %v", err)
	}
}

func TestSchemaGolden_CodexMarketplace_ValidatesAgainstApmSchema(t *testing.T) {
	schema := compileApmSchema(t, "testdata/apm-codex-marketplace.schema.json")
	if err := validateJSONFile(t, schema, "testdata/apm-codex-marketplace.golden.json"); err != nil {
		t.Errorf("golden codex marketplace.json failed validation: %v", err)
	}
}

// TestSchemaGolden_LiveOutput_ClaudeMarketplace and
// TestSchemaGolden_LiveOutput_CodexMarketplace are this file's Tier-4 audit
// fix: TestSchemaGolden_RemoteSourceVariantsMinimal's per-variant documents
// are hand-constructed JSON, and the two ...ValidatesAgainstApmSchema tests
// above read a STATIC committed golden file -- neither actually calls
// ClaudeMapper{}.Compose/CodexMapper{}.Compose at test-run time, so neither
// proves the schema accepts what the real mapper produces today (as opposed
// to what it produced whenever the golden file was last regenerated by
// hand). These two tests close that gap the same way bundle's
// TestSchemaGolden_LiveOutput_PluginClaude/Copilot already do for the
// plugin.json family: build a realistic AuthoringConfig/[]ResolvedPackage
// set covering every source variant (local, github, url-with-non-default-
// host, git-subdir for claude; local, url, git-subdir for codex), call the
// REAL Compose, marshal, and schema.Validate the result.
//
// If a remote ResolvedPackage's SourceRepo is empty, Compose does not itself
// error -- it silently emits {"source":"github","repo":""} (claude) or
// {"source":"url","url":""} (codex), which this schema then correctly
// rejects (omitempty drops an EMPTY repo/url key entirely, landing on
// TestSchemaReject_RemoteSourceVariants' "missing repo/url" cases). This
// schema's stance is unchanged either way -- it describes the LEGAL output
// shape, and rejecting this malformed one is correct regardless of where the
// malformation originated. The upstream guard that used to be missing --
// internal/marketplace/authoring/schema.go's parsePackages only called
// manifest.ValidateMarketplaceSource when `source != ""`, letting an empty
// source: "" flow straight through LoadAuthoringConfig/pack/Compose -- is
// now closed (parsePackages validates source unconditionally, the same as
// every other value; see TestLoadAuthoringConfig_EmptySource_Rejected,
// authoring/schema_test.go, and TestPack_EmptySourcePackage_Rejected,
// cmd/apm-go/pack_test.go).
func TestSchemaGolden_LiveOutput_ClaudeMarketplace(t *testing.T) {
	cfg := &authoring.AuthoringConfig{
		Name:                  "my-marketplace",
		Description:           "A demo marketplace for schema testing.",
		DescriptionOverridden: true,
		Version:               "2.0.0",
		VersionOverridden:     true,
		Owner:                 authoring.Owner{Name: "acme-org", Email: "owner@example.com", URL: "https://github.com/acme-org"},
		Metadata:              map[string]any{"pluginRoot": "./pkgs"},
	}
	resolved := []ResolvedPackage{
		{
			Entry: authoring.PackageEntry{
				Name: "local-tool", Source: "./pkgs/tool-a", Description: "a local tool", Version: "0.1.0",
				Author: map[string]string{"name": "Jane Doe"}, License: "MIT", Repository: "https://github.com/acme/tool-a",
				Homepage: "https://example.com/tool-a",
			},
			IsLocal: true,
			Tags:    []string{"local", "demo"},
		},
		{
			Entry:             authoring.PackageEntry{Name: "impeccable", Source: "pbakaus/impeccable", Category: "Productivity"},
			SourceRepo:        "pbakaus/impeccable",
			Ref:               "fc2e694afca1ac0cc384b4fe56bab3335fea7912",
			SHA:               "fc2e694afca1ac0cc384b4fe56bab3335fea7912",
			RemoteDescription: "The design language that makes your AI harness better at design.",
			RemoteVersion:     "1.0.0",
			Tags:              []string{"remote"},
		},
		{
			Entry:      authoring.PackageEntry{Name: "subdir-tool", Source: "owner/mono", Subdir: "packages/tool-c"},
			SourceRepo: "owner/mono",
			Subdir:     "packages/tool-c",
			Ref:        "v2.0.0",
			SHA:        strings.Repeat("b", 40),
		},
		{
			Entry:      authoring.PackageEntry{Name: "ghe-tool", Source: "ghe.example.com/owner/repo-d"},
			Host:       "ghe.example.com",
			SourceRepo: "owner/repo-d",
			Ref:        "v3.0.0",
			SHA:        strings.Repeat("c", 40),
		},
	}

	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)
	if err != nil {
		t.Fatalf("ClaudeMapper.Compose() error = %v", err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	schema := compileApmSchema(t, "testdata/apm-claude-marketplace.schema.json")
	if err := schema.Validate(v); err != nil {
		t.Errorf("live ClaudeMapper.Compose() output failed validation: %v\noutput: %s", err, raw)
	}
}

func TestSchemaGolden_LiveOutput_CodexMarketplace(t *testing.T) {
	cfg := &authoring.AuthoringConfig{Name: "my-marketplace"}
	resolved := []ResolvedPackage{
		{
			Entry:   authoring.PackageEntry{Name: "local-tool", Source: "./pkgs/tool-a", Category: "Utilities"},
			IsLocal: true,
		},
		{
			Entry:      authoring.PackageEntry{Name: "subdir-tool", Source: "owner/mono", Subdir: "packages/tool-c", Category: "Utilities"},
			SourceRepo: "owner/mono",
			Subdir:     "packages/tool-c",
			Ref:        "v2.0.0",
			SHA:        strings.Repeat("b", 40),
		},
		{
			Entry:             authoring.PackageEntry{Name: "impeccable", Source: "pbakaus/impeccable", Category: "Productivity"},
			SourceRepo:        "pbakaus/impeccable",
			Ref:               "fc2e694afca1ac0cc384b4fe56bab3335fea7912",
			SHA:               "fc2e694afca1ac0cc384b4fe56bab3335fea7912",
			RemoteDescription: "The design language that makes your AI harness better at design.",
			RemoteVersion:     "1.0.0",
		},
	}

	doc, _, err := CodexMapper{}.Compose(cfg, resolved)
	if err != nil {
		t.Fatalf("CodexMapper.Compose() error = %v", err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	schema := compileApmSchema(t, "testdata/apm-codex-marketplace.schema.json")
	if err := schema.Validate(v); err != nil {
		t.Errorf("live CodexMapper.Compose() output failed validation: %v\noutput: %s", err, raw)
	}
}

// TestSchemaGolden_UpstreamClaudeMarketplace_ValidatesAgainstApmSchema validates
// an actual upstream (apm 0.26.0) artifact, not apm-go's own output -- AS4's
// literal requirement ("把 research/ 裡的上游實跑產物餵進去要通過"), which the
// apm-go-generated goldens above cannot exercise since they're produced by the
// same Go types the schema is checked against (a coupled oracle). Verbatim
// copy of research/eval-real-run-20260728.md:243-261 (also
// research/agent-schema-support-matrix.md §2.1); note this upstream document
// includes "category" on the plugin, which apm-go's own ClaudeMapper now
// also emits (mapper.go's ClaudePlugin.Category,
// TestClaudeMapper_Output_CategoryPassedThrough_NoAPMOnlyFieldsInJSON) --
// see apm-claude-marketplace.schema.json's $defs.plugin.properties.category
// (a required Go field like any other now, not schema-only).
func TestSchemaGolden_UpstreamClaudeMarketplace_ValidatesAgainstApmSchema(t *testing.T) {
	schema := compileApmSchema(t, "testdata/apm-claude-marketplace.schema.json")
	if err := validateJSONFile(t, schema, "testdata/upstream-claude-marketplace.golden.json"); err != nil {
		t.Errorf("upstream claude marketplace.json (eval-real-run-20260728.md:243-261) failed validation: %v", err)
	}
}

// TestSchemaGolden_UpstreamCodexMarketplace_ValidatesAgainstApmSchema is the
// codex counterpart of TestSchemaGolden_UpstreamClaudeMarketplace_ValidatesAgainstApmSchema
// above -- verbatim copy of research/eval-real-run-20260728.md:269-287 (also
// research/agent-schema-support-matrix.md §2.2).
func TestSchemaGolden_UpstreamCodexMarketplace_ValidatesAgainstApmSchema(t *testing.T) {
	schema := compileApmSchema(t, "testdata/apm-codex-marketplace.schema.json")
	if err := validateJSONFile(t, schema, "testdata/upstream-codex-marketplace.golden.json"); err != nil {
		t.Errorf("upstream codex marketplace.json (eval-real-run-20260728.md:269-287) failed validation: %v", err)
	}
}

// ── SchemaReject: deliberately broken variants fail validation ──────────

func TestSchemaReject_CodexMarketplaceMissingCategory(t *testing.T) {
	schema := compileApmSchema(t, "testdata/apm-codex-marketplace.schema.json")
	invalid := map[string]any{
		"name":      "my-marketplace",
		"interface": map[string]any{"displayName": "my-marketplace"},
		"plugins": []any{
			map[string]any{
				"name":   "impeccable",
				"source": map[string]any{"source": "url", "url": "pbakaus/impeccable"},
				"policy": map[string]any{"installation": "AVAILABLE", "authentication": "ON_INSTALL"},
				// "category" deliberately omitted -- required.
			},
		},
	}
	if err := schema.Validate(invalid); err == nil {
		t.Fatal("expected validation error for codex plugin missing required 'category'")
	}
}

func TestSchemaReject_ClaudeMarketplaceMissingOwner(t *testing.T) {
	schema := compileApmSchema(t, "testdata/apm-claude-marketplace.schema.json")
	invalid := map[string]any{
		"name":    "my-marketplace",
		"plugins": []any{},
		// "owner" deliberately omitted -- required.
	}
	if err := schema.Validate(invalid); err == nil {
		t.Fatal("expected validation error for claude document missing required 'owner'")
	}
}

// claudeDocWithSource wraps source (a raw remoteSource-shaped map) in an
// otherwise-minimal-but-valid claude marketplace document, for
// TestSchemaReject_RemoteSourceVariants below.
func claudeDocWithSource(source any) map[string]any {
	return map[string]any{
		"name":  "m",
		"owner": map[string]any{"name": "acme"},
		"plugins": []any{
			map[string]any{"name": "p", "source": source},
		},
	}
}

// codexDocWithSource is claudeDocWithSource's codex counterpart -- codex's
// plugin object additionally needs policy/category to be otherwise valid.
func codexDocWithSource(source any) map[string]any {
	return map[string]any{
		"name":      "m",
		"interface": map[string]any{"displayName": "m"},
		"plugins": []any{
			map[string]any{
				"name":     "p",
				"source":   source,
				"policy":   map[string]any{"installation": "AVAILABLE", "authentication": "ON_INSTALL"},
				"category": "Utilities",
			},
		},
	}
}

// TestSchemaReject_RemoteSourceVariants is this file's Tier-2 audit fix for
// the oneOf-per-variant required lists (apm-claude-marketplace.schema.json/
// apm-codex-marketplace.schema.json's $defs.remoteSource*): the drift test's
// coarse union/intersection extractor cannot detect a single branch quietly
// losing one of its required fields (e.g. removing "repo" from claude's
// github branch changes neither the properties union nor the required
// intersection, since "repo" was never in the intersection to begin with).
// A data-driven per-variant negative test closes that gap: each case
// constructs an otherwise-valid document whose plugin source is missing
// exactly one field composeRemoteSource (mapper.go:214-244) /
// composeCodexSource (codexmapper.go:129-155) always sets for that variant,
// and asserts validation fails. If a future change weakens a branch's
// "required" list, the corresponding case's document becomes (wrongly)
// valid and this test goes red.
func TestSchemaReject_RemoteSourceVariants(t *testing.T) {
	cases := []struct {
		name       string
		schemaFile string
		buildDoc   func(source any) map[string]any
		source     map[string]any
	}{
		// Claude: composeRemoteSource's three variants (mapper.go:214-244).
		{"claude github missing repo", "testdata/apm-claude-marketplace.schema.json", claudeDocWithSource,
			map[string]any{"source": "github"}},
		{"claude url missing url", "testdata/apm-claude-marketplace.schema.json", claudeDocWithSource,
			map[string]any{"source": "url"}},
		{"claude git-subdir missing url", "testdata/apm-claude-marketplace.schema.json", claudeDocWithSource,
			map[string]any{"source": "git-subdir", "path": "packages/tool-c"}},
		{"claude git-subdir missing path", "testdata/apm-claude-marketplace.schema.json", claudeDocWithSource,
			map[string]any{"source": "git-subdir", "url": "owner/mono"}},
		// Codex: composeCodexSource's two remote variants (codexmapper.go:129-155).
		{"codex url missing url", "testdata/apm-codex-marketplace.schema.json", codexDocWithSource,
			map[string]any{"source": "url"}},
		{"codex git-subdir missing url", "testdata/apm-codex-marketplace.schema.json", codexDocWithSource,
			map[string]any{"source": "git-subdir", "path": "packages/tool-c"}},
		{"codex git-subdir missing path", "testdata/apm-codex-marketplace.schema.json", codexDocWithSource,
			map[string]any{"source": "git-subdir", "url": "owner/mono"}},
		// Fix 6 (goOnlyAllowed): codex's RemoteSource.Repo is a real Go field
		// (shared with Claude) but composeCodexSource never sets it in
		// EITHER variant -- a document that includes it must be rejected
		// (additionalProperties:false on both codex remoteSource branches).
		// Both branches are covered (a Tier-2 audit fix: an earlier version
		// of this table only covered the url branch, leaving the git-subdir
		// branch's additionalProperties free to be loosened to `true`
		// unnoticed).
		{"codex url variant with forbidden repo", "testdata/apm-codex-marketplace.schema.json", codexDocWithSource,
			map[string]any{"source": "url", "url": "owner/repo", "repo": "owner/repo"}},
		{"codex git-subdir variant with forbidden repo", "testdata/apm-codex-marketplace.schema.json", codexDocWithSource,
			map[string]any{"source": "git-subdir", "url": "owner/mono", "path": "packages/tool-c", "repo": "owner/mono"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			schema := compileApmSchema(t, c.schemaFile)
			doc := c.buildDoc(c.source)
			if err := schema.Validate(doc); err == nil {
				t.Fatalf("expected validation error for %s (source=%v), got none", c.name, c.source)
			}
		})
	}
}

// TestSchemaGolden_RemoteSourceVariantsMinimal is TestSchemaReject_
// RemoteSourceVariants' positive-side counterpart, and closes a different
// oneOf-per-variant escape route (Tier-3 audit): a MISSING negative test
// only proves a branch's required list isn't too WEAK; it says nothing about
// a branch's required list becoming too STRONG (e.g. adding "ref" to
// claude's github branch's required list -- both the union/intersection
// extractor and every existing golden still pass, since every golden
// happens to include ref/sha, but composeRemoteSource only sets Ref when
// pkg.Ref != "", so a legitimate Go output WITHOUT ref would now be wrongly
// rejected). Each case here is the MINIMAL document for one variant --
// containing only the fields composeRemoteSource (mapper.go:214-244) /
// composeCodexSource (codexmapper.go:129-155) ALWAYS set for that variant,
// nothing optional -- and must still validate. Over-tightening any variant's
// required list turns the corresponding case red.
// TestSchemaGolden_RemoteSourceVariantsMinimal shares its per-branch minimal
// document set with TestSchemaReject_RemoteSourceVariantsWrongType's
// remoteSourceBranches table below (a single source of truth for "what does
// composeRemoteSource/composeCodexSource always set for this variant" --
// duplicating it here and there would let the two tables silently drift
// apart). Claude's local variant (a bare string, composeClaudePlugin's
// IsLocal branch, mapper.go:176-192, not part of the object-shaped oneOf
// branches remoteSourceBranches models) is the one case handled separately.
func TestSchemaGolden_RemoteSourceVariantsMinimal(t *testing.T) {
	t.Run("claude local minimal", func(t *testing.T) {
		schema := compileApmSchema(t, "testdata/apm-claude-marketplace.schema.json")
		doc := claudeDocWithSource("./pkgs/tool-a")
		if err := schema.Validate(doc); err != nil {
			t.Fatalf("expected minimal claude local document to validate: %v\ndoc: %#v", err, doc)
		}
	})
	for _, b := range remoteSourceBranches {
		t.Run(b.label+" minimal", func(t *testing.T) {
			schema := compileApmSchema(t, b.schemaFile)
			doc := b.buildDoc(b.minimal)
			if err := schema.Validate(doc); err != nil {
				t.Fatalf("expected minimal %s document to validate (over-tightened required list?): %v\ndoc: %#v", b.label, err, doc)
			}
		})
	}
}

// remoteSourceBranch names one oneOf branch of a marketplace schema's
// "source" field, for TestSchemaReject_RemoteSourceVariantsWrongType's
// programmatic generation below: minimal is that branch's minimal valid
// document (TestSchemaGolden_RemoteSourceVariantsMinimal's same per-variant
// minimal set), defPath points directly at the branch's own (non-oneOf) def.
type remoteSourceBranch struct {
	label      string
	schemaFile string
	defPath    []string
	minimal    map[string]any
	buildDoc   func(source any) map[string]any
}

var remoteSourceBranches = []remoteSourceBranch{
	{"claude github", "testdata/apm-claude-marketplace.schema.json", []string{"$defs", "remoteSourceGithub"},
		map[string]any{"source": "github", "repo": "owner/repo"}, claudeDocWithSource},
	{"claude url", "testdata/apm-claude-marketplace.schema.json", []string{"$defs", "remoteSourceUrl"},
		map[string]any{"source": "url", "url": "https://ghe.example.com/owner/repo"}, claudeDocWithSource},
	{"claude git-subdir", "testdata/apm-claude-marketplace.schema.json", []string{"$defs", "remoteSourceGitSubdir"},
		map[string]any{"source": "git-subdir", "url": "owner/mono", "path": "packages/tool-c"}, claudeDocWithSource},
	{"codex url", "testdata/apm-codex-marketplace.schema.json", []string{"$defs", "remoteSourceUrl"},
		map[string]any{"source": "url", "url": "owner/repo"}, codexDocWithSource},
	{"codex git-subdir", "testdata/apm-codex-marketplace.schema.json", []string{"$defs", "remoteSourceGitSubdir"},
		map[string]any{"source": "git-subdir", "url": "owner/mono", "path": "packages/tool-c"}, codexDocWithSource},
	{"codex local", "testdata/apm-codex-marketplace.schema.json", []string{"$defs", "localSource"},
		map[string]any{"source": "local", "path": "./pkgs/tool-a"}, codexDocWithSource},
}

// TestSchemaReject_RemoteSourceVariantsWrongType is this file's third oneOf
// escape-route fix (Tier-3 audit, "反例 b" -> then Tier-4 audit, "只測代表
// 欄位"): schemaPropertyType (the drift test's Go-vs-schema type checker)
// searches oneOf branches IN ORDER and returns the FIRST branch that
// resolves a property's type -- so if a LATER branch's declared type for
// that same property name is weakened (e.g. git-subdir's "ref" property
// loosened from {"type":"string"} to {} -- accepting any JSON type), the
// drift test stays green because it already found "string" from an earlier
// branch and never looks further. A hand-picked "representative property"
// table only covers whichever fields someone thought to include (an earlier
// version of this test never exercised "ref"/"sha" at all, so a weakened
// git-subdir "ref" schema would have gone unnoticed). This version instead
// reads each branch's schema AT TEST RUN TIME (schemaPropsAndRequired) and
// generates one subtest per (branch, non-discriminator property): the
// branch's own minimal valid document, with that ONE property overwritten
// to a JSON number (every non-discriminator property in these six branches
// is schema-typed as string, so a number is always the wrong type) --
// asserts validation fails. Adding a new property to any branch is
// therefore automatically covered without touching this test. A separate
// discriminator-bogus case ({"source":"bogus",...}) additionally confirms
// an unrecognized discriminator value is rejected outright.
func TestSchemaReject_RemoteSourceVariantsWrongType(t *testing.T) {
	for _, b := range remoteSourceBranches {
		node := schemaNode(t, b.schemaFile, b.defPath)
		props, _ := schemaPropsAndRequired(node)
		for _, prop := range props {
			if prop == "source" {
				continue // discriminator, covered by the bogus-value case below
			}
			t.Run(b.label+"/"+prop, func(t *testing.T) {
				doc := map[string]any{}
				for k, v := range b.minimal {
					doc[k] = v
				}
				doc[prop] = 999999 // wrong type: every non-discriminator property in these branches is schema-typed "string"
				schema := compileApmSchema(t, b.schemaFile)
				full := b.buildDoc(doc)
				if err := schema.Validate(full); err == nil {
					t.Fatalf("expected validation error for branch %q property %q given wrong type (number), got none\ndoc: %#v", b.label, prop, full)
				}
			})
		}
	}

	t.Run("claude bogus discriminator", func(t *testing.T) {
		schema := compileApmSchema(t, "testdata/apm-claude-marketplace.schema.json")
		doc := claudeDocWithSource(map[string]any{"source": "bogus", "repo": "owner/repo", "url": "owner/repo", "path": "packages/tool-c"})
		if err := schema.Validate(doc); err == nil {
			t.Fatalf("expected validation error for an unrecognized claude source discriminator, got none\ndoc: %#v", doc)
		}
	})
	t.Run("codex bogus discriminator", func(t *testing.T) {
		schema := compileApmSchema(t, "testdata/apm-codex-marketplace.schema.json")
		doc := codexDocWithSource(map[string]any{"source": "bogus", "url": "owner/repo", "path": "packages/tool-c"})
		if err := schema.Validate(doc); err == nil {
			t.Fatalf("expected validation error for an unrecognized codex source discriminator, got none\ndoc: %#v", doc)
		}
	})
}

// ── SchemaDrift: schema tree structural invariants ───────────────────────

// schemaTreeHandledKeys is schemaTreeInvariants' fail-closed keyword
// allowlist (Fix 3c, mirroring internal/pack/bundle/schema_sync_test.go's
// identical-in-spirit schemaShapeHandledKeys): every key here is either
// structurally walked or explicitly ignored as free prose. Any OTHER
// top-level key on a schema object node -- a typo, or a real JSON Schema
// keyword this walker was never taught (e.g. "const", "pattern") -- is a
// hard failure, not a silent pass-through.
var schemaTreeHandledKeys = map[string]bool{
	"type": true, "properties": true, "required": true, "additionalProperties": true,
	"enum": true, "items": true, "oneOf": true, "$ref": true,
	"description": true, "title": true, "$schema": true, "$id": true, "$defs": true,
}

// apException describes the EXACT expected shape of an
// additionalProperties exception (Tier-8 audit Fix 3b): an earlier version
// of this whitelist only recorded "this path is allowed to be non-false",
// which meant "author"'s additionalProperties could be silently loosened
// from {"type":"string"} to {} (accept literally anything, including a
// number where the map should hold strings) without this check noticing --
// {} is still just "not false". present records whether "additionalProperties"
// must appear on the node AT ALL (false for "metadata", which has no
// additionalProperties key whatsoever); value, when present is true, is the
// EXACT expected JSON value, compared via reflect.DeepEqual against the
// actual parsed value -- not merely "is it present and non-false".
type apException struct {
	present bool
	value   any
}

// additionalPropertiesExceptions is Fix 3a's explicit, justified whitelist
// of "type":"object" nodes that are NOT additionalProperties:false, keyed by
// schema file then by a stable path string built by schemaTreeInvariants'
// walk ("root.properties.<name>", "$defs.<name>", "$defs.<name>.properties.
// <name>", ...). Confirmed exhaustive by reading both schema files in full
// before writing this map: codex has ZERO exceptions (every object node is
// already additionalProperties:false); claude has exactly two, both
// documented in spec/conformance/agent-schema.md next to their
// field rows:
//
//   - root.properties.metadata: a free-form passthrough object
//     (config.Metadata, mapper.go:102-104) -- apm-go never interprets its
//     contents, so it has no fixed shape to close; its schema entry is
//     bare {"type":"object"}, with NO additionalProperties key at all.
//   - $defs.plugin.properties.author: ClaudePlugin.Author is
//     map[string]string (mapper.go:56), so its schema is EXACTLY
//     additionalProperties:{"type":"string"} (a constrained-but-open map,
//     locked to that literal shape by Fix 3a's Go-cross-check too) -- not a
//     plain object with named properties, so there is no fixed key set to
//     enumerate, but the value type IS fixed and IS checked here.
var additionalPropertiesExceptions = map[string]map[string]apException{
	"testdata/apm-claude-marketplace.schema.json": {
		"root.properties.metadata":       {present: false},
		"$defs.plugin.properties.author": {present: true, value: map[string]any{"type": "string"}},
	},
	"testdata/apm-codex-marketplace.schema.json": {},
}

// isPureRefNode reports whether node is a bare `{"$ref": "..."}` wrapper
// (nothing else) -- schemaTreeInvariants skips recursing into these, since
// the $defs entry they point at is already walked separately (once) by
// schemaTreeInvariants' own top-level $defs iteration; recursing through
// every reference site too would just re-walk the same def redundantly.
func isPureRefNode(node map[string]any) bool {
	if node == nil {
		return false
	}
	_, hasRef := node["$ref"]
	return hasRef && len(node) == 1
}

// checkAdditionalPropertiesInvariant is schemaTreeInvariants' Fix 3a/3b
// check: additionalProperties:false is always fine (closed, no exception
// needed); anything else must match an additionalPropertiesExceptions entry
// EXACTLY -- both its presence and, when present, its literal value.
func checkAdditionalPropertiesInvariant(t *testing.T, node map[string]any, path string, exceptions map[string]apException) {
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
		t.Errorf("%s: type:object node is not closed (%s), and is not in additionalPropertiesExceptions -- either set additionalProperties:false, or add+justify a whitelist entry", path, reason)
		return
	}
	if exc.present != hasAP {
		t.Errorf("%s: additionalProperties presence mismatch against the whitelisted exception -- exception expects present=%v, actual present=%v (%#v)", path, exc.present, hasAP, apRaw)
		return
	}
	if exc.present && !reflect.DeepEqual(exc.value, apRaw) {
		t.Errorf("%s: additionalProperties = %#v does not exactly match the whitelisted exception's expected shape %#v", path, apRaw, exc.value)
	}

	// BLOCKING-2 fix (codex round 7): an additionalProperties-exception node
	// represents a free-form/map-typed Go value with no fixed key set
	// (config.Metadata, a map[string]any; ClaudePlugin.Author, a
	// map[string]string) -- everything above this point only checked the
	// "additionalProperties" key itself, leaving "required"/"properties"
	// completely unchecked on these two nodes. Concrete repro this closes:
	// adding "required":["name"] to $defs.plugin.properties.author (every
	// existing golden/live fixture happens to set author.name, so nothing
	// else in this file would notice) would make the schema wrongly reject a
	// legitimate {"email":"x@example.com"}-only author dict, which
	// authoring/schema.go's own author-parsing (mapping form) allows;
	// likewise "required":["pluginRoot"] on root.properties.metadata would
	// reject a legitimate config.Metadata that never sets pluginRoot. A
	// free-form/map-typed node must therefore never declare "properties" or
	// "required" at all -- those imply a fixed, closed-ish key set that
	// contradicts the map-typed Go value backing it.
	if _, hasProps := node["properties"]; hasProps {
		t.Errorf("%s: free-form/map-typed node (additionalProperties exception) must not declare \"properties\" -- it backs a Go map with no fixed key set", path)
	}
	if _, hasReq := node["required"]; hasReq {
		t.Errorf("%s: free-form/map-typed node (additionalProperties exception) must not declare \"required\" -- it backs a Go map with no fixed key set", path)
	}
}

// checkArrayItemsInvariant is schemaTreeInvariants' Fix 3b (tree-wide) check.
func checkArrayItemsInvariant(t *testing.T, root, node map[string]any, path string) {
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

// walkSchemaTreeNode is schemaTreeInvariants' recursive body: it checks node
// itself (fail-closed keywords, Fix 4's single-string "type", Fix 3a/3b),
// then recurses into every properties/items/oneOf-branch/additionalProperties
// sub-schema node (skipping pure $ref wrappers, per isPureRefNode).
func walkSchemaTreeNode(t *testing.T, root, node map[string]any, path string, exceptions map[string]apException) {
	t.Helper()
	if node == nil {
		return
	}
	for k := range node {
		if !schemaTreeHandledKeys[k] {
			t.Errorf("%s: unrecognized schema keyword %q -- add explicit handling before this structural check can be trusted", path, k)
		}
	}

	// Fix 4: "type" must be a single string, never a JSON array (a union-
	// type escape hatch this schema never needs -- multi-shape fields are
	// always modeled via "oneOf", which is separately projected and
	// negative-tested elsewhere in this file).
	if typRaw, exists := node["type"]; exists {
		if _, ok := typRaw.(string); !ok {
			t.Errorf("%s: \"type\" is %#v, not a single string -- use \"oneOf\" for multi-shape fields instead of a JSON Schema type array", path, typRaw)
		}
	}

	if typ, _ := node["type"].(string); typ == "object" {
		checkAdditionalPropertiesInvariant(t, node, path, exceptions)
	}
	if typ, _ := node["type"].(string); typ == "array" {
		checkArrayItemsInvariant(t, root, node, path)
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
			walkSchemaTreeNode(t, root, v, path+".properties."+name, exceptions)
		}
	}
	if itemsRaw, ok := node["items"].(map[string]any); ok && !isPureRefNode(itemsRaw) {
		walkSchemaTreeNode(t, root, itemsRaw, path+".items", exceptions)
	}
	if oneOfRaw, ok := node["oneOf"].([]any); ok {
		for i, br := range oneOfRaw {
			brNode, _ := br.(map[string]any)
			if isPureRefNode(brNode) {
				continue
			}
			walkSchemaTreeNode(t, root, brNode, fmt.Sprintf("%s.oneOf[%d]", path, i), exceptions)
		}
	}
	if apRaw, ok := node["additionalProperties"].(map[string]any); ok {
		walkSchemaTreeNode(t, root, apRaw, path+".additionalProperties", exceptions)
	}
}

// schemaTreeInvariants walks EVERY object/array node reachable from
// schemaFile's document root (the root node itself, plus every entry under
// "$defs" -- oneOf branches and property $refs all resolve to a $defs
// entry in these two schemas, so iterating $defs directly, once each,
// covers the whole reachable tree without needing to re-walk through every
// reference site) and enforces Fix 3a/3b/3c/Fix 4's structural invariants.
// A Tier-6 audit found all four gaps unchecked by every other test in this
// file: additionalProperties silently loosened, an array property's items
// schema silently loosened, an unrecognized schema keyword silently
// ignored, and a "type" array (union type) silently accepted where only a
// single string was ever intended.
func schemaTreeInvariants(t *testing.T, schemaFile string) {
	t.Helper()
	root := schemaDoc(t, schemaFile)
	exceptions := additionalPropertiesExceptions[schemaFile]
	walkSchemaTreeNode(t, root, root, "root", exceptions)
	if defs, ok := root["$defs"].(map[string]any); ok {
		names := make([]string, 0, len(defs))
		for name := range defs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			defNode, _ := defs[name].(map[string]any)
			walkSchemaTreeNode(t, root, defNode, "$defs."+name, exceptions)
		}
	}
}

func TestSchemaDrift_SchemaStructuralInvariants(t *testing.T) {
	for _, schemaFile := range []string{
		"testdata/apm-claude-marketplace.schema.json",
		"testdata/apm-codex-marketplace.schema.json",
	} {
		t.Run(schemaFile, func(t *testing.T) {
			schemaTreeInvariants(t, schemaFile)
		})
	}
}

// remoteSourceBranchExactProperties is Tier-8 audit Fix 2: the tree
// invariant walker's additionalProperties:false check and the drift test's
// union-across-branches property comparison (schemaNodePropsAndRequired)
// both operate at the SET level, and a property that legitimately belongs
// to ANOTHER branch (e.g. "path", which the git-subdir branches legitimately
// declare) can be copy-pasted onto a branch that should never accept it
// (e.g. claude's github branch) without changing the union, without adding
// any unrecognized keyword, and without weakening any existing type/
// additionalProperties check -- a genuinely new escape route none of the
// other checks in this file close. This table hardcodes each branch's EXACT
// expected property set, read directly off the Go compose functions that
// actually produce these documents (not off the schema, which is exactly
// the thing being checked): a branch's schema properties keys must equal
// this set exactly, neither more nor less.
//
//   - claude github (composeRemoteSource default case, mapper.go:233-235):
//     unconditionally sets Source+Repo, NEVER URL or Path -- {source, repo,
//     ref, sha} (ref/sha conditionally appended, mapper.go:237-242, but
//     structurally always allowed).
//   - claude url (composeRemoteSource's remoteURL!="" case, mapper.go:
//     230-232): unconditionally sets Source+URL, NEVER Repo or Path --
//     {source, url, ref, sha}.
//   - claude git-subdir (composeRemoteSource's Subdir!="" case, mapper.go:
//     222-229): unconditionally sets Source+URL+Path, NEVER Repo -- {source,
//     url, path, ref, sha}.
//   - codex url (composeCodexSource's Subdir=="" case, codexmapper.go:
//     144-146): unconditionally sets Source+URL, NEVER Path (codex has no
//     github/Repo variant at all) -- {source, url, ref, sha}.
//   - codex git-subdir (composeCodexSource's Subdir!="" case, codexmapper.go:
//     140-143): unconditionally sets Source+URL+Path -- {source, url, path,
//     ref, sha}.
//
// (ref/sha conditionally appended for both mappers, codexmapper.go:148-153.)
var remoteSourceBranchExactProperties = []struct {
	schemaFile string
	defPath    []string
	want       []string
}{
	{"testdata/apm-claude-marketplace.schema.json", []string{"$defs", "remoteSourceGithub"}, []string{"source", "repo", "ref", "sha", "tag_pattern"}},
	{"testdata/apm-claude-marketplace.schema.json", []string{"$defs", "remoteSourceUrl"}, []string{"source", "url", "ref", "sha", "tag_pattern"}},
	{"testdata/apm-claude-marketplace.schema.json", []string{"$defs", "remoteSourceGitSubdir"}, []string{"source", "url", "path", "ref", "sha", "tag_pattern"}},
	{"testdata/apm-codex-marketplace.schema.json", []string{"$defs", "remoteSourceUrl"}, []string{"source", "url", "ref", "sha", "tag_pattern"}},
	{"testdata/apm-codex-marketplace.schema.json", []string{"$defs", "remoteSourceGitSubdir"}, []string{"source", "url", "path", "ref", "sha", "tag_pattern"}},
}

func TestSchemaDrift_RemoteSourceBranchExactProperties(t *testing.T) {
	for _, tc := range remoteSourceBranchExactProperties {
		t.Run(tc.schemaFile+"/"+strings.Join(tc.defPath, "."), func(t *testing.T) {
			node := schemaNode(t, tc.schemaFile, tc.defPath)
			props, _ := schemaPropsAndRequired(node)
			assertFieldSetsEqual(t, strings.Join(tc.defPath, "."), props, tc.want)
		})
	}
}

// TestSchemaDrift_RemoteSourceBranchExactDiscriminatorEnum closes this
// file's BLOCKING-1 (codex round 7): schemaPropertyEnum (used by both the
// Go<->schema drift check and the spec<->schema sync check) resolves a
// oneOf-shaped property's enum as the UNION across every branch -- correct
// for the spec's single "source" row, which legitimately documents all
// variants' discriminator values at once, but blind to a single branch's OWN
// enum being individually widened (e.g. remoteSourceGithub.source.enum
// growing from ["github"] to ["github","url"] doesn't change the union
// {github,url,git-subdir} at all, so neither TestSchemaDrift_
// GoTypesMatchSchemaProperties nor TestSchemaSync_
// SpecMatchesSchemaTypesAndRequiredness notices -- yet it silently lets a
// malformed {"source":"url","repo":"owner/repo"} document, which no real
// composer ever produces and which lacks the "url" field the url variant
// actually requires, validate successfully under the github branch instead
// of being correctly rejected). This table locks each branch's discriminator
// enum to its EXACT single expected value, read directly off
// remoteSourceBranches' existing per-branch minimal-document fixtures (the
// same single source of truth TestSchemaGolden_RemoteSourceVariantsMinimal/
// TestSchemaReject_RemoteSourceVariantsWrongType already share) -- so a
// branch's enum can never legitimately claim another branch's discriminator
// value.
func TestSchemaDrift_RemoteSourceBranchExactDiscriminatorEnum(t *testing.T) {
	for _, b := range remoteSourceBranches {
		t.Run(b.label, func(t *testing.T) {
			node := schemaNode(t, b.schemaFile, b.defPath)
			propsRaw, ok := node["properties"].(map[string]any)
			if !ok {
				t.Fatalf("%s: def node has no \"properties\"", b.label)
			}
			sourceNode, ok := propsRaw["source"].(map[string]any)
			if !ok {
				t.Fatalf("%s: def node has no \"source\" property", b.label)
			}
			enumRaw, ok := sourceNode["enum"].([]any)
			if !ok {
				t.Fatalf("%s: \"source\" property has no \"enum\"", b.label)
			}
			var got []string
			for _, e := range enumRaw {
				if s, ok := e.(string); ok {
					got = append(got, s)
				}
			}
			want, ok := b.minimal["source"].(string)
			if !ok {
				t.Fatalf("%s: remoteSourceBranches entry's minimal[\"source\"] is not a string", b.label)
			}
			if len(got) != 1 || got[0] != want {
				t.Errorf("%s: source.enum = %v, want exactly [%q] -- a branch's own discriminator enum must never accept another branch's value (doing so lets a malformed document that doesn't fully satisfy ANY variant's required fields validate under the wrong one)", b.label, got, want)
			}
		})
	}
}

// ── SchemaDrift: Go struct json tags <-> schema properties/required/type ─

// wantSchemaOnlyAllowed is the EXACT, case-by-case map of which driftCase is
// allowed to have schema-only fields (declared in the schema, absent from
// the Go type's json tags) and exactly which fields. A Tier-2 audit finding
// against an earlier version of this test (which only compared the UNION of
// every case's schemaOnlyAllowed against a flat set) showed that a union
// comparison is blind to the field moving to the WRONG case -- e.g. adding
// an unrelated optional field to ClaudeOwner's schema and copying an
// existing whitelist entry onto the ClaudeOwner driftCase would leave the
// union unchanged and still green. Keying by case name and asserting the
// whole map equals this literal closes that hole.
//
// Empty today: ClaudePlugin's prior sole entry ("category") was retired once
// ClaudePlugin.Category (mapper.go) started actually emitting it -- it is no
// longer schema-only, it round-trips through the Go type like every other
// field (see mapper.go's ClaudePlugin.Category doc comment for why the
// pre-existing entry here was a real gap, not a considered exception). A
// genuinely new schema-only field (should one ever be needed again) requires
// updating this map, the relevant driftCase's schemaOnlyAllowed, the schema
// file's property (with a rationale comment), and spec/conformance/
// agent-schema.md's matching field row together.
var wantSchemaOnlyAllowed = map[string][]string{}

// wantGoOnlyAllowed is wantSchemaOnlyAllowed's mirror image: the EXACT,
// case-by-case map of which driftCase is allowed to have GO-only fields
// (present in the Go type's json tags, deliberately absent from every
// schema branch). The only current case: RemoteSource's "Repo" field is
// real (Claude's github variant uses it), but codex's own composeCodexSource
// (codexmapper.go:129-155) never sets it in either of its two variants
// (url/git-subdir) -- unlike Claude, codex has no github-shaped variant at
// all -- so neither codex remoteSource branch declares "repo", and a codex
// document that includes it is rejected (additionalProperties:false; see
// TestSchemaReject_RemoteSourceVariants' "codex url variant with forbidden
// repo" case).
var wantGoOnlyAllowed = map[string][]string{
	"RemoteSource(codex)": {"repo"},
}

// assertExactWhitelistMapping asserts actual (built from the live driftCase
// slice) is exactly want: same set of case names present, and for each,
// exactly the same field list (order-independent).
func assertExactWhitelistMapping(t *testing.T, label string, actual, want map[string][]string) {
	t.Helper()
	assertFieldSetsEqual(t, label+": case names", sliceMapKeys(actual), sliceMapKeys(want))
	for name, wantFields := range want {
		gotFields, ok := actual[name]
		if !ok {
			continue // already reported by the case-names check above
		}
		assertFieldSetsEqual(t, label+" ["+name+"]", gotFields, wantFields)
	}
}

func sliceMapKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestSchemaDrift_GoTypesMatchSchemaProperties(t *testing.T) {
	type driftCase struct {
		name       string
		goType     reflect.Type
		schemaFile string
		path       []string
		// schemaOnlyAllowed lists property names intentionally declared in
		// the schema but NOT present in the Go type's json tags. No case
		// currently sets this -- see wantSchemaOnlyAllowed, asserted below
		// via exact case-by-case mapping (not a flat union, per this file's
		// Tier-2 audit fix).
		schemaOnlyAllowed []string
		// goOnlyAllowed is schemaOnlyAllowed's mirror: property names
		// intentionally present in the Go type's json tags but NOT declared
		// in this schema branch. Only RemoteSource(codex) (for "repo") may
		// ever set this -- see wantGoOnlyAllowed, same exact-mapping
		// enforcement.
		goOnlyAllowed []string
		// typeCheckSkip documents Go fields whose Kind is reflect.Interface
		// (Go's `any`) and therefore have no single JSON type to compare --
		// each is a oneOf-discriminated union in the schema. Every such field
		// MUST appear here with a reason, or the type-check loop below fails
		// it as an undocumented skip.
		typeCheckSkip map[string]string
	}
	cases := []driftCase{
		{name: "ClaudeDocument", goType: reflect.TypeOf(ClaudeDocument{}), schemaFile: "testdata/apm-claude-marketplace.schema.json"},
		{name: "ClaudeOwner", goType: reflect.TypeOf(ClaudeOwner{}), schemaFile: "testdata/apm-claude-marketplace.schema.json", path: []string{"$defs", "owner"}},
		{
			name: "ClaudePlugin", goType: reflect.TypeOf(ClaudePlugin{}), schemaFile: "testdata/apm-claude-marketplace.schema.json", path: []string{"$defs", "plugin"},
			typeCheckSkip: map[string]string{"source": "oneOf: plain string (local package path) or an object (RemoteSource, itself further oneOf-variant) -- see composeClaudePlugin's IsLocal branch (mapper.go:176-192) / composeRemoteSource (mapper.go:214-244)"},
		},
		{name: "RemoteSource(claude)", goType: reflect.TypeOf(RemoteSource{}), schemaFile: "testdata/apm-claude-marketplace.schema.json", path: []string{"$defs", "remoteSource"}},
		{name: "CodexDocument", goType: reflect.TypeOf(CodexDocument{}), schemaFile: "testdata/apm-codex-marketplace.schema.json"},
		{name: "CodexInterface", goType: reflect.TypeOf(CodexInterface{}), schemaFile: "testdata/apm-codex-marketplace.schema.json", path: []string{"$defs", "interface"}},
		{
			name: "CodexPlugin", goType: reflect.TypeOf(CodexPlugin{}), schemaFile: "testdata/apm-codex-marketplace.schema.json", path: []string{"$defs", "plugin"},
			typeCheckSkip: map[string]string{"source": "oneOf: CodexLocalSource (object) or RemoteSource (object, itself further oneOf-variant) -- see composeCodexSource, codexmapper.go:129-155"},
		},
		{name: "CodexPolicy", goType: reflect.TypeOf(CodexPolicy{}), schemaFile: "testdata/apm-codex-marketplace.schema.json", path: []string{"$defs", "policy"}},
		{name: "CodexLocalSource", goType: reflect.TypeOf(CodexLocalSource{}), schemaFile: "testdata/apm-codex-marketplace.schema.json", path: []string{"$defs", "localSource"}},
		{
			name: "RemoteSource(codex)", goType: reflect.TypeOf(RemoteSource{}), schemaFile: "testdata/apm-codex-marketplace.schema.json", path: []string{"$defs", "remoteSource"},
			goOnlyAllowed: []string{"repo"},
		},
	}

	actualSchemaOnlyAllowed := map[string][]string{}
	actualGoOnlyAllowed := map[string][]string{}
	for _, c := range cases {
		if len(c.schemaOnlyAllowed) > 0 {
			actualSchemaOnlyAllowed[c.name] = c.schemaOnlyAllowed
		}
		if len(c.goOnlyAllowed) > 0 {
			actualGoOnlyAllowed[c.name] = c.goOnlyAllowed
		}
	}
	t.Run("schemaOnlyAllowed is exactly the fixed per-case mapping", func(t *testing.T) {
		assertExactWhitelistMapping(t, "schemaOnlyAllowed", actualSchemaOnlyAllowed, wantSchemaOnlyAllowed)
	})
	t.Run("goOnlyAllowed is exactly the fixed per-case mapping", func(t *testing.T) {
		assertExactWhitelistMapping(t, "goOnlyAllowed", actualGoOnlyAllowed, wantGoOnlyAllowed)
	})

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
			schemaProps, schemaRequired, isOneOf := schemaNodePropsAndRequired(root, node)

			assertFieldSetsEqualWithWhitelists(t, c.name+" properties", goAll, schemaProps, c.schemaOnlyAllowed, c.goOnlyAllowed)
			if isOneOf {
				// A oneOf-shaped def's required list is the INTERSECTION
				// across branches (schemaNodePropsAndRequired's doc comment)
				// -- only a Go-required ⊆ schema-required-intersection check
				// makes sense here, not full equality: a field can be
				// unconditionally required by a narrower variant set (e.g.
				// codex's remoteSource always requires "url") without that
				// being true of the flat Go struct, which is shared across a
				// wider set of variants (Claude's "github" variant never
				// sets "url"). This coarse-grained per-def check is
				// deliberately supplemented, not replaced, by
				// TestSchemaReject_RemoteSourceVariants' per-variant
				// negative tests below (a Tier-2 audit finding: silently
				// dropping a single branch's required field, e.g. github's
				// "repo", changes neither the union nor the intersection
				// computed here, so only an explicit per-variant validate-
				// must-fail assertion actually catches that regression).
				assertSubset(t, c.name+" required (Go ⊆ oneOf-intersection)", goRequired, schemaRequired)
			} else {
				assertFieldSetsEqual(t, c.name+" required", goRequired, schemaRequired)
			}

			for _, f := range fields {
				if f.kind == reflect.Interface {
					reason, ok := c.typeCheckSkip[f.name]
					if !ok {
						t.Errorf("%s: field %q is interface{} (any) but not documented in typeCheckSkip -- add a reason or map its type", c.name, f.name)
						continue
					}
					t.Logf("%s: skipping type-check for %q: %s", c.name, f.name, reason)
					continue
				}
				if _, whitelisted := c.typeCheckSkip[f.name]; whitelisted {
					t.Errorf("%s: field %q is in typeCheckSkip but its Go Kind (%v) is not interface{} -- remove it from the skip list and let it be type-checked", c.name, f.name, f.kind)
					continue
				}
				if containsString(c.goOnlyAllowed, f.name) {
					t.Logf("%s: skipping type-check for %q: Go-only field (goOnlyAllowed), no schema property to compare against", c.name, f.name)
					continue
				}
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

				// Fix 3b's Go-cross-check half: a Go []string field's schema
				// property must declare items.type=="string" (the tree-wide
				// half -- every array property has SOME non-empty items.type
				// at all -- is enforced package-wide by
				// TestSchemaDrift_SchemaStructuralInvariants, independent of
				// any particular Go field).
				if f.kind == reflect.Slice && f.elemKind == reflect.String {
					itemsType, found := schemaPropertyItemsType(root, node, f.name)
					if !found {
						t.Errorf("%s: field %q is a Go []string but schema has no resolvable items.type to compare against", c.name, f.name)
					} else if itemsType != "string" {
						t.Errorf("%s: field %q is a Go []string but schema declares items.type=%q, want \"string\"", c.name, f.name, itemsType)
					}
				}

				// Tier-8 audit Fix 3a: a Go map[string]V field's schema
				// property must declare additionalProperties.type equal to
				// V's own expected schema type (currently only
				// ClaudePlugin.Author, a map[string]string -- V=string).
				// map[string]any (V's Kind is reflect.Interface -- e.g.
				// ClaudeDocument.Metadata, a genuinely free-form passthrough
				// object with no single enforceable value type, already
				// covered by additionalPropertiesExceptions instead) is
				// skipped here the same way a top-level interface{} field is
				// skipped via typeCheckSkip -- there is no single JSON type
				// for an arbitrary value to compare against.
				if f.kind == reflect.Map && f.mapValueKind != reflect.Interface {
					expectedValueType, ok := expectedSchemaType(f.mapValueKind)
					if !ok {
						t.Fatalf("%s: unhandled Go map value Kind %v for field %q -- add a case to expectedSchemaType", c.name, f.mapValueKind, f.name)
					}
					actualAPType, found := schemaPropertyAdditionalPropertiesType(root, node, f.name)
					if !found {
						t.Errorf("%s: field %q is a Go map but schema's additionalProperties has no resolvable \"type\" to compare against", c.name, f.name)
					} else if actualAPType != expectedValueType {
						t.Errorf("%s: field %q is a Go map[string]%v but schema declares additionalProperties.type=%q, want %q", c.name, f.name, f.mapValueKind, actualAPType, expectedValueType)
					}
				}
			}
		})
	}
}

// assertFieldSetsEqualWithWhitelists is assertFieldSetsEqual, except fields
// listed in schemaOnlyAllowed (schema has, Go doesn't) or goOnlyAllowed (Go
// has, schema doesn't) are tolerated instead of reported as drift -- AND
// (the reverse check design.md's follow-up review required) any whitelisted
// field that has since stopped being one-sided (schema-only field also
// showed up in the Go type, or vice versa) is itself reported as an error,
// so a future "the field stopped being an exception" change is still caught
// red rather than silently staying green with a now-meaningless whitelist
// entry.
func assertFieldSetsEqualWithWhitelists(t *testing.T, label string, goFields, schemaFields, schemaOnlyAllowed, goOnlyAllowed []string) {
	t.Helper()
	goSet := map[string]bool{}
	for _, x := range goFields {
		goSet[x] = true
	}
	schemaSet := map[string]bool{}
	for _, x := range schemaFields {
		schemaSet[x] = true
	}
	schemaOnlySet := map[string]bool{}
	for _, x := range schemaOnlyAllowed {
		schemaOnlySet[x] = true
	}
	goOnlySet := map[string]bool{}
	for _, x := range goOnlyAllowed {
		goOnlySet[x] = true
	}

	var onlyGo []string
	for k := range goSet {
		if !schemaSet[k] && !goOnlySet[k] {
			onlyGo = append(onlyGo, k)
		}
	}
	var unexpectedSchemaOnly []string
	for k := range schemaSet {
		if !goSet[k] && !schemaOnlySet[k] {
			unexpectedSchemaOnly = append(unexpectedSchemaOnly, k)
		}
	}
	var staleSchemaOnly []string
	for k := range schemaOnlySet {
		if goSet[k] {
			staleSchemaOnly = append(staleSchemaOnly, k)
		}
	}
	var staleGoOnly []string
	for k := range goOnlySet {
		if schemaSet[k] {
			staleGoOnly = append(staleGoOnly, k)
		}
	}

	if len(onlyGo) == 0 && len(unexpectedSchemaOnly) == 0 && len(staleSchemaOnly) == 0 && len(staleGoOnly) == 0 {
		return
	}
	sort.Strings(onlyGo)
	sort.Strings(unexpectedSchemaOnly)
	sort.Strings(staleSchemaOnly)
	sort.Strings(staleGoOnly)
	if len(onlyGo) > 0 {
		t.Errorf("%s: fields present in Go type but missing from schema, and not in goOnlyAllowed %v: %v", label, goOnlyAllowed, onlyGo)
	}
	if len(unexpectedSchemaOnly) > 0 {
		t.Errorf("%s: fields present in schema but missing from Go type, and not in schemaOnlyAllowed %v: %v", label, schemaOnlyAllowed, unexpectedSchemaOnly)
	}
	if len(staleSchemaOnly) > 0 {
		t.Errorf("%s: schemaOnlyAllowed entries %v now also exist in the Go type's json tags -- the field is no longer schema-only, remove it from schemaOnlyAllowed", label, staleSchemaOnly)
	}
	if len(staleGoOnly) > 0 {
		t.Errorf("%s: goOnlyAllowed entries %v now also exist in the schema -- the field is no longer Go-only, remove it from goOnlyAllowed", label, staleGoOnly)
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// interfaceFieldOneOfTopology closes this file's BLOCKING-3 (codex round 7):
// TestSchemaDrift_GoTypesMatchSchemaProperties' type-check loop skips any
// field whose Go Kind is reflect.Interface via typeCheckSkip (ClaudePlugin.
// Source, CodexPlugin.Source -- each a real, mixed-type oneOf with no single
// JSON type to compare), but that skip left the oneOf's own BRANCH LIST
// completely unchecked: appending a third branch (e.g. {"type":"number"})
// changes neither the property-name set, the required set, nor any type
// check (every affected check already treats these fields as "no single
// type, skip"), so a document like {"source":123} would wrongly validate.
// This table locks each interface{}-typed field's oneOf array to its EXACT
// expected branch list (order-independent; each branch's own shape compared
// by exact JSON equality via branchSignatures) -- read directly off
// composeClaudePlugin's IsLocal branch (mapper.go:176-192, a bare string) /
// composeRemoteSource (mapper.go:214-244, RemoteSource) for Claude, and
// composeCodexSource (codexmapper.go:129-155, CodexLocalSource or
// RemoteSource) for Codex.
var interfaceFieldOneOfTopology = []struct {
	label      string
	schemaFile string
	path       []string
	property   string
	want       []map[string]any
}{
	{
		label: "ClaudePlugin.source", schemaFile: "testdata/apm-claude-marketplace.schema.json",
		path: []string{"$defs", "plugin"}, property: "source",
		want: []map[string]any{
			{"type": "string"},
			{"$ref": "#/$defs/remoteSource"},
		},
	},
	{
		label: "CodexPlugin.source", schemaFile: "testdata/apm-codex-marketplace.schema.json",
		path: []string{"$defs", "plugin"}, property: "source",
		want: []map[string]any{
			{"$ref": "#/$defs/localSource"},
			{"$ref": "#/$defs/remoteSource"},
		},
	},
}

// branchSignatures canonicalizes each oneOf branch (a map[string]any) to its
// JSON-marshaled form (encoding/json sorts map keys, so this is a stable,
// order-independent-PER-BRANCH signature) for set comparison via
// assertFieldSetsEqual -- an extra, missing, or shape-altered branch shows up
// as an only-in-one-side signature.
func branchSignatures(t *testing.T, branches []any) []string {
	t.Helper()
	out := make([]string, 0, len(branches))
	for _, b := range branches {
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("marshal oneOf branch %#v: %v", b, err)
		}
		out = append(out, string(data))
	}
	return out
}

func TestSchemaDrift_InterfaceFieldOneOfTopology(t *testing.T) {
	for _, tc := range interfaceFieldOneOfTopology {
		t.Run(tc.label, func(t *testing.T) {
			node := schemaNode(t, tc.schemaFile, tc.path)
			propsRaw, ok := node["properties"].(map[string]any)
			if !ok {
				t.Fatalf("%s: def node has no \"properties\"", tc.label)
			}
			propNode, ok := propsRaw[tc.property].(map[string]any)
			if !ok {
				t.Fatalf("%s: def node has no %q property", tc.label, tc.property)
			}
			branchesRaw, ok := propNode["oneOf"].([]any)
			if !ok {
				t.Fatalf("%s: properties.%s has no \"oneOf\" array (got %#v)", tc.label, tc.property, propNode)
			}
			gotSigs := branchSignatures(t, branchesRaw)
			wantAny := make([]any, len(tc.want))
			for i, w := range tc.want {
				wantAny[i] = w
			}
			wantSigs := branchSignatures(t, wantAny)
			assertFieldSetsEqual(t, tc.label+" oneOf branches", gotSigs, wantSigs)
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
// never a family-wide flattened union. A Tier-5 audit finding against an
// earlier version of this test (which unioned every sub-table's field names
// together before comparing against the family's flattened schema
// properties) showed that a union comparison is blind to a single table
// LOSING a row that also happens to appear in a sibling table -- e.g.
// deleting ClaudeOwner's `name` row changes nothing, since ClaudeDocument's
// and ClaudePlugin's own `name` rows keep the family-wide union unchanged.
// Comparing table-by-table against schemaNodePropsAndRequired's own
// properties (oneOf-aware union across branches, for remoteSource) closes
// that hole. Each table's rows are read from its bindSpecSchemaCase-bound
// occurrence (Tier-8 audit fix), not a first-match substring search, so a
// duplicate/bogus table sharing the same heading prefix can't silently
// substitute its own (unchecked) content for the real one.
func TestSchemaSync_SpecMatchesSchemaFieldSet(t *testing.T) {
	specPath := filepath.Join(findRepoRoot(t), "spec", "conformance", "agent-schema.md")
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
			root := schemaDoc(t, c.schemaFile)
			node := schemaNode(t, c.schemaFile, c.path)
			props, _, _ := schemaNodePropsAndRequired(root, node)
			assertFieldMapsEqual(t, c.subHeading, spec, fieldSet(props...))
		})
	}
}

// fieldTableHeaderRe matches a field-table's markdown header row verbatim --
// every one of the spec's real field tables uses exactly this header text
// (confirmed by grepping the whole file: `grep -n "^|.*|.*|$"
// agent-schema.md` finds exactly 12 lines with this header and exactly 2
// with a different header -- "### claude / copilot 差異"'s 生態/mcpServers
// table and "## 可執行 schema 對照表"'s 產物家族/schema 檔 table -- neither of
// which needs mapping since neither is a field table in the first place).
var fieldTableHeaderRe = regexp.MustCompile(`(?m)^\| 欄位 \| 型別 \| 必填/選填 \| 預設值 \| 上游出處 \|\s*$`)

// anyHeadingRe matches any "## " or "### " markdown heading line verbatim
// (used to find the nearest PRECEDING heading for a given field-table
// header, regardless of heading level or which family it's under).
var anyHeadingRe = regexp.MustCompile(`(?m)^#{2,3} .+$`)

// foreignSubHeadings lists sub-headings this package's specSchemaCases does
// NOT itself map, but that legitimately belong to a DIFFERENT package's own
// specSchemaCases -- currently just the plugin.json family (owned by
// internal/pack/bundle/schema_sync_test.go, which carries the exact mirror-
// image list pointing back at this package's 10 marketplace sub-headings).
// TestSchemaSync_AllFieldTablesAreMapped scans the WHOLE spec file (a Tier-7
// audit fix: an earlier version only scanned inside the "## Claude
// marketplace.json"/"## Codex marketplace.json" sections, so a bogus field
// table pasted OUTSIDE any recognized family section -- e.g. at the very
// end of the file, or under a brand-new "## some new family" heading nobody
// wired in yet -- was completely invisible), so it needs this acknowledgment
// to avoid flagging bundle's own tables as unmapped. A genuinely new/rogue
// table (in NEITHER package's specSchemaCases NOR the other's
// foreignSubHeadings) is still caught by both packages' independent scans.
var foreignSubHeadings = []string{
	"### 欄位（`PluginManifest`",
	"### author（`Author`",
}

// firstBacktickTokenRe extracts the first backtick-quoted token from a
// heading line (e.g. "ClaudeDocument" out of "### 文件層（`ClaudeDocument`,
// `mapper.go:30`）") -- subtestLabel's ASCII-safe building block.
var firstBacktickTokenRe = regexp.MustCompile("`([^`]+)`")

// subtestLabel builds an ASCII-only t.Run name for heading (a Tier-7 audit
// fix: `go test -json`'s output, piped through PowerShell on Windows, was
// observed to mis-encode/mis-split multi-byte CJK + fullwidth-punctuation
// test names badly enough that verify.ps1's line-by-line JSON parser choked
// -- "看起來像 JSON 卻無法解析的行" -- even though the exact same CJK text has
// always rendered fine as ordinary t.Errorf/log OUTPUT text elsewhere in
// this file; it is specifically using it as a *test name* -- embedded in
// every `go test -json` event's "Test" field -- that's new and risky here.
// index disambiguates when the extracted token collides (e.g. "RemoteSource"
// legitimately appears in both the claude and codex families); the full
// original heading is still used verbatim in this test's t.Errorf messages,
// which only affects human-readable Output text, not the Test-name field.
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

// TestSchemaSync_AllFieldTablesAreMapped is TestSchemaSync_
// SpecMatchesSchemaFieldSet's fail-closed complement: that test only checks
// sub-tables specSchemaCases ALREADY lists, so a brand-new field table --
// added to the spec but never wired into specSchemaCases, wherever it
// physically lives in the file -- would silently never be checked against
// anything. This test finds every field-table header (fieldTableHeaderRe)
// anywhere in the whole document (discoverFieldTables), identifies its
// OWNING heading (the nearest PRECEDING "## "/"### " line, by byte offset --
// not scoped to any particular family section), and:
//
//  1. asserts that heading is a prefix-match for some specSchemaCases entry
//     (this package's own) or foreignSubHeadings entry (acknowledged as
//     belonging to bundle's own specSchemaCases);
//  2. (Tier-8 audit fix) separately asserts each specSchemaCases entry
//     matches EXACTLY ONE discovered table -- a second table sharing the
//     same heading prefix (e.g. a bogus duplicate "### owner（`ClaudeOwner`
//     ..." pasted elsewhere) would previously satisfy check 1 for BOTH
//     occurrences without complaint, while every other test in this file
//     only ever reads the FIRST one -- silently ignoring the duplicate's own
//     (possibly wrong) content entirely. bindSpecSchemaCase performs the
//     same "exactly 1" check that TestSchemaSync_SpecMatchesSchemaFieldSet/
//     TestSchemaSync_SpecMatchesSchemaTypesAndRequiredness themselves rely
//     on to pick which table to read, so this loop doubles as that
//     assumption's own explicit test.
func TestSchemaSync_AllFieldTablesAreMapped(t *testing.T) {
	specPath := filepath.Join(findRepoRoot(t), "spec", "conformance", "agent-schema.md")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}
	content := string(data)
	tables := discoverFieldTables(content)
	if len(tables) == 0 {
		t.Fatal("found zero field-table headers in the whole spec file -- fieldTableHeaderRe may be stale (does the header text still read \"| 欄位 | 型別 | 必填/選填 | 預設值 | 上游出處 |\"?)")
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
// Every check above this point is a SEMANTIC PROJECTION: it reads some
// specific slice of a schema file (a property's type, a required list, an
// enum, a oneOf branch list, ...) and compares that slice against the Go
// type or the spec doc. Every one of those projections has a bounded depth
// -- this file's own audit history (see verification-record.md) is eight-plus
// rounds of finding and closing a projection that didn't look far enough
// (items/oneOf/$ref never projected, enum compared as a union instead of
// per-branch, a free-form node's "required" key never checked, ...). A
// coordinator-mandated design decision closes that class of gap completely,
// independent of how deep any future projection reaches: a byte-for-byte
// SHA-256 of the schema file's raw content, recorded in agent-schema.md's
// "## 可執行 schema 對照表" and re-verified here. If EVEN ONE BYTE of a
// schema file changes for ANY reason -- including a change no current
// projection test happens to notice -- this hash goes stale and this test
// goes red; the burden shifts from "did we think to check this" to "did you
// remember to update the hash", which is a single, uniform, always-correct
// check. The semantic projection tests remain load-bearing as the DIAGNOSTIC
// layer: they tell you WHAT changed and WHETHER the spec prose needs a
// matching edit, which the hash alone cannot (a hash mismatch alone doesn't
// say "the owner.email type changed" -- see this test's error message,
// which explicitly routes the reader back to the semantic tests first).

// schemaFileHashRowRe matches EVERY data row of agent-schema.md's "## 可執行
// schema 對照表" table whose SECOND column is a backtick-quoted path ending
// in .schema.json (group 1) -- the row-detection criterion is deliberately
// just that, not "and its hash column happens to already be valid" (codex
// round-8 audit): the previous version of this regex additionally required
// group 2 to already match `[0-9a-fA-F]{64}` at the REGEX level, which meant
// a malformed hash column (wrong length, non-hex characters) made the whole
// row invisible to the parser -- not reported as invalid, just as if the row
// didn't exist at all. A second, otherwise-valid row for an already-covered
// path, given a malformed 63-character "hash", silently escaped both the
// duplicate-row check and the mismatch check, contradicting this file's own
// "缺列/多列全紅" fail-closed claim. Group 2 here is now the LAST backtick-
// quoted token on the row, WHATEVER its shape (matched via a permissive
// non-backtick character class, anchored so the row must still end with a
// backtick-quoted token immediately followed by the row's closing pipe --
// i.e. the hash column is still specifically the row's LAST cell, not just
// any backtick span anywhere on the line) -- hash-SHAPE validation happens
// explicitly, separately, in
// TestSchemaSync_SchemaFileHashesMatchSpec, so a malformed hash column is
// itself a reported failure with the row's actual content quoted, never a
// silent non-match.
var schemaFileHashRowRe = regexp.MustCompile("^\\|[^|]*\\|\\s*`([^`]+\\.schema\\.json)`\\s*\\|.*`([^`]*)`\\s*\\|\\s*$")

// validSHA256HexRe is the hash-column shape check: exactly 64 hex characters
// (case-insensitive on read; this repo's own convention is lower-case, see
// ownSchemaFileHashPaths' entries, but a malformed row -- 63 characters,
// non-hex characters, or entirely empty -- must fail this and be reported,
// not silently compared as an unequal string that happens to also fail).
var validSHA256HexRe = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// specSchemaFileHash is one parsed hash-seal table row.
type specSchemaFileHash struct {
	specPath string // full path exactly as written in the spec table, e.g. "internal/marketplace/build/testdata/apm-claude-marketplace.schema.json"
	hashRaw  string // the hash column's literal text, UNVALIDATED -- may be malformed; see validSHA256HexRe
}

// parseSchemaFileHashTable finds "## 可執行 schema 對照表"'s section in
// content (from that heading up to the next "## " heading, or EOF) and
// extracts every row matching schemaFileHashRowRe -- every row whose second
// column is a .schema.json path, regardless of whether its hash column is
// well-formed (see schemaFileHashRowRe's doc comment for why this is now
// deliberately permissive at the row-detection stage).
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
// spec-table paths (the same strings the table's second column must contain
// verbatim) -- TestSchemaSync_SchemaFileHashesMatchSpec requires EXACTLY one
// row per entry here, with a SHA-256 matching the file's actual current
// bytes.
var ownSchemaFileHashPaths = []string{
	"internal/marketplace/build/testdata/apm-claude-marketplace.schema.json",
	"internal/marketplace/build/testdata/apm-codex-marketplace.schema.json",
}

// foreignSchemaFileHashPaths acknowledges the sibling package's
// (internal/pack/bundle) own two schema-file hash rows -- present in the
// same shared table, intentionally NOT verified by this package's own test
// (bundle's own TestSchemaSync_SchemaFileHashesMatchSpec verifies them
// instead) -- mirrors foreignSubHeadings' cross-acknowledgment pattern.
var foreignSchemaFileHashPaths = []string{
	"internal/pack/bundle/testdata/apm-plugin-claude.schema.json",
	"internal/pack/bundle/testdata/apm-plugin-copilot.schema.json",
}

// sha256HexFile returns the lower-case-hex SHA-256 of path's content with CRLF
// normalized to LF.
//
// Hashing raw bytes made the seal platform-dependent: this repo has
// core.autocrlf=true, so a Windows checkout materializes CRLF while git stores
// LF, and any tool that rewrites a schema file (sed, a Python read/write) can
// silently flip the file to LF. The recorded hash then matches whichever form
// happened to be on disk when it was computed, and fails on a fresh clone --
// observed for real: a hash recorded from an LF working copy did not match the
// same commit's CRLF checkout. Normalizing removes the line-ending degree of
// freedom without weakening detection of any actual content change.
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
// primary R3.1.2 mechanism (see this section's header comment): fails closed
// on a missing row for either of this package's own two schema files, more
// than one row for either (a codex round-8 audit fix: this now also catches
// a duplicate row whose hash column is malformed, not just a duplicate valid
// row -- see schemaFileHashRowRe's doc comment), a row whose hash column
// isn't a well-formed 64-hex string, a row whose SHA-256 doesn't match the
// file's actual current bytes, or an unrecognized schema-file hash row
// (neither this package's own nor the sibling's acknowledged one).
func TestSchemaSync_SchemaFileHashesMatchSpec(t *testing.T) {
	specPath := filepath.Join(findRepoRoot(t), "spec", "conformance", "agent-schema.md")
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

// upstreamGoldenProvenance records which upstream release each
// testdata/upstream-*.golden.json was captured from, and the exact fixture
// that produced it, so the files can be regenerated rather than guessed at.
//
//	cd <tmp> && cat > apm.yml <<'YAML'
//	name: demo
//	version: 1.0.0
//	license: MIT
//	marketplace:
//	  name: my-marketplace
//	  owner: {name: acme-org, url: https://github.com/acme-org}
//	  outputs: {claude: {}, codex: {}}
//	  packages:
//	    - name: impeccable
//	      description: "The design language that makes your AI harness better at design."
//	      source: pbakaus/impeccable
//	      ref: fc2e694afca1ac0cc384b4fe56bab3335fea7912
//	      category: Productivity
//	YAML
//	uv --project <upstream> run apm pack   # needs network
var upstreamGoldenProvenance = []struct {
	file    string
	release string
}{
	{"testdata/upstream-claude-marketplace.golden.json", "v0.27.0"},
	{"testdata/upstream-codex-marketplace.golden.json", "v0.27.0"},
}

// TestSchemaGolden_UpstreamGoldensAreNotStale closes the hole that let the
// v0.26.0-era upstream goldens survive the v0.27.0 tag_pattern change: the
// two TestSchemaGolden_Upstream* tests above only validate each golden
// AGAINST THE SCHEMA, and tag_pattern is deliberately optional there (a
// pre-v0.27.0 marketplace.json must still be accepted -- see
// TestSchemaGolden_RemoteSourceVariantsMinimal, and upstream models.py's
// "None means old marketplace.json"). An optional field can therefore go
// missing from an "upstream" fixture without any test noticing.
//
// This test asserts the stronger, provenance-specific property instead: a
// fixture claiming to be a v0.27.0 capture must carry every field v0.27.0
// unconditionally emits. tag_pattern is one of those -- yml_schema.py:609
// defaults build.tagPattern to "v{version}", so builder.py's
// `entry.tag_pattern or yml.build.tag_pattern` is never empty and
// output_mappers.py's _set_effective_tag_pattern always fires on a REMOTE
// source. (Local sources are plain strings / CodexLocalSource and are
// skipped here, matching upstream, which only calls the helper on the
// remote branches.)
func TestSchemaGolden_UpstreamGoldensAreNotStale(t *testing.T) {
	for _, g := range upstreamGoldenProvenance {
		t.Run(g.file, func(t *testing.T) {
			data, err := os.ReadFile(g.file)
			if err != nil {
				t.Fatalf("read %s: %v", g.file, err)
			}
			var doc struct {
				Plugins []struct {
					Name   string          `json:"name"`
					Source json.RawMessage `json:"source"`
				} `json:"plugins"`
			}
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatalf("parse %s: %v", g.file, err)
			}
			if len(doc.Plugins) == 0 {
				t.Fatalf("%s has no plugins; it cannot evidence anything", g.file)
			}
			remotes := 0
			for _, p := range doc.Plugins {
				var obj map[string]any
				if err := json.Unmarshal(p.Source, &obj); err != nil {
					continue // plain-string (local) source
				}
				if kind, _ := obj["source"].(string); kind == "local" {
					continue
				}
				remotes++
				if _, ok := obj["tag_pattern"]; !ok {
					t.Errorf("plugin %q in %s is a remote source with no tag_pattern; %s always emits it, so this fixture predates %s and must be regenerated (see upstreamGoldenProvenance for the fixture)",
						p.Name, g.file, g.release, g.release)
				}
			}
			if remotes == 0 {
				t.Errorf("%s has no remote source; it cannot evidence remote-branch behaviour", g.file)
			}
		})
	}
}
