package authoring

import (
	"testing"

	"github.com/apm-go/apm/internal/semver"
)

// SPEC marketplace-check-outdated-fixes §C (SC-F6 range-entry-keeps-map,
// regression): a version-range entry keeps the published version from the
// current map even when the remote has no tags (outdated.py:92-103).

func TestOutdatedPackages_ShaPinWithVersion_NoCandidates_CurrentDashes_RangeEntryKeepsMap(t *testing.T) {
	lister := mapRefLister{refs: []semver.TagInfo{headRef(shaA)}}
	cfg := &AuthoringConfig{Packages: []PackageEntry{{Name: "tool", Source: "owner/repo", Version: "^1.0.0"}}}

	rows := OutdatedPackages(cfg, lister, false, false, map[string]string{"tool": "v0.9.0"})

	if len(rows) != 1 || rows[0].Current != "v0.9.0" {
		t.Fatalf("rows = %+v, want Current v0.9.0 from the current map", rows)
	}
}
