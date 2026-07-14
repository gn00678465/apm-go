package resolver

import "github.com/apm-go/apm/internal/lockfile"

// TransitiveOrphans returns the fixed-point closure of every lockfile
// dependency whose ResolvedBy chain traces back to one of the given
// seededKeys, using the same fixed-point "walk ResolvedBy transitively"
// shape PlanScopedUpdate already uses (update.go's `exclude` map, there
// seeded from a single scoped-update target instead of an uninstall's set
// of removal roots). The returned set always includes every key in
// seededKeys itself, in addition to every key transitively resolved by any
// of them.
func TransitiveOrphans(lock *lockfile.Lockfile, seededKeys map[string]bool) map[string]bool {
	closure := make(map[string]bool, len(seededKeys))
	for k := range seededKeys {
		closure[k] = true
	}
	if lock == nil {
		return closure
	}
	changed := true
	for changed {
		changed = false
		for _, dep := range lock.Dependencies {
			if closure[dep.ResolvedBy] && !closure[dep.UniqueKey()] {
				closure[dep.UniqueKey()] = true
				changed = true
			}
		}
	}
	return closure
}

// ActualOrphans computes un-041's "actual_orphans = orphans - remaining"
// (mirrors engine.py's _cleanup_transitive_orphans, engine.py:389-472):
//
//   - orphans = TransitiveOrphans(lock, removedKeys), minus removedKeys
//     themselves (the explicit removal targets aren't "orphans", they're
//     just gone).
//   - remaining = remainingRootKeys (the caller-supplied set of keys still
//     directly declared in apm.yml after the removal -- which section(s)
//     of apm.yml count towards this is a CLI-level policy decision, left to
//     the caller to assemble) UNION every lockfile dependency that is
//     itself neither a removal target nor an orphan candidate (such a
//     dependency is being kept regardless of remainingRootKeys).
//   - actual_orphans = orphans - remaining.
func ActualOrphans(lock *lockfile.Lockfile, removedKeys, remainingRootKeys map[string]bool) map[string]bool {
	orphans := TransitiveOrphans(lock, removedKeys)
	for k := range removedKeys {
		delete(orphans, k)
	}
	if len(orphans) == 0 {
		return orphans
	}

	remaining := make(map[string]bool, len(remainingRootKeys))
	for k := range remainingRootKeys {
		remaining[k] = true
	}
	if lock != nil {
		for i := range lock.Dependencies {
			dep := &lock.Dependencies[i]
			key := dep.UniqueKey()
			if !orphans[key] && !removedKeys[key] {
				remaining[key] = true
			}
		}
	}

	actual := make(map[string]bool, len(orphans))
	for k := range orphans {
		if !remaining[k] {
			actual[k] = true
		}
	}
	return actual
}
