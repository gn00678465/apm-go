package main

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/ux"
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
//
// Phase 7 (07-12-p0-parity-quickwins design.md "audit 掃描接線"): --content
// adds the other half of that contrast -- Python's hidden-Unicode scan
// pillar -- as an opt-in flag, without touching the bare SHA-256 path.
func auditCmd() *cobra.Command {
	var content bool
	var verbose bool

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Re-verify deployed-file integrity against apm.lock.yaml",
		Long: `Re-verify deployed-file integrity against apm.lock.yaml.

apm-go audit (bare) recomputes every deployed file's SHA-256 hash and
compares it against apm.lock.yaml. This differs from Python's 'apm
audit' (bare), which instead runs a hidden-Unicode scan and never
touches SHA-256 -- the two commands share a name but check different
things. Python's equivalent SHA-256 re-verification is buried behind
'apm audit --ci' as its content-integrity check.

--content runs apm-go's shared hidden-Unicode scanner (the same
internal/security scanner 'pack' already runs warn-only over bundle
sources) across every deployed file recorded in apm.lock.yaml --
both dependency deployed_files and the project's own
local_deployed_files. This closes the content-scan gap with Python's
bare audit, but it does NOT reproduce Python's default install-replay
drift detection (which re-materializes the lockfile in a scratch dir
and diffs it against the project to catch orphaned/unintegrated/
modified files), nor any of Python's --ci, --policy, --external,
--format, -o, or --strip flags -- those remain separate,
unimplemented subsystems.`,
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

			if content {
				return runAuditContentScan(cmd.OutOrStdout(), cmd.ErrOrStderr(), lock)
			}

			viol := lockfile.VerifyDeployedState(lock, ".")
			if len(viol) > 0 {
				for _, v := range viol {
					observed := v.Observed
					if observed == "" {
						observed = "<missing>"
					}
					ux.Error(cmd.ErrOrStderr(), "content-integrity violation: %s expected %s, observed %s",
						v.Path, v.Expected, observed)
				}
				return fmt.Errorf("audit failed: %d content-integrity violation(s) (first: %s)", len(viol), viol[0].Path)
			}

			count := 0
			for i := range lock.Dependencies {
				count += len(lock.Dependencies[i].DeployedHashes)
			}
			count += len(lock.LocalDeployedHashes)
			ux.Success(cmd.OutOrStdout(), "audit: %d deployed files verified", count)
			// R12c (prd.md/design.md §3): the verified-file paths are
			// already sitting in lock's DeployedHashes/LocalDeployedHashes
			// maps (that's what count above was tallying) -- --verbose just
			// prints them too, without changing the default "audit: %d
			// deployed files verified" line above at all.
			if verbose {
				printAuditVerifiedFiles(cmd.OutOrStdout(), lock)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&content, "content", false,
		"Scan every deployed file for hidden Unicode characters "+
			"(does not run SHA re-verification, drift replay, or --ci/--policy/--external/--format/-o/--strip)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false,
		"list every deployed file path verified by a successful bare audit (has no effect with --content)")

	return cmd
}

// printAuditVerifiedFiles lists every deployed file path a successful bare
// audit just re-verified, grouped by owning dependency (plus the project's
// own local deployment) -- R12c: --verbose-only detail behind the unchanged
// default "audit: %d deployed files verified" summary line.
func printAuditVerifiedFiles(w io.Writer, lock *lockfile.Lockfile) {
	var items []ux.Item
	for i := range lock.Dependencies {
		dep := &lock.Dependencies[i]
		if len(dep.DeployedHashes) == 0 {
			continue
		}
		items = append(items, ux.Item{Text: dep.UniqueKey()})
		paths := make([]string, 0, len(dep.DeployedHashes))
		for p := range dep.DeployedHashes {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			items = append(items, ux.Item{Level: 1, Text: p})
		}
	}
	if len(lock.LocalDeployedHashes) > 0 {
		items = append(items, ux.Item{Text: "(project)"})
		paths := make([]string, 0, len(lock.LocalDeployedHashes))
		for p := range lock.LocalDeployedHashes {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			items = append(items, ux.Item{Level: 1, Text: p})
		}
	}
	if len(items) == 0 {
		return
	}
	ux.BulletList(w, items)
}
