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
	ensureOraclePackLockfileFields(lockDoc)
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

// ensureOraclePackLockfileFields adds the two top-level keys the Oracle's
// lockfile serializer emits unconditionally but apm-go's omits when empty --
// ticket 27. The Oracle's LockFile.to_yaml (deps/lockfile.py:815-822) seeds
// its output dict with lockfile_version and generated_at, then assigns
// dependencies and deployments, all four outside any `if` guard; everything
// after that is `if <truthy>`. So an embedded lockfile built from a source
// file that declared neither key still comes out with `generated_at: ”` and
// `deployments: []`, while apm-go's came out with neither.
//
// This lives here, not in SerializeLockfile, on purpose. SerializeLockfile is
// also the top-level apm.lock.yaml write path, where apm-go deliberately
// guarantees something the Oracle does not: a byte-identical round-trip of an
// unchanged user file (AGENTS.md's YAML round-trip convention, locked down by
// TestWriteLockfile_RoundTrip_* in internal/lockfile). Making those two keys
// unconditional there would mean every `install` silently rewrites the user's
// lockfile to add them -- those tests caught exactly that when this fix was
// first attempted a layer too low. This file is already the designated
// "independent wrapping layer" for pack-only divergence -- see
// EnrichLockfileForPack's own doc comment ("SerializeLockfile 不動,
// lockfile_pack.go 獨立包裝層").
//
// deployments is appended only when SerializeLockfile did not already carry
// one through: "deployments" is absent from its knownTopKeys, so an existing
// key survives verbatim via the unknown-key preservation loop. Appending
// unconditionally would emit the key twice for an Oracle-written lockfile
// with real ledger rows -- a duplicate mapping key, worse than the omission
// being fixed. apm-go has not ported the deployment-ledger concept, so the
// empty list is the only value it can honestly produce for the absent case.
func ensureOraclePackLockfileFields(doc *yaml.Node) {
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return
	}

	present := make(map[string]bool, len(root.Content)/2)
	for i := 0; i+1 < len(root.Content); i += 2 {
		present[root.Content[i].Value] = true
	}

	if !present["generated_at"] {
		// Immediately after lockfile_version, matching the Oracle's own
		// insertion order.
		pair := []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "generated_at", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "", Tag: "!!str", Style: yaml.DoubleQuotedStyle},
		}
		at := 0
		if len(root.Content) >= 2 && root.Content[0].Value == "lockfile_version" {
			at = 2
		}
		rest := append([]*yaml.Node{}, root.Content[at:]...)
		root.Content = append(append(root.Content[:at:at], pair...), rest...)
	}

	if !present["deployments"] {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "deployments", Tag: "!!str"},
			&yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle},
		)
	}
}
