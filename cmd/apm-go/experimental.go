package main

import (
	"fmt"

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
		Use:   "enable <feature>",
		Short: "Enable an experimental feature",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := experimental.Enable(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Enabled experimental feature: %s\n", args[0])
			if f, ok := experimental.Known(args[0]); ok && f.Hint != "" {
				fmt.Fprintln(cmd.OutOrStdout(), f.Hint)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "disable <feature>",
		Short: "Disable an experimental feature",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := experimental.Disable(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Disabled experimental feature: %s\n", args[0])
			return nil
		},
	})

	return cmd
}
