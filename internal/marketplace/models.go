// Package marketplace implements apm's marketplace registry: the data
// model for a registered marketplace source and its manifest content
// (marketplace.json), plus (in later files of this package) the
// ~/.apm/marketplaces.json registry CRUD and the fetch clients that pull
// marketplace.json over the supported transports (local, direct URL,
// GitHub, GitLab, generic git).
package marketplace

import (
	"encoding/json"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// SourceKind classifies how a MarketplaceSource's manifest content is
// fetched.
type SourceKind string

const (
	KindLocal  SourceKind = "local"
	KindURL    SourceKind = "url"
	KindGitHub SourceKind = "github"
	KindGitLab SourceKind = "gitlab"
	KindGit    SourceKind = "git"
)

// MarketplaceSource is a registered marketplace repository, as stored in
// ~/.apm/marketplaces.json. URL is the canonical location (a local
// filesystem path or a remote URL/SCP-style SSH remote); Owner/Repo/Host/
// Branch are convenience mirror fields kept for parity with the Python
// original's marketplaces.json shape (mkt-001), and Branch is a legacy
// alias for Ref.
type MarketplaceSource struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
	// Ref defaults to "main" when unset (filled in by the SOURCE parser,
	// not by this struct).
	Ref string `json:"ref,omitempty"`
	// Path defaults to "marketplace.json" when unset; an explicit empty
	// string means URL itself names the manifest file directly (see Kind).
	Path   string `json:"path,omitempty"`
	Owner  string `json:"owner,omitempty"`
	Repo   string `json:"repo,omitempty"`
	Host   string `json:"host,omitempty"`
	Branch string `json:"branch,omitempty"`
}

// scpLikeSourceRe matches an SCP-style SSH remote, e.g.
// "git@host:owner/repo.git" -- the same shape url.Parse rejects outright
// ("first path segment in URL cannot contain colon"), so it must be
// recognized before any url.Parse call.
var scpLikeSourceRe = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.+-]*@([^:/]+):.+$`)

// Kind derives this source's fetch strategy from its URL (and Path, for the
// direct-manifest-URL case). Classification order mirrors the Python
// original's MarketplaceSource.kind property: local path first (checked
// before any URL parsing, since a Windows drive letter like "C:\..." would
// otherwise be misparsed by url.Parse as scheme "c"), then a direct hosted
// marketplace.json URL, then host-based github/gitlab/git.
func (s *MarketplaceSource) Kind() SourceKind {
	if s.URL == "" || looksLikeLocalPath(s.URL) {
		return KindLocal
	}
	if s.Path == "" && urlNamesRemoteManifest(s.URL) {
		return KindURL
	}
	host := extractSourceHost(s.URL)
	if host == "" {
		return KindGit
	}
	return classifySourceHost(host)
}

// looksLikeLocalPath reports whether value is shaped like a local
// filesystem path or a file:// URI: absolute ("/..."), relative ("./...",
// "../...", ".\..." or "..\..."), home-relative ("~..." including "~\..."),
// or a Windows drive letter ("C:\..." or "C:/..."). The backslash-relative
// forms only matter for raw SOURCE strings (source.go's
// ParseMarketplaceSource, mkt-010) -- once a local source is canonicalized
// to an absolute path for storage, it always presents in one of the
// already-covered forms.
func looksLikeLocalPath(value string) bool {
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "file://") {
		return true
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "./") ||
		strings.HasPrefix(value, "../") || strings.HasPrefix(value, "~") ||
		strings.HasPrefix(value, `.\`) || strings.HasPrefix(value, `..\`) {
		return true
	}
	if len(value) >= 3 {
		c := value[0]
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		sep := value[1:3]
		if isAlpha && (sep == `:\` || sep == ":/") {
			return true
		}
	}
	return false
}

// urlNamesRemoteManifest reports whether raw is a direct hosted
// marketplace.json document: HTTPS scheme, a host, and a path (ignoring
// any trailing slashes) ending in "/marketplace.json". Any other JSON
// filename does not count -- this is the sole source of truth for the
// "hosted marketplace.json URL" decision (design.md rule 4).
func urlNamesRemoteManifest(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" {
		return false
	}
	p := strings.TrimRight(u.Path, "/")
	return strings.HasSuffix(p, "/marketplace.json")
}

// extractSourceHost best-effort extracts a hostname from either a regular
// URL or an SCP-style SSH remote (git@host:owner/repo); returns "" for
// anything unparseable. Callers must have already excluded local paths
// (looksLikeLocalPath), since a Windows drive letter would otherwise be
// misparsed by url.Parse as scheme "c".
func extractSourceHost(raw string) string {
	if raw == "" {
		return ""
	}
	if m := scpLikeSourceRe.FindStringSubmatch(raw); m != nil {
		return m[1]
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// githubHostEnvVar names the environment variable that designates a
// self-hosted GitHub Enterprise Server (GHES) host, mirroring the Python
// original's GITHUB_HOST env var (utils/github_host.py:170-198). A single
// host value only -- unlike GitLab's env-list sibling, this scope only
// needs the single-host GHES case.
const githubHostEnvVar = "GITHUB_HOST"

// isGitHubEnterpriseServerHost reports whether host is the self-hosted GHES
// host configured via GITHUB_HOST, mirroring is_github_hostname's GHES
// branch (utils/github_host.py:194-202): the env var must be set, match
// host case-insensitively, and not itself be "github.com"/"gitlab.com" (a
// misconfigured GITHUB_HOST must never reclassify those well-known hosts).
func isGitHubEnterpriseServerHost(host string) bool {
	ghesHost := strings.ToLower(os.Getenv(githubHostEnvVar))
	if ghesHost == "" {
		return false
	}
	if ghesHost == "github.com" || ghesHost == "gitlab.com" {
		return false
	}
	return strings.ToLower(host) == ghesHost
}

// isGitHubHostname reports whether host should be treated as GitHub (cloud
// or enterprise): github.com, any "*.ghe.com" host (GitHub Enterprise Cloud
// with data residency), or a host matching GITHUB_HOST (a self-hosted
// GitHub Enterprise Server) -- mirrors the Python original's
// is_github_hostname (utils/github_host.py:170-202). Shared by
// classifySourceHost (SourceKind derivation) and by the install-ref
// resolver's non-GitHub-family routing checks (mkt-027/028), so this
// security-relevant host classification has a single source of truth
// instead of drifting across call sites.
func isGitHubHostname(host string) bool {
	h := strings.ToLower(host)
	if h == "github.com" || strings.HasSuffix(h, ".ghe.com") {
		return true
	}
	return isGitHubEnterpriseServerHost(host)
}

// classifySourceHost maps a hostname to KindGitHub/KindGitLab/KindGit.
// GitHub-family hosts (isGitHubHostname) resolve to KindGitHub rather than
// falling through to the generic KindGit clone path, so they get GitHub's
// Contents API + PAT auth (client_github.go).
func classifySourceHost(host string) SourceKind {
	if isGitHubHostname(host) {
		return KindGitHub
	}
	if isGitLabFamilyHost(strings.ToLower(host)) {
		return KindGitLab
	}
	return KindGit
}

// gitlabHostEnvVar / gitlabHostsEnvVar name the environment variables that
// designate self-managed GitLab hosts, mirroring the Python original's
// is_gitlab_hostname (utils/github_host.py:44-85): a single host and a
// comma-separated allowlist respectively.
const (
	gitlabHostEnvVar  = "GITLAB_HOST"
	gitlabHostsEnvVar = "APM_GITLAB_HOSTS"
)

// isGitLabFamilyHost reports whether host is GitLab SaaS or an explicitly
// allowlisted self-managed GitLab host. This is an EXACT-match allowlist,
// NOT a substring test: a substring check ("gitlab" in host) would classify
// attacker-controlled hosts such as "gitlab.evil.com" or "notgitlab.io" as
// GitLab and forward GITLAB_APM_PAT to them (credential exfiltration).
// Mirrors Python's is_gitlab_hostname: gitlab.com, or a valid-FQDN host that
// exactly matches GITLAB_HOST or an entry in APM_GITLAB_HOSTS. host is
// assumed already lowercased by the caller.
func isGitLabFamilyHost(host string) bool {
	if host == "" {
		return false
	}
	if host == "gitlab.com" {
		return true
	}
	single := strings.ToLower(strings.TrimSpace(os.Getenv(gitlabHostEnvVar)))
	single, _, _ = strings.Cut(single, "/")
	if single != "" && single == host && looksLikeFQDN(host) {
		return true
	}
	for _, part := range strings.Split(os.Getenv(gitlabHostsEnvVar), ",") {
		entry := strings.ToLower(strings.TrimSpace(part))
		entry, _, _ = strings.Cut(entry, "/")
		if entry != "" && entry == host && looksLikeFQDN(entry) {
			return true
		}
	}
	return false
}

// MarketplacePlugin is a single plugin entry inside a marketplace
// manifest. Source is either a relative path string or a structured map
// (e.g. {"type": "github", "repo": "owner/repo"}); routing on its shape
// happens outside this package.
type MarketplacePlugin struct {
	Name        string   `json:"name"`
	Source      any      `json:"source,omitempty"`
	Description string   `json:"description,omitempty"`
	Version     string   `json:"version,omitempty"`
	Tags        []string `json:"tags,omitempty"`

	// SourceMarketplace is populated during resolution (the name of the
	// marketplace this plugin was found in), never read from the manifest
	// JSON itself.
	SourceMarketplace string `json:"-"`

	// Registry is parsed from the manifest's "registry" key for parity
	// with the Python original's field, but nothing in apm-go dispatches
	// on it: the dedicated-registry routing it names was shipped as a
	// parsing-only layer upstream too (mkt-005 revised) -- only tolerant
	// parsing (no error, value otherwise unused) is required here.
	Registry string `json:"registry,omitempty"`
}

// MarketplaceManifest is the parsed content of a marketplace.json
// document.
type MarketplaceManifest struct {
	Name    string              `json:"name"`
	Owner   string              `json:"owner,omitempty"`
	Plugins []MarketplacePlugin `json:"plugins,omitempty"`

	// PluginRoot is metadata.pluginRoot from the manifest: the base path
	// bare-name relative plugin sources resolve under (consumed by the
	// install-ref subtask's resolver; parsed here for manifest parity).
	PluginRoot string `json:"-"`

	// SourceURL and SourceDigest are provenance metadata populated by the
	// fetch layer (client.go, added in a later step), not read from the
	// manifest JSON.
	SourceURL    string `json:"-"`
	SourceDigest string `json:"-"`
}

// UnmarshalJSON parses a marketplace.json document, normalizing the
// real-world shapes the Python original's parse_marketplace_json accepts
// (models.py:454-515) that a naive field-for-field decode would reject or
// miss (caught by A/B testing against the live Python CLI, 2026-07-03):
//
//   - "owner" may be a plain string or an object; the object form's "name"
//     key is the owner name (Claude Code manifests use the object form).
//   - "plugins" that is not a JSON array (an object/string/number) is
//     tolerated as an empty plugin list rather than a hard error, mirroring
//     Python's warn-and-treat-as-empty fallback (:491-497); a non-object
//     element inside a valid "plugins" array is skipped, not fatal (:501-502).
//   - Plugins may use the Copilot CLI shape ("repository": "owner/repo"
//     [+ "ref"]) instead of "source"; a github-typed source map is
//     synthesized. Entries with neither, or without a name, are dropped. A
//     "source" present but neither a string nor an object (e.g. a number or
//     array) drops the entry, mirroring the "unrecognized source format"
//     branch (:387-389).
//   - A plugin whose source map declares type/source == "npm" is dropped at
//     parse time (mkt-026 dual-layer: the "kind: npm" variant is NOT
//     dropped here -- it is rejected later at source-resolution time).
//   - A non-array "tags" value is coerced to empty rather than rejected
//     (:367); a non-string "version" is ignored rather than rejected.
//   - metadata.pluginRoot is captured into PluginRoot; a non-object
//     "metadata" or a non-string "pluginRoot" is tolerated as "" rather
//     than rejected.
func (m *MarketplaceManifest) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name     string          `json:"name"`
		Owner    json.RawMessage `json:"owner"`
		Plugins  json.RawMessage `json:"plugins"`
		Metadata json.RawMessage `json:"metadata"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Name = raw.Name
	m.PluginRoot = parseManifestPluginRoot(raw.Metadata)
	m.Owner = parseManifestOwner(raw.Owner)
	m.Plugins = parseManifestPlugins(raw.Plugins)
	return nil
}

// parseManifestPluginRoot extracts metadata.pluginRoot tolerantly: a
// "metadata" that is not a JSON object, or a "pluginRoot" that is not a
// JSON string, both downgrade to "" rather than failing the parse --
// mirrors Python's isinstance(metadata, dict) / isinstance(raw_root, str)
// guards (models.py:486-489).
func parseManifestPluginRoot(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var obj struct {
		PluginRoot json.RawMessage `json:"pluginRoot"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	return strings.TrimSpace(rawStringField(obj.PluginRoot))
}

// parseManifestPlugins extracts the "plugins" array tolerantly: a value
// that is not a JSON array (object/string/number) is treated as an empty
// list rather than a hard error, mirroring Python's warn-and-treat-as-empty
// fallback (models.py:491-497). Each array element that is not a JSON
// object -- or whose fields don't decode into rawPlugin at all -- is
// skipped rather than failing the whole document (:501-502).
func parseManifestPlugins(raw json.RawMessage) []MarketplacePlugin {
	if len(raw) == 0 {
		return nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil
	}
	var plugins []MarketplacePlugin
	for _, entry := range entries {
		var rp rawPlugin
		if err := json.Unmarshal(entry, &rp); err != nil {
			continue
		}
		if p, ok := rp.normalize(); ok {
			plugins = append(plugins, p)
		}
	}
	return plugins
}

// rawPlugin is a plugin entry as found on disk, before Copilot-shape
// synthesis and npm-source dropping. Version and Tags are decoded as raw
// JSON (rather than string / []string directly) so a wrong-shaped value
// (e.g. a numeric "version" or a string "tags") downgrades to "ignored"
// instead of failing this entry's decode -- mirrors Python's tolerant
// entry.get(...)/isinstance(...) field access (models.py:365-367).
type rawPlugin struct {
	Name        string          `json:"name"`
	Source      any             `json:"source"`
	Repository  string          `json:"repository"`
	Ref         string          `json:"ref"`
	Description string          `json:"description"`
	Version     json.RawMessage `json:"version"`
	Tags        json.RawMessage `json:"tags"`
	Registry    string          `json:"registry"`
}

// rawStringField extracts a JSON string field tolerantly: any other JSON
// type (number, object, array, bool, or an absent field) downgrades to ""
// rather than an error.
func rawStringField(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// rawStringSliceField extracts a JSON string-array field tolerantly: any
// other JSON type (or an absent field) is coerced to an empty slice --
// mirrors Python's `tuple(raw_tags) if isinstance(raw_tags, list) else ()`
// (models.py:367).
func rawStringSliceField(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var s []string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil
	}
	return s
}

// normalize applies the Python parser's per-entry rules; ok=false means the
// entry is silently dropped (matching parse_marketplace_json's debug-log
// skips: nameless entries, sourceless entries, npm-typed sources, and
// sources whose shape is neither a string nor an object).
func (rp rawPlugin) normalize() (MarketplacePlugin, bool) {
	name := strings.TrimSpace(rp.Name)
	if name == "" {
		return MarketplacePlugin{}, false
	}
	source := rp.Source
	if source == nil {
		// Copilot CLI shape: "repository": "owner/repo" (+ optional ref).
		if strings.Contains(rp.Repository, "/") {
			synth := map[string]any{"type": "github", "repo": rp.Repository}
			if rp.Ref != "" {
				synth["ref"] = rp.Ref
			}
			source = synth
		} else {
			return MarketplacePlugin{}, false
		}
	}
	switch srcVal := source.(type) {
	case string:
		// Relative path source -- kept as-is.
	case map[string]any:
		// The parse-layer npm drop reads only "type"/"source" (NOT "kind"),
		// mirroring Python _parse_plugin_entry's discriminator keys.
		t, _ := srcVal["type"].(string)
		if t == "" {
			t, _ = srcVal["source"].(string)
		}
		if strings.EqualFold(strings.TrimSpace(t), "npm") {
			return MarketplacePlugin{}, false
		}
	default:
		// Neither a string nor an object (e.g. a number, array, or bool):
		// Python drops these entries outright ("unrecognized source
		// format", models.py:387-389).
		return MarketplacePlugin{}, false
	}
	return MarketplacePlugin{
		Name:        name,
		Source:      source,
		Description: rp.Description,
		Version:     rawStringField(rp.Version),
		Tags:        rawStringSliceField(rp.Tags),
		Registry:    rp.Registry,
	}, true
}

// parseManifestOwner accepts the manifest "owner" field as either a plain
// string or an object whose "name" key is the owner name.
func parseManifestOwner(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.Name
	}
	return ""
}
