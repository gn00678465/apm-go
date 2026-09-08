// This file (output.go) implements mkt-054's output-location resolution and
// atomic write: each output profile's default path (never the repo root),
// the two YAML forms an apm.yml `marketplace:` block may use to override a
// profile's path, the CLI `--marketplace-path FORMAT=PATH` override (which
// always wins over both), a path-traversal guard applied to every resolved
// path regardless of source, and the atomic JSON writer itself.
//
// LoadOutputPathOverrides deliberately re-reads dir's marketplace authoring
// source directly (a second, narrowly-scoped YAML read) rather than
// extending internal/marketplace/authoring.AuthoringConfig with a new
// field: this sub-task's Rollback Points restricts every already-landed
// file outside this package to a single, unrelated edit (main.go's one-line
// AddCommand), so this keeps that boundary intact instead of widening
// authoring's already-reviewed public schema.
package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/marketplace/authoring"
	"github.com/apm-go/apm/internal/yamlcore"
)

// defaultOutputPaths maps every known marketplace output profile name
// (mkt-054, mirroring Python's known_output_names()) to its default output
// path -- never the repo root.
//
// One table, because the two things derived from it have to agree and a test
// cannot enforce that when one of them is a switch: a `case` added there
// without a matching entry in the name set is not enumerable, so nothing can
// notice it is unreachable. Deriving both from this map makes the two
// physically the same list instead of two lists a reviewer has to keep in
// step (external audit, 2026-08-13).
var defaultOutputPaths = map[string]string{
	"claude": filepath.Join(".claude-plugin", "marketplace.json"),
	"codex":  filepath.Join(".agents", "plugins", "marketplace.json"),
}

// KnownOutputFormats is the set of accepted output profile names, derived
// from defaultOutputPaths.
var KnownOutputFormats = knownOutputFormats()

func knownOutputFormats() map[string]bool {
	names := make(map[string]bool, len(defaultOutputPaths))
	for name := range defaultOutputPaths {
		names[name] = true
	}
	return names
}

// ComposeOptions carries output-mapper options. The variadic form keeps the
// existing default call sites byte-identical while allowing pack's Claude
// source-style selector to reach both the producer and drift gate.
type ComposeOptions struct {
	ClaudeSourceStyle ClaudeSourceStyle
}

// ComposeDocument dispatches to the mkt-050/052/053 mapper for format
// ("claude" or "codex" -- callers already reject anything else before this
// is ever reached). Exported so both `apm-go pack`'s own marketplace
// producer (cmd/apm-go/pack.go's composeMarketplaceDocument, a thin
// wrapper around this) and the release-time drift-check gate
// (drift_check.go's CheckMarketplaceDrift, ticket 17 phase 4) share the
// exact same compose path rather than two independently-drifting copies of
// this dispatch.
func ComposeDocument(format string, cfg *authoring.AuthoringConfig, resolved []ResolvedPackage, options ...ComposeOptions) (any, []string, error) {
	var opts ComposeOptions
	if len(options) > 0 {
		opts = options[0]
	}
	switch format {
	case "claude":
		return ClaudeMapper{SourceStyle: opts.ClaudeSourceStyle}.Compose(cfg, resolved)
	case "codex":
		return CodexMapper{}.Compose(cfg, resolved)
	default:
		return nil, nil, fmt.Errorf("unknown marketplace output format %q", format)
	}
}

// DefaultOutputPath returns format's default output path and whether format
// is a known profile name at all.
func DefaultOutputPath(format string) (string, bool) {
	path, ok := defaultOutputPaths[format]
	return path, ok
}

// ResolveOutputPath computes format's final output path (mkt-054), applying
// overrides in priority order: a CLI --marketplace-path FORMAT=PATH
// override (cliOverrides) always wins, then an apm.yml-declared override
// (configPaths, from LoadOutputPathOverrides), then the profile's own
// default path (DefaultOutputPath). Returns an error only when format is
// not a known output profile at all.
func ResolveOutputPath(format string, configPaths, cliOverrides map[string]string) (string, error) {
	if p, ok := cliOverrides[format]; ok && p != "" {
		return p, nil
	}
	if p, ok := configPaths[format]; ok && p != "" {
		return p, nil
	}
	p, ok := DefaultOutputPath(format)
	if !ok {
		return "", fmt.Errorf("unknown marketplace output format %q", format)
	}
	return p, nil
}

// LoadOutputPathOverrides re-reads dir's marketplace authoring source --
// apm.yml when src is authoring.ConfigSourceApmYML, or a standalone
// marketplace.yml when src is authoring.ConfigSourceLegacy (mkt-047; the
// caller already determined this via authoring.LoadAuthoringConfig, so this
// never re-derives the mutual-exclusivity rule itself) -- and extracts every
// format's declared output-path override, supporting both of mkt-054's YAML
// forms:
//
//   - the map form (`outputs: {<name>: {path: ...}}`, the shape `marketplace
//     init` scaffolds) -- preferred when both forms declare a path for the
//     same format (design.md: "map 形式優先")
//   - the legacy per-format sub-block form (`<name>: {output: ...}` as a
//     sibling of `outputs:`, e.g. `marketplace.claude.output`)
//
// Returns a nil map (not an error) when neither form declares any override.
func LoadOutputPathOverrides(dir string, src authoring.ConfigSource) (map[string]string, error) {
	path := filepath.Join(dir, "apm.yml")
	if src == authoring.ConfigSourceLegacy {
		path = filepath.Join(dir, "marketplace.yml")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil
	}
	root := doc.Content[0]

	block := root
	if src != authoring.ConfigSourceLegacy {
		block = ymlMappingValue(root, "marketplace")
		if block == nil || block.Kind != yaml.MappingNode {
			return nil, nil
		}
	}

	paths := map[string]string{}

	// Legacy per-format sub-block form, parsed first so the map form below
	// can overwrite it for the same key (design.md's stated priority).
	for name := range KnownOutputFormats {
		sub := ymlMappingValue(block, name)
		if sub != nil && sub.Kind == yaml.MappingNode {
			if p := ymlScalarString(sub, "output"); p != "" {
				paths[name] = p
			}
		}
	}

	// Map form: outputs.<name>.path.
	outputsNode := ymlMappingValue(block, "outputs")
	if outputsNode != nil && outputsNode.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(outputsNode.Content); i += 2 {
			name := outputsNode.Content[i].Value
			val := outputsNode.Content[i+1]
			if val.Kind == yaml.MappingNode {
				if p := ymlScalarString(val, "path"); p != "" {
					paths[name] = p
				}
			}
		}
	}

	if len(paths) == 0 {
		return nil, nil
	}
	return paths, nil
}

// ymlMappingValue and ymlScalarString are minimal local copies of
// internal/marketplace/authoring/schema.go's identically-named helpers
// (this file's own doc comment explains why this package keeps its own
// narrow YAML-navigation instead of importing/extending authoring's).
func ymlMappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func ymlScalarString(m *yaml.Node, key string) string {
	v := ymlMappingValue(m, key)
	if v == nil || v.Kind != yaml.ScalarNode || v.Tag == "!!null" {
		return ""
	}
	return v.Value
}

// EnsureWithinRoot resolves path (joined against root when path is not
// already absolute) and rejects it if the result escapes root -- mkt-054's
// path-traversal guard, matching Python's builder.py::write_output
// "ensure_path_within(output_path, project_root)" check, applied uniformly
// to every resolved output path regardless of whether it came from a CLI
// override, an apm.yml override, or a profile default. Returns the resolved
// absolute path on success.
//
// Containment is decided on SYMLINK-RESOLVED paths, not merely lexical ones
// (2026-08-12): a directory symlink inside the root pointing outside it has a
// perfectly innocent lexical form, so an Abs/Clean/Rel-only check let every
// writer downstream follow the link out of the project. Upstream resolves for
// the same reason and says so verbatim -- "symlinks are resolved so that a
// link pointing outside the base is caught as well"
// (v0.28.0:src/apm_cli/utils/path_security.py:98-119).
//
// BOTH sides are resolved. Resolving only the path would reject every project
// whose root is itself reached through a symlink (macOS's /tmp -> /private/tmp
// being the everyday case), turning a security fix into a false-positive
// generator.
//
// The value returned is the RESOLVED path, so that callers write to the exact
// location that was checked. Returning the lexical form instead leaves a
// check-A-write-B window (flagged by an external audit on 2026-08-12): the
// lexical path re-traverses every link on each use, so swapping one of those
// links for an outward-pointing one after the check but before the write
// redirects the write. For a project containing no links the two forms are
// identical, so ordinary output is unchanged.
func EnsureWithinRoot(root, path string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve project root %q: %w", root, err)
	}

	joined := path
	if !filepath.IsAbs(joined) {
		joined = filepath.Join(absRoot, joined)
	}
	absPath, err := filepath.Abs(filepath.Clean(joined))
	if err != nil {
		return "", fmt.Errorf("resolve output path %q: %w", path, err)
	}

	resolvedRoot, err := resolveSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("resolve project root %q: %w", root, err)
	}
	resolvedPath, err := resolveSymlinks(absPath)
	if err != nil {
		return "", fmt.Errorf("resolve output path %q: %w", path, err)
	}

	rel, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("output path %q escapes the project root %q", path, absRoot)
	}
	return resolvedPath, nil
}

// maxLinkHops bounds symlink chasing so a link cycle cannot spin forever.
// POSIX implementations use a similar small constant.
const maxLinkHops = 32

// resolveSymlinks returns path with every link-like component replaced by its
// target, walking the path one component at a time from the volume root down.
// Components that do not exist yet are appended verbatim -- the normal case
// for an output file and the directories leading to it on a first run --
// mirroring Python's Path.resolve(strict=False).
//
// filepath.EvalSymlinks is deliberately NOT used: on Windows it does not
// resolve JUNCTIONS (measured on go1.26.3 -- EvalSymlinks returns a junction
// path unchanged and Lstat reports ModeIrregular rather than ModeSymlink),
// and a junction is precisely the escape a Windows attacker would reach for,
// since creating one needs no privilege while creating a symlink does.
// os.Readlink does report a junction's target, so a hand-rolled walk over
// Lstat+Readlink covers symlinks and junctions on every platform.
//
// A link-like component whose target cannot be read is left as-is rather than
// rejected: Windows marks several benign reparse points (OneDrive placeholders,
// dedup stubs) ModeIrregular too, and those redirect nothing an attacker
// controls. Failing closed on them would break ordinary projects stored in
// OneDrive folders.
//
// Only existing components can carry a link, so appending the non-existent
// tail loses no protection: a component that does not exist cannot redirect
// anywhere, and one created as a link afterwards would be followed by the
// write itself -- the same check-then-write window every filesystem guard in
// this codebase has.
func resolveSymlinks(path string) (string, error) {
	volume := filepath.VolumeName(path)
	resolved := volume + string(filepath.Separator)
	pending := splitPathComponents(path[len(volume):])
	hops := 0

	for len(pending) > 0 {
		part := pending[0]
		pending = pending[1:]

		switch part {
		case ".":
			continue
		case "..":
			resolved = filepath.Dir(resolved)
			continue
		}

		candidate := filepath.Join(resolved, part)
		info, err := os.Lstat(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				// This component and everything after it is new; nothing
				// that does not exist can redirect anywhere.
				resolved = candidate
				continue
			}
			return "", err
		}
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
			resolved = candidate
			continue
		}
		target, rerr := os.Readlink(candidate)
		if rerr != nil {
			resolved = candidate // a reparse point we cannot follow; see doc comment
			continue
		}

		hops++
		if hops > maxLinkHops {
			return "", fmt.Errorf("too many symbolic links while resolving %q", path)
		}
		base, parts, terr := linkTargetStart(target, resolved)
		if terr != nil {
			return "", terr
		}
		// The target's own components go back on the queue instead of being
		// substituted wholesale: each of them must be examined in turn,
		// because any one of them can be a link of its own. Substituting a
		// multi-segment target and Lstat-ing the joined result instead is the
		// bug an external audit found here on 2026-08-12 -- Lstat follows
		// intermediate links silently, so "<root>/b/leaf" reports a plain
		// directory even when b leaves the root.
		resolved = base
		pending = append(append([]string(nil), parts...), pending...)
	}
	return resolved, nil
}

// linkTargetStart says where a link target starts resolving from and which
// components remain to be walked. linkDir is the already-resolved directory
// holding the link.
//
// The Windows-only middle cases are why filepath.IsAbs alone is not enough:
// IsAbs(`\outside`) is false and VolumeName(`C:foo`) is "C:" while IsAbs is
// still false (both measured on go1.26.3), yet neither is relative to the
// link's directory.
func linkTargetStart(target, linkDir string) (string, []string, error) {
	if filepath.IsAbs(target) {
		volume := filepath.VolumeName(target)
		return volume + string(filepath.Separator), splitPathComponents(target[len(volume):]), nil
	}
	if filepath.VolumeName(target) != "" {
		// Drive-relative ("C:foo"): resolves against that drive's current
		// directory, which is per-process state this guard cannot observe.
		// Fail closed rather than guess.
		return "", nil, fmt.Errorf("unsupported drive-relative link target %q", target)
	}
	if len(target) > 0 && os.IsPathSeparator(target[0]) {
		// Volume-rooted without a volume ("\outside"): rooted at the volume
		// the LINK lives on, not at the link's directory.
		return filepath.VolumeName(linkDir) + string(filepath.Separator), splitPathComponents(target), nil
	}
	return linkDir, splitPathComponents(target), nil
}

// splitPathComponents splits p into non-empty path components, honouring both
// separators on Windows and only "/" elsewhere (a backslash is a legal
// filename character on Unix). "." and ".." are kept: the caller interprets
// them.
func splitPathComponents(p string) []string {
	var out []string
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// marshalOutput encodes doc exactly as WriteOutput writes it: HTML escaping
// off (so a package URL keeps its & and < verbatim) and two-space indent,
// with the trailing newline json.Encoder appends. Shared with the
// --check-clean drift gate (drift_check.go), which must compare against the bytes
// that would actually land on disk rather than a second, subtly different
// encoding.
func marshalOutput(doc any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteOutput serializes doc as 2-space-indented JSON with a trailing newline
// (matching Python's `json.dumps(data, indent=2, ensure_ascii=False)` plus a
// trailing newline, minus HTML-escaping which Python's json module never
// applies either) and
// atomically writes it to rel, a path relative to rw's boundary.
//
// It takes a RootWriter rather than a path string because a resolved path
// string cannot survive the trip: between the containment check that produced
// it and the MkdirAll/CreateTemp/Rename that consumed it, a process able to
// write inside the project could swap an ancestor for a junction and redirect
// all three (external audit 2026-08-13, reproduced locally). rw holds a
// directory handle taken once, so that swap is unreachable rather than merely
// unlikely.
func WriteOutput(rw *RootWriter, rel string, doc any) error {
	encoded, err := marshalOutput(doc)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", rw.Path(rel), err)
	}
	rel, err = rw.Rel(rel)
	if err != nil {
		return err
	}
	return rw.WriteFileAtomic(rel, encoded)
}

// OutputDiff is the per-output plugin classification `pack --json`'s
// marketplace.outputs entries report -- Oracle MarketplaceOutputReport's
// unchanged_count/added_count/updated_count/removed_count fields
// (marketplace/builder.py:161-164).
type OutputDiff struct {
	Unchanged int
	Added     int
	Updated   int
	Removed   int
}

// ComputeOutputDiff classifies each plugin in newDoc against the
// marketplace.json currently on disk at path, mirroring the Oracle's
// MarketplaceBuilder._compute_diff (marketplace/builder.py:1262-1300) and
// its _load_existing_json feed: a missing/unreadable file means "everything
// is new" (0 unchanged, len(plugins) added, 0 updated, 0 removed).
//
// A plugin's identity is its name; its comparison key is the source's sha
// -- `source.sha`, falling back to the legacy `source.commit` for
// marketplace.json files written before the Claude-spec rename, or the
// source string itself when source is a bare string (a local-path package,
// where the path IS the identity). Same name + same key is unchanged, same
// name + different key is updated, name only in the new document is added,
// name only in the old one is removed.
func ComputeOutputDiff(path string, newDoc any) OutputDiff {
	newPlugins := pluginKeys(reencode(newDoc))

	raw, err := os.ReadFile(path)
	if err != nil {
		return OutputDiff{Added: len(newPlugins)}
	}
	var oldDoc map[string]any
	if err := json.Unmarshal(raw, &oldDoc); err != nil {
		// _load_existing_json swallows a malformed existing file the same
		// way a missing one is swallowed: the run must not fail because the
		// previous artifact was hand-edited into invalid JSON.
		return OutputDiff{Added: len(newPlugins)}
	}
	oldPlugins := pluginKeys(oldDoc)

	var d OutputDiff
	for name, newKey := range newPlugins {
		oldKey, existed := oldPlugins[name]
		switch {
		case !existed:
			d.Added++
		case oldKey == newKey:
			d.Unchanged++
		default:
			d.Updated++
		}
	}
	for name := range oldPlugins {
		if _, still := newPlugins[name]; !still {
			d.Removed++
		}
	}
	return d
}

// reencode round-trips doc through JSON so ComputeOutputDiff can read it
// with the same shape it reads the on-disk file with, rather than
// reflecting over whatever concrete type ComposeDocument returned.
func reencode(doc any) map[string]any {
	b, err := json.Marshal(doc)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	return m
}

// pluginKeys maps each plugin's name to its comparison key (see
// ComputeOutputDiff).
func pluginKeys(doc map[string]any) map[string]string {
	out := map[string]string{}
	plugins, _ := doc["plugins"].([]any)
	for _, p := range plugins {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		name, _ := pm["name"].(string)
		key := ""
		switch src := pm["source"].(type) {
		case map[string]any:
			if sha, ok := src["sha"].(string); ok && sha != "" {
				key = sha
			} else if commit, ok := src["commit"].(string); ok {
				key = commit
			}
		case string:
			key = src
		}
		out[name] = key
	}
	return out
}
