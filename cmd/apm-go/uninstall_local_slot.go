package main

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/ux"
)

// localSurvivorsAt returns the local declarations of apm.yml that stay and
// whose directory below apm_modules is dir, as the Oracle's
// _surviving_local_refs_at_install_path finds them
// (commands/uninstall/engine.py:85-105).
func localSurvivorsAt(dir string, m *manifest.Manifest, removedIdentities map[string]bool, lock *lockfile.Lockfile) []*manifest.DependencyReference {
	var survivors []*manifest.DependencyReference
	for _, d := range allDirectDeps(m) {
		if !d.IsLocal {
			continue
		}
		id, ok := uninstallIdentity(d)
		if !ok || removedIdentities[id] {
			continue
		}
		if cmp.Or(d.Alias, uninstallRemovalKey(id, lock)) == dir {
			survivors = append(survivors, d)
		}
	}
	return survivors
}

// sharesLockKey reports whether the lock has more than one entry under key.
func sharesLockKey(lock *lockfile.Lockfile, key string) bool {
	if lock == nil {
		return false
	}
	n := 0
	for i := range lock.Dependencies {
		if lock.Dependencies[i].UniqueKey() == key {
			n++
		}
	}
	return n > 1
}

// rekeyLocalEntry moves the local entry of key whose local_path is path to
// the key id, its directory kept as dir, so that removing it under id leaves
// the entry of another declaration of the same key.
func rekeyLocalEntry(lock *lockfile.Lockfile, key, path, id, dir string) {
	if lock == nil {
		return
	}
	abs := resolveLocalSourceAbs(path)
	for i := range lock.Dependencies {
		d := &lock.Dependencies[i]
		if d.Source != "local" || d.UniqueKey() != key || resolveLocalSourceAbs(d.LocalPath) != abs {
			continue
		}
		d.RepoURL = id
		d.VirtualPath = ""
		d.Alias = dir
		lock.Reindex()
		return
	}
}

// localRefreshes holds the copies staged for the shared directories a real
// uninstall refreshes, by directory below apm_modules.
type localRefreshes struct {
	staged  map[string]string
	cleanup []string
}

// stageLocalRefreshes copies the one declaration that stays in each shared
// directory into a staging directory below apm_modules/_local, before any
// write, as the Oracle stages them (commands/uninstall/engine.py:177-261):
// a directory that more than one declaration would still share, or a copy
// that fails, ends the uninstall with nothing changed.
func stageLocalRefreshes(plan *uninstallPlan) (*localRefreshes, error) {
	r := &localRefreshes{staged: map[string]string{}}
	dirs := make([]string, 0, len(plan.refresh))
	for dir := range plan.refresh {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		survivors := plan.refresh[dir]
		label := plan.refreshLabel(dir)
		if len(survivors) > 1 {
			ux.Error(os.Stdout, "Cannot preserve %s because %d declarations would still share one install slot. Use exact paths from apm.yml in one uninstall command so at most one declaration remains.", label, len(survivors))
			r.discard()
			return nil, errors.New("Error uninstalling packages: Multiple local declarations would remain in one shared install slot. No manifest changes were made.")
		}
		staged, err := stageLocalCopy(survivors[0], &r.cleanup)
		if err != nil {
			ux.Error(os.Stdout, "Failed to refresh %s from its declared local path. Check the exact path in apm.yml, run 'apm-go install', and retry.", label)
			r.discard()
			return nil, errors.New("Error uninstalling packages: A surviving local dependency could not be staged. No manifest changes were made.")
		}
		r.staged[dir] = staged
	}
	return r, nil
}

// stageLocalCopy copies d into a new staging directory below
// apm_modules/_local and returns the copy's path.
func stageLocalCopy(d *manifest.DependencyReference, cleanup *[]string) (string, error) {
	if err := os.MkdirAll(filepath.Join("apm_modules", "_local"), 0o755); err != nil {
		return "", err
	}
	root, err := os.MkdirTemp(filepath.Join("apm_modules", "_local"), ".apm-uninstall-refresh-")
	if err != nil {
		return "", err
	}
	*cleanup = append(*cleanup, root)
	ref := *d
	normalizeLocalDep(&ref)
	ref.Alias = ""
	if _, err := newGitPackageLoader(root).LoadPackage(&ref, ""); err != nil {
		return "", err
	}
	return filepath.Join(root, filepath.FromSlash(deploy.DepRefDir(&ref))), nil
}

// activate puts each staged copy in place of its shared directory.
func (r *localRefreshes) activate() error {
	if r == nil {
		return nil
	}
	defer r.discard()
	dirs := make([]string, 0, len(r.staged))
	for dir := range r.staged {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		if _, err := deploy.SafeRemoveModuleDir(".", dir); err != nil {
			return fmt.Errorf("refresh apm_modules/%s: %w", dir, err)
		}
		dest := filepath.Join("apm_modules", filepath.FromSlash(dir))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("refresh apm_modules/%s: %w", dir, err)
		}
		if err := os.Rename(r.staged[dir], dest); err != nil {
			return fmt.Errorf("refresh apm_modules/%s: %w", dir, err)
		}
	}
	return nil
}

// discard removes the staging directories.
func (r *localRefreshes) discard() {
	if r == nil {
		return
	}
	for _, root := range r.cleanup {
		_ = os.RemoveAll(root)
	}
	r.cleanup = nil
}

// refreshedKeys returns the lock keys of the declarations whose directory the
// uninstall refreshes.
func (p *uninstallPlan) refreshedKeys(lock *lockfile.Lockfile) map[string]bool {
	keys := map[string]bool{}
	for _, survivors := range p.refresh {
		for _, d := range survivors {
			if id, ok := uninstallIdentity(d); ok {
				keys[uninstallRemovalKey(id, lock)] = true
			}
		}
	}
	return keys
}

// refreshLabel is the Oracle's public label of the package a shared
// directory refresh stands for: its apm_modules key.
func (p *uninstallPlan) refreshLabel(dir string) string {
	for _, t := range p.apmTargets {
		if p.declaredDirs[p.removalKeyByIdentity[t.IdentityKey]] == dir {
			return p.moduleKey(t)
		}
	}
	return dir
}
