package deploy

import (
	"fmt"
	"path"
)

type antigravityAdapter struct{}

func (a *antigravityAdapter) Name() string { return "antigravity" }

func (a *antigravityAdapter) DeployRoots() []string { return []string{".agents/"} }

func (a *antigravityAdapter) SupportedTypes() []PrimitiveType {
	return []PrimitiveType{TypeInstructions, TypeAgents, TypeSkills, TypeHooks}
}

// DeployPrimitive routes local primitives (p.DepKey == "") to the flat,
// shared workspace paths as before. Dependency primitives (p.DepKey != "")
// instead land under that dependency's plugin bundle directory,
// .agents/plugins/<pkg>/... (antigravity_bundle.go), so each package's
// hooks.json is isolated from every other package's -- the "documented
// extension" this task adds ahead of the Python upstream, which has no
// plugin bundle concept (task 07-11-antigravity-plugins-bundle).
func (a *antigravityAdapter) DeployPrimitive(p Primitive, projectDir string) ([]string, error) {
	switch p.Type {
	case TypeSkills:
		if p.DepKey == "" {
			return deploySkill(p, projectDir)
		}
		return deploySkillTo(p, projectDir, path.Join(antigravityBundleDir(p.DepKey), "skills"))
	case TypeInstructions:
		if p.DepKey == "" {
			return deployFileToPath(p, fmt.Sprintf(".agents/rules/%s.md", p.Name), projectDir)
		}
		return deployFileToPath(p, path.Join(antigravityBundleDir(p.DepKey), "rules", p.Name+".md"), projectDir)
	case TypeAgents:
		// Static custom-agent format of Antigravity CLI >=1.0.16
		// (research/cli-subagents.md): one directory per agent, named after
		// the agent, containing a file always called agent.md -- unlike
		// claude's flat .claude/agents/<name>.md. Byte-copy, no frontmatter
		// transform (adapter-wide convention). This mapping is an apm-go
		// documented extension: the Python upstream has no antigravity
		// agents mapping (prd.md decision 2026-07-10).
		if p.DepKey == "" {
			return deployFileToPath(p, fmt.Sprintf(".agents/agents/%s/agent.md", p.Name), projectDir)
		}
		return deployFileToPath(p, path.Join(antigravityBundleDir(p.DepKey), "agents", p.Name, "agent.md"), projectDir)
	case TypeHooks:
		if p.DepKey == "" {
			return deployFileToPath(p, ".agents/hooks.json", projectDir)
		}
		return deployFileToPath(p, path.Join(antigravityBundleDir(p.DepKey), "hooks.json"), projectDir)
	default:
		return nil, nil
	}
}
