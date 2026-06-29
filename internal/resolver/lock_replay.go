package resolver

import (
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
)

type ReplayAction int

const (
	ReplayLocked ReplayAction = iota // use locked pin
	ReResolve                        // re-resolve against remote
	NewDep                           // no lock entry, fresh resolve
)

// ShouldReplay returns true if the manifest constraint is character-equal
// to the locked constraint (req-rs-004). Any difference, including whitespace,
// triggers re-resolution.
func ShouldReplay(manifestConstraint, lockedConstraint string) bool {
	return manifestConstraint == lockedConstraint
}

// ReplayDecision determines whether to replay, re-resolve, or fresh-resolve
// a dependency given the current manifest and lockfile.
func ReplayDecision(ref *manifest.DependencyReference, lock *lockfile.Lockfile) ReplayAction {
	if lock == nil {
		return NewDep
	}
	key := depKey(ref)
	locked := lock.FindByKey(key)
	if locked == nil {
		return NewDep
	}
	if ShouldReplay(ref.Reference, locked.Constraint) {
		return ReplayLocked
	}
	return ReResolve
}
