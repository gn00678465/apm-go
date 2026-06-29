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
	index               map[string]int    // lazy index: unique key -> slice index
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
