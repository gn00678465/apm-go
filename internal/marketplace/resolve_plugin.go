package marketplace

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/apm-go/apm/internal/manifest"
)

// Provenance is lockfile metadata recording that a dependency was
// discovered through a marketplace (mkt-031's four fields; wiring this into
// the lockfile writer itself is a later step). SourceURL/SourceDigest come
// straight from the fetched manifest's own provenance fields, which are
// only ever populated for kind=url marketplaces (client_url.go) -- every
// other transport leaves them "".
type Provenance struct {
	DiscoveredVia         string
	MarketplacePluginName string
	SourceURL             string
	SourceDigest          string
}

// Resolution is ResolvePlugin's result: a marketplace plugin reference
// collapsed to an ordinary dependency coordinate (mkt-029 -- there is no
// separate marketplace primitive type past this point).
type Resolution struct {
	// Canonical is "owner/repo[/path][#ref]", or an absolute local
	// filesystem path for the mkt-025 local-marketplace fast path.
	Canonical string
	// DepRef is non-nil only for mkt-027's structured-git-reference case (a
	// non-GitHub-family host's in-marketplace subdirectory plugin);
	// callers must prefer it over parsing Canonical when it is set.
	DepRef *manifest.DependencyReference
	// Provenance is always non-nil.
	Provenance *Provenance
	// Warnings holds mkt-034's advisory security messages (a ref-swap-pin
	// change, or the same plugin name shadowing another registered
	// marketplace) -- always attempted, never blocking: a nil/empty slice
	// means no advisory fired, not that the checks were skipped. Callers
	// are expected to surface each entry (e.g. print to stderr) but must
	// never treat a non-empty Warnings as a resolution failure.
	Warnings []string
}

// ErrMarketplaceNotFound is returned by ResolvePlugin when mktName does not
// name a registered marketplace (mkt-022). Wrapped with %w so callers can
// route it to a specific CLI hint via errors.Is.
var ErrMarketplaceNotFound = errors.New("marketplace is not registered")

// ErrPluginNotFound is returned by ResolvePlugin when pluginName does not
// exist in the marketplace's manifest (mkt-024). Wrapped with %w so callers
// can route it to a specific CLI hint via errors.Is.
var ErrPluginNotFound = errors.New("plugin not found in marketplace")

// ResolvePlugin resolves a PLUGIN@MARKETPLACE reference to an ordinary
// dependency coordinate, mirroring the Python original's
// resolve_marketplace_plugin (resolver.py:734-1019) through its mkt-022,
// mkt-024, mkt-025, mkt-026, mkt-027, mkt-028, mkt-034, mkt-035, and (via
// opts.VersionSpec) mkt-021/033 version_spec resolution steps.
func ResolvePlugin(ctx context.Context, pluginName, mktName string, opts ResolveOptions) (*Resolution, error) {
	// mkt-022: only the global registry is consulted, NEVER the current
	// project's own apm.yml "marketplace:" block -- FindByName's only data
	// source is ~/.apm/marketplaces.json (registry.go), so there is no
	// project-manifest read path here to accidentally wire up.
	src, err := FindByName(mktName)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, fmt.Errorf(
			"%w: %q; run `apm marketplace add OWNER/REPO` to register it, or `apm marketplace list` to see registered marketplaces",
			ErrMarketplaceNotFound, mktName,
		)
	}

	manifestDoc, err := Fetch(ctx, src)
	if err != nil {
		return nil, err
	}

	// mkt-024: plugin name lookup is case-insensitive.
	plugin := findPluginCaseInsensitive(manifestDoc.Plugins, pluginName)
	if plugin == nil {
		return nil, fmt.Errorf(
			"%w: %q in marketplace %q; run `apm marketplace browse %s` to see available plugins",
			ErrPluginNotFound, pluginName, mktName, mktName,
		)
	}

	provenance := &Provenance{
		DiscoveredVia:         mktName,
		MarketplacePluginName: plugin.Name,
		SourceURL:             manifestDoc.SourceURL,
		SourceDigest:          manifestDoc.SourceDigest,
	}

	// mkt-025: local-marketplace fast path -- a relative-path plugin source
	// resolves straight to an absolute local filesystem path, never
	// round-tripping through a dependency string.
	if src.Kind() == KindLocal {
		if s, ok := plugin.Source.(string); ok {
			canonical, err := resolveLocalRelativeSource(s, src)
			if err != nil {
				return nil, err
			}
			return &Resolution{Canonical: canonical, Provenance: provenance}, nil
		}
	}

	// mkt-026: plugin.Source shape -> canonical dependency string.
	canonical, err := resolvePluginSource(plugin, src.Owner, src.Repo)
	if err != nil {
		return nil, err
	}

	// mkt-027: non-GitHub-family host + in-marketplace subdirectory plugin
	// -> structured {git, path, ref} DepRef, avoiding the nested-group path
	// ambiguity a bare "owner/repo/subdir" canonical would carry on hosts
	// like self-managed GitLab.
	var depRef *manifest.DependencyReference
	if sourceNeedsExplicitGitPath(src) && isInMarketplaceSource(plugin, src) {
		inRepoPath, pathRef, err := extractInRepoPathAndRef(plugin, manifestDoc.PluginRoot)
		if err != nil {
			return nil, err
		}
		if inRepoPath != "" {
			effectiveRef := pathRef
			if effectiveRef == "" && isPropagatableRef(src.Ref) {
				effectiveRef = src.Ref
			}
			depRef, err = gitLabInMarketplaceDependencyReference(src, inRepoPath, effectiveRef)
			if err != nil {
				return nil, err
			}
			canonical = depRef.ToCanonical(defaultCanonicalHost())
		}
	}

	// mkt-028: cross-repo dependency-confusion fail-closed gate. Computed
	// (and, if triggered, returned) right here -- after mkt-027's
	// structured-DepRef decision, but BEFORE mkt-035's ref propagation and
	// mkt-021/033's version_spec resolution below -- so a detected risk
	// short-circuits this function immediately, guaranteeing zero further
	// network activity (in particular, version_spec's semver-range branch,
	// which would otherwise call opts.tagLister().ListTags against this
	// exact, possibly attacker-controlled, canonical). See crossrepo.go's
	// package doc comment for why this precedes version_spec despite
	// design.md's own step numbering listing version_spec first.
	if risk := detectCrossRepoMisconfigRisk(plugin, src, depRef); risk != nil {
		return nil, fmt.Errorf("%w: plugin %q in marketplace %q -- %s", ErrCrossRepoMisconfig, plugin.Name, mktName, risk)
	}

	// mkt-035: registered-marketplace-ref propagation. Only applies to the
	// plain relative-string-source shape -- a dict source's own "ref"/
	// "path" fields are handled by resolvePluginSource/the mkt-027 block
	// above instead, never by this string-only check. The "canonical
	// doesn't already carry a #ref" guard is defense in depth (mirroring
	// the Python original exactly): resolveRelativeSource never itself
	// emits a "#ref" for a plain relative source, so it is always true in
	// practice today, but nothing prevents that from changing. Skipped
	// entirely when a version_spec is present (opts.VersionSpec != ""):
	// mirrors the Python original's `and not version_spec` guard -- the
	// version_spec override below decides the ref instead.
	if depRef == nil {
		if _, ok := plugin.Source.(string); ok {
			if opts.VersionSpec == "" && !strings.Contains(canonical, "#") && isPropagatableRef(src.Ref) {
				canonical = canonical + "#" + src.Ref
			}
		}
	}

	// mkt-021/033: version_spec override (the CLI's "#REF" suffix, or an
	// apm.yml dict form's "version:" field). Skipped entirely when DepRef is
	// already set -- a structured DepRef already carries mkt-027's own
	// path/ref decision -- mirroring the Python original's `if version_spec
	// and dep_ref is None`.
	if opts.VersionSpec != "" && depRef == nil {
		var err error
		canonical, err = applyVersionSpec(canonical, opts.VersionSpec, opts.tagLister())
		if err != nil {
			return nil, err
		}
	}

	// mkt-034: two advisory security checks, run against the FINAL canonical
	// (after mkt-035's ref propagation and mkt-021/033's version_spec
	// override have both already had their say) -- neither ever turns into
	// an error; both only ever append human-readable warnings for the
	// caller to surface.
	var warnings []string

	// mkt-034a: ref-swap-pin check + record, scoped to (mktName, plugin.Name,
	// plugin.Version) exactly like the Python original. Only meaningful when
	// canonical actually carries a "#ref" fragment -- mirrors resolver.py's
	// `if current_ref:` guard (:973), which treats a "#"-less canonical (and
	// an empty ref right after a bare "#") as "nothing to pin".
	if _, ref, hasRef := strings.Cut(canonical, "#"); hasRef && ref != "" {
		if warning := checkAndRecordRefPin(mktName, plugin.Name, plugin.Version, ref); warning != "" {
			warnings = append(warnings, warning)
		}
	}

	// mkt-034b: shadow-plugin-name detection across every OTHER registered
	// marketplace. Unconditional (unlike mkt-034a, not gated on canonical
	// carrying a ref) and fail-open internally -- see shadow.go's doc
	// comment for why a probe failure against one candidate marketplace
	// must never surface here, let alone abort resolution.
	warnings = append(warnings, detectShadowWarnings(ctx, plugin.Name, mktName)...)

	return &Resolution{Canonical: canonical, DepRef: depRef, Provenance: provenance, Warnings: warnings}, nil
}

// isPropagatableRef reports whether ref is a marketplace-registered ref
// worth propagating onto a plugin canonical (mkt-027/035): non-empty and
// not "main"/"HEAD", which represent "the default branch" and would be a
// no-op at best, misleading at worst, if the repo's actual default branch
// has a different name.
func isPropagatableRef(ref string) bool {
	return ref != "" && ref != "main" && ref != "HEAD"
}

// findPluginCaseInsensitive looks up a plugin by name, case-insensitively
// (mkt-024).
func findPluginCaseInsensitive(plugins []MarketplacePlugin, name string) *MarketplacePlugin {
	lower := strings.ToLower(name)
	for i := range plugins {
		if strings.ToLower(plugins[i].Name) == lower {
			return &plugins[i]
		}
	}
	return nil
}

// defaultCanonicalHost mirrors the Python original's default_host()
// (utils/github_host.py:8-10): GITHUB_HOST when set, else "github.com".
func defaultCanonicalHost() string {
	if h := os.Getenv(githubHostEnvVar); h != "" {
		return h
	}
	return "github.com"
}

// sourceNeedsExplicitGitPath reports whether an in-marketplace subdirectory
// plugin on this marketplace source needs a structured {git,path,ref}
// DependencyReference (mkt-027) instead of a bare "owner/repo/subdir"
// canonical -- true for any host where a subdirectory path segment is
// ambiguous with a nested-group/namespace path (self-managed GitLab,
// generic git, Azure DevOps Server), mirroring the Python original's
// _source_needs_explicit_git_path (resolver.py:243-261).
func sourceNeedsExplicitGitPath(src *MarketplaceSource) bool {
	switch src.Kind() {
	case KindGitHub:
		return false
	case KindGitLab, KindGit:
		return true
	default:
		// Kind()==KindLocal never reaches here (ResolvePlugin's fast path
		// returns first); Kind()==KindURL falls back to the legacy
		// host-based check, matching the Python original.
		return marketplaceHostNeedsExplicitGitPath(src.Host)
	}
}

// marketplaceHostNeedsExplicitGitPath is the legacy host-based fallback
// sourceNeedsExplicitGitPath consults for source kinds its own switch
// doesn't directly decide (kind==KindURL today) -- mirrors
// _marketplace_host_needs_explicit_git_path (resolver.py:226-240).
func marketplaceHostNeedsExplicitGitPath(host string) bool {
	h := strings.TrimSpace(host)
	if h == "" {
		return false
	}
	h, _, _ = strings.Cut(h, "/")
	if isAzureDevOpsHostname(h) {
		return false
	}
	return !isGitHubHostname(h)
}

// isAzureDevOpsHostname reports whether host is Azure DevOps (cloud or
// legacy visualstudio.com), mirroring the Python original's
// is_azure_devops_hostname (utils/github_host.py:13-28).
func isAzureDevOpsHostname(host string) bool {
	h := strings.ToLower(host)
	if h == "dev.azure.com" || h == "ssh.dev.azure.com" {
		return true
	}
	return strings.HasSuffix(h, ".visualstudio.com")
}

// isInMarketplaceSource reports whether plugin's source names the SAME
// repository the marketplace itself is hosted in -- addressed either as a
// plain relative-path string, or as a dict source whose repo field
// resolves to the marketplace's own owner/repo -- mirrors
// _is_in_marketplace_source (resolver.py:209-223). A dict source naming a
// genuinely different (cross-repo) project is NOT in-marketplace, even
// when its coerced type is one of github/git-subdir/gitlab.
func isInMarketplaceSource(plugin *MarketplacePlugin, src *MarketplaceSource) bool {
	switch s := plugin.Source.(type) {
	case string:
		return true
	case map[string]any:
		switch coercePluginType(s) {
		case "github", "git-subdir", "gitlab":
			return repoFieldMatchesMarketplace(stringField(s, "repo"), src.Owner, src.Repo, src.Host)
		default:
			return false
		}
	default:
		return false
	}
}

// repoFieldMatchesMarketplace reports whether a dict source's "repo" field
// identifies the same project as the marketplace source -- mirrors
// _repo_field_matches_marketplace (resolver.py:173-182).
func repoFieldMatchesMarketplace(repoField, owner, repo, host string) bool {
	if repoField == "" || !strings.Contains(repoField, "/") {
		return false
	}
	normalized := normalizeRepoFieldForMatch(repoField, host)
	if normalized == "" {
		return false
	}
	return normalized == marketplaceProjectSlug(owner, repo)
}

// normalizeRepoFieldForMatch normalizes a dict source's "repo" field to a
// logical project path for matching against the marketplace's own
// owner/repo -- mirrors _normalize_repo_field_for_match (resolver.py:140-
// 170). A repo field that explicitly names a different host than the
// marketplace's own returns "" so it cannot match by path suffix alone.
func normalizeRepoFieldForMatch(repoField, marketplaceHost string) string {
	raw := strings.TrimRight(strings.TrimSpace(repoField), "/")
	raw = strings.TrimSuffix(raw, ".git")
	hostL := strings.ToLower(strings.TrimSpace(marketplaceHost))
	lower := strings.ToLower(raw)

	switch {
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "ssh://"):
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		parsedHost := strings.ToLower(strings.TrimSpace(u.Hostname()))
		if parsedHost != "" && parsedHost != hostL {
			return ""
		}
		return strings.ToLower(strings.TrimPrefix(u.Path, "/"))
	case strings.HasPrefix(raw, "git@") && strings.Contains(raw, ":"):
		rest := strings.TrimPrefix(raw, "git@")
		hostPart, pathPart, _ := strings.Cut(rest, ":")
		if strings.ToLower(strings.TrimSpace(hostPart)) != hostL {
			return ""
		}
		return strings.ToLower(strings.TrimPrefix(pathPart, "/"))
	default:
		parts := nonEmptySegments(raw)
		if len(parts) >= 3 && strings.ToLower(strings.TrimSpace(parts[0])) == hostL {
			parts = parts[1:]
		}
		return strings.ToLower(strings.Join(parts, "/"))
	}
}

// marketplaceProjectSlug / normalizeOwnerRepoSlug mirror
// _marketplace_project_slug / _normalize_owner_repo_slug (resolver.py:128-
// 137): a lowercase "owner/repo" slug with any trailing ".git" stripped.
func marketplaceProjectSlug(owner, repo string) string {
	return normalizeOwnerRepoSlug(owner + "/" + repo)
}

func normalizeOwnerRepoSlug(repo string) string {
	r := strings.ToLower(strings.TrimRight(strings.TrimSpace(repo), "/"))
	return strings.TrimSuffix(r, ".git")
}

// extractInRepoPathAndRef returns the subdirectory path (relative to the
// marketplace repo root) and any dict-source-declared ref for building
// mkt-027's structured DepRef -- mirrors _extract_in_repo_path_and_ref
// (resolver.py:406-460). path=="" means the plugin IS the marketplace
// repository root (no subdirectory package, equivalent to Python's None);
// ref is only meaningful when path is non-empty.
func extractInRepoPathAndRef(plugin *MarketplacePlugin, pluginRoot string) (path, ref string, err error) {
	switch src := plugin.Source.(type) {
	case string:
		rel := strings.Trim(src, "/")
		rel = strings.TrimPrefix(rel, "./")
		rel = strings.Trim(rel, "/")
		if pluginRoot != "" && rel != "" && rel != "." && !strings.Contains(rel, "/") {
			root := strings.Trim(pluginRoot, "/")
			root = strings.TrimPrefix(root, "./")
			root = strings.Trim(root, "/")
			if root != "" {
				rel = root + "/" + rel
			}
		}
		if rel == "" || rel == "." {
			return "", "", nil
		}
		if err := validateRelativeSourcePath(rel, "relative source path"); err != nil {
			return "", "", err
		}
		return rel, "", nil
	case map[string]any:
		ref = strings.TrimSpace(stringField(src, "ref"))
		switch coercePluginType(src) {
		case "github":
			p := strings.Trim(stringField(src, "path"), "/")
			if p == "" {
				return "", ref, nil
			}
			if err := validateRelativeSourcePath(p, "github source path"); err != nil {
				return "", "", err
			}
			return p, ref, nil
		case "git-subdir", "gitlab":
			sub := stringField(src, "subdir")
			if sub == "" {
				sub = stringField(src, "path")
			}
			sub = strings.Trim(sub, "/")
			if sub == "" {
				return "", ref, nil
			}
			if err := validateRelativeSourcePath(sub, "git-subdir source path"); err != nil {
				return "", "", err
			}
			return sub, ref, nil
		default:
			return "", "", nil
		}
	default:
		return "", "", nil
	}
}

// marketplaceHTTPSGitURL returns the HTTPS clone URL for the registered
// marketplace project -- mirrors _marketplace_https_git_url
// (resolver.py:385-403). Prefers src.URL verbatim (an https/http/git/ssh
// scheme, or an SCP-style SSH remote, passed through as-is other than a
// ".git" suffix); falls back to synthesising "https://{host}/{owner}/
// {repo}.git" from the legacy mirror fields.
func marketplaceHTTPSGitURL(src *MarketplaceSource) string {
	u := strings.TrimSpace(src.URL)
	lower := strings.ToLower(u)
	if u != "" && (strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "git://") || strings.HasPrefix(lower, "ssh://")) {
		if strings.HasSuffix(u, ".git") {
			return u
		}
		return u + ".git"
	}
	if u != "" && strings.Contains(u, "@") && strings.Contains(u, ":") && !strings.HasPrefix(lower, "file://") {
		return u
	}
	segments := nonEmptySegments(src.Owner + "/" + src.Repo)
	encoded := make([]string, len(segments))
	for i, seg := range segments {
		encoded[i] = url.PathEscape(seg)
	}
	return fmt.Sprintf("https://%s/%s.git", src.Host, strings.Join(encoded, "/"))
}

// gitLabInMarketplaceDependencyReference builds mkt-027's structured
// DependencyReference (equivalent to apm.yml's object-form `git:` + `path:`
// + `ref:`), mirroring _gitlab_in_marketplace_dependency_reference
// (resolver.py:463-472).
func gitLabInMarketplaceDependencyReference(src *MarketplaceSource, inRepoPath, ref string) (*manifest.DependencyReference, error) {
	gitURL := marketplaceHTTPSGitURL(src)
	d, err := manifest.ParseDepString(gitURL)
	if err != nil {
		return nil, fmt.Errorf("marketplace %q: cannot build structured dependency reference for %q: %w", src.Name, gitURL, err)
	}
	if d.IsLocal {
		// Defensive parity with depref.go's "git:" dict branch: a
		// structured git reference always forces source=git even if the
		// URL happened to look like a local path.
		d.IsLocal = false
		d.RepoURL = d.LocalPath
		d.LocalPath = ""
	}
	d.Source = "git"
	d.VirtualPath = inRepoPath
	d.VirtualType = virtualPathKind(inRepoPath)
	if ref != "" {
		d.Reference = ref
	}
	return d, nil
}

// virtualPathKind classifies a virtual path as "file" or "subdirectory",
// mirroring internal/manifest/depref.go's unexported classifyVirtualPath
// (not exported across the package boundary, so this duplicates its small
// constant extension list rather than reaching into manifest internals).
func virtualPathKind(vp string) string {
	for _, ext := range []string{".prompt.md", ".instructions.md", ".agent.md", ".chatmode.md"} {
		if strings.HasSuffix(vp, ext) {
			return "file"
		}
	}
	return "subdirectory"
}
