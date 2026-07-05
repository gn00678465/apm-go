package marketplace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/semver"
)

// panicTagLister is a TagLister fake that panics if ever called -- proving
// a code path takes zero ListTags/network action, mirroring the project's
// existing panicLister convention (internal/marketplace/authoring/
// refcheck_test.go).
type panicTagLister struct{}

func (panicTagLister) ListTags(repoURL string) ([]semver.TagInfo, error) {
	panic("ListTags must not be called: this path must not resolve tags")
}

// mapTagLister is a TagLister fake returning canned tags per repo
// coordinate, or a fixed error when err is set.
type mapTagLister struct {
	tags map[string][]semver.TagInfo
	err  error
}

func (m *mapTagLister) ListTags(repoURL string) ([]semver.TagInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.tags[repoURL], nil
}

// TestApplyVersionSpec_NonRangeAppliedDirectly covers mkt-021/033's raw-ref
// branch: a versionSpec that is not a semver range (per req-rs-003, this
// includes a bare "1.2.3" -- a bare version is a literal tag, not a range)
// replaces canonical's "#ref" fragment directly, with zero tag lookup --
// panicTagLister proves that.
func TestApplyVersionSpec_NonRangeAppliedDirectly(t *testing.T) {
	tests := []struct {
		name        string
		canonical   string
		versionSpec string
		want        string
	}{
		{"raw tag with v prefix", "owner/repo", "v3.0.0", "owner/repo#v3.0.0"},
		{"branch name containing a slash", "owner/repo", "feature/x", "owner/repo#feature/x"},
		{"commit sha", "owner/repo", "abc123def456", "owner/repo#abc123def456"},
		{"bare version is literal per req-rs-003, not a range", "owner/repo", "1.2.3", "owner/repo#1.2.3"},
		{
			"version_spec replaces an existing dict-source ref fragment",
			"owner/repo#v9.0", "v3.0.0", "owner/repo#v3.0.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			got, err := applyVersionSpec(tt.canonical, tt.versionSpec, panicTagLister{})

			// Assert
			if err != nil {
				t.Fatalf("applyVersionSpec() returned unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("applyVersionSpec() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestApplyVersionSpec_RangeResolvesHighestMatchingTag covers mkt-033's
// range path: a semver range expression is resolved against tags.ListTags +
// semver.MaxSatisfying, picking the highest satisfying tag.
func TestApplyVersionSpec_RangeResolvesHighestMatchingTag(t *testing.T) {
	// Arrange
	tags := &mapTagLister{tags: map[string][]semver.TagInfo{
		"owner/repo": {
			{Name: "v1.0.0", Commit: "c1"},
			{Name: "v1.2.0", Commit: "c2"},
			{Name: "v2.0.0", Commit: "c3"},
		},
	}}

	// Act
	got, err := applyVersionSpec("owner/repo", "^1.0.0", tags)

	// Assert
	if err != nil {
		t.Fatalf("applyVersionSpec() returned unexpected error: %v", err)
	}
	want := "owner/repo#v1.2.0"
	if got != want {
		t.Errorf("applyVersionSpec() = %q, want %q (highest tag satisfying ^1.0.0, excluding v2.0.0)", got, want)
	}
}

// TestApplyVersionSpec_RangeStripsSubdirectoryBeforeListingTags covers
// repoCoordinate's role: canonical may carry an in-marketplace subdirectory
// segment, but the coordinate passed to TagLister.ListTags must be trimmed
// to just "owner/repo" -- a tag lookup is repo-scoped, not subdirectory-
// scoped. The resulting "#ref" fragment is still appended to the FULL
// (untrimmed) canonical.
func TestApplyVersionSpec_RangeStripsSubdirectoryBeforeListingTags(t *testing.T) {
	// Arrange -- keyed by the trimmed "owner/repo" coordinate only; a
	// lookup using the untrimmed "owner/repo/plugins/foo" would miss and
	// silently fall back, so a wrong implementation would still produce a
	// value (just the wrong one), not a crash -- assert on the exact tag.
	tags := &mapTagLister{tags: map[string][]semver.TagInfo{
		"owner/repo": {{Name: "v1.5.0", Commit: "c"}},
	}}

	// Act
	got, err := applyVersionSpec("owner/repo/plugins/foo", "^1.0.0", tags)

	// Assert
	if err != nil {
		t.Fatalf("applyVersionSpec() returned unexpected error: %v", err)
	}
	want := "owner/repo/plugins/foo#v1.5.0"
	if got != want {
		t.Errorf("applyVersionSpec() = %q, want %q", got, want)
	}
}

// TestApplyVersionSpec_RangeNoMatchFallsBackToRawRef covers
// design.md/implement.md step 4's "無相符且非嚴格 range → 回退 raw ref": when no
// tag satisfies the range, versionSpec itself is used as the raw ref rather
// than failing the resolution (the Go port does not reproduce the Python
// original's NoMatchingVersionError hard-failure for a genuine range with
// zero matches).
func TestApplyVersionSpec_RangeNoMatchFallsBackToRawRef(t *testing.T) {
	// Arrange
	tags := &mapTagLister{tags: map[string][]semver.TagInfo{
		"owner/repo": {{Name: "v1.0.0", Commit: "c"}},
	}}

	// Act
	got, err := applyVersionSpec("owner/repo", "^9.0.0", tags)

	// Assert
	if err != nil {
		t.Fatalf("applyVersionSpec() returned unexpected error: %v", err)
	}
	want := "owner/repo#^9.0.0"
	if got != want {
		t.Errorf("applyVersionSpec() = %q, want %q (fall back to the raw version_spec as a literal ref)", got, want)
	}
}

// TestApplyVersionSpec_ListTagsErrorPropagates covers a genuine transport
// failure (as opposed to "no matching tag"): this must surface as an error,
// not silently fall back.
func TestApplyVersionSpec_ListTagsErrorPropagates(t *testing.T) {
	// Arrange
	tags := &mapTagLister{err: errors.New("network down")}

	// Act
	_, err := applyVersionSpec("owner/repo", "^1.0.0", tags)

	// Assert
	if err == nil {
		t.Fatal("applyVersionSpec() returned nil error, want the ListTags failure to propagate")
	}
	if !strings.Contains(err.Error(), "network down") {
		t.Errorf("applyVersionSpec() error = %q, want it to contain %q", err.Error(), "network down")
	}
}

// TestRepoCoordinate covers the "owner/repo" trimming helper in isolation.
func TestRepoCoordinate(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"owner/repo", "owner/repo"},
		{"owner/repo/sub", "owner/repo"},
		{"owner/repo/sub/dir", "owner/repo"},
		{"single-segment", "single-segment"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := repoCoordinate(tt.in); got != tt.want {
				t.Errorf("repoCoordinate(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestApplyVersionSpec_RealGitRepoFixture proves genuine integration
// between gitops.RealTagLister (a real `git ls-remote` subprocess) and
// semver.MaxSatisfying against a real local git repository fixture, per
// implement.md step 4's "本地 git repo fixture 打 tag,range 解析到最高相符
// tag" test requirement (not a fake TagLister). "./repo" is a relative-path
// stand-in for a canonical coordinate here -- gitops.RealTagLister.
// resolveCloneURL passes a "./"-prefixed repoURL straight through as a
// local path, run relative to the test process's current working
// directory (pinned to parentDir by the Chdir below); real production
// canonicals are always remote "owner/repo[/path]" coordinates, covered
// with a fake TagLister by the tests above instead.
func TestApplyVersionSpec_RealGitRepoFixture(t *testing.T) {
	// Arrange
	parentDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd(): %v", err)
	}
	if err := os.Chdir(parentDir); err != nil {
		t.Fatalf("Chdir(%q): %v", parentDir, err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	repoDir := filepath.Join(parentDir, "repo")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatalf("Mkdir(%q): %v", repoDir, err)
	}
	initGitRemote(t, repoDir, "main", map[string]string{"file.txt": "hi"})
	for _, tag := range []string{"v1.0.0", "v1.2.0", "v1.5.0", "v2.0.0"} {
		gitCmd(t, repoDir, "tag", tag)
	}

	// Act
	got, err := applyVersionSpec("./repo", "^1.0.0", &gitops.RealTagLister{})

	// Assert
	if err != nil {
		t.Fatalf("applyVersionSpec() returned unexpected error: %v", err)
	}
	want := "./repo#v1.5.0"
	if got != want {
		t.Errorf("applyVersionSpec() = %q, want %q (highest real tag satisfying ^1.0.0, excluding v2.0.0)", got, want)
	}
}

// TestApplyVersionSpec_RealGitRepoFixture_NoMatchFallsBack covers the same
// real-git-fixture integration for the no-match fallback path (implement.md
// step 4's "無相符 tag 的回退行為").
func TestApplyVersionSpec_RealGitRepoFixture_NoMatchFallsBack(t *testing.T) {
	// Arrange
	parentDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd(): %v", err)
	}
	if err := os.Chdir(parentDir); err != nil {
		t.Fatalf("Chdir(%q): %v", parentDir, err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	repoDir := filepath.Join(parentDir, "repo")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatalf("Mkdir(%q): %v", repoDir, err)
	}
	initGitRemote(t, repoDir, "main", map[string]string{"file.txt": "hi"})
	gitCmd(t, repoDir, "tag", "v1.0.0")

	// Act
	got, err := applyVersionSpec("./repo", "^9.0.0", &gitops.RealTagLister{})

	// Assert
	if err != nil {
		t.Fatalf("applyVersionSpec() returned unexpected error: %v", err)
	}
	want := "./repo#^9.0.0"
	if got != want {
		t.Errorf("applyVersionSpec() = %q, want %q (no real tag satisfies ^9.0.0, fall back to raw ref)", got, want)
	}
}
