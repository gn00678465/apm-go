package marketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// localManifestProbeOrder is mkt-003's fallback path order: tried in
// sequence relative to the source's local directory, and the first
// candidate that exists on disk is the one used.
var localManifestProbeOrder = []string{
	"marketplace.json",
	".github/plugin/marketplace.json",
	".claude-plugin/marketplace.json",
}

// fetchLocal reads a KindLocal source's manifest directly from the local
// working tree named by s.URL (an absolute directory path OR a "file://"
// URI naming one -- LocalFilesystemPath, ticket 24 AC3, accepts both
// indefinitely). SourceURL/SourceDigest are left empty: they are provenance
// for network fetches only (design.md).
//
// mkt B5: when the resolved path itself names a file rather than a
// directory (e.g. a SOURCE that pointed straight at a marketplace.json),
// that file is read directly with no mkt-003 candidate probing underneath
// it -- mirroring the Python original's _fetch_local, which branches on
// repo_path.is_file() before ever consulting the probe order.
func fetchLocal(ctx context.Context, s *MarketplaceSource) (*MarketplaceManifest, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	root := LocalFilesystemPath(s.URL)
	if info, statErr := os.Stat(root); statErr == nil && !info.IsDir() {
		return readLocalManifestFile(root)
	}

	candidates := localManifestCandidates(s.Path)
	for _, rel := range candidates {
		p := filepath.Join(root, filepath.FromSlash(rel))
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read local marketplace manifest %q: %w", p, err)
		}
		var manifest MarketplaceManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("parse local marketplace manifest %q: %w", p, err)
		}
		return &manifest, nil
	}
	return nil, fmt.Errorf("no marketplace manifest found under %q (tried %s)", root, strings.Join(candidates, ", "))
}

// readLocalManifestFile reads and parses a single manifest file named
// directly by path (mkt B5's direct-file fetch mode).
func readLocalManifestFile(path string) (*MarketplaceManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read local marketplace manifest %q: %w", path, err)
	}
	var manifest MarketplaceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse local marketplace manifest %q: %w", path, err)
	}
	return &manifest, nil
}

// localManifestCandidates returns the manifest path(s) fetchLocal probes:
// mkt-003's fallback order when path is unset or still the parser's
// default (defaultManifestPath), otherwise just path itself -- an explicit,
// non-default path is used as-is, with no fallback probing.
func localManifestCandidates(path string) []string {
	if path != "" && path != defaultManifestPath {
		return []string{path}
	}
	return localManifestProbeOrder
}
