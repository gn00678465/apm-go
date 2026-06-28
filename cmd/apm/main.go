package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/manifest"
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
	root.AddCommand(initCmd())

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

func initCmd() *cobra.Command {
	var (
		name    string
		version string
		targets []string
		force   bool
	)

	cmd := &cobra.Command{
		Use:          "init",
		Short:        "Initialize a new apm.yml manifest",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				dir, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("cannot determine directory name: %w", err)
				}
				name = filepath.Base(dir)
			}
			if version == "" {
				version = "0.1.0"
			}

			supported := make(map[string]bool)
			for _, t := range manifest.SupportedTargets {
				supported[t] = true
			}
			for _, t := range targets {
				if !supported[t] {
					return fmt.Errorf("target %q is not supported for init; choose from: %s",
						t, strings.Join(manifest.SupportedTargets, ", "))
				}
			}

			if !force {
				if _, err := os.Stat("apm.yml"); err == nil {
					return fmt.Errorf("apm.yml already exists; use --force to overwrite")
				}
			}

			data := map[string]any{
				"name":    name,
				"version": version,
			}
			if len(targets) == 1 {
				data["target"] = targets[0]
			} else if len(targets) > 1 {
				data["target"] = targets
			}

			raw, err := yamllib.Marshal(data)
			if err != nil {
				return fmt.Errorf("serialize: %w", err)
			}
			node, err := yamlcore.SafeLoad(raw)
			if err != nil {
				return fmt.Errorf("generated manifest is invalid: %w", err)
			}
			if _, _, err := manifest.ParseManifest(node); err != nil {
				return fmt.Errorf("generated manifest fails validation: %w", err)
			}

			out, err := yamlcore.SafeDump(node)
			if err != nil {
				return fmt.Errorf("serialize: %w", err)
			}
			if err := os.WriteFile("apm.yml", out, 0644); err != nil {
				return fmt.Errorf("write apm.yml: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Created apm.yml\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Package name (default: directory name)")
	cmd.Flags().StringVar(&version, "version", "", "Package version (default: 0.1.0)")
	cmd.Flags().StringSliceVar(&targets, "target", nil, "Target agent(s): claude, codex, copilot, opencode, antigravity")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing apm.yml")
	return cmd
}
