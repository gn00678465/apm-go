package authoring

import (
	"errors"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/semver"
)

// SPEC marketplace-check-outdated §C: `outdated` on SHA pins (SC-C1..C6,
// C10, C11), named-ref pins still skipped (SC-C7), tag-pattern inference
// (SC-C8) and the Oracle's Note wording (SC-C9).

var (
	shaA = strings.Repeat("a", 40)
	shaB = strings.Repeat("b", 40)
	shaC = strings.Repeat("c", 40)
)

func tagRef(name, sha string) semver.TagInfo {
	return semver.TagInfo{Name: name, Commit: sha, Ref: "refs/tags/" + name}
}

func headRef(sha string) semver.TagInfo {
	return semver.TagInfo{Name: "HEAD", Commit: sha, Ref: "HEAD"}
}

func outdatedRow(t *testing.T, cfg *AuthoringConfig, pkg PackageEntry, lister RefLister, offline bool) OutdatedRow {
	t.Helper()
	if cfg == nil {
		cfg = &AuthoringConfig{}
	}
	cfg.Packages = []PackageEntry{pkg}
	rows := OutdatedPackages(cfg, lister, offline, false, nil)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	return rows[0]
}

func assertRow(t *testing.T, r OutdatedRow, current, latestInRange, latestOverall, status, note string, upgradable bool) {
	t.Helper()
	if r.Current != current || r.LatestInRange != latestInRange || r.LatestOverall != latestOverall || r.Status != status || r.Note != note || r.Upgradable != upgradable {
		t.Errorf("row = {Current:%q LatestInRange:%q LatestOverall:%q Status:%q Note:%q Upgradable:%v}\n want {Current:%q LatestInRange:%q LatestOverall:%q Status:%q Note:%q Upgradable:%v}",
			r.Current, r.LatestInRange, r.LatestOverall, r.Status, r.Note, r.Upgradable,
			current, latestInRange, latestOverall, status, note, upgradable)
	}
}

// ── SC-C1 ────────────────────────────────────────────────────────────────

func TestOutdatedPackages_ShaPinWithVersion_NewerTag_Upgradable(t *testing.T) {
	lister := mapRefLister{refs: []semver.TagInfo{headRef(shaB), tagRef("v1.0.0", shaA), tagRef("v1.1.0", shaB)}}

	r := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA, Version: "1.0.0"}, lister, false)

	assertRow(t, r, "v1.0.0", "v1.0.0", "v1.1.0", "[!]", "", true)

	t.Run("NamePattern", func(t *testing.T) {
		lister := mapRefLister{refs: []semver.TagInfo{headRef(shaB), tagRef("tool_v1.0.0", shaA), tagRef("tool_v1.1.0", shaB)}}
		r := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA, Version: "1.0.0", TagPattern: "{name}_v{version}"}, lister, false)
		assertRow(t, r, "tool_v1.0.0", "tool_v1.0.0", "tool_v1.1.0", "[!]", "", true)
	})
}

// SC-C1 design text: LatestInRange is the declared version's own tag only
// when the remote still has it; a retagged remote leaves it "--" while the
// upgrade verdict still comes from the highest tag.
func TestOutdatedPackages_ShaPinWithVersion_DeclaredTagMissing(t *testing.T) {
	lister := mapRefLister{refs: []semver.TagInfo{headRef(shaC), tagRef("v1.1.0", shaB), tagRef("v1.2.0", shaC)}}

	r := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA, Version: "1.0.0"}, lister, false)

	assertRow(t, r, "v1.0.0", "--", "v1.2.0", "[!]", "", true)
}

// ── SC-C2 ────────────────────────────────────────────────────────────────

func TestOutdatedPackages_ShaPinWithVersion_UpToDate(t *testing.T) {
	lister := mapRefLister{refs: []semver.TagInfo{headRef(shaA), tagRef("v1.0.0", shaA)}}

	r := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA, Version: "1.0.0"}, lister, false)

	assertRow(t, r, "v1.0.0", "v1.0.0", "v1.0.0", "[+]", "", false)
}

// ── SC-C3 ────────────────────────────────────────────────────────────────

func TestOutdatedPackages_ShaPinWithVersion_NoMatchingTags(t *testing.T) {
	lister := mapRefLister{refs: []semver.TagInfo{headRef(shaA), {Name: "main", Commit: shaA, Ref: "refs/heads/main"}}}

	r := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA, Version: "1.0.0"}, lister, false)

	if r.Status != "[!]" || r.Note != "No matching tags found" || r.Upgradable {
		t.Errorf("row = %+v, want [!] \"No matching tags found\" not upgradable", r)
	}
}

// ── SC-C4 / SC-C5 / SC-C6 ────────────────────────────────────────────────

func TestOutdatedPackages_ShaPinWithoutVersion_TipMoved_Upgradable(t *testing.T) {
	lister := mapRefLister{refs: []semver.TagInfo{headRef(shaB), {Name: "main", Commit: shaB, Ref: "refs/heads/main"}}}

	r := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA}, lister, false)

	assertRow(t, r, shaA[:12], "--", shaB[:12], "[!]", "Default branch tip moved", true)
}

func TestOutdatedPackages_ShaPinWithoutVersion_AtTip_UpToDate(t *testing.T) {
	lister := mapRefLister{refs: []semver.TagInfo{headRef(shaA)}}

	r := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA}, lister, false)

	assertRow(t, r, shaA[:12], "--", shaA[:12], "[+]", "", false)
}

func TestOutdatedPackages_ShaPinWithoutVersion_NoHeadEntry_IconX(t *testing.T) {
	lister := mapRefLister{refs: []semver.TagInfo{tagRef("v1.0.0", shaC)}}

	r := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA}, lister, false)

	if r.Status != "[x]" || r.Note != "Remote advertised no HEAD" || r.Upgradable {
		t.Errorf("row = %+v, want [x] \"Remote advertised no HEAD\"", r)
	}
}

// ── SC-C7 ────────────────────────────────────────────────────────────────

func TestOutdatedPackages_NamedRefPin_StillSkipped(t *testing.T) {
	for _, ref := range []string{"v1.0.0", "main", strings.ToUpper(shaA)} {
		r := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: ref}, panicLister{}, false)
		if r.Status != "[i]" || r.Note != "Pinned to ref; skipped" || r.Upgradable {
			t.Errorf("ref %q: row = %+v, want [i] \"Pinned to ref; skipped\"", ref, r)
		}
	}
}

// ── SC-C8 ────────────────────────────────────────────────────────────────

func TestOutdatedPackages_VersionRange_TagPatternFallback(t *testing.T) {
	lister := mapRefLister{refs: []semver.TagInfo{
		headRef(shaB),
		tagRef("tool_v1.2.0", shaA),
		{Name: "v9.9.9", Commit: shaB, Ref: "refs/heads/v9.9.9"},
	}}

	r := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Version: "^1.0.0"}, lister, false)

	if r.LatestInRange != "tool_v1.2.0" || r.LatestOverall != "tool_v1.2.0" {
		t.Errorf("row = %+v, want the inferred {name}_v{version} layout to find tool_v1.2.0 and ignore branch v9.9.9", r)
	}
}

// ── SC-C9 wording ────────────────────────────────────────────────────────

func TestOutdatedPackages_Wording_MatchesOracle(t *testing.T) {
	noRange := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Version: ""}, panicLister{}, false)
	if noRange.Note != "No version range" {
		t.Errorf("Note = %q, want \"No version range\"", noRange.Note)
	}

	failing := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Version: "^1.0.0"},
		mapRefLister{err: errors.New(strings.Repeat("x", 70))}, false)
	if failing.Status != "[x]" || failing.Note != strings.Repeat("x", 60) {
		t.Errorf("ls-remote failure Note = %q (status %s), want str(exc)[:60]", failing.Note, failing.Status)
	}
}

// ── SC-C10 ───────────────────────────────────────────────────────────────

func TestOutdatedPackages_ShaPin_Offline_IconX(t *testing.T) {
	r := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA}, panicLister{}, true)

	full := "Offline mode: no cached refs for 'owner/repo' (package 'tool'). Run a build online first."
	if r.Status != "[x]" || r.Note != full[:60] || r.Upgradable {
		t.Errorf("row = %+v, want [x] with the Oracle's OfflineMissError text truncated to 60", r)
	}
}

// ── SC-C11 ───────────────────────────────────────────────────────────────

func TestOutdatedPackages_ShaPinWithRangeVersion_Skipped(t *testing.T) {
	r := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA, Version: "^1.0.0"}, panicLister{}, false)

	if r.Status != "[i]" || r.Note != "Pinned to ref; skipped" || r.Upgradable {
		t.Errorf("row = %+v, want [i] \"Pinned to ref; skipped\" (decision D-b)", r)
	}
}
