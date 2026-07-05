// This file (mapper.go) implements ClaudeMapper, mkt-050/052 修訂版's Claude
// Code marketplace.json composition -- translating cfg (the marketplace:
// authoring block, internal/marketplace/authoring.AuthoringConfig) and
// resolved (builder.go's ResolvePackages output) into the exact field
// shape the upstream Claude Code marketplace.json schema subset expects,
// with every APM-only field (build/tagPattern/include_prerelease/category)
// stripped and no semver range ever leaking into an output "version".
//
// Ported field-by-field from Python apm's output_mappers.py
// (ClaudeMarketplaceMapper.compose, lines 53-223) -- see design.md's
// per-field trigger-condition table for the authoritative source this file
// implements against. CodexMapper (mkt-052/053, a materially different
// output shape) is a later, not-yet-landed step of this sub-task's
// implement.md.
package build

import (
	"errors"
	"fmt"
	"strings"

	"github.com/apm-go/apm/internal/marketplace/authoring"
)

// ClaudeDocument is the top-level Claude Code marketplace.json document.
// Struct field declaration order matches Python's OrderedDict insertion
// order (name, description, version, owner, metadata, plugins) so
// encoding/json.Marshal emits the same key order without needing an
// ordered-map type.
type ClaudeDocument struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Version     string         `json:"version,omitempty"`
	Owner       ClaudeOwner    `json:"owner"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Plugins     []ClaudePlugin `json:"plugins"`
}

// ClaudeOwner is marketplace.json's top-level "owner" object: name is
// always present, email/url only when the authoring config's owner block
// supplied a value (design.md: "email 也會輸出,先前設計漏了").
type ClaudeOwner struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// ClaudePlugin is one marketplace.json "plugins[]" entry. Source is either
// a plain string (a local package's relative path, pluginRoot-stripped) or
// a *RemoteSource (a remote package's structured source dict) -- mirroring
// the Python original's Union[str, dict] "source" value.
type ClaudePlugin struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Version     string            `json:"version,omitempty"`
	Author      map[string]string `json:"author,omitempty"`
	License     string            `json:"license,omitempty"`
	Repository  string            `json:"repository,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Homepage    string            `json:"homepage,omitempty"`
	Source      any               `json:"source"`
}

// RemoteSource is a remote package's structured plugin.source dict
// (mkt-050 修訂版's four-rule composition, see composeRemoteSource): the
// "source" discriminator is always present; exactly one of
// Repo/URL/(URL+Path) is populated depending on which of the four rules
// fired, and Ref/SHA are appended when known. Field declaration order
// (Source, Repo, URL, Path, Ref, SHA) reproduces the Python original's key
// order for whichever fields end up actually set -- omitempty drops the
// rest.
type RemoteSource struct {
	Source string `json:"source"`
	Repo   string `json:"repo,omitempty"`
	URL    string `json:"url,omitempty"`
	Path   string `json:"path,omitempty"`
	Ref    string `json:"ref,omitempty"`
	SHA    string `json:"sha,omitempty"`
}

// ClaudeMapper implements mkt-050/052 修訂版's Claude Code marketplace.json
// output composition.
type ClaudeMapper struct{}

// Compose produces the Claude marketplace.json document for resolved,
// against cfg's owner/metadata/packages[] declarations. The returned
// warnings are non-fatal diagnostics (a duplicate plugin name, or a local
// package's source falling outside metadata.pluginRoot) that never abort
// the build; a non-nil error is always a hard failure (currently only
// subtractPluginRoot's PluginRootError -- pluginRoot subtraction yielding
// an empty, absolute, or traversal-containing path, design.md's "結果為
// 空/絕對/含 .. -> BuildError").
func (ClaudeMapper) Compose(cfg *authoring.AuthoringConfig, resolved []ResolvedPackage) (ClaudeDocument, []string, error) {
	doc := ClaudeDocument{Name: cfg.Name}
	if cfg.DescriptionOverridden && cfg.Description != "" {
		doc.Description = cfg.Description
	}
	if cfg.VersionOverridden && cfg.Version != "" {
		doc.Version = cfg.Version
	}
	doc.Owner = ClaudeOwner{Name: cfg.Owner.Name, Email: cfg.Owner.Email, URL: cfg.Owner.URL}
	if len(cfg.Metadata) > 0 {
		doc.Metadata = cfg.Metadata
	}

	pluginRoot, _ := cfg.Metadata["pluginRoot"].(string)

	var warnings []string
	plugins := make([]ClaudePlugin, 0, len(resolved))
	for _, pkg := range resolved {
		plugin, warning, err := composeClaudePlugin(pkg, pluginRoot)
		if err != nil {
			return ClaudeDocument{}, nil, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
		plugins = append(plugins, plugin)
	}
	doc.Plugins = plugins

	warnings = append(warnings, duplicateNameWarnings(plugins)...)
	return doc, warnings, nil
}

// composeClaudePlugin builds one plugins[] entry for pkg, per design.md's
// plugin-level field table.
func composeClaudePlugin(pkg ResolvedPackage, pluginRoot string) (ClaudePlugin, string, error) {
	entry := pkg.Entry
	plugin := ClaudePlugin{Name: entry.Name}

	// description/version: for a remote package, pkg.RemoteDescription/
	// RemoteVersion are already the final curator-wins-resolved values
	// (metadata.go's enrichRemoteMetadata, this sub-task's step 2 --
	// including the is_display_version range-leak guard), so they are used
	// as-is with no further precedence logic here. A local package's own
	// declared entry.Description/entry.Version -- unfiltered, since a local
	// package's version is a plain display value, never a range -- always
	// wins when the curator supplied one; otherwise it falls back to
	// pkg.RemoteDescription/RemoteVersion, which for a local package holds
	// whatever that package's own on-disk apm.yml declares (F1 fix,
	// metadata.go's enrichLocalMetadata) -- mirroring output_mappers.py's
	// is_local branch, which applies this identical curator-wins precedence
	// against that package's own metadata rather than a remote fetch.
	if pkg.IsLocal {
		plugin.Description = entry.Description
		if plugin.Description == "" {
			plugin.Description = pkg.RemoteDescription
		}
		plugin.Version = entry.Version
		if plugin.Version == "" {
			plugin.Version = pkg.RemoteVersion
		}
	} else {
		plugin.Description = pkg.RemoteDescription
		plugin.Version = pkg.RemoteVersion
	}

	if len(entry.Author) > 0 {
		plugin.Author = entry.Author
	}
	if entry.License != "" {
		plugin.License = entry.License
	}
	if entry.Repository != "" {
		plugin.Repository = entry.Repository
	}
	if len(pkg.Tags) > 0 {
		plugin.Tags = pkg.Tags
	}
	if pkg.IsLocal && entry.Homepage != "" {
		plugin.Homepage = entry.Homepage
	}

	var warning string
	if pkg.IsLocal {
		sourceValue := entry.Source
		if pluginRoot != "" {
			stripped, err := subtractPluginRoot(entry.Source, pluginRoot)
			switch {
			case err == nil:
				sourceValue = stripped
			case errors.Is(err, errSourceOutsidePluginRoot):
				warning = fmt.Sprintf(
					"[!] Package %q: source %q is outside pluginRoot %q -- emitted as-is",
					entry.Name, entry.Source, pluginRoot,
				)
			default:
				return ClaudePlugin{}, "", err
			}
		}
		plugin.Source = sourceValue
	} else {
		plugin.Source = composeRemoteSource(pkg)
	}

	return plugin, warning, nil
}

// composeRemoteSource implements mkt-050 修訂版's four-rule remote source
// composition (design.md, output_mappers.py:185-201):
//
//  1. (handled by the caller: local packages never reach this function)
//  2. a subdir -> {"source":"git-subdir", "url", "path"}
//  3. a non-default host (GHE etc; github shorthand only ever resolves to
//     github.com) -> {"source":"url", "url"}
//  4. otherwise -> {"source":"github", "repo"}
//
// ref/sha are appended to whichever of 2-4 fired, when known.
// ResolvedPackage carries no SourceURL/sourceBase field (design.md's
// explicit "sourceBase 明確延後"), so the URL a non-default host emits is
// always derived from Host+SourceRepo, never a curator-composed
// sourceBase URL.
func composeRemoteSource(pkg ResolvedPackage) *RemoteSource {
	remoteURL := ""
	if pkg.Host != "" {
		remoteURL = "https://" + pkg.Host + "/" + pkg.SourceRepo
	}

	src := &RemoteSource{}
	switch {
	case pkg.Subdir != "":
		src.Source = "git-subdir"
		if remoteURL != "" {
			src.URL = remoteURL
		} else {
			src.URL = pkg.SourceRepo
		}
		src.Path = pkg.Subdir
	case remoteURL != "":
		src.Source = "url"
		src.URL = remoteURL
	default:
		src.Source = "github"
		src.Repo = pkg.SourceRepo
	}
	if pkg.Ref != "" {
		src.Ref = pkg.Ref
	}
	if pkg.SHA != "" {
		src.SHA = pkg.SHA
	}
	return src
}

// duplicateNameWarnings implements mkt-050's "同名 plugin 提示 consumers 會
// 看到重複條目" diagnostic (output_mappers.py's _duplicate_name_warnings):
// one warning per plugin name seen more than once, naming both entries'
// source labels.
func duplicateNameWarnings(plugins []ClaudePlugin) []string {
	seen := make(map[string]string, len(plugins))
	var warnings []string
	for _, p := range plugins {
		label := sourceLabel(p.Source)
		if prev, ok := seen[p.Name]; ok {
			warnings = append(warnings, fmt.Sprintf(
				"Duplicate package name %q: %q and %q. Consumers will see duplicate entries in browse.",
				p.Name, prev, label,
			))
		} else {
			seen[p.Name] = label
		}
	}
	return warnings
}

// sourceLabel returns a short human-readable label for a plugin.Source
// value, for duplicateNameWarnings' diagnostic text: the path string
// itself for a local package, or (for a remote package) whichever of
// path/repo is set on the RemoteSource, mirroring Python's
// `source.get("path") or source.get("repo") or source.get("repository", "?")`
// (a "url"-shaped source, which sets neither, always falls through to "?").
func sourceLabel(source any) string {
	switch s := source.(type) {
	case string:
		return s
	case *RemoteSource:
		if s.Path != "" {
			return s.Path
		}
		if s.Repo != "" {
			return s.Repo
		}
		return "?"
	default:
		return "?"
	}
}

// ── pluginRoot subtraction (mkt-050 修訂版, output_mappers.py:398-422) ────

// errSourceOutsidePluginRoot is subtractPluginRoot's sentinel for "source
// is not under pluginRoot at all" -- the Python original's
// PurePosixPath.relative_to raising ValueError, which composeClaudePlugin
// downgrades to a warning (keeping the original source as-is) rather than
// aborting the build. This is deliberately distinct from PluginRootError:
// only this ValueError-equivalent case is ever caught softly; the
// PluginRootError cases below always propagate as a hard failure, exactly
// mirroring the Python original's own "except ValueError" (never
// "except BuildError") around this call.
var errSourceOutsidePluginRoot = errors.New("source is outside pluginRoot")

// PluginRootError is a hard build failure from subtractPluginRoot's
// post-computation invariant checks: the pluginRoot-relative path came out
// empty, absolute, or containing a ".." segment.
type PluginRootError struct {
	Source     string
	PluginRoot string
	Reason     string
}

func (e *PluginRootError) Error() string {
	return fmt.Sprintf("subtracting pluginRoot %q from source %q: %s", e.PluginRoot, e.Source, e.Reason)
}

// subtractPluginRoot implements _subtract_plugin_root: strip a local
// package's leading "./" and metadata.pluginRoot's own leading "./"/
// trailing "/", then compute source's path relative to root the same way
// Python's PurePosixPath(source).relative_to(PurePosixPath(root)) does --
// an exact-prefix match only, never introducing "..", and re-prefixes a
// successful result with "./".
func subtractPluginRoot(source, pluginRoot string) (string, error) {
	normSource := strings.TrimSuffix(strings.TrimPrefix(source, "./"), "/")
	normRoot := strings.TrimSuffix(strings.TrimPrefix(pluginRoot, "./"), "/")

	rel, err := purePosixRelativeTo(normSource, normRoot)
	if err != nil {
		return "", err
	}
	return validatePluginRootResult(source, pluginRoot, rel)
}

// purePosixRelativeTo mirrors PurePosixPath(source).relative_to(root): root
// must be an exact segment-wise prefix of source, or errSourceOutsidePluginRoot
// is returned (Python's ValueError). Equal paths return "." (Python's own
// PurePosixPath(".") result), never "" -- validatePluginRootResult treats
// both the same way.
func purePosixRelativeTo(source, root string) (string, error) {
	srcParts := strings.Split(source, "/")
	rootParts := strings.Split(root, "/")
	if len(rootParts) > len(srcParts) {
		return "", errSourceOutsidePluginRoot
	}
	for i, part := range rootParts {
		if srcParts[i] != part {
			return "", errSourceOutsidePluginRoot
		}
	}
	rel := srcParts[len(rootParts):]
	if len(rel) == 0 {
		return ".", nil
	}
	return strings.Join(rel, "/"), nil
}

// validatePluginRootResult enforces the invariants _subtract_plugin_root
// checks after a successful relative-path computation: empty/"." result,
// an absolute-looking result, or a result containing a ".." segment are
// each a hard PluginRootError. In practice purePosixRelativeTo can never
// actually produce the latter two shapes (an exact-prefix relative
// computation only ever returns a strict suffix of source's own segments,
// never introducing "/" or "..") -- these two guards are retained for
// parity with the Python original and defense in depth, and are exercised
// directly by this file's own unit tests rather than through
// subtractPluginRoot's normal call path.
func validatePluginRootResult(source, pluginRoot, rel string) (string, error) {
	if rel == "" || rel == "." {
		return "", &PluginRootError{Source: source, PluginRoot: pluginRoot, Reason: "yields empty path"}
	}
	if strings.HasPrefix(rel, "/") {
		return "", &PluginRootError{Source: source, PluginRoot: pluginRoot, Reason: fmt.Sprintf("produced absolute path: %q", rel)}
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return "", &PluginRootError{Source: source, PluginRoot: pluginRoot, Reason: fmt.Sprintf("produced path with traversal: %q", rel)}
		}
	}
	return "./" + rel, nil
}
