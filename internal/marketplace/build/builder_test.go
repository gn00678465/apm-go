package build

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/marketplace/authoring"
)

// ── test helpers: a real local git repo fixture ─────────────────────────
//
// Mirrors internal/marketplace/authoring/refcheck_test.go's fixture
// helpers: HeadNotAllowedError / same-name-tag-priority / SHA resolution
// need a genuine `git ls-remote` against both tags AND branches, which a
// fake RefLister could only assert by construction, not prove.

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
	return string(bytes.TrimSpace(out))
}

// initGitRepoWithTags creates a real git repository in dir with a single
// commit and every tag in tags, so ResolvePackages' production gitRefLister
// can run a genuine `git ls-remote` against it without any network access
// (implement.md's "本地 git repo fixture" test requirement).
func initGitRepoWithTags(t *testing.T, dir string, tags ...string) {
	t.Helper()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "init")
	for _, tag := range tags {
		gitCmd(t, dir, "tag", tag)
	}
}

func revParse(t *testing.T, dir, ref string) string {
	t.Helper()
	return gitCmd(t, dir, "rev-list", "-n", "1", ref)
}

// ── fakes ─────────────────────────────────────────────────────────────────

// panicLister is a RefLister fake that panics if ever called -- used to
// prove a code path takes zero network/subprocess action (mkt-051's "本地套件
// 零網路" convention).
type panicLister struct{}

func (panicLister) ListRemoteRefs(source string) ([]RemoteRef, error) {
	panic("ListRemoteRefs must not be called: this path must never touch the network")
}

type fakeRefLister struct {
	refs []RemoteRef
	err  error
}

func (f fakeRefLister) ListRemoteRefs(source string) ([]RemoteRef, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.refs, nil
}

// noopMetadataFetcher is a MetadataFetcher fake that returns no metadata and
// never errors -- used by every test in this file that is not itself about
// metadata enrichment, so it can ignore that concern entirely instead of
// triggering the default (real git clone) MetadataFetcher.
type noopMetadataFetcher struct{}

func (noopMetadataFetcher) FetchMetadata(source, ref, subdir string) (string, string, error) {
	return "", "", nil
}

// panicMetadataFetcher is a MetadataFetcher fake that panics if ever
// called -- used to prove a code path takes zero network/subprocess action,
// mirroring panicLister.
type panicMetadataFetcher struct{}

func (panicMetadataFetcher) FetchMetadata(source, ref, subdir string) (string, string, error) {
	panic("FetchMetadata must not be called: this path must never touch the network")
}

// ── local packages never touch the network ──────────────────────────────

func TestResolvePackages_LocalPackage_NeverCallsLister(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "local-a", Source: "./pkgs/a", Version: "^1.0.0"},
		{Name: "local-b", Source: "./pkgs/b", Ref: "v1.0.0"},
	}}

	// Act
	resolved, _, err := ResolvePackages(cfg, Options{Lister: panicLister{}, MetadataFetcher: panicMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("len(resolved) = %d, want 2", len(resolved))
	}
	for i, rp := range resolved {
		if !rp.IsLocal {
			t.Errorf("resolved[%d].IsLocal = false, want true", i)
		}
		if rp.Ref != "" || rp.SHA != "" {
			t.Errorf("resolved[%d] Ref/SHA should be empty for a local package, got Ref=%q SHA=%q", i, rp.Ref, rp.SHA)
		}
	}
	if resolved[0].Subdir != "./pkgs/a" {
		t.Errorf("resolved[0].Subdir = %q, want ./pkgs/a", resolved[0].Subdir)
	}
}

// ── remote package requires ref or version ───────────────────────────────

func TestResolvePackages_RemotePackage_MissingRefAndVersion_ReturnsError(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: "owner/repo"},
	}}

	// Act
	_, _, err := ResolvePackages(cfg, Options{Lister: panicLister{}, MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	if err == nil {
		t.Fatal("expected an error: remote package declares neither ref nor version")
	}
}

// ── explicit ref: 40-hex SHA handling ────────────────────────────────────

func TestResolvePackages_ExplicitRef_Lowercase40HexSHA_AcceptedWithoutNetwork(t *testing.T) {
	// Arrange
	sha := strings.Repeat("a", 40)
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "pinned", Source: "owner/repo", Ref: sha},
	}}

	// Act
	resolved, _, err := ResolvePackages(cfg, Options{Lister: panicLister{}, MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if len(resolved) != 1 || resolved[0].Ref != sha || resolved[0].SHA != sha {
		t.Fatalf("resolved = %+v, want a single entry with Ref=SHA=%q", resolved, sha)
	}
}

func TestResolvePackages_ExplicitRef_Uppercase40Hex_FallsBackToRefLookup(t *testing.T) {
	// Arrange: an uppercase 40-hex string must NOT be treated as a literal
	// SHA (design.md: "大寫落回 ref 查詢") -- it falls back to tag/branch
	// lookup, which then fails to find a literal match.
	upper := strings.Repeat("A", 40)
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "pinned", Source: "owner/repo", Ref: upper},
	}}
	lister := fakeRefLister{refs: []RemoteRef{
		{Name: "refs/tags/v1.0.0", Commit: "deadbeef"},
	}}

	// Act
	_, _, err := ResolvePackages(cfg, Options{Lister: lister, MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	var refErr *RefNotFoundError
	if !errors.As(err, &refErr) {
		t.Fatalf("error = %v, want a *RefNotFoundError (uppercase SHA must fall back to ref lookup)", err)
	}
}

// ── explicit ref: real git repo (tag/branch/HEAD resolution) ────────────

func TestResolvePackages_ExplicitRef_TagMatch(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0", "v1.1.0")
	wantSHA := revParse(t, dir, "v1.1.0")
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: dir, Ref: "v1.1.0"},
	}}

	// Act
	resolved, _, err := ResolvePackages(cfg, Options{MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if resolved[0].Ref != "v1.1.0" || resolved[0].SHA != wantSHA {
		t.Errorf("Ref/SHA = %q/%q, want v1.1.0/%q", resolved[0].Ref, resolved[0].SHA, wantSHA)
	}
}

func TestResolvePackages_ExplicitRef_BranchName_HeadNotAllowed(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")
	gitCmd(t, dir, "branch", "feature-x")
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: dir, Ref: "feature-x"},
	}}

	// Act
	_, _, err := ResolvePackages(cfg, Options{MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	var headErr *HeadNotAllowedError
	if !errors.As(err, &headErr) {
		t.Fatalf("error = %v, want a *HeadNotAllowedError", err)
	}
	if headErr.Ref != "feature-x" {
		t.Errorf("HeadNotAllowedError.Ref = %q, want feature-x", headErr.Ref)
	}
	if strings.Contains(err.Error(), "--allow-head") {
		t.Errorf("error message must not mention the nonexistent --allow-head flag: %v", err)
	}
}

func TestResolvePackages_ExplicitRef_FullyQualifiedBranchRef_HeadNotAllowed(t *testing.T) {
	// Arrange: a curator pins the fully-qualified spelling directly.
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")
	gitCmd(t, dir, "branch", "feature-y")
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: dir, Ref: "refs/heads/feature-y"},
	}}

	// Act
	_, _, err := ResolvePackages(cfg, Options{MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	var headErr *HeadNotAllowedError
	if !errors.As(err, &headErr) {
		t.Fatalf("error = %v, want a *HeadNotAllowedError", err)
	}
}

func TestResolvePackages_ExplicitRef_HEADLiteral_CaseInsensitive_HeadNotAllowed(t *testing.T) {
	for _, refText := range []string{"HEAD", "head", "Head"} {
		t.Run(refText, func(t *testing.T) {
			// Arrange
			dir := t.TempDir()
			initGitRepoWithTags(t, dir, "v1.0.0")
			cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
				{Name: "tool", Source: dir, Ref: refText},
			}}

			// Act
			_, _, err := ResolvePackages(cfg, Options{MetadataFetcher: noopMetadataFetcher{}})

			// Assert
			var headErr *HeadNotAllowedError
			if !errors.As(err, &headErr) {
				t.Fatalf("ref %q: error = %v, want a *HeadNotAllowedError", refText, err)
			}
		})
	}
}

func TestResolvePackages_ExplicitRef_SameNameTagTakesPriorityOverBranch(t *testing.T) {
	// Arrange: a tag and a branch can share the same short name (different
	// ref namespaces) -- the tag must win, not error out (design.md: "同名
	// tag 優先於 branch(tag 先比對,同名時走 tag 不報錯)").
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "shared")
	gitCmd(t, dir, "branch", "shared")
	tagSHA := revParse(t, dir, "refs/tags/shared")
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: dir, Ref: "shared"},
	}}

	// Act
	resolved, _, err := ResolvePackages(cfg, Options{MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v, want the same-named tag to win instead of erroring", err)
	}
	if resolved[0].Ref != "shared" || resolved[0].SHA != tagSHA {
		t.Errorf("Ref/SHA = %q/%q, want shared/%q", resolved[0].Ref, resolved[0].SHA, tagSHA)
	}
}

func TestResolvePackages_ExplicitRef_NotFound(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: dir, Ref: "v9.9.9"},
	}}

	// Act
	_, _, err := ResolvePackages(cfg, Options{MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	var notFound *RefNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("error = %v, want a *RefNotFoundError", err)
	}
}

// ── --offline: no cache layer, fail loud instead of silently degrading ──

func TestResolvePackages_Offline_RemotePackageWithRef_ReturnsErrorWithoutNetwork(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: "owner/repo", Ref: "v1.0.0"},
	}}

	// Act
	_, _, err := ResolvePackages(cfg, Options{Offline: true, Lister: panicLister{}, MetadataFetcher: panicMetadataFetcher{}})

	// Assert
	if err == nil {
		t.Fatal("expected an error: --offline has no cached refs to resolve against")
	}
	if !strings.Contains(err.Error(), "--offline") {
		t.Errorf("error = %v, want it to mention --offline", err)
	}
}

func TestResolvePackages_Offline_RemotePackageWithVersion_ReturnsErrorWithoutNetwork(t *testing.T) {
	// Arrange
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: "owner/repo", Version: "^1.0.0"},
	}}

	// Act
	_, _, err := ResolvePackages(cfg, Options{Offline: true, Lister: panicLister{}, MetadataFetcher: panicMetadataFetcher{}})

	// Assert
	if err == nil {
		t.Fatal("expected an error: --offline has no cached refs to resolve against")
	}
}

func TestResolvePackages_Offline_LocalPackage_StillResolvesWithoutNetwork(t *testing.T) {
	// Arrange: --offline must not affect local packages at all -- they never
	// touch the network regardless (mkt-051).
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "local-a", Source: "./pkgs/a", Version: "^1.0.0"},
	}}

	// Act
	resolved, _, err := ResolvePackages(cfg, Options{Offline: true, Lister: panicLister{}, MetadataFetcher: panicMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if len(resolved) != 1 || !resolved[0].IsLocal {
		t.Fatalf("resolved = %+v, want a single local package", resolved)
	}
}

// ── version range: real git repo ─────────────────────────────────────────

func TestResolvePackages_VersionRange_PicksHighestSatisfyingTag(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0", "v1.1.0", "v2.0.0-rc.1")
	wantSHA := revParse(t, dir, "v1.1.0")
	cfg := &authoring.AuthoringConfig{
		Build: authoring.Build{TagPattern: "v{version}"},
		Packages: []authoring.PackageEntry{
			{Name: "tool", Source: dir, Version: "^1.0.0"},
		},
	}

	// Act
	resolved, _, err := ResolvePackages(cfg, Options{MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if resolved[0].Ref != "v1.1.0" || resolved[0].SHA != wantSHA {
		t.Errorf("Ref/SHA = %q/%q, want v1.1.0/%q", resolved[0].Ref, resolved[0].SHA, wantSHA)
	}
}

func TestResolvePackages_VersionRange_NoMatch_ReturnsNoMatchingVersionError(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: dir, Version: "^2.0.0"},
	}}

	// Act
	_, _, err := ResolvePackages(cfg, Options{MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	var noMatch *NoMatchingVersionError
	if !errors.As(err, &noMatch) {
		t.Fatalf("error = %v, want a *NoMatchingVersionError", err)
	}
}

func TestResolvePackages_VersionRange_IgnoresBranchHeads(t *testing.T) {
	// Arrange: a branch literally named like a matching tag must never be
	// considered a version candidate (mkt-051's version-range resolution
	// only ever looks at refs/tags/*).
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")
	gitCmd(t, dir, "branch", "v2.0.0")
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: dir, Version: "^1.0.0"},
	}}

	// Act
	resolved, _, err := ResolvePackages(cfg, Options{MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if resolved[0].Ref != "v1.0.0" {
		t.Errorf("Ref = %q, want v1.0.0 (a branch head must never be a version candidate)", resolved[0].Ref)
	}
}

func TestResolvePackages_PerPackageTagPatternOverridesBuildDefault(t *testing.T) {
	// Arrange: a monorepo-style tag scheme ("tool-a-v1.5.0") that would
	// never match the plain "v{version}" build default.
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "tool-a-v1.0.0", "tool-a-v1.5.0")
	wantSHA := revParse(t, dir, "tool-a-v1.5.0")
	cfg := &authoring.AuthoringConfig{
		Build: authoring.Build{TagPattern: "v{version}"},
		Packages: []authoring.PackageEntry{
			{Name: "tool-a", Source: dir, Version: "^1.0.0", TagPattern: "{name}-v{version}"},
		},
	}

	// Act
	resolved, _, err := ResolvePackages(cfg, Options{MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if resolved[0].Ref != "tool-a-v1.5.0" || resolved[0].SHA != wantSHA {
		t.Errorf("Ref/SHA = %q/%q, want tool-a-v1.5.0/%q", resolved[0].Ref, resolved[0].SHA, wantSHA)
	}
}

// ── --include-prerelease ──────────────────────────────────────────────────

func TestResolvePackages_IncludePrerelease_GlobalOption(t *testing.T) {
	// Arrange: "^2.0.0-0" is a range that (per npm semver's same-tuple
	// prerelease opt-in rule) DOES match "2.0.0-rc.1" once it is even
	// considered as a candidate -- so this isolates ResolvePackages' own
	// pre-filter (entry.IncludePrerelease || opts.IncludePrerelease) from
	// internal/semver's own range-matching semantics.
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0", "v2.0.0-rc.1")
	prereleaseSHA := revParse(t, dir, "v2.0.0-rc.1")
	newCfg := func() *authoring.AuthoringConfig {
		return &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
			{Name: "tool", Source: dir, Version: "^2.0.0-0"},
		}}
	}

	t.Run("excluded by default", func(t *testing.T) {
		_, _, err := ResolvePackages(newCfg(), Options{MetadataFetcher: noopMetadataFetcher{}})
		var noMatch *NoMatchingVersionError
		if !errors.As(err, &noMatch) {
			t.Fatalf("error = %v, want *NoMatchingVersionError (prerelease invisible by default)", err)
		}
	})

	t.Run("included with --include-prerelease", func(t *testing.T) {
		resolved, _, err := ResolvePackages(newCfg(), Options{IncludePrerelease: true, MetadataFetcher: noopMetadataFetcher{}})
		if err != nil {
			t.Fatalf("ResolvePackages() error = %v", err)
		}
		if resolved[0].Ref != "v2.0.0-rc.1" || resolved[0].SHA != prereleaseSHA {
			t.Errorf("Ref/SHA = %q/%q, want v2.0.0-rc.1/%q", resolved[0].Ref, resolved[0].SHA, prereleaseSHA)
		}
	})
}

func TestResolvePackages_IncludePrerelease_PerPackageOverridesGlobalFalse(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v2.0.0-rc.1")
	wantSHA := revParse(t, dir, "v2.0.0-rc.1")
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: dir, Version: "^2.0.0-0", IncludePrerelease: true},
	}}

	// Act
	resolved, _, err := ResolvePackages(cfg, Options{IncludePrerelease: false, MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if resolved[0].Ref != "v2.0.0-rc.1" || resolved[0].SHA != wantSHA {
		t.Errorf("Ref/SHA = %q/%q, want v2.0.0-rc.1/%q", resolved[0].Ref, resolved[0].SHA, wantSHA)
	}
}

// ── Host / SourceRepo derivation ──────────────────────────────────────────

func TestSplitHostFromSource(t *testing.T) {
	tests := []struct {
		name          string
		source        string
		wantHost      string
		wantOwnerRepo string
	}{
		{"bare owner/repo, no host", "owner/repo", "", "owner/repo"},
		{"host-prefixed shorthand", "git.example.com/owner/repo", "git.example.com", "owner/repo"},
		{"full https URL", "https://git.example.com/owner/repo", "git.example.com", "owner/repo"},
		{"full https URL with .git suffix", "https://git.example.com/owner/repo.git", "git.example.com", "owner/repo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			host, ownerRepo := splitHostFromSource(tt.source)

			// Assert
			if host != tt.wantHost || ownerRepo != tt.wantOwnerRepo {
				t.Errorf("splitHostFromSource(%q) = (%q, %q), want (%q, %q)", tt.source, host, ownerRepo, tt.wantHost, tt.wantOwnerRepo)
			}
		})
	}
}

func TestResolvePackages_HostPrefixedSource_PopulatesHostAndSourceRepo(t *testing.T) {
	// Arrange
	sha := strings.Repeat("b", 40)
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: "git.example.com/owner/repo", Ref: sha},
	}}

	// Act
	resolved, _, err := ResolvePackages(cfg, Options{Lister: panicLister{}, MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if resolved[0].Host != "git.example.com" || resolved[0].SourceRepo != "owner/repo" {
		t.Errorf("Host/SourceRepo = %q/%q, want git.example.com/owner/repo", resolved[0].Host, resolved[0].SourceRepo)
	}
}

func TestResolvePackages_DefaultHostSource_HostFieldEmpty(t *testing.T) {
	// Arrange
	sha := strings.Repeat("c", 40)
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: "owner/repo", Ref: sha},
	}}

	// Act
	resolved, _, err := ResolvePackages(cfg, Options{Lister: panicLister{}, MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if resolved[0].Host != "" {
		t.Errorf("Host = %q, want empty for the default host", resolved[0].Host)
	}
	if resolved[0].SourceRepo != "owner/repo" {
		t.Errorf("SourceRepo = %q, want owner/repo", resolved[0].SourceRepo)
	}
}

// F2 fix: an EXPLICIT "github.com/..." or "https://github.com/..." source
// names the default host just as much as a bare "owner/repo" shorthand
// does, and must converge to Host="" the same way -- otherwise the mapper
// sees a non-empty Host and wrongly emits a url-shaped source instead of
// the github shorthand (design.md/builder.go's own field-contract comment:
// "Host... "" when Entry.Source names the default host, github.com").

func TestResolvePackages_ExplicitGitHubComShorthand_HostFieldConverges(t *testing.T) {
	// Arrange
	sha := strings.Repeat("d", 40)
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: "github.com/acme/tool", Ref: sha},
	}}

	// Act
	resolved, _, err := ResolvePackages(cfg, Options{Lister: panicLister{}, MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if resolved[0].Host != "" {
		t.Errorf("Host = %q, want empty (explicit github.com is still the default host)", resolved[0].Host)
	}
	if resolved[0].SourceRepo != "acme/tool" {
		t.Errorf("SourceRepo = %q, want acme/tool", resolved[0].SourceRepo)
	}
}

func TestResolvePackages_ExplicitGitHubComFullURL_HostFieldConverges(t *testing.T) {
	// Arrange
	sha := strings.Repeat("e", 40)
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: "https://github.com/acme/tool", Ref: sha},
	}}

	// Act
	resolved, _, err := ResolvePackages(cfg, Options{Lister: panicLister{}, MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if resolved[0].Host != "" {
		t.Errorf("Host = %q, want empty (explicit https://github.com is still the default host)", resolved[0].Host)
	}
	if resolved[0].SourceRepo != "acme/tool" {
		t.Errorf("SourceRepo = %q, want acme/tool", resolved[0].SourceRepo)
	}
}

func TestResolvePackages_NonDefaultHostSource_HostFieldPreserved(t *testing.T) {
	// Arrange: only the default host (github.com) converges -- a genuine
	// non-default host must still be preserved verbatim.
	sha := strings.Repeat("2", 40)
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: "gitlab.example.com/acme/tool", Ref: sha},
	}}

	// Act
	resolved, _, err := ResolvePackages(cfg, Options{Lister: panicLister{}, MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if resolved[0].Host != "gitlab.example.com" {
		t.Errorf("Host = %q, want gitlab.example.com (non-default host must not converge)", resolved[0].Host)
	}
}

func TestResolvePackages_ExplicitGitHubComSource_ClaudeOutputIsGithubShorthand(t *testing.T) {
	// Arrange: end-to-end -- the converged Host must make ClaudeMapper emit
	// the github shorthand shape, not a url-shaped source (Claude only, the
	// Codex mapper never has a github-shorthand form at all).
	sha := strings.Repeat("f", 40)
	cfg := &authoring.AuthoringConfig{Name: "m", Packages: []authoring.PackageEntry{
		{Name: "tool", Source: "github.com/acme/tool", Ref: sha},
	}}
	resolved, _, err := ResolvePackages(cfg, Options{Lister: panicLister{}, MetadataFetcher: noopMetadataFetcher{}})
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	src, ok := doc.Plugins[0].Source.(*RemoteSource)
	if !ok {
		t.Fatalf("Source = %#v, want *RemoteSource", doc.Plugins[0].Source)
	}
	if src.Source != "github" || src.Repo != "acme/tool" {
		t.Errorf("source/repo = %q/%q, want github/acme/tool", src.Source, src.Repo)
	}
	if src.URL != "" {
		t.Errorf("url = %q, want empty for github shorthand", src.URL)
	}
}

func TestResolvePackages_ExplicitGitHubComSource_CodexOutputIsBareOwnerRepo(t *testing.T) {
	// Arrange: Codex has no github shorthand form at all -- the converged
	// Host must still produce a bare "owner/repo" url (not a github.com
	// URL), mirroring the Python original's bare owner/repo Codex output.
	sha := strings.Repeat("1", 40)
	cfg := &authoring.AuthoringConfig{Name: "m", Packages: []authoring.PackageEntry{
		{Name: "tool", Source: "github.com/acme/tool", Ref: sha, Category: "utilities"},
	}}
	resolved, _, err := ResolvePackages(cfg, Options{Lister: panicLister{}, MetadataFetcher: noopMetadataFetcher{}})
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}

	// Act
	doc, _, err := CodexMapper{}.Compose(cfg, resolved)

	// Assert
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	src, ok := doc.Plugins[0].Source.(*RemoteSource)
	if !ok {
		t.Fatalf("Source = %#v, want *RemoteSource", doc.Plugins[0].Source)
	}
	if src.Source != "url" || src.URL != "acme/tool" {
		t.Errorf("source/url = %q/%q, want url/acme/tool (bare, no github.com prefix)", src.Source, src.URL)
	}
}

// ── aggregation: multiple packages ────────────────────────────────────────

func TestResolvePackages_AggregatesMultiplePackages(t *testing.T) {
	// Arrange
	localCfg := authoring.PackageEntry{Name: "local", Source: "./pkgs/a"}
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")
	wantSHA := revParse(t, dir, "v1.0.0")
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		localCfg,
		{Name: "remote", Source: dir, Ref: "v1.0.0"},
	}}

	// Act
	resolved, _, err := ResolvePackages(cfg, Options{MetadataFetcher: noopMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("len(resolved) = %d, want 2", len(resolved))
	}
	if !resolved[0].IsLocal {
		t.Error("resolved[0].IsLocal = false, want true")
	}
	if resolved[1].IsLocal {
		t.Error("resolved[1].IsLocal = true, want false")
	}
	if resolved[1].SHA != wantSHA {
		t.Errorf("resolved[1].SHA = %q, want %q", resolved[1].SHA, wantSHA)
	}
}
