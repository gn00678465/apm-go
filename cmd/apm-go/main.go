package main

import (
	"fmt"
	"os"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/ux"
	"github.com/apm-go/apm/internal/version"
	"github.com/apm-go/apm/internal/yamlcore"
	"github.com/spf13/cobra"
)

func main() {
	ux.Init()

	root := &cobra.Command{
		Use:     "apm-go",
		Short:   "Agent Package Manager (Go)",
		Version: version.Version,
	}

	root.AddCommand(validateCmd())
	root.AddCommand(normalizeCmd())
	root.AddCommand(initCmd())
	root.AddCommand(installCmd())
	root.AddCommand(updateCmd())
	root.AddCommand(uninstallCmd())
	root.AddCommand(auditCmd())
	root.AddCommand(experimentalCmd())
	root.AddCommand(marketplaceCmd())
	root.AddCommand(packCmd())
	root.AddCommand(compileCmd())

	if err := root.Execute(); err != nil {
		os.Exit(exitCodeOf(err))
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
			if len(diags) > 0 {
				ux.Warn(os.Stderr, "%d diagnostic(s) found in %s", len(diags), args[0])
				items := make([]ux.Item, len(diags))
				for i, d := range diags {
					items[i] = ux.Item{Text: d.Message}
				}
				ux.BulletList(os.Stderr, items)
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
