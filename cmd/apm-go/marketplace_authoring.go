package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/marketplace/authoring"
	"github.com/apm-go/apm/internal/ux"
	"github.com/apm-go/apm/internal/yamlcore"
	"github.com/spf13/cobra"
)

// marketplaceGitignorePatterns are exact-match .gitignore lines that would
// silently untrack a generated marketplace.json output (mkt-040's
// --no-gitignore-check warning), mirroring Python apm's
// commands/marketplace/__init__.py::_check_gitignore_for_marketplace_json.
var marketplaceGitignorePatterns = map[string]bool{
	"marketplace.json":                 true,
	"**/marketplace.json":              true,
	"/marketplace.json":                true,
	".claude-plugin/marketplace.json":  true,
	".agents/plugins/marketplace.json": true,
	"*.json":                           true,
}

// marketplaceInitCmd implements mkt-040: scaffold a marketplace: block into
// apm.yml (creating apm.yml first, with a minimal shell, if it does not
// exist yet). The scaffold is spliced in surgically -- appended to the tail
// of apm.yml when it has no marketplace: key yet, or spliced into just that
// key's value span via yamlcore.PatchMappingPath under --force -- never by
// re-encoding the whole file (see prd.md's Notes on the PatchMappingPath
// lesson from the --mcp task).
func marketplaceInitCmd() *cobra.Command {
	var (
		force            bool
		noGitignoreCheck bool
		name             string
		owner            string
		verbose          bool
	)

	cmd := &cobra.Command{
		Use:          "init",
		Short:        "Add a 'marketplace:' block to apm.yml (scaffolds apm.yml if missing)",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			scaffoldedApmYML := false

			if _, statErr := os.Stat("apm.yml"); os.IsNotExist(statErr) {
				shell := authoring.RenderMinimalApmYMLShell(name)
				if err := os.WriteFile("apm.yml", []byte(shell), 0o644); err != nil {
					return fmt.Errorf("write apm.yml: %w", err)
				}
				scaffoldedApmYML = true
			} else if statErr != nil {
				return fmt.Errorf("stat apm.yml: %w", statErr)
			}

			src, err := os.ReadFile("apm.yml")
			if err != nil {
				return fmt.Errorf("read apm.yml: %w", err)
			}

			doc, err := yamlcore.SafeLoad(src)
			if err != nil {
				return fmt.Errorf("parse apm.yml: %w", err)
			}
			var root *yamllib.Node
			if len(doc.Content) > 0 {
				root = doc.Content[0]
			}
			if root != nil && root.Kind != yamllib.MappingNode {
				return fmt.Errorf("apm.yml must be a YAML mapping at the top level")
			}

			out, err := spliceMarketplaceBlock(src, doc, root, authoring.RenderInitBlock(owner), force)
			if err != nil {
				return err
			}

			if err := os.WriteFile("apm.yml", out, 0o644); err != nil {
				return fmt.Errorf("write apm.yml: %w", err)
			}

			if scaffoldedApmYML {
				// Oracle commands/marketplace/init.py:97 uses
				// logger.success(..., symbol="check"), hence "[+]".
				ux.Check(w, "Created apm.yml with 'marketplace:' block")
			} else {
				// Oracle commands/marketplace/init.py:99 uses
				// logger.success(..., symbol="check"), hence "[+]".
				ux.Check(w, "Added 'marketplace:' block to apm.yml")
			}
			if verbose {
				cwd, cerr := os.Getwd()
				if cerr == nil {
					ux.List(w, []ux.Item{{Text: fmt.Sprintf("Path: %s", filepath.Join(cwd, "apm.yml"))}})
				}
			}

			if !noGitignoreCheck {
				warnIfGitignoreIgnoresMarketplaceJSON(cmd.ErrOrStderr())
			}

			// Bordered box, not the plain ux.Section+BulletList this used to
			// be: upstream renders this exact step list inside a Rich Panel
			// (border_style="cyan", title=" Next Steps",
			// commands/marketplace/init.py:117-123) with a plain-text
			// fallback only when Rich itself is unavailable
			// (utils/console.py:175-191's `except (ImportError, NameError)`
			// branch) -- not a routine TTY/non-TTY choice. ux.Box is the
			// existing plain (non-interactive, no huh/clack dependency --
			// AC53/D13 requires marketplace init to stay non-interactive)
			// bordered-box primitive already used for init's own "About to
			// create" summary shape; it does not reproduce Rich's exact
			// glyphs or TTY-conditional fallback (that gap is parent
			// prd.md's Out of Scope, ~80-150 LOC), only that a border exists
			// where upstream has one.
			ux.Box(w, "Next steps", []string{
				"1. Edit the 'marketplace:' block in apm.yml to add your packages",
				"2. Run 'apm-go pack' to generate .claude-plugin/marketplace.json",
				"3. Add 'codex' to marketplace.outputs to also generate .agents/plugins/marketplace.json",
				"4. Commit apm.yml and the generated marketplace file(s)",
			})
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing 'marketplace:' block in apm.yml")
	cmd.Flags().BoolVar(&noGitignoreCheck, "no-gitignore-check", false, "Skip the .gitignore staleness check")
	cmd.Flags().StringVar(&name, "name", "", "Marketplace/package name (default: my-marketplace)")
	cmd.Flags().StringVar(&owner, "owner", "", "Owner name for the marketplace (default: acme-org)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed output")
	return cmd
}

// spliceMarketplaceBlock decides, then performs, how blockText gets into
// apm.yml's bytes:
//
//   - No "marketplace" key in root at all -> append blockText to the tail
//     of src as raw text (appendMarketplaceBlock). Every existing byte is
//     untouched.
//   - "marketplace" key present with an explicit null value (a bare
//     "marketplace:" with nothing after it) -> treated the same as init's
//     own mkt-047 "_has_marketplace_block" semantics: not really present,
//     so this always proceeds (no --force needed), replacing just that
//     key's value span via yamlcore.PatchMappingPath.
//   - "marketplace" key present with a non-null value -> requires --force;
//     without it, an error is returned and apm.yml is left untouched. With
//     it, the value span is replaced the same way as the null case.
func spliceMarketplaceBlock(src []byte, doc, root *yamllib.Node, blockText string, force bool) ([]byte, error) {
	if root == nil {
		return appendMarketplaceBlock(src, blockText), nil
	}

	keyIdx := findTopLevelKey(root, "marketplace")
	if keyIdx == -1 {
		return appendMarketplaceBlock(src, blockText), nil
	}

	valNode := root.Content[keyIdx+1]
	if !isExplicitNull(valNode) && !force {
		return nil, fmt.Errorf("apm.yml already has a 'marketplace:' block. Use --force to overwrite.")
	}

	newValNode, err := parseBlockValueNode(blockText)
	if err != nil {
		return nil, fmt.Errorf("render marketplace block: %w", err)
	}
	root.Content[keyIdx+1] = newValNode

	patched, ok, err := yamlcore.PatchMappingPath(src, doc, []string{"marketplace"})
	if err != nil {
		return nil, fmt.Errorf("write apm.yml: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("apm.yml's existing 'marketplace:' block has a structure init cannot surgically overwrite; remove it manually and re-run")
	}
	return patched, nil
}

// findTopLevelKey returns the Content index of key's key-node within
// mapping node m ("m.Content[idx+1]" is the paired value), or -1.
func findTopLevelKey(m *yamllib.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// isExplicitNull reports whether v is an explicit YAML null scalar (the
// value of a bare "key:" with nothing after it).
func isExplicitNull(v *yamllib.Node) bool {
	return v.Kind == yamllib.ScalarNode && v.Tag == "!!null"
}

// parseBlockValueNode parses blockText (a single "marketplace: ..."
// document, as rendered by authoring.RenderInitBlock) and returns its
// "marketplace" key's value node, ready to be spliced into another
// document's tree via yamlcore.PatchMappingPath.
func parseBlockValueNode(blockText string) (*yamllib.Node, error) {
	doc, err := yamlcore.SafeLoad([]byte(blockText))
	if err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("rendered marketplace block is empty")
	}
	root := doc.Content[0]
	idx := findTopLevelKey(root, "marketplace")
	if idx == -1 {
		return nil, fmt.Errorf("rendered marketplace block is missing its 'marketplace:' key")
	}
	return root.Content[idx+1], nil
}

// appendMarketplaceBlock appends blockText to the end of src as raw text
// (mkt-040): a newline is inserted first if src doesn't already end in one,
// and blockText's line endings are normalized to CRLF when src itself is
// CRLF -- so the appended block doesn't leave a mixed-EOL document. Every
// existing byte of src survives untouched (舊坑 1: this must hold even
// against a hand-formatted apm.yml). No blank-line separator is written:
// the Oracle round-trips the document through ruamel and dumps the new
// `marketplace:` key directly after the last top-level key
// (commands/marketplace/init.py:85-91), so the scaffold and the Oracle's
// apm.yml are byte-identical apart from the ruled `# ref:` example line
// (2026-08-29, ticket 32).
func appendMarketplaceBlock(src []byte, blockText string) []byte {
	crlf := bytes.Contains(src, []byte("\r\n"))
	nl := "\n"
	if crlf {
		nl = "\r\n"
	}

	var buf bytes.Buffer
	buf.Write(src)
	if len(src) > 0 && src[len(src)-1] != '\n' {
		buf.WriteString(nl)
	}

	block := blockText
	if crlf {
		block = strings.ReplaceAll(block, "\n", "\r\n")
	}
	buf.WriteString(block)
	return buf.Bytes()
}

// marketplaceCheckCmd implements mkt-041: verify every remote package's
// pinned ref or version range genuinely exists on its remote via `git
// ls-remote` (authoring.CheckPackages/authoring.DefaultRefLister). Local
// (./...) packages never touch the network, but their resolved path is now
// stat-ed on disk and must exist (ticket 20 AC4). Any failure returns a
// non-nil error, which main()'s root.Execute() error path turns into exit 1
// (mkt-041's "任何失敗 exit 1" -- no distinct exit code needed here, unlike
// package add/remove/set's exit 2 in a later step).
func marketplaceCheckCmd() *cobra.Command {
	var offline, verbose bool

	cmd := &cobra.Command{
		Use:          "check",
		Short:        "Verify every marketplace package's pinned ref/version exists on its remote",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, src, err := authoring.LoadAuthoringConfig(".")
			if err != nil {
				return err
			}
			if src == authoring.ConfigSourceLegacy {
				ux.Warn(cmd.ErrOrStderr(), "reading legacy marketplace.yml; run 'apm-go marketplace migrate' to fold it into apm.yml")
			}

			// C6 (defence-in-depth, mirrors Python's
			// _warn_duplicate_names): a case-insensitive duplicate package
			// name is a non-fatal warning, never a check failure -- printed
			// unconditionally before ref/version resolution, and never
			// contributing to `failed` below.
			for _, w := range authoring.DuplicatePackageNames(cfg) {
				ux.Warn(cmd.ErrOrStderr(), "%s", w)
			}

			w := cmd.OutOrStdout()
			if offline {
				// Upstream check.py:69-73's offline-mode notice.
				ux.Info(w, "Offline mode -- only schema and cached-ref checks")
			}

			results := authoring.CheckPackages(".", cfg, authoring.DefaultRefLister, offline)
			failed := 0
			// Upstream's Entry Health Check table (__init__.py:1246-1287):
			// one row per entry -- passing entries included -- with the
			// Reachable/Version Found/Ref OK classification columns.
			tableRows := make([][]string, len(results))
			for i, r := range results {
				detail := "OK"
				if r.Err != nil {
					failed++
					detail = r.Err.Error()
				}
				tableRows[i] = []string{
					checkBoolSymbol(r.RefOK), r.Package.Name,
					checkBoolSymbol(r.Reachable), checkBoolSymbol(r.VersionFound), checkBoolSymbol(r.RefOK),
					detail,
				}
			}
			ux.Table(w, []string{"STATUS", "PACKAGE", "REACHABLE", "VERSION FOUND", "REF OK", "DETAIL"}, tableRows)
			if failed > 0 {
				return fmt.Errorf("check failed: %d/%d package(s) have an unverifiable pin", failed, len(results))
			}
			// Oracle commands/marketplace/audit.py:105 uses symbol="check"
			// for an all-clean summary.
			ux.Check(w, "all %d package(s) verified", len(results))
			return nil
		},
	}

	cmd.Flags().BoolVar(&offline, "offline", false, "fail packages with a pinned ref/version instead of contacting the network")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print a line for every package, not just failures")
	return cmd
}

// checkBoolSymbol renders one Entry Health Check boolean cell.
func checkBoolSymbol(ok bool) string {
	if ok {
		return ux.SymbolSuccess
	}
	return ux.SymbolError
}

// marketplaceOutdatedCmd implements mkt-042 修訂版: report every package's
// upgrade status against real git tags (authoring.OutdatedPackages/
// authoring.DefaultRefLister), printing one line per package with its
// status icon, and returning a non-nil error (exit 1) only when at least
// one row's Upgradable field is set -- never by inspecting which icon was
// displayed, since a [*] row's exit-code contribution depends on which
// icon it would have been before being overridden (see OutdatedRow's own
// doc comment).
//
// The Current column comes from ./marketplace.json in the working directory
// (loadCurrentMarketplaceVersions), mirroring upstream's
// _load_current_versions (__init__.py:1133-1148): a missing or unparsable
// file degrades to "--" for every row, never an error.
func marketplaceOutdatedCmd() *cobra.Command {
	var offline, includePrerelease, verbose bool

	cmd := &cobra.Command{
		Use:          "outdated",
		Short:        "Show marketplace packages with available upgrades",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, src, err := authoring.LoadAuthoringConfig(".")
			if err != nil {
				return err
			}
			if src == authoring.ConfigSourceLegacy {
				ux.Warn(cmd.ErrOrStderr(), "reading legacy marketplace.yml; run 'apm-go marketplace migrate' to fold it into apm.yml")
			}

			rows := authoring.OutdatedPackages(cfg, authoring.DefaultRefLister, offline, includePrerelease, loadCurrentMarketplaceVersions())

			w := cmd.OutOrStdout()
			upgradable := 0
			tableRows := make([][]string, len(rows))
			for i, r := range rows {
				rangeSpec := r.Package.Version
				if rangeSpec == "" || r.Package.Ref != "" {
					rangeSpec = "--"
				}
				tableRows[i] = []string{
					outdatedStatusSymbol(r.Status), r.Package.Name, r.Current, rangeSpec, r.LatestInRange, r.LatestOverall, r.Note,
				}
				if r.Upgradable {
					upgradable++
				}
			}
			ux.Table(w, []string{"STATUS", "NAME", "CURRENT", "RANGE", "LATEST-IN-RANGE", "LATEST", "NOTE"}, tableRows)

			if upgradable > 0 {
				ux.Info(w, "%d package(s) can be updated", upgradable)
			} else {
				ux.Info(w, "All packages are up to date")
			}
			if verbose {
				ux.List(w, []ux.Item{{Text: fmt.Sprintf("%d upgradable entries", upgradable)}})
			}

			if upgradable > 0 {
				return fmt.Errorf("outdated: %d package(s) have an available upgrade", upgradable)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&offline, "offline", false, "use cached refs only (no network)")
	cmd.Flags().BoolVar(&includePrerelease, "include-prerelease", false, "include prerelease versions when determining the latest tag")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print extra diagnostics")
	return cmd
}

// loadCurrentMarketplaceVersions reads ./marketplace.json (the working
// directory's published manifest) and returns each plugin's pinned
// source.ref by name, for outdated's Current column -- mirroring upstream's
// _load_current_versions (__init__.py:1133-1148). Best-effort: a missing,
// unreadable, or unparsable file returns an empty map (every Current cell
// degrades to "--"), never an error.
func loadCurrentMarketplaceVersions() map[string]string {
	data, err := os.ReadFile("marketplace.json")
	if err != nil {
		return nil
	}
	var doc struct {
		Plugins []struct {
			Name   string          `json:"name"`
			Source json.RawMessage `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	current := make(map[string]string, len(doc.Plugins))
	for _, p := range doc.Plugins {
		var src struct {
			Ref string `json:"ref"`
		}
		// A string-form source has no ref; mirror upstream's dict-only read.
		if err := json.Unmarshal(p.Source, &src); err != nil || src.Ref == "" {
			continue
		}
		current[p.Name] = src.Ref
	}
	return current
}

// outdatedStatusSymbol maps authoring.OutdatedRow.Status's bracket token to
// the cmd layer's +/!/i/x symbol set for display. refcheck.go's own Status
// field is left as "[+]"/"[!]"/"[*]"/"[i]"/"[x]" -- internal/marketplace/
// authoring's tests assert those literal values -- so this mapping happens
// only here, at render time.
func outdatedStatusSymbol(status string) string {
	switch status {
	case "[+]":
		return ux.SymbolSuccess
	case "[!]", "[*]":
		return ux.SymbolWarn
	case "[i]":
		return ux.SymbolInfo
	case "[x]":
		return ux.SymbolError
	default:
		return status
	}
}

// warnIfGitignoreIgnoresMarketplaceJSON prints a warning to w when the
// current directory's .gitignore has a rule that would ignore `apm pack`'s
// marketplace.json output(s); it is a no-op (not an error) when .gitignore
// is absent or unreadable.
func warnIfGitignoreIgnoresMarketplaceJSON(w io.Writer) {
	data, err := os.ReadFile(".gitignore")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if marketplaceGitignorePatterns[trimmed] {
			ux.Warn(w, "Your .gitignore ignores marketplace.json. Track apm.yml plus generated "+
				"marketplace files such as .claude-plugin/marketplace.json and .agents/plugins/marketplace.json. "+
				"Remove the .gitignore rule or add explicit unignore entries.")
			return
		}
	}
}
