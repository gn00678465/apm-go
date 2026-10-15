package deploy

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/archive"
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
	prims, _ := collectFromAPMDir(sourceTree{dir: projectDir}, ".apm")
	return prims
}

// sourceTree is a directory primitives are collected from. The project's own
// content is read as it is. A dependency is read the way a remote package
// must be (issue #89): a symlink in it is resolved, and one that leaves the
// package, dangles or loops is an error, so nothing outside the package is
// ever read through it.
type sourceTree struct {
	dir    string // as the caller names it
	depKey string // "" for the project's own content
}

// dependencyTree returns the tree of the dependency vendored at modulePath,
// and false when there is nothing there to read.
func dependencyTree(depKey, modulePath string) (sourceTree, bool) {
	if _, err := archive.ResolveIn(modulePath, "."); err != nil {
		return sourceTree{}, false
	}
	return sourceTree{dir: modulePath, depKey: depKey}, true
}

func (t sourceTree) source() string {
	if t.depKey == "" {
		return "local"
	}
	return "dependency:" + t.depKey
}

// path returns where rel, slash-separated below the tree, is read from. For a
// dependency it is rel with its symlinks resolved, joined to dir: a package
// without symlinks is read at the same paths as the project's own content.
func (t sourceTree) path(rel string) (string, error) {
	if t.depKey == "" {
		return filepath.Join(t.dir, filepath.FromSlash(rel)), nil
	}
	resolved, err := archive.ResolveIn(t.dir, rel)
	if err != nil {
		return "", fmt.Errorf("dependency %s: %w", t.depKey, err)
	}
	return filepath.Join(t.dir, filepath.FromSlash(resolved)), nil
}

// entry returns where the entry e of directory rel, listed at dir, is read
// from, and whether it is a directory. The project's own symlinks are taken
// as they are, as before issue #89.
func (t sourceTree) entry(dir, rel string, e fs.DirEntry) (string, bool, error) {
	if t.depKey == "" || e.Type()&fs.ModeSymlink == 0 {
		return filepath.Join(dir, e.Name()), e.IsDir(), nil
	}
	p, err := t.path(rel + "/" + e.Name())
	if err != nil {
		return "", false, err
	}
	info, err := os.Stat(p)
	return p, err == nil && info.IsDir(), nil
}

// hasSkillMD reports whether the directory rel holds the SKILL.md that makes
// it a skill. For a dependency the marker is resolved first: a dangling or
// looping symlink there is an error, not a directory without a skill.
func (t sourceTree) hasSkillMD(rel string) (bool, error) {
	p, err := t.path(rel + "/SKILL.md")
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	return err == nil, nil
}

// checkSkill fails for a dependency skill directory holding a symlink that a
// copy of it must not follow; the deploy copies, or links, the whole tree.
func (t sourceTree) checkSkill(rel string) error {
	if t.depKey == "" {
		return nil
	}
	if err := archive.CheckTree(t.dir, rel); err != nil {
		return fmt.Errorf("dependency %s: %w", t.depKey, err)
	}
	return nil
}

// CollectDependencyPrimitives returns the primitives of the dependency
// vendored at modulePath. The error is a symlink in it that leaves the
// package, dangles or loops, found before anything is deployed.
func CollectDependencyPrimitives(depKey, modulePath string) ([]Primitive, error) {
	t, ok := dependencyTree(depKey, modulePath)
	if !ok {
		return nil, nil
	}
	return collectDependency(t)
}

func collectDependency(t sourceTree) ([]Primitive, error) {
	prims, err := collectFromAPMDir(t, ".apm")
	if err != nil {
		return nil, err
	}

	// Skill bundle: SKILL.md at package root
	skillMD, err := t.path("SKILL.md")
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(skillMD); err == nil {
		if err := t.checkSkill("."); err != nil {
			return nil, err
		}
		name := skillNameFromDepKey(t.depKey)
		prims = append(prims, Primitive{
			Name:    name,
			Type:    TypeSkills,
			Source:  t.source(),
			DepKey:  t.depKey,
			SrcPath: t.dir,
		})
	}

	// Claude-plugin manifest (.claude-plugin/plugin.json): when present and
	// valid, its skills/agents/commands arrays are the authoritative source
	// for this module. An explicitly-declared "skills" key (even an empty
	// array) takes precedence over the legacy single-level scan below, so a
	// nested layout like skills/<category>/<name>/ (which that scan can't
	// see one level deep) is discovered correctly instead of silently
	// deploying nothing.
	pluginPrims, skillsDeclared, err := collectPluginPrimitives(t)
	if err != nil {
		return nil, err
	}
	prims = append(prims, pluginPrims...)
	if skillsDeclared {
		return prims, nil
	}

	// Skill collection: skills/<name>/SKILL.md
	skills, err := collectSkills(t, "skills")
	if err != nil {
		return nil, err
	}
	return append(prims, skills...), nil
}

// collectFromAPMDir collects the primitives of the .apm directory apmRel of t.
func collectFromAPMDir(t sourceTree, apmRel string) ([]Primitive, error) {
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
		rel := apmRel + "/" + tm.subdir
		dir, err := t.path(rel)
		if err != nil {
			return nil, err
		}
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
			src, isDir, err := t.entry(dir, rel, e)
			if err != nil {
				return nil, err
			}
			if isDir {
				continue
			}
			prims = append(prims, Primitive{
				Name:    name,
				Type:    tm.primType,
				Source:  t.source(),
				DepKey:  t.depKey,
				SrcPath: src,
			})
		}
	}

	// Skills: .apm/skills/<name>/SKILL.md
	skills, err := collectSkills(t, apmRel+"/skills")
	if err != nil {
		return nil, err
	}
	return append(prims, skills...), nil
}

// collectSkills collects the skills <rel>/<name>/SKILL.md of t.
func collectSkills(t sourceTree, rel string) ([]Primitive, error) {
	skillsDir, err := t.path(rel)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, nil
	}
	var prims []Primitive
	for _, e := range entries {
		src, isDir, err := t.entry(skillsDir, rel, e)
		if err != nil {
			return nil, err
		}
		if !isDir {
			continue
		}
		if ok, err := t.hasSkillMD(rel + "/" + e.Name()); !ok {
			if err != nil {
				return nil, err
			}
			continue
		}
		if err := t.checkSkill(rel + "/" + e.Name()); err != nil {
			return nil, err
		}
		prims = append(prims, Primitive{
			Name:    e.Name(),
			Type:    TypeSkills,
			Source:  t.source(),
			DepKey:  t.depKey,
			SrcPath: src,
		})
	}
	return prims, nil
}

// extractInstructionName only accepts *.instructions.md: the
// .instructions.md naming and the applyTo frontmatter are one contract
// (Python find_instruction_files globs *.instructions.md exclusively), so
// plain .md files in .apm/instructions/ are NOT collected.
func extractInstructionName(filename string) string {
	if strings.HasSuffix(filename, ".instructions.md") {
		return strings.TrimSuffix(filename, ".instructions.md")
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
