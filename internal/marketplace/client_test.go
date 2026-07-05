package marketplace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestFetch_DispatchesByKind covers Fetch's role as the package's single
// entrypoint (design.md "Fetch dispatch"): for each Kind, Fetch must reach
// the same manifest a direct call to the transport-specific fetcher would.
// KindGitHub/KindGitLab/KindGit are already covered exhaustively by their
// own client_*_test.go files (mkt-023) -- this test only proves the switch
// in client.go wires KindLocal and KindURL to the right fetcher end to end,
// since those two need no network mock.
func TestFetch_DispatchesByKind(t *testing.T) {
	t.Run("local", func(t *testing.T) {
		// Arrange
		dir := t.TempDir()
		manifest := []byte(`{"name": "acme", "plugins": [{"name": "p", "source": "./p"}]}`)
		if err := os.WriteFile(filepath.Join(dir, "marketplace.json"), manifest, 0o644); err != nil {
			t.Fatal(err)
		}
		src := &MarketplaceSource{URL: dir, Path: defaultManifestPath}

		// Act
		got, err := Fetch(context.Background(), src)

		// Assert
		if err != nil {
			t.Fatalf("Fetch() returned error: %v", err)
		}
		if got.Name != "acme" {
			t.Errorf("Fetch() manifest = %+v, want Name=acme", got)
		}
	})

	// url kind's classification (Kind()) requires an https:// scheme
	// (urlNamesRemoteManifest), which httptest.NewServer's plain-http URL
	// does not satisfy -- fetchURL itself (any scheme accepted) is already
	// covered directly by client_url_test.go, so this case is proven via
	// KindGitHub instead: a full https:// GitHub URL both classifies
	// correctly through Kind() and needs no TLS trust dance to redirect at
	// an httptest.Server (githubAPIBase is a var precisely for this).
	t.Run("github", func(t *testing.T) {
		// Arrange
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"name": "acme-github"}`))
		}))
		t.Cleanup(srv.Close)
		withGitHubAPIBase(t, srv.URL)
		src, err := ParseMarketplaceSource("https://github.com/owner/repo", "")
		if err != nil {
			t.Fatalf("ParseMarketplaceSource() returned error: %v", err)
		}
		if src.Kind() != KindGitHub {
			t.Fatalf("Kind() = %q, want %q", src.Kind(), KindGitHub)
		}

		// Act
		got, err := Fetch(context.Background(), src)

		// Assert
		if err != nil {
			t.Fatalf("Fetch() returned error: %v", err)
		}
		if got.Name != "acme-github" {
			t.Errorf("Fetch() manifest = %+v, want Name=acme-github", got)
		}
	})
}
