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

// convertToClaudeRules converts APM instruction content to Claude Code
// rules .md format, mirroring the Python oracle _convert_to_claude_rules
// (instruction_integrator.py:784-805): the applyTo frontmatter value maps
// to a `paths:` YAML list; instructions without applyTo (or without
// frontmatter) become unconditional rules -- any existing frontmatter is
// stripped and leading newlines of the body are trimmed. Emitted
// frontmatter uses LF line endings; body bytes are preserved as-is.
func convertToClaudeRules(content []byte) ([]byte, error) {
	applyTo, body, err := ReadInstruction(content)
	if err != nil {
		return nil, err
	}

	globs := parseApplyTo(applyTo)
	if len(globs) == 0 {
		// No applyTo -> unconditional rule, body without frontmatter.
		return []byte(strings.TrimLeft(body, "\n")), nil
	}

	parts := []string{"---", "paths:"}
	for _, g := range globs {
		parts = append(parts, "  - "+yamlDoubleQuote(g))
	}
	parts = append(parts, "---")
	return []byte(strings.Join(parts, "\n") + "\n\n" + strings.TrimLeft(body, "\n")), nil
}

// deployClaudeInstructions writes an instruction primitive to
// .claude/rules/<name>.md transformed via convertToClaudeRules (unlike the
// byte-copy deployFileToPath used by other targets' instructions).
func deployClaudeInstructions(p Primitive, projectDir string) ([]string, error) {
	data, err := os.ReadFile(p.SrcPath)
	if err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	rule, err := convertToClaudeRules(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.SrcPath, err)
	}
	destPath := fmt.Sprintf(".claude/rules/%s.md", p.Name)
	if err := CheckDeployWrite(projectDir, destPath); err != nil {
		return nil, err
	}
	absDest := filepath.Join(projectDir, filepath.FromSlash(destPath))
	if err := os.MkdirAll(filepath.Dir(absDest), 0755); err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	if err := os.WriteFile(absDest, rule, 0644); err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	return []string{destPath}, nil
}
