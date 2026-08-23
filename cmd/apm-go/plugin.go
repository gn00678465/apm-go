package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

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
// pluginMode/agentPluginMode (init.go), with its own flag set -- --verbose/-v
// instead of consumer init's --force (R3.3.f), plus upstream's --format and
// --claude-plugin format selectors (commands/plugin/init.py:34-50). The two
// commands deliberately do not share a FlagSet, so --verbose never leaks into
// consumer init's --help (AC33) and --force never leaks into plugin init's
// --help (AC8).
func pluginInitCmd() *cobra.Command {
	var (
		yes          bool
		targetFlag   string
		verbose      bool
		format       string
		claudePlugin bool
	)

	cmd := &cobra.Command{
		Use:          "init [project-name]",
		Short:        "Scaffold a plugin project (creates plugin.json + apm.yml)",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Upstream raises click.UsageError (exit 2) before touching the
			// filesystem (commands/plugin/init.py:58-61).
			pm, err := resolvePluginFormat(format, cmd.Flags().Changed("format"), claudePlugin)
			if err != nil {
				return withExitCode(2, err)
			}
			mode := pluginMode
			if pm == pluginModeAgent {
				mode = agentPluginMode
			}
			return runInitCore(args, mode, yes, targetFlag, false, verbose)
		},
	}
	// Click turns a flag given without its value into a usage error
	// (exit 2, "Option '--format' requires an argument."); cobra reports it
	// as a plain parse error. Map it here so the CLI contract matches.
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		if name, ok := strings.CutPrefix(err.Error(), "flag needs an argument: "); ok {
			return withExitCode(2, fmt.Errorf("Option '%s' requires an argument.", name))
		}
		return withExitCode(2, err)
	})
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive prompts and use auto-detected defaults")
	cmd.Flags().StringVar(&targetFlag, "target", "", "Comma-separated target list (skip prompt, write directly)")
	cmd.Flags().Var(formatChoiceValue{&format}, "format",
		"Plugin format. 'agent-plugin' selects portable Agent Plugins v1; 'plugin', 'claude', and 'claude-plugin' select the current Claude-compatible default.")
	cmd.Flags().BoolVar(&claudePlugin, "claude-plugin", false, "Scaffold the legacy Claude-compatible layout (current no-flag default)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed output")
	return cmd
}
