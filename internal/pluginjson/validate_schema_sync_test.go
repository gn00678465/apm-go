// Anti-drift test for WP01's static rule table (validate.go) against the
// vendored official schema (C-004, research.md R-03/R-04). Mirrors
// internal/pack/bundle/schema_sync_test.go's schemaDoc/schemaPropsAndRequired/
// schemaNode-style helpers (reimplemented locally per the WP02 task prompt:
// there is no shared test-helper package between internal/pack/bundle and
// internal/pluginjson).
//
// Vendored schema provenance (data-model.md's own stated workaround for
// JSON's lack of comment syntax): tests/fixtures/schemas/
// claude-code-plugin.schema.json from upstream microsoft/apm, commit
// b75a02b1 (the pinned Oracle, = v0.29.0), retrieved from the local clone at
// D:/Projects2/apm on 2026-09-09 via
// `git show b75a02b1:tests/fixtures/schemas/claude-code-plugin.schema.json`.
// Copied byte-for-byte into testdata/claude-code-plugin.schema.json.
package pluginjson

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

const schemaFixturePath = "testdata/claude-code-plugin.schema.json"

// pathPatternValue is the exact JSON-Schema "pattern" value the vendored
// schema uses on every path-typed string field (e.g. properties.skills.
// anyOf[0].pattern). A plain, exact string match against this constant --
// not a regex-engine invocation -- is deliberately all this test does
// (WP02 task prompt T009 step 3; Risk note: fail loudly, not silently, if a
// future schema update ever expresses the constraint with different pattern
// text, since collectPathPatternFields would then find zero matches and the
// PathRuleNamesMatchSchemaPatternFields subtest would report every current
// path field as "only in rule table").
const pathPatternValue = `^\.\/.*`

func loadSchemaDoc(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(schemaFixturePath)
	if err != nil {
		t.Fatalf("read %s: %v", schemaFixturePath, err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("unmarshal %s: %v (vendored schema must be valid JSON -- fail-closed per T008 step 3)", schemaFixturePath, err)
	}
	return root
}

func schemaProperties(t *testing.T, root map[string]any) map[string]any {
	t.Helper()
	props, ok := root["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema %s has no top-level \"properties\" object", schemaFixturePath)
	}
	return props
}

func schemaRequiredList(t *testing.T, root map[string]any) []string {
	t.Helper()
	reqRaw, ok := root["required"].([]any)
	if !ok {
		t.Fatalf("schema %s has no top-level \"required\" array", schemaFixturePath)
	}
	out := make([]string, 0, len(reqRaw))
	for _, r := range reqRaw {
		s, ok := r.(string)
		if !ok {
			t.Fatalf("schema %s: \"required\" entry %#v is not a string", schemaFixturePath, r)
		}
		out = append(out, s)
	}
	return out
}

// walkAnyOf calls visit(n) for node and then recurses into every branch of
// node's own "anyOf" array -- the only composition keyword this vendored
// schema uses to express a field's multiple legal SHAPES (WP02 finding 3:
// "allOf" is a same-type pattern refinement here, never a shape alternative,
// so schemaTypeSet/schemaArrayItemTypeSet must not walk into it; unioning
// allOf branches into the legal-shape set would hide a future allOf that
// actually restricts the shape instead of merely narrowing its pattern).
// assertOnlyKnownCombinators verifies that assumption holds for the current
// fixture and fails loudly the moment it does not.
func walkAnyOf(node map[string]any, visit func(map[string]any)) {
	if node == nil {
		return
	}
	visit(node)
	branches, ok := node["anyOf"].([]any)
	if !ok {
		return
	}
	for _, b := range branches {
		if bm, ok := b.(map[string]any); ok {
			walkAnyOf(bm, visit)
		}
	}
}

// schemaTypeSet collects every JSON Schema "type" string reachable from
// node via anyOf composition -- the set of top-level JSON shapes a value of
// this schema node may legally take. It does not descend into "items" (a
// separate axis: an array branch's element shape, not the field's own shape
// -- see schemaArrayItemTypeSet) or "allOf" (see walkAnyOf).
func schemaTypeSet(node map[string]any) map[string]bool {
	out := map[string]bool{}
	walkAnyOf(node, func(n map[string]any) {
		if t, ok := n["type"].(string); ok {
			out[t] = true
		}
	})
	return out
}

// schemaArrayItemTypeSet collects, for every anyOf branch of node that
// declares "type":"array", the JSON Schema "type" set of that branch's
// "items" node (itself resolved via schemaTypeSet, so an items node that is
// itself anyOf-shaped is handled too). Empty when node has no array branch.
func schemaArrayItemTypeSet(node map[string]any) map[string]bool {
	out := map[string]bool{}
	walkAnyOf(node, func(n map[string]any) {
		if t, _ := n["type"].(string); t != "array" {
			return
		}
		items, ok := n["items"].(map[string]any)
		if !ok {
			return
		}
		for k := range schemaTypeSet(items) {
			out[k] = true
		}
	})
	return out
}

// walkAnyOfAllOfForPatterns is hasPathPattern's own traversal: unlike
// schemaTypeSet/schemaArrayItemTypeSet, it also descends into "allOf"
// branches, because every path pattern in this schema lives inside one
// (e.g. a string branch's own "pattern" plus an allOf sibling narrowing it
// further with ".*\.json$"). This is safe only because
// assertOnlyKnownCombinators independently proves every allOf branch here
// shares its parent's type -- a pure refinement, not an alternate shape --
// so walking into it for pattern discovery cannot silently change which
// shapes are legal, only which of them this helper notices a pattern on.
func walkAnyOfAllOfForPatterns(node map[string]any, visit func(map[string]any)) {
	if node == nil {
		return
	}
	visit(node)
	for _, key := range []string{"anyOf", "allOf"} {
		branches, ok := node[key].([]any)
		if !ok {
			continue
		}
		for _, b := range branches {
			if bm, ok := b.(map[string]any); ok {
				walkAnyOfAllOfForPatterns(bm, visit)
			}
		}
	}
}

// hasPathPattern reports whether node's own schema subtree (any anyOf/allOf
// branch, at any depth) declares "pattern" == pathPatternValue on some
// node. This is T009 step 3's "own node has pattern ^\./" marker, applied
// recursively through the anyOf/allOf composition every one of this
// schema's path-typed fields (hooks/commands/agents/skills/outputStyles/
// themes/mcpServers/lspServers/monitors) actually uses to combine the path
// constraint with a second constraint (e.g. a ".*\.json$"/".*\.md$" suffix)
// -- confirmed by direct inspection of the vendored fixture (2026-09-09).
func hasPathPattern(node map[string]any) bool {
	found := false
	walkAnyOfAllOfForPatterns(node, func(n map[string]any) {
		if p, ok := n["pattern"].(string); ok && p == pathPatternValue {
			found = true
		}
	})
	return found
}

// unhandledCombinatorKeys are JSON-Schema composition keywords none of this
// file's traversal helpers implement. This vendored schema does not use any
// of them today (verified 2026-09-09); assertOnlyKnownCombinators exists so
// a future schema update introducing one fails the test loudly instead of
// walkAnyOf/walkAnyOfAllOfForPatterns silently skipping it and reporting a
// stale sync result (WP02 finding 3).
var unhandledCombinatorKeys = []string{"oneOf", "not", "$ref", "if", "then", "else"}

// assertOnlyKnownCombinators walks node (through anyOf, allOf, and items --
// the only nesting axes this schema uses) and fails the test the moment it
// finds an unhandledCombinatorKeys entry, or an "allOf" branch that is not a
// same-type pattern refinement of its parent. It does not attempt to
// validate every JSON-Schema keyword this fixture contains (deliberately
// not a general schema engine): only the two axes schemaTypeSet,
// schemaArrayItemTypeSet, and hasPathPattern actually rely on.
func assertOnlyKnownCombinators(t *testing.T, path string, node map[string]any) {
	t.Helper()
	if node == nil {
		return
	}
	for _, forbidden := range unhandledCombinatorKeys {
		if _, present := node[forbidden]; present {
			t.Fatalf("schema node %s uses %q, a combinator none of this file's traversal helpers implement -- extend schemaTypeSet/schemaArrayItemTypeSet/hasPathPattern (and this guard) before trusting sync results against it", path, forbidden)
		}
	}
	if allOf, ok := node["allOf"].([]any); ok {
		parentType, _ := node["type"].(string)
		for i, b := range allOf {
			bm, ok := b.(map[string]any)
			if !ok {
				t.Fatalf("schema node %s: allOf[%d] is not an object", path, i)
			}
			if bt, ok := bm["type"].(string); ok && bt != parentType {
				t.Fatalf("schema node %s: allOf[%d] declares type %q, which differs from the parent's own %q -- this is a genuine shape restriction, not a same-type pattern refinement, and this traversal's walkAnyOf deliberately does not union allOf branches into the legal-shape set; that assumption just broke", path, i, bt, parentType)
			}
			assertOnlyKnownCombinators(t, fmt.Sprintf("%s.allOf[%d]", path, i), bm)
		}
	}
	if anyOf, ok := node["anyOf"].([]any); ok {
		for i, b := range anyOf {
			if bm, ok := b.(map[string]any); ok {
				assertOnlyKnownCombinators(t, fmt.Sprintf("%s.anyOf[%d]", path, i), bm)
			}
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		assertOnlyKnownCombinators(t, path+".items", items)
	}
}

// TestSchemaSync_NoUnhandledCombinators runs assertOnlyKnownCombinators over
// every top-level schema property so a future vendored-schema update that
// adds $ref/oneOf/not, or turns an allOf branch into a real type
// restriction, fails this suite loudly instead of the other sync tests
// silently reporting a clean result they can no longer trust (WP02 finding
// 3).
func TestSchemaSync_NoUnhandledCombinators(t *testing.T) {
	root := loadSchemaDoc(t)
	props := schemaProperties(t, root)
	for name, node := range props {
		nm, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("schema property %q is not an object node", name)
		}
		assertOnlyKnownCombinators(t, "properties."+name, nm)
	}
}

func schemaSourceRuleNames() map[string]bool {
	out := map[string]bool{}
	for _, r := range rules {
		if r.Source == sourceSchema {
			out[r.Name] = true
		}
	}
	return out
}

func rulePathNames() map[string]bool {
	out := map[string]bool{}
	for _, r := range rules {
		if r.IsPath {
			out[r.Name] = true
		}
	}
	return out
}

func schemaPathPatternFieldNames(t *testing.T, props map[string]any) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for name, node := range props {
		nm, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("schema property %q is not an object node", name)
		}
		if hasPathPattern(nm) {
			out[name] = true
		}
	}
	return out
}

func stringSet(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[v] = true
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// assertFieldSetsEqual mirrors internal/pack/bundle/schema_sync_test.go's
// identical-in-spirit helper: report BOTH directions of a mismatch (only in
// a, only in b), never a bare boolean, so a failure's message alone tells a
// maintainer which invariant broke without re-deriving both sets by hand.
func assertFieldSetsEqual(t *testing.T, label string, a, b map[string]bool) {
	t.Helper()
	var onlyA, onlyB []string
	for k := range a {
		if !b[k] {
			onlyA = append(onlyA, k)
		}
	}
	for k := range b {
		if !a[k] {
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

// assertSubset asserts every element of want is present in have, reporting
// exactly which want elements are missing (data-model.md's "⊇", not "=").
func assertSubset(t *testing.T, label string, have, want map[string]bool) {
	t.Helper()
	var missing []string
	for k := range want {
		if !have[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) == 0 {
		return
	}
	sort.Strings(missing)
	t.Errorf("%s: %v present in the required set but missing from the rule table", label, missing)
}

func TestSchemaSync_KnownFieldsSupersetOfSchemaProperties(t *testing.T) {
	root := loadSchemaDoc(t)
	props := schemaProperties(t, root)
	schemaNames := map[string]bool{}
	for name := range props {
		schemaNames[name] = true
	}
	ruleNames := schemaSourceRuleNames()

	// data-model.md's literal invariant is superset ("WP01's table also has
	// docs- and apm-go-sourced fields the schema doesn't know about"), so the
	// spec-required assertion is assertSubset. In practice every current
	// Source==schema rule corresponds to exactly one real schema property and
	// vice versa (verified 2026-09-09 against the vendored fixture: 22
	// properties, 22 Source==schema rules), so this test also asserts full
	// equality for stronger drift protection -- a rule mislabeled
	// Source==schema for a name the schema doesn't have would otherwise slip
	// through a superset-only check.
	t.Run("SubsetRequirement", func(t *testing.T) {
		assertSubset(t, "schema properties vs rule table (Source==schema)", ruleNames, schemaNames)
	})
	t.Run("ObservedEquality", func(t *testing.T) {
		assertFieldSetsEqual(t, "rule table (Source==schema) vs schema properties", ruleNames, schemaNames)
	})
}

func TestSchemaSync_RequiredIsExactlyName(t *testing.T) {
	root := loadSchemaDoc(t)
	required := schemaRequiredList(t, root)
	if len(required) != 1 || required[0] != "name" {
		t.Errorf("schema %s: required = %v, want exactly [\"name\"]", schemaFixturePath, required)
	}
}

// TestSchemaSync_PathRuleNamesMatchSchemaPatternFields implements data-
// model.md's second invariant: {r.Name | r.IsPath} must equal (schema's
// ^\./-pattern field set) ∪ {"workflows"}. experimental.themes and
// experimental.monitors are path-checked separately below, by behaviour
// rather than by rule-table membership (see the comment at their check).
func TestSchemaSync_PathRuleNamesMatchSchemaPatternFields(t *testing.T) {
	root := loadSchemaDoc(t)
	props := schemaProperties(t, root)
	schemaPathNames := schemaPathPatternFieldNames(t, props)
	want := map[string]bool{}
	for k := range schemaPathNames {
		want[k] = true
	}
	// data-model.md scopes this invariant to the rule table's top-level fields.
	// experimental.themes and experimental.monitors are path-checked by
	// checkExperimentalPathField, which no rules row drives, so they cannot
	// appear in {r.Name | r.IsPath}; they are pinned by behaviour below.
	want["workflows"] = true
	got := rulePathNames()
	assertFieldSetsEqual(t, "rule table IsPath names vs schema pattern fields plus workflows", got, want)

	for _, field := range []string{"themes", "monitors"} {
		manifest := []byte(fmt.Sprintf(`{"name":"x","experimental":{%q:"outside/x"}}`, field))
		report := Validate(manifest)
		found := false
		for _, f := range report.Findings {
			if f.Check == Paths && strings.Contains(f.Message, "experimental."+field) {
				found = true
			}
		}
		if !found {
			t.Errorf("experimental.%s: want a Paths finding for a value missing the './' prefix, got %+v", field, report.Findings)
		}
	}
}

// ruleKindKnownSchemaTypes maps each ruleKind to the set of top-level JSON
// Schema "type" values it accepts, matching checkKind's actual behavior --
// this is the source of truth kindShapeArrayItems below cross-checks against
// the vendored schema, not a second hand-written copy of validate.go's own
// intent (Risk mitigation: derive from the rule table's real Kind values,
// never a shadow list that can drift independently).
func ruleKindKnownSchemaTypes(k ruleKind) map[string]bool {
	switch k {
	case kindString:
		return stringSet("string")
	case kindObject:
		return stringSet("object")
	case kindBool:
		return stringSet("boolean")
	case kindArrayOfString:
		return stringSet("array")
	case kindStringOrArray:
		return stringSet("string", "array")
	case kindStringArrayOrObject:
		return stringSet("string", "object", "array")
	case kindStringOrObjectOrMixedArray:
		return stringSet("string", "object", "array")
	case kindDependencyList:
		return stringSet("array")
	case kindArrayOfObject:
		return stringSet("array")
	case kindStringOrArrayOfObject:
		return stringSet("string", "array")
	default:
		return nil
	}
}

// ruleKindArrayItemCommitment returns the exact array-item type set a
// ruleKind's OWN semantics commit to. Every kind whose array branch's
// element shape checkKind actually enforces must have a case here -- a
// case returning nil is an assertion that no such commitment exists, and
// must be justified by the CODE PATH, never by what the mismatch message
// happens to say (WP02 finding 1: kindStringArrayOrObject used to fall to
// the default nil case reasoning from its own message text, "must be a
// string, array, or object", which hid a real bug -- commands' array items
// really are string-only, so the commitment is knowable and checkable).
// kindDependencyList is the one legitimate nil: Validate special-cases
// "dependencies" before ever calling checkKind on it, so checkDependencies
// -- not checkKind -- owns and validates its element shape.
func ruleKindArrayItemCommitment(k ruleKind) map[string]bool {
	switch k {
	case kindArrayOfString:
		return stringSet("string")
	case kindStringOrArray:
		return stringSet("string")
	case kindArrayOfObject:
		return stringSet("object")
	case kindStringOrArrayOfObject:
		return stringSet("object")
	case kindStringArrayOrObject:
		return stringSet("string")
	case kindStringOrObjectOrMixedArray:
		return stringSet("string", "object")
	default:
		return nil
	}
}

// TestSchemaSync_RuleKindConsistentWithSchemaType is this WP's answer to the
// coordinator's explicit ask: compare each Source==schema rule's Kind
// against the vendored schema's ACTUAL declared type for that property --
// both the top-level type set (string/object/array/boolean, walking anyOf/
// allOf) and, for Kinds that commit to one, the array's item type set
// (walking into "items") -- so a rule claiming array-of-string against a
// schema property whose items are objects fails loudly, not just a
// field-NAME-presence check (which is all channels/monitors' previous wrong
// Kind assignments would have needed to slip past).
func TestSchemaSync_RuleKindConsistentWithSchemaType(t *testing.T) {
	root := loadSchemaDoc(t)
	props := schemaProperties(t, root)

	var names []string
	for _, r := range rules {
		if r.Source == sourceSchema {
			names = append(names, r.Name)
		}
	}
	sort.Strings(names)

	byName := map[string]rule{}
	for _, r := range rules {
		byName[r.Name] = r
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			node, ok := props[name].(map[string]any)
			if !ok {
				t.Fatalf("schema has no property node for %q (should have been caught by TestSchemaSync_KnownFieldsSupersetOfSchemaProperties)", name)
			}
			r := byName[name]

			wantTypes := ruleKindKnownSchemaTypes(r.Kind)
			if wantTypes == nil {
				t.Fatalf("ruleKindKnownSchemaTypes has no case for Kind %v (field %q) -- add one before this check can be trusted", r.Kind, name)
			}
			actualTypes := schemaTypeSet(node)
			assertFieldSetsEqual(t, fmt.Sprintf("%s: rule Kind's implied JSON types vs schema's declared types", name), wantTypes, actualTypes)

			if wantItems := ruleKindArrayItemCommitment(r.Kind); wantItems != nil {
				actualItems := schemaArrayItemTypeSet(node)
				assertFieldSetsEqual(t, fmt.Sprintf("%s: rule Kind's committed array-item type vs schema's declared array items type", name), wantItems, actualItems)
			}
		})
	}
}
