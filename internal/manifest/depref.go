package manifest

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"
)

type DependencyReference struct {
	RepoURL     string
	Host        string
	Owner       string
	Repo        string
	Reference   string
	VirtualPath string
	VirtualType string // "file" or "subdirectory"
	Alias       string
	IsLocal     bool
	LocalPath   string
	// LocalSourcePath is a RUNTIME-ONLY materialization detail: for a
	// dependency that must be vendored into apm_modules by COPYING a local
	// directory (rather than git-cloning), it holds the real absolute
	// filesystem source path. RepoURL then carries the sanitized, contained
	// "_local/<name>" apm_modules KEY (used by the resolver, deploy, and
	// lockfile), keeping the invalid-path-as-key problem out of every
	// filepath.Join(apm_modules, key). Empty for git/registry/marketplace
	// deps. Never serialized to apm.yml or the lockfile.
	LocalSourcePath string
	IsParent        bool
	Port            int
	Scheme          string // "https", "http", "ssh", "git" (SCP)
	Source          string // "git", "registry", "local", "marketplace", "" (inferred)
	RegistryName    string // registry name for source=="registry" (empty = use default)

	// Marketplace* fields (mkt-033) are only ever set for Source=="marketplace"
	// -- an apm.yml dependencies.apm dict entry of the form {name, marketplace,
	// version} straight out of ParseDepDict, still unresolved. RepoURL for
	// such an entry is the "_marketplace/<marketplace>/<name>" placeholder
	// (mirrors the Python original's DependencyReference.to_apm_yml_entry
	// dedup key), not a real repository coordinate.
	MarketplaceName        string // registered marketplace name (dict "marketplace:" key), case preserved
	MarketplacePluginName  string // plugin name within that marketplace (dict "name:" key), case preserved
	MarketplaceVersionSpec string // dict "version:" key verbatim; "" if absent. Parse time performs no semver/format validation (mkt-033)
}

var virtualFileExtensions = []string{
	".prompt.md", ".instructions.md", ".agent.md", ".chatmode.md",
}

var (
	ownerCharRe  = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	repoCharRe   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	hostCharRe   = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)
	segmentRe    = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	refRe        = regexp.MustCompile(`^[\x21-\x7e]+$`) // 1*VCHAR
	portRangeMax = 65535
)

func ParseDepString(s string) (*DependencyReference, error) {
	if s == "" {
		return nil, fmt.Errorf("empty dependency string")
	}

	// An OS-absolute filesystem path (POSIX "/...", Windows "C:\..."/"C:/...",
	// or a "\\host\share" UNC path) is user-intended, not a path-traversal
	// attempt -- accept it as a local dependency outright, WITHOUT running
	// containsEscape below (that guard only makes sense for a path meant to
	// stay relative to -- and inside -- the project root). This also lets an
	// absolute path resolved by mkt-025's local-marketplace fast path
	// round-trip back through apm.yml when install.go can't relativize it
	// into the project tree.
	if IsAbsoluteLocalPath(s) {
		return &DependencyReference{IsLocal: true, LocalPath: s, Source: "local"}, nil
	}

	if isLocalPath(s) {
		if containsEscape(s) {
			return nil, fmt.Errorf("dependency path %q escapes project root", s)
		}
		return &DependencyReference{IsLocal: true, LocalPath: s, Source: "local"}, nil
	}

	if strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") {
		return parseHTTPURL(s)
	}
	if strings.HasPrefix(s, "ssh://git@") {
		return parseSSHURL(s)
	}
	if strings.HasPrefix(s, "git@") {
		return parseSCPURL(s)
	}

	return parseShorthand(s)
}

func parseHTTPURL(s string) (*DependencyReference, error) {
	scheme := "https"
	rest := strings.TrimPrefix(s, "https://")
	if strings.HasPrefix(s, "http://") {
		scheme = "http"
		rest = strings.TrimPrefix(s, "http://")
	}

	ref, rest := splitRef(rest)
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 3 {
		return nil, fmt.Errorf("dependency %q: url-form requires host/owner/repo", s)
	}

	host, port, err := parseHostPort(parts[0])
	if err != nil {
		return nil, fmt.Errorf("dependency %q: %w", s, err)
	}
	owner := parts[1]
	repo := strings.TrimSuffix(parts[2], ".git")

	if !ownerCharRe.MatchString(owner) {
		return nil, fmt.Errorf("dependency %q: invalid owner %q", s, owner)
	}
	if !repoCharRe.MatchString(repo) {
		return nil, fmt.Errorf("dependency %q: invalid repo %q", s, repo)
	}

	d := &DependencyReference{
		Host:    host,
		Port:    port,
		Owner:   owner,
		Repo:    repo,
		RepoURL: owner + "/" + repo,
		Scheme:  scheme,
		Source:  "git",
	}
	if ref != "" {
		if !refRe.MatchString(ref) {
			return nil, fmt.Errorf("dependency %q: invalid ref %q", s, ref)
		}
		d.Reference = ref
	}
	if len(parts) == 4 && parts[3] != "" {
		vp := strings.TrimSuffix(parts[3], ".git")
		if err := validateVirtualPath(vp); err != nil {
			return nil, fmt.Errorf("dependency %q: %w", s, err)
		}
		d.VirtualPath = vp
		d.VirtualType = classifyVirtualPath(vp)
	}
	return d, nil
}

func parseSSHURL(s string) (*DependencyReference, error) {
	rest := strings.TrimPrefix(s, "ssh://git@")
	ref, rest := splitRef(rest)
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 3 {
		return nil, fmt.Errorf("dependency %q: ssh url-form requires host/owner/repo", s)
	}

	host, port, err := parseHostPort(parts[0])
	if err != nil {
		return nil, fmt.Errorf("dependency %q: %w", s, err)
	}
	owner := parts[1]
	repo := strings.TrimSuffix(parts[2], ".git")

	if !ownerCharRe.MatchString(owner) {
		return nil, fmt.Errorf("dependency %q: invalid owner %q", s, owner)
	}
	if !repoCharRe.MatchString(repo) {
		return nil, fmt.Errorf("dependency %q: invalid repo %q", s, repo)
	}

	d := &DependencyReference{
		Host:    host,
		Port:    port,
		Owner:   owner,
		Repo:    repo,
		RepoURL: owner + "/" + repo,
		Scheme:  "ssh",
		Source:  "git",
	}
	if ref != "" {
		d.Reference = ref
	}
	if len(parts) == 4 && parts[3] != "" {
		vp := strings.TrimSuffix(parts[3], ".git")
		if err := validateVirtualPath(vp); err != nil {
			return nil, fmt.Errorf("dependency %q: %w", s, err)
		}
		d.VirtualPath = vp
		d.VirtualType = classifyVirtualPath(vp)
	}
	return d, nil
}

func parseSCPURL(s string) (*DependencyReference, error) {
	rest := strings.TrimPrefix(s, "git@")
	colonIdx := strings.Index(rest, ":")
	if colonIdx < 1 {
		return nil, fmt.Errorf("dependency %q: SCP form requires git@host:path", s)
	}
	host := rest[:colonIdx]
	if !hostCharRe.MatchString(host) {
		return nil, fmt.Errorf("dependency %q: invalid host %q", s, host)
	}
	path := rest[colonIdx+1:]
	ref, path := splitRef(path)
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("dependency %q: SCP form requires git@host:owner/repo", s)
	}
	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")

	if !ownerCharRe.MatchString(owner) {
		return nil, fmt.Errorf("dependency %q: invalid owner %q", s, owner)
	}
	if !repoCharRe.MatchString(repo) {
		return nil, fmt.Errorf("dependency %q: invalid repo %q", s, repo)
	}

	d := &DependencyReference{
		Host:    host,
		Owner:   owner,
		Repo:    repo,
		RepoURL: owner + "/" + repo,
		Scheme:  "git",
		Source:  "git",
	}
	if ref != "" {
		d.Reference = ref
	}
	if len(parts) == 3 && parts[2] != "" {
		vp := strings.TrimSuffix(parts[2], ".git")
		if err := validateVirtualPath(vp); err != nil {
			return nil, fmt.Errorf("dependency %q: %w", s, err)
		}
		d.VirtualPath = vp
		d.VirtualType = classifyVirtualPath(vp)
	}
	return d, nil
}

func parseShorthand(s string) (*DependencyReference, error) {
	ref, rest := splitRef(s)
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("dependency %q does not match any valid form (url, shorthand, or local-path)", s)
	}

	var host, owner, repo string
	var vpParts []string

	if len(parts) >= 3 && strings.Contains(parts[0], ".") {
		host = parts[0]
		if !hostCharRe.MatchString(host) {
			return nil, fmt.Errorf("dependency %q: invalid host %q", s, host)
		}
		owner = parts[1]
		repo = parts[2]
		if len(parts) > 3 {
			vpParts = parts[3:]
		}
	} else {
		owner = parts[0]
		repo = parts[1]
		if len(parts) > 2 {
			vpParts = parts[2:]
		}
	}

	repo = strings.TrimSuffix(repo, ".git")

	if !ownerCharRe.MatchString(owner) {
		return nil, fmt.Errorf("dependency %q: invalid owner %q", s, owner)
	}
	if !repoCharRe.MatchString(repo) {
		return nil, fmt.Errorf("dependency %q: invalid repo %q", s, repo)
	}

	d := &DependencyReference{
		Host:    host,
		Owner:   owner,
		Repo:    repo,
		RepoURL: owner + "/" + repo,
		Source:  "git",
	}
	if ref != "" {
		if !refRe.MatchString(ref) {
			return nil, fmt.Errorf("dependency %q: invalid ref %q", s, ref)
		}
		d.Reference = ref
	}
	if len(vpParts) > 0 {
		vp := strings.Join(vpParts, "/")
		if err := validateVirtualPath(vp); err != nil {
			return nil, fmt.Errorf("dependency %q: %w", s, err)
		}
		d.VirtualPath = vp
		d.VirtualType = classifyVirtualPath(vp)
	}
	return d, nil
}

func ParseDepDict(entry *yaml.Node, idx int) (*DependencyReference, error) {
	kv := make(map[string]string)
	keys := make(map[string]bool)
	for i := 0; i < len(entry.Content)-1; i += 2 {
		k := entry.Content[i].Value
		keys[k] = true
		if entry.Content[i+1].Kind == yaml.ScalarNode {
			kv[k] = entry.Content[i+1].Value
		}
	}

	// mkt-033: the marketplace branch MUST be checked before every other
	// branch below, including "name" -- a marketplace dict entry
	// ({name, marketplace, version}) always carries a "name" key, and the
	// existing `keys["name"]` branch a few lines down would otherwise
	// silently swallow it as a plain git-literal RepoURL (see depref.go's
	// git-literal "name" branch); that shadowing is exactly what the
	// mkt-033 branch-order regression test below locks down.
	if keys["marketplace"] {
		if keys["git"] || keys["path"] || keys["registry"] || keys["id"] {
			return nil, fmt.Errorf("dependency entry %d: Ambiguous dependency - 'marketplace' cannot be combined with 'git', 'path', 'registry', or 'id'", idx)
		}
		for k := range keys {
			switch k {
			case "name", "marketplace", "version":
				// allowed
			default:
				return nil, fmt.Errorf("dependency entry %d: unknown key %q for a marketplace dependency (allowed: name, marketplace, version)", idx, k)
			}
		}

		// name is required, checked before the regex validation below, with
		// its own dedicated error message (mirrors reference.py:763-766).
		name := strings.TrimSpace(kv["name"])
		if name == "" {
			return nil, fmt.Errorf("dependency entry %d: Marketplace dependency must have a non-empty 'name' field", idx)
		}
		mkt := strings.TrimSpace(kv["marketplace"])

		// name/marketplace are only stripped, never lowercased -- case
		// insensitivity happens later at plugin-lookup time, not at parse
		// time (mkt-033: "大小寫保留").
		if !segmentRe.MatchString(name) {
			return nil, fmt.Errorf("dependency entry %d: invalid marketplace plugin name %q", idx, name)
		}
		if !segmentRe.MatchString(mkt) {
			return nil, fmt.Errorf("dependency entry %d: invalid marketplace name %q", idx, mkt)
		}

		// version is optional; when present it must be non-empty, but parse
		// time performs no format/semver validation at all (range legality
		// is deferred to resolve time, mirrors reference.py:781-785).
		version := kv["version"]
		if keys["version"] && strings.TrimSpace(version) == "" {
			return nil, fmt.Errorf("dependency entry %d: marketplace dependency 'version' must be a non-empty string when present", idx)
		}

		return &DependencyReference{
			RepoURL:                "_marketplace/" + mkt + "/" + name,
			Source:                 "marketplace",
			MarketplaceName:        mkt,
			MarketplacePluginName:  name,
			MarketplaceVersionSpec: version,
		}, nil
	}

	if keys["id"] && keys["git"] {
		return nil, fmt.Errorf("dependency entry %d has both 'id' and 'git' keys", idx)
	}

	if keys["path"] && !keys["git"] && !keys["id"] && !keys["name"] {
		p := kv["path"]
		if containsEscape(p) {
			return nil, fmt.Errorf("dependency path %q escapes project root", p)
		}
		return &DependencyReference{
			IsLocal:   true,
			LocalPath: p,
			Alias:     kv["alias"],
			Source:    "local",
		}, nil
	}

	if keys["git"] {
		gitVal := kv["git"]
		if gitVal == "parent" {
			if !keys["path"] {
				return nil, fmt.Errorf("dependency entry %d: git: parent requires a 'path' field", idx)
			}
			if keys["type"] {
				return nil, fmt.Errorf("dependency entry %d: 'type' is not allowed with git: parent", idx)
			}
			return &DependencyReference{
				IsParent:    true,
				VirtualPath: kv["path"],
				VirtualType: classifyVirtualPath(kv["path"]),
				Alias:       kv["alias"],
			}, nil
		}
		d, err := ParseDepString(gitVal)
		if err != nil {
			return nil, fmt.Errorf("dependency entry %d: %w", idx, err)
		}
		// git: key forces source=git even for local filesystem paths
		if d.IsLocal {
			d.IsLocal = false
			d.RepoURL = d.LocalPath
			d.LocalPath = ""
			d.Source = "git"
		}
		if kv["ref"] != "" {
			d.Reference = kv["ref"]
		}
		if kv["alias"] != "" {
			d.Alias = kv["alias"]
		}
		if kv["path"] != "" {
			d.VirtualPath = kv["path"]
			d.VirtualType = classifyVirtualPath(kv["path"])
		}
		return d, nil
	}

	if keys["id"] {
		// Registry object form uses `version:` (docs); accept `ref:` as an alias.
		reference := kv["version"]
		if reference == "" {
			reference = kv["ref"]
		}
		return &DependencyReference{
			RepoURL:      kv["id"],
			Reference:    reference,
			RegistryName: kv["registry"],
			Alias:        kv["alias"],
			Source:       "registry",
		}, nil
	}

	if keys["name"] {
		// A bare {name: ...} entry is a git-literal shorthand ("owner/repo").
		// Validate it as one: this branch previously stored the value
		// VERBATIM with empty Owner/Repo, so a value like
		// "ext::sh -c '<cmd>'" flowed through resolveCloneURL unchanged and
		// reached `git clone` as a remote-helper transport (RCE). Parsing it
		// as a shorthand rejects any non-owner/repo string outright and, for
		// legitimate values, populates Owner/Repo/Host so resolveCloneURL
		// builds a proper https URL instead of cloning the raw string.
		name := kv["name"]
		ref, err := ParseDepString(name)
		if err != nil {
			return nil, fmt.Errorf("dependency entry %d: invalid name %q: %w", idx, name, err)
		}
		if ref.IsLocal || ref.Source != "git" {
			return nil, fmt.Errorf("dependency entry %d: name %q must be a git repository shorthand (owner/repo)", idx, name)
		}
		if kv["alias"] != "" {
			ref.Alias = kv["alias"]
		}
		return ref, nil
	}

	return nil, fmt.Errorf("dependency entry %d has no source key (git, id, path, name, or marketplace)", idx)
}

// ValidateResolved returns an error if d is still an unresolved marketplace
// dependency (Source=="marketplace") -- mkt-030's "resolve before persist"
// invariant. A dependencies.apm dict entry ({name, marketplace, version})
// only ever comes out of ParseDepDict in this state; before any code path
// writes a DependencyReference back into apm.yml it must first be collapsed
// into an ordinary git/local reference via marketplace.ResolvePlugin
// (mkt-029). Mirrors the Python original's raise ValueError guard in
// to_apm_yml_entry() -- an unresolved marketplace ref must never be
// serialized.
func (d *DependencyReference) ValidateResolved() error {
	if d.Source == "marketplace" {
		return fmt.Errorf("cannot write unresolved marketplace dependency %q (marketplace %q) to apm.yml; resolve it via ResolvePlugin first", d.MarketplacePluginName, d.MarketplaceName)
	}
	return nil
}

// ToCanonical returns the canonical form of a dependency reference.
// GROUNDWORK: no CLI caller in Phase 1; normalize stays byte-exact.
func (d *DependencyReference) ToCanonical(defaultHost string) string {
	if d.IsLocal {
		return d.LocalPath
	}
	if d.IsParent {
		return "parent"
	}

	// Local git repo (git: ./path) — Owner/Repo empty, RepoURL is the path
	if d.Owner == "" && d.Repo == "" && d.RepoURL != "" {
		return d.RepoURL
	}

	var sb strings.Builder
	if d.Host != "" && !strings.EqualFold(d.Host, defaultHost) {
		sb.WriteString(d.Host)
		sb.WriteByte('/')
	}
	sb.WriteString(d.Owner)
	sb.WriteByte('/')
	sb.WriteString(strings.TrimSuffix(d.Repo, ".git"))
	if d.VirtualPath != "" {
		sb.WriteByte('/')
		sb.WriteString(d.VirtualPath)
	}
	if d.Reference != "" {
		sb.WriteByte('#')
		sb.WriteString(d.Reference)
	}
	return sb.String()
}

// IdentityKey returns the identity used to compare a dependency reference
// against a lockfile entry (LockedDep.UniqueKey()) or another reference,
// deliberately ignoring Reference (git ref/tag) and Alias -- un-011: two
// references to the same repo_url[/virtual_path] that only differ by ref or
// alias are the same uninstall target. Mirrors deploy.DepRefKey exactly
// (kept as a separate copy rather than an internal/manifest -> internal/deploy
// import to avoid a package cycle: internal/deploy already imports
// internal/manifest). Local and parent references have no stable identity
// (matching deploy.DepRefKey's "" for IsLocal/IsParent) and always return "".
func (d *DependencyReference) IdentityKey() string {
	if d.IsLocal || d.IsParent {
		return ""
	}
	if d.VirtualPath != "" {
		return d.RepoURL + "/" + d.VirtualPath
	}
	return d.RepoURL
}

// IsAbsoluteLocalPath reports whether s is an OS-absolute filesystem path in
// any form apm.yml/the CLI may need to round-trip: POSIX ("/..."), Windows
// drive-letter ("C:\..." or "C:/..."), or UNC ("\\host\share..."). Checked
// via filepath.IsAbs/filepath.VolumeName (native to the running GOOS) plus
// explicit POSIX "/" and UNC "\\" prefix checks, so a path written on one OS
// still parses as absolute when apm.yml is later read on another.
func IsAbsoluteLocalPath(s string) bool {
	if filepath.IsAbs(s) {
		return true
	}
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, `\\`) {
		return true
	}
	return filepath.VolumeName(s) != ""
}

func classifyVirtualPath(vp string) string {
	for _, ext := range virtualFileExtensions {
		if strings.HasSuffix(vp, ext) {
			return "file"
		}
	}
	return "subdirectory"
}

func splitRef(s string) (ref, rest string) {
	idx := strings.LastIndex(s, "#")
	if idx < 0 {
		return "", s
	}
	return s[idx+1:], s[:idx]
}

func parseHostPort(s string) (host string, port int, err error) {
	if idx := strings.LastIndex(s, ":"); idx >= 0 {
		host = s[:idx]
		p, e := strconv.Atoi(s[idx+1:])
		if e != nil || p < 1 || p > portRangeMax {
			return "", 0, fmt.Errorf("invalid port in %q", s)
		}
		port = p
	} else {
		host = s
	}
	if !hostCharRe.MatchString(host) {
		return "", 0, fmt.Errorf("invalid host %q", host)
	}
	return host, port, nil
}

func validateVirtualPath(vp string) error {
	for _, seg := range strings.Split(vp, "/") {
		if seg == "" {
			continue
		}
		if !segmentRe.MatchString(seg) {
			return fmt.Errorf("invalid virtual path segment %q", seg)
		}
	}
	return nil
}
