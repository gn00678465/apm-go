package build

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/apm-go/apm/internal/marketplace/authoring"
)

// maxDiffsRendered mirrors _MAX_DIFFS_RENDERED (drift_check.py:35).
const maxDiffsRendered = 20

// DriftDifference mirrors DriftDifference (drift_check.py:38-47): one
// leaf-key difference between the on-disk marketplace output and the
// freshly-recomposed document.
type DriftDifference struct {
	Path string
	Old  any
	New  any
}

// DriftOutputReport mirrors DriftOutputReport (drift_check.py:50-65):
// drift status for a single configured marketplace output profile.
type DriftOutputReport struct {
	Format      string
	Path        string
	Status      string // "unchanged" | "missing" | "drift"
	Differences []DriftDifference
}

// DriftReport mirrors DriftReport (drift_check.py:68-89): the aggregate
// drift report across every configured output.
type DriftReport struct {
	OK      bool
	Outputs []DriftOutputReport
}

// ErrorMessages mirrors DriftReport.error_messages (drift_check.py:81-89).
func (r DriftReport) ErrorMessages() []string {
	var msgs []string
	for _, out := range r.Outputs {
		switch out.Status {
		case "missing":
			msgs = append(msgs, fmt.Sprintf("%s: missing on disk (would be created)", out.Path))
		case "drift":
			msgs = append(msgs, fmt.Sprintf("%s: %d differences vs. regenerated output", out.Path, len(out.Differences)))
		}
	}
	return msgs
}

func formatPathSegment(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

// jsonKeyDiff mirrors json_key_diff (drift_check.py:102-138): recursively
// diffs two decoded-JSON values (map[string]any / []any / scalar), emitting
// one DriftDifference per leaf. A key/index present only in newVal has
// Old=nil; present only in oldVal has New=nil -- neither side is recursed
// into further in that case, matching the Oracle's own "record the WHOLE
// added/removed subtree as one diff" behavior.
func jsonKeyDiff(oldVal, newVal any, prefix string) []DriftDifference {
	var diffs []DriftDifference

	if oldMap, ok := oldVal.(map[string]any); ok {
		if newMap, ok := newVal.(map[string]any); ok {
			keySet := make(map[string]bool, len(oldMap)+len(newMap))
			for k := range oldMap {
				keySet[k] = true
			}
			for k := range newMap {
				keySet[k] = true
			}
			keys := make([]string, 0, len(keySet))
			for k := range keySet {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, key := range keys {
				childPrefix := formatPathSegment(prefix, key)
				ov, oOK := oldMap[key]
				nv, nOK := newMap[key]
				switch {
				case !oOK:
					diffs = append(diffs, DriftDifference{Path: childPrefix, Old: nil, New: nv})
				case !nOK:
					diffs = append(diffs, DriftDifference{Path: childPrefix, Old: ov, New: nil})
				default:
					diffs = append(diffs, jsonKeyDiff(ov, nv, childPrefix)...)
				}
			}
			return diffs
		}
	}

	if oldList, ok := oldVal.([]any); ok {
		if newList, ok := newVal.([]any); ok {
			maxLen := len(oldList)
			if len(newList) > maxLen {
				maxLen = len(newList)
			}
			for i := 0; i < maxLen; i++ {
				childPrefix := fmt.Sprintf("%s[%d]", prefix, i)
				switch {
				case i >= len(oldList):
					diffs = append(diffs, DriftDifference{Path: childPrefix, Old: nil, New: newList[i]})
				case i >= len(newList):
					diffs = append(diffs, DriftDifference{Path: childPrefix, Old: oldList[i], New: nil})
				default:
					diffs = append(diffs, jsonKeyDiff(oldList[i], newList[i], childPrefix)...)
				}
			}
			return diffs
		}
	}

	if oldVal != newVal {
		diffs = append(diffs, DriftDifference{Path: prefix, Old: oldVal, New: newVal})
	}
	return diffs
}

// loadOnDisk mirrors _load_on_disk (drift_check.py:141-148): (nil, false)
// when path does not exist; (nil, true) when it exists but fails to parse
// as JSON; (decoded, true) otherwise.
func loadOnDisk(path string) (any, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, true
	}
	return v, true
}

// CheckMarketplaceDrift mirrors check_marketplace_drift (drift_check.py:
// 151-238): for each of cfg's configured output profiles, composes the
// document via the SAME ComposeDocument dispatch the real marketplace
// producer uses (never writing anything -- resolved is the caller's own,
// independently-resolved package list, matching the Oracle's own
// dedicated, always-dry-run MarketplaceBuilder for this gate rather than
// reusing whatever the real producer already wrote this same invocation),
// and compares it against whatever is currently on disk at the profile's
// resolved output path.
func CheckMarketplaceDrift(
	cfg *authoring.AuthoringConfig,
	resolved []ResolvedPackage,
	projectRoot string,
	configPaths, outputOverrides map[string]string,
	options ...ComposeOptions,
) (DriftReport, error) {
	configured := cfg.Outputs
	if len(configured) == 0 {
		configured = []string{"claude"}
	}

	var reports []DriftOutputReport
	for _, format := range configured {
		if !KnownOutputFormats[format] {
			continue
		}

		outputPath, err := ResolveOutputPath(format, configPaths, outputOverrides)
		if err != nil {
			return DriftReport{}, err
		}
		absPath, err := EnsureWithinRoot(projectRoot, outputPath)
		if err != nil {
			return DriftReport{}, err
		}

		doc, _, err := ComposeDocument(format, cfg, resolved, options...)
		if err != nil {
			return DriftReport{}, err
		}
		// Round-trip through encoding/json the same way WriteOutput's own
		// bytes would decode, so the diff is semantic (key set + values),
		// not textual (whitespace/field order) -- mirrors the Oracle's own
		// json.loads(_serialize_json(new_doc)) round-trip.
		newBytes, err := json.Marshal(doc)
		if err != nil {
			return DriftReport{}, err
		}
		var canonicalNew any
		if err := json.Unmarshal(newBytes, &canonicalNew); err != nil {
			return DriftReport{}, err
		}

		onDisk, exists := loadOnDisk(absPath)
		if !exists {
			diffs := jsonKeyDiff(map[string]any{}, canonicalNew, "")
			reports = append(reports, DriftOutputReport{Format: format, Path: outputPath, Status: "missing", Differences: diffs})
			continue
		}
		if onDisk == nil {
			// Exists but failed to parse as JSON: treat as drift with one
			// whole-document diff (drift_check.py:202-212).
			reports = append(reports, DriftOutputReport{
				Format: format, Path: outputPath, Status: "drift",
				Differences: []DriftDifference{{Path: "", Old: nil, New: canonicalNew}},
			})
			continue
		}

		diffs := jsonKeyDiff(onDisk, canonicalNew, "")
		if len(diffs) == 0 {
			reports = append(reports, DriftOutputReport{Format: format, Path: outputPath, Status: "unchanged"})
		} else {
			reports = append(reports, DriftOutputReport{Format: format, Path: outputPath, Status: "drift", Differences: diffs})
		}
	}

	sort.Slice(reports, func(i, j int) bool { return reports[i].Format < reports[j].Format })
	overallOK := true
	for _, r := range reports {
		if r.Status != "unchanged" {
			overallOK = false
			break
		}
	}
	return DriftReport{OK: overallOK, Outputs: reports}, nil
}

// RenderDiffLines mirrors render_diff_lines (drift_check.py:241-252):
// human-readable per-diff lines, bounded to limit entries with a trailing
// "... and N more differences" summary.
func RenderDiffLines(report DriftOutputReport, limit int) []string {
	var rendered []string
	diffs := report.Differences
	n := len(diffs)
	if n > limit {
		n = limit
	}
	for _, d := range diffs[:n] {
		rendered = append(rendered, fmt.Sprintf("  %s  %s -> %s", d.Path, jsonCompactASCII(d.Old), jsonCompactASCII(d.New)))
	}
	if extra := len(diffs) - limit; extra > 0 {
		rendered = append(rendered, fmt.Sprintf("  ... and %d more differences", extra))
	}
	return rendered
}

// jsonCompactASCII mirrors json.dumps(v, ensure_ascii=True) (the Python
// default): compact JSON with every non-ASCII rune escaped as \uXXXX
// (surrogate-pair encoded above the BMP). Go's own encoding/json emits raw
// UTF-8 for strings by default; render_diff_lines' values are almost
// always plain-ASCII scalars (version numbers, package names) in practice,
// so this only actually diverges from Go's default on genuinely non-ASCII
// content.
func jsonCompactASCII(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	var sb strings.Builder
	for _, r := range string(b) {
		if r < 0x80 {
			sb.WriteRune(r)
			continue
		}
		if r > 0xFFFF {
			r1, r2 := utf16.EncodeRune(r)
			fmt.Fprintf(&sb, `\u%04x\u%04x`, r1, r2)
		} else {
			fmt.Fprintf(&sb, `\u%04x`, r)
		}
	}
	return sb.String()
}
