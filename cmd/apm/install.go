package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/apm-go/apm/internal/localbundle"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/marketplace"
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
	// allowInsecure permits non-TLS http:// git dependencies (--allow-insecure).
	// Zero value (false) is fail-secure: refuse by default.
	allowInsecure bool
}

func installCmd() *cobra.Command {
	var frozen bool
	var noProvenance bool
	var targetFlag string
	var skillFlags []string
	var maxEntries int
	var maxArchiveBytes int64
	var mcpName string
	var mcpTransport string
	var mcpURL string
	var mcpEnvPairs []string
	var mcpHeaderPairs []string
	var mcpVersion string
	var mcpRegistry string
	var mcpForce bool
	var allowInsecure bool

	cmd := &cobra.Command{
		Use:   "install [packages...]",
		Short: "Install dependencies from apm.yml or by URL/shorthand",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate --target/-t up front, before any other flag routing
			// (mirrors Python's TargetParamType, which validates at CLI
			// argument-parsing time before the command body runs): a
			// genuinely unknown token is rejected naming it, regardless of
			// whether this ends up being a regular package install or an
			// --mcp install (both share this flag). Known targets without a
			// registered adapter (cursor/gemini/windsurf) are valid
			// vocabulary and pass here -- ResolveTargets separately reports
			// the non-fatal "no registered handler" diagnostic for those.
			if targetFlag != "" {
				if _, err := deploy.SplitTargetFlag(targetFlag); err != nil {
					return withExitCode(2, err)
				}
			}

			dashAt := cmd.ArgsLenAtDash()
			var prePackages, stdioCommand []string
			if dashAt >= 0 {
				prePackages = args[:dashAt]
				stdioCommand = args[dashAt:]
			} else {
				prePackages = args
			}

			// cmd.Flags().Changed, not a value/length check, for every
			// MCP-only flag: a value-based check (e.g. mcpTransport != "")
			// misses an explicitly-passed empty value like `--transport ""`
			// or `--registry ""`, which would otherwise silently fall
			// through to a normal package install instead of reporting
			// "requires --mcp" (found by codex review; a first pass only
			// fixed this for --mcp itself, missing the other MCP-only flags
			// and --force entirely).
			mcpGiven := cmd.Flags().Changed("mcp")
			mcpFlagsGiven := cmd.Flags().Changed("transport") || cmd.Flags().Changed("url") ||
				cmd.Flags().Changed("env") || cmd.Flags().Changed("header") ||
				cmd.Flags().Changed("mcp-version") || cmd.Flags().Changed("registry") ||
				cmd.Flags().Changed("force") || dashAt >= 0
			if !mcpGiven && mcpFlagsGiven {
				return fmt.Errorf("--transport, --url, --env, --header, --mcp-version, --registry, --force, and a stdio '--' command all require --mcp")
			}

			// An explicitly-passed EMPTY value (e.g. --url "") is
			// indistinguishable from "not given" once it reaches opts as a
			// plain string, so downstream code (buildPersistEntry etc.)
			// would silently treat it as absent and fall through to a
			// different branch (e.g. --mcp foo --url "" silently becomes a
			// registry lookup for "foo" instead of erroring) -- reject it
			// here, where Changed() is still available (found by codex
			// review: round 5 only fixed the outer requires---mcp gate,
			// missing this same class of gap one level in).
			if cmd.Flags().Changed("url") && mcpURL == "" {
				return fmt.Errorf("--url cannot be empty")
			}
			if cmd.Flags().Changed("transport") && mcpTransport == "" {
				return fmt.Errorf("--transport cannot be empty")
			}
			if cmd.Flags().Changed("registry") && mcpRegistry == "" {
				return fmt.Errorf("--registry cannot be empty")
			}
			if cmd.Flags().Changed("mcp-version") && mcpVersion == "" {
				return fmt.Errorf("--mcp-version cannot be empty")
			}
			if dashAt >= 0 && len(stdioCommand) == 0 {
				return fmt.Errorf("'--' must be followed by a stdio command")
			}

			if mcpGiven {
				return runMCPInstall(mcpInstallOpts{
					Name: mcpName, Transport: mcpTransport, URL: mcpURL,
					EnvPairs: mcpEnvPairs, HeaderPairs: mcpHeaderPairs,
					Version: mcpVersion, Registry: mcpRegistry, Force: mcpForce,
					Command: stdioCommand, PrePackages: prePackages,
					SkillSubset: skillFlags, TargetFlag: targetFlag,
				})
			}

			deps := &installDeps{
				tags: &gitops.RealTagLister{},
				loader: &gitops.RealPackageLoader{
					ModulesDir: "apm_modules",
				},
				maxEntries:      maxEntries,
				maxArchiveBytes: maxArchiveBytes,
				allowInsecure:   allowInsecure,
			}
			return runInstall(deps, frozen, noProvenance, targetFlag, skillFlags, args)
		},
	}

	cmd.Flags().BoolVar(&frozen, "frozen", false, "frozen install mode: lockfile must exist and cover all deps")
	cmd.Flags().BoolVar(&noProvenance, "no-provenance", false, "omit generated_at and apm_version from lockfile")
	cmd.Flags().StringVarP(&targetFlag, "target", "t", "", "explicit target(s) for deployment, comma-separated (overrides auto-detection)")
	cmd.Flags().StringArrayVar(&skillFlags, "skill", nil, "install only named skills from the package (repeatable)")
	cmd.Flags().IntVar(&maxEntries, "max-entries", archive.DefaultMaxEntries, "max archive entries before fail-closed (req-sc-004)")
	cmd.Flags().Int64Var(&maxArchiveBytes, "max-archive-bytes", archive.DefaultMaxBytes, "max uncompressed archive bytes before fail-closed (req-sc-004)")
	cmd.Flags().StringVar(&mcpName, "mcp", "", "add an MCP server entry to apm.yml and deploy it (mutually exclusive with positional packages and --skill)")
	cmd.Flags().StringVar(&mcpTransport, "transport", "", "MCP transport: stdio, http, sse, streamable-http (requires --mcp)")
	cmd.Flags().StringVar(&mcpURL, "url", "", "MCP server URL for http/sse/streamable-http transports (requires --mcp)")
	cmd.Flags().StringArrayVar(&mcpEnvPairs, "env", nil, "environment variable KEY=VALUE for a stdio MCP server, repeatable (requires --mcp)")
	cmd.Flags().StringArrayVar(&mcpHeaderPairs, "header", nil, "HTTP header KEY=VALUE for a remote MCP server, repeatable (requires --mcp and --url)")
	cmd.Flags().StringVar(&mcpVersion, "mcp-version", "", "pin the MCP registry entry to a specific version (requires --mcp)")
	cmd.Flags().StringVar(&mcpRegistry, "registry", "", "MCP registry URL for resolving --mcp NAME (requires --mcp; not valid with --url or a stdio command)")
	cmd.Flags().BoolVar(&mcpForce, "force", false, "overwrite a conflicting existing --mcp entry non-interactively")
	cmd.Flags().BoolVar(&allowInsecure, "allow-insecure", false, "permit direct http:// (non-TLS) dependencies")

	return cmd
}

func runInstall(deps *installDeps, frozen, noProvenance bool, targetFlag string, skillSubset []string, packages []string) error {
	// Local-bundle early-exit (research/pack-parity-findings.md §6; design.md
	// "install <bundle-path> 消費回路"), mirroring Python's install.py:1260's
	// placement: checked before EVERYTHING else in this function -- before
	// apm.yml is even read -- so a bundle path never accidentally falls into
	// the ordinary dependency-resolution pipeline. Only the sole-positional-
	// argument shape is probed (Python: `len(packages) == 1`); zero or
	// multiple positional packages, or an MCP install (already routed to
	// runMCPInstall before runInstall is ever called), never reach here.
	if len(packages) == 1 {
		if handled, err := tryLocalBundleInstall(packages[0], targetFlag, skillSubset, deps.allowInsecure); handled {
			return err
		}
	}

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
	marketplaceProvenance := make(map[string]*marketplace.Provenance)
	// persistPackages mirrors packages 1:1 for the persistPackagesToManifest
	// call in deployAndFinalize below, substituting each marketplace
	// reference's RESOLVED canonical for the raw CLI string (mkt-030): the
	// raw "PLUGIN@MARKETPLACE[#REF]" string isn't even re-parseable by
	// manifest.ParseDepString (it has no "/", so it can't fall through as a
	// git shorthand either) -- persisting it verbatim would leave apm.yml
	// broken for the very next `apm install`. Non-marketplace packages are
	// carried through unchanged, preserving today's exact persisted form.
	persistPackages := make([]string, 0, len(packages))
	if len(packages) > 0 {
		// mi-fix (MI2): key by deploy.DepRefKey (RepoURL, or
		// RepoURL/VirtualPath) instead of bare RepoURL, matching the identity
		// used everywhere else below (requestedKeys, marketplaceProvenance).
		// A bare-RepoURL key wrongly matched a second virtual-path package
		// from the same monorepo against an already-declared dep sharing
		// only the RepoURL, silently dropping it. Local/parent refs have no
		// dep key ("") and are skipped so they don't pollute the map.
		existing := make(map[string]bool)
		for _, d := range m.ParsedDeps {
			if k := deploy.DepRefKey(d); k != "" {
				existing[k] = true
			}
		}
		for _, pkg := range packages {
			ref, provenance, err := resolvePositionalPackage(pkg)
			if err != nil {
				return fmt.Errorf("parse package %q: %w", pkg, err)
			}
			if err := validatePersistableRef(pkg, ref); err != nil {
				return err
			}
			// Compute the apm.yml-persisted form from the ORIGINAL ref, BEFORE
			// normalizeLocalDep rewrites RepoURL into the "_local/<name>" key
			// (ToCanonical would otherwise return that key, not the real local
			// path, and apm.yml would round-trip into a bogus github shorthand).
			persistPkg := pkg
			if provenance != nil {
				canonical := ref.ToCanonical(m.DefaultHost)
				if filepath.IsAbs(canonical) {
					// mkt-025's local-marketplace fast path resolves to an
					// absolute filesystem path. Persist it in the most
					// PORTABLE form apm.yml can round-trip: a project-root-
					// relative "./..." path when the resolved plugin lives
					// inside this project tree (committable, works on any
					// machine); otherwise the absolute path unchanged
					// (machine-specific, but still a valid dependency string
					// -- manifest.ParseDepString accepts an absolute local
					// path -- so the very next `apm install` still parses it
					// back).
					canonical = localPathForManifest(canonical)
				}
				persistPkg = canonical
			}
			persistPackages = append(persistPackages, persistPkg)

			// Normalize a local/absolute-path dependency into its
			// copy-materialized apm_modules form. Done AFTER persistPkg so the
			// dep key (requestedKeys/marketplaceProvenance/existing) and the
			// resolver/deploy/lockfile all agree on the final "_local/<name>"
			// key (F1: an absolute RepoURL was previously joined onto
			// apm_modules verbatim, producing an invalid clone destination).
			normalizeLocalDep(ref)
			key := deploy.DepRefKey(ref)
			requestedKeys[key] = true
			if provenance != nil {
				marketplaceProvenance[key] = provenance
			}
			// mi-fix (MI2): compare against the same key computed above
			// (deploy.DepRefKey), not the bare ref.RepoURL.
			if existing[key] {
				continue
			}
			m.ParsedDeps = append(m.ParsedDeps, ref)
		}
	}

	// 1b-2. Normalize every direct dependency's local/absolute-path form into
	// its copy-materialized apm_modules key (idempotent -- already-normalized
	// positional refs are skipped). Covers apm.yml-declared local deps too, so
	// a bare `apm install` that re-reads a persisted local path (the round-trip
	// after a marketplace-local install) materializes and deploys it the same
	// way the original CLI install did, instead of resolving it in place and
	// silently deploying nothing.
	for _, dep := range m.ParsedDeps {
		normalizeLocalDep(dep)
	}
	for _, dep := range m.ParsedDevDeps {
		normalizeLocalDep(dep)
	}

	// 1c. HTTP dependency policy: refuse non-TLS http:// git dependencies by
	// default -- both the CLI positional packages just merged into
	// m.ParsedDeps above and pre-existing apm.yml dependencies.apm/
	// devDependencies.apm entries -- unless --allow-insecure was passed.
	// Flag-only, no host exemption (Python parity: insecure_policy.py's
	// _check_insecure_dependencies(all_apm_deps, ...), which checks
	// apm_deps + dev_apm_deps together). Must run before any git clone /
	// network fetch (step 4 below).
	for _, dep := range allDirectDeps(m) {
		if err := manifest.CheckInsecureDependencyScheme(dep, deps.allowInsecure, m.DefaultHost); err != nil {
			return err
		}
	}

	// hasAnyDeps mirrors Python's has_any_apm_deps = bool(apm_deps) or
	// bool(dev_apm_deps): a manifest declaring ONLY devDependencies.apm
	// (zero dependencies.apm entries) still has dependencies to install --
	// every len(m.ParsedDeps) gate below that decides "is there anything to
	// resolve/materialize" must consider dev deps too (F3), or a dev-only
	// project silently resolves/deploys/verifies nothing.
	hasAnyDeps := len(m.ParsedDeps) > 0 || len(m.ParsedDevDeps) > 0

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
		// when the manifest declares deps (regular or dev). In verify-only mode
		// (no apm.yml) there is nothing to materialize; (A) is the operative
		// integrity gate.
		if hasAnyDeps {
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
	if !hasAnyDeps {
		fmt.Println("No dependencies to install")
		if len(targets) == 0 {
			for _, d := range targetDiags {
				fmt.Fprintln(os.Stderr, d)
			}
			// Local .apm/ primitives with no target to deploy them to is the
			// same failure as the deps-present zero-target gate below: exit 2
			// with the teaching message instead of silently exiting 0 with the
			// primitives ignored (Python parity: "No harness detected" fires
			// whenever there is ANYTHING to integrate -- deps or local
			// primitives; task 07-11-instructions-applyto-parity req #2). An
			// EMPTY project (no deps, no local primitives) keeps exiting 0.
			if len(deploy.CollectLocalPrimitives(".")) > 0 {
				return errNoDeployTarget()
			}
			return nil
		}
	}

	var result *resolver.ResolutionResult
	var regLoader *registry.Loader
	if !hasAnyDeps {
		result = &resolver.ResolutionResult{}
	} else {
		fmt.Println("[>] Installing dependencies from apm.yml...")
		seen := make(map[string]bool)
		for _, dep := range allDirectDeps(m) {
			canon := dep.ToCanonical(m.DefaultHost)
			if !seen[canon] {
				seen[canon] = true
				fmt.Printf("[>] Resolving %s...\n", canon)
			}
		}

		// Registry access is experimental (API may change); require the opt-in flag
		// before any live registry resolution. Gates network use only — apm.yml
		// registries parsing and lockfile schema stay unconditional.
		for _, d := range allDirectDeps(m) {
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

		result, err = resolver.Resolve(m, existingLock, deps.tags, regLoader, resolver.ResolverConfig{
			MarketplaceResolve: newMarketplaceResolveFunc(),
		})
		if err != nil {
			return fmt.Errorf("resolve: %w", err)
		}
		// mkt-029/033/F1: apm.yml dict-form marketplace dependencies
		// (dependencies.apm entries {name, marketplace, version}) are
		// resolved by the BFS itself now (root and transitive alike), not
		// just the CLI PLUGIN@MARKETPLACE positional-argument path above --
		// merge their provenance into the same map buildLockfile consults.
		mergeMarketplaceProvenance(marketplaceProvenance, result.MarketplaceProvenance)
	}

	// 5. Build lockfile
	newLock, err := buildLockfile(result, existingLock, regLoader, skillSubset, requestedKeys, noProvenance, marketplaceProvenance)
	if err != nil {
		return err
	}

	// There are resolved dependencies to deploy but target resolution came up
	// empty (no --target, no apm.yml target:, no auto-detected harness
	// signal): fail loud with a teaching message and exit 2 (install.md's
	// exit-code table), instead of silently skipping deployment and exiting
	// 0. Checked here -- after resolution/lockfile-build succeed, before any
	// apm.lock.yaml/apm.yml write -- so a doomed install fails closed with
	// zero partial writes rather than persisting a lockfile nothing deployed
	// from. Reuses step-4's targets/targetDiags so an explicit --target of a
	// known-but-adapterless runtime (cursor/gemini/windsurf) still surfaces
	// its "no registered handler" diagnostic (req-tg-004) before we exit.
	if len(result.Deps) > 0 && len(targets) == 0 {
		for _, d := range targetDiags {
			fmt.Fprintln(os.Stderr, d)
		}
		return errNoDeployTarget()
	}

	// 6-9. Deploy primitives, no-op check, write lockfile, persist packages.
	return deployAndFinalize(m, targetFlag, skillSubset, requestedKeys, persistPackages, result, newLock, existingLock, existingNode, node)
}

// errNoDeployTarget is the exit-2 teaching error shared by runInstall's two
// zero-target gates (deps present, and local-primitives-only), so their
// wording and exit code can never drift apart.
func errNoDeployTarget() error {
	return withExitCode(2, fmt.Errorf("no deployment target detected; pass --target <name> or add a target: to apm.yml"))
}

// tryLocalBundleInstall probes bundleArg for a local bundle (a directory,
// .zip, or .tar.gz/.tgz produced by `apm-go pack`), mirroring Python's
// install.py:1260-1331 early-exit. handled=false means bundleArg is not a
// recognized bundle -- the caller falls through to the ordinary
// dependency-resolution install path unchanged. handled=true means this
// call fully owns the outcome (success or failure); the caller must return
// err verbatim without doing anything else.
func tryLocalBundleInstall(bundleArg, targetFlag string, skillSubset []string, allowInsecure bool) (handled bool, err error) {
	fi, statErr := os.Stat(bundleArg)
	if statErr != nil {
		return false, nil // not an existing filesystem path -> not a bundle
	}

	info, detectErr := localbundle.DetectLocalBundle(bundleArg)
	if detectErr != nil {
		return true, fmt.Errorf("bundle security check failed: %w", detectErr)
	}
	if info == nil {
		// IM7: an archive-shaped path that isn't a recognized bundle gets a
		// targeted usage error (naming the extension) instead of silently
		// falling through to the registry-clone path, which would just fail
		// confusingly later. A bare directory (e.g. a local dependency
		// checkout with no plugin.json) DOES fall through -- `apm-go install
		// ./packages/source-pkg` is a supported local-path dependency
		// install, unrelated to bundle detection.
		lower := strings.ToLower(bundleArg)
		if !fi.IsDir() && (strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")) {
			return true, fmt.Errorf("%q is not a valid APM bundle archive (no plugin.json found at the bundle root). "+
				"Use 'apm-go install org/package' for registry installs, or repack the source with 'apm-go pack'", bundleArg)
		}
		return false, nil
	}
	defer info.Cleanup()

	// Local-bundle install is an imperative deploy: it does not interact
	// with the dependency resolver, MCP, or registry machinery, so flags
	// that only make sense for THAT pipeline are rejected with a single
	// consolidated error (Python: install/local_bundle_handler.py:64-75).
	// --mcp is excluded from this set because runInstall is never reached
	// for an --mcp invocation (installCmd routes it to runMCPInstall
	// first) -- mirroring Python's `not mcp_name` gate one level up.
	var rejected []string
	if len(skillSubset) > 0 {
		rejected = append(rejected, "--skill")
	}
	if allowInsecure {
		rejected = append(rejected, "--allow-insecure")
	}
	if len(rejected) > 0 {
		return true, fmt.Errorf("the following flag(s) are not valid with a local bundle install (%s): %s. "+
			"Local-bundle install is an imperative deploy and does not interact with the dependency "+
			"resolver, MCP, registry, or policy machinery", bundleArg, strings.Join(rejected, ", "))
	}

	return true, runLocalBundleInstall(info, bundleArg, targetFlag)
}

// runLocalBundleInstall deploys a detected local bundle: verifies its
// embedded integrity manifest (if any), resolves install targets, warns on
// a bundle/install target mismatch, deploys plugin-native content, and
// persists local_deployed_files/local_deployed_file_hashes to the project
// lockfile -- apm.yml is never touched (imperative deploy, not a
// declarative dependency), mirroring install_local_bundle
// (install/local_bundle_handler.py:34-297).
func runLocalBundleInstall(info *localbundle.BundleInfo, bundleArg, targetFlag string) error {
	if !info.HasLockfile {
		fmt.Fprintln(os.Stderr, "[warn] Bundle has no apm.lock.yaml -- skipping integrity check. "+
			"This bundle may have been produced by an older or non-apm-go tool.")
	} else if !info.HasPackMeta {
		fmt.Fprintln(os.Stderr, "[warn] Bundle has an apm.lock.yaml but no 'pack:' metadata section -- skipping integrity check.")
	} else if errs := localbundle.VerifyBundleIntegrity(info.SourceDir, info.PackMeta); len(errs) > 0 {
		fmt.Fprintln(os.Stderr, "Bundle integrity check failed:")
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  - %s\n", e)
		}
		return fmt.Errorf("bundle integrity check failed for %s", bundleArg)
	}

	targets, targetDiags := deploy.ResolveTargets(targetFlag, nil, ".")
	for _, d := range targetDiags {
		fmt.Fprintln(os.Stderr, d)
	}
	if len(targets) == 0 {
		fmt.Println("[warn] No active targets resolved -- nothing will be deployed. Pass --target to select one explicitly.")
		return nil
	}

	if warning := localbundle.CheckTargetMismatch(info.PackTargets, targets); warning != "" {
		fmt.Fprintf(os.Stderr, "[warn] %s\n", warning)
	}

	result, err := localbundle.IntegrateLocalBundle(info.SourceDir, targets, ".")
	if err != nil {
		return fmt.Errorf("deploy local bundle: %w", err)
	}
	for _, d := range result.Diags {
		fmt.Fprintf(os.Stderr, "[!] %s\n", d)
	}

	if len(result.Files) == 0 {
		fmt.Println("No files deployed from local bundle")
		return nil
	}

	if err := persistLocalBundleDeployment(result); err != nil {
		return err
	}

	fmt.Printf("[+] Installed %d file(s) from local bundle %s\n", len(result.Files), bundleArg)
	return nil
}

// persistLocalBundleDeployment merges result's deployed files/hashes into
// apm.lock.yaml's local_deployed_files/local_deployed_file_hashes,
// mirroring install_local_bundle's lockfile step (install/
// local_bundle_handler.py:189-260, minus the legacy skill-path migration --
// out of scope for this task): union with whatever was already recorded
// (a local bundle install is additive, never replacing a project's own
// local .apm/ primitive deployments or a PRIOR bundle install's files), and
// never touches apm.yml.
func persistLocalBundleDeployment(result *localbundle.IntegrateResult) error {
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

	lf := existingLock
	if lf == nil {
		lf = &lockfile.Lockfile{Version: "1"}
	}

	fileSet := make(map[string]bool, len(lf.LocalDeployedFiles)+len(result.Files))
	for _, f := range lf.LocalDeployedFiles {
		fileSet[f] = true
	}
	for _, f := range result.Files {
		fileSet[f] = true
	}
	merged := make([]string, 0, len(fileSet))
	for f := range fileSet {
		merged = append(merged, f)
	}
	sort.Strings(merged)
	lf.LocalDeployedFiles = merged

	if lf.LocalDeployedHashes == nil {
		lf.LocalDeployedHashes = map[string]string{}
	}
	for f, h := range result.Hashes {
		lf.LocalDeployedHashes[f] = h
	}

	outBytes, err := lockfile.WriteLockfile(lf, existingNode)
	if err != nil {
		return fmt.Errorf("serialize lockfile: %w", err)
	}
	if err := os.WriteFile("apm.lock.yaml", outBytes, 0644); err != nil {
		return fmt.Errorf("write apm.lock.yaml: %w", err)
	}
	return nil
}

// allDirectDeps returns every direct (root) dependency a manifest declares,
// production and dev alike: m.ParsedDeps followed by m.ParsedDevDeps (F3 —
// devDependencies.apm participates in the same direct-dependency scans as
// dependencies.apm: the insecure-http-scheme policy gate, the "Resolving
// X..." progress log, and the registries-experimental-feature gate; mirrors
// Python's all_apm_deps = apm_deps + dev_apm_deps). The resolver's own root
// queue is seeded separately (resolver.collectResolutionRootDeps), not via
// this helper.
func allDirectDeps(m *manifest.Manifest) []*manifest.DependencyReference {
	if len(m.ParsedDevDeps) == 0 {
		return m.ParsedDeps
	}
	all := make([]*manifest.DependencyReference, 0, len(m.ParsedDeps)+len(m.ParsedDevDeps))
	all = append(all, m.ParsedDeps...)
	all = append(all, m.ParsedDevDeps...)
	return all
}

// containsSkillWildcard reports whether skillSubset contains the '*' RESET
// sentinel (install.md: "--skill '*' resets to install all skills"),
// mirroring Python's install.py (~1387-1393): any occurrence -- even mixed
// with other names, e.g. `--skill review --skill '*'` -- means "install ALL
// skills," the same as never passing --skill at all. Every consumer of
// skillSubset (deploy-time filtering via deploy.SkillFilter, lockfile
// skill_subset persistence in buildLockfile, apm.yml skills: persistence in
// persistPackagesToManifest) checks this instead of treating skillSubset as
// a literal name list, so a package's actual skill named "*" (unlikely but
// not impossible) is never mistaken for the reset.
func containsSkillWildcard(skillSubset []string) bool {
	for _, s := range skillSubset {
		if s == "*" {
			return true
		}
	}
	return false
}

// hasMarketplaceProvenance reports whether any of the four mkt-031
// marketplace provenance fields are populated on a locked dependency, used by
// buildLockfile's mkt-032 carry-forward to decide whether an existing
// lockfile entry has anything worth copying forward.
func hasMarketplaceProvenance(d *lockfile.LockedDep) bool {
	return d.DiscoveredVia != "" || d.MarketplacePluginName != "" || d.SourceURL != "" || d.SourceDigest != ""
}

// buildLockfile converts a resolution result into the lockfile that would be
// written for it, without touching disk (steps 5). Shared by runInstall and
// runUpdate so both build the same lockfile shape from a resolution result.
func buildLockfile(result *resolver.ResolutionResult, existingLock *lockfile.Lockfile, regLoader *registry.Loader, skillSubset []string, requestedKeys map[string]bool, noProvenance bool, marketplaceProvenance map[string]*marketplace.Provenance) (*lockfile.Lockfile, error) {
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

		// mkt-031: marketplace provenance is purely additive metadata,
		// attached only to the dependency a CLI `PLUGIN@MARKETPLACE[#REF]`
		// argument actually resolved to (keyed the same way requestedKeys
		// is, via deploy.DepRefKey/resolver's own depKey). source_url/
		// source_digest stay "" unless that marketplace was kind=url --
		// Provenance already only ever carries them in that case.
		//
		// mkt-032 (Go variant, see design.md's "mkt-032 修正" section):
		// buildLockfile rebuilds every LockedDep from scratch on EVERY call,
		// including a bare `apm install` that never re-supplies a
		// marketplace CLI ref. Without a fallback, a dependency discovered
		// via `PLUGIN@MARKETPLACE` on a prior call would silently lose its
		// provenance the instant it's rebuilt without that CLI arg present
		// again -- the from-scratch-rebuild-shaped equivalent of the Python
		// original's known data-loss bug. When this call's own resolution
		// carried no fresh provenance for the dep, carry the four fields
		// forward from the existing lockfile entry sharing the same
		// identity (UniqueKey() -- RepoURL/VirtualPath -- NOT
		// marketplaceProvenance's dep.Key, which is call-scoped).
		if p := marketplaceProvenance[dep.Key]; p != nil {
			ld.DiscoveredVia = p.DiscoveredVia
			ld.MarketplacePluginName = p.MarketplacePluginName
			ld.SourceURL = p.SourceURL
			ld.SourceDigest = p.SourceDigest
		} else if existingLock != nil {
			if existing := existingLock.FindByKey(ld.UniqueKey()); existing != nil && hasMarketplaceProvenance(existing) {
				ld.DiscoveredVia = existing.DiscoveredVia
				ld.MarketplacePluginName = existing.MarketplacePluginName
				ld.SourceURL = existing.SourceURL
				ld.SourceDigest = existing.SourceDigest
			}
		}

		// Record skill_subset only on the dependency this --skill flag was
		// scoped to this call -- not every dep in the resolved graph (bug
		// fix: previously stamped every already-declared, unrelated
		// dependency with the same subset). matchedKeys tracks "this
		// requested package DID resolve" independently of whether a subset
		// is actually recorded, so the '*' RESET sentinel (which records no
		// subset -- install ALL skills) doesn't spuriously fail the
		// "every requested package resolved" validation below.
		if requestedKeys[dep.Key] {
			matchedKeys[dep.Key] = true
			if len(skillSubset) > 0 && !containsSkillWildcard(skillSubset) {
				ld.SkillSubset = skillSubset
			}
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
		// The '*' RESET sentinel means "install ALL skills" (install.md):
		// leave skillFilter nil rather than constructing one with a literal
		// "*" entry, so this call site never depends on deploy.SkillFilter's
		// own wildcard handling to no-op it -- and the "[i] Skill subset:"
		// line, which only makes sense for an actual narrowing, is skipped.
		if len(skillSubset) > 0 && !containsSkillWildcard(skillSubset) {
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

		// Warn about resolved dependencies that deployed zero files to any
		// target -- otherwise "Installed N dependencies" reads as success
		// even when a dependency's primitives went entirely undiscovered
		// (e.g. an unrecognized manifest format).
		for _, dep := range result.Deps {
			if _, ok := deployResult.PerDep[dep.Key]; !ok {
				fmt.Fprintf(os.Stderr, "[!] warning: %s deployed 0 files to any target\n", dep.Key)
			}
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
		// recorded alongside local deployed files (pr-001 per-file source
		// attribution is served by deployResult.MCPProvenance in-memory,
		// not persisted; only the server name list is, via MCPServers below).
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

		// Record the full current set of MCP server names deployed this run
		// (un-060 prerequisite: uninstall's transitive-stale MCP cleanup
		// needs an "old" name list to diff against). deploy.Run recomputes
		// the merged bake from scratch every call, so MCPProvenance already
		// reflects the complete current state, not just a delta -- dedup by
		// name since the same server can appear once per target file.
		if len(deployResult.MCPProvenance) > 0 {
			seen := make(map[string]bool, len(deployResult.MCPProvenance))
			for _, p := range deployResult.MCPProvenance {
				if !seen[p.Server] {
					seen[p.Server] = true
					newLock.MCPServers = append(newLock.MCPServers, p.Server)
				}
			}
			sort.Strings(newLock.MCPServers)
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

	// The '*' RESET sentinel (install.md: "--skill '*' resets to install
	// all skills") means "install ALL skills," so it must never be
	// persisted as a literal skills: ['*'] subset -- mirroring Python's
	// `_skill_subset = None` (no subset persisted = install all).
	skillReset := containsSkillWildcard(skillSubset)

	appended := false
	for _, pkg := range packages {
		if existingPkgs[pkg] {
			// A '*' reset must also undo a previously narrower skills:
			// subset persisted by an earlier `apm install pkg --skill x`
			// for this SAME package -- otherwise a later bare `apm
			// install` (no --skill) would keep reading that stale subset.
			if skillReset {
				clearPersistedSkillSubset(apmSeq, pkg)
			}
			continue
		}
		if len(skillSubset) > 0 && !skillReset {
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
		appended = true
	}

	// mi-fix (#2): a reused pre-existing dependencies.apm sequence node
	// (e.g. a scaffolded `apm: []`) retains its parsed FlowStyle bit, so
	// SafeDump re-renders it flow after appending. Normalize to block
	// (matching dependencies.mcp and the Python original) whenever we
	// actually appended an entry -- leave untouched if nothing changed.
	if appended {
		apmSeq.Style &^= yamllib.FlowStyle
	}

	return nil
}

// clearPersistedSkillSubset undoes a previously narrower skills: subset
// recorded for pkg by an earlier `apm install pkg --skill x`: the '*' RESET
// sentinel (install.md) means "install all skills going forward," so a
// stale object-form entry `{git: pkg, skills: [...]}` must collapse back to
// the plain string form -- otherwise a bare `apm install` (no --skill) run
// afterward would keep reading the stale narrower subset from apm.yml.
// No-op for a scalar entry or an object-form entry with no skills: key.
func clearPersistedSkillSubset(apmSeq *yamllib.Node, pkg string) {
	if apmSeq.Kind != yamllib.SequenceNode {
		return
	}
	for i, entry := range apmSeq.Content {
		if entry.Kind != yamllib.MappingNode {
			continue
		}
		var gitVal string
		hasSkills := false
		for j := 0; j < len(entry.Content)-1; j += 2 {
			switch entry.Content[j].Value {
			case "git":
				gitVal = entry.Content[j+1].Value
			case "skills":
				hasSkills = true
			}
		}
		if gitVal == pkg && hasSkills {
			apmSeq.Content[i] = &yamllib.Node{Kind: yamllib.ScalarNode, Value: pkg, Tag: "!!str"}
		}
	}
}

// resolvePositionalPackage parses a single `apm install` positional package
// argument, recognizing the mkt-020/021 CLI syntax
// "PLUGIN@MARKETPLACE[#REF]" via marketplace.ParseRef before falling back to
// the ordinary dependency-string parser (manifest.ParseDepString) --
// design.md's "interception layer decision": marketplace.ParseRef is the
// ONLY place CLI package-argument parsing may decide something is a
// marketplace reference (mkt-029); this function must never grow its own
// parallel "/" or ":" pre-check.
//
// The returned Provenance is non-nil only when pkg was recognized as a
// marketplace reference; callers attach it to the resulting dependency's
// lockfile entry (mkt-031), keyed the same way as requestedKeys.
func resolvePositionalPackage(pkg string) (*manifest.DependencyReference, *marketplace.Provenance, error) {
	plugin, mkt, ref, ok, err := marketplace.ParseRef(pkg)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		d, err := manifest.ParseDepString(pkg)
		return d, nil, err
	}

	res, err := marketplace.ResolvePlugin(context.Background(), plugin, mkt, marketplace.ResolveOptions{VersionSpec: ref})
	if err != nil {
		return nil, nil, err
	}
	// mkt-034: ref-swap-pin/shadow advisories are never blocking -- surface
	// them and keep going.
	for _, w := range res.Warnings {
		fmt.Fprintf(os.Stderr, "[!] %s\n", w)
	}

	// mkt-027: a structured DepRef (a non-GitHub-family host's
	// in-marketplace subdirectory plugin) always wins over parsing
	// Canonical -- it already carries the decisions Canonical alone
	// couldn't represent unambiguously.
	if res.DepRef != nil {
		return res.DepRef, res.Provenance, nil
	}

	d, err := depRefFromMarketplaceCanonical(res.Canonical)
	if err != nil {
		return nil, nil, err
	}
	return d, res.Provenance, nil
}

// depRefFromMarketplaceCanonical parses a marketplace.Resolution's Canonical
// string through the SAME pipeline an ordinary positional package argument
// already goes through (manifest.ParseDepString), with one necessary
// extension: mkt-025's local-marketplace fast path produces an ABSOLUTE
// local filesystem path (marketplace `add`'s SOURCE parser always
// canonicalizes via filepath.Abs). Although manifest.ParseDepString now
// itself accepts an absolute path (as an IsLocal=true "local" dependency,
// for apm.yml round-tripping), that is NOT what this call site wants: this
// mirrors the SAME normalization runInstall already applies to an ordinary
// local positional package (resolvePositionalPackage's caller) -- forced
// straight into a "git" source pointing at that path (a real git-clone
// fetch into apm_modules), never ParseDepString's own "local" dependency
// kind (a direct, un-cloned reference with no apm_modules materialization).
func depRefFromMarketplaceCanonical(canonical string) (*manifest.DependencyReference, error) {
	if filepath.IsAbs(canonical) {
		return &manifest.DependencyReference{RepoURL: canonical, Source: "git"}, nil
	}
	d, err := manifest.ParseDepString(canonical)
	if err != nil {
		return nil, fmt.Errorf("marketplace canonical %q: %w", canonical, err)
	}
	return d, nil
}

// localPathForManifest converts an absolute local filesystem path resolved
// by mkt-025's local-marketplace fast path into the form persisted to
// apm.yml, per this task's approved design: a portable "./"-relative path
// when abs lives inside the current project root, or the absolute path
// unchanged when it falls outside the project tree. The project root is the
// process's current directory -- runInstall always reads/writes apm.yml via
// the bare relative path "apm.yml", so cwd IS the project root throughout
// this pipeline. Falls back to returning abs unchanged if cwd or the
// relative-path computation can't be determined (never worse than today's
// prior absolute-path behavior).
func localPathForManifest(abs string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return abs
	}
	rel, err := filepath.Rel(cwd, abs)
	if err != nil {
		return abs
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		// Outside the project tree -- relativizing would need a leading
		// ".." escaping the root, which manifest.ParseDepString's
		// containsEscape guard would then reject on the next read. Persist
		// the absolute path instead (accepted by ParseDepString's dedicated
		// absolute-path branch, which skips that guard entirely).
		return abs
	}
	if !strings.HasPrefix(rel, "./") {
		rel = "./" + rel
	}
	return rel
}

// normalizeLocalDep rewrites a local-directory dependency in place into the
// form apm-go materializes by COPYING into apm_modules (Python's local-dep
// model: apm_modules/_local/<name>/), instead of git-cloning it. It targets
// two indistinguishable-downstream shapes:
//
//	(a) a genuine local dep (IsLocal, from a bare path in apm.yml or a CLI
//	    positional arg like "./pkg" or "/abs/pkg"); and
//	(b) mkt-025's local-marketplace fast path result (Source=="git" with an
//	    empty Owner/Repo and an ABSOLUTE RepoURL, no git ref).
//
// Both are converted to {Source:"git", RepoURL:"_local/<name>", LocalSourcePath:
// <abs source>}. RepoURL becomes the sanitized, contained apm_modules key that
// the resolver (depKey), deploy (DepRefKey), and the lockfile (repo_url) all
// use, so no filepath.Join(apm_modules, key) ever sees an absolute path. The
// real source path rides on LocalSourcePath for the loader's copy step and is
// never persisted. RELATIVE git-local paths (git: ./remote, which may pin a
// ref and clone) are deliberately NOT matched -- only absolute ones, which the
// clone path cannot handle at all. No-op (returns without change) for anything
// else, and idempotent (an already-"_local/…" ref matches neither shape).
func normalizeLocalDep(ref *manifest.DependencyReference) {
	var src string
	switch {
	case ref.IsLocal && ref.LocalPath != "":
		src = ref.LocalPath
	case !ref.IsLocal && ref.Source == "git" && ref.Owner == "" && ref.Repo == "" &&
		ref.Reference == "" && ref.RepoURL != "" && manifest.IsAbsoluteLocalPath(ref.RepoURL):
		src = ref.RepoURL
	default:
		return
	}

	abs := resolveLocalSourceAbs(src)
	ref.IsLocal = false
	ref.LocalPath = ""
	ref.Owner = ""
	ref.Repo = ""
	ref.VirtualPath = ""
	ref.VirtualType = ""
	ref.Reference = ""
	ref.Source = "git"
	ref.RepoURL = localModulesKey(abs)
	ref.LocalSourcePath = abs
}

// resolveLocalSourceAbs turns a local dependency source (which may be
// absolute, "~"-prefixed, or relative to the project root) into a cleaned
// absolute path. A relative path is anchored at cwd, which IS the project root
// throughout runInstall. Falls back to a cleaned form if the absolute
// conversion fails, so the value is always deterministic for the same input.
func resolveLocalSourceAbs(src string) string {
	s := src
	if strings.HasPrefix(s, "~/") || strings.HasPrefix(s, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			s = filepath.Join(home, s[2:])
		}
	}
	if abs, err := filepath.Abs(s); err == nil {
		return abs
	}
	return filepath.Clean(s)
}

// localModulesKey derives a stable, path-safe, contained apm_modules key for a
// copy-materialized local dependency, mirroring Python's apm_modules/_local/
// <name>/ layout but appending a short hash of the absolute source path so two
// different sources sharing a basename (e.g. two ".../hello" dirs) never
// collide on the same key. Deterministic for a given absolute source, so a
// re-install (or the round-trip second `apm install`) reuses the same key.
func localModulesKey(abs string) string {
	base := sanitizePathSegment(filepath.Base(abs))
	sum := sha256.Sum256([]byte(filepath.Clean(abs)))
	return "_local/" + base + "-" + hex.EncodeToString(sum[:])[:8]
}

// sanitizePathSegment reduces s to a single safe path segment: every character
// outside [A-Za-z0-9._-] becomes '_', and a leading '.'/empty result is
// replaced so the segment is never "", ".", "..", or a hidden dotfile. This
// guarantees the derived apm_modules key carries no path separators or ".."
// that could defeat archive.ContainedKey.
func sanitizePathSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" || out == "." || out == ".." || strings.HasPrefix(out, ".") {
		out = "pkg" + strings.TrimLeft(out, ".")
	}
	return out
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
