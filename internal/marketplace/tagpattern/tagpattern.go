// Package tagpattern compiles a marketplace `build.tagPattern` /
// `packages[].tagPattern` template (the "{version}"/"{name}" placeholder
// syntax documented in marketplace-checklist.md's mkt-041/mkt-042/mkt-050
// rows) into a matcher that extracts the version portion of a real git tag
// name.
//
// This lives in its own package -- not inside
// internal/marketplace/authoring -- because both the authoring sub-task's
// `check`/`outdated` commands (refcheck.go) and the separate `apm pack`
// sub-task's builder need the exact same template semantics; per that
// sub-task's design.md, whichever lands first establishes the shared
// location and the other must reuse it rather than reimplement it.
package tagpattern

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/apm-go/apm/internal/semver"
)

// DefaultPattern is used whenever a package/build tagPattern is unset,
// matching the "v{version}" default `apm marketplace init` scaffolds
// (internal/marketplace/authoring/template.go's initBlockTemplate).
const DefaultPattern = "v{version}"

// placeholderRe finds every "{...}" token so Validate can reject the ones the
// engine has no substitution for. It deliberately excludes nested braces,
// matching upstream's `re.compile(r"\{[^{}]*\}")`.
var placeholderRe = regexp.MustCompile(`\{[^{}]*\}`)

// Validate returns a normalized, consumer-safe tag pattern, or an error naming
// context (e.g. "build.tagPattern", "packages[2].tag_pattern") so the user
// knows which key is wrong.
//
// It mirrors upstream v0.27.0 marketplace/tag_pattern.py's
// validate_tag_pattern rule for rule. Note this is stricter than v0.26.0,
// which accepted any pattern containing {version} OR {name}: a pattern with no
// {version} produces a regex with no "version" capture group, so every tag is
// silently dropped and the user sees "no matching version" instead of a
// pattern error (see TestFilterTags_PatternWithoutVersionPlaceholder_
// DropsEverything).
func Validate(pattern, context string) (string, error) {
	normalized := strings.TrimSpace(pattern)
	if normalized == "" {
		return "", fmt.Errorf("%q must be a non-empty string, got %q", context, pattern)
	}

	var unsupported []string
	seen := make(map[string]bool)
	for _, ph := range placeholderRe.FindAllString(normalized, -1) {
		if ph == "{version}" || ph == "{name}" || seen[ph] {
			continue
		}
		seen[ph] = true
		unsupported = append(unsupported, ph)
	}
	if len(unsupported) > 0 {
		sort.Strings(unsupported)
		return "", fmt.Errorf("%q contains unsupported placeholder(s): %s", context, strings.Join(unsupported, ", "))
	}

	if n := strings.Count(normalized, "{version}"); n != 1 {
		return "", fmt.Errorf("%q must contain exactly one {version} placeholder, got %q", context, normalized)
	}
	return normalized, nil
}

// Compile turns pattern (using the "{version}" and "{name}" placeholders)
// into a regular expression that matches a full tag name and captures the
// version portion under the "version" named group. name is substituted
// verbatim (escaped) wherever "{name}" appears; every other pattern
// character is matched literally. An empty pattern falls back to
// "v{version}".
func Compile(pattern, name string) *regexp.Regexp {
	if pattern == "" {
		pattern = DefaultPattern
	}

	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(pattern); {
		switch {
		case strings.HasPrefix(pattern[i:], "{version}"):
			sb.WriteString(`(?P<version>.+)`)
			i += len("{version}")
		case strings.HasPrefix(pattern[i:], "{name}"):
			sb.WriteString(regexp.QuoteMeta(name))
			i += len("{name}")
		default:
			sb.WriteString(regexp.QuoteMeta(string(pattern[i])))
			i++
		}
	}
	sb.WriteString("$")
	return regexp.MustCompile(sb.String())
}

// ExtractVersion returns the version captured from tagName by re (as
// returned by Compile), and whether tagName matched the pattern at all.
func ExtractVersion(re *regexp.Regexp, tagName string) (string, bool) {
	m := re.FindStringSubmatch(tagName)
	if m == nil {
		return "", false
	}
	idx := re.SubexpIndex("version")
	if idx < 0 || idx >= len(m) {
		return "", false
	}
	return m[idx], true
}

// RenderTag mirrors render_tag (tag_pattern.py:82-101): expands the
// "{version}" and "{name}" placeholders in pattern with literal
// substitution (order matters -- version first, matching the Oracle's own
// replace order, though the two placeholders can never collide since
// neither's own text contains the other's literal brace form). Used by the
// version-alignment release gate (`apm pack --check-versions`,
// internal/marketplace/build/version_check.go) to render the tag a
// tag_pattern-strategy package's version is EXPECTED to produce, the
// inverse operation of ExtractVersion/Compile (which go the other way,
// tag name -> extracted version).
func RenderTag(pattern, name, version string) string {
	result := strings.ReplaceAll(pattern, "{version}", version)
	result = strings.ReplaceAll(result, "{name}", name)
	return result
}

// DefaultPatterns mirrors tag_pattern.py's DEFAULT_TAG_PATTERNS: the common
// tag layouts tried, in this order, when the configured pattern matches no
// tag at all (#1504, `_extract_tag_versions` in commands/marketplace/
// __init__.py).
var DefaultPatterns = []string{
	"v{version}",
	"{version}",
	"{name}_v{version}",
	"{name}--v{version}",
	"{name}-v{version}",
}

// oracleVersionRe is the Oracle's _SEMVER_RE (marketplace/semver.py:34-38),
// the grammar tag_pattern.py's version capture and iter_semver_tags accept.
// Digits are ASCII and $ is end of text: Python's \d and $ would also accept
// Unicode digits and a trailing newline, which no release tag carries (SPEC
// marketplace-check-outdated-fixes decision D-b).
var oracleVersionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

// IsOracleVersion reports whether a captured "{version}" is one the Oracle
// would treat as a release: x.y.z with optional prerelease and build, no
// leading "v".
func IsOracleVersion(s string) bool {
	return oracleVersionRe.MatchString(s)
}

// Infer mirrors infer_tag_pattern_from_refs (tag_pattern.py): the first
// DefaultPatterns entry that matches the first tag it fits, or "" when no
// tag matches any layout. Only entries that are tags (Ref empty or under
// refs/tags/) are considered, and a match must capture an Oracle version --
// Compile's "{version}" capture is greedy, so "{version}" alone would
// otherwise claim every tag.
func Infer(tags []semver.TagInfo, name string) string {
	for _, t := range tags {
		if !IsTagRef(t) {
			continue
		}
		for _, pattern := range DefaultPatterns {
			if name == "" && strings.Contains(pattern, "{name}") {
				continue
			}
			if v, ok := ExtractVersion(Compile(pattern, name), t.Name); ok && IsOracleVersion(v) {
				return pattern
			}
		}
	}
	return ""
}

// IsTagRef reports whether t was advertised under refs/tags/. A TagInfo
// built from a bare name (Ref == "") counts as a tag so callers that only
// ever knew tag names keep working.
func IsTagRef(t semver.TagInfo) bool {
	return t.Ref == "" || strings.HasPrefix(t.Ref, "refs/tags/")
}

// FilterTags compiles pattern (for name) and returns only the tags in tags
// that match it, with each result's Name replaced by the *extracted
// version* (ready for internal/semver.MaxSatisfying/Satisfies) while
// preserving Commit. Tags that don't match the pattern (e.g. an unrelated
// branch head, or another package's tags in a monorepo) are dropped.
func FilterTags(tags []semver.TagInfo, pattern, name string) []semver.TagInfo {
	re := Compile(pattern, name)
	out := make([]semver.TagInfo, 0, len(tags))
	for _, t := range tags {
		if version, ok := ExtractVersion(re, t.Name); ok {
			out = append(out, semver.TagInfo{Name: version, Commit: t.Commit})
		}
	}
	return out
}
