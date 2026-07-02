package manifest

import (
	"fmt"
	"sort"
	"strings"
)

type ResolveMode int

const (
	ResolveBake ResolveMode = iota
	ResolveTranslate
)

type FieldPos int

const (
	PosEnvDict FieldPos = iota
	PosArgs
	PosRegistryList
	PosURL
	PosHeader
)

// HasPlaceholder reports whether s contains any recognized mf-013 placeholder
// (${VAR}, ${env:VAR}, ${input:id}, or ${{ ... }}). Writers use this to decide
// whether an authored (non-placeholder) translate-mode env value must be
// rewritten to ${<key>} before being written to disk, so secrets never bake
// into a translate-mode target's config.
func HasPlaceholder(s string) bool {
	return ActionsRe.MatchString(s) || InputVarRe.MatchString(s) || EnvVarRe.MatchString(s)
}

// outsideActions filters submatch-index results (as returned by
// FindAllStringSubmatchIndex) to those whose full match does not overlap any
// ${{ ... }} span in value, so text that merely appears INSIDE an Actions
// expression (e.g. ${{ '${input:x}' }}) is never mistaken for a real
// placeholder. Unlike a mask/restore-by-sentinel approach, this never
// rewrites value itself, so it cannot corrupt authored bytes that happen to
// collide with a sentinel.
func outsideActions(value string, matches [][]int) [][]int {
	spans := ActionsRe.FindAllStringIndex(value, -1)
	if len(spans) == 0 {
		return matches
	}
	var out [][]int
	for _, m := range matches {
		overlaps := false
		for _, sp := range spans {
			if m[0] < sp[1] && m[1] > sp[0] {
				overlaps = true
				break
			}
		}
		if !overlaps {
			out = append(out, m)
		}
	}
	return out
}

// ResolvePlaceholders resolves mf-013 placeholders in value per the
// bake-vs-translate dispatch matrix. ${{ ... }} (GitHub Actions) spans are
// excluded from matching by position (see outsideActions), so their content
// is always left byte-for-byte untouched.
//
// refuse=true means the caller must not write the containing server at all.
// omit=true means the caller must drop the containing key/entry rather than
// write a resolved-or-literal value.
func ResolvePlaceholders(value string, mode ResolveMode, pos FieldPos, lookup func(string) (string, bool)) (out string, diags []string, refuse, omit bool) {
	inputMatches := outsideActions(value, InputVarRe.FindAllStringSubmatchIndex(value, -1))
	if len(inputMatches) > 0 {
		if mode == ResolveTranslate {
			return value, nil, false, false
		}
		for _, m := range inputMatches {
			diags = append(diags, fmt.Sprintf("${input:%s} cannot be resolved at non-interactive install; server refused", value[m[2]:m[3]]))
		}
		return value, diags, true, false
	}

	if mode == ResolveTranslate {
		return value, nil, false, false
	}

	if pos == PosArgs {
		return value, nil, false, false
	}

	envMatches := outsideActions(value, EnvVarRe.FindAllStringSubmatchIndex(value, -1))
	if len(envMatches) == 0 {
		return value, nil, false, false
	}

	undefined := map[string]bool{}
	var b strings.Builder
	last := 0
	for _, m := range envMatches {
		start, end := m[0], m[1]
		name := value[m[2]:m[3]]
		b.WriteString(value[last:start])
		if v, ok := lookup(name); ok {
			b.WriteString(v)
		} else {
			undefined[name] = true
			b.WriteString(value[start:end])
		}
		last = end
	}
	b.WriteString(value[last:])

	if len(undefined) == 0 {
		return b.String(), nil, false, false
	}

	// Registry-list undefined vars are silently omitted (apm-cli parity: the
	// registry-declared env schema is not an authoring surface for the local
	// developer, so it warrants no diagnostic — unlike env-dict/header/url).
	if pos == PosRegistryList {
		return value, nil, false, true
	}

	names := make([]string, 0, len(undefined))
	for n := range undefined {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		diags = append(diags, fmt.Sprintf("undefined variable %q referenced in MCP config", n))
	}
	if pos == PosURL {
		return value, diags, true, false
	}
	return value, diags, false, true
}
