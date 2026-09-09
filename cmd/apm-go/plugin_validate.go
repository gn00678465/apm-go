package main

import (
	"github.com/spf13/cobra"
)

// pluginValidateCmd is a placeholder for RED evidence (WP03/T017): the real
// implementation lands once plugin_validate_test.go's failures are recorded.
func pluginValidateCmd() *cobra.Command {
	var strict, verbose bool
	cmd := &cobra.Command{
		Use:   "validate [path]",
		Short: "placeholder",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "placeholder")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "placeholder")
	return cmd
}
