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

type DeployResult struct {
	PerDep map[string]*DepDeployResult // key="" for local
	Diags  []string
}

// Run executes the full deploy pipeline: collect → resolve conflicts → deploy.
func Run(targets []string, projectDir string, m *manifest.Manifest, resolved *resolver.ResolutionResult, lock *lockfile.Lockfile) (*DeployResult, error) {
	// 1. Collect primitives in priority order
	var ordered []Primitive

	// Local primitives first (req-pr-002: always win)
	locals := CollectLocalPrimitives(projectDir)
	ordered = append(ordered, locals...)

	// Direct deps in manifest declaration order (req-pr-003)
	directKeys := make(map[string]bool)
	for _, dep := range m.ParsedDeps {
		key := depRefKey(dep)
		if key == "" {
			continue
		}
		directKeys[key] = true
		modulePath := filepath.Join(projectDir, "apm_modules", key)
		prims := CollectDependencyPrimitives(key, modulePath)
		ordered = append(ordered, prims...)
	}

	// Transitive deps in lockfile sorted order (repo_url, virtual_path)
	if resolved != nil {
		transitive := sortedTransitiveDeps(resolved.Deps, directKeys)
		for _, dep := range transitive {
			modulePath := filepath.Join(projectDir, "apm_modules", dep.Key)
			prims := CollectDependencyPrimitives(dep.Key, modulePath)
			ordered = append(ordered, prims...)
		}
	}

	// 2. Resolve conflicts
	winners, conflictDiags := ResolvePrimitives(ordered)

	// 3. Deploy to each target
	result := &DeployResult{
		PerDep: make(map[string]*DepDeployResult),
		Diags:  conflictDiags,
	}

	// Track already-deployed skills to avoid duplicate writes across targets
	deployedSkills := make(map[string]bool)

	for _, target := range targets {
		adapter, ok := Adapters[target]
		if !ok {
			continue
		}
		for _, p := range winners {
			if !adapterSupports(adapter, p.Type) {
				continue
			}

			// Deduplicate skill deployments across targets (same path)
			if p.Type == TypeSkills {
				skillPath := fmt.Sprintf(".agents/skills/%s/SKILL.md", p.Name)
				if deployedSkills[skillPath] {
					continue
				}
				deployedSkills[skillPath] = true
			}

			files, err := adapter.DeployPrimitive(p, projectDir)
			if err != nil {
				return nil, fmt.Errorf("deploy %s to %s: %w", p.Name, target, err)
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

				// Compute hashes for deployed files
				for _, f := range files {
					absPath := filepath.Join(projectDir, f)
					hash, err := lockfile.HashFileBytes(absPath)
					if err == nil {
						depResult.Hashes[f] = hash
					}
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

func depRefKey(ref *manifest.DependencyReference) string {
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
