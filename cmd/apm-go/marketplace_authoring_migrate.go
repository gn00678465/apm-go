package main

import (
	"fmt"

	"github.com/apm-go/apm/internal/marketplace/authoring"
	"github.com/spf13/cobra"
)

// marketplaceMigrateCmd implements mkt-044: `apm marketplace migrate
// [--force|--yes/-y] [--dry-run] [-v]`. The actual comment-preserving
// surgical edit lives in internal/marketplace/authoring/migrate.go; this
// command wires flags, prints the diff (always under --dry-run, only with
// --verbose otherwise), and reports success the way Python's own migrate
// command does. No exit-code override is needed here (unlike `package
// add/remove/set`'s exit 2, mkt-045) -- any error takes main()'s default
// exit 1 path.
func marketplaceMigrateCmd() *cobra.Command {
	var force, dryRun, verbose bool

	cmd := &cobra.Command{
		Use:          "migrate",
		Short:        "Fold marketplace.yml into apm.yml's 'marketplace:' block",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			diff, err := authoring.Migrate(".", authoring.MigrateOptions{Force: force, DryRun: dryRun})
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if dryRun {
				fmt.Fprintln(w, "Dry run -- the following changes would be applied to apm.yml:")
				if diff == "" {
					fmt.Fprintln(w, "(no changes)")
				} else {
					fmt.Fprint(w, diff)
				}
				return nil
			}

			fmt.Fprintln(w, "[+] Migrated marketplace.yml into apm.yml's 'marketplace:' block")
			fmt.Fprintln(w, "marketplace.yml has been removed. Commit apm.yml to record the migration.")
			if verbose {
				fmt.Fprint(w, diff)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing 'marketplace:' block in apm.yml")
	cmd.Flags().BoolVarP(&force, "yes", "y", false, "Alias for --force")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show the proposed apm.yml changes without writing them")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed output")
	return cmd
}
