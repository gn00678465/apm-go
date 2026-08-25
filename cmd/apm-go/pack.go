package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/marketplace/authoring"
	"github.com/apm-go/apm/internal/marketplace/build"
	"github.com/apm-go/apm/internal/pack"
	"github.com/apm-go/apm/internal/pack/bundle"
	"github.com/apm-go/apm/internal/pack/pluginmanifest"
	"github.com/apm-go/apm/internal/ux"
	"github.com/apm-go/apm/internal/yamlcore"
	"github.com/spf13/cobra"
)

// packCmd implements `apm pack`'s three independent, non-exclusive
// producers (Phase 2-5, research/pack-parity-findings.md §1.3): a
// dependencies: block builds a plugin-native bundle under ./build/
// (BundleProducer), a marketplace: block (or legacy marketplace.yml)
// builds marketplace.json (MarketplaceProducer, mkt-054/055, unchanged from
// its original single-producer form), and a target:/targets: field
// containing "claude" and/or "copilot" builds a standalone plugin.json
// (PluginManifestProducer). Any subset may fire in the same invocation;
// when none apply, pack fails loud (exit 1) rather than silently doing
// nothing -- matching Python's BuildOrchestrator.run BuildError, replacing
// this command's prior exit-0 "nothing to do" (design.md Gate 1
// disposition).
func packCmd() *cobra.Command {
	var (
		offline           bool
		includePrerelease bool
		dryRun            bool
		force             bool
		marketplaceFilter string
		pathOverrideArgs  []string
		verbose           bool
		format            string
		claudePlugin      bool
		output            string
		target            string
		archiveFlag       bool
		archiveFormat     string
	)

	cmd := &cobra.Command{
		Use:   "pack",
		Short: "Build marketplace.json, a plugin bundle, and/or a standalone plugin.json from apm.yml",
		// Ticket 17 phase 1: this is the Oracle's own _PACK_HELP docstring
		// (commands/pack.py:25-66) verbatim, "apm" -> "apm-go" (folded back
		// by the parity runner's rewrite_binary_name normalization for
		// comparison) -- replacing apm-go's previously self-authored
		// three-bullet summary. help_semantic only compares the first
		// paragraph ("Pack distributable artifacts..."), but the ticket
		// asks for the whole docstring (examples, exit-code table) for
		// genuine --help parity, not just to satisfy that narrow check.
		// The docstring references --archive/--check-versions/--check-clean,
		// which this phase does not implement yet (ticket 17's later
		// phases) -- textually accurate to the Oracle regardless of what
		// apm-go has actually wired up so far, same as the Oracle's own
		// docstring is unconditional on nothing else in the file.
		Long: `Pack distributable artifacts from your APM project.

Reads apm.yml to decide what to produce:

  dependencies: block  ->  bundle (directory or archive; see --archive and --archive-format)
  marketplace: block   ->  selected marketplace artifacts
  target: / targets:   ->  ecosystem-specific plugin.json (claude/copilot)
  both blocks present  ->  bundle plus selected marketplace artifacts

The lockfile (apm.lock.yaml) pins bundle contents. An enriched copy
is embedded in each bundle.

Examples:

  # Bundle only (most common -- just dependencies: in apm.yml):
  apm-go pack                              # Legacy Claude plugin bundle (current default)
  apm-go pack --format agent-plugin        # Portable Agent Plugins v1 bundle
  apm-go pack --format apm -o ./dist       # Legacy APM bundle layout

  # Marketplace only (marketplace: in apm.yml, no dependencies:):
  apm-go pack
  apm-go pack --offline --dry-run

  # Both (apm.yml has dependencies: AND marketplace: blocks):
  apm-go pack
  apm-go pack --archive --offline

  # Marketplace output paths are normally configured in apm.yml:
  # marketplace.claude.output / marketplace.codex.output

Exit codes:
  0  Success
  1  Build or runtime error
  2  Manifest schema validation error
  3  Version alignment check failed (--check-versions)
  4  Marketplace working-tree drift detected (--check-clean)`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// resolve_bundle_format runs before ANY producer, lockfile
			// read/write, marketplace build, or output-directory creation
			// (commands/pack.py:318-326; ticket 07 §D of .review/
			// ticket-review.md) -- so this is the very first thing RunE
			// does, ahead of even runPack's read-only apm.yml/lockfile
			// probing.
			bf, err := resolveBundleFormat(format, cmd.Flags().Changed("format"), claudePlugin, packFormatChoices)
			if err != nil {
				return withUsageError(err)
			}
			switch bf {
			case pluginModeAgent:
				return withExitCode(2, fmt.Errorf("bundle format 'agent-plugin' is not yet supported by apm-go; use --format claude-plugin"))
			case bundleModeApm:
				return withExitCode(2, fmt.Errorf("bundle format 'apm' is not yet supported by apm-go; use --format claude-plugin"))
			}
			// Ticket 17 phase 2: --archive-format's Choice validation runs
			// BEFORE the "no effect without --archive" check -- verified
			// live against the pinned Oracle (`--archive-format bogus
			// --dry-run`, no --archive: reports the Choice error, never
			// reaches the no-effect check). Only validated when the flag
			// was actually given (cmd.Flags().Changed) -- an absent flag
			// keeps archiveFormat as "" (bundle.Produce's own
			// effectiveArchiveFormat applies the "zip" default).
			if cmd.Flags().Changed("archive-format") {
				if err := validateArchiveFormatChoice(archiveFormat); err != nil {
					return withUsageError(err)
				}
				if !archiveFlag {
					return withUsageError(fmt.Errorf(
						"--archive-format has no effect without --archive; add --archive to produce a .%s archive.",
						archiveFormat))
				}
			}
			return runPack(cmd, packOptions{
				offline:           offline,
				includePrerelease: includePrerelease,
				dryRun:            dryRun,
				force:             force,
				marketplaceFilter: marketplaceFilter,
				pathOverrideArgs:  pathOverrideArgs,
				verbose:           verbose,
				bundleFormat:      bundleFormatLockValue(bf),
				output:            output,
				target:            target,
				archive:           archiveFlag,
				archiveFormat:     archiveFormat,
			})
		},
	}
	// Click turns a flag given without its value into a usage error (exit
	// 2, "Option '--format' requires an argument."); cobra reports it as a
	// plain parse error. Map it here so the CLI contract matches (shared
	// with `plugin init`, cmd/apm-go/plugin.go).
	setBundleFormatFlagErrorFunc(cmd)

	cmd.Flags().BoolVar(&offline, "offline", false, "use cached refs only (no network); fails packages with a pinned ref/version instead of silently degrading")
	cmd.Flags().BoolVar(&includePrerelease, "include-prerelease", false, "include prerelease versions when resolving semver ranges")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be written without writing")
	cmd.Flags().BoolVar(&force, "force", false, "bundle producer: last writer wins on file_map collisions and overwrite an existing plugin.json; has no effect on the hidden-character scan, which never blocks")
	cmd.Flags().StringVarP(&marketplaceFilter, "marketplace", "m", "", "comma-separated marketplace outputs to build (e.g. 'claude,codex'); 'all' (default) builds every configured output, 'none' skips marketplace entirely")
	cmd.Flags().StringArrayVar(&pathOverrideArgs, "marketplace-path", nil, "override the output path for a format: FORMAT=PATH (repeatable)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print extra diagnostics")
	cmd.Flags().Var(bundleFormatChoiceValue{&format, packFormatChoices}, "format",
		"Bundle format selector. 'agent-plugin' emits portable Agent Plugins v1; "+
			"'plugin' is the compatibility alias for the legacy Claude plugin bundle; "+
			"'claude' / 'claude-plugin' also emit that bundle; and 'apm' emits the "+
			"legacy APM bundle layout. The current no-flag default is 'claude-plugin'. "+
			"apm-go currently implements only the Claude plugin bundle; agent-plugin and apm are accepted but refused.")
	cmd.Flags().BoolVar(&claudePlugin, "claude-plugin", false, "Select the legacy Claude plugin bundle output (current no-flag default).")
	// Ticket 17 phase 1: exact Oracle help text (commands/pack.py:206-211).
	// The Cobra/pflag default is deliberately "" (the zero value), not
	// "./build" -- pflag auto-appends its own `(default "X")` annotation to
	// a flag's help line whenever the default is non-zero, which would
	// double up with (and mismatch the format of) the literal
	// "(default: ./build)" already baked into this string, breaking
	// help_semantic's DefaultIfShown comparison. runBundleProducer applies
	// the "./build" fallback itself when output == "".
	cmd.Flags().StringVarP(&output, "output", "o", "", "Bundle output directory (default: ./build).")
	// Ticket 17 phase 1: exact Oracle help text (commands/pack.py:177-183).
	// Wired through to bundle.ProduceOptions.Target (embedded in the
	// bundle's apm.lock.yaml as pack.target, internal/pack/bundle/
	// lockfile_pack.go's NewPackMetadata) only when explicitly given --
	// apm-go does not replicate the Oracle's detect_target()-based
	// auto-fill when --target is absent (commands/pack.py:361-368, a real,
	// separate auto-detection feature, not "wire an existing value through
	// a flag"); the absent-flag default stays "all", unchanged from before
	// this ticket. Also not replicated: TargetParamType's comma-separated
	// multi-value parsing and "copilot"->"vscode" alias resolution
	// (target_detection.py:744-765) -- apm-go stores whatever single string
	// is given, verbatim. Both simplifications are deliberately out of this
	// phase's "no new logic" scope; the Oracle's own deprecation warning
	// (pack.py:370-374) is likewise not reproduced here.
	cmd.Flags().StringVarP(&target, "target", "t", "", "[Deprecated] Target platform filter. Bundles are now target-agnostic; the consumer's project decides where files land at install time. Value is recorded in pack.target as informational metadata only and is ignored by 'apm-go install'. The flag will be removed in a future release.")
	// Ticket 17 phase 2: exact Oracle help text (commands/pack.py:184-194).
	// --archive itself needs no default annotation: Click never shows one
	// for an is_flag=True option with default=False (verified live), and
	// pflag's own bool zero-value ("false") triggers no auto-annotation
	// either -- a plain BoolVar is enough, unlike --output/--archive-format.
	cmd.Flags().BoolVar(&archiveFlag, "archive", false,
		"Produce a .zip archive instead of a directory (previous default: .tar.gz; "+
			"use --archive-format tar.gz for legacy CI pipelines).")
	// Exact Oracle help text (commands/pack.py:195-205), including Click's
	// own show_default=True auto-suffix ("  [default: zip]", note two
	// leading spaces -- confirmed via direct Click introspection, since a
	// terminal-width-wrapped --help capture alone hid it). Same "empty
	// pflag default + baked-in literal text" pattern as --output/--target
	// (ticket 17 phase 1): the Cobra/pflag default is deliberately "", not
	// "zip", so pflag's own `(default "X")` auto-annotation (parens+quotes,
	// a format that would never match Click's own) never fires; bundle.
	// Produce's effectiveArchiveFormat applies the real "zip" default in
	// code. --archive-format is a case-SENSITIVE Choice on the Oracle side
	// (no case_sensitive=False, unlike --format) -- validated separately in
	// RunE via validateArchiveFormatChoice, not coerceBundleFormat (which
	// normalizes case/aliases, wrong for this flag).
	cmd.Flags().Var(bundleFormatChoiceValue{&archiveFormat, archiveFormatChoices}, "archive-format",
		"Archive format when --archive is set. 'zip' (default) is Claude Code and "+
			"plugin-host compatible and matches apm publish output. 'tar.gz' is "+
			"typically smaller for text-heavy bundles and preserves the previous "+
			"default for CI pipelines that rely on it.  [default: zip]")
	return cmd
}

// archiveFormatChoices mirrors --archive-format's Click Choice values
// (commands/pack.py:197: type=click.Choice(["zip", "tar.gz"])) -- a
// case-sensitive, alias-free list distinct from packFormatChoices/
// bundleFormatAliases (bundle_format.go), which apply only to --format.
var archiveFormatChoices = []string{"zip", "tar.gz"}

// validateArchiveFormatChoice mirrors Click's Choice validation for
// --archive-format: case-sensitive, exact membership only (no
// normalization, no aliases -- unlike coerceBundleFormat). Verified live
// against the pinned Oracle: `--archive-format ZIP` (uppercase) is
// rejected, and the error text is
// "Invalid value for '--archive-format': 'bogus' is not one of 'zip', 'tar.gz'."
func validateArchiveFormatChoice(value string) error {
	for _, c := range archiveFormatChoices {
		if value == c {
			return nil
		}
	}
	return fmt.Errorf("Invalid value for '--archive-format': '%s' is not one of %s.",
		value, quoteJoin(archiveFormatChoices))
}

// packOptions carries packCmd's parsed flag values into runPack.
type packOptions struct {
	offline           bool
	includePrerelease bool
	dryRun            bool
	force             bool
	marketplaceFilter string
	pathOverrideArgs  []string
	verbose           bool
	// bundleFormat is the resolved --format/--claude-plugin selector's
	// canonical BundleFormat.lock_value (bundleFormatLockValue, ticket
	// 07) -- always "claude-plugin" today, since RunE refuses
	// agent-plugin/apm before runPack is ever called.
	bundleFormat string
	// output is --output/-o's raw value ("" when not given, meaning
	// runBundleProducer's own "./build" default applies -- ticket 17).
	output string
	// target is --target/-t's raw value ("" when not given -- ticket 17).
	target string
	// archive/archiveFormat mirror --archive/--archive-format (ticket 17
	// phase 2); archiveFormat is "" when --archive-format was not given
	// (bundle.Produce's effectiveArchiveFormat applies the "zip" default).
	archive       bool
	archiveFormat string
}

// runPack reads apm.yml once, routes to whichever of the three producers
// DetectOutputs says should run (in Python's fixed Bundle -> Marketplace ->
// PluginManifest order, so message sequencing matches the oracle), and
// aborts immediately on the first producer error -- already-completed
// producer output from earlier in the sequence is NOT rolled back
// (findings §7.3: matching Python's own no-transaction semantics; adding
// rollback would be over-engineering beyond the oracle).
//
// mErr (a manifest.ParseManifest failure, e.g. a missing required version:)
// is deliberately NOT returned immediately: Python's own detect_outputs
// (build_orchestrator.py:346-393) determines hasDeps/targets from a raw
// yaml.safe_load dict, never through a full schema-validating parse, so a
// marketplace-only apm.yml that happens to omit version: (legal for
// MarketplaceProducer, which never required it) must keep working exactly
// as it did before this task -- matching the prior deferredPackInputs
// precedent of tolerating a parse failure here. mErr only surfaces if
// either (a) DetectOutputs would otherwise report "nothing to pack" (a
// real, more specific error beats a generic one), or (b) the bundle/
// plugin-manifest producer that mErr prevented us from evaluating actually
// needs to run.
func runPack(cmd *cobra.Command, opts packOptions) error {
	warnIfLicenseUndeclared(cmd.ErrOrStderr())

	hasMarketplace := hasMarketplaceConfig(".")

	m, apmYMLRoot, mErr := loadPackManifest()
	var hasDeps bool
	var targets []string
	if mErr == nil && m != nil {
		// v0.28.0 (build_orchestrator.py:361, PR #2458) changed upstream's
		// test from raw-value truthiness to `is not None`: ANY present,
		// non-null dependencies: value -- including an empty `{}` -- runs
		// the bundle producer. Only a missing key or an explicit null
		// (`dependencies:` with nothing after it) skips it.
		hasDeps = yamlValueIsNotNull(nodeMappingValue(apmYMLRoot, "dependencies"))
		targets = m.Target
	}

	doBundle, doMarketplace, doPluginManifest, detectErr := pack.DetectOutputs(hasDeps, hasMarketplace, targets)
	if detectErr != nil {
		if mErr != nil {
			return mErr
		}
		return detectErr
	}
	if mErr != nil {
		// hasDeps/targets were forced false/nil above, so doBundle and
		// doPluginManifest can only be true here if the raw manifest read
		// failed entirely (apm.yml missing) -- neither is reachable once
		// mErr is a real parse/validation error, but guard anyway rather
		// than silently skipping a producer the user actually asked for.
		if doBundle || doPluginManifest {
			return mErr
		}
	}

	if doBundle {
		if err := runBundleProducer(cmd, m, apmYMLRoot, hasMarketplace, opts); err != nil {
			return err
		}
	}
	if doMarketplace {
		if err := runMarketplaceProducer(cmd, opts); err != nil {
			return err
		}
	}
	if doPluginManifest {
		w := cmd.OutOrStdout()
		if _, err := pluginmanifest.Produce(w, ".", apmYMLRoot, targets, opts.force, opts.dryRun); err != nil {
			return err
		}
	}
	return nil
}

// loadPackManifest reads and parses apm.yml, if present, returning the
// PARSED MANIFEST plus apm.yml's top-level YAML MAPPING node (root, i.e.
// doc.Content[0] -- NOT the yaml.DocumentNode manifest.ParseManifest itself
// consumes) -- bundle.Synthesize/pluginmanifest.Produce both expect the
// mapping root directly. A missing apm.yml is not an error here (m, root
// both nil, err nil) -- DetectOutputs' "nothing to pack" check runs
// regardless and reports it uniformly with every other no-op case. A real
// parse/validation error is returned alongside a nil m/root; see runPack's
// doc comment for why the caller doesn't always propagate it immediately.
// nodeMappingValue returns the value node for key in a mapping node, or nil
// when node is not a mapping or key is absent.
func nodeMappingValue(node *yamllib.Node, key string) *yamllib.Node {
	if node == nil || node.Kind != yamllib.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// yamlValueIsNotNull mirrors v0.28.0's `data.get("dependencies") is not
// None` (build_orchestrator.py:361): a present key with ANY non-null value
// counts -- an empty mapping/sequence and even falsy scalars ("", 0, false)
// all run the bundle producer; only a missing key (nil node) or an explicit
// YAML null does not. This replaced the v0.27 Python-truthiness rule, whose
// load-bearing difference was `dependencies: {}` (falsy dict -> no bundle).
func yamlValueIsNotNull(node *yamllib.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind {
	case yamllib.MappingNode, yamllib.SequenceNode:
		return true
	case yamllib.ScalarNode:
		return node.Tag != "!!null"
	}
	return false
}

func loadPackManifest() (m *manifest.Manifest, root *yamllib.Node, err error) {
	data, err := os.ReadFile("apm.yml")
	if err != nil {
		return nil, nil, nil
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parse apm.yml: %w", err)
	}
	m, _, err = manifest.ParseManifest(doc)
	if err != nil {
		return nil, nil, fmt.Errorf("apm.yml: %w", err)
	}
	return m, doc.Content[0], nil
}

// resolvePackOutputDir implements --output/-o (ticket 17 phase 1): raw's
// own empty-string default means the pre-existing "./build" behavior;
// otherwise raw must resolve within the project root (".").
//
// The Oracle places NO such restriction on -o at all (commands/pack.py:
// 206-211's `type=click.Path()` is a bare path-shape validator, then
// `bundle_output=Path(output)` is used completely unrestricted, including
// an absolute path or one that climbs outside the project via "..") -- this
// is a DELIBERATE apm-go-only hardening beyond the Oracle, not a parity
// fix: an unrestricted, CLI-flag-controlled write-anywhere is a real
// footgun (a wrapper script or CI job interpolating an unsanitized -o
// value could point a build step at an arbitrary path) even for a local
// CLI tool the invoking user already has filesystem permissions for. Reuses
// the same build.EnsureWithinRoot containment helper this exact function
// already calls one level down (OutputDir/bundleRel, producer.go:174) --
// the same convention internal/marketplace/authoring/refcheck.go's
// resolveLocalSourceAgainstRoot applies to a local marketplace source --
// rather than a second, hand-rolled check.
func resolvePackOutputDir(raw string) (string, error) {
	if raw == "" {
		return filepath.Join(".", "build"), nil
	}
	return build.EnsureWithinRoot(".", raw)
}

// runBundleProducer builds the plugin-native bundle from apm.yml's
// dependencies: block, mirroring BundleProducer.produce
// (core/build_orchestrator.py:93-124) -> export_plugin_bundle. m is never
// nil here (DetectOutputs' hasDeps can only be true when apm.yml parsed
// successfully).
func runBundleProducer(cmd *cobra.Command, m *manifest.Manifest, apmYMLNode *yamllib.Node, hasMarketplaceBlock bool, opts packOptions) error {
	w := cmd.OutOrStdout()

	hasLocalDep := false
	for _, d := range m.ParsedDeps {
		if d.IsLocal {
			hasLocalDep = true
			break
		}
	}

	lf, lockNode, err := loadPackLockfile()
	if err != nil {
		return err
	}

	deps := bundleDepSources(m, lf)

	pkgVersion := m.Version
	if pkgVersion == "" {
		pkgVersion = "0.0.0"
	}

	outputDir, err := resolvePackOutputDir(opts.output)
	if err != nil {
		return err
	}

	result, err := bundle.Produce(w, bundle.ProduceOptions{
		ProjectRoot:                   ".",
		OutputDir:                     outputDir,
		PkgName:                       m.Name,
		PkgVersion:                    pkgVersion,
		Target:                        opts.target,
		Force:                         opts.force,
		DryRun:                        opts.dryRun,
		HasLocalDep:                   hasLocalDep,
		Deps:                          deps,
		ApmYMLNode:                    apmYMLNode,
		SuppressMissingPluginJSONInfo: hasMarketplaceBlock,
		Lockfile:                      lf,
		LockfileNode:                  lockNode,
		Format:                        opts.bundleFormat,
		Archive:                       opts.archive,
		ArchiveFormat:                 opts.archiveFormat,
	})
	if err != nil {
		return err
	}

	if opts.dryRun {
		ux.Section(w, fmt.Sprintf("dry-run: Would pack %d file(s) -> %s", len(result.Files), result.BundleDir))
		items := make([]ux.Item, len(result.Files))
		for i, f := range result.Files {
			items[i] = ux.Item{Text: f}
		}
		ux.BulletList(w, items)
		return nil
	}
	displayDir := displayPath(result.BundleDir)
	ux.Sparkle(w, "Packed %d file(s) -> %s", len(result.Files), displayDir)
	// pack.py:679-680 (_render_bundle_result): the real (non-dry-run) file
	// listing is `logger.verbose_detail`, gated on -v -- unlike dry-run's
	// `logger.tree_item` a few lines up in the same function, which is
	// unconditional (ticket 13 finding 1, verified directly: a plain
	// `apm pack` prints only the 3-line summary; the per-file list only
	// shows under `apm pack --verbose`). R12a's original "always list"
	// behavior is preserved for --dry-run (ux.BulletList a few lines
	// above this function's dry-run branch, unaffected) and now also
	// gated the same way for the real run.
	if opts.verbose {
		items := make([]ux.Item, len(result.Files))
		for i, f := range result.Files {
			items[i] = ux.Item{Text: f}
		}
		ux.BulletList(w, items)
	}
	// pack.py:695-699: the Claude-plugin wording branch -- the ONLY one
	// reachable through apm-go pack today. Agent Plugin and legacy APM
	// bundles are refused before this function is ever called
	// (bundle_format.go's own comment: "agent-plugin and apm are refused
	// before any lockfile write"; pack-refuse-agent-plugin/pack-refuse-apm
	// cover that), so their own distinct Oracle wording
	// ("Agent Plugin bundle ready...") and the legacy APM format's total
	// absence of this line are both dead code here, not an omission.
	ux.Info(w, "Claude plugin bundle ready -- contains plugin.json plus "+
		"plugin-native directories and an embedded apm.lock.yaml.")
	ux.Info(w, "Share with: apm-go install %s", displayDir)
	return nil
}

// displayPath renders an absolute bundle/output path the way the Oracle's
// own success/share-with lines do: relative to the current working
// directory (ticket 13 finding 2 -- apm-go printed the absolute
// filesystem path; the Oracle's `bundle_path` is built directly from the
// user-facing `--output`/default "./build" without ever being resolved to
// absolute). Falls back to the absolute path unchanged if Rel fails (e.g.
// a different volume on Windows) rather than erroring a successful pack.
func displayPath(abs string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return abs
	}
	rel, err := filepath.Rel(cwd, abs)
	if err != nil {
		return abs
	}
	return rel
}

// loadPackLockfile reads apm.lock.yaml, if present, mirroring install.go's
// own read pattern. A missing lockfile is not an error (nil, nil, nil) --
// BundleProducer treats it as "no embedded-lockfile step" (§3.6).
func loadPackLockfile() (*lockfile.Lockfile, *yamllib.Node, error) {
	data, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		return nil, nil, nil
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parse apm.lock.yaml: %w", err)
	}
	lf, err := lockfile.ParseLockfile(node)
	if err != nil {
		return nil, nil, fmt.Errorf("validate apm.lock.yaml: %w", err)
	}
	return lf, node, nil
}

// bundleDepSources builds BundleProducer's dependency collection list from
// the lockfile, skipping every DIRECT devDependencies.apm entry (findings
// §3.2 point 1: apm-go's lockfile has no is_dev flag, so this mirrors
// Python's _get_dev_dependency_urls fallback path -- matching by
// (repo_url, virtual_path) rather than a lockfile flag). Returns nil when
// there is no lockfile at all.
func bundleDepSources(m *manifest.Manifest, lf *lockfile.Lockfile) []bundle.DepSource {
	if lf == nil {
		return nil
	}
	devKeys := make(map[string]bool, len(m.ParsedDevDeps))
	for _, d := range m.ParsedDevDeps {
		devKeys[d.RepoURL+"\x00"+d.VirtualPath] = true
	}

	var deps []bundle.DepSource
	for _, dep := range lf.Dependencies {
		if devKeys[dep.RepoURL+"\x00"+dep.VirtualPath] {
			continue
		}
		deps = append(deps, bundle.DepSource{
			Name:        dep.RepoURL,
			InstallPath: filepath.Join("apm_modules", dep.UniqueKey()),
			VirtualPath: dep.VirtualPath,
			RepoURL:     dep.RepoURL,
		})
	}
	return deps
}

// runMarketplaceProducer builds marketplace.json from apm.yml's
// marketplace: block (or a legacy marketplace.yml), mirroring mkt-054/055's
// original single-producer packCmd body -- unchanged behavior, only
// extracted into its own function so runPack can call it conditionally
// alongside the two new producers.
func runMarketplaceProducer(cmd *cobra.Command, opts packOptions) error {
	w := cmd.OutOrStdout()

	cfg, src, err := authoring.LoadAuthoringConfig(".")
	if err != nil {
		return err
	}
	if src == authoring.ConfigSourceLegacy {
		ux.Warn(cmd.ErrOrStderr(), "reading legacy marketplace.yml; run 'apm-go marketplace migrate' to fold it into apm.yml")
	}

	cliOverrides, err := parseMarketplacePathOverrides(opts.pathOverrideArgs)
	if err != nil {
		return err
	}
	filterSet, buildAll, err := parseMarketplaceFilter(opts.marketplaceFilter)
	if err != nil {
		return err
	}

	configuredOutputs := cfg.Outputs
	if len(configuredOutputs) == 0 {
		configuredOutputs = []string{"claude"}
	}
	var activeOutputs []string
	if buildAll {
		activeOutputs = configuredOutputs
	} else {
		for _, o := range configuredOutputs {
			if filterSet[o] {
				activeOutputs = append(activeOutputs, o)
			}
		}
	}
	if len(activeOutputs) == 0 {
		ux.Info(w, "No marketplace outputs selected; nothing to write.")
		return nil
	}

	resolved, warnings, err := build.ResolvePackages(cfg, build.Options{
		IncludePrerelease: opts.includePrerelease,
		Offline:           opts.offline,
		ProjectRoot:       ".",
	})
	if err != nil {
		return err
	}
	for _, warning := range warnings {
		ux.Warn(cmd.ErrOrStderr(), "%s", warning)
	}
	if opts.verbose {
		items := make([]ux.Item, len(resolved))
		for i, pkg := range resolved {
			items[i] = ux.Item{Text: pkg.Entry.Name}
		}
		ux.BulletList(w, items)
	}

	configPaths, err := build.LoadOutputPathOverrides(".", src)
	if err != nil {
		return err
	}

	for _, format := range activeOutputs {
		if err := packOneOutput(cmd, format, cfg, resolved, configPaths, cliOverrides, opts); err != nil {
			return err
		}
	}
	return nil
}

// packOneOutput composes and (unless dry-run) writes a single output
// profile's marketplace.json.
func packOneOutput(
	cmd *cobra.Command,
	format string,
	cfg *authoring.AuthoringConfig,
	resolved []build.ResolvedPackage,
	configPaths, cliOverrides map[string]string,
	opts packOptions,
) error {
	w := cmd.OutOrStdout()

	outputPath, err := build.ResolveOutputPath(format, configPaths, cliOverrides)
	if err != nil {
		return err
	}
	absPath, err := build.EnsureWithinRoot(".", outputPath)
	if err != nil {
		return err
	}

	doc, docWarnings, err := composeMarketplaceDocument(format, cfg, resolved)
	if err != nil {
		return err
	}
	for _, warning := range docWarnings {
		ux.Warn(cmd.ErrOrStderr(), "%s", warning)
	}

	if opts.dryRun {
		ux.Info(w, "dry-run: Would write marketplace.json [%s] (%d package(s)) -> %s", format, len(resolved), outputPath)
		return nil
	}

	if err := build.WriteOutput(absPath, doc); err != nil {
		return err
	}
	// pack.py's _render_marketplace_result: logger.success(f"Built {message}")
	// with no symbol override -- the same "[*]" default as the bundle
	// producer's success line (ux.Sparkle's own doc comment).
	ux.Sparkle(w, "Built marketplace.json [%s] (%d package(s)) -> %s", format, len(resolved), outputPath)
	return nil
}

// composeMarketplaceDocument dispatches to the mkt-050/052/053 mapper for
// format ("claude" or "codex" -- parseMarketplaceFilter/
// build.KnownOutputFormats already reject anything else before this is ever
// reached).
func composeMarketplaceDocument(format string, cfg *authoring.AuthoringConfig, resolved []build.ResolvedPackage) (any, []string, error) {
	switch format {
	case "claude":
		return build.ClaudeMapper{}.Compose(cfg, resolved)
	case "codex":
		return build.CodexMapper{}.Compose(cfg, resolved)
	default:
		return nil, nil, fmt.Errorf("unknown marketplace output format %q", format)
	}
}

// parseMarketplacePathOverrides parses --marketplace-path's repeatable
// "FORMAT=PATH" values into a format -> path override map (mkt-054): FORMAT
// must be a known output profile name, PATH must be non-empty. Malformed
// input is a usage error (surfaced as an ordinary non-nil error, which
// main()'s root.Execute() path turns into exit 1 -- this sub-task
// implements no distinct "usage error" exit code).
func parseMarketplacePathOverrides(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	overrides := make(map[string]string, len(values))
	for _, v := range values {
		idx := strings.Index(v, "=")
		if idx < 0 {
			return nil, fmt.Errorf("--marketplace-path must be FORMAT=PATH, got: %q", v)
		}
		format := strings.TrimSpace(v[:idx])
		path := strings.TrimSpace(v[idx+1:])
		if !build.KnownOutputFormats[format] {
			return nil, fmt.Errorf("unknown marketplace format %q in --marketplace-path; known formats: claude, codex", format)
		}
		if path == "" {
			return nil, fmt.Errorf("--marketplace-path %s= must specify a non-empty path", format)
		}
		overrides[format] = path
	}
	return overrides, nil
}

// parseMarketplaceFilter parses -m/--marketplace's value (mkt-054/055):
// unset or "all" (case-insensitive) means "every configured output"
// (buildAll=true); "none" (case-insensitive) means "skip marketplace
// entirely" (an empty, non-nil filter, buildAll=false); anything else is a
// comma-separated allow-list of known output profile names.
func parseMarketplaceFilter(value string) (filter map[string]bool, buildAll bool, err error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.EqualFold(trimmed, "all") {
		return nil, true, nil
	}
	if strings.EqualFold(trimmed, "none") {
		return map[string]bool{}, false, nil
	}

	filter = map[string]bool{}
	for _, f := range strings.Split(trimmed, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !build.KnownOutputFormats[f] {
			return nil, false, fmt.Errorf("unknown marketplace format %q in --marketplace; known formats: claude, codex", f)
		}
		filter[f] = true
	}
	return filter, false, nil
}

// hasMarketplaceConfig reports whether dir has a marketplace: block in
// apm.yml (present with a non-null value) or a standalone legacy
// marketplace.yml file -- DetectOutputs' marketplace trigger needs to tell
// this apart from every other authoring.LoadAuthoringConfig error, which
// must propagate as a real exit-1 failure instead.
//
// This re-reads apm.yml's top-level shape directly rather than exporting a
// new sentinel error from internal/marketplace/authoring: this sub-task's
// Rollback Points restricts every already-landed file to a single,
// unrelated edit (main.go's one-line AddCommand), so this narrowly-scoped
// duplicate read keeps that boundary intact. It defaults to true (defer to
// authoring.LoadAuthoringConfig's own, more detailed error) whenever apm.yml
// exists but this quick read cannot positively confirm "no marketplace key
// at all" -- so a real parse error is never misreported as "nothing to do".
func hasMarketplaceConfig(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "marketplace.yml")); err == nil {
		return true
	}

	data, err := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if err != nil {
		return false // apm.yml doesn't exist either -> genuinely no config
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		return true // apm.yml exists but fails to parse -> a real error
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yamllib.MappingNode {
		return true // malformed shape -> a real error
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "marketplace" {
			v := root.Content[i+1]
			return !(v.Kind == yamllib.ScalarNode && v.Tag == "!!null")
		}
	}
	return false // apm.yml parses fine but has no marketplace: key at all
}

// licenseUndeclaredWarning is export/authoring.py's _WARN_MESSAGE, ported
// verbatim (minus the leading "[warn] " tag, now supplied by ux.Warn).
const licenseUndeclaredWarning = "No 'license:' field in apm.yml; the SBOM will record NOASSERTION for this package. Add a 'license:' field to apm.yml (an SPDX expression such as MIT or Apache-2.0, or UNLICENSED) to declare it."

// warnIfLicenseUndeclared mirrors export/authoring.py's authoring-path
// license nudge (issue #1777, findings §4): when apm.yml exists and has no
// non-empty license: field, print a single actionable warning. Fires
// unconditionally, before producer routing -- even when pack ultimately
// does nothing (matches Python: commands/pack.py:325-332 runs this before
// BuildOrchestrator().run() is ever called, so it fires regardless of
// which producers end up applicable or whether detect_outputs later raises
// "nothing to pack"). Never blocks; this ASYMMETRICALLY only fires for the
// author's OWN apm.yml (the authoring path) -- consuming other people's
// dependencies stays silent, matching Python's design intent. A missing/
// unreadable/unparsable apm.yml simply yields no warning (mirrors Python's
// "never raises" contract).
func warnIfLicenseUndeclared(w io.Writer) {
	data, err := os.ReadFile("apm.yml")
	if err != nil {
		return
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yamllib.MappingNode {
		return
	}
	root := doc.Content[0]
	declared := false
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "license" {
			continue
		}
		v := root.Content[i+1]
		declared = v.Kind == yamllib.ScalarNode && v.Tag != "!!null" && strings.TrimSpace(v.Value) != ""
		break
	}
	if declared {
		return
	}
	ux.Warn(w, "%s", licenseUndeclaredWarning)
}
