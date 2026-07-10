package deploy

import "fmt"

type antigravityAdapter struct{}

func (a *antigravityAdapter) Name() string { return "antigravity" }

func (a *antigravityAdapter) DeployRoots() []string { return []string{".agents/"} }

func (a *antigravityAdapter) SupportedTypes() []PrimitiveType {
	return []PrimitiveType{TypeInstructions, TypeAgents, TypeSkills, TypeHooks}
}

func (a *antigravityAdapter) DeployPrimitive(p Primitive, projectDir string) ([]string, error) {
	switch p.Type {
	case TypeSkills:
		return deploySkill(p, projectDir)
	case TypeInstructions:
		return deployFileToPath(p, fmt.Sprintf(".agents/rules/%s.md", p.Name), projectDir)
	case TypeAgents:
		// Static custom-agent format of Antigravity CLI >=1.0.16
		// (research/cli-subagents.md): one directory per agent, named after
		// the agent, containing a file always called agent.md -- unlike
		// claude's flat .claude/agents/<name>.md. Byte-copy, no frontmatter
		// transform (adapter-wide convention). This mapping is an apm-go
		// documented extension: the Python upstream has no antigravity
		// agents mapping (prd.md decision 2026-07-10).
		return deployFileToPath(p, fmt.Sprintf(".agents/agents/%s/agent.md", p.Name), projectDir)
	case TypeHooks:
		return deployFileToPath(p, ".agents/hooks.json", projectDir)
	default:
		return nil, nil
	}
}
