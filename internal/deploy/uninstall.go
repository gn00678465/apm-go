package deploy

import (
	"fmt"
	"os"
	"path/filepath"

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
func RemoveDeployedFiles(projectDir string, files []string, hashes map[string]string) (removed []string, kept []string, diags []string) {
	for _, f := range files {
		if !archive.ContainedKey(projectDir, f) {
			kept = append(kept, f)
			diags = append(diags, fmt.Sprintf("refusing to remove %q: path escapes project directory", f))
			continue
		}

		target := filepath.Join(projectDir, filepath.FromSlash(f))
		if _, err := os.Stat(target); err != nil {
			if os.IsNotExist(err) {
				continue // already gone / never deployed here -- not an error
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
