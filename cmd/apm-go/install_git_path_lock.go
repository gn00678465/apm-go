package main

import (
	"slices"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
)

// isGitPathEntry reports whether dep is how apm-go before issue #114 recorded
// a `git:` entry whose value is the local path declared: a git source with
// that path as repo_url, a virtual_path when the entry had a `path:`.
func isGitPathEntry(dep *lockfile.LockedDep, declared string) bool {
	return (dep.Source == "git" || dep.Source == "") && dep.LocalPath == "" && dep.RepoURL == declared
}

// useLocalKeysForGitPaths turns, in lock, each such entry of a direct local
// dependency of m into the record of the local package it is now, under its
// current key, when lock has no entry under that key. Its deployed files and
// their hashes stay, so the stale cleanup and the adopted count judge them as
// the package's own, and a frozen install reads the lock as it is (ruling
// R16) without taking the entry for a git source to clone. m may be
// normalized or not. It changes the keys of lock, so it runs before the first
// lock.FindByKey, whose index is built once.
func useLocalKeysForGitPaths(m *manifest.Manifest, lock *lockfile.Lockfile) {
	for _, dep := range allDirectDeps(m) {
		local := *dep
		normalizeLocalDep(&local)
		if local.LocalSourcePath == "" || slices.ContainsFunc(lock.Dependencies, func(d lockfile.LockedDep) bool {
			return d.UniqueKey() == local.RepoURL
		}) {
			continue
		}
		for i := range lock.Dependencies {
			old := &lock.Dependencies[i]
			if !isGitPathEntry(old, local.LocalDeclaredPath) {
				continue
			}
			old.RepoURL = local.RepoURL
			old.Source = "local"
			old.LocalPath = local.LocalDeclaredPath
			old.VirtualPath = ""
			old.Constraint = ""
			old.ResolvedCommit = ""
			old.ResolvedTag = ""
			old.ResolvedAt = ""
			old.ResolvedRef = local.LocalDeclaredRef
			old.TreeSHA256 = ""
		}
	}
}
