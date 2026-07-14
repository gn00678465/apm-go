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

func TestParseLockfile_RejectsPathTraversal(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"repo_url dotdot", "lockfile_version: \"2\"\ndependencies:\n  - repo_url: ../../escape\n    source: registry\n"},
		{"repo_url absolute", "lockfile_version: \"2\"\ndependencies:\n  - repo_url: /etc/evil\n    source: registry\n"},
		{"virtual_path dotdot", "lockfile_version: \"1\"\ndependencies:\n  - repo_url: github.com/o/r\n    virtual_path: ../../x\n"},
		{"deployed_file dotdot", "lockfile_version: \"1\"\ndependencies:\n  - repo_url: github.com/o/r\n    deployed_files:\n      - ../../outside.md\n"},
		{"local deployed hash dotdot", "lockfile_version: \"1\"\ndependencies: []\nlocal_deployed_file_hashes:\n  ../../x.md: \"sha256:abc\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node, err := yamlcore.SafeLoad([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("SafeLoad: %v", err)
			}
			if _, err := ParseLockfile(node); err == nil {
				t.Fatal("expected path-traversal rejection, got nil")
			} else if !strings.Contains(err.Error(), "..") && !strings.Contains(err.Error(), "absolute") {
				t.Errorf("error should flag traversal/absolute, got %v", err)
			}
		})
	}
}

// TestParseLockfile_AbsoluteRepoURL_GitSourceAllowed covers this task's
// approved design: a "git"-sourced dependency's repo_url MAY be an
// OS-absolute filesystem path -- mkt-025's local-marketplace fast path (and
// a plain `apm install /abs/path` local git dependency) legitimately
// resolve to one, and the lockfile entry apm writes for it must round-trip
// through a later `apm install`'s lockfile read. Windows drive-letter,
// UNC, and POSIX forms are all exercised.
func TestParseLockfile_AbsoluteRepoURL_GitSourceAllowed(t *testing.T) {
	tests := []struct {
		name    string
		repoURL string
	}{
		{"posix absolute", "/home/me/plugins/p"},
		{"windows drive letter", `C:\Users\me\plugins\p`},
		{"windows UNC", `\\myserver\share\plugin`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yamlSrc := "lockfile_version: \"2\"\ndependencies:\n  - repo_url: " +
				yamlQuote(tt.repoURL) + "\n    source: git\n"
			node, err := yamlcore.SafeLoad([]byte(yamlSrc))
			if err != nil {
				t.Fatalf("SafeLoad: %v", err)
			}
			lf, err := ParseLockfile(node)
			if err != nil {
				t.Fatalf("ParseLockfile: %v", err)
			}
			if len(lf.Dependencies) != 1 || lf.Dependencies[0].RepoURL != tt.repoURL {
				t.Fatalf("Dependencies = %+v, want one dep with repo_url %q", lf.Dependencies, tt.repoURL)
			}
		})
	}
}

// TestParseLockfile_AbsoluteRepoURL_NonGitSourceRejected covers the other
// half of the same design decision: an absolute repo_url is REJECTED for
// any source other than "git" -- in particular "registry" and "local" --
// since a materialization step must never treat a package-registry
// identifier (or a genuine `path:` local dependency's own LocalPath) as an
// arbitrary filesystem path. Extends TestParseLockfile_RejectsPathTraversal's
// pre-existing "repo_url absolute"/source=registry case with an explicit
// source=local negative, proving the "git"-only carve-out is precise, not a
// blanket relaxation.
func TestParseLockfile_AbsoluteRepoURL_NonGitSourceRejected(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{"registry", "registry"},
		{"local", "local"},
		{"no source at all", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceLine := ""
			if tt.source != "" {
				sourceLine = "\n    source: " + tt.source
			}
			yamlSrc := "lockfile_version: \"2\"\ndependencies:\n  - repo_url: /home/me/plugins/p" + sourceLine + "\n"
			node, err := yamlcore.SafeLoad([]byte(yamlSrc))
			if err != nil {
				t.Fatalf("SafeLoad: %v", err)
			}
			if _, err := ParseLockfile(node); err == nil {
				t.Fatal("expected an error, got nil")
			} else if !strings.Contains(err.Error(), "absolute") {
				t.Errorf("error should mention absolute: %v", err)
			}
		})
	}
}

// yamlQuote double-quotes a value for embedding into a hand-written YAML
// snippet, escaping backslashes so Windows-style paths survive as literal
// text rather than YAML escape sequences.
func yamlQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
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
