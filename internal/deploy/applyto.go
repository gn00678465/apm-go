package deploy

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/apm-go/apm/internal/yamlcore"
)

// ReadInstruction splits an instruction file into its applyTo and the body
// after the frontmatter, reading the frontmatter as YAML the way the Oracle
// does (instruction_integrator.py:81-86, primitives/parser.py:115-116): a
// list is one comma-joined value, a scalar that is not a string is Python's
// str() of it, and "" is no applyTo. A frontmatter that is not valid YAML,
// or that the OpenAPM safe subset or Python's loader refuses, is an error;
// the Oracle refuses it too.
func ReadInstruction(content []byte) (applyTo, body string, err error) {
	text := string(content)
	loc := claudeFrontmatterRE.FindStringSubmatchIndex(text)
	if loc == nil {
		return "", text, nil
	}
	// The Oracle's block ends with the line break before the closing ---,
	// which a literal or folded last value keeps.
	doc, err := yamlcore.SafeLoad([]byte(text[loc[2]:loc[3]] + "\n"))
	if errors.Is(err, io.EOF) {
		// Only comments or blank lines: PyYAML reads None.
		return "", text[loc[1]:], nil
	}
	if err != nil {
		return "", "", fmt.Errorf("frontmatter: %w", err)
	}
	v, err := constructPy(doc)
	if err != nil {
		return "", "", fmt.Errorf("frontmatter: %w", err)
	}
	if d, ok := v.(*pyDict); ok {
		applyTo = normalizeApplyTo(d.get(pyStr("applyTo")))
	}
	return applyTo, text[loc[1]:], nil
}

// applyTo's escape and separator (oracle: utils/patterns.py:18-20).
const (
	applyToEscape    = '\\'
	applyToSeparator = ','
)

// normalizeApplyTo ports normalize_apply_to (utils/patterns.py:174-192): a
// list keeps each item that is not None and not blank once stripped,
// escaped so parseApplyTo splits it back into the same items.
func normalizeApplyTo(v pyValue) string {
	switch x := v.(type) {
	case nil, pyNone:
		return ""
	case pyList:
		var patterns []string
		for _, item := range x {
			if _, ok := item.(pyNone); ok {
				continue
			}
			if s := pyStrip(item.str()); s != "" {
				patterns = append(patterns, escapeApplyToSegment(s))
			}
		}
		return strings.Join(patterns, string(applyToSeparator))
	}
	return v.str()
}

// escapeApplyToSegment ports escape_apply_to_segment
// (utils/patterns.py:195-231): a backslash is doubled, and a comma outside
// braces and a [...] character class is escaped.
func escapeApplyToSegment(pattern string) string {
	var b strings.Builder
	depth := 0
	inClass := false
	for _, r := range pattern {
		switch {
		case inClass:
			b.WriteRune(r)
			if r == ']' {
				inClass = false
			}
		case r == applyToEscape:
			b.WriteString(`\\`)
		case r == '[':
			inClass = true
			b.WriteRune(r)
		case r == '{':
			depth++
			b.WriteRune(r)
		case r == '}':
			if depth > 0 {
				depth--
			}
			b.WriteRune(r)
		case r == applyToSeparator && depth == 0:
			b.WriteString(`\,`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// parseApplyTo ports parse_apply_to (utils/patterns.py:234-295): only a
// comma outside braces and a [...] character class separates globs, \, and
// \\ are a literal comma and backslash, and each glob is stripped, the
// empty ones dropped.
func parseApplyTo(value string) []string {
	if value == "" {
		return nil
	}
	runes := []rune(value)
	var segments []string
	var current strings.Builder
	depth := 0
	inClass := false
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == applyToEscape && i+1 < len(runes) && (runes[i+1] == applyToSeparator || runes[i+1] == applyToEscape) {
			current.WriteRune(runes[i+1])
			i++
			continue
		}
		switch {
		case inClass:
			current.WriteRune(r)
			if r == ']' {
				inClass = false
			}
		case r == '[':
			inClass = true
			current.WriteRune(r)
		case r == '{':
			depth++
			current.WriteRune(r)
		case r == '}':
			if depth > 0 {
				depth--
			}
			current.WriteRune(r)
		case r == applyToSeparator && depth == 0:
			segments = append(segments, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	segments = append(segments, current.String())

	var globs []string
	for _, s := range segments {
		if s = pyStrip(s); s != "" {
			globs = append(globs, s)
		}
	}
	return globs
}

// pyStrip is Python's str.strip(): Go's whitespace plus the separators
// U+001C to U+001F.
func pyStrip(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
	})
}

// yamlDoubleQuote ports yaml_double_quote (utils/patterns.py:79-95), which
// is json.dumps(value, ensure_ascii=True): every character outside space to
// tilde is escaped, one beyond U+FFFF as a UTF-16 surrogate pair.
func yamlDoubleQuote(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r >= ' ' && r <= '~':
			b.WriteRune(r)
		case r > 0xffff:
			r -= 0x10000
			fmt.Fprintf(&b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
