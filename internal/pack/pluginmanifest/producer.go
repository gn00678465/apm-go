package pluginmanifest

import (
	"io"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/pack/bundle"
)

// ecosystems are the targets that have a plugin.json:
// PLUGIN_MANIFEST_ECOSYSTEMS (core/plugin_manifest.py:58).
var ecosystems = map[string]bool{"claude": true, "copilot": true}

// Manifests holds the absolute plugin.json paths Produce handled, by the
// outcome Write gave each, in the order apm.yml's targets name them: the
// Oracle's plugin_manifests payload (core/build_orchestrator.py:411-438).
type Manifests struct {
	Written []string
	Skipped []string
	DryRun  []string
}

// Produce runs PluginManifestProducer for every plugin-manifest ecosystem
// present in targets (claude/copilot), mirroring
// PluginManifestProducer.produce (core/build_orchestrator.py:253-338):
// synthesize from apm.yml (root; conventions like agents/skills/commands/
// instructions are never part of PluginManifest's field set, so there is
// nothing to strip -- unlike build_plugin_manifest, which pops those keys
// from a dict), attach sanitized mcpServers for claude only, then write.
// Returns each ecosystem's plugin.json path grouped by Write's outcome (the
// payload pack --json reports as plugin_manifests) and the first error
// encountered (a synthesis error, or a write error) --
// mirrors Python's produce() which lets any exception from
// build_plugin_manifest/write_plugin_manifest propagate to the
// orchestrator, aborting the remaining ecosystems.
func Produce(w io.Writer, projectRoot string, root *yaml.Node, targets []string, force, dryRun bool) (out Manifests, err error) {
	// The order of apm.yml's targets, each once, as the Oracle builds its
	// list (core/build_orchestrator.py:375-389).
	seen := make(map[string]bool, len(targets))
	for _, ecosystem := range targets {
		if !ecosystems[ecosystem] || seen[ecosystem] {
			continue
		}
		seen[ecosystem] = true
		m, serr := bundle.Synthesize(root)
		if serr != nil {
			return out, serr
		}

		if ecosystem == "claude" {
			servers := bundle.ReadMCPServers(projectRoot)
			sanitized, dropped := bundle.SanitizeServers(servers)
			bundle.PrintSecretWarning(w, dropped)
			m.MCPServers = &sanitized
		}

		path, outcome, werr := Write(w, projectRoot, ecosystem, m, force, dryRun)
		if werr != nil {
			return out, werr
		}
		switch outcome {
		case Written:
			out.Written = append(out.Written, path)
		case DryRun:
			out.DryRun = append(out.DryRun, path)
		default:
			out.Skipped = append(out.Skipped, path)
		}
	}
	return out, nil
}
