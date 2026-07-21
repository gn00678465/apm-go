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
// local -- may jointly contribute servers to. Target names the adapter
// (e.g. "claude", "codex") that wrote File, so a caller can aggregate
// "server -> which targets got it" (R13) without re-deriving the target
// from File's path shape.
type MCPProv struct {
	Server string
	Source string // "local" or "dependency:<key>"
	File   string
	Target string
}

type DeployResult struct {
	PerDep        map[string]*DepDeployResult // key="" for local
	Diags         []string
	MCPFiles      map[string]string // relative path -> sha256 hash, for merged (multi-source) MCP config files
	MCPProvenance []MCPProv
}

// SkillFilter scopes a per-dependency --skill name whitelist (BUG-2, design
// §1.2b): Subsets maps a dependency's CanonicalDepKey to the list of skill
// names it may deploy. A dependency whose key is ABSENT from Subsets is
// unaffected -- "no entry" is the ONLY representation of "deploy every
// skill" (H6 invariant): a value must never be an empty slice. Only
// TypeSkills primitives belonging to a dependency present in Subsets are
// whitelisted; local primitives (no dep key) and any dependency absent from
// Subsets pass through untouched, regardless of whether their skill names
// happen to appear in some OTHER dependency's whitelist.
type SkillFilter struct {
	Subsets map[string][]string
}

// CanonicalDepKey returns the SkillFilter map key for ref: its
// manifest.CanonicalRepoIdentity plus a virtual-path suffix when set. A
// virtual-path sub-package within a monorepo is a distinct filterable
// dependency from its siblings even though they share a repository
// identity, so VirtualPath must be included on top of the bare identity
// (unlike DepRefKey, which composes the same way but from the RAW,
// case-sensitive RepoURL -- this is the case-fold-safe counterpart used by
// the --skill subset plumbing so a repo referenced with different case
// variants across calls resolves to the same filter entry). Returns "" for
// local/parent references (no stable identity), matching
// CanonicalRepoIdentity/DepRefKey.
func CanonicalDepKey(ref *manifest.DependencyReference) string {
	id := manifest.CanonicalRepoIdentity(ref)
	if id == "" {
		return ""
	}
	if ref.VirtualPath != "" {
		return id + "/" + ref.VirtualPath
	}
	return id
}

func skillNameInSubset(subset []string, name string) bool {
	for _, s := range subset {
		if s == name {
			return true
		}
	}
	return false
}

// Run executes the full deploy pipeline: collect → resolve conflicts → deploy.
func Run(targets []string, projectDir string, m *manifest.Manifest, resolved *resolver.ResolutionResult, filter *SkillFilter) (*DeployResult, error) {
	// 1. Collect primitives in priority order
	var ordered []Primitive
	var mcpDiags []string

	// Local primitives first (req-pr-002: always win)
	locals := CollectLocalPrimitives(projectDir)
	ordered = append(ordered, locals...)

	localMCP, localMCPDiags := collectMCPPrimitives(m.MCPServers, "local", "")
	ordered = append(ordered, localMCP...)
	mcpDiags = append(mcpDiags, localMCPDiags...)

	// Direct deps in manifest declaration order (req-pr-003), deduplicated.
	// Production dependencies.apm followed by devDependencies.apm (F3): a
	// dev dependency is a direct (depth-1) dependency exactly like a
	// production one -- same primitive priority, same MCP auto-trust --
	// mirroring Python's all_apm_deps = apm_deps + dev_apm_deps.
	directDeps := make([]*manifest.DependencyReference, 0, len(m.ParsedDeps)+len(m.ParsedDevDeps))
	directDeps = append(directDeps, m.ParsedDeps...)
	directDeps = append(directDeps, m.ParsedDevDeps...)

	// depCanonKeys maps each direct dependency's raw DepRefKey (the key every
	// Primitive.DepKey below actually carries) to its CanonicalDepKey, so the
	// --skill filter application below can look a primitive's dependency up
	// in filter.Subsets by canonical (case-fold-safe) identity without
	// requiring Primitive itself to carry the full DependencyReference.
	directKeys := make(map[string]bool)
	depCanonKeys := make(map[string]string, len(directDeps))
	for _, dep := range directDeps {
		key := DepRefKey(dep)
		if key == "" || directKeys[key] {
			continue
		}
		directKeys[key] = true
		depCanonKeys[key] = CanonicalDepKey(dep)
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

	// 2. Apply --skill filter before conflict resolution. A skill primitive
	// is only suppressed when its dependency's CanonicalDepKey has an entry
	// in filter.Subsets AND its name isn't in that entry's whitelist -- local
	// primitives (DepKey == "") and any dependency absent from Subsets
	// (transitive deps, or a direct dep with no persisted/requested subset)
	// are never affected.
	var skillSubsetDiags []string
	if filter != nil && len(filter.Subsets) > 0 {
		// H3 (design.md §1.2f): a PERSISTED subset entry naming a skill the
		// dependency no longer actually has (e.g. an upstream update dropped
		// it) is a non-fatal drift -- warn instead of silently losing the
		// name or failing the whole install. A brand-new name this call's
		// --skill flag introduces is validated separately, BEFORE this
		// function is ever called (cmd/apm-go's validateNewSkillNames),
		// which fails the whole install closed with zero writes -- so by the
		// time Run reaches here, any mismatch left is necessarily an
		// already-persisted name, never a fresh typo.
		availableByIdentity := make(map[string]map[string]bool, len(filter.Subsets))
		for _, p := range ordered {
			if p.Type != TypeSkills {
				continue
			}
			identity, ok := depCanonKeys[p.DepKey]
			if !ok || identity == "" {
				continue
			}
			if _, wanted := filter.Subsets[identity]; !wanted {
				continue
			}
			set := availableByIdentity[identity]
			if set == nil {
				set = make(map[string]bool)
				availableByIdentity[identity] = set
			}
			set[p.Name] = true
		}
		identities := make([]string, 0, len(filter.Subsets))
		for identity := range filter.Subsets {
			identities = append(identities, identity)
		}
		sort.Strings(identities)
		for _, identity := range identities {
			available := availableByIdentity[identity]
			names := append([]string(nil), filter.Subsets[identity]...)
			sort.Strings(names)
			for _, name := range names {
				if !available[name] {
					skillSubsetDiags = append(skillSubsetDiags, fmt.Sprintf(
						"skill %q persisted for %s no longer exists in the dependency -- keeping the persisted subset unchanged", name, identity))
				}
			}
		}

		var filtered []Primitive
		for _, p := range ordered {
			if p.Type == TypeSkills {
				if subset, ok := filter.Subsets[depCanonKeys[p.DepKey]]; ok && !skillNameInSubset(subset, p.Name) {
					continue
				}
			}
			filtered = append(filtered, p)
		}
		ordered = filtered
	}

	// 3. Resolve conflicts
	winners, conflictDiags := ResolvePrimitives(ordered)

	// 3.5. Bundle-target dependency naming must be validated for every
	// target BEFORE any primitive is deployed anywhere in this Run: a
	// bundle-directory name collision (e.g. two dependencies whose DepKey
	// sanitizes to the same antigravity plugin bundle name) fails closed --
	// nothing written for either dependency -- rather than let their files
	// silently mix into one physical directory (BundleTarget doc).
	for _, target := range targets {
		adapter, ok := Adapters[target]
		if !ok {
			continue
		}
		bundleAdapter, ok := adapter.(BundleTarget)
		if !ok {
			continue
		}
		if err := bundleAdapter.ValidateBundleNames(bundleCandidateDepKeys(adapter, winners)); err != nil {
			return nil, fmt.Errorf("%s: %w", target, err)
		}
	}

	// 4. Deploy to each target
	allDiags := make([]string, 0, len(mcpDiags)+len(conflictDiags)+len(skillSubsetDiags))
	allDiags = append(allDiags, mcpDiags...)
	allDiags = append(allDiags, conflictDiags...)
	allDiags = append(allDiags, skillSubsetDiags...)
	result := &DeployResult{
		PerDep: make(map[string]*DepDeployResult),
		Diags:  allDiags,
	}

	deployedSkills := make(map[string]bool)
	// Track which primitive first wrote each destination path, to warn on
	// fixed-path overwrites (e.g. multiple hook files -> .agents/hooks.json).
	writtenBy := make(map[string]string)
	// Track, per bundle-target, the ordered/deduplicated DepKeys that
	// actually produced at least one file this Run -- passed to
	// FinalizeBundles below.
	bundledDepsByTarget := make(map[string][]string)
	bundledDepsSeen := make(map[string]map[string]bool)

	for _, target := range targets {
		adapter, ok := Adapters[target]
		if !ok {
			continue
		}
		_, isBundleTarget := adapter.(BundleTarget)
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

			// Deduplicate skill file writes across targets: convergent
			// targets (codex/copilot/opencode/...) share the same canonical
			// .agents/skills/<name>/... path (req-tg-003), so only count/hash
			// each distinct path once per primitive. This is file-level
			// rather than "skip the whole primitive" because target-native
			// roots (claude's .claude/skills/) produce paths no other target
			// writes -- skipping the call entirely would drop those whenever
			// a convergent target ran first.
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

				if isBundleTarget && p.DepKey != "" {
					if bundledDepsSeen[target] == nil {
						bundledDepsSeen[target] = make(map[string]bool)
					}
					if !bundledDepsSeen[target][p.DepKey] {
						bundledDepsSeen[target][p.DepKey] = true
						bundledDepsByTarget[target] = append(bundledDepsByTarget[target], p.DepKey)
					}
				}
			}
		}
	}

	// 4.5. Finalize bundle-target manifests once per target, mirroring the
	// once-per-target MCP write below, for every dependency that actually
	// produced at least one bundled file this Run (e.g. antigravity's
	// plugin.json -- BundleTarget doc).
	for _, target := range targets {
		adapter, ok := Adapters[target]
		if !ok {
			continue
		}
		bundleAdapter, ok := adapter.(BundleTarget)
		if !ok {
			continue
		}
		depKeys := bundledDepsByTarget[target]
		if len(depKeys) == 0 {
			continue
		}
		filesByDep, err := bundleAdapter.FinalizeBundles(depKeys, projectDir)
		if err != nil {
			return nil, fmt.Errorf("finalize %s bundles: %w", target, err)
		}
		for depKey, files := range filesByDep {
			depResult := result.PerDep[depKey]
			if depResult == nil {
				depResult = &DepDeployResult{Hashes: make(map[string]string)}
				result.PerDep[depKey] = depResult
			}
			for _, f := range files {
				hash, err := lockfile.HashFileBytes(filepath.Join(projectDir, f))
				if err != nil {
					return nil, fmt.Errorf("hash bundle manifest %s: %w", f, err)
				}
				depResult.Files = append(depResult.Files, f)
				depResult.Hashes[f] = hash
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
						Target: target,
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

// bundleCandidateDepKeys returns the ordered, deduplicated DepKeys among
// prims that are non-local (DepKey != "") and whose Type this adapter
// supports -- the dependencies that WILL land at least one primitive under
// this adapter's bundle scheme, computed before any file is written so
// BundleTarget.ValidateBundleNames can fail closed up front.
func bundleCandidateDepKeys(adapter TargetAdapter, prims []Primitive) []string {
	seen := make(map[string]bool)
	var keys []string
	for _, p := range prims {
		if p.DepKey == "" || !adapterSupports(adapter, p.Type) {
			continue
		}
		if !seen[p.DepKey] {
			seen[p.DepKey] = true
			keys = append(keys, p.DepKey)
		}
	}
	return keys
}
