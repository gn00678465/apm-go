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
	cmd.Flags().BoolVarP(&opts.Global, "global", "g", false, "user-scope (~/.apm/) uninstall -- not supported yet")
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
	if opts.Global {
		// un-090/091, definite A: apm-go has no InstallScope/user-scope
		// concept at all (install/update always operate on cwd-relative
		// apm.yml/apm.lock.yaml/apm_modules) -- report clearly rather than
		// silently ignoring -g or doing something unintended.
		return fmt.Errorf("user scope (-g/--global) is not supported yet; run apm-go uninstall from within the project directory")
	}

	data, node, m, err := readUninstallManifest()
	if err != nil {
		return err
	}
	lock, lockNode, err := readUninstallLockfile()
	if err != nil {
		return err
	}

	plan := prepareUninstallPlan(args, m, lock, opts.DryRun)
	for _, nf := range plan.resolution.NotFound {
		printUninstallNotFound(nf)
	}
	for _, rej := range plan.resolution.SupplyChainRejected {
		ux.Error(os.Stderr, "%q: refused to remove -- registry resolved %q, which is not present in apm.lock.yaml (supply-chain guard)", rej.Name, rej.Canonical)
	}
	if len(plan.resolution.APMTargets) == 0 && len(plan.resolution.MCPTargets) == 0 {
		// un-013: every argument was some flavor of not-found -- no changes.
		fmt.Println("No packages found in apm.yml to remove")
		return nil
	}

	if opts.DryRun {
		printUninstallDryRunPlan(plan.resolution, plan.allRemovalKeys, plan.orphans)
		return nil
	}

	return applyUninstallPlan(plan, data, node, m, lock, lockNode, opts.Verbose)
}

// uninstallPlan is prepareUninstallPlan's pure-computation result: everything
// runUninstall needs to decide what to do, before anything is written. Safe
// to compute for --dry-run too (design.md's step 8, orphan BFS, is
// deliberately done up front rather than after a --dry-run early return:
// un-080 requires the dry-run preview to include "the orphan BFS result").
type uninstallPlan struct {
	resolution        *uninstallResolution
	removedIdentities map[string]bool
	// removedModuleKeys is removedIdentities translated through
	// uninstallRemovalKey into the apm.lock.yaml / apm_modules key space --
	// identical for git/marketplace targets, "_local/<base>-<sha8>" for
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
	for _, t := range resolution.APMTargets {
		removedIdentities[t.IdentityKey] = true
		removedModuleKeys[uninstallRemovalKey(t.IdentityKey)] = true
	}
	mcpNames := make(map[string]bool, len(resolution.MCPTargets))
	for _, t := range resolution.MCPTargets {
		mcpNames[t.Name] = true
	}

	remainingRootKeys := uninstallRemainingRootKeys(m, removedIdentities)
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
		resolution:        resolution,
		removedIdentities: removedIdentities,
		removedModuleKeys: removedModuleKeys,
		mcpNames:          mcpNames,
		orphans:           orphans,
		allRemovalKeys:    allRemovalKeys,
	}
}

// uninstallRemovalKey translates a matched target's identity key (the
// uninstallIdentity space used to MATCH a PACKAGE argument against apm.yml)
// into the key space apm.lock.yaml and apm_modules actually use (ag-23
// defect fix). Git and marketplace identities already ARE that key space and
// pass through unchanged. A local-path dependency's synthetic
// "local:<path>" matching key has no on-disk counterpart: install
// (install.go's normalizeLocalDep) materializes the package under
// localModulesKey(resolveLocalSourceAbs(path)) == "_local/<base>-<sha8>"
// and records that same key as the lockfile repo_url -- so THAT is the key
// SafeRemoveModuleDir, lock.RemoveKeys, and the deployed-provenance lookup
// must receive. A relative path resolves against cwd, which is the project
// root throughout runUninstall exactly as it was throughout runInstall, so
// the same source path always re-derives the same key install produced.
func uninstallRemovalKey(identity string) string {
	path, isLocal := strings.CutPrefix(identity, "local:")
	if !isLocal {
		return identity
	}
	return localModulesKey(resolveLocalSourceAbs(path))
}

// applyUninstallPlan performs every actual write for a non-dry-run uninstall:
// apm.yml splice, apm_modules deletion, target deployed-file reversal,
// standalone-MCP reversal, transitive MCP stale-diff, and the lockfile
// update -- in that order (un-050's "collect deployed_files before mutating
// anything" already happened inside prepareUninstallPlan).
func applyUninstallPlan(plan *uninstallPlan, data []byte, node *yamllib.Node, m *manifest.Manifest, lock *lockfile.Lockfile, lockNode *yamllib.Node, verbose bool) error {
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
	remainingRootKeys := uninstallRemainingRootKeys(m, plan.removedIdentities)
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
	deployedFiles, deployedHashes := collectUninstallDeployedProvenance(lock, allRemovalKeys)

	removedModuleDirs, err := removeUninstallModuleDirs(allRemovalKeys, verbose)
	if err != nil {
		return err
	}

	removedFiles, _, diags := deploy.RemoveDeployedFiles(".", deployedFiles, deployedHashes)
	for _, d := range diags {
		ux.Warn(os.Stderr, "%s", d)
	}
	if verbose && len(removedFiles) > 0 {
		items := make([]ux.Item, len(removedFiles))
		for i, f := range removedFiles {
			items[i] = ux.Item{Text: f}
		}
		ux.BulletList(os.Stdout, items)
	}

	removeUninstallStandaloneMCP(plan.mcpNames, lock)

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
			for _, d := range deploy.RemoveMCPServersFromTargets(".", stale) {
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

	printUninstallSummary(plan.resolution, orphans, removedModuleDirs)
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
func uninstallRemainingRootKeys(m *manifest.Manifest, removedIdentities map[string]bool) map[string]bool {
	remaining := map[string]bool{}
	addRemaining := func(deps []*manifest.DependencyReference) {
		for _, d := range deps {
			if k, ok := uninstallIdentity(d); ok && !removedIdentities[k] {
				remaining[uninstallRemovalKey(k)] = true
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
func removeUninstallModuleDirs(removalKeys map[string]bool, verbose bool) (removedCount int, err error) {
	var removed []ux.Item
	for _, key := range sortedStringSet(removalKeys) {
		wasRemoved, rerr := deploy.SafeRemoveModuleDir(".", key)
		if rerr != nil {
			return removedCount, fmt.Errorf("remove apm_modules/%s: %w", key, rerr)
		}
		if wasRemoved {
			removedCount++
			if verbose {
				removed = append(removed, ux.Item{Text: "apm_modules/" + key})
			}
		}
	}
	if len(removed) > 0 {
		ux.BulletList(os.Stdout, removed)
	}
	return removedCount, nil
}

// removeUninstallStandaloneMCP reverse-removes every explicitly-targeted MCP
// server (un-019/064/065: `uninstall <mcp-name>` matched against
// dependencies.mcp, not a transitive-stale diff) from every target's config
// file, and drops its name from lock.MCPServers so a later uninstall's
// transitive-stale diff (un-061, deferred to 8b) doesn't see a stale name
// that's already gone from every target.
func removeUninstallStandaloneMCP(mcpNames map[string]bool, lock *lockfile.Lockfile) {
	if len(mcpNames) == 0 {
		return
	}
	names := sortedStringSet(mcpNames)
	diags := deploy.RemoveMCPServersFromTargets(".", names)
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

// writeUninstallLockfile removes removalKeys from lock.Dependencies and
// writes the result -- or, if that empties it entirely, deletes
// apm.lock.yaml outright (un-071) instead of writing back an empty shell.
func writeUninstallLockfile(lock *lockfile.Lockfile, lockNode *yamllib.Node, removalKeys map[string]bool) error {
	if lock == nil {
		return nil
	}
	lock.RemoveKeys(sortedStringSet(removalKeys))
	if len(lock.Dependencies) == 0 {
		if err := os.Remove("apm.lock.yaml"); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove apm.lock.yaml: %w", err)
		}
		return nil
	}
	out, err := lockfile.WriteLockfile(lock, lockNode)
	if err != nil {
		return fmt.Errorf("serialize apm.lock.yaml: %w", err)
	}
	if err := os.WriteFile("apm.lock.yaml", out, 0644); err != nil {
		return fmt.Errorf("write apm.lock.yaml: %w", err)
	}
	return nil
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

// printUninstallDryRunPlan is un-080/081's preview: what would be removed
// from apm.yml, whether each apm_modules/<key> path currently exists, and
// which transitive dependencies would be pruned as orphans -- no writes.
func printUninstallDryRunPlan(resolution *uninstallResolution, allRemovalKeys, orphans map[string]bool) {
	ux.Section(os.Stdout, "dry-run: would remove from apm.yml")
	targetItems := make([]ux.Item, 0, len(resolution.APMTargets)+len(resolution.MCPTargets))
	for _, t := range resolution.APMTargets {
		section := "dependencies.apm"
		if t.IsDev {
			section = "devDependencies.apm"
		}
		targetItems = append(targetItems, ux.Item{Text: fmt.Sprintf("%s (%s)", t.Name, section)})
	}
	for _, t := range resolution.MCPTargets {
		targetItems = append(targetItems, ux.Item{Text: fmt.Sprintf("%s (dependencies.mcp)", t.Name)})
	}
	ux.BulletList(os.Stdout, targetItems)

	if len(orphans) > 0 {
		ux.Section(os.Stdout, "dry-run: transitive orphans that would also be removed")
		orphanItems := make([]ux.Item, 0, len(orphans))
		for _, k := range sortedStringSet(orphans) {
			orphanItems = append(orphanItems, ux.Item{Text: k})
		}
		ux.BulletList(os.Stdout, orphanItems)
	}

	ux.Section(os.Stdout, "dry-run: apm_modules")
	moduleItems := make([]ux.Item, 0, len(allRemovalKeys))
	for _, k := range sortedStringSet(allRemovalKeys) {
		state := "missing"
		if _, err := os.Stat(filepath.Join("apm_modules", filepath.FromSlash(k))); err == nil {
			state = "exists"
		}
		moduleItems = append(moduleItems, ux.Item{Text: fmt.Sprintf("apm_modules/%s: %s", k, state)})
	}
	ux.BulletList(os.Stdout, moduleItems)

	ux.Info(os.Stdout, "dry-run: no changes made")
}

// printUninstallSummary is un-101's closing summary.
func printUninstallSummary(resolution *uninstallResolution, orphans map[string]bool, removedModuleDirs int) {
	removedPackages := len(resolution.APMTargets) + len(resolution.MCPTargets)
	if len(orphans) > 0 {
		ux.Success(os.Stdout, "Removed %d package(s) (+%d transitive orphan(s))", removedPackages, len(orphans))
	} else {
		ux.Success(os.Stdout, "Removed %d package(s)", removedPackages)
	}
	ux.Success(os.Stdout, "apm_modules: removed %d director%s", removedModuleDirs, pluralYIES(removedModuleDirs))
}

func pluralYIES(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
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
