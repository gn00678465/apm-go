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

// gitlabAPIBaseFn computes the REST v4 API root for a given host
// ("https://{host}/api/v4", which also covers gitlab.com itself). It is a
// var, not a plain function, so tests can redirect it at an httptest.Server
// instead of a real (https) GitLab instance.
var gitlabAPIBaseFn = func(host string) string {
	return "https://" + host + "/api/v4"
}

// gitlabPATEnvVar names the environment variable fetchGitLab reads its
// GitLab personal access token from (mkt-023/mkt-011).
const gitlabPATEnvVar = "GITLAB_APM_PAT"

const gitlabFetchTimeout = 30 * time.Second

// fetchGitLab retrieves a KindGitLab source's manifest via the GitLab REST
// v4 "get raw file" endpoint, a plain-text response -- no base64 envelope
// to unwrap, unlike GitHub's default Contents API media type. Both the
// project path (owner/repo) and the file path are percent-encoded as a
// whole (including their internal "/" separators, per the GitLab API's own
// requirement), unlike GitHub's Contents API which needs literal "/"
// separators preserved.
//
// mkt-003: when s.Path is unset or still the parser's default
// (defaultManifestPath), a 404 on one candidate path falls through to the
// next in localManifestProbeOrder, mirroring fetchGitHub/fetchLocal's
// fallback probing. An explicit, non-default path is tried as-is, with no
// fallback probing. Any non-404 failure is returned immediately without
// trying further candidates.
func fetchGitLab(ctx context.Context, s *MarketplaceSource) (*MarketplaceManifest, error) {
	ref := s.Ref
	if ref == "" {
		ref = defaultSourceRef
	}
	candidates := localManifestCandidates(s.Path)

	for _, path := range candidates {
		manifest, notFound, err := fetchGitLabAtPath(ctx, s, path, ref)
		if err == nil {
			return manifest, nil
		}
		if !notFound {
			return nil, err
		}
	}
	return nil, fmt.Errorf("no marketplace manifest found in %s/%s@%s on GitLab (tried %s)",
		s.Owner, s.Repo, ref, strings.Join(candidates, ", "))
}

// fetchGitLabAtPath fetches a single candidate manifest path, reporting
// whether the failure (if any) was a 404 -- the only case fetchGitLab's
// caller treats as "try the next candidate" rather than a hard failure.
func fetchGitLabAtPath(ctx context.Context, s *MarketplaceSource, path, ref string) (*MarketplaceManifest, bool, error) {
	projectPath := url.PathEscape(s.Owner + "/" + s.Repo)
	filePath := url.PathEscape(path)
	reqURL := fmt.Sprintf("%s/projects/%s/repository/files/%s/raw?ref=%s",
		gitlabAPIBaseFn(s.Host), projectPath, filePath, url.QueryEscape(ref))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("build GitLab repository files request: %w", err)
	}
	if isTrustedGitLabHost(s.Host) {
		if pat := os.Getenv(gitlabPATEnvVar); pat != "" {
			req.Header.Set("PRIVATE-TOKEN", pat)
		}
	}

	client := &http.Client{Timeout: gitlabFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		// credsec: never echo reqURL or the transport error's own message,
		// same rationale as fetchGitHub/fetchURL.
		return nil, false, fmt.Errorf("could not reach GitLab (network error)")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, true, fmt.Errorf("marketplace manifest %q not found in %s/%s@%s on GitLab", path, s.Owner, s.Repo, ref)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("GitLab returned HTTP %d fetching the marketplace manifest", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, urlFetchMaxBytes+1))
	if err != nil {
		return nil, false, fmt.Errorf("read GitLab response: %w", err)
	}
	if int64(len(data)) > urlFetchMaxBytes {
		return nil, false, fmt.Errorf("GitLab marketplace manifest response exceeds %d byte limit", urlFetchMaxBytes)
	}

	var manifest MarketplaceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, false, fmt.Errorf("parse marketplace manifest from GitLab: %w", err)
	}
	return &manifest, false, nil
}

// isTrustedGitLabHost reports whether host is trusted to receive the
// GITLAB_APM_PAT token (mkt-011 revised): gitlab.com or an explicitly
// allowlisted self-managed GitLab host (GITLAB_HOST / APM_GITLAB_HOSTS),
// via the same exact-match allowlist classifySourceHost uses to route a
// source to KindGitLab. This must NEVER be a substring test: forwarding the
// PAT to any host merely containing "gitlab" would exfiltrate the token to
// attacker-controlled hosts like gitlab.evil.com. Like isTrustedGitHubHost,
// this guard is independent of Kind()'s own classification having already
// filtered untrusted hosts out (defense in depth).
func isTrustedGitLabHost(host string) bool {
	return isGitLabFamilyHost(strings.ToLower(host))
}
