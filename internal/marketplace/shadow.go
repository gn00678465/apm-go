// This file (shadow.go) implements mkt-034b's shadow-plugin-name detection:
// when installing PLUGIN@MARKETPLACE, warn if the same plugin name also
// exists in some OTHER registered marketplace, since that may indicate
// name-squatting by an attacker who published a same-named plugin
// elsewhere. Mirrors the Python original's shadow_detector.py::detect_shadows,
// wired into resolve_marketplace_plugin (resolver.py:1003-1019).
//
// Design note (design.md gaps A7): the Python original's detect_shadows
// reuses fetch_or_cache, so in the common case this scan costs no extra
// network round-trips (the manifest is already cached from a prior browse/
// install). apm-go's consumer-side Fetch (client.go) has no such cache layer
// yet, so this does one live fetch per OTHER registered marketplace every
// time -- correct but slower; the design explicitly chooses correctness over
// performance here (a security advisory is not something to skip for
// speed), to be revisited once a manifest cache layer exists.
package marketplace

import (
	"context"
	"fmt"
	"strings"
)

// detectShadowWarnings scans every registered marketplace OTHER than
// primaryMktName (name comparison case-insensitive, matching FindByName's
// own case-insensitivity) for a plugin named pluginName, returning one
// warning string per hit. Any failure loading the registry, or fetching a
// candidate marketplace's manifest, is swallowed silently -- this function
// never returns an error, mirroring detect_shadows' own
// `except Exception: logger.debug(...)` catch-all: a shadow-detection
// failure must never interrupt installation (mkt-034b), and one candidate
// marketplace failing to fetch must not stop the scan of the rest.
func detectShadowWarnings(ctx context.Context, pluginName, primaryMktName string) []string {
	sources, err := LoadRegistry()
	if err != nil {
		return nil
	}
	var warnings []string
	for i := range sources {
		src := sources[i]
		if strings.EqualFold(src.Name, primaryMktName) {
			continue
		}
		manifestDoc, fetchErr := Fetch(ctx, &src)
		if fetchErr != nil {
			continue
		}
		if match := findPluginCaseInsensitive(manifestDoc.Plugins, pluginName); match != nil {
			warnings = append(warnings, fmt.Sprintf(
				"Plugin %q also found in marketplace %q. Verify you are installing from the intended source.",
				pluginName, src.Name,
			))
		}
	}
	return warnings
}
