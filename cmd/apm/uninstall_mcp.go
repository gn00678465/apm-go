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
// from what's already on disk/in the manifest:
//
//   - root: every dependencies.mcp/devDependencies.mcp entry in the
//     ORIGINAL parsed manifest m, minus the standalone names this uninstall
//     itself is removing (mcpNames, un-064/065). deploy.Run always deploys
//     every root-declared MCP entry regardless of Registry (collectMCPPrimitives
//     resolves registry-backed local entries too), so nothing here is
//     filtered by Registry.
//   - transitive: every lockfile dependency NOT in removalKeys, read via
//     deploy.LoadDependencyMCP against its own apm_modules/<key>/apm.yml,
//     counting only self-defined (Registry==false) servers -- the only MCP
//     servers deploy.Run ever auto-trusts for a dependency without a live
//     registry round-trip (collectMCPPrimitives/collectTransitiveMCPDiagnostics
//     in internal/deploy/mcpcollect.go). A dependency with a missing or
//     unparseable apm.yml simply contributes nothing, matching
//     loadDependencyMCP's own lenience -- it never aborts the rest.
func computeUninstallStaleMCP(m *manifest.Manifest, lock *lockfile.Lockfile, mcpNames, removalKeys map[string]bool) map[string]bool {
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
		if removalKeys[key] {
			continue
		}
		servers, _ := deploy.LoadDependencyMCP(key, filepath.Join("apm_modules", key))
		for _, s := range servers {
			if s.Registry == false {
				newMCP[s.Name] = true
			}
		}
	}
	return newMCP
}
