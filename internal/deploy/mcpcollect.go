package deploy

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

// collectMCPPrimitives converts a Manifest's MCP servers into deploy
// Primitives, diagnosing and skipping registry-backed entries (R8 --
// registry server-info resolution is out of scope for this task; only
// self-defined servers with registry:false carry deployable command/url).
func collectMCPPrimitives(servers []*manifest.MCPDependency, source, depKey string) ([]Primitive, []string) {
	var prims []Primitive
	var diags []string
	for _, s := range servers {
		if !isSelfDefinedMCP(s) {
			diags = append(diags, fmt.Sprintf("mcp %q: registry-backed servers are not deployed by this apm-go version; skipped", s.Name))
			continue
		}
		prims = append(prims, Primitive{
			Name:   s.Name,
			Type:   TypeMCP,
			Source: source,
			DepKey: depKey,
			MCP:    s,
		})
	}
	return prims, diags
}

// collectTransitiveMCPDiagnostics reports every MCP server declared by a
// transitive dependency without producing any Primitive: registry-backed
// entries are out of scope (R8), and self-defined entries from a transitive
// (depth>1) dependency are not auto-trusted -- only direct deps are.
func collectTransitiveMCPDiagnostics(servers []*manifest.MCPDependency, depKey string) []string {
	var diags []string
	for _, s := range servers {
		if !isSelfDefinedMCP(s) {
			diags = append(diags, fmt.Sprintf("mcp %q (transitive dep %s): registry-backed servers are not deployed; skipped", s.Name, depKey))
			continue
		}
		diags = append(diags, fmt.Sprintf("mcp %q (transitive dep %s): self-defined MCP servers from transitive dependencies are not auto-trusted; skipped", s.Name, depKey))
	}
	return diags
}

// isSelfDefinedMCP mirrors ValidateMCP's own check (mcp.go): only
// registry:false carries deployable command/url; nil (default registry) and
// a custom registry URL string are both registry-backed.
func isSelfDefinedMCP(s *manifest.MCPDependency) bool {
	return s.Registry == false
}

// loadDependencyMCP reads and parses a dependency's own apm.yml to collect
// its prod MCP servers. A missing apm.yml is treated as "no MCP servers",
// matching the lenience CollectDependencyPrimitives already applies to
// missing module content. An apm.yml that exists but fails to parse is
// surfaced as a diagnostic (it should already be valid from install-time
// validation, so a failure here indicates a corrupted apm_modules tree).
func loadDependencyMCP(depKey, modulePath string) ([]*manifest.MCPDependency, []string) {
	data, err := os.ReadFile(filepath.Join(modulePath, "apm.yml"))
	if err != nil {
		return nil, nil
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, []string{fmt.Sprintf("mcp: dependency %s has an unparseable apm.yml, skipping its MCP servers: %v", depKey, err)}
	}
	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		return nil, []string{fmt.Sprintf("mcp: dependency %s has an invalid apm.yml, skipping its MCP servers: %v", depKey, err)}
	}
	return m.MCPServers, nil
}
