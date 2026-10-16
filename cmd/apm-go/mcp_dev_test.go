package main

import (
	"regexp"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
)

// The expected bytes below are what Oracle 0.33.0 printed and wrote for the
// same apm.yml, with the status symbols in apm-go's shape and apm_version as
// apm-go writes it.

const mcpDevManifest = "name: g8\nversion: 1.0.0\ntargets: [claude]\ndevDependencies:\n  mcp:\n" +
	"    - name: devsrv\n      registry: false\n      transport: stdio\n      command: npx\n      args: [\"-y\", \"dev\"]\n"

const mcpDevOracleMCPJSON = `{
  "mcpServers": {
    "devsrv": {
      "type": "stdio",
      "command": "npx",
      "args": [
        "-y",
        "dev"
      ]
    }
  }
}
`

const mcpDevOracleLockTail = `dependencies: []
deployments:
- kind: uri
  target: mcp
  value: devsrv
  runtime: claude
  scope: project
  owners:
  - .
  active_owner: .
  content_hash: null
mcp_servers:
- devsrv
mcp_configs:
  devsrv:
    name: devsrv
    transport: stdio
    args:
    - -y
    - dev
    registry: false
    command: npx
mcp_target_servers:
  claude:
  - devsrv
`

func normalizeMCPDevElapsed(out string) string {
	return regexp.MustCompile(`in [0-9.]+s\.`).ReplaceAllString(out, "in Ns.")
}

func TestInstall_MCPDevServerOnlyIsConfigured(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	writeStaleMCPFile(t, "apm.yml", mcpDevManifest)

	out := normalizeMCPDevElapsed(mustStaleMCPInstall(t, "", ""))
	want := " > Installing dependencies from apm.yml...\n" +
		"+- MCP Servers (1)\n" +
		" i Targeting declared target from apm.yml: claude\n" +
		"|   >  devsrv (self-defined, stdio)\n" +
		"|     +- Configuring for Claude...\n" +
		"Successfully configured MCP server 'devsrv' for Claude Code\n" +
		"  + devsrv\n" +
		"|   +  devsrv -> Claude (configured)\n" +
		" + Configured 1 server\n" +
		"\n" +
		" + Installed 1 MCP server in Ns.\n"
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
	if got := readStaleMCPFile(t, ".mcp.json"); got != mcpDevOracleMCPJSON {
		t.Errorf(".mcp.json:\n%s\nwant:\n%s", got, mcpDevOracleMCPJSON)
	}
	wantLock := "lockfile_version: '1'\napm_version: dev\n" + mcpDevOracleLockTail
	if got := readStaleMCPFile(t, "apm.lock.yaml"); got != wantLock {
		t.Errorf("apm.lock.yaml:\n%s\nwant:\n%s", got, wantLock)
	}
}

func TestUpdate_MCPDevServerAddedIsConfigured(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("claude", "prodsrv"))
	mustStaleMCPInstall(t, "", "")
	writeStaleMCPFile(t, "apm.yml", staleMCPManifest("claude", "prodsrv")+
		"devDependencies:\n  mcp:\n    - name: devsrv\n      registry: false\n      transport: stdio\n      command: npx\n      args: [\"-y\", \"dev\"]\n")

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	out := captureStdout(t, func() {
		if err := runUpdate(deps, false, true, "", false); err != nil {
			t.Errorf("update: %v", err)
		}
	})

	wantMCP := `{
  "mcpServers": {
    "prodsrv": {
      "type": "stdio",
      "command": "npx",
      "args": [
        "-y",
        "prodsrv"
      ]
    },
    "devsrv": {
      "type": "stdio",
      "command": "npx",
      "args": [
        "-y",
        "dev"
      ]
    }
  }
}
`
	if got := readStaleMCPFile(t, ".mcp.json"); got != wantMCP {
		t.Errorf(".mcp.json:\n%s\nwant:\n%s\nupdate output:\n%s", got, wantMCP, out)
	}
	wantLock := `lockfile_version: '1'
apm_version: dev
dependencies: []
deployments:
- kind: uri
  target: mcp
  value: devsrv
  runtime: claude
  scope: project
  owners:
  - .
  active_owner: .
  content_hash: null
- kind: uri
  target: mcp
  value: prodsrv
  runtime: claude
  scope: project
  owners:
  - .
  active_owner: .
  content_hash: null
mcp_servers:
- devsrv
- prodsrv
mcp_configs:
  devsrv:
    name: devsrv
    transport: stdio
    args:
    - -y
    - dev
    registry: false
    command: npx
  prodsrv:
    name: prodsrv
    transport: stdio
    args:
    - -y
    - prodsrv
    registry: false
    command: npx
mcp_target_servers:
  claude:
  - devsrv
  - prodsrv
`
	if got := readStaleMCPFile(t, "apm.lock.yaml"); got != wantLock {
		t.Errorf("apm.lock.yaml:\n%s\nwant:\n%s", got, wantLock)
	}
}

func TestInstall_MCPDevServerInOracleLockIsUpToDate(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	oracleLock := "lockfile_version: '1'\napm_version: 0.33.0\n" + mcpDevOracleLockTail
	writeStaleMCPFile(t, "apm.yml", mcpDevManifest)
	writeStaleMCPFile(t, "apm.lock.yaml", oracleLock)
	writeStaleMCPFile(t, ".mcp.json", mcpDevOracleMCPJSON)

	out := normalizeMCPDevElapsed(mustStaleMCPInstall(t, "", ""))
	want := " > Installing dependencies from apm.yml...\n" +
		"+- MCP Servers (1)\n" +
		" i Targeting declared target from apm.yml: claude\n" +
		"|   + devsrv (already configured)\n" +
		" + All servers up to date\n" +
		"\n" +
		" i No changes -- install state already up to date in Ns.\n"
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
	if got := readStaleMCPFile(t, "apm.lock.yaml"); got != oracleLock {
		t.Errorf("apm.lock.yaml changed:\n%s", got)
	}
	if got := readStaleMCPFile(t, ".mcp.json"); got != mcpDevOracleMCPJSON {
		t.Errorf(".mcp.json changed:\n%s", got)
	}
}
