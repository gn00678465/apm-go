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

// deployRule writes the instruction p to destPath as render gives it, the
// rule formats the Oracle converts and compares by their output
// (integration/targets.py:779-784, instruction_integrator.py:450-485): no
// byte-copy through deployFileToPath, and no guard of a file the user put
// there, since the Oracle writes over it.
func deployRule(p Primitive, projectDir, destPath string, render func(Primitive, string) ([]byte, error)) ([]string, error) {
	rule, err := render(p, destPath)
	if err != nil {
		return nil, err
	}
	if err := CheckDeployWrite(projectDir, destPath); err != nil {
		return nil, err
	}
	absDest := filepath.Join(projectDir, filepath.FromSlash(destPath))
	// The Oracle adopts a rule whose bytes, read through a symlink, are
	// the rule (integration/instruction_integrator.py:450-485).
	noteAdopted(destPath, absDest, rule, false)
	if err := os.MkdirAll(filepath.Dir(absDest), 0755); err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	if err := os.WriteFile(absDest, rule, 0644); err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	return []string{destPath}, nil
}

// claudeRule is the rule the instruction p deploys to claude at destRel.
func claudeRule(p Primitive, destRel string) ([]byte, error) {
	return instructionRule(p, destRel, convertToClaudeRules)
}

// instructionRule is p's source through oracleText, then convert, then its
// links rewritten for destRel (instruction_integrator.py:177-202).
func instructionRule(p Primitive, destRel string, convert func([]byte) ([]byte, error)) ([]byte, error) {
	data, err := os.ReadFile(p.SrcPath)
	if err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	rule, err := convert(oracleText(data))
	if err != nil {
		// A relative path is printed with "/" on every system, as the
		// Oracle and compile's Skipping line print one; an absolute one
		// (--global) stays native, as the Oracle prints it.
		src := p.SrcPath
		if !filepath.IsAbs(src) {
			src = filepath.ToSlash(src)
		}
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	return rewriteLinks(p, destRel, rule), nil
}
