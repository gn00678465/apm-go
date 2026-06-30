package deploy

import "fmt"

type copilotAdapter struct{}

func (a *copilotAdapter) Name() string { return "copilot" }

func (a *copilotAdapter) DeployRoots() []string { return []string{".github/", ".agents/"} }

func (a *copilotAdapter) SupportedTypes() []PrimitiveType {
	return []PrimitiveType{TypeInstructions, TypePrompts, TypeAgents, TypeSkills, TypeHooks}
}

func (a *copilotAdapter) DeployPrimitive(p Primitive, projectDir string) ([]string, error) {
	switch p.Type {
	case TypeSkills:
		return deploySkill(p, projectDir)
	case TypeInstructions:
		return deployFileToPath(p, fmt.Sprintf(".github/instructions/%s.instructions.md", p.Name), projectDir)
	case TypePrompts:
		return deployFileToPath(p, fmt.Sprintf(".github/prompts/%s.prompt.md", p.Name), projectDir)
	case TypeAgents:
		return deployFileToPath(p, fmt.Sprintf(".github/agents/%s.agent.md", p.Name), projectDir)
	case TypeHooks:
		return deployFileToPath(p, fmt.Sprintf(".github/hooks/%s.json", p.Name), projectDir)
	default:
		return nil, nil
	}
}
