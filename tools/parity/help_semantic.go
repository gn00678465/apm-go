//go:build unix

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// isHelpCase reports whether argv ends in "--help" (ticket 02 attempt 2:
// "for any case whose argv ends in --help"). "-h" is deliberately excluded:
// the acceptance criterion names --help specifically, and the two known
// help cases (doctor-help, search-help) both use the long form.
func isHelpCase(argv []string) bool {
	return len(argv) > 0 && argv[len(argv)-1] == "--help"
}

// helpFlagInfo is one flag's semantic surface, extracted from a --help
// listing independent of the surrounding CLI framework's layout (Click's
// "Options:" vs Cobra's "Flags:").
type helpFlagInfo struct {
	LongFlag       string `json:"long_flag"`
	ShortAlias     string `json:"short_alias,omitempty"`
	DefaultIfShown string `json:"default_if_shown,omitempty"`
	Description    string `json:"description"`
}

// helpSemanticDiff is the help_semantic field's detail: which flags differ
// (by long_flag, present only when the flag sets or any flag's own fields
// differ) and whether the command's own first description paragraph
// differs, independent of Usage:/Options:/Flags: layout.
type helpSemanticDiff struct {
	Flags                *helpFlagsOldNew `json:"flags,omitempty"`
	DescriptionParagraph *stringOldNew    `json:"description_paragraph,omitempty"`
}

type helpFlagsOldNew struct {
	Old []helpFlagInfo `json:"old"`
	New []helpFlagInfo `json:"new"`
}

// flagSpecRe matches the leading "-x, --long-flag" (or bare "--long-flag")
// spec at the start of a flag-definition line from either Click's or
// Cobra's --help rendering:
//
//	-v, --verbose  Show detailed output           (Click)
//	-h, --help      help for doctor                (Cobra)
//	--help         Show this message and exit.     (Click, no short alias)
//	--limit int    number of results (default 20)  (Cobra, with metavar)
//
// The rest of the line (metavar, then 2+ spaces, then description) is
// split off separately by flagSpecRe's trailing capture and reparsed by
// splitFlagDescription, since the metavar's own width varies per flag.
var flagSpecRe = regexp.MustCompile(`^\s*(?:-(\w),\s*)?--([a-zA-Z][\w-]*)(.*)$`)

// flagDescGapRe finds the 2+-space gap both Click and Cobra use to visually
// separate a flag's spec (name, and optional metavar/type) from its
// description text.
var flagDescGapRe = regexp.MustCompile(`\s{2,}`)

// defaultAnnotationRe extracts a flag's displayed default from its
// description, in either Click's "[default: X]" or Cobra/pflag's
// "(default X)" style.
var defaultAnnotationRe = regexp.MustCompile(`(?i)\[default:\s*([^\]]+)\]|\(default:?\s*([^)]+)\)`)

// helpFlagOwnHelp is the "--help" flag itself. Both Click and Cobra inject
// it automatically -- the command author never defines it -- and each
// framework gives it its own boilerplate short alias and description
// ("Show this message and exit." vs "help for <command>", Click omitting a
// short alias entirely while Cobra always adds "-h"). That is exactly the
// same kind of framework-rendering artifact heuristicTaxonomy's F01
// already treats as a stdout-only, waivable difference for --help cases,
// so it is excluded here rather than manufacturing a permanent
// help_semantic false positive on every --help case that has one.
const helpFlagOwnHelp = "help"

// parseHelpFlags scans every line of text for a flag-definition line and
// returns one helpFlagInfo per distinct long flag the COMMAND itself
// defines (excluding the framework-injected "--help" flag, see
// helpFlagOwnHelp), sorted by long flag name so two independently-ordered
// listings compare equal when their content matches.
func parseHelpFlags(text string) []helpFlagInfo {
	seen := make(map[string]helpFlagInfo)
	for _, line := range strings.Split(text, "\n") {
		short, long, desc, ok := parseFlagLine(line)
		if !ok || long == helpFlagOwnHelp {
			continue
		}
		info := helpFlagInfo{LongFlag: long, ShortAlias: short}
		if dm := defaultAnnotationRe.FindStringSubmatch(desc); dm != nil {
			if dm[1] != "" {
				info.DefaultIfShown = strings.TrimSpace(dm[1])
			} else {
				info.DefaultIfShown = strings.TrimSpace(dm[2])
			}
		}
		// Strip the annotation itself out of the prose, then collapse the
		// whitespace it leaves behind -- otherwise Click's "[default: 20]"
		// and Cobra/pflag's "(default 20)" describe the exact same default
		// but leave the two sides' Description unequal purely on framework
		// syntax (ticket 02 attempt 3: eval-ticket-02-r2.md Issue 2).
		info.Description = strings.Join(strings.Fields(defaultAnnotationRe.ReplaceAllString(desc, "")), " ")
		seen[long] = info
	}

	flags := make([]helpFlagInfo, 0, len(seen))
	for _, f := range seen {
		flags = append(flags, f)
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i].LongFlag < flags[j].LongFlag })
	return flags
}

// parseFlagLine splits one --help line into its short alias (if any), long
// flag name, and description, or reports ok=false if line isn't a
// flag-definition line at all. The description is whatever follows the
// first 2+-space gap after the long flag name -- everything between the
// flag name and that gap (a metavar like "int" or "TEXT") is intentionally
// discarded, since it varies by CLI framework and isn't part of the
// semantic surface this comparison cares about.
func parseFlagLine(line string) (short, long, desc string, ok bool) {
	m := flagSpecRe.FindStringSubmatch(line)
	if m == nil {
		return "", "", "", false
	}
	short, long, rest := m[1], m[2], m[3]

	loc := flagDescGapRe.FindStringIndex(rest)
	if loc == nil {
		return "", "", "", false
	}
	desc = strings.TrimSpace(rest[loc[1]:])
	if desc == "" {
		return "", "", "", false
	}
	return short, long, desc, true
}

// helpHeaderBlockRe matches a paragraph's opening line that marks it as
// Usage/Options/Flags scaffolding rather than the command's own prose
// description.
var helpHeaderBlockRe = regexp.MustCompile(`(?i)^usage:|(?:options|flags):\s*$`)

// parseHelpDescriptionParagraph returns the first paragraph (a run of
// non-blank lines) in text that isn't Usage:/Options:/Flags: scaffolding,
// with internal whitespace collapsed so line-wrapping differences between
// Click and Cobra don't matter. Click puts this paragraph after its
// "Usage: ..." line; Cobra puts it before its own "Usage:" block -- this
// scans every paragraph rather than assuming a position, so it finds the
// same text either way.
func parseHelpDescriptionParagraph(text string) string {
	var blocks [][]string
	var current []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(current) > 0 {
				blocks = append(blocks, current)
				current = nil
			}
			continue
		}
		current = append(current, trimmed)
	}
	if len(current) > 0 {
		blocks = append(blocks, current)
	}

	for _, block := range blocks {
		if helpHeaderBlockRe.MatchString(block[0]) {
			continue
		}
		return strings.Join(strings.Fields(strings.Join(block, " ")), " ")
	}
	return ""
}

// diffHelpSemantic reads each side's stdout.bin, normalizes it the same way
// diffNormalizedField does, and compares the extracted flag set and first
// description paragraph. Parsing from the normalized text (not raw) means a
// help_semantic comparison is immune to sandbox-path noise the same way
// stdout itself is.
func diffHelpSemantic(oracleCaseDir, targetCaseDir, oCwd, oCfg, oHome, tCwd, tCfg, tHome string, rewriteBinaryName bool) (helpSemanticDiff, bool, error) {
	oRaw, err := os.ReadFile(filepath.Join(oracleCaseDir, "stdout.bin"))
	if err != nil {
		return helpSemanticDiff{}, false, err
	}
	tRaw, err := os.ReadFile(filepath.Join(targetCaseDir, "stdout.bin"))
	if err != nil {
		return helpSemanticDiff{}, false, err
	}

	oText := normalizeString(string(oRaw), oCwd, oCfg, oHome, rewriteBinaryName)
	tText := normalizeString(string(tRaw), tCwd, tCfg, tHome, rewriteBinaryName)

	oFlags := parseHelpFlags(oText)
	tFlags := parseHelpFlags(tText)
	oPara := parseHelpDescriptionParagraph(oText)
	tPara := parseHelpDescriptionParagraph(tText)

	var d helpSemanticDiff
	differ := false
	if !helpFlagsEqual(oFlags, tFlags) {
		d.Flags = &helpFlagsOldNew{Old: oFlags, New: tFlags}
		differ = true
	}
	if oPara != tPara {
		d.DescriptionParagraph = &stringOldNew{Old: oPara, New: tPara}
		differ = true
	}
	return d, differ, nil
}

func helpFlagsEqual(a, b []helpFlagInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
