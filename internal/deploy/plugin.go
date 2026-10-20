package deploy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

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

// collectPluginPrimitives returns the skills, agents and commands, as
// prompts, that m, t's valid plugin.json, declares. staged is for a package
// that is no APM package (isAPMPackage), a Claude plugin to the Oracle: its
// agents and commands are staged with its own .apm/agents/ and .apm/prompts/
// (stagePluginFiles), and an agents or commands field that is neither a list
// nor a string leaves the default directory. skillsDeclared reports whether the manifest's "skills"
// key was present at all (even as an empty array) -- callers use this to
// decide whether the plugin.json declaration is authoritative for skills
// (skip the legacy skills/<name>/SKILL.md scan) or whether the manifest
// simply has no opinion on skills (legacy scan still applies).
//
// A nil m (no plugin.json, invalid JSON, or no "name") results in (nil,
// false), so callers fall back to whatever non-plugin.json discovery they
// already do. The error is a symlink in a declared skill directory, or in
// the package's .apm/agents/ or .apm/prompts/ when staged.
func collectPluginPrimitives(t sourceTree, m *pluginManifest, staged bool) (prims []Primitive, skillsDeclared bool, err error) {
	if m == nil {
		return nil, false, nil
	}

	depKey, modulePath := t.depKey, t.dir
	source := "dependency:" + depKey

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

	if staged {
		staging, err := stagePluginFiles(t, m)
		if err != nil {
			return nil, false, err
		}
		return append(prims, staging...), isJSONKeyPresent(m.Skills), nil
	}

	// An APM package's listed agents and commands follow its own .apm/; the
	// Oracle reads no plugin.json of an APM package (#88).
	for _, rel := range decodeStringOrList(m.Agents) {
		prims = append(prims, collectPluginFlatFiles(rel, modulePath, depKey, source, TypeAgents, extractAgentName)...)
	}
	for _, rel := range decodeStringOrList(m.Commands) {
		for _, p := range collectPluginFlatFiles(rel, modulePath, depKey, source, TypePrompts, pluginCommandName) {
			if !isWorkflowPrompt(p.SrcPath) {
				prims = append(prims, p)
			}
		}
	}

	return prims, isJSONKeyPresent(m.Skills), nil
}

// pluginStage is a staging directory of the Oracle, .apm/agents/ or
// .apm/prompts/, once the plugin's files are copied into it: each file name
// holds the source of the last copy.
type pluginStage map[string]string

func (s pluginStage) put(name, src string) { s[name] = src }

// stagePluginFiles returns a Claude plugin's agents and prompts as the Oracle
// stages them before it collects .apm/ (deps/plugin_parser.py:1232-1296): the
// package's own .apm/agents/ and .apm/prompts/ first, then the agents
// directories, then the agent files, then each command path in the order it
// is listed, a later copy over an earlier one of the same name. A command is
// staged as plugin_command_prompt_name names it. The Oracle then finds the
// staged files in name order (integration/base_integrator.py:1059), so they
// are named in that order, and of two staged names of one agent the first
// one is kept by the conflict resolution. A workflow prompt is dropped only
// then, so a workflow staged last over a command leaves no prompt.
func stagePluginFiles(t sourceTree, m *pluginManifest) ([]Primitive, error) {
	agents, prompts := pluginStage{}, pluginStage{}
	if err := eachFlatFile(t, ".apm/agents", extractAgentName, agents.put); err != nil {
		return nil, err
	}
	if err := eachFlatFile(t, ".apm/prompts", extractPromptName, prompts.put); err != nil {
		return nil, err
	}

	agentPaths := componentPaths(m.Agents, "agents", true)
	for _, dirs := range []bool{true, false} {
		for _, rel := range agentPaths {
			if files, isDir := pluginSourceFiles(t.dir, rel); isDir == dirs {
				for _, f := range files {
					agents.put(filepath.Base(f), f)
				}
			}
		}
	}
	for _, rel := range componentPaths(m.Commands, "commands", true) {
		files, _ := pluginSourceFiles(t.dir, rel)
		for _, f := range files {
			prompts.put(pluginCommandPromptName(filepath.Base(f)), f)
		}
	}

	var prims []Primitive
	for _, name := range slices.Sorted(maps.Keys(agents)) {
		if n := extractAgentName(name); n != "" {
			prims = append(prims, Primitive{Name: n, Type: TypeAgents, Source: t.source(), DepKey: t.depKey, SrcPath: agents[name], Origin: t.logical(".apm/agents/" + name)})
		}
	}
	for _, name := range slices.Sorted(maps.Keys(prompts)) {
		if n := extractPromptName(name); n != "" && !isWorkflowPrompt(prompts[name]) {
			prims = append(prims, Primitive{Name: n, Type: TypePrompts, Source: t.source(), DepKey: t.depKey, SrcPath: prompts[name], Origin: t.logical(".apm/prompts/" + name)})
		}
	}
	return prims, nil
}

// loadPluginManifest returns t's plugin.json, nil when it cannot be read, is
// not valid JSON or is no Claude plugin's.
func loadPluginManifest(t sourceTree) (*pluginManifest, error) {
	m, _, err := findPluginManifest(t)
	return m, err
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

// PluginManifestPaths are the places the Oracle's find_plugin_json looks for a
// plugin.json, in its order (utils/helpers.py:105-129).
var PluginManifestPaths = []string{"plugin.json", ".github/plugin/plugin.json", ".claude-plugin/plugin.json", ".cursor-plugin/plugin.json"}

// agentPluginSchemaID is the $schema of an Agent Plugins 1.0.0 manifest
// (agent_plugins/constants.py:7), which the Oracle installs as an Agent Plugin.
const agentPluginSchemaID = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"

// findPluginManifest returns t's plugin.json, nil when there is none, or the
// first one found cannot be read (failure, when it is there) or is not valid
// JSON; a later place is not tried then. An APM package keeps reading
// .claude-plugin/plugin.json alone (#88 item 75). Any other package takes
// the first of PluginManifestPaths that exists, as the Oracle does, and a
// manifest with no name, an empty one or null, is named after the package's
// directory (deps/plugin_parser.py:501-505). A root plugin.json the Oracle
// does not take for a Claude plugin's, an Agent Plugin's
// (install/primitive_classification.py:88-92), or one it refuses (no name,
// deps/plugin_parser.py:503-504, or not valid JSON), is passed over as it was
// before.
func findPluginManifest(t sourceTree) (*pluginManifest, *sourceReadFailure, error) {
	apmPackage := isAPMPackage(t)
	paths := PluginManifestPaths
	if apmPackage {
		paths = []string{".claude-plugin/plugin.json"}
	}
	for _, rel := range paths {
		p, err := t.path(rel)
		if err != nil {
			return nil, nil, err
		}
		if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, readFailure(p, err), nil
		}
		var m pluginManifest
		err = json.Unmarshal(data, &m)
		if rel == "plugin.json" && (err != nil || m.Name == "" || !legacyPluginSchema(data)) {
			continue
		}
		if err != nil {
			return nil, nil, nil
		}
		if m.Name == "" {
			if apmPackage {
				return nil, nil, nil
			}
			m.Name = filepath.Base(t.dir)
		}
		return &m, nil, nil
	}
	return nil, nil, nil
}

// legacyPluginSchema reports whether a root plugin.json goes to the Oracle's
// Claude plugin normalization (install/primitive_classification.py:75-102):
// it has no $schema, or a string one other than an Agent Plugin's. A $schema
// that is no string fails the Oracle's install.
func legacyPluginSchema(data []byte) bool {
	var head map[string]any
	if json.Unmarshal(data, &head) != nil {
		return false
	}
	schema, ok := head["$schema"]
	if !ok {
		return true
	}
	s, isString := schema.(string)
	return isString && s != agentPluginSchemaID
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

// pluginCommandPromptName is the Oracle's plugin_command_prompt_name
// (bundle/plugin_layout.py:44-50): the file name a plugin command is staged
// under in .apm/prompts/.
func pluginCommandPromptName(filename string) string {
	if !strings.HasSuffix(filename, ".prompt.md") && strings.HasSuffix(filename, ".md") {
		return strings.TrimSuffix(filename, ".md") + ".prompt.md"
	}
	return filename
}

// pluginCommandName names a plugin command after the prompt it is staged as:
// <name>.md and <name>.prompt.md are <name>, any other file is no prompt.
func pluginCommandName(filename string) string {
	return extractPromptName(pluginCommandPromptName(filename))
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
// component directories.
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
// directory's files come in name order.
func collectPluginFlatFiles(rel, modulePath, depKey, source string, pt PrimitiveType, nameFn func(string) string) []Primitive {
	files, isDir := pluginSourceFiles(modulePath, rel)
	if isDir {
		slices.Sort(files)
	}
	var prims []Primitive
	for _, f := range files {
		if name := nameFn(filepath.Base(f)); name != "" {
			prims = append(prims, Primitive{Name: name, Type: pt, Source: source, DepKey: depKey, SrcPath: f})
		}
	}
	return prims
}

// pluginSourceFiles returns the files an agents or commands path of
// plugin.json, rel, gives, and whether it is a directory: the file itself,
// or each file directly in the directory, in the order the file
// system lists them, the order of the Oracle's copy (os.walk). A file below
// the directory is not deployed (#88), and a symlink in it is skipped.
func pluginSourceFiles(modulePath, rel string) (files []string, isDir bool) {
	abs, ok := resolvePluginPath(modulePath, rel)
	if !ok {
		return nil, false
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, false
	}
	if !info.IsDir() {
		return []string{abs}, false
	}
	d, err := os.Open(abs)
	if err != nil {
		return nil, true
	}
	defer d.Close()
	entries, err := d.ReadDir(-1)
	if err != nil {
		return nil, true
	}
	for _, e := range entries {
		if !e.IsDir() && e.Type()&os.ModeSymlink == 0 {
			files = append(files, filepath.Join(abs, e.Name()))
		}
	}
	return files, true
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
