package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// TestRunUninstall_DirectDepRegistryBackedMCPServerSurvives is Bug A's main
// direction: deploy.Run always auto-trusts every MCP server a *direct*
// (depth==1) dependency declares in its own apm.yml -- self-defined AND
// registry-backed alike (collectMCPPrimitives resolves registry-backed
// entries too, internal/deploy/deploy.go:82-88). computeUninstallStaleMCP
// must treat a still-installed direct dependency's registry-backed server as
// "new" (kept), not stale, even though it isn't self-defined.
func TestRunUninstall_DirectDepRegistryBackedMCPServerSurvives(t *testing.T) {
	dir := chdirTemp(t)

	manifestYAML := "name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/pkgA\n    - acme/pkgB\n"
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}

	pkgAModDir := filepath.Join(dir, "apm_modules", "acme", "pkgA")
	if err := os.MkdirAll(pkgAModDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgAModDir, "apm.yml"), []byte("name: pkgA\nversion: \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pkgBModDir := filepath.Join(dir, "apm_modules", "acme", "pkgB")
	if err := os.MkdirAll(pkgBModDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// pkgB is a direct (root apm.yml) dependency declaring a registry-backed
	// MCP server (Registry != false) of its own.
	pkgBManifest := "name: pkgB\nversion: \"1.0.0\"\ndependencies:\n  mcp:\n    - name: srvR\n      registry: \"https://custom-registry.example.com\"\n"
	if err := os.WriteFile(filepath.Join(pkgBModDir, "apm.yml"), []byte(pkgBManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	mcpJSON := `{"mcpServers":{"srvR":{"type":"stdio","command":"srvR-server"}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(mcpJSON), 0644); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/pkgA", Source: "git"},
			{RepoURL: "acme/pkgB", Source: "git"},
		},
		MCPServers: []string{"srvR"},
	}
	writeUninstallLockfileFixture(t, lock)

	// Uninstall an unrelated package -- pkgB (and its registry-backed MCP
	// server) is untouched by this call.
	if err := runUninstall([]string{"acme/pkgA"}, uninstallOptions{}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	mcpData, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mcpRoot map[string]any
	if err := json.Unmarshal(mcpData, &mcpRoot); err != nil {
		t.Fatal(err)
	}
	servers := mcpRoot["mcpServers"].(map[string]any)
	if _, ok := servers["srvR"]; !ok {
		t.Errorf("expected pkgB's registry-backed MCP server srvR (a still-installed direct dependency's own server) to survive in .mcp.json, got %v", servers)
	}

	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("expected apm.lock.yaml to survive (acme/pkgB still locked): %v", err)
	}
	lockNode, _ := yamlcore.SafeLoad(lockData)
	newLock, err := lockfile.ParseLockfile(lockNode)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range newLock.MCPServers {
		if s == "srvR" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected lock.MCPServers to still contain srvR, got %v", newLock.MCPServers)
	}
}

// TestRunUninstall_TransitiveDepSelfDefinedMCPServerTreatedAsStale is Bug A's
// other direction: deploy.Run NEVER auto-trusts a *transitive* (depth>1)
// dependency's own MCP servers, self-defined or registry-backed alike
// (collectTransitiveMCPDiagnostics never emits a Primitive,
// internal/deploy/mcpcollect.go:90-100). computeUninstallStaleMCP must not
// treat a surviving transitive dependency's self-defined server as "new" --
// it was never something deploy.Run would deploy, so it must be reported
// stale (and reverse-removed) regardless of which unrelated package this
// uninstall call actually targets.
func TestRunUninstall_TransitiveDepSelfDefinedMCPServerTreatedAsStale(t *testing.T) {
	dir := chdirTemp(t)

	// acme/pkgA (root, survives) transitively resolves acme/pkgC (never a
	// root apm.yml entry). acme/pkgB (root, removed) is unrelated to both.
	manifestYAML := "name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/pkgA\n    - acme/pkgB\n"
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}

	pkgAModDir := filepath.Join(dir, "apm_modules", "acme", "pkgA")
	if err := os.MkdirAll(pkgAModDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgAModDir, "apm.yml"), []byte("name: pkgA\nversion: \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pkgBModDir := filepath.Join(dir, "apm_modules", "acme", "pkgB")
	if err := os.MkdirAll(pkgBModDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgBModDir, "apm.yml"), []byte("name: pkgB\nversion: \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pkgCModDir := filepath.Join(dir, "apm_modules", "acme", "pkgC")
	if err := os.MkdirAll(pkgCModDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pkgCManifest := "name: pkgC\nversion: \"1.0.0\"\ndependencies:\n  mcp:\n    - name: srvT\n      registry: false\n      transport: stdio\n      command: srvT-server\n"
	if err := os.WriteFile(filepath.Join(pkgCModDir, "apm.yml"), []byte(pkgCManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	mcpJSON := `{"mcpServers":{"srvT":{"type":"stdio","command":"srvT-server"}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(mcpJSON), 0644); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/pkgA", Source: "git"},
			{RepoURL: "acme/pkgB", Source: "git"},
			{RepoURL: "acme/pkgC", Source: "git", ResolvedBy: "acme/pkgA"},
		},
		MCPServers: []string{"srvT"},
	}
	writeUninstallLockfileFixture(t, lock)

	// Uninstall pkgB -- unrelated to pkgA/pkgC. pkgC is not an orphan of this
	// removal (its ResolvedBy is pkgA, which survives), so it stays installed
	// as a transitive dependency, still contributing srvT.
	if err := runUninstall([]string{"acme/pkgB"}, uninstallOptions{}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	if _, err := os.Stat(pkgCModDir); err != nil {
		t.Fatalf("expected acme/pkgC (transitive, not an orphan of this removal) to survive, stat err=%v", err)
	}

	mcpData, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mcpRoot map[string]any
	if err := json.Unmarshal(mcpData, &mcpRoot); err != nil {
		t.Fatal(err)
	}
	servers, _ := mcpRoot["mcpServers"].(map[string]any)
	if _, ok := servers["srvT"]; ok {
		t.Errorf("expected srvT (a transitive dependency's self-defined server, never auto-trusted by deploy.Run) to be reverse-removed as stale from .mcp.json, got %v", servers)
	}

	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("expected apm.lock.yaml to survive (acme/pkgA and acme/pkgC still locked): %v", err)
	}
	lockNode, _ := yamlcore.SafeLoad(lockData)
	newLock, err := lockfile.ParseLockfile(lockNode)
	if err != nil {
		t.Fatal(err)
	}
	if len(newLock.MCPServers) != 0 {
		t.Errorf("expected lock.MCPServers to no longer contain srvT, got %v", newLock.MCPServers)
	}
	if newLock.FindByKey("acme/pkgC") == nil {
		t.Error("expected acme/pkgC to remain locked as a surviving transitive dependency")
	}
}
