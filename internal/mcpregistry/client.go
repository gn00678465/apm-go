// Package mcpregistry implements a minimal MCP Registry v0.1 client
// (https://github.com/modelcontextprotocol/registry) for resolving a bare
// server name (as used by `apm install --mcp <name>`) into its remote
// (http/sse/streamable-http) connection details. Package-based (npm/docker/
// pypi/homebrew) stdio servers are intentionally out of scope -- see
// FindServerByReference.
package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/apm-go/apm/internal/manifest"
)

// DefaultBaseURL is used when no --registry flag or MCP_REGISTRY_URL env
// override is given.
const DefaultBaseURL = "https://api.mcp.github.com"

const v0_1Prefix = "/v0.1"

const maxBaseURLLength = 2048

// mcpRegistryMaxBytes caps how much of a registry JSON response getJSON will
// read before decoding, bounding memory use against a hostile or misbehaving
// registry. A var so tests can shrink it instead of allocating a real 10MB+
// body.
var mcpRegistryMaxBytes int64 = 10 * 1024 * 1024

// Client is a minimal MCP Registry v0.1 HTTP client.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	// Source is where BaseURL came from; it names the setting to check in
	// an UnreachableError.
	Source Source
}

// Source is the setting a registry URL came from, as the Oracle's
// resolve_mcp_registry_url names it (registry/client.py:162-205). apm-go has
// no `apm config`, so it has no "config" source.
type Source string

const (
	SourceDefault Source = "default"
	// SourceEnv is MCP_REGISTRY_URL.
	SourceEnv Source = "env"
	// SourceExplicit is the registry: field of an apm.yml MCP entry.
	SourceExplicit Source = "explicit"
	// SourceFlag is install --mcp's --registry.
	SourceFlag Source = "flag"
)

// InsecureURLError refuses an http:// registry URL from MCP_REGISTRY_URL or
// a registry: field while MCP_REGISTRY_ALLOW_HTTP is unset or empty
// (registry/client.py:325-338). No request was sent.
type InsecureURLError struct {
	URL    string
	Source Source
}

func (e *InsecureURLError) Error() string {
	hint := "Check the --registry value or the dependency's registry: URL."
	if e.Source == SourceEnv {
		hint = "Check MCP_REGISTRY_URL if set."
	}
	return fmt.Sprintf("Insecure MCP registry URL %s: http:// is not allowed by default. Set MCP_REGISTRY_ALLOW_HTTP=1 to opt in to plaintext HTTP (not recommended for production). %s",
		manifest.PyRepr(e.URL), hint)
}

// UnreachableError is a lookup of Server that the registry did not answer:
// a network error, a search that is not 2xx, or a server fetch that is
// neither 2xx nor 404 (registry/operations.py:229-256, registry/client.py:
// 422,457,623-640).
type UnreachableError struct {
	URL    string
	Source Source
	Server string
}

func (e *UnreachableError) Error() string {
	var hint string
	switch e.Source {
	case SourceEnv:
		hint = "Check MCP_REGISTRY_URL."
	case SourceDefault:
		// The Oracle takes the default registry's failure for a server
		// that is not there; apm-go uses the sentence of its fail-closed
		// branch. Deviation (issue #123 comment 6284804045, item 2).
		hint = "Check network connectivity and registry reachability."
	default:
		hint = "Check the --registry value or the dependency's registry: URL."
	}
	return fmt.Sprintf("Could not reach MCP registry at %s while validating server '%s'. %s", e.URL, e.Server, hint)
}

// IsFailure reports whether err is a registry failure that ends an install:
// a refused http:// URL or a registry that did not answer.
func IsFailure(err error) bool {
	var insecure *InsecureURLError
	var unreachable *UnreachableError
	return errors.As(err, &insecure) || errors.As(err, &unreachable)
}

// NewClientFor is NewClient for a URL taken from source: an http:// URL from
// MCP_REGISTRY_URL or a registry: field needs MCP_REGISTRY_ALLOW_HTTP set to
// any non-empty value, as the Oracle reads it with os.environ.get. --registry
// is the user's own choice and needs none (commands/install.py:853).
func NewClientFor(baseURL string, source Source) (*Client, error) {
	c, err := NewClient(baseURL)
	if err != nil {
		return nil, err
	}
	c.Source = source
	if (source == SourceEnv || source == SourceExplicit) && strings.HasPrefix(c.BaseURL, "http://") && os.Getenv("MCP_REGISTRY_ALLOW_HTTP") == "" {
		return nil, &InsecureURLError{URL: c.BaseURL, Source: source}
	}
	return c, nil
}

// NormalizeBaseURL trims whitespace and a trailing slash the same way
// NewClient does internally. Callers that need to persist or compare a
// registry URL (e.g. apm.yml's registry: field) must apply this first, or
// "https://reg" and "https://reg/" -- the same registry to NewClient --
// would compare as different values and force a spurious --force conflict
// (found by codex review).
func NormalizeBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

// NewClient validates baseURL (http/https scheme, length cap) and returns a
// Client with a default HTTP timeout. An empty baseURL resolves to
// DefaultBaseURL.
func NewClient(baseURL string) (*Client, error) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseURL = NormalizeBaseURL(baseURL)
	// A coarse, parse-independent credential check FIRST, before any error
	// message below could echo the raw string back: url.Parse can still
	// "succeed enough" to reach an error branch (e.g. an empty Host) on a
	// malformed credentialed URL like "https://user:pass@", and that branch
	// used to echo baseURL verbatim -- leaking the credential even though
	// the later, parse-based check never got a chance to run (found by
	// codex review). None of this package's valid inputs need a literal
	// "@" (rejected outright, not just redacted), so this is safe to reject
	// unconditionally rather than try to cleverly redact.
	if strings.Contains(baseURL, "@") {
		return nil, fmt.Errorf("refusing registry URL with embedded credentials")
	}
	if len(baseURL) > maxBaseURLLength {
		return nil, fmt.Errorf("registry URL is too long (%d > %d characters)", len(baseURL), maxBaseURLLength)
	}
	// Error messages below deliberately never echo baseURL: it has passed
	// the credential check above, but a query string/fragment (rejected
	// next) could still carry a secret, and echoing it back is not worth
	// the risk for a diagnostic message.
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid registry URL: expected scheme://host")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		// Never echo u.Scheme: it's a parsed field, not free text, but
		// nothing stops a caller from putting something sensitive-looking
		// in the scheme position (e.g. MCP_REGISTRY_URL="t-secret://..."),
		// and the surrounding policy in this function is to never echo any
		// part of the input URL (found by codex review).
		return nil, fmt.Errorf("invalid registry URL: scheme is not supported; use http:// or https://")
	}
	// Reject a query string or fragment on the base URL: request URLs are
	// built by string concatenation (searchServers/getServer append
	// "/v0.1/servers?search=..." etc. directly onto BaseURL), so an
	// existing "?token=..." would both produce a malformed request AND leak
	// the token through error messages and, for --registry, persistence
	// into apm.yml (found by codex review).
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("registry URL must not contain a query string or fragment")
	}
	return &Client{
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		Source:  SourceDefault,
	}, nil
}

// ServerInfo is the subset of an MCP Registry v0.1 server record this
// package resolves: identity plus remote (network-reachable) endpoints.
type ServerInfo struct {
	ID      string
	Name    string
	Remotes []Remote
	// HasPackages records whether the registry response also carried a
	// non-empty packages[] array, so callers can distinguish "server has no
	// deployable endpoint at all" from "server only offers a package-based
	// (stdio) install, which this client does not resolve".
	HasPackages bool
}

// Remote is a network-reachable MCP server endpoint.
type Remote struct {
	TransportType string // "http", "sse", or "streamable-http" ("" normalizes to "http")
	URL           string
	// RequiredHeaders names headers the registry says the server needs (e.g.
	// "Authorization" for github-mcp-server). The registry's header entries
	// are requirement descriptors ({name, description, isSecret}), never
	// literal values -- there is nothing here to copy into a deployed
	// config. apm-go does not auto-resolve auth (no token injection, unlike
	// the Python original); callers should surface this list as a
	// diagnostic so the user knows to add --header manually if needed.
	RequiredHeaders []string
}

type rawRemoteHeader struct {
	Name string `json:"name"`
}

type rawRemote struct {
	TransportType string            `json:"type"`
	URL           string            `json:"url"`
	Headers       []rawRemoteHeader `json:"headers"`
}

type rawServer struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Remotes  []rawRemote       `json:"remotes"`
	Packages []json.RawMessage `json:"packages"`
}

type serverListEntry struct {
	Server *rawServer `json:"server"`
}

type serverListResponse struct {
	Servers []serverListEntry `json:"servers"`
}

type serverGetResponse struct {
	Server *rawServer `json:"server"`
}

// FindServerByReference resolves reference (a bare or namespaced server
// name, e.g. "io.github.github/github-mcp-server") against the registry:
// search for candidates, prefer an exact name match, else fall back to a
// namespace-boundary fuzzy match, then fetch the full server record for the
// matched name at the given version ("" means "latest").
//
// Returns (nil, nil) when no candidate matches -- callers should report
// this as "not found in registry", not treat it as a request failure.
func (c *Client) FindServerByReference(ctx context.Context, reference, version string) (*ServerInfo, error) {
	list, err := c.searchServers(ctx, reference)
	var status *statusError
	if errors.Is(err, errNetwork) || errors.As(err, &status) {
		return nil, c.unreachable(reference)
	}
	if err != nil {
		return nil, err
	}

	matched := ""
	for _, entry := range list {
		if entry.Server != nil && entry.Server.Name == reference {
			matched = entry.Server.Name
			break
		}
	}
	if matched == "" {
		for _, entry := range list {
			if entry.Server != nil && isServerMatch(reference, entry.Server.Name) {
				matched = entry.Server.Name
				break
			}
		}
	}
	if matched == "" {
		return nil, nil
	}

	info, err := c.getServer(ctx, matched, version)
	if errors.Is(err, errNetwork) || (errors.As(err, &status) && status.code != http.StatusNotFound) {
		return nil, c.unreachable(reference)
	}
	return info, err
}

func (c *Client) unreachable(reference string) error {
	return &UnreachableError{URL: c.BaseURL, Source: c.Source, Server: reference}
}

// isServerMatch reports whether name is a namespace-boundary fuzzy match for
// an already-qualified reference, e.g. "github/github-mcp-server" matches
// "io.github.github/github-mcp-server" (name ends with "."+reference), but
// "microsoftdocs/mcp" must not match "com.supabase/mcp". An unqualified
// reference (no "/") falls back to comparing the last path segment of name.
func isServerMatch(reference, name string) bool {
	if strings.Contains(reference, "/") {
		return strings.HasSuffix(name, "."+reference)
	}
	parts := strings.Split(name, "/")
	return parts[len(parts)-1] == reference
}

func (c *Client) searchServers(ctx context.Context, reference string) ([]serverListEntry, error) {
	u := fmt.Sprintf("%s%s/servers?search=%s", c.BaseURL, v0_1Prefix, url.QueryEscape(reference))
	var resp serverListResponse
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	for _, entry := range resp.Servers {
		if entry.Server == nil {
			return nil, fmt.Errorf("registry returned a non-spec list entry (missing 'server' key); expected MCP Registry v0.1 response shape")
		}
	}
	return resp.Servers, nil
}

func (c *Client) getServer(ctx context.Context, name, version string) (*ServerInfo, error) {
	if version == "" {
		version = "latest"
	}
	u := fmt.Sprintf("%s%s/servers/%s/versions/%s", c.BaseURL, v0_1Prefix, url.PathEscape(name), url.PathEscape(version))
	var resp serverGetResponse
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	if resp.Server == nil {
		return nil, fmt.Errorf("registry returned a non-spec response for %q (missing 'server' key); expected MCP Registry v0.1 response shape", name)
	}

	info := &ServerInfo{
		ID:          resp.Server.ID,
		Name:        resp.Server.Name,
		HasPackages: len(resp.Server.Packages) > 0,
	}
	for _, r := range resp.Server.Remotes {
		if r.URL == "" {
			continue
		}
		remote := Remote{TransportType: r.TransportType, URL: r.URL}
		for _, h := range r.Headers {
			if h.Name != "" {
				remote.RequiredHeaders = append(remote.RequiredHeaders, h.Name)
			}
		}
		info.Remotes = append(info.Remotes, remote)
	}
	return info, nil
}

func (c *Client) getJSON(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("build registry request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		// Never echo the request URL, c.BaseURL, or the underlying
		// transport error's own message: Go's http.Client wraps a failed
		// request in a *url.Error whose Error() method embeds the full
		// request URL, which could still carry a path-embedded token even
		// after NewClient's query-string/userinfo rejection (found by
		// codex review -- query/userinfo are gone, but a token embedded in
		// the URL PATH itself, e.g. "--registry
		// https://reg.example/t-secret123/", was still reachable through
		// %w here).
		return errNetwork
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &statusError{code: resp.StatusCode}
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, mcpRegistryMaxBytes+1))
	if err != nil {
		return fmt.Errorf("read registry response: %w", err)
	}
	if int64(len(data)) > mcpRegistryMaxBytes {
		return fmt.Errorf("registry response exceeds %d byte limit", mcpRegistryMaxBytes)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode registry response: %w", err)
	}
	return nil
}

var errNetwork = errors.New("could not reach the MCP registry (network error)")

// statusError is a registry response that is not 2xx, as requests'
// raise_for_status refuses it.
type statusError struct{ code int }

func (e *statusError) Error() string {
	if e.code == http.StatusNotFound {
		return "not found in registry"
	}
	return fmt.Sprintf("registry returned HTTP %d", e.code)
}
