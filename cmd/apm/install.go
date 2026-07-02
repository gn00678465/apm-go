package main

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/archive"
	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/experimental"
	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/registry"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/apm-go/apm/internal/yamlcore"
	"github.com/spf13/cobra"
)

type installDeps struct {
	tags   resolver.TagLister
	loader resolver.PackageLoader
	// Archive extraction caps (req-sc-004). Zero values normalize to the spec
	// defaults (100 MB / 10,000) inside internal/archive.
	maxEntries      int
	maxArchiveBytes int64
}

func installCmd() *cobra.Command {
	var frozen bool
	var noProvenance bool
	var targetFlag string
	var skillFlags []string
	var maxEntries int
	var maxArchiveBytes int64

	cmd := &cobra.Command{
		Use:   "install [packages...]",
		Short: "Install dependencies from apm.yml or by URL/shorthand",
		RunE: func(cmd *cobra.Command, args []string) error {
			deps := &installDeps{
				tags: &gitops.RealTagLister{},
				loader: &gitops.RealPackageLoader{
					ModulesDir: "apm_modules",
				},
				maxEntries:      maxEntries,
				maxArchiveBytes: maxArchiveBytes,
			}
			return runInstall(deps, frozen, noProvenance, targetFlag, skillFlags, args)
		},
	}

	cmd.Flags().BoolVar(&frozen, "frozen", false, "frozen install mode: lockfile must exist and cover all deps")
	cmd.Flags().BoolVar(&noProvenance, "no-provenance", false, "omit generated_at and apm_version from lockfile")
	cmd.Flags().StringVar(&targetFlag, "target", "", "explicit target for deployment (overrides auto-detection)")
	cmd.Flags().StringArrayVar(&skillFlags, "skill", nil, "install only named skills from the package (repeatable)")
	cmd.Flags().IntVar(&maxEntries, "max-entries", archive.DefaultMaxEntries, "max archive entries before fail-closed (req-sc-004)")
	cmd.Flags().Int64Var(&maxArchiveBytes, "max-archive-bytes", archive.DefaultMaxBytes, "max uncompressed archive bytes before fail-closed (req-sc-004)")

	return cmd
}

func runInstall(deps *installDeps, frozen, noProvenance bool, targetFlag string, skillSubset []string, packages []string) error {
	// Determine frozen mode up front (explicit flag or CI default) so apm.yml can
	// be optional in frozen verify-only mode (integrity is checked from lockfile+disk).
	if !frozen && lockfile.IsCIEnvironment() {
		frozen = true
		fmt.Fprintln(os.Stderr, "CI environment detected, defaulting to frozen install")
	}

	// --skill requires an actual package to scope to. Reject up front rather
	// than silently no-op-ing: frozen installs skip resolution/deploy
	// filtering entirely (nothing for --skill to scope), and with no
	// positional package, requestedKeys below would stay empty.
	if len(skillSubset) > 0 {
		if frozen {
			return fmt.Errorf("--skill is not supported with a frozen install (frozen installs pin exactly what's locked, with no per-package skill selection)")
		}
		if len(packages) == 0 {
			return fmt.Errorf("--skill requires at least one positional package to install")
		}
	}

	// 1. Parse apm.yml — optional in frozen mode.
	var m *manifest.Manifest
	var node *yamllib.Node
	data, err := os.ReadFile("apm.yml")
	if err != nil {
		switch {
		case frozen && os.IsNotExist(err):
			m = &manifest.Manifest{} // frozen verifies from lockfile + disk alone
		case len(packages) > 0 && os.IsNotExist(err):
			return fmt.Errorf("apm.yml not found; run 'apm-go init' first, then 'apm-go install <package>'")
		default:
			return fmt.Errorf("read apm.yml: %w", err)
		}
	} else {
		node, err = yamlcore.SafeLoad(data)
		if err != nil {
			return fmt.Errorf("parse apm.yml: %w", err)
		}
		m, _, err = manifest.ParseManifest(node)
		if err != nil {
			return fmt.Errorf("validate apm.yml: %w", err)
		}
	}

	// 1b. Add positional packages to deps (skip if already in manifest).
	// requestedKeys tracks every positional package's dep key for THIS call
	// (whether newly added or already declared), so --skill can later be
	// scoped to only these dependencies instead of the whole resolved graph.
	requestedKeys := make(map[string]bool)
	if len(packages) > 0 {
		existing := make(map[string]bool)
		for _, d := range m.ParsedDeps {
			existing[d.RepoURL] = true
		}
		for _, pkg := range packages {
			ref, err := manifest.ParseDepString(pkg)
			if err != nil {
				return fmt.Errorf("parse package %q: %w", pkg, err)
			}
			if ref.IsLocal {
				ref.IsLocal = false
				ref.RepoURL = ref.LocalPath
				ref.LocalPath = ""
				ref.Source = "git"
			}
			requestedKeys[deploy.DepRefKey(ref)] = true
			if existing[ref.RepoURL] {
				continue
			}
			m.ParsedDeps = append(m.ParsedDeps, ref)
		}
	}

	// 2. Load existing lockfile
	var existingLock *lockfile.Lockfile
	var existingNode *yamllib.Node
	lockData, lockErr := os.ReadFile("apm.lock.yaml")
	if lockErr == nil {
		lockNode, err := yamlcore.SafeLoad(lockData)
		if err != nil {
			return fmt.Errorf("parse apm.lock.yaml: %w", err)
		}
		existingNode = lockNode
		existingLock, err = lockfile.ParseLockfile(lockNode)
		if err != nil {
			return fmt.Errorf("validate apm.lock.yaml: %w", err)
		}
	}

	// 3. Frozen install (frozen mode was resolved up front, incl. CI default).
	if frozen {
		if existingLock == nil {
			return fmt.Errorf("frozen install requires a lockfile but none was found")
		}
		if err := lockfile.CheckFrozenInstall(m, existingLock); err != nil {
			return err
		}

		// (A) Disk-only integrity — verified from lockfile + disk, before any
		// network fetch or source materialization, without requiring apm.yml.

		// (A1) Re-verify deployed-file hashes (req-lk-017 / req-sc-001). MUST run
		// before any git download so a tampered deployed file is reported by path.
		if viol := lockfile.VerifyDeployedState(existingLock, "."); len(viol) > 0 {
			v := viol[0]
			observed := v.Observed
			if observed == "" {
				observed = "<missing>"
			}
			return fmt.Errorf("frozen install: content-integrity violation: %s expected %s, observed %s",
				v.Path, v.Expected, observed)
		}

		// (A2) Registry archives: verify bytes' SHA-256 before extraction
		// (req-lk-013), then safe-extract enforcing path/link/size/entry guards
		// (req-sc-002/004). Offline archive located in CWD by repo basename.
		for i := range existingLock.Dependencies {
			dep := &existingLock.Dependencies[i]
			if dep.Source != "registry" {
				continue
			}
			// A registry lock entry MUST carry a resolved_hash; a missing one is a
			// malformed or tampered lockfile — fail closed rather than skip the
			// integrity gate (req-lk-013).
			if dep.ResolvedHash == "" {
				return fmt.Errorf("frozen install: registry dependency %q has no resolved_hash", dep.UniqueKey())
			}
			// Defense in depth: the extraction root is derived from lockfile
			// repo_url (validated at parse time). Refuse to extract outside
			// apm_modules even if that validation is ever bypassed (req-sc-002).
			destDir := filepath.Join("apm_modules", dep.UniqueKey())
			if !archive.Contained("apm_modules", destDir) {
				return fmt.Errorf("frozen install: refusing to extract %q outside apm_modules", dep.RepoURL)
			}

			archivePath := path.Base(dep.RepoURL) + ".tar.gz"
			_, localErr := os.Stat(archivePath)
			hasLocal := localErr == nil
			info, destErr := os.Stat(destDir)
			materialized := destErr == nil && info.IsDir()

			// Without a trust anchor (local archive or resolved_url) the archive
			// cannot be re-verified. If already materialized, (A1) already verified
			// the deployed files and apm_modules is a rebuilt cache (not deployed in
			// frozen mode) — consistent with the git frozen path's skip-if-present.
			// Otherwise the lockfile is malformed — fail closed.
			if !hasLocal && dep.ResolvedURL == "" {
				if materialized {
					continue
				}
				return fmt.Errorf("frozen install: cannot materialize registry dependency %q (no local archive and no resolved_url)", dep.UniqueKey())
			}

			// A trust anchor exists: (re)materialize from verified bytes so the
			// cache provably matches resolved_hash, replacing any pre-existing tree.
			// Hash is verified BEFORE any extraction (req-lk-013).
			if materialized {
				if err := os.RemoveAll(destDir); err != nil {
					return fmt.Errorf("frozen install: reset %s: %w", destDir, err)
				}
			}

			if hasLocal {
				// Read once, then verify and extract the SAME in-memory bytes so a
				// concurrent swap of the on-disk archive between hash and extract
				// cannot slip unverified bytes through (no reopen -> no TOCTOU).
				data, rErr := os.ReadFile(archivePath)
				if rErr != nil {
					return fmt.Errorf("frozen install: read archive %s: %w", archivePath, rErr)
				}
				if err := lockfile.VerifyArchiveBytes(data, dep.ResolvedHash); err != nil {
					return fmt.Errorf("frozen install: %s: %w", dep.UniqueKey(), err) // entry/expected/actual; no extraction
				}
				if _, exErr := archive.SafeExtract(bytes.NewReader(data), destDir, archive.Limits{
					MaxBytes:   deps.maxArchiveBytes,
					MaxEntries: deps.maxEntries,
				}); exErr != nil {
					return fmt.Errorf("frozen install: %w", exErr)
				}
				continue
			}

			// Network replay from resolved_url (trust anchor). resolved_url is the
			// trust anchor; re-verify bytes against the lockfile hash before extract.
			// Live registry access is experimental (offline-archive extraction above
			// is not gated — it needs no network).
			if err := experimental.RequireEnabled("registries"); err != nil {
				return fmt.Errorf("frozen install: %w", err)
			}
			client, cErr := registry.ClientForURL(dep.ResolvedURL, m.Registries)
			if cErr != nil {
				return fmt.Errorf("frozen install: %w", cErr)
			}
			body, _, dErr := client.FetchURL(dep.ResolvedURL)
			if dErr != nil {
				return fmt.Errorf("frozen install: fetch %s: %w", dep.UniqueKey(),
					registry.RemediateFetchAuth(dErr, dep.ResolvedURL, m.Registries))
			}
			if err := lockfile.VerifyArchiveBytes(body, dep.ResolvedHash); err != nil {
				return fmt.Errorf("frozen install: %s: %w", dep.UniqueKey(), err)
			}
			if _, exErr := archive.SafeExtract(bytes.NewReader(body), destDir, archive.Limits{
				MaxBytes:   deps.maxArchiveBytes,
				MaxEntries: deps.maxEntries,
			}); exErr != nil {
				return fmt.Errorf("frozen install: %w", exErr)
			}
		}

		// (B) Source materialization (git download + tree_sha256, req-lk-015) — only
		// when the manifest declares deps. In verify-only mode (no apm.yml) there is
		// nothing to materialize; (A) is the operative integrity gate.
		if len(m.ParsedDeps) > 0 {
			for _, dep := range existingLock.Dependencies {
				if dep.Source == "registry" || dep.Source == "local" {
					continue
				}
				// req-lk-007: always call LoadPackage rather than short-
				// circuiting on directory existence here -- LoadPackage
				// itself verifies an existing checkout's HEAD against the
				// locked ref before deciding whether to skip re-cloning, so
				// a stale/tampered checkout is replaced rather than
				// silently trusted.
				ref := &manifest.DependencyReference{
					RepoURL:     dep.RepoURL,
					VirtualPath: dep.VirtualPath,
					Owner:       ownerFromRepoURL(dep.RepoURL),
					Repo:        repoFromRepoURL(dep.RepoURL),
					Source:      "git",
				}
				// Frozen mode already has the authoritative locked commit;
				// prefer it over resolved_ref (which may name a mutable
				// branch, e.g. "main") so the req-lk-007 skip check verifies
				// against the actual pin rather than a ref that could point
				// somewhere else than what was locked.
				resolvedRef := dep.ResolvedCommit
				if resolvedRef == "" {
					resolvedRef = dep.ResolvedRef
				}
				if _, loadErr := deps.loader.LoadPackage(ref, resolvedRef); loadErr != nil {
					return fmt.Errorf("frozen install: download %s: %w", dep.UniqueKey(), loadErr)
				}
			}
			for _, dep := range existingLock.Dependencies {
				if dep.ResolvedCommit != "" && dep.Source != "registry" {
					if dep.TreeSHA256 == "" {
						return fmt.Errorf("frozen install: entry %s missing required tree_sha256", dep.UniqueKey())
					}
					installDir := filepath.Join("apm_modules", dep.UniqueKey())
					if err := lockfile.VerifyTreeSHA256(dep.TreeSHA256, installDir, dep.ResolvedCommit); err != nil {
						return fmt.Errorf("frozen install: entry %s: %w", dep.UniqueKey(), err)
					}
				}
			}
		}

		fmt.Println("Frozen install: all dependencies pinned and verified")
		return nil
	}

	// 4. Resolve dependency graph, unless this is a local-only deploy.
	targets, targetDiags := deploy.ResolveTargets(targetFlag, m.Target, ".")
	if len(m.ParsedDeps) == 0 {
		fmt.Println("No dependencies to install")
		if len(targets) == 0 {
			for _, d := range targetDiags {
				fmt.Fprintln(os.Stderr, d)
			}
			return nil
		}
	}

	var result *resolver.ResolutionResult
	var regLoader *registry.Loader
	if len(m.ParsedDeps) == 0 {
		result = &resolver.ResolutionResult{}
	} else {
		fmt.Println("[>] Installing dependencies from apm.yml...")
		seen := make(map[string]bool)
		for _, dep := range m.ParsedDeps {
			canon := dep.ToCanonical(m.DefaultHost)
			if !seen[canon] {
				seen[canon] = true
				fmt.Printf("[>] Resolving %s...\n", canon)
			}
		}

		// Registry access is experimental (API may change); require the opt-in flag
		// before any live registry resolution. Gates network use only — apm.yml
		// registries parsing and lockfile schema stay unconditional.
		for _, d := range m.ParsedDeps {
			if d.Source == "registry" {
				if err := experimental.RequireEnabled("registries"); err != nil {
					return err
				}
				break
			}
		}

		// Composite loader: registry-sourced deps go through the HTTP consumer
		// (wiring credsec sc-003/005/007/008 + lk-013), everything else via git.
		regLoader = &registry.Loader{
			Registries:      m.Registries,
			DefaultRegistry: m.DefaultRegistry,
			ModulesDir:      "apm_modules",
			Next:            deps.loader,
			MaxBytes:        deps.maxArchiveBytes,
			MaxEntries:      deps.maxEntries,
		}

		result, err = resolver.Resolve(m, existingLock, deps.tags, regLoader, resolver.ResolverConfig{})
		if err != nil {
			return fmt.Errorf("resolve: %w", err)
		}
	}

	// 5. Build lockfile
	newLock, err := buildLockfile(result, existingLock, regLoader, skillSubset, requestedKeys, noProvenance)
	if err != nil {
		return err
	}

	// 6-9. Deploy primitives, no-op check, write lockfile, persist packages.
	return deployAndFinalize(m, targetFlag, skillSubset, requestedKeys, packages, result, newLock, existingLock, existingNode, node)
}

// buildLockfile converts a resolution result into the lockfile that would be
// written for it, without touching disk (steps 5). Shared by runInstall and
// runUpdate so both build the same lockfile shape from a resolution result.
func buildLockfile(result *resolver.ResolutionResult, existingLock *lockfile.Lockfile, regLoader *registry.Loader, skillSubset []string, requestedKeys map[string]bool, noProvenance bool) (*lockfile.Lockfile, error) {
	existingVersion := ""
	if existingLock != nil {
		existingVersion = existingLock.Version
	}

	newLock := &lockfile.Lockfile{
		Version: lockfile.DetermineVersion(toLockDeps(result.Deps), existingVersion),
	}
	if !noProvenance {
		newLock.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
		newLock.APMVersion = "0.1.0"
	}

	matchedKeys := make(map[string]bool, len(requestedKeys))
	for _, dep := range result.Deps {
		ld := lockfile.LockedDep{
			RepoURL:        dep.RepoURL,
			VirtualPath:    dep.VirtualPath,
			Source:         kindToSource(dep.Kind),
			ResolvedTag:    dep.ResolvedTag,
			ResolvedRef:    dep.ResolvedRef,
			ResolvedCommit: dep.Commit,
			Constraint:     dep.Constraint,
			ResolvedBy:     dep.ResolvedBy,
			Depth:          dep.Depth,
		}

		// req-lk-002/003: registry deps carry resolved_url + resolved_hash +
		// version, collected out-of-band by the registry loader.
		if dep.Kind == resolver.KindRegistry {
			if r, ok := regLoader.Resolutions()[dep.Key]; ok {
				ld.ResolvedURL = r.ResolvedURL
				ld.ResolvedHash = r.ResolvedHash
				ld.Version = r.Version
			}
		}

		// Record skill_subset only on the dependency this --skill flag was
		// scoped to this call -- not every dep in the resolved graph (bug
		// fix: previously stamped every already-declared, unrelated
		// dependency with the same subset).
		if len(skillSubset) > 0 && requestedKeys[dep.Key] {
			ld.SkillSubset = skillSubset
			matchedKeys[dep.Key] = true
		}

		// req-lk-008: record resolved_at for git-semver entries
		if dep.Kind == resolver.KindGitSemver && dep.Constraint != "" {
			ld.ResolvedAt = time.Now().UTC().Format(time.RFC3339)
		}

		// Resolve commit SHA for git deps that don't have it yet
		if (dep.Kind == resolver.KindGitSemver || dep.Kind == resolver.KindGitLiteral) && dep.Commit == "" {
			installDir := filepath.Join("apm_modules", dep.Key)
			if commit, err := gitops.ResolveCommit(installDir); err == nil {
				ld.ResolvedCommit = commit
			}
		}

		// req-lk-015: compute tree_sha256 for git-sourced deps (required)
		if dep.Kind == resolver.KindGitSemver || dep.Kind == resolver.KindGitLiteral {
			installDir := filepath.Join("apm_modules", dep.Key)
			commit := ld.ResolvedCommit
			if commit != "" {
				treeHash, hashErr := lockfile.ComputeTreeSHA256(installDir, commit)
				if hashErr != nil {
					return nil, fmt.Errorf("tree_sha256 for %s: %w", dep.Key, hashErr)
				}
				ld.TreeSHA256 = treeHash
			}
		}

		newLock.Dependencies = append(newLock.Dependencies, ld)
	}

	// Every requested package must have actually resolved into the graph --
	// fail loud instead of silently doing nothing for it (e.g. it collided
	// with an already-declared dependency during positional-package dedup
	// and was never added to m.ParsedDeps). Checking "at least one matched"
	// is not enough: with multiple positional packages, one valid match
	// would mask another that silently never resolved.
	if len(skillSubset) > 0 {
		if len(requestedKeys) == 0 {
			return nil, fmt.Errorf("--skill %s requires at least one resolved package to scope to", strings.Join(skillSubset, ", "))
		}
		var unmatched []string
		for key := range requestedKeys {
			if !matchedKeys[key] {
				unmatched = append(unmatched, key)
			}
		}
		if len(unmatched) > 0 {
			sort.Strings(unmatched)
			return nil, fmt.Errorf("--skill %s: package(s) %s did not resolve into the dependency graph", strings.Join(skillSubset, ", "), strings.Join(unmatched, ", "))
		}
	}

	return newLock, nil
}

// deployAndFinalize runs deploy.Run, prints the deploy summary, checks for a
// no-op (steps 6-7), then writes apm.lock.yaml and (for positional package
// installs) apm.yml (steps 8-9). Shared by runInstall and runUpdate.
func deployAndFinalize(m *manifest.Manifest, targetFlag string, skillSubset []string, requestedKeys map[string]bool, packages []string, result *resolver.ResolutionResult, newLock, existingLock *lockfile.Lockfile, existingNode, node *yamllib.Node) error {
	targets, targetDiags := deploy.ResolveTargets(targetFlag, m.Target, ".")

	// 6. Deploy primitives to targets
	for _, d := range targetDiags {
		fmt.Fprintln(os.Stderr, d)
	}
	if len(targets) > 0 {
		targetSource := "auto-detect"
		if targetFlag != "" {
			targetSource = "--target"
		} else if len(m.Target) > 0 {
			targetSource = "apm.yml"
		}
		fmt.Printf("[i] Targets: %s  (source: %s)\n", strings.Join(targets, ", "), targetSource)

		var skillFilter *deploy.SkillFilter
		if len(skillSubset) > 0 {
			fmt.Printf("[i] Skill subset: %s\n", strings.Join(skillSubset, ", "))
			depKeys := make([]string, 0, len(requestedKeys))
			for k := range requestedKeys {
				depKeys = append(depKeys, k)
			}
			skillFilter = &deploy.SkillFilter{Names: skillSubset, DepKeys: depKeys}
		}

		deployResult, err := deploy.Run(targets, ".", m, result, skillFilter)
		if err != nil {
			return fmt.Errorf("deploy: %w", err)
		}
		for _, d := range deployResult.Diags {
			fmt.Fprintf(os.Stderr, "[!] %s\n", d)
		}

		// Print deploy summary per dep
		for key, dr := range deployResult.PerDep {
			label := key
			if label == "" {
				label = "(local)"
			}
			fmt.Printf("  [+] %s\n", label)
			printDeploySummary(dr.Files, targets)
		}

		// Populate per-dep DeployedFiles/DeployedHashes in lockfile entries
		for i := range newLock.Dependencies {
			dep := &newLock.Dependencies[i]
			key := dep.UniqueKey()
			if dr, ok := deployResult.PerDep[key]; ok {
				dep.DeployedFiles = dr.Files
				dep.DeployedHashes = dr.Hashes
			}
		}
		// Populate local deployed files
		if dr, ok := deployResult.PerDep[""]; ok {
			newLock.LocalDeployedFiles = dr.Files
			newLock.LocalDeployedHashes = dr.Hashes
		}

		// Merged MCP config files (e.g. .mcp.json) are multi-source -- no
		// single dep or "local" bucket owns them -- so their hashes are
		// recorded alongside local deployed files (pr-001 source attribution
		// is served by deployResult.MCPProvenance in-memory, not persisted).
		if len(deployResult.MCPFiles) > 0 {
			if newLock.LocalDeployedHashes == nil {
				newLock.LocalDeployedHashes = map[string]string{}
			}
			for f, hash := range deployResult.MCPFiles {
				newLock.LocalDeployedFiles = append(newLock.LocalDeployedFiles, f)
				newLock.LocalDeployedHashes[f] = hash
			}
			sort.Strings(newLock.LocalDeployedFiles)
		}
	}

	// 7. No-op check
	if existingLock != nil && lockfile.IsSemanticEqual(existingLock, newLock) {
		fmt.Println("Already up to date")
		return nil
	}

	// 8. Write lockfile
	outBytes, err := lockfile.WriteLockfile(newLock, existingNode)
	if err != nil {
		return fmt.Errorf("serialize lockfile: %w", err)
	}

	if err := os.WriteFile("apm.lock.yaml", outBytes, 0644); err != nil {
		return fmt.Errorf("write apm.lock.yaml: %w", err)
	}

	// 9. Persist positional packages to apm.yml
	if len(packages) > 0 {
		if err := persistPackagesToManifest(node, packages, skillSubset); err != nil {
			return fmt.Errorf("update apm.yml: %w", err)
		}
		manifestBytes, err := yamlcore.SafeDump(node)
		if err != nil {
			return fmt.Errorf("serialize apm.yml: %w", err)
		}
		if err := os.WriteFile("apm.yml", manifestBytes, 0644); err != nil {
			return fmt.Errorf("write apm.yml: %w", err)
		}
	}

	fmt.Printf("\n[*] Installed %d dependencies\n", len(result.Deps))
	for _, dep := range result.Deps {
		tag := dep.ResolvedTag
		if tag == "" {
			tag = dep.ResolvedRef
		}
		fmt.Printf("  %s@%s (depth %d)\n", dep.Key, tag, dep.Depth)
	}

	return nil
}

func printDeploySummary(files []string, targets []string) {
	counts := map[string][]string{}
	for _, f := range files {
		var ptype string
		switch {
		case strings.Contains(f, "/skills/"):
			ptype = "skill(s)"
		case strings.Contains(f, "/agents/") && !strings.Contains(f, ".agents/"):
			ptype = "agent(s)"
		case strings.Contains(f, "/rules/") || strings.Contains(f, "/instructions/"):
			ptype = "instruction(s)"
		case strings.Contains(f, "/commands/"):
			ptype = "command(s)"
		case strings.Contains(f, "/prompts/"):
			ptype = "prompt(s)"
		default:
			ptype = "file(s)"
		}
		dir := f[:strings.LastIndex(f, "/")+1]
		key := ptype + " -> " + dir
		counts[key] = append(counts[key], f)
	}
	for key, items := range counts {
		fmt.Printf("  |-- %d %s\n", len(items), key)
	}
}

func toLockDeps(deps []resolver.ResolvedDep) []lockfile.LockedDep {
	result := make([]lockfile.LockedDep, len(deps))
	for i, d := range deps {
		result[i] = lockfile.LockedDep{Source: kindToSource(d.Kind)}
	}
	return result
}

func persistPackagesToManifest(doc *yamllib.Node, packages, skillSubset []string) error {
	root := doc
	if root.Kind == yamllib.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yamllib.MappingNode {
		return fmt.Errorf("manifest root is not a mapping")
	}

	// Find or create dependencies.apm sequence
	var depsNode *yamllib.Node
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == "dependencies" {
			depsNode = root.Content[i+1]
			break
		}
	}
	if depsNode == nil {
		depsNode = &yamllib.Node{Kind: yamllib.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content,
			&yamllib.Node{Kind: yamllib.ScalarNode, Value: "dependencies", Tag: "!!str"},
			depsNode,
		)
	}

	var apmSeq *yamllib.Node
	if depsNode.Kind == yamllib.MappingNode {
		for i := 0; i < len(depsNode.Content)-1; i += 2 {
			if depsNode.Content[i].Value == "apm" {
				apmSeq = depsNode.Content[i+1]
				break
			}
		}
	}
	if apmSeq == nil {
		apmSeq = &yamllib.Node{Kind: yamllib.SequenceNode, Tag: "!!seq"}
		depsNode.Content = append(depsNode.Content,
			&yamllib.Node{Kind: yamllib.ScalarNode, Value: "apm", Tag: "!!str"},
			apmSeq,
		)
	}

	// Check which packages already exist in the sequence
	existingPkgs := make(map[string]bool)
	if apmSeq.Kind == yamllib.SequenceNode {
		for _, entry := range apmSeq.Content {
			if entry.Kind == yamllib.ScalarNode {
				existingPkgs[entry.Value] = true
			} else if entry.Kind == yamllib.MappingNode {
				for j := 0; j < len(entry.Content)-1; j += 2 {
					if entry.Content[j].Value == "git" {
						existingPkgs[entry.Content[j+1].Value] = true
					}
				}
			}
		}
	}

	for _, pkg := range packages {
		if existingPkgs[pkg] {
			continue
		}
		if len(skillSubset) > 0 {
			// Object form: { git: <pkg>, skills: [<skill>...] }
			entry := &yamllib.Node{Kind: yamllib.MappingNode, Tag: "!!map"}
			entry.Content = append(entry.Content,
				&yamllib.Node{Kind: yamllib.ScalarNode, Value: "git", Tag: "!!str"},
				&yamllib.Node{Kind: yamllib.ScalarNode, Value: pkg, Tag: "!!str"},
			)
			skillSeq := &yamllib.Node{Kind: yamllib.SequenceNode, Tag: "!!seq"}
			for _, s := range skillSubset {
				skillSeq.Content = append(skillSeq.Content,
					&yamllib.Node{Kind: yamllib.ScalarNode, Value: s, Tag: "!!str"},
				)
			}
			entry.Content = append(entry.Content,
				&yamllib.Node{Kind: yamllib.ScalarNode, Value: "skills", Tag: "!!str"},
				skillSeq,
			)
			apmSeq.Content = append(apmSeq.Content, entry)
		} else {
			// String form
			apmSeq.Content = append(apmSeq.Content,
				&yamllib.Node{Kind: yamllib.ScalarNode, Value: pkg, Tag: "!!str"},
			)
		}
	}

	return nil
}

func kindToSource(k resolver.ReferenceKind) string {
	switch k {
	case resolver.KindRegistry:
		return "registry"
	case resolver.KindLocal:
		return "local"
	case resolver.KindGitSemver, resolver.KindGitLiteral:
		return "git"
	default:
		return ""
	}
}

func ownerFromRepoURL(repoURL string) string {
	parts := strings.Split(repoURL, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return repoURL
}

func repoFromRepoURL(repoURL string) string {
	parts := strings.Split(repoURL, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}
	return repoURL
}
