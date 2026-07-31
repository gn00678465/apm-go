package main

import "github.com/spf13/cobra"

// pluginCmd is the `apm-go plugin` command group (R3.1). Upstream has
// exactly one subcommand (commands/plugin/__init__.py:16-21), so this group
// intentionally has exactly one child (AC30).
func pluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Commands for authoring APM plugins",
	}
	cmd.AddCommand(pluginInitCmd())
	return cmd
}

// pluginInitCmd is `apm-go plugin init` (R3.2): a plugin-author variant of
// `apm-go init` sharing runInitCore's common body (design.md §3) via
// pluginMode (init.go), with its own flag set -- --verbose/-v instead of
// consumer init's --force (R3.3.f). The two commands deliberately do not
// share a FlagSet, so --verbose never leaks into consumer init's --help
// (AC33) and --force never leaks into plugin init's --help (AC8).
func pluginInitCmd() *cobra.Command {
	var (
		yes        bool
		targetFlag string
		verbose    bool
	)

	cmd := &cobra.Command{
		Use:          "init [project-name]",
		Short:        "Initialize a new APM plugin project",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInitCore(args, pluginMode, yes, targetFlag, false, verbose)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive prompts and use auto-detected defaults")
	cmd.Flags().StringVar(&targetFlag, "target", "", "Comma-separated target list (skip prompt)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	return cmd
}
