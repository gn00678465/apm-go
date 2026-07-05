// This file (codexmapper.go) implements CodexMapper, mkt-052/053's Codex
// marketplace.json composition -- a materially different output shape from
// ClaudeMapper's (mapper.go), NOT "the Claude shape plus category" (design.md's
// explicit warning: "⚠️與 Claude 差異很大"). Ported field-by-field from
// Python apm's output_mappers.py (CodexMarketplaceMapper.compose and
// _codex_source, lines 226-309) -- see design.md's Codex-specific field
// table for the authoritative source this file implements against.
package build

import (
	"fmt"

	"github.com/apm-go/apm/internal/marketplace/authoring"
)

// CodexDocument is the top-level Codex marketplace.json document: only
// name, interface.displayName, and plugins -- no description/version/owner/
// metadata (those are Claude-only top-level fields).
type CodexDocument struct {
	Name      string         `json:"name"`
	Interface CodexInterface `json:"interface"`
	Plugins   []CodexPlugin  `json:"plugins"`
}

// CodexInterface is marketplace.json's Codex-only top-level "interface"
// object: displayName always mirrors config.name (design.md).
type CodexInterface struct {
	DisplayName string `json:"displayName"`
}

// CodexPlugin is one marketplace.json "plugins[]" entry in Codex's shape:
// name, source, a fixed policy, and a required category -- no description/
// version/author/license/repository/tags/homepage (those are Claude-only
// plugin-level fields). Source is either a *CodexLocalSource (a local
// package) or a *RemoteSource (a remote package) -- see composeCodexSource.
type CodexPlugin struct {
	Name     string      `json:"name"`
	Source   any         `json:"source"`
	Policy   CodexPolicy `json:"policy"`
	Category string      `json:"category"`
}

// CodexPolicy is every Codex plugin's fixed installation/authentication
// policy (design.md: "policy{installation:AVAILABLE,authentication:ON_INSTALL}
// 固定值" -- never derived from input, always these two literal values).
type CodexPolicy struct {
	Installation   string `json:"installation"`
	Authentication string `json:"authentication"`
}

// CodexLocalSource is a local package's Codex "source" value: unlike
// Claude's plain relative-path STRING, Codex's local source is always a
// DICT (design.md: "本地 source 是 dict {source:local,path}"). Path is the
// package's raw declared source, never pluginRoot-subtracted -- the Python
// original's _codex_source never calls _subtract_plugin_root for the local
// branch, unlike ClaudeMapper's composeClaudePlugin.
type CodexLocalSource struct {
	Source string `json:"source"`
	Path   string `json:"path"`
}

// CategoryRequiredError is mkt-053's category-required gate: every resolved
// package must declare a non-empty category for Codex output. This is the
// ONLY place this rule is enforced (F3 fix): it deliberately does not also
// live in internal/marketplace/authoring.LoadAuthoringConfig's config-
// loading layer, since that loader is shared by callers (e.g. `apm pack -m
// claude`, `apm marketplace package add/remove/set`) that must not be
// blocked by a codex-only rule when codex was never actually going to be
// composed -- mirroring the Python original's own compose-time-only
// BuildError (output_mappers.py, not yml_schema.py).
type CategoryRequiredError struct {
	Package string
}

func (e *CategoryRequiredError) Error() string {
	return fmt.Sprintf("package %q is missing category required for Codex output", e.Package)
}

// CodexMapper implements mkt-052/053's Codex marketplace.json output
// composition.
type CodexMapper struct{}

// Compose produces the Codex marketplace.json document for resolved.
// Codex composes no diagnostics of its own (no duplicate-name warning,
// unlike ClaudeMapper -- Python's CodexMarketplaceMapper.compose never
// calls _duplicate_name_warnings), so the second return value is always
// nil; a non-nil error is always a hard failure (currently only
// CategoryRequiredError).
func (CodexMapper) Compose(cfg *authoring.AuthoringConfig, resolved []ResolvedPackage) (CodexDocument, []string, error) {
	doc := CodexDocument{
		Name:      cfg.Name,
		Interface: CodexInterface{DisplayName: cfg.Name},
	}

	plugins := make([]CodexPlugin, 0, len(resolved))
	for _, pkg := range resolved {
		if pkg.Entry.Category == "" {
			return CodexDocument{}, nil, &CategoryRequiredError{Package: pkg.Entry.Name}
		}
		plugins = append(plugins, CodexPlugin{
			Name:     pkg.Entry.Name,
			Source:   composeCodexSource(pkg),
			Policy:   CodexPolicy{Installation: "AVAILABLE", Authentication: "ON_INSTALL"},
			Category: pkg.Entry.Category,
		})
	}
	doc.Plugins = plugins
	return doc, nil, nil
}

// composeCodexSource implements mkt-052's Codex-specific "source" shapes
// (design.md, output_mappers.py:283-309):
//
//   - a local package -> *CodexLocalSource{"local", entry.source} (a DICT,
//     never a plain string).
//   - a remote package with a subdir -> *RemoteSource{"git-subdir", url,
//     path} -- the same shape ClaudeMapper emits.
//   - any other remote package -> *RemoteSource{"url", url} -- Codex has NO
//     github-shorthand form at all (design.md: "遠端無 github shorthand"),
//     unlike Claude, which falls back to {"source":"github","repo":...} on
//     the default host.
//
// url is the non-default host's https:// URL when pkg.Host is set,
// otherwise pkg.SourceRepo verbatim (mirroring Python's own
// `_remote_source_url(pkg) or pkg.source_repo` fallback -- on the default
// host this is a bare "owner/repo" string, not a full URL, exactly as the
// Python original composes it). ref/sha are appended to either shape when
// known.
func composeCodexSource(pkg ResolvedPackage) any {
	if pkg.IsLocal {
		return &CodexLocalSource{Source: "local", Path: pkg.Entry.Source}
	}

	urlOrRepo := pkg.SourceRepo
	if pkg.Host != "" {
		urlOrRepo = "https://" + pkg.Host + "/" + pkg.SourceRepo
	}

	src := &RemoteSource{}
	if pkg.Subdir != "" {
		src.Source = "git-subdir"
		src.URL = urlOrRepo
		src.Path = pkg.Subdir
	} else {
		src.Source = "url"
		src.URL = urlOrRepo
	}
	if pkg.Ref != "" {
		src.Ref = pkg.Ref
	}
	if pkg.SHA != "" {
		src.SHA = pkg.SHA
	}
	return src
}
