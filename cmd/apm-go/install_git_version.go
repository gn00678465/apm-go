package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
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
	switch {
	case isClaudePluginDir(dir):
		v := pluginField(pluginJSONPath(dir), "version")
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

// pluginField is a field of the plugin manifest at path when it is a
// non-empty string, the one kind the apm.yml the Oracle builds from it loads
// with.
func pluginField(path, field string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return ""
	}
	v, _ := m[field].(string)
	return v
}

// isClaudePluginDir is the Oracle's MARKETPLACE_PLUGIN detection
// (models/format_detection.py:455-470) of the package at dir: a plugin
// manifest or a .claude-plugin/ directory, and no apm.yml that makes it an
// APM package.
func isClaudePluginDir(dir string) bool {
	if deploy.IsAPMPackage(dir) {
		return false
	}
	info, _ := os.Stat(filepath.Join(dir, ".claude-plugin"))
	return (info != nil && info.IsDir()) || pluginJSONPath(dir) != ""
}

// gitPackageIdentity is the name and package_type Oracle 0.33.0's lock
// records for a git dependency (measured: issue151/r3-or/kinds,
// r5-or/kinds2, issue151-impl/m4-or/kinds3, gh1-or/vfile). The type is what
// detect_package_type gives the package its resolver leaves in apm_modules
// (models/format_detection.py:436-491), where a Claude plugin has by then an
// apm.yml and .apm/ built from plugin.json, so it is an APM package; the
// name is that of the package CachedDependencySource reads there
// (install/sources.py:436-471): the apm.yml's, a plugin manifest's or, for
// one with none, the name of the slot the Oracle staged it in
// (install/resolution_staging.py:215-222, deps/plugin_parser.py:501-505), a
// Claude skill's frontmatter or directory (models/validation.py:573), else
// the repository's last segment, a subdirectory skill bundle's too. A virtual
// file package is the apm.yml the Oracle synthesizes for it, named by
// get_virtual_package_name. A package the Oracle refuses to install (a hook
// package with no apm.yml) gets neither.
func gitPackageIdentity(dir, modulesRel, repoURL, virtualPath string) (name, packageType string) {
	if manifest.VirtualTypeOf(virtualPath) == "file" {
		return virtualPackageName(repoURL, virtualPath), lockfile.PackageTypeAPMPackage
	}
	apmYML, hasAPMYML := readPackageYAML(filepath.Join(dir, "apm.yml"))
	apmName := ""
	if n := nodeMappingValue(apmYML, "name"); n != nil && n.Kind == yamllib.ScalarNode {
		apmName = n.Value
	}
	_, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	rootSkill := err == nil
	nested, _ := filepath.Glob(filepath.Join(dir, "skills", "*", "SKILL.md"))
	plugin := isClaudePluginDir(dir)
	typed := func(base string) string {
		switch {
		case rootSkill:
			return "hybrid"
		case len(nested) > 0:
			return "skill_bundle"
		}
		return base
	}
	switch {
	case plugin:
		name = pluginField(pluginJSONPath(dir), "name")
		if name == "" {
			sum := sha256.Sum256([]byte(modulesRel))
			name = hex.EncodeToString(sum[:])[:16]
		}
		return name, typed(lockfile.PackageTypeAPMPackage)
	case deploy.IsAPMPackage(dir):
		return apmName, typed(lockfile.PackageTypeAPMPackage)
	case hasAPMYML && rootSkill:
		return apmName, "hybrid"
	case rootSkill:
		return deploy.SkillName(filepath.Join(dir, "SKILL.md"), filepath.Base(dir)), "claude_skill"
	case len(nested) > 0:
		if hasAPMYML {
			return apmName, "skill_bundle"
		}
		return path.Base(repoURL), "skill_bundle"
	}
	return "", ""
}

// packageContentHash is the content_hash Oracle 0.33.0 records for a
// dependency that is not local (install/sources.py:266-267,554-555,800-801):
// compute_package_hash of its directory below apm_modules. A Claude plugin
// and a virtual file package get none: the Oracle hashes the apm.yml and .apm/
// it builds into their directory, which apm-go does not write, and a later
// Oracle install that downloads the package again refuses a hash that does
// not match (sources.py:803-840). A package with no directory there, which
// the install has not fetched, gets none either.
func packageContentHash(moduleDir, virtualPath string) (string, error) {
	if manifest.VirtualTypeOf(virtualPath) == "file" || isClaudePluginDir(moduleDir) {
		return "", nil
	}
	if info, err := os.Stat(moduleDir); err != nil || !info.IsDir() {
		return "", nil
	}
	return lockfile.ComputePackageHash(moduleDir)
}
