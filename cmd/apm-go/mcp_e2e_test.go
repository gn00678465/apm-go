package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// TestRunInstall_MCP_DeploysAndRecordsLockState is the AC10/step-5 E2E
// check: a self-defined MCP server declared in apm.yml is deployed to the
// active target's config file and recorded in the lock.
func TestRunInstall_MCP_DeploysAndRecordsLockState(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	manifestYAML := `
name: test
version: "1.0.0"
target: [claude]
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
	if err := runInstall(deps, false, true, "", "", nil, nil); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	mcpPath := filepath.Join(dir, ".mcp.json")
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("expected .mcp.json to be deployed: %v", err)
	}
	var mcpRoot map[string]any
	if err := json.Unmarshal(data, &mcpRoot); err != nil {
		t.Fatal(err)
	}
	servers, ok := mcpRoot["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers missing: %v", mcpRoot)
	}
	if _, ok := servers["e2e-server"]; !ok {
		t.Errorf("e2e-server not deployed: %v", servers)
	}

	// The lock records the server the way the Oracle does (mcp_servers,
	// mcp_configs, mcp_target_servers); the config file is not a deployed
	// file of the project.
	lock := readLockfile(t)
	if !reflect.DeepEqual(lock.MCPServers, []string{"e2e-server"}) {
		t.Errorf("mcp_servers = %v, want [e2e-server]", lock.MCPServers)
	}
	if !reflect.DeepEqual(lock.MCPTargetServers, map[string][]string{"claude": {"e2e-server"}}) {
		t.Errorf("mcp_target_servers = %v, want claude: [e2e-server]", lock.MCPTargetServers)
	}
	if lock.MCPConfigs["e2e-server"] == nil {
		t.Errorf("mcp_configs has no e2e-server entry: %v", lock.MCPConfigs)
	}
	if len(lock.LocalDeployedFiles) != 0 {
		t.Errorf("local_deployed_files = %v, want none", lock.LocalDeployedFiles)
	}
}

// TestRunInstall_MCP_OnlyChangeStillRewritesLockfile guards req-lk-005: an
// MCP-only edit (no dependency change) must still update the lock on the
// next install, not be treated as a no-op. The lock holds the server's
// configuration in mcp_configs.
func TestRunInstall_MCP_OnlyChangeStillRewritesLockfile(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	writeManifest := func(command string) {
		manifestYAML := `
name: test
version: "1.0.0"
target: [claude]
dependencies:
  mcp:
    - name: e2e-server
      registry: false
      transport: stdio
      command: ` + command + `
`
		if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
			t.Fatal(err)
		}
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	writeManifest("my-mcp-server")
	if err := runInstall(deps, false, true, "", "", nil, nil); err != nil {
		t.Fatalf("first runInstall: %v", err)
	}
	firstHash, err := lockfile.HashFileBytes(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Change only the MCP server command. There are no apm dependencies, so
	// the lockfile rewrite must be driven by mcp_configs.
	writeManifest("my-mcp-server-v2")
	if err := runInstall(deps, false, true, "", "", nil, nil); err != nil {
		t.Fatalf("second runInstall: %v", err)
	}
	secondHash, err := lockfile.HashFileBytes(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if firstHash == secondHash {
		t.Fatal("expected .mcp.json content hash to change after editing the MCP command")
	}

	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("read apm.lock.yaml: %v", err)
	}
	if !strings.Contains(string(lockData), "    command: my-mcp-server-v2\n") {
		t.Errorf("mcp_configs still holds the first command -- lockfile was not rewritten after an MCP-only change:\n%s", lockData)
	}
}

// TestRunInstall_MCP_RecordsMCPServersInLockfile is the un-060 prerequisite
// E2E check: installing a manifest with a self-defined MCP server records
// that server's name in the lockfile's mcp_servers list, so a later
// uninstall has an "old" name list to diff against (design.md N3).
func TestRunInstall_MCP_RecordsMCPServersInLockfile(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	manifestYAML := `
name: test
version: "1.0.0"
target: [claude]
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
	if err := runInstall(deps, false, true, "", "", nil, nil); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("read apm.lock.yaml: %v", err)
	}
	node, err := yamlcore.SafeLoad(lockData)
	if err != nil {
		t.Fatal(err)
	}
	lf, err := lockfile.ParseLockfile(node)
	if err != nil {
		t.Fatalf("ParseLockfile: %v", err)
	}

	found := false
	for _, s := range lf.MCPServers {
		if s == "e2e-server" {
			found = true
		}
	}
	if !found {
		t.Errorf("lockfile MCPServers = %v, want to contain e2e-server", lf.MCPServers)
	}
}
