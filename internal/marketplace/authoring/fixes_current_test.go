package authoring

import (
	"errors"
	"testing"

	"github.com/apm-go/apm/internal/semver"
)

// SPEC marketplace-check-outdated-fixes §C: a SHA pin's Current comes only
// from the comparison with the remote (SC-F6, SC-F17, decisions D-f and
// D-2), and a display version with one leading "v" renders like the bare
// version (SC-F8, decision D-3).

func outdatedRowWithCurrent(t *testing.T, pkg PackageEntry, lister RefLister, offline bool, current map[string]string) OutdatedRow {
	t.Helper()
	cfg := &AuthoringConfig{Packages: []PackageEntry{pkg}}
	rows := OutdatedPackages(cfg, lister, offline, false, current)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	return rows[0]
}

func TestOutdatedPackages_ShaPinWithVersion_NoCandidates_CurrentDashes(t *testing.T) {
	pkg := PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA, Version: "1.0.0"}
	current := map[string]string{"tool": shaA}

	t.Run("NoTags", func(t *testing.T) {
		lister := mapRefLister{refs: []semver.TagInfo{headRef(shaA)}}
		r := outdatedRowWithCurrent(t, pkg, lister, false, current)
		if r.Current != "--" || r.Note != "No matching tags found" {
			t.Errorf("row = %+v, want Current -- and Note \"No matching tags found\"", r)
		}
	})
	t.Run("Offline", func(t *testing.T) {
		r := outdatedRowWithCurrent(t, pkg, panicLister{}, true, current)
		if r.Current != "--" || r.Status != "[x]" {
			t.Errorf("row = %+v, want Current -- and [x]", r)
		}
	})
	t.Run("ListRefsError", func(t *testing.T) {
		r := outdatedRowWithCurrent(t, pkg, mapRefLister{err: errors.New("unreachable")}, false, current)
		if r.Current != "--" || r.Status != "[x]" {
			t.Errorf("row = %+v, want Current -- and [x]", r)
		}
	})
}

func TestOutdatedPackages_ShaPinWithoutVersion_ErrorRows_CurrentDashes(t *testing.T) {
	pkg := PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA}
	current := map[string]string{"tool": shaA}

	t.Run("Offline", func(t *testing.T) {
		r := outdatedRowWithCurrent(t, pkg, panicLister{}, true, current)
		if r.Current != "--" || r.Status != "[x]" {
			t.Errorf("row = %+v, want Current -- and [x]", r)
		}
	})
	t.Run("ListRefsError", func(t *testing.T) {
		r := outdatedRowWithCurrent(t, pkg, mapRefLister{err: errors.New("unreachable")}, false, current)
		if r.Current != "--" || r.Status != "[x]" {
			t.Errorf("row = %+v, want Current -- and [x]", r)
		}
	})
	t.Run("NoHEAD", func(t *testing.T) {
		lister := mapRefLister{refs: []semver.TagInfo{tagRef("v1.0.0", shaC)}}
		r := outdatedRowWithCurrent(t, pkg, lister, false, current)
		if r.Current != "--" || r.Note != "Remote advertised no HEAD" {
			t.Errorf("row = %+v, want Current -- and Note \"Remote advertised no HEAD\"", r)
		}
	})
}

func TestOutdatedPackages_ShaPinWithVersion_LeadingV_SameAsBare(t *testing.T) {
	t.Run("NewerTag", func(t *testing.T) {
		lister := mapRefLister{refs: []semver.TagInfo{headRef(shaB), tagRef("v1.0.0", shaA), tagRef("v1.1.0", shaB)}}
		r := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA, Version: "v1.0.0"}, lister, false)
		assertRow(t, r, "v1.0.0", "v1.0.0", "v1.1.0", "[!]", "", true)
	})
	t.Run("UpToDate", func(t *testing.T) {
		lister := mapRefLister{refs: []semver.TagInfo{headRef(shaA), tagRef("v1.0.0", shaA)}}
		r := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA, Version: "v1.0.0"}, lister, false)
		assertRow(t, r, "v1.0.0", "v1.0.0", "v1.0.0", "[+]", "", false)
	})
	t.Run("NamePattern", func(t *testing.T) {
		lister := mapRefLister{refs: []semver.TagInfo{headRef(shaB), tagRef("tool_v1.0.0", shaA), tagRef("tool_v1.1.0", shaB)}}
		r := outdatedRow(t, nil, PackageEntry{Name: "tool", Source: "owner/repo", Ref: shaA, Version: "v1.0.0", TagPattern: "{name}_v{version}"}, lister, false)
		assertRow(t, r, "tool_v1.0.0", "tool_v1.0.0", "tool_v1.1.0", "[!]", "", true)
	})
}
