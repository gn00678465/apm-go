package marketplace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// urlFetchTimeout bounds fetchURL's HTTPS GET request.
const urlFetchTimeout = 30 * time.Second

// urlFetchMaxBytes caps how much of a direct marketplace.json response
// fetchURL will read (mkt-023's "10MB 上限"). It is a var, not a const, so
// tests can shrink it instead of allocating a real 10MB+ response body.
var urlFetchMaxBytes int64 = 10 * 1024 * 1024

// fetchURL retrieves a KindURL source's manifest with a plain HTTPS GET on
// s.URL (already validated by ParseMarketplaceSource/Kind() to point
// directly at a hosted marketplace.json), recording its SHA-256 digest as
// provenance -- SourceURL/SourceDigest are only ever populated for kind=url
// (mkt-002/mkt-031). ETag caching is out of scope for this step (design.md
// "快取策略": deferred, non-goal).
func fetchURL(ctx context.Context, s *MarketplaceSource) (*MarketplaceManifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("build marketplace manifest request: %w", err)
	}

	client := &http.Client{Timeout: urlFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		// credsec: never echo s.URL or the transport error's own message
		// (mirrors internal/mcpregistry.getJSON) -- Go's http.Client wraps a
		// failed request in a *url.Error whose Error() embeds the full
		// request URL, which could carry a path-embedded token.
		return nil, fmt.Errorf("could not reach the marketplace manifest URL (network error)")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("marketplace manifest URL returned HTTP %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, urlFetchMaxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read marketplace manifest response: %w", err)
	}
	if int64(len(data)) > urlFetchMaxBytes {
		return nil, fmt.Errorf("marketplace manifest response exceeds %d byte limit", urlFetchMaxBytes)
	}

	var manifest MarketplaceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse marketplace manifest: %w", err)
	}

	digest := sha256.Sum256(data)
	manifest.SourceURL = s.URL
	manifest.SourceDigest = "sha256:" + hex.EncodeToString(digest[:])
	return &manifest, nil
}
