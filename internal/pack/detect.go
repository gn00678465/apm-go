// Package pack implements apm-go pack's three-producer output detection,
// mirroring Python's build_orchestrator.py:detect_outputs/BuildOrchestrator.run
// (research/pack-parity-findings.md §1).
package pack

import "errors"

// pluginManifestEcosystems mirrors core/plugin_manifest.py's
// PLUGIN_MANIFEST_ECOSYSTEMS: only these two target values route to
// PluginManifestProducer.
var pluginManifestEcosystems = map[string]bool{"claude": true, "copilot": true}

// ErrNothingToPack mirrors BuildOrchestrator.run's BuildError text
// (build_orchestrator.py:417-425) for the case where no producer has
// anything to do: apm.yml has neither dependencies: nor marketplace:, and
// target:/targets: does not include claude or copilot.
var ErrNothingToPack = errors.New(
	"apm.yml has neither 'dependencies:' nor 'marketplace:' block, and " +
		"'target:' does not include 'claude' or 'copilot'. Nothing to pack. " +
		"Add dependencies via 'apm-go install <pkg>', configure a " +
		"'marketplace:' block, or set 'target:' to include 'claude' or 'copilot'.",
)

// DetectOutputs implements the three-producer trigger matrix (findings §1.3):
// bundle/marketplace/pluginManifest are independent, non-exclusive checks --
// any subset may be true simultaneously. hasDeps must reflect ONLY
// apm.yml's top-level dependencies: block (ParsedDeps), never devDependencies
// or mcp servers (findings §1.2 point 3 -- matching Python's
// data.get("dependencies") check, which devDependencies never satisfies).
// hasMarketplace must reflect a present, non-null marketplace: block or a
// legacy marketplace.yml (already computed by the caller, e.g.
// hasMarketplaceConfig). targets is the already-parsed target:/targets:
// list (manifest.Manifest.Target). When none of the three trigger, returns
// ErrNothingToPack -- exit 1, matching Python's BuildError (findings §1.1),
// not apm-go's prior silent exit-0 "nothing to do".
func DetectOutputs(hasDeps, hasMarketplace bool, targets []string) (bundle, marketplace, pluginManifest bool, err error) {
	bundle = hasDeps
	marketplace = hasMarketplace
	pluginManifest = targetsIncludePluginEcosystem(targets)
	if !bundle && !marketplace && !pluginManifest {
		return false, false, false, ErrNothingToPack
	}
	return bundle, marketplace, pluginManifest, nil
}

// targetsIncludePluginEcosystem reports whether targets contains at least
// one of "claude"/"copilot" (PLUGIN_MANIFEST_ECOSYSTEMS).
func targetsIncludePluginEcosystem(targets []string) bool {
	for _, t := range targets {
		if pluginManifestEcosystems[t] {
			return true
		}
	}
	return false
}
