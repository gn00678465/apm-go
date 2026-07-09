package deploy

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/resolver"
)

type DepDeployResult struct {
	Files  []string
	Hashes map[string]string
}

// MCPProv records where one deployed MCP server entry came from (pr-001
// source attribution), for a merged config file that multiple deps -- or
// local -- may jointly contribute servers to.
type MCPProv struct {
	Server string
	Source string // "local" or "dependency:<key>"
	File   string
}

type DeployResult struct {
	PerDep        map[string]*DepDeployResult // key="" for local
	Diags         []string
	MCPFiles      map[string]string // relative path -> sha256 hash, for merged (multi-source) MCP config files
	MCPProvenance []MCPProv
}

// SkillFilter scopes a --skill name whitelist to the specific dependency
// key(s) it was requested for (install.go's positional package args). Only
// primitives whose DepKey is in DepKeys are subject to the Names whitelist;
// everything else -- local primitives, or any other already-declared
// dependency -- passes through untouched, regardless of whether its skill
// names happen to appear in Names.
type SkillFilter struct {
	Names   []string
	DepKeys []string
}

// hasSkillWildcard reports whether names contains the '*' RESET sentinel
// (install.md: "--skill '*' resets to install all skills"), mirroring
// Python's install.py (~1387-1393): any occurrence -- even mixed with other
// names, e.g. `--skill review --skill '*'` -- means "install ALL skills,"
// not "whitelist a skill literally named *".
func hasSkillWildcard(names []string) bool {
	for _, n := range names {
		if n == "*" {
			return true
		}
	}
	return false
}

// Run executes the full deploy pipeline: collect → resolve conflicts → deploy.
func Run(targets []string, projectDir string, m *manifest.Manifest, resolved *resolver.ResolutionResult, filter *SkillFilter) (*DeployResult, error) {
	var skillNames, skillDepKeys map[string]bool
	if filter != nil && len(filter.Names) > 0 && !hasSkillWildcard(filter.Names) {
		skillNames = make(map[string]bool, len(filter.Names))
		for _, s := range filter.Names {
			skillNames[s] = true
		}
		skillDepKeys = make(map[string]bool, len(filter.DepKeys))
		for _, k := range filter.DepKeys {
			skillDepKeys[k] = true
		}
	}
	// 1. Collect primitives in priority order
	var ordered []Primitive
	var mcpDiags []string

	// Local primitives first (req-pr-002: always win)
	locals := CollectLocalPrimitives(projectDir)
	ordered = append(ordered, locals...)

	localMCP, localMCPDiags := collectMCPPrimitives(m.MCPServers, "local", "")
	ordered = append(ordered, localMCP...)
	mcpDiags = append(mcpDiags, localMCPDiags...)

	// Direct deps in manifest declaration order (req-pr-003), deduplicated
	directKeys := make(map[string]bool)
	for _, dep := range m.ParsedDeps {
		key := DepRefKey(dep)
		if key == "" || directKeys[key] {
			continue
		}
		directKeys[key] = true
		modulePath := filepath.Join(projectDir, "apm_modules", key)
		prims := CollectDependencyPrimitives(key, modulePath)
		ordered = append(ordered, prims...)

		// direct (depth==1) self-defined MCP servers are auto-trusted.
		depServers, loadDiags := loadDependencyMCP(key, modulePath)
		depMCP, depMCPDiags := collectMCPPrimitives(depServers, "dependency:"+key, key)
		ordered = append(ordered, depMCP...)
		mcpDiags = append(mcpDiags, loadDiags...)
		mcpDiags = append(mcpDiags, depMCPDiags...)
	}

	// Transitive deps in lockfile sorted order (repo_url, virtual_path)
	if resolved != nil {
		transitive := sortedTransitiveDeps(resolved.Deps, directKeys)
		for _, dep := range transitive {
			modulePath := filepath.Join(projectDir, "apm_modules", dep.Key)
			prims := CollectDependencyPrimitives(dep.Key, modulePath)
			ordered = append(ordered, prims...)

			// transitive (depth>1) MCP servers are never auto-trusted (design §4).
			transitiveServers, loadDiags := loadDependencyMCP(dep.Key, modulePath)
			mcpDiags = append(mcpDiags, loadDiags...)
			mcpDiags = append(mcpDiags, collectTransitiveMCPDiagnostics(transitiveServers, dep.Key)...)
		}
	}

	// 2. Apply --skill filter before conflict resolution. Scoped to
	// skillDepKeys so it only suppresses unselected skills belonging to the
	// dependency (or dependencies) --skill was requested for -- local
	// primitives (DepKey == "") and any other already-declared dependency
	// are never affected.
	if skillNames != nil {
		var filtered []Primitive
		for _, p := range ordered {
			if p.Type == TypeSkills && skillDepKeys[p.DepKey] && !skillNames[p.Name] {
				continue
			}
			filtered = append(filtered, p)
		}
		ordered = filtered
	}

	// 3. Resolve conflicts
	winners, conflictDiags := ResolvePrimitives(ordered)

	// 4. Deploy to each target
	result := &DeployResult{
		PerDep: make(map[string]*DepDeployResult),
		Diags:  append(mcpDiags, conflictDiags...),
	}

	deployedSkills := make(map[string]bool)
	// Track which primitive first wrote each destination path, to warn on
	// fixed-path overwrites (e.g. multiple hook files -> .agents/hooks.json).
	writtenBy := make(map[string]string)

	for _, target := range targets {
		adapter, ok := Adapters[target]
		if !ok {
			continue
		}
		for _, p := range winners {
			if !adapterSupports(adapter, p.Type) {
				continue
			}

			files, err := adapter.DeployPrimitive(p, projectDir)
			if err != nil {
				result.Diags = append(result.Diags,
					fmt.Sprintf("deploy %s to %s failed: %v", p.Name, target, err))
				continue
			}

			// Deduplicate skill file writes across targets: most targets
			// converge on the same canonical .agents/skills/<name>/... path
			// (req-tg-003), so only count/hash each distinct path once per
			// primitive. This is file-level rather than "skip the whole
			// primitive" because claude also writes a target-specific extra
			// copy under .claude/skills/ that no other target produces --
			// skipping the call entirely (as before) would drop that extra
			// copy whenever another skill-supporting target ran first.
			if p.Type == TypeSkills {
				var deduped []string
				for _, f := range files {
					if deployedSkills[f] {
						continue
					}
					deployedSkills[f] = true
					deduped = append(deduped, f)
				}
				files = deduped
			}

			// Warn when a different primitive overwrites a path already written.
			for _, f := range files {
				if prev, ok := writtenBy[f]; ok && prev != p.Name {
					result.Diags = append(result.Diags, fmt.Sprintf(
						"%s %q overwrites %s already written by %q (single-file target)",
						p.Type, p.Name, f, prev))
				}
				writtenBy[f] = p.Name
			}

			if len(files) > 0 {
				depResult := result.PerDep[p.DepKey]
				if depResult == nil {
					depResult = &DepDeployResult{
						Hashes: make(map[string]string),
					}
					result.PerDep[p.DepKey] = depResult
				}
				depResult.Files = append(depResult.Files, files...)

				for _, f := range files {
					absPath := filepath.Join(projectDir, f)
					hash, err := lockfile.HashFileBytes(absPath)
					if err != nil {
						return nil, fmt.Errorf("hash deployed file %s: %w", f, err)
					}
					depResult.Hashes[f] = hash
				}
			}
		}
	}

	// 5. Write merged MCP config files, one shot per target (design.md §4) --
	// N servers merge into a single file, unlike the per-primitive copy above.
	var mcpWinners []Primitive
	mcpSourceByName := map[string]string{}
	for _, p := range winners {
		if p.Type == TypeMCP {
			mcpWinners = append(mcpWinners, p)
			mcpSourceByName[p.Name] = p.Source
		}
	}
	if len(mcpWinners) > 0 {
		for _, target := range targets {
			adapter, ok := Adapters[target]
			if !ok {
				continue
			}
			mcpAdapter, ok := adapter.(MCPTarget)
			if !ok {
				continue
			}
			files, written, mcpWriteDiags, err := mcpAdapter.WriteMCP(mcpWinners, projectDir)
			if err != nil {
				result.Diags = append(result.Diags, fmt.Sprintf("write mcp config for %s failed: %v", target, err))
				continue
			}
			result.Diags = append(result.Diags, mcpWriteDiags...)
			for _, f := range files {
				hash, err := lockfile.HashFileBytes(filepath.Join(projectDir, f))
				if err != nil {
					return nil, fmt.Errorf("hash mcp file %s: %w", f, err)
				}
				if result.MCPFiles == nil {
					result.MCPFiles = map[string]string{}
				}
				result.MCPFiles[f] = hash
				for _, name := range written {
					result.MCPProvenance = append(result.MCPProvenance, MCPProv{
						Server: name,
						Source: mcpSourceByName[name],
						File:   f,
					})
				}
			}
		}
	}

	// Sort deployed files within each dep for determinism
	for _, dr := range result.PerDep {
		sort.Strings(dr.Files)
	}

	return result, nil
}

// DepRefKey returns the unique dependency key (repo_url, or
// repo_url/virtual_path) for a manifest dependency reference, matching
// resolver.ResolvedDep.Key and Primitive.DepKey. Local/parent references
// have no dep key ("").
func DepRefKey(ref *manifest.DependencyReference) string {
	if ref.IsLocal || ref.IsParent {
		return ""
	}
	if ref.VirtualPath != "" {
		return ref.RepoURL + "/" + ref.VirtualPath
	}
	return ref.RepoURL
}

func sortedTransitiveDeps(deps []resolver.ResolvedDep, directKeys map[string]bool) []resolver.ResolvedDep {
	var transitive []resolver.ResolvedDep
	for _, d := range deps {
		if !directKeys[d.Key] {
			transitive = append(transitive, d)
		}
	}
	sort.Slice(transitive, func(i, j int) bool {
		ri := transitive[i].RepoURL
		rj := transitive[j].RepoURL
		if ri != rj {
			return ri < rj
		}
		return transitive[i].VirtualPath < transitive[j].VirtualPath
	})
	return transitive
}

func adapterSupports(adapter TargetAdapter, pt PrimitiveType) bool {
	for _, t := range adapter.SupportedTypes() {
		if t == pt {
			return true
		}
	}
	return false
}
