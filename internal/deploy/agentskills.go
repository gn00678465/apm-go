package deploy

type agentSkillsAdapter struct{}

func (a *agentSkillsAdapter) Name() string { return "agent-skills" }

func (a *agentSkillsAdapter) DeployRoots() []string { return []string{".agents/"} }

func (a *agentSkillsAdapter) SupportedTypes() []PrimitiveType {
	return []PrimitiveType{TypeSkills}
}

func (a *agentSkillsAdapter) DeployPrimitive(p Primitive, projectDir string) ([]string, error) {
	if p.Type == TypeSkills {
		return deploySkill(p, projectDir)
	}
	return nil, nil
}
