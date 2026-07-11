package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/apm-go/apm/internal/archive"
	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/experimental"
	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/marketplace"
	"github.com/apm-go/apm/internal/registry"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/apm-go/apm/internal/yamlcore"
	"github.com/spf13/cobra"
)

func updateCmd() *cobra.Command {
	var frozen bool
	var noFrozen bool

	cmd := &cobra.Command{
		Use:   "update [package]",
		Short: "Re-resolve dependencies to their newest matching version",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deps := &installDeps{
				tags:   &gitops.RealTagLister{},
				loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"},
			}
			pkg := ""
			if len(args) == 1 {
				pkg = args[0]
			}
			return runUpdate(deps, frozen, noFrozen, pkg)
		},
	}
	cmd.Flags().BoolVar(&frozen, "frozen", false, "refuse a scoped update against a frozen install (req-rs-012); auto-enabled in CI")
	cmd.Flags().BoolVar(&noFrozen, "no-frozen", false, "override CI auto-frozen detection to allow a scoped update")
	cmd.MarkFlagsMutuallyExclusive("frozen", "no-frozen")
	return cmd
}

func runUpdate(deps *installDeps, frozen, noFrozen bool, pkg string) error {
	if noFrozen {
		frozen = false
	} else if !frozen && lockfile.IsCIEnvironment() {
		frozen = true
	}

	// req-rs-012: refuse a scoped update against a frozen install before any
	// disk mutation. PlanScopedUpdate below also refuses, but by then the
	// apm_modules clearing further down would already have run -- checking
	// here first keeps a refused update from having any side effect at all.
	if pkg != "" && frozen {
		return fmt.Errorf("cannot update in frozen install mode; use --no-frozen to override")
	}

	data, err := os.ReadFile("apm.yml")
	if err != nil {
		return fmt.Errorf("read apm.yml: %w", err)
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		return fmt.Errorf("parse apm.yml: %w", err)
	}
	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		return fmt.Errorf("validate apm.yml: %w", err)
	}

	// F1 (07-11-update-local-deps): mirror install.go's 1b-2 normalization so
	// a local/absolute-path dependency is copy-materialized into
	// apm_modules/_local/<name>/ on `apm update` too, instead of resolving it
	// in place (via depKey==ref.LocalPath) and deploying nothing -- or worse,
	// letting PlanFullUpdate rebuild the lockfile entry under the bare
	// LocalPath key and lose the existing deployed_files/deployed_file_hashes
	// provenance an install had already recorded under "_local/...".
	for _, dep := range m.ParsedDeps {
		normalizeLocalDep(dep)
	}
	for _, dep := range m.ParsedDevDeps {
		normalizeLocalDep(dep)
	}

	// C3 (07-11-update-local-deps): translate a scoped positional token that
	// names a local/absolute-path dependency into the same synthetic
	// apm_modules key normalizeLocalDep just gave it, mirroring
	// uninstallRemovalKey (uninstall.go:186-192) -- otherwise the
	// normalization above moves the manifest's dependency key space out from
	// under a still-relative-path `apm update ./dep-pkg` invocation and
	// PlanScopedUpdate's depKey(dep)==packageName lookup would report
	// "package not found" for a dependency that IS present.
	if pkg != "" {
		if ref, perr := manifest.ParseDepString(pkg); perr == nil && ref.IsLocal {
			pkg = localModulesKey(resolveLocalSourceAbs(ref.LocalPath))
		}
	}

	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		return fmt.Errorf("apm-go update requires an existing apm.lock.yaml: %w", err)
	}
	lockNode, err := yamlcore.SafeLoad(lockData)
	if err != nil {
		return fmt.Errorf("parse apm.lock.yaml: %w", err)
	}
	existingLock, err := lockfile.ParseLockfile(lockNode)
	if err != nil {
		return fmt.Errorf("validate apm.lock.yaml: %w", err)
	}
	existingNode := lockNode

	// Registry access is experimental; mirrors runInstall's gate (regular
	// and dev dependencies both in scope).
	for _, d := range allDirectDeps(m) {
		if d.Source == "registry" {
			if err := experimental.RequireEnabled("registries"); err != nil {
				return err
			}
			break
		}
	}

	// Composite loader: registry-sourced deps go through the HTTP consumer,
	// everything else via git (mirrors runInstall).
	regLoader := &registry.Loader{
		Registries:      m.Registries,
		DefaultRegistry: m.DefaultRegistry,
		ModulesDir:      "apm_modules",
		Next:            deps.loader,
		MaxBytes:        deps.maxArchiveBytes,
		MaxEntries:      deps.maxEntries,
	}

	// req-lk-010: force a from-scratch download for the update's scope, even
	// if the re-resolved tag doesn't change -- LoadPackage's req-lk-007
	// skip-if-matching optimization would otherwise keep trusting the
	// existing checkout instead of re-running the download callback.
	for _, key := range directGitSemverUpdateScope(m, pkg) {
		// repo_url and virtual_path are only charset-validated at parse time,
		// not checked for ".." traversal (unlike local-path deps) -- a ".."
		// segment can resolve to somewhere else entirely under apm_modules
		// (e.g. a sibling package's directory, or apm_modules itself), which
		// a plain Contained check after the join would not catch since that
		// still counts as "inside" apm_modules. ContainedKey rejects any ".."
		// segment outright before joining/cleaning the path.
		if !archive.ContainedKey("apm_modules", key) {
			return fmt.Errorf("refusing to clear %q outside apm_modules", key)
		}
		installDir := filepath.Join("apm_modules", key)
		if err := os.RemoveAll(installDir); err != nil {
			return fmt.Errorf("clear %s before update: %w", key, err)
		}
	}

	// mkt-029/033/F1: same BFS-level marketplace-dict resolution as
	// runInstall -- an apm.yml dependencies.apm dict entry
	// ({name, marketplace, version}) must resolve identically whether
	// reached via `apm install` or `apm update`.
	resolverCfg := resolver.ResolverConfig{MarketplaceResolve: newMarketplaceResolveFunc()}

	var result *resolver.ResolutionResult
	if pkg == "" {
		result, err = resolver.PlanFullUpdate(m, existingLock, deps.tags, regLoader, resolverCfg)
	} else {
		result, err = resolver.PlanScopedUpdate(m, existingLock, deps.tags, regLoader, resolverCfg, pkg, frozen)
	}
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}

	marketplaceProvenance := make(map[string]*marketplace.Provenance)
	mergeMarketplaceProvenance(marketplaceProvenance, result.MarketplaceProvenance)

	newLock, err := buildLockfile(result, existingLock, regLoader, nil, nil, false, marketplaceProvenance)
	if err != nil {
		return err
	}

	printUpdateSummary(existingLock, newLock)

	// C2 (07-11-update-local-deps): same deps-present/zero-target teaching
	// exit as runInstall (install.go:613-618), reusing the identical
	// errNoDeployTarget() wording/exit-code so the two commands never drift.
	// Placed after printUpdateSummary (which has no side effect) so a doomed
	// update's plan output still prints before the teaching error, matching
	// the oracle's observed stdout ordering -- and before deployAndFinalize,
	// so a doomed update never writes apm.lock.yaml (zero partial writes).
	// Previously a deps-present update with no resolvable target silently
	// exited 0 after already rewriting the lockfile (deployAndFinalize's
	// no-op check compares semantic equality, not "did anything deploy"),
	// destroying any local dep's deployed_files/deployed_file_hashes
	// provenance with nothing deployed to show for it. targetFlag is always
	// "" here -- apm-go's `update` has no --target flag (Python parity gap,
	// out of this task's scope).
	targets, targetDiags := deploy.ResolveTargets("", m.Target, ".")
	if len(result.Deps) > 0 && len(targets) == 0 {
		for _, d := range targetDiags {
			fmt.Fprintln(os.Stderr, d)
		}
		return errNoDeployTarget()
	}

	return deployAndFinalize(m, "", nil, nil, nil, result, newLock, existingLock, existingNode, node)
}

// directGitSemverUpdateScope returns the apm_modules/<key> keys that must be
// cleared before re-resolving, so a from-scratch download happens even when
// the winning tag doesn't change (req-lk-010). pkg == "" means every direct
// git-semver dependency is in scope (full update); otherwise only pkg itself
// is cleared, not its transitive subtree, to avoid an unnecessary re-clone
// of everything reachable from it. Regular and dev dependencies are both in
// scope (F3-adjacent: `apm update` resolves devDependencies.apm exactly like
// dependencies.apm, mirroring Python's apm_deps + dev_apm_deps).
func directGitSemverUpdateScope(m *manifest.Manifest, pkg string) []string {
	var keys []string
	for _, dep := range allDirectDeps(m) {
		if resolver.ClassifyReference(dep) != resolver.KindGitSemver {
			continue
		}
		key := dep.RepoURL
		if dep.VirtualPath != "" {
			key += "/" + dep.VirtualPath
		}
		if pkg != "" && key != pkg {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

// printUpdateSummary reports which packages changed version and to what,
// comparing resolved tags (falling back to resolved refs for non-semver
// pins) between the existing and newly-resolved lockfiles. Unchanged
// packages are silent; deployAndFinalize's own no-op check reports "Already
// up to date" when nothing at all changed.
//
// ULD-13 (07-11-update-local-deps): when there is at least one change, the
// summary is prefixed with a "[i] Update plan for apm.yml" heading, mirroring
// the oracle's render_plan_text heading (apm/src/apm_cli/install/plan.py:333)
// -- the C2 zero-target gate relies on this heading appearing in stdout
// before its teaching error so a doomed update's plan is still visible.
func printUpdateSummary(oldLock, newLock *lockfile.Lockfile) {
	var lines []string
	for i := range newLock.Dependencies {
		nd := &newLock.Dependencies[i]
		newTag := nd.ResolvedTag
		if newTag == "" {
			newTag = nd.ResolvedRef
		}

		old := oldLock.FindByKey(nd.UniqueKey())
		if old == nil {
			lines = append(lines, fmt.Sprintf("  + %s@%s (new)", nd.UniqueKey(), newTag))
			continue
		}
		oldTag := old.ResolvedTag
		if oldTag == "" {
			oldTag = old.ResolvedRef
		}
		if oldTag != newTag {
			lines = append(lines, fmt.Sprintf("  %s: %s -> %s", nd.UniqueKey(), oldTag, newTag))
		}
	}
	if len(lines) == 0 {
		return
	}
	fmt.Println("[i] Update plan for apm.yml")
	for _, line := range lines {
		fmt.Println(line)
	}
}
