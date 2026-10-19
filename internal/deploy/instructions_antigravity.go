package deploy

import "strings"

// convertToAntigravityRules ports _convert_to_antigravity_rules
// (integration/instruction_integrator.py:809-833): applyTo becomes
// `trigger: glob` with `globs`, one glob as a scalar and more as a list, and
// every other key is dropped; with no glob the rule is the body alone.
func convertToAntigravityRules(content []byte) ([]byte, error) {
	applyTo, body, err := ReadInstruction(content)
	if err != nil {
		return nil, err
	}
	body = strings.TrimLeft(body, "\r\n")
	globs := parseApplyTo(pyStrip(strings.NewReplacer("\n", " ", "\r", " ").Replace(applyTo)))
	if len(globs) == 0 {
		return []byte(body), nil
	}
	parts := []string{"---", "trigger: glob"}
	if len(globs) == 1 {
		parts = append(parts, "globs: "+yamlDoubleQuote(globs[0]))
	} else {
		parts = append(parts, "globs:")
		for _, g := range globs {
			parts = append(parts, "  - "+yamlDoubleQuote(g))
		}
	}
	parts = append(parts, "---")
	return []byte(strings.Join(parts, "\n") + "\n\n" + body), nil
}

// antigravityRule is the rule the instruction p deploys to antigravity at
// destRel.
func antigravityRule(p Primitive, destRel string) ([]byte, error) {
	return instructionRule(p, destRel, convertToAntigravityRules)
}
