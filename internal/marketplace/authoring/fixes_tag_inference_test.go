package authoring

import (
	"testing"

	"github.com/apm-go/apm/internal/semver"
)

// SPEC marketplace-check-outdated-fixes §E: tag inference and candidate
// collection use the Oracle's x.y.z grammar (SC-F12, SC-F13).

func TestCheckPackages_TagInference_RejectsShortVersions(t *testing.T) {
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "1", "tool_v1.2.0")

	// ^1.1.0, not ^1.0.0: NPM reads the tag "1" as 1.0.0, which would
	// satisfy ^1.0.0 and hide the wrong layout choice.
	wantPass(t, singleResult(t, dir, PackageEntry{Name: "tool", Source: dir, Version: "^1.1.0"}, noProbeDeps()))

	refs, err := gitRefLister{}.ListRefs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, used := versionTagCandidatesWithPattern(refs, "", "tool", false); used != "{name}_v{version}" {
		t.Errorf("inferred pattern = %q, want {name}_v{version}", used)
	}
}

func TestVersionTagCandidates_VPrefixedCapture(t *testing.T) {
	t.Run("CaptureRejected", func(t *testing.T) {
		refs := []semver.TagInfo{tagRef("1.0.0", shaA), tagRef("v1.2.0", shaB)}
		got, used := versionTagCandidatesWithPattern(refs, "{version}", "tool", false)
		if used != "{version}" || len(got) != 1 || got[0].tag != "1.0.0" {
			t.Errorf("candidates = %+v via %q, want only 1.0.0 via {version}", got, used)
		}
	})
	// The configured pattern matches nothing here, so the Oracle infers a
	// layout (commands/marketplace/__init__.py:1178-1190 at b75a02b1);
	// infer_tag_pattern("v1.2.0") returns "v{version}" against its code.
	t.Run("FallbackInfers", func(t *testing.T) {
		refs := []semver.TagInfo{tagRef("v1.2.0", shaA)}
		got, used := versionTagCandidatesWithPattern(refs, "{version}", "tool", false)
		if used != "v{version}" || len(got) != 1 || got[0].tag != "v1.2.0" || got[0].version != "1.2.0" {
			t.Errorf("candidates = %+v via %q, want v1.2.0 (version 1.2.0) via v{version}", got, used)
		}
	})
}
