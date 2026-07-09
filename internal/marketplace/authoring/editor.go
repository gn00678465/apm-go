// This file (editor.go) implements mkt-045/046: the shared "surgically
// edit marketplace.packages[]" machinery behind `apm marketplace package
// add/remove/set`.
//
// Every mutation follows the same shape (editPackagesFile, the package's
// single write path -- Review Gate A requires there be no second one):
//  1. Locate which file is authoritative (mkt-047's rule, shared with
//     schema.go's LoadAuthoringConfig via the two sentinel errors declared
//     there).
//  2. Compute the edited bytes via yamlcore.SpliceSequenceElement, falling
//     back to a whole-packages-value replace via yamlcore.PatchMappingPath
//     only when the splice itself declines (a flow-style sequence, no
//     existing element to derive indentation from, or no packages: key at
//     all yet) -- design.md's fallback chain. Never a full-document
//     re-encode.
//  3. Validate the *edited bytes* still parse to a valid AuthoringConfig,
//     in memory, before ever writing them to disk.
//  4. Atomic-write (temp+fsync+rename).
//
// Because validation (step 3) happens before the write (step 4), a failing
// edit never touches the file on disk at all -- design.md's "Go 版小幅改
// 良": the same net effect Python's write-then-validate-then-restore-
// original achieves, reached by simply never writing a bad version in the
// first place.
package authoring

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

// ── locating the file to edit (mkt-047, shared with schema.go) ─────────────

// locateEditableConfig applies mkt-047's mutual-exclusion rule (reusing
// schema.go's fileExists/loadApmMarketplaceBlock -- the actual rule
// implementation, not a reimplementation of it) to find which file
// `apm marketplace package add/remove/set` should edit: its filesystem
// path, and the yaml.Node key-path prefix leading to the marketplace block
// within it (nil for a legacy marketplace.yml, whose document root *is*
// the block already; []string{"marketplace"} for apm.yml).
func locateEditableConfig(dir string) (path string, prefix []string, err error) {
	apmPath := filepath.Join(dir, apmYMLFilename)
	legacyPath := filepath.Join(dir, legacyYMLFilename)

	legacyExists, err := fileExists(legacyPath)
	if err != nil {
		return "", nil, err
	}
	_, apmBlock, err := loadApmMarketplaceBlock(apmPath)
	if err != nil {
		return "", nil, err
	}

	switch {
	case apmBlock != nil && legacyExists:
		return "", nil, errMarketplaceConfigsMutuallyExclusive
	case apmBlock != nil:
		return apmPath, []string{"marketplace"}, nil
	case legacyExists:
		return legacyPath, nil, nil
	default:
		return "", nil, errNoMarketplaceConfig
	}
}

// ── the shared edit + validate + write primitive ────────────────────────

// packageEditValidate is exposed as a package-level var so a test can force
// a failure -- proving the "never write a bad edit to disk" contract
// (implement.md step 5's "注入寫後驗證失敗 -> 檔案內容回到原文" requirement)
// without having to organically construct edited output that happens to
// fail validation.
var packageEditValidate = validateEditedPackageBytes

// validateEditedPackageBytes re-parses out (the full file content editSequence
// is about to write) the same way LoadAuthoringConfig would, navigating
// down through prefix to the marketplace block and running it through
// parseAuthoringNode -- reusing schema.go's own parser (including its
// req-mf-017 manifest.ValidateMarketplaceSource call for every package's
// source) rather than re-implementing validation here.
func validateEditedPackageBytes(out []byte, prefix []string) error {
	doc, err := yamlcore.SafeLoad(out)
	if err != nil {
		return fmt.Errorf("edited config does not parse: %w", err)
	}
	if len(doc.Content) == 0 {
		return fmt.Errorf("edited config is empty")
	}
	node := doc.Content[0]
	for _, key := range prefix {
		node = mappingValue(node, key)
		if node == nil {
			return fmt.Errorf("edited config is missing the expected %q key", key)
		}
	}
	if _, err := parseAuthoringNode(node, topLevelFields{}, len(prefix) == 0); err != nil {
		return fmt.Errorf("edited config failed validation: %w", err)
	}
	return nil
}

// packagesSequenceNode locates the packages: sequence node within doc,
// following prefix from the document root (nil prefix -> the document
// root itself is the marketplace block, i.e. a legacy marketplace.yml).
// When the key is absent, a fresh empty sequence node is created and
// appended to the mapping's Content in-memory -- its key node's Line
// field defaults to 0, the same "newly created, not really present yet"
// signal yamlcore.SpliceSequenceElement/PatchMappingPath's own missing-key
// handling already relies on, so this creation never disturbs
// SpliceSequenceElement's "doc must be unmutated" contract for the (far
// more common) case where packages: already exists: this function makes
// no change to doc at all in that case.
//
// A "packages" key that exists but isn't a SequenceNode (e.g. an explicit
// null) cannot reach this function in practice: every caller loads and
// validates the config via LoadAuthoringConfig first, and schema.go's own
// parsePackages already hard-errors on a non-sequence packages: value, so
// AddPackage/SetPackage/RemovePackage return that error before ever
// calling editPackagesFile.
func packagesSequenceNode(doc *yaml.Node, prefix []string) (*yaml.Node, error) {
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("marketplace config must be a YAML mapping")
	}
	cur := root
	for _, key := range prefix {
		v := mappingValue(cur, key)
		if v == nil || v.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("marketplace config: %q is not a mapping", key)
		}
		cur = v
	}
	if v := mappingValue(cur, "packages"); v != nil && v.Kind == yaml.SequenceNode {
		return v, nil
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "packages"}
	seqNode := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	cur.Content = append(cur.Content, keyNode, seqNode)
	return seqNode, nil
}

// editPackagesFile performs one SpliceSequenceElement edit against dir's
// active config file, falling back to a whole-packages-value
// PatchMappingPath replace when the splice declines (design.md's fallback
// chain -- see this file's own doc comment), memory-validates the result,
// and atomic-writes it. mutateFallback is invoked only on the fallback
// path (seq located, but the splice couldn't apply): it must mutate seq's
// Content in place to reach the desired end state (e.g. append a new
// element for `add`).
func editPackagesFile(dir string, op yamlcore.SeqOp, idx int, newNode *yaml.Node, mutateFallback func(seq *yaml.Node)) (fallbackUsed bool, err error) {
	path, prefix, err := locateEditableConfig(dir)
	if err != nil {
		return false, err
	}

	src, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	doc, err := yamlcore.SafeLoad(src)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}

	seq, err := packagesSequenceNode(doc, prefix)
	if err != nil {
		return false, err
	}

	fullPath := append(append([]string{}, prefix...), "packages")

	out, ok, spliceErr := yamlcore.SpliceSequenceElement(src, doc, fullPath, op, idx, newNode)
	if spliceErr != nil {
		return false, spliceErr
	}
	if !ok {
		// Falling back to a whole-value replace: normalize away any
		// leftover flow style from the existing (or freshly created)
		// sequence node, since PatchMappingPath is about to re-render the
		// entire value fresh in block style regardless.
		seq.Style = 0
		mutateFallback(seq)
		var patchOk bool
		out, patchOk, err = yamlcore.PatchMappingPath(src, doc, fullPath)
		if err != nil {
			return false, err
		}
		if !patchOk {
			return false, fmt.Errorf("unable to edit %s: the packages: block has a structure this editor cannot surgically edit or fall back to overwriting", path)
		}
		fallbackUsed = true
	}

	if err := packageEditValidate(out, prefix); err != nil {
		return false, fmt.Errorf("edit produced an invalid config, aborting without writing: %w", err)
	}
	if err := atomicWriteFile(path, out); err != nil {
		return false, err
	}
	return fallbackUsed, nil
}

// atomicWriteFile writes data to path via a temp file in the same
// directory, fsync'd and renamed over the destination (mkt-045's "atomic
// write(temp+fsync+rename)").
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file for %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("fsync temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("commit write to %s: %w", path, err)
	}
	return nil
}

// ── rendering a PackageEntry as a *yaml.Node ────────────────────────────

// packageEntryNode renders entry as a *yaml.Node mapping for use as
// yamlcore.SpliceSequenceElement/PatchMappingPath's newNode: field order
// and presence mirror Python apm's add_plugin_entry/update_plugin_entry
// (yml_editor.py) -- name, source, whichever of version/ref is set,
// subdir, tag_pattern, include_prerelease (only when true), tags (only
// when non-empty). description/category are carried through as-is (needed
// for `set`'s "unspecified fields keep their existing value" contract),
// but neither add nor set exposes a flag to change them -- design.md's
// flag table has no --description/--category (mkt-053's codex `category`
// gate belongs to the not-yet-landed `apm pack` sub-task).
func packageEntryNode(entry PackageEntry) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	putStr := func(key, value string) {
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
		)
	}
	putStr("name", entry.Name)
	putStr("source", entry.Source)
	if entry.Description != "" {
		putStr("description", entry.Description)
	}
	if entry.Version != "" {
		putStr("version", entry.Version)
	}
	if entry.Ref != "" {
		putStr("ref", entry.Ref)
	}
	if entry.Subdir != "" {
		putStr("subdir", entry.Subdir)
	}
	if entry.TagPattern != "" {
		putStr("tag_pattern", entry.TagPattern)
	}
	if entry.IncludePrerelease {
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "include_prerelease"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"},
		)
	}
	if entry.Category != "" {
		putStr("category", entry.Category)
	}
	if len(entry.Tags) > 0 {
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
		for _, tag := range entry.Tags {
			seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: tag})
		}
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "tags"},
			seq,
		)
	}
	return n
}

// ── mkt-046: source verification / naming helpers ───────────────────────

// verifyPackageSource implements mkt-046's fix for `package add`: a local
// (./) source is always OK without ever touching the network -- regardless
// of noVerify -- reusing refcheck.go's own isLocalPackageSource rule so
// `check`/`outdated`/`package add` all agree on what counts as local. A
// remote source is checked with a single lister.ListRefs call unless
// noVerify skips it.
func verifyPackageSource(source string, lister RefLister, noVerify bool) error {
	if isLocalPackageSource(source) {
		return nil
	}
	if noVerify {
		return nil
	}
	if _, err := lister.ListRefs(source); err != nil {
		return fmt.Errorf("source %q is not reachable: %w", source, err)
	}
	return nil
}

// defaultNameFromSource derives a package name from source's final path
// segment when --name is not given (add's own default), mirroring Python's
// _default_name_from_source: trim a trailing "/", trim a trailing ".git",
// then take the text after the last "/".
func defaultNameFromSource(source string) string {
	s := strings.TrimSuffix(source, "/")
	s = strings.TrimSuffix(s, ".git")
	if i := strings.LastIndex(s, "/"); i != -1 {
		return s[i+1:]
	}
	return s
}

// validateSubdir rejects a --subdir value that could escape the package
// root, mirroring Python's yml_editor._validate_subdir ->
// path_security.validate_path_segments(subdir, context="subdir"): any "."
// or ".." path segment (POSIX or Windows separators) is rejected outright,
// regardless of whether it nets to an actual escape -- Python's own guard
// is this strict, and mirroring it (rather than apm-go's existing, laxer
// net-depth escape checks elsewhere) is required here (S2 security fix).
// An absolute path (POSIX "/...", Windows "C:\..." or "\...") is rejected
// as well: an escaping/absolute subdir here has nothing to be relative to.
func validateSubdir(subdir string) error {
	norm := strings.ReplaceAll(subdir, "\\", "/")
	if strings.HasPrefix(norm, "/") || filepath.IsAbs(subdir) || filepath.VolumeName(filepath.FromSlash(subdir)) != "" {
		return fmt.Errorf("invalid subdir %q: absolute paths are not allowed", subdir)
	}
	for _, seg := range strings.Split(norm, "/") {
		if seg == "." || seg == ".." {
			return fmt.Errorf("invalid subdir %q: segment %q is a traversal sequence", subdir, seg)
		}
	}
	return nil
}

func findPackageIndex(cfg *AuthoringConfig, name string) int {
	lower := strings.ToLower(name)
	for i, pkg := range cfg.Packages {
		if strings.ToLower(pkg.Name) == lower {
			return i
		}
	}
	return -1
}

// ── public API: add / set / remove ──────────────────────────────────────

// AddOptions holds `apm marketplace package add`'s flags (mkt-045
// 修訂版's add-only column: --name and --subdir's -s shorthand belong only
// here, not to SetOptions/`set`).
type AddOptions struct {
	Name              string
	Version           string
	Ref               string
	Subdir            string
	TagPattern        string
	Tags              []string
	IncludePrerelease bool
	NoVerify          bool
}

// AddPackage implements `apm marketplace package add SOURCE` (mkt-045):
// append a new packages[] entry. mkt-046's fix lives entirely in how
// source verification is skipped for a local source (see
// verifyPackageSource) -- unlike Python's add_plugin_entry, neither
// Version nor Ref is required: prd.md AC3's explicit regression scenario
// (`package add ./pkgs/tool` with zero flags) must succeed.
//
// Returns the resolved package name (opts.Name, or -- when empty --
// derived from source's final path segment via defaultNameFromSource),
// whether editPackagesFile's whole-value fallback was used (so callers can
// warn), or a non-nil error for: --version/--ref both given, an invalid
// source (req-mf-017, via manifest.ValidateMarketplaceSource), an
// unreachable remote source, a duplicate (case-insensitive) name, or a
// write/validate failure.
func AddPackage(dir, source string, opts AddOptions, lister RefLister) (name string, fallbackUsed bool, err error) {
	if opts.Version != "" && opts.Ref != "" {
		return "", false, fmt.Errorf("--version and --ref are mutually exclusive; use --version for a semver range or --ref for a git ref")
	}
	if opts.Subdir != "" {
		if err := validateSubdir(opts.Subdir); err != nil {
			return "", false, err
		}
	}
	if err := manifest.ValidateMarketplaceSource(source); err != nil {
		return "", false, err
	}
	if err := verifyPackageSource(source, lister, opts.NoVerify); err != nil {
		return "", false, err
	}

	cfg, _, err := LoadAuthoringConfig(dir)
	if err != nil {
		return "", false, err
	}

	name = opts.Name
	if name == "" {
		name = defaultNameFromSource(source)
	}
	if findPackageIndex(cfg, name) != -1 {
		return "", false, fmt.Errorf("package %q already exists", name)
	}

	newNode := packageEntryNode(PackageEntry{
		Name:              name,
		Source:            source,
		Version:           opts.Version,
		Ref:               opts.Ref,
		Subdir:            opts.Subdir,
		TagPattern:        opts.TagPattern,
		Tags:              opts.Tags,
		IncludePrerelease: opts.IncludePrerelease,
	})

	fallbackUsed, err = editPackagesFile(dir, yamlcore.SeqAdd, -1, newNode, func(seq *yaml.Node) {
		seq.Content = append(seq.Content, newNode)
	})
	if err != nil {
		return "", false, err
	}
	return name, fallbackUsed, nil
}

// SetOptions holds `apm marketplace package set`'s flags (mkt-045).
// Every field is a pointer so nil means "flag not given, leave the
// existing value alone" -- including IncludePrerelease, which design.md
// calls out by name as needing this three-state behavior (add's own
// --include-prerelease is a plain bool flag; set's is not). Tags follows
// the same nil-means-untouched convention: a non-nil (even empty) slice
// means --tags was given.
type SetOptions struct {
	Version           *string
	Ref               *string
	Subdir            *string
	TagPattern        *string
	Tags              []string
	IncludePrerelease *bool
}

// SetPackage implements `apm marketplace package set NAME` (mkt-045):
// update only the fields opts explicitly provides on an existing
// packages[] entry (case-insensitive name match), fully re-rendering that
// one element (yamlcore.SeqSet). Giving both Version and Ref is rejected;
// giving one clears the other in storage, mirroring Python's
// update_plugin_entry.
func SetPackage(dir, name string, opts SetOptions) (fallbackUsed bool, err error) {
	if opts.Version != nil && opts.Ref != nil {
		return false, fmt.Errorf("--version and --ref are mutually exclusive; use --version for a semver range or --ref for a git ref")
	}
	if opts.Subdir != nil {
		if err := validateSubdir(*opts.Subdir); err != nil {
			return false, err
		}
	}

	cfg, _, err := LoadAuthoringConfig(dir)
	if err != nil {
		return false, err
	}
	idx := findPackageIndex(cfg, name)
	if idx == -1 {
		return false, fmt.Errorf("package %q not found", name)
	}

	merged := cfg.Packages[idx]
	if opts.Version != nil {
		merged.Version = *opts.Version
		merged.Ref = ""
	}
	if opts.Ref != nil {
		merged.Ref = *opts.Ref
		merged.Version = ""
	}
	if opts.Subdir != nil {
		merged.Subdir = *opts.Subdir
	}
	if opts.TagPattern != nil {
		merged.TagPattern = *opts.TagPattern
	}
	if opts.Tags != nil {
		merged.Tags = opts.Tags
	}
	if opts.IncludePrerelease != nil {
		merged.IncludePrerelease = *opts.IncludePrerelease
	}

	newNode := packageEntryNode(merged)
	return editPackagesFile(dir, yamlcore.SeqSet, idx, newNode, func(seq *yaml.Node) {
		if idx < len(seq.Content) {
			seq.Content[idx] = newNode
		}
	})
}

// RemovePackage implements `apm marketplace package remove NAME` (mkt-045):
// delete a packages[] entry by case-insensitive name match. The --yes/-y
// confirmation gate is a CLI-layer (terminal) concern -- see
// cmd/apm/marketplace_package.go -- not performed here.
func RemovePackage(dir, name string) (fallbackUsed bool, err error) {
	cfg, _, err := LoadAuthoringConfig(dir)
	if err != nil {
		return false, err
	}
	idx := findPackageIndex(cfg, name)
	if idx == -1 {
		return false, fmt.Errorf("package %q not found", name)
	}

	return editPackagesFile(dir, yamlcore.SeqRemove, idx, nil, func(seq *yaml.Node) {
		if idx < len(seq.Content) {
			seq.Content = append(seq.Content[:idx], seq.Content[idx+1:]...)
		}
	})
}
