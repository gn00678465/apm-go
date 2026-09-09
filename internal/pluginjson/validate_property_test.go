// Randomized zero-false-positive witness for NFR-004 ("known fields, correct
// types -> zero findings") and SC-003, complementing WP01's fixed table-
// driven scenarios in validate_test.go. Generates >=100 manifests built only
// from known field names (Source in {schema, docs, apm-go}) with values of
// the CORRECT ruleKind for each included field, read directly from the
// `rules` table at test time (Risk mitigation: never a second hand-
// maintained field list that can drift from WP01's actual table).
package pluginjson

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// propertyTestExcludedFields are known Source==schema field names this
// generator deliberately never emits, because their mere PRESENCE at the
// top level -- regardless of how correctly they are typed -- always
// produces a warning finding by design (spec.md Edge Cases: 頂層 themes/
// monitors 建議搬移至 experimental 之下; pinned by validate_test.go's own
// EdgeCase-top-level-{themes,monitors}-belongs-under-experimental
// subtests, both of which use a fully legal value and still expect exactly
// one Unrecognized warning). NFR-004's "known fields, correct types -> zero
// findings" claim does not cover these two; excluding them here keeps this
// generator honest rather than manufacturing an expected failure.
var propertyTestExcludedFields = map[string]bool{
	"themes":   true,
	"monitors": true,
}

var propertyTestWords = []string{
	"alpha", "beta", "gamma", "widget", "sample", "demo", "item", "thing", "acme", "proto",
}

func propertyTestWord(rnd *rand.Rand) string {
	return propertyTestWords[rnd.Intn(len(propertyTestWords))]
}

// propertyTestKebabName always produces a string that satisfies checkName's
// full chain (non-empty, no space/control/bidi chars, and kebabCaseRe) so a
// generated manifest never trips the Name check.
func propertyTestKebabName(rnd *rand.Rand) string {
	n := 1 + rnd.Intn(3)
	parts := make([]string, n)
	for i := range parts {
		parts[i] = propertyTestWord(rnd)
	}
	return strings.Join(parts, "-")
}

// propertyTestPathString always satisfies pathSyntaxError's three checks
// (not absolute, starts with "./", no ".." segment) -- validate.go's Paths
// check has no file-extension requirement (confirmed by reading
// pathSyntaxError: only those three checks), so no per-field suffix is
// needed here.
func propertyTestPathString(rnd *rand.Rand) string {
	n := 1 + rnd.Intn(3)
	parts := make([]string, n)
	for i := range parts {
		parts[i] = propertyTestWord(rnd)
	}
	return "./" + strings.Join(parts, "/")
}

func propertyTestPathStringArray(rnd *rand.Rand) []any {
	n := rnd.Intn(3)
	out := make([]any, n)
	for i := range out {
		out[i] = propertyTestPathString(rnd)
	}
	return out
}

func propertyTestStringArray(rnd *rand.Rand) []any {
	n := rnd.Intn(4)
	out := make([]any, n)
	for i := range out {
		out[i] = propertyTestWord(rnd)
	}
	return out
}

// propertyTestObjectArray produces a JSON array of objects -- satisfies
// kindArrayOfObject (channels) and, when reached, kindStringOrArrayOfObject's
// array branch (top-level "monitors" is excluded above, but this stays
// available for any future Source==schema field reusing the Kind).
func propertyTestObjectArray(rnd *rand.Rand) []any {
	n := rnd.Intn(3)
	out := make([]any, n)
	for i := range out {
		out[i] = map[string]any{"server": propertyTestWord(rnd)}
	}
	return out
}

// propertyTestDependencyList satisfies checkDependencies/checkDependencyElement
// directly (Validate special-cases "dependencies", bypassing checkKind
// entirely): every element is either a bare string or an object carrying a
// string "name" and, optionally, a string "version".
func propertyTestDependencyList(rnd *rand.Rand) []any {
	n := rnd.Intn(3)
	out := make([]any, n)
	for i := range out {
		if rnd.Intn(2) == 0 {
			out[i] = propertyTestWord(rnd)
			continue
		}
		dep := map[string]any{"name": propertyTestWord(rnd)}
		if rnd.Intn(2) == 0 {
			dep["version"] = "1.0.0"
		}
		out[i] = dep
	}
	return out
}

// propertyTestLegalValue returns a JSON-marshalable value for rule r that is
// guaranteed, by construction, to pass Validate's Fields check for r.Kind
// and, when r.IsPath, the Paths check too. author/metadata/experimental/
// userConfig/settings/extensions all resolve to kindObject here and get an
// empty object: checkAuthor/checkExperimental only add findings for sub-keys
// that are actually present, and Validate's metadata/experimental
// special-cases only warn when the value is NOT an object -- an empty
// object is always zero-finding for all of these.
func propertyTestLegalValue(rnd *rand.Rand, r rule) any {
	if r.IsPath {
		switch r.Kind {
		case kindString:
			return propertyTestPathString(rnd)
		case kindStringOrArray:
			if rnd.Intn(2) == 0 {
				return propertyTestPathString(rnd)
			}
			return propertyTestPathStringArray(rnd)
		case kindStringArrayOrObject:
			switch rnd.Intn(3) {
			case 0:
				return propertyTestPathString(rnd)
			case 1:
				return propertyTestPathStringArray(rnd)
			default:
				return map[string]any{}
			}
		default:
			panic(fmt.Sprintf("propertyTestLegalValue: unhandled IsPath Kind for %q: %v (Risk mitigation: this generator must be taught every IsPath Kind WP01's rule table actually uses, not silently fall through)", r.Name, r.Kind))
		}
	}

	switch r.Kind {
	case kindString:
		return propertyTestWord(rnd)
	case kindObject:
		return map[string]any{}
	case kindBool:
		return rnd.Intn(2) == 0
	case kindArrayOfString:
		return propertyTestStringArray(rnd)
	case kindDependencyList:
		return propertyTestDependencyList(rnd)
	case kindArrayOfObject:
		return propertyTestObjectArray(rnd)
	case kindStringOrArrayOfObject:
		if rnd.Intn(2) == 0 {
			return propertyTestObjectArray(rnd)
		}
		return propertyTestWord(rnd)
	case kindStringArrayOrObject:
		switch rnd.Intn(3) {
		case 0:
			return propertyTestWord(rnd)
		case 1:
			return propertyTestStringArray(rnd)
		default:
			return map[string]any{}
		}
	default:
		panic(fmt.Sprintf("propertyTestLegalValue: unhandled Kind for %q: %v", r.Name, r.Kind))
	}
}

// propertyTestBuildManifest builds one random manifest object: "name" is
// always present and always legal; every other known field (read from the
// live `rules` table, minus propertyTestExcludedFields) is independently
// included about half the time, with a value from propertyTestLegalValue.
func propertyTestBuildManifest(rnd *rand.Rand) map[string]any {
	m := map[string]any{"name": propertyTestKebabName(rnd)}
	for _, r := range rules {
		if r.Name == "name" || propertyTestExcludedFields[r.Name] {
			continue
		}
		if rnd.Intn(2) == 0 {
			continue
		}
		m[r.Name] = propertyTestLegalValue(rnd, r)
	}
	return m
}

// TestValidate_KnownFieldsCorrectTypes_ZeroFindings is NFR-004/SC-003's
// randomized witness: >=100 independently-seeded manifests, each built only
// from known, correctly-typed fields, must all validate with zero findings.
// Each iteration's RNG is seeded by its own index, so a failure's exact
// manifest is reproducible by re-running that single iteration (not just a
// "test failed" report).
func TestValidate_KnownFieldsCorrectTypes_ZeroFindings(t *testing.T) {
	const iterations = 200
	for i := 0; i < iterations; i++ {
		rnd := rand.New(rand.NewSource(int64(i)))
		manifest := propertyTestBuildManifest(rnd)
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatalf("iteration %d: json.Marshal(%+v): %v", i, manifest, err)
		}
		report := Validate(data)
		if len(report.Findings) != 0 {
			t.Fatalf("iteration %d (seed %d): Validate reported %d unexpected finding(s) for a manifest built only from known, correctly-typed fields (NFR-004)\nmanifest: %s\nfindings: %+v",
				i, i, len(report.Findings), data, report.Findings)
		}
	}
}
