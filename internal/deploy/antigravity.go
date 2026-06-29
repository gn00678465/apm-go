package deploy

import "fmt"

type antigravityAdapter struct{}

func (a *antigravityAdapter) Name() string { return "antigravity" }

func (a *antigravityAdapter) DeployRoots() []string { return []string{".agents/"} }

func (a *antigravityAdapter) SupportedTypes() []PrimitiveType {
	return []PrimitiveType{TypeInstructions, TypeSkills, TypeHooks}
}

func (a *antigravityAdapter) DeployPrimitive(p Primitive, projectDir string) ([]string, error) {
	switch p.Type {
	case TypeSkills:
		return deploySkill(p, projectDir)
	case TypeInstructions:
		return deployFileToPath(p, fmt.Sprintf(".agents/rules/%s.md", p.Name), projectDir)
	case TypeHooks:
		return deployFileToPath(p, ".agents/hooks.json", projectDir)
	default:
		return nil, nil
	}
}
