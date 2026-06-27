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
		Use:   "validate <file>",
		Short: "Validate a YAML file against the OpenAPM safe subset",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("%s: %w", args[0], err)
			}
			if _, err := yamlcore.SafeLoad(data); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", args[0], err)
				os.Exit(1)
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
}

func normalizeCmd() *cobra.Command {
	var stdout bool

	cmd := &cobra.Command{
		Use:   "normalize <file>",
		Short: "Parse and re-emit a YAML file (round-trip)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("%s: %w", args[0], err)
			}
			node, err := yamlcore.SafeLoad(data)
			if err != nil {
				return fmt.Errorf("%s: %v", args[0], err)
			}
			out, err := yamlcore.SafeDump(node)
			if err != nil {
				return fmt.Errorf("dump: %w", err)
			}
			if stdout {
				os.Stdout.Write(out)
			} else {
				if err := os.WriteFile(args[0], out, 0644); err != nil {
					return fmt.Errorf("write: %w", err)
				}
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.Flags().BoolVar(&stdout, "stdout", false, "Write to stdout instead of overwriting the file")
	return cmd
}
