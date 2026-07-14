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
	)

	cmd := &cobra.Command{
		Use:   "pack",
		Short: "Build marketplace.json, a plugin bundle, and/or a standalone plugin.json from apm.yml",
		Long: `Build whichever of apm.yml's three artifacts apply -- any subset may fire
in the same invocation:

  - marketplace.json, from a 'marketplace:' block (or a legacy
    marketplace.yml) -- written under .claude-plugin/ and/or .agents/plugins/.
  - a plugin-native bundle, from a 'dependencies:' block -- written under
    ./build/<name>-<version>/, containing plugin.json plus plugin-native
    directories (agents/, skills/, commands/, ...) and an embedded
    apm.lock.yaml for install-time integrity verification.
  - a standalone plugin.json, from 'target:'/'targets:' containing "claude"
    and/or "copilot" -- written at .claude-plugin/plugin.json and/or
    .github/plugin/plugin.json.

If none of the three apply, pack fails (exit 1) instead of silently doing
nothing.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPack(cmd, packOptions{
				offline:           offline,
				includePrerelease: includePrerelease,
				dryRun:            dryRun,
				force:             force,
				marketplaceFilter: marketplaceFilter,
				pathOverrideArgs:  pathOverrideArgs,
				verbose:           verbose,
			})
		},
	}

	cmd.Flags().BoolVar(&offline, "offline", false, "use cached refs only (no network); fails packages with a pinned ref/version instead of silently degrading")
	cmd.Flags().BoolVar(&includePrerelease, "include-prerelease", false, "include prerelease versions when resolving semver ranges")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be written without writing")
	cmd.Flags().BoolVar(&force, "force", false, "bundle producer: last writer wins on file_map collisions and overwrite an existing plugin.json; has no effect on the hidden-character scan, which never blocks")
	cmd.Flags().StringVarP(&marketplaceFilter, "marketplace", "m", "", "comma-separated marketplace outputs to build (e.g. 'claude,codex'); 'all' (default) builds every configured output, 'none' skips marketplace entirely")
	cmd.Flags().StringArrayVar(&pathOverrideArgs, "marketplace-path", nil, "override the output path for a format: FORMAT=PATH (repeatable)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print extra diagnostics")
	return cmd
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
		hasDeps = len(m.ParsedDeps) > 0
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

	result, err := bundle.Produce(w, bundle.ProduceOptions{
		ProjectRoot:                   ".",
		OutputDir:                     filepath.Join(".", "build"),
		PkgName:                       m.Name,
		PkgVersion:                    pkgVersion,
		Target:                        "all",
		Force:                         opts.force,
		DryRun:                        opts.dryRun,
		HasLocalDep:                   hasLocalDep,
		Deps:                          deps,
		ApmYMLNode:                    apmYMLNode,
		SuppressMissingPluginJSONInfo: hasMarketplaceBlock,
		Lockfile:                      lf,
		LockfileNode:                  lockNode,
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
	ux.Success(w, "Packed %d file(s) -> %s", len(result.Files), result.BundleDir)
	fmt.Fprintln(w, "Plugin bundle ready -- contains plugin.json plus plugin-native directories "+
		"(agents/, skills/, commands/, ...) and an embedded apm.lock.yaml for install-time "+
		"integrity verification.")
	fmt.Fprintf(w, "Share with: apm-go install %s\n", result.BundleDir)
	return nil
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
		for _, pkg := range resolved {
			fmt.Fprintf(w, "    %s\n", pkg.Entry.Name)
		}
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
	ux.Success(w, "Built marketplace.json [%s] (%d package(s)) -> %s", format, len(resolved), outputPath)
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
