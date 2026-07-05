package main

import (
	"fmt"
	"strings"

	"github.com/apm-go/apm/internal/marketplace/authoring"
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

// marketplacePackageAddCmd implements `apm marketplace package add SOURCE`
// (mkt-045/046). --name and -s/--subdir's shorthand and --no-verify are
// add-only, per design.md's flag table.
func marketplacePackageAddCmd() *cobra.Command {
	var (
		name, version, ref, subdir, tagPattern, tags string
		includePrerelease, noVerify                  bool
	)

	cmd := &cobra.Command{
		Use:          "add SOURCE",
		Short:        "Add a package to the marketplace authoring config",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("version") && cmd.Flags().Changed("ref") {
				return withExitCode(2, errVersionRefMutuallyExclusive)
			}
			opts := authoring.AddOptions{
				Name:              name,
				Version:           version,
				Ref:               ref,
				Subdir:            subdir,
				TagPattern:        tagPattern,
				Tags:              parseTagsFlag(tags),
				IncludePrerelease: includePrerelease,
				NoVerify:          noVerify,
			}
			resolved, fallbackUsed, err := authoring.AddPackage(".", args[0], opts, authoring.DefaultRefLister)
			if err != nil {
				return withExitCode(2, err)
			}
			if fallbackUsed {
				fmt.Fprintln(cmd.ErrOrStderr(), "[warn] packages: block structure required rewriting the whole list; hand formatting on other entries may have changed")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "[+] Added package %q from %s\n", resolved, args[0])
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
		includePrerelease                      bool
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

			fallbackUsed, err := authoring.SetPackage(".", args[0], opts)
			if err != nil {
				return withExitCode(2, err)
			}
			if fallbackUsed {
				fmt.Fprintln(cmd.ErrOrStderr(), "[warn] packages: block structure required rewriting the whole list; hand formatting on other entries may have changed")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "[+] Updated package %q\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&version, "version", "", "Semver range (e.g. '>=1.0.0')")
	cmd.Flags().StringVar(&ref, "ref", "", "Pin to a git ref")
	cmd.Flags().StringVar(&subdir, "subdir", "", "Subdirectory inside the source repo")
	cmd.Flags().StringVar(&tagPattern, "tag-pattern", "", "Tag pattern (e.g. 'v{version}')")
	cmd.Flags().StringVar(&tags, "tags", "", "Comma-separated tags")
	cmd.Flags().BoolVar(&includePrerelease, "include-prerelease", false, "Include prerelease versions")
	return cmd
}

// marketplacePackageRemoveCmd implements `apm marketplace package remove
// NAME` (mkt-045): -y/--yes skips confirmation entirely; otherwise an
// interactive terminal is prompted (isInteractive/confirmPrompt, shared
// with mkt-015's own `marketplace remove` and init.go's confirmation
// flow), and a non-interactive session without -y is a hard error -- exit
// 1, mkt-045's one exit-code exception, not the 2 every other package
// edit failure uses.
func marketplacePackageRemoveCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:          "remove NAME",
		Short:        "Remove a package from the marketplace authoring config",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !yes {
				if !isInteractive() {
					return fmt.Errorf("marketplace package remove requires -y/--yes in a non-interactive environment")
				}
				if !confirmPrompt(fmt.Sprintf("Remove package %q from the marketplace authoring config?", name), false) {
					fmt.Fprintln(cmd.ErrOrStderr(), "Aborted.")
					return nil
				}
			}
			if _, err := authoring.RemovePackage(".", name); err != nil {
				return withExitCode(2, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "[-] Removed package %q\n", name)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the interactive confirmation prompt")
	return cmd
}
