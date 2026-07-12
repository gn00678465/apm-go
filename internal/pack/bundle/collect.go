package bundle

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Component is one (source, outputRel) pair collected from a package,
// mirroring Python's (Path, str) tuples in _collect_apm_components/
// _collect_root_plugin_components/_collect_bare_skill
// (plugin_exporter.py:86-166). Source is an absolute filesystem path;
// OutputRel is the bundle-relative POSIX output path.
type Component struct {
	Source    string
	OutputRel string
}

// CollectAPMComponents walks apmDir's convention subdirectories, mirroring
// _collect_apm_components (plugin_exporter.py:86-117): agents/ -> agents/
// (flat), skills/ -> skills/ (recursive), prompts/ -> commands/ (recursive,
// renaming *.prompt.md -> *.md), instructions/ -> instructions/
// (recursive), commands/ -> commands/ (recursive), extensions/ ->
// extensions/ (recursive, verbatim).
func CollectAPMComponents(apmDir string) []Component {
	if !isDir(apmDir) {
		return nil
	}
	var out []Component
	out = append(out, collectFlat(filepath.Join(apmDir, "agents"), "agents", nil)...)
	out = append(out, collectRecursive(filepath.Join(apmDir, "skills"), "skills", nil)...)
	out = append(out, collectRecursive(filepath.Join(apmDir, "prompts"), "commands", renamePrompt)...)
	out = append(out, collectRecursive(filepath.Join(apmDir, "instructions"), "instructions", nil)...)
	out = append(out, collectRecursive(filepath.Join(apmDir, "commands"), "commands", nil)...)
	out = append(out, collectRecursive(filepath.Join(apmDir, "extensions"), "extensions", nil)...)
	return out
}

// CollectRootPluginComponents collects plugin-native convention directories
// authored at a package's root (as opposed to under .apm/), mirroring
// _collect_root_plugin_components (plugin_exporter.py:120-129).
func CollectRootPluginComponents(root string) []Component {
	var out []Component
	for _, dir := range []string{"agents", "skills", "commands", "instructions", "extensions"} {
		out = append(out, collectRecursive(filepath.Join(root, dir), dir, nil)...)
	}
	return out
}

// CollectBareSkill detects a bare Claude skill (a SKILL.md at installPath's
// root with no .apm/skills or root skills/ already collected into existing)
// and maps the whole package directory into skills/{slug}/, mirroring
// _collect_bare_skill (plugin_exporter.py:132-166). slug prefers
// virtualPath (normalized), falling back to repoURL's last path segment,
// then the literal "skill". apm.yml/apm.lock.yaml/plugin.json are excluded;
// symlinked entries are skipped.
func CollectBareSkill(installPath, virtualPath, repoURL string, existing []Component) []Component {
	skillMD := filepath.Join(installPath, "SKILL.md")
	info, err := os.Stat(skillMD)
	if err != nil || info.IsDir() {
		return nil
	}
	for _, c := range existing {
		if strings.HasPrefix(c.OutputRel, "skills/") {
			return nil
		}
	}

	slug := normalizeBareSkillSlug(virtualPath)
	if slug == "" {
		if repoURL != "" {
			parts := strings.Split(repoURL, "/")
			slug = parts[len(parts)-1]
		} else {
			slug = "skill"
		}
	}

	excluded := map[string]bool{"apm.yml": true, "apm.lock.yaml": true, "plugin.json": true}
	entries, err := os.ReadDir(installPath) // already sorted by name
	if err != nil {
		return nil
	}
	var out []Component
	for _, e := range entries {
		if !e.Type().IsRegular() || excluded[e.Name()] {
			continue
		}
		out = append(out, Component{
			Source:    filepath.Join(installPath, e.Name()),
			OutputRel: "skills/" + slug + "/" + e.Name(),
		})
	}
	return out
}

// normalizeBareSkillSlug mirrors _normalize_bare_skill_slug
// (plugin_exporter.py:71-78).
func normalizeBareSkillSlug(slug string) string {
	normalized := strings.Trim(strings.ReplaceAll(slug, "\\", "/"), "/")
	for strings.HasPrefix(normalized, "skills/") {
		normalized = strings.TrimLeft(normalized[len("skills/"):], "/")
	}
	if normalized == "skills" {
		return ""
	}
	return normalized
}

// renamePrompt strips the ".prompt" infix, mirroring _rename_prompt
// (plugin_exporter.py:64-68).
func renamePrompt(name string) string {
	if strings.HasSuffix(name, ".prompt.md") {
		return strings.TrimSuffix(name, ".prompt.md") + ".md"
	}
	return name
}

// collectFlat adds every regular non-symlink file directly inside srcDir,
// mirroring _collect_flat (plugin_exporter.py:172-185). os.ReadDir already
// returns entries sorted by filename, matching Python's sorted(iterdir()).
func collectFlat(srcDir, outputPrefix string, rename func(string) string) []Component {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return nil
	}
	var out []Component
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		name := e.Name()
		if rename != nil {
			name = rename(name)
		}
		out = append(out, Component{Source: filepath.Join(srcDir, e.Name()), OutputRel: outputPrefix + "/" + name})
	}
	return out
}

// collectRecursive adds every regular non-symlink file under srcDir,
// preserving its subdirectory hierarchy, mirroring _collect_recursive
// (plugin_exporter.py:188-204). filepath.WalkDir visits each directory's
// entries in os.ReadDir's sorted order.
func collectRecursive(srcDir, outputPrefix string, rename func(string) string) []Component {
	if !isDir(srcDir) {
		return nil
	}
	var out []Component
	_ = filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || p == srcDir || !d.Type().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(srcDir, p)
		if rerr != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		dir, base := path.Split(relSlash)
		if rename != nil {
			base = rename(base)
		}
		out = append(out, Component{Source: p, OutputRel: outputPrefix + "/" + dir + base})
		return nil
	})
	return out
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
