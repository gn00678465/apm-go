package marketplace

import (
	"context"
	"fmt"
)

// Fetch retrieves and parses s's marketplace.json manifest, dispatching to
// the transport-specific fetcher for s.Kind() (design.md's "Fetch
// dispatch", mkt-023). This is the package's single fetch entrypoint: CLI
// commands (cmd/apm/marketplace.go) call only Fetch, never
// fetchGitHub/fetchGitLab/fetchGit/fetchLocal/fetchURL directly.
func Fetch(ctx context.Context, s *MarketplaceSource) (*MarketplaceManifest, error) {
	switch s.Kind() {
	case KindGitHub:
		return fetchGitHub(ctx, s)
	case KindGitLab:
		return fetchGitLab(ctx, s)
	case KindGit:
		return fetchGit(ctx, s)
	case KindLocal:
		return fetchLocal(ctx, s)
	case KindURL:
		return fetchURL(ctx, s)
	default:
		return nil, fmt.Errorf("unsupported marketplace source kind %q", s.Kind())
	}
}
