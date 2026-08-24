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
	"github.com/apm-go/apm/internal/ux"
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

// hostFQDNPattern implements C5's `--host` validation: mirrors Python's
// is_valid_fqdn (utils/github_host.py) and internal/marketplace's own
// unexported looksLikeFQDN (source.go) -- labels of alphanumerics/hyphens
// that never start or end with a hyphen, with at least two labels (one
// dot). Duplicated here rather than exported from internal/marketplace
// because this fix's file scope does not include internal/marketplace/
// source.go.
var hostFQDNPattern = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)

func isValidHostFQDN(host string) bool {
	return hostFQDNPattern.MatchString(host)
}

// ── C10: shared remove-confirmation helper ──────────────────────────────
//
// richCheck and confirmFn are swappable function vars (mirroring the
// pre-existing isInteractiveCheck seam) so a test can drive
// confirmOrRequireYes's "genuinely interactive" branch deterministically,
// without needing a real terminal: ux.CanPrompt() itself cannot be forced
// from outside the ux package. richCheck defaults to ux.CanPrompt, which --
// unlike the crude os.Stdin.Stat() ModeCharDevice check isInteractiveCheck
// used to rely on -- performs a real term.IsTerminal() check, so a git-bash
// pipe on Windows (C10's original footgun) is no longer mistaken for an
// interactive terminal in the first place.
//
// richCheck deliberately uses ux.CanPrompt, not ux.IsRich: IsRich() also
// requires NO_COLOR to be unset, which would make a real, TTY-backed
// terminal that merely has NO_COLOR set (nothing to do with whether it can
// answer a yes/no question) hard-require -y/--yes the same as a genuinely
// non-interactive session -- a footgun of its own.
var richCheck = ux.CanPrompt
var confirmFn = ux.Confirm

// confirmOrRequireYes is C10's shared fix for `marketplace remove` and
// `marketplace package remove`'s confirmation gate -- the one place either
// command should call to decide whether a destructive removal without
// -y/--yes may proceed. errMsg is returned verbatim in two cases: the
// session cannot prompt at all (richCheck() false -- not a real terminal on
// stdin/stderr, or running in CI), and -- C10's fix -- richCheck() is true
// but the confirmation prompt itself fails (e.g. the huh form is aborted).
// Before the fix, a failed read was silently treated the same as "declined"
// (Aborted, exit 0) -- a CI/script footgun, since exit 0 reads as success.
// proceed is only true after a prompt that genuinely completes; a prompt
// that completes with "no" returns (false, nil), which the caller renders
// as a clean "Aborted." and a normal exit 0.
func confirmOrRequireYes(label, errMsg string) (proceed bool, err error) {
	if !richCheck() {
		return false, fmt.Errorf("%s", errMsg)
	}
	yes, cerr := confirmFn(label, false)
	if cerr != nil {
		return false, fmt.Errorf("%s", errMsg)
	}
	return yes, nil
}

// marketplaceNotRegisteredErr builds the "not registered" error shared by
// browse/update/validate/remove/audit's NAME lookup miss (mkt-013/014/015/
// 016, plus mkt-043 修訂版's audit): the Oracle's MarketplaceNotFoundError
// (marketplace/errors.py:10-24) is a FIXED-FORMAT message parameterized only
// by name (and host, always "github.com" here via registry.py:117,141's
// default_host()) -- it never varies by registration state or by how many
// other marketplaces are already registered. Each of the 5 commands
// independently catches that same exception and wraps it with its own
// command-specific "Failed to <verb> marketplace: " prefix, verified
// directly against the pinned Oracle for every one of them:
//   - browse:   commands/marketplace/__init__.py:959
//   - update:   commands/marketplace/__init__.py:1005
//   - remove:   commands/marketplace/__init__.py:1045
//   - validate: commands/marketplace/validate.py:90
//   - audit:    commands/marketplace/audit.py:141
//
// Ticket 14 replaced this function's previous apm-go-invented "Did you
// mean"/"Registered: <list>" hints (R6/AC22) with the Oracle's exact wording:
// probed live, the message body never changes shape whether zero, one, or
// many marketplaces are registered, so there is no registry data left to
// consult here at all.
func marketplaceNotRegisteredErr(verb, name string) error {
	return fmt.Errorf(
		"Failed to %s marketplace: Marketplace '%s' is not registered. "+
			"Run 'apm-go marketplace add https://github.com/OWNER/REPO' or "+
			"'apm-go marketplace add OWNER/REPO' to register it, or "+
			"'apm-go marketplace list' to see registered marketplaces.",
		verb, name)
}

// marketplaceCmd wires internal/marketplace's data model, registry and fetch
// clients (built in earlier steps) into the six `apm marketplace` consumer
// subcommands (mkt-010..mkt-016) plus the `build` tombstone (mkt-019), and
// (from internal/marketplace/authoring) the producer-side `init`, `check`,
// `outdated`, `package add/remove/set`, `audit`, and `migrate` subcommands
// (mkt-040, mkt-041, mkt-042 修訂版, mkt-045/046, mkt-043 修訂版, mkt-044 --
// Phase M3's full producer-side command set). Deliberately absent as a
// MARKETPLACE subcommand, per Phase M5 of marketplace-checklist.md: doctor
// (mkt-061, which apm-go does have -- top-level only, main.go's
// root.AddCommand(doctorCmd()), matching the Oracle's own top-level `apm
// doctor`, never nested under marketplace), publish (mkt-062), and a browse
// --json flag (mkt-063). validate's --check-refs is ported as a hidden
// no-op below (ticket 06), not absent. "update" has no "refresh" alias
// (mkt-064). search (mkt-060) is likewise
// never nested here -- it is registered top-level only, in search.go
// (main.go's root.AddCommand(searchCmd())), matching the Oracle's own
// top-level `apm search` alias (cli.py:224) without also exposing a
// redundant `apm-go marketplace search`.
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

			// C5: reject a malformed --host FQDN and an invalid --name
			// before ever touching the network (mirrors Python
			// __init__.py:565-570 and :621-628's placement -- both checks
			// run before the slow probe + fetch).
			if host != "" && !isValidHostFQDN(host) {
				return fmt.Errorf("invalid host %q: expected a valid host FQDN, e.g. github.com", host)
			}
			if name != "" && !isValidMarketplaceAlias(name) {
				return fmt.Errorf("invalid marketplace name %q: names may only contain letters, digits, '.', '_', and '-' (required for apm-go install's plugin@marketplace syntax)", name)
			}

			w := cmd.OutOrStdout()

			// Mirrors upstream __init__.py:630-635: surface progress before
			// the slow probe + fetch (5-30s for generic-git) so the user
			// sees activity instead of staring at a blank terminal.
			provisionalLabel := name
			if provisionalLabel == "" {
				provisionalLabel = fallbackMarketplaceAlias(src)
			}
			ux.Info(w, "Registering marketplace %q...", provisionalLabel)

			wasFullHTTPSSource := strings.HasPrefix(strings.ToLower(rawSource), "https://")
			if needsUnpinnedGitRefWarning(wasFullHTTPSSource, src.Kind(), effectiveRef) {
				ux.Warn(cmd.ErrOrStderr(), "Pin this git marketplace with a #ref (e.g. SOURCE#v1.2.3) to avoid silently tracking a moving branch")
			}

			m, err := marketplace.Fetch(context.Background(), src)
			if err != nil {
				return fmt.Errorf("could not reach marketplace source: %w", err)
			}

			effectiveName, aliasWarning := resolveMarketplaceAlias(name, m.Name, src)
			if aliasWarning != "" {
				ux.Warn(cmd.ErrOrStderr(), "%s", aliasWarning)
			}
			src.Name = effectiveName

			if err := marketplace.AddSource(*src); err != nil {
				return fmt.Errorf("register marketplace: %w", err)
			}

			// Upstream __init__.py:721-724's success line carries the plugin
			// count on the default path, not only under --verbose.
			ux.Success(w, "Marketplace %q registered (%d plugins)", effectiveName, len(m.Plugins))
			if verbose {
				items := []ux.Item{
					{Text: fmt.Sprintf("source: %s", src.URL)},
					{Text: fmt.Sprintf("source type: %s", src.Kind())},
					{Text: fmt.Sprintf("ref: %s", src.Ref)},
					{Text: fmt.Sprintf("alias source: %s", aliasSourceLabel(name, effectiveName, m.Name))},
					{Text: fmt.Sprintf("plugins: %d", len(m.Plugins))},
				}
				if m.Description != "" {
					items = append(items, ux.Item{Text: fmt.Sprintf("description: %s", m.Description)})
				}
				ux.BulletList(w, items)
			}
			// Upstream __init__.py:728-732: when the registered alias came
			// from the manifest (not --name, not the repo-derived fallback),
			// tell the user what name to install against.
			if name == "" && effectiveName != provisionalLabel {
				ux.Info(w, "Install plugins with: apm-go install <plugin>@%s", effectiveName)
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

// aliasSourceLabel names where `marketplace add`'s registered alias came
// from, for --verbose output (upstream __init__.py:678-695's alias_source).
func aliasSourceLabel(explicitName, effectiveName, manifestName string) string {
	switch {
	case explicitName != "":
		return "--name flag"
	case effectiveName == manifestName:
		return fmt.Sprintf("manifest.name (%q)", manifestName)
	case manifestName != "":
		return fmt.Sprintf("derived name (manifest.name %q invalid)", manifestName)
	default:
		return "derived name (manifest.name missing)"
	}
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
				// commands/marketplace/__init__.py:859-862, verified
				// directly against the pinned Oracle (ticket 14): the full
				// sentence including the parenthetical, not apm-go's own
				// shorter "Add one with: ..." phrasing.
				ux.Info(w, "No marketplaces registered. Use 'apm-go marketplace add SOURCE' to register one (OWNER/REPO, HTTPS URL, SSH URL, or local path).")
				return nil
			}
			headers := []string{"NAME", "SOURCE", "REF", "PATH"}
			if verbose {
				headers = []string{"NAME", "SOURCE", "REF", "HOST", "PATH"}
			}
			rows := make([][]string, 0, len(sources))
			for _, s := range sources {
				if verbose {
					rows = append(rows, []string{s.Name, s.URL, s.Ref, s.Host, s.Path})
				} else {
					rows = append(rows, []string{s.Name, s.URL, s.Ref, s.Path})
				}
			}
			ux.Table(w, headers, rows)
			// Upstream __init__.py:883-886's post-table usage hint.
			ux.Info(w, "Use 'apm-go marketplace browse <name>' to see plugins")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "include each marketplace's host in the listing")
	return cmd
}

// marketplaceBrowseCmd implements mkt-013: force-refresh a single registered
// marketplace (there is no cache to skip in this MVP -- see design.md
// "快取策略" -- so every browse is already a fresh Fetch) and render the
// original's rich-style Plugin/Description/Version/Install box table,
// followed by a generic `apm-go install <plugin-name>@{name}` usage tip.
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
				return marketplaceNotRegisteredErr("browse", name)
			}
			w := cmd.OutOrStdout()
			sp := ux.Spinner(w, fmt.Sprintf("Fetching plugins from '%s'...", name))
			m, err := marketplace.Fetch(context.Background(), src)
			if err != nil {
				sp.Fail(fmt.Sprintf("could not reach marketplace %q", name))
				return fmt.Errorf("could not reach marketplace %q: %w", name, err)
			}
			sp.Success(fmt.Sprintf("Fetched %d plugin(s) from '%s'", len(m.Plugins), name))
			if len(m.Plugins) == 0 {
				ux.Warn(w, "Marketplace '%s' has no plugins", name)
				return nil
			}

			rows := make([][]string, 0, len(m.Plugins))
			for _, p := range m.Plugins {
				desc, ver := p.Description, p.Version
				if desc == "" {
					desc = "--"
				}
				if ver == "" {
					ver = "--"
				}
				rows = append(rows, []string{p.Name, desc, ver, p.Name + "@" + name})
			}
			fmt.Fprintln(w)
			renderBrowseTable(w, fmt.Sprintf("Plugins in '%s'", name), rows)
			if verbose {
				ux.BulletList(w, []ux.Item{{Text: fmt.Sprintf("%d plugin(s) in %q", len(m.Plugins), name)}})
			}
			ux.Info(w, "Install a plugin: apm-go install <plugin-name>@%s", name)
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
	var verbose bool
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
					return marketplaceNotRegisteredErr("update", name)
				}
				ux.Info(w, "Refreshing marketplace %q...", name)
				m, err := marketplace.Fetch(context.Background(), src)
				if err != nil {
					return fmt.Errorf("refresh marketplace %q: %w", name, err)
				}
				ux.Success(w, "Refreshed marketplace %q (%d plugins)", name, len(m.Plugins))
				if verbose {
					ux.BulletList(w, []ux.Item{{Text: fmt.Sprintf("source: %s", src.URL)}})
				}
				return nil
			}

			sources, err := marketplace.LoadRegistry()
			if err != nil {
				return err
			}
			// Upstream __init__.py:980-982: an empty registry is reported,
			// never a silent exit-0 with zero output.
			if len(sources) == 0 {
				ux.Info(w, "No marketplaces registered.")
				return nil
			}
			ux.Info(w, "Refreshing %d marketplace(s)...", len(sources))
			for i := range sources {
				s := sources[i]
				m, ferr := marketplace.Fetch(context.Background(), &s)
				if ferr != nil {
					ux.Error(cmd.ErrOrStderr(), "failed to refresh marketplace %q: %v", s.Name, ferr)
					continue
				}
				ux.Success(w, "Refreshed marketplace %q (%d plugins)", s.Name, len(m.Plugins))
				if verbose {
					ux.BulletList(w, []ux.Item{{Text: fmt.Sprintf("source: %s", s.URL)}})
				}
			}
			// Upstream __init__.py:993's closing line.
			ux.Success(w, "Marketplace cache refreshed")
			return nil
		},
	}
	// C1: doc's marketplace.md:283-285 promises --verbose/-v on every
	// subcommand; update was missing it entirely (an unknown-flag hard
	// error). Its effect here mirrors the Python original's own (minimal
	// -- verbose there only adds traceback detail on error): printing each
	// successfully refreshed marketplace's source.
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print each marketplace's source after refreshing")
	return cmd
}

// marketplaceRemoveCmd implements mkt-015: -y/--yes skips confirmation
// entirely; otherwise a genuinely interactive session is prompted via
// confirmOrRequireYes (ux.Confirm), and a non-interactive session without
// -y is a hard error rather than a silent no-confirm removal.
func marketplaceRemoveCmd() *cobra.Command {
	var yes, verbose bool
	cmd := &cobra.Command{
		Use:          "remove NAME",
		Short:        "Unregister a marketplace",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			src, err := marketplace.FindByName(name)
			if err != nil {
				return err
			}
			if src == nil {
				return marketplaceNotRegisteredErr("remove", name)
			}
			if !yes {
				// C10: confirmOrRequireYes (not the old bare
				// isInteractive()+confirmPrompt combo) ensures a failed
				// confirmation read (EOF, or any other scanner error) is
				// never conflated with "user declined" -- it requires
				// -y/--yes instead, the same as an outright non-interactive
				// session.
				// Upstream __init__.py:1023-1026's prompt names the source
				// alongside the alias, and a decline prints "Cancelled".
				proceed, err := confirmOrRequireYes(
					fmt.Sprintf("Remove marketplace %q (%s)?", name, src.URL),
					"marketplace remove requires -y/--yes in a non-interactive environment",
				)
				if err != nil {
					return err
				}
				if !proceed {
					ux.Info(cmd.ErrOrStderr(), "Cancelled")
					return nil
				}
			}
			if err := marketplace.RemoveSource(name); err != nil {
				return err
			}
			ux.Success(cmd.OutOrStdout(), "Removed marketplace %q", name)
			if verbose {
				ux.BulletList(cmd.OutOrStdout(), []ux.Item{{Text: fmt.Sprintf("source: %s", src.URL)}})
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the interactive confirmation prompt")
	// C1: doc's marketplace.md:283-285 promises --verbose/-v on every
	// subcommand; remove was missing it entirely (an unknown-flag hard
	// error).
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print the removed marketplace's source")
	return cmd
}

// marketplaceValidateCmd implements mkt-016: validate an already-registered
// marketplace's manifest (never a local authoring config -- that is a
// producer-side concern, out of this task's scope), printing every finding
// followed by a "Summary: N passed, N warnings, N errors" line, and failing
// (exit 1) when any error was found.
func marketplaceValidateCmd() *cobra.Command {
	var verbose bool
	var checkRefs bool
	cmd := &cobra.Command{
		// Ticket 11: matches validate.py:13's Click `help=` string verbatim
		// -- help_semantic's description_paragraph comparison requires it
		// byte-for-byte, and Click uses this same string for both the
		// parent `marketplace --help` one-liner and validate's own --help.
		Use:          "validate NAME",
		Short:        "Validate marketplace structure and plugin schema",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			src, err := marketplace.FindByName(name)
			if err != nil {
				return err
			}
			if src == nil {
				return marketplaceNotRegisteredErr("validate", name)
			}
			w := cmd.OutOrStdout()
			// Mirrors upstream validate.py:29-36's pre-fetch progress and
			// post-fetch plugin count.
			ux.Info(w, "Validating marketplace %q...", name)
			m, err := marketplace.Fetch(context.Background(), src)
			if err != nil {
				return fmt.Errorf("could not reach marketplace %q: %w", name, err)
			}

			// Per-check rendering, mirroring upstream validate.py:54-80:
			// every check prints a line -- a passing check included -- and
			// the Summary counts passed checks and individual warning/error
			// messages, not an approximation.
			checks := marketplace.ValidateChecks(m)

			// has_structure_errors (validate.py:31-33): gates the plugin
			// count, verbose per-plugin detail, and every OTHER check's
			// passing line below -- a broken manifest means "N plugins"
			// (and Schema/Names having "passed") is misleading noise, not
			// useful signal (ticket 11).
			hasStructureErrors := false
			for _, c := range checks {
				if c.CheckName != "Structure" {
					continue
				}
				for _, f := range c.Findings {
					if f.Level == marketplace.LevelError {
						hasStructureErrors = true
					}
				}
			}

			if !hasStructureErrors {
				ux.Info(w, "Found %d plugins", len(m.Plugins))
			}

			if verbose && !hasStructureErrors {
				// Mirrors Python's validate.py:38-42 per-plugin verbose
				// detail (source type: dict vs string), printed after the
				// fetch and before the validation results.
				items := make([]ux.Item, len(m.Plugins))
				for i, p := range m.Plugins {
					sourceType := "string"
					if _, ok := p.Source.(map[string]any); ok {
						sourceType = "dict"
					}
					items[i] = ux.Item{Text: fmt.Sprintf("%s: source type: %s", p.Name, sourceType)}
				}
				ux.BulletList(w, items)
			}

			// check-refs placeholder: mirrors upstream validate.py:49-54
			// exactly -- results are already computed above, this warning
			// prints before they're rendered, and it performs no ref lookup
			// or network call of its own.
			if checkRefs {
				ux.Warn(w, "Ref checking not yet implemented -- skipping ref reachability checks")
			}

			passed, warnings, errs := 0, 0, 0
			fmt.Fprintln(w)
			ux.Info(w, "Validation Results:")
			for _, check := range checks {
				hasErr, hasWarn := false, false
				for _, f := range check.Findings {
					if f.Level == marketplace.LevelError {
						hasErr = true
					} else {
						hasWarn = true
					}
				}
				switch {
				case !hasErr && !hasWarn:
					// validate.py:60-64: a fully-passing check is skipped
					// entirely (not just uncounted) once Structure itself
					// has already failed -- "Schema: passed" would be
					// misleading when the manifest couldn't even parse.
					if hasStructureErrors {
						continue
					}
					ux.Success(w, "  %s: passed", check.CheckName)
					passed++
				case hasWarn && !hasErr:
					for _, f := range check.Findings {
						ux.Warn(w, "  %s: %s", check.CheckName, f.Message)
						warnings++
					}
				default:
					// validate.py:70-74: errors first, then warnings --
					// Python's ValidationResult keeps them in two separate
					// lists; Findings is one mixed slice, so filter twice
					// to reproduce that grouping regardless of append order.
					for _, f := range check.Findings {
						if f.Level == marketplace.LevelError {
							ux.Error(w, "  %s: %s", check.CheckName, f.Message)
							errs++
						}
					}
					for _, f := range check.Findings {
						if f.Level == marketplace.LevelWarning {
							ux.Warn(w, "  %s: %s", check.CheckName, f.Message)
							warnings++
						}
					}
				}
			}
			fmt.Fprintln(w)
			ux.Info(w, "Summary: %d passed, %d warnings, %d errors", passed, warnings, errs)
			if errs > 0 {
				// validate.py:81-82: `sys.exit(1)` with no additional
				// message -- the Results/Summary lines above already said
				// everything. withSilentExitCode matches that contract
				// (see its own doc comment; first added for ticket 08's
				// doctor, same "[x] <message>" extra-line bug shape here).
				return withSilentExitCode(1, fmt.Errorf("marketplace %q failed validation with %d error(s)", name, errs))
			}
			return nil
		},
	}
	// C1: doc's marketplace.md:283-285 promises --verbose/-v on every
	// subcommand; validate was missing it entirely (an unknown-flag hard
	// error). Description matches validate.py:18's Click option help
	// verbatim (ticket 11: help_semantic requires per-flag description
	// equality, not just the flag/alias/default set).
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed output")
	// Ticket 06: upstream validate.py:16-18 accepts --check-refs as a
	// hidden, not-yet-implemented placeholder (network ref reachability
	// checking); ported as a hidden no-op for CLI surface parity, not a
	// real feature.
	cmd.Flags().BoolVar(&checkRefs, "check-refs", false, "verify version refs are reachable (network) -- not yet implemented")
	if err := cmd.Flags().MarkHidden("check-refs"); err != nil {
		panic(err)
	}
	return cmd
}

// marketplaceBuildCmd implements mkt-019: `marketplace build` was removed
// upstream in favor of `apm pack`; this tombstone keeps the subcommand name
// resolvable (so a stale script/doc gets a clear pointer instead of cobra's
// generic "unknown command" error) but always fails.
func marketplaceBuildCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "build",
		Short:        "Removed: use 'apm-go pack' instead",
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("'marketplace build' has been removed; use 'apm-go pack' instead")
		},
	}
}
