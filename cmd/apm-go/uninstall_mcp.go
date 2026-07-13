package main

import (
	"path/filepath"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
)

// computeUninstallStaleMCP is un-061: recomputes the full "new" MCP server
// name set the surviving apm.yml + apm_modules tree still declares after
// this uninstall, so applyUninstallPlan can diff it against
// lock.MCPServers's pre-uninstall value and reverse-remove whatever server
// dropped out as a side effect of removing an unrelated package (not just
// the standalone names the user explicitly targeted -- that's
// removeUninstallStandaloneMCP's job, un-064/065).
//
// This deliberately does NOT call deploy.Run to get a faithful answer --
// that would require re-resolving the remaining dependency graph, which is
// un-054's deferred Phase 2 (see the KNOWN LIMITATION note in
// applyUninstallPlan). Instead it approximates the same question directly
// from what's already on disk/in the manifest, mirroring deploy.Run's own
// depth split (internal/deploy/deploy.go:82-102) exactly:
//
//   - root: every dependencies.mcp/devDependencies.mcp entry in the
//     ORIGINAL parsed manifest m, minus the standalone names this uninstall
//     itself is removing (mcpNames, un-064/065). deploy.Run always deploys
//     every root-declared MCP entry regardless of Registry (collectMCPPrimitives
//     resolves registry-backed local entries too), so nothing here is
//     filtered by Registry.
//   - direct (depth==1, remainingRootKeys): every lockfile dependency that
//     is ALSO still a root apm.yml/devDependencies.apm entry after this
//     uninstall contributes ALL the MCP servers its own apm.yml declares --
//     self-defined AND registry-backed alike (deploy.go:82-88 calls
//     collectMCPPrimitives unconditionally on a direct dep's own servers,
//     resolving registry-backed entries too; nothing here is filtered by
//     Registry).
//   - transitive (depth>1): every OTHER lockfile dependency NOT in
//     removalKeys -- i.e. surviving but not a root key -- contributes
//     NOTHING. deploy.Run never auto-trusts a transitive dependency's own
//     MCP servers, self-defined or registry-backed (deploy.go:98-101 calls
//     collectTransitiveMCPDiagnostics instead, which never emits a
//     Primitive). A dependency with a missing or unparseable apm.yml simply
//     contributes nothing either way, matching loadDependencyMCP's own
//     lenience -- it never aborts the rest.
func computeUninstallStaleMCP(m *manifest.Manifest, lock *lockfile.Lockfile, mcpNames, removalKeys, remainingRootKeys map[string]bool) map[string]bool {
	newMCP := map[string]bool{}
	addRoot := func(servers []*manifest.MCPDependency) {
		for _, s := range servers {
			if !mcpNames[s.Name] {
				newMCP[s.Name] = true
			}
		}
	}
	addRoot(m.MCPServers)
	addRoot(m.MCPDevServers)

	if lock == nil {
		return newMCP
	}
	for i := range lock.Dependencies {
		dep := &lock.Dependencies[i]
		key := dep.UniqueKey()
		if removalKeys[key] || !remainingRootKeys[key] {
			// Removed outright, or surviving only as a transitive (depth>1)
			// dependency -- deploy.Run never deploys either shape's own MCP
			// servers on its behalf.
			continue
		}
		servers, _ := deploy.LoadDependencyMCP(key, filepath.Join("apm_modules", key))
		for _, s := range servers {
			newMCP[s.Name] = true
		}
	}
	return newMCP
}
