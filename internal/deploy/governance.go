package deploy

import (
	"path"
	"slices"
	"sort"
	"strings"
)

// DeployBucket names whose deployed files a lock list records: the project's
// own .apm/ content or one dependency's.
type DeployBucket int

const (
	LocalBucket DeployBucket = iota
	DependencyBucket
)

// governedRoots lists the project-relative locations one target's adapter
// can write for each bucket kind (its DeployPrimitive / FinalizeBundles
// destinations).
type governedRoots struct {
	local      []string
	dependency []string
}

func sameForBothBuckets(roots ...string) governedRoots {
	return governedRoots{local: roots, dependency: roots}
}

// governedPaths is finer than DeployRoots on two points. codex, copilot,
// opencode and agent-skills list ".agents/" but write only ".agents/skills/".
// antigravity writes local content to flat ".agents/" paths and a dependency
// only under ".agents/plugins/" (antigravity.go), so a dependency's
// ".agents/skills/" copy is never antigravity's. Mirrors the oracle's
// per-target managed prefixes (src/apm_cli/install/manifest_reconcile.py:60-104).
var governedPaths = map[string]governedRoots{
	"claude":       sameForBothBuckets(".claude"),
	"codex":        sameForBothBuckets(".codex", ".agents/skills"),
	"copilot":      sameForBothBuckets(".github", ".agents/skills"),
	"opencode":     sameForBothBuckets(".opencode", ".agents/skills"),
	"agent-skills": sameForBothBuckets(".agents/skills"),
	"antigravity": {
		local:      []string{".agents/skills", ".agents/rules", ".agents/agents", ".agents/hooks.json"},
		dependency: []string{antigravityBundleRoot},
	},
}

// userGovernedPaths is governedPaths for a --global deploy: copilot writes
// below ~/.copilot and opencode below ~/.config/opencode, with its skills in
// its own skills/ (integration/targets.py:538-700).
var userGovernedPaths = map[string]governedRoots{
	"claude":       governedPaths["claude"],
	"codex":        governedPaths["codex"],
	"copilot":      sameForBothBuckets(".copilot", ".agents/skills"),
	"opencode":     sameForBothBuckets(".config/opencode"),
	"agent-skills": governedPaths["agent-skills"],
	"antigravity":  governedPaths["antigravity"],
}

// legacyUserRoots are where a --global deploy of apm-go before issue #125
// wrote copilot's and opencode's files: ~/.github and ~/.opencode, which
// neither tool reads at user scope, and opencode's skills in ~/.agents/skills.
var legacyUserRoots = map[string][]string{
	"copilot":  {".github"},
	"opencode": {".opencode", ".agents/skills"},
}

// Governs is TargetsGovernPath at user scope (userScope, a --global deploy)
// or in a project.
func Governs(userScope bool, targets []string, bucket DeployBucket, relPath string) bool {
	if !userScope {
		return TargetsGovernPath(targets, bucket, relPath)
	}
	return governedBy(userGovernedPaths, targets, bucket, relPath)
}

// GovernsLegacyUserPath reports whether relPath, a path a --global lock
// records, is where apm-go before issue #125 wrote a copilot or opencode
// file: below ~/.github or ~/.opencode, whatever targets are, or below
// ~/.agents/skills when opencode is one of targets. No deploy writes there
// any more, so it is stale.
func GovernsLegacyUserPath(targets []string, relPath string) bool {
	p := normalizeRelPath(relPath)
	for target, roots := range legacyUserRoots {
		for _, root := range roots {
			if (p == root || strings.HasPrefix(p, root+"/")) && (root != sharedSkillRoot || slices.Contains(targets, target)) {
				return true
			}
		}
	}
	return false
}

// AllTargets lists every target apm-go deploys to, sorted.
func AllTargets() []string {
	targets := make([]string, 0, len(governedPaths))
	for t := range governedPaths {
		targets = append(targets, t)
	}
	sort.Strings(targets)
	return targets
}

func normalizeRelPath(p string) string {
	return path.Clean(strings.ReplaceAll(p, "\\", "/"))
}

// TargetsGovernPath reports whether an adapter of one of targets can write
// relPath for a bucket of the given kind. A path that no listed target
// governs must not be treated as stale output of a run over those targets.
func TargetsGovernPath(targets []string, bucket DeployBucket, relPath string) bool {
	return governedBy(governedPaths, targets, bucket, relPath)
}

func governedBy(paths map[string]governedRoots, targets []string, bucket DeployBucket, relPath string) bool {
	p := normalizeRelPath(relPath)
	for _, t := range targets {
		roots := paths[t].local
		if bucket == DependencyBucket {
			roots = paths[t].dependency
		}
		for _, root := range roots {
			if p == root || strings.HasPrefix(p, root+"/") {
				return true
			}
		}
	}
	return false
}

// IsMCPConfigPath reports whether relPath is a merged MCP config file. These
// files are shared with the user's own entries, so they are never stale
// deploy output.
func IsMCPConfigPath(relPath string) bool {
	p := normalizeRelPath(relPath)
	for _, t := range mcpRemoveTargets {
		if p == t.relPath {
			return true
		}
	}
	return p == userCopilotMCPPath
}
