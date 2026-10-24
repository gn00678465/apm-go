package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
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
	"github.com/apm-go/apm/internal/pack/bundle"
	"github.com/apm-go/apm/internal/registry"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/apm-go/apm/internal/ux"
	"github.com/apm-go/apm/internal/version"
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
	// allowInsecure is --allow-insecure, and allowInsecureHosts the
	// normalized --allow-insecure-host values (install_insecure.go). The
	// zero values let no http:// dependency through.
	allowInsecure      bool
	allowInsecureHosts []string
	// verbose enables --verbose-only detail in low-priority presentation
	// paths (R12d, prd.md/design.md §3) -- currently only the frozen-install
	// success summary. The zero value (false) keeps every default output
	// byte-identical to before this field existed.
	verbose bool
	// dev routes positional packages into devDependencies.apm instead of
	// dependencies.apm (--dev, R9/AC42-45, design.md §11). The zero value
	// (false) keeps every call without --dev byte-identical to before this
	// field existed (AC44's regression gate).
	dev bool
}

func installCmd() *cobra.Command {
	var frozen bool
	var noProvenance bool
	var targetFlag string
	var skillFlags []string
	var maxEntries int
	var maxArchiveBytes int64
	var verbose bool
	var mcpName string
	var mcpTransport string
	var mcpURL string
	var mcpEnvPairs []string
	var mcpHeaderPairs []string
	var mcpVersion string
	var mcpRegistry string
	var mcpForce bool
	var allowInsecure bool
	var allowInsecureHosts []string
	var dev bool
	var globalFlag bool

	cmd := &cobra.Command{
		Use:   "install [packages...]",
		Short: "Install dependencies from apm.yml or by URL/shorthand",
		RunE: func(cmd *cobra.Command, args []string) (runErr error) {
			defer silenceUsageForRuntimeError(cmd, &runErr)
			insecureHosts, err := normalizeInsecureHosts(allowInsecureHosts)
			if err != nil {
				return withUsageError(err)
			}
			var deployDir string
			if globalFlag {
				var err error
				deployDir, err = enterGlobalScope()
				if err != nil {
					return err
				}
			}

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
				err := runMCPInstall(mcpInstallOpts{
					Name: mcpName, Transport: mcpTransport, URL: mcpURL,
					EnvPairs: mcpEnvPairs, HeaderPairs: mcpHeaderPairs,
					Version: mcpVersion, Registry: mcpRegistry, Force: mcpForce,
					Command: stdioCommand, PrePackages: prePackages,
					SkillSubset: skillFlags, TargetFlag: targetFlag,
					DeployDir: deployDir, NoProvenance: noProvenance, Dev: dev,
				})
				var mh *multipleHarnessError
				var us *userScopeMCPError
				var ndt *noDeployTargetError
				if errors.As(err, &mh) || errors.As(err, &us) || errors.As(err, &ndt) {
					cmd.SilenceUsage = true
				}
				return err
			}

			deps := &installDeps{
				tags:               &gitops.RealTagLister{},
				loader:             newGitPackageLoader("apm_modules"),
				maxEntries:         maxEntries,
				maxArchiveBytes:    maxArchiveBytes,
				allowInsecure:      allowInsecure,
				allowInsecureHosts: insecureHosts,
				verbose:            verbose,
				dev:                dev,
			}
			err = runInstall(deps, frozen, noProvenance, targetFlag, deployDir, skillFlags, args)
			// R17 (codex H8): suppress cobra's default usage dump for JUST
			// the no-deployment-target diagnostic -- it is a structured,
			// self-contained teaching message (scanned markers + concrete
			// fixes), not a flag/argument mistake the 14-line flag usage
			// dump would help with. unknownSkillError is the same class:
			// the flag syntax is correct, the named skill just doesn't
			// exist in the resolved package.
			var ndt *noDeployTargetError
			var usk *unknownSkillError
			var mh *multipleHarnessError
			if errors.As(err, &ndt) || errors.As(err, &usk) || errors.As(err, &mh) {
				cmd.SilenceUsage = true
			}
			return err
		},
	}

	cmd.Flags().BoolVar(&frozen, "frozen", false, "frozen install mode: lockfile must exist and cover all deps")
	cmd.Flags().BoolVar(&noProvenance, "no-provenance", false, "omit apm_version from lockfile")
	cmd.Flags().StringVarP(&targetFlag, "target", "t", "", "explicit target(s) for deployment; use commas for multiple targets; repeating the flag keeps only the last value (use commas instead); overrides auto-detection")
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
	cmd.Flags().BoolVar(&allowInsecure, "allow-insecure", false, "Allow HTTP (insecure) dependencies. Required when dependencies use http:// URLs.")
	cmd.Flags().StringArrayVar(&allowInsecureHosts, "allow-insecure-host", nil, "Allow transitive HTTP (insecure) dependencies from this hostname. Repeat for multiple hosts.")
	cmd.Flags().BoolVar(&dev, "dev", false, "install positional packages into devDependencies.apm instead of dependencies.apm")
	cmd.Flags().BoolVarP(&globalFlag, "global", "g", false, "user-scope install: manage packages under ~/.apm/ instead of the project directory")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false,
		"print extra diagnostics (currently: list every pinned dependency after a successful --frozen install)")

	return cmd
}

func runInstall(deps *installDeps, frozen, noProvenance bool, targetFlag, deployDir string, skillSubset []string, packages []string) (runErr error) {
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
	started := time.Now()
	// newModuleDirs are the apm_modules directories of the packages this run
	// fetched; an install the deploy refuses removes them.
	var newModuleDirs []string
	// manifestBefore is apm.yml as it was before the run, when it existed.
	var manifestBefore []byte
	defer func() {
		var refused *installRefusedError
		if errors.As(runErr, &refused) {
			removeNewLocalCopies(newModuleDirs)
			refused.printEnd()
			runErr = withSilentExitCode(1, runErr)
		}
		var mcpFailed *mcpFailedError
		if errors.As(runErr, &mcpFailed) {
			removeNewLocalCopies(newModuleDirs)
			restoreManifest(manifestBefore)
			mcpFailed.printEnd()
			runErr = withSilentExitCode(1, runErr)
		}
		var outside *resolveRefusedError
		if errors.As(runErr, &outside) {
			outside.print()
			runErr = withSilentExitCode(1, runErr)
		}
	}()

	// Determine frozen mode up front (explicit flag or CI default) so apm.yml can
	// be optional in frozen verify-only mode (integrity is checked from lockfile+disk).
	if !frozen && lockfile.IsCIEnvironment() {
		frozen = true
		ux.Info(os.Stderr, "CI environment detected, defaulting to frozen install")
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

	// --dev requires an actual package to route into devDependencies.apm,
	// and frozen installs never reach the manifest-write path at all: a
	// frozen run returns from the "3. Frozen install" branch below (well
	// before persistPackagesToManifest), so `--frozen --dev X` against an
	// already-declared dependency previously printed
	// moveDependencyBetweenSections' "moved from ... to devDependencies.apm"
	// info message (a pure in-memory mutation of m, made before frozen mode
	// is even checked) and then silently discarded it -- a misleading
	// success-shaped message for a change that was never persisted. Reject
	// up front, mirroring the --skill guard immediately above.
	if deps.dev {
		if frozen {
			return fmt.Errorf("--dev is not supported with a frozen install (frozen installs pin exactly what's locked, with no section changes)")
		}
		if len(packages) == 0 {
			return fmt.Errorf("--dev requires at least one positional package to install")
		}
	}

	if deployDir != "" && !frozen {
		// install/manifest_helpers.py:63-74. Deliberate deviation by owner
		// ruling (issue #122 comment 6232580233): the Oracle's warning after
		// it, which names the user-scope support of its 16 targets
		// (core/scope.py:228-276), is not printed, since apm-go deploys to 6.
		ux.Info(os.Stdout, "Installing to user scope (~/.apm/)")
	}

	// 1. Parse apm.yml — optional in frozen mode.
	var m *manifest.Manifest
	var node *yamllib.Node
	data, err := os.ReadFile("apm.yml")
	if err == nil {
		manifestBefore = data
	}
	manifestCreated := false
	if !frozen && len(packages) > 0 && os.IsNotExist(err) {
		if data, err = createInstallManifest(targetFlag, deployDir != ""); err != nil {
			return err
		}
		manifestCreated = true
		defer func() {
			if runErr != nil {
				removeCreatedManifest()
			}
		}()
	}
	if err != nil {
		switch {
		case frozen && os.IsNotExist(err):
			m = &manifest.Manifest{} // frozen verifies from lockfile + disk alone
		case os.IsNotExist(err):
			return errNoManifest(deployDir != "")
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
	// existing tracks every dep key ALREADY declared in apm.yml before this
	// call's positional packages are merged in below (deploy.DepRefKey,
	// matching requestedKeys's identity). The install summary (in
	// deployAndFinalize) uses requestedKeys+existing together to tell a
	// genuinely new addition from a dep the user re-requested that was
	// already declared, so it can mute the latter (R9/R10c) instead of
	// presenting it as a fresh install.
	//
	// A pre-existing local/absolute-path dep MUST be normalized (into its
	// copy-materialized apm_modules/_local/<name> key, step 1b-2's job)
	// BEFORE it's used to key this map: a still-IsLocal ref has no
	// deploy.DepRefKey at all ("" -- skipped by the guard below), while the
	// SAME dependency re-requested positionally this call gets normalized
	// (and keyed) immediately in the loop right below. Without normalizing
	// here first, that key-space mismatch made `existing` silently miss an
	// already-declared local-path dependency on every subsequent positional
	// re-install of it, duplicating its apm.yml/lockfile entry instead of
	// recognizing it as already declared. m.ParsedDevDeps is normalized here
	// too (external Tier-2 audit finding, scenario A/B below) so its keys
	// are computed the same way BEFORE existing/existingIsDev is indexed.
	// Decided before anything is written: apm.yml, apm_modules and the lock
	// are untouched when two local packages collide.
	locals := make(localSources)
	for _, d := range m.ParsedDeps {
		normalizeLocalDep(d)
		if err := locals.add(d); err != nil {
			return err
		}
	}
	for _, d := range m.ParsedDevDeps {
		normalizeLocalDep(d)
		if err := locals.add(d); err != nil {
			return err
		}
	}
	existing := make(map[string]bool)
	// existingIsDev tracks, for every key in `existing`, which section it is
	// CURRENTLY declared in (true = devDependencies.apm, false =
	// dependencies.apm). Used below to detect a positional re-install whose
	// --dev flag (or lack thereof) targets the OTHER section than where the
	// dependency is presently declared -- external Tier-2 audit finding: the
	// dedup below previously only ever indexed m.ParsedDeps, so re-installing
	// an already-declared devDependencies.apm entry silently appended a
	// SECOND, duplicate entry into dependencies.apm (and vice versa) instead
	// of relocating it. Per the 2026-07-30 decision (npm `npm i [-D] X`
	// parity), the fix is to MOVE the dependency to the section this call's
	// flag names (moveDependencyBetweenSections below), not to leave it
	// alone or duplicate it.
	existingIsDev := make(map[string]bool)
	// existingByIdentity indexes m.ParsedDeps/m.ParsedDevDeps by canonical
	// repo identity (BUG-1, design.md §2/H2/H7): a positional package
	// sharing GitHub repository identity with an already-declared
	// dependency -- even spelled with different case ("Owner/Repo" vs
	// "owner/repo") -- must resolve to that SAME first-declared dep key
	// below, never appended as a second entry the resolver/deploy would then
	// process as if it were a genuinely different repository (the root
	// cause of "Resolved 2" for one physical repo, shadowed-primitive
	// noise, and a "deployed 0 files" ghost). Local/parent refs have no
	// stable identity (deploy.CanonicalDepKey returns "") and are
	// intentionally excluded -- they keep relying on the exact-string
	// `existing` map above/below.
	existingByIdentity := make(map[string]string)
	// foldedRefByKey remembers which literal ref currently "owns" each
	// first-declared dep key, purely so a same-identity fold below (either
	// against a pre-existing apm.yml entry or an earlier positional package
	// THIS call already processed) can compare selectors (design.md §0/§2:
	// same identity, different ref/selector must never be silently merged
	// without at least a warning -- a fold onto a different Reference is
	// the first-declared ref winning, but the caller is told about it).
	foldedRefByKey := make(map[string]*manifest.DependencyReference)
	indexDeclaredSection := func(refs []*manifest.DependencyReference, isDev bool) {
		for _, d := range refs {
			if k := deploy.DepRefKey(d); k != "" {
				existing[k] = true
				existingIsDev[k] = isDev
				foldedRefByKey[k] = d
				if id := deploy.CanonicalDepKey(d); id != "" {
					if _, ok := existingByIdentity[id]; !ok {
						existingByIdentity[id] = k
					}
				}
			}
		}
	}
	indexDeclaredSection(m.ParsedDeps, false)
	indexDeclaredSection(m.ParsedDevDeps, true)
	// persistPackages mirrors packages 1:1 for the persistPackagesToManifest
	// call in deployAndFinalize below, substituting each marketplace
	// reference's RESOLVED canonical for the raw CLI string (mkt-030): the
	// raw "PLUGIN@MARKETPLACE[#REF]" string isn't even re-parseable by
	// manifest.ParseDepString (it has no "/", so it can't fall through as a
	// git shorthand either) -- persisting it verbatim would leave apm.yml
	// broken for the very next `apm install`. Non-marketplace packages are
	// carried through unchanged, preserving today's exact persisted form.
	persistPackages := make([]string, 0, len(packages))
	// appendedThisCall tracks a dep key ALREADY appended to m.ParsedDeps
	// during THIS loop -- kept separate from `existing` (which must keep
	// meaning "declared before this call" for the R9/R10c "already in
	// apm.yml" label below) so a THIRD or later positional package folding
	// onto the SAME canonical identity as an earlier positional package in
	// this same call (BUG-1) is still recognized as a duplicate and skipped,
	// without marking the first, genuinely-new occurrence as pre-existing.
	appendedThisCall := make(map[string]bool)
	// manifestChanged is set whenever apm.yml's entries change THIS call:
	// moveDependencyBetweenSections below relocates an already-declared
	// dependency between dependencies.apm/devDependencies.apm, or an
	// already-declared dependency gets another ref. It must gate the
	// no-op check in deployAndFinalize alongside lockfile.IsSemanticEqual:
	// a section move can leave the LOCKFILE content unchanged (e.g. a
	// legacy lock entry whose package_type was never populated by an
	// earlier version of apm-go reads as "" both before and after moving
	// the dependency OUT of devDependencies.apm, since the post-move value
	// is also ""), in which case IsSemanticEqual alone would report a false
	// no-op and persistPackagesToManifest -- the only place the actual
	// apm.yml section move gets written -- would never run (2026-07-30
	// Tier-2 finding).
	manifestChanged := false
	// The lines the Oracle prints while it validates positional packages
	// (core/command_logger.py:276-295, install/manifest_helpers.py:40). A
	// frozen install keeps its own output.
	announce := !frozen && len(packages) > 0
	newPackages := 0
	updatedEntries := 0
	// heldLines are the lines from "Updated apm.yml" to the last "Resolving",
	// printed once the resolve has returned. apm.yml is written at the end of
	// the run, so "Updated apm.yml" is true only when the run gets there: a
	// run that fails up to and in the resolve does not print it.
	var heldLines []heldLine
	invalid := 0
	if announce {
		ux.Success(os.Stdout, "Validating %d %s...", len(packages), pluralWord(len(packages), "package", "packages"))
	}
	if len(packages) > 0 {
		// mi-fix (MI2): key by deploy.DepRefKey (RepoURL, or
		// RepoURL/VirtualPath) instead of bare RepoURL, matching the identity
		// used everywhere else below (requestedKeys, marketplaceProvenance).
		// A bare-RepoURL key wrongly matched a second virtual-path package
		// from the same monorepo against an already-declared dep sharing
		// only the RepoURL, silently dropping it. Local/parent refs have no
		// dep key ("") and are skipped so they don't pollute the map.
		for _, pkg := range packages {
			ref, provenance, err := resolvePositionalPackage(pkg)
			if err != nil {
				return fmt.Errorf("parse package %q: %w", pkg, err)
			}
			if err := validatePersistableRef(pkg, ref); err != nil {
				return err
			}
			if announce && deployDir != "" && relativeAtUserScope(ref) {
				ux.Error(os.Stdout, "%s -- %s", pkg, userScopeRelativeReason)
				invalid++
				continue
			}
			// install/validation.py:155-164.
			if announce && isMissingLocalPath(ref) {
				ux.Error(os.Stdout, "%s -- path does not exist", pkg)
				invalid++
				continue
			}
			// install/validation.py:166-168,241-263: a directory that is no
			// package.
			if dir, ok := localDirWithoutMarkers(ref); announce && ok {
				printNoMarkersHint(dir, deps.verbose)
				// A marketplace plugin is named by its source directory, as
				// Oracle 0.33.0 names it (measured, 128-2/mkt-r1).
				name := pkg
				if provenance != nil {
					name = dir
				}
				ux.Error(os.Stdout, "%s -- no apm.yml, SKILL.md, or plugin.json found", name)
				invalid++
				continue
			}
			// commands/install.py:418-430: an http:// package is valid only
			// with --allow-insecure, and is then declared with
			// allow_insecure: true.
			if ref.IsInsecure() {
				if !deps.allowInsecure {
					printInsecureRefusal(manifest.InsecureDependencyRequirements(manifest.InsecureDependencyURL(ref, m.DefaultHost), true, true))
					invalid++
					continue
				}
				ref.AllowInsecure = true
			}
			// Compute the apm.yml-persisted form from the ORIGINAL ref, BEFORE
			// normalizeLocalDep rewrites RepoURL into the "_local/<name>" key
			// (ToCanonical would otherwise return that key, not the real local
			// path, and apm.yml would round-trip into a bogus github shorthand).
			persistPkg := pkg
			if provenance != nil {
				canonical := ref.ToCanonical(m.DefaultHost)
				// At user scope the absolute path is kept, as Oracle 0.33.0
				// writes it into ~/.apm/apm.yml (measured, issue #128), because
				// a relative local path is refused there.
				if filepath.IsAbs(canonical) && deployDir == "" {
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
			if err := locals.add(ref); err != nil {
				return err
			}
			key := deploy.DepRefKey(ref)
			// BUG-1: fold this pkg onto the first-declared dep key sharing
			// its canonical repo identity -- whether that first declaration
			// came from apm.yml (existingByIdentity was seeded above) or an
			// EARLIER positional package this same call already processed
			// (the else-branch below records it as soon as it's seen, so a
			// third/fourth same-repo case variant in one call folds onto the
			// first one too, not just a 1:1 pair).
			if identity := deploy.CanonicalDepKey(ref); identity != "" {
				if firstKey, ok := existingByIdentity[identity]; ok {
					// A frozen install writes no apm.yml and keeps the first-declared
					// ref, as before issue #156; the other installs rewrite it below.
					if winner := foldedRefByKey[firstKey]; frozen && winner != nil && winner.Reference != ref.Reference {
						ux.Warn(os.Stderr, "%s: ref %q conflicts with already-declared %q (%s#%s) -- keeping the first-declared ref",
							pkg, ref.Reference, firstKey, firstKey, winner.Reference)
					}
					key = firstKey
				} else {
					existingByIdentity[identity] = key
					foldedRefByKey[key] = ref
				}
			}
			requestedKeys[key] = true
			if provenance != nil {
				marketplaceProvenance[key] = provenance
			}
			// mi-fix (MI2): compare against the same key computed above
			// (deploy.DepRefKey), not the bare ref.RepoURL. existing itself is
			// intentionally left untouched here -- it distinguishes "declared
			// BEFORE this call" (R9/R10c's "already in apm.yml" label) from a
			// genuinely new addition. appendedThisCall additionally catches a
			// third/later positional package folding onto an identity a
			// PRECEDING positional package (not apm.yml) already added this
			// same call (BUG-1).
			if existing[key] && !appendedThisCall[key] {
				// External Tier-2 audit finding (scenario A/B): a package
				// already declared, but in the OTHER section than this
				// call's --dev flag names, must be MOVED there -- never left
				// alone (which would leave devDependencies.apm/package_type
				// stale, scenario B) and never duplicated into both sections
				// (scenario A).
				if existingIsDev[key] != deps.dev {
					if moveDependencyBetweenSections(m, key, deps.dev) {
						manifestChanged = true
					}
					existingIsDev[key] = deps.dev
				}
				appendedThisCall[key] = true
				// Oracle 0.33.0 rewrites the declared entry to the ref this call
				// names and says so, whether or not the ref changed
				// (install/package_resolution.py:345-378,
				// core/command_logger.py:289-291). apm-go changes the ref alone
				// (issue #156 comment 6290306546, item 1).
				winner := foldedRefByKey[key]
				if frozen {
					continue
				}
				if !declaredEntryUpdated(winner) {
					if announce {
						ux.Success(os.Stdout, "%s (already in apm.yml)", persistPkg)
					}
					continue
				}
				if declaredRefRewritable(winner) && winner.Reference != ref.Reference {
					winner.Reference = ref.Reference
					manifestChanged = true
				}
				updatedEntries++
				if announce {
					ux.Success(os.Stdout, "%s (updated ref in apm.yml)", persistPkg)
				}
				continue
			}
			// A second package of this call with the same identity: the first one
			// decided the ref (Oracle 0.33.0, install/package_resolution.py).
			if appendedThisCall[key] {
				if announce {
					ux.Success(os.Stdout, "%s (already in apm.yml)", persistPkg)
				}
				continue
			}
			appendedThisCall[key] = true
			newPackages++
			if announce {
				ux.Success(os.Stdout, "%s", persistPkg)
			}
			// R9/AC42: --dev routes this call's positional packages into
			// devDependencies.apm instead of dependencies.apm. allDirectDeps
			// (resolver root seeding, allDirectDeps-based scans) already
			// treats m.ParsedDevDeps uniformly with m.ParsedDeps, so no other
			// change is needed for resolution/deploy to pick this up.
			if deps.dev {
				m.ParsedDevDeps = append(m.ParsedDevDeps, ref)
			} else {
				m.ParsedDeps = append(m.ParsedDeps, ref)
			}
		}
	}
	if announce {
		// core/command_logger.py:302-315.
		if invalid == len(packages) {
			ux.Plain(os.Stdout, "All packages failed validation. Nothing to install.")
			return withSilentExitCode(1, errors.New("all packages failed validation"))
		}
		if invalid > 0 {
			ux.Warn(os.Stdout, "%d %s failed validation and will be skipped.", invalid, pluralWord(invalid, "package", "packages"))
		}
		// apm.yml itself is written at the end of the run, with the lock.
		if newPackages > 0 {
			heldLines = append(heldLines, heldLine{onlyOnSuccess: true, print: func() {
				ux.Success(os.Stdout, "Updated apm.yml with %d new package(s)", newPackages)
			}})
		} else if updatedEntries > 0 {
			// package_resolution.py:476.
			heldLines = append(heldLines, heldLine{onlyOnSuccess: true, print: func() {
				ux.Success(os.Stdout, "Updated apm.yml dependency entries")
			}})
		} else {
			ux.Progress(os.Stdout, "No new packages to add")
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
	if deployDir != "" {
		if err := checkUserScopeLocalPaths(m); err != nil {
			return err
		}
	}

	// 1c. HTTP dependency policy of the direct dependencies, regular and
	// dev, before anything is fetched (commands/install.py:1819); a frozen
	// install checks its lock first (:1762) and this after.
	if !frozen {
		if err := checkInsecureDirect(allDirectDeps(m), deps.allowInsecure, m.DefaultHost); err != nil {
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

	// A project that detects several harnesses and names no target stops
	// before anything is fetched or written, a frozen install too
	// (core/target_detection.py:1137-1146).
	if deployDir == "" {
		if t := resolveInstallTargets(targetFlag, m.Target, ""); len(t.ambiguous) > 0 && hasContentToIntegrate(m) {
			return errMultipleHarnesses(t.ambiguous)
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
		if err := checkFrozenManifestSync(allDirectDeps(m), existingLock); err != nil {
			return err
		}
		if err := checkInsecureDirect(allDirectDeps(m), deps.allowInsecure, m.DefaultHost); err != nil {
			return err
		}
		useLegacyLocalKeys(m, existingLock)
		useLocalKeysForGitPaths(allDirectDeps(m), existingLock)
		useLockedAliases(allDirectDeps(m), existingLock)
		if err := checkFrozenCommitPins(m.Name, existingLock); err != nil {
			return err
		}

		// (A) Disk-only integrity — verified from lockfile + disk, before any
		// network fetch or source materialization, without requiring apm.yml.

		// (A1) Re-verify deployed-file hashes (req-lk-017 / req-sc-001). MUST run
		// before any git download so a tampered deployed file is reported by path.
		// A --global install deploys under deployDir, not the working directory.
		// Locked files that are not on disk at all are deployed after (B).
		deployPlan, err := planFrozenDeploy(existingLock, m, node != nil, targetFlag, deployDir)
		if err != nil {
			return err
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
			destDir := filepath.Join("apm_modules", filepath.FromSlash(dep.ModuleDir()))
			if !archive.Contained("apm_modules", destDir) {
				return fmt.Errorf("frozen install: refusing to extract %q outside apm_modules", dep.RepoURL)
			}
			if err := archive.CheckWithin("apm_modules", destDir); err != nil {
				return fmt.Errorf("frozen install: refusing to extract %q: %w", dep.RepoURL, err)
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
			approvedHTTP, err := checkFrozenInsecure(existingLock, newInsecurePolicy(allDirectDeps(m), deps.allowInsecure, deps.allowInsecureHosts, m.DefaultHost))
			if err != nil {
				return err
			}
			for _, dep := range existingLock.Dependencies {
				if dep.Source == "registry" || dep.Source == "local" {
					continue
				}
				// The lock does not record a local-path dependency's source
				// path, so the loader would take it for a git dependency.
				if strings.HasPrefix(dep.RepoURL, localModulesKeyPrefix) {
					continue
				}
				// req-lk-007: always call LoadPackage rather than short-
				// circuiting on directory existence here -- LoadPackage
				// itself verifies an existing checkout's HEAD against the
				// locked ref before deciding whether to skip re-cloning, so
				// a stale/tampered checkout is replaced rather than
				// silently trusted.
				// VirtualType is set because the loader takes only the
				// virtual path's directory for a subdirectory reference and
				// the whole repository for a reference with no type.
				ref := &manifest.DependencyReference{
					RepoURL:           dep.RepoURL,
					Host:              dep.Host,
					Port:              dep.Port,
					ArtifactoryPrefix: dep.RegistryPrefix,
					VirtualPath:       dep.VirtualPath,
					VirtualType:       manifest.VirtualTypeOf(dep.VirtualPath),
					Owner:             ownerFromRepoURL(dep.RepoURL),
					Repo:              repoFromRepoURL(dep.RepoURL),
					Source:            "git",
					Alias:             dep.Alias,
				}
				if dep.IsInsecure {
					ref.Scheme, ref.AllowInsecure, ref.HTTPApproved = "http", dep.AllowInsecure, approvedHTTP[dep.UniqueKey()]
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
				// An entry with no tree_sha256, a subdirectory package's
				// (buildLockfile) or any of a lock the Oracle wrote, is
				// pinned by resolved_commit and deployed_file_hashes alone.
				if dep.ResolvedCommit != "" && dep.Source != "registry" && dep.TreeSHA256 != "" && !isSubdirectoryPackage(dep.VirtualPath) {
					installDir := filepath.Join("apm_modules", filepath.FromSlash(dep.ModuleDir()))
					if err := lockfile.VerifyTreeSHA256(dep.TreeSHA256, installDir, dep.ResolvedCommit); err != nil {
						return fmt.Errorf("frozen install: entry %s: %w", dep.UniqueKey(), err)
					}
				}
			}
		}

		if err := copyMissingLocalPackages(deps, m); err != nil {
			return err
		}
		if deployPlan != nil {
			if err := deployPlan.run(deps, m, existingLock); err != nil {
				return err
			}
		}

		ux.Success(os.Stdout, "Frozen install: all dependencies pinned and verified")
		// R12d (prd.md/design.md §3): existingLock.Dependencies is already
		// the exact set (A)/(B) above just re-verified -- --verbose just
		// lists it too, without changing the default success line above.
		if deps.verbose {
			printFrozenVerifiedDeps(os.Stdout, existingLock)
		}
		return nil
	}

	// 4. Resolve dependency graph, unless this is a local-only deploy.
	chosen := resolveInstallTargets(targetFlag, m.Target, deployDir)
	targets, targetDiags := chosen.targets, chosen.diags
	if !hasAnyDeps {
		if len(targets) == 0 {
			// R16 (prd.md/design.md §3): this message is only printed when
			// nothing else is about to happen either -- either genuinely
			// nothing (an empty project) or local .apm/ primitives that
			// CAN'T be deployed (no target), both cases where "No
			// dependencies to install" is the complete, accurate story.
			// When a target DOES resolve below, printing it here would be
			// immediately contradicted by a deployed-files tree and a
			// mismatched "Installed 0 dependencies" summary -- deferred
			// entirely to deployAndFinalize's post-deploy summary instead
			// (codex M2: decided from the ACTUAL deploy result, not a
			// second up-front scan).
			ux.Info(os.Stdout, "No dependencies to install")
			for _, d := range targetDiags {
				ux.Error(os.Stderr, "%s", d)
			}
			// Local .apm/ primitives with no target to deploy them to is the
			// same failure as the deps-present zero-target gate below: exit 2
			// with the teaching message instead of silently exiting 0 with the
			// primitives ignored (Python parity: "No harness detected" fires
			// whenever there is ANYTHING to integrate -- deps or local
			// primitives; task 07-11-instructions-applyto-parity req #2). An
			// EMPTY project (no deps, no local primitives) keeps exiting 0.
			// Declared MCP servers count too in a project (issue #123 item
			// c57-3), as they do for the Oracle's harness check.
			if len(deploy.CollectScopeLocalPrimitives(".", deployDir != "")) > 0 ||
				(deployDir == "" && len(m.MCPServers)+len(m.MCPDevServers) > 0) {
				return errNoDeployTarget()
			}
			return nil
		}
	}

	var result *resolver.ResolutionResult
	var regLoader *registry.Loader
	var fetched *fetchedDirsLoader
	// core/command_logger.py:319-347.
	heldLines = append(heldLines, heldLine{print: func() {
		if len(packages) > 0 {
			ux.Progress(os.Stdout, "Installing %d new %s...", len(packages), pluralWord(len(packages), "package", "packages"))
		} else {
			ux.Progress(os.Stdout, "Installing dependencies from apm.yml...")
		}
	}})
	if !hasAnyDeps {
		printHeldLines(heldLines, true)
		result = &resolver.ResolutionResult{}
	} else {
		// No target ends the install before any package is fetched or copied:
		// Oracle 0.33.0 prints no Resolving line, leaves no apm_modules/ and
		// refuses before a package that cannot be fetched fails. It has
		// written apm.yml by then and puts it back, so it says so
		// (install/transaction.py:61-70; measured, issue #125 item c47-3).
		if len(targets) == 0 {
			printHeldLines(heldLines, true)
			if manifestBefore != nil && (newPackages > 0 || manifestChanged) {
				ux.Info(os.Stdout, "apm.yml restored to its previous state.")
			}
			for _, d := range targetDiags {
				ux.Error(os.Stderr, "%s", d)
			}
			return errNoDeployTarget()
		}
		// Registry access is experimental (API may change); require the opt-in flag
		// before any live registry resolution. Gates network use only — apm.yml
		// registries parsing and lockfile schema stay unconditional.
		for _, d := range allDirectDeps(m) {
			if d.Source == "registry" {
				if err := experimental.RequireEnabled("registries"); err != nil {
					printHeldLines(heldLines, false)
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
			Cached:          lockedRegistryResolution(existingLock, "apm_modules"),
		}

		newLocalCopies := absentLocalCopies(m)
		insecure := &insecureLoader{next: regLoader, refs: map[string]*manifest.DependencyReference{},
			policy: newInsecurePolicy(allDirectDeps(m), deps.allowInsecure, deps.allowInsecureHosts, m.DefaultHost)}
		fetched = &fetchedDirsLoader{next: insecure, lock: existingLock, fetched: map[string]bool{},
			announce: func(name string) {
				heldLines = append(heldLines, heldLine{print: func() { ux.Progress(os.Stdout, "Resolving %s...", name) }})
			}}
		result, err = resolver.Resolve(m, existingLock, deps.tags, fetched, resolver.ResolverConfig{
			MarketplaceResolve: newMarketplaceResolveFunc(),
		})
		printHeldLines(heldLines, err == nil)
		if err == nil {
			err = insecure.reportInsecure(result.Deps)
		}
		if err != nil {
			removeNewLocalCopies(newLocalCopies)
			if isSilentExit(err) {
				return err
			}
			var outside *archive.ContainmentError
			if errors.As(err, &outside) {
				return &resolveRefusedError{err: err, reason: outside, verbose: deps.verbose, started: started}
			}
			return fmt.Errorf("resolve: %w", err)
		}
		newModuleDirs = fetched.keys
		// A registry package downloaded again over its directory, because
		// the cache no longer holds what the lock pins, is fetched, with no
		// Resolving line, as Oracle 0.33.0 prints it (measured, A/p1-before).
		for _, d := range result.Deps {
			if d.Kind == resolver.KindRegistry && regLoader.Downloaded(d.Key) {
				fetched.fetched[d.Key] = true
			}
		}
		// mkt-029/033/F1: apm.yml dict-form marketplace dependencies
		// (dependencies.apm entries {name, marketplace, version}) are
		// resolved by the BFS itself now (root and transitive alike), not
		// just the CLI PLUGIN@MARKETPLACE positional-argument path above --
		// merge their provenance into the same map buildLockfile consults.
		mergeMarketplaceProvenance(marketplaceProvenance, result.MarketplaceProvenance)
	}

	// 5. Build lockfile. effectiveSubsets (design.md §1.2c, codex C2) is
	// computed ONCE here and fed to both buildLockfile and deployAndFinalize
	// below, so apm.yml, apm.lock.yaml, and the actual deploy filter can
	// never disagree about which skills a dependency gets (BUG-2).
	effectiveSubsets := effectiveSkillSubsets(m, requestedKeys, skillSubset)

	// H3 (design.md §1.2f, prd.md B2-6): a brand-new --skill name THIS call
	// introduces must be validated against the requested dependency's actual
	// resolved skills BEFORE any write happens -- apm.lock.yaml, apm.yml, and
	// deploy.Run's target-directory writes all still lie ahead of this point.
	// A typo'd name fails the whole install closed with zero partial state,
	// rather than silently persisting a subset that deploys 0 files.
	if err := validateNewSkillNames(result, requestedKeys, skillSubset); err != nil {
		return err
	}

	newLock, err := buildLockfile(result, existingLock, regLoader, effectiveSubsets, skillSubset, requestedKeys, noProvenance, marketplaceProvenance, localDirectDeps(m))
	if err != nil {
		return err
	}

	// R9.4/AC45: mark every DIRECT devDependencies.apm entry's lockfile
	// record with package_type, mirroring upstream's package_type:
	// marketplace_plugin marker (research/eval-real-run-20260728.md §D5).
	// Keyed off m.ParsedDevDeps -- not deps.dev/this call's --dev flag --
	// so a bare `apm install` that merely re-resolves an already-declared
	// devDependencies.apm entry keeps reporting the same package_type
	// (idempotency: TestRunInstall_DevDependency_SecondBareInstallIsNoOp).
	// Transitive dependencies pulled in BY a dev dependency are NOT marked:
	// devKeys only ever contains direct entries.
	if len(m.ParsedDevDeps) > 0 {
		devKeys := make(map[string]bool, len(m.ParsedDevDeps))
		for _, d := range m.ParsedDevDeps {
			if k := deploy.DepRefKey(d); k != "" {
				devKeys[k] = true
			}
		}
		for i := range newLock.Dependencies {
			if devKeys[newLock.Dependencies[i].UniqueKey()] {
				newLock.Dependencies[i].PackageType = lockfile.PackageTypeMarketplacePlugin
			}
		}
	}

	// 6-9. Deploy primitives, no-op check, write lockfile, persist packages.
	return deployAndFinalize(m, targetFlag, deployDir, effectiveSubsets, skillSubset, requestedKeys, existing, persistPackages, deps.dev, manifestChanged, len(requestedKeys) > 0, requestedKeys, result, newLock, existingLock, existingNode, node,
		&installRun{started: started, localDeclared: localDeclaredPaths(m), targetFlag: targetFlag, noProvenance: noProvenance,
			manifestCreated: manifestCreated, global: deployDir != "", verbose: deps.verbose, manifestTargets: len(m.Target) > 0,
			fetched: fetchedKeys(fetched)})
}

// printFrozenVerifiedDeps lists every dependency a successful --frozen
// install just pinned+verified -- R12d (prd.md/design.md §3): --verbose-only
// detail behind the unchanged default "Frozen install: all dependencies
// pinned and verified" summary line. lock is existingLock, the same
// dependency set steps (A)/(B) above already walked; nothing new is
// computed here.
func printFrozenVerifiedDeps(w io.Writer, lock *lockfile.Lockfile) {
	if lock == nil || len(lock.Dependencies) == 0 {
		return
	}
	items := make([]ux.Item, len(lock.Dependencies))
	for i := range lock.Dependencies {
		items[i] = ux.Item{Text: lock.Dependencies[i].UniqueKey()}
	}
	ux.List(w, items)
}

// noDeployTargetError marks errNoDeployTarget's failure so installCmd's RunE
// can selectively suppress cobra's default usage dump for JUST this error
// (R17, codex H8), via errors.As, without setting SilenceUsage on the
// command as a whole -- every other install error (bad --target token,
// --skill validation, resolve/deploy failures, ...) must keep showing usage
// exactly as before.
type noDeployTargetError struct {
	err error
}

func (e *noDeployTargetError) Error() string { return e.err.Error() }
func (e *noDeployTargetError) Unwrap() error { return e.err }

type unknownSkillError struct{ err error }

func (e *unknownSkillError) Error() string { return e.err.Error() }
func (e *unknownSkillError) Unwrap() error { return e.err }

// errNoDeployTarget is the exit-2 teaching error shared by runInstall's two
// zero-target gates (deps present, and local-primitives-only), so their
// wording and exit code can never drift apart. R17 (prd.md, design.md §3):
// apm-go's own signal-detection already scans manifest.SignalWhitelist's
// marker paths to decide "nothing auto-detected" -- rather than a bare
// one-line message followed by cobra's 14-flag usage dump, this prints that
// same scanned list plus concrete remediation (presentation-only: no new
// scanning is introduced here).
func errNoDeployTarget() error {
	var b strings.Builder
	b.WriteString("no deployment target detected\n\n")
	b.WriteString("Scanned for these harness markers (none found):\n")
	for _, sig := range manifest.SignalWhitelist {
		b.WriteString("  - " + sig.Path + "\n")
	}
	b.WriteString("\nTo fix, do one of the following:\n")
	b.WriteString("  1. Pass --target <name> (e.g. --target claude)\n")
	b.WriteString("  2. Add a targets: field to apm.yml, e.g.:\n")
	b.WriteString("       targets:\n")
	b.WriteString("         - claude\n")
	b.WriteString("  3. Create one of the marker paths above so apm-go can auto-detect a target\n")
	return withExitCode(2, &noDeployTargetError{err: errors.New(b.String())})
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
		ux.Warn(os.Stderr, "Bundle has no apm.lock.yaml -- skipping integrity check. "+
			"This bundle may have been produced by an older or non-apm-go tool.")
	} else if !info.HasPackMeta {
		ux.Warn(os.Stderr, "Bundle has an apm.lock.yaml but no 'pack:' metadata section -- skipping integrity check.")
	} else if errs := localbundle.VerifyBundleIntegrity(info.SourceDir, info.PackMeta); len(errs) > 0 {
		ux.Error(os.Stderr, "Bundle integrity check failed:")
		items := make([]ux.Item, len(errs))
		for i, e := range errs {
			items[i] = ux.Item{Text: e}
		}
		ux.List(os.Stderr, items)
		return fmt.Errorf("bundle integrity check failed for %s", bundleArg)
	}

	targets, targetDiags := deploy.ResolveTargets(targetFlag, nil, ".")
	for _, d := range targetDiags {
		ux.Warn(os.Stderr, "%s", d)
	}
	if len(targets) == 0 {
		ux.Warn(os.Stdout, "No active targets resolved -- nothing will be deployed. Pass --target to select one explicitly.")
		return nil
	}

	if warning := localbundle.CheckTargetMismatch(info.PackTargets, targets); warning != "" {
		ux.Warn(os.Stderr, "%s", warning)
	}

	var packMeta *bundle.PackMetadata
	if info.HasPackMeta {
		packMeta = &info.PackMeta
	}
	result, err := localbundle.IntegrateLocalBundle(info.SourceDir, packMeta, targets, ".")
	if err != nil {
		return fmt.Errorf("deploy local bundle: %w", err)
	}
	for _, d := range result.Diags {
		ux.Warn(os.Stderr, "%s", d)
	}

	if len(result.Files) == 0 {
		ux.Info(os.Stdout, "No files deployed from local bundle")
		return nil
	}

	if err := persistLocalBundleDeployment(result); err != nil {
		return err
	}

	ux.Success(os.Stdout, "Installed %d file(s) from local bundle %s", len(result.Files), bundleArg)
	// R12b (prd.md/design.md §3): an ordinary `apm install` already breaks
	// down its deployed files by primitive kind and target root
	// (deployedFilesTree, R10b) -- local-bundle install used to only print
	// the aggregate count, a regression relative to that. result.Files is
	// already the exact per-file list deployAndFinalize's tree would
	// consume; reusing the same helper here needs no new computation.
	ux.Tree(os.Stdout, deployedFilesTree(bundleArg+" (local bundle)", result.Files))
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
	if err := writeLockfileBytes(outBytes); err != nil {
		return err
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

// effectiveSkillSubsets is BUG-2's single per-dependency skill-subset
// computation point (design.md §1.2c, codex C2): every consumer that needs
// to know "which skills does dependency X actually get" -- apm.yml
// persistence (persistPackagesToManifest), apm.lock.yaml's skill_subset
// field (buildLockfile), and deploy.Run's SkillFilter (deployAndFinalize) --
// derives its answer from this SAME map, keyed by deploy.CanonicalDepKey,
// instead of each independently re-deriving "the effective subset" from
// this call's raw CLI flags alone (which only ever describes the packages
// THIS call named, not every other already-declared dependency that may
// carry its own persisted subset from an earlier install).
//
// Rules (mirrors Python issue #1771's normalize_and_merge_skill_subset):
//  1. Every direct dependency (m.ParsedDeps + m.ParsedDevDeps, F3) that
//     already carries a persisted SkillSubset seeds the map with it.
//  2. A dependency requestedKeys marks as targeted by THIS call (keyed the
//     same way requestedKeys itself is built in runInstall, deploy.DepRefKey)
//     has cliSubset UNION'd into whatever it already carries -- narrowing is
//     additive across repeated installs, never destructive.
//  3. The '*' RESET sentinel anywhere in cliSubset (containsSkillWildcard,
//     even mixed with concrete names) DELETES that dependency's entry
//     entirely: "no entry" is the map's only representation of "deploy every
//     skill" (H6 invariant -- a value is never an empty slice).
//  4. DUPLICATE manifest entries sharing one canonical identity (a
//     pre-BUG-1-fix polluted apm.yml, e.g. `Owner/Repo` AND `owner/repo` as
//     two entries): their persisted subsets are UNION'd, and if ANY of the
//     duplicates carries no subset at all ("deploy everything"), the whole
//     identity gets NO filter entry -- the widest reading always wins. A
//     later entry must never silently OVERWRITE an earlier one's subset
//     (re-verification codex gate finding): the resolver folds duplicates
//     onto the first-declared ref, so an overwrite would deploy-filter with
//     entry B's subset against entry A's content -- worst case deploying
//     zero files and letting stale reconciliation delete entry A's
//     still-wanted skills. Widest-wins can never delete anything. A
//     warning is emitted so the user cleans the duplicate up.
func effectiveSkillSubsets(m *manifest.Manifest, requestedKeys map[string]bool, cliSubset []string) map[string][]string {
	result, duplicates := persistedSkillSubsets(m)
	for _, repoURL := range duplicates {
		ux.Warn(os.Stderr, "apm.yml declares %q more than once (case/spelling variants of the same repository) -- consider removing the duplicate entry", repoURL)
	}
	wildcard := containsSkillWildcard(cliSubset)

	// Pass 2 -- this call's CLI narrowing on the requested dependencies.
	// Runs after the duplicate resolution above: an explicit --skill flag
	// NOW expresses fresh user intent and applies even when a stale
	// duplicate had widened the persisted state.
	if len(cliSubset) == 0 {
		return result
	}
	for _, dep := range allDirectDeps(m) {
		identity := deploy.CanonicalDepKey(dep)
		if identity == "" || !requestedKeys[deploy.DepRefKey(dep)] {
			continue
		}
		if wildcard {
			delete(result, identity)
			continue
		}
		// Belt-and-braces for the H6 invariant (no empty slice may enter
		// the filter map): an all-blank cliSubset unions to empty when
		// nothing was persisted. validateNewSkillNames rejects that input
		// before any write, so this branch is unreachable in practice --
		// but this map must uphold the invariant on its own, not by
		// trusting a distant caller.
		if u := unionSortedSkills(result[identity], cliSubset); len(u) > 0 {
			result[identity] = u
		}
	}
	return result
}

// persistedSkillSubsets is pass 1 of effectiveSkillSubsets, which prints
// nothing: the persisted subsets with rule 4's duplicate handling, and the
// RepoURL of each entry that repeats an earlier identity, for the caller to
// warn about. uninstall's trial deploy uses it alone, so the warning install
// already gave is not printed again.
func persistedSkillSubsets(m *manifest.Manifest) (subsets map[string][]string, duplicates []string) {
	result := make(map[string][]string)

	// duplicateBare marks identities declared MORE THAN ONCE where at least
	// one duplicate carries no subset ("deploy everything" outvotes a
	// sibling's narrowing). A single subset-free entry is just the normal
	// "no subset" case and must NOT block the CLI narrowing
	// of effectiveSkillSubsets' pass 2.
	occurrences := make(map[string]int)
	bareSeen := make(map[string]bool)
	for _, dep := range allDirectDeps(m) {
		identity := deploy.CanonicalDepKey(dep)
		if identity == "" {
			continue
		}
		occurrences[identity]++
		if occurrences[identity] > 1 {
			duplicates = append(duplicates, dep.RepoURL)
		}
		if len(dep.SkillSubset) > 0 {
			result[identity] = unionSortedSkills(result[identity], dep.SkillSubset)
		} else {
			bareSeen[identity] = true
		}
	}
	// Widest-wins resolution, deliberately AFTER the whole walk so it is
	// order-independent: a subset-free duplicate outvotes its siblings'
	// narrowing whether it appears before or after them. A SINGLE
	// subset-free entry (occurrences == 1) is just the ordinary "no subset"
	// case -- no map entry exists for it anyway.
	for identity := range bareSeen {
		if occurrences[identity] > 1 {
			delete(result, identity)
		}
	}
	return result, duplicates
}

// validateNewSkillNames enforces H3's atomicity requirement (design.md
// §1.2f, prd.md B2-6): a --skill name THIS call introduces must match at
// least one skill primitive among the dependencies THIS call targeted
// (requestedKeys), or the whole install fails BEFORE any write (this is
// called before buildLockfile/deployAndFinalize -- no apm.lock.yaml,
// apm.yml, or target-directory write has happened yet). "At least one",
// not "every", targeted dependency, deliberately: a single --skill flag
// list applies uniformly across every positional package this call names
// (effectiveSkillSubsets unions the SAME cliSubset into each targeted dep),
// so installing two DIFFERENT repos together with a shared --skill list
// naming one skill from each repo must not be treated as a typo just
// because neither repo happens to have the OTHER repo's skill -- only a
// name absent from ALL targeted repos is a genuine typo. Only checked
// against cliSubset (this call's raw --skill flags, not the union'd
// effective subset) -- an already-persisted name from an earlier install
// that no longer matches is a separate, non-fatal case (deploy.Run's own
// skillSubsetDiags warning, not this function). The '*' RESET sentinel
// never names a real skill, so it's exempt.
//
// Scope of "before any write" (codex gate, recorded by-design): resolution
// has already materialized apm_modules/ by the time this runs -- that's the
// vendor CACHE, not one of the three account states B2-6's atomicity
// protects (apm.yml, apm.lock.yaml, deployed target files), and EVERY
// post-resolution failure path (deploy error, lockfile write error) leaves
// it refreshed the same way. Moving resolution into a temp dir just for
// this check would be a cross-cutting architectural change for no
// account-state benefit.
func validateNewSkillNames(result *resolver.ResolutionResult, requestedKeys map[string]bool, cliSubset []string) error {
	if len(cliSubset) == 0 || containsSkillWildcard(cliSubset) {
		return nil
	}
	var names []string
	for _, s := range cliSubset {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			// A blank name would vanish in unionSortedSkills' trimming; for
			// a dependency with no persisted subset that union comes out
			// EMPTY, and an empty slice in SkillFilter.Subsets means
			// "deploy zero skills" (H6 forbids exactly that state). Reject
			// loudly instead of silently deploying nothing.
			return fmt.Errorf("--skill: skill name must be a non-empty string")
		}
		names = append(names, trimmed)
	}

	availableAnywhere := make(map[string]bool)
	for _, dep := range result.Deps {
		if !requestedKeys[dep.Key] {
			continue
		}
		prims, err := deploy.CollectDependencyPrimitives(dep.Key, filepath.Join("apm_modules", filepath.FromSlash(dep.ModuleDir())))
		if err != nil {
			return fmt.Errorf("deploy: %w", err)
		}
		for _, p := range prims {
			if p.Type == deploy.TypeSkills {
				availableAnywhere[p.Name] = true
			}
		}
	}

	var unknown []string
	for _, name := range names {
		if !availableAnywhere[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return &unknownSkillError{fmt.Errorf("--skill %s: unknown skill(s) %s", strings.Join(cliSubset, ", "), strings.Join(unknown, ", "))}
	}
	return nil
}

// normalizeDeployPath canonicalizes a deployed-file path purely for the
// STALE-vs-CLAIMED equality check below -- never used as the key handed to
// RemoveDeployedFiles/DeployedHashes, which must still receive the path
// EXACTLY as recorded in the lockfile (that original string is the hash-map
// key). Guards against the same logical path comparing unequal across two
// lockfile snapshots due to representational drift alone (redundant "./"
// segments, or a backslash-separated path from a hand-edited/legacy
// lockfile) -- codex final-gate finding: without this, a path that is
// genuinely still claimed could be misread as stale by a raw string
// comparison. filepath.ToSlash is NOT used here: on a Unix CI runner it is a
// no-op (backslash is a legal filename byte there, not a separator), so it
// would silently fail to normalize a legacy Windows-authored lockfile path
// read on Linux (a second codex final-gate finding) -- replace explicitly
// instead, unconditionally, regardless of the host OS.
// mergeDeployedFiles combines the existing lockfile's deployed_files with
// this run's deploy output, dropping an old entry only when os.Stat says the
// file does not exist under deployRoot. This handles re-install with a
// different --target (old target's files survive and stay tracked) without
// preserving files that were deliberately removed (e.g. stale-skill
// reconciliation). A path that cannot be read (a permission error) is kept:
// it may still be on disk, and without its record no later run cleans it.
func mergeDeployedFiles(oldFiles []string, oldHashes map[string]string, newFiles []string, newHashes map[string]string, deployRoot string) ([]string, map[string]string) {
	return mergeDeployedFilesIf(oldFiles, oldHashes, newFiles, newHashes, func(f string) bool {
		_, err := os.Stat(filepath.Join(deployRoot, filepath.FromSlash(f)))
		return !errors.Is(err, os.ErrNotExist)
	})
}

// mergeDeployedFilesIf adds to this run's deploy output each old entry that
// the run did not produce and that keep accepts, with its old hash.
func mergeDeployedFilesIf(oldFiles []string, oldHashes map[string]string, newFiles []string, newHashes map[string]string, keep func(file string) bool) ([]string, map[string]string) {
	seen := make(map[string]bool, len(newFiles))
	merged := make(map[string]string, len(oldHashes)+len(newHashes))
	for _, f := range newFiles {
		seen[f] = true
	}
	for k, v := range newHashes {
		merged[k] = v
	}
	for _, f := range oldFiles {
		if seen[f] || !keep(f) {
			continue
		}
		newFiles = append(newFiles, f)
		seen[f] = true
	}
	for k, v := range oldHashes {
		if _, ok := merged[k]; !ok && seen[k] {
			merged[k] = v
		}
	}
	sort.Strings(newFiles)
	return withoutOrphanSkillDirs(newFiles, merged), merged
}

func normalizeDeployPath(p string) string {
	return path.Clean(strings.ReplaceAll(p, "\\", "/"))
}

// skillNameFromDeployPath extracts the skill name from a deploy path shaped
// like ".agents/skills/<name>/...", ".claude/skills/<name>/...", or
// "<bundle>/skills/<name>/..." (every skill deploy root deploy.Run
// produces) -- the segment immediately following a "skills" path segment.
// ok is false for any path with no "skills" segment (or "skills" as the
// final segment, which no real deploy ever produces).
func skillNameFromDeployPath(p string) (name string, ok bool) {
	segs := strings.Split(normalizeDeployPath(p), "/")
	for i, seg := range segs {
		if seg == "skills" && i+1 < len(segs) {
			return segs[i+1], true
		}
	}
	return "", false
}

// reconcileStaleSkillDeployments is the pollution-convergence half of BUG-2
// (design.md §1.2g, codex C1, prd.md B2-3/AC-B2-7): it removes a dependency's
// skill files that a --skill subset narrowing excluded, deliberately narrow
// in ways two rounds of codex final-gate review found an earlier draft was
// not (that draft treated every lockfile-recorded path as fair game for
// deletion on every install, keyed purely by "is this path claimed by MY
// dependency's fresh entry" -- both too broad an ELIGIBILITY test and too
// narrow a CLAIM test):
//
//   - Eligibility is decided PER SKILL NAME, not per dependency or per raw
//     path: a stale path under dependency D is only a deletion CANDIDATE
//     when (a) it's a skill deploy path (skillNameFromDeployPath) and (b)
//     that specific skill name is ABSENT from D's fresh SkillSubset. A
//     dependency with no active subset at all (full install, or a --skill
//     '*' RESET) never has anything considered here. Critically, a skill
//     that IS still in the fresh subset is never a candidate even if this
//     particular path (e.g. one target's copy) momentarily isn't claimed --
//     that disappearance is a --target selection change between calls, not
//     a subset narrowing, and this mechanism must not conflate the two.
//   - The CLAIM check (is this path still wanted by anything) stays GLOBAL
//     across the WHOLE fresh lockfile, not scoped to the same dependency's
//     own fresh entry -- an earlier revision that scoped it per-dependency
//     could delete a path a DIFFERENT dependency has since taken ownership
//     of (same path, matching old hash, but now serving a new owner).
//   - Non-skill paths (instructions/agents/commands/hooks) are never
//     eligible at all -- this mechanism only ever prunes skill deploys.
//
// The local bucket (LocalDeployedFiles) is out of scope entirely: --skill
// only ever applies to a declared git dependency's SkillSubset, never to the
// project's own local .apm/ content, so pruning "stale" local files here
// would be scope creep with no --skill-narrowing justification.
//
// Ownership-aware on top of all of the above: a stale path is only deleted
// when its on-disk content still matches the OLD recorded hash (deploy.
// RemoveDeployedFiles, reused rather than reimplemented) -- a user-modified
// file, a file some OTHER dependency's NEW deployment has since taken over
// (its hash won't match the stale entry's old one), or a file that's already
// gone are all left untouched, reported via the returned diagnostics
// (printed as warnings by the caller) instead of silently vanishing or
// silently staying forever.
func reconcileStaleSkillDeployments(existingLock, newLock *lockfile.Lockfile, projectDir string) []string {
	if existingLock == nil {
		return nil
	}

	// newByKey looks up each dependency's FRESH lock entry by its identity
	// (UniqueKey: RepoURL[/VirtualPath]), to read its fresh SkillSubset.
	newByKey := make(map[string]*lockfile.LockedDep, len(newLock.Dependencies))
	for i := range newLock.Dependencies {
		newByKey[newLock.Dependencies[i].UniqueKey()] = &newLock.Dependencies[i]
	}

	// claimedAnywhere is every path ANY dependency -- OR the local bucket --
	// in the freshly-rebuilt lockfile still deploys this run, normalized --
	// deliberately global (not scoped to the old path's own dependency), so
	// a path some OTHER dependency or the project's own local content has
	// since taken ownership of is never deleted out from under its new
	// owner. The local bucket itself is never a PRUNING candidate (--skill
	// doesn't apply to it), but it must still count as a claim.
	claimedAnywhere := make(map[string]bool)
	for _, d := range newLock.Dependencies {
		for _, f := range d.DeployedFiles {
			claimedAnywhere[normalizeDeployPath(f)] = true
		}
	}
	for _, f := range newLock.LocalDeployedFiles {
		claimedAnywhere[normalizeDeployPath(f)] = true
	}

	var diags []string
	for _, old := range existingLock.Dependencies {
		fresh, ok := newByKey[old.UniqueKey()]
		// A dependency absent from the fresh lock, or present but with no
		// active skill subset (full install / '*' RESET), is never touched
		// by this mechanism -- an actually-removed dependency is
		// uninstall's job, not an automatic side effect of install/update.
		if !ok || len(fresh.SkillSubset) == 0 {
			continue
		}
		freshSubset := make(map[string]bool, len(fresh.SkillSubset))
		for _, s := range fresh.SkillSubset {
			freshSubset[s] = true
		}

		var stale []string
		for _, f := range old.DeployedFiles {
			name, isSkillPath := skillNameFromDeployPath(f)
			if !isSkillPath {
				continue // never prune a non-skill deploy path
			}
			if freshSubset[name] {
				// This skill is STILL selected -- its path merely not being
				// claimed this run reflects a --target change, not a subset
				// narrowing; never a deletion candidate.
				continue
			}
			if claimedAnywhere[normalizeDeployPath(f)] {
				continue // some OTHER dependency has since taken this path over
			}
			stale = append(stale, f)
		}
		if len(stale) == 0 {
			continue
		}
		_, _, keptDiags := deploy.RemoveDeployedFiles(projectDir, stale, old.DeployedHashes)
		diags = append(diags, keptDiags...)
	}

	return diags
}

// unionSortedSkills merges add into existing, trimming, deduplicating, and
// sorting the result (Python parity: normalize_and_merge_skill_subset).
func unionSortedSkills(existing, add []string) []string {
	seen := make(map[string]bool, len(existing)+len(add))
	merged := make([]string, 0, len(existing)+len(add))
	for _, group := range [][]string{existing, add} {
		for _, s := range group {
			s = strings.TrimSpace(s)
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			merged = append(merged, s)
		}
	}
	sort.Strings(merged)
	return merged
}

// resolvedDepCanonicalKey mirrors deploy.CanonicalDepKey for a resolved
// dependency (resolver.ResolvedDep), which carries only RepoURL/VirtualPath
// -- no separately-tracked Owner/Repo/Host. Feeding just those two fields
// into a DependencyReference hits CanonicalRepoIdentity's "bare literal"
// branch, which case-folds a default-host git RepoURL exactly the same way
// a fully-populated manifest reference does -- the only shape a --skill
// subset is ever recorded against (the source matrix rejects skills: for
// registry/marketplace/local dict forms at parse time).
func resolvedDepCanonicalKey(dep resolver.ResolvedDep) string {
	// Re-parse the RepoURL so a host-qualified spelling
	// ("git.internal/Acme/Repo") recovers its Host/Owner/Repo structure and
	// CanonicalRepoIdentity applies its host-aware case rules -- feeding the
	// raw string into the bare-literal branch would blanket-lowercase it,
	// which diverges from the manifest-refs side for a case-sensitive
	// self-hosted repo (final codex gate finding: the lockfile lookup would
	// miss the subset effectiveSkillSubsets recorded, splitting apm.yml,
	// apm.lock.yaml, and the deploy filter three ways).
	if ref, err := manifest.ParseDepString(dep.RepoURL); err == nil {
		ref.VirtualPath = dep.VirtualPath
		return deploy.CanonicalDepKey(ref)
	}
	ref := &manifest.DependencyReference{RepoURL: dep.RepoURL, VirtualPath: dep.VirtualPath}
	return deploy.CanonicalDepKey(ref)
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
func buildLockfile(result *resolver.ResolutionResult, existingLock *lockfile.Lockfile, regLoader *registry.Loader, effectiveSubsets map[string][]string, skillSubset []string, requestedKeys map[string]bool, noProvenance bool, marketplaceProvenance map[string]*marketplace.Provenance, localDeps map[string]*manifest.DependencyReference) (*lockfile.Lockfile, error) {
	existingVersion := ""
	if existingLock != nil {
		existingVersion = existingLock.Version
	}

	newLock := &lockfile.Lockfile{
		Version: lockfile.DetermineVersion(toLockDeps(result.Deps), existingVersion),
	}
	if !noProvenance {
		newLock.APMVersion = version.Version
	}

	matchedKeys := make(map[string]bool, len(requestedKeys))
	for _, dep := range result.Deps {
		ld := lockfile.LockedDep{
			RepoURL:        dep.RepoURL,
			Host:           dep.Host,
			Port:           dep.Port,
			RegistryPrefix: dep.ArtifactoryPrefix,
			VirtualPath:    dep.VirtualPath,
			Source:         kindToSource(dep.Kind),
			ResolvedTag:    dep.ResolvedTag,
			ResolvedRef:    dep.ResolvedRef,
			ResolvedCommit: dep.Commit,
			ResolvedBy:     dep.ResolvedBy,
			Depth:          dep.Depth,
			IsInsecure:     dep.Insecure,
			AllowInsecure:  dep.AllowInsecure,
			Alias:          dep.Alias,
		}
		// The Oracle's lock writes no source of a git entry, and a
		// constraint only for a semver range (deps/lockfile.py:351-430,
		// 564-713; measured, ~/.cache/apm-go-oracle-ref/A/124-82/r3).
		if dep.Kind == resolver.KindGitSemver {
			ld.Constraint = dep.Constraint
		}
		if ld.Source == "git" {
			ld.Source = ""
		}
		moduleDir := filepath.Join("apm_modules", filepath.FromSlash(dep.ModuleDir()))

		// req-lk-002/003: registry deps carry resolved_url + resolved_hash +
		// version, collected out-of-band by the registry loader.
		// A local package: the Oracle records where it came from and what
		// its own apm.yml says, and no commit (install/sources.py:246).
		// A `git:` entry's ref is recorded as resolved_ref
		// (deps/lockfile.py:647).
		local, isLocal := localDeps[dep.Key]
		if isLocal {
			ld.Source = "local"
			ld.LocalPath = local.LocalDeclaredPath
			ld.ResolvedRef = local.LocalDeclaredRef
			ld.ResolvedCommit = ""
			ld.Name, ld.Version, ld.PackageType = localPackageIdentity(moduleDir)
		}

		// A registry entry also records the name and package_type of the
		// package, its default host, its version as resolved_ref
		// (registry_entry.py:101, deps/lockfile.py:564-713; measured,
		// ~/.cache/apm-go-oracle-ref/A/124-36/r1).
		if dep.Kind == resolver.KindRegistry {
			if r, ok := regLoader.Resolutions()[dep.Key]; ok {
				ld.ResolvedURL = r.ResolvedURL
				ld.ResolvedHash = r.ResolvedHash
				ld.Version = r.Version
				ld.ResolvedRef = r.Version
			}
			if ld.Host == "" {
				ld.Host = "github.com"
			}
			ld.Name, ld.PackageType = gitPackageIdentity(moduleDir, dep.ModuleDir(), dep.RepoURL, dep.VirtualPath)
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

		// matchedKeys tracks "this requested package DID resolve",
		// independent of whether it ended up with a subset recorded (the '*'
		// RESET sentinel records none), so the "every requested package
		// resolved" validation below isn't spuriously failed.
		if requestedKeys[dep.Key] {
			matchedKeys[dep.Key] = true
		}

		// BUG-2 fix: skill_subset is stamped for EVERY dependency that has
		// an entry in effectiveSubsets (the single per-dep computation
		// point, design.md §1.2c/e) -- not just the dependency THIS call's
		// --skill flag was scoped to. Previously this only ever considered
		// requestedKeys, so a bare `apm install`/`apm update` (empty
		// requestedKeys) silently forgot every already-persisted subset the
		// very next time the lockfile was rebuilt from scratch.
		if identity := resolvedDepCanonicalKey(dep); identity != "" {
			if subset, ok := effectiveSubsets[identity]; ok {
				ld.SkillSubset = subset
			}
		}
		ld.TargetSubset = dep.TargetSubset

		// req-lk-008: record resolved_at for git-semver entries
		if dep.Kind == resolver.KindGitSemver && dep.Constraint != "" {
			ld.ResolvedAt = time.Now().UTC().Format(time.RFC3339)
		}

		// Resolve commit SHA for git deps that don't have it yet
		if isLocal {
			newLock.Dependencies = append(newLock.Dependencies, ld)
			continue
		}

		// A subdirectory package's module directory has no .git: git run
		// there answers for the repository that encloses the project. Its
		// commit comes from the loader that cloned it, and it has no
		// tree_sha256; the commit and deployed_file_hashes pin its content.
		isGit := dep.Kind == resolver.KindGitSemver || dep.Kind == resolver.KindGitLiteral
		isSubdir := isSubdirectoryPackage(dep.VirtualPath)
		if isGit && dep.Commit == "" {
			if isSubdir {
				ld.ResolvedCommit = clonedSubdirCommit(regLoader, dep.ModuleDir())
			} else if commit, err := gitops.ResolveCommit(moduleDir); err == nil {
				ld.ResolvedCommit = commit
			}
		}

		if isGit {
			ld.Version = gitDependencyVersion(dep, moduleDir, ld.ResolvedCommit)
			ld.Name, ld.PackageType = gitPackageIdentity(moduleDir, dep.ModuleDir(), dep.RepoURL, dep.VirtualPath)
			ld.IsVirtual = dep.VirtualPath != ""
		}

		// req-lk-015: compute tree_sha256 for git-sourced deps (required)
		if isGit && !isSubdir {
			commit := ld.ResolvedCommit
			if commit != "" {
				treeHash, hashErr := lockfile.ComputeTreeSHA256(moduleDir, commit)
				if hashErr != nil {
					return nil, fmt.Errorf("tree_sha256 for %s: %w", dep.Key, hashErr)
				}
				ld.TreeSHA256 = treeHash
			}
		}
		hash, err := packageContentHash(moduleDir, dep.VirtualPath)
		if err != nil {
			return nil, fmt.Errorf("content_hash for %s: %w", dep.Key, err)
		}
		ld.ContentHash = hash

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
// requestedKeys and existing (both dep keys, deploy.DepRefKey identity) are
// only meaningful for a real `install` call: requestedKeys is nil for
// `update` (which has no per-call "was this positionally requested"
// concept), and the R9/R10c muting below is skipped whenever it is.
// effectiveSubsets is the single per-dependency skill-subset computation
// point (effectiveSkillSubsets, design.md §1.2c) -- the ONLY source
// deploy.Run's SkillFilter is built from, so a bare re-deploy honors every
// already-persisted subset, not just the one this call's --skill flag named.
// dev (R9/AC42) selects which apm.yml section packages is persisted into --
// always false for `update`, which never persists positional packages.
// scoped says the command named packages (`install <pkg>`, `update <pkg>`):
// the files of a package apm.yml no longer declares are then not cleaned and
// its lock entry stays as it was, as the Oracle skips its orphan cleanup when
// it has only_packages (install/phases/cleanup.py:69).
//
// named are the packages the command named; with scoped, only they and the
// packages they bring in are deployed, the project's own content and the MCP
// servers too, and the lock keeps the record of every other package as it
// was (the Oracle's only_packages, install/phases/cleanup.py:69-75).
func deployAndFinalize(m *manifest.Manifest, targetFlag, deployDir string, effectiveSubsets map[string][]string, skillSubset []string, requestedKeys, existing map[string]bool, packages []string, dev, manifestChanged, scoped bool, named map[string]bool, result *resolver.ResolutionResult, newLock, existingLock *lockfile.Lockfile, existingNode, node *yamllib.Node, run *installRun) error {
	detectRoot := "."
	if deployDir != "" {
		detectRoot = deployDir
	}
	chosen := resolveInstallTargets(targetFlag, m.Target, deployDir)
	targets, targetDiags := chosen.targets, chosen.diags
	var only map[string]bool
	if scoped {
		only = listedDeps(result, named, true)
		delete(only, "")
	}
	// localProjectDeployed is R16's post-deploy decision point (design.md
	// §3, codex M2): whether THIS run's deploy.Run actually deployed at
	// least one file from the project's own .apm/ tree (deployResult.
	// PerDep[""], captured BEFORE the MCP-files merge below folds into the
	// SAME LocalDeployedFiles slice) -- never a second up-front directory
	// scan just to decide what to print.
	var localProjectDeployed bool
	// apmInstalled and mcpConfigured are the counts of the Oracle's summary
	// line: what this run installed or configured.
	var apmInstalled, mcpConfigured int
	// mcpUnplaced: at user scope the MCP servers declared have no target
	// that takes them there, which fails the install.
	var mcpUnplaced bool
	// mcpFailure: the MCP registry refused or did not answer, so no MCP
	// config was written and the install ends with exit 1 once the lock is.
	var mcpFailure error
	// apmPhase: a dependency or the project's own content was deployed.
	var apmPhase bool
	// keptDirs and failedBuckets are deploy.DeployResult.KeptDirs and
	// .FailedBuckets, and refused every path of .RejectedPaths, for the lock
	// merge in step 6b.
	var keptDirs []string
	var failedBuckets, refused map[string]bool
	var skippedDeps []deploy.SkippedDep
	var commandNotes []deploy.CommandNote
	var shadowed []deploy.Shadowed
	var unmanaged map[string][]string
	// orphans are the lock entries step 6b carries over for packages this
	// run no longer resolves: whole when they were not cleaned, else the
	// paths the cleanup could not remove.
	var orphans []lockfile.LockedDep
	var prevLock *lockfile.Lockfile
	if existingLock != nil {
		prevLock = lockUnderCurrentKeys(existingLock, m)
		orphans = orphanDeps(prevLock, newLock)
	}

	// 6. Deploy primitives to targets
	for _, d := range targetDiags {
		ux.Warn(os.Stderr, "%s", d)
	}
	if len(targets) > 0 {
		targetSource := "auto-detect"
		if run != nil {
			targetSource = "auto-detect from " + strings.Join(manifest.DetectSignals(detectRoot), ", ")
		}
		if targetFlag != "" {
			targetSource = "--target"
			if run != nil {
				// The Oracle's provenance wording (core/target_detection.py:814).
				targetSource = "--target flag"
			}
		} else if len(m.Target) > 0 {
			targetSource = "apm.yml"
		}
		printTargets := func() {
			ux.Info(os.Stdout, "Targets: %s  (source: %s)", sortedTargets(targets), targetSource)
		}
		if run == nil {
			printTargets()
		}

		var skillFilter *deploy.SkillFilter
		if len(effectiveSubsets) > 0 {
			skillFilter = &deploy.SkillFilter{Subsets: effectiveSubsets}
		}
		// The "Skill subset:" info line only makes sense when THIS call
		// narrowed something -- an already-persisted subset from an earlier
		// call being silently re-applied isn't news the user asked for now.
		printSkillSubset := func() {
			if len(skillSubset) > 0 && !containsSkillWildcard(skillSubset) {
				ux.Info(os.Stdout, "Skill subset: %s", strings.Join(skillSubset, ", "))
			}
		}
		if run == nil {
			printSkillSubset()
		}

		beforeDeploy := hashesOnDisk(existingLock, detectRoot)
		deployResult, err := deploy.Run(targets, ".", m, result, skillFilter, deployDir, deployDir != "", lockManaged(existingLock, detectRoot), deploy.WithMCPTargets(chosen.mcp), deploy.WithOnly(only))
		var rejected *deploy.RejectedInstructionError
		if run != nil && errors.As(err, &rejected) {
			printTargets()
			return run.rejectedInstall(rejected, result, newLock, existingLock)
		}
		if err != nil {
			return fmt.Errorf("deploy: %w", err)
		}
		// A path left to the user is content the Oracle integrated too.
		apmPhase = len(result.Deps) > 0 || len(deployResult.PerDep) > 0 || len(deployResult.Unmanaged) > 0
		if run != nil && apmPhase {
			// The Oracle names the targets only when it has a package or the
			// project's own content to integrate (core/target_detection.py:1186),
			// and not for the user scope.
			if !run.global {
				printTargets()
			}
			printSkillSubset()
		}
		for _, d := range deployResult.Diags {
			// install counts shadowed primitives as the Oracle's skipped
			// files (finish) instead.
			if run != nil && slices.Contains(deployResult.Conflicts, d) {
				continue
			}
			if !slices.Contains(deployResult.MCPDiags, d) {
				run.noteDiagnostic()
			}
			ux.Warn(os.Stderr, "%s", d)
		}
		commandNotes = deployResult.CommandNotes
		if run == nil {
			// update's output is its own (ruling R13): the notes go with the
			// other deploy warnings.
			for _, n := range commandNotes {
				if n.Info {
					ux.Info(os.Stdout, "%s", commandNoteText(n))
				} else {
					ux.Warn(os.Stderr, "%s", commandNoteText(n))
				}
			}
		}
		keptDirs, failedBuckets, refused = deployResult.KeptDirs, deployResult.FailedBuckets, rejectedSet(deployResult.RejectedPaths)
		skippedDeps, shadowed, unmanaged = deployResult.SkippedDeps, deployResult.Shadowed, deployResult.Unmanaged
		skipped := make(map[string]bool, len(skippedDeps))
		for _, sd := range skippedDeps {
			skipped[sd.Key] = true
		}

		// Print deploy summary per dep: dependencies in resolver order
		// (result.Deps; at one depth, the order apm.yml declares them), then
		// local content. deployResult.PerDep is a map, so its own order is
		// not usable (issue #47).
		// Deliberate deviation from the Oracle, by owner ruling: the Oracle
		// also puts local content last (install/phases/integrate.py:716), but
		// it orders the dependencies by (depth, node id)
		// (deps/apm_resolver.py:46). apm-go keeps the resolver order (issue
		// #47, kept by issue #50 ruling R1).
		if run == nil {
			for _, dep := range result.Deps {
				if dr, ok := deployResult.PerDep[dep.Key]; ok {
					ux.Tree(os.Stdout, deployedFilesTree(dep.Key+depVersionLabel(dep), dr.Files))
				}
			}
			if dr, ok := deployResult.PerDep[""]; ok {
				ux.Tree(os.Stdout, deployedFilesTree("<project root> (local)", dr.Files))
			}
			// Warn about resolved dependencies that deployed zero files to
			// any target -- otherwise the summary reads as success even when
			// a dependency's primitives went entirely undiscovered.
			for _, dep := range result.Deps {
				if _, ok := deployResult.PerDep[dep.Key]; !ok && !skipped[dep.Key] && len(deployResult.RejectedPaths[dep.Key]) == 0 && (only == nil || only[dep.Key]) {
					ux.Warn(os.Stderr, "%s deployed 0 files to any target", dep.Key)
				}
			}
		} else if apmPhase || run.global {
			// The user scope always integrates ~/.apm's own content
			// (install/phases/local_content.py:45-56), also with nothing in it.
			apmInstalled = run.printBlocks(result, deployResult, existingLock, newLock, beforeDeploy, skipped,
				listedDeps(result, requestedKeys, scoped))
			// Here, before the MCP servers, is where the Oracle prints it; the
			// user scope keeps no .gitignore (install/pipeline.py:820-826).
			if !run.global {
				updateGitignoreForApmModules()
			}
		}

		// Populate per-dep DeployedFiles/DeployedHashes in lockfile entries
		for i := range newLock.Dependencies {
			dep := &newLock.Dependencies[i]
			key := dep.UniqueKey()
			if dr, ok := deployResult.PerDep[key]; ok {
				files, hashes := dr.Recorded()
				dep.DeployedFiles = withSkillDirs(files)
				dep.DeployedHashes = hashes
			}
		}
		// A package the command did not name keeps its old record, its
		// deployed files merged back in step 6b; one with none is not
		// recorded (measured, issue125 upd2-or).
		if only != nil {
			kept := newLock.Dependencies[:0]
			for _, dep := range newLock.Dependencies {
				if only[dep.UniqueKey()] {
					kept = append(kept, dep)
				} else if prevLock != nil && prevLock.FindByKey(dep.UniqueKey()) != nil {
					entry := *prevLock.FindByKey(dep.UniqueKey())
					entry.DeployedFiles, entry.DeployedHashes = nil, nil
					kept = append(kept, entry)
				}
			}
			newLock.Dependencies = kept
		}
		// Populate local deployed files
		if dr, ok := deployResult.PerDep[""]; ok {
			files, hashes := dr.Recorded()
			newLock.LocalDeployedFiles = withSkillDirs(files)
			newLock.LocalDeployedHashes = hashes
			localProjectDeployed = len(dr.Files) > 0
		}

		// A merged MCP config file (.mcp.json) is shared with the user's own
		// servers, so the lock does not list it as a deployed file; the
		// servers are recorded instead (the Oracle's mcp_servers, mcp_configs
		// and mcp_target_servers).
		if deployResult.MCPFailure != nil {
			mcpFailure = deployResult.MCPFailure
			keepMCPState(newLock, existingLock)
			if run != nil {
				printMCPHeader(len(deployResult.DeclaredMCPServers), orderedTargets(mcpTargetSet(chosen.mcp), targets),
					targetFlag != "", len(m.Target) > 0, &chosen.userScope)
			}
		} else if len(deployResult.MCPProvenance) > 0 {
			seen := make(map[string]bool, len(deployResult.MCPProvenance))
			targetsByServer := make(map[string]map[string]bool, len(deployResult.MCPProvenance))
			for _, p := range deployResult.MCPProvenance {
				if !seen[p.Server] {
					seen[p.Server] = true
					newLock.MCPServers = append(newLock.MCPServers, p.Server)
				}
				set, ok := targetsByServer[p.Server]
				if !ok {
					set = map[string]bool{}
					targetsByServer[p.Server] = set
				}
				if p.Target != "" {
					set[p.Target] = true
				}
			}
			sort.Strings(newLock.MCPServers)
			declaredMCP := manifestMCPByName(m)
			recordMCPState(newLock, declaredMCP, deployResult.DependencyMCP, targetsByServer)

			if run != nil {
				mcpTargets := map[string]bool{}
				for _, set := range targetsByServer {
					for t := range set {
						mcpTargets[t] = true
					}
				}
				ensureClaudeDir(detectRoot, mcpTargets)
				mcpConfigured = printMCPBlock(manifestMCPOrder(m, newLock.MCPServers, targetsByServer), declaredMCP,
					newLock, existingLock, targetsByServer, targets, targetFlag != "", len(m.Target) > 0, &chosen.userScope)
			} else {
				mcpItems := make([]ux.Item, 0, len(newLock.MCPServers))
				for _, server := range newLock.MCPServers {
					mcpItems = append(mcpItems, ux.Item{
						Text: fmt.Sprintf("%s -> %s", server, strings.Join(sortedStringSet(targetsByServer[server]), ", ")),
					})
				}
				chosen.userScope.warn()
				ux.Info(os.Stdout, "MCP servers configured:")
				ux.List(os.Stdout, mcpItems)
			}
		} else if deployDir != "" && len(chosen.mcp) == 0 && len(chosen.userScope.targets) > 0 && len(deployResult.DeclaredMCPServers) > 0 {
			mcpUnplaced = true
			if run != nil {
				run.noteDiagnostic()
			}
			printUnplacedMCP(len(deployResult.DeclaredMCPServers), targets, targetFlag != "", &chosen.userScope)
		}

		// Pollution convergence (design.md §1.2g, codex C1, prd.md B2-3/
		// AC-B2-7): a file a PREVIOUS install deployed that this run's
		// rebuilt lockfile no longer claims for ANY dependency (e.g. a
		// --skill subset narrowing what used to be a full install, or a
		// pre-fix BUG-2 leak of another dependency's whole skill set) is
		// removed -- but only when its on-disk content still matches the
		// hash recorded for it. A user-modified file, a file some OTHER
		// dependency has since taken over, or a file that's already gone are
		// all left untouched and reported via a warning instead.
		for _, d := range reconcileStaleSkillDeployments(existingLock, newLock, ".") {
			run.noteDiagnostic()
			ux.Warn(os.Stderr, "%s", d)
		}

		// Stale-file cleanup (issues #30, #32). --global deploys symlinks
		// from deployDir into this directory, so there a stale symlink is
		// removed itself and nothing is deleted through it: a delete through
		// one would remove the source file.
		if existingLock != nil {
			cleanRoot, linkSourceRoot := ".", ""
			if deployDir != "" {
				cleanRoot, linkSourceRoot = deployDir, "."
			}
			if !scoped {
				oc := cleanOrphanedDeployedFiles(orphans, newLock, refused, keptDirs, cleanRoot, linkSourceRoot)
				if n := len(oc.removed); n > 0 {
					// Oracle sentence: core/command_logger.py:563-576. The
					// paths are listed as for a stale file below (issue #30).
					ux.Info(os.Stdout, "Cleaned %d %s from packages no longer in apm.yml", n, pluralWord(n, "file", "files"))
					items := make([]ux.Item, 0, n)
					for _, f := range oc.removed {
						items = append(items, ux.Item{Text: f})
					}
					ux.List(os.Stdout, items)
				}
				for _, d := range oc.diags {
					run.noteDiagnostic()
					ux.Warn(os.Stderr, "%s", d)
				}
				orphans = oc.kept
			}
			var declared []string
			if len(m.Target) > 0 {
				declared, _ = deploy.ResolveTargets("", m.Target, "")
			}
			notCleaned := maps.Clone(deployResult.FailedBuckets)
			for _, dep := range newLock.Dependencies {
				if only != nil && !only[dep.UniqueKey()] {
					if notCleaned == nil {
						notCleaned = map[string]bool{}
					}
					notCleaned[dep.UniqueKey()] = true
				}
			}
			for _, c := range cleanStaleDeployedFiles(prevLock, newLock, notCleaned, refused, keptDirs, targets, declared, cleanRoot, linkSourceRoot) {
				if n := len(c.removed); n > 0 {
					noun := "files"
					if n == 1 {
						noun = "file"
					}
					// Oracle sentence: core/command_logger.py:482-496. The
					// oracle prints the count only; apm-go also lists the
					// paths, because issue #30 asks for them and the deleted
					// files are in the user's workspace.
					label := c.label
					if declared, ok := run.declaredPath(c.label); ok {
						// The Oracle names a local package as apm.yml declares
						// it (core/command_logger.py:482-496).
						label = declared
					}
					ux.Info(os.Stdout, "Cleaned %d stale %s from %s", n, noun, label)
					items := make([]ux.Item, 0, n)
					for _, f := range c.removed {
						items = append(items, ux.Item{Text: f})
					}
					ux.List(os.Stdout, items)
				}
				// The oracle says "Kept user-edited file ..."
				// (command_logger.py:518-529). apm-go prints the diag of
				// deploy.RemoveDeployedFiles, the one delete path install and
				// uninstall share, whose strings are existing output.
				for _, d := range c.diags {
					run.noteDiagnostic()
					ux.Warn(os.Stderr, "%s", d)
				}
			}
		}

		// The lock below no longer records a server that is no longer
		// declared, so its entry has to leave the config files first. A
		// frozen install writes no lock and does not come here.
		if mcpFailure == nil {
			if err := cleanStaleMCPServers(detectRoot, targets, deployResult.DeclaredMCPServers, existingLock, deployDir != ""); err != nil {
				return err
			}
		}
	}

	// 6b. Merge surviving deployed files from the existing lockfile.
	// A re-install with a different --target (or stale-skill reconciliation
	// that kept a hand-edited file) may leave files on disk that this run's
	// deploy did not produce. mergeDeployedFiles drops the entries whose
	// files are gone, so files reconciliation removed in step 6a stay out. A path below a kept directory still exists
	// but is the user's now, so the old record of it is dropped first.
	// A bucket with a failed deploy was not cleaned in step 6a, so every other
	// old entry of it is kept, also one that os.Stat cannot reach (a path
	// through a dangling skill symlink): the lock is the only record a later
	// run has to clean it.
	if existingLock != nil {
		notKept := func(files []string) []string {
			if len(keptDirs) == 0 {
				return files
			}
			var out []string
			for _, f := range files {
				if !deploy.UnderKeptDir(keptDirs, f) {
					out = append(out, f)
				}
			}
			return out
		}
		effectiveDeployRoot := "."
		if deployDir != "" {
			effectiveDeployRoot = deployDir
		}
		claimed := claimedPaths(newLock)
		merge := func(bucket, dir string, oldFiles []string, oldHashes map[string]string, newFiles []string, newHashes map[string]string) ([]string, map[string]string) {
			if failedBuckets[bucket] {
				return mergeDeployedFilesIf(notKept(oldFiles), oldHashes, newFiles, newHashes,
					func(string) bool { return true })
			}
			// A rejected path is the user's symlink, which notTakenOver
			// drops on --global; its record follows the file it names. A
			// path this run deployed is recorded by the package that deployed
			// it, so an old owner of it lets it go (the packages reordered,
			// the project's own primitive of the same name), as the Oracle
			// records one owner.
			var userPaths, rest []string
			for _, f := range notKept(oldFiles) {
				switch {
				case refused[normalizeDeployPath(f)]:
					userPaths = append(userPaths, f)
				case claimed[normalizeDeployPath(f)]:
				default:
					rest = append(rest, f)
				}
			}
			return mergeDeployedFiles(append(notTakenOver(bucket, dir, rest, targets, deployDir), userPaths...), oldHashes, newFiles, newHashes, effectiveDeployRoot)
		}
		for i := range newLock.Dependencies {
			dep := &newLock.Dependencies[i]
			if old := prevLock.FindByKey(dep.UniqueKey()); old != nil {
				dep.DeployedFiles, dep.DeployedHashes = merge(dep.UniqueKey(), old.ModuleDir(),
					old.DeployedFiles, old.DeployedHashes, dep.DeployedFiles, dep.DeployedHashes)
			}
		}
		if len(existingLock.LocalDeployedFiles) > 0 {
			// A lock written before issue #50 lists the merged MCP config
			// file here; it is not carried over.
			var oldLocal []string
			for _, f := range existingLock.LocalDeployedFiles {
				if !deploy.IsMCPConfigPath(f) {
					oldLocal = append(oldLocal, f)
				}
			}
			newLock.LocalDeployedFiles, newLock.LocalDeployedHashes = merge("", "",
				oldLocal, existingLock.LocalDeployedHashes,
				newLock.LocalDeployedFiles, newLock.LocalDeployedHashes)
		}
		newLock.Dependencies = append(newLock.Dependencies, orphans...)
	}

	// 7. No-op check. manifestChanged must gate this alongside lockfile
	// equality (2026-07-30 Tier-2 finding): a dependencies.apm/
	// devDependencies.apm section move can leave the lockfile's own
	// comparison fields unchanged -- notably PackageType, which reads "" on
	// BOTH sides of a move-out-of-dev when the pre-move lock predates the
	// package_type feature and was never marked -- while the manifest still
	// genuinely needs the write that only happens below/in
	// persistPackagesToManifest. Without this, IsSemanticEqual alone would
	// report a false "Already up to date" and the section move's apm.yml
	// write would silently never happen, despite the "moved from ..."
	// info message already printed above.
	newLock.PathTargets = pathTargets(newLock, existingLock, targets, deployDir != "")
	if run != nil {
		return run.finish(installOutcome{
			node: node, packages: packages, effectiveSubsets: effectiveSubsets, dev: dev,
			manifestChanged: manifestChanged, result: result, targets: targets,
			newLock: newLock, existingLock: existingLock, existingNode: existingNode,
			apmInstalled: apmInstalled, mcpConfigured: mcpConfigured, apmPhase: apmPhase, mcpUnplaced: mcpUnplaced, mcpFailure: mcpFailure,
			skippedDeps: skippedDeps, commandNotes: commandNotes, shadowed: shadowed, unmanaged: unmanaged,
			listed: listedDeps(result, requestedKeys, scoped),
		})
	}

	// The Oracle's update ends on RequiredIntegrationError with this line
	// alone, after the lock is written (commands/update.py:889-892).
	unplacedErr := func() error {
		ux.Error(os.Stdout, "MCP dependencies are declared, but no effective target can accept their configuration. Choose a supported --target and retry.")
		return withSilentExitCode(1, errors.New("no target takes the MCP servers at user scope"))
	}
	if existingLock != nil && lockfile.IsSemanticEqual(existingLock, newLock) && !manifestChanged {
		if mcpFailure != nil {
			return mcpFailure
		}
		if mcpUnplaced {
			return unplacedErr()
		}
		ux.Info(os.Stdout, "Already up to date")
		return nil
	}

	// 8. Write lockfile
	outBytes, err := lockfile.WriteLockfile(newLock, existingNode)
	if err != nil {
		return fmt.Errorf("serialize lockfile: %w", err)
	}

	if err := writeLockfileBytes(outBytes); err != nil {
		return err
	}
	// update rewrites a lock from before issue #50 too; the directories that
	// lock named would otherwise stay with no lock entry to clean them (R16).
	removeLegacyLocalModules(existingLock, newLock)
	if mcpFailure != nil {
		return mcpFailure
	}
	if mcpUnplaced {
		return unplacedErr()
	}

	// 9. Persist positional packages to apm.yml
	if len(packages) > 0 {
		if err := persistPackagesToManifest(node, packages, effectiveSubsets, dev); err != nil {
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
	// The user scope keeps no .gitignore (install/pipeline.py:820-826).
	if deployDir == "" {
		updateGitignoreForApmModules()
	}

	fmt.Println()
	// Closing summary. Built as parts joined with " and " so R15's MCP-count
	// clause and R16's local-only wording compose independently instead of
	// each needing their own combinatorial branch:
	//   - R16 (prd.md/design.md §3): a zero-resolved-dependency run that
	//     actually deployed at least one file from the project's own .apm/
	//     tree (localProjectDeployed, decided from the ACTUAL deploy
	//     result, never a second scan) says "local project" instead of the
	//     misleading "0 dependencies" -- without pretending local content
	//     IS a dependency (no lockfile/count semantics change).
	//   - R15: when this run actually configured MCP server(s), name that
	//     count too -- silent when there are none, so a project with
	//     neither MCP nor local-only content keeps today's exact wording.
	//     newLock.MCPServers is populated ONLY a few lines above from
	//     deployResult.MCPProvenance's dedup pass (never carried forward
	//     from existingLock elsewhere in this function), so it already IS
	//     "this run's actual successful server count" (codex M3), not a
	//     stale lockfile total.
	// update <pkg> deployed and records the named packages alone.
	summaryDeps := result.Deps
	if only != nil {
		summaryDeps = slices.DeleteFunc(slices.Clone(result.Deps), func(d resolver.ResolvedDep) bool { return !only[d.Key] })
	}
	var summaryParts []string
	if len(summaryDeps) == 0 && localProjectDeployed {
		summaryParts = append(summaryParts, "local project")
	} else {
		summaryParts = append(summaryParts, fmt.Sprintf("%d %s", len(summaryDeps), pluralWord(len(summaryDeps), "dependency", "dependencies")))
	}
	if n := len(newLock.MCPServers); n > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d %s", n, pluralWord(n, "MCP server", "MCP servers")))
	}
	ux.Success(os.Stdout, "Installed %s", strings.Join(summaryParts, " and "))
	items := make([]ux.Item, 0, len(summaryDeps))
	for _, dep := range summaryDeps {
		label := dep.Key + depVersionLabel(dep)
		// R9/R10c: a dep is "new" only if this call's positional args asked
		// for it AND it wasn't already declared in apm.yml before this call.
		// Everything else here -- transitively-resolved deps, deps a bare
		// `install` re-resolved from an already-populated apm.yml, or a
		// re-requested already-declared dep -- is muted rather than
		// presented as a fresh install. requestedKeys is nil for `update`
		// (see deployAndFinalize's doc comment), where every dep counts as
		// "new" to preserve its unchanged summary rendering.
		isNew := requestedKeys == nil || (requestedKeys[dep.Key] && !existing[dep.Key])
		text := fmt.Sprintf("%s (depth %d)", label, dep.Depth)
		if !isNew {
			// P4-10: the distinction must survive ANSI stripping too, so a
			// non-new dep isn't identified by color alone. A transitive dep
			// (depth>0) is muted as well but is NOT necessarily declared in
			// apm.yml, so it gets an accurate "(transitive)" marker instead of
			// the misleading "(already in apm.yml)".
			switch {
			case existing[dep.Key]:
				text += " (already in apm.yml)"
			case dep.Depth > 0:
				text += " (transitive)"
			}
		}
		items = append(items, ux.Item{Text: text, Muted: !isNew})
	}
	if len(items) > 0 {
		ux.List(os.Stdout, items)
	}

	return nil
}

// updateGitignoreForApmModules mirrors the Oracle's project-scope install
// helper (src/apm_cli/commands/_helpers.py:489-527 and
// src/apm_cli/install/pipeline.py:807-813): keep the generated dependency
// cache out of version control, while preserving an existing .gitignore.
// Gitignore maintenance is best-effort in the Oracle, so read/write failures
// are warnings rather than install failures.
func updateGitignoreForApmModules() {
	updateGitignoreForApmModulesWithWrite(io.WriteString)
}

func updateGitignoreForApmModulesWithWrite(write func(io.Writer, string) (int, error)) {
	const pattern = "apm_modules/"

	data, err := os.ReadFile(".gitignore")
	if err != nil && !os.IsNotExist(err) {
		ux.Warn(os.Stdout, "Could not read .gitignore: %v", err)
		return
	}
	current := string(data)
	lines := strings.Split(current, "\n")
	if strings.HasSuffix(current, "\n") {
		lines = lines[:len(lines)-1]
	}
	for _, line := range lines {
		if strings.TrimSpace(strings.TrimSuffix(line, "\r")) == pattern {
			return
		}
	}

	f, err := os.OpenFile(".gitignore", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		ux.Warn(os.Stdout, "Could not update .gitignore: %v", err)
		return
	}
	defer f.Close()
	appendContent := "\n# APM dependencies\n" + pattern + "\n"
	if len(lines) > 0 && strings.TrimSpace(strings.TrimSuffix(lines[len(lines)-1], "\r")) != "" {
		appendContent = "\n" + appendContent
	}
	if _, err := write(f, appendContent); err != nil {
		ux.Warn(os.Stdout, "Could not update .gitignore: %v", err)
		return
	}
	ux.Info(os.Stdout, "Added %s to .gitignore", pattern)
}

// deployedFilesTree groups a single dependency's deployed files by
// primitive type (skill/agent/instruction/command/prompt/file) into ONE
// aggregated line per kind (R10b), instead of the former one-line-per-exact
// subdirectory layout that produced dozens of lines for a dep with many
// skills. Each line reports the number of distinct primitives of that kind
// (deduplicated across every target root they were mirrored to, e.g. a
// skill deployed to both ".agents/skills/" and ".claude/skills/" counts
// once) and the comma-separated set of target roots they landed in -- e.g.
// "22 skill(s) -> .agents/skills/, .claude/skills/".
func deployedFilesTree(label string, files []string) ux.TreeNode {
	type group struct {
		kind      string
		names     map[string]bool
		roots     map[string]bool
		rootOrder []string
	}
	order := make([]string, 0)
	groups := map[string]*group{}
	for _, f := range files {
		kind := deployedFileKind(f)
		g, ok := groups[kind]
		if !ok {
			g = &group{kind: kind, names: map[string]bool{}, roots: map[string]bool{}}
			groups[kind] = g
			order = append(order, kind)
		}
		root := deployedFileRoot(f, kind)
		if !g.roots[root] {
			g.roots[root] = true
			g.rootOrder = append(g.rootOrder, root)
		}
		// The generic "file" kind has no primitive identity shared across
		// roots (unlike a skill directory mirrored to N roots), so key it by
		// full path -- two different roots' "config.json" stay two files.
		name := f
		if kind != "file" {
			name = deployedPrimitiveName(f, root)
		}
		g.names[name] = true
	}
	children := make([]ux.TreeNode, 0, len(order))
	for _, kind := range order {
		g := groups[kind]
		roots := append([]string(nil), g.rootOrder...)
		sort.Strings(roots)
		count := len(g.names)
		children = append(children, ux.TreeNode{
			Text: fmt.Sprintf("%d %s -> %s", count, pluralWord(count, g.kind, g.kind+"s"), strings.Join(roots, ", ")),
		})
	}
	return ux.TreeNode{Text: label, Children: children}
}

// deployedFileKind classifies a deployed file path into the primitive type
// it belongs to, for deployedFilesTree's grouping.
func deployedFileKind(f string) string {
	switch {
	case strings.Contains(f, "/skills/"):
		return "skill"
	case strings.Contains(f, "/agents/") && !strings.Contains(f, ".agents/"):
		return "agent"
	case strings.Contains(f, "/rules/") || strings.Contains(f, "/instructions/"):
		return "instruction"
	case strings.Contains(f, "/commands/"):
		return "command"
	case strings.Contains(f, "/prompts/"):
		return "prompt"
	default:
		return "file"
	}
}

// deployedKindMarkers maps each deployedFileKind result to the directory
// segment(s) that mark its deploy target root, for deployedFileRoot.
var deployedKindMarkers = map[string][]string{
	"skill":       {"/skills/"},
	"agent":       {"/agents/"},
	"instruction": {"/rules/", "/instructions/"},
	"command":     {"/commands/"},
	"prompt":      {"/prompts/"},
}

// deployedFileRoot returns the target root directory for a deployed file of
// the given kind: the path prefix through (and including) the
// kind-identifying directory segment, e.g. ".claude/skills/foo/SKILL.md" ->
// ".claude/skills/". Falls back to the file's own parent directory for the
// generic "file" kind, which has no fixed marker segment.
func deployedFileRoot(f, kind string) string {
	for _, marker := range deployedKindMarkers[kind] {
		if idx := strings.Index(f, marker); idx >= 0 {
			return f[:idx+len(marker)]
		}
	}
	return f[:strings.LastIndex(f, "/")+1]
}

// deployedPrimitiveName extracts the distinguishing primitive name from a
// deployed file path (relative to its already-computed root), so the same
// primitive mirrored to multiple target roots -- e.g. a skill deployed to
// both ".agents/skills/foo/SKILL.md" and ".claude/skills/foo/SKILL.md", or
// an agent written as "foo.md" by one target and "foo.agent.md" by another
// -- collapses to a single counted entity instead of one per root/target.
func deployedPrimitiveName(f, root string) string {
	rest := f[len(root):]
	if idx := strings.IndexByte(rest, '/'); idx >= 0 {
		// Directory-based primitive (skills/<name>/..., or an
		// agents/<name>/agent.md style layout): the first path segment
		// after root IS the primitive name, shared by all its files/roots.
		return rest[:idx]
	}
	// Flat-file primitive: strip ONE file extension, then ONE known
	// target-specific type suffix (".agent"/".instructions"/...), so the same
	// primitive written as "foo.agent.md" (one target) and "foo.md" (another)
	// collapses to "foo", while unrelated dotted names ("foo.bar.md" vs
	// "foo.baz.md") stay distinct instead of both being flattened to "foo".
	rest = strings.TrimSuffix(rest, filepath.Ext(rest))
	for _, suf := range []string{".agent", ".instructions", ".instruction", ".prompt", ".command"} {
		if strings.HasSuffix(rest, suf) {
			return strings.TrimSuffix(rest, suf)
		}
	}
	return rest
}

// pluralWord returns singular when n == 1, plural otherwise.
func pluralWord(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// shortCommitLen is the number of leading commit-hash characters shown by
// depVersionLabel's unpinned fallback (R10a).
const shortCommitLen = 8

// depVersionLabel returns a "@<version>" suffix for dep, preferring the
// human-readable ResolvedTag, then ResolvedRef; when both are empty (an
// unpinned dependency resolved straight to a commit) it falls back to the
// first 8 characters of the resolved commit SHA so the label is never a
// bare "pkg@" with nothing after it. Returns "" (no suffix at all) when dep
// has neither a tag/ref nor a resolved commit.
func depVersionLabel(dep resolver.ResolvedDep) string {
	switch {
	case dep.ResolvedTag != "":
		return "@" + dep.ResolvedTag
	case dep.ResolvedRef != "":
		return "@" + dep.ResolvedRef
	case dep.Commit != "":
		commit := dep.Commit
		if len(commit) > shortCommitLen {
			commit = commit[:shortCommitLen]
		}
		return "@" + commit
	default:
		return ""
	}
}

func toLockDeps(deps []resolver.ResolvedDep) []lockfile.LockedDep {
	result := make([]lockfile.LockedDep, len(deps))
	for i, d := range deps {
		result[i] = lockfile.LockedDep{
			Host:           d.Host,
			Port:           d.Port,
			RegistryPrefix: d.ArtifactoryPrefix,
			Source:         kindToSource(d.Kind),
		}
	}
	return result
}

// persistPackagesToManifest persists packages into apm.yml's
// dependencies.apm sequence, or -- when dev is true (R9/AC42-43) --
// devDependencies.apm instead. A freshly-created devDependencies key is
// inserted right after includes: (falling back to the end of the document if
// no includes: key exists), matching the R2 semantic key order established
// by 07-29-targets-init-shape's buildManifestNode (design.md §11 "鍵序約束").
// A freshly-created dependencies key keeps its prior append-at-end placement
// unchanged (AC44: the non-dev path must stay byte-identical).
func persistPackagesToManifest(doc *yamllib.Node, packages []string, effectiveSubsets map[string][]string, dev bool) error {
	root := doc
	if root.Kind == yamllib.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yamllib.MappingNode {
		return fmt.Errorf("manifest root is not a mapping")
	}

	sectionKey := "dependencies"
	otherSectionKey := "devDependencies"
	if dev {
		sectionKey, otherSectionKey = "devDependencies", "dependencies"
	}

	// Find or create the target section's mapping node.
	var depsNode *yamllib.Node
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == sectionKey {
			depsNode = root.Content[i+1]
			break
		}
	}
	if depsNode == nil {
		depsNode = &yamllib.Node{Kind: yamllib.MappingNode, Tag: "!!map"}
		keyNode := &yamllib.Node{Kind: yamllib.ScalarNode, Value: sectionKey, Tag: "!!str"}
		if dev {
			insertAt := len(root.Content)
			for i := 0; i < len(root.Content)-1; i += 2 {
				if root.Content[i].Value == "includes" {
					insertAt = i + 2
					break
				}
			}
			root.Content = insertKeyValueAt(root.Content, insertAt, keyNode, depsNode)
		} else {
			root.Content = append(root.Content, keyNode, depsNode)
		}
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

	// Index existing entries two ways: by canonical identity (codex
	// C3/H2 -- locates the SAME already-declared dependency even across a
	// pkg string that differs only by case/URL form, so its skills: field
	// gets updated in place instead of silently `continue`-d past) and by
	// exact literal string (the fallback for parent and unparseable forms,
	// which have no identity and so no entry in effectiveSubsets). A local
	// path has an identity (canonicalIdentityForDepString) and carries its
	// skill subset on the path dict form.
	//
	// The two indexes are populated INDEPENDENTLY (2026-07-30 round-4 fix):
	// entryDepString only recognizes a ScalarNode or a MappingNode carrying
	// a "git:" key, so it returns "" for any other dict shape ParseDepDict
	// still accepts -- notably the legacy `{name: owner/repo}` shorthand,
	// whose canonical identity is the SAME git identity as the plain scalar
	// form. An earlier version `continue`-d past the WHOLE entry whenever
	// entryDepString returned "", which skipped entryCanonicalIdentity too
	// and left such an entry out of existingByIdentity entirely -- so
	// re-declaring it (same section, no cross-section move involved)
	// silently appended a second, duplicate entry instead of being
	// recognized as already-present.
	existingByIdentity := make(map[string]int)
	existingPkgs := make(map[string]bool)
	if apmSeq.Kind == yamllib.SequenceNode {
		for i, entry := range apmSeq.Content {
			if raw := entryDepString(entry); raw != "" {
				existingPkgs[raw] = true
			}
			// Identity comes from the FULL entry, not just its git value: a
			// monorepo dict `{git: repo, path: sub}` is a different
			// dependency from bare `repo` (CanonicalDepKey includes the
			// virtual path), and keying it by the git string alone would
			// let a positional base-repo install mis-hit and rewrite the
			// sub-package entry (final codex gate finding).
			if id := entryCanonicalIdentity(entry); id != "" {
				existingByIdentity[id] = i
			}
		}
	}

	// otherApmSeq is the OTHER section's apm sequence (read-only lookup --
	// never created if absent, unlike depsNode/apmSeq above). External
	// Tier-2 audit finding (scenario A/B): a package already declared here
	// must be relocated into the target section below rather than left to
	// accumulate as a duplicate in both (npm `npm i [-D] X` parity -- this
	// call's --dev flag is the final word on section ownership).
	otherApmSeq := findApmSeq(root, otherSectionKey)

	// refSet holds the identities whose entry already has this call's ref.
	refSet := make(map[string]bool)
	appended := false
	movedOut := false
	for _, pkg := range packages {
		identity := canonicalIdentityForDepString(pkg)

		if identity != "" {
			if idx, ok := existingByIdentity[identity]; ok {
				// C3 fix: rewrite the EXISTING entry in place to the
				// effective (union'd or reset) subset, instead of the old
				// `continue` that left a stale apm.yml value behind.
				// Only the first package of a call with this identity sets the
				// ref; later ones fold onto it (issue #156, Oracle 0.33.0
				// install/package_resolution.py).
				if !refSet[identity] {
					refSet[identity] = true
					if ref, ok := refOfDepString(pkg); ok {
						setEntryRef(apmSeq, idx, ref)
					}
				}
				setEntrySkillSubset(apmSeq, idx, effectiveSubsets[identity])
				appended = true
				// 2026-07-30 round-5 fix: the package can ALSO be declared in
				// the OTHER section (a pre-existing polluted apm.yml, or a
				// prior run that appended before this cleanup existed). The
				// early `continue` below used to skip straight past the
				// cross-section relocation logic further down this loop,
				// leaving the duplicate in the other section forever --
				// re-running install never healed it. Clean it up here too.
				if removeMatchingEntry(otherApmSeq, pkg, identity) != nil {
					movedOut = true
				}
				continue
			}
		} else if existingPkgs[pkg] {
			if removeMatchingEntry(otherApmSeq, pkg, identity) != nil {
				movedOut = true
			}
			continue
		}

		// Not declared in the target section. If it's declared in the OTHER
		// section, relocate it here instead of writing a second, duplicate
		// entry (external Tier-2 audit finding, scenario A/B).
		movedEntry := removeMatchingEntry(otherApmSeq, pkg, identity)
		if movedEntry != nil {
			movedOut = true
		}

		subset := effectiveSubsets[identity]
		switch {
		case movedEntry != nil:
			// 2026-07-30 Tier-2 finding: transplant the ORIGINAL entry node
			// removed from the other section instead of rebuilding a fresh
			// {git, skills}/scalar entry from just pkg -- rebuilding here
			// silently dropped every other persisted field (ref:, alias:,
			// path:, ...) a cross-section move carried, e.g. `{git: acme/foo,
			// ref: stable}` became a bare `acme/foo` scalar. Only the
			// skills: field is adjusted, via the SAME setEntrySkillSubset
			// helper the same-section update path above already uses.
			apmSeq.Content = append(apmSeq.Content, movedEntry)
			if ref, ok := refOfDepString(pkg); ok {
				setEntryRef(apmSeq, len(apmSeq.Content)-1, ref)
			}
			setEntrySkillSubset(apmSeq, len(apmSeq.Content)-1, subset)
		case len(subset) > 0:
			apmSeq.Content = append(apmSeq.Content, newGitSkillEntry(pkg, subset))
		case isInsecureDepString(pkg):
			// An http:// package install takes only with --allow-insecure
			// is declared as an object with allow_insecure: true
			// (models/dependency/reference.py:1956-1969).
			apmSeq.Content = append(apmSeq.Content, insecureGitEntry(pkg))
		default:
			// String form. A local path is written without the whitespace
			// around it, as newGitSkillEntry writes it.
			value := pkg
			if ref, err := manifest.ParseDepString(pkg); err == nil && ref.IsLocal {
				value = ref.LocalPath
			}
			apmSeq.Content = append(apmSeq.Content,
				&yamllib.Node{Kind: yamllib.ScalarNode, Value: value, Tag: "!!str"},
			)
		}
		appended = true

		// BUG-1 (design.md §2): existingByIdentity/existingPkgs are seeded
		// ONCE from apmSeq before this loop -- without registering the entry
		// just appended, a SECOND `packages` argument in this SAME call
		// sharing this pkg's canonical identity (e.g. "Owner/Repo" then
		// "owner/repo" in one `apm-go install` invocation) would fail this
		// loop's own existingByIdentity lookup and append a SECOND,
		// differently-cased apm.yml entry instead of folding onto the one
		// just written.
		if identity != "" {
			existingByIdentity[identity] = len(apmSeq.Content) - 1
			refSet[identity] = true
		} else {
			existingPkgs[pkg] = true
		}
	}

	// mi-fix (#2): a reused pre-existing dependencies.apm sequence node
	// (e.g. a scaffolded `apm: []`) retains its parsed FlowStyle bit, so
	// SafeDump re-renders it flow after appending. Normalize to block
	// (matching dependencies.mcp and the Python original) whenever we
	// actually appended an entry -- leave untouched if nothing changed.
	if appended {
		apmSeq.Style &^= yamllib.FlowStyle
	}
	// A relocated-out other-section sequence emptying to zero entries stays
	// a normal `apm: []` skeleton (2026-07-30 decision) -- no special
	// handling needed here beyond keeping its style normalized, matching
	// apmSeq's own treatment above.
	if movedOut && otherApmSeq != nil {
		otherApmSeq.Style &^= yamllib.FlowStyle
	}

	return nil
}

// insertKeyValueAt returns a copy of content (a flat key/value-alternating
// yaml.Node slice) with the key/val pair inserted at position at (an even
// index -- a key position). Used to place a freshly-created devDependencies
// key at a specific key-order position instead of appending at the end.
func insertKeyValueAt(content []*yamllib.Node, at int, key, val *yamllib.Node) []*yamllib.Node {
	out := make([]*yamllib.Node, 0, len(content)+2)
	out = append(out, content[:at]...)
	out = append(out, key, val)
	out = append(out, content[at:]...)
	return out
}

// findApmSeq locates <sectionKey>.apm's sequence node under root, or nil if
// the section, or its apm: key, doesn't exist. Read-only lookup -- unlike
// the target-section find-or-create logic in persistPackagesToManifest,
// this NEVER creates the section: it's used to look for an already-declared
// dependency that a move-between-sections needs to remove FROM, not a
// target to write into.
func findApmSeq(root *yamllib.Node, sectionKey string) *yamllib.Node {
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value != sectionKey {
			continue
		}
		section := root.Content[i+1]
		if section.Kind != yamllib.MappingNode {
			return nil
		}
		for j := 0; j < len(section.Content)-1; j += 2 {
			if section.Content[j].Value == "apm" {
				return section.Content[j+1]
			}
		}
		return nil
	}
	return nil
}

// removeMatchingEntry removes the FIRST entry in seq matching identity (via
// entryCanonicalIdentity) or, when identity is "", pkg (via entryDepString
// literal match). Used by persistPackagesToManifest to relocate an
// already-declared dependency out of the section this call's --dev flag did
// NOT target (external Tier-2 audit finding, scenario A/B: this call's flag
// is the final word on section ownership, npm `npm i [-D] X` parity).
// Returns the removed entry NODE itself (nil for a nil/non-sequence seq, or
// when nothing matches) -- not just whether something was removed -- so the
// caller can transplant its original fields (ref:, alias:, path:, skills:,
// ...) into the target section instead of rebuilding a fresh entry from just
// pkg, which would silently drop everything but the git value (2026-07-30
// Tier-2 finding).
func removeMatchingEntry(seq *yamllib.Node, pkg, identity string) *yamllib.Node {
	if seq == nil || seq.Kind != yamllib.SequenceNode {
		return nil
	}
	for i, entry := range seq.Content {
		if identity != "" {
			if entryCanonicalIdentity(entry) != identity {
				continue
			}
		} else if entryDepString(entry) != pkg {
			continue
		}
		seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
		return entry
	}
	return nil
}

// entryDepString returns the dependency string a dependencies.apm sequence
// entry carries -- the scalar value itself, or the "git:" key's value for an
// object-form entry -- or "" if neither shape matches.
func entryDepString(entry *yamllib.Node) string {
	if entry.Kind == yamllib.ScalarNode {
		return entry.Value
	}
	if entry.Kind == yamllib.MappingNode {
		for j := 0; j < len(entry.Content)-1; j += 2 {
			if entry.Content[j].Value == "git" {
				return entry.Content[j+1].Value
			}
		}
	}
	return ""
}

// canonicalIdentityForDepString parses an apm.yml-persisted dependency
// string and returns its deploy.CanonicalDepKey, or "" if it doesn't parse
// as a dependency string at all, or parses to a genuinely local/parent
// reference (which has no stable repository identity -- see
// CanonicalRepoIdentity). normalizeLocalDep is applied first so a
// LOCAL-PATH dependency (which ParseDepString alone reports as IsLocal, but
// which runInstall's own step 1b-2 loop always normalizes into a
// Source="git" apm_modules/_local/<name> materialization key before
// effectiveSkillSubsets/deploy.Run ever see it) computes the SAME identity
// here that the rest of the --skill subset pipeline uses -- without this,
// a positional local-path package would parse straight to IsLocal=true and
// silently fall out of identity-based matching entirely (its subset would
// never be found OR written).
func canonicalIdentityForDepString(s string) string {
	ref, err := manifest.ParseDepString(s)
	if err != nil {
		return ""
	}
	normalizeLocalDep(ref)
	return deploy.CanonicalDepKey(ref)
}

// entryCanonicalIdentity computes a dependencies.apm sequence entry's
// canonical identity from the WHOLE entry: a dict entry is parsed with
// ParseDepDict so its path:/type: selectors participate exactly the way
// they do everywhere else in the --skill pipeline (CanonicalDepKey includes
// the virtual path -- `{git: repo, path: sub}` must never share an identity
// with bare `repo`). Falls back to "" for entries that don't parse (never
// matched by identity, only by the raw-string existingPkgs index).
func entryCanonicalIdentity(entry *yamllib.Node) string {
	if entry.Kind == yamllib.ScalarNode {
		return canonicalIdentityForDepString(entry.Value)
	}
	if entry.Kind == yamllib.MappingNode {
		ref, err := manifest.ParseDepDict(entry, 0)
		if err != nil {
			return ""
		}
		normalizeLocalDep(ref)
		return deploy.CanonicalDepKey(ref)
	}
	return ""
}

// setEntrySkillSubset updates ONLY the skills: key of apmSeq.Content[idx],
// leaving every other persisted field (ref:, path:, type:, alias:, ...)
// untouched -- the first draft rebuilt the whole entry as {git, skills} and
// silently dropped a version pin like `ref: stable` (final codex gate
// finding). A scalar entry gaining a subset is upgraded to the object form;
// a mapping whose subset is cleared drops just its skills: pair, and only
// collapses back to the scalar form when git: or path: was the sole
// remaining key (preserving the pre-existing wildcard-RESET round-trip
// shape).
// The entry's ORIGINAL persisted git value is preserved verbatim
// (first-declared spelling wins). A local path is written as the path the
// entry reads as, without the whitespace around it, as the Oracle writes it
// (models/dependency/reference.py:1955-1963), whenever the subset of the
// entry is set or cleared.
//
// Keeping the other fields when a subset is ADDED is a deliberate deviation
// from the Oracle, which rebuilds a local entry from the new reference and
// so drops a hand-written alias: (models/dependency/reference.py:1955-1963).
// Adding a subset must not make a field the user wrote disappear (owner
// ruling 2026-10-13, issue #61).
func setEntrySkillSubset(apmSeq *yamllib.Node, idx int, subset []string) {
	entry := apmSeq.Content[idx]

	if entry.Kind == yamllib.ScalarNode {
		if len(subset) == 0 {
			return // scalar form already means "no subset"
		}
		apmSeq.Content[idx] = newGitSkillEntry(entry.Value, subset)
		return
	}
	if entry.Kind != yamllib.MappingNode {
		return
	}
	trimLocalPath := func() {
		ref, err := manifest.ParseDepDict(entry, idx)
		if err != nil || !ref.IsLocal {
			return
		}
		for j := 0; j < len(entry.Content)-1; j += 2 {
			if v := entry.Content[j+1]; entry.Content[j].Value == "path" && v.Value != ref.LocalPath {
				v.Value, v.Style = ref.LocalPath, 0
			}
		}
	}

	for j := 0; j < len(entry.Content)-1; j += 2 {
		if entry.Content[j].Value != "skills" {
			continue
		}
		trimLocalPath()
		if len(subset) == 0 {
			entry.Content = append(entry.Content[:j], entry.Content[j+2:]...)
			// Collapse {git: x} or {path: x} (nothing else left) to the
			// scalar form, matching how a subset-free dependency is
			// persisted in the first place; any other surviving key
			// (ref:, alias:, ...) keeps the mapping. manifest.ParseDepDict
			// reads a path: value as the string form reads the same
			// string, so the value is written back as it is.
			if len(entry.Content) != 2 {
				return
			}
			if key := entry.Content[0].Value; key == "git" || key == "path" {
				apmSeq.Content[idx] = &yamllib.Node{Kind: yamllib.ScalarNode, Value: entry.Content[1].Value, Tag: "!!str"}
			}
			return
		}
		entry.Content[j+1] = newSkillSeqNode(subset)
		return
	}
	if len(subset) > 0 {
		trimLocalPath()
		entry.Content = append(entry.Content,
			&yamllib.Node{Kind: yamllib.ScalarNode, Value: "skills", Tag: "!!str"},
			newSkillSeqNode(subset),
		)
	}
}

// newGitSkillEntry builds the object-form dependencies.apm entry
// `{git: gitVal, skills: [subset...]}`. A local path gets the key `path`
// instead and loses the whitespace around it, as the Oracle writes it
// (models/dependency/reference.py:1955-1963, object_fields.py:87-101):
// `git: ./dep` would make the next install clone the path (issue #61).
func newGitSkillEntry(gitVal string, subset []string) *yamllib.Node {
	sourceKey := "git"
	if ref, err := manifest.ParseDepString(gitVal); err == nil && ref.IsLocal {
		sourceKey, gitVal = "path", ref.LocalPath
	}
	entry := &yamllib.Node{Kind: yamllib.MappingNode, Tag: "!!map"}
	if isInsecureDepString(gitVal) {
		entry = insecureGitEntry(gitVal)
	} else {
		entry.Content = append(entry.Content,
			&yamllib.Node{Kind: yamllib.ScalarNode, Value: sourceKey, Tag: "!!str"},
			&yamllib.Node{Kind: yamllib.ScalarNode, Value: gitVal, Tag: "!!str"},
		)
	}
	entry.Content = append(entry.Content,
		&yamllib.Node{Kind: yamllib.ScalarNode, Value: "skills", Tag: "!!str"},
		newSkillSeqNode(subset),
	)
	return entry
}

func isInsecureDepString(pkg string) bool {
	ref, err := manifest.ParseDepString(pkg)
	return err == nil && ref.IsInsecure()
}

// insecureGitEntry is the object entry of an http:// package: its git value
// and allow_insecure: true.
func insecureGitEntry(gitVal string) *yamllib.Node {
	return &yamllib.Node{Kind: yamllib.MappingNode, Tag: "!!map", Content: []*yamllib.Node{
		{Kind: yamllib.ScalarNode, Value: "git", Tag: "!!str"},
		{Kind: yamllib.ScalarNode, Value: gitVal, Tag: "!!str"},
		{Kind: yamllib.ScalarNode, Value: "allow_insecure", Tag: "!!str"},
		{Kind: yamllib.ScalarNode, Value: "true", Tag: "!!bool"},
	}}
}

// newSkillSeqNode builds the skills: sequence node for subset.
func newSkillSeqNode(subset []string) *yamllib.Node {
	skillSeq := &yamllib.Node{Kind: yamllib.SequenceNode, Tag: "!!seq"}
	for _, s := range subset {
		skillSeq.Content = append(skillSeq.Content,
			&yamllib.Node{Kind: yamllib.ScalarNode, Value: s, Tag: "!!str"},
		)
	}
	return skillSeq
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
		ux.Warn(os.Stderr, "%s", w)
	}

	// mkt-027: a structured DepRef (a non-GitHub-family host's
	// in-marketplace plugin) always wins over parsing
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
		// Outside the project tree the absolute path is written, as the
		// design of mkt-025 chose. A "../" path would parse too since issue
		// #64; what the Oracle writes for this case has not been measured.
		return abs
	}
	if !strings.HasPrefix(rel, "./") {
		rel = "./" + rel
	}
	return rel
}

// moveDependencyBetweenSections relocates the ALREADY-DECLARED dependency
// identified by key from its current section (m.ParsedDeps or
// m.ParsedDevDeps -- whichever is NOT what toDev names) into the section
// this call's --dev flag requested, and tells the user about it (never
// silent). This is the fix for the external Tier-2 audit's scenario A/B
// finding: a positional re-install of an already-declared dependency,
// targeting the OTHER section than where it's currently declared, must MOVE
// it there -- mirroring npm's `npm i [-D] X` behavior of relocating an
// already-declared dependency between sections -- instead of leaving a
// duplicate entry behind in both. A no-op (returns false, changing nothing)
// if key isn't actually found in the expected source section. The returned
// bool tells the caller whether a move actually happened, so it can force a
// write even when the resulting lockfile happens to compare equal to the
// existing one (manifestChanged, install.go's runInstall -- see its
// declaration for why lockfile equality alone isn't sufficient).
func moveDependencyBetweenSections(m *manifest.Manifest, key string, toDev bool) bool {
	if toDev {
		ref, rest := extractDepByKey(m.ParsedDeps, key)
		if ref == nil {
			return false
		}
		m.ParsedDeps = rest
		m.ParsedDevDeps = append(m.ParsedDevDeps, ref)
		ux.Info(os.Stdout, "%s: moved from dependencies.apm to devDependencies.apm (--dev)", key)
		return true
	}
	ref, rest := extractDepByKey(m.ParsedDevDeps, key)
	if ref == nil {
		return false
	}
	m.ParsedDevDeps = rest
	m.ParsedDeps = append(m.ParsedDeps, ref)
	ux.Info(os.Stdout, "%s: moved from devDependencies.apm to dependencies.apm", key)
	return true
}

// extractDepByKey removes and returns the FIRST dependency reference in refs
// whose deploy.DepRefKey matches key, along with the remaining slice (a new
// backing array, so the original slice/caller's other references aren't
// aliased by the removal). Returns (nil, refs) unchanged if key isn't found.
func extractDepByKey(refs []*manifest.DependencyReference, key string) (*manifest.DependencyReference, []*manifest.DependencyReference) {
	rest := make([]*manifest.DependencyReference, 0, len(refs))
	var found *manifest.DependencyReference
	for _, r := range refs {
		if found == nil && deploy.DepRefKey(r) == key {
			found = r
			continue
		}
		rest = append(rest, r)
	}
	return found, rest
}

// normalizeLocalDep rewrites a local-directory dependency in place into the
// form apm-go materializes by COPYING into apm_modules (Python's local-dep
// model: apm_modules/_local/<name>/), instead of git-cloning it. It targets
// two indistinguishable-downstream shapes:
//
//	(a) a genuine local dep (IsLocal, from a path in apm.yml, under `path:`
//	    or `git:` too, or a CLI positional arg like "./pkg" or "/abs/pkg"); and
//	(b) mkt-025's local-marketplace fast path result (Source=="git" with an
//	    empty Owner/Repo and an ABSOLUTE RepoURL, no git ref).
//
// Both are converted to {Source:"git", RepoURL:"_local/<name>", LocalSourcePath:
// <abs source>, LocalDeclaredPath: <path as written>}. RepoURL becomes the
// contained apm_modules key that the resolver (depKey), deploy (DepRefKey),
// and the lockfile (repo_url) all use, so no filepath.Join(apm_modules, key)
// ever sees an absolute path. The real source path rides on LocalSourcePath
// for the loader's copy step and is never persisted; LocalDeclaredPath is what
// the lock records as local_path, and LocalDeclaredRef the ref a `git:` entry
// declares, which the copy does not use. No-op (returns without change) for
// anything else, and idempotent (an already-"_local/…" ref matches neither
// shape).
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
	ref.LocalDeclaredRef = ref.Reference
	ref.Reference = ""
	ref.Source = "git"
	ref.RepoURL = localModulesKey(abs)
	ref.LocalSourcePath = abs
	ref.LocalDeclaredPath = src
}

// errLocalNameCollision reports two local packages whose source directories
// have the same name. Both would be copied to apm_modules/_local/<name>.
//
// Deliberate deviation from the Oracle (issue #50, owner ruling R6): it
// copies the second over the first and exits 0, leaving one directory and two
// lock entries with the same repo_url.
func errLocalNameCollision(key, first, second string) error {
	return fmt.Errorf("local packages %s and %s have the same directory name %q; a local package is installed to apm_modules/%s, so the two would overwrite each other -- rename one of the directories",
		first, second, strings.TrimPrefix(key, localModulesKeyPrefix), key)
}

// localSources records the source directory of each local package by its
// apm_modules key and refuses a second directory for the same key, and a
// copy gitops.CheckLocalCopy refuses. Every direct local dependency passes
// here before the first copy, so a refusal leaves earlier copies as they were.
type localSources map[string]string

func (s localSources) add(ref *manifest.DependencyReference) error {
	if ref.LocalSourcePath == "" {
		return nil
	}
	if first, ok := s[ref.RepoURL]; ok && first != ref.LocalSourcePath {
		return errLocalNameCollision(ref.RepoURL, first, ref.LocalSourcePath)
	}
	s[ref.RepoURL] = ref.LocalSourcePath
	return gitops.CheckLocalCopy("apm_modules", ref)
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

// localModulesKey is the apm_modules key and lock repo_url of a
// copy-materialized local dependency: _local/<name of the source directory>,
// the Oracle's layout (install/phases/local_content.py:184). Two sources with
// the same directory name share a key; runInstall refuses that
// (errLocalNameCollision).
func localModulesKey(abs string) string {
	name := filepath.Base(abs)
	if name == "." || name == ".." || strings.ContainsAny(name, `/\`+"\x00") {
		name = sanitizePathSegment(name)
	}
	return localModulesKeyPrefix + name
}

// legacyLocalModulesKey is the key apm-go gave a local dependency before
// issue #50: the directory name plus eight hex characters of the hash of the
// absolute source path. A lock written then still names it, so uninstall and
// the conversion of an old lock have to find it.
func legacyLocalModulesKey(abs string) string {
	base := sanitizePathSegment(filepath.Base(abs))
	sum := sha256.Sum256([]byte(filepath.Clean(abs)))
	return localModulesKeyPrefix + base + "-" + hex.EncodeToString(sum[:])[:8]
}

// heldLine is one stdout line of install whose printing waits for the resolve.
type heldLine struct {
	// onlyOnSuccess marks a line that states something the run does only when
	// it succeeds.
	onlyOnSuccess bool
	print         func()
}

func printHeldLines(lines []heldLine, succeeded bool) {
	for _, l := range lines {
		if succeeded || !l.onlyOnSuccess {
			l.print()
		}
	}
}

// isMissingLocalPath reports whether ref names a local directory that is not
// there. It reads the path as normalizeLocalDep does, on a copy.
func isMissingLocalPath(ref *manifest.DependencyReference) bool {
	c := *ref
	normalizeLocalDep(&c)
	if c.LocalSourcePath == "" {
		return false
	}
	_, err := os.Stat(c.LocalSourcePath)
	return os.IsNotExist(err)
}

// useLegacyLocalKeys gives a local dependency the key a lock from before
// issue #50 pins it under, when that lock has no entry under the current key.
// A frozen install changes neither the lock nor apm_modules/, so it has to
// read such a lock as it is (ruling R16); every other install converts it.
func useLegacyLocalKeys(m *manifest.Manifest, lock *lockfile.Lockfile) {
	for _, deps := range [][]*manifest.DependencyReference{m.ParsedDeps, m.ParsedDevDeps} {
		for _, dep := range deps {
			if dep.LocalSourcePath == "" || lock.FindByKey(dep.RepoURL) != nil {
				continue
			}
			if legacy := legacyLocalModulesKey(dep.LocalSourcePath); lock.FindByKey(legacy) != nil {
				dep.RepoURL = legacy
			}
		}
	}
}

// localModulesKeyPrefix marks an apm_modules key and lock repo_url that
// localModulesKey produced.
const localModulesKeyPrefix = "_local/"

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

// absentLocalCopies lists the apm_modules directories of the direct local
// packages that do not exist yet. A failed resolve removes exactly these
// (ruling R20): the loader copies a local package before the resolver can
// refuse it, and a refused package must not stay in apm_modules.
func absentLocalCopies(m *manifest.Manifest) []string {
	var absent []string
	for _, d := range allDirectDeps(m) {
		if d.LocalSourcePath == "" {
			continue
		}
		dir := deploy.DepRefDir(d)
		if _, err := os.Lstat(filepath.Join("apm_modules", filepath.FromSlash(dir))); os.IsNotExist(err) {
			absent = append(absent, dir)
		}
	}
	return absent
}

// removeNewLocalCopies deletes the directories absentLocalCopies or
// fetchedDirsLoader listed, and the parent directories that leaves empty,
// apm_modules included.
func removeNewLocalCopies(dirs []string) {
	for _, dir := range dirs {
		if _, err := deploy.SafeRemoveModuleDir(".", dir); err != nil {
			ux.Warn(os.Stderr, "could not remove apm_modules/%s: %v", dir, err)
		}
	}
}

// fetchedDirsLoader records the apm_modules directory of each package the
// resolve loads, direct or transitive, that did not exist before its load.
// An install the deploy refuses removes them, as the Oracle's install leaves
// no package it fetched (measured for a local, a git and a registry package,
// and for a registry package's dependency). It also decides which packages
// this run fetches, as the Oracle's download callback does
// (install/phases/resolve.py:455-481): one is fetched, and announced with
// its Resolving line, when its directory is absent or its remote reference
// left the lock (shouldFetch); every other one is cached.
type fetchedDirsLoader struct {
	next resolver.PackageLoader
	keys []string
	// lock is the lock before this run; fetched gets each key it fetches.
	lock     *lockfile.Lockfile
	fetched  map[string]bool
	announce func(name string)
}

func (l *fetchedDirsLoader) LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error) {
	if key := deploy.DepRefKey(ref); key != "" {
		rel := deploy.DepRefDir(ref)
		dir := filepath.Join("apm_modules", filepath.FromSlash(rel))
		_, statErr := os.Lstat(dir)
		absent := os.IsNotExist(statErr)
		if absent {
			l.keys = append(l.keys, rel)
		}
		// The Oracle checks the path before it prints the Resolving line
		// (resolve.py:455-464); a local copy has its own check, run before
		// the resolve (gitops.CheckLocalCopy), but for the directory of an
		// alias, which the copy refuses as the Oracle does.
		if ref.LocalSourcePath == "" || ref.Alias != "" {
			if err := archive.CheckPackageDir("apm_modules", dir, ref.Alias); err != nil {
				return nil, err
			}
		}
		if l.fetched != nil && !l.fetched[key] && (absent || (ref.LocalSourcePath == "" && shouldFetch(ref, l.lock))) {
			l.fetched[key] = true
			if l.announce != nil {
				l.announce(resolvingName(ref))
			}
		}
	}
	return l.next.LoadPackage(ref, resolvedRef)
}

func fetchedKeys(l *fetchedDirsLoader) map[string]bool {
	if l == nil {
		return nil
	}
	return l.fetched
}

// shouldFetch is the Oracle's should_force_ref_recheck for a remote package
// whose directory is there (drift.py:95-218): it is fetched again when the
// lock has no entry for it or its reference changed since.
func shouldFetch(ref *manifest.DependencyReference, lock *lockfile.Lockfile) bool {
	if ref.ArtifactoryPrefix != "" {
		return false
	}
	var locked *lockfile.LockedDep
	if lock != nil {
		locked = lock.FindByKey(deploy.DepRefKey(ref))
	}
	return locked == nil || lockedRefChanged(ref, locked)
}

// lockedRegistryResolution gives the registry loader the resolution the lock
// records for a registry package whose reference has not changed and whose
// directory below modulesDir hashes to the lock's content_hash, so a second
// install neither lists its versions nor downloads it again. As Oracle
// 0.33.0's _should_skip_redownload (install/phases/_redownload.py:25-37,
// integrate.py:166-177), a lock entry with no content_hash, or a directory
// that is missing or changed, is downloaded and checked against the digest
// again, so an edited cache is never deployed or recorded.
func lockedRegistryResolution(lock *lockfile.Lockfile, modulesDir string) func(*manifest.DependencyReference, string) (registry.Resolution, bool) {
	return func(ref *manifest.DependencyReference, version string) (registry.Resolution, bool) {
		if lock == nil {
			return registry.Resolution{}, false
		}
		entry := lock.FindByKey(deploy.DepRefKey(ref))
		if entry == nil || entry.Source != "registry" || entry.ResolvedHash == "" || entry.ContentHash == "" ||
			entry.Version != version || lockedRefChanged(ref, entry) {
			return registry.Resolution{}, false
		}
		dir := filepath.Join(modulesDir, filepath.FromSlash(ref.ModuleDir()))
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return registry.Resolution{}, false
		}
		if hash, err := lockfile.ComputePackageHash(dir); err != nil || hash != entry.ContentHash {
			return registry.Resolution{}, false
		}
		return registry.Resolution{ResolvedURL: entry.ResolvedURL, ResolvedHash: entry.ResolvedHash, Version: entry.Version}, true
	}
}

// resolvingName is the Oracle's get_display_name
// (models/dependency/reference.py:2009-2017): a local package's declared
// path, a virtual package's name, else the repository path.
func resolvingName(ref *manifest.DependencyReference) string {
	switch {
	case ref.Alias != "":
		return ref.Alias
	case ref.LocalSourcePath != "":
		return ref.LocalDeclaredPath
	case ref.VirtualPath != "":
		return virtualPackageName(ref.RepoURL, ref.VirtualPath)
	}
	return ref.RepoURL
}

// virtualPackageName is the Oracle's get_virtual_package_name
// (reference.py:302-325): the repository's last segment and the virtual
// path's last segment, its virtual file extension dropped.
func virtualPackageName(repoURL, virtualPath string) string {
	last := path.Base(virtualPath)
	for _, ext := range []string{".prompt.md", ".instructions.md", ".agent.md"} {
		if strings.HasSuffix(last, ext) {
			last = strings.TrimSuffix(last, ext)
			break
		}
	}
	return path.Base(repoURL) + "-" + last
}

// newGitPackageLoader is the loader install and update fetch packages into
// modulesDir with. Tests replace it to clone a git dependency from a path
// below the working directory instead of from its host.
var newGitPackageLoader = func(modulesDir string) resolver.PackageLoader {
	return &gitops.RealPackageLoader{ModulesDir: modulesDir}
}

// localDirectDeps maps the apm_modules key of each direct local package to
// its reference, after normalizeLocalDep.
func localDirectDeps(m *manifest.Manifest) map[string]*manifest.DependencyReference {
	local := make(map[string]*manifest.DependencyReference)
	for _, d := range allDirectDeps(m) {
		if d.LocalSourcePath != "" {
			local[deploy.DepRefKey(d)] = d
		}
	}
	return local
}

// localDeclaredPaths maps the apm_modules key of each direct local package
// to its path as declared, after normalizeLocalDep.
func localDeclaredPaths(m *manifest.Manifest) map[string]string {
	declared := make(map[string]string)
	for _, d := range allDirectDeps(m) {
		if d.LocalSourcePath != "" {
			declared[deploy.DepRefKey(d)] = d.LocalDeclaredPath
		}
	}
	return declared
}

// localPackageIdentity reads what the lock records about a materialized
// local package, as Oracle 0.33.0 records it (measured,
// ~/.cache/apm-go-oracle-ref/A/75 and c50): the package type of the Oracle's
// cascade (models/format_detection.py:436-491), and the name and version of
// the package the Oracle reads there. An APM package (an apm.yml with .apm/
// or that declares a dependency) and every other package with an apm.yml are
// those of its apm.yml. A Claude plugin is those of the apm.yml the Oracle
// builds from plugin.json (deps/plugin_parser.py:501-505,1369-1373): the
// manifest's name, else the directory's, and its version, else the apm.yml's,
// else 0.0.0. A root SKILL.md alone is the name of its frontmatter and
// "unknown" (models/validation.py:573-581).
func localPackageIdentity(dir string) (name, pkgVersion, packageType string) {
	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
		return err == nil
	}
	hasManifest := false
	if data, err := os.ReadFile(filepath.Join(dir, "apm.yml")); err == nil {
		if node, err := yamlcore.SafeLoad(data); err == nil {
			if sub, _, err := manifest.ParseManifest(node); err == nil {
				hasManifest = true
				name, pkgVersion = sub.Name, sub.Version
			}
		}
	}
	rootSkill := exists("SKILL.md")
	nestedSkills, _ := filepath.Glob(filepath.Join(dir, "skills", "*", "SKILL.md"))
	switch {
	case deploy.IsAPMPackage(dir) && rootSkill:
		packageType = "hybrid"
	case deploy.IsAPMPackage(dir) && len(nestedSkills) > 0:
		packageType = "skill_bundle"
	case deploy.IsAPMPackage(dir):
		packageType = lockfile.PackageTypeAPMPackage
	case isClaudePluginDir(dir):
		manifestPath := pluginJSONPath(dir)
		name = pluginField(manifestPath, "name")
		if name == "" {
			name = filepath.Base(dir)
		}
		if v := pluginField(manifestPath, "version"); v != "" {
			pkgVersion = v
		} else if pkgVersion == "" {
			pkgVersion = "0.0.0"
		}
		packageType = lockfile.PackageTypeMarketplacePlugin
	case hasManifest && rootSkill:
		packageType = "hybrid"
	case rootSkill:
		name = deploy.SkillName(filepath.Join(dir, "SKILL.md"), filepath.Base(dir))
		pkgVersion = "unknown"
		packageType = "claude_skill"
	case len(nestedSkills) > 0:
		packageType = "skill_bundle"
	case hasManifest:
		packageType = lockfile.PackageTypeAPMPackage
	case exists("hooks"):
		packageType = "hook_package"
	}
	return name, pkgVersion, packageType
}

// isSubdirectoryPackage reports whether a dependency with this virtual path
// is a directory taken out of a repository, as opposed to a whole repository
// or a single file.
func isSubdirectoryPackage(virtualPath string) bool {
	return manifest.VirtualTypeOf(virtualPath) == "subdirectory"
}

// clonedSubdirCommit returns the commit the git loader behind regLoader cloned
// the subdirectory package key from in this run, or "" when it did not clone
// it.
func clonedSubdirCommit(regLoader *registry.Loader, key string) string {
	if regLoader == nil {
		return ""
	}
	if source, ok := regLoader.Next.(interface{ SubdirCommit(string) string }); ok {
		return source.SubdirCommit(key)
	}
	return ""
}

// lockedKind is the reference kind of a dependency the lock records, for a
// resolution rebuilt from the lock: deploy.Run reads a registry package's
// root skill whole.
func lockedKind(d *lockfile.LockedDep) resolver.ReferenceKind {
	switch {
	case d.Source == "registry":
		return resolver.KindRegistry
	case d.Source == "local":
		return resolver.KindLocal
	case d.Constraint != "":
		return resolver.KindGitSemver
	default:
		return resolver.KindGitLiteral
	}
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
