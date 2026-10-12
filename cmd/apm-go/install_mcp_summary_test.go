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
	err := runInstall(deps, false, true, "", "", nil, nil)
	os.Stdout = origStdout
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	stdout := buf.String()

	if err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	if !strings.Contains(stdout, "+- MCP Servers (1)\n") {
		t.Errorf("expected the MCP Servers section, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "e2e-server -> Claude, Codex (configured)") {
		t.Errorf("expected e2e-server aggregated across both targets on one line, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Installed 1 MCP server in ") {
		t.Errorf("expected the closing summary to mention 1 MCP server, got:\n%s", stdout)
	}
}

// TestInstall_MCPSummary_NoMCPServersOmitsMention: a run that configured zero
// MCP servers says nothing about them, not "and 0 MCP servers"; with nothing
// else to install the closing line is the Oracle's "No changes" one.
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
	err := runInstall(deps, false, true, "", "", nil, nil)
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
	if !strings.Contains(stdout, "No changes -- install state already up to date in ") {
		t.Errorf("expected the \"No changes\" closing line, got:\n%s", stdout)
	}
}
