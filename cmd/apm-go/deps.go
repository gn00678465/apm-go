package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
	"github.com/spf13/cobra"
)

// depsCmd is the Oracle's `apm deps` group (commands/deps/cli.py:436) with
// two of its six subcommands. Owner ruling on issue #82 (2026-10-15): only
// the read-only list and tree are ported; clean and update delete files or
// rewrite the lock, and info and why are left out with them.
func depsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deps",
		Short: "Manage APM package dependencies",
		// Click prints a group's usage to stderr and exits 2 when it is given
		// no subcommand, and refuses an unknown one as a usage error.
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			if len(args) > 0 {
				return withUsageError(fmt.Errorf("No such command '%s'.", args[0]))
			}
			fmt.Fprint(os.Stderr, cmd.UsageString())
			return withSilentExitCode(2, errors.New("missing command"))
		},
	}
	cmd.AddCommand(depsListCmd())
	cmd.AddCommand(depsTreeCmd())
	return cmd
}

// depsScope is one of the two places `deps` reads: the project (cwd) or the
// user scope (~/.apm).
type depsScope struct {
	label string
	dir   string
}

func projectDepsScope() depsScope { return depsScope{label: "Project", dir: "."} }

// globalDepsScope computes ~/.apm without enterGlobalScope, which creates the
// directory and changes into it: deps writes nothing.
func globalDepsScope() (depsScope, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return depsScope{}, fmt.Errorf("cannot determine home directory: %w", err)
	}
	return depsScope{label: "Global", dir: globalDir(home)}, nil
}

// primitiveCounts is the Oracle's _count_primitives (commands/deps/_utils.py:83-121),
// with the skill names behind the skills count.
type primitiveCounts struct {
	prompts, instructions, agents, hooks int
	skills                               []string
}

// summary is _format_primitive_counts (cli.py:42-48): the non-zero counts in
// the order prompts, instructions, agents, skills, hooks.
func (c primitiveCounts) summary() string {
	var parts []string
	for _, kv := range []struct {
		n    int
		name string
	}{{c.prompts, "prompts"}, {c.instructions, "instructions"}, {c.agents, "agents"}, {len(c.skills), "skills"}, {c.hooks, "hooks"}} {
		if kv.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", kv.n, kv.name))
		}
	}
	return strings.Join(parts, ", ")
}

// countPrimitives counts what the Oracle counts, which is not what deploy
// deploys (a root skills/<name>/SKILL.md is not counted). A root SKILL.md is
// the skill deploy names after the last segment of the apm_modules key
// (internal/deploy/primitive.go skillNameFromDepKey), so --skills and the
// Skills column agree. The Oracle names an alias package's root skill after
// the alias, which is that segment too (measured: {git: .../skill.git, alias:
// custom} deploys .claude/skills/custom).
func countPrimitives(dir, rel string) primitiveCounts {
	var c primitiveCounts
	apmDir := filepath.Join(dir, ".apm")
	apmExists := pathExists(apmDir)
	if apmExists {
		c.prompts += countMatches(filepath.Join(apmDir, "prompts"), "*.prompt.md")
		c.instructions += countMatches(filepath.Join(apmDir, "instructions"), "*.md")
		c.agents += countMatches(filepath.Join(apmDir, "agents"), "*.md")
		if entries, err := readDirIfDir(filepath.Join(apmDir, "skills")); err == nil {
			for _, e := range entries {
				sub := filepath.Join(apmDir, "skills", e.Name())
				if isDir(sub) && pathExists(filepath.Join(sub, "SKILL.md")) {
					c.skills = append(c.skills, e.Name())
				}
			}
		}
	}
	c.prompts += countMatches(dir, "*.prompt.md")
	if pathExists(filepath.Join(dir, "SKILL.md")) {
		c.skills = append(c.skills, rel[strings.LastIndex(rel, "/")+1:])
	}
	c.hooks += countMatches(filepath.Join(dir, "hooks"), "*.json")
	if apmExists {
		c.hooks += countMatches(filepath.Join(apmDir, "hooks"), "*.json")
	}
	return c
}

// countMatches is len(list(dir.glob(pattern))) when dir is a directory:
// every entry whose name matches, of any type, dot-names included.
func countMatches(dir, pattern string) int {
	entries, err := readDirIfDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if ok, _ := filepath.Match(pattern, e.Name()); ok {
			n++
		}
	}
	return n
}

func readDirIfDir(dir string) ([]os.DirEntry, error) {
	if !isDir(dir) {
		return nil, fs.ErrNotExist
	}
	return os.ReadDir(dir)
}

// isDir and isFile follow symlinks, as Python's Path.is_dir and is_file do
// (and pathExists, in uninstall.go, as Path.exists).
func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// pkgCandidate is a directory below apm_modules that carries a package
// marker.
type pkgCandidate struct {
	dir         string
	rel         string // slash-separated, relative to apm_modules
	hasManifest bool
}

// scanModules returns each directory below apm_modules that accept keeps, in
// the order Python 3.12's Path.rglob("*") yields it (pathlib.py:228-238):
// the entries of apm_modules, then of each of its subdirectories, then of
// theirs, a directory's subdirectories visited depth first, each directory
// read in the file system's own order and no symlinked directory entered.
// deps list keeps that order, as the Oracle's rglob scan does (cli.py:333);
// deps tree sorts it. A directory that cannot be read is skipped, as
// Path.walk skips it.
func scanModules(modules string, accept func(path string, d fs.DirEntry, parts []string) bool) []pkgCandidate {
	starts := []string{modules}
	var descend func(dir string)
	descend = func(dir string) {
		var sub []string
		for _, e := range readDirInOrder(dir) {
			if e.IsDir() {
				sub = append(sub, filepath.Join(dir, e.Name()))
			}
		}
		starts = append(starts, sub...)
		for _, d := range sub {
			descend(d)
		}
	}
	descend(modules)

	var found []pkgCandidate
	for _, dir := range starts {
		for _, e := range readDirInOrder(dir) {
			path := filepath.Join(dir, e.Name())
			rel, err := filepath.Rel(modules, path)
			if err != nil {
				continue
			}
			parts := strings.Split(filepath.ToSlash(rel), "/")
			if accept(path, e, parts) {
				found = append(found, pkgCandidate{dir: path, rel: strings.Join(parts, "/"), hasManifest: pathExists(filepath.Join(path, "apm.yml"))})
			}
		}
	}
	return found
}

// readDirInOrder lists dir in the order the file system returns it, which is
// the order os.scandir gives Python; os.ReadDir would sort it.
func readDirInOrder(dir string) []fs.DirEntry {
	f, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer f.Close()
	entries, _ := f.ReadDir(-1)
	return entries
}

// nestedUnderPackage is _is_nested_under_package (_utils.py:43-63): a parent
// below apm_modules already is a package. The Oracle's Agent Plugin root
// (plugin.json admitted by its agent_plugins loader) is not recognized:
// apm-go does not install Agent Plugins.
func nestedUnderPackage(dir, modules string) bool {
	for parent := filepath.Dir(dir); parent != modules && parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		if pathExists(filepath.Join(parent, "apm.yml")) || pathExists(filepath.Join(parent, ".apm")) || isFile(filepath.Join(parent, "SKILL.md")) {
			return true
		}
	}
	return false
}

// loadStrictManifest reads an apm.yml as APMPackage.from_apm_yml does
// (models/apm_package.py:646-668, :469-476): a YAML mapping whose name and
// version are non-blank strings, then the rest of the manifest has to parse.
func loadStrictManifest(path string) (*manifest.Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, err
	}
	root := docRoot(doc)
	if root == nil || root.Kind != yamllib.MappingNode {
		return nil, errors.New("apm.yml must contain a YAML object")
	}
	for _, key := range []string{"name", "version"} {
		v := mappingValue(root, key)
		if v == nil {
			return nil, fmt.Errorf("Missing required field '%s' in apm.yml", key)
		}
		if !isPyYAMLString(v) || strings.TrimSpace(v.Value) == "" {
			return nil, fmt.Errorf("Invalid apm.yml identity: '%s' must be a non-empty string", key)
		}
	}
	m, _, err := manifest.ParseManifest(doc)
	return m, err
}

// tolerantIdentity is _tolerant_identity (_utils.py:178-206): the name and
// version of an apm.yml the strict read refused, or ok false when the file is
// not a YAML mapping at all.
func tolerantIdentity(dir string) (name, version string, ok bool) {
	data, err := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if err != nil {
		return "", "", false
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		return "", "", false
	}
	root := docRoot(doc)
	if root == nil || root.Kind != yamllib.MappingNode {
		return "", "", false
	}
	name, version = filepath.Base(dir), "unknown"
	if v := mappingValue(root, "name"); v != nil && isPyYAMLString(v) && strings.TrimSpace(v.Value) != "" {
		name = strings.TrimSpace(v.Value)
	}
	if v := mappingValue(root, "version"); v != nil && isPyYAMLString(v) && strings.TrimSpace(v.Value) != "" {
		version = strings.TrimSpace(v.Value)
	}
	return name, version, true
}

func docRoot(doc *yamllib.Node) *yamllib.Node {
	if doc == nil || doc.Kind != yamllib.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	return doc.Content[0]
}

func mappingValue(m *yamllib.Node, key string) *yamllib.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// isPyYAMLString reports whether PyYAML's YAML 1.1 resolver reads v as a
// str: a plain yes/no/on/off is a bool there and a plain 1_000 an int.
func isPyYAMLString(v *yamllib.Node) bool {
	if v.Kind != yamllib.ScalarNode || v.ShortTag() != "!!str" {
		return false
	}
	plain := v.Style&(yamllib.SingleQuotedStyle|yamllib.DoubleQuotedStyle|yamllib.LiteralStyle|yamllib.FoldedStyle) == 0
	return !plain || !(manifest.PyYAMLBoolRe.MatchString(v.Value) || manifest.PyYAMLNullRe.MatchString(v.Value) || manifest.PyYAMLDecimalIntRe.MatchString(v.Value))
}

// lockEntry is a dependency of the scope's lock as the Oracle's LockFile.read
// sees it: what lockfile.LockedDep models, plus the keys it does not, which
// apm-go's own install never writes and which are read from the lock document
// by position.
type lockEntry struct {
	*lockfile.LockedDep
	alias             string
	declaringParent   string
	anchoredLocalPath string
	insecure          bool
}

// readScopeLock reads <dir>/apm.lock.yaml, or nothing when there is none or it
// cannot be read; the Oracle goes on without it. LockFile.read stores the
// dependencies in a dict by unique key (deps/lockfile.py:791), so a later
// entry with the same key replaces an earlier one in its place.
func readScopeLock(dir string) []lockEntry {
	data, err := os.ReadFile(filepath.Join(dir, "apm.lock.yaml"))
	if err != nil {
		return nil
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil
	}
	lock, err := lockfile.ParseLockfile(doc)
	if err != nil {
		return nil
	}
	var nodes []*yamllib.Node
	if root := docRoot(doc); root != nil && root.Kind == yamllib.MappingNode {
		if deps := mappingValue(root, "dependencies"); deps != nil {
			nodes = deps.Content
		}
	}
	var entries []lockEntry
	at := map[string]int{}
	for i := range lock.Dependencies {
		e := lockEntry{LockedDep: &lock.Dependencies[i]}
		if i < len(nodes) && nodes[i].Kind == yamllib.MappingNode {
			e.alias = scalarValue(nodes[i], "alias")
			e.declaringParent = scalarValue(nodes[i], "declaring_parent")
			e.anchoredLocalPath = scalarValue(nodes[i], "anchored_local_path")
			// The Oracle tests is_insecure for truth (deps/lockfile.py:548).
			if v := mappingValue(nodes[i], "is_insecure"); v != nil {
				e.insecure = pyYAMLTruthy(v)
			}
		}
		key := e.uniqueKey()
		if j, seen := at[key]; seen {
			entries[j] = e
			continue
		}
		at[key] = len(entries)
		entries = append(entries, e)
	}
	return entries
}

func scalarValue(m *yamllib.Node, key string) string {
	if v := mappingValue(m, key); v != nil && v.Kind == yamllib.ScalarNode && v.ShortTag() != "!!null" {
		return strings.TrimSpace(v.Value)
	}
	return ""
}

// uniqueKey is LockedDependency.get_unique_key
// (models/dependency/identity.py:184-235): a local package is
// local:<anchored_local_path> when another local package declared it, else its
// local_path, and a host other than github.com qualifies the key unless the
// package came through a registry proxy. apm-go's LockedDep.UniqueKey is
// host-blind.
func (e lockEntry) uniqueKey() string {
	if e.Source == "local" && e.LocalPath != "" {
		if e.anchoredLocalPath != "" {
			return "local:" + e.anchoredLocalPath
		}
		return e.LocalPath
	}
	key := e.UniqueKey()
	if e.RegistryPrefix != "" {
		return key
	}
	if host := strings.ToLower(strings.TrimSpace(e.Host)); host != "" && host != "github.com" {
		return host + "/" + key
	}
	return key
}

// canonicalString is get_canonical_dependency_string (identity.py:238-261):
// the host-blind name deps list matches against apm_modules.
func (e lockEntry) canonicalString() string {
	if e.Source == "local" && e.LocalPath != "" {
		return e.LocalPath
	}
	return e.UniqueKey()
}

// materializedAt is the part of build_materialization_path
// (models/dependency/materialization.py:114-150) that deps reaches, the only
// placements the Oracle does not derive from the repository: an alias names
// the directory, and a local package sits at _local/<name>, or, when another
// local package declared it, in a slot named after its anchored path so that
// two such packages of one name do not collide. apm-go's install uses neither
// the alias nor the slot, so these are directories the Oracle wrote.
func materializedAt(alias string, local bool, localPath, declaringParent, anchoredLocalPath, repoURL string) string {
	if alias != "" {
		return alias
	}
	if !local || localPath == "" {
		return repoURL
	}
	name := path.Base(strings.TrimRight(localPath, "/"))
	if declaringParent == "" {
		return "_local/" + name
	}
	identity := anchoredLocalPath
	if identity == "" {
		identity = localPath
	}
	sum := sha256.Sum256([]byte(identity))
	return "_local/" + hex.EncodeToString(sum[:])[:12] + "/" + name
}
