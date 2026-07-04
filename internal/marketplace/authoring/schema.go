// Package authoring implements the producer-side `apm marketplace` data
// model: the `marketplace:` authoring block that a project author maintains
// in apm.yml (or, for pre-migration projects, a standalone legacy
// marketplace.yml) to describe how their repo's plugins are packaged for
// consumption via `apm marketplace add`/`apm install`.
//
// This file (schema.go) only covers the data model and its loading
// (mkt-047, req-mf-017): AuthoringConfig and LoadAuthoringConfig. Field
// scope deliberately follows design.md, not Python apm's full
// yml_schema.py.
//
// mkt-053's codex-output `category` required-field gate deliberately does
// NOT live here (F3 fix): LoadAuthoringConfig is shared by `apm pack`'s
// config loading, `apm marketplace package add/remove/set`'s pre-edit load
// (editor.go), and `apm marketplace migrate` -- none of which should be
// blocked by a rule that only matters once a codex build is actually
// composed (e.g. `apm pack -m claude` with a codex-missing-category package
// must succeed). That gate is enforced compose-time-only, in
// internal/marketplace/build/codexmapper.go's CodexMapper.Compose, mirroring
// the Python original's own compose-time-only BuildError
// (output_mappers.py, not yml_schema.py).
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
			"these are mutually exclusive — remove one, or run 'apm-go marketplace migrate' " +
			"to fold marketplace.yml into apm.yml",
	)
	errNoMarketplaceConfig = errors.New(
		"no marketplace authoring config found (neither apm.yml's marketplace: block " +
			"nor a legacy marketplace.yml exist); run 'apm-go marketplace init' to scaffold one",
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
	Name  string
	Email string
	URL   string
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
	// Homepage, Author, License, Repository are Anthropic pass-through
	// fields (mkt-050/052 修訂版's plugin-level table): Homepage is only
	// ever emitted for a *local* package (design.md); Author is
	// normalized to a Claude-Code-compliant {name, email?, url?} object
	// whether the YAML `author:` key was authored as a bare string
	// (treated as name) or a mapping.
	Homepage   string
	Author     map[string]string
	License    string
	Repository string
}

// AuthoringConfig is the parsed marketplace: authoring block, regardless of
// whether it came from apm.yml or a legacy marketplace.yml.
//
// Name/Description/Version mirror Python apm's MarketplaceConfig
// inheritance rule: each is read from the marketplace: block's own
// same-named key when present and non-null (an "override"), otherwise
// inherited from apm.yml's own top-level scalar of the same name (or, for a
// legacy standalone marketplace.yml, read directly -- DescriptionOverridden
// and VersionOverridden are always true for a legacy config, since there is
// no separate top level to inherit from). ClaudeMapper (mkt-050/052 修訂版)
// only ever emits Description/Version at the marketplace.json top level
// when the corresponding Overridden flag is true; Name is unconditional.
type AuthoringConfig struct {
	Name                  string
	Description           string
	Version               string
	DescriptionOverridden bool
	VersionOverridden     bool
	Owner                 Owner
	Build                 Build
	Outputs               []string
	// Metadata is the marketplace.metadata block, preserved verbatim
	// (arbitrary caller-defined keys, including "pluginRoot") for
	// ClaudeMapper to pass through to marketplace.json's top-level
	// "metadata" key.
	Metadata map[string]any
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

	apmRoot, apmBlock, err := loadApmMarketplaceBlock(apmPath)
	if err != nil {
		return nil, 0, err
	}

	switch {
	case apmBlock != nil && legacyExists:
		return nil, 0, errMarketplaceConfigsMutuallyExclusive
	case apmBlock != nil:
		inherited := topLevelFields{
			name:        scalarString(apmRoot, "name"),
			description: scalarString(apmRoot, "description"),
			version:     scalarString(apmRoot, "version"),
		}
		cfg, err := parseAuthoringNode(apmBlock, inherited, false)
		if err != nil {
			return nil, 0, err
		}
		return cfg, ConfigSourceApmYML, nil
	case legacyExists:
		root, err := loadYAMLRoot(legacyPath)
		if err != nil {
			return nil, 0, err
		}
		cfg, err := parseAuthoringNode(root, topLevelFields{}, true)
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

// loadApmMarketplaceBlock returns apm.yml's own top-level mapping node
// (root) plus its top-level `marketplace:` value node (block) -- root is
// needed so LoadAuthoringConfig can read the sibling top-level name/
// description/version scalars an unoverridden marketplace: block inherits
// (AuthoringConfig's doc comment). block is nil if apm.yml does not exist,
// has no `marketplace:` key, or the key's value is explicit YAML null
// (`marketplace:` with nothing after it) -- the "_has_marketplace_block"
// semantics mkt-047 requires; root is nil under those same first two
// conditions (apm.yml missing) but still populated when the key is merely
// absent/null, since the file itself was read successfully.
func loadApmMarketplaceBlock(apmPath string) (root, block *yaml.Node, err error) {
	exists, err := fileExists(apmPath)
	if err != nil {
		return nil, nil, err
	}
	if !exists {
		return nil, nil, nil
	}

	data, err := os.ReadFile(apmPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", apmPath, err)
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", apmPath, err)
	}
	if len(doc.Content) == 0 {
		return nil, nil, nil
	}
	root = doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("%s: top-level must be a YAML mapping", apmPath)
	}

	val := mappingValue(root, "marketplace")
	if val == nil || isNullNode(val) {
		return root, nil, nil
	}
	return root, val, nil
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

// topLevelFields carries apm.yml's own top-level name/description/version
// scalars -- the inheritance fallback an unoverridden marketplace: block's
// same-named key falls back to (AuthoringConfig's doc comment). Zero value
// for a legacy standalone marketplace.yml, which has no separate top level
// to inherit from (its own name/description/version are read directly from
// the block node itself).
type topLevelFields struct {
	name, description, version string
}

// parseAuthoringNode builds an AuthoringConfig from a marketplace block
// mapping node -- either apm.yml's `marketplace:` value, or a legacy
// marketplace.yml's document root, which share the same field shape.
// inherited supplies apm.yml's own top-level name/description/version for
// the non-legacy case (ignored, pass the zero value, when isLegacy is
// true -- node itself already holds those keys directly, and a legacy
// config's description/version are always considered "overridden" since
// there is no separate top level for them to inherit from).
func parseAuthoringNode(node *yaml.Node, inherited topLevelFields, isLegacy bool) (*AuthoringConfig, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("marketplace config must be a YAML mapping")
	}
	packages, err := parsePackages(node)
	if err != nil {
		return nil, err
	}
	metadata, err := parseMetadata(node)
	if err != nil {
		return nil, err
	}

	name, nameOverridden := overridableString(node, "name")
	if !nameOverridden {
		name = inherited.name
	}
	description, descriptionOverridden := overridableString(node, "description")
	if !descriptionOverridden {
		description = inherited.description
	}
	version, versionOverridden := overridableString(node, "version")
	if !versionOverridden {
		version = inherited.version
	}
	if isLegacy {
		descriptionOverridden = true
		versionOverridden = true
	}

	outputs := parseOutputs(node)

	return &AuthoringConfig{
		Name:                  name,
		Description:           description,
		Version:               version,
		DescriptionOverridden: descriptionOverridden,
		VersionOverridden:     versionOverridden,
		Owner:                 parseOwner(node),
		Build:                 parseBuild(node),
		Outputs:               outputs,
		Metadata:              metadata,
		Packages:              packages,
	}, nil
}

// overridableString returns key's scalar value on node and whether key was
// present with an explicit, non-null value -- distinct from the returned
// value simply being the empty-string zero value -- mirroring Python's
// "key in raw_block and raw_block[key] is not None" override-detection
// semantics used for marketplace.name/description/version.
func overridableString(node *yaml.Node, key string) (value string, overridden bool) {
	v := mappingValue(node, key)
	if v == nil || isNullNode(v) {
		return "", false
	}
	if v.Kind == yaml.ScalarNode {
		return v.Value, true
	}
	return "", true
}

func parseOwner(node *yaml.Node) Owner {
	v := mappingValue(node, "owner")
	return Owner{
		Name:  scalarString(v, "name"),
		Email: scalarString(v, "email"),
		URL:   scalarString(v, "url"),
	}
}

// parseMetadata returns the marketplace.metadata block decoded to a plain
// map[string]any, preserving arbitrary caller-defined keys (including
// "pluginRoot") verbatim for ClaudeMapper's pass-through "metadata" output
// field -- nil when the key is absent or explicit YAML null.
func parseMetadata(node *yaml.Node) (map[string]any, error) {
	v := mappingValue(node, "metadata")
	if v == nil || isNullNode(v) {
		return nil, nil
	}
	if v.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("marketplace.metadata must be a mapping")
	}
	var out map[string]any
	if err := v.Decode(&out); err != nil {
		return nil, fmt.Errorf("marketplace.metadata: %w", err)
	}
	return out, nil
}

// parseAuthor normalizes a packages[].author value to a Claude-Code-
// compliant {name, email?, url?} object: a bare string is treated as name;
// a mapping's name/email/url scalar keys are copied over. Returns nil when
// the key is absent, explicit YAML null, or -- defensively -- an author
// mapping with no usable string values at all.
func parseAuthor(item *yaml.Node) map[string]string {
	v := mappingValue(item, "author")
	if v == nil || isNullNode(v) {
		return nil
	}
	if v.Kind == yaml.ScalarNode {
		if v.Value == "" {
			return nil
		}
		return map[string]string{"name": v.Value}
	}
	if v.Kind != yaml.MappingNode {
		return nil
	}
	out := make(map[string]string, 3)
	if name := scalarString(v, "name"); name != "" {
		out["name"] = name
	}
	if email := scalarString(v, "email"); email != "" {
		out["email"] = email
	}
	if url := scalarString(v, "url"); url != "" {
		out["url"] = url
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
			Homepage:          scalarString(item, "homepage"),
			Author:            parseAuthor(item),
			License:           scalarString(item, "license"),
			Repository:        scalarString(item, "repository"),
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
