package marketplace

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/manifest"
)

// coercePluginType normalizes a dict-shaped plugin source's type
// discriminator, mirroring the Python original's _coerce_dict_plugin_type
// (resolver.py:185-206, design.md gaps A4). It reads "type" -> "source" ->
// "kind" in that order (the first non-empty string wins, lower-cased and
// trimmed) -- three keys, NOT the two ("type"/"source") the manifest parse
// layer (models.go's rawPlugin.normalize) checks when deciding whether to
// drop an npm-typed plugin at parse time. That deliberate two-vs-three-key
// gap IS mkt-026's dual-layer behavior: a "kind: npm" source survives
// manifest parsing (models.go never looks at "kind") and is only rejected
// here, at resolution (resolvePluginSource).
//
// When none of the three keys carry a value, the type is inferred from
// "repo" plus "subdir": a "repo" containing "/" implies "github", or
// "git-subdir" when "subdir" is also non-empty. Anything else (no usable
// "repo") returns "" -- the caller's job to treat that as "no inferrable
// type".
func coercePluginType(m map[string]any) string {
	for _, key := range []string{"type", "source", "kind"} {
		if v, ok := m[key].(string); ok {
			v = strings.ToLower(strings.TrimSpace(v))
			if v != "" {
				return v
			}
		}
	}
	repo := stringField(m, "repo")
	if !strings.Contains(strings.TrimSpace(repo), "/") {
		return ""
	}
	if strings.TrimSpace(stringField(m, "subdir")) != "" {
		return "git-subdir"
	}
	return "github"
}

// stringField reads a string-typed key from a dict-shaped plugin source,
// tolerating an absent or wrong-typed value as "" rather than panicking --
// manifest.json content is untrusted input (mirrors Python's permissive
// dict.get(key, "") plus isinstance(..., str) idiom used throughout
// resolver.py's per-type resolvers).
func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// resolvePluginSource maps a marketplace plugin's Source field to a
// canonical "owner/repo[/path][#ref]" dependency string (mkt-026),
// mirroring the Python original's resolve_plugin_source
// (resolver.py:659-719). marketplaceOwner/marketplaceRepo supply the
// relative-string case's base coordinate (the registered marketplace's own
// repo).
//
// This function does NOT handle the mkt-025 local-marketplace fast path
// (resolveLocalRelativeSource) or mkt-027's non-GitHub-family
// structured-DepRef case -- both are the calling ResolvePlugin flow's
// responsibility (a later step), exactly as in the Python original, where
// resolve_plugin_source is one helper resolve_marketplace_plugin calls
// after its own local/host-specific branching.
func resolvePluginSource(plugin *MarketplacePlugin, marketplaceOwner, marketplaceRepo string) (string, error) {
	switch src := plugin.Source.(type) {
	case nil:
		return "", fmt.Errorf("plugin %q has no source defined", plugin.Name)
	case string:
		return resolveRelativeSource(src, marketplaceOwner, marketplaceRepo)
	case map[string]any:
		sourceType := coercePluginType(src)
		switch sourceType {
		case "":
			return "", fmt.Errorf("plugin %q has dict source with no 'type' and no inferrable 'repo' field", plugin.Name)
		case "github":
			return resolveGitHubSource(plugin.Name, src)
		case "url":
			return resolveURLSource(plugin.Name, src)
		case "git-subdir", "gitlab":
			return resolveGitSubdirSource(plugin.Name, src)
		case "npm":
			return "", fmt.Errorf("plugin %q uses npm source type which is not supported by APM; APM requires Git-based sources (ask the marketplace maintainer to add a 'github' source)", plugin.Name)
		default:
			return "", fmt.Errorf("plugin %q has unsupported source type: %q", plugin.Name, sourceType)
		}
	default:
		return "", fmt.Errorf("plugin %q has unrecognized source format: %T", plugin.Name, plugin.Source)
	}
}

// resolveRelativeSource resolves a plain relative-path plugin source (a
// string, not a dict) to "marketplaceOwner/marketplaceRepo[/rel]" --
// mirrors _resolve_relative_source (resolver.py:590-605). An empty or
// "."-only relative path resolves to the marketplace project root itself.
func resolveRelativeSource(source, marketplaceOwner, marketplaceRepo string) (string, error) {
	rel, err := normalizeRelativePluginSource(source)
	if err != nil {
		return "", err
	}
	if rel != "" && rel != "." {
		return fmt.Sprintf("%s/%s/%s", marketplaceOwner, marketplaceRepo, rel), nil
	}
	return fmt.Sprintf("%s/%s", marketplaceOwner, marketplaceRepo), nil
}

// normalizeRelativePluginSource trims a relative plugin source string to its
// canonical relative-path form and rejects any "."/".." path-traversal
// segment, mirroring _normalise_relative_plugin_source
// (resolver.py:608-632, sans the metadata.pluginRoot bare-name backfill --
// out of this step's scope). Returns "" or "." when source names the
// marketplace project root itself.
func normalizeRelativePluginSource(source string) (string, error) {
	rel := strings.Trim(source, "/")
	rel = strings.TrimPrefix(rel, "./")
	rel = strings.Trim(rel, "/")
	if rel != "" && rel != "." {
		if err := validateRelativeSourcePath(rel, "relative source path"); err != nil {
			return "", err
		}
	}
	return rel, nil
}

// resolveLocalRelativeSource resolves a purely relative-path plugin source
// declared by a local (Kind() == KindLocal) marketplace's manifest to an
// absolute local filesystem canonical path -- mkt-025's fast path,
// mirroring _resolve_local_relative_source (resolver.py:635-656). Callers
// are responsible for having already confirmed mkt.Kind() == KindLocal and
// that plugin.Source is a string; the routing decision itself belongs to
// ResolvePlugin's main flow (a later step), not this helper.
func resolveLocalRelativeSource(source string, mkt *MarketplaceSource) (string, error) {
	rel, err := normalizeRelativePluginSource(source)
	if err != nil {
		return "", err
	}
	if mkt.URL == "" {
		return "", fmt.Errorf("marketplace %q is kind=local but has no resolvable filesystem path; cannot resolve relative plugin source %q", mkt.Name, source)
	}
	if rel != "" && rel != "." {
		return filepath.Join(mkt.URL, filepath.FromSlash(rel)), nil
	}
	return mkt.URL, nil
}

// resolveGitHubSource resolves a dict source whose coerced type is "github"
// to "owner/repo[/path][#ref]" -- mirrors _resolve_github_source
// (resolver.py:514-536). Accepts a "path" field (the Copilot CLI format) as
// a virtual subdirectory, in addition to "repo"'s "repository" alias.
func resolveGitHubSource(pluginName string, src map[string]any) (string, error) {
	repo := stringField(src, "repo")
	if repo == "" {
		repo = stringField(src, "repository")
	}
	if repo == "" || !strings.Contains(repo, "/") {
		return "", fmt.Errorf("plugin %q has invalid github source: 'repo' (or 'repository') field must be 'owner/repo', got %q", pluginName, repo)
	}
	ref := stringField(src, "ref")
	path := strings.Trim(stringField(src, "path"), "/")

	base := repo
	if path != "" {
		if err := validateRelativeSourcePath(path, "github source path"); err != nil {
			return "", err
		}
		base = repo + "/" + path
	}
	if ref != "" {
		return base + "#" + ref, nil
	}
	return base, nil
}

// resolveURLSource resolves a dict source whose coerced type is "url" by
// delegating to manifest.ParseDepString to extract an owner/repo coordinate
// from any Git URL shape it accepts (HTTPS, SCP-style SSH) -- mirrors
// _resolve_url_source (resolver.py:539-559). The URL's host is deliberately
// NOT preserved in the canonical, matching the Python original (tracked
// upstream as #1010, not a Go-side gap to fix here).
func resolveURLSource(pluginName string, src map[string]any) (string, error) {
	rawURL := stringField(src, "url")
	if rawURL == "" {
		return "", fmt.Errorf("plugin %q has a url source with an empty 'url' field", pluginName)
	}
	dep, err := manifest.ParseDepString(rawURL)
	if err != nil {
		return "", fmt.Errorf("plugin %q: cannot resolve url source %q: %w", pluginName, rawURL, err)
	}
	if dep.IsLocal {
		return "", fmt.Errorf("plugin %q: url source %q resolves to a local path, not a git coordinate", pluginName, rawURL)
	}
	if dep.Reference != "" {
		return dep.RepoURL + "#" + dep.Reference, nil
	}
	return dep.RepoURL, nil
}

// resolveGitSubdirSource resolves a dict source whose coerced type is
// "git-subdir" or "gitlab" to "owner/repo[/subdir][#ref]" -- mirrors
// _resolve_git_subdir_source (resolver.py:562-587); GitLab-native manifest
// entries share this resolver with git-subdir, per the Python original's
// own comment ("GitLab-native marketplace entries mirror git-subdir").
func resolveGitSubdirSource(pluginName string, src map[string]any) (string, error) {
	repo := stringField(src, "repo")
	if repo == "" {
		repo = stringField(src, "url")
	}
	if strings.Contains(repo, "://") {
		return "", fmt.Errorf("plugin %q has invalid git-subdir source: expected 'owner/repo' but got a URL %q; use source type 'url' for full URL references", pluginName, repo)
	}
	if repo == "" || !strings.Contains(repo, "/") {
		return "", fmt.Errorf("plugin %q has invalid git-subdir source: 'repo' (or 'url') must be 'owner/repo', got %q", pluginName, repo)
	}
	ref := stringField(src, "ref")
	subdir := stringField(src, "subdir")
	if subdir == "" {
		subdir = stringField(src, "path")
	}
	subdir = strings.Trim(subdir, "/")

	base := repo
	if subdir != "" {
		if err := validateRelativeSourcePath(subdir, "git-subdir source path"); err != nil {
			return "", err
		}
		base = repo + "/" + subdir
	}
	if ref != "" {
		return base + "#" + ref, nil
	}
	return base, nil
}

// validateRelativeSourcePath rejects a "."/".." path-traversal segment
// (checked against both the raw segment and its iteratively
// percent-decoded form, up to 8 rounds) anywhere in a manifest-declared
// relative source/subdir/path field -- mirrors the Python original's
// validate_path_segments(path_str, context, reject_empty=False,
// allow_current_dir=False) (utils/path_security.py:32-82). This is
// deliberately NOT source.go's validateSourcePathSegment: that helper's
// stricter reject set (also "~", and empty segments) is specific to the
// `marketplace add SOURCE` CLI argument (mkt-010), a different context from
// a manifest's own relative-source/path/subdir field.
func validateRelativeSourcePath(path, context string) error {
	normalized := strings.ReplaceAll(path, `\`, "/")
	for _, seg := range strings.Split(normalized, "/") {
		decoded := seg
		for i := 0; i < 8; i++ {
			next, err := url.PathUnescape(decoded)
			if err != nil || next == decoded {
				break
			}
			decoded = next
		}
		if seg == "." || seg == ".." || decoded == "." || decoded == ".." {
			return fmt.Errorf("invalid %s %q: segment %q is a traversal sequence", context, path, seg)
		}
	}
	return nil
}
