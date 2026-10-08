package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/apm-go/apm/internal/archive"
)

// RemoveStaleLinkedFiles deletes the stale deployed paths of one lock bucket
// under deployRoot when the deploy made symlinks into sourceRoot (a
// user-scope install: one symlink per file, or one per skill directory with
// the lock recording the files below it).
//
// A deploy symlink is one whose target is inside sourceRoot and that is
// either the stale path itself or a parent that is a skill directory,
// skills/<name>. Those are the only places the deploy makes a symlink, so a
// symlink on any other parent (a target root, a skills directory) is the
// user's wherever it leads. The first deploy symlink on a path, from
// deployRoot down to the path itself, decides: only that symlink is removed,
// never anything through it, so no hash is compared. It is kept when claimed
// (normalized lock paths of the new lock) still has a path at or below it.
//
// Every other symlink is the user's. As a parent directory (~/.claude linked
// into a dotfiles directory) it is walked through and never removed, also not
// when the directory behind it becomes empty. As the path itself, with a
// target outside sourceRoot, it is kept with a diagnostic.
//
// A path with no deploy symlink on it that is a file goes to
// RemoveDeployedFiles, unless a user symlink was walked through and the file
// is in fact inside sourceRoot: then it is kept with a diagnostic. A path
// that does not exist is skipped.
//
// removed lists each deleted symlink once by its own lock path, plus the
// files RemoveDeployedFiles deleted, all relative to deployRoot.
func RemoveStaleLinkedFiles(deployRoot, sourceRoot string, stale []string, hashes map[string]string, claimed map[string]bool) (removed []string, diags []string) {
	var plain []string
	decided := make(map[string]bool)
	for _, f := range stale {
		if !archive.ContainedKey(deployRoot, f) {
			plain = append(plain, f) // RemoveDeployedFiles refuses and reports it
			continue
		}
		loc, err := locateStalePath(deployRoot, sourceRoot, f)
		switch {
		case err != nil:
			diags = append(diags, fmt.Sprintf("keeping %q: %v", f, err))
		case !loc.exists:
		case loc.userTarget != "":
			diags = append(diags, fmt.Sprintf("keeping %q: symlink target %q is outside the apm project directory", f, loc.userTarget))
		case loc.deployLink != "":
			if decided[loc.deployLink] {
				continue
			}
			decided[loc.deployLink] = true
			if claimedAtOrBelow(claimed, loc.deployLink) {
				continue
			}
			full := filepath.Join(deployRoot, filepath.FromSlash(loc.deployLink))
			if err := os.Remove(full); err != nil {
				diags = append(diags, fmt.Sprintf("keeping %q: failed to remove: %v", loc.deployLink, err))
				continue
			}
			cleanupEmptyParents(loc.cleanupRoot, filepath.Dir(full))
			removed = append(removed, loc.deployLink)
		case loc.cleanupRoot == deployRoot:
			plain = append(plain, f)
		case resolvesInside(sourceRoot, filepath.Join(deployRoot, filepath.FromSlash(f))):
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
	// deployLink is the lock path of the first symlink into sourceRoot.
	deployLink string
	// userTarget is the target of the path itself when it is a symlink that
	// leaves sourceRoot.
	userTarget string
	// cleanupRoot is the deepest directory that must survive the removal of
	// empty parents: deployRoot, or the deepest user symlink walked through.
	cleanupRoot string
	// belowCleanupRoot is the path relative to cleanupRoot when cleanupRoot
	// is a user symlink.
	belowCleanupRoot string
}

// locateStalePath walks rel from deployRoot down. os.Lstat does not follow
// the last component, so each symlink is seen before anything behind it.
func locateStalePath(deployRoot, sourceRoot, rel string) (staleLocation, error) {
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
		switch {
		case archive.Contained(sourceRoot, target) && (isPathItself || isSkillDir):
			loc.exists, loc.deployLink = true, filepath.ToSlash(prefix)
			return loc, nil
		case isPathItself:
			loc.exists, loc.userTarget = true, target
			return loc, nil
		}
		loc.cleanupRoot = full
		loc.belowCleanupRoot = filepath.ToSlash(filepath.Join(segments[i+1:]...))
	}
	loc.exists = true
	return loc, nil
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
