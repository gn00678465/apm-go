package build

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/marketplace/authoring"
	"github.com/apm-go/apm/internal/marketplace/tagpattern"
	"github.com/apm-go/apm/internal/yamlcore"
)

// maxPluginJSONBytes mirrors _MAX_PLUGIN_JSON_BYTES (version_check.py:26),
// the release-time version-check gate's own cap on a local package's
// fallback plugin.json (distinct from localMetadataMaxBytes, which bounds
// apm.yml reads for the unrelated metadata-enrichment path).
const maxPluginJSONBytes = 1024 * 1024

// pluginJSONLocalCandidates mirrors find_plugin_json's search order
// (utils/helpers.py:105-129) -- the SAME candidate list
// internal/pack/bundle's own pluginJSONCandidates uses, duplicated here
// (not imported) since the two packages read plugin.json for unrelated
// purposes (bundling vs. this gate's version lookup) and neither owns the
// other.
var pluginJSONLocalCandidates = []string{
	"plugin.json",
	filepath.Join(".github", "plugin", "plugin.json"),
	filepath.Join(".claude-plugin", "plugin.json"),
	filepath.Join(".cursor-plugin", "plugin.json"),
}

// PackageVersionRow mirrors PackageVersionRow (version_check.py:29-37): one
// local package's version-alignment status.
type PackageVersionRow struct {
	Path        string
	Version     string // "" when unreadable (Reason names why)
	OK          bool
	Reason      string
	RenderedTag string // "" unless Reason/strategy produced one (tag_pattern)
}

// VersionAlignmentReport mirrors VersionAlignmentReport (version_check.py:
// 40-97): the result of CheckVersionAlignment.
type VersionAlignmentReport struct {
	Strategy string
	Expected string // "" unless Strategy == "lockstep"
	OK       bool
	Packages []PackageVersionRow
}

// ErrorMessages mirrors VersionAlignmentReport.error_messages
// (version_check.py:65-97): one human-readable string per misaligned
// package, byte-identical wording to the Oracle's own per-reason text.
func (r VersionAlignmentReport) ErrorMessages() []string {
	var msgs []string
	for _, row := range r.Packages {
		if row.OK {
			continue
		}
		switch {
		case row.Reason == "missing_version":
			msgs = append(msgs, fmt.Sprintf("%s: missing 'version' in apm.yml", row.Path))
		case row.Reason == "invalid_yaml":
			msgs = append(msgs, fmt.Sprintf("%s: malformed YAML in apm.yml (failed to parse)", row.Path))
		case row.Reason == "invalid_yaml_manifest":
			msgs = append(msgs, fmt.Sprintf("%s: invalid apm.yml (must be a regular file within the project)", row.Path))
		case row.Reason == "no_apm_yml":
			msgs = append(msgs, fmt.Sprintf("%s: no apm.yml or plugin.json found", row.Path))
		case row.Reason == "invalid_plugin_json":
			msgs = append(msgs, fmt.Sprintf("%s: malformed JSON in plugin.json (failed to parse)", row.Path))
		case row.Reason == "missing_plugin_version":
			msgs = append(msgs, fmt.Sprintf("%s: missing 'version' in plugin.json", row.Path))
		case row.Reason == "invalid_plugin_version":
			msgs = append(msgs, fmt.Sprintf("%s: invalid 'version' in plugin.json (must use printable ASCII)", row.Path))
		case strings.HasPrefix(row.Reason, "drift:expected="):
			expected := strings.TrimPrefix(row.Reason, "drift:expected=")
			msgs = append(msgs, fmt.Sprintf("%s: expected %s, found %s", row.Path, expected, row.Version))
		case strings.HasPrefix(row.Reason, "duplicate_tag:other="):
			other := strings.TrimPrefix(row.Reason, "duplicate_tag:other=")
			msgs = append(msgs, fmt.Sprintf("%s: rendered tag collides with %s", row.Path, other))
		default:
			msgs = append(msgs, fmt.Sprintf("%s: %s", row.Path, row.Reason))
		}
	}
	return msgs
}

// isLocalPackage mirrors _is_local_package (version_check.py:100-105).
// isLocalPackageSource (builder.go) only checks the "./" prefix; the Oracle
// ALSO accepts "../" (never reachable through apm-go's own schema today,
// since marketplace package sources are validated at parse time to stay
// within the project root -- kept here anyway for a byte-for-byte
// algorithm mirror rather than silently narrowing it).
func isLocalPackage(entry authoring.PackageEntry) bool {
	return isLocalPackageSource(entry.Source) || strings.HasPrefix(entry.Source, "../")
}

// localPackagePath mirrors _local_path (version_check.py:108-113): the
// project-relative path, stripping a leading "./" and any trailing "/".
func localPackagePath(entry authoring.PackageEntry) string {
	src := strings.TrimSuffix(entry.Source, "/")
	return strings.TrimPrefix(src, "./")
}

// readLocalVersion mirrors _read_local_version (version_check.py:116-162):
// reads a local package's version from its canonical manifest (apm.yml
// first, falling back to plugin.json only when apm.yml does not exist at
// all), applying the SAME safe-open primitives
// (authoring.ResolveLocalSourceAgainstRoot / authoring.OpenLocalFileWithinRoot)
// enrichLocalMetadata already established for reading a local package's
// apm.yml -- fail-closed on any parse/read error rather than silently
// falling back, matching the Oracle's own "apm.yml takes precedence
// whenever it exists; a malformed or incomplete preferred manifest fails
// closed rather than silently falling back to plugin.json" contract.
func readLocalVersion(projectRoot, rel string) (version, reason string) {
	if _, err := authoring.ResolveLocalSourceAgainstRoot(projectRoot, rel); err != nil {
		return "", "invalid_yaml_manifest"
	}

	f, err := authoring.OpenLocalFileWithinRoot(projectRoot, filepath.Join(rel, "apm.yml"))
	if err != nil {
		if errors.Is(err, authoring.ErrLocalFileEscapesRoot) {
			return "", "invalid_yaml_manifest"
		}
		// No apm.yml at all (or any other open failure) -> the Oracle's
		// "package_root / apm.yml doesn't exist" branch: fall back to a
		// plugin.json-based plugin-collection package.
		return readLocalPluginJSONVersion(projectRoot, rel)
	}
	defer f.Close()

	info, statErr := f.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		return "", "invalid_yaml_manifest"
	}

	data, err := readCappedFile(f, localMetadataMaxBytes)
	if err != nil {
		return "", "invalid_yaml"
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		return "", "invalid_yaml"
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return "", "invalid_yaml"
	}
	version = strings.TrimSpace(metadataScalarString(doc.Content[0], "version"))
	if version == "" {
		return "", "missing_version"
	}
	return version, "ok"
}

// readLocalPluginJSONVersion mirrors _read_plugin_json_version
// (version_check.py:165-188): a plugin-collection package with no apm.yml
// reads its version from the first standard plugin.json location.
func readLocalPluginJSONVersion(projectRoot, rel string) (version, reason string) {
	for _, candidate := range pluginJSONLocalCandidates {
		relPath := filepath.Join(rel, candidate)
		f, err := authoring.OpenLocalFileWithinRoot(projectRoot, relPath)
		if err != nil {
			continue
		}
		info, statErr := f.Stat()
		if statErr != nil || !info.Mode().IsRegular() {
			f.Close()
			return "", "invalid_plugin_json"
		}
		data, readErr := readCappedFile(f, maxPluginJSONBytes)
		f.Close()
		if readErr != nil {
			return "", "invalid_plugin_json"
		}
		v, ok := jsonStringField(data, "version")
		if !ok {
			return "", "invalid_plugin_json"
		}
		if v == nil {
			return "", "missing_plugin_version"
		}
		trimmed := strings.TrimSpace(*v)
		if trimmed == "" {
			return "", "missing_plugin_version"
		}
		if !isPrintableASCII(trimmed) {
			return "", "invalid_plugin_version"
		}
		return trimmed, "ok"
	}
	return "", "no_apm_yml"
}

// jsonStringField extracts a top-level string field from a JSON object,
// distinguishing "not valid JSON at all" / "not an object" (ok=false --
// invalid_plugin_json) from "valid object, key absent or not a string"
// (ok=true, value=nil -- missing_plugin_version) from "present as a
// string" (ok=true, value=&s).
func jsonStringField(data []byte, key string) (value *string, ok bool) {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false
	}
	obj, isObj := raw.(map[string]any)
	if !isObj {
		return nil, false
	}
	v, exists := obj[key]
	if !exists {
		return nil, true
	}
	s, isString := v.(string)
	if !isString {
		return nil, true
	}
	return &s, true
}

// isPrintableASCII mirrors Python's str.isascii() and str.isprintable()
// combined (version_check.py:186): every byte in [0x20, 0x7e].
func isPrintableASCII(s string) bool {
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

// resolveTagPattern mirrors _resolve_tag_pattern (version_check.py:191-195):
// a package's own tagPattern wins; otherwise the marketplace's
// build.tagPattern (tagpattern.Compile's own "v{version}" fallback applies
// when that is also empty, matching the Oracle's DefaultPattern).
func resolveTagPattern(entry authoring.PackageEntry, defaultPattern string) string {
	if entry.TagPattern != "" {
		return entry.TagPattern
	}
	if defaultPattern == "" {
		return tagpattern.DefaultPattern
	}
	return defaultPattern
}

// CheckVersionAlignment mirrors check_version_alignment (version_check.py:
// 198-333): runs the release-time version-alignment gate (`apm pack
// --check-versions`) against cfg's local packages. Pure: only reads files
// under projectRoot, no git, no network.
func CheckVersionAlignment(cfg *authoring.AuthoringConfig, projectRoot string) VersionAlignmentReport {
	strategy := cfg.Versioning.Strategy

	var localEntries []authoring.PackageEntry
	for _, e := range cfg.Packages {
		if isLocalPackage(e) {
			localEntries = append(localEntries, e)
		}
	}

	var rows []PackageVersionRow
	rendered := map[string]string{} // rendered tag -> first package path that produced it

	for _, entry := range localEntries {
		rel := localPackagePath(entry)
		version, status := readLocalVersion(projectRoot, rel)
		if status != "ok" {
			rows = append(rows, PackageVersionRow{Path: rel, Reason: status})
			continue
		}

		switch strategy {
		case "lockstep":
			if version == cfg.Version {
				rows = append(rows, PackageVersionRow{Path: rel, Version: version, OK: true, Reason: "matches"})
			} else {
				rows = append(rows, PackageVersionRow{
					Path: rel, Version: version, OK: false,
					Reason: "drift:expected=" + cfg.Version,
				})
			}
		case "tag_pattern":
			pattern := resolveTagPattern(entry, cfg.Build.TagPattern)
			tag := tagpattern.RenderTag(pattern, entry.Name, version)
			if other, collide := rendered[tag]; collide {
				rows = append(rows, PackageVersionRow{
					Path: rel, Version: version, OK: false,
					Reason: "duplicate_tag:other=" + other, RenderedTag: tag,
				})
				// Flip the earlier-matched row to drift, mirroring the
				// Oracle's own "both collide" symmetry (version_check.py:
				// 287-297).
				for i, prev := range rows[:len(rows)-1] {
					if prev.Path == other && prev.OK {
						rows[i] = PackageVersionRow{
							Path: prev.Path, Version: prev.Version, OK: false,
							Reason: "duplicate_tag:other=" + rel, RenderedTag: prev.RenderedTag,
						}
						break
					}
				}
				rendered[tag] = rel
			} else {
				rendered[tag] = rel
				rows = append(rows, PackageVersionRow{
					Path: rel, Version: version, OK: true, Reason: "matches", RenderedTag: tag,
				})
			}
		case "per_package":
			// Only requires a version field; equality is not enforced.
			rows = append(rows, PackageVersionRow{Path: rel, Version: version, OK: true, Reason: "matches"})
		default: // pragma: schema validation already rejects an unknown strategy
			rows = append(rows, PackageVersionRow{Path: rel, Version: version, OK: false, Reason: "unknown_strategy:" + strategy})
		}
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })

	expected := ""
	if strategy == "lockstep" {
		expected = cfg.Version
	}
	overallOK := true
	for _, r := range rows {
		if !r.OK {
			overallOK = false
			break
		}
	}
	return VersionAlignmentReport{
		Strategy: strategy,
		Expected: expected,
		OK:       overallOK,
		Packages: rows,
	}
}
