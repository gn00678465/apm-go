package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/archive"
	"github.com/apm-go/apm/internal/lockfile"
)

// RemoveDeployedFiles reverses the additive deploy pipeline for a single set
// of previously-deployed relative paths (a LockedDep's DeployedFiles, or the
// lockfile self-entry's LocalDeployedFiles). It is the only place uninstall
// deletes target files, and it only ever deletes paths the caller explicitly
// passes in files -- it never scans directories for extra content, so
// user-authored files living alongside deployed ones are never touched.
//
// For each entry in files:
//  1. path-containment (reusing archive.ContainedKey, the same guard used for
//     untrusted manifest-derived paths elsewhere) -- an escaping path is
//     refused outright and reported via diags/kept, never touched.
//  2. a missing file is treated as already-gone (skipped silently, not an
//     error, and not reported in removed/kept).
//  3. the on-disk sha256 is compared against hashes[path] (un-053, the
//     project's non-negotiable safety line): a mismatch -- including no
//     recorded hash to compare against at all -- means the file was hand
//     edited or its provenance can't be verified, so it is kept and a
//     warning is recorded instead of being silently deleted.
//
// Only once all three checks pass is the file removed, after which any
// now-empty ancestor directories (up to but excluding projectDir) are
// cleaned up.
//
// A copy deploy never makes a symlink, so a symlink at a skill's directory,
// skills/<name>, is the user's: no path at or below it is compared or
// removed, one diagnostic names the symlink (skillSymlink), and a file that
// exists there is reported in kept. A deploy that
// does make symlinks is removed by RemoveStaleLinkedFiles, which calls this
// function only for paths with no such symlink on them.
//
// A lock also lists each deployed skill's directory, a path with no hash
// (recordedDir). Such an entry is never removed by itself and gets no
// warning: it is reported in removed when it was there and is gone once the
// files of this call are, and it stays when something is left in it.
func RemoveDeployedFiles(projectDir string, files []string, hashes map[string]string) (removed []string, kept []string, diags []string) {
	var dirs []string
	isDir := make(map[string]bool)
	for _, d := range recordedDirs(projectDir, files, hashes) {
		if skillSymlink(projectDir, d) == "" {
			dirs = append(dirs, d)
			isDir[d] = true
		}
	}
	defer func() { removed = append(removed, goneDirs(projectDir, dirs)...) }()
	reported := make(map[string]bool)
	for _, f := range files {
		if isDir[f] {
			continue
		}
		if link := skillSymlink(projectDir, f); link != "" && archive.ContainedKey(projectDir, f) {
			if !reported[link] {
				reported[link] = true
				diags = append(diags, fmt.Sprintf("keeping %q: it is a symlink, so nothing below it is removed", link))
			}
			_, hashed := hashes[f]
			if _, err := os.Stat(filepath.Join(projectDir, filepath.FromSlash(f))); hashed && err == nil {
				kept = append(kept, f)
			}
			continue
		}
		if !archive.ContainedKey(projectDir, f) {
			kept = append(kept, f)
			diags = append(diags, fmt.Sprintf("refusing to remove %q: path escapes project directory", f))
			continue
		}

		target := filepath.Join(projectDir, filepath.FromSlash(f))
		if _, err := os.Stat(target); err != nil {
			if os.IsNotExist(err) {
				cleanupDanglingSymlinks(projectDir, filepath.Dir(target))
				continue
			}
			kept = append(kept, f)
			diags = append(diags, fmt.Sprintf("keeping %q: %v", f, err))
			continue
		}

		expected, ok := hashes[f]
		if !ok {
			kept = append(kept, f)
			diags = append(diags, fmt.Sprintf("keeping %q: no recorded hash to verify against", f))
			continue
		}

		actual, err := lockfile.HashFileBytes(target)
		if err != nil {
			kept = append(kept, f)
			diags = append(diags, fmt.Sprintf("keeping %q: %v", f, err))
			continue
		}
		_, expHex, expErr := lockfile.ParseHashEnvelope(expected)
		_, actHex, _ := lockfile.ParseHashEnvelope(actual)
		if expErr != nil || expHex != actHex {
			kept = append(kept, f)
			diags = append(diags, fmt.Sprintf("keeping %q: modified since deploy (hash mismatch)", f))
			continue
		}

		if err := os.Remove(target); err != nil {
			kept = append(kept, f)
			diags = append(diags, fmt.Sprintf("keeping %q: failed to remove: %v", f, err))
			continue
		}
		removed = append(removed, f)
		cleanupEmptyParents(projectDir, filepath.Dir(target))
	}
	return removed, kept, diags
}

// recordedDir reports whether the lock path rel is a deployed skill's
// directory: an entry with no hash that is a directory on disk, also when it
// is reached through a symlink (the skill symlink of a user-scope deploy).
func recordedDir(root, rel string, hashes map[string]string) bool {
	if _, hashed := hashes[rel]; hashed {
		return false
	}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil && info.IsDir()
}

// skillSymlink returns the lock path of the first symlink on rel that sits at
// a skill's directory, the segment after "skills", or "" when there is none.
func skillSymlink(root, rel string) string {
	segs := strings.Split(filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel))), "/")
	for i := 1; i < len(segs); i++ {
		if segs[i-1] != "skills" {
			continue
		}
		prefix := strings.Join(segs[:i+1], "/")
		if info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(prefix))); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return prefix
		}
	}
	return ""
}

// recordedDirs returns the entries of files that are recordedDir and inside
// root, found before anything is removed, so the answer does not depend on
// the order of files.
func recordedDirs(root string, files []string, hashes map[string]string) []string {
	var dirs []string
	for _, f := range files {
		if archive.ContainedKey(root, f) && recordedDir(root, f, hashes) {
			dirs = append(dirs, f)
		}
	}
	return dirs
}

// goneDirs returns the entries of dirs that are not on disk any more.
func goneDirs(root string, dirs []string) []string {
	var gone []string
	for _, d := range dirs {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(d))); os.IsNotExist(err) {
			gone = append(gone, d)
		}
	}
	return gone
}

// SafeRemoveModuleDir deletes a single dependency's vendored directory under
// projectDir/apm_modules (un-030~032) -- the whole-tree counterpart to
// RemoveDeployedFiles's per-file deletion above, for the OTHER thing
// uninstall must delete: the package's own extracted/cloned copy, not
// anything it deployed to a target.
//
// identityKey is the same key space as manifest.DependencyReference.
// IdentityKey() / lockfile.LockedDep.UniqueKey() (RepoURL[/VirtualPath]),
// used verbatim as apm_modules's relative subpath -- exactly how install.go
// and gitops.RealPackageLoader already lay out apm_modules/<key>.
//
// Uses archive.ContainedKey rather than a plain archive.Contained(root,
// filepath.Join(root, identityKey)) check: identityKey is only
// charset-validated at manifest-parse time (its VirtualPath segment grammar
// does not reject a ".." segment, see manifest.validateVirtualPath), so a
// crafted or corrupted identity could otherwise resolve to a path that is
// still technically "inside" apm_modules but is the wrong directory
// entirely -- the same threat model update.go's directGitSemverUpdateScope
// already guards against for this identical key space, kept consistent here
// (checklist's "path 防護全庫一致" caution).
//
// A missing target directory is a no-op (removed=false, err=nil) -- already
// gone, or never materialized (e.g. a local-path dependency, which
// gitops.RealPackageLoader never vendors under apm_modules at all). On
// success, any now-empty ancestor directories up to (but excluding)
// projectDir are cleaned up too, reusing cleanupEmptyParents exactly as
// RemoveDeployedFiles does.
func SafeRemoveModuleDir(projectDir, identityKey string) (removed bool, err error) {
	if identityKey == "" {
		return false, nil
	}
	modulesRoot := filepath.Join(projectDir, "apm_modules")
	target := filepath.Join(modulesRoot, filepath.FromSlash(identityKey))
	if !archive.ContainedKey(modulesRoot, identityKey) {
		return false, fmt.Errorf("refusing to remove apm_modules entry %q: path escapes apm_modules", identityKey)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		if os.IsNotExist(statErr) {
			return false, nil
		}
		return false, statErr
	}
	if err := os.RemoveAll(target); err != nil {
		return false, err
	}
	cleanupEmptyParents(projectDir, filepath.Dir(target))
	return true, nil
}

// cleanupDanglingSymlinks walks from dir up toward projectDir, removing any
// directory that is a dangling symlink (target no longer exists). Stops at
// projectDir. This handles the case where a symlink deploy's target was
// deleted (e.g. apm_modules removed), leaving a dangling directory symlink
// in the deploy tree.
func cleanupDanglingSymlinks(projectDir, dir string) {
	root := filepath.Clean(projectDir)
	for {
		dir = filepath.Clean(dir)
		if dir == root || !archive.Contained(root, dir) {
			return
		}
		info, err := os.Lstat(dir)
		if err != nil {
			return
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if _, statErr := os.Stat(dir); statErr != nil && os.IsNotExist(statErr) {
				os.Remove(dir)
				dir = filepath.Dir(dir)
				continue
			}
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		os.Remove(dir)
		dir = filepath.Dir(dir)
	}
}

// cleanupEmptyParents removes dir and, walking upward, each ancestor that has
// become empty, stopping at (and never removing) projectDir itself.
func cleanupEmptyParents(projectDir, dir string) {
	root := filepath.Clean(projectDir)
	for {
		dir = filepath.Clean(dir)
		if dir == root || !archive.Contained(root, dir) {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
