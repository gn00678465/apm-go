package manifest

import "fmt"

var CanonicalTargets = map[string]bool{
	"copilot":      true,
	"claude":       true,
	"cursor":       true,
	"codex":        true,
	"gemini":       true,
	"opencode":     true,
	"windsurf":     true,
	"agent-skills": true,
	"all":          true,
	"antigravity":  true, // pre-standard, tracking microsoft/apm#1650
}

var TargetAliases = map[string]string{
	"vscode": "copilot",
	"agents": "copilot",
	"agy":    "antigravity",
}

var SupportedTargets = []string{
	"claude", "codex", "copilot", "opencode", "antigravity",
}

var adapterTargets = map[string]bool{
	"claude":       true,
	"codex":        true,
	"copilot":      true,
	"opencode":     true,
	"antigravity":  true,
	"agent-skills": true,
}

func ValidateTarget(token string) (string, error) {
	if token == "minimal" {
		return "", fmt.Errorf("target 'minimal' must not be set explicitly")
	}
	if alias, ok := TargetAliases[token]; ok {
		return alias, nil
	}
	if CanonicalTargets[token] {
		return token, nil
	}
	if isVendorTarget(token) {
		return token, nil
	}
	return "", fmt.Errorf("unknown target %q", token)
}

func HasAdapter(token string) bool {
	return adapterTargets[token]
}

// isVendorTarget checks x-<vendor>-<name> pattern per req-tg-004.
// Requires at least two segments after x- separated by hyphen,
// each starting with [a-z] and containing [a-z0-9-].
func isVendorTarget(s string) bool {
	if len(s) < 5 || s[0] != 'x' || s[1] != '-' {
		return false
	}
	rest := s[2:]
	segments := 0
	i := 0
	for i < len(rest) {
		if rest[i] < 'a' || rest[i] > 'z' {
			return false
		}
		i++
		for i < len(rest) && rest[i] != '-' {
			c := rest[i]
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
				return false
			}
			i++
		}
		segments++
		if i < len(rest) {
			i++ // skip hyphen
		}
	}
	return segments >= 2
}
