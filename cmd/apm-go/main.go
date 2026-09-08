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

// buildRootCmd assembles the full apm-go command tree -- extracted from
// main() so a test can build the SAME tree (ticket 13 attempt 2's
// command-level regression test needs a real root.ExecuteC() to reproduce
// cobra's own flag-parse errors, not a re-derivation of them).
func buildRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "apm-go",
		Short:   "Agent Package Manager (Go)",
		Version: version.Version,
		// SilenceErrors: this app owns error printing (ux.Error, stdout with
		// "[x] ", ticket 10 decision A) instead of cobra's own
		// c.PrintErrln(c.ErrPrefix(), err.Error()) default, which writes
		// "Error: ..." to stderr -- see renderRootError below.
		SilenceErrors: true,
	}

	root.AddCommand(validateCmd())
	root.AddCommand(normalizeCmd())
	root.AddCommand(initCmd())
	root.AddCommand(pluginCmd())
	root.AddCommand(installCmd())
	root.AddCommand(updateCmd())
	root.AddCommand(uninstallCmd())
	root.AddCommand(auditCmd())
	root.AddCommand(experimentalCmd())
	root.AddCommand(marketplaceCmd())
	root.AddCommand(packCmd())
	root.AddCommand(compileCmd())
	root.AddCommand(doctorCmd())
	root.AddCommand(searchCmd())
	return root
}

// renderRootError prints err's user-facing message for cmd (the command
// root.ExecuteC() reports as having failed) and returns the process exit
// code -- extracted from main() so a test can exercise this exact
// rendering (wrapped in captureStdout/captureStderr, which redirect the
// real os.Stdout/os.Stderr this function writes to) without spawning a
// real apm-go subprocess.
func renderRootError(cmd *cobra.Command, err error) int {
	switch {
	case isSilentExit(err):
		// nothing to print
	case isUsageError(err):
		// Click's own UsageError.show() (ticket 13, decision recorded
		// in .scratch/parity-runner/issues/10-error-output-contract.md):
		// a "Usage: ...\nTry '<path> --help' for help.\n\n" block
		// followed by a plain "Error: " line, all on STDERR --
		// verified directly against the pinned Oracle, distinct from
		// decision (A)'s stdout/"[x] " contract for ordinary runtime
		// errors (CommandLogger._rich_error), which Click's own
		// exception-handling layer never goes through. isBareUsageError
		// skips the preamble for the one verified exception (a flag
		// missing its argument, raised before Click has a Context to
		// render a Usage line from -- see withBareUsageError).
		if !isBareUsageError(err) {
			fmt.Fprintf(os.Stderr, "Usage: %s\n", cmd.UseLine())
			fmt.Fprintf(os.Stderr, "Try '%s --help' for help.\n\n", cmd.CommandPath())
		}
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
	case isStderrError(err):
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
	default:
		ux.Error(os.Stderr, "%s", err)
	}
	return exitCodeOf(err)
}

func main() {
	ux.Init()
	root := buildRootCmd()
	cmd, err := root.ExecuteC()
	if err != nil {
		os.Exit(renderRootError(cmd, err))
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
				ux.List(os.Stderr, items)
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
