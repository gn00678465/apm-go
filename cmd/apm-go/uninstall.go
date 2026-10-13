package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/apm-go/apm/internal/ux"
	"github.com/apm-go/apm/internal/yamlcore"
	"github.com/spf13/cobra"
)

// uninstallOptions carries every `apm-go uninstall` flag (un-003: the flag
// set is exactly --dry-run, -v/--verbose, -g/--global, --help).
type uninstallOptions struct {
	DryRun  bool
	Verbose bool
	Global  bool
}

func uninstallCmd() *cobra.Command {
	var opts uninstallOptions

	cmd := &cobra.Command{
		Use:          "uninstall [packages...]",
		Short:        "Remove APM packages, their integrated files, and apm.yml entries",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUninstall(args, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "preview what would be removed without changing anything")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "print detailed removal information")
	cmd.Flags().BoolVarP(&opts.Global, "global", "g", false, "user-scope uninstall: remove packages from ~/.apm/ instead of the project directory")
	return cmd
}

// runUninstall is the un-001~un-103 CLI orchestration: it wires together the
// already-implemented, already-unit-tested primitives (resolveUninstallTargets,
// manifest.RemovePackagesFromManifest/RemoveMCPServersFromManifest,
// resolver.ActualOrphans, deploy.SafeRemoveModuleDir/RemoveDeployedFiles/
// RemoveMCPServersFromTargets, lockfile.RemoveKeys) into the actual command.
// Split into prepareUninstallPlan (pure computation, safe to reuse for both
// --dry-run and the real run) and applyUninstallPlan (every actual write).
func runUninstall(args []string, opts uninstallOptions) error {
	var deployDir string
	if opts.Global {
		var err error
		deployDir, err = enterGlobalScope()
		if err != nil {
			return err
		}
	}

	data, node, m, err := readUninstallManifest()
	if err != nil {
		return err
	}
	lock, lockNode, err := readUninstallLockfile()
	if err != nil {
		return err
	}

	// The transcript of the Oracle (commands/uninstall/cli.py:288,
	// engine.py:676-681), issue #50 ruling R10.
	ux.Progress(os.Stdout, "Uninstalling %d package(s)...", len(args))
	plan := prepareUninstallPlan(args, m, lock, opts.DryRun)
	for _, t := range plan.resolution.APMTargets {
		ux.Success(os.Stdout, "%s - found in apm.yml", plan.moduleKey(t))
	}
	for _, t := range plan.resolution.MCPTargets {
		ux.Success(os.Stdout, "%s - found in apm.yml", t.Name)
	}
	for _, nf := range plan.resolution.NotFound {
		printUninstallNotFound(nf)
	}
	for _, rej := range plan.resolution.SupplyChainRejected {
		ux.Error(os.Stderr, "%q: refused to remove -- registry resolved %q, which is not present in apm.lock.yaml (supply-chain guard)", rej.Name, rej.Canonical)
	}
	if len(plan.resolution.APMTargets) == 0 && len(plan.resolution.MCPTargets) == 0 {
		// un-013: every argument was some flavor of not-found -- no changes.
		ux.Info(os.Stdout, "No packages found in apm.yml to remove")
		return nil
	}

	if opts.DryRun {
		printUninstallDryRunPlan(plan)
		return nil
	}

	return applyUninstallPlan(plan, data, node, m, lock, lockNode, opts.Verbose, deployDir)
}

// uninstallPlan is prepareUninstallPlan's pure-computation result: everything
// runUninstall needs to decide what to do, before anything is written. Safe
// to compute for --dry-run too (design.md's step 8, orphan BFS, is
// deliberately done up front rather than after a --dry-run early return:
// un-080 requires the dry-run preview to include "the orphan BFS result").
type uninstallPlan struct {
	resolution *uninstallResolution
	// moduleKeyByIdentity is the apm_modules key of each matched target.
	moduleKeyByIdentity map[string]string
	removedIdentities   map[string]bool
	// removedModuleKeys is removedIdentities translated through
	// uninstallRemovalKey into the apm.lock.yaml / apm_modules key space --
	// identical for git/marketplace targets, "_local/<name>" for
	// local-path targets. Everything that touches the lockfile or
	// apm_modules must use this set; removedIdentities stays in
	// uninstallIdentity's matching space for the apm.yml splice and the
	// remaining-roots filter.
	removedModuleKeys map[string]bool
	mcpNames          map[string]bool
	orphans           map[string]bool
	allRemovalKeys    map[string]bool
}

// prepareUninstallPlan resolves args against m/lock and computes everything
// downstream of that resolution: the removal identity sets and transitive
// orphans (un-041's remainingRootKeys is computed directly from the
// already-parsed in-memory manifest, filtering out what's being removed,
// rather than by physically splicing apm.yml first and re-reading it back --
// both produce the identical key set). plan.orphans/allRemovalKeys are already
// corrected for CRITICAL #1's diamond-dependency false positive here (via
// reachableFromRemainingRoots), so --dry-run's preview and the real run's
// deletions operate on the identical, corrected orphan set. applyUninstallPlan
// re-applies the same reachability veto (idempotent) as a defence-in-depth
// guard immediately before deletion.
func prepareUninstallPlan(args []string, m *manifest.Manifest, lock *lockfile.Lockfile, dryRun bool) *uninstallPlan {
	resolution := resolveUninstallTargets(args, m, lock, defaultMarketplaceRegistryResolver, dryRun)

	removedIdentities := make(map[string]bool, len(resolution.APMTargets))
	removedModuleKeys := make(map[string]bool, len(resolution.APMTargets))
	moduleKeyByIdentity := make(map[string]string, len(resolution.APMTargets))
	for _, t := range resolution.APMTargets {
		removedIdentities[t.IdentityKey] = true
		key := uninstallRemovalKey(t.IdentityKey, lock)
		moduleKeyByIdentity[t.IdentityKey] = key
		removedModuleKeys[key] = true
	}
	mcpNames := make(map[string]bool, len(resolution.MCPTargets))
	for _, t := range resolution.MCPTargets {
		mcpNames[t.Name] = true
	}

	remainingRootKeys := uninstallRemainingRootKeys(m, removedIdentities, lock)
	orphans := resolver.ActualOrphans(lock, removedModuleKeys, remainingRootKeys)

	// CRITICAL #1 fix (applied here so --dry-run's preview matches the real
	// run): resolver.ActualOrphans walks LockedDep.ResolvedBy, a single-parent
	// field that can't represent a diamond dependency shared by two roots.
	// Re-verify every orphan candidate against the ACTUAL dependency graph
	// declared on disk by every surviving root (transitively) and veto any
	// still reachable. applyUninstallPlan re-applies this (idempotent) as a
	// defence-in-depth guard immediately before deletion.
	reachable := reachableFromRemainingRoots(remainingRootKeys, lock, ".")
	for k := range orphans {
		if reachable[k] {
			delete(orphans, k)
		}
	}

	allRemovalKeys := make(map[string]bool, len(removedModuleKeys)+len(orphans))
	for k := range removedModuleKeys {
		allRemovalKeys[k] = true
	}
	for k := range orphans {
		allRemovalKeys[k] = true
	}

	return &uninstallPlan{
		resolution:          resolution,
		moduleKeyByIdentity: moduleKeyByIdentity,
		removedIdentities:   removedIdentities,
		removedModuleKeys:   removedModuleKeys,
		mcpNames:            mcpNames,
		orphans:             orphans,
		allRemovalKeys:      allRemovalKeys,
	}
}

// moduleKey is the name the transcript gives a matched target: its
// apm_modules key, which is the Oracle's public label of a package.
func (p *uninstallPlan) moduleKey(t uninstallAPMTarget) string {
	return p.moduleKeyByIdentity[t.IdentityKey]
}

// uninstallRemovalKey translates a matched target's identity key (the
// uninstallIdentity space used to MATCH a PACKAGE argument against apm.yml)
// into the key space apm.lock.yaml and apm_modules actually use. Git and
// marketplace identities already ARE that key space and pass through
// unchanged. A local-path dependency's synthetic "local:<path>" matching key
// has no on-disk counterpart: install materializes the package under
// localModulesKey(<source>) == "_local/<name>" and records that key as the
// lock's repo_url. A relative path resolves against cwd, which is the project
// root throughout runUninstall exactly as it was throughout runInstall. A lock
// written before issue #50 names the package _local/<name>-<hash8> instead;
// when the lock has that entry and not the current one, it is the key.
func uninstallRemovalKey(identity string, lock *lockfile.Lockfile) string {
	path, isLocal := strings.CutPrefix(identity, "local:")
	if !isLocal {
		return identity
	}
	abs := resolveLocalSourceAbs(path)
	key := localModulesKey(abs)
	if lock != nil && lock.FindByKey(key) == nil {
		if legacy := legacyLocalModulesKey(abs); lock.FindByKey(legacy) != nil {
			return legacy
		}
	}
	return key
}

// applyUninstallPlan performs every actual write for a non-dry-run uninstall:
// apm.yml splice, apm_modules deletion, target deployed-file reversal,
// standalone-MCP reversal, transitive MCP stale-diff, and the lockfile
// update -- in that order (un-050's "collect deployed_files before mutating
// anything" already happened inside prepareUninstallPlan).
func applyUninstallPlan(plan *uninstallPlan, data []byte, node *yamllib.Node, m *manifest.Manifest, lock *lockfile.Lockfile, lockNode *yamllib.Node, verbose bool, deployDir string) error {
	// un-061: capture the pre-uninstall MCP server name set before anything
	// below mutates it (removeUninstallStandaloneMCP re-slices
	// lock.MCPServers in place further down, so this must be a real copy
	// taken before that call, not just a reference to the same backing
	// array).
	var oldMCP []string
	if lock != nil {
		oldMCP = append([]string(nil), lock.MCPServers...)
	}

	if err := writeUninstallManifest(data, node, plan.removedIdentities, plan.mcpNames); err != nil {
		return err
	}

	// CRITICAL #1 fix: resolver.ActualOrphans (via TransitiveOrphans) only
	// walks LockedDep.ResolvedBy, which records a single parent per
	// dependency -- it can't represent a diamond dependency shared by two
	// root packages, so a transitive dep whose ResolvedBy happens to record
	// the package being removed gets misclassified as an orphan even though
	// another surviving root still depends on it. Re-verify every orphan
	// candidate against the ACTUAL dependency graph declared on disk by
	// every surviving root (transitively), and veto any candidate that's
	// still reachable before anything is deleted.
	remainingRootKeys := uninstallRemainingRootKeys(m, plan.removedIdentities, lock)
	reachable := reachableFromRemainingRoots(remainingRootKeys, lock, ".")
	orphans := plan.orphans
	for k := range orphans {
		if reachable[k] {
			delete(orphans, k)
		}
	}

	allRemovalKeys := make(map[string]bool, len(plan.removedModuleKeys)+len(orphans))
	for k := range plan.removedModuleKeys {
		allRemovalKeys[k] = true
	}
	for k := range orphans {
		allRemovalKeys[k] = true
	}
	removeRoot := "."
	if deployDir != "" {
		removeRoot = deployDir
	}
	removedFiles, keptFiles, diags := removeUninstallDeployed(lock, allRemovalKeys, deployDir)
	cleaned := uninstallCleanedByOwner(lock, allRemovalKeys, removedFiles)

	removedModuleDirs, err := removeUninstallModuleDirs(allRemovalKeys)
	if err != nil {
		return err
	}
	for _, d := range diags {
		ux.Warn(os.Stderr, "%s", d)
	}
	if verbose && len(removedFiles) > 0 {
		items := make([]ux.Item, len(removedFiles))
		for i, f := range removedFiles {
			items[i] = ux.Item{Text: f}
		}
		ux.List(os.Stdout, items)
	}

	removeUninstallStandaloneMCP(plan.mcpNames, lock, deployDir)

	// un-061: transitive MCP stale-diff. removeUninstallStandaloneMCP above
	// only reverse-removes MCP servers the user named directly on the
	// command line (un-064/065). This additionally recomputes the full "new"
	// MCP server set the remaining apm.yml + apm_modules tree still
	// declares, and reverse-removes whatever dropped out of oldMCP as a side
	// effect of removing an unrelated package (mirrors Python's
	// _cleanup_stale_mcp). oldMCP empty (nil lockfile, or a pre-mcp_servers
	// lockfile) fails open here -- nothing to diff against, so nothing is
	// touched.
	if len(oldMCP) > 0 {
		newMCP := computeUninstallStaleMCP(m, lock, plan.mcpNames, allRemovalKeys, remainingRootKeys)
		var stale []string
		for _, name := range oldMCP {
			if !newMCP[name] {
				stale = append(stale, name)
			}
		}
		if len(stale) > 0 {
			sort.Strings(stale)
			for _, d := range deploy.RemoveMCPServersFromTargets(removeRoot, stale) {
				ux.Warn(os.Stderr, "%s", d)
			}
		}
		lock.MCPServers = sortedStringSet(newMCP)
	}

	// KNOWN LIMITATION (un-054, documented deviation): Phase 2 re-integration
	// is not implemented. RemoveDeployedFiles above only looks at the
	// REMOVED/orphaned deps' own deployed_files -- a path also contributed by
	// a still-installed dependency (e.g. two packages declaring the same
	// skill name, sharing the same target file) is deleted here with no
	// attempt to restore it. Python's engine.py re-walks every surviving
	// dependency's primitives and re-deploys them after Phase 1; the apm-go
	// equivalent would be re-running deploy.Run(targets, ".", m, <resolution
	// of the remaining graph>, nil) at this point, with a single package's
	// re-integration failure only warning (not aborting). Not implemented in
	// this pass: building that remaining-graph ResolutionResult faithfully
	// requires re-running dependency resolution, a bigger unit of work than
	// fits this orchestration pass. Every other piece of this pipeline is
	// still safe on its own (RemoveDeployedFiles's hash check keeps a
	// hand-edited shared file even without Phase 2); the residual risk is
	// narrowly scoped to a shared, unedited deployed file getting dropped
	// when it should have been restored.

	if err := writeUninstallLockfile(lock, lockNode, allRemovalKeys); err != nil {
		return err
	}

	printUninstallSummary(plan, orphans, removedModuleDirs, cleaned, keptFiles)
	return nil
}

// reachableFromRemainingRoots is CRITICAL #1's fix: computes every lockfile
// dependency key still reachable from remainingRootKeys by walking the
// ACTUAL dependency graph declared on disk (each key's own apm_modules/<key>/
// apm.yml, recursively), rather than trusting LockedDep.ResolvedBy -- which
// only records a single parent and silently loses a diamond dependency's
// other parent(s) whenever a later install/update overwrites it. Every key
// in remainingRootKeys is trivially reachable (they're the roots themselves,
// still directly declared in apm.yml); the BFS then follows each reachable
// key's own prod dependencies.apm entries, but only ever descends into a
// dependency that's actually present in the lockfile (an apm.yml entry with
// no matching lockfile entry has no deployed state to protect and isn't
// itself a valid BFS node). Used by applyUninstallPlan to veto any orphan
// candidate resolver.ActualOrphans proposed removing that is, in fact, still
// reachable this way.
func reachableFromRemainingRoots(remainingRootKeys map[string]bool, lock *lockfile.Lockfile, projectDir string) map[string]bool {
	reachable := make(map[string]bool, len(remainingRootKeys))
	queue := make([]string, 0, len(remainingRootKeys))
	for k := range remainingRootKeys {
		reachable[k] = true
		queue = append(queue, k)
	}
	if lock == nil {
		return reachable
	}
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		deps, _ := deploy.LoadDependencyDeps(key, filepath.Join(projectDir, "apm_modules", key))
		for _, depKey := range deps {
			if lock.FindByKey(depKey) == nil {
				continue
			}
			if !reachable[depKey] {
				reachable[depKey] = true
				queue = append(queue, depKey)
			}
		}
	}
	return reachable
}

// readUninstallManifest reads and parses apm.yml (un-100: missing apm.yml is
// a hard error, there is nothing to uninstall from).
func readUninstallManifest() (data []byte, node *yamllib.Node, m *manifest.Manifest, err error) {
	data, err = os.ReadFile("apm.yml")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil, fmt.Errorf("apm.yml not found; nothing to uninstall")
		}
		return nil, nil, nil, fmt.Errorf("read apm.yml: %w", err)
	}
	node, err = yamlcore.SafeLoad(data)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse apm.yml: %w", err)
	}
	m, _, err = manifest.ParseManifest(node)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("validate apm.yml: %w", err)
	}
	return data, node, m, nil
}

// readUninstallLockfile reads apm.lock.yaml if present. A missing lockfile is
// NOT an error (un-018): every downstream consumer of lock treats a nil
// *lockfile.Lockfile as "no provenance/anchor available" and fails open
// (no deployed_files to reverse-clean, no orphan/supply-chain data to use).
func readUninstallLockfile() (lock *lockfile.Lockfile, lockNode *yamllib.Node, err error) {
	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read apm.lock.yaml: %w", err)
	}
	lockNode, err = yamlcore.SafeLoad(lockData)
	if err != nil {
		return nil, nil, fmt.Errorf("parse apm.lock.yaml: %w", err)
	}
	lock, err = lockfile.ParseLockfile(lockNode)
	if err != nil {
		return nil, nil, fmt.Errorf("validate apm.lock.yaml: %w", err)
	}
	return lock, lockNode, nil
}

// uninstallRemainingRootKeys computes the identity-key set of every
// dependencies.apm/devDependencies.apm entry that is NOT being removed --
// un-041's remaining_deps, reused as-is by resolver.ActualOrphans. Reuses
// uninstallIdentity (uninstall_resolve.go) so this is always the exact same
// key space removedIdentities was built from.
func uninstallRemainingRootKeys(m *manifest.Manifest, removedIdentities map[string]bool, lock *lockfile.Lockfile) map[string]bool {
	remaining := map[string]bool{}
	addRemaining := func(deps []*manifest.DependencyReference) {
		for _, d := range deps {
			if k, ok := uninstallIdentity(d); ok && !removedIdentities[k] {
				remaining[uninstallRemovalKey(k, lock)] = true
			}
		}
	}
	addRemaining(m.ParsedDeps)
	addRemaining(m.ParsedDevDeps)
	return remaining
}

// collectUninstallDeployedProvenance gathers the deployed_files/
// deployed_file_hashes for every key in removalKeys (un-050: this must
// happen BEFORE apm.yml/lockfile are mutated, and is the only place this
// data comes from -- a nil lock or an unmatched key simply contributes
// nothing, never an error). Iterates removalKeys in sorted order (rather
// than raw map range, whose iteration order Go deliberately randomizes)
// so that merging DeployedHashes across multiple removal keys -- if two of
// them recorded a hash for the same target-relative path -- resolves
// deterministically instead of picking a different, random winner on every
// call.
func collectUninstallDeployedProvenance(lock *lockfile.Lockfile, removalKeys map[string]bool) (files []string, hashes map[string]string) {
	hashes = map[string]string{}
	if lock == nil {
		return nil, hashes
	}
	for _, key := range sortedStringSet(removalKeys) {
		dep := lock.FindByKey(key)
		if dep == nil {
			continue
		}
		files = append(files, dep.DeployedFiles...)
		for f, h := range dep.DeployedHashes {
			hashes[f] = h
		}
	}
	sort.Strings(files)
	return files, hashes
}

// removeUninstallDeployed deletes what the removed dependencies deployed and
// returns the lock paths that are gone, sorted, the files that stayed, and
// the diagnostics. A project deploy is copies and goes to
// deploy.RemoveDeployedFiles. A --global deploy is symlinks into the global
// project directory (the working directory here), so each dependency goes to
// deploy.RemoveStaleLinkedFiles, as the stale cleanup of `install --global`
// does: a symlink the deploy made is removed itself, nothing is deleted
// through a symlink, and one the user pointed elsewhere is kept. That
// function names a removed symlink once; every lock path at or below it is
// reported here, as the Oracle counts the files of a skill.
func removeUninstallDeployed(lock *lockfile.Lockfile, removalKeys map[string]bool, deployDir string) (removed, kept, diags []string) {
	if deployDir == "" {
		files, hashes := collectUninstallDeployedProvenance(lock, removalKeys)
		removed, kept, diags = deploy.RemoveDeployedFiles(".", files, hashes)
		sort.Strings(removed)
		return removed, kept, diags
	}
	if lock == nil {
		return nil, nil, nil
	}
	for _, key := range sortedStringSet(removalKeys) {
		dep := lock.FindByKey(key)
		if dep == nil {
			continue
		}
		source := filepath.Join(".", "apm_modules", filepath.FromSlash(key))
		gone, depDiags := deploy.RemoveStaleLinkedFiles(deployDir, ".", source, dep.DeployedFiles, dep.DeployedHashes, nil)
		diags = append(diags, depDiags...)
		for _, f := range dep.DeployedFiles {
			switch _, hashed := dep.DeployedHashes[f]; {
			case atOrBelowAny(gone, f):
				removed = append(removed, f)
			case hashed && pathExists(filepath.Join(deployDir, filepath.FromSlash(f))):
				kept = append(kept, f)
			}
		}
	}
	sort.Strings(removed)
	return removed, kept, diags
}

// atOrBelowAny reports whether the lock path f is one of paths or below one.
func atOrBelowAny(paths []string, f string) bool {
	p := normalizeDeployPath(f)
	for _, q := range paths {
		q = normalizeDeployPath(q)
		if p == q || strings.HasPrefix(p, q+"/") {
			return true
		}
	}
	return false
}

func pathExists(full string) bool {
	_, err := os.Stat(full)
	return err == nil
}

// writeUninstallManifest splices the removed apm/mcp entries out of apm.yml
// and writes the result. The two removal calls are structural byte-splice
// edits located by yaml.Node Line numbers computed at parse time -- if the
// apm call changes anything, a fresh parse is required before the mcp call
// runs, or its Line numbers (from the ORIGINAL node) would be stale against
// the already-edited bytes, corrupting the result when dependencies.apm and
// dependencies.mcp interleave in ways the two independent calls can't see
// past each other. Re-parsing (apm.yml is tiny) sidesteps that entirely.
func writeUninstallManifest(data []byte, node *yamllib.Node, removedIdentities, mcpNames map[string]bool) error {
	if len(removedIdentities) == 0 && len(mcpNames) == 0 {
		return nil
	}
	out := data
	if len(removedIdentities) > 0 {
		next, _, err := manifest.RemovePackagesFromManifest(out, node, removedIdentities)
		if err != nil {
			return fmt.Errorf("remove apm.yml dependency entries: %w", err)
		}
		out = next
	}
	if len(mcpNames) > 0 {
		mcpNode := node
		if len(removedIdentities) > 0 {
			var err error
			mcpNode, err = yamlcore.SafeLoad(out)
			if err != nil {
				return fmt.Errorf("reparse apm.yml: %w", err)
			}
		}
		next, _, err := manifest.RemoveMCPServersFromManifest(out, mcpNode, mcpNames)
		if err != nil {
			return fmt.Errorf("remove apm.yml mcp entries: %w", err)
		}
		out = next
	}
	if err := os.WriteFile("apm.yml", out, 0644); err != nil {
		return fmt.Errorf("write apm.yml: %w", err)
	}
	return nil
}

// removeUninstallModuleDirs deletes apm_modules/<key> for every removal key
// (un-030~032, direct targets and transitive orphans alike).
func removeUninstallModuleDirs(removalKeys map[string]bool) (removed map[string]bool, err error) {
	_, statErr := os.Stat("apm_modules")
	hadModules := statErr == nil
	removed = make(map[string]bool)
	for _, key := range sortedStringSet(removalKeys) {
		wasRemoved, rerr := deploy.SafeRemoveModuleDir(".", key)
		if rerr != nil {
			return removed, fmt.Errorf("remove apm_modules/%s: %w", key, rerr)
		}
		if wasRemoved {
			removed[key] = true
		}
	}
	// The Oracle removes the package directory and its empty parents below
	// apm_modules, and leaves apm_modules itself.
	if hadModules {
		if err := os.MkdirAll("apm_modules", 0o755); err != nil {
			return removed, fmt.Errorf("keep apm_modules: %w", err)
		}
	}
	return removed, nil
}

// uninstallCleanedByOwner counts the deployed files removed for each removed
// package, named as the Oracle names the owner of a file: a local package by
// its declared path, any other by its key.
func uninstallCleanedByOwner(lock *lockfile.Lockfile, removalKeys map[string]bool, removedFiles []string) []uninstallCleaned {
	if lock == nil {
		return nil
	}
	gone := make(map[string]bool, len(removedFiles))
	for _, f := range removedFiles {
		gone[f] = true
	}
	var out []uninstallCleaned
	for _, key := range sortedStringSet(removalKeys) {
		dep := lock.FindByKey(key)
		if dep == nil {
			continue
		}
		n := 0
		for _, f := range dep.DeployedFiles {
			if gone[f] {
				n++
				delete(gone, f)
			}
		}
		owner := key
		if dep.LocalPath != "" {
			owner = dep.LocalPath
		}
		if n > 0 {
			out = append(out, uninstallCleaned{owner: owner, files: n})
		}
	}
	return out
}

type uninstallCleaned struct {
	owner string
	files int
}

// removeUninstallStandaloneMCP reverse-removes every explicitly-targeted MCP
// server (un-019/064/065: `uninstall <mcp-name>` matched against
// dependencies.mcp, not a transitive-stale diff) from every target's config
// file, and drops its name from lock.MCPServers so a later uninstall's
// transitive-stale diff (un-061, deferred to 8b) doesn't see a stale name
// that's already gone from every target.
func removeUninstallStandaloneMCP(mcpNames map[string]bool, lock *lockfile.Lockfile, deployDir string) {
	if len(mcpNames) == 0 {
		return
	}
	root := "."
	if deployDir != "" {
		root = deployDir
	}
	names := sortedStringSet(mcpNames)
	diags := deploy.RemoveMCPServersFromTargets(root, names)
	for _, d := range diags {
		ux.Warn(os.Stderr, "%s", d)
	}
	if lock == nil || len(lock.MCPServers) == 0 {
		return
	}
	kept := lock.MCPServers[:0]
	for _, s := range lock.MCPServers {
		if !mcpNames[s] {
			kept = append(kept, s)
		}
	}
	lock.MCPServers = kept
}

// lockModeledTopKeys are the top-level keys of apm.lock.yaml that hold only
// what apm-go records itself.
var lockModeledTopKeys = map[string]bool{
	"lockfile_version": true, "generated_at": true, "apm_version": true,
	"dependencies": true, "deployments": true, "mcp_servers": true,
	"mcp_configs": true, "mcp_target_servers": true,
	"local_deployed_files": true, "local_deployed_file_hashes": true,
}

// lockHasUnmodeledKey reports whether the lock document has a top-level key
// outside lockModeledTopKeys. lockNode passed lockfile.ParseLockfile: a
// document whose root is a mapping.
func lockHasUnmodeledKey(lockNode *yamllib.Node) bool {
	root := lockNode.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if !lockModeledTopKeys[root.Content[i].Value] {
			return true
		}
	}
	return false
}

// writeUninstallLockfile removes removalKeys from lock.Dependencies and
// writes the result. The lock is deleted only when nothing is left in it: it
// stays while it records the project's own deployed files or an MCP server,
// as the Oracle's does (issue #50, item 6a), and while it has a top-level
// key apm-go does not model. The last is a deliberate deviation: the Oracle
// deletes that lock, and PRODUCT.md promises such keys survive.
func writeUninstallLockfile(lock *lockfile.Lockfile, lockNode *yamllib.Node, removalKeys map[string]bool) error {
	if lock == nil {
		return nil
	}
	lock.RemoveKeys(sortedStringSet(removalKeys))

	// mcp_configs and mcp_target_servers follow mcp_servers.
	kept := make(map[string]bool, len(lock.MCPServers))
	for _, name := range lock.MCPServers {
		kept[name] = true
	}
	for name := range lock.MCPConfigs {
		if !kept[name] {
			delete(lock.MCPConfigs, name)
		}
	}
	for target, servers := range lock.MCPTargetServers {
		var remaining []string
		for _, name := range servers {
			if kept[name] {
				remaining = append(remaining, name)
			}
		}
		if len(remaining) == 0 {
			delete(lock.MCPTargetServers, target)
		} else {
			lock.MCPTargetServers[target] = remaining
		}
	}

	if len(lock.Dependencies) == 0 && len(lock.LocalDeployedFiles) == 0 && len(lock.MCPServers) == 0 && len(lock.MCPTargetServers) == 0 && !lockHasUnmodeledKey(lockNode) {
		if err := os.Remove("apm.lock.yaml"); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove apm.lock.yaml: %w", err)
		}
		return nil
	}
	out, err := lockfile.WriteLockfile(lock, lockNode)
	if err != nil {
		return fmt.Errorf("serialize apm.lock.yaml: %w", err)
	}
	return writeLockfileBytes(out)
}

// printUninstallNotFound reports why one PACKAGE argument produced no
// uninstall target (un-013/017/081), warning and letting the rest of the
// command proceed.
func printUninstallNotFound(nf uninstallNotFound) {
	switch nf.Reason {
	case uninstallNotFoundDryRunSkipped:
		ux.Warn(os.Stderr, "%q: cannot preview with --dry-run (marketplace reference has no lockfile anchor); use owner/repo, or run without --dry-run", nf.Name)
	case uninstallNotFoundUnresolvable:
		if nf.Detail != "" {
			ux.Warn(os.Stderr, "%q: could not resolve (%s)", nf.Name, nf.Detail)
		} else {
			ux.Warn(os.Stderr, "%q: could not resolve", nf.Name)
		}
	default:
		ux.Warn(os.Stderr, "%q: not found in apm.yml", nf.Name)
	}
}

// printUninstallDryRunPlan previews an uninstall without writing: what would
// leave apm.yml, which apm_modules directories exist to remove, and which
// transitive dependencies would go with them. The lines are the Oracle's
// (commands/uninstall/engine.py:745-810); a matched MCP server, which the
// Oracle does not uninstall, gets a line of the same shape.
func printUninstallDryRunPlan(plan *uninstallPlan) {
	resolution := plan.resolution
	ux.Info(os.Stdout, "Dry run: Would remove %d package(s):", len(resolution.APMTargets)+len(resolution.MCPTargets))
	for _, t := range resolution.APMTargets {
		key := plan.moduleKey(t)
		ux.Info(os.Stdout, "  - %s from apm.yml", key)
		if _, err := os.Stat(filepath.Join("apm_modules", filepath.FromSlash(key))); err == nil {
			ux.Info(os.Stdout, "  - %s from apm_modules/", key)
		}
	}
	for _, t := range resolution.MCPTargets {
		ux.Info(os.Stdout, "  - %s from apm.yml", t.Name)
	}
	if len(plan.orphans) > 0 {
		ux.Info(os.Stdout, "  Transitive dependencies that would be removed:")
		for _, k := range sortedStringSet(plan.orphans) {
			ux.Info(os.Stdout, "    - %s", k)
		}
	}
	ux.Success(os.Stdout, "Dry run complete - no changes made")
}

// printUninstallSummary prints what an uninstall did, in the Oracle's words
// and order (commands/uninstall/engine.py:855-944, cli.py:495-506, :689-691):
// the directories removed from apm_modules, the deployed files cleaned for
// each package, the entries removed from apm.yml, and the closing count. The
// counts come from what was removed, not from what the lock listed.
func printUninstallSummary(plan *uninstallPlan, orphans, removedModuleDirs map[string]bool, cleaned []uninstallCleaned, keptFiles []string) {
	resolution := plan.resolution
	fromModules := 0
	for _, t := range resolution.APMTargets {
		key := plan.moduleKey(t)
		if removedModuleDirs[key] {
			fromModules++
			ux.Info(os.Stdout, "Removed %s from apm_modules/", key)
		} else {
			ux.Warn(os.Stdout, "Package %s not found in apm_modules/", key)
		}
	}
	for _, k := range sortedStringSet(orphans) {
		if removedModuleDirs[k] {
			ux.Info(os.Stdout, "Removed transitive dependency %s from apm_modules/", k)
		}
	}
	for _, c := range cleaned {
		ux.Info(os.Stdout, "Cleaned %d stale %s from %s", c.files, pluralWord(c.files, "file", "files"), c.owner)
	}
	if len(keptFiles) > 0 {
		ux.Warn(os.Stdout, "Kept %d deployed file(s) that were modified or are shared", len(keptFiles))
	}
	for _, t := range resolution.APMTargets {
		section := "dependencies.apm"
		if t.IsDev {
			section = "devDependencies.apm"
		}
		ux.Info(os.Stdout, "Removed %s from %s in apm.yml", plan.moduleKey(t), section)
	}
	for _, t := range resolution.MCPTargets {
		ux.Info(os.Stdout, "Removed %s from dependencies.mcp in apm.yml", t.Name)
	}
	removedPackages := len(resolution.APMTargets) + len(resolution.MCPTargets)
	apmYMLPath := "apm.yml"
	if abs, err := filepath.Abs("apm.yml"); err == nil {
		apmYMLPath = abs
	}
	ux.Success(os.Stdout, "Updated %s (removed %d package(s))", apmYMLPath, removedPackages)

	summary := fmt.Sprintf("Removed %d package(s) from apm.yml", removedPackages)
	if fromModules > 0 {
		summary += fmt.Sprintf(", Removed %d package(s) from apm_modules/", fromModules)
	}
	ux.Success(os.Stdout, "Uninstall complete: %s", summary)
}

// sortedStringSet returns the sorted keys of a string set, for deterministic
// output ordering.
func sortedStringSet(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
