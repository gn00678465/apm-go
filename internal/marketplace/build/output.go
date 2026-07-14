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

// KnownOutputFormats is mkt-054's set of known marketplace output profile
// names ("claude", "codex") -- mirroring Python's known_output_names().
var KnownOutputFormats = map[string]bool{"claude": true, "codex": true}

// DefaultOutputPath returns format's default output path (mkt-054: never
// the repo root) and whether format is a known profile name at all.
func DefaultOutputPath(format string) (string, bool) {
	switch format {
	case "claude":
		return filepath.Join(".claude-plugin", "marketplace.json"), true
	case "codex":
		return filepath.Join(".agents", "plugins", "marketplace.json"), true
	default:
		return "", false
	}
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

	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("output path %q escapes the project root %q", path, absRoot)
	}
	return absPath, nil
}

// WriteOutput serializes doc as 2-space-indented JSON with a trailing
// newline (matching Python's `json.dumps(data, indent=2,
// ensure_ascii=False) + "\n"`, minus HTML-escaping which Python's json
// module never applies either) and atomically writes it to path (temp file
// in the same directory, then rename), creating any missing parent
// directories first.
func WriteOutput(path string, doc any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory %q: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".marketplace-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file to %q: %w", path, err)
	}
	return nil
}
