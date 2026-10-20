package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// TestInstall_PlainHTTPRegistryNeedsOptIn follows Oracle 0.33.0 (issue #123
// run h23_pkg; registry/client.py:325-338): with MCP_REGISTRY_URL on
// http:// and no MCP_REGISTRY_ALLOW_HTTP, install sends no request, deploys
// the package and records it, and ends with exit 1.
func TestInstall_PlainHTTPRegistryNeedsOptIn(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	t.Cleanup(srv.Close)
	dir := chdirTemp(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MCP_REGISTRY_URL", srv.URL+"/")
	t.Setenv("MCP_REGISTRY_ALLOW_HTTP", "")
	writeCompileFile(t, dir, "pkg/apm.yml", "name: pkg\nversion: 1.0.0\n")
	writeCompileFile(t, dir, "pkg/.apm/agents/a.agent.md", "---\nname: a\n---\nhello\n")
	writeCompileFile(t, dir, "apm.yml", "name: p\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - ./pkg\n  mcp:\n    - name: io.example/demo\n")
	if err := os.MkdirAll(".claude", 0o755); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runCompileCLI(t, "install")
	if code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, stdout)
	}
	want := " x Error installing dependencies: Insecure MCP registry URL '" + srv.URL + "': http:// is not allowed by default. Set MCP_REGISTRY_ALLOW_HTTP=1 to opt in to plaintext HTTP (not recommended for production). Check MCP_REGISTRY_URL if set.\n" +
		" i Run with --verbose for detailed diagnostics\n"
	if !strings.Contains(stdout, want) {
		t.Fatalf("stdout lacks the Oracle's lines:\n%s", stdout)
	}
	if hits.Load() != 0 {
		t.Errorf("registry got %d requests, want 0", hits.Load())
	}
	if _, err := os.Stat(".claude/agents/a.md"); err != nil {
		t.Errorf("package not deployed: %v", err)
	}
	lock := readText(t, "apm.lock.yaml")
	if !strings.Contains(lock, "repo_url: _local/pkg") || strings.Contains(lock, "mcp_servers") {
		t.Errorf("lock should record the package and no MCP server:\n%s", lock)
	}
	if _, err := os.Stat(".mcp.json"); !os.IsNotExist(err) {
		t.Errorf(".mcp.json written: %v", err)
	}
}
