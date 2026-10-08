package deploy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/apm-go/apm/internal/archive"
	"github.com/apm-go/apm/internal/lockfile"
)

// RemoveStaleLinkedFiles deletes the stale deployed paths of one lock bucket
// under deployRoot when the deploy made symlinks into projectDir (a
// user-scope install: one symlink per file, or one per skill directory with
// the lock recording the files below it).
//
// bucketSource is the directory the bucket's content is read from, inside
// projectDir: apm_modules/<key> for a dependency, .apm for local content. It
// decides ownership. A deploy symlink is one whose target is inside
// bucketSource and that is either the stale path itself or a parent that is
// a skill directory, skills/<name>. Those are the only places the deploy
// makes a symlink, and it points them nowhere else, so any other symlink is
// the user's, also one that leads elsewhere into projectDir. The first deploy
// symlink on a path, from deployRoot down to the path itself, decides: only
// that symlink is removed, never anything through it. It is kept when claimed
// (normalized lock paths of the new lock) still has a path at or below it.
//
// A target inside bucketSource does not prove the deploy made the symlink:
// the user can point an entry at another file of the same package, and the
// lock does not record a symlink's target. So the symlink is removed only
// when it dangles (its source is gone) or when the stale paths at or below it
// read the content hashes the lock recorded (holdsDeployedContent). Otherwise
// it is kept with a diagnostic.
//
// A symlink in one of those two places that leads outside bucketSource is an
// entry the user took over. It is kept with one diagnostic, and for a skill
// directory nothing below it is inspected, compared or removed: a user-scope
// deploy never writes a file below skills/<name>, so what is found there
// through the user's symlink is the user's.
//
// A user's symlink on any other parent (~/.claude linked into a dotfiles
// directory) is walked through and never removed, also not when the directory
// behind it becomes empty.
//
// A path with no deploy symlink on it that is a file goes to
// RemoveDeployedFiles, unless a user symlink was walked through and the file
// is in fact anywhere inside projectDir: then it is kept with a diagnostic. A
// path that does not exist is skipped.
//
// removed lists each deleted symlink once by its own lock path, plus the
// files RemoveDeployedFiles deleted, all relative to deployRoot.
func RemoveStaleLinkedFiles(deployRoot, projectDir, bucketSource string, stale []string, hashes map[string]string, claimed map[string]bool) (removed []string, diags []string) {
	var plain []string
	decided := make(map[string]bool)
	for _, f := range stale {
		if !archive.ContainedKey(deployRoot, f) {
			plain = append(plain, f) // RemoveDeployedFiles refuses and reports it
			continue
		}
		loc, err := locateStalePath(deployRoot, bucketSource, f)
		switch {
		case err != nil:
			diags = append(diags, fmt.Sprintf("keeping %q: %v", f, err))
		case !loc.exists:
		case loc.userLink != "":
			if !decided[loc.userLink] {
				decided[loc.userLink] = true
				diags = append(diags, fmt.Sprintf("keeping %q: symlink target %q is not in the source of this package", loc.userLink, loc.linkTarget))
			}
		case loc.deployLink != "":
			if decided[loc.deployLink] {
				continue
			}
			decided[loc.deployLink] = true
			if claimedAtOrBelow(claimed, loc.deployLink) {
				continue
			}
			full := filepath.Join(deployRoot, filepath.FromSlash(loc.deployLink))
			deployed, err := holdsDeployedContent(deployRoot, full, lockPathsAtOrBelow(stale, loc.deployLink), hashes)
			if err != nil {
				diags = append(diags, fmt.Sprintf("keeping %q: %v", loc.deployLink, err))
				continue
			}
			if !deployed {
				diags = append(diags, fmt.Sprintf("keeping %q: symlink target %q does not hold what this package deployed", loc.deployLink, loc.linkTarget))
				continue
			}
			if err := os.Remove(full); err != nil {
				diags = append(diags, fmt.Sprintf("keeping %q: failed to remove: %v", loc.deployLink, err))
				continue
			}
			cleanupEmptyParents(loc.cleanupRoot, filepath.Dir(full))
			removed = append(removed, loc.deployLink)
		case loc.cleanupRoot == deployRoot:
			plain = append(plain, f)
		case resolvesInside(projectDir, filepath.Join(deployRoot, filepath.FromSlash(f))):
			diags = append(diags, fmt.Sprintf("keeping %q: it is a file in the apm project directory, reached through a symlink", f))
		default:
			ok, diag := removeFileBelowUserSymlink(loc, f, hashes)
			if ok {
				removed = append(removed, f)
			}
			if diag != "" {
				diags = append(diags, diag)
			}
		}
	}
	plainRemoved, _, plainDiags := RemoveDeployedFiles(deployRoot, plain, hashes)
	return append(removed, plainRemoved...), append(diags, plainDiags...)
}

// staleLocation is what a walk down one stale path found.
type staleLocation struct {
	exists bool
	// deployLink is the lock path of the first symlink in a place where the
	// deploy makes one, the path itself or a skill directory, that leads into
	// the bucket's source.
	deployLink string
	// userLink is the lock path of a symlink in such a place that leads
	// outside the bucket's source: the user took the entry over.
	userLink string
	// linkTarget is where deployLink or userLink leads.
	linkTarget string
	// cleanupRoot is the deepest directory that must survive the removal of
	// empty parents: deployRoot, or the deepest user symlink walked through.
	cleanupRoot string
	// belowCleanupRoot is the path relative to cleanupRoot when cleanupRoot
	// is a user symlink.
	belowCleanupRoot string
}

// locateStalePath walks rel from deployRoot down. os.Lstat does not follow
// the last component, so each symlink is seen before anything behind it.
func locateStalePath(deployRoot, bucketSource, rel string) (staleLocation, error) {
	loc := staleLocation{cleanupRoot: deployRoot}
	segments := strings.Split(filepath.Clean(filepath.FromSlash(rel)), string(filepath.Separator))
	for i := range segments {
		prefix := filepath.Join(segments[:i+1]...)
		full := filepath.Join(deployRoot, prefix)
		info, err := os.Lstat(full)
		isLink := err == nil && info.Mode()&os.ModeSymlink != 0
		var target string
		if isLink {
			target, err = os.Readlink(full)
		}
		if os.IsNotExist(err) {
			return loc, nil
		}
		if err != nil {
			return loc, err
		}
		if !isLink {
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(full), target)
		}
		isPathItself := i == len(segments)-1
		// The deploy makes a directory symlink in one place only:
		// symlinkSkillTo, at skills/<name>. When a deploy makes one anywhere
		// else, this test must follow, or that symlink is taken for the
		// user's.
		isSkillDir := i > 0 && segments[i-1] == "skills"
		if isPathItself || isSkillDir {
			loc.exists, loc.linkTarget = true, target
			if archive.Contained(bucketSource, target) {
				loc.deployLink = filepath.ToSlash(prefix)
			} else {
				loc.userLink = filepath.ToSlash(prefix)
			}
			return loc, nil
		}
		loc.cleanupRoot = full
		loc.belowCleanupRoot = filepath.ToSlash(filepath.Join(segments[i+1:]...))
	}
	loc.exists = true
	return loc, nil
}

// lockPathsAtOrBelow returns the entries of stale that are link itself or
// below it, as the lock spells them.
func lockPathsAtOrBelow(stale []string, link string) []string {
	var paths []string
	for _, f := range stale {
		p := filepath.ToSlash(filepath.Clean(filepath.FromSlash(f)))
		if p == link || strings.HasPrefix(p, link+"/") {
			paths = append(paths, f)
		}
	}
	return paths
}

// holdsDeployedContent reports whether the symlink at full is the one this
// bucket's deploy made. The lock does not record a symlink's target, so the
// proof is what the lock does record: the symlink dangles because its source
// is gone, or at least one of lockPaths exists and each one that exists reads
// through the symlink the content hash the lock holds for it. The comparison
// is the one RemoveDeployedFiles makes.
func holdsDeployedContent(deployRoot, full string, lockPaths []string, hashes map[string]string) (bool, error) {
	if _, err := os.Stat(full); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	found := false
	for _, f := range lockPaths {
		actual, err := lockfile.HashFileBytes(filepath.Join(deployRoot, filepath.FromSlash(f)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		_, wantHex, wantErr := lockfile.ParseHashEnvelope(hashes[f])
		_, gotHex, _ := lockfile.ParseHashEnvelope(actual)
		if wantErr != nil || wantHex != gotHex {
			return false, nil
		}
		found = true
	}
	return found, nil
}

// resolvesInside reports whether full, with every symlink followed, is inside
// root. locateStalePath compares the text of a symlink target, so a deploy
// symlink written through another name of root (a symlinked home directory)
// looks like the user's; this check keeps the file behind it. root is made
// absolute first because filepath.EvalSymlinks returns "." unresolved. It
// also reports true when a path cannot be resolved.
func resolvesInside(root, full string) bool {
	absRoot, absErr := filepath.Abs(root)
	realRoot, rootErr := filepath.EvalSymlinks(absRoot)
	realFull, fullErr := filepath.EvalSymlinks(full)
	if absErr != nil || rootErr != nil || fullErr != nil {
		return true
	}
	return archive.Contained(realRoot, realFull)
}

// removeFileBelowUserSymlink runs RemoveDeployedFiles rooted at the user's
// symlink instead of deployRoot. RemoveDeployedFiles removes every empty
// parent below its root and does not see that one of them is a symlink, so
// rooted at deployRoot it would unlink the user's symlink. The diagnostic
// names lockPath, not the path below the symlink.
func removeFileBelowUserSymlink(loc staleLocation, lockPath string, hashes map[string]string) (removed bool, diag string) {
	var below map[string]string
	if h, ok := hashes[lockPath]; ok {
		below = map[string]string{loc.belowCleanupRoot: h}
	}
	gone, _, diags := RemoveDeployedFiles(loc.cleanupRoot, []string{loc.belowCleanupRoot}, below)
	if len(diags) > 0 {
		diag = strings.Replace(diags[0], strconv.Quote(loc.belowCleanupRoot), strconv.Quote(lockPath), 1)
	}
	return len(gone) > 0, diag
}

func claimedAtOrBelow(claimed map[string]bool, link string) bool {
	for p := range claimed {
		if p == link || strings.HasPrefix(p, link+"/") {
			return true
		}
	}
	return false
}
