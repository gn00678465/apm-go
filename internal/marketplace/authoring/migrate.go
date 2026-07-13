// This file (migrate.go) implements mkt-044: `apm marketplace migrate`,
// folding a standalone legacy marketplace.yml into apm.yml's marketplace:
// block.
//
// Comment preservation (AC4) works by moving the actual *yaml.Node parsed
// from marketplace.yml -- not re-deriving it from an AuthoringConfig struct
// -- into apm.yml's own parsed Node tree, then writing the result back via
// yamlcore.PatchMappingPath's single-key-value-span splice: the same "never
// a full-document re-encode" contract editor.go and
// cmd/apm-go/marketplace_authoring.go's init already rely on (design.md's
// explicit callback to the PatchMappingPath lesson from the --mcp task).
// Every HeadComment/LineComment/FootComment attached anywhere in the legacy
// document's Node tree survives because it is the identical *yaml.Node
// object being rendered, not a copy reconstructed from scalar field
// values -- and because only the "marketplace: ..." key's byte span is
// replaced or inserted, every other byte of apm.yml (its own comments and
// hand formatting included) is untouched.
//
// This deliberately diverges from Python apm's own migrate_marketplace_yml
// (marketplace/migration.py), which re-dumps the *entire* apm.yml document
// via ruamel.yaml's round-trip loader/dumper: ruamel is built to preserve
// comments/formatting across a full re-encode, but go.yaml.in/yaml/v4 is
// not (see prd.md's Notes and the --mcp task's PatchMappingPath lesson), so
// the Go port must stay surgical instead of copying that re-encode
// strategy.
package authoring

import (
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/yamlcore"
)

// MigrateOptions holds `apm marketplace migrate`'s flags. Force covers all
// three of mkt-044's equivalent spellings (--force, --yes, -y are aliases
// for one flag, per Python's own
// click.option("--force", "--yes", "-y", "force", ...)); the CLI layer
// collapses them into this single bool before calling Migrate.
type MigrateOptions struct {
	// Force overwrites an existing non-null marketplace: block in apm.yml.
	Force bool
	// DryRun computes and returns the diff without writing apm.yml or
	// removing marketplace.yml.
	DryRun bool
}

// Migrate implements `apm marketplace migrate` (mkt-044): fold
// marketplace.yml into apm.yml's marketplace: block, preserving every
// comment in the legacy file (see this file's own doc comment), then
// remove marketplace.yml -- unless opts.DryRun, in which case neither file
// is touched. Returns a unified diff describing the proposed apm.yml
// change either way.
//
// Requires both marketplace.yml and apm.yml to already exist (mirrors
// Python's migrate_marketplace_yml: migrate never scaffolds apm.yml itself
// -- that is `apm marketplace init`'s job). apm.yml's existing
// marketplace: block must be null/absent unless opts.Force is set.
func Migrate(dir string, opts MigrateOptions) (diff string, err error) {
	legacyPath := filepath.Join(dir, legacyYMLFilename)
	apmPath := filepath.Join(dir, apmYMLFilename)

	legacyExists, err := fileExists(legacyPath)
	if err != nil {
		return "", err
	}
	if !legacyExists {
		return "", fmt.Errorf("marketplace.yml not found -- nothing to migrate")
	}
	apmExists, err := fileExists(apmPath)
	if err != nil {
		return "", err
	}
	if !apmExists {
		return "", fmt.Errorf("apm.yml not found; run 'apm-go init' first")
	}

	// Validate the legacy file parses and passes schema validation before
	// doing anything destructive -- mirrors Python's own
	// load_marketplace_from_legacy_yml call, made before it ever reads
	// apm.yml.
	legacyRoot, err := loadYAMLRoot(legacyPath)
	if err != nil {
		return "", err
	}
	if _, verr := parseAuthoringNode(legacyRoot, topLevelFields{}, true); verr != nil {
		return "", fmt.Errorf("marketplace.yml is invalid: %w", verr)
	}

	apmSrc, err := os.ReadFile(apmPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", apmPath, err)
	}
	apmDoc, err := yamlcore.SafeLoad(apmSrc)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", apmPath, err)
	}
	if len(apmDoc.Content) == 0 {
		return "", fmt.Errorf("%s is empty", apmPath)
	}
	root := apmDoc.Content[0]
	if root.Kind != yaml.MappingNode {
		return "", fmt.Errorf("%s: top-level must be a YAML mapping", apmPath)
	}

	existingIdx := topLevelKeyIndex(root, "marketplace")
	if existingIdx != -1 && !isNullNode(root.Content[existingIdx+1]) && !opts.Force {
		return "", fmt.Errorf("apm.yml already has a 'marketplace:' block; re-run with --force/--yes/-y to overwrite")
	}

	// Move legacyRoot in as the marketplace: key's value node -- the same
	// *yaml.Node object the legacy file's parse produced, comments and
	// all -- either replacing an existing (null, or --force'd non-null)
	// value, or appending a brand new key.
	if existingIdx != -1 {
		root.Content[existingIdx+1] = legacyRoot
	} else {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "marketplace"}
		root.Content = append(root.Content, keyNode, legacyRoot)
	}

	out, ok, perr := yamlcore.PatchMappingPath(apmSrc, apmDoc, []string{"marketplace"})
	if perr != nil {
		return "", perr
	}
	if !ok {
		return "", fmt.Errorf("unable to surgically migrate marketplace.yml into apm.yml: apm.yml's top-level structure is not supported for surgical editing")
	}

	diff = unifiedDiff(string(apmSrc), string(out), "apm.yml (current)", "apm.yml (after migrate)")

	if opts.DryRun {
		return diff, nil
	}

	// Re-validate the bytes actually about to be written, in memory,
	// before ever touching disk -- the same "never write a bad file"
	// discipline editor.go's editPackagesFile follows.
	if _, verr := yamlcore.SafeLoad(out); verr != nil {
		return "", fmt.Errorf("migration produced invalid YAML, aborting without writing: %w", verr)
	}
	if err := atomicWriteFile(apmPath, out); err != nil {
		return "", err
	}
	if err := os.Remove(legacyPath); err != nil {
		return "", fmt.Errorf("apm.yml was migrated but marketplace.yml could not be removed: %w", err)
	}
	return diff, nil
}

// topLevelKeyIndex returns the Content index of key's key-node within
// mapping node m ("m.Content[idx+1]" is the paired value), or -1.
func topLevelKeyIndex(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}
