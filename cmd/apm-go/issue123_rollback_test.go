package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// TestInstallPkg_RegistryFailureRestoresManifest follows Oracle 0.33.0
// (issue #123 run rv_addpkg503; install/transaction.py:61-70): install
// <pkg> over an existing apm.yml deploys and records the package, then the
// registry fails; apm.yml gets its bytes back, the package directory the run
// fetched is removed, and the restore line comes before the last line.
func TestInstallPkg_RegistryFailureRestoresManifest(t *testing.T) {
	var down atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
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
	const manifest = "name: p\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  mcp:\n    - name: io.example/demo\n"
	writeCompileFile(t, dir, "apm.yml", manifest)
	writeCompileFile(t, dir, "pkg/apm.yml", "name: pkg\nversion: 1.0.0\n")
	writeCompileFile(t, dir, "pkg/.apm/agents/a.agent.md", "---\nname: a\n---\nhello\n")
	if err := os.MkdirAll(".claude", 0o755); err != nil {
		t.Fatal(err)
	}
	mustRunCLI(t, "install")

	down.Store(true)
	stdout, _, code := runCompileCLI(t, "install", "./pkg")
	if code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, stdout)
	}
	want := " i Run with --verbose for detailed diagnostics\n i apm.yml restored to its previous state.\n x Install failed after "
	if !strings.Contains(stdout, want) {
		t.Fatalf("stdout lacks the Oracle's last lines:\n%s", stdout)
	}
	if got := readText(t, "apm.yml"); got != manifest {
		t.Errorf("apm.yml = %q, want the bytes before the run", got)
	}
	if !strings.Contains(readText(t, "apm.lock.yaml"), "- repo_url: _local/pkg\n") {
		t.Errorf("lock does not record ./pkg:\n%s", readText(t, "apm.lock.yaml"))
	}
	if _, err := os.Stat("apm_modules/_local/pkg"); !os.IsNotExist(err) {
		t.Errorf("apm_modules/_local/pkg stays: %v", err)
	}

	// An install that leaves apm.yml as it was prints no restore line.
	stdout, _, code = runCompileCLI(t, "install")
	if code != 1 || strings.Contains(stdout, "restored") {
		t.Fatalf("exit %d, stdout:\n%s", code, stdout)
	}
}
