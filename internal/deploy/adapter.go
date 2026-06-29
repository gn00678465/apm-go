package deploy

import (
	"fmt"
	"os"

	"github.com/apm-go/apm/internal/manifest"
)

type TargetAdapter interface {
	Name() string
	DeployRoots() []string
	SupportedTypes() []PrimitiveType
	DeployPrimitive(p Primitive, projectDir string) ([]string, error)
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

// deploySkill deploys a skill directory to .agents/skills/<name>/SKILL.md (req-tg-003).
// Shared by all adapters.
func deploySkill(p Primitive, projectDir string) ([]string, error) {
	destDir := fmt.Sprintf(".agents/skills/%s", p.Name)
	destPath := destDir + "/SKILL.md"
	absDestDir := joinPath(projectDir, destDir)
	if err := os.MkdirAll(absDestDir, 0755); err != nil {
		return nil, fmt.Errorf("create skill dir: %w", err)
	}

	srcSkillMD := joinPath(p.SrcPath, "SKILL.md")
	if err := copyFile(srcSkillMD, joinPath(projectDir, destPath)); err != nil {
		return nil, fmt.Errorf("deploy skill %s: %w", p.Name, err)
	}
	return []string{destPath}, nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func deployFileToPath(p Primitive, destPath, projectDir string) ([]string, error) {
	absDest := joinPath(projectDir, destPath)
	if err := os.MkdirAll(joinDir(absDest), 0755); err != nil {
		return nil, err
	}
	if err := copyFile(p.SrcPath, absDest); err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	return []string{destPath}, nil
}

func joinPath(base, rel string) string {
	return base + "/" + rel
}

func joinDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}
