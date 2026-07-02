package mcpregistry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClient_DefaultsAndValidation(t *testing.T) {
	c, err := NewClient("")
	if err != nil {
		t.Fatalf("NewClient(\"\"): %v", err)
	}
	if c.BaseURL != DefaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, DefaultBaseURL)
	}

	if _, err := NewClient("ftp://example.com"); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("expected scheme rejection, got %v", err)
	}
	if _, err := NewClient("not-a-url"); err == nil {
		t.Error("expected error for missing scheme/host")
	}
	if _, err := NewClient("https://user:token@example.com"); err == nil || !strings.Contains(err.Error(), "embedded credentials") {
		t.Errorf("expected embedded-credentials rejection, got %v", err)
	}

	// Request URLs are built by string-concatenating BaseURL with
	// "/v0.1/servers?search=..." -- an existing query string would both
	// malform the request and leak a query-embedded token (e.g.
	// "?token=x") through error messages and apm.yml persistence.
	if _, err := NewClient("https://reg.example.com/api?token=secret"); err == nil || !strings.Contains(err.Error(), "query string") {
		t.Errorf("expected query-string rejection, got %v", err)
	}
	if _, err := NewClient("https://reg.example.com/api#frag"); err == nil || !strings.Contains(err.Error(), "fragment") {
		t.Errorf("expected fragment rejection, got %v", err)
	}

	if _, err := NewClient("https://" + strings.Repeat("a", maxBaseURLLength)); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("expected length-cap rejection, got %v", err)
	}

	// The malformed-credentialed case: "https://user:pass@" has an empty
	// Host after parsing, so it used to fall into the "invalid registry
	// URL: expected scheme://host" branch, which echoed the raw string
	// (including the credential) before the later u.User check ever ran.
	// The coarse "@"-contains check must catch this BEFORE any error
	// message is built, and the message itself must never contain the
	// credential.
	if _, err := NewClient("https://user:pass@"); err == nil {
		t.Error("expected embedded-credentials rejection for a malformed credentialed URL")
	} else if strings.Contains(err.Error(), "user:pass") {
		t.Errorf("error message leaked the credential: %v", err)
	}
}

func TestNewClient_NeverEchoesRawURLOnError(t *testing.T) {
	// None of NewClient's error paths may echo the raw input back, since a
	// malformed-but-credentialed or malformed-but-tokened URL could reach
	// them before its secret is provably safe to display.
	for _, bad := range []string{
		"not-a-url",
		"ftp://example.com",
		"https://user:pass@",
	} {
		_, err := NewClient(bad)
		if err == nil {
			t.Fatalf("NewClient(%q): expected an error", bad)
		}
		if strings.Contains(err.Error(), bad) {
			t.Errorf("NewClient(%q) error echoed the raw input: %v", bad, err)
		}
	}
}

// TestNewClient_NeverEchoesScheme is a regression test (tenth codex review
// round): the unsupported-scheme error interpolated the PARSED u.Scheme
// value directly (e.g. NewClient("t-secret://registry") -> error containing
// `scheme "t-secret"`) -- a narrower leak than a full raw-URL echo (which
// TestNewClient_NeverEchoesRawURLOnError already covers), since nothing
// stops a caller from putting something sensitive-looking in the scheme
// position of MCP_REGISTRY_URL.
func TestNewClient_NeverEchoesScheme(t *testing.T) {
	_, err := NewClient("t-secret://registry.example.com")
	if err == nil {
		t.Fatal("expected a scheme-rejection error")
	}
	if strings.Contains(err.Error(), "t-secret") {
		t.Errorf("error message leaked the scheme value: %v", err)
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := map[string]string{
		"https://reg.example.com":      "https://reg.example.com",
		"https://reg.example.com/":     "https://reg.example.com",
		"  https://reg.example.com/  ": "https://reg.example.com",
	}
	for in, want := range cases {
		if got := NormalizeBaseURL(in); got != want {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", in, got, want)
		}
	}

	c2, err := NewClient("https://example.com/registry/")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c2.BaseURL != "https://example.com/registry" {
		t.Errorf("trailing slash not trimmed: %q", c2.BaseURL)
	}
}

// mockRegistry serves /v0.1/servers?search= and /v0.1/servers/{name}/versions/{version}
// from an in-memory table keyed by exact server name.
func mockRegistry(t *testing.T, servers map[string]rawServer) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v0.1/servers", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("search")
		var entries []serverListEntry
		for name, s := range servers {
			if strings.Contains(strings.ToLower(name), strings.ToLower(q)) {
				sCopy := s
				entries = append(entries, serverListEntry{Server: &sCopy})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(serverListResponse{Servers: entries})
	})
	mux.HandleFunc("/v0.1/servers/", func(w http.ResponseWriter, r *http.Request) {
		// path: /v0.1/servers/{name}/versions/{version}
		rest := strings.TrimPrefix(r.URL.Path, "/v0.1/servers/")
		parts := strings.SplitN(rest, "/versions/", 2)
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		name := parts[0]
		s, ok := servers[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(serverGetResponse{Server: &s})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestFindServerByReference_ExactMatch(t *testing.T) {
	srv := mockRegistry(t, map[string]rawServer{
		"io.github.github/github-mcp-server": {
			ID:   "abc123",
			Name: "io.github.github/github-mcp-server",
			Remotes: []rawRemote{
				{TransportType: "http", URL: "https://api.githubcopilot.com/mcp/"},
			},
		},
	})
	c, err := NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	info, err := c.FindServerByReference(context.Background(), "io.github.github/github-mcp-server", "")
	if err != nil {
		t.Fatalf("FindServerByReference: %v", err)
	}
	if info == nil {
		t.Fatal("expected a match, got nil")
	}
	if info.Name != "io.github.github/github-mcp-server" {
		t.Errorf("Name = %q", info.Name)
	}
	if len(info.Remotes) != 1 || info.Remotes[0].URL != "https://api.githubcopilot.com/mcp/" {
		t.Errorf("Remotes = %+v", info.Remotes)
	}
}

func TestFindServerByReference_FuzzyNamespaceMatch(t *testing.T) {
	srv := mockRegistry(t, map[string]rawServer{
		"io.github.github/github-mcp-server": {
			Name:    "io.github.github/github-mcp-server",
			Remotes: []rawRemote{{TransportType: "http", URL: "https://api.githubcopilot.com/mcp/"}},
		},
	})
	c, err := NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	info, err := c.FindServerByReference(context.Background(), "github/github-mcp-server", "")
	if err != nil {
		t.Fatalf("FindServerByReference: %v", err)
	}
	if info == nil || info.Name != "io.github.github/github-mcp-server" {
		t.Fatalf("expected fuzzy match to io.github.github/github-mcp-server, got %+v", info)
	}
}

func TestFindServerByReference_FuzzyMatchDoesNotCrossNamespace(t *testing.T) {
	srv := mockRegistry(t, map[string]rawServer{
		"com.supabase/mcp": {
			Name:    "com.supabase/mcp",
			Remotes: []rawRemote{{TransportType: "http", URL: "https://supabase.example/mcp"}},
		},
	})
	c, err := NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	info, err := c.FindServerByReference(context.Background(), "microsoftdocs/mcp", "")
	if err != nil {
		t.Fatalf("FindServerByReference: %v", err)
	}
	if info != nil {
		t.Errorf("expected no match (must not cross namespace boundary), got %+v", info)
	}
}

func TestFindServerByReference_NoMatch(t *testing.T) {
	srv := mockRegistry(t, map[string]rawServer{})
	c, err := NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	info, err := c.FindServerByReference(context.Background(), "nonexistent/server", "")
	if err != nil {
		t.Fatalf("expected nil error for no-match, got %v", err)
	}
	if info != nil {
		t.Errorf("expected nil ServerInfo, got %+v", info)
	}
}

func TestFindServerByReference_PackagesOnly_HasPackagesTrue(t *testing.T) {
	srv := mockRegistry(t, map[string]rawServer{
		"npm-only/server": {
			Name:     "npm-only/server",
			Packages: []json.RawMessage{json.RawMessage(`{"registry_name":"npm","name":"pkg"}`)},
		},
	})
	c, err := NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	info, err := c.FindServerByReference(context.Background(), "npm-only/server", "")
	if err != nil {
		t.Fatalf("FindServerByReference: %v", err)
	}
	if info == nil {
		t.Fatal("expected a ServerInfo (client itself does not reject package-only servers)")
	}
	if !info.HasPackages {
		t.Error("expected HasPackages = true")
	}
	if len(info.Remotes) != 0 {
		t.Errorf("expected no remotes, got %+v", info.Remotes)
	}
}

func TestFindServerByReference_NonSpecListShape_Errors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v0.1/servers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Flat shape (no nested "server" key) -- not v0.1 spec.
		w.Write([]byte(`{"servers": [{"name": "flat-server"}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.FindServerByReference(context.Background(), "flat-server", "")
	if err == nil || !strings.Contains(err.Error(), "non-spec") {
		t.Errorf("expected non-spec-shape error, got %v", err)
	}
}

// TestFindServerByReference_RealRegistryResponseShape decodes an actual
// response body captured from the live api.mcp.github.com registry (verified
// via `curl` during implementation) -- a regression guard for the JSON tag
// mismatch this caught: the registry uses "type" for a remote's transport,
// not "transport_type" (the Python original's internal field name, which an
// earlier draft of this client copied without live verification). Also
// locks in that header entries are requirement descriptors ({name,
// description, isSecret}) with no "value" key, not literal key/value pairs.
func TestFindServerByReference_RealRegistryResponseShape(t *testing.T) {
	const captured = `{
	  "_meta": {"io.modelcontextprotocol.registry/official": {"isLatest": true, "status": "active"}},
	  "server": {
	    "name": "io.github.github/github-mcp-server",
	    "version": "1.5.0",
	    "remotes": [
	      {
	        "type": "streamable-http",
	        "url": "https://api.githubcopilot.com/mcp/",
	        "headers": [
	          {"name": "Authorization", "description": "Authorization header with authentication token (PAT or App token)", "isSecret": true}
	        ]
	      }
	    ],
	    "packages": [{"registryType": "npm", "identifier": "@github/github-mcp-server"}]
	  }
	}`

	mux := http.NewServeMux()
	mux.HandleFunc("/v0.1/servers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var entry map[string]any
		json.Unmarshal([]byte(captured), &entry)
		json.NewEncoder(w).Encode(map[string]any{"servers": []map[string]any{{"server": entry["server"]}}})
	})
	mux.HandleFunc("/v0.1/servers/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(captured))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	info, err := c.FindServerByReference(context.Background(), "io.github.github/github-mcp-server", "")
	if err != nil {
		t.Fatalf("FindServerByReference: %v", err)
	}
	if info == nil {
		t.Fatal("expected a match")
	}
	if len(info.Remotes) != 1 {
		t.Fatalf("expected 1 remote, got %d: %+v", len(info.Remotes), info.Remotes)
	}
	r := info.Remotes[0]
	if r.TransportType != "streamable-http" {
		t.Errorf("TransportType = %q, want %q (JSON tag must be \"type\", not \"transport_type\")", r.TransportType, "streamable-http")
	}
	if r.URL != "https://api.githubcopilot.com/mcp/" {
		t.Errorf("URL = %q", r.URL)
	}
	if len(r.RequiredHeaders) != 1 || r.RequiredHeaders[0] != "Authorization" {
		t.Errorf("RequiredHeaders = %v, want [Authorization]", r.RequiredHeaders)
	}
	if !info.HasPackages {
		t.Error("expected HasPackages = true (this response also carries a packages[] entry)")
	}
}

func TestFindServerByReference_RegistryUnreachable(t *testing.T) {
	c, err := NewClient("https://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.FindServerByReference(context.Background(), "anything", "")
	if err == nil {
		t.Fatal("expected an error for an unreachable registry")
	}
}

// TestGetJSON_NetworkAndHTTPErrors_NeverEchoRequestURL is a regression test
// (ninth codex review round): Go's http.Client wraps a failed request in a
// *url.Error whose own Error() method embeds the full request URL -- a %w
// wrap of that error, or an explicit c.BaseURL interpolation, would leak a
// path-embedded token even after NewClient's query-string/userinfo
// rejection (which only covers the base URL's query and userinfo, not an
// unusual but valid auth-via-path pattern like ".../t-<token>/").
func TestGetJSON_NetworkAndHTTPErrors_NeverEchoRequestURL(t *testing.T) {
	pathToken := "t-should-not-leak-in-error"

	// (a) unreachable registry -- transport-level failure.
	unreachable, err := NewClient("https://127.0.0.1:1/" + pathToken)
	if err != nil {
		t.Fatal(err)
	}
	_, err = unreachable.FindServerByReference(context.Background(), "anything", "")
	if err == nil {
		t.Fatal("expected an error for an unreachable registry")
	}
	if strings.Contains(err.Error(), pathToken) {
		t.Errorf("unreachable-registry error leaked the path token: %v", err)
	}

	// (b) reachable registry, but every request 404s -- HTTP-level failure.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	notFound, err := NewClient(srv.URL + "/" + pathToken)
	if err != nil {
		t.Fatal(err)
	}
	_, err = notFound.FindServerByReference(context.Background(), "anything", "")
	if err == nil {
		t.Fatal("expected an error for a 404 registry")
	}
	if strings.Contains(err.Error(), pathToken) {
		t.Errorf("404 registry error leaked the path token: %v", err)
	}
}
