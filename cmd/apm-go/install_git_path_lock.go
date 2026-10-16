package main

import (
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
)

// isGitPathEntry reports whether dep is how apm-go before issue #114 recorded
// a `git:` entry whose value is the local path declared: a git source with
// that path as repo_url, a virtual_path when the entry had a `path:`.
func isGitPathEntry(dep *lockfile.LockedDep, declared string) bool {
	return (dep.Source == "git" || dep.Source == "") && dep.LocalPath == "" && dep.RepoURL == declared
}

// useLocalKeysForGitPaths turns, in lock, each such entry of the local
// dependencies deps into the record of the local package it is now, under its
// current key, when lock has no entry under that key. Its deployed files and
// their hashes stay, so the stale cleanup and the adopted count judge them as
// the package's own, and a frozen install reads the lock as it is (ruling
// R16) without taking the entry for a git source to clone. deps may be
// normalized or not.
//
// Only a run that copies the package to its current key may write the turned
// entry: install and update do; uninstall turns the entries it removes alone.
func useLocalKeysForGitPaths(deps []*manifest.DependencyReference, lock *lockfile.Lockfile) {
	var oldKeys []string
	for _, dep := range deps {
		local := *dep
		normalizeLocalDep(&local)
		if local.LocalSourcePath == "" || lock.FindByKey(local.RepoURL) != nil {
			continue
		}
		for _, d := range lock.Dependencies {
			if !isGitPathEntry(&d, local.LocalDeclaredPath) {
				continue
			}
			oldKeys = append(oldKeys, d.UniqueKey())
			d.RepoURL = local.RepoURL
			d.Source = "local"
			d.LocalPath = local.LocalDeclaredPath
			d.VirtualPath = ""
			d.Constraint = ""
			d.ResolvedCommit = ""
			d.ResolvedTag = ""
			d.ResolvedAt = ""
			d.ResolvedRef = local.LocalDeclaredRef
			d.TreeSHA256 = ""
			lock.Dependencies = append(lock.Dependencies, d)
		}
	}
	// RemoveKeys also rebuilds the index FindByKey built under the old keys.
	lock.RemoveKeys(oldKeys)
}

// useGitPathKeys gives each normalized local dependency of m the key lock
// records its `git:` path under (isGitPathEntry), when lock has no entry
// under its current key, so the hand-over of uninstall, which turns no entry
// that stays, deploys such a package from the clone the old install made.
func useGitPathKeys(m *manifest.Manifest, lock *lockfile.Lockfile) {
	for _, dep := range allDirectDeps(m) {
		if dep.LocalSourcePath == "" || lock.FindByKey(dep.RepoURL) != nil {
			continue
		}
		for i := range lock.Dependencies {
			if old := &lock.Dependencies[i]; isGitPathEntry(old, dep.LocalDeclaredPath) {
				dep.RepoURL = old.UniqueKey()
			}
		}
	}
}
