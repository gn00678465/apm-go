package resolver

import (
	"reflect"
	"sort"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
)

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestTransitiveOrphans_IncludesSeedRootsAndDescendants(t *testing.T) {
	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/a"},
			{RepoURL: "acme/b", ResolvedBy: "acme/a"},
		},
	}
	got := TransitiveOrphans(lock, map[string]bool{"acme/a": true})
	want := []string{"acme/a", "acme/b"}
	if !reflect.DeepEqual(sortedKeys(got), want) {
		t.Errorf("TransitiveOrphans = %v, want %v", sortedKeys(got), want)
	}
}

func TestTransitiveOrphans_NilLockfile_ReturnsSeedOnly(t *testing.T) {
	got := TransitiveOrphans(nil, map[string]bool{"acme/a": true})
	want := []string{"acme/a"}
	if !reflect.DeepEqual(sortedKeys(got), want) {
		t.Errorf("TransitiveOrphans = %v, want %v", sortedKeys(got), want)
	}
}

// TestActualOrphans_SimpleChain_ChildBecomesOrphan: A depends on B; B has no
// other parent. Uninstalling A must mark B as an actual orphan (un-040).
func TestActualOrphans_SimpleChain_ChildBecomesOrphan(t *testing.T) {
	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/a"},
			{RepoURL: "acme/b", ResolvedBy: "acme/a"},
		},
	}
	removed := map[string]bool{"acme/a": true}
	got := ActualOrphans(lock, removed, nil)
	want := []string{"acme/b"}
	if !reflect.DeepEqual(sortedKeys(got), want) {
		t.Errorf("ActualOrphans = %v, want %v", sortedKeys(got), want)
	}
}

// TestActualOrphans_ChildWithOtherParent_NotOrphan: B's parent is C (a
// surviving, unrelated package), not the removed A -- removing A must not
// touch B at all (un-041's "still needed" exclusion).
func TestActualOrphans_ChildWithOtherParent_NotOrphan(t *testing.T) {
	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/a"},
			{RepoURL: "acme/c"},
			{RepoURL: "acme/b", ResolvedBy: "acme/c"},
		},
	}
	removed := map[string]bool{"acme/a": true}
	got := ActualOrphans(lock, removed, nil)
	if len(got) != 0 {
		t.Errorf("ActualOrphans = %v, want empty (b belongs to c, unaffected by removing a)", sortedKeys(got))
	}
}

// TestActualOrphans_MultiLevelChain_AllDescendantsOrphaned: A -> B -> C.
// Uninstalling A must transitively orphan both B and C.
func TestActualOrphans_MultiLevelChain_AllDescendantsOrphaned(t *testing.T) {
	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/a"},
			{RepoURL: "acme/b", ResolvedBy: "acme/a"},
			{RepoURL: "acme/c", ResolvedBy: "acme/b"},
		},
	}
	removed := map[string]bool{"acme/a": true}
	got := ActualOrphans(lock, removed, nil)
	want := []string{"acme/b", "acme/c"}
	if !reflect.DeepEqual(sortedKeys(got), want) {
		t.Errorf("ActualOrphans = %v, want %v", sortedKeys(got), want)
	}
}

// TestActualOrphans_StillDeclaredInManifest_Kept: B is still directly
// declared in apm.yml after the removal (remainingRootKeys) -- must not be
// deleted even though its resolved_by traces back to the removed root.
func TestActualOrphans_StillDeclaredInManifest_Kept(t *testing.T) {
	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/a"},
			{RepoURL: "acme/b", ResolvedBy: "acme/a"},
		},
	}
	removed := map[string]bool{"acme/a": true}
	remaining := map[string]bool{"acme/b": true}
	got := ActualOrphans(lock, removed, remaining)
	if len(got) != 0 {
		t.Errorf("ActualOrphans = %v, want empty (b still directly declared)", sortedKeys(got))
	}
}

func TestActualOrphans_NoOrphans_ReturnsEmpty(t *testing.T) {
	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/a"},
		},
	}
	got := ActualOrphans(lock, map[string]bool{"acme/a": true}, nil)
	if len(got) != 0 {
		t.Errorf("ActualOrphans = %v, want empty", sortedKeys(got))
	}
}

func TestActualOrphans_NilLockfile_ReturnsEmpty(t *testing.T) {
	got := ActualOrphans(nil, map[string]bool{"acme/a": true}, nil)
	if len(got) != 0 {
		t.Errorf("ActualOrphans = %v, want empty", sortedKeys(got))
	}
}

// TestActualOrphans_MultiplePackagesRemovedTogether: uninstalling two
// packages in one call (A and D) where D's own child E has no other parent
// -- both B (child of A) and E (child of D) become orphans in a single call.
func TestActualOrphans_MultiplePackagesRemovedTogether(t *testing.T) {
	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/a"},
			{RepoURL: "acme/b", ResolvedBy: "acme/a"},
			{RepoURL: "acme/d"},
			{RepoURL: "acme/e", ResolvedBy: "acme/d"},
		},
	}
	removed := map[string]bool{"acme/a": true, "acme/d": true}
	got := ActualOrphans(lock, removed, nil)
	want := []string{"acme/b", "acme/e"}
	if !reflect.DeepEqual(sortedKeys(got), want) {
		t.Errorf("ActualOrphans = %v, want %v", sortedKeys(got), want)
	}
}
