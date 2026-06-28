package manifest

import (
	"fmt"
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
	IsParent    bool
	Port        int
	Scheme      string // "https", "http", "ssh", "git" (SCP)
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

	if strings.HasPrefix(s, "/") {
		return nil, fmt.Errorf("dependency path %q is absolute; only relative paths are allowed", s)
	}

	if isLocalPath(s) {
		if containsEscape(s) {
			return nil, fmt.Errorf("dependency path %q escapes project root", s)
		}
		return &DependencyReference{IsLocal: true, LocalPath: s}, nil
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
		return &DependencyReference{
			RepoURL:   kv["id"],
			Reference: kv["ref"],
			Alias:     kv["alias"],
		}, nil
	}

	if keys["name"] {
		return &DependencyReference{
			RepoURL: kv["name"],
			Alias:   kv["alias"],
		}, nil
	}

	return nil, fmt.Errorf("dependency entry %d has no source key (git, id, path, or name)", idx)
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
