package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// claudeFrontmatterRE mirrors the Python oracle's frontmatter regex
// (instruction_integrator.py: `^---\s*\n(.*?)\n---\s*\n?` with DOTALL).
var claudeFrontmatterRE = regexp.MustCompile(`(?s)^---\s*\n(.*?)\n---\s*\n?`)

// yamlQuoteReplacer escapes the minimal set needed for a YAML 1.2
// double-quoted scalar (oracle: patterns.py yaml_double_quote). A single
// left-to-right pass is equivalent to Python's sequential replaces because
// no replacement output re-matches a later pattern.
var yamlQuoteReplacer = strings.NewReplacer(
	"\\", "\\\\",
	"\"", "\\\"",
	"\n", "\\n",
	"\r", "\\r",
	"\t", "\\t",
)

// yamlDoubleQuote returns value escaped and wrapped in double quotes for
// embedding inside emitted YAML frontmatter.
func yamlDoubleQuote(value string) string {
	return "\"" + yamlQuoteReplacer.Replace(value) + "\""
}

// parseApplyTo splits an applyTo value into individual glob patterns
// (oracle: patterns.py parse_apply_to). Only TOP-LEVEL commas separate
// patterns -- commas inside brace alternation (`**/*.{css,scss}`) are part
// of the glob. Segments are whitespace-trimmed; empties are dropped, so
// leading/trailing/doubled-up commas are tolerated. Empty input yields nil.
func parseApplyTo(value string) []string {
	if value == "" {
		return nil
	}
	var segments []string
	depth := 0
	var current strings.Builder
	for _, ch := range value {
		switch {
		case ch == '{':
			depth++
			current.WriteRune(ch)
		case ch == '}':
			if depth > 0 {
				depth--
			}
			current.WriteRune(ch)
		case ch == ',' && depth == 0:
			segments = append(segments, current.String())
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}
	segments = append(segments, current.String())

	var globs []string
	for _, s := range segments {
		if s = strings.TrimSpace(s); s != "" {
			globs = append(globs, s)
		}
	}
	return globs
}

// convertToClaudeRules converts APM instruction content to Claude Code
// rules .md format, mirroring the Python oracle _convert_to_claude_rules
// (instruction_integrator.py:670-703): the applyTo frontmatter value maps
// to a `paths:` YAML list; instructions without applyTo (or without
// frontmatter) become unconditional rules -- any existing frontmatter is
// stripped and leading newlines of the body are trimmed. Emitted
// frontmatter uses LF line endings; body bytes are preserved as-is.
func convertToClaudeRules(content []byte) []byte {
	body := string(content)
	applyTo := ""

	if loc := claudeFrontmatterRE.FindStringSubmatchIndex(body); loc != nil {
		fmBlock := body[loc[2]:loc[3]]
		body = body[loc[1]:]
		for _, line := range strings.Split(fmBlock, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "applyTo:") {
				applyTo = strings.Trim(strings.TrimSpace(line[len("applyTo:"):]), "'\"")
			}
		}
	}

	globs := parseApplyTo(applyTo)
	if len(globs) == 0 {
		// No applyTo -> unconditional rule, body without frontmatter.
		return []byte(strings.TrimLeft(body, "\n"))
	}

	parts := []string{"---", "paths:"}
	for _, g := range globs {
		parts = append(parts, "  - "+yamlDoubleQuote(g))
	}
	parts = append(parts, "---")
	return []byte(strings.Join(parts, "\n") + "\n\n" + strings.TrimLeft(body, "\n"))
}

// deployClaudeInstructions writes an instruction primitive to
// .claude/rules/<name>.md transformed via convertToClaudeRules (unlike the
// byte-copy deployFileToPath used by other targets' instructions).
func deployClaudeInstructions(p Primitive, projectDir string) ([]string, error) {
	data, err := os.ReadFile(p.SrcPath)
	if err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	destPath := fmt.Sprintf(".claude/rules/%s.md", p.Name)
	absDest := filepath.Join(projectDir, filepath.FromSlash(destPath))
	if err := os.MkdirAll(filepath.Dir(absDest), 0755); err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	if err := os.WriteFile(absDest, convertToClaudeRules(data), 0644); err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	return []string{destPath}, nil
}
