package deploy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/apm-go/apm/internal/archive"
)

// DeployPathError is a path below the deploy root that leads, through the
// symlink Link (one of its directories, or for a write the path itself), out
// of its target root (.claude, .github, ...): removing or writing the path
// would act on what Link points at.
type DeployPathError struct {
	// Path is the path relative to the deploy root.
	Path string
	// Link is the first directory on Path, or Path itself, relative to the
	// deploy root, that resolves outside Root: a symlink.
	Link string
	// Resolved is Link with every symlink resolved.
	Resolved string
	// Root is the target root, the first component of Path below the deploy
	// root, with every symlink resolved.
	Root string
}

func (e *DeployPathError) Error() string {
	return fmt.Sprintf("%s: symlink %s resolves to %s, outside %s", e.Path, e.Link, e.Resolved, e.Root)
}

// CheckDeployPath refuses rel, a path relative to deployRoot whose last
// component a cleanup removes or a deploy replaces (os.Remove, os.RemoveAll,
// os.Remove then os.Symlink), unless each directory on it, every symlink
// resolved, is below its target root resolved, as Oracle 0.33.0's
// validate_deploy_path refuses (integration/base_integrator.py:506-590). The
// last component is not resolved: those calls act on a symlink there, not on
// what it points at. A symlink at a skill's directory, skills/<name>, counts
// as the last component, since the deploy makes one there and a copy deploy
// replaces one there, and what lies below it is decided by that symlink. The
// target root may itself be a symlink (~/.claude linked into a dotfiles
// directory): both sides are compared resolved.
func CheckDeployPath(deployRoot, rel string) error {
	return checkDeployPath(deployRoot, rel, false)
}

// CheckDeployWrite is CheckDeployPath for a path whose content a copy deploy
// writes (os.WriteFile, copyFile): those follow a symlink at the last
// component, so the whole path, that component included (a dangling one by
// its target, where os.WriteFile would create the file), has to resolve
// below the target root as well.
func CheckDeployWrite(deployRoot, rel string) error {
	return checkDeployPath(deployRoot, rel, true)
}

func checkDeployPath(deployRoot, rel string, write bool) error {
	if !archive.ContainedKey(deployRoot, rel) {
		return fmt.Errorf("%s: path escapes the deploy root", rel)
	}
	last := rel
	if link := skillSymlink(deployRoot, rel); link != "" && !write {
		last = link
	}
	segs := strings.Split(filepath.ToSlash(filepath.Clean(filepath.FromSlash(last))), "/")
	if len(segs) < 2 {
		return nil
	}
	root, err := archive.RealPath(filepath.Join(deployRoot, segs[0]))
	if err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	end := len(segs)
	if write {
		end++
	}
	for i := 2; i < end; i++ {
		dir := strings.Join(segs[:i], "/")
		resolved, err := archive.RealPath(filepath.Join(deployRoot, filepath.FromSlash(dir)))
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		if !archive.Within(root, resolved) {
			return &DeployPathError{Path: rel, Link: dir, Resolved: resolved, Root: root}
		}
	}
	return nil
}

// keepOutsideDiag is the one diagnostic of a cleanup that keeps the paths
// below e.Link.
func keepOutsideDiag(e *DeployPathError) string {
	return fmt.Sprintf("keeping %q: it is a symlink to %s, outside %s, so nothing below it is removed", e.Link, e.Resolved, e.Root)
}

// withinTargetRoot reports whether the cleanup of empty parents may remove
// dir, a directory below root: it resolves inside the target root it is in,
// and it is not that target root when the target root is a symlink, which is
// the user's.
func withinTargetRoot(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." || !archive.Within(root, dir) {
		return false
	}
	first, _, deeper := strings.Cut(filepath.ToSlash(rel), "/")
	targetRoot := filepath.Join(root, first)
	if info, err := os.Lstat(targetRoot); !deeper && err == nil && info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	realRoot, err := archive.RealPath(targetRoot)
	if err != nil {
		return false
	}
	resolved, err := archive.RealPath(dir)
	return err == nil && archive.Within(realRoot, resolved)
}

// OutsideDeployPaths returns, sorted, the paths Oracle 0.33.0's uninstall
// lists as the tracked files it could not remove (measured, issue #86): each
// of files (lock paths relative to deployRoot) that CheckDeployPath refuses
// because a symlink leads it out of its target root, each recorded skill
// directory (an entry with no hash) with such a path below it, and each
// skill directory one of files is in whose tree, walked without following a
// symlink, holds a symlink that resolves (a dangling one by its target) out
// of its target root. The skill directory passes as a whole to a package
// that stays (the uninstall hand-over), also files the removed package does
// not have, so a symlink anywhere in it would take that write outside. A
// skill directory that is itself a symlink is not walked: it is the user's
// (skillSymlink).
func OutsideDeployPaths(deployRoot string, files []string, hashes map[string]string) []string {
	var out []string
	seen := make(map[string]bool)
	add := func(f string) {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	skillDirs := make(map[string]bool)
	for _, f := range files {
		if dir := SkillDirOf(f); dir != "" {
			skillDirs[dir] = true
		}
		var outside *DeployPathError
		if !errors.As(CheckDeployPath(deployRoot, f), &outside) {
			continue
		}
		add(f)
		p := filepath.ToSlash(filepath.Clean(filepath.FromSlash(f)))
		for _, d := range files {
			dir := filepath.ToSlash(filepath.Clean(filepath.FromSlash(d)))
			if _, hashed := hashes[d]; !hashed && strings.HasPrefix(p, dir+"/") {
				add(d)
			}
		}
	}
	for dir := range skillDirs {
		if !seen[dir] && treeLeavesTargetRoot(deployRoot, dir) {
			add(dir)
		}
	}
	sort.Strings(out)
	return out
}

// treeLeavesTargetRoot reports whether the skill directory dir (relative to
// deployRoot), a real directory whose parents pass CheckDeployPath, holds a
// symlink that resolves out of its target root.
func treeLeavesTargetRoot(deployRoot, dir string) bool {
	abs := filepath.Join(deployRoot, filepath.FromSlash(dir))
	if info, err := os.Lstat(abs); err != nil || !info.IsDir() || CheckDeployPath(deployRoot, dir) != nil {
		return false
	}
	first, _, _ := strings.Cut(dir, "/")
	root, err := archive.RealPath(filepath.Join(deployRoot, first))
	if err != nil {
		return false
	}
	leaves := false
	filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		if resolved, err := archive.RealPath(p); err != nil || !archive.Within(root, resolved) {
			leaves = true
			return filepath.SkipAll
		}
		return nil
	})
	return leaves
}

// SkillDirOf returns the skill directory (<root>/skills/<name>) that the
// slash path p is or lies below, or "" when p is in no skill.
func SkillDirOf(p string) string {
	segs := strings.Split(path.Clean(strings.ReplaceAll(p, "\\", "/")), "/")
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] == "skills" {
			return strings.Join(segs[:i+2], "/")
		}
	}
	return ""
}
