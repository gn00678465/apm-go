package deploy

type agentSkillsAdapter struct{}

func (a *agentSkillsAdapter) Name() string { return "agent-skills" }

func (a *agentSkillsAdapter) DeployRoots() []string { return []string{".agents/"} }

func (a *agentSkillsAdapter) SupportedTypes() []PrimitiveType {
	return []PrimitiveType{TypeSkills}
}

func (a *agentSkillsAdapter) skillRoot(Primitive) string { return sharedSkillRoot }

func (a *agentSkillsAdapter) DeployPrimitive(p Primitive, projectDir string) ([]string, error) {
	if p.Type == TypeSkills {
		return deploySkillTo(p, projectDir, a.skillRoot(p))
	}
	return nil, nil
}
