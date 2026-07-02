package mcpregistry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestResolveDeployable_Success(t *testing.T) {
	srv := mockRegistry(t, map[string]rawServer{
		"io.github.github/github-mcp-server": {
			Name: "io.github.github/github-mcp-server",
			Remotes: []rawRemote{
				{TransportType: "streamable-http", URL: "https://api.githubcopilot.com/mcp/",
					Headers: []rawRemoteHeader{{Name: "Authorization"}}},
			},
		},
	})
	client, err := NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	dep, requiredHeaders, err := ResolveDeployable(context.Background(), client, "io.github.github/github-mcp-server", "", "")
	if err != nil {
		t.Fatalf("ResolveDeployable: %v", err)
	}
	if dep.Name != "io.github.github/github-mcp-server" || dep.URL != "https://api.githubcopilot.com/mcp/" || dep.Transport != "streamable-http" {
		t.Errorf("unexpected dep: %+v", dep)
	}
	if dep.Registry != false {
		t.Errorf("expected Registry=false on a resolved dep, got %v", dep.Registry)
	}
	if len(requiredHeaders) != 1 || requiredHeaders[0] != "Authorization" {
		t.Errorf("expected [Authorization], got %v", requiredHeaders)
	}
}

func TestResolveDeployable_TransportOverride(t *testing.T) {
	srv := mockRegistry(t, map[string]rawServer{
		"svc": {Name: "svc", Remotes: []rawRemote{{TransportType: "http", URL: "https://example.com/mcp"}}},
	})
	client, _ := NewClient(srv.URL)

	dep, _, err := ResolveDeployable(context.Background(), client, "svc", "", "sse")
	if err != nil {
		t.Fatalf("ResolveDeployable: %v", err)
	}
	if dep.Transport != "sse" {
		t.Errorf("expected transport override to apply, got %q", dep.Transport)
	}

	if _, _, err := ResolveDeployable(context.Background(), client, "svc", "", "stdio"); err == nil || !strings.Contains(err.Error(), "not valid") {
		t.Errorf("expected rejection of a non-remote transport override, got %v", err)
	}
}

func TestResolveDeployable_NotFound(t *testing.T) {
	srv := mockRegistry(t, map[string]rawServer{"other": {Name: "other"}})
	client, _ := NewClient(srv.URL)

	if _, _, err := ResolveDeployable(context.Background(), client, "missing", "", ""); err == nil || !strings.Contains(err.Error(), "not found in registry") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestResolveDeployable_PackagesOnly_ReportsStdioGap(t *testing.T) {
	srv := mockRegistry(t, map[string]rawServer{
		"pkg-only": {Name: "pkg-only", Packages: []json.RawMessage{json.RawMessage(`{"registry_type":"npm"}`)}},
	})
	client, _ := NewClient(srv.URL)

	if _, _, err := ResolveDeployable(context.Background(), client, "pkg-only", "", ""); err == nil || !strings.Contains(err.Error(), "package-based (stdio)") {
		t.Errorf("expected package-based-only error, got %v", err)
	}
}

func TestResolveDeployable_RejectsCredentialedRemoteURL(t *testing.T) {
	srv := mockRegistry(t, map[string]rawServer{
		"evil": {Name: "evil", Remotes: []rawRemote{{TransportType: "http", URL: "https://user:pass@evil.example.com/mcp"}}},
	})
	client, _ := NewClient(srv.URL)

	if _, _, err := ResolveDeployable(context.Background(), client, "evil", "", ""); err == nil || !strings.Contains(err.Error(), "invalid entry") {
		t.Errorf("expected the credentialed remote URL to be rejected, got %v", err)
	}
}

func TestIsRemoteTransport(t *testing.T) {
	for _, tt := range []struct {
		transport string
		want      bool
	}{
		{"http", true}, {"sse", true}, {"streamable-http", true},
		{"stdio", false}, {"", false}, {"websocket", false},
	} {
		if got := IsRemoteTransport(tt.transport); got != tt.want {
			t.Errorf("IsRemoteTransport(%q) = %v, want %v", tt.transport, got, tt.want)
		}
	}
}
