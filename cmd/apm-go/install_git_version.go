package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/apm-go/apm/internal/semver"
	"github.com/apm-go/apm/internal/yamlcore"
)

// gitDependencyVersion is the version the Oracle's lock records for a git
// dependency (deps/lockfile.py:650-656): the version a semver constraint
// resolved to, otherwise the version of the package in dir at commit.
func gitDependencyVersion(dep resolver.ResolvedDep, dir, commit string) string {
	if dep.Kind == resolver.KindGitSemver && dep.Constraint != "" {
		if v, ok := renderTagVersion(dep.ResolvedTag); ok {
			return v
		}
	}
	return packageVersion(dir, commit)
}

// oracleSemverRe is the Oracle's semver grammar (marketplace/semver.py:34-38).
var oracleSemverRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

// renderTagVersion renders a tag's version as the Oracle's _render_version
// does (deps/git_semver_resolver.py:324-331): numeric parts as integers, so
// leading zeros go.
func renderTagVersion(tag string) (string, bool) {
	m := oracleSemverRe.FindStringSubmatch(semver.StripVPrefix(tag))
	if m == nil {
		return "", false
	}
	num := func(s string) string {
		if s = strings.TrimLeft(s, "0"); s == "" {
			return "0"
		}
		return s
	}
	v := num(m[1]) + "." + num(m[2]) + "." + num(m[3])
	if m[4] != "" {
		v += "-" + m[4]
	}
	if m[5] != "" {
		v += "+" + m[5]
	}
	return v, true
}

// packageVersion is the version the Oracle's CachedDependencySource reads
// from the package its resolver left in dir (install/sources.py:436-472),
// which is how Oracle 0.33.0 records every git dependency it installs. The
// resolver has by then replaced a Claude plugin's apm.yml with one built from
// plugin.json (models/format_detection.py:476-482, deps/plugin_parser.py:1371)
// and stamped a 0.0.0 there with the commit (deps/package_validator.py:261-301).
// A package with neither apm.yml nor plugin manifest is "unknown": a root
// SKILL.md by validation.py:578-581, anything else by sources.py:466-471.
func packageVersion(dir, commit string) string {
	apmYML, hasAPMYML := readPackageYAML(filepath.Join(dir, "apm.yml"))
	apmDir, _ := os.Stat(filepath.Join(dir, ".apm"))
	eligible := hasAPMYML && ((apmDir != nil && apmDir.IsDir()) || declaresDependencies(apmYML))
	claudePluginDir, _ := os.Stat(filepath.Join(dir, ".claude-plugin"))
	switch {
	case !eligible && ((claudePluginDir != nil && claudePluginDir.IsDir()) || pluginJSONPath(dir) != ""):
		v := pluginVersion(pluginJSONPath(dir))
		if v == "" && hasAPMYML {
			if n := nodeMappingValue(apmYML, "version"); n != nil {
				v = n.Value
			}
		}
		if v == "" || v == "0.0.0" {
			v = "0.0.0"
			if len(commit) >= 7 {
				v = commit[:7]
			}
		}
		return v
	case hasAPMYML:
		return manifestVersion(apmYML)
	}
	return "unknown"
}

// readPackageYAML loads the top-level mapping of a package's apm.yml.
func readPackageYAML(path string) (*yamllib.Node, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, true
	}
	if doc.Kind == yamllib.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	if doc.Kind != yamllib.MappingNode {
		return nil, true
	}
	return doc, true
}

// manifestVersion is the version APMPackage.from_apm_yml accepts: a
// non-empty string (models/apm_package.py:472-477), else none.
func manifestVersion(m *yamllib.Node) string {
	v := nodeMappingValue(m, "version")
	if v == nil || v.Kind != yamllib.ScalarNode || v.ShortTag() != "!!str" || strings.TrimSpace(v.Value) == "" {
		return ""
	}
	return v.Value
}

// declaresDependencies is the Oracle's _apm_yml_declares_dependencies
// (models/validation.py:298-335).
func declaresDependencies(m *yamllib.Node) bool {
	for _, section := range []string{"dependencies", "devDependencies"} {
		block := nodeMappingValue(m, section)
		for _, key := range []string{"apm", "mcp"} {
			list := nodeMappingValue(block, key)
			if list == nil || list.Kind != yamllib.SequenceNode {
				continue
			}
			for _, entry := range list.Content {
				if entry.Kind == yamllib.MappingNode || (entry.Kind == yamllib.ScalarNode && entry.ShortTag() == "!!str") {
					return true
				}
			}
		}
	}
	return false
}

// pluginJSONPath is the Oracle's find_plugin_json (utils/helpers.py:105-129).
func pluginJSONPath(dir string) string {
	for _, rel := range deploy.PluginManifestPaths {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// pluginVersion is the plugin manifest's version when it is a non-empty
// string, the one kind the synthesized apm.yml loads with.
func pluginVersion(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var m struct {
		Version any `json:"version"`
	}
	if json.Unmarshal(data, &m) != nil {
		return ""
	}
	v, _ := m.Version.(string)
	return v
}
