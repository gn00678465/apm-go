package semver

import (
	"fmt"
	"sort"
	"strings"

	depsdev "deps.dev/util/semver"
)

type TagInfo struct {
	Name   string // full tag string, e.g. "v1.2.3+build.1"
	Commit string // SHA
}

func Satisfies(version, rangeExpr string) (bool, error) {
	c, err := depsdev.NPM.ParseConstraint(rangeExpr)
	if err != nil {
		return false, fmt.Errorf("parse range %q: %w", rangeExpr, err)
	}
	return c.Match(version), nil
}

// IsSemverRange returns true if ref is a semver range expression (not a bare version).
// Per req-rs-003: a bare version like "1.2.3" or "v1.2.3" is a literal tag, not a range.
// Ranges use operators: ^, ~, >=, >, <=, <, =, ||, *, x, X, or hyphen notation.
func IsSemverRange(ref string) bool {
	if ref == "" {
		return false
	}
	// Bare versions (with optional v prefix) are literal, not ranges
	stripped := ref
	if len(stripped) > 0 && stripped[0] == 'v' {
		stripped = stripped[1:]
	}
	_, err := depsdev.NPM.Parse(stripped)
	if err == nil {
		// Parses as an exact version — only a range if it contains an operator
		if !hasRangeOperator(ref) {
			return false
		}
	}
	_, err = depsdev.NPM.ParseConstraint(ref)
	return err == nil
}

func hasRangeOperator(s string) bool {
	for _, c := range s {
		switch c {
		case '^', '~', '>', '<', '=', '|', '*':
			return true
		}
	}
	// Hyphen range: "1.2.3 - 1.5.0"
	if strings.Contains(s, " - ") {
		return true
	}
	// Wildcard: 1.x, 1.2.x, x.x.x — only when x/X appears as a whole version segment
	for _, seg := range strings.Split(s, ".") {
		if seg == "x" || seg == "X" {
			return true
		}
	}
	return false
}

func CompareVersions(a, b string) int {
	cmp := depsdev.NPM.Compare(a, b)
	if cmp != 0 {
		return cmp
	}
	return strings.Compare(a, b)
}

func MaxSatisfying(tags []TagInfo, rangeExpr string) (TagInfo, bool, error) {
	c, err := depsdev.NPM.ParseConstraint(rangeExpr)
	if err != nil {
		return TagInfo{}, false, fmt.Errorf("parse range %q: %w", rangeExpr, err)
	}

	var matching []TagInfo
	for _, t := range tags {
		ver := StripVPrefix(t.Name)
		if c.Match(ver) {
			matching = append(matching, t)
		}
	}

	if len(matching) == 0 {
		return TagInfo{}, false, nil
	}

	sort.Slice(matching, func(i, j int) bool {
		vi := StripVPrefix(matching[i].Name)
		vj := StripVPrefix(matching[j].Name)
		cmp := CompareVersions(vi, vj)
		if cmp != 0 {
			return cmp < 0
		}
		// req-rs-014: build-metadata tie-break by bytewise ASCII on full tag name
		return matching[i].Name < matching[j].Name
	})

	return matching[len(matching)-1], true, nil
}

func StripVPrefix(tag string) string {
	if strings.HasPrefix(tag, "v") {
		return tag[1:]
	}
	return tag
}

// IsPrerelease reports whether version (parsed under the NPM dialect, after
// stripping an optional leading "v") carries a prerelease tag -- used by
// `apm marketplace outdated`'s --include-prerelease filter (mkt-042 修訂版)
// to decide whether a candidate tag should even be considered before ranking
// (a separate concern from MaxSatisfying's own npm-style range matching,
// which already excludes most prereleases from non-prerelease ranges on its
// own). A version that fails to parse is reported as not a prerelease.
func IsPrerelease(version string) bool {
	v, err := depsdev.NPM.Parse(StripVPrefix(version))
	if err != nil {
		return false
	}
	return v.IsPrerelease()
}
