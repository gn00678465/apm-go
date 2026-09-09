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
	{Name: "channels", Kind: kindArrayOfString, Mismatch: LevelError, Source: sourceSchema},
	{Name: "commands", Kind: kindStringOrArray, Mismatch: LevelError, IsPath: true, Source: sourceSchema},
	{Name: "dependencies", Kind: kindDependencyList, Mismatch: LevelError, Source: sourceSchema},
	{Name: "description", Kind: kindString, Mismatch: LevelError, Source: sourceSchema},
	{Name: "homepage", Kind: kindString, Mismatch: LevelError, Source: sourceSchema},
	{Name: "hooks", Kind: kindStringArrayOrObject, Mismatch: LevelError, IsPath: true, Source: sourceSchema},
	{Name: "keywords", Kind: kindArrayOfString, Mismatch: LevelError, Source: sourceSchema},
	{Name: "license", Kind: kindString, Mismatch: LevelError, Source: sourceSchema},
	{Name: "lspServers", Kind: kindStringArrayOrObject, Mismatch: LevelError, IsPath: true, Source: sourceSchema},
	{Name: "mcpServers", Kind: kindStringArrayOrObject, Mismatch: LevelError, IsPath: true, Source: sourceSchema},
	{Name: "monitors", Kind: kindStringOrArray, Mismatch: LevelError, Source: sourceSchema},
	{Name: "name", Kind: kindString, Mismatch: LevelError, Source: sourceSchema},
	{Name: "outputStyles", Kind: kindStringOrArray, Mismatch: LevelError, IsPath: true, Source: sourceSchema},
	{Name: "repository", Kind: kindString, Mismatch: LevelError, Source: sourceSchema},
	{Name: "settings", Kind: kindObject, Mismatch: LevelError, Source: sourceSchema},
	{Name: "skills", Kind: kindStringOrArray, Mismatch: LevelError, IsPath: true, Source: sourceSchema},
	{Name: "themes", Kind: kindStringOrArray, Mismatch: LevelError, Source: sourceSchema},
	{Name: "userConfig", Kind: kindObject, Mismatch: LevelError, Source: sourceSchema},
	{Name: "version", Kind: kindString, Mismatch: LevelError, Source: sourceSchema},
	{Name: "displayName", Kind: kindString, Mismatch: LevelError, Source: sourceDocs},
	{Name: "defaultEnabled", Kind: kindBool, Mismatch: LevelError, Source: sourceDocs},
	{Name: "metadata", Kind: kindObject, Mismatch: LevelWarning, Source: sourceDocs},
	{Name: "experimental", Kind: kindObject, Mismatch: LevelWarning, Source: sourceDocs},
	{Name: "workflows", Kind: kindStringOrArray, Mismatch: LevelError, IsPath: true, Source: sourceDocs},
	{Name: "extensions", Kind: kindObject, Mismatch: LevelError, Source: sourceApmGo},
}

// Validate checks manifest bytes against all five categories and returns a
// Report. It performs no filesystem access, imports stdlib only (C-002),
// and never panics on malformed input (NFR-002).
//
// STUB (T001): fleshed out by T002-T006. Returning a zero Report here is
// this WP's committed RED state -- validate_test.go's table expects real
// findings, so every non-trivial subtest fails until the check bodies land.
func Validate(data []byte) Report {
	return Report{}
}
