// Package pluginjson writes the root plugin.json scaffold `apm-go plugin
// init` produces (07-29-plugin-init R3.3.d), mirroring upstream's
// commands/init.py:237-238 + commands/_helpers.py:636-653.
//
// This is deliberately NOT internal/pack/bundle.PluginManifest.Synthesize:
// that type reads an already-written apm.yml and mirrors
// synthesize_plugin_json_from_apm_yml (no hardcoded license, used by `apm
// pack`'s plugin-format bundle export). plugin init's plugin.json is a
// template with a hardcoded "license": "MIT" that is never written into
// apm.yml itself (design.md §4; research/agent-schema-support-matrix.md
// §3.4 confirms this is upstream's own behavior, not a gap). Sharing one
// type between the two would leak the hardcoded license into `apm pack`'s
// synthesis path.
package pluginjson

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/apm-go/apm/internal/pack/bundle"
)

// Scaffold writes dir/plugin.json in the exact field order upstream's
// _helpers.py:644-653 uses: name, version, description, author.name,
// license (hardcoded "MIT"). Output is 2-space-indented JSON with a single
// trailing newline, matching Python's json.dumps(indent=2) + "\n".
func Scaffold(dir, name, version, description, author string) error {
	return writeJSON(filepath.Join(dir, "plugin.json"), baseFields(name, version, description, author))
}

// ScaffoldFiles is the file set each plugin mode generates besides apm.yml,
// in upstream's commit order (commands/init.py:441, 196-202).
func ScaffoldFiles(agent bool) []string {
	if agent {
		return []string{"plugin.json", "mcp.json"}
	}
	return []string{"plugin.json"}
}

// Agent Plugins v1 identifiers, mirroring upstream agent_plugins/constants.py.
const (
	PluginSchemaID      = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
	MCPSchemaID         = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"
	apmExtNamespace     = "com.microsoft.apm"
	apmExtSchemaVersion = "1"
)

// ScaffoldAgent writes the Agent Plugins v1 scaffold upstream's
// _create_plugin_json(mode="agent") (_helpers.py:642-660) and
// _write_and_validate_agent_plugin_scaffold (init.py:417-428) produce:
// plugin.json with a leading "$schema" and a trailing "extensions" block
// (insertion order), and mcp.json with sort_keys=True ("$schema" before
// "mcpServers").
func ScaffoldAgent(dir, name, version, description, author string) error {
	fields := append([]bundle.JSONField{
		{Key: "$schema", Val: bundle.StringValue(PluginSchemaID)},
	}, baseFields(name, version, description, author)...)
	fields = append(fields, bundle.JSONField{Key: "extensions", Val: bundle.ObjectValue(
		bundle.JSONField{Key: apmExtNamespace, Val: bundle.ObjectValue(
			bundle.JSONField{Key: "schemaVersion", Val: bundle.StringValue(apmExtSchemaVersion)},
		)},
	)})
	if err := writeJSON(filepath.Join(dir, "plugin.json"), fields); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "mcp.json"), []bundle.JSONField{
		{Key: "$schema", Val: bundle.StringValue(MCPSchemaID)},
		{Key: "mcpServers", Val: bundle.ObjectValue()},
	})
}

func baseFields(name, version, description, author string) []bundle.JSONField {
	return []bundle.JSONField{
		{Key: "name", Val: bundle.StringValue(name)},
		{Key: "version", Val: bundle.StringValue(version)},
		{Key: "description", Val: bundle.StringValue(description)},
		{Key: "author", Val: bundle.ObjectValue(
			bundle.JSONField{Key: "name", Val: bundle.StringValue(author)},
		)},
		{Key: "license", Val: bundle.StringValue("MIT")},
	}
}

func writeJSON(path string, fields []bundle.JSONField) error {
	out := bundle.MarshalIndent(bundle.ObjectValue(fields...))
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0644); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}
