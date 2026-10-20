package mcpregistry

import (
	"context"
	"errors"
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

// TestNewClientFor_PlainHTTPAnyCase follows Oracle 0.33.0 (issue #123 run
// p22-oracle): urlparse lowercases the scheme, so HTTP:// and Http:// are
// refused too, and the message names the URL lowercased.
func TestNewClientFor_PlainHTTPAnyCase(t *testing.T) {
	t.Setenv("MCP_REGISTRY_ALLOW_HTTP", "")
	for _, c := range []struct {
		url    string
		source Source
		hint   string
	}{
		{"HTTP://127.0.0.1:34385", SourceEnv, "Check MCP_REGISTRY_URL if set."},
		{"Http://127.0.0.1:34385/", SourceEnv, "Check MCP_REGISTRY_URL if set."},
		{"HTTP://127.0.0.1:34385/", SourceExplicit, "Check the --registry value or the dependency's registry: URL."},
		{"Http://127.0.0.1:34385", SourceExplicit, "Check the --registry value or the dependency's registry: URL."},
	} {
		_, err := NewClientFor(c.url, c.source)
		want := "Insecure MCP registry URL 'http://127.0.0.1:34385': http:// is not allowed by default. Set MCP_REGISTRY_ALLOW_HTTP=1 to opt in to plaintext HTTP (not recommended for production). " + c.hint
		if err == nil || err.Error() != want {
			t.Errorf("NewClientFor(%q, %s) = %v, want %q", c.url, c.source, err, want)
		}
	}
}

// TestFindServerByReference_Answers covers the closed list of what one
// lookup gives, each measured against Oracle 0.33.0 (issue #123 run
// p22-oracle; registry/client.py:37-83,534-551,647-660).
func TestFindServerByReference_Answers(t *testing.T) {
	const server = `{"name":"io.example/demo","remotes":[{"type":"streamable-http","url":"https://example.com/mcp"}]}`
	truncated := func(w http.ResponseWriter) {
		w.Header().Set("Content-Length", "2000")
		w.Write([]byte(`{"servers":`))
	}
	write := func(body string) func(w http.ResponseWriter) {
		return func(w http.ResponseWriter) { w.Write([]byte(body)) }
	}
	const (
		unreachable = "unreachable"
		format      = "format"
		notFound    = "not found"
	)
	cases := []struct {
		name        string
		search, get func(w http.ResponseWriter)
		want        string
	}{
		{"search truncated", truncated, nil, unreachable},
		{"search not JSON", write(`{"bad`), nil, unreachable},
		{"search not UTF-8", write("{\"x\":\"\xff\"}"), nil, unreachable},
		{"get truncated", nil, truncated, unreachable},
		{"get not JSON", nil, write(`{"bad`), unreachable},
		{"get not UTF-8", nil, write("{\"x\":\"\xff\"}"), unreachable},
		{"search entry without server", write(`{"servers":[{"name":"io.example/demo"}]}`), nil, format},
		{"search no match", write(`{"servers":[]}`), nil, notFound},
		{"get 404", nil, func(w http.ResponseWriter) { w.WriteHeader(http.StatusNotFound) }, notFound},
		{"get empty body", nil, write(""), notFound},
		{"get {}", nil, write(`{}`), notFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.RawQuery != "" {
					if c.search != nil {
						c.search(w)
						return
					}
					w.Write([]byte(`{"servers":[{"server":` + server + `}]}`))
					return
				}
				if c.get != nil {
					c.get(w)
					return
				}
				w.Write([]byte(`{"server":` + server + `}`))
			}))
			t.Cleanup(srv.Close)
			client, err := NewClientFor(srv.URL, SourceFlag)
			if err != nil {
				t.Fatal(err)
			}
			info, err := client.FindServerByReference(context.Background(), "io.example/demo", "")
			var unreach *UnreachableError
			var fmtErr *FormatError
			switch c.want {
			case unreachable:
				if !errors.As(err, &unreach) {
					t.Fatalf("err = %v, want an UnreachableError", err)
				}
			case format:
				const msg = "Registry returned a non-spec list entry (missing 'server' key); expected MCP Registry v0.1 response shape."
				if !errors.As(err, &fmtErr) || err.Error() != msg || !IsFailure(err) {
					t.Fatalf("err = %v, want %q", err, msg)
				}
			case notFound:
				if IsFailure(err) || info != nil {
					t.Fatalf("info = %v, err = %v, want a server not found", info, err)
				}
			}
		})
	}
}
