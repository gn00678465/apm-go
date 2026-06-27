package main

import (
	"fmt"
	"os"

	"github.com/apm-go/apm/internal/yamlcore"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "apm",
		Short: "Agent Package Manager (Go)",
	}

	root.AddCommand(validateCmd())
	root.AddCommand(normalizeCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "validate <file>",
		Short:        "Validate a YAML file against the OpenAPM safe subset",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("%s: %w", args[0], err)
			}
			if _, err := yamlcore.SafeLoad(data); err != nil {
				return fmt.Errorf("%s: %w", args[0], err)
			}
			return nil
		},
	}
}

func normalizeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "normalize <file>",
		Short:        "Parse and re-emit a YAML file (round-trip)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("%s: %w", args[0], err)
			}
			node, err := yamlcore.SafeLoad(data)
			if err != nil {
				return fmt.Errorf("%s: %w", args[0], err)
			}
			out, err := yamlcore.SafeDump(node)
			if err != nil {
				return fmt.Errorf("dump: %w", err)
			}
			_, err = os.Stdout.Write(out)
			return err
		},
	}
	cmd.Flags().Bool("stdout", false, "Write to stdout (default behavior, kept for runner compatibility)")
	return cmd
}
