package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/marketplace/authoring"
	"github.com/apm-go/apm/internal/marketplace/build"
	"github.com/apm-go/apm/internal/yamlcore"
	"github.com/spf13/cobra"
)

// packCmd implements mkt-054/055's `apm pack`: read apm.yml's `marketplace:`
// block (or a legacy marketplace.yml, mkt-047), resolve every
// marketplace.packages[] entry (internal/marketplace/build.ResolvePackages,
// mkt-051), compose each configured output profile's marketplace.json
// (build.ClaudeMapper/build.CodexMapper, mkt-050/052/053), and atomically
// write each to its resolved location (build.WriteOutput, mkt-054).
//
// This sub-task's scope is marketplace.json generation only (design.md's
// "範圍界定") -- the Python original's plugin-bundling half of `apm pack`
// (--format/--archive/-o etc.) is out of scope, and a project with no
// `marketplace:` block at all is not an error: pack prints a message and
// exits 0 rather than reporting a missing feature as a failure.
//
// Exit codes (mkt-055, corrected against the Python original's actual
// runtime behavior rather than its help text): 0 success, 1 for every
// marketplace config/build error (ls-remote failure, NoMatchingVersionError,
// HeadNotAllowedError, a missing Codex category, a CLI usage error). 2/3/4
// (--check-versions/--check-clean's gates) are not implemented in this
// sub-task -- their flags do not exist at all, rather than shipping an inert
// placeholder.
func packCmd() *cobra.Command {
	var (
		offline           bool
		includePrerelease bool
		dryRun            bool
		marketplaceFilter string
		pathOverrideArgs  []string
		verbose           bool
	)

	cmd := &cobra.Command{
		Use:          "pack",
		Short:        "Generate marketplace.json from apm.yml's 'marketplace:' block",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPack(cmd, packOptions{
				offline:           offline,
				includePrerelease: includePrerelease,
				dryRun:            dryRun,
				marketplaceFilter: marketplaceFilter,
				pathOverrideArgs:  pathOverrideArgs,
				verbose:           verbose,
			})
		},
	}

	cmd.Flags().BoolVar(&offline, "offline", false, "use cached refs only (no network); fails packages with a pinned ref/version instead of silently degrading")
	cmd.Flags().BoolVar(&includePrerelease, "include-prerelease", false, "include prerelease versions when resolving semver ranges")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be written without writing")
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
	marketplaceFilter string
	pathOverrideArgs  []string
	verbose           bool
}

func runPack(cmd *cobra.Command, opts packOptions) error {
	w := cmd.OutOrStdout()

	if !hasMarketplaceConfig(".") {
		fmt.Fprintln(w, "[i] No 'marketplace:' block found (neither apm.yml's marketplace: block nor a legacy marketplace.yml exist); nothing to do.")
		return nil
	}

	cfg, src, err := authoring.LoadAuthoringConfig(".")
	if err != nil {
		return err
	}
	if src == authoring.ConfigSourceLegacy {
		fmt.Fprintln(cmd.ErrOrStderr(), "[warn] reading legacy marketplace.yml; run 'apm marketplace migrate' to fold it into apm.yml")
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
		fmt.Fprintln(w, "[i] No marketplace outputs selected; nothing to write.")
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
		fmt.Fprintf(cmd.ErrOrStderr(), "[warn] %s\n", warning)
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
		fmt.Fprintf(cmd.ErrOrStderr(), "[warn] %s\n", warning)
	}

	if opts.dryRun {
		fmt.Fprintf(w, "[i] Would write marketplace.json [%s] (%d package(s)) -> %s\n", format, len(resolved), outputPath)
		return nil
	}

	if err := build.WriteOutput(absPath, doc); err != nil {
		return err
	}
	fmt.Fprintf(w, "[+] Built marketplace.json [%s] (%d package(s)) -> %s\n", format, len(resolved), outputPath)
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
// marketplace.yml file -- pack's "no marketplace: block -> print message,
// exit 0" case (design.md) needs to tell this apart from every other
// authoring.LoadAuthoringConfig error, which must propagate as a real exit-1
// failure instead.
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
