package registry

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

func TestFetchURL(t *testing.T) {
	fx := buildFixture(t)
	srv := fixtureServer(t, fx, sha256Envelope(fx))
	c, _ := NewClient(srv.URL, Credential{}, nil, false)
	body, ctype, err := c.FetchURL(srv.URL + "/v1/packages/acme/sample/versions/1.0.0/download")
	if err != nil || string(body) != string(fx) || ctype != "application/gzip" {
		t.Fatalf("FetchURL: err=%v ctype=%q lenmatch=%v", err, ctype, len(body) == len(fx))
	}
}

func TestClientForURL_MatchedAndAnonymous(t *testing.T) {
	regs := map[string]manifest.Registry{
		"corp": {URL: "https://reg.example.com/api/corp"},
	}
	// matched: URL under a configured registry base
	c, err := ClientForURL("https://reg.example.com/api/corp/v1/packages/a/b/versions/1/download", regs)
	if err != nil || c == nil || c.baseURL != "https://reg.example.com/api/corp" {
		t.Errorf("matched: c=%+v err=%v", c, err)
	}
	// unmatched: anonymous client scoped to scheme+host
	c2, err := ClientForURL("https://other.example.org/v1/packages/a/b/versions/1/download", regs)
	if err != nil || c2.baseURL != "https://other.example.org" || c2.cred.Header() != "" {
		t.Errorf("anonymous: c=%+v err=%v", c2, err)
	}
	// invalid URL
	if _, err := ClientForURL("://bad", regs); err == nil {
		t.Errorf("want error on invalid resolved_url")
	}
}

func TestSplitOwnerRepo(t *testing.T) {
	cases := []struct {
		in, owner, repo string
		wantErr         bool
	}{
		{"acme/sample", "acme", "sample", false},
		{"group/sub/repo", "group/sub", "repo", false},
		{"solo", "", "", true},
	}
	for _, c := range cases {
		o, r, err := splitOwnerRepo(c.in)
		if (err != nil) != c.wantErr || o != c.owner || r != c.repo {
			t.Errorf("splitOwnerRepo(%q) = (%q,%q,%v), want (%q,%q,err=%v)", c.in, o, r, err, c.owner, c.repo, c.wantErr)
		}
	}
}

func TestAliasMap(t *testing.T) {
	if got := aliasMap(manifest.Registry{URL: "https://reg.example.com", Aliases: []string{"cdn.example.net"}}); got["reg.example.com"][0] != "cdn.example.net" {
		t.Errorf("aliasMap = %+v", got)
	}
	if got := aliasMap(manifest.Registry{URL: "https://reg.example.com"}); got != nil {
		t.Errorf("no aliases should yield nil, got %+v", got)
	}
}

func TestLoader_ErrorPaths(t *testing.T) {
	// registry name not in map
	l := &Loader{Registries: map[string]manifest.Registry{}, DefaultRegistry: "", ModulesDir: t.TempDir()}
	if _, err := l.LoadPackage(&manifest.DependencyReference{Source: "registry", RepoURL: "a/b"}, ""); err == nil {
		t.Errorf("want error: no registry and no default")
	}
	l2 := &Loader{Registries: map[string]manifest.Registry{}, DefaultRegistry: "missing", ModulesDir: t.TempDir()}
	if _, err := l2.LoadPackage(&manifest.DependencyReference{Source: "registry", RepoURL: "a/b", Reference: "1.0.0"}, ""); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("want 'not configured' error, got %v", err)
	}

	// version not found
	fx := buildFixture(t)
	srv := fixtureServer(t, fx, sha256Envelope(fx))
	l3 := newLoader(t, srv.URL)
	if _, err := l3.LoadPackage(&manifest.DependencyReference{Source: "registry", RepoURL: "acme/sample", Reference: "9.9.9", RegistryName: "local"}, ""); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want version-not-found error, got %v", err)
	}
}
