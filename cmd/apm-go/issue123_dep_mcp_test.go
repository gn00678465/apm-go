package main

import (
	"os"
	"strings"
	"testing"
)

// The lock Oracle 0.33.0 writes for a local package that declares a
// self-defined MCP server (issue #123 run i30), its apm_version aside.
const i30OracleLock = `lockfile_version: '1'
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
  value: depsrv
  runtime: claude
  scope: project
  owners:
  - .
  active_owner: .
  content_hash: null
mcp_servers:
- depsrv
mcp_configs:
  depsrv:
    name: depsrv
    transport: stdio
    env:
      B: '2'
      A: '1'
    args:
    - hi
    registry: false
    command: echo
mcp_target_servers:
  claude:
  - depsrv
mcp_config_provenance:
  depsrv: pkg
`

func writeI30Project(t *testing.T, rootMCP string) {
	t.Helper()
	dir := chdirTemp(t)
	t.Setenv("HOME", t.TempDir())
	writeCompileFile(t, dir, "pkg/apm.yml", "name: pkg\nversion: 1.0.0\ndependencies:\n  mcp:\n    - name: depsrv\n      registry: false\n      transport: stdio\n      command: echo\n      args: [hi]\n      env:\n        B: \"2\"\n        A: \"1\"\n")
	writeCompileFile(t, dir, "apm.yml", "name: p\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - ./pkg\n"+rootMCP)
	if err := os.MkdirAll(".claude", 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestInstall_DependencyMCPServerLockAndRerun follows Oracle 0.33.0 (run
// i30): the lock records the config the dependency declares and its
// package, and an install that changes nothing reports the server as
// already configured and leaves it out of the last line.
func TestInstall_DependencyMCPServerLockAndRerun(t *testing.T) {
	writeI30Project(t, "")
	mustRunCLI(t, "install")
	if got := readText(t, "apm.lock.yaml"); got != i30OracleLock {
		t.Fatalf("apm.lock.yaml:\n%s\nwant:\n%s", got, i30OracleLock)
	}

	out := mustRunCLI(t, "install")
	for _, want := range []string{"|   + depsrv (already configured)\n", " + All servers up to date\n", " + Installed 1 APM dependency in <DUR>.\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("rerun stdout lacks %q:\n%s", want, out)
		}
	}
	if got := readText(t, "apm.lock.yaml"); got != i30OracleLock {
		t.Fatalf("rerun rewrote apm.lock.yaml:\n%s", got)
	}

	writeCompileFile(t, ".", "pkg/apm.yml", strings.Replace(readText(t, "pkg/apm.yml"), "[hi]", "[bye]", 1))
	out = mustRunCLI(t, "install")
	for _, want := range []string{"|     +- Updating for Claude...\n", "depsrv -> Claude (updated)\n", " + Updated 1 server\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("changed-args stdout lacks %q:\n%s", want, out)
		}
	}
	if got := readText(t, "apm.lock.yaml"); !strings.Contains(got, "    args:\n    - bye\n") {
		t.Errorf("lock keeps the old args:\n%s", got)
	}
}

// TestUninstall_DropsDependencyMCPServerConfig: uninstall of the package
// leaves the lock Oracle 0.33.0 leaves, with no mcp_configs or
// mcp_config_provenance of its server.
func TestUninstall_DropsDependencyMCPServerConfig(t *testing.T) {
	writeI30Project(t, "  mcp:\n    - name: rootsrv\n      registry: false\n      transport: stdio\n      command: echo\n")
	mustRunCLI(t, "install")
	mustRunCLI(t, "uninstall", "./pkg")
	const want = `lockfile_version: '1'
apm_version: dev
dependencies: []
deployments:
- kind: uri
  target: mcp
  value: rootsrv
  runtime: claude
  scope: project
  owners:
  - .
  active_owner: .
  content_hash: null
mcp_servers:
- rootsrv
mcp_configs:
  rootsrv:
    name: rootsrv
    transport: stdio
    registry: false
    command: echo
mcp_target_servers:
  claude:
  - rootsrv
`
	if got := readText(t, "apm.lock.yaml"); got != want {
		t.Fatalf("apm.lock.yaml:\n%s\nwant:\n%s", got, want)
	}
}
