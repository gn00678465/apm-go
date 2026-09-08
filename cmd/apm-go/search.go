package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/apm-go/apm/internal/marketplace"
	"github.com/apm-go/apm/internal/ux"
	"github.com/spf13/cobra"
)

// searchDescriptionMaxLen is the rich-table Description column's truncation
// threshold (Oracle __init__.py:1417-1419: `if len(desc) > 60: desc =
// desc[:57] + "..."`).
const searchDescriptionMaxLen = 60

// searchTableHeaders mirrors the Oracle's rich.Table columns
// (__init__.py:1412-1415): Plugin/Description/Install -- no Version column,
// unlike `marketplace browse`'s table (marketplace_browse_table.go).
var searchTableHeaders = []string{"Plugin", "Description", "Install"}

// searchCmd implements mkt-060: a TOP-LEVEL `apm-go search QUERY@MARKETPLACE`
// command (registered directly on root in main.go, deliberately NOT nested
// under marketplaceCmd() -- see marketplace.go's package doc comment), ported
// from Oracle commands/marketplace/__init__.py:1351-1444's `search`.
//
// The Oracle registers that command at the TOP LEVEL ONLY -- cli.py:224's
// `cli.add_command(marketplace_search, name="search")`, and nowhere else.
// Unlike every other command in that file, it is declared with a bare
// `@click.command(...)` rather than `@marketplace.command(...)`, so it is
// never attached to the `marketplace` group: `apm marketplace search` does
// not exist. The Oracle's own `search --help` agrees, printing
// `Usage: apm search [OPTIONS] QUERY@MARKETPLACE`.
//
// Yet the Oracle's three hint strings (__init__.py:1361, 1371, 1379) are
// hardcoded to `apm marketplace search security@skills` -- an invocation
// that would fail if a user typed it. It is an upstream copy-paste slip, not
// a real invocation surface. apm-go's hints below therefore read `apm-go
// search ...`/`apm-go marketplace list`, naming the command path that
// actually works, rather than transliterating a dead one. This is the
// ticket's only sanctioned wording deviation; every other message is copied
// verbatim (with `apm-go` in place of `apm`).
//
// Recorded in tools/parity/waivers.json for search-missing-at,
// search-empty-query, and search-empty-marketplace. An earlier version of
// this comment claimed two things that were not true, both corrected here
// (ticket 23): that the Oracle also registers the command as `apm
// marketplace search`, and that search-unknown-marketplace carries a waiver
// on this difference -- that case's waiver is about the separate
// "Marketplace '%s' is not registered..." string at search.go:82. Ticket 23
// itself proposed "fixing" apm-go's wording to match the Oracle's; it was
// closed as invalid on the evidence above.
func searchCmd() *cobra.Command {
	var limit int
	var verbose bool

	cmd := &cobra.Command{
		Use:          "search QUERY@MARKETPLACE",
		Short:        "Search plugins in a marketplace (QUERY@MARKETPLACE)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			expression := args[0]

			// Oracle __init__.py:1361-1365.
			idx := strings.LastIndex(expression, "@")
			if idx < 0 {
				return fmt.Errorf(
					"Invalid format: '%s'. Use QUERY@MARKETPLACE, e.g.: apm-go search security@skills",
					expression,
				)
			}

			// Oracle __init__.py:1367-1372: split on the LAST '@', so a
			// query that itself contains '@' (e.g. "foo@bar@skills") keeps
			// "foo@bar" as the query and "skills" as the marketplace.
			query, marketplaceName := expression[:idx], expression[idx+1:]
			if query == "" || marketplaceName == "" {
				return fmt.Errorf(
					"Both QUERY and MARKETPLACE are required. Use QUERY@MARKETPLACE, e.g.: apm-go search security@skills",
				)
			}

			// Oracle __init__.py:1374-1379 (get_marketplace_by_name /
			// MarketplaceNotFoundError).
			src, err := marketplace.FindByName(marketplaceName)
			if err != nil {
				return err
			}
			if src == nil {
				return fmt.Errorf(
					"Marketplace '%s' is not registered. Use 'apm-go marketplace list' to see registered marketplaces.",
					marketplaceName,
				)
			}

			w := cmd.OutOrStdout()

			// Oracle __init__.py:1392 (logger.start(f"Searching '{marketplace_name}'
			// for '{query}'...", symbol="search")): printed once the registry lookup
			// has succeeded, before the fetch -- a fetch failure still shows it, since
			// it narrates the fetch itself starting, not its outcome.
			ux.Running(w, "Searching '%s' for '%s'...", marketplaceName, query)

			// Oracle __init__.py:1382-1383 (search_marketplace -> fetch_marketplace).
			m, err := marketplace.Fetch(context.Background(), src)
			if err != nil {
				if verbose {
					ux.List(cmd.ErrOrStderr(), []ux.Item{{Text: err.Error()}})
				}
				return fmt.Errorf("Search failed: %w", err)
			}

			var results []marketplace.MarketplacePlugin
			for _, p := range m.Plugins {
				if pluginMatchesSearchQuery(p, query) {
					results = append(results, p)
				}
			}
			if limit < 0 {
				limit = 0
			}
			if limit < len(results) {
				results = results[:limit]
			}

			// Oracle __init__.py:1385-1389.
			if len(results) == 0 {
				ux.Warn(w,
					"No plugins found matching '%s' in '%s'. Try 'apm-go marketplace browse %s' to see all plugins.",
					query, marketplaceName, marketplaceName,
				)
				return nil
			}

			// Oracle __init__.py:1405-1424 (rich table). Ticket 10 decision
			// (A): the Oracle's _get_console() never returns a
			// plain-fallback console in a normal install, so the table path
			// is unconditional -- ux.Table already always renders
			// box-drawing (lipgloss downsamples color only, never the
			// border characters), so NO_COLOR/CI/no-TTY still gets a table,
			// just without ANSI. __init__.py:1397-1403's colorama fallback
			// has no apm-go equivalent.
			rows := make([][]string, 0, len(results))
			for _, p := range results {
				rows = append(rows, []string{p.Name, truncateSearchDescription(p.Description), p.Name + "@" + marketplaceName})
			}
			fmt.Fprintln(w)
			renderSearchTable(w, fmt.Sprintf("Search Results: '%s' in %s", query, marketplaceName), rows)
			ux.Info(w, "Install: apm-go install <plugin-name>@%s", marketplaceName)
			return nil
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "Max results to show")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed output")
	return cmd
}

// pluginMatchesSearchQuery ports MarketplacePlugin.matches_query
// (marketplace/models.py:375-382): a case-insensitive substring match over
// name, description, or any tag.
func pluginMatchesSearchQuery(p marketplace.MarketplacePlugin, query string) bool {
	q := strings.ToLower(query)
	if strings.Contains(strings.ToLower(p.Name), q) {
		return true
	}
	if strings.Contains(strings.ToLower(p.Description), q) {
		return true
	}
	for _, tag := range p.Tags {
		if strings.Contains(strings.ToLower(tag), q) {
			return true
		}
	}
	return false
}

// truncateSearchDescription mirrors __init__.py:1417-1419: "--" for an empty
// description, otherwise truncated to 57 runes + "..." past
// searchDescriptionMaxLen. Runes, not bytes, so a multi-byte description
// truncates at the same character count the Oracle's Python `len()` would.
func truncateSearchDescription(desc string) string {
	if desc == "" {
		return "--"
	}
	if utf8.RuneCountInString(desc) <= searchDescriptionMaxLen {
		return desc
	}
	r := []rune(desc)
	return string(r[:57]) + "..."
}

// renderSearchTable prints the search results table: a title line followed
// by a boxed ux.Table. Like renderBrowseTable (marketplace_browse_table.go),
// this does not reproduce rich's HEAVY_HEAD box styling byte-for-byte
// (design.md accepts the visual difference) -- unlike browse's Description
// column, search's is already capped at searchDescriptionMaxLen runes, so no
// further word-wrapping is needed here.
func renderSearchTable(w io.Writer, title string, rows [][]string) {
	fmt.Fprintln(w, title)
	ux.Table(w, searchTableHeaders, rows)
}
