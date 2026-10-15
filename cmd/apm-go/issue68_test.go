package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Issue #68: the enabled value of an MCP server reaches opencode.json and the
// lock's mcp_configs. The wanted texts are Oracle 0.33.0's output for the same
// apm.yml, with apm-go's status symbols.

const issue68Manifest = `name: g9
version: 1.0.0
targets: [%TARGET%]
dependencies:
  mcp:
    - name: example
      registry: false
      transport: http
      url: https://example.com/mcp
      enabled: %ENABLED%
`

func writeIssue68Manifest(t *testing.T, target, enabled string) {
	t.Helper()
	text := strings.NewReplacer("%TARGET%", target, "%ENABLED%", enabled).Replace(issue68Manifest)
	if err := os.WriteFile("apm.yml", []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runIssue68Install(t *testing.T) string {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = runInstall(&installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}, false, false, "", "", nil, nil)
	})
	if err != nil {
		t.Fatalf("runInstall: %v\n%s", err, out)
	}
	return elapsed86.ReplaceAllString(out, "in 0.0s.")
}

func issue68LockConfig(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "mcp_configs:\n")
	end := strings.Index(text, "mcp_target_servers:\n")
	if start < 0 || end < start {
		t.Fatalf("lock has no mcp_configs block:\n%s", text)
	}
	return text[start:end]
}

func issue68LockWant(enabled string) string {
	return "mcp_configs:\n  example:\n    name: example\n    transport: http\n    registry: false\n    url: https://example.com/mcp\n    enabled: " + enabled + "\n"
}

func TestInstall_Issue68_OpencodeEnabledFalseThenString(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)

	writeIssue68Manifest(t, "opencode", "false")
	runIssue68Install(t)
	data, err := os.ReadFile("opencode.json")
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		MCP map[string]map[string]json.RawMessage `json:"mcp"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if got := string(root.MCP["example"]["enabled"]); got != "false" {
		t.Errorf("opencode.json enabled = %s, want false\n%s", got, data)
	}
	if got := issue68LockConfig(t); got != issue68LockWant("false") {
		t.Errorf("lock mcp_configs =\n%s\nwant\n%s", got, issue68LockWant("false"))
	}

	writeIssue68Manifest(t, "opencode", `"false"`)
	out := runIssue68Install(t)
	if got := issue68LockConfig(t); got != issue68LockWant("'false'") {
		t.Errorf("lock mcp_configs =\n%s\nwant\n%s", got, issue68LockWant("'false'"))
	}
	if !strings.Contains(out, "|     +- Updating for Opencode...\n") || !strings.Contains(out, "example -> Opencode (updated)\n") {
		t.Errorf("a changed enabled type must be an update, got:\n%s", out)
	}
}

func TestInstall_Issue68_ClaudeEnabledFlipIsUpdate(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)

	writeIssue68Manifest(t, "claude", "false")
	runIssue68Install(t)
	if got := issue68LockConfig(t); got != issue68LockWant("false") {
		t.Errorf("lock mcp_configs =\n%s\nwant\n%s", got, issue68LockWant("false"))
	}

	writeIssue68Manifest(t, "claude", "true")
	out := runIssue68Install(t)
	want := ` > Installing dependencies from apm.yml...
+- MCP Servers (1)
 i Targeting declared target from apm.yml: claude
|   >  example (self-defined, http)
|     +- Updating for Claude...
Successfully configured MCP server 'example' for Claude Code
  + example
|   +  example -> Claude (updated)
 + Updated 1 server

 + Installed 1 MCP server in 0.0s.
`
	if out != want {
		t.Errorf("second install stdout =\n%s\nwant\n%s", out, want)
	}
	if got := issue68LockConfig(t); got != issue68LockWant("true") {
		t.Errorf("lock mcp_configs =\n%s\nwant\n%s", got, issue68LockWant("true"))
	}
}

// oracleIntArgsEnvLock is the lock Oracle 0.33.0 writes for issue68IntManifest.
// It keeps 8080 an integer where apm-go reads args and env as strings, so only
// enabled may be compared by type.
const oracleIntArgsEnvLock = `lockfile_version: '1'
apm_version: 0.33.0
dependencies: []
deployments:
- kind: uri
  target: mcp
  value: s
  runtime: claude
  scope: project
  owners:
  - .
  active_owner: .
  content_hash: null
mcp_servers:
- s
mcp_configs:
  s:
    name: s
    transport: stdio
    env:
      PORT: 8080
    args:
    - -p
    - 8080
    registry: false
    command: srv
mcp_target_servers:
  claude:
  - s
`

const issue68IntManifest = `name: g9
version: 1.0.0
targets: [claude]
dependencies:
  mcp:
    - name: s
      registry: false
      transport: stdio
      command: srv
      args: ["-p", 8080]
      env:
        PORT: 8080
`

func TestInstall_Issue68_OracleIntegerArgsStayConfigured(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	if err := os.WriteFile("apm.yml", []byte(issue68IntManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("apm.lock.yaml", []byte(oracleIntArgsEnvLock), 0o600); err != nil {
		t.Fatal(err)
	}

	out := runIssue68Install(t)
	if !strings.Contains(out, "|   + s (already configured)\n") || strings.Contains(out, "Updating") {
		t.Errorf("an unchanged server must stay configured, got:\n%s", out)
	}
	got, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != oracleIntArgsEnvLock {
		t.Errorf("lock changed:\n%s", got)
	}
}
