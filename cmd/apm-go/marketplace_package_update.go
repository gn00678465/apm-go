package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/apm-go/apm/internal/marketplace/authoring"
	"github.com/apm-go/apm/internal/ux"
	"github.com/spf13/cobra"
)

// marketplacePackageUpdateCmd is `apm-go marketplace package update
// [NAME...]` (issue #25). The pinned Oracle (b75a02b1) has no counterpart:
// its `package set` rejects --version together with --ref and drops the
// other field when given one, so an entry that carries a display version
// and a SHA ref -- the pair `outdated` and `check` compare -- could only be
// upgraded by hand. This command writes back what `outdated` already
// resolved. With no Oracle output to compare against, its output contract is
// pinned by tools/gate/realexec.sh instead of a tools/parity corpus case
// (ticket 37, .scratch/parity-runner/issues/
// 37-marketplace-package-update-realexec-contract.md).
func marketplacePackageUpdateCmd() *cobra.Command {
	var dryRun, includePrerelease bool

	cmd := &cobra.Command{
		Use:   "update [NAME...]",
		Short: "Upgrade SHA-pinned packages to what 'marketplace outdated' reports",
		Long: "Update SHA-pinned packages in the marketplace authoring config to what " +
			"'marketplace outdated' reports. An entry with a 40-character SHA ref and an " +
			"exact version moves to the highest matching tag (version and ref); one " +
			"with a SHA ref and no version moves to the default branch tip (ref). " +
			"When such an entry has a subdir, it moves only if the content of that " +
			"directory at the default branch tip differs from the pinned commit; " +
			"with the same content nothing is written. " +
			"Local packages, named refs, and version ranges are skipped. With no NAME " +
			"every package is considered. Run 'apm-go pack' afterwards to rebuild the outputs.",
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, src, err := authoring.LoadAuthoringConfig(".")
			if err != nil {
				return configLoadError(err)
			}
			if src == authoring.ConfigSourceLegacy {
				ux.Warn(cmd.ErrOrStderr(), "reading legacy marketplace.yml; run 'apm-go marketplace migrate' to fold it into apm.yml")
			}

			updates, err := authoring.PlanPackageUpdatesWith(cfg, args, includePrerelease, authoring.OutdatedDeps{
				Lister:  authoring.DefaultRefLister,
				Objects: authoring.DefaultSubdirObjectReader,
			})
			var unresolved *authoring.UpdateResolutionError
			if errors.As(err, &unresolved) {
				// One status line per package; the root handler would print
				// the joined text as a single record.
				for _, line := range unresolved.Lines {
					ux.Error(cmd.ErrOrStderr(), "%s", line)
				}
				return withSilentExitCode(2, err)
			}
			if err != nil {
				return withExitCode(2, err)
			}
			if err := authoring.ApplyPackageUpdates(".", updates, dryRun); err != nil {
				return withExitCode(2, err)
			}

			w := cmd.OutOrStdout()
			report, verb, summary := ux.Check, "Updated", "%d package(s) updated"
			if dryRun {
				report, verb, summary = ux.Info, "Would update", "%d package(s) would be updated"
			}
			updated := 0
			for _, u := range updates {
				name := u.Package.Name
				if u.Action != authoring.UpdateApply {
					// A skip is reported only for a package the user named;
					// with no NAME the skipped majority would bury the updates.
					if len(args) > 0 {
						ux.Info(w, "Skipped package '%s': %s", name, u.Note)
					}
					continue
				}
				updated++
				change := fmt.Sprintf("ref %s -> %s", shortSHA(u.Package.Ref), shortSHA(u.NewRef))
				if u.NewVersion != "" {
					change = fmt.Sprintf("version %s -> %s, %s", strings.TrimSpace(u.Package.Version), u.NewVersion, change)
				}
				report(w, "%s package '%s': %s", verb, name, change)
			}
			if updated == 0 {
				ux.Info(w, "All packages are up to date")
				return nil
			}
			ux.Info(w, summary, updated)
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the version and ref that would be written without changing the file")
	cmd.Flags().BoolVar(&includePrerelease, "include-prerelease", false, "include prerelease versions when determining the latest tag")
	return cmd
}
