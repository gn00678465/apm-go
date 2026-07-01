package main

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
	"sync"
	"testing"

	"github.com/apm-go/apm/internal/experimental"
)

// enableRegistries turns on the experimental registries flag in an isolated,
// per-test config dir so live registry install is permitted without touching the
// real ~/.apm/config.json.
func enableRegistries(t *testing.T) {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	if err := experimental.Enable("registries"); err != nil {
		t.Fatal(err)
	}
}

// buildRegistryFixture builds a deterministic tar.gz APM package.
func buildRegistryFixture(t *testing.T) []byte {
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
	add(".apm/skills/probe/SKILL.md", []byte("---\nname: probe\ndescription: p\n---\n\n# Probe\n"))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

type e2eServer struct {
	*httptest.Server
	mu           sync.Mutex
	downloadAuth string
	versionsHits int
	downloadHits int
}

func startE2EServer(t *testing.T, archive []byte, digest string) *e2eServer {
	t.Helper()
	s := &e2eServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/versions"):
			s.versionsHits++
			s.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"package":"acme/sample","versions":[{"version":"1.0.0","digest":%q,"published_at":"2026-01-01T00:00:00Z"}]}`, digest)
		case strings.HasSuffix(r.URL.Path, "/download"):
			s.downloadHits++
			s.downloadAuth = r.Header.Get("Authorization")
			s.mu.Unlock()
			w.Header().Set("Content-Type", "application/gzip")
			w.Write(archive)
		default:
			s.mu.Unlock()
			http.Error(w, "nf", 404)
		}
	}))
	t.Cleanup(s.Server.Close)
	return s
}

// End-to-end through the real install pipeline (runInstall -> resolver ->
// registry.Loader -> client -> credsec). Covers AC1 (attach), AC3 (hash parity),
// AC6 (lockfile v2 + frozen replay).
func TestRegistryInstall_EndToEnd(t *testing.T) {
	enableRegistries(t)
	fx := buildRegistryFixture(t)
	sum := sha256.Sum256(fx)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	srv := startE2EServer(t, fx, digest)

	dir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	apmYML := fmt.Sprintf(`name: consumer
version: 0.1.0
registries:
  local:
    url: %s
    insecure: true
  default: local
dependencies:
  apm:
    - id: acme/sample
      version: 1.0.0
`, srv.URL)
	os.WriteFile("apm.yml", []byte(apmYML), 0644)

	t.Setenv("APM_REGISTRY_TOKEN_LOCAL", "e2e-secret-token")

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
		t.Fatalf("fresh install: %v", err)
	}

	// AC1: server saw Bearer on the real /download request (loopback => attach).
	if srv.downloadAuth != "Bearer e2e-secret-token" {
		t.Errorf("AC1: /download Authorization = %q, want Bearer e2e-secret-token", srv.downloadAuth)
	}

	// AC3 + AC6: lockfile v2 registry fields, resolved_hash == sha256(bytes).
	lock, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	ls := string(lock)
	for _, want := range []string{"source: registry", digest, "/v1/packages/acme/sample/versions/1.0.0/download", "version: 1.0.0"} {
		if !strings.Contains(ls, want) {
			t.Errorf("AC3/AC6: lockfile missing %q\n---\n%s", want, ls)
		}
	}
	// deployed skill exists (apm-go routes skills to the cross-tool .agents/skills/).
	if _, err := os.Stat(filepath.Join(".agents", "skills", "probe", "SKILL.md")); err != nil {
		t.Errorf("expected deployed skill: %v", err)
	}

	// AC6 replay: simulate clean checkout (apm_modules absent, lockfile+deployed
	// files committed), then frozen install re-fetches resolved_url.
	os.RemoveAll("apm_modules")
	srv.mu.Lock()
	srv.versionsHits, srv.downloadHits = 0, 0
	srv.mu.Unlock()

	if err := runInstall(deps, true, true, "claude", nil, nil); err != nil {
		t.Fatalf("frozen replay install: %v", err)
	}
	srv.mu.Lock()
	vh, dh := srv.versionsHits, srv.downloadHits
	srv.mu.Unlock()
	if vh != 0 {
		t.Errorf("AC6: frozen replay queried /versions %d times, want 0 (replay uses resolved_url)", vh)
	}
	if dh != 1 {
		t.Errorf("AC6: frozen replay fetched /download %d times, want 1", dh)
	}
	if _, err := os.Stat(filepath.Join("apm_modules", "acme", "sample", ".apm", "skills", "probe", "SKILL.md")); err != nil {
		t.Errorf("AC6: replay did not re-materialize package: %v", err)
	}
}

// Registry access is experimental: a fresh install of a registry dep must be
// refused (with a remediation hint) until the flag is enabled.
func TestRegistryInstall_RequiresExperimentalFlag(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir()) // isolated, flag OFF

	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	apmYML := "name: consumer\nversion: 0.1.0\n" +
		"registries:\n  local:\n    url: http://127.0.0.1:1\n  default: local\n" +
		"dependencies:\n  apm:\n    - id: acme/sample\n      version: 1.0.0\n"
	os.WriteFile("apm.yml", []byte(apmYML), 0o644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runInstall(deps, false, true, "claude", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "apm experimental enable registries") {
		t.Fatalf("want experimental-flag refusal, got %v", err)
	}
}

// AC8: a frozen NETWORK replay is refused when the flag is off (offline path is
// not gated; this exercises the network branch specifically).
func TestFrozen_RegistryNetwork_RequiresExperimentalFlag(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir()) // flag OFF

	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	lock := "lockfile_version: \"2\"\ndependencies:\n" +
		"  - repo_url: acme/sample\n    source: registry\n" +
		"    resolved_url: http://127.0.0.1:1/v1/packages/acme/sample/versions/1.0.0/download\n" +
		"    resolved_hash: sha256:" + strings.Repeat("00", 32) + "\n" +
		"    version: \"1.0.0\"\n    depth: 1\n"
	os.WriteFile("apm.lock.yaml", []byte(lock), 0o644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runInstall(deps, true, false, "", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "apm experimental enable registries") {
		t.Fatalf("want experimental-flag refusal on frozen network replay, got %v", err)
	}
}

// AC4 (frozen network): a 401 during frozen replay surfaces the env-var hint and
// never the token.
func TestFrozen_Network_401_NamesEnvVar(t *testing.T) {
	enableRegistries(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", 401)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	apmYML := fmt.Sprintf("name: c\nversion: 0.1.0\nregistries:\n  local:\n    url: %s\n  default: local\n"+
		"dependencies:\n  apm:\n    - id: acme/sample\n      version: 1.0.0\n", srv.URL)
	os.WriteFile("apm.yml", []byte(apmYML), 0o644)
	lock := fmt.Sprintf("lockfile_version: \"2\"\ndependencies:\n  - repo_url: acme/sample\n    source: registry\n"+
		"    resolved_url: %s/v1/packages/acme/sample/versions/1.0.0/download\n"+
		"    resolved_hash: sha256:%s\n    version: \"1.0.0\"\n    depth: 1\n", srv.URL, strings.Repeat("00", 32))
	os.WriteFile("apm.lock.yaml", []byte(lock), 0o644)
	t.Setenv("APM_REGISTRY_TOKEN_LOCAL", "hidden-tok")

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runInstall(deps, true, false, "", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "APM_REGISTRY_TOKEN_LOCAL") {
		t.Fatalf("want 401 remediation naming the env var, got %v", err)
	}
	if strings.Contains(err.Error(), "hidden-tok") {
		t.Errorf("token leaked in frozen 401 error: %v", err)
	}
}

// lk-013 (frozen network): a hash mismatch on replay names the entry + expected + actual.
func TestFrozen_Network_HashMismatch_NamesEntry(t *testing.T) {
	enableRegistries(t)
	fx := buildRegistryFixture(t)
	sum := sha256.Sum256(fx)
	srv := startE2EServer(t, fx, "sha256:"+hex.EncodeToString(sum[:])) // serves good bytes

	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	apmYML := fmt.Sprintf("name: c\nversion: 0.1.0\nregistries:\n  local:\n    url: %s\n  default: local\n"+
		"dependencies:\n  apm:\n    - id: acme/sample\n      version: 1.0.0\n", srv.URL)
	os.WriteFile("apm.yml", []byte(apmYML), 0o644)
	// lockfile records a WRONG resolved_hash -> verify fails after fetch.
	lock := fmt.Sprintf("lockfile_version: \"2\"\ndependencies:\n  - repo_url: acme/sample\n    source: registry\n"+
		"    resolved_url: %s/v1/packages/acme/sample/versions/1.0.0/download\n"+
		"    resolved_hash: sha256:%s\n    version: \"1.0.0\"\n    depth: 1\n", srv.URL, strings.Repeat("00", 32))
	os.WriteFile("apm.lock.yaml", []byte(lock), 0o644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runInstall(deps, true, false, "", nil, nil)
	if err == nil {
		t.Fatal("want hash-mismatch failure on frozen network replay")
	}
	msg := err.Error()
	if !strings.Contains(msg, "acme/sample") || !strings.Contains(msg, "expected") || !strings.Contains(msg, "actual") {
		t.Errorf("frozen mismatch must name entry+expected+actual, got %q", msg)
	}
}

// AC8: the experimental CLI command reflects and mutates flag state.
func TestExperimentalCmd_ListEnableDisable(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())

	run := func(args ...string) string {
		c := experimentalCmd()
		var buf bytes.Buffer
		c.SetOut(&buf)
		c.SetErr(&buf)
		c.SetArgs(args)
		if err := c.Execute(); err != nil {
			t.Fatalf("experimental %v: %v", args, err)
		}
		return buf.String()
	}

	if out := run("list"); !strings.Contains(out, "registries") || !strings.Contains(out, "disabled") {
		t.Errorf("list (default) = %q", out)
	}
	if out := run("enable", "registries"); !strings.Contains(out, "Enabled") {
		t.Errorf("enable = %q", out)
	}
	if !experimental.IsEnabled("registries") {
		t.Error("registries should be enabled after CLI enable")
	}
	if out := run("list"); !strings.Contains(out, "enabled") {
		t.Errorf("list after enable = %q", out)
	}
	run("disable", "registries")
	if experimental.IsEnabled("registries") {
		t.Error("registries should be disabled after CLI disable")
	}
}

// HIGH #1 fail-closed: a registry lock entry with no resolved_hash must error in
// frozen mode, not silently skip the integrity gate.
func TestFrozen_RegistryMissingHash_FailsClosed(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	lock := "lockfile_version: \"2\"\ndependencies:\n" +
		"  - repo_url: acme/sample\n    source: registry\n" +
		"    resolved_url: http://127.0.0.1:1/v1/packages/acme/sample/versions/1.0.0/download\n" +
		"    version: \"1.0.0\"\n    depth: 1\n"
	os.WriteFile("apm.lock.yaml", []byte(lock), 0o644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runInstall(deps, true, false, "", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "no resolved_hash") {
		t.Fatalf("want fail-closed on missing resolved_hash, got %v", err)
	}
}

// HIGH #1 fail-closed: a registry lock entry with a hash but no resolved_url and
// no local archive cannot be materialized/verified — must error, not skip.
func TestFrozen_RegistryNoURLNoArchive_FailsClosed(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	lock := "lockfile_version: \"2\"\ndependencies:\n" +
		"  - repo_url: acme/sample\n    source: registry\n" +
		"    resolved_hash: sha256:" + strings.Repeat("00", 32) + "\n" +
		"    version: \"1.0.0\"\n    depth: 1\n"
	os.WriteFile("apm.lock.yaml", []byte(lock), 0o644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	err := runInstall(deps, true, false, "", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot materialize") {
		t.Fatalf("want fail-closed when no url and no archive, got %v", err)
	}
}

// codex re-verify residual: a pre-existing (possibly tampered) apm_modules tree
// must be re-materialized from verified bytes on frozen replay, not accepted.
func TestFrozen_RegistryReplacesStaleMaterializedTree(t *testing.T) {
	enableRegistries(t)
	fx := buildRegistryFixture(t)
	sum := sha256.Sum256(fx)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	srv := startE2EServer(t, fx, digest)

	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	lock := fmt.Sprintf("lockfile_version: \"2\"\ndependencies:\n"+
		"  - repo_url: acme/sample\n    source: registry\n"+
		"    resolved_url: %s/v1/packages/acme/sample/versions/1.0.0/download\n"+
		"    resolved_hash: %s\n    version: \"1.0.0\"\n    depth: 1\n", srv.URL, digest)
	os.WriteFile("apm.lock.yaml", []byte(lock), 0o644)

	// pre-existing tampered/stale cache tree
	staleDir := filepath.Join("apm_modules", "acme", "sample")
	os.MkdirAll(staleDir, 0o755)
	os.WriteFile(filepath.Join(staleDir, "STALE.txt"), []byte("tampered"), 0o644)

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	if err := runInstall(deps, true, false, "", nil, nil); err != nil {
		t.Fatalf("frozen replace: %v", err)
	}
	srv.mu.Lock()
	dh := srv.downloadHits
	srv.mu.Unlock()
	if dh != 1 {
		t.Errorf("want 1 /download (re-materialize from trust anchor), got %d", dh)
	}
	if _, err := os.Stat(filepath.Join(staleDir, "STALE.txt")); err == nil {
		t.Errorf("stale/tampered cache file was not removed")
	}
	if _, err := os.Stat(filepath.Join(staleDir, ".apm", "skills", "probe", "SKILL.md")); err != nil {
		t.Errorf("verified tree not re-materialized: %v", err)
	}
}
