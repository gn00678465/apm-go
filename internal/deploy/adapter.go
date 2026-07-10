package deploy

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/manifest"
)

type TargetAdapter interface {
	Name() string
	DeployRoots() []string
	SupportedTypes() []PrimitiveType
	DeployPrimitive(p Primitive, projectDir string) ([]string, error)
}

// MCPTarget is implemented by adapters that can write a merged MCP config
// file. Unlike DeployPrimitive (one file-copy per primitive), WriteMCP is
// called once per target with every TypeMCP winner, because N MCP servers
// merge into a single config file. written names the servers that actually
// landed in files (a server can be dropped from prims for this target
// without erroring, e.g. refused or a non-https/unsupported transport), so
// the caller can build accurate per-server source provenance (pr-001).
type MCPTarget interface {
	MCPResolveMode() manifest.ResolveMode
	WriteMCP(prims []Primitive, projectDir string) (files []string, written []string, diags []string, err error)
}

var Adapters = map[string]TargetAdapter{
	"claude":       &claudeAdapter{},
	"codex":        &codexAdapter{},
	"copilot":      &copilotAdapter{},
	"antigravity":  &antigravityAdapter{},
	"opencode":     &opencodeAdapter{},
	"agent-skills": &agentSkillsAdapter{},
}

// SplitTargetFlag splits a --target/-t CLI flag value on commas (trimming
// whitespace, dropping empty segments) and validates each resulting token
// against the canonical target vocabulary via manifest.ValidateTarget --
// req-mf-005. That is the SAME validator apm.yml's target: field already
// uses, so the CLI flag and the manifest field reject exactly the same set
// of unknown tokens (canonical names including "all", known aliases, and
// x-<vendor>-<name> extension tokens are all accepted). A genuinely unknown
// token is rejected, naming it.
//
// Known-but-adapterless canonical targets (cursor/gemini/windsurf) pass
// validation here -- ResolveTargets's checkUnsupported separately reports
// the non-fatal "no registered handler" diagnostic for those (req-tg-004);
// this function must never turn that into a hard error.
func SplitTargetFlag(flagTarget string) ([]string, error) {
	var tokens []string
	for _, part := range strings.Split(flagTarget, ",") {
		t := strings.TrimSpace(part)
		if t == "" {
			continue
		}
		normalized, err := manifest.ValidateTarget(t)
		if err != nil {
			return nil, fmt.Errorf("--target %q: %w", t, err)
		}
		tokens = append(tokens, normalized)
	}
	return tokens, nil
}

// ResolveTargets determines active targets by priority:
// 1. --target flag (explicit CLI, comma-separated, validated against the
//    canonical target vocabulary -- see SplitTargetFlag)
// 2. manifest target: field
// 3. auto-detection from filesystem signals
// Returns empty if nothing detected (no-deploy).
func ResolveTargets(flagTarget string, manifestTargets []string, projectDir string) ([]string, []string) {
	var diags []string

	if flagTarget != "" {
		tokens, err := SplitTargetFlag(flagTarget)
		if err != nil {
			return nil, []string{err.Error()}
		}
		var targets []string
		for _, t := range tokens {
			if t == "all" {
				targets = append(targets, allAutoDetectableTargets()...)
			} else {
				targets = append(targets, t)
			}
		}
		diags = append(diags, checkUnsupported(targets)...)
		return filterSupported(targets), diags
	}

	if len(manifestTargets) > 0 {
		var targets []string
		for _, t := range manifestTargets {
			if t == "all" {
				targets = append(targets, allAutoDetectableTargets()...)
			} else {
				targets = append(targets, t)
			}
		}
		diags = append(diags, checkUnsupported(targets)...)
		return filterSupported(targets), diags
	}

	detected := manifest.DetectTargets(projectDir)
	detected = filterExplicitOnly(detected)
	if len(detected) > 0 {
		return detected, nil
	}

	return nil, nil
}

// explicitOnlyTargets must never be activated by auto-detection
// (req-tg-001), nor be included in the "all" expansion. agent-skills is the
// target the spec designates explicit-only. antigravity is explicit-only per
// the user decision of 2026-07-05, aligning with Python apm_cli's
// EXPLICIT_ONLY_TARGETS={"agent-skills","antigravity"}: its former detection
// signals (GEMINI.md/AGENTS.md) are cross-tool files also read by
// opencode/agent-skills tooling, so their presence must not auto-enable
// antigravity. Select it via --target antigravity (alias agy) or apm.yml
// target:. (This supersedes acceptance-checklist.md's earlier research note
// that had antigravity auto-detecting.)
var explicitOnlyTargets = map[string]bool{
	"agent-skills": true,
	"antigravity":  true,
}

func allAutoDetectableTargets() []string {
	return []string{"claude", "codex", "copilot", "opencode"}
}

// filterExplicitOnly removes targets that require explicit --target selection.
func filterExplicitOnly(targets []string) []string {
	var result []string
	for _, t := range targets {
		if !explicitOnlyTargets[t] {
			result = append(result, t)
		}
	}
	return result
}

func checkUnsupported(targets []string) []string {
	var diags []string
	for _, t := range targets {
		if _, ok := Adapters[t]; !ok {
			diags = append(diags, fmt.Sprintf("no registered handler for target %q", t))
		}
	}
	return diags
}

func filterSupported(targets []string) []string {
	var result []string
	for _, t := range targets {
		if _, ok := Adapters[t]; ok {
			result = append(result, t)
		}
	}
	return result
}

// deploySkill recursively copies a skill directory to .agents/skills/<name>/ (req-tg-003).
// Shared by all adapters.
func deploySkill(p Primitive, projectDir string) ([]string, error) {
	return deploySkillTo(p, projectDir, ".agents/skills")
}

// deploySkillTo recursively copies a skill directory to <root>/<name>/.
// Extracted from deploySkill so the claude adapter can additionally deploy
// to .claude/skills/ (Claude Code does not discover skills from the
// cross-tool .agents/skills/ canonical path -- see claude.go).
func deploySkillTo(p Primitive, projectDir, root string) ([]string, error) {
	destDir := path.Join(root, p.Name)
	absDestDir := filepath.Join(projectDir, filepath.FromSlash(destDir))
	if err := os.MkdirAll(absDestDir, 0755); err != nil {
		return nil, fmt.Errorf("create skill dir: %w", err)
	}

	var deployed []string
	err := copyDirRecursive(p.SrcPath, absDestDir, destDir, &deployed)
	if err != nil {
		return nil, fmt.Errorf("deploy skill %s: %w", p.Name, err)
	}
	return deployed, nil
}

func copyDirRecursive(srcDir, dstDir, relPrefix string, deployed *[]string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(srcDir, e.Name())
		dstPath := filepath.Join(dstDir, e.Name())
		relPath := path.Join(relPrefix, e.Name())
		if e.IsDir() {
			if err := os.MkdirAll(dstPath, 0755); err != nil {
				return err
			}
			if err := copyDirRecursive(srcPath, dstPath, relPath, deployed); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
			*deployed = append(*deployed, relPath)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func deployFileToPath(p Primitive, destPath, projectDir string) ([]string, error) {
	absDest := filepath.Join(projectDir, filepath.FromSlash(destPath))
	if err := os.MkdirAll(filepath.Dir(absDest), 0755); err != nil {
		return nil, err
	}
	if err := copyFile(p.SrcPath, absDest); err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	return []string{destPath}, nil
}
