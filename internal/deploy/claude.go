package deploy

import "fmt"

type claudeAdapter struct{}

func (a *claudeAdapter) Name() string { return "claude" }

func (a *claudeAdapter) DeployRoots() []string { return []string{".claude/"} }

// SupportedTypes omits hooks: claude hooks are merged into .claude/settings.json
// at compile time (deferred, alongside CLAUDE.md), not deployed as standalone files.
func (a *claudeAdapter) SupportedTypes() []PrimitiveType {
	return []PrimitiveType{TypeInstructions, TypeAgents, TypeSkills, TypeCommands, TypePrompts}
}

func (a *claudeAdapter) checkInstruction(p Primitive, deployDir string) error {
	return CheckDeployWrite(deployDir, claudeRulePath(p))
}

func claudeRulePath(p Primitive) string { return fmt.Sprintf(".claude/rules/%s.md", p.Name) }

func (a *claudeAdapter) commandPath(name string) string {
	return fmt.Sprintf(".claude/commands/%s.md", name)
}

// Skills deploy to claude's native .claude/skills/<name>/ ONLY -- not the
// cross-tool canonical .agents/skills/ (issue #10). The targets-matrix
// registry lists .claude/ as claude's sole deploy root and names claude a
// target-native exception to skill convergence, matching the Python
// reference implementation (integration/targets.py: claude's skills mapping
// has no deploy_root override). Claude Code only discovers skills under
// .claude/skills anyway, so the canonical copy served no tool and confused
// claude-only users with a stray .agents/ directory.
func (a *claudeAdapter) skillRoot(Primitive) string { return ".claude/skills" }

func (a *claudeAdapter) DeployPrimitive(p Primitive, projectDir string) ([]string, error) {
	switch p.Type {
	case TypeSkills:
		return deploySkillTo(p, projectDir, a.skillRoot(p))
	case TypeInstructions:
		return deployClaudeInstructions(p, projectDir, claudeRulePath(p))
	case TypeAgents:
		return deployFileToPath(p, fmt.Sprintf(".claude/agents/%s.md", p.Name), projectDir)
	case TypeCommands:
		return deployFileToPath(p, a.commandPath(p.Name), projectDir)
	case TypePrompts:
		files, _, err := deployPromptCommand(p, projectDir, a.commandPath(p.Name), a.Name())
		return files, err
	default:
		return nil, nil
	}
}
