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

// Validate runs structural checks on a marketplace manifest -- this is
// mkt-016's dependency (`marketplace validate`, wired in a later step, owns
// turning these findings into the "N passed, N warnings, N errors" summary
// and exit code). It mirrors the Python original's
// marketplace.validator.validate_marketplace: validate_plugin_schema
// (every plugin has a non-empty name and a "source") and
// validate_no_duplicate_names (no two plugin names collide
// case-insensitively), flattened into a single ordered []Finding slice
// instead of Python's list of per-check ValidationResult objects. The
// manifest-level name check has no Python equivalent -- the original's JSON
// parser always defaults a missing name to the source's repo name (or
// "unknown"), so validate_marketplace never sees an empty manifest.Name;
// apm-go's json.Unmarshal-based parsing does not backfill a default, so an
// empty name is a real state this function must catch.
func Validate(m *MarketplaceManifest) []Finding {
	if m == nil {
		return []Finding{{Level: LevelError, Message: "marketplace manifest is nil"}}
	}

	var findings []Finding

	if strings.TrimSpace(m.Name) == "" {
		findings = append(findings, Finding{Level: LevelError, Message: "marketplace manifest name is empty"})
	}

	for _, p := range m.Plugins {
		if strings.TrimSpace(p.Name) == "" {
			findings = append(findings, Finding{Level: LevelError, Message: "plugin entry has empty name"})
		}
		if p.Source == nil {
			findings = append(findings, Finding{
				Level:   LevelError,
				Message: fmt.Sprintf("plugin %q is missing required field 'source'", p.Name),
			})
		}
	}

	// Mirrors validate_no_duplicate_names verbatim: it does not special-case
	// empty names, so two plugins that both failed the empty-name check
	// above will also collide with each other here.
	seen := make(map[string]string, len(m.Plugins))
	for _, p := range m.Plugins {
		lower := strings.ToLower(strings.TrimSpace(p.Name))
		if original, ok := seen[lower]; ok {
			findings = append(findings, Finding{
				Level:   LevelError,
				Message: fmt.Sprintf("duplicate plugin name: %q (conflicts with %q)", p.Name, original),
			})
			continue
		}
		seen[lower] = p.Name
	}

	return findings
}
