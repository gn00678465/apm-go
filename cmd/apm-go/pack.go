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

// marketplaceDocsURL is the docs anchor renderMarketplaceCatalog points at,
// copied verbatim from the Oracle's MARKETPLACE_DOCS_URL (pack.py:21-23).
// The Oracle deliberately never names a vendor CLI inline here -- APM is
// vendor-agnostic and the install command varies by AI assistant -- so this
// single link stands in for every per-assistant install recipe.
const marketplaceDocsURL = "https://microsoft.github.io/apm/producer/publish-to-a-marketplace/#consume-from-any-assistant"

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
		checkVersions     bool
		checkClean        bool
		jsonOutput        bool
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
			archiveFormatExplicit := cmd.Flags().Changed("archive-format")
			if archiveFormatExplicit {
				if err := validateArchiveFormatChoice(archiveFormat); err != nil {
					return withUsageError(err)
				}
				if !archiveFlag {
					return withUsageError(fmt.Errorf(
						"--archive-format has no effect without --archive; add --archive to produce a .%s archive.",
						archiveFormat))
				}
			}
			// Ticket 17 phase 2 (eval follow-up): mirrors show_zip_migration_notice
			// (commands/pack.py:566-570) exactly -- both of the Oracle's own
			// conditions, not a simplified version: --archive was given, the
			// EFFECTIVE archive format resolves to "zip" (the no-flag
			// default), AND --archive-format was NOT itself given explicitly
			// (ctx.get_parameter_source(...) is not COMMANDLINE) -- an
			// explicit `--archive --archive-format zip` does NOT show the
			// notice, only the implicit-default path does.
			effectiveArchiveFormat := archiveFormat
			if effectiveArchiveFormat == "" {
				effectiveArchiveFormat = "zip"
			}
			showZipMigrationNotice := archiveFlag && effectiveArchiveFormat == "zip" && !archiveFormatExplicit
			return runPack(cmd, packOptions{
				offline:                offline,
				includePrerelease:      includePrerelease,
				dryRun:                 dryRun,
				force:                  force,
				marketplaceFilter:      marketplaceFilter,
				pathOverrideArgs:       pathOverrideArgs,
				verbose:                verbose,
				bundleFormat:           bundleFormatLockValue(bf),
				output:                 output,
				target:                 target,
				archive:                archiveFlag,
				archiveFormat:          archiveFormat,
				showZipMigrationNotice: showZipMigrationNotice,
				checkVersions:          checkVersions,
				checkClean:             checkClean,
				jsonOutput:             jsonOutput,
			})
		},
	}
	// Click turns a flag given without its value into a usage error (exit
	// 2, "Option '--format' requires an argument."); cobra reports it as a
	// plain parse error. Map it here so the CLI contract matches (shared
	// with `plugin init`, cmd/apm-go/plugin.go).
	setBundleFormatFlagErrorFunc(cmd)

	cmd.Flags().BoolVar(&offline, "offline", false, "Marketplace: use cached refs, skip network.")
	cmd.Flags().BoolVar(&includePrerelease, "include-prerelease", false, "Marketplace: include pre-release version tags.")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be packed without writing")
	cmd.Flags().BoolVar(&force, "force", false, "Allow overwriting on collision: last-writer-wins in plugin bundles; overwrites any existing plugin.json at the generated manifest path.")
	cmd.Flags().StringVarP(&marketplaceFilter, "marketplace", "m", "", "Comma-separated marketplace outputs to build (e.g. 'claude,codex'). Use 'all' for every configured output, 'none' to skip marketplace. Default: build all configured outputs.")
	cmd.Flags().StringArrayVar(&pathOverrideArgs, "marketplace-path", nil, "Override output path for a format: FORMAT=PATH (repeatable). Example: --marketplace-path claude=dist/marketplace.json")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed packing information.")
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
	// Ticket 17 phase 3: --legacy-skill-paths, exact Oracle help text
	// (commands/pack.py:286-295). Deliberately a pure accept-and-ignore flag,
	// with NO Go variable bound to its value at all (unlike every other flag
	// on this command) -- this is not a shortcut, it FAITHFULLY mirrors the
	// Oracle's own behavior for `pack` specifically: `legacy_skill_paths` is
	// a declared parameter of pack_cmd (pack.py:314) but is never read
	// anywhere else in that file's body, and its real effect
	// (apply_legacy_skill_paths, integration/targets.py:994) is wired ONLY
	// into `install`/`deps update` (install.py:1221-1225, deps/cli.py:
	// 1047-1051) -- commands that deploy files into per-client target
	// directories, a concept `pack`'s own producers (which build a single,
	// target-agnostic bundle -- see this command's own Long text) never
	// have. Verified LIVE against the pinned Oracle, not assumed from
	// reading the source alone: `pack --legacy-skill-paths` and a plain
	// `pack` produce byte-identical output and an identical bundle tree, in
	// BOTH the BundleProducer path (a dependencies:-only apm.yml with a real
	// .apm/skills/hello/SKILL.md) and the PluginManifestProducer path (a
	// target: claude apm.yml) -- the flag is a genuine no-op on this
	// command on the Oracle side, so apm-go's own no-op is the correct,
	// parity-matching implementation, not a stub.
	cmd.Flags().Bool("legacy-skill-paths", false,
		"Deploy skill files to per-client paths (e.g. .cursor/skills/) instead of "+
			"the shared .agents/skills/ directory. Compatibility flag for projects "+
			"that need per-client skill layouts.")
	// Ticket 17 phase 4: exact Oracle help text (commands/pack.py:305-320).
	// Both are release gates: pure verification, never write anything of
	// their own (--check-clean's own MarketplaceBuilder is unconditionally
	// dry-run, matching the Oracle's, regardless of this command's own
	// --dry-run). Exit codes 3 (version)/4 (drift) are applied at the very
	// end of runReleaseGates' caller, after every producer's own output has
	// rendered -- 3 wins over 4 when both fail simultaneously, per the
	// Oracle's own explicit "Gate exit codes... 3 wins over 4" comment
	// (pack.py:576-580), confirmed live.
	cmd.Flags().BoolVar(&jsonOutput, "json", false,
		"Emit machine-readable JSON to stdout; logs go to stderr.")
	cmd.Flags().BoolVar(&checkVersions, "check-versions", false,
		"Release gate: verify per-package versions agree with the configured "+
			"marketplace.versioning.strategy (lockstep | tag_pattern | per_package). "+
			"Exits 3 on misalignment. Composes with --check-clean and --dry-run.")
	cmd.Flags().BoolVar(&checkClean, "check-clean", false,
		"Release gate: regenerate every configured marketplace output to a "+
			"temp representation and diff against the effective on-disk path, "+
			"including --marketplace-path overrides. Exits 4 for drift. Use "+
			"with --dry-run to check without normal pack output generation.")
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
	// showZipMigrationNotice mirrors show_zip_migration_notice
	// (commands/pack.py:566-570), computed once in RunE (where
	// cmd.Flags().Changed("archive-format") is available) and threaded
	// through so runBundleProducer's rendering doesn't need cobra flag
	// access itself.
	showZipMigrationNotice bool
	// checkVersions/checkClean mirror --check-versions/--check-clean
	// (ticket 17 phase 4): release gates run by runReleaseGates.
	checkVersions bool
	checkClean    bool
	// jsonOutput mirrors --json (ticket 17 phase 5): the envelope becomes
	// the only thing on stdout and every human line moves to stderr.
	jsonOutput bool
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
	if opts.jsonOutput {
		// Move ux's error/warning/info channel off stdout for the whole
		// invocation, so the envelope is the only thing a consuming
		// pipeline reads there (--json's own help text). Mirrors the
		// Oracle's set_console_stderr; restored on the way out so a
		// long-lived process or a test running several commands in
		// sequence does not inherit it.
		ux.SetConsoleStderr(true)
		defer ux.SetConsoleStderr(false)
	}
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

	// Ticket 17 phase 4: build/write every producer FIRST (including their
	// own inline messages -- a bundle's "No plugin.json found;
	// synthesising..." notice, marketplace's collision/secret warnings,
	// plugin-manifest's own completion line), matching the Oracle's own
	// orchestrator.run() step; the --check-versions/--check-clean release
	// gates run strictly AFTER that (their own, independent config
	// load/resolve, never reusing whatever a producer above already
	// built); each producer's own completion/dry-run TAIL line is deferred
	// past the gates too (renderBundleResult/renderMarketplaceOutput) --
	// verified live against the pinned Oracle, in this exact order,
	// including with plugin-manifest and a failing gate combined.
	var bundleResult *bundle.ProduceResult
	var err error
	if doBundle {
		bundleResult, err = runBundleProducer(cmd, m, apmYMLRoot, hasMarketplace, opts)
		if err != nil {
			return err
		}
	}
	var marketplaceRenders []marketplaceRender
	if doMarketplace {
		marketplaceRenders, err = runMarketplaceProducer(cmd, opts)
		if err != nil {
			return err
		}
	}
	if doPluginManifest {
		w := packLogWriter(cmd, opts)
		if _, err := pluginmanifest.Produce(w, ".", apmYMLRoot, targets, opts.force, opts.dryRun); err != nil {
			return err
		}
	}

	gates, err := runReleaseGates(cmd, opts)
	if err != nil {
		return err
	}

	// The deferred completion/tail lines are SKIPPED ENTIRELY under --json,
	// not merely redirected: the Oracle emits its envelope and `return`s
	// (pack.py:551-556) before ever reaching its own _render_bundle_result/
	// _render_marketplace_result block, so its stderr carries nothing from
	// this stage -- verified live on the pack-json fixture, where the
	// Oracle's stderr is empty while apm-go's initially carried all three
	// bundle lines. "logs go to stderr" in the flag's help covers the
	// producers' own inline CommandLogger messages (which packLogWriter
	// handles), not this result rendering.
	if !opts.jsonOutput {
		w := packLogWriter(cmd, opts)
		if doBundle {
			renderBundleResult(w, bundleResult, opts)
		}
		for _, r := range marketplaceRenders {
			renderMarketplaceOutput(w, r)
		}
		renderMarketplaceCatalog(w, marketplaceRenders)
	}

	// --json: one envelope on stdout, then the gates' own exit codes -- the
	// Oracle emits it and returns BEFORE its non-JSON rendering block, but
	// apm-go's producers print inline as they go (they cannot be deferred
	// wholesale, see runPack's phase-4 comment), so the equivalent here is
	// to have sent every one of those lines to stderr via packLogWriter and
	// emit the envelope at the same point in the sequence.
	if opts.jsonOutput {
		env := packJSONEnvelope{
			OK:               true,
			DryRun:           opts.dryRun,
			Warnings:         []string{},
			Errors:           []packJSONError{},
			Marketplace:      packJSONMarketplace{Outputs: marketplaceOutputsJSON(marketplaceRenders)},
			PluginManifests:  packJSONPluginManifests{Written: []string{}, Skipped: []string{}, DryRun: []string{}},
			VersionAlignment: versionAlignmentJSON(gates.version),
			Drift:            driftJSON(gates.drift),
		}
		// A failed gate is reported IN the envelope as well as through the
		// exit code (pack.py:548-550's gate_errors merge), so a consumer
		// reading only stdout still learns why.
		if gates.versionFailed {
			env.Errors = append(env.Errors, packJSONError{Code: "version_misalignment", Message: "version alignment check failed"})
			env.OK = false
		}
		if gates.driftFailed {
			env.Errors = append(env.Errors, packJSONError{Code: "marketplace_drift", Message: "marketplace working tree dirty"})
			env.OK = false
		}
		if err := emitPackJSON(cmd.OutOrStdout(), env); err != nil {
			return err
		}
	}

	// Gate exit codes, applied after every producer's own output has
	// rendered (pack.py:576-580's own comment: "3 wins over 4") -- verified
	// live that Click's ctx.exit(3) short-circuits before the drift check's
	// own ctx.exit(4) line ever runs when both gates fail simultaneously.
	// Silent: the Oracle's ctx.exit(N) is a bare process exit with no
	// additional message -- runReleaseGates' own renderVersionAlignment/
	// renderDrift calls already printed every detail; a plain withExitCode
	// here would append a redundant "[x] ..." line the Oracle never prints
	// (doctor's own exit-code contract, cmd/apm-go/withSilentExitCode's doc
	// comment, is the same pattern).
	if gates.versionFailed {
		return withSilentExitCode(3, fmt.Errorf("version alignment check failed"))
	}
	if gates.driftFailed {
		return withSilentExitCode(4, fmt.Errorf("marketplace drift check failed"))
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
//
// Ticket 17 phase 4: this function used to also print the "Packed N
// file(s) -> ..." tail (dry-run summary or the real 3-line success set)
// immediately after producing. It no longer does: the Oracle's own
// _render_bundle_result is called strictly AFTER the --check-versions/
// --check-clean release gates run (pack.py:558-566), verified live --
// `orchestrator.run()` (which performs the actual build, including this
// function's own inline messages, e.g. "No plugin.json found;
// synthesising...") completes BEFORE the gates, but the bundle's own
// completion/summary lines print AFTER them. renderBundleResult (below)
// is the deferred half, called from runPack once the gates have already
// printed their own output.
func runBundleProducer(cmd *cobra.Command, m *manifest.Manifest, apmYMLNode *yamllib.Node, hasMarketplaceBlock bool, opts packOptions) (*bundle.ProduceResult, error) {
	w := packLogWriter(cmd, opts)

	hasLocalDep := false
	for _, d := range m.ParsedDeps {
		if d.IsLocal {
			hasLocalDep = true
			break
		}
	}

	lf, lockNode, err := loadPackLockfile()
	if err != nil {
		return nil, err
	}

	deps := bundleDepSources(m, lf)

	pkgVersion := m.Version
	if pkgVersion == "" {
		pkgVersion = "0.0.0"
	}

	outputDir, err := resolvePackOutputDir(opts.output)
	if err != nil {
		return nil, err
	}

	return bundle.Produce(w, bundle.ProduceOptions{
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
}

// renderBundleResult prints runBundleProducer's deferred tail (see that
// function's doc comment for why it's deferred at all): the dry-run
// summary, or the real-run "Packed N file(s) -> ..." 3-line success set.
func renderBundleResult(w io.Writer, result *bundle.ProduceResult, opts packOptions) {
	if opts.dryRun {
		ux.Section(w, fmt.Sprintf("dry-run: Would pack %d file(s) -> %s", len(result.Files), result.BundleDir))
		items := make([]ux.Item, len(result.Files))
		for i, f := range result.Files {
			items[i] = ux.Item{Text: f}
		}
		ux.BulletList(w, items)
		return
	}
	displayDir := displayPath(result.BundleDir)
	ux.Sparkle(w, "Packed %d file(s) -> %s%s", len(result.Files), displayDir, bundleSizeSuffix(result.BundleDir))
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
	// pack.py:566-570/681-687: the zip-migration notice, shown only when
	// --archive was given, the EFFECTIVE format resolved to "zip" (the
	// no-flag default -- opts.showZipMigrationNotice, computed in RunE
	// where cmd.Flags().Changed("archive-format") is available), AND (the
	// Oracle's own SECOND, independent check at render time) the produced
	// bundle path itself ends in ".zip" -- both conditions kept, not
	// simplified into just one.
	if opts.showZipMigrationNotice && strings.HasSuffix(result.BundleDir, ".zip") {
		ux.Info(w, "Note: --archive now produces .zip by default. Use --archive-format tar.gz "+
			"to restore the previous format for legacy pipelines.")
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
}

// bundleSizeSuffix mirrors _bundle_size_suffix (commands/pack.py:617-629):
// a directory bundle (path.is_file() false) gets no suffix at all -- only
// an archive FILE does. Three branches, exact Oracle thresholds/formatting
// (one decimal place, the literal " (" / ")" wrapping): <1024 bytes plain
// integer, <1 MiB in KiB, otherwise in MiB. The absolute byte count will
// generally differ from the Oracle's own archive of logically-equivalent
// content (different DEFLATE encoders, different embedded timestamps
// pre-compression) -- that is a documented, expected residual, not
// something this function tries to normalize away.
func bundleSizeSuffix(path string) string {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	size := info.Size()
	switch {
	case size < 1024:
		return fmt.Sprintf(" (%d bytes)", size)
	case size < 1024*1024:
		return fmt.Sprintf(" (%.1f KiB)", float64(size)/1024)
	default:
		return fmt.Sprintf(" (%.1f MiB)", float64(size)/(1024*1024))
	}
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

// marketplaceRender carries packOneOutput's already-computed (and, unless
// dry-run, already-written) result to renderMarketplaceOutput -- the
// deferred tail-print half, called from runPack after the release gates
// (see runBundleProducer's doc comment for why this split exists at all).
type marketplaceRender struct {
	format     string
	outputPath string
	count      int
	dryRun     bool
	// absPath is outputPath resolved against the project root, and is what
	// EVERY user-facing rendering of this path uses -- the completion line,
	// the catalog row, and the `--json` payload alike. The Oracle has only
	// one path value here: resolve_effective_output_path
	// (marketplace/output_profiles.py:134-136) joins any relative
	// configured path onto the absolute project root before returning, so
	// MarketplaceOutputReport.output_path is absolute at every use site
	// (pack.py:738 success line, pack.py:771 catalog row, builder.py:242
	// JSON). Ticket 13's displayPath relativisation is BUNDLE-only and must
	// not be applied here: the Oracle's bundle_path genuinely is the
	// unresolved user-facing "./build" string, but this one is not. Ticket
	// 28 -- verified live, both lines absolute in the same invocation.
	absPath string
	// diff carries the per-plugin classification `--json`'s
	// marketplace.outputs entry reports (Oracle
	// MarketplaceOutputReport.added_count et al). Computed against the
	// marketplace.json already on disk, BEFORE this run overwrites it.
	diff build.OutputDiff
}

// runMarketplaceProducer builds marketplace.json from apm.yml's
// marketplace: block (or a legacy marketplace.yml), mirroring mkt-054/055's
// original single-producer packCmd body -- unchanged behavior except that
// (ticket 17 phase 4) each output's own completion/dry-run line is no
// longer printed here: it's deferred to renderMarketplaceOutput, called
// from runPack after the --check-versions/--check-clean release gates run
// (verified live: the Oracle's own marketplace success/dry-run lines print
// AFTER the gates too). The "-m none" early return keeps printing inline,
// unchanged -- that's a pre-existing, unrelated convention (mkt-054/055),
// not part of what this phase restructures, and Oracle itself has no
// producer_result at all for a zero-output selection (verified live:
// "-m none" combined with the gates produces no marketplace-related
// output on either side).
func runMarketplaceProducer(cmd *cobra.Command, opts packOptions) ([]marketplaceRender, error) {
	w := packLogWriter(cmd, opts)

	cfg, src, err := authoring.LoadAuthoringConfig(".")
	if err != nil {
		return nil, err
	}
	if err := authoring.ValidateOutputRequirements(cfg); err != nil {
		return nil, withStderrError(err)
	}
	if src == authoring.ConfigSourceLegacy {
		ux.Warn(cmd.ErrOrStderr(), "reading legacy marketplace.yml; run 'apm-go marketplace migrate' to fold it into apm.yml")
	}

	cliOverrides, err := parseMarketplacePathOverrides(opts.pathOverrideArgs)
	if err != nil {
		return nil, err
	}
	filterSet, buildAll, err := parseMarketplaceFilter(opts.marketplaceFilter)
	if err != nil {
		return nil, err
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
		return nil, nil
	}

	resolved, warnings, err := build.ResolvePackages(cfg, build.Options{
		IncludePrerelease: opts.includePrerelease,
		Offline:           opts.offline,
		ProjectRoot:       ".",
	})
	if err != nil {
		return nil, err
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
		return nil, err
	}

	renders := make([]marketplaceRender, 0, len(activeOutputs))
	for _, format := range activeOutputs {
		r, err := packOneOutput(cmd, format, cfg, resolved, configPaths, cliOverrides, opts)
		if err != nil {
			return nil, err
		}
		renders = append(renders, r)
	}
	return renders, nil
}

// packOneOutput composes and (unless dry-run) writes a single output
// profile's marketplace.json, returning what renderMarketplaceOutput needs
// to print its deferred tail line.
func packOneOutput(
	cmd *cobra.Command,
	format string,
	cfg *authoring.AuthoringConfig,
	resolved []build.ResolvedPackage,
	configPaths, cliOverrides map[string]string,
	opts packOptions,
) (marketplaceRender, error) {
	outputPath, err := build.ResolveOutputPath(format, configPaths, cliOverrides)
	if err != nil {
		return marketplaceRender{}, err
	}
	absPath, err := build.EnsureWithinRoot(".", outputPath)
	if err != nil {
		return marketplaceRender{}, err
	}

	doc, docWarnings, err := composeMarketplaceDocument(format, cfg, resolved)
	if err != nil {
		return marketplaceRender{}, err
	}
	for _, warning := range docWarnings {
		ux.Warn(cmd.ErrOrStderr(), "%s", warning)
	}

	// Classified against the file already on disk, before WriteOutput below
	// replaces it -- the Oracle's own ordering (_load_existing_json then
	// _compute_diff, builder.py:1237-1238). Computed unconditionally so a
	// --dry-run --json run still reports what WOULD change.
	render := marketplaceRender{
		format:     format,
		outputPath: outputPath,
		absPath:    absPath,
		count:      len(resolved),
		dryRun:     opts.dryRun,
		diff:       build.ComputeOutputDiff(absPath, doc),
	}
	if opts.dryRun {
		return render, nil
	}

	if err := build.WriteOutput(absPath, doc); err != nil {
		return marketplaceRender{}, err
	}
	return render, nil
}

// renderMarketplaceOutput prints packOneOutput's deferred tail: the
// dry-run notice, or the real-run success line -- mirroring
// _render_marketplace_result's per-output line (pack.py's own
// logger.success(f"Built {message}") uses no symbol override, the same
// "[*]" default as the bundle producer's success line, ux.Sparkle's own
// doc comment).
func renderMarketplaceOutput(w io.Writer, r marketplaceRender) {
	if r.dryRun {
		ux.Info(w, "dry-run: Would write marketplace.json [%s] (%d package(s)) -> %s", r.format, r.count, r.absPath)
		return
	}
	ux.Sparkle(w, "Built marketplace.json [%s] (%d package(s)) -> %s", r.format, r.count, r.absPath)
}

// renderMarketplaceCatalog appends the vendor-neutral artifact catalog the
// Oracle prints after its per-output success lines
// (_render_marketplace_catalog, pack.py:763-777), called from the same
// place its own caller is (_render_marketplace_result's tail,
// pack.py:748-749): only when at least one output was actually WRITTEN and
// the run is not a dry-run, since a dry-run wrote no files to catalogue.
//
// The Oracle's `written` list carries an optional profile per row and
// branches on whether ANY row has one (pack.py:768). apm-go's
// activeOutputs are always named profiles ("claude"/"codex" --
// build.KnownOutputFormats rejects everything else long before this), so
// only the tagged branch is reachable here; the Oracle's untagged fallback
// exists for its `outputs`-without-`output_reports` path (pack.py:729-736),
// which apm-go has no equivalent of. Tag width is the widest profile name,
// left-justified, matching Python's str.ljust.
func renderMarketplaceCatalog(w io.Writer, renders []marketplaceRender) {
	written := make([]marketplaceRender, 0, len(renders))
	for _, r := range renders {
		if !r.dryRun {
			written = append(written, r)
		}
	}
	if len(written) == 0 {
		return
	}

	labelWidth := 0
	for _, r := range written {
		if len(r.format) > labelWidth {
			labelWidth = len(r.format)
		}
	}

	ux.Info(w, "Marketplace artifacts ready:")
	for _, r := range written {
		ux.Info(w, "  [%-*s] %s", labelWidth, r.format, r.absPath)
	}
	ux.Info(w, "How consumers install from this marketplace varies by AI assistant.")
	ux.Info(w, "See: %s", marketplaceDocsURL)
}

// composeMarketplaceDocument dispatches to the mkt-050/052/053 mapper for
// format ("claude" or "codex" -- parseMarketplaceFilter/
// build.KnownOutputFormats already reject anything else before this is ever
// reached). A thin wrapper around build.ComposeDocument (ticket 17 phase 4
// exported it so the check-clean drift gate shares this exact dispatch
// instead of a second copy).
func composeMarketplaceDocument(format string, cfg *authoring.AuthoringConfig, resolved []build.ResolvedPackage) (any, []string, error) {
	return build.ComposeDocument(format, cfg, resolved)
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

// runReleaseGates mirrors pack.py's "-- Release gates (--check-versions /
// --check-clean) --" block (pack.py:396-527): runs whichever of the two
// gates the caller requested against their OWN, independently loaded
// marketplace config -- never reusing whatever runMarketplaceProducer
// already built this same invocation, matching the Oracle's own dedicated
// (always-dry-run, for --check-clean) MarketplaceBuilder. Returns whether
// each gate failed; runPack applies the exit-3/exit-4 precedence after
// rendering every producer's own output, matching the Oracle's own
// ordering (verified live).
func runReleaseGates(cmd *cobra.Command, opts packOptions) (gates packGateResults, err error) {
	if !opts.checkVersions && !opts.checkClean {
		return gates, nil
	}
	w := packLogWriter(cmd, opts)

	cfg, src, loadErr := authoring.LoadAuthoringConfig(".")
	if loadErr != nil {
		if !authoring.IsNoConfigError(loadErr) {
			return gates, loadErr
		}
		// gate_config is None (pack.py:427-436): skip both requested gates
		// with an [i] info line each, never an error -- a dependencies:-only
		// project asking for a release gate that only makes sense with a
		// marketplace: block is a no-op, not a failure.
		if opts.checkVersions {
			ux.Info(w, "Version alignment check skipped: no marketplace block; nothing to check.")
		}
		if opts.checkClean {
			ux.Info(w, "Marketplace drift check skipped: no marketplace block; nothing to check.")
		}
		return gates, nil
	}
	if opts.checkVersions {
		report := build.CheckVersionAlignment(cfg, ".")
		gates.version = &report
		renderVersionAlignment(w, report)
		if !report.OK {
			gates.versionFailed = true
		}
	}

	if opts.checkClean {
		cliOverrides, perr := parseMarketplacePathOverrides(opts.pathOverrideArgs)
		if perr != nil {
			return gates, perr
		}
		configPaths, perr := build.LoadOutputPathOverrides(".", src)
		if perr != nil {
			return gates, perr
		}
		// The drift gate always resolves packages fresh, independent of
		// whether the marketplace producer also ran this invocation --
		// mirrors the Oracle's own dedicated MarketplaceBuilder.from_config
		// (drift_check.py's own doc comment: "the gate writes nothing").
		resolved, warnings, rerr := build.ResolvePackages(cfg, build.Options{
			IncludePrerelease: opts.includePrerelease,
			Offline:           opts.offline,
			ProjectRoot:       ".",
		})
		if rerr != nil {
			return gates, rerr
		}
		for _, warning := range warnings {
			ux.Warn(cmd.ErrOrStderr(), "%s", warning)
		}
		report, derr := build.CheckMarketplaceDrift(cfg, resolved, ".", configPaths, cliOverrides)
		if derr != nil {
			return gates, derr
		}
		gates.drift = &report
		renderDrift(w, report, cliOverrides)
		if !report.OK {
			gates.driftFailed = true
		}
	}

	return gates, nil
}

// renderVersionAlignment mirrors the check_versions half of pack.py's gate
// block (pack.py:439-465): the header line (success or error, with or
// without an "expected=" clause depending on strategy), one detail row per
// package, and (failure only) one error line per ErrorMessages() entry.
func renderVersionAlignment(w io.Writer, report build.VersionAlignmentReport) {
	if report.OK {
		if report.Expected != "" {
			ux.Sparkle(w, "Version alignment OK [strategy=%s, expected=%s]", report.Strategy, report.Expected)
		} else {
			ux.Sparkle(w, "Version alignment OK [strategy=%s]", report.Strategy)
		}
		for _, row := range report.Packages {
			ux.Info(w, "    %s  %s%s  [%s]", row.Path, row.Version, tagSuffix(row.RenderedTag), row.Reason)
		}
		return
	}

	if report.Expected != "" {
		ux.Error(w, "Version alignment failed [strategy=%s, expected=%s]", report.Strategy, report.Expected)
	} else {
		ux.Error(w, "Version alignment failed [strategy=%s]", report.Strategy)
	}
	for _, row := range report.Packages {
		versionStr := row.Version
		if versionStr == "" {
			versionStr = "<none>"
		}
		ux.Info(w, "    %s  %s%s  [%s]", row.Path, versionStr, tagSuffix(row.RenderedTag), row.Reason)
	}
	for _, msg := range report.ErrorMessages() {
		ux.Error(w, "    %s", msg)
	}
}

// tagSuffix mirrors pack.py's `tag_str = f"  -> tag {row.rendered_tag}" if
// row.rendered_tag else ""` (repeated at pack.py:449/463).
func tagSuffix(tag string) string {
	if tag == "" {
		return ""
	}
	return "  -> tag " + tag
}

// renderDrift mirrors the check_clean half of pack.py's gate block
// (pack.py:479-527): the header line (success or error, naming only the
// non-unchanged outputs on failure), one detail row per output (plus a
// recovery recipe for "missing"/"drift" rows), matching the Oracle's own
// per-status branching exactly.
func renderDrift(w io.Writer, report build.DriftReport, outputOverrides map[string]string) {
	if report.OK {
		formats := make([]string, len(report.Outputs))
		for i, out := range report.Outputs {
			formats[i] = out.Format
		}
		ux.Sparkle(w, "Marketplace working tree clean [outputs=%s]", strings.Join(formats, ", "))
		for _, out := range report.Outputs {
			ux.Info(w, "    %s  [unchanged]", out.Path)
		}
		return
	}

	var dirtyFormats []string
	for _, out := range report.Outputs {
		if out.Status != "unchanged" {
			dirtyFormats = append(dirtyFormats, out.Format)
		}
	}
	ux.Error(w, "Marketplace working tree dirty [outputs=%s]", strings.Join(dirtyFormats, ", "))
	for _, out := range report.Outputs {
		switch out.Status {
		case "unchanged":
			ux.Info(w, "    %s  [unchanged]", out.Path)
		case "missing":
			ux.Info(w, "    %s  [missing on disk; would be created]", out.Path)
			emitDriftRecipe(w, out.Path, out.Format, outputOverrides[out.Format])
		default: // "drift"
			ux.Info(w, "    %s  [drift: %d differences]", out.Path, len(out.Differences))
			for _, line := range build.RenderDiffLines(out, 20) {
				ux.Info(w, "%s", line)
			}
			emitDriftRecipe(w, out.Path, out.Format, outputOverrides[out.Format])
		}
	}
}

// emitDriftRecipe mirrors _emit_drift_recipe (pack.py:583-613) verbatim,
// including its exact literal spacing -- "apm-go" in place of "apm" (this
// binary's own name, matching every other self-referential hint text in
// this command, e.g. "Share with: apm-go install ..."; rewrite_binary_name
// folds it back for parity comparison). outputOverride is "" unless the
// caller passed --marketplace-path FORMAT=PATH for this exact format.
func emitDriftRecipe(w io.Writer, outPath, outputFormat, outputOverride string) {
	packCommand := "apm-go pack"
	if outputOverride != "" {
		packCommand += fmt.Sprintf(" --marketplace-path %s=%s", outputFormat, outputOverride)
	}
	stageCommand := "git add -- " + outPath

	ux.Info(w, "")
	ux.Info(w, "    To recover cleanly (fold into the current commit):")
	ux.Info(w, "")
	ux.Info(w, "      %s                       # regenerate locally", packCommand)
	ux.Info(w, "      %s", stageCommand)
	ux.Info(w, "      git commit --amend --no-edit   # fold into the current commit")
	ux.Info(w, "      git push --force-with-lease    # safe re-push")
	ux.Info(w, "")
	ux.Info(w, "    Or as a follow-up commit:")
	ux.Info(w, "")
	ux.Info(w, "      %s && %s", packCommand, stageCommand)
	ux.Info(w, "      git commit -m 'chore(marketplace): regen'")
	ux.Info(w, "")
	ux.Info(w, "    Why this exists: marketplace.json is checked in (lockfile pattern)")
	ux.Info(w, "    so consumers can resolve packages without running 'apm pack'. CI")
	ux.Info(w, "    enforces that the checked-in copy matches the apm.yml source of truth.")
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
