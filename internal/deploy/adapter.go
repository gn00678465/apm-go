package deploy

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

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

// ResolveTargets determines active targets by priority:
// 1. --target flag (explicit CLI)
// 2. manifest target: field
// 3. auto-detection from filesystem signals
// Returns empty if nothing detected (no-deploy).
func ResolveTargets(flagTarget string, manifestTargets []string, projectDir string) ([]string, []string) {
	var diags []string

	if flagTarget != "" {
		targets := []string{flagTarget}
		if flagTarget == "all" {
			targets = allAutoDetectableTargets()
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

// explicitOnlyTargets must never be activated by auto-detection (req-tg-001).
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
	destDir := path.Join(".agents/skills", p.Name)
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
