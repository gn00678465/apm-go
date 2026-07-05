package marketplace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestFetchURL_HappyPath covers the KindURL fetch path end to end: GET the
// exact manifest URL, unmarshal the body, and record SHA-256 provenance
// (mkt-023/mkt-031: SourceURL/SourceDigest are only ever populated for
// kind=url).
func TestFetchURL_HappyPath(t *testing.T) {
	// Arrange
	body := []byte(`{"name": "acme", "owner": "acme-owner", "plugins": [{"name": "p", "source": "./p"}]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/marketplace.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	src := &MarketplaceSource{URL: srv.URL + "/marketplace.json"}
	wantDigest := sha256.Sum256(body)

	// Act
	got, err := fetchURL(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchURL() returned error: %v", err)
	}
	if got.Name != "acme" || got.Owner != "acme-owner" {
		t.Errorf("fetchURL() manifest = %+v", got)
	}
	if got.SourceURL != src.URL {
		t.Errorf("SourceURL = %q, want %q", got.SourceURL, src.URL)
	}
	wantDigestStr := "sha256:" + hex.EncodeToString(wantDigest[:])
	if got.SourceDigest != wantDigestStr {
		t.Errorf("SourceDigest = %q, want %q", got.SourceDigest, wantDigestStr)
	}
}

// TestFetchURL_NonOKStatus covers a non-200 response.
func TestFetchURL_NonOKStatus(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	src := &MarketplaceSource{URL: srv.URL + "/marketplace.json"}

	// Act
	_, err := fetchURL(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchURL() returned no error for a 404 response")
	}
}

// TestFetchURL_InvalidJSON covers a malformed response body.
func TestFetchURL_InvalidJSON(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{not json"))
	}))
	t.Cleanup(srv.Close)
	src := &MarketplaceSource{URL: srv.URL + "/marketplace.json"}

	// Act
	_, err := fetchURL(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchURL() returned no error for malformed JSON")
	}
}

// TestFetchURL_NetworkErrorNeverEchoesURL covers the credsec convention:
// a transport-level failure must not leak the request URL in its error
// message (mirrors internal/mcpregistry.getJSON's regression test -- Go's
// http.Client wraps a failed request in a *url.Error whose Error() embeds
// the full request URL).
func TestFetchURL_NetworkErrorNeverEchoesURL(t *testing.T) {
	// Arrange
	pathToken := "t-should-not-leak-in-error"
	src := &MarketplaceSource{URL: "https://127.0.0.1:1/" + pathToken}

	// Act
	_, err := fetchURL(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchURL() returned no error for an unreachable host")
	}
	if strings.Contains(err.Error(), pathToken) {
		t.Errorf("fetchURL() error leaked the request URL: %v", err)
	}
}

// TestFetchURL_TolerantOfRegistryKey re-confirms mkt-005's tolerant parsing
// through the URL fetch path.
func TestFetchURL_TolerantOfRegistryKey(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name": "acme", "plugins": [{"name": "p", "source": "./p", "registry": "custom"}]}`))
	}))
	t.Cleanup(srv.Close)
	src := &MarketplaceSource{URL: srv.URL + "/marketplace.json"}

	// Act
	got, err := fetchURL(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchURL() returned error for a manifest with a 'registry' key: %v", err)
	}
	if len(got.Plugins) != 1 || got.Plugins[0].Registry != "custom" {
		t.Errorf("fetchURL() Plugins = %+v, want one plugin with Registry=%q", got.Plugins, "custom")
	}
}

// TestFetchURL_SizeCapEnforced covers mkt-023's response-size guard,
// shrinking the package-level cap for the duration of the test instead of
// allocating a real 10MB+ response body.
func TestFetchURL_SizeCapEnforced(t *testing.T) {
	// Arrange
	origCap := urlFetchMaxBytes
	urlFetchMaxBytes = 8
	t.Cleanup(func() { urlFetchMaxBytes = origCap })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name": "this-is-longer-than-eight-bytes"}`))
	}))
	t.Cleanup(srv.Close)
	src := &MarketplaceSource{URL: srv.URL + "/marketplace.json"}

	// Act
	_, err := fetchURL(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchURL() returned no error, want a size-cap rejection")
	}
	if !strings.Contains(err.Error(), "byte limit") {
		t.Errorf("fetchURL() error = %v, want it to mention the byte limit", err)
	}
}
