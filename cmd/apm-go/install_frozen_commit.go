package main

import (
	"fmt"
	"strings"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/semver"
)

// checkFrozenCommitPins is the commit check of the Oracle's _check_locked
// (deps/immutable_requirements.py:116-140), which runs on every node of the
// graph, a transitive one included (deps/apm_resolver.py:665-671): a git
// dependency whose ref names a commit has to be locked to a commit that
// starts with it. A tag named like a commit prefix is installed as the tag,
// so its lock fails here, as in the Oracle. The ref and the chain come from
// the lock, which a frozen install replays as it is.
func checkFrozenCommitPins(rootName string, lock *lockfile.Lockfile) error {
	for i := range lock.Dependencies {
		dep := &lock.Dependencies[i]
		if dep.Source == "local" || dep.Source == "registry" || dep.RegistryPrefix != "" || semver.IsSemverRange(dep.Constraint) {
			continue
		}
		ref, commit := strings.ToLower(dep.ResolvedRef), strings.ToLower(dep.ResolvedCommit)
		if !gitops.IsCommitRef(ref) || len(commit) != 40 || strings.HasPrefix(commit, ref) {
			continue
		}
		key := lockEntry{LockedDep: dep}.uniqueKey()
		return fmt.Errorf("frozen install: Cannot resolve %s: incompatible immutable requirements.\n"+
			"  apm.lock.yaml -> %s@%s (commit %s)\n"+
			"  %s (commit %s)\n"+
			"Align the requested refs in the root or parent manifests to the same commit, "+
			"then run 'apm-go install' without --frozen to regenerate apm.lock.yaml.",
			key, key, dep.ResolvedRef, commit, frozenChain(rootName, lock, dep), ref)
	}
	return nil
}

// frozenChain is the Oracle's requirement chain (immutable_requirements.py:
// 71-82): the root's name, then each dependency from the direct one down,
// as <unique key>@<ref>, HEAD for a dependency with none. resolved_by names
// a dependency's parent.
func frozenChain(rootName string, lock *lockfile.Lockfile, dep *lockfile.LockedDep) string {
	var parts []string
	seen := map[*lockfile.LockedDep]bool{}
	for cur := dep; cur != nil && !seen[cur]; {
		seen[cur] = true
		ref := cur.ResolvedRef
		if ref == "" {
			ref = "HEAD"
		}
		parts = append(parts, lockEntry{LockedDep: cur}.uniqueKey()+"@"+ref)
		var parent *lockfile.LockedDep
		if cur.ResolvedBy != "" {
			for j := range lock.Dependencies {
				if lock.Dependencies[j].UniqueKey() == cur.ResolvedBy {
					parent = &lock.Dependencies[j]
					break
				}
			}
		}
		cur = parent
	}
	chain := []string{rootName}
	for i := len(parts) - 1; i >= 0; i-- {
		chain = append(chain, parts[i])
	}
	return strings.Join(chain, " -> ")
}
