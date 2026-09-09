// Pure validation core for `apm-go plugin validate` (feat/plugin-validate,
// mission plugin-manifest-validate-01M21E5Q, WP01). Validate takes already
// -read manifest bytes and returns a Report; it performs no filesystem I/O
// (locating the manifest file, enforcing the 5 MiB cap via Stat, and
// rendering the Report are cmd/apm-go's job, per data-model.md and
// ARCHITECTURE.md's "terminal output belongs to the command layer" rule).
//
// Known fields and their Source come from research.md R-03: schema =
// upstream's vendored schemastore claude-code-plugin.json properties; docs =
// Claude Code plugins-reference fields the schema omits; apm-go = this
// project's own scaffold-only addition (`extensions`, written by
// ScaffoldAgent in pluginjson.go), which must never be flagged Unrecognized.
package pluginjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Check identifies one of the five validation categories, in the fixed
// output order data-model.md specifies (Findings are ordered by Check).
type Check int

const (
	Structure Check = iota
	Name
	Fields
	Paths
	Unrecognized
)

// String renders the category name cmd/apm-go's renderer prints verbatim.
func (c Check) String() string {
	switch c {
	case Structure:
		return "Structure"
	case Name:
		return "Name"
	case Fields:
		return "Fields"
	case Paths:
		return "Paths"
	case Unrecognized:
		return "Unrecognized"
	default:
		return "Unknown"
	}
}

// Level is a Finding's severity.
type Level int

const (
	LevelError Level = iota
	LevelWarning
)

// Finding is one validation result: a Check category, its severity, and the
// exact message text cmd/apm-go renders unchanged (contracts/cli-plugin-
// validate.md's Messages table).
type Finding struct {
	Check   Check
	Level   Level
	Message string
}

// Report is Validate's complete output. Findings is ordered by Check, and
// within a Check, errors precede warnings with each group in manifest file
// order (data-model.md).
type Report struct {
	Findings           []Finding
	KnownFieldsPresent []string
	StructureFailed    bool
}

// ruleKind is the JSON shape a rule's value must have.
type ruleKind int

const (
	kindString ruleKind = iota
	kindObject
	kindBool
	kindArrayOfString
	kindStringOrArray
	kindStringArrayOrObject
	kindDependencyList
	kindArrayOfObject
	kindStringOrArrayOfObject
)

// ruleSource records where a known field comes from (research.md R-03),
// consumed by WP02's schema-sync test to check the rule table against the
// vendored schema mechanically.
type ruleSource int

const (
	sourceSchema ruleSource = iota
	sourceDocs
	sourceApmGo
)

// rule is one row of the static, hand-written rule table (research.md
// R-04): a known top-level field name, its expected JSON shape, the
// severity when the shape doesn't match, whether its value is path-typed
// (subject to the Paths check), and its Source.
type rule struct {
	Name     string
	Kind     ruleKind
	Mismatch Level
	IsPath   bool
	Source   ruleSource
}

// rules is the full known-field set from research.md R-03 / the WP01 task
// prompt's field list: schema properties, then docs-only additions, then
// apm-go's own scaffold field. Only metadata and experimental mismatch at
// LevelWarning (FR-005); every other rule mismatches at LevelError.
// experimental.themes/experimental.monitors are handled inline by the
// Fields/Paths/Unrecognized logic, not as rows here (they nest one level
// down under a rule -- "experimental" -- that is itself a row).
var rules = []rule{
	{Name: "$schema", Kind: kindString, Mismatch: LevelError, Source: sourceSchema},
	{Name: "agents", Kind: kindStringOrArray, Mismatch: LevelError, IsPath: true, Source: sourceSchema},
	{Name: "author", Kind: kindObject, Mismatch: LevelError, Source: sourceSchema},
	// Elements are objects (each carries "server"), never bare strings --
	// upstream b75a02b1's vendored tests/fixtures/schemas/claude-code-
	// plugin.schema.json: "channels": {"type":"array","items":{"type":
	// "object",...,"required":["server"]}}, no string alternative.
	{Name: "channels", Kind: kindArrayOfObject, Mismatch: LevelError, Source: sourceSchema},
	// anyOf a "./"-prefixed path string, an array of such paths, or an
	// object mapping command names to metadata (source/content/etc) --
	// upstream b75a02b1's vendored tests/fixtures/schemas/claude-code-
	// plugin.schema.json's third "commands" anyOf branch. kindStringOrArray
	// wrongly rejected the object form as a Fields error.
	{Name: "commands", Kind: kindStringArrayOrObject, Mismatch: LevelError, IsPath: true, Source: sourceSchema},
	{Name: "dependencies", Kind: kindDependencyList, Mismatch: LevelError, Source: sourceSchema},
	{Name: "description", Kind: kindString, Mismatch: LevelError, Source: sourceSchema},
	{Name: "homepage", Kind: kindString, Mismatch: LevelError, Source: sourceSchema},
	{Name: "hooks", Kind: kindStringArrayOrObject, Mismatch: LevelError, IsPath: true, Source: sourceSchema},
	{Name: "keywords", Kind: kindArrayOfString, Mismatch: LevelError, Source: sourceSchema},
	{Name: "license", Kind: kindString, Mismatch: LevelError, Source: sourceSchema},
	{Name: "lspServers", Kind: kindStringArrayOrObject, Mismatch: LevelError, IsPath: true, Source: sourceSchema},
	{Name: "mcpServers", Kind: kindStringArrayOrObject, Mismatch: LevelError, IsPath: true, Source: sourceSchema},
	// anyOf a "./"-prefixed .json path string or an array of objects (each
	// requiring name/command/description) -- upstream b75a02b1's vendored
	// tests/fixtures/schemas/claude-code-plugin.schema.json, "monitors";
	// never a bare string array like themes/commands/skills. Element field
	// requirements are out of scope for FR-005 (shape only).
	{Name: "monitors", Kind: kindStringOrArrayOfObject, Mismatch: LevelError, IsPath: true, Source: sourceSchema},
	{Name: "name", Kind: kindString, Mismatch: LevelError, Source: sourceSchema},
	{Name: "outputStyles", Kind: kindStringOrArray, Mismatch: LevelError, IsPath: true, Source: sourceSchema},
	{Name: "repository", Kind: kindString, Mismatch: LevelError, Source: sourceSchema},
	{Name: "settings", Kind: kindObject, Mismatch: LevelError, Source: sourceSchema},
	{Name: "skills", Kind: kindStringOrArray, Mismatch: LevelError, IsPath: true, Source: sourceSchema},
	{Name: "themes", Kind: kindStringOrArray, Mismatch: LevelError, IsPath: true, Source: sourceSchema},
	{Name: "userConfig", Kind: kindObject, Mismatch: LevelError, Source: sourceSchema},
	{Name: "version", Kind: kindString, Mismatch: LevelError, Source: sourceSchema},
	{Name: "displayName", Kind: kindString, Mismatch: LevelError, Source: sourceDocs},
	{Name: "defaultEnabled", Kind: kindBool, Mismatch: LevelError, Source: sourceDocs},
	{Name: "metadata", Kind: kindObject, Mismatch: LevelWarning, Source: sourceDocs},
	{Name: "experimental", Kind: kindObject, Mismatch: LevelWarning, Source: sourceDocs},
	{Name: "workflows", Kind: kindStringOrArray, Mismatch: LevelError, IsPath: true, Source: sourceDocs},
	{Name: "extensions", Kind: kindObject, Mismatch: LevelError, Source: sourceApmGo},
}

var ruleIndex = buildRuleIndex(rules)

func buildRuleIndex(rs []rule) map[string]rule {
	idx := make(map[string]rule, len(rs))
	for _, r := range rs {
		idx[r.Name] = r
	}
	return idx
}

var topLevelNames = collectNames(rules)

func collectNames(rs []rule) []string {
	names := make([]string, len(rs))
	for i, r := range rs {
		names[i] = r.Name
	}
	return names
}

const maxManifestBytes = 5 * 1024 * 1024

// Validate checks manifest bytes against all five categories and returns a
// Report. It performs no filesystem access, imports stdlib only (C-002),
// and never panics on malformed input (NFR-002).
//
// Structure (T002) runs first and, on any error, returns immediately with
// only that finding (data-model.md StructureFailed). The remaining four
// checks (T003-T006) then run as a single pass over the manifest's
// top-level keys in file order, since several of them (Fields' errors-
// before-warnings grouping, Paths' array indices, Unrecognized's
// experimental.* nesting) all need that same file-order key list.
func Validate(data []byte) Report {
	if n := len(data); n > maxManifestBytes {
		return structureFailure(fmt.Sprintf("file exceeds 5 MiB cap (%d bytes)", n))
	}
	if !utf8.Valid(data) {
		return structureFailure("invalid UTF-8")
	}

	var probe json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return structureFailure("invalid JSON: " + err.Error())
	}
	if !isJSONObject(probe) {
		return structureFailure("top-level value must be an object")
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return structureFailure("invalid JSON: " + err.Error())
	}

	rawOrder, counts := scanObjectKeys(data)
	dedupedKeys := dedupeKeepFirst(rawOrder)

	var findings []Finding
	seenDup := make(map[string]bool, len(counts))
	for _, k := range rawOrder {
		if counts[k] >= 2 && !seenDup[k] {
			seenDup[k] = true
			findings = append(findings, Finding{Structure, LevelWarning, fmt.Sprintf("duplicate key '%s' (last value wins)", k)})
		}
	}

	findings = append(findings, checkName(m)...)

	var fieldErrs, fieldWarns, pathErrs, unrec []Finding
	for _, key := range dedupedKeys {
		if key == "name" {
			continue
		}
		r, isKnown := ruleIndex[key]
		if !isKnown {
			unrec = append(unrec, unrecognizedFinding(key))
			continue
		}
		raw := m[key]

		switch key {
		case "metadata":
			if !isJSONObject(raw) {
				fieldWarns = append(fieldWarns, Finding{Fields, LevelWarning, "'metadata' should be an object; Claude Code ignores other values"})
			}
		case "experimental":
			if !isJSONObject(raw) {
				fieldWarns = append(fieldWarns, Finding{Fields, LevelWarning, "'experimental' should be an object; Claude Code ignores other values"})
			} else {
				ee, ep, eu := checkExperimental(raw)
				fieldErrs = append(fieldErrs, ee...)
				pathErrs = append(pathErrs, ep...)
				unrec = append(unrec, eu...)
			}
		case "author":
			if !checkKind(raw, kindObject) {
				fieldErrs = append(fieldErrs, Finding{Fields, LevelError, kindMismatchMessage("author", kindObject)})
			} else {
				fieldErrs = append(fieldErrs, checkAuthor(raw)...)
			}
		case "dependencies":
			fieldErrs = append(fieldErrs, checkDependencies(raw)...)
		default:
			if !checkKind(raw, r.Kind) {
				fnd := Finding{Fields, r.Mismatch, kindMismatchMessage(key, r.Kind)}
				if r.Mismatch == LevelError {
					fieldErrs = append(fieldErrs, fnd)
				} else {
					fieldWarns = append(fieldWarns, fnd)
				}
			} else if r.IsPath {
				pathErrs = append(pathErrs, checkPathField(key, raw)...)
			}
		}

		if key == "themes" || key == "monitors" {
			unrec = append(unrec, Finding{Unrecognized, LevelWarning, fmt.Sprintf("'%s' belongs under 'experimental'", key)})
		}
	}

	findings = append(findings, fieldErrs...)
	findings = append(findings, fieldWarns...)
	findings = append(findings, pathErrs...)
	findings = append(findings, unrec...)

	return Report{Findings: findings, KnownFieldsPresent: knownFieldsPresent(dedupedKeys)}
}

func structureFailure(message string) Report {
	return Report{
		Findings:        []Finding{{Structure, LevelError, message}},
		StructureFailed: true,
	}
}

// checkName implements the Name check (T003): missing/type/empty/space/
// control/bidi are mutually-exclusive errors checked in this fixed order,
// stopping at the first that fires; kebab-case is a warning checked only
// when none of those errors fired.
func checkName(m map[string]json.RawMessage) []Finding {
	raw, present := m["name"]
	if !present {
		return []Finding{{Name, LevelError, "missing required field 'name'"}}
	}
	s, ok := decodeString(raw)
	if !ok {
		return []Finding{{Name, LevelError, "'name' must be a string"}}
	}
	if s == "" {
		return []Finding{{Name, LevelError, "'name' must not be empty"}}
	}
	for _, r := range s {
		if unicode.IsSpace(r) {
			return []Finding{{Name, LevelError, "'name' must not contain spaces"}}
		}
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return []Finding{{Name, LevelError, "'name' must not contain control characters"}}
		}
	}
	for _, r := range s {
		if bidiFormattingChars[r] {
			return []Finding{{Name, LevelError, "'name' must not contain bidirectional formatting characters"}}
		}
	}
	if !kebabCaseRe.MatchString(s) {
		return []Finding{{Name, LevelWarning, "'name' is not kebab-case"}}
	}
	return nil
}

// checkAuthor implements T004 step 3: only sub-fields that are present and
// not strings are reported, walked in the sub-object's own file order
// (data-model.md: same-level findings keep file order) rather than a fixed
// name/email/url sequence; author has no required sub-fields here.
func checkAuthor(raw json.RawMessage) []Finding {
	subOrder, _ := scanObjectKeys(raw)
	subOrder = dedupeKeepFirst(subOrder)
	var sub map[string]json.RawMessage
	if err := json.Unmarshal(raw, &sub); err != nil {
		return nil
	}
	var findings []Finding
	for _, k := range subOrder {
		if k != "name" && k != "email" && k != "url" {
			continue
		}
		if v, present := sub[k]; present && !isJSONString(v) {
			findings = append(findings, Finding{Fields, LevelError, fmt.Sprintf("'author.%s' must be a string", k)})
		}
	}
	return findings
}

// checkDependencies implements T004 step 4. The whole-field-not-an-array
// shape has no scenario in the contract's Messages table; "must be an
// array" follows the table's own naming convention for an unlisted case.
//
// A bare string element is accepted with no further checks: the contract's
// own message for the reject case ("must be a string or object") asserts
// that a string is a legal shape, and Claude Code's own dependency docs
// describe a plain-string dependency form -- rejecting every non-object
// element here (as the previous code did) contradicted that message's own
// wording. What (if anything) should additionally be validated on a bare
// string entry is not specified by contracts/cli-plugin-validate.md or
// data-model.md; this is left unchecked pending that ruling.
func checkDependencies(raw json.RawMessage) []Finding {
	if !isJSONArray(raw) {
		return []Finding{{Fields, LevelError, "'dependencies' must be an array"}}
	}
	var elems []json.RawMessage
	if err := json.Unmarshal(raw, &elems); err != nil {
		return nil
	}
	var findings []Finding
	for i, elem := range elems {
		if isJSONString(elem) {
			continue
		}
		if !isJSONObject(elem) {
			findings = append(findings, Finding{Fields, LevelError, fmt.Sprintf("'dependencies[%d]' must be a string or object", i)})
			continue
		}
		findings = append(findings, checkDependencyElement(i, elem)...)
	}
	return findings
}

// checkDependencyElement walks one dependency object's own keys in file
// order (data-model.md: same-level findings keep file order), the same
// rule checkAuthor applies to author.<k>, so a mistyped "version" preceding
// a mistyped "name" in the manifest reports in that order. A present but
// wrong-typed "name" is a type mismatch ("must be a string"), distinct from
// an absent "name" key ("is missing 'name'") -- presence alone must not
// mask a wrong type, and a wrong type must not be misreported as absence.
func checkDependencyElement(i int, elem json.RawMessage) []Finding {
	subOrder, _ := scanObjectKeys(elem)
	subOrder = dedupeKeepFirst(subOrder)
	var sub map[string]json.RawMessage
	if err := json.Unmarshal(elem, &sub); err != nil {
		return nil
	}
	var findings []Finding
	hasName := false
	for _, k := range subOrder {
		switch k {
		case "name":
			hasName = true
			if !isJSONString(sub[k]) {
				findings = append(findings, Finding{Fields, LevelError, fmt.Sprintf("'dependencies[%d].name' must be a string", i)})
			}
		case "version":
			if !isJSONString(sub[k]) {
				findings = append(findings, Finding{Fields, LevelError, fmt.Sprintf("'dependencies[%d].version' must be a string", i)})
			}
		}
	}
	if !hasName {
		findings = append(findings, Finding{Fields, LevelError, fmt.Sprintf("'dependencies[%d]' is missing 'name'", i)})
	}
	return findings
}

// checkExperimental implements T004 step 5 and the experimental.* half of
// T006: themes/monitors get the same Fields+Paths treatment as any other
// IsPath rule (but at Fields error level even though the parent field is
// warning-tier); every other sub-key is Unrecognized's concern.
//
// experimental.monitors shares kindStringOrArrayOfObject with top-level
// "monitors" (same anyOf in upstream's vendored schema, and the Claude Code
// plugins-reference Monitors section documents the same inline object-array
// shape at both nesting levels) -- unlike experimental.themes, which has no
// such object-array form and stays on kindStringOrArray.
func checkExperimental(raw json.RawMessage) (fieldErrs, pathErrs, unrec []Finding) {
	subOrder, subCounts := scanObjectKeys(raw)
	subOrder = dedupeKeepFirst(subOrder)
	_ = subCounts
	var sub map[string]json.RawMessage
	if err := json.Unmarshal(raw, &sub); err != nil {
		return nil, nil, nil
	}
	for _, sk := range subOrder {
		switch sk {
		case "themes":
			fieldErrs, pathErrs = checkExperimentalPathField(sk, sub[sk], kindStringOrArray, fieldErrs, pathErrs)
		case "monitors":
			fieldErrs, pathErrs = checkExperimentalPathField(sk, sub[sk], kindStringOrArrayOfObject, fieldErrs, pathErrs)
		default:
			unrec = append(unrec, unrecognizedExperimentalFinding(sk))
		}
	}
	return fieldErrs, pathErrs, unrec
}

// checkExperimentalPathField applies kind's Fields check to one
// experimental.<subKey> value and, only when it matches, the Paths check
// (which is itself a no-op for the object-array form -- pathValues rejects
// anything that isn't a string or array of strings, per checkPathField).
func checkExperimentalPathField(subKey string, sraw json.RawMessage, kind ruleKind, fieldErrs, pathErrs []Finding) ([]Finding, []Finding) {
	field := "experimental." + subKey
	if !checkKind(sraw, kind) {
		return append(fieldErrs, Finding{Fields, LevelError, kindMismatchMessage(field, kind)}), pathErrs
	}
	return fieldErrs, append(pathErrs, checkPathField(field, sraw)...)
}

// checkPathField runs the Paths syntax check (T005) on every value of a
// path-typed field already confirmed (by the caller) to be a JSON string or
// array of strings; an object-form value (hooks/mcpServers/lspServers) is
// legal and produces no findings here.
func checkPathField(field string, raw json.RawMessage) []Finding {
	values, isArray, ok := pathValues(raw)
	if !ok {
		return nil
	}
	var findings []Finding
	for i, v := range values {
		name := field
		if isArray {
			name = fmt.Sprintf("%s[%d]", field, i)
		}
		if msg := pathSyntaxError(name, v); msg != "" {
			findings = append(findings, Finding{Paths, LevelError, msg})
		}
	}
	return findings
}

func unrecognizedFinding(key string) Finding {
	msg := fmt.Sprintf("unrecognized field '%s'", key)
	if s := suggestFor(key, topLevelNames); s != "" {
		msg += fmt.Sprintf(" (did you mean '%s'?)", s)
	}
	return Finding{Unrecognized, LevelWarning, msg}
}

var experimentalKnownSubKeys = []string{"themes", "monitors"}

func unrecognizedExperimentalFinding(subKey string) Finding {
	msg := fmt.Sprintf("unrecognized field 'experimental.%s'", subKey)
	if s := suggestFor(subKey, experimentalKnownSubKeys); s != "" {
		msg += fmt.Sprintf(" (did you mean '%s'?)", s)
	}
	return Finding{Unrecognized, LevelWarning, msg}
}

// -- low-level JSON-shape and path-syntax helpers --

var winAbsPathRe = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
var kebabCaseRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

var bidiFormattingChars = map[rune]bool{
	0x200E: true, 0x200F: true,
	0x202A: true, 0x202B: true, 0x202C: true, 0x202D: true, 0x202E: true,
	0x2066: true, 0x2067: true, 0x2068: true, 0x2069: true,
}

func rawKind(raw json.RawMessage) byte {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return 0
	}
	return t[0]
}

func isJSONString(raw json.RawMessage) bool { return rawKind(raw) == '"' }
func isJSONObject(raw json.RawMessage) bool { return rawKind(raw) == '{' }
func isJSONArray(raw json.RawMessage) bool  { return rawKind(raw) == '[' }
func isJSONBool(raw json.RawMessage) bool {
	k := rawKind(raw)
	return k == 't' || k == 'f'
}

func decodeString(raw json.RawMessage) (string, bool) {
	if !isJSONString(raw) {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func decodeArrayOfStrings(raw json.RawMessage) ([]string, bool) {
	if !isJSONArray(raw) {
		return nil, false
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		s, ok := decodeString(e)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// checkKind reports whether raw matches kind's JSON shape. kindDependencyList
// is only reached defensively (Validate special-cases "dependencies" before
// ever calling checkKind on it) but is implemented for completeness.
func checkKind(raw json.RawMessage, kind ruleKind) bool {
	switch kind {
	case kindString:
		return isJSONString(raw)
	case kindObject:
		return isJSONObject(raw)
	case kindBool:
		return isJSONBool(raw)
	case kindArrayOfString:
		_, ok := decodeArrayOfStrings(raw)
		return ok
	case kindStringOrArray:
		if isJSONString(raw) {
			return true
		}
		_, ok := decodeArrayOfStrings(raw)
		return ok
	case kindStringArrayOrObject:
		if isJSONString(raw) || isJSONObject(raw) {
			return true
		}
		_, ok := decodeArrayOfStrings(raw)
		return ok
	case kindDependencyList:
		return isJSONArray(raw)
	case kindArrayOfObject:
		return isArrayOfObjects(raw)
	case kindStringOrArrayOfObject:
		return isJSONString(raw) || isArrayOfObjects(raw)
	default:
		return false
	}
}

// isArrayOfObjects reports whether raw is a JSON array whose every element
// is a JSON object (kindArrayOfObject, e.g. "channels").
func isArrayOfObjects(raw json.RawMessage) bool {
	if !isJSONArray(raw) {
		return false
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return false
	}
	for _, e := range arr {
		if !isJSONObject(e) {
			return false
		}
	}
	return true
}

func kindMismatchMessage(field string, kind ruleKind) string {
	switch kind {
	case kindString:
		return fmt.Sprintf("'%s' must be a string", field)
	case kindObject:
		return fmt.Sprintf("'%s' must be an object", field)
	case kindBool:
		return fmt.Sprintf("'%s' must be a boolean", field)
	case kindArrayOfString:
		return fmt.Sprintf("'%s' must be an array of strings", field)
	case kindStringOrArray:
		return fmt.Sprintf("'%s' must be a string or array", field)
	case kindStringArrayOrObject:
		return fmt.Sprintf("'%s' must be a string, array, or object", field)
	case kindDependencyList:
		return fmt.Sprintf("'%s' must be an array", field)
	case kindArrayOfObject:
		return fmt.Sprintf("'%s' must be an array of objects", field)
	case kindStringOrArrayOfObject:
		return fmt.Sprintf("'%s' must be a string or array of objects", field)
	default:
		return fmt.Sprintf("'%s' has an unrecognized type", field)
	}
}

// pathValues extracts the string(s) to run the Paths syntax check against.
// ok is false when raw is neither a JSON string nor an array of strings
// (an object-form hooks/mcpServers/lspServers value, or a value T004 has
// already reported a Fields mismatch for) -- callers must not add a Paths
// finding in that case.
func pathValues(raw json.RawMessage) (values []string, isArray bool, ok bool) {
	if s, sok := decodeString(raw); sok {
		return []string{s}, false, true
	}
	if arr, aok := decodeArrayOfStrings(raw); aok {
		return arr, true, true
	}
	return nil, false, false
}

// isAbsolutePathValue reports whether v is an absolute path by POSIX (/) or
// Windows (drive letter or UNC) convention -- these are manifest string
// values, not OS paths, so both forms are checked regardless of host OS.
func isAbsolutePathValue(v string) bool {
	return strings.HasPrefix(v, "/") || winAbsPathRe.MatchString(v)
}

func hasDotDotSegment(v string) bool {
	for _, seg := range strings.FieldsFunc(v, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return true
		}
	}
	return false
}

// pathSyntaxError returns the Paths message for v under field's name, or ""
// if v is syntactically valid. Absolute is checked before the './' prefix:
// almost every absolute value (POSIX or Windows) also fails the prefix
// check, so checking prefix first would make the dedicated "must not be an
// absolute path" message (contracts/cli-plugin-validate.md) unreachable.
func pathSyntaxError(field, v string) string {
	if isAbsolutePathValue(v) {
		return fmt.Sprintf("'%s' must not be an absolute path", field)
	}
	if !strings.HasPrefix(v, "./") {
		return fmt.Sprintf("'%s' must start with './'", field)
	}
	if hasDotDotSegment(v) {
		return fmt.Sprintf("'%s' must not contain '..'", field)
	}
	return ""
}

// scanObjectKeys walks data's outermost JSON object (data must already be
// known-valid JSON) and returns its top-level keys in file order, with
// repeats and a per-key occurrence count for duplicate-key detection. Each
// value is skipped as json.RawMessage rather than tokenized, so a number
// encoding/json's decoder cannot parse as float64 (e.g. 1e1000) never
// aborts the scan before later sibling keys are read.
func scanObjectKeys(data []byte) (order []string, counts map[string]int) {
	counts = map[string]int{}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return order, counts
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return order, counts
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return order, counts
		}
		key, ok := keyTok.(string)
		if !ok {
			return order, counts
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return order, counts
		}
		order = append(order, key)
		counts[key]++
	}
	return order, counts
}

func dedupeKeepFirst(keys []string) []string {
	seen := make(map[string]bool, len(keys))
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

func knownFieldsPresent(dedupedKeys []string) []string {
	var out []string
	for _, k := range dedupedKeys {
		if _, ok := ruleIndex[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

// damerauLevenshtein returns the optimal-string-alignment edit distance
// (insertion, deletion, substitution, adjacent transposition) between a and
// b over runes, used by the Unrecognized check's "did you mean" suggestion
// (research.md/T006: plain Levenshtein can overcount an adjacent-swap typo
// by one, which matters right at the <=2 suggestion threshold).
func damerauLevenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	d := make([][]int, la+1)
	for i := range d {
		d[i] = make([]int, lb+1)
		d[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		d[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			min := d[i-1][j] + 1
			if v := d[i][j-1] + 1; v < min {
				min = v
			}
			if v := d[i-1][j-1] + cost; v < min {
				min = v
			}
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				if v := d[i-2][j-2] + 1; v < min {
					min = v
				}
			}
			d[i][j] = min
		}
	}
	return d[la][lb]
}

// suggestFor returns the candidate closest to key by damerauLevenshtein when
// that distance is <= 2, breaking ties alphabetically; "" when none qualify.
func suggestFor(key string, candidates []string) string {
	best := ""
	bestDist := -1
	for _, c := range candidates {
		dist := damerauLevenshtein(key, c)
		if bestDist == -1 || dist < bestDist || (dist == bestDist && c < best) {
			bestDist = dist
			best = c
		}
	}
	if bestDist >= 0 && bestDist <= 2 {
		return best
	}
	return ""
}
