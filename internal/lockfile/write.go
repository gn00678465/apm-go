package lockfile

import (
	"sort"
	"strconv"

	"github.com/apm-go/apm/internal/yamlcore"
	"go.yaml.in/yaml/v4"
)

// entryFieldOrder is the key order of one dependencies entry: the order the
// Oracle's LockedDependency.to_dict assigns them (deps/lockfile.py:351).
// A field apm-go does not model is passed through from the original entry in
// the same slot; tree_sha256 is apm-go's own field, which the Oracle would
// replay after every field it knows.
var entryFieldOrder = []string{
	"repo_url", "materialization_repo_url", "alias", "name", "host", "host_type",
	"port", "registry_prefix", "resolved_commit", "resolved_ref", "version",
	"virtual_path", "is_virtual", "depth", "resolved_by", "package_type",
	"deployed_files", "deployed_file_hashes", "source", "local_path",
	"declaring_parent", "anchored_local_path", "content_hash", "is_dev",
	"discovered_via", "marketplace_plugin_name", "source_url", "source_digest",
	"is_insecure", "allow_insecure", "skill_subset", "target_subset",
	"resolved_url", "resolved_hash", "constraint", "resolved_tag", "resolved_at",
	"declared_license", "exec_status",
	"tree_sha256",
}

// topFieldOrder is the top-level key order of the Oracle's LockFile.to_yaml
// (deps/lockfile.py:844). Keys apm-go does not model keep their slot.
var topFieldOrder = []string{
	"lockfile_version", "generated_at", "apm_version", "dependencies",
	"deployments", "mcp_servers", "mcp_configs", "mcp_target_servers",
	"mcp_config_provenance", "lsp_servers", "lsp_configs", "lsp_target_servers",
	"lsp_config_provenance", "local_deployed_files", "local_deployed_file_hashes",
}

// WriteLockfile serializes a Lockfile to the bytes of apm.lock.yaml. The
// result is the Oracle's lock byte for byte, except the apm_version value:
// PyYAML's block style, single quotes, indentless sequences. generated_at is
// not written (the Oracle stopped writing it in 0.29.0). original supplies
// the keys and entry fields apm-go does not model, so they survive; comments
// and the quote style of the original file do not.
func WriteLockfile(lf *Lockfile, original *yaml.Node) ([]byte, error) {
	withoutTimestamp := *lf
	withoutTimestamp.GeneratedAt = ""
	node, err := SerializeLockfile(&withoutTimestamp, original)
	if err != nil {
		return nil, err
	}
	return yamlcore.SafeDumpManifest(node)
}

// SerializeLockfile builds a yaml.Node tree from a Lockfile.
// If original is provided, unknown fields and x-* keys are preserved.
func SerializeLockfile(lf *Lockfile, original *yaml.Node) (*yaml.Node, error) {
	SortDependencies(lf.Dependencies)

	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	origTop := buildOriginalTopPairs(original)
	originalEntries := buildOriginalEntriesIndex(original)
	known := make(map[string]bool, len(topFieldOrder))

	for _, key := range topFieldOrder {
		known[key] = true
		switch key {
		case "lockfile_version":
			addScalar(root, key, lf.Version)
		case "generated_at":
			if lf.GeneratedAt != "" {
				addScalar(root, key, lf.GeneratedAt)
			}
		case "apm_version":
			if lf.APMVersion != "" {
				addScalar(root, key, lf.APMVersion)
			}
		case "dependencies":
			seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for i := range lf.Dependencies {
				dep := &lf.Dependencies[i]
				seq.Content = append(seq.Content, serializeEntry(dep, originalEntries[dep.UniqueKey()]))
			}
			addNode(root, key, flowIfEmpty(seq))
		case "deployments":
			addNode(root, key, flowIfEmpty(deploymentRows(lf)))
		case "mcp_servers":
			if len(lf.MCPServers) > 0 {
				addNode(root, key, stringSeq(sortedCopy(lf.MCPServers)))
			}
		case "mcp_configs":
			if len(lf.MCPConfigs) > 0 {
				m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				for _, name := range sortedKeys(lf.MCPConfigs) {
					addNode(m, name, lf.MCPConfigs[name])
				}
				addNode(root, key, m)
			}
		case "mcp_target_servers":
			if len(lf.MCPTargetServers) > 0 {
				m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				for _, target := range sortedKeys(lf.MCPTargetServers) {
					addNode(m, target, flowIfEmpty(stringSeq(sortedCopy(lf.MCPTargetServers[target]))))
				}
				addNode(root, key, m)
			}
		case "mcp_config_provenance":
			// Sorted by server, a list of owners sorted too
			// (deps/lockfile.py:160-166,881-883).
			if len(lf.MCPConfigProvenance) > 0 {
				m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				for _, server := range sortedKeys(lf.MCPConfigProvenance) {
					owners := lf.MCPConfigProvenance[server]
					if owners.List {
						addNode(m, server, flowIfEmpty(stringSeq(sortedCopy(owners.Names))))
					} else if len(owners.Names) == 1 {
						addScalar(m, server, owners.Names[0])
					}
				}
				addNode(root, key, m)
			}
		case "local_deployed_files":
			if len(lf.LocalDeployedFiles) > 0 {
				addNode(root, key, stringSeq(sortedCopy(lf.LocalDeployedFiles)))
			}
		case "local_deployed_file_hashes":
			if len(lf.LocalDeployedHashes) > 0 {
				addNode(root, key, stringMap(lf.LocalDeployedHashes))
			}
		default:
			if pair, ok := origTop[key]; ok {
				root.Content = append(root.Content, pair.key, pair.val)
			}
		}
	}

	// Top-level keys no version of the lock format names (x-* and others).
	if origRoot := mappingRoot(original); origRoot != nil {
		for i := 0; i < len(origRoot.Content)-1; i += 2 {
			if !known[origRoot.Content[i].Value] {
				root.Content = append(root.Content, origRoot.Content[i], origRoot.Content[i+1])
			}
		}
	}

	doc := &yaml.Node{Kind: yaml.DocumentNode}
	doc.Content = append(doc.Content, root)
	return doc, nil
}

func serializeEntry(dep *LockedDep, original *yaml.Node) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	origPairs := buildOriginalPairs(original)

	scalars := map[string]string{
		"repo_url":                dep.RepoURL,
		"alias":                   dep.Alias,
		"name":                    dep.Name,
		"host":                    dep.Host,
		"registry_prefix":         dep.RegistryPrefix,
		"resolved_commit":         dep.ResolvedCommit,
		"resolved_ref":            dep.ResolvedRef,
		"version":                 dep.Version,
		"virtual_path":            dep.VirtualPath,
		"resolved_by":             dep.ResolvedBy,
		"package_type":            dep.PackageType,
		"source":                  dep.Source,
		"local_path":              dep.LocalPath,
		"discovered_via":          dep.DiscoveredVia,
		"marketplace_plugin_name": dep.MarketplacePluginName,
		"source_url":              dep.SourceURL,
		"source_digest":           dep.SourceDigest,
		"resolved_url":            dep.ResolvedURL,
		"resolved_hash":           dep.ResolvedHash,
		"constraint":              dep.Constraint,
		"resolved_tag":            dep.ResolvedTag,
		"resolved_at":             dep.ResolvedAt,
		"tree_sha256":             dep.TreeSHA256,
	}

	for _, key := range entryFieldOrder {
		if val, modeled := scalars[key]; modeled {
			if val != "" {
				addScalar(node, key, val)
			}
			continue
		}
		switch key {
		case "port":
			if dep.Port > 0 {
				addNode(node, key, intNode(dep.Port))
			}
		case "is_virtual":
			addTrue(node, key, dep.IsVirtual)
		case "is_insecure":
			addTrue(node, key, dep.IsInsecure)
		case "allow_insecure":
			addTrue(node, key, dep.AllowInsecure)
		case "depth":
			// The Oracle's default depth is 1 and it writes only another value.
			if dep.Depth > 1 {
				addNode(node, key, intNode(dep.Depth))
			}
		case "deployed_files":
			if len(dep.DeployedFiles) > 0 {
				addNode(node, key, stringSeq(sortedUnique(dep.DeployedFiles)))
			}
		case "deployed_file_hashes":
			if len(dep.DeployedHashes) > 0 {
				addNode(node, key, stringMap(dep.DeployedHashes))
			}
		case "skill_subset":
			if len(dep.SkillSubset) > 0 {
				addNode(node, key, stringSeq(sortedCopy(dep.SkillSubset)))
			}
		case "target_subset":
			if len(dep.TargetSubset) > 0 {
				addNode(node, key, stringSeq(sortedCopy(dep.TargetSubset)))
			}
		default:
			if pair, ok := origPairs[key]; ok {
				node.Content = append(node.Content, pair.key, pair.val)
			}
		}
	}

	// Preserve unknown/x-* fields from original entry
	if original != nil && original.Kind == yaml.MappingNode {
		for i := 0; i < len(original.Content)-1; i += 2 {
			if !isEntryField(original.Content[i].Value) {
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
	if !slicesEqual(a.LocalDeployedFiles, b.LocalDeployedFiles) {
		return false
	}
	if !mapsEqual(a.LocalDeployedHashes, b.LocalDeployedHashes) {
		return false
	}
	if !slicesEqual(a.MCPServers, b.MCPServers) {
		return false
	}
	if !mcpConfigsEqual(a.MCPConfigs, b.MCPConfigs) || !targetServersEqual(a.MCPTargetServers, b.MCPTargetServers) ||
		!provenanceEqual(a.MCPConfigProvenance, b.MCPConfigProvenance) {
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
		a.Host == b.Host &&
		a.Port == b.Port &&
		a.RegistryPrefix == b.RegistryPrefix &&
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
		a.Name == b.Name &&
		a.LocalPath == b.LocalPath &&
		max(a.Depth, 1) == max(b.Depth, 1) &&
		a.TreeSHA256 == b.TreeSHA256 &&
		// Marketplace provenance (mkt-031) participates in semantic equality
		// (mirrors the Python original's is_semantically_equivalent): a
		// no-provenance rebuild is NOT a no-op against a provenance-bearing
		// existing entry, so the no-op check (IsSemanticEqual, called from
		// deployAndFinalize before writing apm.lock.yaml) doesn't mask a
		// provenance loss as "Already up to date". mkt-032's carry-forward
		// (a later step) is what keeps a legitimate rebuild's provenance
		// identical to what was already locked, so this participation
		// causes no spurious rewrites in practice.
		a.DiscoveredVia == b.DiscoveredVia &&
		a.MarketplacePluginName == b.MarketplacePluginName &&
		a.SourceURL == b.SourceURL &&
		a.SourceDigest == b.SourceDigest &&
		// PackageType (R9.4/AC45) is content, not advisory: a change from ""
		// to "marketplace_plugin" (e.g. a dependency moving from
		// dependencies.apm to devDependencies.apm) must not be treated as a
		// no-op that skips rewriting apm.lock.yaml.
		a.PackageType == b.PackageType &&
		a.Alias == b.Alias &&
		a.IsVirtual == b.IsVirtual &&
		a.IsInsecure == b.IsInsecure &&
		a.AllowInsecure == b.AllowInsecure &&
		// SkillSubset (BUG-2's per-dep --skill filter) must participate in
		// semantic equality too: without this, a --skill RESET/narrowing
		// that changes nothing else about the dependency (same commit,
		// same everything) was invisible to the no-op check above, so
		// deployAndFinalize returned "Already up to date" BEFORE ever
		// reaching the apm.yml/apm.lock.yaml write step -- silently
		// discarding the very subset change the caller just asked for
		// (found via BUG-1's case-fold RESET regression test, which
		// exercises exactly this "nothing else changed" shape).
		slicesEqual(a.SkillSubset, b.SkillSubset) &&
		slicesEqual(a.TargetSubset, b.TargetSubset) &&
		slicesEqual(a.DeployedFiles, b.DeployedFiles) &&
		mapsEqual(a.DeployedHashes, b.DeployedHashes)
}

func targetServersEqual(a, b map[string][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for target, servers := range a {
		other, ok := b[target]
		if !ok || !slicesEqual(sortedCopy(servers), sortedCopy(other)) {
			return false
		}
	}
	return true
}

func provenanceEqual(a, b map[string]MCPOwners) bool {
	if len(a) != len(b) {
		return false
	}
	for server, owners := range a {
		other, ok := b[server]
		if !ok || owners.List != other.List || !slicesEqual(sortedCopy(owners.Names), sortedCopy(other.Names)) {
			return false
		}
	}
	return true
}

func mcpConfigsEqual(a, b map[string]*yaml.Node) bool {
	if len(a) != len(b) {
		return false
	}
	for name, cfg := range a {
		other, ok := b[name]
		if !ok || !MCPConfigEqual(cfg, other) {
			return false
		}
	}
	return true
}

// MCPConfigEqual reports whether two mcp_configs values record the same
// server. Only enabled is compared by type too, as the Oracle's
// opencode_enabled_matches does (models/dependency/mcp.py:97-104): false,
// 'false' and 0 are three values. Deviation: the Oracle's config comparison
// still finds 0 == False, so changing enabled from false to 0 leaves its lock
// as it was; apm-go records the 0 apm.yml declares. The other fields are not
// compared by type, because apm-go reads args and env as strings where the
// Oracle's lock keeps an integer.
func MCPConfigEqual(a, b *yaml.Node) bool {
	return NodesEqual(a, b) && tagsEqual(mappingValue(a, "enabled"), mappingValue(b, "enabled"))
}

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

// tagsEqual compares the resolved tags of two values NodesEqual found equal.
func tagsEqual(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.ShortTag() != b.ShortTag() {
		return false
	}
	for i := range a.Content {
		if !tagsEqual(a.Content[i], b.Content[i]) {
			return false
		}
	}
	return true
}

// NodesEqual compares two YAML values by kind, tag-resolved scalar text and
// children, in order; style and comments do not count.
func NodesEqual(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind || len(a.Content) != len(b.Content) {
		return false
	}
	if a.Kind == yaml.ScalarNode && a.Value != b.Value {
		return false
	}
	for i := range a.Content {
		if !NodesEqual(a.Content[i], b.Content[i]) {
			return false
		}
	}
	return true
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

// SortDependencies orders the dependencies as the Oracle writes them: by
// depth, then repo_url (get_all_dependencies, deps/lockfile.py:815). An
// absent depth is 1. virtual_path breaks a tie the Oracle leaves to
// insertion order.
func SortDependencies(deps []LockedDep) {
	depth := func(d *LockedDep) int {
		if d.Depth < 1 {
			return 1
		}
		return d.Depth
	}
	sort.SliceStable(deps, func(i, j int) bool {
		if di, dj := depth(&deps[i]), depth(&deps[j]); di != dj {
			return di < dj
		}
		if deps[i].RepoURL != deps[j].RepoURL {
			return deps[i].RepoURL < deps[j].RepoURL
		}
		return deps[i].VirtualPath < deps[j].VirtualPath
	})
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

func isEntryField(key string) bool {
	for _, f := range entryFieldOrder {
		if f == key {
			return true
		}
	}
	return false
}

func mappingRoot(doc *yaml.Node) *yaml.Node {
	if doc == nil {
		return nil
	}
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	return root
}

func addNode(node *yaml.Node, key string, val *yaml.Node) {
	node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"}, val)
}

// addTrue writes a boolean field as the Oracle does, only when it is true.
func addTrue(node *yaml.Node, key string, set bool) {
	if set {
		addNode(node, key, &yaml.Node{Kind: yaml.ScalarNode, Value: "true", Tag: "!!bool"})
	}
}

func intNode(n int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.Itoa(n), Tag: "!!int"}
}

func nullNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: "null", Tag: "!!null"}
}

func stringSeq(items []string) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, s := range items {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: s, Tag: "!!str"})
	}
	return seq
}

func stringMap(m map[string]string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, k := range sortedKeys(m) {
		addScalar(node, k, m[k])
	}
	return node
}

// flowIfEmpty makes an empty sequence print as "[]", as PyYAML does.
func flowIfEmpty(seq *yaml.Node) *yaml.Node {
	if len(seq.Content) == 0 {
		seq.Style = yaml.FlowStyle
	}
	return seq
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedCopy(items []string) []string {
	out := append([]string(nil), items...)
	sort.Strings(out)
	return out
}

func sortedUnique(items []string) []string {
	out := sortedCopy(items)
	n := 0
	for i, s := range out {
		if i == 0 || s != out[i-1] {
			out[n] = s
			n++
		}
	}
	return out[:n]
}
