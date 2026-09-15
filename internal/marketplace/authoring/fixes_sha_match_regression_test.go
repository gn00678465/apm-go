package authoring

import (
	"strings"
	"testing"
)

// SPEC marketplace-check-outdated-fixes §A (SC-F2, regression): refs that
// are not a lowercase 40-hex keep matching by name, with no probe.

func TestCheckPackages_NonShaRef_StillMatchesByName(t *testing.T) {
	repoWithBranch := func(t *testing.T, branch string) string {
		t.Helper()
		dir := t.TempDir()
		initGitRepoWithTags(t, dir, "v1.0.0")
		if branch != "" {
			gitCmd(t, dir, "branch", branch)
		}
		return dir
	}

	t.Run("DefaultBranch", func(t *testing.T) {
		dir := repoWithBranch(t, "")
		branch := gitCmd(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
		wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: branch}, noProbeDeps()))
	})
	t.Run("TagName", func(t *testing.T) {
		dir := repoWithBranch(t, "")
		wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: "v1.0.0"}, noProbeDeps()))
	})
	t.Run("FullTagRef", func(t *testing.T) {
		dir := repoWithBranch(t, "")
		wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: "refs/tags/v1.0.0"}, noProbeDeps()))
	})
	t.Run("UppercaseHexName", func(t *testing.T) {
		name := strings.Repeat("A", 40)
		dir := repoWithBranch(t, name)
		wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: name}, noProbeDeps()))
	})
	t.Run("AbbreviatedHexName", func(t *testing.T) {
		name := "abc1234"
		dir := repoWithBranch(t, name)
		wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Ref: name}, noProbeDeps()))
	})
}
