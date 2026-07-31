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
	fields := []bundle.JSONField{
		{Key: "name", Val: bundle.StringValue(name)},
		{Key: "version", Val: bundle.StringValue(version)},
		{Key: "description", Val: bundle.StringValue(description)},
		{Key: "author", Val: bundle.ObjectValue(
			bundle.JSONField{Key: "name", Val: bundle.StringValue(author)},
		)},
		{Key: "license", Val: bundle.StringValue("MIT")},
	}

	out := bundle.MarshalIndent(bundle.ObjectValue(fields...))
	out = append(out, '\n')

	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), out, 0644); err != nil {
		return fmt.Errorf("write plugin.json: %w", err)
	}
	return nil
}
