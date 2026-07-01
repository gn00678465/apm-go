package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

// buildFixture returns a deterministic tar.gz APM package (apm.yml + .apm/).
func buildFixture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(name string, data []byte) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: 0o644}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	add("apm.yml", []byte("name: sample\nversion: 1.0.0\n"))
	add(".apm/skills/probe/SKILL.md", []byte("---\nname: probe\ndescription: p\n---\n"))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func sha256Envelope(b []byte) string {
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}

// fixtureServer serves /versions + /download for acme/sample@1.0.0.
func fixtureServer(t *testing.T, archive []byte, advertisedDigest string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/versions"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"package":"acme/sample","versions":[{"version":"1.0.0","digest":%q,"published_at":"2026-01-01T00:00:00Z"}]}`, advertisedDigest)
		case strings.HasSuffix(r.URL.Path, "/download"):
			w.Header().Set("Content-Type", "application/gzip")
			w.Write(archive)
		default:
			http.Error(w, "nf", 404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newLoader(t *testing.T, srvURL string) *Loader {
	return &Loader{
		Registries:      map[string]manifest.Registry{"local": {URL: srvURL, Insecure: true}},
		DefaultRegistry: "local",
		ModulesDir:      t.TempDir(),
	}
}

func regRef() *manifest.DependencyReference {
	return &manifest.DependencyReference{Source: "registry", RepoURL: "acme/sample", Reference: "1.0.0", RegistryName: "local"}
}

func TestLoader_FreshInstall_ExtractsAndRecordsResolution(t *testing.T) {
	fx := buildFixture(t)
	srv := fixtureServer(t, fx, sha256Envelope(fx))
	l := newLoader(t, srv.URL)

	sub, err := l.LoadPackage(regRef(), "1.0.0")
	if err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}
	if sub == nil || sub.Name != "sample" {
		t.Fatalf("sub-manifest = %+v", sub)
	}
	// extracted onto disk under modulesDir/acme/sample
	skill := filepath.Join(l.ModulesDir, "acme", "sample", ".apm", "skills", "probe", "SKILL.md")
	if _, err := os.Stat(skill); err != nil {
		t.Errorf("expected extracted skill at %s: %v", skill, err)
	}
	// resolution recorded for lockfile v2
	r, ok := l.Resolutions()["acme/sample"]
	if !ok || r.Version != "1.0.0" || r.ResolvedHash != sha256Envelope(fx) {
		t.Errorf("resolution = %+v ok=%v", r, ok)
	}
	if !strings.HasSuffix(r.ResolvedURL, "/v1/packages/acme/sample/versions/1.0.0/download") {
		t.Errorf("ResolvedURL = %q", r.ResolvedURL)
	}
}

// AC3 negative: advertised digest != served bytes -> fail closed, no extraction.
func TestLoader_HashMismatch_FailsClosed(t *testing.T) {
	fx := buildFixture(t)
	badDigest := "sha256:" + strings.Repeat("00", 32)
	srv := fixtureServer(t, fx, badDigest)
	l := newLoader(t, srv.URL)

	_, err := l.LoadPackage(regRef(), "1.0.0")
	if err == nil {
		t.Fatal("expected hash-mismatch failure")
	}
	// lk-013: diagnostic names the entry + expected + actual.
	msg := err.Error()
	if !strings.Contains(msg, "acme/sample") || !strings.Contains(msg, "expected") || !strings.Contains(msg, "actual") {
		t.Errorf("mismatch diagnostic must name entry+expected+actual, got %q", msg)
	}
	if _, statErr := os.Stat(filepath.Join(l.ModulesDir, "acme", "sample")); statErr == nil {
		t.Errorf("extraction dir must not exist after hash mismatch")
	}
}

// lk-016: a bare-hex digest advertised by the registry is stored as a normalized
// sha256:<hex> envelope in resolved_hash.
func TestLoader_NormalizesBareDigest(t *testing.T) {
	fx := buildFixture(t)
	bareHex := strings.TrimPrefix(sha256Envelope(fx), "sha256:")
	srv := fixtureServer(t, fx, bareHex) // advertise WITHOUT the sha256: prefix
	l := newLoader(t, srv.URL)

	if _, err := l.LoadPackage(regRef(), "1.0.0"); err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}
	if got := l.Resolutions()["acme/sample"].ResolvedHash; got != "sha256:"+bareHex {
		t.Errorf("resolved_hash = %q, want normalized sha256:%s", got, bareHex)
	}
}

// AC4 / sc-007: a 401 surfaces a remediation hint naming the env var, and never
// the token literal.
func TestLoader_401_RemediationHintNoToken(t *testing.T) {
	t.Setenv("APM_REGISTRY_TOKEN_LOCAL", "top-secret-tok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", 401)
	}))
	t.Cleanup(srv.Close)
	l := newLoader(t, srv.URL)

	_, err := l.LoadPackage(regRef(), "1.0.0")
	if err == nil {
		t.Fatal("expected 401 failure")
	}
	msg := err.Error()
	if !strings.Contains(msg, "APM_REGISTRY_TOKEN_LOCAL") {
		t.Errorf("401 must surface env-var remediation, got %q", msg)
	}
	if strings.Contains(msg, "top-secret-tok") {
		t.Errorf("token leaked in 401 error: %q", msg)
	}
}
