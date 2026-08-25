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
				return marketplaceNotRegisteredErr("audit", name)
			}
			w := cmd.OutOrStdout()
			// Mirrors upstream audit.py:36-40's progress lines around the
			// fetch.
			ux.Info(w, "Auditing marketplace %q...", name)
			m, err := marketplace.Fetch(context.Background(), src)
			if err != nil {
				return fmt.Errorf("could not reach marketplace %q: %w", name, err)
			}
			plural := "s"
			if len(m.Plugins) == 1 {
				plural = ""
			}
			ux.Info(w, "Checking %d plugin%s...", len(m.Plugins), plural)

			// v0.28.0 (PR #2460): a LOCAL marketplace's string-source
			// plugins are audited against their on-disk apm.yml instead of
			// being skipped; localRoot flags that mode.
			localRoot := ""
			if src.Kind() == marketplace.KindLocal {
				// Ticket 24 AC3: src.URL may be a bare path or a "file://"
				// URI (this ticket's new writes) -- LocalFilesystemPath
				// accepts both, indefinitely.
				localRoot = marketplace.LocalFilesystemPath(src.URL)
			}
			reports := authoring.RunAudit(m, name, src.Host, localRoot, authoring.DefaultApmYMLFetcher)

			// Upstream audit.py:49-56: the section header is suppressed on
			// an all-clean default run, where it would hang above an empty
			// body.
			hasFindings := false
			for _, r := range reports {
				if r.FetchStatus != authoring.FetchOK || len(r.Issues) > 0 {
					hasFindings = true
					break
				}
			}
			fmt.Fprintln(w)
			if hasFindings || verbose {
				ux.Info(w, "Audit Results:")
			}
			ok, bypassTotal, skipped, unverifiable := printAuditReports(cmd, reports, verbose)

			fmt.Fprintln(w)
			// v0.28.0 (PR #2460): the Summary line only reads as a success
			// when something was audited AND nothing was skipped or
			// unverifiable; every other mix is neutral info.
			summary := fmt.Sprintf("Summary: %d clean, %d bypass warning(s), %d skipped, %d unverifiable error(s)",
				ok, bypassTotal, skipped, unverifiable)
			if ok > 0 && bypassTotal == 0 && skipped == 0 && unverifiable == 0 {
				ux.Success(w, "%s", summary)
			} else {
				ux.Info(w, "%s", summary)
			}
			if bypassTotal > 0 {
				// Upstream audit.py:106-113's closing explainer.
				fmt.Fprintln(w)
				ux.Info(w, "Marketplace refs (name@marketplace) pin transitive deps through the catalogue "+
					"so consumers get the same versions you tested. See: "+
					"https://microsoft.github.io/apm/reference/cli/marketplace/#apm-marketplace-audit-name")
			}

			// v0.28.0 --strict tightening (audit.py:117-138): an audit that
			// verified nothing, or skipped any plugin, cannot claim
			// supply-chain integrity and fails before the bypass/error check.
			if strict && ok == 0 && bypassTotal == 0 {
				if skipped > 0 && !verbose {
					ux.Info(w, "Run 'apm-go marketplace audit %s --strict --verbose' to see skipped plugin reasons.", name)
				}
				return fmt.Errorf("--strict: no plugins were audited; cannot verify supply-chain integrity")
			}
			if strict && skipped > 0 {
				if !verbose {
					ux.Info(w, "Run 'apm-go marketplace audit %s --strict --verbose' to see skipped plugin reasons.", name)
				}
				return fmt.Errorf("--strict: %d plugin source(s) skipped; cannot verify a complete marketplace audit", skipped)
			}
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
	// Upstream audit.py:68-71's singular/plural phrasing.
	verbPhrase := fmt.Sprintf("%d dependencies bypass", len(r.Issues))
	if len(r.Issues) == 1 {
		verbPhrase = "1 dependency bypasses"
	}
	root := ux.TreeNode{
		Text: fmt.Sprintf("%s: %s the marketplace", r.PluginName, verbPhrase),
	}
	for _, issue := range r.Issues {
		root.Children = append(root.Children, ux.TreeNode{
			Text:     issue.Dep,
			Children: []ux.TreeNode{{Text: "hint: " + issue.Suggestion}},
		})
	}
	ux.Tree(w, root)
}
