package marketplace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// registryFileName is the marketplace registry's filename inside the apm
// config directory.
const registryFileName = "marketplaces.json"

// registryDocument is the on-disk shape of marketplaces.json: a wrapping
// object with a "marketplaces" array, matching the Python original's
// registry.py (`{"marketplaces": [...]}` -- :35, :57, :72). apm-go and the
// Python CLI can share a single ~/.apm directory, so this shape is not a
// stylistic choice; a bare top-level array is incompatible with the Python
// reader (mkt-002).
type registryDocument struct {
	Marketplaces []MarketplaceSource `json:"marketplaces"`
}

// marketplaceSourceJSON is the on-disk JSON shape of a single
// MarketplaceSource entry, field-for-field aligned with the Python
// original's MarketplaceSource.to_dict/from_dict (models.py:243-287). Path
// and Host use pointer fields so UnmarshalJSON can distinguish "key absent"
// (apply the Python default) from "key present with an explicit empty
// string" (keep it as-is) -- a distinction encoding/json's zero-value
// unmarshaling into a plain string cannot make on its own.
type marketplaceSourceJSON struct {
	Name   string  `json:"name"`
	URL    string  `json:"url,omitempty"`
	Ref    string  `json:"ref,omitempty"`
	Path   *string `json:"path,omitempty"`
	Owner  string  `json:"owner,omitempty"`
	Repo   string  `json:"repo,omitempty"`
	Host   *string `json:"host,omitempty"`
	Branch string  `json:"branch,omitempty"`
}

// MarshalJSON aligns with the Python original's MarketplaceSource.to_dict
// (models.py:243-265): name is always written; url only when non-empty; ref
// only when non-empty and not "main"; path only when it differs from the
// "marketplace.json" default (an explicit empty string -- the url-kind
// direct-manifest-URL marker -- is written, not treated as absent);
// owner/repo only when non-empty; host only when non-empty and not
// "github.com"; branch is not read from the struct's own Branch field but
// mirrors Ref, written under the same condition as ref (to_dict relies on
// the dataclass's __post_init__ keeping branch == ref at all times, which
// this package enforces at the serialization boundary instead).
func (s MarketplaceSource) MarshalJSON() ([]byte, error) {
	out := marketplaceSourceJSON{Name: s.Name, URL: s.URL, Owner: s.Owner, Repo: s.Repo}
	if s.Ref != "" && s.Ref != "main" {
		out.Ref = s.Ref
		out.Branch = s.Ref
	}
	if s.Path != "marketplace.json" {
		p := s.Path
		out.Path = &p
	}
	if s.Host != "" && s.Host != "github.com" {
		h := s.Host
		out.Host = &h
	}
	return json.Marshal(out)
}

// UnmarshalJSON aligns with the Python original's MarketplaceSource.from_dict
// (models.py:267-287): ref falls back through ref -> branch -> "main" (an
// explicit empty string counts as absent for this fallback, mirroring
// Python's "a or b or c" chaining, which treats "" as falsy); path defaults
// to "marketplace.json" only when the key itself is absent (an explicit
// empty string, the url-kind direct-manifest-URL marker, is preserved);
// host likewise defaults to "github.com" only when the key is absent.
func (s *MarketplaceSource) UnmarshalJSON(data []byte) error {
	var in marketplaceSourceJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	ref := in.Ref
	if ref == "" {
		ref = in.Branch
	}
	if ref == "" {
		ref = "main"
	}
	path := "marketplace.json"
	if in.Path != nil {
		path = *in.Path
	}
	host := "github.com"
	if in.Host != nil {
		host = *in.Host
	}
	s.Name = in.Name
	s.URL = in.URL
	s.Ref = ref
	s.Path = path
	s.Owner = in.Owner
	s.Repo = in.Repo
	s.Host = host
	s.Branch = in.Branch
	return nil
}

// RegistryPath returns the path to the marketplace registry file
// (~/.apm/marketplaces.json by default). It honors $APM_CONFIG_DIR the same
// way internal/experimental's config.json does, so tests can isolate the
// registry from a developer's real home directory.
func RegistryPath() (string, error) {
	dir := os.Getenv("APM_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		dir = filepath.Join(home, ".apm")
	}
	return filepath.Join(dir, registryFileName), nil
}

// LoadRegistry reads every registered marketplace source. A registry file
// that does not exist yet is an empty list, not an error (mkt-002: a fresh
// install has no marketplaces registered). The on-disk shape is the wrapping
// {"marketplaces": [...]} object the Python original uses, not a bare
// top-level array (mkt-002: apm-go and the Python CLI can share ~/.apm).
func LoadRegistry() ([]MarketplaceSource, error) {
	p, err := RegistryPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return []MarketplaceSource{}, nil
		}
		return nil, fmt.Errorf("read marketplace registry: %w", err)
	}
	var doc registryDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse marketplace registry: %w", err)
	}
	if doc.Marketplaces == nil {
		return []MarketplaceSource{}, nil
	}
	return doc.Marketplaces, nil
}

// SaveRegistry atomically overwrites the registry file with sources: write
// to a temp file in the same directory, then rename over the destination
// (mkt-002). The parent directory is created with mode 0700 if missing --
// best-effort, since Windows ignores this permission bit and that is not
// treated as a failure.
func SaveRegistry(sources []MarketplaceSource) error {
	p, err := RegistryPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create marketplace registry directory: %w", err)
	}
	if sources == nil {
		sources = []MarketplaceSource{}
	}
	data, err := json.MarshalIndent(registryDocument{Marketplaces: sources}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal marketplace registry: %w", err)
	}

	tmp, err := os.CreateTemp(dir, registryFileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp marketplace registry file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp marketplace registry file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp marketplace registry file: %w", err)
	}
	if err := os.Rename(tmpPath, p); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("commit marketplace registry: %w", err)
	}
	return nil
}

// FindByName looks up a registered marketplace by name, matching
// case-insensitively (mkt-006). It returns a nil source (and no error) when
// nothing matches; deciding whether that miss is fatal is the caller's job.
func FindByName(name string) (*MarketplaceSource, error) {
	sources, err := LoadRegistry()
	if err != nil {
		return nil, err
	}
	for i := range sources {
		if strings.EqualFold(sources[i].Name, name) {
			return &sources[i], nil
		}
	}
	return nil, nil
}

// AddSource registers a marketplace source. When an entry with the same
// name already exists (case-insensitive), it is silently replaced -- no
// error, no confirmation -- matching the Python original's registry.py
// behavior (mkt-006): the old entry is dropped and s is appended to the end
// of the list, not swapped in place (registry.py add_marketplace,
// :104-108), so a re-added marketplace always sorts last.
func AddSource(s MarketplaceSource) error {
	sources, err := LoadRegistry()
	if err != nil {
		return err
	}
	filtered := make([]MarketplaceSource, 0, len(sources)+1)
	for _, existing := range sources {
		if !strings.EqualFold(existing.Name, s.Name) {
			filtered = append(filtered, existing)
		}
	}
	filtered = append(filtered, s)
	return SaveRegistry(filtered)
}

// RemoveSource unregisters a marketplace by name, matching
// case-insensitively (mkt-006). Unlike AddSource, a missing name is an
// error: there is nothing silent about removing something that was never
// there.
func RemoveSource(name string) error {
	sources, err := LoadRegistry()
	if err != nil {
		return err
	}
	idx := -1
	for i := range sources {
		if strings.EqualFold(sources[i].Name, name) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("marketplace %q is not registered", name)
	}
	sources = append(sources[:idx], sources[idx+1:]...)
	return SaveRegistry(sources)
}
