package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/mcpregistry"
	"github.com/apm-go/apm/internal/yamlcore"
)

// collectMCPPrimitives converts a Manifest's MCP servers into deploy
// Primitives. Self-defined servers (registry:false) carry their own
// command/url and are used as-is. Registry-backed entries are resolved
// live against the MCP Registry v0.1 API (internal/mcpregistry), matching
// the Python original's behavior: a plain `apm install` deploys them too,
// not just `apm install --mcp NAME`. A resolution failure for one entry is
// a diagnostic, not a hard install failure -- consistent with how the rest
// of this deploy pipeline treats per-item errors.
func collectMCPPrimitives(servers []*manifest.MCPDependency, source, depKey string) ([]Primitive, []string) {
	var prims []Primitive
	var diags []string
	for _, s := range servers {
		if isSelfDefinedMCP(s) {
			prims = append(prims, Primitive{
				Name:   s.Name,
				Type:   TypeMCP,
				Source: source,
				DepKey: depKey,
				MCP:    s,
			})
			continue
		}
		dep, resolveDiags := resolveRegistryMCP(s)
		diags = append(diags, resolveDiags...)
		if dep == nil {
			continue
		}
		prims = append(prims, Primitive{
			Name:   dep.Name,
			Type:   TypeMCP,
			Source: source,
			DepKey: depKey,
			MCP:    dep,
		})
	}
	return prims, diags
}

// registryURLForMCPEntry resolves the registry base URL for a
// registry-backed entry: the entry's own registry: string (an explicit
// per-server override in apm.yml) takes precedence over MCP_REGISTRY_URL,
// which takes precedence over the client's built-in default -- the same
// precedence `apm install --mcp` uses for its --registry flag vs env.
func registryURLForMCPEntry(s *manifest.MCPDependency) string {
	if url, ok := s.Registry.(string); ok && url != "" {
		return mcpregistry.NormalizeBaseURL(url)
	}
	if env := os.Getenv("MCP_REGISTRY_URL"); env != "" {
		return mcpregistry.NormalizeBaseURL(env)
	}
	return ""
}

func resolveRegistryMCP(s *manifest.MCPDependency) (*manifest.MCPDependency, []string) {
	client, cerr := mcpregistry.NewClient(registryURLForMCPEntry(s))
	if cerr != nil {
		return nil, []string{fmt.Sprintf("mcp %q: %v; skipped", s.Name, cerr)}
	}
	dep, requiredHeaders, rerr := mcpregistry.ResolveDeployable(context.Background(), client, s.Name, s.Version, s.Transport)
	if rerr != nil {
		return nil, []string{fmt.Sprintf("mcp %q: %v; skipped", s.Name, rerr)}
	}
	var diags []string
	if len(requiredHeaders) > 0 {
		diags = append(diags, fmt.Sprintf(
			"mcp %q requires header(s) %s which apm-go does not resolve automatically; declare it with registry:false and an explicit headers: map if the server needs authentication",
			s.Name, strings.Join(requiredHeaders, ", ")))
	}
	return dep, diags
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

// LoadDependencyMCP is an exported wrapper around loadDependencyMCP for
// callers outside this package (cmd/apm's uninstall un-061 transitive MCP
// stale-diff needs the same "read a dependency's own apm.yml prod MCP
// servers" lenience rules) without duplicating its logic.
func LoadDependencyMCP(depKey, modulePath string) ([]*manifest.MCPDependency, []string) {
	return loadDependencyMCP(depKey, modulePath)
}

// LoadDependencyDeps reads a dependency's own apm.yml and returns the
// identity keys (DependencyReference.IdentityKey(), the same key space as
// LockedDep.UniqueKey()) of its own PROD dependencies.apm entries -- never
// devDependencies, matching deploy.Run's own depth split, which never
// follows a transitive dependency's devDependencies either. Mirrors
// loadDependencyMCP's lenience exactly: a missing apm.yml is "no
// dependencies" (nil, nil), and one that exists but fails to parse is a
// diagnostic, not an error. Used by cmd/apm's uninstall orchestration
// (reachableFromRemainingRoots) to walk the actual dependency graph declared
// on disk, rather than trusting LockedDep.ResolvedBy -- which only records a
// single parent and can't represent a diamond dependency shared by two root
// packages.
func LoadDependencyDeps(depKey, modulePath string) ([]string, []string) {
	data, err := os.ReadFile(filepath.Join(modulePath, "apm.yml"))
	if err != nil {
		return nil, nil
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, []string{fmt.Sprintf("deps: dependency %s has an unparseable apm.yml, skipping its dependencies: %v", depKey, err)}
	}
	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		return nil, []string{fmt.Sprintf("deps: dependency %s has an invalid apm.yml, skipping its dependencies: %v", depKey, err)}
	}
	var keys []string
	for _, d := range m.ParsedDeps {
		if k := d.IdentityKey(); k != "" {
			keys = append(keys, k)
		}
	}
	return keys, nil
}
