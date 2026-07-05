package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/marketplace/authoring"
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
				fmt.Fprintln(w, "[+] Created apm.yml with 'marketplace:' block")
			} else {
				fmt.Fprintln(w, "[+] Added 'marketplace:' block to apm.yml")
			}
			if verbose {
				cwd, cerr := os.Getwd()
				if cerr == nil {
					fmt.Fprintf(w, "    Path: %s\n", filepath.Join(cwd, "apm.yml"))
				}
			}

			if !noGitignoreCheck {
				warnIfGitignoreIgnoresMarketplaceJSON(cmd.ErrOrStderr())
			}

			fmt.Fprintln(w, "\nNext steps:")
			fmt.Fprintln(w, "  1. Edit the 'marketplace:' block in apm.yml to add your packages")
			fmt.Fprintln(w, "  2. Run 'apm pack' to generate .claude-plugin/marketplace.json")
			fmt.Fprintln(w, "  3. Add 'codex' to marketplace.outputs to also generate .agents/plugins/marketplace.json")
			fmt.Fprintln(w, "  4. Commit apm.yml and the generated marketplace file(s)")
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
// followed by a blank-line separator, and blockText's line endings are
// normalized to CRLF when src itself is CRLF -- so the appended block
// doesn't leave a mixed-EOL document. Every existing byte of src survives
// untouched (舊坑 1: this must hold even against a hand-formatted apm.yml).
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
	if len(src) > 0 {
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
// (./...) packages always pass without touching the network. Any failure
// returns a non-nil error, which main()'s root.Execute() error path turns
// into exit 1 (mkt-041's "任何失敗 exit 1" -- no distinct exit code needed
// here, unlike package add/remove/set's exit 2 in a later step).
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
				fmt.Fprintln(cmd.ErrOrStderr(), "[warn] reading legacy marketplace.yml; run 'apm marketplace migrate' to fold it into apm.yml")
			}

			results := authoring.CheckPackages(cfg, authoring.DefaultRefLister, offline)
			w := cmd.OutOrStdout()
			failed := 0
			for _, r := range results {
				if r.Err != nil {
					failed++
					fmt.Fprintf(w, "[x] %s: %v\n", r.Package.Name, r.Err)
					continue
				}
				if verbose {
					fmt.Fprintf(w, "[+] %s: ok\n", r.Package.Name)
				}
			}
			if failed > 0 {
				return fmt.Errorf("check failed: %d/%d package(s) have an unverifiable pin", failed, len(results))
			}
			fmt.Fprintf(w, "[+] all %d package(s) verified\n", len(results))
			return nil
		},
	}

	cmd.Flags().BoolVar(&offline, "offline", false, "fail packages with a pinned ref/version instead of contacting the network")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print a line for every package, not just failures")
	return cmd
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
// current-version tracking (telling "[+] already up to date" apart from a
// merely-not-yet-published state) is not wired up yet: `apm pack`
// (mkt-050+), the command that would produce a marketplace.json to read
// that from, is a separate, not-yet-landed sub-task. OutdatedPackages is
// called with a nil map, which still reports every other icon correctly.
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
				fmt.Fprintln(cmd.ErrOrStderr(), "[warn] reading legacy marketplace.yml; run 'apm marketplace migrate' to fold it into apm.yml")
			}

			rows := authoring.OutdatedPackages(cfg, authoring.DefaultRefLister, offline, includePrerelease, nil)

			w := cmd.OutOrStdout()
			upgradable := 0
			for _, r := range rows {
				note := ""
				if r.Note != "" {
					note = fmt.Sprintf("  (%s)", r.Note)
				}
				fmt.Fprintf(w, "%s %-20s current=%-10s latest-in-range=%-12s latest=%-12s%s\n",
					r.Status, r.Package.Name, r.Current, r.LatestInRange, r.LatestOverall, note)
				if r.Upgradable {
					upgradable++
				}
			}

			if upgradable > 0 {
				fmt.Fprintf(w, "%d package(s) can be updated\n", upgradable)
			} else {
				fmt.Fprintln(w, "All packages are up to date")
			}
			if verbose {
				fmt.Fprintf(w, "    %d upgradable entries\n", upgradable)
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
			fmt.Fprintln(w, "[warn] Your .gitignore ignores marketplace.json. Track apm.yml plus generated "+
				"marketplace files such as .claude-plugin/marketplace.json and .agents/plugins/marketplace.json. "+
				"Remove the .gitignore rule or add explicit unignore entries.")
			return
		}
	}
}
