package mcpregistry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNewClientFor_PlainHTTP follows registry/client.py:325-338: an http://
// URL from MCP_REGISTRY_URL or a registry: field is refused unless
// MCP_REGISTRY_ALLOW_HTTP is non-empty; --registry and https are not.
func TestNewClientFor_PlainHTTP(t *testing.T) {
	t.Setenv("MCP_REGISTRY_ALLOW_HTTP", "")
	cases := []struct {
		url    string
		source Source
		want   string
	}{
		{"http://127.0.0.1:8080/", SourceEnv, "Insecure MCP registry URL 'http://127.0.0.1:8080': http:// is not allowed by default. Set MCP_REGISTRY_ALLOW_HTTP=1 to opt in to plaintext HTTP (not recommended for production). Check MCP_REGISTRY_URL if set."},
		{"http://127.0.0.1:8080", SourceExplicit, "Insecure MCP registry URL 'http://127.0.0.1:8080': http:// is not allowed by default. Set MCP_REGISTRY_ALLOW_HTTP=1 to opt in to plaintext HTTP (not recommended for production). Check the --registry value or the dependency's registry: URL."},
		{"http://127.0.0.1:8080", SourceFlag, ""},
		{"https://reg.example", SourceEnv, ""},
	}
	for _, c := range cases {
		_, err := NewClientFor(c.url, c.source)
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != c.want {
			t.Errorf("NewClientFor(%q, %s) = %q, want %q", c.url, c.source, got, c.want)
		}
	}
	t.Setenv("MCP_REGISTRY_ALLOW_HTTP", "0")
	if _, err := NewClientFor("http://127.0.0.1:8080", SourceEnv); err != nil {
		t.Errorf("MCP_REGISTRY_ALLOW_HTTP=0 is non-empty and allows http: %v", err)
	}
}

// TestFindServerByReference_Classification follows registry/operations.py:
// 229-256 and registry/client.py:422,457,623-640: a search that is not 2xx,
// 404 included, and a server fetch that is neither 2xx nor 404 are an
// unreachable registry; a server fetch that is 404 is a server not found.
func TestFindServerByReference_Classification(t *testing.T) {
	cases := []struct {
		name                string
		searchCode, getCode int
		unreachable         bool
	}{
		{"search 503", 503, 200, true},
		{"search 404", 404, 200, true},
		{"get 500", 200, 500, true},
		{"get 404", 200, 404, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v0.1/servers" {
					w.WriteHeader(c.searchCode)
					w.Write([]byte(`{"servers":[{"server":{"name":"io.example/demo"}}]}`))
					return
				}
				w.WriteHeader(c.getCode)
				w.Write([]byte(`{}`))
			}))
			t.Cleanup(srv.Close)
			client, err := NewClientFor(srv.URL, SourceFlag)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.FindServerByReference(context.Background(), "io.example/demo", "")
			if IsFailure(err) != c.unreachable {
				t.Fatalf("err = %v, unreachable want %v", err, c.unreachable)
			}
		})
	}
}

// TestUnreachableError_DefaultRegistry is the sentence of issue #123
// comment 6284804045, item 2.
func TestUnreachableError_DefaultRegistry(t *testing.T) {
	err := &UnreachableError{URL: DefaultBaseURL, Source: SourceDefault, Server: "io.example/demo"}
	const want = "Could not reach MCP registry at https://api.mcp.github.com while validating server 'io.example/demo'. Check network connectivity and registry reachability."
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err.Error(), want)
	}
}
