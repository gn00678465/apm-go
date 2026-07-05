// Package authoring implements the producer-side `apm marketplace` data
// model: the `marketplace:` authoring block that a project author maintains
// in apm.yml (or, for pre-migration projects, a standalone legacy
// marketplace.yml) to describe how their repo's plugins are packaged for
// consumption via `apm marketplace add`/`apm install`.
//
// This file (schema.go) only covers the data model and its loading
// (mkt-047, req-mf-017): AuthoringConfig and LoadAuthoringConfig. Field
// scope deliberately follows design.md, not Python apm's full
// yml_schema.py -- fields that only matter to a later sub-task (e.g. the
// codex-output `category` required-field gate, mkt-053) are intentionally
// left unvalidated here; see the task's implement.md "已知延後項目".
package authoring

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

const (
	apmYMLFilename    = "apm.yml"
	legacyYMLFilename = "marketplace.yml"
)

// errMarketplaceConfigsMutuallyExclusive and errNoMarketplaceConfig are
// mkt-047's two "which file is authoritative" hard-error outcomes.
// Exported as shared sentinel values (rather than inlined per call site)
// so editor.go's locateEditableConfig -- which applies the exact same
// mkt-047 rule for `package add/remove/set` -- reports byte-identical
// error text instead of a second, driftable copy of the message.
var (
	errMarketplaceConfigsMutuallyExclusive = errors.New(
		"apm.yml's marketplace: block and legacy marketplace.yml both exist; " +
			"these are mutually exclusive — remove one, or run 'apm marketplace migrate' " +
			"to fold marketplace.yml into apm.yml",
	)
	errNoMarketplaceConfig = errors.New(
		"no marketplace authoring config found (neither apm.yml's marketplace: block " +
			"nor a legacy marketplace.yml exist); run 'apm marketplace init' to scaffold one",
	)
)

// ConfigSource identifies which file an AuthoringConfig was loaded from.
type ConfigSource int

const (
	// ConfigSourceApmYML means the config came from apm.yml's marketplace:
	// block (the current, preferred location).
	ConfigSourceApmYML ConfigSource = iota
	// ConfigSourceLegacy means the config came from a standalone
	// marketplace.yml (deprecated). Callers should print a deprecation
	// warning pointing at `apm marketplace migrate` when this is returned.
	ConfigSourceLegacy
)

// Owner is the marketplace.owner block: who publishes this marketplace.
type Owner struct {
	Name string
	URL  string
}

// Build is the marketplace.build block: APM-only build-time configuration.
type Build struct {
	TagPattern string
}

// PackageEntry is one entry of marketplace.packages[]. Version and Ref are
// mutually exclusive pins (enforced at the CLI/editor layer for
// `package add/set`, mkt-045 -- not at load time here).
type PackageEntry struct {
	Name        string
	Description string
	Source      string
	Version     string
	Ref         string
	Subdir      string
	TagPattern  string
	// Tags is the union of the package entry's `tags` and `keywords` YAML
	// keys, deduplicated and order-preserving (tags first, then any new
	// keywords entries) -- matching Python apm's yml_schema.py:896-913, so
	// a later `apm pack` produces the same tag set as the original.
	Tags              []string
	IncludePrerelease bool
	Category          string
}

// AuthoringConfig is the parsed marketplace: authoring block, regardless of
// whether it came from apm.yml or a legacy marketplace.yml.
type AuthoringConfig struct {
	Owner    Owner
	Build    Build
	Outputs  []string
	Packages []PackageEntry
}

// LoadAuthoringConfig loads the marketplace authoring config for the
// project rooted at dir (mkt-047):
//
//   - apm.yml has a non-null `marketplace:` key AND a legacy
//     marketplace.yml file exists (existence alone -- an empty legacy file
//     still counts) -> hard error, the two sources are never merged.
//   - Only one of the two exists -> read that one. A legacy-only result
//     (ConfigSourceLegacy) is a signal for the caller to print a
//     deprecation warning suggesting `apm marketplace migrate`; this
//     function itself performs no I/O beyond reading the config files.
//   - Neither exists -> explicit error pointing at `apm marketplace init`.
func LoadAuthoringConfig(dir string) (*AuthoringConfig, ConfigSource, error) {
	apmPath := filepath.Join(dir, apmYMLFilename)
	legacyPath := filepath.Join(dir, legacyYMLFilename)

	legacyExists, err := fileExists(legacyPath)
	if err != nil {
		return nil, 0, err
	}

	apmBlock, err := loadApmMarketplaceBlock(apmPath)
	if err != nil {
		return nil, 0, err
	}

	switch {
	case apmBlock != nil && legacyExists:
		return nil, 0, errMarketplaceConfigsMutuallyExclusive
	case apmBlock != nil:
		cfg, err := parseAuthoringNode(apmBlock)
		if err != nil {
			return nil, 0, err
		}
		return cfg, ConfigSourceApmYML, nil
	case legacyExists:
		root, err := loadYAMLRoot(legacyPath)
		if err != nil {
			return nil, 0, err
		}
		cfg, err := parseAuthoringNode(root)
		if err != nil {
			return nil, 0, err
		}
		return cfg, ConfigSourceLegacy, nil
	default:
		return nil, 0, errNoMarketplaceConfig
	}
}

// fileExists reports whether path exists as a regular file (or at least a
// non-directory) -- used for mkt-047's legacy detection, which is bare
// existence, not "parses to non-empty content".
func fileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		return !info.IsDir(), nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("stat %s: %w", path, err)
}

// loadApmMarketplaceBlock returns apm.yml's top-level `marketplace:` value
// node, or nil if apm.yml does not exist, has no `marketplace:` key, or the
// key's value is explicit YAML null (`marketplace:` with nothing after it) --
// the "_has_marketplace_block" semantics mkt-047 requires.
func loadApmMarketplaceBlock(apmPath string) (*yaml.Node, error) {
	exists, err := fileExists(apmPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	data, err := os.ReadFile(apmPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", apmPath, err)
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", apmPath, err)
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: top-level must be a YAML mapping", apmPath)
	}

	val := mappingValue(root, "marketplace")
	if val == nil || isNullNode(val) {
		return nil, nil
	}
	return val, nil
}

// loadYAMLRoot reads and parses a standalone marketplace.yml (legacy),
// returning its top-level mapping node (the legacy file holds the
// marketplace block directly at the document root).
func loadYAMLRoot(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("%s is empty", path)
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: top-level must be a YAML mapping", path)
	}
	return root, nil
}

// isNullNode reports whether v is an explicit YAML null scalar (e.g. the
// value of a bare `marketplace:` key with nothing after it).
func isNullNode(v *yaml.Node) bool {
	return v.Kind == yaml.ScalarNode && v.Tag == "!!null"
}

// parseAuthoringNode builds an AuthoringConfig from a marketplace block
// mapping node -- either apm.yml's `marketplace:` value, or a legacy
// marketplace.yml's document root, which share the same field shape.
func parseAuthoringNode(node *yaml.Node) (*AuthoringConfig, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("marketplace config must be a YAML mapping")
	}
	packages, err := parsePackages(node)
	if err != nil {
		return nil, err
	}
	return &AuthoringConfig{
		Owner:    parseOwner(node),
		Build:    parseBuild(node),
		Outputs:  parseOutputs(node),
		Packages: packages,
	}, nil
}

func parseOwner(node *yaml.Node) Owner {
	v := mappingValue(node, "owner")
	return Owner{
		Name: scalarString(v, "name"),
		URL:  scalarString(v, "url"),
	}
}

func parseBuild(node *yaml.Node) Build {
	v := mappingValue(node, "build")
	return Build{
		TagPattern: scalarString(v, "tagPattern"),
	}
}

// parseOutputs accepts the map form (`outputs: {claude: {}, codex: {}}`,
// the shape `init` scaffolds), the deprecated list/string forms
// (`outputs: [claude, codex]` / `outputs: claude`), and returns nil when
// the key is absent entirely.
func parseOutputs(node *yaml.Node) []string {
	v := mappingValue(node, "outputs")
	if v == nil {
		return nil
	}
	switch v.Kind {
	case yaml.MappingNode:
		var out []string
		for i := 0; i+1 < len(v.Content); i += 2 {
			out = append(out, v.Content[i].Value)
		}
		return out
	case yaml.SequenceNode:
		var out []string
		for _, item := range v.Content {
			if item.Kind == yaml.ScalarNode {
				out = append(out, item.Value)
			}
		}
		return out
	case yaml.ScalarNode:
		if isNullNode(v) || v.Value == "" {
			return nil
		}
		return []string{v.Value}
	default:
		return nil
	}
}

// parsePackages validates every entry's source via
// manifest.ValidateMarketplaceSource (req-mf-017) -- reusing the existing
// implementation manifest.go's own `marketplace:` block validation already
// calls, rather than reimplementing the rule here (gaps B2).
func parsePackages(node *yaml.Node) ([]PackageEntry, error) {
	v := mappingValue(node, "packages")
	// A bare "packages:" key with nothing after it (explicit YAML null)
	// counts as "no packages yet", the same way mkt-047's own bare
	// "marketplace:" key does (isNullNode) -- notably, this is what
	// yamlcore.SpliceSequenceElement's SeqRemove leaves behind when the
	// *last* remaining element of a block sequence is removed (the "- ..."
	// line disappears entirely, and a key with nothing after it parses as
	// null, not an empty sequence), so `package remove` down to zero
	// packages must not become a hard error here.
	if v == nil || isNullNode(v) {
		return nil, nil
	}
	if v.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("marketplace.packages must be a list")
	}

	entries := make([]PackageEntry, 0, len(v.Content))
	for i, item := range v.Content {
		if item.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("marketplace.packages[%d] must be a mapping", i)
		}
		source := scalarString(item, "source")
		if source != "" {
			if err := manifest.ValidateMarketplaceSource(source); err != nil {
				return nil, fmt.Errorf("marketplace.packages[%d]: %w", i, err)
			}
		}
		entries = append(entries, PackageEntry{
			Name:              scalarString(item, "name"),
			Description:       scalarString(item, "description"),
			Source:            source,
			Version:           scalarString(item, "version"),
			Ref:               scalarString(item, "ref"),
			Subdir:            scalarString(item, "subdir"),
			TagPattern:        scalarString(item, "tag_pattern"),
			Tags:              mergeTagsKeywords(item),
			IncludePrerelease: boolValue(item, "include_prerelease"),
			Category:          scalarString(item, "category"),
		})
	}
	return entries, nil
}

// mergeTagsKeywords implements the tags+keywords merge rule documented on
// PackageEntry.Tags.
func mergeTagsKeywords(item *yaml.Node) []string {
	tags := stringListValue(item, "tags")
	keywords := stringListValue(item, "keywords")
	if len(keywords) == 0 {
		return tags
	}
	seen := make(map[string]bool, len(tags)+len(keywords))
	merged := make([]string, 0, len(tags)+len(keywords))
	for _, t := range tags {
		if !seen[t] {
			seen[t] = true
			merged = append(merged, t)
		}
	}
	for _, k := range keywords {
		if !seen[k] {
			seen[k] = true
			merged = append(merged, k)
		}
	}
	return merged
}

// ── small yaml.Node mapping helpers ──

func mappingValue(m *yaml.Node, key string) *yaml.Node {
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

func scalarString(m *yaml.Node, key string) string {
	v := mappingValue(m, key)
	if v == nil || v.Kind != yaml.ScalarNode || isNullNode(v) {
		return ""
	}
	return v.Value
}

func boolValue(m *yaml.Node, key string) bool {
	v := mappingValue(m, key)
	if v == nil || v.Kind != yaml.ScalarNode {
		return false
	}
	return v.Value == "true"
}

func stringListValue(m *yaml.Node, key string) []string {
	v := mappingValue(m, key)
	if v == nil || v.Kind != yaml.SequenceNode {
		return nil
	}
	var out []string
	for _, item := range v.Content {
		if item.Kind == yaml.ScalarNode {
			out = append(out, item.Value)
		}
	}
	return out
}
