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
	"kiro":         true, // parity: Python CANONICAL_TARGETS (apm_yml.py:25-37)
	"all":          true,
	"antigravity":  true, // pre-standard, tracking microsoft/apm#1650
}

var TargetAliases = map[string]string{
	"vscode": "copilot",
	"agents": "copilot",
	"agy":    "antigravity",
}

// deployTargets is the single source of truth for every target with a
// deploy adapter (R8): SupportedTargets (init's --target/prompt whitelist),
// adapterTargets (HasAdapter), and the init MultiSelect prompt options are
// all derived from this one slice. That only guarantees they start in sync
// at compile time -- it does NOT stop a later edit from reassigning one of
// the derived vars independently (SupportedTargets is a reassignable
// package-level var, not a const derived by the type system), so
// TestSupportedTargets_MatchesDeployTargetsExactly (internal/manifest) and
// TestAdapterTargetsSet_MatchesDeployTargets exist specifically to catch
// that drift at test time (2026-07-30 codex Tier 2 B2: an earlier version of
// this comment claimed the three "cannot drift apart", which a one-line
// mutation -- SupportedTargets = deployTargets[:5] -- disproved; the two
// tests that existed at the time only compared derived vars against each
// other, not against this source slice, so neither caught it). Order is the
// order the init/plugin-init MultiSelect prompt shows them in (copilot
// first, matching the prompt's pre-existing order; agent-skills appended at
// the end since it was the missing entry, see R8.1).
var deployTargets = []string{
	"copilot", "claude", "opencode", "codex", "antigravity", "agent-skills",
}

// SupportedTargets is the whitelist `apm-go init --target` validates
// against. Derived from deployTargets (R8.3).
var SupportedTargets = deployTargets

// adapterTargets backs HasAdapter. Derived from deployTargets (R8.3).
var adapterTargets = targetSet(deployTargets)

// ExplicitOnlyTargets are omitted from the init/plugin-init MultiSelect
// prompt; they stay selectable via --target/apm.yml. Membership is locked by
// TestPromptTargets_ExactMembership against a literal list.
var ExplicitOnlyTargets = map[string]bool{
	"agent-skills": true,
}

// PromptTargets is the subset of deployTargets shown in the init/plugin-init
// MultiSelect prompt: deployTargets with ExplicitOnlyTargets filtered out,
// preserving deployTargets' display order (same derivation pattern as
// SupportedTargets/adapterTargets above).
var PromptTargets = filterOutExplicitOnly(deployTargets)

func filterOutExplicitOnly(targets []string) []string {
	var result []string
	for _, t := range targets {
		if !ExplicitOnlyTargets[t] {
			result = append(result, t)
		}
	}
	return result
}

func targetSet(targets []string) map[string]bool {
	m := make(map[string]bool, len(targets))
	for _, t := range targets {
		m[t] = true
	}
	return m
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
