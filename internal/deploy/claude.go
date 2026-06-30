package deploy

import "fmt"

type claudeAdapter struct{}

func (a *claudeAdapter) Name() string { return "claude" }

func (a *claudeAdapter) DeployRoots() []string { return []string{".claude/", ".agents/"} }

// SupportedTypes omits hooks: claude hooks are merged into .claude/settings.json
// at compile time (deferred, alongside CLAUDE.md), not deployed as standalone files.
func (a *claudeAdapter) SupportedTypes() []PrimitiveType {
	return []PrimitiveType{TypeInstructions, TypeAgents, TypeSkills, TypeCommands}
}

func (a *claudeAdapter) DeployPrimitive(p Primitive, projectDir string) ([]string, error) {
	switch p.Type {
	case TypeSkills:
		return deploySkill(p, projectDir)
	case TypeInstructions:
		return deployFileToPath(p, fmt.Sprintf(".claude/rules/%s.md", p.Name), projectDir)
	case TypeAgents:
		return deployFileToPath(p, fmt.Sprintf(".claude/agents/%s.md", p.Name), projectDir)
	case TypeCommands:
		return deployFileToPath(p, fmt.Sprintf(".claude/commands/%s.md", p.Name), projectDir)
	default:
		return nil, nil
	}
}
