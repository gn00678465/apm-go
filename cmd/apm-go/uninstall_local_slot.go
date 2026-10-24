package main

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
// uninstall refreshes, by directory below apm_modules, and, once they are in
// place, the original directories kept as backups until the uninstall
// commits.
type localRefreshes struct {
	staged    map[string]string
	cleanup   []string
	activated []string
	backups   map[string]string
}

// stageLocalRefreshes copies the one declaration that stays in each shared
// directory into a staging directory, before any write, as the Oracle stages
// them (commands/uninstall/engine.py:177-261): a directory that more than one
// declaration would still share, a copy that fails, or a copy that is no
// package ends the uninstall with nothing changed.
func stageLocalRefreshes(plan *uninstallPlan) (*localRefreshes, error) {
	r := &localRefreshes{staged: map[string]string{}, backups: map[string]string{}}
	for _, dir := range sortedStringSet(stringKeys(plan.refresh)) {
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

func stringKeys[V any](m map[string]V) map[string]bool {
	keys := make(map[string]bool, len(m))
	for k := range m {
		keys[k] = true
	}
	return keys
}

// stageLocalCopy copies d into a new staging directory directly below
// apm_modules and returns the copy's path. The Oracle stages below
// apm_modules/_local (commands/uninstall/engine.py:192-195), which is inside
// the directory it replaces when the declarations share it through
// `alias: _local`, so its refresh fails there; a staging directory beside
// every package directory cannot be inside the one replaced.
func stageLocalCopy(d *manifest.DependencyReference, cleanup *[]string) (string, error) {
	if err := os.MkdirAll("apm_modules", 0o755); err != nil {
		return "", err
	}
	root, err := os.MkdirTemp("apm_modules", ".apm-uninstall-refresh-")
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
	staged := filepath.Join(root, filepath.FromSlash(deploy.DepRefDir(&ref)))
	if !refreshCopyHasMarker(staged) {
		return "", fmt.Errorf("%s: no apm.yml, SKILL.md or plugin.json", d.LocalPath)
	}
	return staged, nil
}

// refreshCopyHasMarker reports whether a staged copy is a package, as the
// Oracle refuses a local source with no apm.yml, SKILL.md or plugin.json
// (install/phases/local_content.py:253-266) before it validates the staged
// copy (commands/uninstall/engine.py:235-243).
func refreshCopyHasMarker(dir string) bool {
	for _, name := range []string{"apm.yml", "SKILL.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return pluginJSONPath(dir) != ""
}

// activate puts each staged copy in place of its shared directory, keeping
// the original as a backup beside it, as the Oracle's
// _activate_staged_local_refresh does (commands/uninstall/engine.py:262-289).
// When one fails, every directory already replaced gets its original back and
// the error is returned.
func (r *localRefreshes) activate() error {
	if r == nil {
		return nil
	}
	for _, dir := range sortedStringSet(stringKeys(r.staged)) {
		if err := r.activateOne(dir); err != nil {
			r.rollback()
			return fmt.Errorf("refresh apm_modules/%s: %w", dir, err)
		}
	}
	return nil
}

func (r *localRefreshes) activateOne(dir string) error {
	if err := deploy.CheckModuleDir(".", dir); err != nil {
		return err
	}
	dest := filepath.Join("apm_modules", filepath.FromSlash(dir))
	backup := filepath.Join(filepath.Dir(dest), "."+filepath.Base(dest)+".apm-uninstall-backup")
	if _, err := os.Lstat(backup); err == nil {
		return fmt.Errorf("refresh backup %s already exists", backup)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	hadDest := true
	if err := os.Rename(dest, backup); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		hadDest = false
	}
	if err := os.Rename(r.staged[dir], dest); err != nil {
		if hadDest {
			_ = os.Rename(backup, dest)
		}
		return err
	}
	r.activated = append(r.activated, dir)
	if hadDest {
		r.backups[dir] = backup
	}
	return nil
}

// rollback gives each replaced directory its original back, the last
// replaced first.
func (r *localRefreshes) rollback() {
	if r == nil {
		return
	}
	for i := len(r.activated) - 1; i >= 0; i-- {
		dir := r.activated[i]
		dest := filepath.Join("apm_modules", filepath.FromSlash(dir))
		err := os.RemoveAll(dest)
		if backup, ok := r.backups[dir]; ok && err == nil {
			err = os.Rename(backup, dest)
		}
		if err != nil {
			ux.Warn(os.Stderr, "Could not restore apm_modules/%s: %v", dir, err)
		}
	}
	r.activated, r.backups = nil, map[string]string{}
	r.discard()
}

// commit drops the backups once the uninstall has written apm.yml.
func (r *localRefreshes) commit() {
	if r == nil {
		return
	}
	for _, backup := range r.backups {
		_ = os.RemoveAll(backup)
	}
	r.backups = map[string]string{}
	r.discard()
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
