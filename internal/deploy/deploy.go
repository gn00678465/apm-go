package deploy

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"

	"github.com/apm-go/apm/internal/archive"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/apm-go/apm/internal/yamlcore"
	"go.yaml.in/yaml/v4"
)

type DepDeployResult struct {
	Files  []string
	Hashes map[string]string
	// Overwritten holds each path of Files that a later bucket of the Run
	// wrote again, a single-file target such as .codex/hooks.json: the
	// content on disk is that bucket's, so only that bucket records it.
	Overwritten map[string]bool
}

// Recorded returns Files and Hashes without Overwritten: what the lock
// records for the bucket.
func (d *DepDeployResult) Recorded() ([]string, map[string]string) {
	if len(d.Overwritten) == 0 {
		return d.Files, d.Hashes
	}
	var files []string
	hashes := make(map[string]string, len(d.Hashes))
	for _, f := range d.Files {
		if !d.Overwritten[f] {
			files = append(files, f)
		}
	}
	for f, h := range d.Hashes {
		if !d.Overwritten[f] {
			hashes[f] = h
		}
	}
	return files, hashes
}

// DeclaredMCP is an MCP server as a direct dependency's apm.yml declares it
// and the name of that apm.yml's package, which apm.yml requires.
type DeclaredMCP struct {
	Config *manifest.MCPDependency
	Owner  string
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
	// DeclaredMCPServers names, sorted, every MCP server declared for this
	// run, taken before registry resolution: apm.yml's dependencies.mcp and
	// devDependencies.mcp, and the dependencies.mcp of each direct
	// dependency. A server whose lookup, resolution or write fails is still
	// here, so the old lock's record of it is not stale.
	DeclaredMCPServers []string
	// FailedBuckets holds the PerDep key of each bucket with a source that
	// could not be read or a primitive that failed to deploy. Such a bucket's
	// file list is incomplete, so a path missing from it is not evidence that
	// the source is gone.
	FailedBuckets map[string]bool
	// KeptDirs holds the deploy-root-relative path of each real skill
	// directory a symlink deploy left in place because it holds content the
	// package does not have. Nothing below one is this run's deploy output,
	// and nothing below one is stale output of an earlier run either.
	KeptDirs []string
	// RejectedPaths holds, by the PerDep key of the bucket that was refused,
	// the deploy-root-relative destination of each agent, command or prompt
	// not deployed because a symlink there leads out of its directory. The
	// path is the user's whichever package records it: no cleanup of this
	// run removes it, and each old lock record of it stays with its owner
	// while the file it names exists, as the Oracle keeps it
	// (integration/agent_integrator.py:204).
	RejectedPaths map[string][]string
	// SkippedDeps holds, in the order Run takes the dependencies, each one
	// whose `targets:` names none of the active targets: it deployed nothing.
	SkippedDeps []SkippedDep
	// CommandNotes holds, for each target in turn, what the deploy of a
	// prompt as a command has to tell, in the order the prompts deployed.
	CommandNotes []CommandNote
	// Conflicts holds the entries of Diags that name a primitive another
	// one of the same name shadowed. install reports Shadowed in their
	// place, as the Oracle's count of skipped files.
	Conflicts []string
	// MCPFailure is the registry failure (mcpregistry.IsFailure) of the
	// first registry-backed server, in declaration order, that the registry
	// refused or did not answer. Run then writes no MCP config file, as the
	// Oracle's install validates every server before it writes any
	// (integration/mcp_integrator_install.py:54-94); the packages deploy as
	// before.
	MCPFailure error
	// DependencyMCP holds, by name, each MCP server a direct dependency
	// declares, as its apm.yml declares it, and the package that declares
	// it. The first declaration wins and the project's own come first, so a
	// name the project declares is not here (integration/mcp_config_view.py:
	// 84-88,350).
	DependencyMCP map[string]DeclaredMCP
	// MCPDiags holds the entries of Diags about MCP servers. The Oracle
	// reports those in its MCP block, not among the diagnostics whose
	// absence install marks with an empty line (install/summary.py:115-118).
	MCPDiags []string
	// Shadowed holds, for each target, each dependency primitive that the
	// primitive of another dependency, or another of its own, kept off that
	// target by name.
	Shadowed []Shadowed
	// RootSkills holds, by DepKey, the skill of each package whose root is a
	// skill.
	RootSkills map[string]RootSkill
	// Adopted holds, by deploy-root-relative path, whether each agent,
	// prompt or instruction file the deploy wrote was, before it wrote it,
	// what the Oracle compares there and adopts (noteAdopted).
	Adopted map[string]bool
	// Unmanaged holds, by PerDep key, each path a project deploy left to the
	// user: a file or skill directory there that the lock before the run
	// does not record and that is not what the Oracle compares it with
	// (Managed). It is in no PerDep entry.
	Unmanaged map[string][]string
}

// RootSkill is the skill of a package whose root is a skill. Created is
// whether its directory on the first target by name that deploys it was
// absent before the Run: the Oracle's skill_created
// (integration/skill_integrator.py:1150-1156).
type RootSkill struct {
	Name    string
	Created bool
}

// Shadowed is a primitive of the dependency DepKey that lost its name on a
// target to Winner.
type Shadowed struct {
	DepKey string
	Type   PrimitiveType
	Name   string
	// Paths are the deploy-root-relative paths the target holds for the
	// winner: the file it wrote, or for a skill its directory.
	Paths []string
	// ManagedAs are Paths as the Oracle looks them up in its managed set
	// (oracleRels).
	ManagedAs []string
	// Same is whether the winner's file on disk equals what the Oracle
	// compares it with before it writes this primitive (shadowedContent,
	// skillDirsEqual).
	Same bool
	// Deployed is whether it equals what this primitive's deploy writes:
	// the Oracle's overwrite of a path it manages leaves the disk as it is.
	Deployed bool
	// Rule is a claude or antigravity rule, which the Oracle writes over a
	// file it does not manage (writesRule).
	Rule bool
}

// RejectedInstructionError is an instruction whose destination a symlink takes
// out of its directory. Run returns it before it writes anything: the Oracle
// fails the install on it (integration/instruction_integrator.py:237,440).
type RejectedInstructionError struct {
	DepKey string // "" for the project's own content
	Err    *archive.ContainmentError
}

func (e *RejectedInstructionError) Error() string { return e.Err.Error() }

func (e *RejectedInstructionError) Unwrap() error { return e.Err }

// SkippedDep is a dependency that deploys to no active target: the
// `targets:` of its entry names none (Targets), or, when ByPackage, the
// targets of its own apm.yml (Targets) name none of those its entry allows.
type SkippedDep struct {
	Key       string
	Targets   []string
	ByPackage bool
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

// RunOption changes what Run does beyond its arguments.
type RunOption func(*runOptions)

type runOptions struct {
	mcpTargets []string
	mcpSet     bool
	only       map[string]bool
}

// WithOnly has Run deploy the packages of only (by DepKey), the project's own
// content and every MCP server, and nothing of the other packages.
func WithOnly(only map[string]bool) RunOption {
	return func(o *runOptions) { o.only = only }
}

// WithMCPTargets has Run write the MCP servers to targets alone, not to every
// target it deploys to: a --global install leaves out the targets that take
// no MCP at user scope.
func WithMCPTargets(targets []string) RunOption {
	return func(o *runOptions) { o.mcpTargets, o.mcpSet = targets, true }
}

// Run executes the full deploy pipeline: collect → resolve conflicts → deploy.
// deployDir controls where target files are written (DeployPrimitive, WriteMCP,
// FinalizeBundles and their post-write hashes). When empty it defaults to
// projectDir, preserving the existing single-root behavior. projectDir is
// always used for reading primitives and apm_modules.
// global is a --global deploy: per-primitive deploys create symlinks to
// apm_modules sources instead of copying files, unless the process cannot
// make a symlink (canSymlink), when the whole Run copies, as the Oracle's
// user scope always does. Merged outputs (WriteMCP, FinalizeBundles) always
// write regardless of this flag.
// managed, for a project deploy, leaves the files the user put where a
// primitive deploys (Managed); nil writes over them.
func Run(targets []string, projectDir string, m *manifest.Manifest, resolved *resolver.ResolutionResult, filter *SkillFilter, deployDir string, global bool, managed *Managed, opts ...RunOption) (*DeployResult, error) {
	var o runOptions
	for _, opt := range opts {
		opt(&o)
	}
	mcpTargets := targets
	if o.mcpSet {
		mcpTargets = o.mcpTargets
	}
	if deployDir == "" {
		deployDir = projectDir
	}
	activeDeployMode.symlink = global && symlinkProbe()
	activeDeployMode.global, activeDeployMode.projectDir = global, projectDir
	activeDeployMode.adopted = map[string]bool{}
	if !global {
		activeDeployMode.unmanaged = newUnmanagedGuard(managed)
	}
	hookRoot := projectDir
	if home, err := os.UserHomeDir(); global && err == nil {
		hookRoot = home
	}
	activeDeployMode.hookPackage = projectHookPackage(hookRoot)
	// Links are rewritten as they read from the real deploy root, also by a
	// trial in a temporary directory, so the trial records the lock's hashes.
	linkRoot := deployDir
	if managed != nil && managed.Root != "" {
		linkRoot = managed.Root
	}
	activeDeployMode.links = newLinkRun(linkRoot, deployDir, global)
	defer func() {
		activeDeployMode.symlink, activeDeployMode.global, activeDeployMode.projectDir = false, false, ""
		activeDeployMode.adopted, activeDeployMode.unmanaged, activeDeployMode.hookPackage = nil, nil, ""
		activeDeployMode.links = nil
	}()
	// 1. Collect primitives in priority order
	var ordered []Primitive
	var mcpDiags []string

	var failedBuckets map[string]bool
	var sourceDiags []string
	noteSourceReadFailure := func(key, label string, f *sourceReadFailure) {
		if f == nil {
			return
		}
		if failedBuckets == nil {
			failedBuckets = make(map[string]bool)
		}
		failedBuckets[key] = true
		shown := f.path
		if rel, err := filepath.Rel(projectDir, f.path); err == nil {
			shown = filepath.ToSlash(rel)
		}
		sourceDiags = append(sourceDiags, fmt.Sprintf("%s: read %s failed: %v", label, shown, f.err))
	}

	// Local primitives first (req-pr-002: always win).
	locals := CollectScopeLocalPrimitives(projectDir, global)
	ordered = append(ordered, locals...)
	noteSourceReadFailure("", "<local .apm/>", localSourceReadFailure(projectDir, global))
	// packageNames names each source in a note: the project's own content
	// "_local", as the Oracle does.
	packageNames := map[string]string{"": "_local"}

	declaredMCP := map[string]bool{}
	noteDeclaredMCP := func(servers []*manifest.MCPDependency) {
		for _, s := range servers {
			declaredMCP[s.Name] = true
		}
	}
	// The project's devDependencies.mcp deploys like its dependencies.mcp; a
	// dependency's does not (Oracle integration/mcp_config_view.py:84-88).
	rootMCP := slices.Concat(m.MCPServers, m.MCPDevServers)
	noteDeclaredMCP(rootMCP)
	var mcpFailure error
	// Only the first declaration of a name is resolved, as the Oracle drops
	// the others before it looks a server up (integration/mcp_config_view.py:
	// 84-88,131-133). A later one gets a placeholder with no MCP config, which
	// loses the conflict for its diagnostic and is never written.
	resolvedMCP := map[string]bool{}
	collectMCP := func(servers []*manifest.MCPDependency, source, key string) {
		for _, s := range servers {
			if mcpFailure != nil {
				return
			}
			if resolvedMCP[s.Name] {
				ordered = append(ordered, Primitive{Name: s.Name, Type: TypeMCP, Source: source, DepKey: key})
				continue
			}
			resolvedMCP[s.Name] = true
			prims, diags, err := collectMCPPrimitives([]*manifest.MCPDependency{s}, source, key)
			if err != nil {
				mcpFailure = err
				return
			}
			ordered = append(ordered, prims...)
			mcpDiags = append(mcpDiags, diags...)
		}
	}
	collectMCP(rootMCP, "local", "")
	dependencyMCP := map[string]DeclaredMCP{}

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
	// targetSubsets maps a dependency's DepKey to the targets it may deploy
	// to; a dependency with no restriction has no entry.
	targetSubsets := make(map[string][]string)
	var skippedDeps []SkippedDep
	// noteTargetSubset narrows the active targets by the `targets:` of the
	// dependency's entry (subset), then by its apm.yml's own (declared), as
	// resolve_effective_package_targets does (install/target_filter.py:
	// 72-150): the package's own never adds a target.
	noteTargetSubset := func(key string, subset, declared []string) {
		if len(subset) == 0 && declared == nil {
			return
		}
		authorized := targets
		if len(subset) > 0 {
			targetSubsets[key] = subset
			authorized = slices.DeleteFunc(slices.Clone(targets), func(t string) bool { return !slices.Contains(subset, t) })
			if len(authorized) == 0 {
				skippedDeps = append(skippedDeps, SkippedDep{Key: key, Targets: subset})
				return
			}
		}
		if declared == nil {
			return
		}
		allowed := slices.DeleteFunc(slices.Clone(authorized), func(t string) bool { return !slices.Contains(declared, t) })
		if len(allowed) == len(authorized) {
			return
		}
		targetSubsets[key] = allowed
		if len(allowed) == 0 {
			skippedDeps = append(skippedDeps, SkippedDep{Key: key, Targets: declared, ByPackage: true})
		}
	}
	for _, dep := range directDeps {
		key := DepRefKey(dep)
		if key == "" || directKeys[key] {
			continue
		}
		directKeys[key] = true
		depCanonKeys[key] = CanonicalDepKey(dep)
		modulePath := filepath.Join(projectDir, "apm_modules", filepath.FromSlash(DepRefDir(dep)))
		prims, depServers, loadDiags, name, declared, err := collectDependencySources(key, modulePath, dep.Source == "registry")
		if err != nil {
			return nil, err
		}
		noteTargetSubset(key, dep.TargetSubset, declared)
		packageNames[key] = name
		ordered = append(ordered, prims...)
		noteSourceReadFailure(key, key, dependencySourceReadFailure(modulePath))

		// direct (depth==1) self-defined MCP servers are auto-trusted.
		for _, s := range depServers {
			if _, ok := dependencyMCP[s.Name]; !ok && !declaredMCP[s.Name] {
				dependencyMCP[s.Name] = DeclaredMCP{Config: s, Owner: name}
			}
		}
		noteDeclaredMCP(depServers)
		mcpDiags = append(mcpDiags, loadDiags...)
		collectMCP(depServers, "dependency:"+key, key)
	}

	// Transitive deps in lockfile sorted order (repo_url, virtual_path)
	if resolved != nil {
		transitive := sortedTransitiveDeps(resolved.Deps, directKeys)
		for _, dep := range transitive {
			modulePath := filepath.Join(projectDir, "apm_modules", filepath.FromSlash(dep.ModuleDir()))
			prims, transitiveServers, loadDiags, name, declared, err := collectDependencySources(dep.Key, modulePath, dep.Kind == resolver.KindRegistry)
			if err != nil {
				return nil, err
			}
			noteTargetSubset(dep.Key, dep.TargetSubset, declared)
			packageNames[dep.Key] = name
			ordered = append(ordered, prims...)
			noteSourceReadFailure(dep.Key, dep.Key, dependencySourceReadFailure(modulePath))

			// transitive (depth>1) MCP servers are never auto-trusted (design §4).
			mcpDiags = append(mcpDiags, loadDiags...)
			mcpDiags = append(mcpDiags, collectTransitiveMCPDiagnostics(transitiveServers, dep.Key)...)
		}
	}

	// install <pkg> and update <pkg> integrate the named packages and the
	// project's own content alone, as the Oracle's only_packages does
	// (install/phases/integrate.py); a primitive of another package does
	// not take part in the conflicts either (measured, issue125 impl1/same1).
	if o.only != nil {
		ordered = slices.DeleteFunc(ordered, func(p Primitive) bool {
			return p.DepKey != "" && p.Type != TypeMCP && !o.only[p.DepKey]
		})
		skippedDeps = slices.DeleteFunc(skippedDeps, func(d SkippedDep) bool { return !o.only[d.Key] })
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

	// 3. Resolve conflicts. A dependency with `targets:` competes for a name
	// only on the targets it allows, so with any such dependency each
	// target gets its own winners, as in the Oracle (install/services.py:294).
	// A copilot hook competes only for its file (targetKey). The diagnostics
	// keep naming hooks of one name while another target deploys them by
	// name, and leave them out when copilot is the one target deploying
	// hooks.
	diagKey, copilotHooksOnly := nameKey, copilotOnlyHookTarget(targets)
	if copilotHooksOnly {
		diagKey = targetKey("copilot")
	}
	resolveFor := func(target string, prims []Primitive) ([]Primitive, []string) {
		w, diags := resolvePrimitivesBy(prims, diagKey)
		if target == "copilot" && !copilotHooksOnly {
			w, _ = resolvePrimitivesBy(prims, targetKey(target))
		}
		return w, diags
	}
	winners, conflictDiags := resolvePrimitivesBy(ordered, diagKey)
	winnersByTarget := make(map[string][]Primitive, len(targets))
	if len(targetSubsets) == 0 {
		for _, target := range targets {
			winnersByTarget[target], _ = resolveFor(target, ordered)
		}
	} else {
		conflictDiags = nil
		for _, target := range targets {
			var allowed []Primitive
			for _, p := range ordered {
				if subset, ok := targetSubsets[p.DepKey]; !ok || p.Type == TypeMCP || slices.Contains(subset, target) {
					allowed = append(allowed, p)
				}
			}
			w, diags := resolveFor(target, allowed)
			winnersByTarget[target] = w
			conflictDiags = append(conflictDiags, diags...)
		}
		conflictDiags = append(conflictDiags, keepFirstSharedSkill(targets, ordered, winnersByTarget)...)
		seenDiag := make(map[string]bool)
		var unique []string
		for _, d := range conflictDiags {
			if !seenDiag[d] {
				seenDiag[d] = true
				unique = append(unique, d)
			}
		}
		conflictDiags = unique
	}
	conflictDiags = appendUnique(conflictDiags, keepPromptOverCommand(targets, ordered, winnersByTarget)...)

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
		if err := bundleAdapter.ValidateBundleNames(bundleCandidateDepKeys(adapter, winnersByTarget[target])); err != nil {
			return nil, fmt.Errorf("%s: %w", target, err)
		}
	}

	if err := checkInstructions(targets, ordered, winnersByTarget, deployDir); err != nil {
		return nil, err
	}

	// 4. Deploy to each target
	allDiags := make([]string, 0, len(sourceDiags)+len(mcpDiags)+len(conflictDiags)+len(skillSubsetDiags))
	allDiags = append(allDiags, sourceDiags...)
	allDiags = append(allDiags, mcpDiags...)
	allDiags = append(allDiags, conflictDiags...)
	allDiags = append(allDiags, skillSubsetDiags...)
	result := &DeployResult{
		PerDep:        make(map[string]*DepDeployResult),
		Diags:         allDiags,
		FailedBuckets: failedBuckets,
		SkippedDeps:   skippedDeps,
		MCPDiags:      slices.Clone(mcpDiags),
		MCPFailure:    mcpFailure,
		DependencyMCP: dependencyMCP,
	}
	for name := range declaredMCP {
		result.DeclaredMCPServers = append(result.DeclaredMCPServers, name)
	}
	sort.Strings(result.DeclaredMCPServers)

	result.RootSkills = rootSkillsCreated(targets, winnersByTarget, deployDir)
	// written holds, by target, the files each primitive deployed there.
	written := make(map[string]map[primitiveID][]string, len(targets))

	deployedSkills := make(map[string]bool)
	// Track which primitive first wrote each destination path, to warn on
	// fixed-path overwrites (e.g. multiple hook files -> .agents/hooks.json).
	writtenBy := make(map[string]string)
	// lastBucket holds the PerDep key of the bucket that last wrote each path.
	lastBucket := make(map[string]string)
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
		for _, p := range winnersByTarget[target] {
			if !adapterSupports(adapter, p.Type) {
				continue
			}

			var files []string
			var notes []CommandNote
			var err error
			if c, ok := adapter.(promptCommander); ok && p.Type == TypePrompts {
				files, notes, err = deployPromptCommand(p, deployDir, c.commandPath(p.Name), target)
			} else {
				files, err = adapter.DeployPrimitive(p, deployDir)
			}
			for _, n := range notes {
				n.DepKey, n.Package = p.DepKey, packageNames[p.DepKey]
				result.CommandNotes = append(result.CommandNotes, n)
			}
			// The Oracle skips such a prompt with a warning and deploys the
			// rest (command_integrator.py:548-562,606-627). The reason text of
			// a frontmatter is apm-go's own.
			var fmErr *frontmatterError
			var nameErr *commandNameError
			switch {
			case errors.As(err, &fmErr):
				result.CommandNotes = append(result.CommandNotes, CommandNote{DepKey: p.DepKey, Package: packageNames[p.DepKey],
					Text: fmt.Sprintf("Skipped command %s.prompt.md: malformed or over-budget YAML frontmatter (%v).", p.Name, fmErr)})
				continue
			case errors.As(err, &nameErr):
				result.CommandNotes = append(result.CommandNotes, CommandNote{DepKey: p.DepKey, Package: packageNames[p.DepKey],
					Text: "Rejected command filename: " + nameErr.Error()})
				continue
			}
			// The Oracle skips such an agent, command or prompt with a
			// warning, and the user's symlink stays
			// (integration/agent_integrator.py:204, command_integrator.py:569,
			// prompt_integrator.py:299).
			var outside *archive.ContainmentError
			if kind, phase := rejectedKind(adapter, p); kind != "" && errors.As(err, &outside) {
				result.CommandNotes = append(result.CommandNotes, CommandNote{DepKey: p.DepKey, Package: packageNames[p.DepKey],
					Text: fmt.Sprintf("Rejected %s target path: %v", kind, outside), phase: phase})
				if root, absErr := filepath.Abs(deployDir); absErr == nil {
					if rel, relErr := filepath.Rel(root, outside.Path); relErr == nil {
						if result.RejectedPaths == nil {
							result.RejectedPaths = make(map[string][]string)
						}
						result.RejectedPaths[p.DepKey] = append(result.RejectedPaths[p.DepKey], filepath.ToSlash(rel))
					}
				}
				continue
			}
			if err != nil {
				var kept *keptSkillDirError
				if errors.As(err, &kept) {
					// Targets that share .agents/skills/<name> report the
					// same directory once each.
					if !slices.Contains(result.KeptDirs, kept.dir) {
						result.KeptDirs = append(result.KeptDirs, kept.dir)
						result.Diags = append(result.Diags, fmt.Sprintf("%v; skill %q not deployed", kept, p.Name))
					}
					continue
				}
				result.Diags = append(result.Diags,
					fmt.Sprintf("deploy %s to %s failed: %v", p.Name, target, err))
				if result.FailedBuckets == nil {
					result.FailedBuckets = make(map[string]bool)
				}
				result.FailedBuckets[p.DepKey] = true
				continue
			}

			// The Oracle warns only for an agent it writes, not one it adopts
			// or leaves to the user (integration/agent_integrator.py:263-286).
			if target == "opencode" && p.Type == TypeAgents && len(files) == 1 && !activeDeployMode.adopted[files[0]] {
				for _, n := range opencodeAgentNotes(p.SrcPath, filepath.Base(p.origin()), packageNames[p.DepKey]) {
					n.DepKey, n.Path = p.DepKey, files[0]
					result.CommandNotes = append(result.CommandNotes, n)
				}
			}

			if written[target] == nil {
				written[target] = make(map[primitiveID][]string)
			}
			written[target][idOf(p)] = files

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
				lastBucket[f] = p.DepKey
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
					hash, err := activeDeployMode.unmanaged.hash(deployDir, f)
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

	for key, dr := range result.PerDep {
		for _, f := range dr.Files {
			if lastBucket[f] != key {
				if dr.Overwritten == nil {
					dr.Overwritten = make(map[string]bool)
				}
				dr.Overwritten[f] = true
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
		filesByDep, err := bundleAdapter.FinalizeBundles(depKeys, deployDir)
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
				hash, err := lockfile.HashDeployedFile(filepath.Join(deployDir, f))
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
		if p.Type == TypeMCP && p.MCP != nil {
			mcpWinners = append(mcpWinners, p)
			mcpSourceByName[p.Name] = p.Source
		}
	}
	if len(mcpWinners) > 0 && mcpFailure == nil {
		for _, target := range mcpTargets {
			adapter, ok := Adapters[target]
			if !ok {
				continue
			}
			mcpAdapter, ok := adapter.(MCPTarget)
			if !ok {
				continue
			}
			files, written, mcpWriteDiags, err := mcpAdapter.WriteMCP(mcpWinners, deployDir)
			if err != nil {
				d := fmt.Sprintf("write mcp config for %s failed: %v", target, err)
				result.Diags = append(result.Diags, d)
				result.MCPDiags = append(result.MCPDiags, d)
				continue
			}
			result.Diags = append(result.Diags, mcpWriteDiags...)
			result.MCPDiags = append(result.MCPDiags, mcpWriteDiags...)
			for _, f := range files {
				hash, err := lockfile.HashDeployedFile(filepath.Join(deployDir, f))
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

	result.Conflicts = conflictDiags
	result.Adopted = activeDeployMode.adopted
	if g := activeDeployMode.unmanaged; g != nil {
		result.Unmanaged = g.skipped
	}
	result.Shadowed = shadowedPrimitives(targets, ordered, targetSubsets, winnersByTarget, written, deployDir)

	// Sort deployed files within each dep for determinism
	for _, dr := range result.PerDep {
		sort.Strings(dr.Files)
	}
	slices.SortStableFunc(result.CommandNotes, func(a, b CommandNote) int { return a.phase - b.phase })

	return result, nil
}

// checkInstructions runs, before the first write, the check each target's
// deploy of an instruction makes, and returns the first refusal: the
// dependencies first, in the order they were collected, then the project's
// own content, each target in turn, as the Oracle integrates them
// (install/phases/integrate.py:716). Any other error is the deploy's to
// report.
func checkInstructions(targets []string, ordered []Primitive, winnersByTarget map[string][]Primitive, deployDir string) error {
	var buckets []string
	seen := map[string]bool{"": true}
	for _, p := range ordered {
		if !seen[p.DepKey] {
			seen[p.DepKey] = true
			buckets = append(buckets, p.DepKey)
		}
	}
	for _, key := range append(buckets, "") {
		for _, target := range targets {
			checker, ok := Adapters[target].(instructionChecker)
			if !ok {
				continue
			}
			for _, p := range winnersByTarget[target] {
				if p.DepKey != key || p.Type != TypeInstructions {
					continue
				}
				var outside *archive.ContainmentError
				if errors.As(checker.checkInstruction(p, deployDir), &outside) {
					return &RejectedInstructionError{DepKey: key, Err: outside}
				}
			}
		}
	}
	return nil
}

// rejectedKind names a primitive the Oracle skips with a warning when a
// symlink takes its destination out of its directory, and the phase of its
// integration that warning comes in: prompts, agents, then commands, each
// over every target (install/services.py:416-424). A prompt a target deploys
// as a command is a command. Every other kind has "".
func rejectedKind(adapter TargetAdapter, p Primitive) (string, int) {
	switch p.Type {
	case TypePrompts:
		if _, ok := adapter.(promptCommander); ok {
			return "command", phaseCommands
		}
		return "prompt", phasePrompts
	case TypeAgents:
		return "agent", phaseAgents
	case TypeCommands:
		return "command", phaseCommands
	}
	return "", 0
}

// keepFirstSharedSkill makes a skill directory that several targets write
// (sharedSkillRoot) one package's: the first package in priority order that
// one of those targets deploys the skill from, as the Oracle keeps the first
// writer. Each other winner of that directory is dropped from its target, so
// the bytes on disk are the ones recorded for the package that owns them. It
// returns a conflict diagnostic per dropped winner.
func keepFirstSharedSkill(targets []string, ordered []Primitive, winnersByTarget map[string][]Primitive) []string {
	same := func(a, b Primitive) bool { return a.DepKey == b.DepKey && a.Source == b.Source }
	wins := func(target string, p Primitive) bool {
		for _, w := range winnersByTarget[target] {
			if w.Type == p.Type && w.Name == p.Name {
				return same(w, p)
			}
		}
		return false
	}
	owner := map[string]Primitive{}
	for _, p := range ordered {
		if p.Type != TypeSkills {
			continue
		}
		for _, target := range targets {
			r, ok := Adapters[target].(skillRooter)
			if !ok || !wins(target, p) {
				continue
			}
			dir := path.Join(r.skillRoot(p), p.Name)
			if _, taken := owner[dir]; !taken {
				owner[dir] = p
			}
		}
	}
	var diags []string
	for _, target := range targets {
		r, ok := Adapters[target].(skillRooter)
		if !ok {
			continue
		}
		var kept []Primitive
		for _, w := range winnersByTarget[target] {
			if w.Type == TypeSkills {
				if o := owner[path.Join(r.skillRoot(w), w.Name)]; !same(o, w) {
					diags = append(diags, fmt.Sprintf("%s %q from %s shadowed by %s (first-declared wins)", w.Type, w.Name, w.Source, o.Source))
					continue
				}
			}
			kept = append(kept, w)
		}
		winnersByTarget[target] = kept
	}
	return diags
}

// collectDependencySources reads the primitives, the MCP servers, the
// package name and the targets its apm.yml restricts it to (packageTargets)
// of the dependency vendored at modulePath, a registry package when
// registry. Every check of its symlinks runs here, before Run writes
// anything.
func collectDependencySources(key, modulePath string, registry bool) ([]Primitive, []*manifest.MCPDependency, []string, string, []string, error) {
	name := filepath.Base(modulePath)
	t, ok := dependencyTree(key, modulePath)
	if !ok {
		return nil, nil, nil, name, nil, nil
	}
	t.registry = registry
	if m, err := loadPluginManifest(t); err != nil {
		return nil, nil, nil, "", nil, err
	} else if m != nil {
		if err := checkDeclaredComponents(t.dir, m); err != nil {
			return nil, nil, nil, "", nil, fmt.Errorf("dependency %s: %w", key, err)
		}
	}
	prims, err := collectDependency(t)
	if err != nil {
		return nil, nil, nil, "", nil, err
	}
	data, err := readDependencyManifest(t)
	if err != nil {
		return nil, nil, nil, "", nil, err
	}
	servers, diags := loadDependencyMCP(key, data)
	if n := manifestName(data); n != "" {
		name = n
	}
	pluginName, err := claudePluginName(t)
	if err != nil {
		return nil, nil, nil, "", nil, err
	}
	if pluginName != "" {
		name = pluginName
	}
	return prims, servers, diags, name, packageTargets(data), nil
}

// manifestName is the name an apm.yml gives its package, "" when it gives
// none. The Oracle names a package with no apm.yml after its repository or
// directory (install/sources.py:466-472), which modulePath ends with.
func manifestName(data []byte) string {
	doc, err := yamlcore.SafeLoad(data)
	if err != nil || len(doc.Content) == 0 {
		return ""
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "name" && root.Content[i+1].Kind == yaml.ScalarNode {
			return root.Content[i+1].Value
		}
	}
	return ""
}

// keepPromptOverCommand gives a command path that a prompt and a command of
// .apm/commands/ (apm-go's own source, #88 item 67) both deploy to on a
// promptCommander target to one of them: the prompt within one package, else
// the first in priority order, as for two primitives of one type. It drops
// the other from that target's winners and returns a diagnostic for each
// one another package shadowed.
func keepPromptOverCommand(targets []string, ordered []Primitive, winnersByTarget map[string][]Primitive) []string {
	var diags []string
	for _, target := range targets {
		if _, ok := Adapters[target].(promptCommander); !ok {
			continue
		}
		winners := winnersByTarget[target]
		isWinner := func(p Primitive) bool {
			return slices.ContainsFunc(winners, func(w Primitive) bool {
				return w.Type == p.Type && w.Name == p.Name && w.Source == p.Source
			})
		}
		owner := map[string]Primitive{}
		for _, p := range ordered {
			if (p.Type != TypePrompts && p.Type != TypeCommands) || !isWinner(p) {
				continue
			}
			o, taken := owner[p.Name]
			if !taken || (o.Source == p.Source && p.Type == TypePrompts) {
				owner[p.Name] = p
			}
		}
		var kept []Primitive
		for _, w := range winners {
			if o, ok := owner[w.Name]; ok && (w.Type == TypePrompts || w.Type == TypeCommands) && o.Type != w.Type {
				if o.Source != w.Source {
					diags = append(diags, fmt.Sprintf("%s %q from %s shadowed by %s %q from %s", w.Type, w.Name, w.Source, o.Type, o.Name, o.Source))
				}
				continue
			}
			kept = append(kept, w)
		}
		winnersByTarget[target] = kept
	}
	return diags
}

func appendUnique(list []string, items ...string) []string {
	for _, item := range items {
		if !slices.Contains(list, item) {
			list = append(list, item)
		}
	}
	return list
}

// DepRefDir is the directory below apm_modules, slash-separated, of the
// dependency DepRefKey names: its alias, else that key. "" for a local or
// parent reference, which has no key.
func DepRefDir(ref *manifest.DependencyReference) string {
	if DepRefKey(ref) == "" {
		return ""
	}
	return ref.ModuleDir()
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
