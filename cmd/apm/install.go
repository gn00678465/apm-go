package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/archive"
	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
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

	// 1b. Add positional packages to deps (skip if already in manifest)
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
			if dep.Source != "registry" || dep.ResolvedHash == "" {
				continue
			}
			archivePath := path.Base(dep.RepoURL) + ".tar.gz"
			if _, statErr := os.Stat(archivePath); statErr != nil {
				continue // no local archive offline; nothing to verify/extract here
			}
			if err := lockfile.VerifyArchiveHash(archivePath, dep.ResolvedHash); err != nil {
				return fmt.Errorf("frozen install: %w", err) // names expected/actual; no extraction
			}
			// Defense in depth: the extraction root is derived from lockfile
			// repo_url (validated at parse time). Refuse to extract outside
			// apm_modules even if that validation is ever bypassed (req-sc-002).
			destDir := filepath.Join("apm_modules", dep.UniqueKey())
			if !archive.Contained("apm_modules", destDir) {
				return fmt.Errorf("frozen install: refusing to extract %q outside apm_modules", dep.RepoURL)
			}
			f, openErr := os.Open(archivePath)
			if openErr != nil {
				return fmt.Errorf("frozen install: open archive %s: %w", archivePath, openErr)
			}
			_, exErr := archive.SafeExtract(f, destDir, archive.Limits{
				MaxBytes:   deps.maxArchiveBytes,
				MaxEntries: deps.maxEntries,
			})
			f.Close()
			if exErr != nil {
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
				installDir := filepath.Join("apm_modules", dep.UniqueKey())
				if _, statErr := os.Stat(installDir); os.IsNotExist(statErr) {
					ref := &manifest.DependencyReference{
						RepoURL: dep.RepoURL,
						Owner:   ownerFromRepoURL(dep.RepoURL),
						Repo:    repoFromRepoURL(dep.RepoURL),
						Source:  "git",
					}
					resolvedRef := dep.ResolvedRef
					if resolvedRef == "" {
						resolvedRef = dep.ResolvedCommit
					}
					if _, loadErr := deps.loader.LoadPackage(ref, resolvedRef); loadErr != nil {
						return fmt.Errorf("frozen install: download %s: %w", dep.UniqueKey(), loadErr)
					}
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

	// 4. Resolve
	if len(m.ParsedDeps) == 0 {
		fmt.Println("No dependencies to install")
		return nil
	}

	fmt.Println("[>] Installing dependencies from apm.yml...")
	seen := make(map[string]bool)
	for _, dep := range m.ParsedDeps {
		canon := dep.ToCanonical(m.DefaultHost)
		if !seen[canon] {
			seen[canon] = true
			fmt.Printf("[>] Resolving %s...\n", canon)
		}
	}

	result, err := resolver.Resolve(m, existingLock, deps.tags, deps.loader, resolver.ResolverConfig{})
	if err != nil {
		return fmt.Errorf("resolve: %w", err)
	}

	// 5. Build lockfile
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

		// Record skill_subset for positional installs with --skill
		if len(skillSubset) > 0 && len(packages) > 0 {
			ld.SkillSubset = skillSubset
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
					return fmt.Errorf("tree_sha256 for %s: %w", dep.Key, hashErr)
				}
				ld.TreeSHA256 = treeHash
			}
		}

		newLock.Dependencies = append(newLock.Dependencies, ld)
	}

	// 6. Deploy primitives to targets
	targets, targetDiags := deploy.ResolveTargets(targetFlag, m.Target, ".")
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

		if len(skillSubset) > 0 {
			fmt.Printf("[i] Skill subset: %s\n", strings.Join(skillSubset, ", "))
		}

		deployResult, err := deploy.Run(targets, ".", m, result, skillSubset)
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
