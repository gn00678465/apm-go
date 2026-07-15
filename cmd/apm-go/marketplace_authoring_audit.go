package main

import (
	"context"
	"fmt"
	"io"

	"github.com/apm-go/apm/internal/marketplace"
	"github.com/apm-go/apm/internal/marketplace/authoring"
	"github.com/apm-go/apm/internal/ux"
	"github.com/spf13/cobra"
)

// marketplaceAuditCmd implements mkt-043 修訂版: `apm marketplace audit NAME
// [--strict]`. For an already-*registered* marketplace (marketplace.FindByName
// + marketplace.Fetch, the same consumer-package plumbing `check`/`browse`
// use), fetch every plugin's own apm.yml (authoring.RunAudit /
// authoring.DefaultApmYMLFetcher) and report dependencies.apm/
// devDependencies.apm entries that bypass the marketplace's version pinning.
//
// Only bypass findings and unverifiable (NETWORK_ERROR/PARSE_ERROR) fetch
// failures count toward --strict's exit-1 decision; NO_MANIFEST and
// UNSUPPORTED_SOURCE are always skipped, matching authoring.RunAudit's own
// FetchStatus classification (mkt-043's "NO_MANIFEST/UNSUPPORTED_SOURCE 算
// skipped,不觸發"). Without --strict, this command always exits 0 -- a
// bypass finding is only ever a warning printed to stdout.
func marketplaceAuditCmd() *cobra.Command {
	var strict, verbose bool

	cmd := &cobra.Command{
		Use:          "audit NAME",
		Short:        "Check that a registered marketplace's plugins resolve their dependencies through the marketplace",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			src, err := marketplace.FindByName(name)
			if err != nil {
				return err
			}
			if src == nil {
				return marketplaceNotRegisteredErr(name)
			}
			m, err := marketplace.Fetch(context.Background(), src)
			if err != nil {
				return fmt.Errorf("could not reach marketplace %q: %w", name, err)
			}

			reports := authoring.RunAudit(m, name, src.Host, authoring.DefaultApmYMLFetcher)
			ok, bypassTotal, skipped, unverifiable := printAuditReports(cmd, reports, verbose)

			fmt.Fprintln(cmd.OutOrStdout())
			ux.Info(cmd.OutOrStdout(), "Summary: %d clean, %d bypass warning(s), %d skipped, %d unverifiable error(s)",
				ok, bypassTotal, skipped, unverifiable)

			if strict && (bypassTotal > 0 || unverifiable > 0) {
				return fmt.Errorf("audit %q failed: %d bypass warning(s), %d unverifiable error(s)", name, bypassTotal, unverifiable)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&strict, "strict", false, "exit non-zero when any plugin has bypass dependencies or unverifiable fetch errors")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print extra diagnostics, including clean/skipped plugins")
	return cmd
}

// printAuditReports writes one line (or more, for a plugin with bypass
// issues) per plugin report to cmd's stdout, and returns the four Summary-
// line counters mkt-043's --strict decision and closing line both need.
func printAuditReports(cmd *cobra.Command, reports []authoring.PluginAuditReport, verbose bool) (ok, bypassTotal, skipped, unverifiable int) {
	w := cmd.OutOrStdout()
	for _, r := range reports {
		switch r.FetchStatus {
		case authoring.FetchOK:
			if len(r.Issues) == 0 {
				ok++
				if verbose {
					ux.Success(w, "%s: deps are marketplace-resolved", r.PluginName)
				}
				continue
			}
			bypassTotal += len(r.Issues)
			printBypassTree(w, r)
		case authoring.FetchNoManifest, authoring.FetchUnsupportedSource:
			skipped++
			if verbose {
				ux.Info(w, "%s: skipped (%s)", r.PluginName, r.Detail)
			}
		default:
			unverifiable++
			ux.Warn(w, "%s: could not verify (%s)", r.PluginName, r.Detail)
		}
	}
	return ok, bypassTotal, skipped, unverifiable
}

// printBypassTree renders one plugin's marketplace-bypass findings as a
// two-level nested tree (plugin -> dependency -> hint), replacing the
// former flat "- dep" / "  hint: ..." indentation.
func printBypassTree(w io.Writer, r authoring.PluginAuditReport) {
	root := ux.TreeNode{
		Text: fmt.Sprintf("%s: %d dependencies bypass the marketplace", r.PluginName, len(r.Issues)),
	}
	for _, issue := range r.Issues {
		root.Children = append(root.Children, ux.TreeNode{
			Text:     issue.Dep,
			Children: []ux.TreeNode{{Text: "hint: " + issue.Suggestion}},
		})
	}
	ux.Tree(w, root)
}
