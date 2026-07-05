package lockfile

// LockedDep represents a single resolved dependency in the lockfile.
type LockedDep struct {
	RepoURL        string
	VirtualPath    string
	Source         string // "git", "registry", "local"
	Constraint     string // verbatim semver range from manifest at lock time
	ResolvedTag    string
	ResolvedCommit string
	ResolvedRef    string
	ResolvedURL    string // registry download URL (advisory)
	ResolvedHash   string // registry archive hash (authoritative)
	ResolvedBy     string // parent unique key format: "<repo_url>" or "<repo_url>/<virtual_path>"
	ResolvedAt     string // ISO 8601 UTC
	Version        string // registry version
	Depth          int
	TreeSHA256     string
	SkillSubset    []string
	DeployedFiles  []string
	DeployedHashes map[string]string // path -> hash

	// Marketplace provenance (mkt-031): purely additive metadata recording
	// that this dependency was discovered via `apm install
	// PLUGIN@MARKETPLACE[#REF]` (or the apm.yml dict form). Deliberately NOT
	// consulted by UniqueKey() -- dependency identity stays RepoURL/
	// VirtualPath only, matching the Python original's get_unique_key().
	DiscoveredVia         string // registered marketplace name
	MarketplacePluginName string // plugin name within that marketplace (manifest casing preserved)
	SourceURL             string // only set when the marketplace is kind=url
	SourceDigest          string // only set when the marketplace is kind=url
}

// UniqueKey returns the dedup/lookup key for a locked dependency.
func (d *LockedDep) UniqueKey() string {
	if d.VirtualPath != "" {
		return d.RepoURL + "/" + d.VirtualPath
	}
	return d.RepoURL
}

// Lockfile represents the parsed apm.lock.yaml.
type Lockfile struct {
	Version             string
	GeneratedAt         string
	APMVersion          string
	Dependencies        []LockedDep
	LocalDeployedFiles  []string          // self-entry deployed file paths
	LocalDeployedHashes map[string]string // self-entry path -> hash

	// MCPServers is the full current set of MCP server names deployed
	// across all targets (local + all resolved deps), refreshed in full on
	// every install/update (deploy.Run always recomputes the merged bake,
	// not just a delta). Mirrors the Python original's LockFile.mcp_servers
	// (YAML key mcp_servers): uninstall's transitive-stale MCP cleanup
	// diffs this "old" list against the recomputed "new" one. Absent on
	// lockfiles written before this field existed -- callers must treat a
	// nil/empty slice as "unknown, assume none" (fail-open), never as
	// "explicitly no MCP servers were ever deployed".
	MCPServers []string

	index map[string]int // lazy index: unique key -> slice index
}

// FindByKey looks up a locked dependency by unique key. O(1) after first call.
func (l *Lockfile) FindByKey(key string) *LockedDep {
	if l.index == nil {
		l.index = make(map[string]int, len(l.Dependencies))
		for i := range l.Dependencies {
			l.index[l.Dependencies[i].UniqueKey()] = i
		}
	}
	if i, ok := l.index[key]; ok {
		return &l.Dependencies[i]
	}
	return nil
}

// RemoveKeys removes the locked dependencies matching any of the given
// unique keys (same key space as FindByKey/UniqueKey -- un-072: this
// deliberately reuses the existing key logic rather than introducing a
// second comparison scheme) and rebuilds the lookup index so FindByKey
// stays correct afterward. Keys not present are ignored.
func (l *Lockfile) RemoveKeys(keys []string) {
	if len(keys) == 0 {
		return
	}
	toRemove := make(map[string]bool, len(keys))
	for _, k := range keys {
		toRemove[k] = true
	}

	kept := l.Dependencies[:0]
	for i := range l.Dependencies {
		if !toRemove[l.Dependencies[i].UniqueKey()] {
			kept = append(kept, l.Dependencies[i])
		}
	}
	l.Dependencies = kept

	l.index = make(map[string]int, len(l.Dependencies))
	for i := range l.Dependencies {
		l.index[l.Dependencies[i].UniqueKey()] = i
	}
}
