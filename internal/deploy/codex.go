package deploy

import "fmt"

type codexAdapter struct{}

func (a *codexAdapter) Name() string { return "codex" }

func (a *codexAdapter) DeployRoots() []string { return []string{".codex/", ".agents/"} }

func (a *codexAdapter) SupportedTypes() []PrimitiveType {
	return []PrimitiveType{TypeAgents, TypeSkills}
}

func (a *codexAdapter) DeployPrimitive(p Primitive, projectDir string) ([]string, error) {
	switch p.Type {
	case TypeSkills:
		return deploySkill(p, projectDir)
	case TypeAgents:
		return deployFileToPath(p, fmt.Sprintf(".codex/agents/%s.toml", p.Name), projectDir)
	default:
		return nil, nil
	}
}
