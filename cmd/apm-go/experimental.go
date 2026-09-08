package main

import (
	"strings"

	"github.com/apm-go/apm/internal/experimental"
	"github.com/apm-go/apm/internal/ux"
	"github.com/spf13/cobra"
)

func experimentalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "experimental",
		Short: "Manage experimental feature flags",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List experimental features and their status",
		RunE: func(cmd *cobra.Command, args []string) error {
			features := experimental.All()
			rows := make([][]string, len(features))
			for i, f := range features {
				status := "disabled"
				if experimental.IsEnabled(f.Name) {
					status = "enabled"
				}
				rows[i] = []string{f.Name, status, f.Description}
			}
			ux.Table(cmd.OutOrStdout(), []string{"FEATURE", "STATUS", "DESCRIPTION"}, rows)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:          "enable <feature>",
		Short:        "Enable an experimental feature",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := experimental.Enable(args[0]); err != nil {
				if strings.HasPrefix(err.Error(), "unknown experimental feature") {
					// Oracle commands/experimental.py:129-137 renders an
					// unknown feature as an error plus this list hint and exits
					// without Cobra's usage block.
					ux.Error(cmd.OutOrStdout(), "Unknown experimental feature: %s", args[0])
					ux.Info(cmd.OutOrStdout(), "Run 'apm experimental list' to see all available features.")
					return withSilentExitCode(1, err)
				}
				return err
			}
			// Oracle commands/experimental.py:133-137 uses
			// logger.success(..., symbol="check"), which renders "[+]".
			ux.Check(cmd.OutOrStdout(), "Enabled experimental feature: %s", args[0])
			if f, ok := experimental.Known(args[0]); ok && f.Hint != "" {
				ux.Info(cmd.OutOrStdout(), "%s", f.Hint)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:          "disable <feature>",
		Short:        "Disable an experimental feature",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := experimental.Disable(args[0]); err != nil {
				if strings.HasPrefix(err.Error(), "unknown experimental feature") {
					// Same Oracle unknown-feature contract as enable above.
					ux.Error(cmd.OutOrStdout(), "Unknown experimental feature: %s", args[0])
					ux.Info(cmd.OutOrStdout(), "Run 'apm experimental list' to see all available features.")
					return withSilentExitCode(1, err)
				}
				return err
			}
			// Oracle commands/experimental.py:277-281 uses
			// logger.success(..., symbol="check"), which renders "[+]".
			ux.Check(cmd.OutOrStdout(), "Disabled experimental feature: %s", args[0])
			return nil
		},
	})

	return cmd
}
