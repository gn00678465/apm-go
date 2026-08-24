// Package marketplace implements apm's marketplace registry: the data
// model for a registered marketplace source and its manifest content
// (marketplace.json), plus (in later files of this package) the
// ~/.apm/marketplaces.json registry CRUD and the fetch clients that pull
// marketplace.json over the supported transports (local, direct URL,
// GitHub, GitLab, generic git).
package marketplace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/apm-go/apm/internal/marketplace/tagpattern"
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

	// TagPattern is the producer's tag convention, read from the source
	// object's "tag_pattern" key (not a top-level plugin key, hence json:"-").
	// "" means the key was absent -- an older marketplace.json -- and the
	// resolver supplies its own default, mirroring upstream's
	// `tag_pattern: str | None = None` (models.py:325-330).
	TagPattern string `json:"-"`
}

// MarketplaceManifest is the parsed content of a marketplace.json
// document.
type MarketplaceManifest struct {
	Name        string              `json:"name"`
	Owner       string              `json:"owner,omitempty"`
	Description string              `json:"description,omitempty"`
	Plugins     []MarketplacePlugin `json:"plugins,omitempty"`

	// PluginRoot is metadata.pluginRoot from the manifest: the base path
	// bare-name relative plugin sources resolve under (consumed by the
	// install-ref subtask's resolver; parsed here for manifest parity).
	PluginRoot string `json:"-"`

	// SourceURL and SourceDigest are provenance metadata populated by the
	// fetch layer (client.go, added in a later step), not read from the
	// manifest JSON.
	SourceURL    string `json:"-"`
	SourceDigest string `json:"-"`

	// StructuralErrors are raw manifest-shape diagnostics retained by the
	// tolerant parser above (ticket 11), mirroring Python's
	// MarketplaceManifest.structural_errors (marketplace/models.py:396):
	// this is what `apm marketplace validate`'s "Structure" check
	// (validator.go) reports. Populated by parseManifestPlugins/
	// parsePluginEntry for the Oracle's full per-element diagnostic set
	// (ticket 11 attempt 2, after attempt 1 shipped only the two top-level
	// "plugins: expected a list"/"plugins[N]: expected an object" cases and
	// silently dropped everything else): a missing/blank name, a "source"
	// that is neither a string nor an object, an unsupported or
	// underspecified dict source (npm, an unrecognized type, or a
	// known type missing/invalid its required repo/url field -- see
	// dictSourceDiagnostic), and a "repository" fallback that isn't an
	// "owner/repo"-shaped string. isValidRemoteCoordinate's own doc comment
	// records the one deliberately bounded piece: it does not replicate
	// DependencyReference.parse's full acceptance grammar.
	StructuralErrors []string `json:"-"`
}

// UnmarshalJSON parses a marketplace.json document, normalizing the
// real-world shapes the Python original's parse_marketplace_json accepts
// (models.py:454-515) that a naive field-for-field decode would reject or
// miss (caught by A/B testing against the live Python CLI, 2026-07-03):
//
//   - "owner" may be a plain string or an object; the object form's "name"
//     key is the owner name (Claude Code manifests use the object form).
//   - "plugins" that is not a JSON array (an object/string/number/null) is
//     tolerated as an empty plugin list rather than a hard error, mirroring
//     Python's warn-and-treat-as-empty fallback (:491-497); a non-object
//     element (including a JSON null one) inside a valid "plugins" array is
//     skipped, not fatal (:501-502). Both cases are also recorded on
//     StructuralErrors (ticket 11).
//   - Plugins may use the Copilot CLI shape ("repository": "owner/repo"
//     [+ "ref"]) instead of "source"; a github-typed source map is
//     synthesized. Entries with neither, or without a name, are dropped. A
//     "source" present but neither a string nor an object (e.g. a number,
//     array, or explicit null) drops the entry, mirroring the
//     "unrecognized source format" branch (:387-389). All these drops are
//     also recorded on StructuralErrors, with the Oracle's own message
//     (parsePluginEntry).
//   - A plugin whose dict source resolves (via "type", then "source", then
//     "kind" -- all three keys, matching _parse_plugin_entry's own
//     source_type derivation exactly) to "npm", an unsupported type, or a
//     known type missing/invalid its required field is dropped at parse
//     time -- there is no parse-vs-resolve split for this (ticket 11
//     attempt 2 correction: an earlier reading of this parser mistakenly
//     believed "kind" was resolve-layer-only; dictSourceDiagnostic's own
//     doc comment has the full account).
//   - A non-array "tags" value is coerced to empty rather than rejected
//     (:367); a non-string "version" is ignored rather than rejected.
//   - metadata.pluginRoot is captured into PluginRoot; a non-object
//     "metadata" or a non-string "pluginRoot" is tolerated as "" rather
//     than rejected.
func (m *MarketplaceManifest) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name        string          `json:"name"`
		Owner       json.RawMessage `json:"owner"`
		Description string          `json:"description"`
		Plugins     json.RawMessage `json:"plugins"`
		Metadata    json.RawMessage `json:"metadata"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Name = raw.Name
	m.Description = raw.Description
	m.PluginRoot = parseManifestPluginRoot(raw.Metadata)
	m.Owner = parseManifestOwner(raw.Owner)
	plugins, structuralErrors, err := parseManifestPlugins(raw.Plugins)
	if err != nil {
		return err
	}
	m.Plugins = plugins
	m.StructuralErrors = structuralErrors
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

// parseManifestPlugins extracts the "plugins" array, collecting the
// structural diagnostics documented on MarketplaceManifest.StructuralErrors
// exactly the way Python's parse_marketplace_json does (models.py:588-608):
// a "plugins" value present but not a JSON array (including explicit JSON
// null, since isinstance(None, list) is also False) becomes "plugins:
// expected a list" and the array is treated as empty; each element that is
// not a JSON object becomes "plugins[N]: expected an object"; each element
// that IS an object but fails per-field validation gets
// "plugins[N].<field-diagnostic>" from parsePluginEntry (ticket 11 attempt
// 2 -- see its doc comment for the full diagnostic set). A "plugins" key
// that is simply ABSENT is not an error either side (Python's
// `data.get("plugins", [])` default).
func parseManifestPlugins(raw json.RawMessage) ([]MarketplacePlugin, []string, error) {
	if len(raw) == 0 {
		return nil, nil, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, []string{"plugins: expected a list"}, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, []string{"plugins: expected a list"}, nil
	}
	var plugins []MarketplacePlugin
	var structuralErrors []string
	for i, entry := range entries {
		var obj map[string]json.RawMessage
		// A JSON null array element unmarshals into a nil map with NO
		// error (encoding/json leaves a map destination untouched -- i.e.
		// nil -- for a null source value), which is indistinguishable from
		// an error unless checked explicitly: obj==nil here means EITHER
		// "not an object at all" (null, a string, a number, ...) OR
		// "unmarshal itself failed" (an array/bool value) -- both cases
		// Python's `isinstance(entry, dict)` (models.py:600) also rejects.
		// A genuine empty object ({}) unmarshals to a non-nil, zero-length
		// map, so it is NOT caught by this check (matching Python: {} IS a
		// dict, just one that fails validation).
		if err := json.Unmarshal(entry, &obj); err != nil || obj == nil {
			structuralErrors = append(structuralErrors, fmt.Sprintf("plugins[%d]: expected an object", i))
			continue
		}
		p, ok, diag, err := parsePluginEntry(obj)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			plugins = append(plugins, p)
		} else if diag != "" {
			structuralErrors = append(structuralErrors, fmt.Sprintf("plugins[%d].%s", i, diag))
		}
	}
	return plugins, structuralErrors, nil
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

// jsonValueKind classifies a raw JSON value's top-level shape from its
// first non-whitespace byte -- the only thing needed to distinguish
// "string" from "object" from "explicit null" from "anything else"
// (number/array/bool) without a type-specific unmarshal that would itself
// obscure the distinction (e.g. unmarshaling null and an absent field both
// into a bare `any` gives the same Go nil). "absent" covers a zero-length
// RawMessage (the key was never in the source map).
func jsonValueKind(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "absent"
	}
	switch trimmed[0] {
	case '"':
		return "string"
	case '{':
		return "object"
	case 'n':
		return "null"
	default:
		return "other" // number, array, or bool
	}
}

// isValidRemoteCoordinate is a bounded, deliberately partial port of
// _is_valid_remote_coordinate (models.py:39-45): non-empty after trim, not
// shaped like a local filesystem path (looksLikeLocalPath, already shared
// with SourceKind classification), and free of control characters --
// mirroring DependencyReference.parse's own first rejection
// (models/dependency/reference.py:1745-1751), the one check
// _is_valid_remote_coordinate applies beyond local-path detection before
// asking whether the whole string parses as SOME valid dependency
// reference. It does NOT replicate DependencyReference.parse's full
// acceptance grammar (SSH URLs, ADO/Artifactory coordinates, GitLab nested
// groups, virtual-package subpaths, alias syntax, ...) -- that parser is
// its own multi-hundred-line module, well beyond the Structure check's
// scope (AGENTS.md's "deliberate but partial" parity). What this DOES
// correctly reject, matching the Oracle on every case this ticket's own
// fixtures and existing test suite exercise, is exactly what a genuinely
// malformed manifest looks like: an empty/missing coordinate, or a local
// path mistakenly given as a remote source.
func isValidRemoteCoordinate(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" {
		return false
	}
	if looksLikeLocalPath(v) {
		return false
	}
	for _, r := range v {
		if r < 32 {
			return false
		}
	}
	return true
}

// dictSourceDiagnostic ports _dict_source_error (models.py:50-77) for a
// plugin's dict-shaped source, returning "" when the source is valid or
// the Oracle's exact diagnostic message otherwise (always already prefixed
// "source: ..." -- callers do not add another "source." segment).
//
// sourceType is derived from "type", then "source", then "kind" -- all
// three keys, matching _parse_plugin_entry's OWN source_type derivation
// (models.py:447-454) verbatim: `for key in ("type", "source", "kind")`.
// Ticket 11 attempt 2 correction: an earlier version of this port
// deliberately checked only "type"/"source" here, believing it preserved
// mkt-026's documented "two-vs-three-key" parse-vs-resolve split
// (resolver.go's coercePluginType, TestResolvePluginSource_NPMDualLayer).
// Reading _parse_plugin_entry in full for this attempt shows that premise
// was wrong: the Oracle's OWN manifest-parse layer already derives
// source_type from all three keys, so a "kind: npm" source is rejected --
// with a structural_errors entry -- at PARSE time in the real Oracle, not
// deferred to a later resolve step. TestResolvePluginSource_NPMDualLayer
// and coercePluginType's doc comment are corrected in the same commit.
func dictSourceDiagnostic(src map[string]any) string {
	sourceType := strings.ToLower(strings.TrimSpace(firstNonEmptyString(src, "type", "source", "kind")))

	repo := firstNonEmptyString(src, "repo", "repository")
	hasRepo := strings.Contains(strings.TrimSpace(repo), "/")

	url, _ := src["url"].(string)
	hasURL := strings.TrimSpace(url) != ""

	switch {
	case sourceType == "npm":
		return "source: unsupported source type 'npm'"
	case sourceType == "" && !hasRepo:
		return "source: expected a supported source type or an owner/repository field"
	}
	switch sourceType {
	case "", "github", "url", "git-subdir", "gitlab":
		// Known set (including "" -- a bare repo with no explicit type,
		// handled entirely by the has_repo branch above): fall through to
		// each type's own field requirements below.
	default:
		return fmt.Sprintf("source: unsupported source type '%s'", sourceType)
	}
	switch sourceType {
	case "github":
		if !hasRepo {
			return "source: github requires an owner/repository field"
		}
		if !isValidRemoteCoordinate(repo) {
			return "source: github requires a valid non-local owner/repository field"
		}
	case "url":
		if !hasURL {
			return "source: url requires a non-empty url field"
		}
		if !isValidRemoteCoordinate(url) {
			return "source: url requires a valid non-local url field"
		}
	case "git-subdir", "gitlab":
		locator := url
		if hasRepo {
			locator = repo
		}
		if strings.TrimSpace(locator) == "" {
			return fmt.Sprintf("source: %s requires an owner/repository or url field", sourceType)
		}
		if !isValidRemoteCoordinate(locator) {
			return fmt.Sprintf("source: %s requires a valid non-local owner/repository or url field", sourceType)
		}
	}
	return ""
}

// firstNonEmptyString returns the first key in keys whose value in src is a
// non-empty string, or "" if none qualify -- mirrors Python's generator-
// expression discriminator lookups (`next((... for key in (...) if
// isinstance(...) and ...), "")`, models.py:447-454 and the repo/repository
// "or" fallback at :457).
func firstNonEmptyString(src map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := src[k].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// parsePluginEntry ports _parse_plugin_entry (models.py:422-547) in full:
// given one already-confirmed-to-be-a-JSON-object plugins[] element, it
// returns either (plugin, true, "", nil) for a structurally valid entry,
// or (zero, false, diagnostic, nil) naming exactly why Python would reject
// it (the caller prefixes "plugins[N]." to build the full structural_errors
// message), or (zero, false, "", err) for the one case that fails the
// WHOLE document instead of just this entry: an invalid source.tag_pattern
// (models.py:531 lets TagPatternError propagate uncaught -- silently
// skipping would change which tag a version range resolves to).
//
// Diagnostics ported, in the same order Python checks them:
//   - name absent, non-string, or blank after trim: "name: expected a
//     non-empty string"
//   - "source" key present but its JSON value is null, a number, an array,
//     or a bool (i.e. neither a string nor an object): "source: expected a
//     string or object"
//   - "source" is a dict: dictSourceDiagnostic's full _dict_source_error
//     port (npm rejection, no-type-and-no-repo, unsupported type, and each
//     of github/url/git-subdir/gitlab's own required-field and
//     valid-coordinate checks)
//   - "source" absent, "repository" present but not a string containing
//     "/": "repository: expected an owner/repository string"
//   - neither "source" nor "repository" present: "source: expected a
//     source or repository field"
//
// A `map[string]json.RawMessage` (not a struct decode) is used throughout
// specifically so a key's PRESENCE (even with an explicit JSON null value)
// is distinguishable from its ABSENCE -- the same class of bug ticket 11
// attempt 2 fixed for the array-element check itself: unmarshaling
// `{"source": null}`'s "source" value into a bare `any` field would give
// the same Go nil as an absent "source" key entirely, silently falling
// through to the "repository" fallback Python never takes once "source" is
// present at all (however null its value).
func parsePluginEntry(obj map[string]json.RawMessage) (plugin MarketplacePlugin, ok bool, diag string, err error) {
	name := strings.TrimSpace(rawStringField(obj["name"]))
	if name == "" {
		return MarketplacePlugin{}, false, "name: expected a non-empty string", nil
	}

	var source any
	if sourceRaw, hasSource := obj["source"]; hasSource {
		switch jsonValueKind(sourceRaw) {
		case "string":
			var s string
			if err := json.Unmarshal(sourceRaw, &s); err != nil {
				return MarketplacePlugin{}, false, "source: expected a string or object", nil
			}
			source = s
		case "object":
			var srcMap map[string]any
			_ = json.Unmarshal(sourceRaw, &srcMap) // kind=="object" guarantees success
			if d := dictSourceDiagnostic(srcMap); d != "" {
				return MarketplacePlugin{}, false, d, nil
			}
			source = srcMap
		default: // "null", "other" (number/array/bool)
			return MarketplacePlugin{}, false, "source: expected a string or object", nil
		}
	} else if repoRaw, hasRepo := obj["repository"]; hasRepo {
		// Copilot CLI shape: "repository": "owner/repo" (+ optional "ref").
		repo := ""
		if jsonValueKind(repoRaw) == "string" {
			_ = json.Unmarshal(repoRaw, &repo)
		}
		if !strings.Contains(repo, "/") {
			return MarketplacePlugin{}, false, "repository: expected an owner/repository string", nil
		}
		synth := map[string]any{"type": "github", "repo": repo}
		if ref := rawStringField(obj["ref"]); ref != "" {
			synth["ref"] = ref
		}
		source = synth
	} else {
		return MarketplacePlugin{}, false, "source: expected a source or repository field", nil
	}

	// Upstream validates a present source.tag_pattern here and lets the error
	// propagate (models.py:459-467). An absent key stays "" -- upstream's None
	// explicitly means "old marketplace.json", with the default supplied by the
	// resolver, not by this parser.
	tagPattern := ""
	if srcMap, isMap := source.(map[string]any); isMap {
		if rawTP, present := srcMap["tag_pattern"]; present {
			s, isStr := rawTP.(string)
			if !isStr {
				return MarketplacePlugin{}, false, "", fmt.Errorf("plugin %q source.tag_pattern must be a string, got %T", name, rawTP)
			}
			validated, verr := tagpattern.Validate(s, fmt.Sprintf("plugin %q source.tag_pattern", name))
			if verr != nil {
				return MarketplacePlugin{}, false, "", verr
			}
			tagPattern = validated
		}
	}

	return MarketplacePlugin{
		Name:        name,
		Source:      source,
		TagPattern:  tagPattern,
		Description: rawStringField(obj["description"]),
		Version:     rawStringField(obj["version"]),
		Tags:        rawStringSliceField(obj["tags"]),
		Registry:    rawStringField(obj["registry"]),
	}, true, "", nil
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
