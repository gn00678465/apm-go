package main

import (
	"fmt"
	"strings"

	"github.com/apm-go/apm/internal/marketplace/authoring"
	"github.com/apm-go/apm/internal/ux"
	"github.com/spf13/cobra"
)

// marketplacePackageCmd implements mkt-045/046: `apm marketplace package
// add/remove/set`, editing the packages: sequence inside the active
// marketplace authoring config (mkt-047's apm.yml marketplace: block, or a
// legacy standalone marketplace.yml). Every subcommand's non-guard error
// path exits 2 (via withExitCode), not main()'s default 1 -- mkt-045's
// "package 子指令錯誤路徑 exit code 為 2"; the one exception is remove's
// non-interactive confirmation guard, which exits 1 like every other
// `apm marketplace *` confirmation guard (mkt-015's own remove).
func marketplacePackageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "package",
		Short: "Manage packages in the marketplace authoring config",
	}
	cmd.AddCommand(marketplacePackageAddCmd())
	cmd.AddCommand(marketplacePackageSetCmd())
	cmd.AddCommand(marketplacePackageRemoveCmd())
	return cmd
}

// errVersionRefMutuallyExclusive is mkt-045's --version/--ref guard,
// checked at both the command layer (add/set's RunE, via
// cmd.Flags().Changed -- so it fires before any I/O) and the editor layer
// (authoring.AddPackage/SetPackage) for defense in depth; the two layers
// share this exact message so the guard reads identically no matter which
// one catches it.
var errVersionRefMutuallyExclusive = fmt.Errorf("--version and --ref are mutually exclusive; use --version for a semver range or --ref for a git ref")

// errNoSetFieldsSpecified is C2's fix: `package set NAME` with none of its
// field flags given used to silently no-op-rewrite the entry and exit 0;
// Python (set.py:98-103) treats this as a user error. This is the exact
// message text Python uses. Exit code 1 (not mkt-045's usual 2 for an edit
// failure) matches Python's sys.exit(1) here -- this is the cmd layer's own
// guard, not an authoring.SetPackage failure that would otherwise be
// wrapped via withExitCode(2).
var errNoSetFieldsSpecified = fmt.Errorf("No fields specified. Pass at least one option (e.g. --version, --ref, --subdir).")

// setFieldFlags is `package set`'s complete set of field-editing flags
// (mkt-045); C2's guard requires at least one of these to have been given.
var setFieldFlags = []string{"version", "ref", "subdir", "tag-pattern", "tags", "include-prerelease"}

func anySetFieldFlagChanged(cmd *cobra.Command) bool {
	for _, name := range setFieldFlags {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

// shortSHA truncates a commit SHA to upstream's 12-character display form
// (plugin/__init__.py:148's sha[:12]) for the "Resolved <ref> to <sha>"
// progress line.
func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// parseTagsFlag splits a comma-separated --tags value into a trimmed,
// non-empty slice, or nil when raw is empty -- mirrors Python's
// _parse_tags. Used by `add`, where an omitted --tags must leave
// AddOptions.Tags nil (add always creates a brand new entry, so there is
// no existing value to distinguish "not given" from "given empty").
func parseTagsFlag(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for _, t := range strings.Split(raw, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// parseTagsFlagGiven is `set`'s variant of parseTagsFlag: it always
// returns a non-nil slice (possibly empty), because `set` uses
// SetOptions.Tags == nil to mean "flag not given, leave existing tags
// alone" (this function is only ever called after cmd.Flags().Changed
// confirms --tags was given at all).
func parseTagsFlagGiven(raw string) []string {
	if parsed := parseTagsFlag(raw); parsed != nil {
		return parsed
	}
	return []string{}
}

// marketplaceOutputsIncludeCodex reports whether dir's active marketplace
// authoring config declares a "codex" entry in its `outputs:` block. It is
// a best-effort, read-only check (R10's add-time codex-category warning):
// a config that fails to load here (e.g. `marketplace init` was never run)
// simply reports false, exactly like marketplaceNotRegisteredErr's own
// best-effort registry lookup.
func marketplaceOutputsIncludeCodex(dir string) bool {
	cfg, _, err := authoring.LoadAuthoringConfig(dir)
	if err != nil {
		return false
	}
	for _, o := range cfg.Outputs {
		if strings.EqualFold(o, "codex") {
			return true
		}
	}
	return false
}

// marketplacePackageAddCmd implements `apm marketplace package add SOURCE`
// (mkt-045/046). --name and -s/--subdir's shorthand, --no-verify and
// --category are add-only, per design.md's flag table.
func marketplacePackageAddCmd() *cobra.Command {
	var (
		name, version, ref, subdir, tagPattern, tags, category string
		includePrerelease, noVerify, verbose                   bool
	)

	cmd := &cobra.Command{
		Use:   "add SOURCE",
		Short: "Add a package to the marketplace authoring config",
		Long: "Add a package to the marketplace authoring config. SOURCE accepts " +
			"owner/repo, host.tld/owner/repo, https://host.tld/owner/repo " +
			"(nested paths allowed), or a ./local path.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("version") && cmd.Flags().Changed("ref") {
				return withExitCode(2, errVersionRefMutuallyExclusive)
			}
			// R10/AC48: outputs: codex without --category would otherwise
			// leave the added package unable to ever `pack` a codex
			// output (mkt-053's CategoryRequiredError) with no CLI-level
			// way to fix it -- design.md §13's "刻意不做" keeps this a
			// warning, not a block, so `pack -m claude` still succeeds
			// regardless.
			warnMissingCategory := category == "" && marketplaceOutputsIncludeCodex(".")
			opts := authoring.AddOptions{
				Name:              name,
				Version:           version,
				Ref:               ref,
				Subdir:            subdir,
				TagPattern:        tagPattern,
				Tags:              parseTagsFlag(tags),
				IncludePrerelease: includePrerelease,
				NoVerify:          noVerify,
				Category:          category,
				// R5/AC19: an explicit `--ref HEAD` (any case) additionally
				// warns that HEAD is a mutable ref, printed right before
				// resolution is actually attempted -- mirroring upstream's
				// plugin/__init__.py _resolve_ref ordering. resolveRef only
				// invokes this hook once every other AddPackage pre-flight
				// step has already passed and classifyRefResolution has
				// confirmed noVerify does not block resolution (see
				// resolveRef's own doc comment).
				//
				// BLOCKING 2 (external audit round 3, 2026-07-30): this used
				// to be decided by calling authoring.WillResolveMutableRefForAdd
				// BEFORE ever invoking AddPackage at all, so it printed even
				// when AddPackage was about to fail outright for an
				// unrelated reason (a missing config, an unreachable
				// source, a duplicate name), or when --no-verify made HEAD
				// resolution impossible (reproduced live: `add owner/repo
				// --ref HEAD --no-verify` against a directory with no
				// marketplace config printed the warning, then exited 2 on
				// "no marketplace authoring config found"). Wiring the
				// warning through this hook instead avoids that -- for
				// every pre-flight check AddPackage currently runs before
				// its resolveRef call (see authoring/editor.go's AddPackage
				// body) -- without hand-duplicating AddPackage's pre-flight
				// order here. See
				// TestMarketplacePackageAdd_ExplicitRefHead_NoVerify_NoMutableRefWarning_ExitsCode2/
				// _MissingConfig_/_UnreachableSource_/_DuplicateName_NoMutableRefWarning
				// (marketplace_package_test.go) for the four pre-flight
				// failures this is regression-tested against.
				OnExplicitHeadWillResolve: func() {
					ux.Warn(cmd.ErrOrStderr(), "'HEAD' is a mutable ref. Resolving to current SHA for safety.")
				},
				// Upstream plugin/__init__.py:147-150/179-182: report what
				// SHA the mutable/named ref actually resolved to, so the
				// user learns what got written into apm.yml.
				OnRefResolved: func(ref, sha string) {
					ux.Info(cmd.OutOrStdout(), "Resolved %s to %s", ref, shortSHA(sha))
				},
			}
			resolved, fallbackUsed, err := authoring.AddPackage(".", args[0], opts, authoring.DefaultRefLister)
			if err != nil {
				return withExitCode(2, err)
			}
			if fallbackUsed {
				ux.Warn(cmd.ErrOrStderr(), "packages: block structure required rewriting the whole list; hand formatting on other entries may have changed")
			}
			if warnMissingCategory {
				ux.Warn(cmd.ErrOrStderr(), "package %q has no --category; marketplace.outputs includes 'codex', which requires one at `pack` time", resolved)
			}
			ux.Success(cmd.OutOrStdout(), "Added package %q from %s", resolved, args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Package name (default: derived from SOURCE)")
	cmd.Flags().StringVar(&version, "version", "", "Semver range (e.g. '>=1.0.0')")
	cmd.Flags().StringVar(&ref, "ref", "", "Pin to a git ref")
	cmd.Flags().StringVarP(&subdir, "subdir", "s", "", "Subdirectory inside the source repo")
	cmd.Flags().StringVar(&tagPattern, "tag-pattern", "", "Tag pattern (e.g. 'v{version}')")
	cmd.Flags().StringVar(&tags, "tags", "", "Comma-separated tags")
	cmd.Flags().BoolVar(&includePrerelease, "include-prerelease", false, "Include prerelease versions")
	cmd.Flags().BoolVar(&noVerify, "no-verify", false, "Skip the remote reachability check")
	// B-MINOR-2 (external audit round 8, 2026-07-31 follow-up): a backtick
	// pair anywhere in a pflag usage string is not just decoration --
	// pflag's UnquoteUsage treats the FIRST backtick-quoted substring as the
	// flag's help metavar override, replacing the default "string" type name
	// shown in `--help` output. The original "at `pack` time" wording made
	// `--help` print "--category pack" (implying pack takes a literal
	// argument named "pack") instead of "--category string". Single quotes
	// (the convention already used by --version/--tag-pattern above) avoid
	// triggering that behavior.
	cmd.Flags().StringVar(&category, "category", "", "Package category (required for Codex output at 'pack' time)")
	// C1: doc's marketplace.md:283-285 promises --verbose/-v on every
	// subcommand; `package add` was missing it entirely (an unknown-flag
	// hard error). Python's own add.py accepts it with no observable
	// effect on the success path (only feeds an internal logger's verbosity
	// level, never actually consulted there) -- mirrored here as-is.
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed output")
	return cmd
}

// marketplacePackageSetCmd implements `apm marketplace package set NAME`
// (mkt-045). Unlike add, every flag here is tri-state via
// cmd.Flags().Changed: an unset flag must leave the existing field alone,
// not overwrite it with a zero value -- design.md calls this out
// explicitly for --include-prerelease, but the same "only touch what was
// given" contract applies to every field SetOptions carries.
func marketplacePackageSetCmd() *cobra.Command {
	var (
		version, ref, subdir, tagPattern, tags string
		includePrerelease, verbose             bool
	)

	cmd := &cobra.Command{
		Use:          "set NAME",
		Short:        "Update a package entry in the marketplace authoring config",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("version") && cmd.Flags().Changed("ref") {
				return withExitCode(2, errVersionRefMutuallyExclusive)
			}
			// C2: zero field flags used to silently no-op-rewrite the entry
			// and exit 0; Python (set.py:98-103) exits 1 instead. Checked
			// before any I/O, same as the mutual-exclusion guard above.
			if !anySetFieldFlagChanged(cmd) {
				return errNoSetFieldsSpecified
			}
			var opts authoring.SetOptions
			if cmd.Flags().Changed("version") {
				opts.Version = &version
			}
			if cmd.Flags().Changed("ref") {
				opts.Ref = &ref
			}
			if cmd.Flags().Changed("subdir") {
				opts.Subdir = &subdir
			}
			if cmd.Flags().Changed("tag-pattern") {
				opts.TagPattern = &tagPattern
			}
			if cmd.Flags().Changed("tags") {
				opts.Tags = parseTagsFlagGiven(tags)
			}
			if cmd.Flags().Changed("include-prerelease") {
				opts.IncludePrerelease = &includePrerelease
			}
			// BLOCKING 3 (external audit round 4, 2026-07-30): upstream
			// warns on `set --ref HEAD` too (commands/marketplace/plugin/
			// set.py:80 calls the same _resolve_ref plugin/__init__.py:
			// 120-137 warns from), but SetPackage used to hardcode nil for
			// this hook, so `set` never printed the warning at all. Wired
			// identically to `add`'s own hook above: resolveRef only invokes
			// it immediately before the real lister.ListRefs call for an
			// explicitly-given "HEAD"/"head" ref, once noVerify (`set` has no
			// --no-verify escape hatch, so this never applies) and the
			// mutual-exclusion/no-op guards above have already passed.
			opts.OnExplicitHeadWillResolve = func() {
				ux.Warn(cmd.ErrOrStderr(), "'HEAD' is a mutable ref. Resolving to current SHA for safety.")
			}
			// Mirrors `add`'s OnRefResolved wiring above (upstream's set.py
			// resolves through the same _resolve_ref and prints the same
			// "Resolved <ref> to <sha12>" progress line).
			opts.OnRefResolved = func(ref, sha string) {
				ux.Info(cmd.OutOrStdout(), "Resolved %s to %s", ref, shortSHA(sha))
			}

			fallbackUsed, err := authoring.SetPackage(".", args[0], opts, authoring.DefaultRefLister)
			if err != nil {
				return withExitCode(2, err)
			}
			if fallbackUsed {
				ux.Warn(cmd.ErrOrStderr(), "packages: block structure required rewriting the whole list; hand formatting on other entries may have changed")
			}
			ux.Success(cmd.OutOrStdout(), "Updated package %q", args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&version, "version", "", "Semver range (e.g. '>=1.0.0')")
	cmd.Flags().StringVar(&ref, "ref", "", "Pin to a git ref")
	cmd.Flags().StringVar(&subdir, "subdir", "", "Subdirectory inside the source repo")
	cmd.Flags().StringVar(&tagPattern, "tag-pattern", "", "Tag pattern (e.g. 'v{version}')")
	cmd.Flags().StringVar(&tags, "tags", "", "Comma-separated tags")
	cmd.Flags().BoolVar(&includePrerelease, "include-prerelease", false, "Include prerelease versions")
	// C1: doc's marketplace.md:283-285 promises --verbose/-v on every
	// subcommand; `package set` was missing it entirely (an unknown-flag
	// hard error). Python's own set.py accepts it with no observable effect
	// on the success path -- mirrored here as-is.
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed output")
	return cmd
}

// marketplacePackageRemoveCmd implements `apm marketplace package remove
// NAME` (mkt-045): -y/--yes skips confirmation entirely; otherwise a
// genuinely interactive session is prompted via confirmOrRequireYes
// (ux.Confirm, shared with mkt-015's own `marketplace remove`), and a
// non-interactive session without -y is a hard error -- exit 1, mkt-045's
// one exit-code exception, not the 2 every other package edit failure uses.
func marketplacePackageRemoveCmd() *cobra.Command {
	var yes, verbose bool

	cmd := &cobra.Command{
		Use:          "remove NAME",
		Short:        "Remove a package from the marketplace authoring config",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !yes {
				// C10: confirmOrRequireYes (marketplace.go, shared with
				// mkt-015's own `marketplace remove`) ensures a failed
				// confirmation read (EOF, or any other scanner error) is
				// never conflated with "user declined" -- it requires
				// -y/--yes instead, the same as an outright non-interactive
				// session.
				proceed, err := confirmOrRequireYes(
					fmt.Sprintf("Remove package %q from the marketplace authoring config?", name),
					"marketplace package remove requires -y/--yes in a non-interactive environment",
				)
				if err != nil {
					return err
				}
				if !proceed {
					ux.Info(cmd.ErrOrStderr(), "Cancelled")
					return nil
				}
			}
			if _, err := authoring.RemovePackage(".", name); err != nil {
				return withExitCode(2, err)
			}
			ux.Success(cmd.OutOrStdout(), "Removed package %q", name)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the interactive confirmation prompt")
	// C1: doc's marketplace.md:283-285 promises --verbose/-v on every
	// subcommand; `package remove` was missing it entirely (an
	// unknown-flag hard error). Python's own remove.py accepts it with no
	// observable effect on the success path -- mirrored here as-is.
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed output")
	return cmd
}
