package deploy

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/archive"
	"github.com/apm-go/apm/internal/yamlcore"
	"go.yaml.in/yaml/v4"
)

// pluginManifest is the subset of a Claude Code plugin manifest
// (.claude-plugin/plugin.json) that apm-go's deploy pipeline consumes.
// Spec: https://docs.anthropic.com/en/docs/claude-code/plugins -- "name" is
// the only required field; everything else is optional metadata. Mirrors
// the Python oracle's plugin_parser.parse_plugin_manifest /
// _map_plugin_artifacts (skills/agents/commands mapping).
//
// Skills/Agents/Commands are kept as json.RawMessage rather than []string
// so a present-but-empty array (an explicit "no components" declaration)
// can be told apart from an absent key (no opinion, legacy scan applies) --
// len(raw) == 0 means the key was absent from the JSON.
type pluginManifest struct {
	Name     string          `json:"name"`
	Skills   json.RawMessage `json:"skills"`
	Agents   json.RawMessage `json:"agents"`
	Commands json.RawMessage `json:"commands"`
}

// collectPluginPrimitives reads <modulePath>/.claude-plugin/plugin.json, if
// present and valid (has a non-empty "name", the spec's only required
// field), and returns the skills, agents and commands, as prompts, its
// component fields declare; for a package that is no APM package
// (isAPMPackage), an agents or commands field that is neither a list nor a
// string leaves the default directory. skillsDeclared reports whether the manifest's "skills"
// key was present at all (even as an empty array) -- callers use this to
// decide whether the plugin.json declaration is authoritative for skills
// (skip the legacy skills/<name>/SKILL.md scan) or whether the manifest
// simply has no opinion on skills (legacy scan still applies).
//
// A missing file, invalid JSON, or a manifest without "name" all result in
// (nil, false) -- entirely ignored, callers fall back to whatever
// non-plugin.json discovery they already do. The error is a symlink at the
// manifest that leaves the package, or one in a declared skill directory.
func collectPluginPrimitives(t sourceTree) (prims []Primitive, skillsDeclared bool, err error) {
	m, err := loadPluginManifest(t)
	if m == nil {
		return nil, false, err
	}

	depKey, modulePath := t.depKey, t.dir
	source := "dependency:" + depKey
	apmPackage := isAPMPackage(t)

	for _, rel := range decodeStringOrList(m.Skills) {
		abs, ok := resolvePluginPath(modulePath, rel)
		if !ok {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			continue
		}
		if ok, err := t.hasSkillMD(filepath.ToSlash(rel)); !ok {
			if err != nil {
				return nil, false, err
			}
			continue
		}
		if err := t.checkSkill(filepath.ToSlash(rel)); err != nil {
			return nil, false, err
		}
		prims = append(prims, Primitive{
			Name:    filepath.Base(abs),
			Type:    TypeSkills,
			Source:  source,
			DepKey:  depKey,
			SrcPath: abs,
		})
	}

	for _, rel := range componentPaths(m.Agents, "agents", !apmPackage) {
		prims = append(prims, collectPluginFlatFiles(rel, modulePath, depKey, source, TypeAgents, extractAgentName)...)
	}

	// The Oracle copies each command to .apm/prompts/ and deploys it from
	// there as any prompt (deps/plugin_parser.py:1264-1290).
	for _, rel := range componentPaths(m.Commands, "commands", !apmPackage) {
		for _, p := range collectPluginFlatFiles(rel, modulePath, depKey, source, TypePrompts, extractPluginCommandName) {
			if !isWorkflowPrompt(p.SrcPath) {
				prims = append(prims, p)
			}
		}
	}

	return prims, isJSONKeyPresent(m.Skills), nil
}

// loadPluginManifest returns t's plugin.json, nil when it cannot be read, is
// not valid JSON or has no name.
func loadPluginManifest(t sourceTree) (*pluginManifest, error) {
	data, err := readPluginManifest(t)
	if data == nil {
		return nil, err
	}
	var m pluginManifest
	if err := json.Unmarshal(data, &m); err != nil || m.Name == "" {
		return nil, nil
	}
	return &m, nil
}

// claudePluginName is the name of t's plugin.json when the Oracle takes t for
// a Claude plugin, "" otherwise: it names the package by the apm.yml it builds
// from plugin.json (deps/plugin_parser.py:1370).
func claudePluginName(t sourceTree) (string, error) {
	m, err := loadPluginManifest(t)
	if m == nil || isAPMPackage(t) {
		return "", err
	}
	return m.Name, nil
}

// readPluginManifest returns the bytes of t's .claude-plugin/plugin.json, nil
// when it cannot be read. The manifest is read through t, so a symlink at it
// that leaves the package is an error.
func readPluginManifest(t sourceTree) ([]byte, error) {
	p, err := t.path(".claude-plugin/plugin.json")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, nil
	}
	return data, nil
}

// decodeStringOrList normalizes a manifest component field that the spec
// allows to be either a single path string or an array of path strings.
func decodeStringOrList(raw json.RawMessage) []string {
	if !isJSONKeyPresent(raw) {
		return nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil && single != "" {
		return []string{single}
	}
	return nil
}

// extractPluginCommandName names a plugin command after the prompt the Oracle
// copies it to, plugin_command_prompt_name (bundle/plugin_layout.py:44-50):
// <name>.md and <name>.prompt.md are <name>, any other file is no prompt.
func extractPluginCommandName(filename string) string {
	if name := extractPromptName(filename); name != "" {
		return name
	}
	return extractBaseName(filename)
}

// componentPaths is what the Oracle's _resolve_sources reads for an agents or
// commands component (deps/plugin_parser.py:1180-1215): the path or paths a
// list or a string declares, else, when useDefault, the default directory.
func componentPaths(raw json.RawMessage, defaultDir string, useDefault bool) []string {
	if v := bytes.TrimSpace(raw); len(v) > 0 && (v[0] == '[' || v[0] == '"') {
		return decodeStringOrList(raw)
	}
	if useDefault {
		return []string{defaultDir}
	}
	return nil
}

// isAPMPackage is the Oracle's has_eligible_apm_yml
// (models/format_detection.py:207-229,451): an apm.yml beside a .apm/
// directory, or one that declares a dependency. The Oracle takes such a
// package for an APM package, not a Claude plugin, so it gets no default
// component directories. cmd/apm-go's packageVersion has its own copy (#88).
// A path that leaves the package is not read and counts as missing, as for
// uninstall (readManifestLeniently): install refuses that apm.yml where it
// reads the package's manifest, and compile, which reads no apm.yml, must
// not start to fail on it.
func isAPMPackage(t sourceTree) bool {
	manifestPath, err := t.path("apm.yml")
	if err != nil {
		return false
	}
	if _, err := os.Stat(manifestPath); err != nil {
		return false
	}
	if apmDir, err := t.path(".apm"); err == nil {
		if info, err := os.Stat(apmDir); err == nil && info.IsDir() {
			return true
		}
	}
	data, err := os.ReadFile(manifestPath)
	return err == nil && declaresDependencies(data)
}

// declaresDependencies is the Oracle's _apm_yml_declares_dependencies
// (models/validation.py:298-335): a list under dependencies or
// devDependencies, apm or mcp, with a string or mapping entry.
func declaresDependencies(data []byte) bool {
	doc, err := yamlcore.SafeLoad(data)
	if err != nil || len(doc.Content) == 0 {
		return false
	}
	for _, section := range []string{"dependencies", "devDependencies"} {
		block := mappingValue(doc.Content[0], section)
		for _, key := range []string{"apm", "mcp"} {
			list := mappingValue(block, key)
			if list == nil || list.Kind != yaml.SequenceNode {
				continue
			}
			for _, entry := range list.Content {
				if entry.Kind == yaml.MappingNode || (entry.Kind == yaml.ScalarNode && entry.ShortTag() == "!!str") {
					return true
				}
			}
		}
	}
	return false
}

// mappingValue is the value of key in the mapping n, nil when n is no mapping
// or has no such key.
func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// isJSONKeyPresent reports whether raw represents a JSON key that was
// actually present in the source document (as opposed to absent, which
// encoding/json leaves as a nil/empty RawMessage). An explicit JSON null is
// treated as absent too, since it carries no declaration either way.
func isJSONKeyPresent(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

// collectPluginFlatFiles resolves a single agents/commands manifest entry
// (per spec, a component path may be a directory or an individual .md
// file) into zero or more flat-file primitives -- matching how apm-go's
// existing Primitive model represents agents/commands (one Primitive per
// source file, SrcPath pointing at that file), not a directory copy. A
// directory entry contributes one primitive per top-level .md file it
// directly contains (not recursive -- deeper nesting isn't representable
// by the flat-file model and isn't needed for the plugin.json shapes this
// unblocks).
func collectPluginFlatFiles(rel, modulePath, depKey, source string, pt PrimitiveType, nameFn func(string) string) []Primitive {
	abs, ok := resolvePluginPath(modulePath, rel)
	if !ok {
		return nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil
	}

	if !info.IsDir() {
		name := nameFn(filepath.Base(abs))
		if name == "" {
			return nil
		}
		return []Primitive{{Name: name, Type: pt, Source: source, DepKey: depKey, SrcPath: abs}}
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil
	}
	var prims []Primitive
	for _, e := range entries {
		if e.IsDir() || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := nameFn(e.Name())
		if name == "" {
			continue
		}
		prims = append(prims, Primitive{
			Name:    name,
			Type:    pt,
			Source:  source,
			DepKey:  depKey,
			SrcPath: filepath.Join(abs, e.Name()),
		})
	}
	return prims
}

// resolvePluginPath resolves a manifest-declared relative path against
// modulePath and enforces the trust boundary on this attacker-controlled
// input (mirrors Python's _is_within_plugin): rejects absolute paths, ".."
// segments, any path that resolves outside modulePath, and any path whose
// resolved target -- or any intermediate path component -- is a symlink.
// Returns the resolved absolute path and true only when every guard
// passes; any rejection returns ("", false) and the caller skips the entry
// with no primitive emitted.
func resolvePluginPath(modulePath, rel string) (string, bool) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", false
	}
	slashRel := filepath.ToSlash(rel)
	if strings.HasPrefix(slashRel, "/") {
		return "", false
	}
	for _, seg := range strings.Split(slashRel, "/") {
		if seg == ".." {
			return "", false
		}
	}

	abs := filepath.Join(modulePath, filepath.FromSlash(rel))
	if !archive.Contained(modulePath, abs) {
		return "", false
	}
	if hasSymlinkComponent(modulePath, abs) {
		return "", false
	}
	return abs, true
}

// hasSymlinkComponent reports whether target, or any path component
// between root and target, is a symlink. Uses Lstat (not Stat) so the
// check itself never follows a hostile symlink. A missing component is not
// itself a symlink-rejection -- the caller's own os.Stat existence check
// handles that case with its own diagnostic path.
func hasSymlinkComponent(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return true
	}
	cur := root
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return false
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}
