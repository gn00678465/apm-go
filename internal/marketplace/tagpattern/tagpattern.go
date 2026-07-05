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
	"regexp"
	"strings"

	"github.com/apm-go/apm/internal/semver"
)

// defaultPattern is used whenever a package/build tagPattern is unset,
// matching the "v{version}" default `apm marketplace init` scaffolds
// (internal/marketplace/authoring/template.go's initBlockTemplate).
const defaultPattern = "v{version}"

// Compile turns pattern (using the "{version}" and "{name}" placeholders)
// into a regular expression that matches a full tag name and captures the
// version portion under the "version" named group. name is substituted
// verbatim (escaped) wherever "{name}" appears; every other pattern
// character is matched literally. An empty pattern falls back to
// "v{version}".
func Compile(pattern, name string) *regexp.Regexp {
	if pattern == "" {
		pattern = defaultPattern
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
