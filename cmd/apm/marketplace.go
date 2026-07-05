package main

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/apm-go/apm/internal/marketplace"
	"github.com/spf13/cobra"
)

// marketplaceAliasPattern is mkt-004's alias/name format: a marketplace
// alias must be safe to appear on the right of "@" in a "plugin@marketplace"
// reference. It is consulted here only as part of mkt-018's --name fallback
// (resolveMarketplaceAlias) -- registry.go's own FindByName/AddSource never
// enforce it, matching the Python original's registry.py, which stores
// whatever name it is given.
var marketplaceAliasPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func isValidMarketplaceAlias(name string) bool {
	return name != "" && marketplaceAliasPattern.MatchString(name)
}

// marketplaceCmd wires internal/marketplace's data model, registry and fetch
// clients (built in earlier steps) into the six `apm marketplace` consumer
// subcommands (mkt-010..mkt-016) plus the `build` tombstone (mkt-019), and
// (from internal/marketplace/authoring) the producer-side `init`, `check`,
// `outdated`, `package add/remove/set`, `audit`, and `migrate` subcommands
// (mkt-040, mkt-041, mkt-042 修訂版, mkt-045/046, mkt-043 修訂版, mkt-044 --
// Phase M3's full producer-side command set). Deliberately absent, per
// Phase M5 of marketplace-checklist.md:
// search (mkt-060, a top-level command, not a marketplace subcommand),
// doctor (mkt-061), publish (mkt-062), a browse --json flag (mkt-063), a validate
// --check-refs flag (mkt-017: an upstream placeholder that never did
// anything), and an "update" alias named "refresh" (mkt-064).
func marketplaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "marketplace",
		Short: "Manage marketplace sources (add/list/browse/update/remove/validate)",
	}
	cmd.AddCommand(marketplaceAddCmd())
	cmd.AddCommand(marketplaceListCmd())
	cmd.AddCommand(marketplaceBrowseCmd())
	cmd.AddCommand(marketplaceUpdateCmd())
	cmd.AddCommand(marketplaceRemoveCmd())
	cmd.AddCommand(marketplaceValidateCmd())
	cmd.AddCommand(marketplaceBuildCmd())
	cmd.AddCommand(marketplaceInitCmd())
	cmd.AddCommand(marketplaceCheckCmd())
	cmd.AddCommand(marketplaceOutdatedCmd())
	cmd.AddCommand(marketplacePackageCmd())
	cmd.AddCommand(marketplaceAuditCmd())
	cmd.AddCommand(marketplaceMigrateCmd())
	return cmd
}

// marketplaceAddCmd implements mkt-010 (SOURCE auto-detection, delegated
// entirely to marketplace.ParseMarketplaceSource), mkt-011 (--host
// conflict/ignore handling, also entirely inside ParseMarketplaceSource --
// this command only needs to propagate whatever error it returns), and
// mkt-018 (the "#ref" fragment / --ref /--branch / --name alias fallback
// behavior layered on top, which belongs at the CLI layer, not the SOURCE
// parser, since it needs the fetched manifest's name).
func marketplaceAddCmd() *cobra.Command {
	var name, ref, branch, host string
	var verbose bool

	cmd := &cobra.Command{
		Use:          "add SOURCE",
		Short:        "Register a marketplace source",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			refGiven := cmd.Flags().Changed("ref") || cmd.Flags().Changed("branch")
			flagRef := ref
			if flagRef == "" {
				flagRef = branch
			}

			rawSource, fragmentRef := splitHTTPSSourceFragment(args[0])
			if fragmentRef != "" && refGiven {
				return fmt.Errorf("SOURCE's '#%s' fragment cannot be combined with --ref/--branch", fragmentRef)
			}
			effectiveRef := fragmentRef
			if effectiveRef == "" {
				effectiveRef = flagRef
			}

			src, err := marketplace.ParseMarketplaceSource(rawSource, host)
			if err != nil {
				return err
			}
			if effectiveRef != "" {
				src.Ref = effectiveRef
			}

			wasFullHTTPSSource := strings.HasPrefix(strings.ToLower(rawSource), "https://")
			if needsUnpinnedGitRefWarning(wasFullHTTPSSource, src.Kind(), effectiveRef) {
				fmt.Fprintln(cmd.ErrOrStderr(), "[warn] Pin this git marketplace with a #ref (e.g. SOURCE#v1.2.3) to avoid silently tracking a moving branch")
			}

			m, err := marketplace.Fetch(context.Background(), src)
			if err != nil {
				return fmt.Errorf("could not reach marketplace source: %w", err)
			}

			effectiveName, aliasWarning := resolveMarketplaceAlias(name, m.Name, src)
			if aliasWarning != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "[warn] %s\n", aliasWarning)
			}
			src.Name = effectiveName

			if err := marketplace.AddSource(*src); err != nil {
				return fmt.Errorf("register marketplace: %w", err)
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "[+] Added marketplace %q (kind: %s)\n", effectiveName, src.Kind())
			if verbose {
				fmt.Fprintf(w, "  source: %s\n", src.URL)
				fmt.Fprintf(w, "  ref: %s\n", src.Ref)
				fmt.Fprintf(w, "  plugins: %d\n", len(m.Plugins))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&name, "name", "n", "", "marketplace alias to register under (defaults to the manifest name, or the repo name)")
	cmd.Flags().StringVarP(&ref, "ref", "r", "", "git ref (branch/tag) to pin the marketplace source to")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "deprecated alias for --ref")
	if err := cmd.Flags().MarkHidden("branch"); err != nil {
		panic(err)
	}
	cmd.Flags().StringVar(&host, "host", "", "override the host for an OWNER/REPO shorthand SOURCE")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print extra diagnostics after a successful add")
	cmd.MarkFlagsMutuallyExclusive("ref", "branch")
	return cmd
}

// splitHTTPSSourceFragment implements mkt-018's "#ref" fragment support: it
// only applies to a raw SOURCE string using the full "https://" form
// (design.md rule 4) -- a local path, an SCP-style remote, or an OWNER/REPO
// shorthand never carries a "#ref" fragment. Returns raw unchanged (and an
// empty ref) for every other shape.
func splitHTTPSSourceFragment(raw string) (source, ref string) {
	if !strings.HasPrefix(strings.ToLower(raw), "https://") {
		return raw, ""
	}
	idx := strings.Index(raw, "#")
	if idx < 0 {
		return raw, ""
	}
	return raw[:idx], raw[idx+1:]
}

// needsUnpinnedGitRefWarning implements mkt-018's "Pin this git marketplace
// with a #ref" warning: it only fires for a full "https://" SOURCE (not an
// OWNER/REPO shorthand, which always gets an implicit "main" default without
// the user spelling out a URL to pin) resolving to a git-backed Kind (not
// the direct-manifest-URL shortcut, which has no ref concept at all), when
// neither the SOURCE's own fragment nor --ref/--branch supplied a ref.
func needsUnpinnedGitRefWarning(wasFullHTTPSSource bool, kind marketplace.SourceKind, effectiveRef string) bool {
	if effectiveRef != "" || !wasFullHTTPSSource {
		return false
	}
	switch kind {
	case marketplace.KindGitHub, marketplace.KindGitLab, marketplace.KindGit:
		return true
	default:
		return false
	}
}

// resolveMarketplaceAlias implements mkt-018's --name fallback chain: an
// explicit --name always wins; otherwise the fetched manifest's own name is
// used if it passes mkt-004's alias format check; otherwise a warning is
// produced and the source's repo name (fallbackMarketplaceAlias) is used
// instead. A manifest with no name at all (empty string) falls back
// silently -- there is nothing invalid to warn about, just nothing to use.
func resolveMarketplaceAlias(explicitName, manifestName string, src *marketplace.MarketplaceSource) (name, warning string) {
	if explicitName != "" {
		return explicitName, ""
	}
	if isValidMarketplaceAlias(manifestName) {
		return manifestName, ""
	}
	fallback := fallbackMarketplaceAlias(src)
	if manifestName != "" {
		return fallback, fmt.Sprintf("manifest name %q is not a valid marketplace alias; falling back to %q", manifestName, fallback)
	}
	return fallback, ""
}

// fallbackMarketplaceAlias derives a repo-name-shaped alias from src when
// neither --name nor a usable manifest name is available: Owner/Repo for
// every remote Kind that has them (SCP, full URL, shorthand), the local
// directory's base name for KindLocal, and the parent path segment of a
// direct-manifest-URL KindURL source. "marketplace" is the last-resort
// fallback for a source that produces none of the above (never actually
// invalid as an alias, since it matches marketplaceAliasPattern).
func fallbackMarketplaceAlias(src *marketplace.MarketplaceSource) string {
	if src.Repo != "" {
		return src.Repo
	}
	switch src.Kind() {
	case marketplace.KindLocal:
		if base := filepath.Base(src.URL); base != "" && base != "." && base != string(filepath.Separator) {
			return base
		}
	case marketplace.KindURL:
		if u, err := url.Parse(src.URL); err == nil {
			if base := path.Base(path.Dir(u.Path)); base != "" && base != "." && base != "/" {
				return base
			}
		}
	}
	return "marketplace"
}

// marketplaceListCmd implements mkt-012: no arguments, a Name/Source/Ref/
// Path table of every registered marketplace. --verbose adds a Host column.
func marketplaceListCmd() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List every registered marketplace",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			sources, err := marketplace.LoadRegistry()
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if len(sources) == 0 {
				fmt.Fprintln(w, "No marketplaces registered. Add one with: apm marketplace add SOURCE")
				return nil
			}
			if verbose {
				fmt.Fprintf(w, "%-20s %-40s %-10s %-24s %s\n", "NAME", "SOURCE", "REF", "HOST", "PATH")
			} else {
				fmt.Fprintf(w, "%-20s %-40s %-10s %s\n", "NAME", "SOURCE", "REF", "PATH")
			}
			for _, s := range sources {
				if verbose {
					fmt.Fprintf(w, "%-20s %-40s %-10s %-24s %s\n", s.Name, s.URL, s.Ref, s.Host, s.Path)
				} else {
					fmt.Fprintf(w, "%-20s %-40s %-10s %s\n", s.Name, s.URL, s.Ref, s.Path)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "include each marketplace's host in the listing")
	return cmd
}

// marketplaceBrowseCmd implements mkt-013: force-refresh a single registered
// marketplace (there is no cache to skip in this MVP -- see design.md
// "快取策略" -- so every browse is already a fresh Fetch) and render its
// Plugin/Description/Version/Install table, followed by a generic
// `apm install <plugin-name>@{name}` usage tip.
func marketplaceBrowseCmd() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:          "browse NAME",
		Short:        "Force-refresh and list the plugins in a registered marketplace",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			src, err := marketplace.FindByName(name)
			if err != nil {
				return err
			}
			if src == nil {
				return fmt.Errorf("marketplace %q is not registered", name)
			}
			m, err := marketplace.Fetch(context.Background(), src)
			if err != nil {
				return fmt.Errorf("could not reach marketplace %q: %w", name, err)
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "%-24s %-40s %-10s %s\n", "PLUGIN", "DESCRIPTION", "VERSION", "INSTALL")
			for _, p := range m.Plugins {
				fmt.Fprintf(w, "%-24s %-40s %-10s apm install %s@%s\n", p.Name, p.Description, p.Version, p.Name, name)
			}
			if verbose {
				fmt.Fprintf(w, "\n%d plugin(s) in %q\n", len(m.Plugins), name)
			}
			fmt.Fprintf(w, "\n[i] apm install <plugin-name>@%s\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print extra diagnostics")
	return cmd
}

// marketplaceUpdateCmd implements mkt-014: a given NAME refreshes only that
// marketplace (a fetch failure is fatal, matching "只刷新一個"); omitting
// NAME refreshes every registered marketplace, logging (not aborting on) any
// individual failure (design.md: "任何一個失敗記診斷、不中斷其餘"). As with
// browse, there is no cache to actually update in this MVP -- "refresh"
// here means "prove the source is still reachable and report its current
// plugin count".
func marketplaceUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "update [NAME]",
		Short:        "Refresh one or every registered marketplace",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			if len(args) == 1 {
				name := args[0]
				src, err := marketplace.FindByName(name)
				if err != nil {
					return err
				}
				if src == nil {
					return fmt.Errorf("marketplace %q is not registered", name)
				}
				m, err := marketplace.Fetch(context.Background(), src)
				if err != nil {
					return fmt.Errorf("refresh marketplace %q: %w", name, err)
				}
				fmt.Fprintf(w, "[+] Refreshed marketplace %q (%d plugins)\n", name, len(m.Plugins))
				return nil
			}

			sources, err := marketplace.LoadRegistry()
			if err != nil {
				return err
			}
			for i := range sources {
				s := sources[i]
				m, ferr := marketplace.Fetch(context.Background(), &s)
				if ferr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "[!] failed to refresh marketplace %q: %v\n", s.Name, ferr)
					continue
				}
				fmt.Fprintf(w, "[+] Refreshed marketplace %q (%d plugins)\n", s.Name, len(m.Plugins))
			}
			return nil
		},
	}
	return cmd
}

// marketplaceRemoveCmd implements mkt-015: -y/--yes skips confirmation
// entirely; otherwise an interactive terminal is prompted (isInteractive/
// confirmPrompt, shared with init.go's confirmation flow), and a
// non-interactive session without -y is a hard error rather than a silent
// no-confirm removal.
func marketplaceRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:          "remove NAME",
		Short:        "Unregister a marketplace",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !yes {
				if !isInteractive() {
					return fmt.Errorf("marketplace remove requires -y/--yes in a non-interactive environment")
				}
				if !confirmPrompt(fmt.Sprintf("Remove marketplace %q?", name), false) {
					fmt.Fprintln(cmd.ErrOrStderr(), "Aborted.")
					return nil
				}
			}
			if err := marketplace.RemoveSource(name); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "[-] Removed marketplace %q\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the interactive confirmation prompt")
	return cmd
}

// marketplaceValidateCmd implements mkt-016: validate an already-registered
// marketplace's manifest (never a local authoring config -- that is a
// producer-side concern, out of this task's scope), printing every finding
// followed by a "Summary: N passed, N warnings, N errors" line, and failing
// (exit 1) when any error was found.
func marketplaceValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "validate NAME",
		Short:        "Validate a registered marketplace's manifest",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			src, err := marketplace.FindByName(name)
			if err != nil {
				return err
			}
			if src == nil {
				return fmt.Errorf("marketplace %q is not registered", name)
			}
			m, err := marketplace.Fetch(context.Background(), src)
			if err != nil {
				return fmt.Errorf("could not reach marketplace %q: %w", name, err)
			}

			findings := marketplace.Validate(m)
			w := cmd.OutOrStdout()
			for _, f := range findings {
				level := "warning"
				if f.Level == marketplace.LevelError {
					level = "error"
				}
				fmt.Fprintf(w, "  [%s] %s\n", level, f.Message)
			}
			passed, warnings, errs := summarizeFindings(m, findings)
			fmt.Fprintf(w, "Summary: %d passed, %d warnings, %d errors\n", passed, warnings, errs)
			if errs > 0 {
				return fmt.Errorf("marketplace %q failed validation with %d error(s)", name, errs)
			}
			return nil
		},
	}
	return cmd
}

// summarizeFindings turns Validate's flat []Finding slice into the
// passed/warnings/errors counts validate's Summary line reports. "passed" is
// counted against a fixed unit count (the manifest name check, plus one unit
// per plugin) minus the number of errors, floored at zero -- Validate does
// not expose which specific check(s) each finding came from, so this is an
// approximation of "how much of the manifest came back clean", not a
// literal per-check tally.
func summarizeFindings(m *marketplace.MarketplaceManifest, findings []marketplace.Finding) (passed, warnings, errs int) {
	for _, f := range findings {
		if f.Level == marketplace.LevelError {
			errs++
		} else {
			warnings++
		}
	}
	total := 1 + len(m.Plugins)
	passed = total - errs
	if passed < 0 {
		passed = 0
	}
	return passed, warnings, errs
}

// marketplaceBuildCmd implements mkt-019: `marketplace build` was removed
// upstream in favor of `apm pack`; this tombstone keeps the subcommand name
// resolvable (so a stale script/doc gets a clear pointer instead of cobra's
// generic "unknown command" error) but always fails.
func marketplaceBuildCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "build",
		Short:        "Removed: use 'apm pack' instead",
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("'marketplace build' has been removed; use 'apm pack' instead")
		},
	}
}
