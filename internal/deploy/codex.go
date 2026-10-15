package deploy

type codexAdapter struct{}

func (a *codexAdapter) Name() string { return "codex" }

func (a *codexAdapter) DeployRoots() []string { return []string{".codex/", ".agents/"} }

func (a *codexAdapter) SupportedTypes() []PrimitiveType {
	return []PrimitiveType{TypeAgents, TypeSkills, TypeHooks}
}

func (a *codexAdapter) skillRoot(Primitive) string { return sharedSkillRoot }

func (a *codexAdapter) DeployPrimitive(p Primitive, projectDir string) ([]string, error) {
	switch p.Type {
	case TypeSkills:
		return deploySkillTo(p, projectDir, a.skillRoot(p))
	case TypeAgents:
		return deployCodexAgentTOML(p, projectDir)
	case TypeHooks:
		return deployFileToPath(p, ".codex/hooks.json", projectDir)
	default:
		return nil, nil
	}
}
