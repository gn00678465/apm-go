package deploy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// PackageError is why the Oracle's validate_apm_package refuses the package
// at dir, nil when it takes it (models/validation.py:338-480), following its
// format cascade (models/validation.py:256-295): an apm.yml with .apm/ or a
// declared dependency, then a plugin manifest, then an apm.yml with a root
// SKILL.md, a root SKILL.md, a skills/<name>/SKILL.md bundle and hooks/.
// What each format checks beyond that (a skill bundle's frontmatter, a
// package's apm.yml, which the loader has parsed) is not ported. Measured
// with Oracle 0.33.0 on uninstall's staged copy (issue #129 r2 runs).
func PackageError(dir string) error {
	t := sourceTree{dir: dir}
	if isAPMPackage(t) {
		return nil
	}
	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
		return err == nil
	}
	// A root plugin.json has to load and declare a name
	// (agent_plugins/loader.py:176-216, deps/plugin_parser.py:501-504);
	// a manifest elsewhere has to load (deps/plugin_parser.py:429-450).
	for _, rel := range PluginManifestPaths {
		if !exists(rel) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return fmt.Errorf("%s is not valid JSON: %w", rel, err)
		}
		if name, _ := m["name"].(string); rel == "plugin.json" && name == "" {
			return errors.New("plugin.json declares no name")
		}
		return nil
	}
	if info, err := os.Stat(filepath.Join(dir, ".claude-plugin")); err == nil && info.IsDir() {
		return nil
	}
	if exists("SKILL.md") {
		if exists("apm.yml") {
			return nil
		}
		content, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
		if err != nil {
			return err
		}
		if _, err := readPromptDoc(content); err != nil {
			return fmt.Errorf("SKILL.md: %w", err)
		}
		return nil
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "skills", "*", "SKILL.md")); len(matches) > 0 {
		return nil
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "hooks", "*.json")); len(matches) > 0 && !exists("apm.yml") {
		return nil
	}
	return errors.New("no apm.yml with .apm/ or dependencies, plugin manifest, SKILL.md or skills/<name>/SKILL.md")
}
