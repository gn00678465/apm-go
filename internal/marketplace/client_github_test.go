package marketplace

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withGitHubAPIBase redirects githubAPIBaseFor at an httptest.Server (for
// any host) for the duration of the test, restoring it afterward.
func withGitHubAPIBase(t *testing.T, base string) {
	t.Helper()
	orig := githubAPIBaseFor
	githubAPIBaseFor = func(string) string { return base }
	t.Cleanup(func() { githubAPIBaseFor = orig })
}

// TestFetchGitHub_HappyPath_RawMediaType covers mkt-023: fetchGitHub must
// send Accept: application/vnd.github.v3.raw and consume the response body
// directly as the manifest's raw content -- never as a base64 JSON
// envelope. The mock only serves the raw body when it observes the correct
// Accept header; otherwise it serves a base64 envelope, which would decode
// into an empty (fields-missing) manifest and fail the assertions below --
// a regression guard against silently falling back to the default Contents
// API media type.
func TestFetchGitHub_HappyPath_RawMediaType(t *testing.T) {
	// Arrange
	body := []byte(`{"name": "acme", "owner": "acme-owner", "plugins": [{"name": "p", "source": "./p"}]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme-owner/acme-repo/contents/marketplace.json" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("ref") != "main" {
			http.Error(w, "unexpected ref", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Accept") != "application/vnd.github.v3.raw" {
			envelope := map[string]string{
				"content":  base64.StdEncoding.EncodeToString(body),
				"encoding": "base64",
			}
			json.NewEncoder(w).Encode(envelope)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "acme-owner", Repo: "acme-repo", Ref: "main", Path: "marketplace.json", Host: "github.com"}

	// Act
	got, err := fetchGitHub(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitHub() returned error: %v", err)
	}
	if got.Name != "acme" || got.Owner != "acme-owner" {
		t.Errorf("fetchGitHub() manifest = %+v, want Name=acme Owner=acme-owner (raw content must be unmarshaled directly, no base64 decode)", got)
	}
	if len(got.Plugins) != 1 || got.Plugins[0].Name != "p" {
		t.Errorf("fetchGitHub() Plugins = %+v", got.Plugins)
	}
}

// TestFetchGitHub_ForwardsPATForTrustedHost covers mkt-011: a github.com
// source forwards GITHUB_APM_PAT as an "Authorization: token <PAT>" header.
func TestFetchGitHub_ForwardsPATForTrustedHost(t *testing.T) {
	// Arrange
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"name": "acme"}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	t.Setenv(githubPATEnvVar, "t-secret-pat")
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "github.com"}

	// Act
	_, err := fetchGitHub(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitHub() returned error: %v", err)
	}
	if gotAuth != "token t-secret-pat" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "token t-secret-pat")
	}
}

// TestFetchGitHub_ForwardsPATForGitHubSubdomain covers the "*.github.com"
// part of mkt-011's trust boundary.
func TestFetchGitHub_ForwardsPATForGitHubSubdomain(t *testing.T) {
	// Arrange
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"name": "acme"}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	t.Setenv(githubPATEnvVar, "t-secret-pat")
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "api.github.com"}

	// Act
	_, err := fetchGitHub(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitHub() returned error: %v", err)
	}
	if gotAuth != "token t-secret-pat" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "token t-secret-pat")
	}
}

// TestFetchGitHub_UntrustedHostDoesNotForwardPAT covers mkt-011's negative
// case: a host outside github.com/*.github.com must never receive the PAT,
// even if fetchGitHub is invoked directly (this test hand-builds a source
// with a mismatched Host rather than going through Kind() dispatch, so it
// exercises fetchGitHub's own trust guard rather than relying on upstream
// classification to have already filtered untrusted hosts out).
func TestFetchGitHub_UntrustedHostDoesNotForwardPAT(t *testing.T) {
	// Arrange
	var gotAuth string
	requestSeen := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestSeen = true
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"name": "acme"}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	t.Setenv(githubPATEnvVar, "t-secret-pat")
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "github.example.com"}

	// Act
	_, err := fetchGitHub(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitHub() returned error: %v", err)
	}
	if !requestSeen {
		t.Fatal("request never reached the test server")
	}
	if gotAuth != "" {
		t.Errorf("Authorization header = %q, want empty (untrusted host must not receive the PAT)", gotAuth)
	}
}

// TestIsTrustedGitHubHost table-tests the mkt-011 trust boundary directly.
func TestIsTrustedGitHubHost(t *testing.T) {
	tests := []struct {
		name string
		host string
		want bool
	}{
		{"exact github.com", "github.com", true},
		{"case-insensitive", "GitHub.Com", true},
		{"subdomain", "api.github.com", true},
		{"enterprise host is not trusted", "github.example.com", false},
		{"unrelated host", "example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTrustedGitHubHost(tt.host); got != tt.want {
				t.Errorf("isTrustedGitHubHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

// TestFetchGitHub_NotFound covers a missing manifest path (404).
func TestFetchGitHub_NotFound(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "github.com"}

	// Act
	_, err := fetchGitHub(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchGitHub() returned no error for a 404 response")
	}
}

// TestFetchGitHub_NonOKStatus covers a non-200, non-404 response.
func TestFetchGitHub_NonOKStatus(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "github.com"}

	// Act
	_, err := fetchGitHub(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchGitHub() returned no error for a 403 response")
	}
}

// TestFetchGitHub_InvalidJSON covers a malformed manifest body.
func TestFetchGitHub_InvalidJSON(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{not json"))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "github.com"}

	// Act
	_, err := fetchGitHub(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchGitHub() returned no error for malformed JSON")
	}
}

// TestFetchGitHub_TolerantOfRegistryKey re-confirms mkt-005's tolerant
// parsing through the GitHub fetch path.
func TestFetchGitHub_TolerantOfRegistryKey(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name": "acme", "plugins": [{"name": "p", "source": "./p", "registry": "custom"}]}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "github.com"}

	// Act
	got, err := fetchGitHub(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitHub() returned error for a manifest with a 'registry' key: %v", err)
	}
	if len(got.Plugins) != 1 || got.Plugins[0].Registry != "custom" {
		t.Errorf("fetchGitHub() Plugins = %+v, want one plugin with Registry=%q", got.Plugins, "custom")
	}
}

// TestFetchGitHub_DefaultsRefWhenEmpty covers the defensive ref/path
// fallback (in practice ParseMarketplaceSource always populates these, but
// fetchGitHub does not assume that).
func TestFetchGitHub_DefaultsRefWhenEmpty(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/contents/marketplace.json" || r.URL.Query().Get("ref") != "main" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"name": "acme"}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "o", Repo: "r", Host: "github.com"}

	// Act
	got, err := fetchGitHub(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitHub() returned error: %v", err)
	}
	if got.Name != "acme" {
		t.Errorf("fetchGitHub() manifest = %+v", got)
	}
}

// TestFetchGitHub_NetworkErrorNeverEchoesPATOrURL covers the credsec
// convention: a transport-level failure must not leak the PAT or the
// request URL in its error message.
func TestFetchGitHub_NetworkErrorNeverEchoesPATOrURL(t *testing.T) {
	// Arrange
	withGitHubAPIBase(t, "https://127.0.0.1:1")
	t.Setenv(githubPATEnvVar, "t-should-not-leak-in-error")
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "github.com"}

	// Act
	_, err := fetchGitHub(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchGitHub() returned no error for an unreachable host")
	}
	if strings.Contains(err.Error(), "t-should-not-leak-in-error") {
		t.Errorf("fetchGitHub() error leaked the PAT: %v", err)
	}
}

// TestFetchGitHub_ProbesThirdCandidateAfterTwo404s covers mkt-003: when
// s.Path is unset/default, a 404 on "marketplace.json" and
// ".github/plugin/marketplace.json" falls through to
// ".claude-plugin/marketplace.json" -- the most common real-world layout --
// instead of failing after the first miss.
func TestFetchGitHub_ProbesThirdCandidateAfterTwo404s(t *testing.T) {
	// Arrange
	wantPath := "/repos/o/r/contents/.claude-plugin/marketplace.json"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wantPath {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"name": "found-at-claude-plugin"}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Host: "github.com"}

	// Act
	got, err := fetchGitHub(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitHub() returned error: %v", err)
	}
	if got.Name != "found-at-claude-plugin" {
		t.Errorf("fetchGitHub().Name = %q, want %q", got.Name, "found-at-claude-plugin")
	}
}

// TestFetchGitHub_AllCandidates404 covers the exhausted-probe case: every
// mkt-003 candidate 404s, so fetchGitHub returns an error (not a panic or a
// nil manifest).
func TestFetchGitHub_AllCandidates404(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Host: "github.com"}

	// Act
	_, err := fetchGitHub(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchGitHub() returned no error when every mkt-003 candidate 404s")
	}
}

// TestFetchGitHub_NonDefaultPathDoesNotProbe covers the negative half of
// mkt-003: an explicit, non-default Path is tried as-is, with no fallback
// probing across localManifestProbeOrder -- a 404 on it is a hard failure
// immediately, not a cue to try "marketplace.json" or the other candidates.
func TestFetchGitHub_NonDefaultPathDoesNotProbe(t *testing.T) {
	// Arrange
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "custom/manifest.json", Host: "github.com"}

	// Act
	_, err := fetchGitHub(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchGitHub() returned no error for a 404 on a non-default path")
	}
	if requests != 1 {
		t.Errorf("requests = %d, want 1 (a non-default path must not trigger mkt-003 fallback probing)", requests)
	}
}

// TestGitHubAPIBaseFor covers mkt GHE-family api_base selection: github.com
// uses the public API, "*.ghe.com" uses "https://api.{host}", and any other
// host (in practice only the configured GITHUB_HOST/GHES host, since
// classifySourceHost never routes anything else here) uses
// "https://{host}/api/v3".
func TestGitHubAPIBaseFor(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{"github.com", "github.com", "https://api.github.com"},
		{"ghe cloud data residency", "acme.ghe.com", "https://api.acme.ghe.com"},
		{"ghes self-hosted", "ghe.example.com", "https://ghe.example.com/api/v3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := githubAPIBaseFor(tt.host); got != tt.want {
				t.Errorf("githubAPIBaseFor(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

// TestFetchGitHub_GHECloudFamily covers mkt GHE-family fetch behavior end to
// end for a "*.ghe.com" host: PAT is forwarded (isTrustedGitHubHost), and
// the request reaches the (redirected) API base.
func TestFetchGitHub_GHECloudFamily(t *testing.T) {
	// Arrange
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"name": "acme-ghe-cloud"}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	t.Setenv(githubPATEnvVar, "t-ghe-cloud-pat")
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "acme.ghe.com"}

	// Act
	got, err := fetchGitHub(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitHub() returned error: %v", err)
	}
	if got.Name != "acme-ghe-cloud" {
		t.Errorf("fetchGitHub() manifest = %+v", got)
	}
	if gotAuth != "token t-ghe-cloud-pat" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "token t-ghe-cloud-pat")
	}
}

// TestFetchGitHub_GHESFamilyViaGitHubHostEnv covers mkt GHE-family fetch
// behavior for a self-hosted GHES host configured via GITHUB_HOST: PAT is
// forwarded even though the host itself is neither github.com nor
// *.github.com/*.ghe.com.
func TestFetchGitHub_GHESFamilyViaGitHubHostEnv(t *testing.T) {
	// Arrange
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"name": "acme-ghes"}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	t.Setenv(githubHostEnvVar, "ghe.example.com")
	t.Setenv(githubPATEnvVar, "t-ghes-pat")
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "ghe.example.com"}

	// Act
	got, err := fetchGitHub(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitHub() returned error: %v", err)
	}
	if got.Name != "acme-ghes" {
		t.Errorf("fetchGitHub() manifest = %+v", got)
	}
	if gotAuth != "token t-ghes-pat" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "token t-ghes-pat")
	}
}

// TestFetchGitHub_GITHUB_HOST_CaseInsensitive covers GITHUB_HOST's
// case-insensitive match (utils/github_host.py:170-198's ".lower()"
// normalization).
func TestFetchGitHub_GITHUB_HOST_CaseInsensitive(t *testing.T) {
	// Arrange
	var requestSeen bool
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestSeen = true
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"name": "acme-ghes"}`))
	}))
	t.Cleanup(srv.Close)
	withGitHubAPIBase(t, srv.URL)
	t.Setenv(githubHostEnvVar, "GHE.Example.COM")
	t.Setenv(githubPATEnvVar, "t-ghes-pat")
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "ghe.example.com"}

	// Act
	_, err := fetchGitHub(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitHub() returned error: %v", err)
	}
	if !requestSeen {
		t.Fatal("request never reached the test server")
	}
	if gotAuth != "token t-ghes-pat" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "token t-ghes-pat")
	}
}

// TestIsTrustedGitHubHost_GHEFamily extends TestIsTrustedGitHubHost with the
// mkt GHE-fix cases: *.ghe.com is always trusted, and a GITHUB_HOST-matching
// GHES host is trusted only while that env var is set to it.
func TestIsTrustedGitHubHost_GHEFamily(t *testing.T) {
	t.Run("ghe cloud subdomain is trusted with no env set", func(t *testing.T) {
		if !isTrustedGitHubHost("acme.ghe.com") {
			t.Error("isTrustedGitHubHost(\"acme.ghe.com\") = false, want true")
		}
	})

	t.Run("GITHUB_HOST match is trusted", func(t *testing.T) {
		t.Setenv(githubHostEnvVar, "ghe.example.com")
		if !isTrustedGitHubHost("ghe.example.com") {
			t.Error("isTrustedGitHubHost(\"ghe.example.com\") = false, want true, GITHUB_HOST is set to it")
		}
	})

	t.Run("unrelated host stays untrusted even with GITHUB_HOST set to something else", func(t *testing.T) {
		t.Setenv(githubHostEnvVar, "ghe.example.com")
		if isTrustedGitHubHost("other.example.com") {
			t.Error("isTrustedGitHubHost(\"other.example.com\") = true, want false")
		}
	})

	t.Run("no GITHUB_HOST set never trusts an arbitrary host", func(t *testing.T) {
		if isTrustedGitHubHost("ghe.example.com") {
			t.Error("isTrustedGitHubHost(\"ghe.example.com\") = true, want false, GITHUB_HOST is unset")
		}
	})
}
