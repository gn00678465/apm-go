package lockfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
)

func oraclePath(t *testing.T, rel string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(wd, "..", "..")
	candidates := []string{
		filepath.Join(root, "conformance-kit", "oracle", rel),
		filepath.Join(root, "..", "conformance-kit", "oracle", rel),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skipf("oracle fixture not found: %s", rel)
	return ""
}

func loadFixture(t *testing.T, rel string) *Lockfile {
	t.Helper()
	p := oraclePath(t, rel)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	lf, err := ParseLockfile(node)
	if err != nil {
		t.Fatalf("ParseLockfile: %v", err)
	}
	return lf
}

func TestParseLockfile_V1GitOnly(t *testing.T) {
	lf := loadFixture(t, filepath.Join("lockfile", "v1-git-only.yml"))
	if lf.Version != "1" {
		t.Errorf("version = %q, want %q", lf.Version, "1")
	}
	if len(lf.Dependencies) != 1 {
		t.Fatalf("deps count = %d, want 1", len(lf.Dependencies))
	}
	dep := lf.Dependencies[0]
	if dep.RepoURL != "github.com/octocat/example" {
		t.Errorf("repo_url = %q", dep.RepoURL)
	}
	if dep.ResolvedCommit != "7f3c9a4d2e1b8c7f0a9e6d5c4b3a2918f7e6d5c4" {
		t.Errorf("resolved_commit = %q", dep.ResolvedCommit)
	}
	if dep.ResolvedRef != "v1.2.0" {
		t.Errorf("resolved_ref = %q", dep.ResolvedRef)
	}
	if dep.Depth != 1 {
		t.Errorf("depth = %d, want 1", dep.Depth)
	}
	if len(dep.DeployedFiles) != 1 {
		t.Errorf("deployed_files count = %d", len(dep.DeployedFiles))
	}
}

func TestParseLockfile_V2WithRegistry(t *testing.T) {
	lf := loadFixture(t, filepath.Join("lockfile", "v2-with-registry.yml"))
	if lf.Version != "2" {
		t.Errorf("version = %q, want %q", lf.Version, "2")
	}
	if len(lf.Dependencies) != 1 {
		t.Fatalf("deps count = %d, want 1", len(lf.Dependencies))
	}
	dep := lf.Dependencies[0]
	if dep.Source != "registry" {
		t.Errorf("source = %q, want registry", dep.Source)
	}
	if dep.ResolvedURL == "" {
		t.Error("resolved_url should not be empty")
	}
	if dep.ResolvedHash == "" {
		t.Error("resolved_hash should not be empty")
	}
	if dep.Version != "1.4.2" {
		t.Errorf("version = %q", dep.Version)
	}
	if len(dep.DeployedHashes) != 1 {
		t.Errorf("deployed_file_hashes count = %d", len(dep.DeployedHashes))
	}
}

func TestParseLockfile_RoundTripUnknownFields(t *testing.T) {
	lf := loadFixture(t, filepath.Join("lockfile", "round-trip-unknown-fields.yml"))
	if lf.Version != "2" {
		t.Errorf("version = %q", lf.Version)
	}
	if len(lf.Dependencies) != 1 {
		t.Fatalf("deps count = %d", len(lf.Dependencies))
	}
	dep := lf.Dependencies[0]
	if dep.RepoURL != "github.com/acme/foo" {
		t.Errorf("repo_url = %q", dep.RepoURL)
	}
}

func TestParseLockfile_UnknownVersion(t *testing.T) {
	p := oraclePath(t, filepath.Join("lockfile", "invalid-unknown-version.yml"))
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	_, err = ParseLockfile(node)
	if err == nil {
		t.Fatal("expected error for unknown lockfile_version")
	}
	if !strings.Contains(err.Error(), "99") {
		t.Errorf("error should mention version 99: %v", err)
	}
}

func TestParseLockfile_FindByKey(t *testing.T) {
	lf := loadFixture(t, filepath.Join("lockfile", "v1-git-only.yml"))
	dep := lf.FindByKey("github.com/octocat/example")
	if dep == nil {
		t.Fatal("FindByKey should find the dependency")
	}
	if dep.ResolvedRef != "v1.2.0" {
		t.Errorf("resolved_ref = %q", dep.ResolvedRef)
	}

	missing := lf.FindByKey("nonexistent")
	if missing != nil {
		t.Error("FindByKey should return nil for missing key")
	}
}
