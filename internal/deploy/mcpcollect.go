package deploy

import (
	"context"
	"fmt"
	"os"
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
// not just `apm install --mcp NAME`. A server the registry does not have is
// a diagnostic; a registry that refuses or does not answer is the error
// (mcpregistry.IsFailure) that ends the MCP part of the install, returned
// at the first such server, as the Oracle validates every server before it
// writes one (integration/mcp_integrator_install.py:54-94).
func collectMCPPrimitives(servers []*manifest.MCPDependency, source, depKey string) ([]Primitive, []string, error) {
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
		dep, resolveDiags, err := resolveRegistryMCP(s)
		if err != nil {
			return nil, nil, err
		}
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
	return prims, diags, nil
}

// registryURLForMCPEntry resolves the registry base URL for a
// registry-backed entry and the setting it came from: the entry's own
// registry: string (an explicit per-server override in apm.yml) takes
// precedence over MCP_REGISTRY_URL, which takes precedence over the client's
// built-in default -- the same precedence `apm install --mcp` uses for its
// --registry flag vs env.
func registryURLForMCPEntry(s *manifest.MCPDependency) (string, mcpregistry.Source) {
	if url, ok := s.Registry.(string); ok && url != "" {
		return mcpregistry.NormalizeBaseURL(url), mcpregistry.SourceExplicit
	}
	if env := os.Getenv("MCP_REGISTRY_URL"); env != "" {
		return mcpregistry.NormalizeBaseURL(env), mcpregistry.SourceEnv
	}
	return "", mcpregistry.SourceDefault
}

func resolveRegistryMCP(s *manifest.MCPDependency) (*manifest.MCPDependency, []string, error) {
	client, cerr := mcpregistry.NewClientFor(registryURLForMCPEntry(s))
	if mcpregistry.IsFailure(cerr) {
		return nil, nil, cerr
	}
	if cerr != nil {
		return nil, []string{fmt.Sprintf("mcp %q: %v; skipped", s.Name, cerr)}, nil
	}
	dep, requiredHeaders, rerr := mcpregistry.ResolveDeployable(context.Background(), client, s.Name, s.Version, s.Transport)
	if mcpregistry.IsFailure(rerr) {
		return nil, nil, rerr
	}
	if rerr != nil {
		return nil, []string{fmt.Sprintf("mcp %q: %v; skipped", s.Name, rerr)}, nil
	}
	// The registry knows nothing of enabled; the Oracle overlays the apm.yml
	// value on the resolved server (integration/mcp_integrator.py:508).
	dep.Enabled = s.Enabled
	var diags []string
	if len(requiredHeaders) > 0 {
		diags = append(diags, fmt.Sprintf(
			"mcp %q requires header(s) %s which apm-go does not resolve automatically; declare it with registry:false and an explicit headers: map if the server needs authentication",
			s.Name, strings.Join(requiredHeaders, ", ")))
	}
	return dep, diags, nil
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

// readDependencyManifest returns the bytes of the dependency's own apm.yml,
// nil when there is none or it cannot be read. A symlink at it that leaves
// the package is an error.
func readDependencyManifest(t sourceTree) ([]byte, error) {
	p, err := t.path("apm.yml")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, nil
	}
	return data, nil
}

// readManifestLeniently is readDependencyManifest for a caller that must go on
// without the manifest: uninstall cannot be stopped by the package it removes,
// so an apm.yml it must not read is taken as one it cannot read.
func readManifestLeniently(depKey, modulePath string) []byte {
	t, ok := dependencyTree(depKey, modulePath)
	if !ok {
		return nil
	}
	data, _ := readDependencyManifest(t)
	return data
}

// loadDependencyMCP parses a dependency's own apm.yml, data, to collect its
// prod MCP servers. A missing apm.yml (nil data) is treated as "no MCP
// servers", matching the lenience CollectDependencyPrimitives already applies
// to missing module content. An apm.yml that exists but fails to parse is
// surfaced as a diagnostic (it should already be valid from install-time
// validation, so a failure here indicates a corrupted apm_modules tree).
func loadDependencyMCP(depKey string, data []byte) ([]*manifest.MCPDependency, []string) {
	if data == nil {
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
// callers outside this package (cmd/apm-go's uninstall un-061 transitive MCP
// stale-diff needs the same "read a dependency's own apm.yml prod MCP
// servers" lenience rules) without duplicating its logic.
func LoadDependencyMCP(depKey, modulePath string) ([]*manifest.MCPDependency, []string) {
	return loadDependencyMCP(depKey, readManifestLeniently(depKey, modulePath))
}

// LoadDependencyDeps reads a dependency's own apm.yml and returns the
// identity keys (DependencyReference.IdentityKey(), the same key space as
// LockedDep.UniqueKey()) of its own PROD dependencies.apm entries -- never
// devDependencies, matching deploy.Run's own depth split, which never
// follows a transitive dependency's devDependencies either. Mirrors
// loadDependencyMCP's lenience exactly: a missing apm.yml is "no
// dependencies" (nil, nil), and one that exists but fails to parse is a
// diagnostic, not an error. Used by cmd/apm-go's uninstall orchestration
// (reachableFromRemainingRoots) to walk the actual dependency graph declared
// on disk, rather than trusting LockedDep.ResolvedBy -- which only records a
// single parent and can't represent a diamond dependency shared by two root
// packages.
func LoadDependencyDeps(depKey, modulePath string) ([]string, []string) {
	data := readManifestLeniently(depKey, modulePath)
	if data == nil {
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
