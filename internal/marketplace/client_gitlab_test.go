package marketplace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withGitLabAPIBase redirects gitlabAPIBaseFn at an httptest.Server for the
// duration of the test (ignoring the host argument, since the real function
// builds an https:// URL and httptest.Server serves plain http), restoring
// it afterward.
func withGitLabAPIBase(t *testing.T, base string) {
	t.Helper()
	orig := gitlabAPIBaseFn
	gitlabAPIBaseFn = func(string) string { return base }
	t.Cleanup(func() { gitlabAPIBaseFn = orig })
}

// TestFetchGitLab_HappyPath covers mkt-023: fetchGitLab hits the REST v4
// raw-file endpoint with both the project path and file path percent-encoded
// as a whole (including their internal "/" separators), and consumes the
// plain-text response body directly as the manifest content.
func TestFetchGitLab_HappyPath(t *testing.T) {
	// Arrange
	body := []byte(`{"name": "acme", "owner": "acme-owner", "plugins": [{"name": "p", "source": "./p"}]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/projects/acme-owner%2Facme-repo/repository/files/marketplace.json/raw"
		if r.URL.EscapedPath() != wantPath {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("ref") != "main" {
			http.Error(w, "unexpected ref", http.StatusBadRequest)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "acme-owner", Repo: "acme-repo", Ref: "main", Path: "marketplace.json", Host: "gitlab.com"}

	// Act
	got, err := fetchGitLab(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitLab() returned error: %v", err)
	}
	if got.Name != "acme" || got.Owner != "acme-owner" {
		t.Errorf("fetchGitLab() manifest = %+v, want Name=acme Owner=acme-owner", got)
	}
	if len(got.Plugins) != 1 || got.Plugins[0].Name != "p" {
		t.Errorf("fetchGitLab() Plugins = %+v", got.Plugins)
	}
}

// TestFetchGitLab_NestedFilePathFullyEncoded covers a manifest path with its
// own "/" separators (e.g. mkt-003's ".github/plugin/marketplace.json"
// fallback shape): every "/" in the file path, not just the project path,
// must be percent-encoded.
func TestFetchGitLab_NestedFilePathFullyEncoded(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/projects/o%2Fr/repository/files/.github%2Fplugin%2Fmarketplace.json/raw"
		if r.URL.EscapedPath() != wantPath {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"name": "acme"}`))
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: ".github/plugin/marketplace.json", Host: "gitlab.com"}

	// Act
	got, err := fetchGitLab(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitLab() returned error: %v", err)
	}
	if got.Name != "acme" {
		t.Errorf("fetchGitLab() manifest = %+v", got)
	}
}

// TestFetchGitLab_ForwardsPATForTrustedHost covers mkt-011: a gitlab.com (or
// self-hosted GitLab) source forwards GITLAB_APM_PAT as a "PRIVATE-TOKEN"
// header.
func TestFetchGitLab_ForwardsPATForTrustedHost(t *testing.T) {
	// Arrange
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		w.Write([]byte(`{"name": "acme"}`))
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	t.Setenv(gitlabPATEnvVar, "t-secret-pat")
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "gitlab.com"}

	// Act
	_, err := fetchGitLab(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitLab() returned error: %v", err)
	}
	if gotToken != "t-secret-pat" {
		t.Errorf("PRIVATE-TOKEN header = %q, want %q", gotToken, "t-secret-pat")
	}
}

// TestFetchGitLab_ForwardsPATForSelfHostedGitLab covers the self-hosted
// GitLab part of mkt-011's trust boundary: a self-managed host is trusted
// only when it is explicitly allowlisted via GITLAB_HOST (or
// APM_GITLAB_HOSTS), NOT merely because its name contains "gitlab".
func TestFetchGitLab_ForwardsPATForSelfHostedGitLab(t *testing.T) {
	// Arrange
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		w.Write([]byte(`{"name": "acme"}`))
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	t.Setenv(gitlabPATEnvVar, "t-secret-pat")
	t.Setenv(gitlabHostEnvVar, "gitlab.example.com")
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "gitlab.example.com"}

	// Act
	_, err := fetchGitLab(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitLab() returned error: %v", err)
	}
	if gotToken != "t-secret-pat" {
		t.Errorf("PRIVATE-TOKEN header = %q, want %q", gotToken, "t-secret-pat")
	}
}

// TestFetchGitLab_SubstringHostDoesNotForwardPAT is the regression guard for
// the credential-exfiltration bug where any host merely CONTAINING "gitlab"
// (e.g. an attacker's gitlab.evil.com) was trusted and received the PAT. The
// host is not gitlab.com and is not in any allowlist env var, so it must NOT
// receive the token even though "gitlab" is a substring of its name.
func TestFetchGitLab_SubstringHostDoesNotForwardPAT(t *testing.T) {
	// Arrange
	var gotToken string
	requestSeen := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestSeen = true
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		w.Write([]byte(`{"name": "acme"}`))
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	t.Setenv(gitlabPATEnvVar, "t-secret-pat")
	// No GITLAB_HOST / APM_GITLAB_HOSTS set: gitlab.evil.com is not allowlisted.
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "gitlab.evil.com"}

	// Act
	_, err := fetchGitLab(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitLab() returned error: %v", err)
	}
	if !requestSeen {
		t.Fatal("request never reached the test server")
	}
	if gotToken != "" {
		t.Errorf("PRIVATE-TOKEN header = %q, want empty (substring-only host must not receive the PAT)", gotToken)
	}
}

// TestFetchGitLab_UntrustedHostDoesNotForwardPAT covers mkt-011's negative
// case, exercising fetchGitLab's own trust guard directly (a hand-built
// source with a non-GitLab Host, bypassing Kind() dispatch).
func TestFetchGitLab_UntrustedHostDoesNotForwardPAT(t *testing.T) {
	// Arrange
	var gotToken string
	requestSeen := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestSeen = true
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		w.Write([]byte(`{"name": "acme"}`))
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	t.Setenv(gitlabPATEnvVar, "t-secret-pat")
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "git.example.com"}

	// Act
	_, err := fetchGitLab(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitLab() returned error: %v", err)
	}
	if !requestSeen {
		t.Fatal("request never reached the test server")
	}
	if gotToken != "" {
		t.Errorf("PRIVATE-TOKEN header = %q, want empty (untrusted host must not receive the PAT)", gotToken)
	}
}

// TestIsTrustedGitLabHost table-tests the mkt-011 trust boundary directly:
// an EXACT-match allowlist (gitlab.com + GITLAB_HOST/APM_GITLAB_HOSTS), not
// a substring test. The gitlab.evil.com / notgitlab.io rows are the
// regression guards for the credential-exfiltration bug.
func TestIsTrustedGitLabHost(t *testing.T) {
	t.Setenv(gitlabHostEnvVar, "gitlab.example.com")
	t.Setenv(gitlabHostsEnvVar, "gl1.corp.io, gl2.corp.io")
	tests := []struct {
		name string
		host string
		want bool
	}{
		{"exact gitlab.com", "gitlab.com", true},
		{"case-insensitive", "GitLab.Com", true},
		{"allowlisted via GITLAB_HOST", "gitlab.example.com", true},
		{"allowlisted via APM_GITLAB_HOSTS", "gl2.corp.io", true},
		{"substring attacker host rejected", "gitlab.evil.com", false},
		{"substring suffix attacker host rejected", "notgitlab.io", false},
		{"non-allowlisted self-managed rejected", "gitlab.internal.corp", false},
		{"unrelated host", "git.example.com", false},
		{"github host", "github.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTrustedGitLabHost(tt.host); got != tt.want {
				t.Errorf("isTrustedGitLabHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

// TestFetchGitLab_NotFound covers a missing manifest path (404).
func TestFetchGitLab_NotFound(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "gitlab.com"}

	// Act
	_, err := fetchGitLab(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchGitLab() returned no error for a 404 response")
	}
}

// TestFetchGitLab_NonOKStatus covers a non-200, non-404 response.
func TestFetchGitLab_NonOKStatus(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "gitlab.com"}

	// Act
	_, err := fetchGitLab(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchGitLab() returned no error for a 403 response")
	}
}

// TestFetchGitLab_InvalidJSON covers a malformed manifest body.
func TestFetchGitLab_InvalidJSON(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{not json"))
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "gitlab.com"}

	// Act
	_, err := fetchGitLab(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchGitLab() returned no error for malformed JSON")
	}
}

// TestFetchGitLab_TolerantOfRegistryKey re-confirms mkt-005's tolerant
// parsing through the GitLab fetch path.
func TestFetchGitLab_TolerantOfRegistryKey(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name": "acme", "plugins": [{"name": "p", "source": "./p", "registry": "custom"}]}`))
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "gitlab.com"}

	// Act
	got, err := fetchGitLab(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitLab() returned error for a manifest with a 'registry' key: %v", err)
	}
	if len(got.Plugins) != 1 || got.Plugins[0].Registry != "custom" {
		t.Errorf("fetchGitLab() Plugins = %+v, want one plugin with Registry=%q", got.Plugins, "custom")
	}
}

// TestFetchGitLab_DefaultsRefWhenEmpty covers the defensive ref/path
// fallback (in practice ParseMarketplaceSource always populates these, but
// fetchGitLab does not assume that).
func TestFetchGitLab_DefaultsRefWhenEmpty(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/projects/o%2Fr/repository/files/marketplace.json/raw"
		if r.URL.EscapedPath() != wantPath || r.URL.Query().Get("ref") != "main" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"name": "acme"}`))
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "o", Repo: "r", Host: "gitlab.com"}

	// Act
	got, err := fetchGitLab(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitLab() returned error: %v", err)
	}
	if got.Name != "acme" {
		t.Errorf("fetchGitLab() manifest = %+v", got)
	}
}

// TestFetchGitLab_NetworkErrorNeverEchoesPATOrURL covers the credsec
// convention: a transport-level failure must not leak the PAT or the
// request URL in its error message.
func TestFetchGitLab_NetworkErrorNeverEchoesPATOrURL(t *testing.T) {
	// Arrange
	withGitLabAPIBase(t, "https://127.0.0.1:1")
	t.Setenv(gitlabPATEnvVar, "t-should-not-leak-in-error")
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "marketplace.json", Host: "gitlab.com"}

	// Act
	_, err := fetchGitLab(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchGitLab() returned no error for an unreachable host")
	}
	if strings.Contains(err.Error(), "t-should-not-leak-in-error") {
		t.Errorf("fetchGitLab() error leaked the PAT: %v", err)
	}
}

// TestFetchGitLab_ProbesThirdCandidateAfterTwo404s covers mkt-003: when
// s.Path is unset/default, a 404 on "marketplace.json" and
// ".github/plugin/marketplace.json" falls through to
// ".claude-plugin/marketplace.json" instead of failing after the first miss.
func TestFetchGitLab_ProbesThirdCandidateAfterTwo404s(t *testing.T) {
	// Arrange
	wantPath := "/projects/o%2Fr/repository/files/.claude-plugin%2Fmarketplace.json/raw"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != wantPath {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"name": "found-at-claude-plugin"}`))
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Host: "gitlab.com"}

	// Act
	got, err := fetchGitLab(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGitLab() returned error: %v", err)
	}
	if got.Name != "found-at-claude-plugin" {
		t.Errorf("fetchGitLab().Name = %q, want %q", got.Name, "found-at-claude-plugin")
	}
}

// TestFetchGitLab_AllCandidates404 covers the exhausted-probe case: every
// mkt-003 candidate 404s, so fetchGitLab returns an error.
func TestFetchGitLab_AllCandidates404(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Host: "gitlab.com"}

	// Act
	_, err := fetchGitLab(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchGitLab() returned no error when every mkt-003 candidate 404s")
	}
}

// TestFetchGitLab_NonDefaultPathDoesNotProbe covers the negative half of
// mkt-003: an explicit, non-default Path is tried as-is, with no fallback
// probing across localManifestProbeOrder.
func TestFetchGitLab_NonDefaultPathDoesNotProbe(t *testing.T) {
	// Arrange
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	withGitLabAPIBase(t, srv.URL)
	src := &MarketplaceSource{Owner: "o", Repo: "r", Ref: "main", Path: "custom/manifest.json", Host: "gitlab.com"}

	// Act
	_, err := fetchGitLab(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchGitLab() returned no error for a 404 on a non-default path")
	}
	if requests != 1 {
		t.Errorf("requests = %d, want 1 (a non-default path must not trigger mkt-003 fallback probing)", requests)
	}
}
