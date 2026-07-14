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
		return deploySkillClaude(p, projectDir)
	case TypeInstructions:
		return deployClaudeInstructions(p, projectDir)
	case TypeAgents:
		return deployFileToPath(p, fmt.Sprintf(".claude/agents/%s.md", p.Name), projectDir)
	case TypeCommands:
		return deployFileToPath(p, fmt.Sprintf(".claude/commands/%s.md", p.Name), projectDir)
	default:
		return nil, nil
	}
}

// deploySkillClaude deploys a skill to the canonical cross-tool path
// (.agents/skills/<name>/, req-tg-003) and additionally to
// .claude/skills/<name>/. Claude Code itself only discovers skills under
// .claude/skills, not .agents/skills, so the canonical-only deployment left
// skills invisible to Claude Code even though req-tg-003 was satisfied.
func deploySkillClaude(p Primitive, projectDir string) ([]string, error) {
	canonical, err := deploySkill(p, projectDir)
	if err != nil {
		return nil, err
	}
	extra, err := deploySkillTo(p, projectDir, ".claude/skills")
	if err != nil {
		return nil, err
	}
	return append(canonical, extra...), nil
}
