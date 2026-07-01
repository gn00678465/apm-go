package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/apm-go/apm/internal/credsec"
)

// credentialHeaders are stripped on a redirect that either crosses host class
// (sc-003) or downgrades below the non-https gate (sc-008). Mirrors credsec's
// internal set (which is unexported); kept local so the registry client owns its
// composed redirect policy without touching the Phase-5 credsec package.
var credentialHeaders = []string{"Authorization", "Proxy-Authorization", "Cookie"}

// VersionEntry is one row from GET /versions (registry-http-api.md §3.1).
type VersionEntry struct {
	Version     string
	Digest      string
	PublishedAt string
}

// HTTPError carries the registry HTTP status so callers can map 401/403 to a
// remediation hint. Its message is already credential-redacted.
type HTTPError struct {
	Status int
	URL    string
	Msg    string
}

func (e *HTTPError) Error() string {
	if e.Msg != "" {
		return fmt.Sprintf("registry HTTP %d from %s: %s", e.Status, e.URL, e.Msg)
	}
	return fmt.Sprintf("registry HTTP %d from %s", e.Status, e.URL)
}

// Client is a minimal registry consumer for one base URL.
type Client struct {
	baseURL  string
	http     *http.Client
	cred     Credential
	insecure bool
	redactor *credsec.Redactor
}

// NewClient builds a registry client. aliases is credsec's host-class alias map
// (map[primaryHost][]alias) built from the registry's aliases. insecure permits
// credential attach over http to loopback/insecure registries (sc-008).
func NewClient(base string, cred Credential, aliases map[string][]string, insecure bool) (*Client, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil, fmt.Errorf("registry base url is required")
	}
	dropCrossClass := credsec.NewAuthDropRedirect(aliases)
	c := &Client{
		baseURL:  base,
		cred:     cred,
		insecure: insecure,
		// Redact the header value AND every raw credential literal (Basic user/pass)
		// so a server echoing decoded credentials still gets scrubbed (sc-007).
		redactor: credsec.NewRedactor(append([]string{cred.Value}, cred.redact...)...),
	}
	c.http = &http.Client{
		Timeout: 60 * time.Second,
		// Composed redirect policy: drop credentials on cross-host-class (sc-003)
		// AND on any redirect target that fails the non-https gate (sc-008) — the
		// latter catches a same-host https->http downgrade the host-class check
		// alone would miss.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if err := dropCrossClass(req, via); err != nil {
				return err
			}
			if ok, _ := credsec.ShouldAttachCredential(req.URL.String(), c.insecure); !ok {
				for _, h := range credentialHeaders {
					req.Header.Del(h)
				}
			}
			return nil
		},
	}
	return c, nil
}

// attachAuth sets Authorization on req only when the target passes the sc-008
// non-https gate (req-sc-008) and a credential is configured.
func (c *Client) attachAuth(req *http.Request) error {
	if c.cred.Header() == "" {
		return nil
	}
	ok, err := credsec.ShouldAttachCredential(req.URL.String(), c.insecure)
	if err != nil {
		return fmt.Errorf("credential gate: %s", c.redactor.Redact(err.Error()))
	}
	if ok {
		req.Header.Set("Authorization", c.cred.Header())
	}
	return nil
}

func (c *Client) get(rawURL, accept string) ([]byte, string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build request: %s", c.redactor.Redact(err.Error()))
	}
	// Reject URL-embedded credentials (userinfo): Go would synthesize a Basic
	// Authorization from req.URL.User, bypassing the sc-008 gate and cross-class
	// drop, and such a URL must never have been persisted to the lockfile.
	if req.URL.User != nil {
		return nil, "", fmt.Errorf("refusing registry request to a URL with embedded credentials")
	}
	req.Header.Set("Accept", accept)
	if err := c.attachAuth(req); err != nil {
		return nil, "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("registry request failed: %s", c.redactor.Redact(err.Error()))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read registry response: %s", c.redactor.Redact(err.Error()))
	}
	if resp.StatusCode >= 400 {
		return nil, "", &HTTPError{
			Status: resp.StatusCode,
			URL:    rawURL,
			Msg:    c.redactor.Redact(strings.TrimSpace(string(body))),
		}
	}
	ctype := strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0])
	return body, ctype, nil
}

// ListVersions calls GET /v1/packages/{owner}/{repo}/versions.
func (c *Client) ListVersions(owner, repo string) ([]VersionEntry, error) {
	u := fmt.Sprintf("%s/v1/packages/%s/%s/versions", c.baseURL, url.PathEscape(owner), url.PathEscape(repo))
	body, _, err := c.get(u, "application/json")
	if err != nil {
		return nil, err
	}
	var payload struct {
		Versions []struct {
			Version     string `json:"version"`
			Digest      string `json:"digest"`
			PublishedAt string `json:"published_at"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("registry /versions is not valid JSON: %w", err)
	}
	out := make([]VersionEntry, 0, len(payload.Versions))
	for _, v := range payload.Versions {
		out = append(out, VersionEntry{Version: v.Version, Digest: v.Digest, PublishedAt: v.PublishedAt})
	}
	return out, nil
}

// ArchiveURL is the canonical resolved_url for (owner, repo, version).
func (c *Client) ArchiveURL(owner, repo, version string) string {
	return fmt.Sprintf("%s/v1/packages/%s/%s/versions/%s/download",
		c.baseURL, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(version))
}

// Download calls GET /v1/packages/{owner}/{repo}/versions/{version}/download.
// Returns (archive bytes, content-type). apm-go accepts only tar.gz containers
// (req-sc-004 rejects application/zip), so it advertises application/gzip only.
func (c *Client) Download(owner, repo, version string) ([]byte, string, error) {
	return c.get(c.ArchiveURL(owner, repo, version), "application/gzip")
}

// FetchURL fetches an absolute URL (lockfile replay path) with the same auth
// gate and redirect policy.
func (c *Client) FetchURL(rawURL string) ([]byte, string, error) {
	return c.get(rawURL, "application/gzip")
}
