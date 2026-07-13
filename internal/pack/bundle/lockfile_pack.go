package bundle

import (
	"sort"
	"time"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// PackMetadata is the pack: section prepended ahead of an apm.lock.yaml
// embedded in a bundle, mirroring enrich_lockfile_for_pack's pack_meta dict
// (bundle/lockfile_enrichment.py:241-269). BundleFiles values are
// deliberately BARE hex sha256 digests (no "sha256:" envelope prefix) --
// findings §3.6 point 1: matching Python's hashlib.sha256(...).hexdigest(),
// a DELIBERATE deviation from internal/lockfile.HashFileBytes's own
// "sha256:"-prefixed envelope convention, chosen for Python-bundle
// interop (Python's own _normalize_hash on the install/consumer side
// accepts both forms, so this only matters for producing an
// oracle-comparable byte layout).
type PackMetadata struct {
	Format      string
	Target      string            // comma-joined if originally a list; pure informational metadata (design.md)
	PackedAt    string            // ISO-8601 UTC; caller-supplied so tests can pin a value
	BundleFiles map[string]string // bundle-relative path -> bare hex sha256
}

// NewPackMetadata returns a PackMetadata with PackedAt set to now (UTC,
// RFC3339/ISO-8601 -- matching Python's datetime.now(timezone.utc).isoformat()).
func NewPackMetadata(format, target string, bundleFiles map[string]string) PackMetadata {
	return PackMetadata{
		Format:      format,
		Target:      target,
		PackedAt:    time.Now().UTC().Format(time.RFC3339),
		BundleFiles: bundleFiles,
	}
}

// toYAMLDoc builds the "pack:\n  format: ...\n  ..." top-level mapping
// document, mirroring enrich_lockfile_for_pack's field order (format,
// target, packed_at, bundle_files) and bundle_files' key-sorted-map
// requirement (lockfile_enrichment.py:269:
// "dict(sorted(bundle_files.items()))"). bundle_files is omitted entirely
// when empty, matching Python's "if bundle_files:" guard.
func (p PackMetadata) toYAMLDoc() *yaml.Node {
	packMap := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	addYAMLStr(packMap, "format", p.Format)
	addYAMLStr(packMap, "target", p.Target)
	addYAMLStr(packMap, "packed_at", p.PackedAt)
	if len(p.BundleFiles) > 0 {
		keys := make([]string, 0, len(p.BundleFiles))
		for k := range p.BundleFiles {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		filesMap := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, k := range keys {
			addYAMLStr(filesMap, k, p.BundleFiles[k])
		}
		packMap.Content = append(packMap.Content, yamlStrNode("bundle_files"), filesMap)
	}
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = append(root.Content, yamlStrNode("pack"), packMap)
	doc := &yaml.Node{Kind: yaml.DocumentNode}
	doc.Content = append(doc.Content, root)
	return doc
}

func yamlStrNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: s, Tag: "!!str"}
}

func addYAMLStr(m *yaml.Node, key, value string) {
	m.Content = append(m.Content, yamlStrNode(key), yamlStrNode(value))
}

// ParsePackMetadata extracts the "pack:" top-level section from an
// already-parsed apm.lock.yaml document's root mapping node (doc.Content[0]
// for a document produced by yamlcore.SafeLoad), mirroring Python's
// bundle/local_bundle.py: `_read_bundle_lockfile` (a plain yaml.safe_load,
// no schema validation) followed by `lockfile.get("pack") or {}`. Returns
// ok=false when root has no "pack" key at all, or "pack" is present but not
// a mapping -- callers (internal/localbundle) treat that the same as
// Python's empty-dict fallback (verify_bundle_integrity: an empty
// bundle_files map, so every bundle file is reported as "unlisted" --
// matching the oracle's own strict behavior for a lockfile that lacks pack
// metadata, rather than silently skipping verification).
func ParsePackMetadata(root *yaml.Node) (PackMetadata, bool) {
	if root == nil || root.Kind != yaml.MappingNode {
		return PackMetadata{}, false
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "pack" {
			continue
		}
		packNode := root.Content[i+1]
		if packNode.Kind != yaml.MappingNode {
			return PackMetadata{}, false
		}
		meta := PackMetadata{}
		for j := 0; j+1 < len(packNode.Content); j += 2 {
			key := packNode.Content[j].Value
			val := packNode.Content[j+1]
			switch key {
			case "format":
				meta.Format = val.Value
			case "target":
				meta.Target = val.Value
			case "packed_at":
				meta.PackedAt = val.Value
			case "bundle_files":
				if val.Kind == yaml.MappingNode {
					meta.BundleFiles = make(map[string]string, len(val.Content)/2)
					for k := 0; k+1 < len(val.Content); k += 2 {
						meta.BundleFiles[val.Content[k].Value] = val.Content[k+1].Value
					}
				}
			}
		}
		return meta, true
	}
	return PackMetadata{}, false
}

// EnrichLockfileForPack serializes lf -- with LocalDeployedFiles/
// LocalDeployedHashes stripped (findings §3.6 point 3: issue #887, a
// bundle's embedded lockfile must never carry the packager's own repo
// content) -- prefixed with a pack: metadata section, mirroring
// enrich_lockfile_for_pack (bundle/lockfile_enrichment.py:180-276):
// "pack_section + lockfile_yaml" string concatenation, NOT a merged YAML
// document -- SerializeLockfile/WriteLockfile stay untouched (design.md:
// "SerializeLockfile 不動，lockfile_pack.go 獨立包裝層"). original is lf's
// already-parsed source yaml.Node (for round-trip style preservation via
// SerializeLockfile), or nil for a from-scratch lockfile.
func EnrichLockfileForPack(lf *lockfile.Lockfile, meta PackMetadata, original *yaml.Node) ([]byte, error) {
	stripped := *lf
	stripped.LocalDeployedFiles = nil
	stripped.LocalDeployedHashes = nil

	lockDoc, err := lockfile.SerializeLockfile(&stripped, original)
	if err != nil {
		return nil, err
	}
	lockBytes, err := yamlcore.SafeDump(lockDoc)
	if err != nil {
		return nil, err
	}

	packBytes, err := yamlcore.SafeDump(meta.toYAMLDoc())
	if err != nil {
		return nil, err
	}

	return append(packBytes, lockBytes...), nil
}
