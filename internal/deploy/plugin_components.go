package deploy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/archive"
)

// checkDeclaredComponents is the Oracle's _validate_declared_component_paths
// (deps/plugin_parser.py:516-551), which fails the install of a Claude plugin
// whose plugin.json declares an agents, skills, commands or hooks path that
// is empty, leaves the plugin's directory dir, or is not there, before
// anything of it is deployed. A null or empty list, and hooks given as an
// object, declare nothing. The messages name dir as an absolute path, as the
// Oracle names the plugin's directory.
func checkDeclaredComponents(dir string, m *pluginManifest) error {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	for _, field := range []struct {
		name string
		raw  json.RawMessage
	}{{"agents", m.Agents}, {"skills", m.Skills}, {"commands", m.Commands}, {"hooks", m.Hooks}} {
		raw := bytes.TrimSpace(field.raw)
		if !isJSONKeyPresent(raw) || (field.name == "hooks" && raw[0] == '{') {
			continue
		}
		values := []json.RawMessage{raw}
		if raw[0] == '[' {
			if err := json.Unmarshal(raw, &values); err != nil {
				continue
			}
		}
		for _, v := range values {
			if err := checkDeclaredComponent(dir, m.Name, field.name, pyStrJSON(v)); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkDeclaredComponent(dir, plugin, field, declared string) error {
	if strings.TrimSpace(declared) == "" {
		return fmt.Errorf("Plugin '%s' declares an empty '%s' component path in plugin root '%s'. Remove the empty declaration from plugin.json, then reinstall.", plugin, field, dir)
	}
	candidate := declared
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(dir, filepath.FromSlash(declared))
	}
	// A path that cannot be read is no content decision here: the read
	// failure of its bucket (pluginSourceReadFailure) reports it.
	realBase, err := archive.RealPath(dir)
	if err != nil {
		return nil
	}
	resolved, err := archive.RealPath(candidate)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil
		}
		resolved = ""
	}
	if resolved == "" || !archive.Within(realBase, resolved) {
		return fmt.Errorf("Plugin '%s' declares an invalid '%s' component path '%s' outside plugin root '%s'. Move the component inside the plugin root or remove the declaration from plugin.json, then reinstall.", plugin, field, declared, dir)
	}
	_, statErr := os.Stat(resolved)
	if errors.Is(statErr, fs.ErrPermission) {
		return nil
	}
	info, lstatErr := os.Lstat(candidate)
	if statErr == nil && lstatErr == nil && info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	return fmt.Errorf("Plugin '%s' declares missing '%s' component path '%s' in plugin root '%s'. Add the component or remove the declaration from plugin.json, then reinstall.", plugin, field, declared, dir)
}

// pyStrJSON is Python's str() of a JSON value json.loads gives, for the
// scalars a component path can be; any other value is its JSON text.
func pyStrJSON(v json.RawMessage) string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	switch string(v) {
	case "true":
		return "True"
	case "false":
		return "False"
	case "null":
		return "None"
	}
	return string(v)
}
