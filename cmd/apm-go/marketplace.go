package main

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"sort"
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
// 016, plus mkt-043 修訂版's audit). It keeps the original callers' exact
// "is not registered" substring (existing tests assert on it verbatim with
// strings.Contains), then layers on two best-effort UX aids the bare message
// never had: `marketplace add OWNER/REPO` registers under a *derived* alias
// (resolveMarketplaceAlias/fallbackMarketplaceAlias), never the raw
// OWNER/REPO string itself, so a user who later queries with that same raw
// string gets an unhelpful "not registered" with no hint of what name it
// actually registered under.
//   - if name looks like a copy-pasted "OWNER/REPO" (it contains a "/"),
//     the part after the last "/" is compared case-insensitively against
//     every registered name; a match appends a "Did you mean" hint.
//   - the full, sorted list of registered names is appended, or -- when
//     nothing is registered at all -- a pointer at `marketplace add` instead.
//
// Both aids are best-effort: a LoadRegistry failure here must not replace an
// already-correct "not registered" error with a different, confusing one, so
// it silently falls back to the plain message instead of propagating.
func marketplaceNotRegisteredErr(name string) error {
	msg := fmt.Sprintf("marketplace %q is not registered", name)

	sources, err := marketplace.LoadRegistry()
	if err != nil {
		return fmt.Errorf("%s", msg)
	}

	names := make([]string, 0, len(sources))
	for _, s := range sources {
		names = append(names, s.Name)
	}
	sort.Strings(names)

	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		candidate := name[idx+1:]
		for _, n := range names {
			if strings.EqualFold(n, candidate) {
				msg += fmt.Sprintf(". Did you mean %q?", n)
				break
			}
		}
	}

	if len(names) == 0 {
		msg += " (no marketplaces registered; add one with: apm-go marketplace add SOURCE)"
	} else {
		msg += "\nRegistered: " + strings.Join(names, ", ")
	}
	return fmt.Errorf("%s", msg)
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

			w := cmd.OutOrStdout()
			ux.Success(w, "Added marketplace %q (kind: %s)", effectiveName, src.Kind())
			if verbose {
				ux.BulletList(w, []ux.Item{
					{Text: fmt.Sprintf("source: %s", src.URL)},
					{Text: fmt.Sprintf("ref: %s", src.Ref)},
					{Text: fmt.Sprintf("plugins: %d", len(m.Plugins))},
				})
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
				ux.Info(w, "No marketplaces registered. Add one with: apm-go marketplace add SOURCE")
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
				return marketplaceNotRegisteredErr(name)
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
					return marketplaceNotRegisteredErr(name)
				}
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
				return marketplaceNotRegisteredErr(name)
			}
			if !yes {
				// C10: confirmOrRequireYes (not the old bare
				// isInteractive()+confirmPrompt combo) ensures a failed
				// confirmation read (EOF, or any other scanner error) is
				// never conflated with "user declined" -- it requires
				// -y/--yes instead, the same as an outright non-interactive
				// session.
				proceed, err := confirmOrRequireYes(
					fmt.Sprintf("Remove marketplace %q?", name),
					"marketplace remove requires -y/--yes in a non-interactive environment",
				)
				if err != nil {
					return err
				}
				if !proceed {
					ux.Info(cmd.ErrOrStderr(), "Aborted.")
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
				return marketplaceNotRegisteredErr(name)
			}
			m, err := marketplace.Fetch(context.Background(), src)
			if err != nil {
				return fmt.Errorf("could not reach marketplace %q: %w", name, err)
			}

			w := cmd.OutOrStdout()
			if verbose {
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

			findings := marketplace.Validate(m)
			if len(findings) > 0 {
				items := make([]ux.Item, len(findings))
				for i, f := range findings {
					icon := ux.SymbolWarn
					if f.Level == marketplace.LevelError {
						icon = ux.SymbolError
					}
					items[i] = ux.Item{Text: fmt.Sprintf("%s %s", icon, f.Message)}
				}
				ux.BulletList(w, items)
			}
			passed, warnings, errs := summarizeFindings(m, findings)
			ux.Info(w, "Summary: %d passed, %d warnings, %d errors", passed, warnings, errs)
			if errs > 0 {
				return fmt.Errorf("marketplace %q failed validation with %d error(s)", name, errs)
			}
			return nil
		},
	}
	// C1: doc's marketplace.md:283-285 promises --verbose/-v on every
	// subcommand; validate was missing it entirely (an unknown-flag hard
	// error).
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print each plugin's source type before the validation results")
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
		Short:        "Removed: use 'apm-go pack' instead",
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("'marketplace build' has been removed; use 'apm-go pack' instead")
		},
	}
}
