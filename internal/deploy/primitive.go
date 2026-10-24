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
	// SkipRootGit marks a skill whose SrcPath is the root of a cloned
	// package: the .git there is the checkout's, not the skill's.
	SkipRootGit bool
	// RootSkill marks the skill of a package whose root holds SKILL.md.
	RootSkill bool
	// PluginRoot marks the root skill of a Claude plugin, whose apm.yml
	// the deploy leaves out (skillCopyRules).
	PluginRoot bool
	// Origin is where the Oracle reads p from when it rewrites p's links:
	// SrcPath with no symlink of the package resolved, since the Oracle's
	// apm_modules holds none (link/g1out), and for a Claude plugin's agent
	// or command the place it stages it in (deps/plugin_parser.py:
	// 1232-1296). "" is SrcPath.
	Origin string
	// PackageDir is the directory of the dependency p comes from, below
	// apm_modules: the key's, or its alias's. "" for local content.
	PackageDir string
}

func (p Primitive) origin() string {
	if p.Origin != "" {
		return p.Origin
	}
	return p.SrcPath
}

// CollectLocalPrimitives returns the primitives of the project's own content
// at projectDir.
func CollectLocalPrimitives(projectDir string) []Primitive {
	return CollectScopeLocalPrimitives(projectDir, false)
}

// CollectScopeLocalPrimitives reads the own content a deploy of that scope
// deploys: the project's .apm/ and the prompts, agents and hooks beside it,
// which the Oracle reads only when the project has a .apm/ directory
// (install/phases/local_content.py:44-56); for global, ~/.apm/.apm/ alone
// (#88 item 68).
func CollectScopeLocalPrimitives(projectDir string, global bool) []Primitive {
	root := !global
	if info, err := os.Stat(filepath.Join(projectDir, ".apm")); err != nil || !info.IsDir() {
		root = false
	}
	prims, _ := collectFromAPMDir(sourceTree{dir: projectDir}, ".apm", root, false)
	return prims
}

// sourceTree is a directory primitives are collected from. The project's own
// content is read as it is. A dependency is read the way a remote package
// must be (issue #89): a symlink in it is resolved, and one that leaves the
// package, dangles or loops is an error, so nothing outside the package is
// ever read through it.
type sourceTree struct {
	dir      string // as the caller names it
	depKey   string // "" for the project's own content
	registry bool   // a registry package: its archive's .git is its content
}

// dependencyTree returns the tree of the dependency vendored at modulePath,
// and false when there is nothing there to read.
func dependencyTree(depKey, modulePath string) (sourceTree, bool) {
	if _, err := archive.ResolveIn(modulePath, "."); err != nil {
		return sourceTree{}, false
	}
	return sourceTree{dir: modulePath, depKey: depKey}, true
}

// cloned reports whether t is a package apm-go took with git. The Oracle
// drops the .git of a cloned package's root when it puts it in apm_modules
// (deps/github_downloader.py:1923-1925,2015-2018), and keeps it for a local
// package, which it copies (key _local/<name>), and for a registry package,
// whose archive it extracts whole (deps/registry/extractor.py:151-170).
// apm-go keeps the checkout's .git there for resolved_commit and
// tree_sha256, so a skill read from the root leaves it out instead.
func (t sourceTree) cloned() bool {
	return t.depKey != "" && !t.registry && !strings.HasPrefix(t.depKey, "_local/")
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

// logical is rel, slash-separated below the tree, joined to dir with no
// symlink resolved.
func (t sourceTree) logical(rel string) string {
	return filepath.Join(t.dir, filepath.FromSlash(rel))
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

// collectDependency returns the primitives of the package t, each with t's
// directory as its PackageDir.
func collectDependency(t sourceTree) ([]Primitive, error) {
	prims, err := collectPackage(t)
	for i := range prims {
		prims[i].PackageDir = t.dir
	}
	return prims, err
}

func collectPackage(t sourceTree) ([]Primitive, error) {
	m, manifestErr := loadPluginManifest(t)
	staged := m != nil
	prims, err := collectFromAPMDir(t, ".apm", true, staged)
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
		// The root skill is named after the package's directory below
		// apm_modules, an alias when there is one (install_path.name,
		// integration/skill_integrator.py:1769).
		prims = append(prims, Primitive{
			Name:        filepath.Base(t.dir),
			Type:        TypeSkills,
			Source:      t.source(),
			DepKey:      t.depKey,
			SrcPath:     t.dir,
			SkipRootGit: t.cloned(),
			RootSkill:   true,
			PluginRoot:  staged,
		})
	}

	// Plugin manifest (findPluginManifest): when present and
	// valid, its skills/agents/commands arrays are the authoritative source
	// for this module. An explicitly-declared "skills" key (even an empty
	// array) takes precedence over the legacy single-level scan below, so a
	// nested layout like skills/<category>/<name>/ (which that scan can't
	// see one level deep) is discovered correctly instead of silently
	// deploying nothing.
	if manifestErr != nil {
		return nil, manifestErr
	}
	pluginPrims, skillsDeclared, err := collectPluginPrimitives(t, m)
	if err != nil {
		return nil, err
	}
	prims = append(prims, withoutRepeatedSkills(prims, pluginPrims)...)
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

// collectFromAPMDir collects the primitives of the .apm directory apmRel of
// t, and with root the prompts, agents and hooks at t's root too. With staged
// it leaves out apmRel's agents and prompts, which a Claude plugin's
// collection reads with the plugin's own (pluginStage).
func collectFromAPMDir(t sourceTree, apmRel string, root, staged bool) ([]Primitive, error) {
	type source struct {
		rel       string
		primType  PrimitiveType
		extractFn func(string) string
	}
	// Each directory as the Oracle's finders read it, not recursive and in
	// name order: an agent at the root before .apm/agents
	// (find_agent_files, integration/agent_integrator.py:71-113), and a hook
	// of .apm/hooks before hooks/ (find_hook_files,
	// integration/hook_integrator.py:449-488). A Claude plugin's root agents
	// are not staged; they come before the staged ones.
	var sources []source
	sources = append(sources, source{apmRel + "/instructions", TypeInstructions, extractInstructionName})
	if root {
		sources = append(sources, source{".", TypeAgents, extractRootAgentName})
	}
	if !staged {
		sources = append(sources, source{apmRel + "/agents", TypeAgents, extractAgentName})
	}
	sources = append(sources,
		source{apmRel + "/commands", TypeCommands, extractBaseName},
		source{apmRel + "/hooks", TypeHooks, extractHookName})
	if root {
		sources = append(sources, source{"hooks", TypeHooks, extractHookName})
	}
	var prims []Primitive
	hookStems := map[string]bool{}
	for _, src := range sources {
		found, err := collectFlat(t, src.rel, src.primType, src.extractFn)
		if err != nil {
			return nil, err
		}
		for _, p := range found {
			if p.Type == TypeHooks {
				// find_hook_files keeps one hook per stem.lower().
				if hookStems[strings.ToLower(p.Name)] {
					continue
				}
				hookStems[strings.ToLower(p.Name)] = true
			}
			prims = append(prims, p)
		}
	}

	// Prompts as the Oracle finds them: find_files_by_glob(root,
	// "*.prompt.md", subdirs=[".apm/prompts"]) (base_integrator.py:1031-1080),
	// the root's first, each directory in name order, not recursive.
	var promptDirs []string
	if root {
		promptDirs = append(promptDirs, ".")
	}
	if !staged {
		promptDirs = append(promptDirs, apmRel+"/prompts")
	}
	for _, rel := range promptDirs {
		found, err := collectFlat(t, rel, TypePrompts, extractPromptName)
		if err != nil {
			return nil, err
		}
		for _, p := range found {
			// A workflow belongs to the Copilot App, which apm-go does not
			// deploy to; every other target skips it (command_integrator.py:
			// 524-538, prompt_integrator.py:288).
			if !isWorkflowPrompt(p.SrcPath) {
				prims = append(prims, p)
			}
		}
	}

	// Skills: .apm/skills/<name>/SKILL.md
	skills, err := collectSkills(t, apmRel+"/skills")
	if err != nil {
		return nil, err
	}
	return append(prims, skills...), nil
}

// collectFlat collects the files of the directory rel of t that extractFn
// names, as primitives of type pt.
func collectFlat(t sourceTree, rel string, pt PrimitiveType, extractFn func(string) string) ([]Primitive, error) {
	var prims []Primitive
	err := eachFlatFile(t, rel, extractFn, func(file, src string) {
		prims = append(prims, Primitive{
			Name:    extractFn(file),
			Type:    pt,
			Source:  t.source(),
			DepKey:  t.depKey,
			SrcPath: src,
			Origin:  t.logical(rel + "/" + file),
		})
	})
	return prims, err
}

// eachFlatFile calls visit with the name and source of each file of the
// directory rel of t that extractFn names.
func eachFlatFile(t sourceTree, rel string, extractFn func(string) string, visit func(file, src string)) error {
	dir, err := t.path(rel)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() || extractFn(e.Name()) == "" {
			continue
		}
		src, isDir, err := t.entry(dir, rel, e)
		if err != nil {
			return err
		}
		if !isDir {
			visit(e.Name(), src)
		}
	}
	return nil
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
			Origin:  t.logical(rel + "/" + e.Name()),
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

// extractRootAgentName only accepts *.agent.md, the glob of an agent at a
// package's root (integration/agent_integrator.py:93), which Path.glob
// matches without regard to case on Windows. Of what that glob finds, the
// Oracle takes a name ending in .md as written for an agent and skips any
// other (_is_markdown, install/primitive_classification.py:214-215), and
// names one not ending in .agent.md as written by its stem
// (agent_integrator.py:136): Y.Agent.md is the agent Y.Agent, X.AGENT.MD none.
func extractRootAgentName(filename string) string {
	if strings.HasSuffix(filename, ".agent.md") {
		return strings.TrimSuffix(filename, ".agent.md")
	}
	if globSuffix(filename, ".agent.md") && strings.HasSuffix(filename, ".md") {
		return strings.TrimSuffix(filename, ".md")
	}
	return ""
}

// globSuffix is whether name ends in suffix as Python 3.12's Path.glob
// matches a pattern: without regard to case on Windows.
func globSuffix(name, suffix string) bool {
	if !windowsPaths {
		return strings.HasSuffix(name, suffix)
	}
	return len(name) >= len(suffix) && strings.EqualFold(name[len(name)-len(suffix):], suffix)
}

func extractBaseName(filename string) string {
	if strings.HasSuffix(filename, ".md") {
		return strings.TrimSuffix(filename, ".md")
	}
	return ""
}

// extractHookName accepts *.json, the glob of find_hook_files
// (integration/hook_integrator.py:449-488), and names the hook by the stem
// as written.
func extractHookName(filename string) string {
	if globSuffix(filename, ".json") {
		return filename[:len(filename)-len(".json")]
	}
	return ""
}

// extractPromptName only accepts *.prompt.md, the Oracle's glob for a
// prompt.
func extractPromptName(filename string) string {
	if strings.HasSuffix(filename, ".prompt.md") {
		return strings.TrimSuffix(filename, ".prompt.md")
	}
	return ""
}

// withoutRepeatedSkills returns add without each skill whose directory a skill
// of have, or an earlier one of add, already reads: a plugin.json that lists
// the root skill ("./") or one directory twice names one skill, which would
// otherwise shadow itself.
func withoutRepeatedSkills(have, add []Primitive) []Primitive {
	seen := map[string]bool{}
	for _, p := range have {
		if p.Type == TypeSkills {
			seen[filepath.Clean(p.SrcPath)] = true
		}
	}
	var out []Primitive
	for _, p := range add {
		if p.Type == TypeSkills {
			src := filepath.Clean(p.SrcPath)
			if seen[src] {
				continue
			}
			seen[src] = true
		}
		out = append(out, p)
	}
	return out
}
