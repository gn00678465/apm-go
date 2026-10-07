package deploy

import (
	"path"
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

func normalizeRelPath(p string) string {
	return path.Clean(strings.ReplaceAll(p, "\\", "/"))
}

// TargetsGovernPath reports whether an adapter of one of targets can write
// relPath for a bucket of the given kind. A path that no listed target
// governs must not be treated as stale output of a run over those targets.
func TargetsGovernPath(targets []string, bucket DeployBucket, relPath string) bool {
	p := normalizeRelPath(relPath)
	for _, t := range targets {
		roots := governedPaths[t].local
		if bucket == DependencyBucket {
			roots = governedPaths[t].dependency
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
	return false
}
