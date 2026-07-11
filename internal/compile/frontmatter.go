package compile

import (
	"regexp"
	"strings"
)

// frontmatterRE mirrors the Python oracle's frontmatter delimiter regex
// (deploy.claudeFrontmatterRE / instruction_integrator.py: `^---\s*\n(.*?)\n---\s*\n?`
// with DOTALL). Duplicated here (rather than exported from internal/deploy)
// per design.md §7's "minimal copy" option -- it's a frozen, oracle-pinned
// literal, not behavior that would ever need to drift between the two
// packages independently.
var frontmatterRE = regexp.MustCompile(`(?s)^---\s*\n(.*?)\n---\s*\n?`)

// ParsedInstruction is one *.instructions.md file's parsed frontmatter +
// body (oracle: primitives/parser.py:80-119).
type ParsedInstruction struct {
	// ApplyTo is the raw applyTo value, verbatim (not split on commas --
	// design.md §3: compile groups by the raw string, unlike install's
	// comma/brace-aware parseApplyTo). Empty means "no applyTo" (global).
	ApplyTo string
	// Body is the content after the frontmatter block, stripped of
	// leading/trailing whitespace (oracle: instruction.content.strip()).
	Body string
}

// ParseInstruction parses one instruction file's raw bytes.
func ParseInstruction(content []byte) ParsedInstruction {
	text := string(content)
	applyTo := ""
	body := text

	if loc := frontmatterRE.FindStringSubmatchIndex(text); loc != nil {
		fmBlock := text[loc[2]:loc[3]]
		body = text[loc[1]:]
		applyTo = extractApplyTo(fmBlock)
	}

	return ParsedInstruction{
		ApplyTo: applyTo,
		Body:    strings.TrimSpace(body),
	}
}

// extractApplyTo finds the "applyTo:" key in a YAML frontmatter block and
// normalizes its value (oracle: primitives/parser.py:95-119
// _normalize_apply_to): a scalar is unquoted and kept verbatim; a YAML list
// (flow `[a, b]` or block `- a\n- b` form) yields its first non-null
// element.
func extractApplyTo(fmBlock string) string {
	lines := strings.Split(fmBlock, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "applyTo:") {
			continue
		}
		rest := strings.TrimSpace(trimmed[len("applyTo:"):])
		if rest == "" {
			return firstBlockListItem(lines[i+1:])
		}
		if strings.HasPrefix(rest, "[") {
			return firstFlowListItem(rest)
		}
		return unquoteScalar(rest)
	}
	return ""
}

// firstBlockListItem scans block-style YAML list items ("  - value") that
// follow an "applyTo:" key with no inline value, stopping at the first
// dedented (non "-") or blank-then-nonlist line, and returns the first
// non-null element.
func firstBlockListItem(rest []string) string {
	for _, line := range rest {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "-") {
			break
		}
		item := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		if isYAMLNull(item) {
			continue
		}
		return unquoteScalar(item)
	}
	return ""
}

// firstFlowListItem parses a flow-style YAML list on one line (e.g.
// `['**/*.py', '**/*.rb']`) and returns its first non-null element,
// respecting nested {}/[] so a brace-alternation glob inside a list
// element isn't mistaken for a list boundary.
func firstFlowListItem(rest string) string {
	inner := strings.TrimPrefix(rest, "[")
	inner = strings.TrimSuffix(strings.TrimSpace(inner), "]")

	depth := 0
	var b strings.Builder
	flush := func() string {
		item := strings.TrimSpace(b.String())
		b.Reset()
		return item
	}
	for _, ch := range inner {
		switch {
		case ch == '{' || ch == '[':
			depth++
			b.WriteRune(ch)
		case ch == '}' || ch == ']':
			if depth > 0 {
				depth--
			}
			b.WriteRune(ch)
		case ch == ',' && depth == 0:
			if item := flush(); !isYAMLNull(item) {
				return unquoteScalar(item)
			}
		default:
			b.WriteRune(ch)
		}
	}
	if item := flush(); !isYAMLNull(item) {
		return unquoteScalar(item)
	}
	return ""
}

func isYAMLNull(s string) bool {
	return s == "" || s == "null" || s == "~"
}

// unquoteScalar strips a single matching pair of surrounding quotes from a
// YAML scalar. Content (including embedded commas/braces) is otherwise kept
// byte-for-byte verbatim.
func unquoteScalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
