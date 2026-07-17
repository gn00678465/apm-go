package registry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recordingServer captures the Authorization header seen on each path.
type recordingServer struct {
	*httptest.Server
	authByPath map[string]string
}

func newVersionsServer(t *testing.T) *recordingServer {
	t.Helper()
	rs := &recordingServer{authByPath: map[string]string{}}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.authByPath[r.URL.Path] = r.Header.Get("Authorization")
		switch {
		case strings.HasSuffix(r.URL.Path, "/versions"):
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"package":"acme/sample","versions":[{"version":"1.0.0","digest":"sha256:abc","published_at":"2026-01-01T00:00:00Z"}]}`))
		case strings.HasSuffix(r.URL.Path, "/download"):
			w.Header().Set("Content-Type", "application/gzip")
			w.Write([]byte("ARCHIVE-BYTES"))
		default:
			http.Error(w, "not found", 404)
		}
	}))
	t.Cleanup(rs.Close)
	return rs
}

func TestClient_ListAndDownload_AttachesTokenOnLoopback(t *testing.T) {
	srv := newVersionsServer(t)
	c, err := NewClient(srv.URL, Credential{Scheme: "bearer", Value: "secret-tok"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	vs, err := c.ListVersions("acme", "sample")
	if err != nil || len(vs) != 1 || vs[0].Version != "1.0.0" || vs[0].Digest != "sha256:abc" {
		t.Fatalf("ListVersions: %v %+v", err, vs)
	}
	body, ctype, err := c.Download("acme", "sample", "1.0.0")
	if err != nil || string(body) != "ARCHIVE-BYTES" || ctype != "application/gzip" {
		t.Fatalf("Download: %v body=%q ctype=%q", err, body, ctype)
	}
	// httptest binds 127.0.0.1 (loopback) -> sc-008 permits attach.
	if got := srv.authByPath["/v1/packages/acme/sample/versions/1.0.0/download"]; got != "Bearer secret-tok" {
		t.Errorf("download Authorization = %q, want Bearer secret-tok", got)
	}
}

func TestClient_NoToken_AnonymousRequest(t *testing.T) {
	srv := newVersionsServer(t)
	c, _ := NewClient(srv.URL, Credential{}, nil, false)
	if _, err := c.ListVersions("acme", "sample"); err != nil {
		t.Fatal(err)
	}
	if got := srv.authByPath["/v1/packages/acme/sample/versions"]; got != "" {
		t.Errorf("anonymous request carried Authorization = %q", got)
	}
}

func TestClient_401_RedactsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", 401)
	}))
	t.Cleanup(srv.Close)
	c, _ := NewClient(srv.URL, Credential{Scheme: "bearer", Value: "super-secret-token"}, nil, false)
	_, err := c.ListVersions("acme", "sample")
	if err == nil {
		t.Fatal("want error")
	}
	he, ok := err.(*HTTPError)
	if !ok || he.Status != 401 {
		t.Fatalf("want HTTPError 401, got %T %v", err, err)
	}
	if strings.Contains(err.Error(), "super-secret-token") {
		t.Errorf("token leaked in error: %q", err.Error())
	}
}

// AC5a: never attach a credential to a non-loopback http base URL.
func TestClient_NoAttach_NonLoopbackHTTP(t *testing.T) {
	c, _ := NewClient("http://registry.example.com", Credential{Scheme: "bearer", Value: "tok"}, nil, false)
	req, _ := http.NewRequest("GET", "http://registry.example.com/v1/packages/a/b/versions", nil)
	if err := c.attachAuth(req); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "" {
		t.Errorf("credential attached to non-loopback http (sc-008 violation)")
	}
}

// sc-004: apm-go only accepts tar.gz, so Download must advertise application/gzip
// only (never application/zip).
func TestClient_Download_AcceptGzipOnly(t *testing.T) {
	var acceptSeen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/download") {
			acceptSeen = r.Header.Get("Accept")
			w.Header().Set("Content-Type", "application/gzip")
			w.Write([]byte("x"))
		}
	}))
	t.Cleanup(srv.Close)
	c, _ := NewClient(srv.URL, Credential{}, nil, false)
	if _, _, err := c.Download("a", "b", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if acceptSeen != "application/gzip" {
		t.Errorf("Download Accept = %q, want application/gzip only (sc-004)", acceptSeen)
	}
}

// AC2 + AC5b: composed CheckRedirect. Exercise the closure directly with crafted
// origin/target requests carrying an Authorization header.
func TestClient_CheckRedirect_DropPolicy(t *testing.T) {
	c, _ := NewClient("https://reg.example.com", Credential{Scheme: "bearer", Value: "tok"}, nil, false)

	mk := func(origin, target string) *http.Request {
		req, _ := http.NewRequest("GET", target, nil)
		req.Header.Set("Authorization", "Bearer tok")
		via0, _ := http.NewRequest("GET", origin, nil)
		if err := c.http.CheckRedirect(req, []*http.Request{via0}); err != nil {
			t.Fatalf("CheckRedirect err: %v", err)
		}
		return req
	}

	// AC2: cross-host-class redirect drops Authorization.
	if got := mk("https://reg.example.com/a", "https://cdn.other-host.net/a").Header.Get("Authorization"); got != "" {
		t.Errorf("cross-class: Authorization retained = %q", got)
	}
	// AC5b: same-host https->http downgrade drops Authorization (sc-008 gate).
	if got := mk("https://reg.example.com/a", "http://reg.example.com/a").Header.Get("Authorization"); got != "" {
		t.Errorf("downgrade: Authorization retained over http = %q", got)
	}
	// retain: same-host same-scheme redirect keeps Authorization.
	if got := mk("https://reg.example.com/a", "https://reg.example.com/b").Header.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("same-host retain: Authorization = %q, want kept", got)
	}
}

// TestClient_SizeCapEnforced covers H2: Client.get must reject an oversized
// registry response body rather than buffer it all.
func TestClient_SizeCapEnforced(t *testing.T) {
	orig := registryMaxBytes
	registryMaxBytes = 8
	t.Cleanup(func() { registryMaxBytes = orig })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"versions": ["this-is-longer-than-eight-bytes"]}`))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL, Credential{}, nil, false)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.ListVersions("acme", "sample")
	if err == nil {
		t.Fatal("ListVersions returned no error, want a size-cap rejection")
	}
	if !strings.Contains(err.Error(), "byte limit") {
		t.Errorf("error = %v, want it to mention the byte limit", err)
	}
}

// TestClient_SizeCap_ErrorBodyPreservesHTTPError covers the H2 follow-up: an
// oversized body on a >=400 response must still surface as *HTTPError (status
// preserved for auth remediation), not a plain error, with a fixed message.
func TestClient_SizeCap_ErrorBodyPreservesHTTPError(t *testing.T) {
	orig := registryMaxBytes
	registryMaxBytes = 8
	t.Cleanup(func() { registryMaxBytes = orig })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("this-error-body-is-way-longer-than-eight-bytes"))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL, Credential{}, nil, false)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.ListVersions("acme", "sample")
	he, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("error = %T (%v), want *HTTPError", err, err)
	}
	if he.Status != http.StatusForbidden {
		t.Errorf("HTTPError.Status = %d, want 403", he.Status)
	}
	if !strings.Contains(he.Msg, "byte limit") {
		t.Errorf("HTTPError.Msg = %q, want it to mention the byte limit", he.Msg)
	}
}
