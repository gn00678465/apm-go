package main

import (
	"cmp"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/apm-go/apm/internal/ux"
	"github.com/spf13/cobra"
)

// depsTreeCmd is `apm deps tree` (commands/deps/cli.py:821-918).
func depsTreeCmd() *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Show the full dependency tree",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			defer silenceUsageForRuntimeError(cmd, &err)
			return runDepsTree(os.Stdout, global)
		},
	}
	cmd.Flags().BoolVarP(&global, "global", "g", false, "Show user-scope dependency tree (~/.apm/)")
	return cmd
}

var (
	treeRootStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	treeDirectStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	treeDimStyle    = lipgloss.NewStyle().Faint(true)
	treeWarnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

func runDepsTree(w io.Writer, global bool) error {
	scope := projectDepsScope()
	if global {
		g, err := globalDepsScope()
		if err != nil {
			return fmt.Errorf("Error showing dependency tree: %w", err)
		}
		scope = g
	}
	ux.RenderRichTree(w, buildDepsTree(scope.dir))
	return nil
}

// buildDepsTree is _build_dep_tree and the rendering of tree (cli.py:697-918):
// from the lock when it records a dependency, else from a scan of
// apm_modules.
func buildDepsTree(dir string) ux.RichNode {
	name := "my-project"
	if m, err := loadStrictManifest(filepath.Join(dir, "apm.yml")); err == nil {
		name = m.Name
	}
	root := ux.RichNode{Label: treeRootStyle.Render(name) + " (local)"}
	modules := filepath.Join(dir, "apm_modules")
	noDeps := ux.RichNode{Label: "No dependencies installed", Style: treeDimStyle}

	if deps := lockPackageDeps(readScopeLock(dir)); len(deps) > 0 {
		direct, children, unresolved := placeLockDeps(deps)
		if len(direct) == 0 {
			root.Children = append(root.Children, noDeps)
			return root
		}
		for _, d := range direct {
			branch := ux.RichNode{Label: depDisplayName(d), Style: treeDirectStyle}
			if p := oraclePathJoin(modules, d.uniqueKey()); pathExists(p) {
				if s := countPrimitives(p, filepath.ToSlash(p)).summary(); s != "" {
					branch.Children = append(branch.Children, ux.RichNode{Label: s, Style: treeDimStyle})
				}
			}
			branch.Children = append(branch.Children, treeChildren(d.uniqueKey(), children)...)
			root.Children = append(root.Children, branch)
		}
		for _, d := range unresolved {
			branch := ux.RichNode{Label: depDisplayName(d) + " (could not place in tree; run apm install to resolve)", Style: treeWarnStyle}
			branch.Children = treeChildren(d.uniqueKey(), children)
			root.Children = append(root.Children, branch)
		}
		return root
	}

	if !pathExists(modules) {
		root.Children = append(root.Children, noDeps)
		return root
	}
	pkgs := scanModules(modules, func(path string, d fs.DirEntry, parts []string) bool {
		if !isDir(path) || strings.HasPrefix(d.Name(), ".") {
			return false
		}
		if !pathExists(filepath.Join(path, "apm.yml")) && !pathExists(filepath.Join(path, "SKILL.md")) {
			return false
		}
		if len(parts) < 2 || slices.Contains(parts, ".apm") {
			return false
		}
		return !nestedUnderPackage(path, modules, nil)
	})
	// sorted(apm_modules_path.rglob("*")) (cli.py:782): Path order compares
	// the path components.
	slices.SortFunc(pkgs, func(a, b pkgCandidate) int {
		return slices.Compare(strings.Split(a.rel, "/"), strings.Split(b.rel, "/"))
	})
	for _, p := range pkgs {
		branch := ux.RichNode{Label: packageDisplayInfo(p), Style: treeDirectStyle}
		if s := countPrimitives(p.dir, p.rel).summary(); s != "" {
			branch.Children = append(branch.Children, ux.RichNode{Label: s, Style: treeDimStyle})
		}
		root.Children = append(root.Children, branch)
	}
	return root
}

// lockPackageDeps is LockFile.get_package_dependencies (deps/lockfile.py:815-821):
// the dependencies by depth, then repo_url, without the self-entry.
func lockPackageDeps(lock []lockEntry) []lockEntry {
	var deps []lockEntry
	for _, e := range lock {
		if e.LocalPath != "." {
			deps = append(deps, e)
		}
	}
	slices.SortStableFunc(deps, func(a, b lockEntry) int {
		return cmp.Or(cmp.Compare(a.Depth, b.Depth), strings.Compare(a.RepoURL, b.RepoURL))
	})
	return deps
}

// placeLockDeps hangs each transitive dependency under the one dependency of
// the depth above that its resolved_by names, by unique key, else by
// repo_url (cli.py:745-775); one with no single such parent is unresolved.
func placeLockDeps(deps []lockEntry) (direct []lockEntry, children map[string][]lockEntry, unresolved []lockEntry) {
	type slot struct {
		depth int
		key   string
	}
	byKey := map[slot][]lockEntry{}
	byRepo := map[slot][]lockEntry{}
	for _, d := range deps {
		byKey[slot{d.Depth, d.uniqueKey()}] = append(byKey[slot{d.Depth, d.uniqueKey()}], d)
		byRepo[slot{d.Depth, d.RepoURL}] = append(byRepo[slot{d.Depth, d.RepoURL}], d)
	}
	children = map[string][]lockEntry{}
	for _, d := range deps {
		if d.Depth <= 1 {
			direct = append(direct, d)
			continue
		}
		parents := byKey[slot{d.Depth - 1, d.ResolvedBy}]
		if len(parents) == 0 {
			parents = byRepo[slot{d.Depth - 1, d.ResolvedBy}]
		}
		if len(parents) != 1 {
			unresolved = append(unresolved, d)
			continue
		}
		key := parents[0].uniqueKey()
		children[key] = append(children[key], d)
	}
	return direct, children, unresolved
}

// treeChildren is _add_tree_children (cli.py:191-212): every descendant of
// parentKey, with no depth limit. The Oracle's " (circular)" mark cannot
// apply: the lock holds one dependency per unique key (readScopeLock) and a child
// is always one level deeper than its parent, so no dependency is its own
// ancestor.
func treeChildren(parentKey string, children map[string][]lockEntry) []ux.RichNode {
	var nodes []ux.RichNode
	for _, d := range children[parentKey] {
		nodes = append(nodes, ux.RichNode{Label: depDisplayName(d), Style: treeDimStyle, Children: treeChildren(d.uniqueKey(), children)})
	}
	return nodes
}

// depDisplayName is _dep_display_name (cli.py:113-140).
func depDisplayName(d lockEntry) string {
	key := d.uniqueKey()
	if d.Source == "local" {
		switch {
		case d.LocalPath != "" && !isAbsoluteLocalPath(d.LocalPath):
			key = d.LocalPath
		case d.LocalPath != "":
			key = absoluteLocalDisplay(d.LocalPath, d.RepoURL)
		default:
			key = d.RepoURL
		}
	}
	version := d.Version
	if version == "" && d.ResolvedCommit != "" {
		version = d.ResolvedCommit[:min(7, len(d.ResolvedCommit))]
	}
	if version == "" {
		version = d.ResolvedRef
	}
	if version == "" {
		version = "latest"
	}
	return key + "@" + version
}

// isAbsoluteLocalPath is _is_absolute_local_path (cli.py:72-84): a path that
// starts with ~, a POSIX absolute path, or a Windows drive or UNC path.
func isAbsoluteLocalPath(p string) bool {
	return strings.HasPrefix(p, "~") || strings.HasPrefix(p, "/") || isWindowsAbsolute(p)
}

func isWindowsAbsolute(p string) bool {
	if len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') && ('A' <= p[0] && p[0] <= 'Z' || 'a' <= p[0] && p[0] <= 'z') {
		return true
	}
	return strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//")
}

// absoluteLocalDisplay is _absolute_local_display (cli.py:87-94):
// _local/<last path component>, or fallback when the path has none.
func absoluteLocalDisplay(p, fallback string) string {
	seps := "/"
	if isWindowsAbsolute(p) || strings.HasPrefix(p, `~\`) {
		seps = `/\`
	}
	var last string
	for _, part := range strings.FieldsFunc(p, func(r rune) bool { return strings.ContainsRune(seps, r) }) {
		if part != "." {
			last = part
		}
	}
	if seps != "/" && len(last) == 2 && last[1] == ':' {
		last = ""
	}
	if last == "" {
		return fallback
	}
	return "_local/" + last
}

// oraclePathJoin is Python's apm_modules / key: an absolute key replaces the
// whole path, so the Oracle counts the primitives of an absolute local
// dependency in its source directory, and finds nothing for a relative one.
// On Windows the join is PureWindowsPath's, where a drive or UNC path is
// absolute and "/" is a separator too.
func oraclePathJoin(base, key string) string {
	if runtime.GOOS == "windows" {
		return filepath.Clean(ntJoin(base, key))
	}
	if strings.HasPrefix(key, "/") {
		return key
	}
	return filepath.Join(base, filepath.FromSlash(key))
}

// ntJoin is Python 3.12's ntpath.join(base, key): a key with a root replaces
// the path, keeping base's drive when it has none of its own (\x on C:); a
// key with another drive and no root replaces it all; a key on base's drive
// with no root is joined below base.
func ntJoin(base, key string) string {
	drive, root, path := ntSplitRoot(base)
	kDrive, kRoot, kPath := ntSplitRoot(key)
	switch {
	case kRoot != "":
		if kDrive != "" || drive == "" {
			drive = kDrive
		}
		return drive + kRoot + kPath
	case kDrive != "" && !strings.EqualFold(kDrive, drive):
		return key
	case kDrive != "":
		drive = kDrive
	}
	if path != "" && !strings.ContainsRune(`\/`, rune(path[len(path)-1])) {
		path += `\`
	}
	return drive + root + path + kPath
}

// ntSplitRoot is Python 3.12's ntpath.splitroot: a UNC share
// (\\server\share) or "X:" is the drive, a leading separator the root.
func ntSplitRoot(p string) (drive, root, rest string) {
	norm := strings.ReplaceAll(p, "/", `\`)
	switch {
	case strings.HasPrefix(norm, `\\`):
		start := 2
		if len(norm) >= 8 && strings.EqualFold(norm[:8], `\\?\UNC\`) {
			start = 8
		}
		i := strings.IndexByte(norm[start:], '\\')
		if i < 0 {
			return p, "", ""
		}
		i += start
		j := strings.IndexByte(norm[i+1:], '\\')
		if j < 0 {
			return p, "", ""
		}
		j += i + 1
		return p[:j], p[j : j+1], p[j+1:]
	case strings.HasPrefix(norm, `\`):
		return "", p[:1], p[1:]
	case len(norm) >= 2 && norm[1] == ':':
		if len(norm) >= 3 && norm[2] == '\\' {
			return p[:2], p[2:3], p[3:]
		}
		return p[:2], "", p[2:]
	}
	return "", "", p
}

// packageDisplayInfo is _get_package_display_info (_utils.py:209-241):
// <name>@<version>, @unknown without apm.yml, @error for a file that is not a
// YAML mapping.
func packageDisplayInfo(p pkgCandidate) string {
	if !p.hasManifest {
		return filepath.Base(p.dir) + "@unknown"
	}
	if m, err := loadStrictManifest(filepath.Join(p.dir, "apm.yml")); err == nil {
		return m.Name + "@" + m.Version
	}
	if name, version, ok := tolerantIdentity(p.dir); ok {
		return name + "@" + version
	}
	return filepath.Base(p.dir) + "@error"
}
