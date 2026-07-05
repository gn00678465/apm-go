// This file (audit.go) implements mkt-043 修訂版 (`marketplace audit NAME
// [--strict]`): for a *registered* marketplace (consumer's
// internal/marketplace.FindByName + Fetch, this sub-task's sole
// cross-sub-task dependency), fetch every plugin's own apm.yml at its pinned
// ref (falling back to HEAD when unpinned) and flag dependencies.apm /
// devDependencies.apm entries that bypass the marketplace's version pinning
// by naming a direct git ref instead of a marketplace-shaped reference.
//
// audit's fetch reach is deliberately narrow -- mirroring Python apm's own
// audit.py::_resolve_plugin_github_coords exactly: only a plugin.source dict
// shaped {"type": "github", "repo": "owner/repo", ref?, host?, path?} is
// addressable. A bare relative-path string source, or a dict of any other
// "type", is FetchUnsupportedSource (skipped, never trips --strict). This is
// narrower than the full mkt-025/026 plugin-source resolution the separate,
// not-yet-landed install-ref sub-task implements -- audit does not depend on
// that work, by design.
//
// Likewise, the dependencies.apm/devDependencies.apm scan below is audit's
// own tolerant, best-effort yaml.Node walk (reusing this package's existing
// mappingValue/scalarString helpers from schema.go), NOT
// internal/manifest.ParseManifest: that domain parser is built to hard-fail
// a whole manifest on any dependency shape it does not recognize (mf-002/
// mf-003's required name/version, and -- more importantly -- ParseDepString
// currently rejects "pkg@mkt" marketplace-ref strings outright, since that
// grammar is mkt-020/033's install-ref scope, not yet landed). Reusing it
// here would make audit's single most important classification -- "is this
// dependency marketplace-shaped?" -- throw a hard parse error on exactly the
// clean case it must recognize, and would make one untrusted plugin's
// malformed apm.yml (missing name/version, an unrelated unknown dependency
// shape, ...) abort classification of every other entry in the same file.
// Python's own audit.py never goes through its manifest/dependency
// validation layer either (marketplace/audit.py:150's _collect_apm_dep_strings
// walks a raw yaml.safe_load() dict directly) -- confirming a tolerant,
// narrowly-scoped walk, not the strict domain parser, is the correct parity
// target here, despite design.md's shorthand "重用 internal/manifest 既有
// 解析" phrasing (which this file's authors intend as "don't invent a
// parallel *reader* for the devDependencies key name", not "route through
// ParseManifest").
package authoring

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/marketplace"
	"github.com/apm-go/apm/internal/yamlcore"
)

// FetchStatus is the outcome of fetching a single plugin's apm.yml, mirroring
// Python's audit.py::FetchStatus enum exactly -- confirmed by design.md's
// independent full-source audit to have exactly these five members, no more.
type FetchStatus int

const (
	// FetchOK means the plugin's apm.yml was fetched and parsed; Issues (on
	// the report) may still be empty.
	FetchOK FetchStatus = iota
	// FetchNoManifest means the plugin's repo has no apm.yml at the pinned
	// ref/HEAD (a 404) -- skipped, never trips --strict.
	FetchNoManifest
	// FetchUnsupportedSource means plugin.Source is not an addressable
	// github-dict manifest -- skipped, never trips --strict.
	FetchUnsupportedSource
	// FetchNetworkError means the fetch attempt failed for a reason other
	// than a confirmed 404 (transport failure, non-200/404 HTTP status, or
	// an untrusted host rejected before any request was sent). Counts toward
	// --strict's "unverifiable" bucket.
	FetchNetworkError
	// FetchParseError means the fetched bytes were not valid YAML, or did
	// not parse to a mapping at the document root. Counts toward --strict's
	// "unverifiable" bucket.
	FetchParseError
)

// DepClassification is how one dependencies.apm/devDependencies.apm entry
// resolves from the consumer's perspective (mkt-043 修訂版).
type DepClassification int

const (
	// DepMarketplace is a dict {name, marketplace} entry, or a string
	// matching the "pkg@mkt[#ref]" marketplace-ref grammar: already pinned
	// through the marketplace, never a bypass issue.
	DepMarketplace DepClassification = iota
	// DepLocal is a local (in-repo) path dependency: not a supply-chain
	// pinning concern.
	DepLocal
	// DepBypass is everything else that resolves via a direct git remote --
	// a bare shorthand, a full URL, an SCP remote, or a {git: ...} dict --
	// which bypasses the marketplace's version pinning and tracks that
	// remote's own ref directly.
	DepBypass
)

// DepIssue is one DepBypass finding: the offending dependency string (best-
// effort display form, not necessarily the exact source bytes) and a
// suggested dict-form replacement.
type DepIssue struct {
	Dep            string
	Classification DepClassification
	Suggestion     string
}

// PluginAuditReport is one plugin's audit outcome.
type PluginAuditReport struct {
	PluginName  string
	FetchStatus FetchStatus
	// Issues is only ever non-empty when FetchStatus == FetchOK.
	Issues []DepIssue
	// Detail is a human-readable explanation for any non-OK FetchStatus.
	Detail string
}

// ErrApmYMLNotFound is the sentinel ApmYMLFetcher implementations return for
// a confirmed 404 (as opposed to any other failure) -- auditPlugin uses
// errors.Is against this to distinguish FetchNoManifest from
// FetchNetworkError.
var ErrApmYMLNotFound = errors.New("apm.yml not found at the pinned ref")

// ApmYMLFetcher abstracts "fetch one file's raw bytes from a GitHub-
// compatible host at a given ref" for testing, mirroring refcheck.go's
// RefLister seam pattern.
type ApmYMLFetcher interface {
	// FetchRaw returns the raw bytes of path (a repo-relative apm.yml
	// location) inside owner/repo on host, at ref. Implementations must
	// return an error wrapping ErrApmYMLNotFound for a confirmed 404, and
	// any other error for every other failure.
	FetchRaw(host, owner, repo, path, ref string) ([]byte, error)
}

// DefaultApmYMLFetcher is the production ApmYMLFetcher: real GitHub Contents
// API requests.
var DefaultApmYMLFetcher ApmYMLFetcher = githubRawFetcher{}

// RunAudit implements mkt-043 修訂版 for every plugin in m, isolating each
// plugin's failure into its own report (mirrors Python's run_audit: one bad
// plugin never aborts the rest of the marketplace's audit).
func RunAudit(m *marketplace.MarketplaceManifest, marketplaceName, marketplaceHost string, fetcher ApmYMLFetcher) []PluginAuditReport {
	reports := make([]PluginAuditReport, 0, len(m.Plugins))
	for _, p := range m.Plugins {
		reports = append(reports, auditPlugin(p, marketplaceName, marketplaceHost, fetcher))
	}
	return reports
}

// auditPlugin fetches and audits a single plugin's apm.yml.
func auditPlugin(p marketplace.MarketplacePlugin, marketplaceName, marketplaceHost string, fetcher ApmYMLFetcher) PluginAuditReport {
	host, owner, repo, ref, path, ok := resolvePluginGithubCoords(p.Source, marketplaceHost)
	if !ok {
		return PluginAuditReport{
			PluginName:  p.Name,
			FetchStatus: FetchUnsupportedSource,
			Detail:      "plugin source is not an addressable github manifest",
		}
	}

	data, err := fetcher.FetchRaw(host, owner, repo, path, ref)
	if err != nil {
		if errors.Is(err, ErrApmYMLNotFound) {
			return PluginAuditReport{
				PluginName:  p.Name,
				FetchStatus: FetchNoManifest,
				Detail:      fmt.Sprintf("no apm.yml at %q @ %s", path, ref),
			}
		}
		return PluginAuditReport{PluginName: p.Name, FetchStatus: FetchNetworkError, Detail: err.Error()}
	}

	root, err := parseApmYMLRoot(data)
	if err != nil {
		return PluginAuditReport{PluginName: p.Name, FetchStatus: FetchParseError, Detail: err.Error()}
	}

	var issues []DepIssue
	for _, entry := range collectApmDepEntries(root) {
		cls, dep, ok := classifyDepEntry(entry)
		if !ok || cls != DepBypass {
			continue
		}
		issues = append(issues, DepIssue{Dep: dep, Classification: cls, Suggestion: suggestReplacement(dep, marketplaceName)})
	}
	return PluginAuditReport{PluginName: p.Name, FetchStatus: FetchOK, Issues: issues}
}

// parseApmYMLRoot parses a fetched apm.yml's raw bytes and returns its
// top-level mapping node, or an error describing why the bytes could not be
// used (malformed YAML, or a non-mapping document root) -- both fold into
// FetchParseError.
func parseApmYMLRoot(data []byte) (*yaml.Node, error) {
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, fmt.Errorf("malformed YAML: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("apm.yml root is not a mapping")
	}
	return doc.Content[0], nil
}

// ── dependency collection + classification ──────────────────────────────

// collectApmDepEntries gathers every dependencies.apm[] and
// devDependencies.apm[] element (in that order) from a plugin apm.yml's
// parsed root mapping -- mkt-043's "掃 dependencies 與 devDependencies".
func collectApmDepEntries(root *yaml.Node) []*yaml.Node {
	var entries []*yaml.Node
	for _, section := range [2]string{"dependencies", "devDependencies"} {
		sec := mappingValue(root, section)
		if sec == nil || sec.Kind != yaml.MappingNode {
			continue
		}
		apmList := mappingValue(sec, "apm")
		if apmList == nil || apmList.Kind != yaml.SequenceNode {
			continue
		}
		entries = append(entries, apmList.Content...)
	}
	return entries
}

// classifyDepEntry classifies one dependencies.apm[]/devDependencies.apm[]
// element node. ok=false means the entry could not be flattened to anything
// classifiable (a blank string, an empty {git: ""}, or a dict shape this
// audit does not recognize, e.g. a registry {id: ...} entry) and must be
// skipped entirely -- mirroring Python's _normalize_dep_entry returning None
// (silently dropped, never counted as any classification).
//
// Dict-shape handling deliberately does not re-inspect a {git: ...} dict's
// value for a local-path shape the way a plain string entry would (Python's
// classify_dependency technically would, since it flattens-then-reclassifies
// through the same string path): design.md/checklist's mkt-043 修訂版 text
// reads "其餘 git ref 與 {git:} 物件" as bypass, full stop -- any {git: ...}
// dict is a structural bypass signal regardless of its value's shape. This
// is a narrower, more defensible reading than Python's incidental behavior,
// which was never called out as a checklist requirement to replicate.
func classifyDepEntry(entry *yaml.Node) (cls DepClassification, dep string, ok bool) {
	switch entry.Kind {
	case yaml.ScalarNode:
		s := strings.TrimSpace(entry.Value)
		if s == "" {
			return 0, "", false
		}
		return classifyDepString(s), s, true
	case yaml.MappingNode:
		if mappingValue(entry, "marketplace") != nil {
			name := scalarString(entry, "name")
			mkt := scalarString(entry, "marketplace")
			return DepMarketplace, fmt.Sprintf("{name: %s, marketplace: %s}", name, mkt), true
		}
		if mappingValue(entry, "git") != nil {
			s := strings.TrimSpace(scalarString(entry, "git"))
			if s == "" {
				return 0, "", false
			}
			return DepBypass, s, true
		}
		if mappingValue(entry, "path") != nil {
			s := strings.TrimSpace(scalarString(entry, "path"))
			if s == "" {
				return 0, "", false
			}
			return DepLocal, s, true
		}
		return 0, "", false
	default:
		return 0, "", false
	}
}

// marketplaceDepRefPattern mirrors Python's resolver.py::_MARKETPLACE_RE
// (`^([a-zA-Z0-9._-]+)@([a-zA-Z0-9._-]+)(?:#(.+))?$`): audit only needs to
// know "is this string marketplace-ref-shaped", not extract/validate the
// individual name/marketplace/ref groups (mkt-021's semver-range-in-ref
// rejection is install-ref's ParseRef concern, not audit's -- audit does not
// "relitigate grammar", matching classify_dependency's own doc comment).
var marketplaceDepRefPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+@[a-zA-Z0-9._-]+(#.+)?$`)

// classifyDepString classifies a plain dependencies.apm[] string entry.
func classifyDepString(s string) DepClassification {
	if isLocalDependencyPath(s) {
		return DepLocal
	}
	if marketplaceDepRefPattern.MatchString(s) {
		return DepMarketplace
	}
	return DepBypass
}

// isLocalDependencyPath mirrors Python's
// DependencyReference.is_local_path exactly: "./", "../", "/", "~/", "~\",
// ".\", "..\" prefixes, or a Windows drive letter ("C:\..."/"C:/..."), with
// a protocol-relative "//..." URL explicitly excluded first (it shares the
// "/" prefix but is not a local path). This is intentionally a local copy,
// not a re-export of internal/manifest's unexported isLocalPath or
// internal/marketplace's unexported looksLikeLocalPath: this sub-task's
// Rollback Points restrict every existing file to a single one-line change
// (marketplaceCmd()'s subcommand wiring) -- audit.go's own file must be
// self-contained.
func isLocalDependencyPath(s string) bool {
	if strings.HasPrefix(s, "//") {
		return false
	}
	if strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") || strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, "~/") || strings.HasPrefix(s, `~\`) || strings.HasPrefix(s, `.\`) || strings.HasPrefix(s, `..\`) {
		return true
	}
	if len(s) >= 3 {
		c := s[0]
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if isAlpha && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
			return true
		}
	}
	return false
}

// suggestReplacement builds a DepBypass issue's hint text (mkt-043 修訂版):
// it must point at the dict form {name: X, marketplace: Y} -- the shape
// dependencies.apm actually accepts (mkt-033) -- never the "pkg@mkt" string
// shorthand the dependency parser rejects (that was the original Python
// tool's own internal contradiction; this checklist entry explicitly says
// "不可照抄"). pkgHint extraction mirrors Python's audit.py::_suggest_replacement:
// strip any "#ref" fragment, take the last "/"-separated segment, strip a
// trailing ".git".
func suggestReplacement(dep, marketplaceName string) string {
	base := dep
	if idx := strings.Index(dep, "#"); idx >= 0 {
		base = dep[:idx]
	}
	pkgHint := base
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		pkgHint = base[idx+1:]
	}
	pkgHint = strings.TrimSuffix(pkgHint, ".git")
	pkgHint = strings.TrimSpace(pkgHint)
	if pkgHint == "" {
		pkgHint = "package"
	}
	return fmt.Sprintf("depend on it via the marketplace instead of a direct git ref: {name: %s, marketplace: %s}", pkgHint, marketplaceName)
}

// ── plugin source resolution (github-dict sources only) ──────────────────

// resolvePluginGithubCoords extracts (host, owner, repo, ref, apmYMLPath)
// for a plugin whose Source is a github-typed dict shape ({"type": "github",
// "repo": "owner/repo", ref?, host?, path?}). ok=false for any other Source
// shape (a bare relative-path string, or a dict of any other "type") --
// this file's own doc comment explains why that reach is deliberately
// narrower than the full mkt-025/026 plugin-source resolution. Mirrors
// Python's audit.py::_resolve_plugin_github_coords field-for-field,
// including its "repo must be exactly owner/repo, not owner/repo/extra"
// guard (a sub-path belongs in the separate "path" field) and its ref
// default ("ref" absent or empty -> "HEAD", not the marketplace's own
// default ref).
func resolvePluginGithubCoords(source any, fallbackHost string) (host, owner, repo, ref, apmYMLPath string, ok bool) {
	m, isMap := source.(map[string]any)
	if !isMap {
		return "", "", "", "", "", false
	}
	t, _ := m["type"].(string)
	if t != "github" {
		return "", "", "", "", "", false
	}

	repoField, _ := m["repo"].(string)
	ownerPart, repoPart, found := strings.Cut(repoField, "/")
	if !found || ownerPart == "" || repoPart == "" || strings.Contains(repoPart, "/") {
		return "", "", "", "", "", false
	}

	ref, _ = m["ref"].(string)
	if ref == "" {
		ref = "HEAD"
	}

	host, _ = m["host"].(string)
	if host == "" {
		host = fallbackHost
	}
	if host == "" {
		host = "github.com"
	}

	pathField, _ := m["path"].(string)
	pathField = strings.Trim(pathField, "/")
	apmYMLPath = "apm.yml"
	if pathField != "" {
		if pathHasTraversalSegment(pathField) {
			return "", "", "", "", "", false
		}
		apmYMLPath = pathField + "/apm.yml"
	}

	return host, ownerPart, repoPart, ref, apmYMLPath, true
}

// pathHasTraversalSegment reports whether any "/"-separated segment of p is
// "..", rejecting a plugin dict source's "path" field from walking the
// GitHub Contents API request outside the declared subdirectory.
func pathHasTraversalSegment(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// ── production ApmYMLFetcher: GitHub Contents API ────────────────────────

// auditGithubPATEnvVar / auditGithubHostEnvVar name the same environment
// variables internal/marketplace/client_github.go reads
// (GITHUB_APM_PAT/GITHUB_HOST) -- duplicated as local constants (not
// imported) for the same Rollback Points reason isLocalDependencyPath is a
// local copy: this sub-task's only permitted edit to an existing file is
// marketplaceCmd()'s one-line subcommand registration.
const (
	auditGithubPATEnvVar  = "GITHUB_APM_PAT"
	auditGithubHostEnvVar = "GITHUB_HOST"
)

const auditGithubFetchTimeout = 30 * time.Second

// auditGithubAPIBaseFor computes the GitHub Contents API root for host,
// mirroring internal/marketplace/client_github.go's githubAPIBaseFor. A var
// (not a plain function) so tests can redirect it at an httptest.Server.
var auditGithubAPIBaseFor = func(host string) string {
	h := strings.ToLower(host)
	switch {
	case h == "github.com":
		return "https://api.github.com"
	case strings.HasSuffix(h, ".ghe.com"):
		return "https://api." + host
	default:
		return "https://" + host + "/api/v3"
	}
}

// isGithubFamilyAuditHost reports whether host is within the GitHub host
// family (github.com, any "*.github.com" subdomain, any "*.ghe.com" GHE
// Cloud host, or the self-hosted GHES host configured via GITHUB_HOST).
// githubRawFetcher.FetchRaw refuses to make any request at all for a host
// outside this family -- mirroring Python's fetch_raw, which raises
// MarketplaceError (surfaced by auditPlugin as FetchNetworkError, an
// "unverifiable" outcome that trips --strict) before any network access for
// exactly this case, rather than silently skipping an attacker-controlled
// "host" field in a plugin's dict source.
func isGithubFamilyAuditHost(host string) bool {
	h := strings.ToLower(host)
	if h == "github.com" || strings.HasSuffix(h, ".github.com") || strings.HasSuffix(h, ".ghe.com") {
		return true
	}
	ghesHost := strings.ToLower(strings.TrimSpace(os.Getenv(auditGithubHostEnvVar)))
	if ghesHost == "" || ghesHost == "github.com" || ghesHost == "gitlab.com" {
		return false
	}
	return h == ghesHost
}

// githubRawFetcher is DefaultApmYMLFetcher's real implementation.
type githubRawFetcher struct{}

// FetchRaw fetches path from owner/repo on host at ref via the GitHub
// Contents API, requesting the raw media type so the response body IS the
// file's raw content (same convention as client_github.go's fetchGitHub).
func (githubRawFetcher) FetchRaw(host, owner, repo, path, ref string) ([]byte, error) {
	if !isGithubFamilyAuditHost(host) {
		return nil, fmt.Errorf("host is not a supported plugin source (only GitHub-family hosts can be audited)")
	}

	reqURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s",
		auditGithubAPIBaseFor(host),
		url.PathEscape(owner),
		url.PathEscape(repo),
		escapeAuditContentsPath(path),
		url.QueryEscape(ref),
	)

	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build GitHub contents request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3.raw")
	if pat := os.Getenv(auditGithubPATEnvVar); pat != "" {
		req.Header.Set("Authorization", "token "+pat)
	}

	client := &http.Client{Timeout: auditGithubFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		// credsec: never echo reqURL or the transport error's own message --
		// mirrors internal/marketplace/client_github.go's fetchGitHubAtPath.
		return nil, fmt.Errorf("could not reach GitHub (network error)")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrApmYMLNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned HTTP %d fetching apm.yml", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read GitHub response: %w", err)
	}
	return data, nil
}

// escapeAuditContentsPath percent-encodes each "/"-separated segment of a
// Contents API path independently, preserving the path's own "/"
// separators -- mirrors client_github.go's escapeContentsPath.
func escapeAuditContentsPath(path string) string {
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "/")
}
