package lockfile

import "go.yaml.in/yaml/v4"

// MCPOwners is one mcp_config_provenance value: a single owner, or a list
// of them, which the Oracle also reads (deps/lockfile.py:138-149).
type MCPOwners struct {
	Names []string
	List  bool
}

// LockedDep represents a single resolved dependency in the lockfile.
type LockedDep struct {
	RepoURL        string
	Alias          string // the directory below apm_modules, when the entry names one
	Name           string // the package's own name, from its apm.yml
	Host           string
	Port           int
	RegistryPrefix string
	VirtualPath    string
	IsVirtual      bool
	Source         string // "git", "registry", "local"
	LocalPath      string // source "local": the path as apm.yml declares it
	Constraint     string // verbatim semver range from manifest at lock time
	ResolvedTag    string
	ResolvedCommit string
	ResolvedRef    string
	ResolvedURL    string // registry download URL (advisory)
	ResolvedHash   string // registry archive hash (authoritative)
	ResolvedBy     string // parent unique key format: "<repo_url>" or "<repo_url>/<virtual_path>"
	ResolvedAt     string // ISO 8601 UTC
	Version        string // registry version; source "local": the package's own version
	Depth          int
	TreeSHA256     string
	ContentHash    string // the Oracle's content_hash, ComputePackageHash of the package
	SkillSubset    []string
	TargetSubset   []string
	DeployedFiles  []string
	DeployedHashes map[string]string // path -> hash

	// IsInsecure records that the source was fetched over http://, and
	// AllowInsecure that apm.yml allowed it (deps/lockfile.py:238-239).
	IsInsecure    bool
	AllowInsecure bool

	// Marketplace provenance (mkt-031): purely additive metadata recording
	// that this dependency was discovered via `apm install
	// PLUGIN@MARKETPLACE[#REF]` (or the apm.yml dict form). Deliberately NOT
	// consulted by UniqueKey() -- dependency identity stays RepoURL/
	// VirtualPath only, matching the Python original's get_unique_key().
	DiscoveredVia         string // registered marketplace name
	MarketplacePluginName string // plugin name within that marketplace (manifest casing preserved)
	SourceURL             string // only set when the marketplace is kind=url
	SourceDigest          string // only set when the marketplace is kind=url

	// PackageType classifies a dependency declared under devDependencies.apm
	// (R9.4/AC45, design.md §11.3): apm-go's install --dev sets this to
	// PackageTypeMarketplacePlugin for every direct devDependencies.apm
	// entry, mirroring the upstream lockfile's package_type marker
	// (research/eval-real-run-20260728.md §D5). Empty for a regular
	// dependencies.apm entry.
	PackageType string
}

// PackageTypeAPMPackage is the package_type of a package with an apm.yml
// and .apm/ content.
const PackageTypeAPMPackage = "apm_package"

// PackageTypeMarketplacePlugin is the package_type value apm-go writes for a
// devDependencies.apm entry (the only classification currently produced).
const PackageTypeMarketplacePlugin = "marketplace_plugin"

// UniqueKey returns the dedup/lookup key for a locked dependency.
func (d *LockedDep) UniqueKey() string {
	if d.VirtualPath != "" {
		return d.RepoURL + "/" + d.VirtualPath
	}
	return d.RepoURL
}

// ModuleDir is the entry's directory below apm_modules, slash-separated: its
// alias, else its key.
func (d *LockedDep) ModuleDir() string {
	if d.Alias != "" {
		return d.Alias
	}
	return d.UniqueKey()
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

	// MCPConfigs is the configuration of each self-defined or resolved MCP
	// server by name, and MCPTargetServers the servers written for each
	// target (the Oracle's mcp_configs and mcp_target_servers). A config is
	// kept as the YAML mapping the lock holds, because its keys depend on
	// the transport.
	MCPConfigs       map[string]*yaml.Node
	MCPTargetServers map[string][]string
	// MCPConfigProvenance names, by server, the package whose apm.yml
	// declares an MCP server a dependency brought (the Oracle's
	// mcp_config_provenance, deps/lockfile.py:768).
	MCPConfigProvenance map[string]MCPOwners

	// PathTargets names the target that owns a deployed path, for the
	// deployments section. Parsed from that section and set by install; a
	// path it lacks takes its target from the path prefix.
	PathTargets map[string]string

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

// Reindex drops the lookup index of FindByKey, for a caller that changed the
// key of an entry in Dependencies.
func (l *Lockfile) Reindex() {
	l.index = nil
}
