package deploy

import "fmt"

type opencodeAdapter struct{}

func (a *opencodeAdapter) Name() string { return "opencode" }

func (a *opencodeAdapter) DeployRoots() []string { return []string{".opencode/", ".agents/"} }

func (a *opencodeAdapter) SupportedTypes() []PrimitiveType {
	return []PrimitiveType{TypeAgents, TypeCommands, TypeSkills, TypePrompts}
}

func (a *opencodeAdapter) commandPath(name string) string {
	return fmt.Sprintf("%s/commands/%s.md", opencodeRoot(), name)
}

// skillRoot is .agents/skills in a project and opencode's own skills
// directory at user scope (user_primitive_overrides, integration/
// targets.py:640-700).
func (a *opencodeAdapter) skillRoot(Primitive) string {
	if activeDeployMode.global {
		return opencodeRoot() + "/skills"
	}
	return sharedSkillRoot
}

// opencodeRoot is the directory opencode's files go to: .opencode in a
// project, ~/.config/opencode at user scope (user_root_dir, integration/
// targets.py:640-700).
func opencodeRoot() string {
	if activeDeployMode.global {
		return ".config/opencode"
	}
	return ".opencode"
}

func (a *opencodeAdapter) DeployPrimitive(p Primitive, projectDir string) ([]string, error) {
	switch p.Type {
	case TypeSkills:
		return deploySkillTo(p, projectDir, a.skillRoot(p))
	case TypeAgents:
		return deployFileToPath(p, fmt.Sprintf("%s/agents/%s.md", opencodeRoot(), p.Name), projectDir)
	case TypeCommands:
		return deployFileToPath(p, a.commandPath(p.Name), projectDir)
	case TypePrompts:
		files, _, err := deployPromptCommand(p, projectDir, a.commandPath(p.Name), a.Name())
		return files, err
	default:
		return nil, nil
	}
}
