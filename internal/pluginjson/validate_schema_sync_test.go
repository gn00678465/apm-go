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

// walkAnyOfAllOf calls visit(n) for node and then recurses into every branch
// of node's own "anyOf"/"allOf" arrays (the only two composition keywords
// this vendored schema uses to express a field's multiple legal shapes) --
// shared traversal for both schemaTypeSet (this node's own declared "type"s)
// and schemaArrayItemTypeSet (the "type"s reachable inside an array
// branch's "items").
func walkAnyOfAllOf(node map[string]any, visit func(map[string]any)) {
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
				walkAnyOfAllOf(bm, visit)
			}
		}
	}
}

// schemaTypeSet collects every JSON Schema "type" string reachable from
// node via anyOf/allOf composition -- the set of top-level JSON shapes a
// value of this schema node may legally take. It does not descend into
// "items" (a separate axis: an array branch's element shape, not the
// field's own shape -- see schemaArrayItemTypeSet).
func schemaTypeSet(node map[string]any) map[string]bool {
	out := map[string]bool{}
	walkAnyOfAllOf(node, func(n map[string]any) {
		if t, ok := n["type"].(string); ok {
			out[t] = true
		}
	})
	return out
}

// schemaArrayItemTypeSet collects, for every anyOf/allOf branch of node that
// declares "type":"array", the JSON Schema "type" set of that branch's
// "items" node (itself resolved via schemaTypeSet, so an items node that is
// itself anyOf-shaped is handled too). Empty when node has no array branch.
func schemaArrayItemTypeSet(node map[string]any) map[string]bool {
	out := map[string]bool{}
	walkAnyOfAllOf(node, func(n map[string]any) {
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
	walkAnyOfAllOf(node, func(n map[string]any) {
		if p, ok := n["pattern"].(string); ok && p == pathPatternValue {
			found = true
		}
	})
	return found
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
// model.md's second invariant literally: {r.Name | r.IsPath} must equal
// (schema's ^\./-pattern field set) ∪ {"workflows", "experimental.themes",
// "experimental.monitors"}.
//
// This is EXPECTED TO FAIL at the time this test was written, for two
// distinct, separately-reported reasons (see this WP's Activity Log /
// hand-off report -- not silently downgraded to a superset check, per this
// WP's explicit instruction to leave a genuine mismatch failing rather than
// weaken the assertion):
//
//  1. The vendored schema's "themes" and "monitors" top-level properties
//     both carry the ^\./ path pattern (confirmed 2026-09-09 by direct
//     inspection), but WP01's rule table does not set IsPath for either --
//     only their experimental.* counterparts do (handled by
//     checkExperimentalPathField, not a rules-table row). Whether that is
//     deliberate (top-level themes/monitors are always redirected to
//     experimental via the Unrecognized "belongs under experimental"
//     warning, so perhaps their Paths check was intentionally skipped) or
//     an oversight is WP01's call, not this WP's -- reported, not fixed
//     here.
//  2. "experimental.themes" and "experimental.monitors" can never appear as
//     a literal r.Name: WP01's rules table has no dotted-name rows for
//     experimental's sub-fields (checkExperimental/checkExperimentalPathField
//     hardcode "themes"/"monitors" handling directly, unconditionally, not
//     gated by any rules-table entry). This half of the invariant is
//     therefore unsatisfiable by {r.Name | r.IsPath} as literally written,
//     independent of any future fix to (1).
func TestSchemaSync_PathRuleNamesMatchSchemaPatternFields(t *testing.T) {
	root := loadSchemaDoc(t)
	props := schemaProperties(t, root)
	schemaPathNames := schemaPathPatternFieldNames(t, props)
	want := map[string]bool{}
	for k := range schemaPathNames {
		want[k] = true
	}
	for _, extra := range []string{"workflows", "experimental.themes", "experimental.monitors"} {
		want[extra] = true
	}
	got := rulePathNames()
	assertFieldSetsEqual(t, "rule table IsPath names vs schema ^\\./-pattern fields ∪ {workflows, experimental.themes, experimental.monitors}", got, want)
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
// ruleKind's OWN semantics commit to (nil when the kind's shape/message
// makes no claim about item type -- kindStringArrayOrObject's message is
// "must be a string, array, or object", naming no item shape, and
// kindDependencyList's array-element shape is validated by dedicated code
// (checkDependencies), not by checkKind at all). Only kinds that DO commit
// to a single item type are checked against the schema's actual item-type
// set below -- this is exactly the class of bug that let channels/monitors
// through before (a Kind claiming "array of string" against a schema
// property whose array items are objects).
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
