package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/archive"
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
// field), and returns the skill/agent/command primitives its component
// arrays declare. skillsDeclared reports whether the manifest's "skills"
// key was present at all (even as an empty array) -- callers use this to
// decide whether the plugin.json declaration is authoritative for skills
// (skip the legacy skills/<name>/SKILL.md scan) or whether the manifest
// simply has no opinion on skills (legacy scan still applies).
//
// A missing file, invalid JSON, or a manifest without "name" all result in
// (nil, false) -- entirely ignored, callers fall back to whatever
// non-plugin.json discovery they already do.
func collectPluginPrimitives(depKey, modulePath string) (prims []Primitive, skillsDeclared bool) {
	data, err := os.ReadFile(filepath.Join(modulePath, ".claude-plugin", "plugin.json"))
	if err != nil {
		return nil, false
	}

	var m pluginManifest
	if err := json.Unmarshal(data, &m); err != nil || m.Name == "" {
		return nil, false
	}

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
		if _, err := os.Stat(filepath.Join(abs, "SKILL.md")); err != nil {
			continue
		}
		prims = append(prims, Primitive{
			Name:    filepath.Base(abs),
			Type:    TypeSkills,
			Source:  source,
			DepKey:  depKey,
			SrcPath: abs,
		})
	}

	for _, rel := range decodeStringOrList(m.Agents) {
		prims = append(prims, collectPluginFlatFiles(rel, modulePath, depKey, source, TypeAgents, extractAgentName)...)
	}

	for _, rel := range decodeStringOrList(m.Commands) {
		prims = append(prims, collectPluginFlatFiles(rel, modulePath, depKey, source, TypeCommands, extractBaseName)...)
	}

	return prims, isJSONKeyPresent(m.Skills)
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
