package deploy

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/manifest"
)

type PrimitiveType string

const (
	TypeInstructions PrimitiveType = "instructions"
	TypeAgents       PrimitiveType = "agents"
	TypeSkills       PrimitiveType = "skills"
	TypeCommands     PrimitiveType = "commands"
	TypeHooks        PrimitiveType = "hooks"
	TypePrompts      PrimitiveType = "prompts"
	TypeMCP          PrimitiveType = "mcp"
)

type Primitive struct {
	Name    string
	Type    PrimitiveType
	Source  string                  // "local" or "dependency:<key>"
	DepKey  string                  // dependency unique key ("" for local)
	SrcPath string                  // absolute path to source file or dir (for skills)
	MCP     *manifest.MCPDependency // set only when Type == TypeMCP
}

func CollectLocalPrimitives(projectDir string) []Primitive {
	apmDir := filepath.Join(projectDir, ".apm")
	return collectFromAPMDir(apmDir, "local", "")
}

func CollectDependencyPrimitives(depKey, modulePath string) []Primitive {
	apmDir := filepath.Join(modulePath, ".apm")
	prims := collectFromAPMDir(apmDir, "dependency:"+depKey, depKey)

	// Skill bundle: SKILL.md at package root
	skillMD := filepath.Join(modulePath, "SKILL.md")
	if _, err := os.Stat(skillMD); err == nil {
		name := skillNameFromDepKey(depKey)
		prims = append(prims, Primitive{
			Name:    name,
			Type:    TypeSkills,
			Source:  "dependency:" + depKey,
			DepKey:  depKey,
			SrcPath: modulePath,
		})
	}

	// Claude-plugin manifest (.claude-plugin/plugin.json): when present and
	// valid, its skills/agents/commands arrays are the authoritative source
	// for this module. An explicitly-declared "skills" key (even an empty
	// array) takes precedence over the legacy single-level scan below, so a
	// nested layout like skills/<category>/<name>/ (which that scan can't
	// see one level deep) is discovered correctly instead of silently
	// deploying nothing.
	pluginPrims, skillsDeclared := collectPluginPrimitives(depKey, modulePath)
	prims = append(prims, pluginPrims...)
	if skillsDeclared {
		return prims
	}

	// Skill collection: skills/<name>/SKILL.md
	skillsDir := filepath.Join(modulePath, "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			sm := filepath.Join(skillsDir, e.Name(), "SKILL.md")
			if _, err := os.Stat(sm); err == nil {
				prims = append(prims, Primitive{
					Name:    e.Name(),
					Type:    TypeSkills,
					Source:  "dependency:" + depKey,
					DepKey:  depKey,
					SrcPath: filepath.Join(skillsDir, e.Name()),
				})
			}
		}
	}

	return prims
}

func collectFromAPMDir(apmDir, source, depKey string) []Primitive {
	var prims []Primitive

	typeMap := []struct {
		subdir    string
		primType  PrimitiveType
		extractFn func(string) string
	}{
		{"instructions", TypeInstructions, extractInstructionName},
		{"agents", TypeAgents, extractAgentName},
		{"commands", TypeCommands, extractBaseName},
		{"hooks", TypeHooks, extractHookName},
		{"prompts", TypePrompts, extractPromptName},
	}

	for _, tm := range typeMap {
		dir := filepath.Join(apmDir, tm.subdir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := tm.extractFn(e.Name())
			if name == "" {
				continue
			}
			prims = append(prims, Primitive{
				Name:    name,
				Type:    tm.primType,
				Source:  source,
				DepKey:  depKey,
				SrcPath: filepath.Join(dir, e.Name()),
			})
		}
	}

	// Skills: .apm/skills/<name>/SKILL.md
	skillsDir := filepath.Join(apmDir, "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			sm := filepath.Join(skillsDir, e.Name(), "SKILL.md")
			if _, err := os.Stat(sm); err == nil {
				prims = append(prims, Primitive{
					Name:    e.Name(),
					Type:    TypeSkills,
					Source:  source,
					DepKey:  depKey,
					SrcPath: filepath.Join(skillsDir, e.Name()),
				})
			}
		}
	}

	return prims
}

func extractInstructionName(filename string) string {
	if strings.HasSuffix(filename, ".instructions.md") {
		return strings.TrimSuffix(filename, ".instructions.md")
	}
	if strings.HasSuffix(filename, ".md") {
		return strings.TrimSuffix(filename, ".md")
	}
	return ""
}

func extractAgentName(filename string) string {
	if strings.HasSuffix(filename, ".agent.md") {
		return strings.TrimSuffix(filename, ".agent.md")
	}
	if strings.HasSuffix(filename, ".md") {
		return strings.TrimSuffix(filename, ".md")
	}
	return ""
}

func extractBaseName(filename string) string {
	if strings.HasSuffix(filename, ".md") {
		return strings.TrimSuffix(filename, ".md")
	}
	return ""
}

func extractHookName(filename string) string {
	if strings.HasSuffix(filename, ".json") {
		return strings.TrimSuffix(filename, ".json")
	}
	return ""
}

func extractPromptName(filename string) string {
	if strings.HasSuffix(filename, ".prompt.md") {
		return strings.TrimSuffix(filename, ".prompt.md")
	}
	if strings.HasSuffix(filename, ".md") {
		return strings.TrimSuffix(filename, ".md")
	}
	return ""
}

func skillNameFromDepKey(depKey string) string {
	parts := strings.Split(depKey, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}
	return depKey
}
