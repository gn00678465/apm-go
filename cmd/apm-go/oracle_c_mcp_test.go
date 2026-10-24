package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The expected values below are what Oracle 0.33.0 printed and wrote for the
// same project (runs under ~/.cache/apm-go-oracle-ref/C/<item>/after-b/or),
// with apm-go's status symbols and the deviations PRODUCT.md records.

// issue #123 item c13-2: a name apm.yml already has, with no --force and no
// terminal to ask on, is click's usage error (install/mcp/writer.py:110-120).
func TestInstallMCP_ExistingNameIsUsageError(t *testing.T) {
	dir := chdirTemp(t)
	t.Setenv("HOME", t.TempDir())
	manifest := "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  mcp:\n    - name: two\n      registry: false\n      transport: stdio\n      command: echo\n"
	writeCompileFile(t, dir, "apm.yml", manifest)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCompileCLI(t, "install", "--mcp", "two", "--", "echo", "hi")

	want := "Usage: apm-go install [packages...] [flags]\nTry 'apm-go install --help' for help.\n\n" +
		"Error: MCP server 'two' already exists in " + filepath.Join(cwd, "apm.yml") + ". Use --force to replace (non-interactive).\n"
	if code != 2 || stdout != "" || stderr != want {
		t.Errorf("exit %d\nstdout %q\nstderr %q\nwant exit 2, no stdout and stderr %q", code, stdout, stderr, want)
	}
	if got := readText(t, "apm.yml"); got != manifest {
		t.Errorf("apm.yml changed:\n%s", got)
	}
}

// issue #123 item 32: a direct dependency's self-defined server gets the
// trust line, its label in the MCP block and "args": [] in a .mcp.json of
// mode 0644 (integration/mcp_config_view.py:349-361,
// mcp_integrator_install.py:997-1000, adapters/client/claude.py:240).
func TestInstall_DependencySelfDefinedMCP(t *testing.T) {
	dir := chdirTemp(t)
	t.Setenv("HOME", t.TempDir())
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - ./dep\n")
	writeCompileFile(t, dir, "dep/apm.yml", "name: dep\nversion: 1.0.0\ndependencies:\n  mcp:\n    - name: depmcp\n      registry: false\n      transport: stdio\n      command: echo\n")
	if err := os.MkdirAll(".claude", 0o755); err != nil {
		t.Fatal(err)
	}

	got := mustRunCLI(t, "install")

	want := " > Installing dependencies from apm.yml...\n" +
		" > Resolving ./dep...\n" +
		" i Targets: claude  (source: apm.yml)\n" +
		"   + ./dep (local)\n" +
		"  |-- (files unchanged)\n" +
		" i Added apm_modules/ to .gitignore\n" +
		" i Trusting direct dependency MCP 'depmcp' from 'dep'\n" +
		"+- MCP Servers (1)\n" +
		" i Targeting declared target from apm.yml: claude\n" +
		"|   >  depmcp (self-defined, stdio)\n" +
		"|     +- Configuring for Claude...\n" +
		"Successfully configured MCP server 'depmcp' for Claude Code\n" +
		"  + depmcp\n" +
		"|   +  depmcp -> Claude (configured)\n" +
		" + Configured 1 server\n" +
		" + Installed 1 APM dependency and 1 MCP server in <DUR>.\n"
	if got != want {
		t.Errorf("install:\n%s\nwant:\n%s", got, want)
	}
	wantJSON := "{\n  \"mcpServers\": {\n    \"depmcp\": {\n      \"type\": \"stdio\",\n      \"command\": \"echo\",\n      \"args\": []\n    }\n  }\n}\n"
	if got := readText(t, ".mcp.json"); got != wantJSON {
		t.Errorf(".mcp.json:\n%s\nwant:\n%s", got, wantJSON)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(".mcp.json")
		if err != nil {
			t.Fatal(err)
		}
		// Path.write_text: 0666 narrowed by the umask, readable by others.
		if info.Mode().Perm()&0o044 == 0 {
			t.Errorf(".mcp.json mode %v, want the mode of a new file (0644 with umask 022)", info.Mode().Perm())
		}
	}
}

// The trust lines come in the order of the lock's dependencies, a name the
// project declares too included; update prints no warning about the server
// the project's own declaration shadows (issue #123 item c57-7).
func TestInstall_DependencyMCPTrustLinesAndUpdate(t *testing.T) {
	dir := chdirTemp(t)
	t.Setenv("HOME", t.TempDir())
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - ./b\n    - ./a\n  mcp:\n    - name: shared\n      registry: false\n      transport: stdio\n      command: echo\n")
	writeCompileFile(t, dir, "b/apm.yml", "name: bee\nversion: 1.0.0\ndependencies:\n  mcp:\n    - name: bsrv\n      registry: false\n      transport: http\n      url: https://example.com/mcp\n    - name: shared\n      registry: false\n      transport: stdio\n      command: echo\n")
	writeCompileFile(t, dir, "a/apm.yml", "name: ay\nversion: 1.0.0\ndependencies:\n  mcp:\n    - name: asrv\n      registry: false\n      transport: stdio\n      command: echo\n      args: [x]\n")
	if err := os.MkdirAll(".claude", 0o755); err != nil {
		t.Fatal(err)
	}

	got := mustRunCLI(t, "install")

	for _, line := range []string{
		" i Trusting direct dependency MCP 'asrv' from 'ay'\n i Trusting direct dependency MCP 'bsrv' from 'bee'\n i Trusting direct dependency MCP 'shared' from 'bee'\n+- MCP Servers (3)\n",
		"|   >  bsrv (self-defined, http)\n",
	} {
		if !strings.Contains(got, line) {
			t.Errorf("install output lacks %q:\n%s", line, got)
		}
	}
	if got := mustRunCLI(t, "update"); strings.Contains(got, "shadowed") {
		t.Errorf("update warns about the shadowed server:\n%s", got)
	}
}

// issue #123 item x1: a streamable-http server is "type": "http" in
// .mcp.json, as the Oracle's copilot formatter writes every remote
// (adapters/client/copilot.py:485-490); sse stays sse (PRODUCT.md).
func TestInstall_ClaudeRemoteMCPType(t *testing.T) {
	dir := chdirTemp(t)
	t.Setenv("HOME", t.TempDir())
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  mcp:\n"+
		"    - name: h\n      registry: false\n      transport: http\n      url: https://example.com/h\n"+
		"    - name: s\n      registry: false\n      transport: sse\n      url: https://example.com/s\n"+
		"    - name: sh\n      registry: false\n      transport: streamable-http\n      url: https://example.com/sh\n")
	mustRunCLI(t, "install")

	want := "{\n  \"mcpServers\": {\n" +
		"    \"h\": {\n      \"type\": \"http\",\n      \"url\": \"https://example.com/h\"\n    },\n" +
		"    \"s\": {\n      \"type\": \"sse\",\n      \"url\": \"https://example.com/s\"\n    },\n" +
		"    \"sh\": {\n      \"type\": \"http\",\n      \"url\": \"https://example.com/sh\"\n    }\n  }\n}\n"
	if got := readText(t, ".mcp.json"); got != want {
		t.Errorf(".mcp.json:\n%s\nwant:\n%s", got, want)
	}
}

// issue #123 items c57-6/c62-6 and c62-1: .codex/config.toml as tomlkit
// writes it and .agents/mcp_config.json as json.dumps writes it, both 0600
// (adapters/client/codex.py:105-126, gemini.py:93-111), less the ruled
// deviations: no id = "", and serverUrl where the Oracle writes httpUrl.
func TestInstall_CodexAndAntigravityMCPLayout(t *testing.T) {
	dir := chdirTemp(t)
	t.Setenv("HOME", t.TempDir())
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntargets: [codex, antigravity]\ndependencies:\n  mcp:\n"+
		"    - name: zeta\n      registry: false\n      transport: stdio\n      command: run-zeta\n      args: [--flag, \"two words\"]\n      env:\n        ZED: \"1\"\n        ALPHA: \"2\"\n"+
		"    - name: bare\n      registry: false\n      transport: stdio\n      command: run-bare\n"+
		"    - name: aweb\n      registry: false\n      transport: http\n      url: https://example.com/mcp\n      headers:\n        X-Zed: \"z\"\n        X-Api: \"a\"\n")
	for _, d := range []string{".codex", ".agents"} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustRunCLI(t, "install")

	wantTOML := "[mcp_servers.zeta]\ncommand = \"run-zeta\"\nargs = [\"--flag\", \"two words\"]\n\n[mcp_servers.zeta.env]\nZED = \"1\"\nALPHA = \"2\"\n\n" +
		"[mcp_servers.bare]\ncommand = \"run-bare\"\nargs = []\n\n[mcp_servers.bare.env]\n\n" +
		"[mcp_servers.aweb]\nurl = \"https://example.com/mcp\"\n\n[mcp_servers.aweb.http_headers]\nX-Zed = \"z\"\nX-Api = \"a\"\n"
	if got := readText(t, ".codex/config.toml"); got != wantTOML {
		t.Errorf(".codex/config.toml:\n%s\nwant:\n%s", got, wantTOML)
	}
	wantJSON := "{\n  \"mcpServers\": {\n" +
		"    \"zeta\": {\n      \"command\": \"run-zeta\",\n      \"env\": {\n        \"ZED\": \"1\",\n        \"ALPHA\": \"2\"\n      },\n      \"args\": [\n        \"--flag\",\n        \"two words\"\n      ]\n    },\n" +
		"    \"bare\": {\n      \"command\": \"run-bare\",\n      \"args\": []\n    },\n" +
		"    \"aweb\": {\n      \"serverUrl\": \"https://example.com/mcp\",\n      \"headers\": {\n        \"X-Zed\": \"z\",\n        \"X-Api\": \"a\"\n      }\n    }\n  }\n}"
	if got := readText(t, ".agents/mcp_config.json"); got != wantJSON {
		t.Errorf(".agents/mcp_config.json:\n%s\nwant:\n%s", got, wantJSON)
	}
	if runtime.GOOS != "windows" {
		for _, f := range []string{".codex/config.toml", ".agents/mcp_config.json"} {
			if info, err := os.Stat(f); err != nil || info.Mode().Perm() != 0o600 {
				t.Errorf("%s: mode %v (err %v), want 0600", f, info.Mode().Perm(), err)
			}
		}
	}
}

// issue #123 item c62-2: install --mcp of a registry server warns about a
// registry on this machine, names no transport for it and records the entry
// apm.yml declares (install/mcp/registry.py:197-217, command.py:262-328).
func TestInstallMCP_RegistryServerLinesAndLock(t *testing.T) {
	server := map[string]any{"name": "io.example/demo", "version": "1.0.0",
		"remotes": []any{map[string]any{"type": "streamable-http", "url": "https://example.com/mcp"}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body any
		switch {
		case strings.HasPrefix(r.URL.Path, "/v0.1/servers/"):
			body = map[string]any{"server": server}
		case r.URL.Path == "/v0.1/servers":
			body = map[string]any{"servers": []any{map[string]any{"server": server}}}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	dir := chdirTemp(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MCP_REGISTRY_URL", srv.URL)
	t.Setenv("MCP_REGISTRY_ALLOW_HTTP", "1")
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntargets: [claude]\n")
	if err := os.MkdirAll(".claude", 0o755); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	got := mustRunCLI(t, "install", "--mcp", "io.example/demo")

	want := loopbackRegistryWarning +
		"+- MCP Servers (1)\n" +
		" i Targeting declared target from apm.yml: claude\n" +
		"|   >  io.example/demo\n" +
		"|     +- Configuring for Claude...\n" +
		"Successfully configured MCP server 'io.example/demo' for Claude Code\n" +
		"  + io.example/demo\n" +
		"|   +  io.example/demo -> Claude (configured)\n" +
		" + Configured 1 server\n" +
		" + Added MCP server 'io.example/demo'\n" +
		"  transport: registry\n" +
		"  apm.yml: " + filepath.Join(cwd, "apm.yml") + "\n"
	if got != want {
		t.Errorf("install --mcp:\n%s\nwant:\n%s", got, want)
	}
	wantConfigs := "mcp_configs:\n  io.example/demo:\n    name: io.example/demo\n    registry: " + srv.URL + "\nmcp_target_servers:\n"
	if lock := readText(t, "apm.lock.yaml"); !strings.Contains(lock, wantConfigs) {
		t.Errorf("lock:\n%s\nwant it to hold:\n%s", lock, wantConfigs)
	}
}
