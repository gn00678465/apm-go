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
	ResolvedBy     string
	ResolvedAt     string // ISO 8601 UTC
	Version        string // registry version
	Depth          int
	TreeSHA256     string
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
	Version      string
	GeneratedAt  string
	APMVersion   string
	Dependencies []LockedDep
}

// FindByKey looks up a locked dependency by unique key.
func (l *Lockfile) FindByKey(key string) *LockedDep {
	for i := range l.Dependencies {
		if l.Dependencies[i].UniqueKey() == key {
			return &l.Dependencies[i]
		}
	}
	return nil
}
