package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// TestRunInstall_MCP_DeploysAndRecordsLockfileHash is the AC10/step-5 E2E
// check: a self-defined MCP server declared in apm.yml is deployed to the
// active target's config file, and the merged file's hash lands in the
// lockfile's local_deployed_file_hashes (install.go's minimal, no-new-schema
// wiring of DeployResult.MCPFiles -- see design.md §6).
func TestRunInstall_MCP_DeploysAndRecordsLockfileHash(t *testing.T) {
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
	if err := runInstall(deps, false, true, "", nil, nil); err != nil {
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

	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("read apm.lock.yaml: %v", err)
	}
	node, err := yamlcore.SafeLoad(lockData)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		LocalDeployedFiles      []string          `yaml:"local_deployed_files"`
		LocalDeployedFileHashes map[string]string `yaml:"local_deployed_file_hashes"`
	}
	rootNode := node.Content[0]
	b, err := yamlMarshalNode(rootNode)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}

	foundFile := false
	for _, f := range doc.LocalDeployedFiles {
		if f == ".mcp.json" {
			foundFile = true
		}
	}
	if !foundFile {
		t.Errorf("local_deployed_files = %v, want to contain .mcp.json", doc.LocalDeployedFiles)
	}
	if doc.LocalDeployedFileHashes[".mcp.json"] == "" {
		t.Errorf("local_deployed_file_hashes[.mcp.json] missing, got: %v", doc.LocalDeployedFileHashes)
	}
}

func yamlMarshalNode(n *yaml.Node) ([]byte, error) {
	return yaml.Marshal(n)
}

// TestRunInstall_MCP_AntigravityExplicitTarget is the step-6 E2E check for
// AC10, using --target antigravity explicitly so the test doesn't depend on
// the tempdir's auto-detection state. This exercises the full CLI
// runInstall() pipeline (manifest parse -> resolve -> deploy -> lockfile
// write), unlike deploy_test.go's TestRun_MCP_AntigravityExplicitTargetEndToEnd
// which calls deploy.Run() directly.
func TestRunInstall_MCP_AntigravityExplicitTarget(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	manifestYAML := `
name: test
version: "1.0.0"
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
	if err := runInstall(deps, false, true, "antigravity", nil, nil); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	mcpPath := filepath.Join(dir, ".agents", "mcp_config.json")
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("expected .agents/mcp_config.json to be deployed: %v", err)
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
}

// TestRunInstall_MCP_OnlyChangeStillRewritesLockfile guards req-lk-005: an
// MCP-only edit (no dependency change) must still update local_deployed_file_hashes
// on the next install, not be treated as a no-op (IsSemanticEqual previously
// ignored LocalDeployedFiles/LocalDeployedHashes entirely -- see write.go).
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
	if err := runInstall(deps, false, true, "", nil, nil); err != nil {
		t.Fatalf("first runInstall: %v", err)
	}
	firstHash, err := lockfile.HashFileBytes(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Change only the MCP server command. There are no apm dependencies, so
	// the lockfile rewrite must be driven by local_deployed_file_hashes.
	writeManifest("my-mcp-server-v2")
	if err := runInstall(deps, false, true, "", nil, nil); err != nil {
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
	node, err := yamlcore.SafeLoad(lockData)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		LocalDeployedFileHashes map[string]string `yaml:"local_deployed_file_hashes"`
	}
	b, err := yamlMarshalNode(node.Content[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}

	if doc.LocalDeployedFileHashes[".mcp.json"] != secondHash {
		t.Errorf("lockfile local_deployed_file_hashes[.mcp.json] = %q, want %q (the second install's hash) -- lockfile was not rewritten after an MCP-only change",
			doc.LocalDeployedFileHashes[".mcp.json"], secondHash)
	}
}
