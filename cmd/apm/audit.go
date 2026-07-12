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
//
// P0 #3 (register §3.1/§5): apm-go audit (bare) and Python's `apm audit`
// (bare) share a name but check different things -- see Long below and
// .trellis/spec/backend/cli-parity-notes.md for the full contrast. This
// task does not change audit's behavior, only documents the gap.
func auditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "audit",
		Short: "Re-verify deployed-file integrity against apm.lock.yaml",
		Long: `Re-verify deployed-file integrity against apm.lock.yaml.

apm-go audit (bare) recomputes every deployed file's SHA-256 hash and
compares it against apm.lock.yaml. This differs from Python's 'apm
audit' (bare), which instead runs a hidden-Unicode scan and never
touches SHA-256 -- the two commands share a name but check different
things. Python's equivalent SHA-256 re-verification is buried behind
'apm audit --ci' as its content-integrity check.`,
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
