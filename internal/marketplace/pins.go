// This file (pins.go) implements mkt-034a's ref-swap-pin advisory: recording
// a (marketplace, plugin, version) -> ref mapping on disk, and warning when a
// later resolution of the same triple observes a DIFFERENT ref -- a possible
// ref-swap attack where an attacker rewrote which commit an existing tagged
// version points to. Mirrors the Python original's version_pins.py, wired
// into resolve_marketplace_plugin (resolver.py:968-994).
//
// Storage (design.md gaps A7, cross-checked against version_pins.py:1-65):
// ~/.apm/cache/marketplace/version-pins.json, a FLAT dict
// {"<marketplace>/<plugin>/<version>": "<ref>"} (the version segment is
// omitted entirely -- not even an empty segment -- when the plugin has no
// declared version), every key lowercased. All operations are fail-open: a
// missing file, an unreadable file, or malformed JSON all silently degrade
// to "no pin recorded yet" rather than surfacing an error to the caller, and
// a write failure is likewise swallowed -- this is strictly an advisory
// security signal, never a blocking one.
package marketplace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// pinsFileName is the ref-pin cache's filename inside the apm cache
// directory (~/.apm/cache/marketplace/version-pins.json).
const pinsFileName = "version-pins.json"

// pinsPath returns the full path to the version-pins file, honoring
// $APM_CONFIG_DIR the same way registry.go's RegistryPath does, so tests can
// isolate the pin cache from a developer's real home directory.
func pinsPath() (string, error) {
	dir := os.Getenv("APM_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		dir = filepath.Join(home, ".apm")
	}
	return filepath.Join(dir, "cache", "marketplace", pinsFileName), nil
}

// pinKey builds the flat-dict key for a (marketplace, plugin, version)
// triple, mirroring version_pins.py's _pin_key (:53-64): "<mkt>/<plugin>"
// lowercased, with "/<version>" appended (and the whole key re-lowercased)
// only when version is non-empty -- an empty version omits the third segment
// entirely rather than leaving a trailing "/".
func pinKey(mktName, pluginName, version string) string {
	key := mktName + "/" + pluginName
	if version != "" {
		key = key + "/" + version
	}
	return strings.ToLower(key)
}

// loadRefPins reads the flat pin dict from disk. Fail-open (version_pins.py's
// load_ref_pins, :72-106): a missing file, an unreadable file, JSON that
// doesn't parse, or JSON that parses to something other than a flat
// string-to-string object all degrade to an empty map, never an error.
func loadRefPins() map[string]string {
	p, err := pinsPath()
	if err != nil {
		return map[string]string{}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return map[string]string{}
	}
	var pins map[string]string
	if err := json.Unmarshal(data, &pins); err != nil {
		return map[string]string{}
	}
	if pins == nil {
		return map[string]string{}
	}
	return pins
}

// saveRefPins atomically overwrites the pin file with pins: write a temp
// file in the same directory, then rename over the destination (mirrors
// registry.go's SaveRegistry). Fail-open (version_pins.py's save_ref_pins,
// :109-124): any error along the way is silently swallowed rather than
// propagated -- this is an advisory cache, not a source of truth.
func saveRefPins(pins map[string]string) {
	p, err := pinsPath()
	if err != nil {
		return
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	data, err := json.MarshalIndent(pins, "", "  ")
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, pinsFileName+".*.tmp")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return
	}
	if err := os.Rename(tmpPath, p); err != nil {
		os.Remove(tmpPath)
	}
}

// checkAndRecordRefPin implements mkt-034a end to end: look up the pin
// previously recorded for (mktName, pluginName, version); if one exists and
// differs from ref, return a non-empty ref-swap warning. Either way, the pin
// is then unconditionally overwritten with ref (mirrors resolver.py's
// check_ref_pin + record_ref_pin call pair, :968-994: the current ref is
// always (re-)recorded, even right after a swap is detected). A first-time
// key (no previous pin) never warns -- only a genuine CHANGE does, so a
// legitimate version bump (a different pinKey entirely, since version is
// part of the key) never produces a false positive.
func checkAndRecordRefPin(mktName, pluginName, version, ref string) string {
	pins := loadRefPins()
	key := pinKey(mktName, pluginName, version)
	var warning string
	if previous, existed := pins[key]; existed && previous != ref {
		warning = fmt.Sprintf(
			"Plugin %s@%s ref changed: was %q, now %q. This may indicate a ref swap attack.",
			pluginName, mktName, previous, ref,
		)
	}
	pins[key] = ref
	saveRefPins(pins)
	return warning
}
