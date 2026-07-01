package registry

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/archive"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/apm-go/apm/internal/yamlcore"
)

// Resolution is the out-of-band lockfile data a registry install produces. The
// resolver's PackageLoader interface returns only a sub-manifest, so resolved_url
// / resolved_hash / version are collected here keyed by dep unique key.
type Resolution struct {
	ResolvedURL  string
	ResolvedHash string
	Version      string
}

// Loader is a composite PackageLoader: registry-sourced deps go through the HTTP
// consumer (list -> pick exact -> download -> verify -> safe-extract); everything
// else delegates to Next (the git loader).
type Loader struct {
	Registries      map[string]manifest.Registry
	DefaultRegistry string
	ModulesDir      string
	Next            resolver.PackageLoader
	MaxBytes        int64
	MaxEntries      int

	resolutions map[string]Resolution
}

// Resolutions returns the collected resolved_url/hash/version keyed by dep unique
// key (repo_url or repo_url/virtual_path).
func (l *Loader) Resolutions() map[string]Resolution {
	return l.resolutions
}

func (l *Loader) LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error) {
	if ref == nil || ref.Source != "registry" {
		return l.Next.LoadPackage(ref, resolvedRef)
	}

	name := ref.RegistryName
	if name == "" {
		name = l.DefaultRegistry
	}
	if name == "" {
		return nil, fmt.Errorf("registry dependency %q has no registry and no default registry is configured", ref.RepoURL)
	}
	reg, ok := l.Registries[name]
	if !ok {
		return nil, fmt.Errorf("registry %q is not configured in registries:", name)
	}

	owner, repo, err := splitOwnerRepo(ref.RepoURL)
	if err != nil {
		return nil, err
	}

	client, err := NewClient(reg.URL, ResolveCredential(name), aliasMap(reg), reg.Insecure)
	if err != nil {
		return nil, err
	}

	versions, err := client.ListVersions(owner, repo)
	if err != nil {
		return nil, remediateAuth(err, name)
	}
	want := resolvedRef
	if want == "" {
		want = ref.Reference
	}
	if want == "" {
		return nil, fmt.Errorf("registry dependency %q requires a version selector", ref.RepoURL)
	}
	var chosen *VersionEntry
	for i := range versions {
		if versions[i].Version == want {
			chosen = &versions[i]
			break
		}
	}
	if chosen == nil {
		return nil, fmt.Errorf("version %q not found for %q in registry %q", want, ref.RepoURL, name)
	}

	body, _, err := client.Download(owner, repo, chosen.Version)
	if err != nil {
		return nil, remediateAuth(err, name)
	}
	// lk-013: verify bytes before extraction; mismatch fails closed and names
	// the entry (entry/expected/actual).
	if err := lockfile.VerifyArchiveBytes(body, chosen.Digest); err != nil {
		return nil, fmt.Errorf("%s: %w", ref.RepoURL, err)
	}

	key := ref.RepoURL
	if ref.VirtualPath != "" {
		key += "/" + ref.VirtualPath
	}
	destDir := filepath.Join(l.ModulesDir, filepath.FromSlash(key))
	// sc-002 defense in depth: never extract outside the modules dir.
	if !archive.Contained(l.ModulesDir, destDir) {
		return nil, fmt.Errorf("refusing to extract %q outside %s", ref.RepoURL, l.ModulesDir)
	}
	if _, err := archive.SafeExtract(bytes.NewReader(body), destDir, archive.Limits{
		MaxBytes:   l.MaxBytes,
		MaxEntries: l.MaxEntries,
	}); err != nil {
		return nil, err
	}

	if l.resolutions == nil {
		l.resolutions = make(map[string]Resolution)
	}
	// lk-016: emit a normalized <algo>:<hex> envelope even if the registry
	// advertised a bare-hex digest (VerifyArchiveBytes already validated it).
	_, digHex, _ := lockfile.ParseHashEnvelope(chosen.Digest)
	l.resolutions[key] = Resolution{
		ResolvedURL:  client.ArchiveURL(owner, repo, chosen.Version),
		ResolvedHash: lockfile.HashEnvelope("sha256", digHex),
		Version:      chosen.Version,
	}

	return parseSubManifest(destDir)
}

// remediateAuth appends a redacted remediation hint to a 401/403 registry error
// pointing at the env-var credential source (req-sc-007: source descriptor, never
// the literal). Non-auth errors pass through unchanged.
func remediateAuth(err error, registryName string) error {
	var he *HTTPError
	if errors.As(err, &he) && (he.Status == 401 || he.Status == 403) {
		n := envSuffix(registryName)
		return fmt.Errorf("%w; set APM_REGISTRY_TOKEN_%s (or APM_REGISTRY_USER_%s + APM_REGISTRY_PASS_%s)", err, n, n, n)
	}
	return err
}

// RemediateFetchAuth is the lockfile-replay equivalent of remediateAuth: it
// resolves the registry name that owns resolvedURL (to name the env var) and
// appends the 401/403 remediation hint. Used by the frozen network replay path.
func RemediateFetchAuth(err error, resolvedURL string, registries map[string]manifest.Registry) error {
	var he *HTTPError
	if !errors.As(err, &he) || (he.Status != 401 && he.Status != 403) {
		return err
	}
	for name, reg := range registries {
		base := strings.TrimRight(strings.TrimSpace(reg.URL), "/")
		if base != "" && (resolvedURL == base || strings.HasPrefix(resolvedURL, base+"/")) {
			return remediateAuth(err, name)
		}
	}
	return fmt.Errorf("%w; set the APM_REGISTRY_TOKEN_<NAME> for this registry", err)
}

// ClientForURL builds a client for a lockfile-replay fetch: it finds the
// configured registry whose base URL owns resolvedURL (to resolve its
// credential + insecure + aliases), else falls back to an anonymous client
// scoped to the URL's scheme+host (matches the original apm-cli replay path).
func ClientForURL(resolvedURL string, registries map[string]manifest.Registry) (*Client, error) {
	for name, reg := range registries {
		base := strings.TrimRight(strings.TrimSpace(reg.URL), "/")
		if base != "" && (resolvedURL == base || strings.HasPrefix(resolvedURL, base+"/")) {
			return NewClient(reg.URL, ResolveCredential(name), aliasMap(reg), reg.Insecure)
		}
	}
	u, err := url.Parse(resolvedURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("cannot build client for resolved_url %q", resolvedURL)
	}
	return NewClient(u.Scheme+"://"+u.Host, Credential{}, nil, false)
}

// splitOwnerRepo splits "owner/repo" (or longer paths, last segment = repo).
func splitOwnerRepo(repoURL string) (string, string, error) {
	var parts []string
	for _, p := range strings.Split(repoURL, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) < 2 {
		return "", "", fmt.Errorf("registry dependency %q needs an owner/repo identity", repoURL)
	}
	if len(parts) == 2 {
		return parts[0], parts[1], nil
	}
	return strings.Join(parts[:len(parts)-1], "/"), parts[len(parts)-1], nil
}

// aliasMap builds credsec's host-class alias map ({primaryHost: aliases}) from a
// registry entry. Returns nil when the registry declares no aliases.
func aliasMap(reg manifest.Registry) map[string][]string {
	if len(reg.Aliases) == 0 {
		return nil
	}
	u, err := url.Parse(reg.URL)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	return map[string][]string{u.Hostname(): reg.Aliases}
}

func parseSubManifest(dir string) (*manifest.Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // leaf package
		}
		return nil, err
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, fmt.Errorf("parse extracted apm.yml: %w", err)
	}
	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		return nil, fmt.Errorf("validate extracted apm.yml: %w", err)
	}
	return m, nil
}
