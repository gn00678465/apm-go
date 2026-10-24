package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A registry package is taken from apm_modules only while its directory
// hashes to the lock's content_hash, as Oracle 0.33.0's
// _should_skip_redownload decides (install/phases/_redownload.py:25-37). A
// cache that was edited or lost a file, and a lock with no content_hash (one
// an earlier apm-go wrote), make the second install download the package
// again: the deployed file and the cache are the package's again, and the
// lock is the one the first install wrote. The header has no "(cached)",
// there is no Resolving line and the last line counts the package, as the
// Oracle prints them (measured, A/p1-before/{edit,delete,nohash}/or).
func TestRegistryInstall_ChangedCacheIsDownloadedAgain(t *testing.T) {
	skillRel := filepath.Join(".apm", "skills", "probe", "SKILL.md")
	contentHash := regexp.MustCompile(`(?m)^  content_hash: sha256:[0-9a-f]+\n`)
	cases := map[string]func(t *testing.T, cache, lockPath string){
		"edited cache": func(t *testing.T, cache, _ string) {
			if err := os.WriteFile(filepath.Join(cache, skillRel), []byte("---\nname: probe\ndescription: p\n---\n\nINJECTED\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"file removed from cache": func(t *testing.T, cache, _ string) {
			if err := os.Remove(filepath.Join(cache, skillRel)); err != nil {
				t.Fatal(err)
			}
		},
		"lock without content_hash": func(t *testing.T, _, lockPath string) {
			data, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			stripped := contentHash.ReplaceAll(data, nil)
			if string(stripped) == string(data) {
				t.Fatalf("lock has no content_hash to drop:\n%s", data)
			}
			if err := os.WriteFile(lockPath, stripped, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			enableRegistries(t)
			fx := buildRegistryFixture(t)
			sum := sha256.Sum256(fx)
			srv := startE2EServer(t, fx, "sha256:"+hex.EncodeToString(sum[:]))
			dir := chdirTemp(t)
			writeGlobalStaleFile(t, filepath.Join(dir, "apm.yml"), "name: consumer\nversion: 0.1.0\nregistries:\n  local:\n    url: "+srv.URL+
				"\n    insecure: true\n  default: local\ndependencies:\n  apm:\n    - id: acme/sample\n      version: 1.0.0\n")
			deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
			var err error
			captureUninstallStdout(t, func() { err = runInstall(deps, false, true, "claude", "", nil, nil) })
			if err != nil {
				t.Fatalf("first install: %v", err)
			}
			lockPath := filepath.Join(dir, "apm.lock.yaml")
			firstLock, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			cache := filepath.Join(dir, "apm_modules", "acme", "sample")
			original, err := os.ReadFile(filepath.Join(cache, skillRel))
			if err != nil {
				t.Fatal(err)
			}

			change(t, cache, lockPath)
			srv.mu.Lock()
			srv.versionsHits, srv.downloadHits = 0, 0
			srv.mu.Unlock()
			out := captureUninstallStdout(t, func() { err = runInstall(deps, false, true, "claude", "", nil, nil) })
			if err != nil {
				t.Fatalf("second install: %v\n%s", err, out)
			}

			srv.mu.Lock()
			dh := srv.downloadHits
			srv.mu.Unlock()
			if dh != 1 {
				t.Errorf("second install fetched /download %d times, want 1", dh)
			}
			for _, p := range []string{filepath.Join(cache, skillRel), filepath.Join(dir, ".claude", "skills", "probe", "SKILL.md")} {
				if got, err := os.ReadFile(p); err != nil || string(got) != string(original) {
					t.Errorf("%s = %q (%v), want the package's %q", p, got, err, original)
				}
			}
			if got, _ := os.ReadFile(lockPath); string(got) != string(firstLock) {
				t.Errorf("lock after the second install\n%s\nwant the first install's\n%s", got, firstLock)
			}
			if strings.Contains(out, "(cached)") || strings.Contains(out, "Resolving") || !strings.Contains(out, "Installed 1 APM dependency") {
				t.Errorf("second install output, want a header with no (cached), no Resolving line and Installed 1:\n%s", out)
			}
		})
	}
}
