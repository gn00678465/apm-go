package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/marketplace"
	"github.com/apm-go/apm/internal/ux"
	"github.com/spf13/cobra"
)

// depsListCmd is `apm deps list` (commands/deps/cli.py:606-694), plus
// apm-go's own --skills (owner ruling on issue #82, 2026-10-15).
func depsListCmd() *cobra.Command {
	var global, all, insecure, skills bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed APM dependencies and their primitives",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			defer silenceUsageForRuntimeError(cmd, &err)
			return runDepsList(os.Stdout, global, all, insecure, skills)
		},
	}
	cmd.Flags().BoolVarP(&global, "global", "g", false, "List user-scope dependencies (~/.apm/) instead of project")
	cmd.Flags().BoolVar(&all, "all", false, "Show both project and user-scope dependencies")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "Show only installed dependencies locked to http:// sources")
	cmd.Flags().BoolVar(&skills, "skills", false, "List each package's skill names below the table (names only; does not filter packages)")
	return cmd
}

// runDepsList prints the scopes in the Oracle's order: --all wins over -g and
// puts one empty line between Project and Global.
func runDepsList(w io.Writer, global, all, insecure, skills bool) error {
	var scopes []depsScope
	switch {
	case all, global:
		g, err := globalDepsScope()
		if err != nil {
			return fmt.Errorf("Error listing dependencies: %w", err)
		}
		if all {
			scopes = append(scopes, projectDepsScope())
		}
		scopes = append(scopes, g)
	default:
		scopes = append(scopes, projectDepsScope())
	}
	width := max(120, ux.TerminalColumns(120))
	for i, s := range scopes {
		if i > 0 {
			fmt.Fprintln(w)
		}
		showScopeDeps(w, s, insecure, skills, width)
	}
	return nil
}

// depsRow is one package of the deps list table.
type depsRow struct {
	name, version, source, origin string
	counts                        primitiveCounts
	insecure                      bool
}

// showScopeDeps is _show_scope_deps (cli.py:484-603). The Oracle's last
// orphan line, "Run 'apm prune' to remove orphaned packages" (cli.py:554,
// :601), is not printed: apm-go has no prune (owner ruling on issue #82,
// 2026-10-16). _report_native_plugins (cli.py:459-481) is not ported: it
// reports Agent Plugins registered with GitHub Copilot, which apm-go never
// registers, so its line cannot appear.
func showScopeDeps(w io.Writer, s depsScope, insecureOnly, skills bool, width int) {
	rows, orphans, ok := resolveScopeDeps(w, s.dir, insecureOnly)
	if !ok {
		ux.Info(w, "No APM dependencies installed (%s scope)", s.label)
		return
	}
	if len(rows) == 0 {
		if insecureOnly {
			ux.Info(w, "No insecure APM dependencies installed (%s scope)", s.label)
		} else {
			ux.Info(w, "apm_modules/ directory exists but contains no valid packages (%s scope)", s.label)
		}
		return
	}

	ux.RenderRichTable(w, depsTable(s.label, rows, insecureOnly), width)
	if skills {
		printSkillsByPackage(w, rows)
	}
	if len(orphans) > 0 {
		ux.Warn(w, "%d orphaned package(s) found (not in resolved dependency graph):", len(orphans))
		for _, o := range orphans {
			ux.Warn(w, "  - %s", o)
		}
	}
}

func depsTable(label string, rows []depsRow, insecureOnly bool) ux.RichTable {
	title := " APM Dependencies (" + label + ")"
	if insecureOnly {
		title = " Insecure APM Dependencies (" + label + ")"
	}
	style := func(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }
	cols := []ux.RichColumn{
		{Header: "Package", Style: style("7").Bold(true)},
		{Header: "Version", Style: style("3")},
		{Header: "Source", Style: style("4")},
	}
	if insecureOnly {
		cols = append(cols, ux.RichColumn{Header: "Origin", Style: style("1").Bold(true)})
	}
	cols = append(cols,
		ux.RichColumn{Header: "Prompts", Justify: ux.RichCenter, Style: style("5")},
		ux.RichColumn{Header: "Instructions", Justify: ux.RichCenter, Style: style("2")},
		ux.RichColumn{Header: "Agents", Justify: ux.RichCenter, Style: style("6")},
		ux.RichColumn{Header: "Skills", Justify: ux.RichCenter, Style: style("3")},
		ux.RichColumn{Header: "Hooks", Justify: ux.RichCenter, Style: style("1")},
	)
	t := ux.RichTable{Title: title, HeaderStyle: style("6").Bold(true), Columns: cols}
	count := func(n int) string {
		if n > 0 {
			return strconv.Itoa(n)
		}
		return "-"
	}
	for _, r := range rows {
		cells := []string{r.name, r.version, r.source}
		if insecureOnly {
			cells = append(cells, r.origin)
		}
		c := r.counts
		cells = append(cells, count(c.prompts), count(c.instructions), count(c.agents), count(len(c.skills)), count(c.hooks))
		t.Rows = append(t.Rows, cells)
	}
	return t
}

// printSkillsByPackage is apm-go's --skills: each table row's skill names,
// sorted, from the same reading the Skills column counts.
func printSkillsByPackage(w io.Writer, rows []depsRow) {
	ux.Info(w, "Skills by package:")
	items := make([]ux.Item, 0, len(rows))
	for _, r := range rows {
		names := slices.Clone(r.counts.skills)
		slices.Sort(names)
		list := "-"
		if len(names) > 0 {
			list = strings.Join(names, ", ")
		}
		items = append(items, ux.Item{Text: r.name + ": " + list})
	}
	ux.List(w, items)
}

// resolveScopeDeps is _resolve_scope_deps (cli.py:215-433): the packages
// installed below <dir>/apm_modules and the sorted names of those no
// dependency declares. ok is false when there is no apm_modules.
func resolveScopeDeps(w io.Writer, dir string, insecureOnly bool) (rows []depsRow, orphans []string, ok bool) {
	modules := filepath.Join(dir, "apm_modules")
	if !pathExists(modules) {
		return nil, nil, false
	}

	declared := map[string]string{} // logical key -> Source column
	var declaredOrder []string
	declare := func(key, source string) {
		if _, seen := declared[key]; !seen {
			declared[key] = source
			declaredOrder = append(declaredOrder, key)
		}
	}
	physicalToLogical := map[string]string{}
	// roots holds the directory of each declared package. apm-go installs a
	// Claude plugin and a skill bundle with neither apm.yml nor a root
	// SKILL.md, where the Oracle writes an apm.yml into a plugin and lists a
	// skill bundle's skills as orphaned (issue #178).
	roots := map[string]bool{}

	// The keys follow the Oracle's placement, not apm-go's install, because
	// the apm_modules read may be the Oracle's. A package apm-go installed
	// elsewhere is still declared by its lock entry, repo_url plus
	// virtual_path, which is where apm-go put it.
	if m, err := loadStrictManifest(filepath.Join(dir, "apm.yml")); err == nil {
		for _, dep := range m.ParsedDeps {
			ref := *dep
			normalizeLocalDep(&ref)
			local := ref.LocalSourcePath != "" || ref.IsLocal
			source := depsSourceLabel(ref.Host, local, ref.Source)
			if alias := strings.TrimSpace(ref.Alias); alias != "" {
				key := ref.RepoURL
				switch {
				case local && ref.LocalDeclaredPath != "":
					key = ref.LocalDeclaredPath
				case ref.VirtualPath != "":
					key += "/" + ref.VirtualPath
				}
				physicalToLogical[alias] = key
				roots[alias] = true
				declare(key, source)
				continue
			}
			if key := manifestDeclaredKey(&ref); key != "" {
				roots[key] = true
				declare(key, source)
			}
		}
	}

	lock := readScopeLock(dir)
	insecureDeps := map[string]lockEntry{}
	lockKeys := map[string]bool{}
	lockVersion := map[string]string{}
	for _, e := range lock {
		key := e.canonicalString()
		physical := key
		if e.Source == "local" || e.alias != "" {
			physical = materializedAt(e.alias, e.Source == "local", e.LocalPath, e.declaringParent, e.anchoredLocalPath, e.RepoURL)
			if e.Source == "local" {
				key = e.RepoURL
			}
			physicalToLogical[physical] = key
		}
		if key != "" {
			roots[physical] = true
			lockVersion[key] = e.Version
			declare(key, depsSourceLabel(e.Host, false, e.Source))
		}
		if e.insecure {
			insecureDeps[key] = e
		}
		if k := e.uniqueKey(); k != "" {
			lockKeys[k] = true
		}
	}

	candidates := scanModules(modules, func(path string, d fs.DirEntry, parts []string) bool {
		if d.Type()&fs.ModeSymlink != 0 || !d.IsDir() {
			return false
		}
		if strings.HasPrefix(d.Name(), ".") && len(parts) > 1 {
			return false
		}
		if !pathExists(filepath.Join(path, "apm.yml")) && !pathExists(filepath.Join(path, "SKILL.md")) && !roots[strings.Join(parts, "/")] {
			return false
		}
		if _, mapped := physicalToLogical[strings.Join(parts, "/")]; len(parts) < 2 && !mapped {
			return false
		}
		if slices.Contains(parts, ".apm") {
			return false
		}
		return !nestedUnderPackage(path, modules, roots)
	})

	var standalone []string
	for _, c := range candidates {
		if lockKeys[c.rel] || c.hasManifest || isFile(filepath.Join(c.dir, "SKILL.md")) {
			standalone = append(standalone, c.rel)
		}
	}
	known := expandWithAncestors(declaredOrder, standalone)

	for _, c := range candidates {
		logical, mapped := physicalToLogical[c.rel]
		if !mapped {
			logical = c.rel
		}
		orphaned := !known[logical]
		display := logical
		if orphaned {
			display = logicalLocalDisplay(logical)
		}
		version := "unknown"
		if c.hasManifest {
			m, err := loadStrictManifest(filepath.Join(c.dir, "apm.yml"))
			if err != nil {
				// The Oracle never forwards the error, which names the
				// absolute apm.yml path (cli.py:418-424).
				ux.Warn(w, "Failed to inspect package %s. Check its package files and rerun.", display)
				continue
			}
			version = m.Version
		} else if v := lockVersion[logical]; v != "" && !pathExists(filepath.Join(c.dir, "SKILL.md")) {
			version = v
		}
		if orphaned {
			orphans = append(orphans, display)
		}
		source := "orphaned"
		if !orphaned {
			source = declared[logical]
			if source == "" {
				source = "github"
			}
		}
		insec, isInsecure := insecureDeps[logical]
		origin := "direct"
		if isInsecure && insec.ResolvedBy != "" {
			origin = "via " + insec.ResolvedBy
		}
		rows = append(rows, depsRow{
			name: display, version: version, source: source, origin: origin,
			counts: countPrimitives(c.dir, c.rel), insecure: isInsecure,
		})
	}

	if insecureOnly {
		rows = slices.DeleteFunc(rows, func(r depsRow) bool { return !r.insecure })
	}
	slices.Sort(orphans)
	return rows, orphans, true
}

// pyYAMLTruthy is Python's bool() of the value PyYAML loads for a scalar.
func pyYAMLTruthy(v *yamllib.Node) bool {
	if v.Kind != yamllib.ScalarNode {
		return len(v.Content) > 0
	}
	if !isPyYAMLString(v) {
		switch strings.ToLower(v.Value) {
		case "false", "no", "off", "", "~", "null":
			return false
		}
		if f, err := strconv.ParseFloat(strings.ReplaceAll(v.Value, "_", ""), 64); err == nil {
			return f != 0
		}
		return true
	}
	return v.Value != ""
}

// manifestDeclaredKey is the key _resolve_scope_deps declares for an apm.yml
// dependency with no alias (cli.py:256-288): owner/repo, or org/project/repo
// on Azure DevOps, a subdirectory package's path below it, and for a virtual
// file package <owner>/<repo>-<file name>, which is where the Oracle puts one
// (models/dependency/materialization.py:171-176). A local package is
// _local/<name>.
func manifestDeclaredKey(ref *manifest.DependencyReference) string {
	if ref.IsParent {
		return ""
	}
	parts := strings.Split(ref.RepoURL, "/")
	if len(parts) < 2 {
		return ""
	}
	n := 2
	if len(parts) >= 3 && ref.Host != "" && marketplace.IsAzureDevOpsHostname(ref.Host) {
		n = 3
	}
	repo := strings.Join(parts[:n], "/")
	switch manifest.VirtualTypeOf(ref.VirtualPath) {
	case "":
		return repo
	case "subdirectory":
		return repo + "/" + ref.VirtualPath
	}
	// get_virtual_package_name (models/dependency/reference.py:302-324).
	file := ref.VirtualPath[strings.LastIndex(ref.VirtualPath, "/")+1:]
	for _, ext := range []string{".prompt.md", ".instructions.md", ".agent.md"} {
		if strings.HasSuffix(file, ext) {
			file = strings.TrimSuffix(file, ext)
			break
		}
	}
	return strings.Join(parts[:n-1], "/") + "/" + parts[len(parts)-1] + "-" + file
}

// depsSourceLabel is _deps_list_source_label (cli.py:50-66). The Oracle also
// counts a host named by ADO_HOST or APM_ADO_HOSTS as Azure DevOps and lets
// GITHUB_HOST claim a host before GitLab; apm-go's host checks do not read
// those variables.
func depsSourceLabel(host string, isLocal bool, source string) string {
	switch {
	case isLocal || source == "local":
		return "local"
	case source == "registry":
		return "registry"
	case host != "" && marketplace.IsAzureDevOpsHostname(host):
		return "azure-devops"
	case host != "" && marketplace.IsGitLabHostname(host):
		return "gitlab"
	}
	return "github"
}

// expandWithAncestors is _expand_with_ancestors (commands/_helpers.py:182-247):
// the declared keys plus their prefixes of two and three segments, except a
// prefix that is itself an installed standalone package nobody declared.
func expandWithAncestors(declared, standalone []string) map[string]bool {
	isDeclared := map[string]bool{}
	for _, p := range declared {
		isDeclared[p] = true
	}
	isStandalone := map[string]bool{}
	for _, p := range standalone {
		isStandalone[p] = true
	}
	expanded := map[string]bool{}
	for _, p := range declared {
		expanded[p] = true
		parts := strings.Split(strings.ReplaceAll(p, `\`, "/"), "/")
		if slices.Contains(parts, ".") || slices.Contains(parts, "..") {
			continue
		}
		for i := 2; i < min(4, len(parts)); i++ {
			ancestor := strings.Join(parts[:i], "/")
			if isStandalone[ancestor] && !isDeclared[ancestor] {
				continue
			}
			expanded[ancestor] = true
		}
	}
	return expanded
}

var localHashSlotRe = regexp.MustCompile(`^(_local)/[0-9a-f]{12}/(.+)$`)

// logicalLocalDisplay is _logical_local_display (cli.py:96-110): an orphaned
// _local/<12 hex>/<name> slot of the Oracle is shown as _local/<name>.
func logicalLocalDisplay(name string) string {
	if m := localHashSlotRe.FindStringSubmatch(name); m != nil {
		return m[1] + "/" + m[2]
	}
	return name
}
