package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestInstall_MCPSummary is the R13+R15 regression (prd.md/design.md §3):
// runInstall's deploy summary used to compute deployResult.MCPProvenance
// (server -> which target file(s) it landed in) purely for lockfile
// persistence, never printing a "server -> targets" breakdown, and the
// closing "Installed N dependencies" line never mentioned MCP servers at
// all even when this run configured one.
func TestInstall_MCPSummary(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	manifestYAML := `
name: test
version: "1.0.0"
target: [claude, codex]
dependencies:
  mcp:
    - name: e2e-server
      registry: false
      transport: stdio
      command: my-mcp-server
`
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	r, w, _ := os.Pipe()
	origStdout := os.Stdout
	os.Stdout = w
	err := runInstall(deps, false, true, "", nil, nil)
	os.Stdout = origStdout
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	stdout := buf.String()

	if err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	if !strings.Contains(stdout, "MCP servers configured:") {
		t.Errorf("expected an MCP servers configured section, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "e2e-server -> claude, codex") {
		t.Errorf("expected e2e-server aggregated across both targets on one line, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Installed 0 dependencies and 1 MCP server") {
		t.Errorf("expected the closing summary to mention 1 MCP server, got:\n%s", stdout)
	}
}

// TestInstall_MCPSummary_NoMCPServersOmitsMention keeps today's exact
// wording ("Installed N dependencies") when this run configured zero MCP
// servers -- the R15 addition must be silent, not "and 0 MCP servers".
func TestInstall_MCPSummary_NoMCPServersOmitsMention(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(".claude", 0755)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	r, w, _ := os.Pipe()
	origStdout := os.Stdout
	os.Stdout = w
	err := runInstall(deps, false, true, "", nil, nil)
	os.Stdout = origStdout
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	stdout := buf.String()

	if err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	if strings.Contains(stdout, "MCP server") {
		t.Errorf("expected no MCP server mention when none were configured, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Installed 0 dependencies") {
		t.Errorf("expected the unchanged summary wording, got:\n%s", stdout)
	}
}
