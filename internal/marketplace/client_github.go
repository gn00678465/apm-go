package marketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// githubAPIBaseFor computes the GitHub Contents API root for host: the
// public API for github.com, "https://api.{host}" for a GitHub Enterprise
// Cloud data-residency host ("*.ghe.com" -- GitHub's own docs name
// api.{subdomain}.ghe.com as that tenant's API root), or
// "https://{host}/api/v3" for a self-hosted GitHub Enterprise Server host
// (GITHUB_HOST, aligned with the Python original's
// AuthResolver.classify_host api_base convention for "ghes"). It is a var
// (a func value), not a plain function, so tests can redirect it at an
// httptest.Server instead of the real API. fetchGitHub only ever runs for
// Kind() == KindGitHub, which classifySourceHost (models.go) only assigns
// to one of these three host shapes, so any host reaching the default
// branch here is the configured GHES host.
var githubAPIBaseFor = func(host string) string {
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

// githubPATEnvVar names the environment variable fetchGitHub reads its
// GitHub personal access token from (mkt-023/mkt-011).
const githubPATEnvVar = "GITHUB_APM_PAT"

const githubFetchTimeout = 30 * time.Second

// fetchGitHub retrieves a KindGitHub source's manifest via the GitHub
// Contents API, requesting the raw media type so the response body IS the
// file's raw content -- not a base64-encoded JSON envelope (design.md: live
// curl-verified, 2026-07-03). This also sidesteps the Contents API's
// base64-response size limit for files over ~1MB.
//
// mkt-003: when s.Path is unset or still the parser's default
// (defaultManifestPath), a 404 on one candidate path falls through to the
// next in localManifestProbeOrder -- the same fallback fetchLocal applies to
// a plain local checkout -- so the common ".claude-plugin/marketplace.json"
// layout is found without the caller having to know it in advance. An
// explicit, non-default path is tried as-is, with no fallback probing. Any
// non-404 failure (network error, non-200/404 status, malformed JSON) is
// returned immediately without trying further candidates.
func fetchGitHub(ctx context.Context, s *MarketplaceSource) (*MarketplaceManifest, error) {
	ref := s.Ref
	if ref == "" {
		ref = defaultSourceRef
	}
	candidates := localManifestCandidates(s.Path)

	for _, path := range candidates {
		manifest, notFound, err := fetchGitHubAtPath(ctx, s, path, ref)
		if err == nil {
			return manifest, nil
		}
		if !notFound {
			return nil, err
		}
	}
	return nil, fmt.Errorf("no marketplace manifest found in %s/%s@%s on GitHub (tried %s)",
		s.Owner, s.Repo, ref, strings.Join(candidates, ", "))
}

// fetchGitHubAtPath fetches a single candidate manifest path, reporting
// whether the failure (if any) was a 404 -- the only case fetchGitHub's
// caller treats as "try the next candidate" rather than a hard failure.
func fetchGitHubAtPath(ctx context.Context, s *MarketplaceSource, path, ref string) (*MarketplaceManifest, bool, error) {
	reqURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s",
		githubAPIBaseFor(s.Host),
		url.PathEscape(s.Owner),
		url.PathEscape(s.Repo),
		escapeContentsPath(path),
		url.QueryEscape(ref),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("build GitHub contents request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3.raw")
	if isTrustedGitHubHost(s.Host) {
		if pat := os.Getenv(githubPATEnvVar); pat != "" {
			req.Header.Set("Authorization", "token "+pat)
		}
	}

	client := &http.Client{Timeout: githubFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		// credsec: never echo reqURL or the transport error's own message --
		// Go's http.Client wraps a failed request in a *url.Error whose
		// Error() embeds the full request URL (mirrors
		// internal/mcpregistry.getJSON's regression test).
		return nil, false, fmt.Errorf("could not reach GitHub (network error)")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, true, fmt.Errorf("marketplace manifest %q not found in %s/%s@%s on GitHub", path, s.Owner, s.Repo, ref)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("GitHub returned HTTP %d fetching the marketplace manifest", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("read GitHub response: %w", err)
	}

	var manifest MarketplaceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, false, fmt.Errorf("parse marketplace manifest from GitHub: %w", err)
	}
	return &manifest, false, nil
}

// isTrustedGitHubHost reports whether host is trusted to receive the
// GITHUB_APM_PAT token (mkt-011 revised): github.com, any *.github.com
// subdomain, any *.ghe.com host (GitHub Enterprise Cloud), or the
// self-hosted GHES host configured via GITHUB_HOST -- the same github-family
// classification classifySourceHost (models.go) uses to route a source to
// KindGitHub in the first place. Other hosts never receive the token, even
// if fetchGitHub is invoked directly with such a host -- this guard does not
// rely on Kind()'s own host classification having already filtered
// untrusted hosts out.
func isTrustedGitHubHost(host string) bool {
	h := strings.ToLower(host)
	if h == "github.com" || strings.HasSuffix(h, ".github.com") || strings.HasSuffix(h, ".ghe.com") {
		return true
	}
	return isGitHubEnterpriseServerHost(host)
}

// escapeContentsPath percent-encodes each "/"-separated segment of a
// Contents API path independently, preserving the path's own "/"
// separators (unlike url.PathEscape applied to the whole string, which
// would also escape them into "%2F" -- the GitHub Contents API expects a
// normal hierarchical path, unlike GitLab's raw-file endpoint).
func escapeContentsPath(path string) string {
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "/")
}
