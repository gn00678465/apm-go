package marketplace

import (
	"fmt"
	"strings"
)

// FindingLevel classifies a Validate finding's severity.
type FindingLevel int

const (
	LevelWarning FindingLevel = iota
	LevelError
)

// Finding is a single validation result surfaced by Validate.
type Finding struct {
	Level   FindingLevel
	Message string
}

// ValidationResult is one named check's outcome, mirroring the Python
// original's marketplace.validator.ValidationResult (check_name / passed /
// warnings / errors). `marketplace validate` renders one line per check --
// including passing checks -- and counts its Summary from these, exactly
// like Python's validate.py:54-80.
type ValidationResult struct {
	CheckName string
	Findings  []Finding
}

// Passed reports whether the check produced no findings at all.
func (r ValidationResult) Passed() bool { return len(r.Findings) == 0 }

// ValidateChecks runs the same checks as Validate but grouped per named
// check, mirroring Python's validate_marketplace returning one
// ValidationResult per check: "Schema" (validate_plugin_schema) and "Names"
// (validate_no_duplicate_names). The manifest-level name check has no Python
// equivalent -- the original's JSON parser always defaults a missing name to
// the source's repo name (or "unknown"), so validate_marketplace never sees
// an empty manifest.Name; apm-go's json.Unmarshal-based parsing does not
// backfill a default, so an empty name is a real state, folded into the
// "Schema" check here.
func ValidateChecks(m *MarketplaceManifest) []ValidationResult {
	if m == nil {
		return []ValidationResult{{
			CheckName: "Schema",
			Findings:  []Finding{{Level: LevelError, Message: "marketplace manifest is nil"}},
		}}
	}

	var schema []Finding
	if strings.TrimSpace(m.Name) == "" {
		schema = append(schema, Finding{Level: LevelError, Message: "marketplace manifest name is empty"})
	}
	for _, p := range m.Plugins {
		if strings.TrimSpace(p.Name) == "" {
			schema = append(schema, Finding{Level: LevelError, Message: "plugin entry has empty name"})
		}
		if p.Source == nil {
			schema = append(schema, Finding{
				Level:   LevelError,
				Message: fmt.Sprintf("plugin %q is missing required field 'source'", p.Name),
			})
		}
	}

	// Mirrors validate_no_duplicate_names verbatim: it does not special-case
	// empty names, so two plugins that both failed the empty-name check
	// above will also collide with each other here.
	var names []Finding
	seen := make(map[string]string, len(m.Plugins))
	for _, p := range m.Plugins {
		lower := strings.ToLower(strings.TrimSpace(p.Name))
		if original, ok := seen[lower]; ok {
			names = append(names, Finding{
				Level:   LevelError,
				Message: fmt.Sprintf("duplicate plugin name: %q (conflicts with %q)", p.Name, original),
			})
			continue
		}
		seen[lower] = p.Name
	}

	return []ValidationResult{
		{CheckName: "Schema", Findings: schema},
		{CheckName: "Names", Findings: names},
	}
}

// Validate runs structural checks on a marketplace manifest and returns the
// findings flattened into a single ordered slice (ValidateChecks' per-check
// grouping, in check order). Kept for callers that only need the flat list.
func Validate(m *MarketplaceManifest) []Finding {
	var findings []Finding
	for _, check := range ValidateChecks(m) {
		findings = append(findings, check.Findings...)
	}
	return findings
}
