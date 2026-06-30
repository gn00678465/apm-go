package main

import (
	"fmt"
	"os"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
	"github.com/spf13/cobra"
)

// auditCmd implements `apm audit` (req-sc-001): re-verify every deployed file's
// recorded SHA-256 against disk and report content-integrity violations. It
// operates from the lockfile + disk alone — apm.yml is NOT required.
func auditCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "audit",
		Short:        "Re-verify deployed-file integrity against apm.lock.yaml",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile("apm.lock.yaml")
			if err != nil {
				return fmt.Errorf("read apm.lock.yaml: %w", err)
			}
			node, err := yamlcore.SafeLoad(data)
			if err != nil {
				return fmt.Errorf("parse apm.lock.yaml: %w", err)
			}
			lock, err := lockfile.ParseLockfile(node)
			if err != nil {
				return fmt.Errorf("validate apm.lock.yaml: %w", err)
			}

			viol := lockfile.VerifyDeployedState(lock, ".")
			if len(viol) > 0 {
				for _, v := range viol {
					observed := v.Observed
					if observed == "" {
						observed = "<missing>"
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "content-integrity violation: %s expected %s, observed %s\n",
						v.Path, v.Expected, observed)
				}
				return fmt.Errorf("audit failed: %d content-integrity violation(s) (first: %s)", len(viol), viol[0].Path)
			}

			count := 0
			for i := range lock.Dependencies {
				count += len(lock.Dependencies[i].DeployedHashes)
			}
			count += len(lock.LocalDeployedHashes)
			fmt.Fprintf(cmd.OutOrStdout(), "audit: %d deployed files verified\n", count)
			return nil
		},
	}
}
