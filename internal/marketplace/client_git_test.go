package marketplace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitCmd runs a git subcommand in dir, failing the test on error. Mirrors
// internal/gitops/clone_test.go's helper of the same name/shape.
func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// initGitRemote creates a real local git repo at dir on the given branch
// (set at init time via "-b" so the fixture never depends on the host's
// init.defaultBranch config), writes files (path -> content, relative to
// dir) and commits them. Returns the resulting commit SHA.
func initGitRemote(t *testing.T, dir, branch string, files map[string]string) string {
	t.Helper()
	gitCmd(t, dir, "init", "-b", branch)
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", full, err)
		}
	}
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "fixture commit")
	return gitCmd(t, dir, "rev-parse", "HEAD")
}

// TestFetchGit_HappyPath covers mkt-023's generic-git fallback path
// end-to-end: shallow clone a real local repo into a temp dir, read its
// top-level marketplace.json.
func TestFetchGit_HappyPath(t *testing.T) {
	// Arrange
	remote := t.TempDir()
	initGitRemote(t, remote, "main", map[string]string{
		"marketplace.json": `{"name": "acme", "owner": "acme-owner", "plugins": [{"name": "p", "source": "./p"}]}`,
	})
	src := &MarketplaceSource{URL: remote, Ref: "main", Path: defaultManifestPath}

	// Act
	got, err := fetchGit(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGit() returned error: %v", err)
	}
	if got.Name != "acme" || got.Owner != "acme-owner" {
		t.Errorf("fetchGit() manifest = %+v, want Name=acme Owner=acme-owner", got)
	}
	if len(got.Plugins) != 1 || got.Plugins[0].Name != "p" {
		t.Errorf("fetchGit() Plugins = %+v", got.Plugins)
	}
}

// TestFetchGit_ProbePathFallback covers mkt-003's fallback probing surviving
// the clone+read round trip: only the .github/plugin/marketplace.json
// candidate exists, no top-level marketplace.json.
func TestFetchGit_ProbePathFallback(t *testing.T) {
	// Arrange
	remote := t.TempDir()
	initGitRemote(t, remote, "main", map[string]string{
		".github/plugin/marketplace.json": `{"name": "fallback-found"}`,
	})
	src := &MarketplaceSource{URL: remote, Ref: "main", Path: defaultManifestPath}

	// Act
	got, err := fetchGit(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGit() returned error: %v", err)
	}
	if got.Name != "fallback-found" {
		t.Errorf("fetchGit().Name = %q, want %q", got.Name, "fallback-found")
	}
}

// TestFetchGit_ClonesAtPinnedRef proves the shallow clone actually honors
// s.Ref, not just whatever the remote's default branch currently points at:
// a tag pinned to the first commit must return that commit's content even
// though the branch has since moved on.
func TestFetchGit_ClonesAtPinnedRef(t *testing.T) {
	// Arrange
	remote := t.TempDir()
	initGitRemote(t, remote, "main", map[string]string{
		"marketplace.json": `{"name": "v1"}`,
	})
	gitCmd(t, remote, "tag", "v1.0.0")
	initGitRemote(t, remote, "main", map[string]string{
		"marketplace.json": `{"name": "v2"}`,
	})
	src := &MarketplaceSource{URL: remote, Ref: "v1.0.0", Path: defaultManifestPath}

	// Act
	got, err := fetchGit(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGit() returned error: %v", err)
	}
	if got.Name != "v1" {
		t.Errorf("fetchGit().Name = %q, want %q (pinned tag content, not current branch HEAD)", got.Name, "v1")
	}
}

// TestFetchGit_ClonesAtPinnedCommitSHA covers mkt-010's "--ref can be a
// commit SHA" case end to end: `git clone --branch <ref>` cannot resolve a
// raw commit SHA, so shallowCloneGit must fall back to a full clone +
// `git checkout <sha>` for a SHA-shaped ref. Pins to the *first* commit's
// SHA even though the branch has since moved on to a second commit, proving
// the checkout actually lands on that commit's content (not just "the
// clone succeeded").
func TestFetchGit_ClonesAtPinnedCommitSHA(t *testing.T) {
	// Arrange
	remote := t.TempDir()
	firstSHA := initGitRemote(t, remote, "main", map[string]string{
		"marketplace.json": `{"name": "v1"}`,
	})
	initGitRemote(t, remote, "main", map[string]string{
		"marketplace.json": `{"name": "v2"}`,
	})
	src := &MarketplaceSource{URL: remote, Ref: firstSHA, Path: defaultManifestPath}

	// Act
	got, err := fetchGit(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGit() returned error: %v", err)
	}
	if got.Name != "v1" {
		t.Errorf("fetchGit().Name = %q, want %q (pinned commit SHA content, not current branch HEAD)", got.Name, "v1")
	}
}

// TestShallowCloneGit_CommitSHARef covers shallowCloneGit directly: a
// SHA-shaped ref (upper/lowercase both accepted, since the check lowercases
// first) must produce a working tree checked out at that exact commit, not
// merely "a clone that happens to succeed".
func TestShallowCloneGit_CommitSHARef(t *testing.T) {
	// Arrange
	remote := t.TempDir()
	firstSHA := initGitRemote(t, remote, "main", map[string]string{
		"file.txt": "v1",
	})
	initGitRemote(t, remote, "main", map[string]string{
		"file.txt": "v2",
	})
	dir := t.TempDir()
	target := filepath.Join(dir, "clone")

	// Act
	err := shallowCloneGit(context.Background(), remote, strings.ToUpper(firstSHA), target)

	// Assert
	if err != nil {
		t.Fatalf("shallowCloneGit() returned error: %v", err)
	}
	data, readErr := os.ReadFile(filepath.Join(target, "file.txt"))
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(data) != "v1" {
		t.Errorf("file.txt content = %q, want %q (checkout must land on the pinned commit, not branch HEAD)", string(data), "v1")
	}
}

// TestFetchGit_DefaultsRefWhenEmpty covers the defensive ref fallback (in
// practice ParseMarketplaceSource always populates Ref, but fetchGit does
// not assume that), mirroring fetchGitHub/fetchGitLab's equivalent test.
func TestFetchGit_DefaultsRefWhenEmpty(t *testing.T) {
	// Arrange
	remote := t.TempDir()
	initGitRemote(t, remote, defaultSourceRef, map[string]string{
		"marketplace.json": `{"name": "acme"}`,
	})
	src := &MarketplaceSource{URL: remote, Path: defaultManifestPath}

	// Act
	got, err := fetchGit(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGit() returned error: %v", err)
	}
	if got.Name != "acme" {
		t.Errorf("fetchGit() manifest = %+v", got)
	}
}

// TestFetchGit_NoManifestFound covers the miss case: the clone succeeds but
// none of mkt-003's candidate paths exist in it.
func TestFetchGit_NoManifestFound(t *testing.T) {
	// Arrange
	remote := t.TempDir()
	initGitRemote(t, remote, "main", map[string]string{
		"README.md": "no manifest here",
	})
	src := &MarketplaceSource{URL: remote, Ref: "main", Path: defaultManifestPath}

	// Act
	_, err := fetchGit(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchGit() returned no error, want one for a repo with no manifest")
	}
}

// TestFetchGit_InvalidJSON covers a malformed manifest file in the clone.
func TestFetchGit_InvalidJSON(t *testing.T) {
	// Arrange
	remote := t.TempDir()
	initGitRemote(t, remote, "main", map[string]string{
		"marketplace.json": "{not json",
	})
	src := &MarketplaceSource{URL: remote, Ref: "main", Path: defaultManifestPath}

	// Act
	_, err := fetchGit(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchGit() returned no error for malformed JSON")
	}
}

// TestFetchGit_TolerantOfRegistryKey re-confirms mkt-005's tolerant parsing
// through the git fetch path.
func TestFetchGit_TolerantOfRegistryKey(t *testing.T) {
	// Arrange
	remote := t.TempDir()
	initGitRemote(t, remote, "main", map[string]string{
		"marketplace.json": `{"name": "acme", "plugins": [{"name": "p", "source": "./p", "registry": "custom"}]}`,
	})
	src := &MarketplaceSource{URL: remote, Ref: "main", Path: defaultManifestPath}

	// Act
	got, err := fetchGit(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGit() returned error for a manifest with a 'registry' key: %v", err)
	}
	if len(got.Plugins) != 1 || got.Plugins[0].Registry != "custom" {
		t.Errorf("fetchGit() Plugins = %+v, want one plugin with Registry=%q", got.Plugins, "custom")
	}
}

// TestFetchGit_ProvenanceEmpty covers design.md's "SourceURL/SourceDigest
// provenance is url-kind only" contract: a git-kind fetch must leave both
// empty.
func TestFetchGit_ProvenanceEmpty(t *testing.T) {
	// Arrange
	remote := t.TempDir()
	initGitRemote(t, remote, "main", map[string]string{
		"marketplace.json": `{"name": "acme"}`,
	})
	src := &MarketplaceSource{URL: remote, Ref: "main", Path: defaultManifestPath}

	// Act
	got, err := fetchGit(context.Background(), src)

	// Assert
	if err != nil {
		t.Fatalf("fetchGit() returned error: %v", err)
	}
	if got.SourceURL != "" || got.SourceDigest != "" {
		t.Errorf("fetchGit() SourceURL/SourceDigest = %q/%q, want both empty", got.SourceURL, got.SourceDigest)
	}
}

// countGitCloneTempDirs counts leftover apm-marketplace-git-* directories
// under os.TempDir(), used to prove fetchGit's temp clone directory is
// actually removed (defer RemoveAll), not merely orphaned.
func countGitCloneTempDirs(t *testing.T) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), gitCloneTempDirPrefix+"*"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	return len(matches)
}

// TestFetchGit_CleansUpTempCloneOnSuccess covers the "defer RemoveAll"
// requirement: after a successful fetch, no apm-marketplace-git-* directory
// is left behind under the OS temp dir.
func TestFetchGit_CleansUpTempCloneOnSuccess(t *testing.T) {
	// Arrange
	remote := t.TempDir()
	initGitRemote(t, remote, "main", map[string]string{
		"marketplace.json": `{"name": "acme"}`,
	})
	src := &MarketplaceSource{URL: remote, Ref: "main", Path: defaultManifestPath}
	before := countGitCloneTempDirs(t)

	// Act
	if _, err := fetchGit(context.Background(), src); err != nil {
		t.Fatalf("fetchGit() returned error: %v", err)
	}

	// Assert
	if after := countGitCloneTempDirs(t); after != before {
		t.Errorf("leftover apm-marketplace-git-* temp dirs after success: before=%d after=%d", before, after)
	}
}

// TestFetchGit_CleansUpTempCloneOnFailure covers the same cleanup guarantee
// on the error path (clone succeeds, manifest read/parse fails): the defer
// must still fire.
func TestFetchGit_CleansUpTempCloneOnFailure(t *testing.T) {
	// Arrange
	remote := t.TempDir()
	initGitRemote(t, remote, "main", map[string]string{
		"README.md": "no manifest here",
	})
	src := &MarketplaceSource{URL: remote, Ref: "main", Path: defaultManifestPath}
	before := countGitCloneTempDirs(t)

	// Act
	if _, err := fetchGit(context.Background(), src); err == nil {
		t.Fatal("fetchGit() returned no error, want one for a repo with no manifest")
	}

	// Assert
	if after := countGitCloneTempDirs(t); after != before {
		t.Errorf("leftover apm-marketplace-git-* temp dirs after failure: before=%d after=%d", before, after)
	}
}

// TestFetchGit_CloneFailureNotAGitRepo covers a hard clone failure (source
// is not a git repository at all) and that cleanup still happens.
func TestFetchGit_CloneFailureNotAGitRepo(t *testing.T) {
	// Arrange
	notARepo := t.TempDir()
	if err := os.WriteFile(filepath.Join(notARepo, "file.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	src := &MarketplaceSource{URL: notARepo, Ref: "main", Path: defaultManifestPath}
	before := countGitCloneTempDirs(t)

	// Act
	_, err := fetchGit(context.Background(), src)

	// Assert
	if err == nil {
		t.Fatal("fetchGit() returned no error for a non-git source directory")
	}
	if after := countGitCloneTempDirs(t); after != before {
		t.Errorf("leftover apm-marketplace-git-* temp dirs after clone failure: before=%d after=%d", before, after)
	}
}
