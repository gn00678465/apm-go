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
	"regexp"
	"strings"
	"unicode"

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
// subdir, tag_pattern, include_prerelease (only when true), category (only
// when non-empty), tags (only when non-empty). description and category
// are both carried through as-is (needed for `set`'s "unspecified fields
// keep their existing value" contract). `add` has R10's --category flag
// (marketplace_package.go); `set` does not (upstream set.py has no
// --category/--description -- TestMarketplacePackageSetCmd_HasNoAddOnlyFlags
// locks this), so on an existing entry, category can only ever be carried
// through unchanged by `set`, never written by it.
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
// (./) source never touches the network -- regardless of noVerify -- reusing
// refcheck.go's own isLocalPackageSource rule so `check`/`outdated`/`package
// add` all agree on what counts as local. A remote source is checked with a
// single lister.ListRefs call unless noVerify skips it.
//
// dir is AddPackage's own project-root parameter (NOT the process's cwd --
// AddPackage's callers, including its own unit tests, are not guaranteed to
// have os.Chdir-ed into dir): a local source is resolved and containment-
// checked against dir directly, via resolveLocalSourceAgainstRoot
// (refcheck.go).
//
// BLOCKING 2 (2026-07-31 follow-up, live end-to-end reproduction): a local
// source used to be waved through here with an unconditional `return nil`
// and NO path check at all -- resolveRef's mkt-046 short-circuit
// (classifyRefResolution's skipLocalSource branch) also never resolves or
// validates a local source's path, so `package add ./linked`, where "linked"
// is a directory symlink or Windows junction (the latter needs no special
// privilege to create) pointing outside the project root, was accepted
// outright: no call anywhere in AddPackage ever ran resolveCloneURL's
// containment check for it. A subsequent `pack` then faithfully read the
// escaping target's apm.yml contents into the marketplace.json output --
// mkt-046's "no network access" contract does not mean "no path check": it
// only exempts a local source from the `lister.ListRefs` reachability call,
// not from proving its resolved path stays inside the project root. This now
// reuses resolveCloneURL's shared resolveLocalSourceAgainstRoot (refcheck.go)
// instead of a second, hand-rolled copy of that check.
//
// Ticket 20 (2026-08-25, user-reported): the Oracle's own add_plugin_entry
// never stats a local source at all -- utils/path_security.py:64-82's
// validate_path_segments(source, allow_current_dir=True) runs with
// reject_empty=False, so a trailing separator's empty path segment (e.g.
// `./llm-wiki\`, a Windows/PowerShell tab-completion artifact) passes
// straight through untouched, and marketplace/yml_schema.py:216 places no
// charset/existence requirement on the stored value at all -- the broken
// name and source both ride verbatim into apm.yml, then into `pack`'s
// marketplace.json (marketplace/builder.py:613-628 short-circuits
// `entry.is_local` with no stat either) and `check` reports it REACHABLE
// (commands/marketplace/check.py:121-134, same unconditional local pass).
// This is a DELIBERATE apm-go-only hardening beyond the Oracle, not a parity
// fix -- apm-go refuses outright, before ever writing apm.yml, instead of
// reproducing that gap. --no-verify skips only the existence check below (it
// already means "skip the reachability check" for a remote source); the
// containment/traversal guard above runs unconditionally either way.
func verifyPackageSource(dir, source string, lister RefLister, noVerify bool) error {
	if isLocalPackageSource(source) {
		abs, err := resolveLocalSourceAgainstRoot(dir, source)
		if err != nil {
			return err
		}
		if noVerify {
			return nil
		}
		return verifyLocalSourceExists(source, abs)
	}
	if noVerify {
		return nil
	}
	if _, err := lister.ListRefs(source); err != nil {
		return fmt.Errorf("source %q is not reachable: %w", source, err)
	}
	return nil
}

// verifyLocalSourceExists implements ticket 20 AC1: resolvedPath (already
// containment-checked by resolveLocalSourceAgainstRoot) must exist and be a
// directory, or AddPackage refuses to write the entry at all. Shared with
// checkPackage (refcheck.go), which re-runs this same check at `check` time
// for the same "a local source can be written before its directory exists,
// then never re-verified" reason ResolveLocalSourceAgainstRoot's own doc
// comment already documents for the containment half of this check.
func verifyLocalSourceExists(source, resolvedPath string) error {
	info, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("local source %q does not exist", source)
		}
		return fmt.Errorf("local source %q: %w", source, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("local source %q is not a directory", source)
	}
	return nil
}

// packageNameSeparators are the characters ticket 20 AC3 rejects in a
// package name outright: a path separator can never be a legitimate single
// path segment, on either POSIX or Windows.
const packageNameSeparators = "/\\"

// packageNameProblem names the specific reason packageNameIssue rejected a
// name, as a ready-to-print predicate phrase ("name %s <problem>"). Ticket
// 21 AC1: shared between validatePackageName's own message and AddPackage's
// derived-name diagnostic (packageNameDerivedError below) so both describe
// the SAME violation in the same words, instead of AddPackage re-deriving
// or string-matching validatePackageName's error text.
type packageNameProblem string

const (
	packageNameProblemNone       packageNameProblem = ""
	packageNameProblemDotSegment packageNameProblem = "is not a valid name"
	packageNameProblemSeparator  packageNameProblem = "must not contain a path separator"
	packageNameProblemControl    packageNameProblem = "must not contain whitespace or control characters"
)

// packageNameIssue implements ticket 20 AC3's charset rule:
// marketplace/yml_schema.py:216 places no charset requirement on
// PackageEntry.name at all -- it is only required to be a non-empty string
// -- so a name derived from a source with a trailing separator
// (defaultNameFromSource's `llm-wiki\` in ticket 20's reproducer) or an
// explicit --name containing one rides straight into apm.yml/
// marketplace.json unrejected on both the Oracle and (until now) apm-go.
// This is a DELIBERATE apm-go-only hardening beyond the Oracle, not a
// parity fix: it rejects only names that could never be a legitimate single
// path segment -- a path separator, "." or "..", whitespace, or a control
// character -- deliberately NOT `init`'s much stricter pluginNameRe
// (`^[a-z][a-z0-9-]{0,63}$`, cmd/apm-go/init.go), which would reject
// existing legitimate marketplace package names like "My_Tool".
func packageNameIssue(name string) packageNameProblem {
	switch {
	case name == "." || name == "..":
		return packageNameProblemDotSegment
	case strings.ContainsAny(name, packageNameSeparators):
		return packageNameProblemSeparator
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return packageNameProblemControl
		}
	}
	return packageNameProblemNone
}

// packageNameDiagnosticQuote wraps s in plain double quotes for a
// user-facing diagnostic, with NO escaping at all -- ticket 21 AC3: Go's
// `%q` renders a single backslash as two (`\` -> `\\`), a faithful quoted
// literal but one that makes a user decode Go string-escaping to recognise
// their own `./llm-wiki\` input (ticket 20's reproducer). These strings are
// printed into diagnostics only, never re-parsed, so there is nothing to
// protect by escaping them.
func packageNameDiagnosticQuote(s string) string {
	return `"` + s + `"`
}

// packageNameError renders problem (from packageNameIssue) as an error for
// name itself -- e.g. an explicit --name rejection, where name is already
// the right thing to blame (ticket 21 AC2, unchanged from ticket 20 except
// for the %q -> packageNameDiagnosticQuote fix, AC3).
func packageNameError(name string, problem packageNameProblem) error {
	return fmt.Errorf("package name %s %s", packageNameDiagnosticQuote(name), problem)
}

// validatePackageName rejects name per packageNameIssue's rule, in the
// caller-blaming shape appropriate for an explicit --name (AddPackage's
// derived-name path builds its own, source-blaming message instead --
// packageNameDerivedError below).
func validatePackageName(name string) error {
	if problem := packageNameIssue(name); problem != packageNameProblemNone {
		return packageNameError(name, problem)
	}
	return nil
}

// packageNameDerivedError implements ticket 21 AC1: when the rejected name
// came from defaultNameFromSource (no explicit --name given), the earlier
// message blamed the derived name alone -- e.g. `package name "llm-wiki\\"
// must not contain a path separator` -- which points at nothing the user
// actually typed (they gave `./llm-wiki\`, a source, not a --name). This
// names the source as given instead, and points at the remedy.
func packageNameDerivedError(source, name string, problem packageNameProblem) error {
	return fmt.Errorf("local source %s derives package name %s, which %s; pass --name to set it explicitly",
		packageNameDiagnosticQuote(source), packageNameDiagnosticQuote(name), problem)
}

// ── F4: mutable-ref auto-resolution (marketplace.md:253-254's documented
// promise: "Mutable refs (HEAD, branches) are auto-resolved to a concrete
// SHA at write time") ───────────────────────────────────────────────────

// shaRefPattern matches a concrete 40-hex-char git commit SHA, mirroring
// Python's plugin/__init__.py _SHA_RE. A --ref matching this is already
// concrete and is stored as-is, with no lister call.
var shaRefPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// errCannotResolveHeadOffline is R5/AC18's exact upstream message
// (commands/marketplace/plugin/__init__.py:127-132's sys.exit(2) path):
// HEAD (implicit or explicit) cannot be pinned to a concrete SHA without a
// network call, and --no-verify explicitly forbids that call -- so this is
// a hard failure, not a silent fall-back to storing the mutable ref
// verbatim.
var errCannotResolveHeadOffline = fmt.Errorf("Cannot resolve HEAD ref without network access. Provide an explicit --ref SHA.")

// refResolutionKind is classifyRefResolution's output: which of resolveRef's
// branches a given (source, ref, version, ...) combination takes, computed
// with no lister I/O at all.
type refResolutionKind int

const (
	// refKindNone: nothing to resolve -- a --version range was given, the
	// source is local and skipLocalSource applies (mkt-046, add-only), or
	// resolveRef was not asked to treat a missing ref as implicit HEAD
	// (SetPackage's call shape).
	refKindNone refResolutionKind = iota
	// refKindVerbatim: ref already matches shaRefPattern (a concrete 40-hex
	// SHA) -- stored as-is, no lister call.
	refKindVerbatim
	// refKindHead: ref is "" (implicit) or case-insensitively "HEAD"
	// (explicit), and network resolution is actually possible (noVerify is
	// false) -- resolveRef must call lister.ListRefs and search its result
	// for a "HEAD" entry.
	refKindHead
	// refKindHeadOffline: same ref shape as refKindHead, but noVerify is
	// true -- resolution is impossible without a network call that
	// --no-verify explicitly forbids, so resolveRef must fail with
	// errCannotResolveHeadOffline instead of ever reaching the lister.
	// Split out as its own kind (rather than a noVerify check the caller
	// performs after seeing refKindHead) so this is decided in exactly one
	// place: BLOCKING 2 (external audit round 3, 2026-07-30) found that
	// classifyRefResolution not taking noVerify into account at all let an
	// explicit `--ref HEAD --no-verify` be classified as refKindHead (i.e.
	// "will resolve"), which caused a caller predicting off of that
	// classification (WillResolveMutableRefForAdd) to say a mutable-ref
	// warning should print even though resolveRef was about to hard-fail
	// offline instead.
	refKindHeadOffline
	// refKindNamed: ref is an ordinary tag/branch name -- resolveRef must
	// call lister.ListRefs and search its result for an exact name match.
	refKindNamed
)

// classifyRefResolution is resolveRef's decision tree (design.md §6 --
// local must be checked before implicit-HEAD, or mkt-046's "local source
// never touches the network" contract breaks), extracted into its own pure
// function with no lister I/O.
//
// classifyRefResolution is the one function resolveRef and
// WillResolveMutableRefForAdd both call, rather than each restating this
// decision by hand -- BLOCKING 1 (external audit round 2, 2026-07-30) found
// WillResolveMutableRefForAdd had drifted from resolveRef's actual branches
// once before (it silently omitted the concrete-SHA branch), and nothing
// caught that drift because nothing locked the two decisions together. This
// does not make future drift impossible in an absolute sense -- BLOCKING 2
// (external audit round 3, 2026-07-30) found a second, different gap here:
// this function did not take noVerify as an input at all, so an explicit
// `--ref HEAD --no-verify` was classified as refKindHead ("will resolve")
// when resolveRef would actually hard-fail offline before ever reaching the
// lister. Fixed by adding noVerify as a parameter and a dedicated
// refKindHeadOffline result (see that constant's own doc comment). See
// resolveref_test.go's
// TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct for
// the cross-product test this enables.
//
//  1. version != "" -> refKindNone (a version range pins nothing here).
//  2. skipLocalSource is true (add only, mkt-046) and source is a local
//     ("./") source -> refKindNone, never touches lister. This rule must
//     NOT apply to `set`: install-marketplace-contracts.md:87 documents
//     "`set` always resolves a given ref (no --no-verify escape hatch on
//     set)" with no local-source exemption -- SetPackage passes
//     skipLocalSource=false so an explicit `set --ref` on a local package
//     still resolves via lister.ListRefs like any other source (BLOCKING 2,
//     external audit round 2, 2026-07-30: the add-only short-circuit used to
//     apply unconditionally here, silently clearing an explicitly-given
//     --ref on a local package back to "").
//  3. implicitHeadOnEmpty == false and ref == "" -> refKindNone (SetPackage's
//     call, which only ever resolves a ref the caller explicitly gave, and
//     must not treat an unrelated field update as an implicit-HEAD pin).
//  4. ref is already a concrete 40-hex SHA -> refKindVerbatim.
//  5. ref == "" (implicit) or ref case-insensitively "HEAD" (explicit), and
//     noVerify is false -> refKindHead.
//  6. same ref shape as 5, but noVerify is true -> refKindHeadOffline.
//  7. otherwise (a tag/branch name) -> refKindNamed.
func classifyRefResolution(source, ref, version string, noVerify, implicitHeadOnEmpty, skipLocalSource bool) refResolutionKind {
	if version != "" {
		return refKindNone
	}
	if skipLocalSource && isLocalPackageSource(source) {
		return refKindNone
	}
	if ref == "" && !implicitHeadOnEmpty {
		return refKindNone
	}
	if shaRefPattern.MatchString(ref) {
		return refKindVerbatim
	}
	if ref == "" || strings.EqualFold(ref, "HEAD") {
		if noVerify {
			return refKindHeadOffline
		}
		return refKindHead
	}
	return refKindNamed
}

// resolveRef mirrors Python's plugin/__init__.py _resolve_ref (R5's
// implicit-HEAD parity fix): classifyRefResolution decides which branch
// applies, then this function performs the corresponding lister I/O (if
// any) and returns the resolved ref (or an error, per AC18's exit-2 offline
// case, or a lister/not-found failure).
//
// onExplicitHeadWillResolve, when non-nil, is invoked immediately before (and
// only immediately before) the refKindHead branch performs its actual
// lister.ListRefs call for an EXPLICITLY-given "HEAD"/"head" ref (ref != "").
// It is the mutable-ref-warning hook for both `add` (R5/AC19,
// AddOptions.OnExplicitHeadWillResolve) and `set` (BLOCKING 3, external
// audit round 4, 2026-07-30: SetOptions.OnExplicitHeadWillResolve --
// upstream warns on `set --ref HEAD` too, commands/marketplace/plugin/
// set.py:80 calling the same _resolve_ref plugin/__init__.py:120-137 warns
// from; SetPackage used to hardcode nil here, so `set` never warned at
// all). BLOCKING 2 (external audit round 3, 2026-07-30): the CLI used to
// decide whether to print `add`'s warning BEFORE ever calling AddPackage at
// all (via a standalone WillResolveMutableRefForAdd prediction), so it
// printed even when AddPackage was about to fail for an unrelated reason
// entirely -- a missing config, an unreachable source, a duplicate name, or
// (compounding classifyRefResolution's own noVerify gap above) an offline
// HEAD resolution that was about to hard-fail anyway. Invoking the warning
// from exactly the call site that is about to perform the real resolution --
// after every one of AddPackage's other pre-flight checks has already run
// and succeeded -- avoids the warning printing on any of those paths without
// hand-duplicating AddPackage's own pre-flight order in the CLI: this
// depends on every AddPackage pre-flight check before this call site
// actually returning before resolveRef is reached (true as of this file's
// current AddPackage body, editor.go's AddPackage function, verified by
// TestMarketplacePackageAdd_ExplicitRefHead_NoVerify_NoMutableRefWarning_ExitsCode2/
// _MissingConfig_/_UnreachableSource_/_DuplicateName_NoMutableRefWarning in
// cmd/apm-go/marketplace_package_test.go) -- a future pre-flight check added
// AFTER this resolveRef call, or reordered before it without adding a
// regression test for it, is not covered by this reasoning and would need
// its own test.
func resolveRef(source, ref, version string, lister RefLister, noVerify, implicitHeadOnEmpty, skipLocalSource bool, onExplicitHeadWillResolve func(), onRefResolved func(ref, sha string)) (string, error) {
	kind := classifyRefResolution(source, ref, version, noVerify, implicitHeadOnEmpty, skipLocalSource)
	return resolveRefForKind(kind, source, ref, lister, onExplicitHeadWillResolve, onRefResolved)
}

// resolveRefForKind performs kind's corresponding lister I/O (if any) and
// returns the resolved ref. Split out from resolveRef so a test can drive
// the kind-dispatch switch directly with a kind value classifyRefResolution
// itself could never actually return today (MAJOR 5, external audit round
// 4, 2026-07-30's fail-closed `default` case below), without needing to
// contrive a (source, ref, version, noVerify, ...) input tuple that reaches
// it through classifyRefResolution -- there is no such tuple today, by
// construction, since every kind classifyRefResolution can return has its
// own explicit case.
func resolveRefForKind(kind refResolutionKind, source, ref string, lister RefLister, onExplicitHeadWillResolve func(), onRefResolved func(ref, sha string)) (string, error) {
	// reportResolved fires the CLI's "Resolved <ref> to <sha12>" progress
	// hook only when a real resolution happened (the stored value differs
	// from what the user gave).
	reportResolved := func(given, sha string) {
		if onRefResolved != nil && given != sha {
			onRefResolved(given, sha)
		}
	}
	switch kind {
	case refKindNone:
		return "", nil
	case refKindVerbatim:
		return ref, nil
	case refKindHeadOffline:
		return "", errCannotResolveHeadOffline
	case refKindHead:
		if ref != "" && onExplicitHeadWillResolve != nil {
			onExplicitHeadWillResolve()
		}
		refs, err := lister.ListRefs(source)
		if err != nil {
			return "", fmt.Errorf("could not resolve ref %q for %q: %w", "HEAD", source, err)
		}
		for _, r := range refs {
			if strings.EqualFold(r.Name, "HEAD") {
				reportResolved("HEAD", r.Commit)
				return r.Commit, nil
			}
		}
		return "", fmt.Errorf("ref %q not found on %q", "HEAD", source)
	case refKindNamed:
		refs, err := lister.ListRefs(source)
		if err != nil {
			return "", fmt.Errorf("could not resolve ref %q for %q: %w", ref, source, err)
		}
		for _, r := range refs {
			if r.Name == ref {
				reportResolved(ref, r.Commit)
				return r.Commit, nil
			}
		}
		return "", fmt.Errorf("ref %q not found on %q", ref, source)
	default:
		// MAJOR 5 (external audit round 4, 2026-07-30): classifyRefResolution
		// is a closed, exhaustive enum today (refKindNone/Verbatim/Head/
		// HeadOffline/Named), but this switch used to fall back to
		// refKindNamed's own network-resolving branch for anything it didn't
		// recognize by name (a bare `default:` with a "// refKindNamed"
		// comment, not an explicit `case refKindNamed:`). A future kind added
		// to the enum without updating this switch would silently touch the
		// network with whatever `ref` happens to hold, instead of failing
		// closed -- the exact "fail open on an unrecognized case" shape this
		// function's own callers (AddPackage/SetPackage) are fixed elsewhere
		// in this file to never do. Every currently-defined kind now has its
		// own explicit case above; this default only exists for a future kind
		// that has NOT yet been wired to a case here, and now fails closed
		// with an explicit error naming the unrecognized value instead of
		// guessing "named ref" for it.
		//
		// MINOR (external audit round 5, 2026-07-30): this comment already
		// promised "an explicit error naming the unrecognized value", but the
		// message below did not actually interpolate kind -- only ref and
		// source. Formatted in now so the comment's own claim holds.
		return "", fmt.Errorf("resolveRef: unrecognized ref resolution kind %d for ref %q on %q", kind, ref, source)
	}
}

// WillResolveMutableRefForAdd reports whether AddPackage's own resolveRef
// call (skipLocalSource=true, implicitHeadOnEmpty=true) would enter the
// network-resolving refKindHead branch for the given source/ref/version/
// noVerify combination, with no I/O of its own. Calls classifyRefResolution
// directly (see that function's doc comment for why this is the only place
// this decision is expressed) rather than restating any of resolveRef's
// branches by hand.
//
// This is a static predictor, not the CLI's actual warning trigger: BLOCKING
// 2 (external audit round 3, 2026-07-30) found that deciding the CLI's
// mutable-ref warning from a call to this function BEFORE ever invoking
// AddPackage prints the warning even when AddPackage is about to fail for a
// reason this function cannot see (a missing config, an unreachable source,
// a duplicate name) -- since those are AddPackage-internal pre-flight steps
// this function has no access to. The CLI now wires its warning through
// resolveRef's own onExplicitHeadWillResolve hook (AddOptions.
// OnExplicitHeadWillResolve) instead, which fires only once every one of
// those steps has already passed. This function remains exported and is
// still regression-tested against resolveRef's actual behavior (see
// resolveref_test.go's
// TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct) as a
// no-I/O predictor for any other caller that wants one.
func WillResolveMutableRefForAdd(source, ref, version string, noVerify bool) bool {
	return classifyRefResolution(source, ref, version, noVerify, true, true) == refKindHead
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
	// Category is R10's add-only field (mkt-053's compose-time codex
	// category gate): stored as-is when non-empty; when empty,
	// packageEntryNode omits the category: key entirely (see its own
	// `if entry.Category != ""` guard).
	Category string
	// OnExplicitHeadWillResolve, when non-nil, is invoked by resolveRef
	// immediately before (and only immediately before) it performs the real
	// lister.ListRefs call for an explicitly-given "--ref HEAD"/"head" --
	// i.e. only once every other AddPackage pre-flight step (the --version/
	// --ref guard, subdir validation, source validation, reachability check,
	// config load, and duplicate-name check) has already passed, and only
	// once classifyRefResolution has confirmed noVerify does not make
	// resolution impossible. The CLI wires this to print R5/AC19's
	// mutable-ref warning (see resolveRef's own doc comment for the
	// BLOCKING 2 fix this hook exists for).
	OnExplicitHeadWillResolve func()
	// OnRefResolved, when non-nil, is invoked by resolveRef after a
	// mutable/named ref was genuinely resolved to a different commit SHA,
	// with the ref as given ("HEAD" for the implicit-HEAD case) and the
	// resolved SHA. The CLI wires this to print upstream's "Resolved <ref>
	// to <sha12>" progress line (plugin/__init__.py:147-150, 179-182) so the
	// user learns what actually got written into apm.yml.
	OnRefResolved func(ref, sha string)
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
// source (req-mf-017, via manifest.ValidateMarketplaceSource), a local
// source that escapes the project root or does not resolve to an existing
// directory (ticket 20 AC1/AC2, via verifyPackageSource), an unreachable
// remote source, an invalid resolved name (ticket 20 AC3's packageNameIssue
// rule; a derived name's error blames the source, not the name itself --
// ticket 21 AC1), a duplicate (case-insensitive) name, or a write/validate
// failure.
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
	if err := verifyPackageSource(dir, source, lister, opts.NoVerify); err != nil {
		return "", false, err
	}

	cfg, _, err := LoadAuthoringConfig(dir)
	if err != nil {
		return "", false, err
	}

	name = opts.Name
	derivedFromSource := name == ""
	if derivedFromSource {
		name = defaultNameFromSource(source)
	}
	// Ticket 21 AC1: a name derived from source gets a diagnostic naming
	// the source the user actually typed (packageNameDerivedError), not
	// just the derived name -- an explicit --name rejection (AC2) keeps
	// blaming the name itself, unchanged.
	if problem := packageNameIssue(name); problem != packageNameProblemNone {
		if derivedFromSource {
			return "", false, packageNameDerivedError(source, name, problem)
		}
		return "", false, packageNameError(name, problem)
	}
	if findPackageIndex(cfg, name) != -1 {
		return "", false, fmt.Errorf("package %q already exists", name)
	}

	resolvedRef, err := resolveRef(source, opts.Ref, opts.Version, lister, opts.NoVerify, true, true, opts.OnExplicitHeadWillResolve, opts.OnRefResolved)
	if err != nil {
		return "", false, err
	}

	newNode := packageEntryNode(PackageEntry{
		Name:              name,
		Source:            source,
		Version:           opts.Version,
		Ref:               resolvedRef,
		Subdir:            opts.Subdir,
		TagPattern:        opts.TagPattern,
		Tags:              opts.Tags,
		IncludePrerelease: opts.IncludePrerelease,
		Category:          opts.Category,
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
	// OnExplicitHeadWillResolve, when non-nil, is invoked by resolveRef
	// immediately before (and only immediately before) it performs the real
	// lister.ListRefs call for an explicitly-given "--ref HEAD"/"head" --
	// `set`'s own R5/AC19-equivalent mutable-ref-warning hook, mirroring
	// AddOptions.OnExplicitHeadWillResolve (see that field's own doc comment
	// for why the warning is wired through resolveRef's hook rather than
	// predicted before calling SetPackage).
	//
	// BLOCKING 3 (external audit round 4, 2026-07-30): SetPackage used to
	// pass a hardcoded nil for this parameter, so `apm marketplace package
	// set NAME --ref HEAD` never printed the mutable-ref warning at all --
	// contradicting upstream, which warns on `set` too (Python's
	// commands/marketplace/plugin/set.py:80 calls the same `_resolve_ref`
	// plugin/__init__.py:120-137 warns from). Every existing `set --ref` CLI
	// test only ever used a tag/branch ref, never HEAD, so nothing caught
	// this gap.
	OnExplicitHeadWillResolve func()
	// OnRefResolved mirrors AddOptions.OnRefResolved (see that field's doc
	// comment): invoked with (ref-as-given, resolved SHA) after resolveRef
	// genuinely resolved the ref to a different commit.
	OnRefResolved func(ref, sha string)
}

// SetPackage implements `apm marketplace package set NAME` (mkt-045):
// update only the fields opts explicitly provides on an existing
// packages[] entry (case-insensitive name match), fully re-rendering that
// one element (yamlcore.SeqSet). Giving both Version and Ref is rejected;
// giving one clears the other in storage, mirroring Python's
// update_plugin_entry. A non-SHA opts.Ref is resolved to a concrete SHA via
// lister (F4/resolveRef) before being stored -- mirroring Python's set.py,
// which always attempts resolution for a given ref (no --no-verify escape
// hatch on `set`).
func SetPackage(dir, name string, opts SetOptions, lister RefLister) (fallbackUsed bool, err error) {
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
		// version="" (set never bundles this resolveRef call with a
		// simultaneous --version) and implicitHeadOnEmpty=false (`set`
		// only ever resolves a ref explicitly given via cmd.Flags().
		// Changed("ref"); it must not treat an unrelated update as an
		// implicit-HEAD pin -- R5's implicit-HEAD default is `add`-only,
		// design.md §6). noVerify=false: `set` has no --no-verify escape
		// hatch (mirrors Python's set.py). skipLocalSource=false: unlike
		// `add`'s mkt-046 rule, `set` always resolves an explicitly-given
		// ref regardless of source (install-marketplace-contracts.md:87);
		// passing true here silently cleared Ref (and, via the branch
		// below, Version too) back to "" for a local package's `set --ref`
		// (BLOCKING 2, external audit 2026-07-30) -- fixed by threading a
		// dedicated skipLocalSource argument through resolveRef instead of
		// letting it infer add-vs-set from implicitHeadOnEmpty alone.
		resolvedRef, rerr := resolveRef(merged.Source, *opts.Ref, "", lister, false, false, false, opts.OnExplicitHeadWillResolve, opts.OnRefResolved)
		if rerr != nil {
			return false, rerr
		}
		merged.Ref = resolvedRef
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
// cmd/apm-go/marketplace_package.go -- not performed here.
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
