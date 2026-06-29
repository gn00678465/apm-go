package lockfile

import (
	"sort"
	"strconv"

	"github.com/apm-go/apm/internal/yamlcore"
	"go.yaml.in/yaml/v4"
)

// Field order derived from oracle fixtures (tree_sha256 before depth).
var entryFieldOrder = []string{
	"repo_url", "host", "port", "source",
	"resolved_commit", "resolved_ref", "resolved_tag",
	"resolved_url", "resolved_hash",
	"constraint", "resolved_at", "resolved_by",
	"version", "virtual_path", "is_virtual",
	"tree_sha256", "depth", "content_hash",
	"package_type", "skill_subset",
	"local_path", "is_dev",
	"deployed_files", "deployed_file_hashes",
}

// WriteLockfile serializes a Lockfile to bytes via yaml.Node for round-trip fidelity.
func WriteLockfile(lf *Lockfile, original *yaml.Node) ([]byte, error) {
	node, err := SerializeLockfile(lf, original)
	if err != nil {
		return nil, err
	}
	return yamlcore.SafeDump(node)
}

// SerializeLockfile builds a yaml.Node tree from a Lockfile.
// If original is provided, unknown fields and x-* keys are preserved.
func SerializeLockfile(lf *Lockfile, original *yaml.Node) (*yaml.Node, error) {
	SortDependencies(lf.Dependencies)

	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

	// Build index of original top-level pairs for style preservation
	origTopPairs := buildOriginalTopPairs(original)

	addScalarPreserve(root, "lockfile_version", lf.Version, origTopPairs)
	if lf.GeneratedAt != "" {
		addScalarPreserve(root, "generated_at", lf.GeneratedAt, origTopPairs)
	}
	if lf.APMVersion != "" {
		addScalarPreserve(root, "apm_version", lf.APMVersion, origTopPairs)
	}

	// Preserve top-level x-* and unknown keys from original.
	// Note: local_deployed_files and local_deployed_file_hashes are NOT in this set
	// because they are not parsed into Lockfile struct fields; treating them as unknown
	// keys ensures they are preserved on round-trip.
	knownTopKeys := map[string]bool{
		"lockfile_version": true, "generated_at": true, "apm_version": true,
		"dependencies": true,
	}
	if original != nil {
		origRoot := original
		if origRoot.Kind == yaml.DocumentNode && len(origRoot.Content) > 0 {
			origRoot = origRoot.Content[0]
		}
		if origRoot.Kind == yaml.MappingNode {
			for i := 0; i < len(origRoot.Content)-1; i += 2 {
				key := origRoot.Content[i].Value
				if !knownTopKeys[key] {
					root.Content = append(root.Content, origRoot.Content[i], origRoot.Content[i+1])
				}
			}
		}
	}

	// Dependencies sequence
	depsSeq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	originalEntries := buildOriginalEntriesIndex(original)

	for _, dep := range lf.Dependencies {
		entryNode := serializeEntry(&dep, originalEntries[dep.UniqueKey()])
		depsSeq.Content = append(depsSeq.Content, entryNode)
	}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "dependencies", Tag: "!!str"},
		depsSeq,
	)

	doc := &yaml.Node{Kind: yaml.DocumentNode}
	doc.Content = append(doc.Content, root)
	return doc, nil
}

func serializeEntry(dep *LockedDep, original *yaml.Node) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

	// Build index of original key/value node pairs for style preservation
	origPairs := buildOriginalPairs(original)

	// Write fields in canonical order, omitting empty values
	fields := map[string]string{
		"repo_url":        dep.RepoURL,
		"source":          dep.Source,
		"resolved_commit": dep.ResolvedCommit,
		"resolved_ref":    dep.ResolvedRef,
		"resolved_tag":    dep.ResolvedTag,
		"resolved_url":    dep.ResolvedURL,
		"resolved_hash":   dep.ResolvedHash,
		"constraint":      dep.Constraint,
		"resolved_at":     dep.ResolvedAt,
		"resolved_by":     dep.ResolvedBy,
		"version":         dep.Version,
		"virtual_path":    dep.VirtualPath,
		"tree_sha256":     dep.TreeSHA256,
		"content_hash":    "",
		"local_path":      "",
	}

	for _, key := range entryFieldOrder {
		val, ok := fields[key]
		if !ok {
			continue
		}
		if val == "" {
			continue
		}
		// Reuse original node pair if value unchanged (preserves quote style)
		if pair, exists := origPairs[key]; exists && pair.val.Value == val {
			node.Content = append(node.Content, pair.key, pair.val)
		} else {
			addScalar(node, key, val)
		}
	}

	// depth (int field, omit if 0)
	if dep.Depth > 0 {
		depthStr := strconv.Itoa(dep.Depth)
		if pair, exists := origPairs["depth"]; exists && pair.val.Value == depthStr {
			node.Content = append(node.Content, pair.key, pair.val)
		} else {
			node.Content = append(node.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: "depth", Tag: "!!str"},
				&yaml.Node{Kind: yaml.ScalarNode, Value: depthStr, Tag: "!!int"},
			)
		}
	}

	// skill_subset (list)
	if len(dep.SkillSubset) > 0 {
		if pair, exists := origPairs["skill_subset"]; exists && deployedFilesMatch(pair.val, dep.SkillSubset) {
			node.Content = append(node.Content, pair.key, pair.val)
		} else {
			seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for _, s := range dep.SkillSubset {
				seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: s, Tag: "!!str"})
			}
			node.Content = append(node.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: "skill_subset", Tag: "!!str"},
				seq,
			)
		}
	}

	// deployed_files (list) — reuse original only if unchanged
	if len(dep.DeployedFiles) > 0 {
		if pair, exists := origPairs["deployed_files"]; exists && deployedFilesMatch(pair.val, dep.DeployedFiles) {
			node.Content = append(node.Content, pair.key, pair.val)
		} else {
			seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for _, f := range dep.DeployedFiles {
				seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: f, Tag: "!!str"})
			}
			node.Content = append(node.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: "deployed_files", Tag: "!!str"},
				seq,
			)
		}
	}

	// deployed_file_hashes (map) — reuse original only if unchanged
	if len(dep.DeployedHashes) > 0 {
		if pair, exists := origPairs["deployed_file_hashes"]; exists && deployedHashesMatch(pair.val, dep.DeployedHashes) {
			node.Content = append(node.Content, pair.key, pair.val)
		} else {
			mapNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			keys := make([]string, 0, len(dep.DeployedHashes))
			for k := range dep.DeployedHashes {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				addScalar(mapNode, k, dep.DeployedHashes[k])
			}
			node.Content = append(node.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: "deployed_file_hashes", Tag: "!!str"},
				mapNode,
			)
		}
	}

	// Preserve unknown/x-* fields from original entry
	if original != nil && original.Kind == yaml.MappingNode {
		for i := 0; i < len(original.Content)-1; i += 2 {
			key := original.Content[i].Value
			if !isKnownEntryField(key) {
				node.Content = append(node.Content, original.Content[i], original.Content[i+1])
			}
		}
	}

	return node
}

type nodePair struct {
	key *yaml.Node
	val *yaml.Node
}

func buildOriginalPairs(original *yaml.Node) map[string]nodePair {
	pairs := make(map[string]nodePair)
	if original == nil || original.Kind != yaml.MappingNode {
		return pairs
	}
	for i := 0; i < len(original.Content)-1; i += 2 {
		key := original.Content[i].Value
		pairs[key] = nodePair{key: original.Content[i], val: original.Content[i+1]}
	}
	return pairs
}

// IsSemanticEqual returns true if two lockfiles differ only in advisory fields (req-lk-005).
// Advisory fields: generated_at, apm_version, resolved_at.
// Order-insensitive: dependencies are matched by unique key, not by position.
func IsSemanticEqual(a, b *Lockfile) bool {
	if a.Version != b.Version {
		return false
	}
	if len(a.Dependencies) != len(b.Dependencies) {
		return false
	}
	bIndex := make(map[string]*LockedDep, len(b.Dependencies))
	for i := range b.Dependencies {
		bIndex[b.Dependencies[i].UniqueKey()] = &b.Dependencies[i]
	}
	for i := range a.Dependencies {
		bDep, ok := bIndex[a.Dependencies[i].UniqueKey()]
		if !ok {
			return false
		}
		if !depSemanticEqual(&a.Dependencies[i], bDep) {
			return false
		}
	}
	return true
}

func depSemanticEqual(a, b *LockedDep) bool {
	return a.RepoURL == b.RepoURL &&
		a.VirtualPath == b.VirtualPath &&
		a.Source == b.Source &&
		a.ResolvedCommit == b.ResolvedCommit &&
		a.ResolvedRef == b.ResolvedRef &&
		a.ResolvedTag == b.ResolvedTag &&
		a.ResolvedURL == b.ResolvedURL &&
		a.ResolvedHash == b.ResolvedHash &&
		a.Constraint == b.Constraint &&
		a.ResolvedBy == b.ResolvedBy &&
		a.Version == b.Version &&
		a.Depth == b.Depth &&
		a.TreeSHA256 == b.TreeSHA256 &&
		slicesEqual(a.DeployedFiles, b.DeployedFiles) &&
		mapsEqual(a.DeployedHashes, b.DeployedHashes)
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// DetermineVersion returns the lockfile version based on entries and monotonicity (req-lk-002).
func DetermineVersion(deps []LockedDep, existingVersion string) string {
	for _, d := range deps {
		if d.Source == "registry" {
			return "2"
		}
	}
	if existingVersion == "2" {
		return "2"
	}
	return "1"
}

// SortDependencies sorts by (repo_url, virtual_path) ascending (req-lk-005).
func SortDependencies(deps []LockedDep) {
	sort.Slice(deps, func(i, j int) bool {
		if deps[i].RepoURL != deps[j].RepoURL {
			return deps[i].RepoURL < deps[j].RepoURL
		}
		return deps[i].VirtualPath < deps[j].VirtualPath
	})
}

// addScalarPreserve adds a key-value pair, reusing original nodes if value unchanged.
func addScalarPreserve(node *yaml.Node, key, value string, origPairs map[string]nodePair) {
	if pair, ok := origPairs[key]; ok && pair.val.Value == value {
		node.Content = append(node.Content, pair.key, pair.val)
		return
	}
	addScalar(node, key, value)
}

func buildOriginalTopPairs(original *yaml.Node) map[string]nodePair {
	pairs := make(map[string]nodePair)
	if original == nil {
		return pairs
	}
	root := original
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return pairs
	}
	for i := 0; i < len(root.Content)-1; i += 2 {
		key := root.Content[i].Value
		pairs[key] = nodePair{key: root.Content[i], val: root.Content[i+1]}
	}
	return pairs
}

func addScalar(node *yaml.Node, key, value string) {
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value, Tag: "!!str"},
	)
}

func buildOriginalEntriesIndex(original *yaml.Node) map[string]*yaml.Node {
	idx := make(map[string]*yaml.Node)
	if original == nil {
		return idx
	}
	root := original
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return idx
	}
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == "dependencies" {
			seq := root.Content[i+1]
			if seq.Kind == yaml.SequenceNode {
				for _, entry := range seq.Content {
					key := extractEntryKey(entry)
					if key != "" {
						idx[key] = entry
					}
				}
			}
		}
	}
	return idx
}

func extractEntryKey(entry *yaml.Node) string {
	if entry.Kind != yaml.MappingNode {
		return ""
	}
	repoURL := ""
	virtualPath := ""
	for i := 0; i < len(entry.Content)-1; i += 2 {
		switch entry.Content[i].Value {
		case "repo_url":
			repoURL = entry.Content[i+1].Value
		case "virtual_path":
			virtualPath = entry.Content[i+1].Value
		}
	}
	if virtualPath != "" {
		return repoURL + "/" + virtualPath
	}
	return repoURL
}

// knownEntryFields lists fields that the serializer explicitly handles.
// Fields NOT listed here are preserved verbatim from the original node (passthrough).
// Deliberately excludes: host, port, is_virtual, package_type, skill_subset,
// is_dev, content_hash, local_path — these are spec-recognized optional fields
// that the serializer does not yet model but must survive round-trip (req-lk-011).
var knownEntryFields = map[string]bool{
	"repo_url": true, "source": true,
	"resolved_commit": true, "resolved_ref": true, "resolved_tag": true,
	"resolved_url": true, "resolved_hash": true,
	"constraint": true, "resolved_at": true, "resolved_by": true,
	"version": true, "virtual_path": true,
	"tree_sha256": true, "depth": true,
	"skill_subset": true,
	"deployed_files": true, "deployed_file_hashes": true,
}

func isKnownEntryField(key string) bool {
	return knownEntryFields[key]
}

func deployedFilesMatch(node *yaml.Node, files []string) bool {
	if node == nil || node.Kind != yaml.SequenceNode {
		return false
	}
	if len(node.Content) != len(files) {
		return false
	}
	for i, f := range files {
		if node.Content[i].Value != f {
			return false
		}
	}
	return true
}

func deployedHashesMatch(node *yaml.Node, hashes map[string]string) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	if len(node.Content)/2 != len(hashes) {
		return false
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		k := node.Content[i].Value
		v := node.Content[i+1].Value
		if hashes[k] != v {
			return false
		}
	}
	return true
}
