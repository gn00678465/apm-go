package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// The lock Oracle 0.33.0 writes when the project declares a self-defined
// dup and its dependency a registry dup (issue #123 run s22-a, dep).
const shadowOracleLock = `lockfile_version: '1'
apm_version: dev
dependencies:
- repo_url: _local/pkg
  name: pkg
  version: 1.0.0
  package_type: apm_package
  source: local
  local_path: ./pkg
deployments:
- kind: uri
  target: mcp
  value: dup
  runtime: claude
  scope: project
  owners:
  - .
  active_owner: .
  content_hash: null
mcp_servers:
- dup
mcp_configs:
  dup:
    name: dup
    transport: stdio
    registry: false
    command: echo
mcp_target_servers:
  claude:
  - dup
`

// TestInstall_ShadowedRegistryServerIsNotLookedUp follows Oracle 0.33.0
// (issue #123 run s22-a; integration/mcp_config_view.py:84-88,131-133): a
// registry server whose name an earlier declaration has is never looked up,
// so a registry that fails, or an http:// URL with no opt-in, does not stop
// the install.
func TestInstall_ShadowedRegistryServerIsNotLookedUp(t *testing.T) {
	const self = "  - name: dup\n    registry: false\n    transport: stdio\n    command: echo\n"
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	reg := "  - name: dup\n    registry: " + srv.URL + "\n"
	const head = "name: p\nversion: 1.0.0\ntargets: [claude]\n"
	cases := []struct {
		name  string
		files map[string]string
		allow string
	}{
		{"dependency", map[string]string{"apm.yml": head + "dependencies:\n  apm: [./pkg]\n  mcp:\n" + self, "pkg/apm.yml": "name: pkg\nversion: 1.0.0\ndependencies:\n  mcp:\n" + reg}, "1"},
		{"devDependencies", map[string]string{"apm.yml": head + "dependencies:\n  mcp:\n" + self + "devDependencies:\n  mcp:\n" + reg}, "1"},
		{"two dependencies", map[string]string{"apm.yml": head + "dependencies:\n  apm: [./pkg1, ./pkg2]\n", "pkg1/apm.yml": "name: pkg1\nversion: 1.0.0\ndependencies:\n  mcp:\n" + self, "pkg2/apm.yml": "name: pkg2\nversion: 1.0.0\ndependencies:\n  mcp:\n" + reg}, "1"},
		{"http without opt-in", map[string]string{"apm.yml": head + "dependencies:\n  apm: [./pkg]\n  mcp:\n" + self, "pkg/apm.yml": "name: pkg\nversion: 1.0.0\ndependencies:\n  mcp:\n" + reg}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := chdirTemp(t)
			t.Setenv("HOME", t.TempDir())
			t.Setenv("MCP_REGISTRY_ALLOW_HTTP", c.allow)
			for rel, content := range c.files {
				writeCompileFile(t, dir, rel, content)
			}
			if err := os.MkdirAll(".claude", 0o755); err != nil {
				t.Fatal(err)
			}
			hits.Store(0)
			mustRunCLI(t, "install")
			if hits.Load() != 0 {
				t.Errorf("registry got %d requests, want 0", hits.Load())
			}
			var mcp struct {
				Servers map[string]map[string]any `json:"mcpServers"`
			}
			if err := json.Unmarshal([]byte(readText(t, ".mcp.json")), &mcp); err != nil || mcp.Servers["dup"]["command"] != "echo" {
				t.Fatalf(".mcp.json = %s (%v), want the self-defined dup", readText(t, ".mcp.json"), err)
			}
			if c.name == "dependency" {
				if got := readText(t, "apm.lock.yaml"); got != shadowOracleLock {
					t.Errorf("apm.lock.yaml:\n%s\nwant:\n%s", got, shadowOracleLock)
				}
			}
		})
	}
}

// TestFrozenInstall_RegistryFailsInTheDeployInPlace: a frozen install that
// restores a missing file deploys twice, in a trial and in place; a registry
// that fails only for the second ends it with the trial's error (issue #123,
// review of step 20, frozen-probe.py).
func TestFrozenInstall_RegistryFailsInTheDeployInPlace(t *testing.T) {
	var hits atomic.Int32
	var flaky atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) > 2 && flaky.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		server := `{"name":"io.example/demo","remotes":[{"type":"streamable-http","url":"https://example.com/mcp"}]}`
		if r.URL.RawQuery != "" {
			w.Write([]byte(`{"servers":[{"server":` + server + `}]}`))
			return
		}
		w.Write([]byte(`{"server":` + server + `}`))
	}))
	t.Cleanup(srv.Close)
	dir := chdirTemp(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MCP_REGISTRY_URL", srv.URL)
	t.Setenv("MCP_REGISTRY_ALLOW_HTTP", "1")
	writeCompileFile(t, dir, "apm.yml", "name: p\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  mcp:\n  - name: io.example/demo\n")
	writeCompileFile(t, dir, ".apm/agents/a.agent.md", "---\nname: a\n---\nhello\n")
	mustRunCLI(t, "install")
	lock := readText(t, "apm.lock.yaml")
	if err := os.Remove(".claude/agents/a.md"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(".mcp.json"); err != nil {
		t.Fatal(err)
	}

	hits.Store(0)
	flaky.Store(true)
	stdout, _, code := runCompileCLI(t, "install", "--frozen")
	want := " x frozen install: Could not reach MCP registry at " + srv.URL + " while validating server 'io.example/demo'. Check MCP_REGISTRY_URL."
	if code != 1 || !strings.Contains(stdout, want) {
		t.Fatalf("exit %d, stdout:\n%s", code, stdout)
	}
	if hits.Load() < 3 {
		t.Fatalf("registry got %d requests; the trial did not pass", hits.Load())
	}
	if _, err := os.Stat(".mcp.json"); !os.IsNotExist(err) {
		t.Errorf(".mcp.json written: %v", err)
	}
	if got := readText(t, "apm.lock.yaml"); got != lock {
		t.Errorf("frozen install changed the lock")
	}
}
