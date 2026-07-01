package main

import (
	"fmt"
	"os"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "apm-go",
		Short: "Agent Package Manager (Go)",
	}

	root.AddCommand(validateCmd())
	root.AddCommand(normalizeCmd())
	root.AddCommand(initCmd())
	root.AddCommand(installCmd())
	root.AddCommand(auditCmd())
	root.AddCommand(experimentalCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "validate <file>",
		Short:        "Validate a YAML file against the OpenAPM safe subset and manifest schema",
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

			if node.Kind != yamllib.DocumentNode || len(node.Content) == 0 {
				return fmt.Errorf("%s: invalid YAML document", args[0])
			}
			root := node.Content[0]

			if root.Kind != yamllib.MappingNode {
				return fmt.Errorf("%s: top-level must be a YAML mapping", args[0])
			}

			if manifest.NodeHasKey(root, "lockfile_version") {
				return nil
			}

			_, diags, err := manifest.ParseManifest(node)
			if err != nil {
				return fmt.Errorf("%s: %w", args[0], err)
			}
			for _, d := range diags {
				fmt.Fprintf(os.Stderr, "warning: %s\n", d.Message)
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

// initCmd is defined in init.go
