package deploy

import "fmt"

type opencodeAdapter struct{}

func (a *opencodeAdapter) Name() string { return "opencode" }

func (a *opencodeAdapter) DeployRoots() []string { return []string{".opencode/", ".agents/"} }

func (a *opencodeAdapter) SupportedTypes() []PrimitiveType {
	return []PrimitiveType{TypeAgents, TypeCommands, TypeSkills}
}

func (a *opencodeAdapter) skillRoot(Primitive) string { return sharedSkillRoot }

func (a *opencodeAdapter) DeployPrimitive(p Primitive, projectDir string) ([]string, error) {
	switch p.Type {
	case TypeSkills:
		return deploySkillTo(p, projectDir, a.skillRoot(p))
	case TypeAgents:
		return deployFileToPath(p, fmt.Sprintf(".opencode/agents/%s.md", p.Name), projectDir)
	case TypeCommands:
		return deployFileToPath(p, fmt.Sprintf(".opencode/commands/%s.md", p.Name), projectDir)
	default:
		return nil, nil
	}
}
