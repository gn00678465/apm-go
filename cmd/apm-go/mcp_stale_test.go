package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
)

// Issue #71: install and update remove from the MCP config files a server
// the old lock records and this run no longer writes. The expected lines and
// .mcp.json bytes are Oracle 0.33.0's output for the same steps
// (~/.cache/apm-go-verify/issue71/runs/py-*).

func staleMCPManifest(targets string, servers ...string) string {
	var b strings.Builder
	b.WriteString("name: g8\nversion: 1.0.0\n")
	if targets != "" {
		b.WriteString("targets: [" + targets + "]\n")
	}
	b.WriteString("dependencies:\n")
	if len(servers) == 0 {
		b.WriteString("  mcp: []\n")
		return b.String()
	}
	b.WriteString("  mcp:\n")
	for _, s := range servers {
		b.WriteString("    - name: " + s + "\n      registry: false\n      transport: stdio\n      command: npx\n      args: [\"-y\", \"" + s + "\"]\n")
	}
	return b.String()
}

func writeStaleMCPFile(t *testing.T, rel, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rel, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readStaleMCPFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func staleMCPInstall(t *testing.T, targetFlag, deployDir string) (string, error) {
	t.Helper()
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	var err error
	out := captureStdout(t, func() {
		err = runInstall(deps, false, false, targetFlag, deployDir, nil, nil)
	})
	return out, err
}

func mustStaleMCPInstall(t *testing.T, targetFlag, deployDir string) string {
	t.Helper()
	out, err := staleMCPInstall(t, targetFlag, deployDir)
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	return out
}

func staleMCPLines(out string) string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Removed stale MCP server") {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

const staleMCPOracleKeepAndMine = `{
  "mcpServers": {
    "keep": {
      "type": "stdio",
      "command": "npx",
      "args": [
        "-y",
        "keep"
      ]
    },
    "mine": {
      "command": "mine"
    }
  }
}
`

func TestInstall_StaleMCP_TargetsShrunkInAPMYml(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("claude, codex, opencode", "example"))
	mustStaleMCPInstall(t, "", "")

	codex := readStaleMCPFile(t, ".codex/config.toml") + "\n[mcp_servers.mine]\ncommand = \"mine\"\n"
	writeStaleMCPFile(t, ".codex/config.toml", codex)
	writeStaleMCPFile(t, "opencode.json", `{
  "mcp": {
    "example": {
      "command": [
        "npx",
        "-y",
        "example"
      ],
      "type": "local"
    },
    "mine": {
      "command": [
        "mine"
      ],
      "type": "local"
    }
  }
}`)
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("claude", "example"))
	out := mustStaleMCPInstall(t, "", "")

	want := " + Removed stale MCP server 'example' from Codex CLI config\n" +
		" i Removed stale MCP server 'example' from opencode.json"
	if got := staleMCPLines(out); got != want {
		t.Errorf("removal lines:\n%s\nwant:\n%s\nfull output:\n%s", got, want, out)
	}
	if !strings.Contains(out, " + All servers up to date\n"+want+"\n\n i No changes -- install state already up to date in ") {
		t.Errorf("the lines must follow the MCP block and precede the summary:\n%s", out)
	}
	if got := readStaleMCPFile(t, "opencode.json"); got != `{
  "mcp": {
    "mine": {
      "command": [
        "mine"
      ],
      "type": "local"
    }
  }
}` {
		t.Errorf("opencode.json:\n%s", got)
	}
	toml := readStaleMCPFile(t, ".codex/config.toml")
	if strings.Contains(toml, "example") || !strings.Contains(toml, "[mcp_servers.mine]") {
		t.Errorf(".codex/config.toml must keep mine only:\n%s", toml)
	}
	if !strings.Contains(readStaleMCPFile(t, ".mcp.json"), `"example"`) {
		t.Error(".mcp.json lost the server claude still has")
	}

	if again := mustStaleMCPInstall(t, "", ""); strings.Contains(again, "Removed stale") {
		t.Errorf("a second install removed something again:\n%s", again)
	}
}

func TestInstall_StaleMCP_TargetFlagShrunk(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("", "example"))
	mustStaleMCPInstall(t, "claude,codex", "")
	if !strings.Contains(readStaleMCPFile(t, ".codex/config.toml"), "example") {
		t.Fatal("first install did not configure codex")
	}

	out := mustStaleMCPInstall(t, "claude", "")
	if got, want := staleMCPLines(out), " + Removed stale MCP server 'example' from Codex CLI config"; got != want {
		t.Errorf("removal lines:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(readStaleMCPFile(t, ".codex/config.toml"), "example") {
		t.Error(".codex/config.toml still has example")
	}
}

func staleMCPServerRemovedProject(t *testing.T) {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("claude", "foo-mcp", "keep"))
	mustStaleMCPInstall(t, "", "")
	writeStaleMCPFile(t, ".mcp.json", `{
  "mcpServers": {
    "foo-mcp": {
      "type": "stdio",
      "command": "npx",
      "args": [
        "-y",
        "foo-mcp"
      ]
    },
    "keep": {
      "type": "stdio",
      "command": "npx",
      "args": [
        "-y",
        "keep"
      ]
    },
    "mine": {
      "command": "mine"
    }
  }
}
`)
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("claude", "keep"))
}

func TestInstall_StaleMCP_ServerRemoved(t *testing.T) {
	staleMCPServerRemovedProject(t)
	out := mustStaleMCPInstall(t, "", "")
	if got, want := staleMCPLines(out), " i Removed stale MCP server 'foo-mcp' from .mcp.json"; got != want {
		t.Errorf("removal lines:\n%s\nwant:\n%s", got, want)
	}
	if got := readStaleMCPFile(t, ".mcp.json"); got != staleMCPOracleKeepAndMine {
		t.Errorf(".mcp.json:\n%s\nwant:\n%s", got, staleMCPOracleKeepAndMine)
	}
}

func TestUpdate_StaleMCP_ServerRemoved(t *testing.T) {
	staleMCPServerRemovedProject(t)
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	out := captureStdout(t, func() {
		if err := runUpdate(deps, false, true, "", false); err != nil {
			t.Errorf("update: %v", err)
		}
	})
	if got, want := staleMCPLines(out), " i Removed stale MCP server 'foo-mcp' from .mcp.json"; got != want {
		t.Errorf("removal lines:\n%s\nwant:\n%s\nfull output:\n%s", got, want, out)
	}
	if got := readStaleMCPFile(t, ".mcp.json"); got != staleMCPOracleKeepAndMine {
		t.Errorf(".mcp.json:\n%s\nwant:\n%s", got, staleMCPOracleKeepAndMine)
	}
}

func TestInstall_StaleMCP_AllServersRemoved(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("claude, codex", "foo-mcp"))
	mustStaleMCPInstall(t, "", "")
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("claude, codex"))

	out := mustStaleMCPInstall(t, "", "")
	want := " i Removed stale MCP server 'foo-mcp' from .mcp.json\n" +
		" + Removed stale MCP server 'foo-mcp' from Codex CLI config"
	if got := staleMCPLines(out); got != want {
		t.Errorf("removal lines:\n%s\nwant:\n%s", got, want)
	}
	if got := readStaleMCPFile(t, ".mcp.json"); got != "{\n  \"mcpServers\": {}\n}\n" {
		t.Errorf(".mcp.json:\n%s", got)
	}
	if strings.Contains(readStaleMCPFile(t, ".codex/config.toml"), "foo-mcp") {
		t.Error(".codex/config.toml still has foo-mcp")
	}
	if lock := readLockfile(t); len(lock.MCPServers) != 0 || len(lock.MCPTargetServers) != 0 {
		t.Errorf("lock still records servers: %v %v", lock.MCPServers, lock.MCPTargetServers)
	}
}

func TestInstall_StaleMCP_DependencyServerRemoved(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	writeStaleMCPFile(t, "pkg/.apm/instructions/a.instructions.md", "---\napplyTo: \"**\"\n---\nhi\n")
	writeStaleMCPFile(t, "pkg/apm.yml", staleMCPManifest("", "depsrv"))
	writeStaleMCPFile(t, "apm.yml", "name: g8\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - ./pkg\n")
	mustStaleMCPInstall(t, "", "")
	if !strings.Contains(readStaleMCPFile(t, ".mcp.json"), "depsrv") {
		t.Fatal("first install did not configure depsrv")
	}

	writeStaleMCPFile(t, "pkg/apm.yml", "name: pkg\nversion: 1.0.0\n")
	out := mustStaleMCPInstall(t, "", "")
	if got, want := staleMCPLines(out), " i Removed stale MCP server 'depsrv' from .mcp.json"; got != want {
		t.Errorf("removal lines:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(readStaleMCPFile(t, ".mcp.json"), "depsrv") {
		t.Error(".mcp.json still has depsrv")
	}
}

func TestInstall_StaleMCP_Global(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	home := t.TempDir()
	chdirTemp(t)
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("claude, codex", "foo-mcp", "keep"))
	mustStaleMCPInstall(t, "", home)
	mcpJSON := filepath.Join(home, ".mcp.json")
	codex := filepath.Join(home, ".codex", "config.toml")
	if !strings.Contains(readStaleMCPFile(t, codex), "foo-mcp") {
		t.Fatal("first install did not configure codex under HOME")
	}

	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("claude", "keep"))
	out := mustStaleMCPInstall(t, "", home)
	want := " + Removed stale MCP server 'foo-mcp' from Codex CLI config\n" +
		" + Removed stale MCP server 'keep' from Codex CLI config\n" +
		" i Removed stale MCP server 'foo-mcp' from .mcp.json"
	if got := staleMCPLines(out); got != want {
		t.Errorf("removal lines:\n%s\nwant:\n%s", got, want)
	}
	if got := readStaleMCPFile(t, codex); strings.Contains(got, "foo-mcp") || strings.Contains(got, "keep") {
		t.Errorf("%s still has a server:\n%s", codex, got)
	}
	if got := readStaleMCPFile(t, mcpJSON); strings.Contains(got, "foo-mcp") || !strings.Contains(got, "keep") {
		t.Errorf("%s:\n%s", mcpJSON, got)
	}
	if _, err := os.Stat(".mcp.json"); !os.IsNotExist(err) {
		t.Errorf("a --global install wrote the project's .mcp.json: %v", err)
	}
}

// A lock written by apm-go v0.3.0 has mcp_servers and no mcp_target_servers;
// its codex entries carry an id.
const staleMCPLegacyLock = "lockfile_version: '1'\napm_version: 0.3.0\ndependencies: []\nmcp_servers:\n- foo-mcp\n- keep\n"

const staleMCPLegacyCodex = `[mcp_servers]
[mcp_servers.foo-mcp]
args = ['-y', 'foo-mcp']
command = 'npx'
id = 'foo-mcp'

[mcp_servers.keep]
args = ['-y', 'keep']
command = 'npx'
id = 'keep'
`

func TestInstall_StaleMCP_LegacyLockRemovesStaleServer(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("claude, codex", "keep"))
	writeStaleMCPFile(t, "apm.lock.yaml", staleMCPLegacyLock)
	writeStaleMCPFile(t, ".codex/config.toml", staleMCPLegacyCodex)
	writeStaleMCPFile(t, ".mcp.json", "{\n  \"mcpServers\": {\n    \"foo-mcp\": {\n      \"command\": \"npx\"\n    }\n  }\n}\n")

	out := mustStaleMCPInstall(t, "", "")
	want := " i Removed stale MCP server 'foo-mcp' from .mcp.json\n" +
		" + Removed stale MCP server 'foo-mcp' from Codex CLI config"
	if got := staleMCPLines(out); got != want {
		t.Errorf("removal lines:\n%s\nwant:\n%s", got, want)
	}
	got := readStaleMCPFile(t, ".codex/config.toml")
	if strings.Contains(got, "foo-mcp") || strings.Contains(got, "id =") || !strings.Contains(got, "[mcp_servers.keep]") {
		t.Errorf(".codex/config.toml:\n%s", got)
	}
}

func TestInstall_StaleMCP_LegacyLockAllRemovedCleansNothing(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("claude, codex"))
	writeStaleMCPFile(t, "apm.lock.yaml", staleMCPLegacyLock)
	writeStaleMCPFile(t, ".codex/config.toml", staleMCPLegacyCodex)

	out := mustStaleMCPInstall(t, "", "")
	if strings.Contains(out, "Removed stale") {
		t.Errorf("a lock with no mcp_target_servers names no owner, nothing may be removed:\n%s", out)
	}
	if got := readStaleMCPFile(t, ".codex/config.toml"); got != staleMCPLegacyCodex {
		t.Errorf(".codex/config.toml changed:\n%s", got)
	}
}

func TestInstall_StaleMCP_InvalidConfigFailsWithoutLockWrite(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("claude, codex", "foo-mcp"))
	mustStaleMCPInstall(t, "", "")
	lockBefore := readStaleMCPFile(t, "apm.lock.yaml")
	writeStaleMCPFile(t, ".codex/config.toml", "[mcp_servers\n")
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("claude", "foo-mcp"))

	out, err := staleMCPInstall(t, "", "")
	if err == nil {
		t.Fatalf("install succeeded:\n%s", out)
	}
	wantPrefix := "MCP cleanup failed for Codex CLI config: existing .codex/config.toml is not valid, refusing to overwrite: "
	if !strings.HasPrefix(err.Error(), wantPrefix) || !strings.HasSuffix(err.Error(), ". Check the config path and permissions, then retry.") {
		t.Errorf("error = %q", err)
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exit code = %d, want 1", exitCodeOf(err))
	}
	if got := readStaleMCPFile(t, "apm.lock.yaml"); got != lockBefore {
		t.Errorf("lock was written:\n%s", got)
	}
}

func TestInstall_StaleMCP_SymlinkedConfigIsRefused(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	dir := chdirTemp(t)
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("claude, codex", "foo-mcp"))
	mustStaleMCPInstall(t, "", "")
	lockBefore := readStaleMCPFile(t, "apm.lock.yaml")
	outside := filepath.Join(t.TempDir(), "real.toml")
	const real = "[mcp_servers.foo-mcp]\ncommand = \"npx\"\n"
	if err := os.WriteFile(outside, []byte(real), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(".codex/config.toml"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, ".codex/config.toml"); err != nil {
		t.Fatal(err)
	}
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("claude", "foo-mcp"))

	out, err := staleMCPInstall(t, "", "")
	want := "Refusing to clean symlinked MCP config: Codex CLI config (" + filepath.Join(dir, ".codex/config.toml") +
		"). Replace the symlink with a regular file or directory, then retry."
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q\n%s", err, want, out)
	}
	if got := readStaleMCPFile(t, outside); got != real {
		t.Errorf("the link target changed:\n%s", got)
	}
	if got := readStaleMCPFile(t, "apm.lock.yaml"); got != lockBefore {
		t.Errorf("lock was written:\n%s", got)
	}
}

// A server that is still declared but that a target fails to write is not
// stale. The expected output and lock are what apm-go printed and wrote
// before issue #71 for the same steps
// (~/.cache/apm-go-verify/issue71/writefail/run-XYBb/old-*).
func staleMCPWriteFailure(t *testing.T, targets, breakFile, broken string) (string, error) {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest(targets, "keep"))
	mustStaleMCPInstall(t, "", "")
	writeStaleMCPFile(t, breakFile, broken)
	out, err := staleMCPInstall(t, "", "")
	return regexp.MustCompile(`in [0-9.]+s\.`).ReplaceAllString(out, "in Ns."), err
}

func TestInstall_StaleMCP_DeclaredServerWhoseOnlyWriteFailsIsKept(t *testing.T) {
	out, err := staleMCPWriteFailure(t, "claude", ".mcp.json", "{bad")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	want := " > Installing dependencies from apm.yml...\n" +
		" ! write mcp config for claude failed: existing .mcp.json is not valid, refusing to overwrite: invalid character 'b' looking for beginning of object key string\n" +
		"\n" +
		" i No changes -- install state already up to date in Ns.\n"
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
	if got := readStaleMCPFile(t, "apm.lock.yaml"); got != "lockfile_version: '1'\napm_version: dev\ndependencies: []\ndeployments: []\n" {
		t.Errorf("apm.lock.yaml:\n%s", got)
	}
	if got := readStaleMCPFile(t, ".mcp.json"); got != "{bad" {
		t.Errorf(".mcp.json changed: %s", got)
	}
}

func TestInstall_StaleMCP_DeclaredServerWithOneFailedTargetIsKept(t *testing.T) {
	out, err := staleMCPWriteFailure(t, "claude, codex", ".codex/config.toml", "[mcp_servers\n")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	want := " > Installing dependencies from apm.yml...\n" +
		" ! write mcp config for codex failed: existing .codex/config.toml is not valid, refusing to overwrite: toml: expected ']' to close table name\n" +
		"+- MCP Servers (1)\n" +
		" i Targeting declared target from apm.yml: claude\n" +
		"|   + keep (already configured)\n" +
		" + All servers up to date\n" +
		"\n" +
		" i No changes -- install state already up to date in Ns.\n"
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
	const lock = `lockfile_version: '1'
apm_version: dev
dependencies: []
deployments:
- kind: uri
  target: mcp
  value: keep
  runtime: claude
  scope: project
  owners:
  - .
  active_owner: .
  content_hash: null
mcp_servers:
- keep
mcp_configs:
  keep:
    name: keep
    transport: stdio
    args:
    - -y
    - keep
    registry: false
    command: npx
mcp_target_servers:
  claude:
  - keep
`
	if got := readStaleMCPFile(t, "apm.lock.yaml"); got != lock {
		t.Errorf("apm.lock.yaml:\n%s", got)
	}
	if !strings.Contains(readStaleMCPFile(t, ".mcp.json"), `"keep"`) {
		t.Error(".mcp.json lost keep")
	}
}

// A declared server is not stale whatever became of it later in the run. The
// expected output and files are what apm-go printed and wrote before issue
// #71 for the same steps
// (~/.cache/apm-go-verify/issue71/regold-step16/run-o4i5jyhd/old-*).

const staleMCPRegistryOnlyHeaderLock = "lockfile_version: '1'\napm_version: dev\ndependencies: []\ndeployments: []\n"

func staleMCPRegistryRerun(t *testing.T, mode string) {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	var current atomic.Value
	current.Store("ok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if current.Load() == "503" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		server := map[string]any{"name": "example", "remotes": []any{map[string]any{"type": "streamable-http", "url": "https://example.com/mcp"}}}
		body := map[string]any{"server": server}
		if r.URL.RawQuery != "" {
			list := []any{map[string]any{"server": server}}
			if current.Load() == "missing" {
				list = []any{}
			}
			body = map[string]any{"servers": list}
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)

	writeStaleMCPFile(t, "apm.yml", "name: g8\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  mcp:\n    - name: example\n      registry: "+srv.URL+"\n")
	mustStaleMCPInstall(t, "", "")
	current.Store(mode)
	out := mustStaleMCPInstall(t, "", "")
	out = regexp.MustCompile(`in [0-9.]+s\.`).ReplaceAllString(out, "in Ns.")

	reason := `mcp registry: registry returned HTTP 503`
	if mode == "missing" {
		reason = `MCP server "example" not found in registry`
	}
	want := " > Installing dependencies from apm.yml...\n" +
		" ! mcp \"example\": " + reason + "; skipped\n" +
		"\n" +
		" i No changes -- install state already up to date in Ns.\n"
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
	if got := readStaleMCPFile(t, ".mcp.json"); got != "{\n  \"mcpServers\": {\n    \"example\": {\n      \"type\": \"streamable-http\",\n      \"url\": \"https://example.com/mcp\"\n    }\n  }\n}\n" {
		t.Errorf(".mcp.json:\n%s", got)
	}
	if got := readStaleMCPFile(t, "apm.lock.yaml"); got != staleMCPRegistryOnlyHeaderLock {
		t.Errorf("apm.lock.yaml:\n%s", got)
	}
}

func TestInstall_StaleMCP_RegistryOutageKeepsDeclaredServer(t *testing.T) {
	staleMCPRegistryRerun(t, "503")
}

func TestInstall_StaleMCP_RegistryNotFoundKeepsDeclaredServer(t *testing.T) {
	staleMCPRegistryRerun(t, "missing")
}

func TestInstall_StaleMCP_ServerMovedToDevDependenciesIsKept(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("claude", "keep"))
	mustStaleMCPInstall(t, "", "")
	before := readStaleMCPFile(t, ".mcp.json")
	moved := strings.Replace(staleMCPManifest("claude", "keep"), "dependencies:\n  mcp:\n", "dependencies:\n  mcp: []\ndevDependencies:\n  mcp:\n", 1)
	writeStaleMCPFile(t, "apm.yml", moved)

	out := mustStaleMCPInstall(t, "", "")
	out = regexp.MustCompile(`in [0-9.]+s\.`).ReplaceAllString(out, "in Ns.")
	if want := " > Installing dependencies from apm.yml...\n\n i No changes -- install state already up to date in Ns.\n"; out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
	if got := readStaleMCPFile(t, ".mcp.json"); got != before {
		t.Errorf(".mcp.json changed:\n%s", got)
	}
	if got := readStaleMCPFile(t, "apm.lock.yaml"); got != staleMCPRegistryOnlyHeaderLock {
		t.Errorf("apm.lock.yaml:\n%s", got)
	}
}
