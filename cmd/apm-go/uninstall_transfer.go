package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/apm-go/apm/internal/ux"
)

// uninstallHandover holds the paths of the removed packages that a package
// which stays also deploys (issue #74). The lock records a shared path under
// the first-declared package only, so without this step uninstalling that
// package deletes a file the other one still provides.
//
// Only these paths are touched: deploying every remaining package in place
// would rewrite files the user changed in them. Oracle 0.33.0 picks the owner
// of a shared file in its own way. Removing a package that is not that owner
// gives the same result here; removing that owner deviates: Oracle deletes the
// file, redeploys the remaining packages to other targets, and records no
// owner for it (commands/uninstall/engine.py:1106-1419).
type uninstallHandover struct {
	trialDir string
	// owner maps each handed-over lock path to the bucket that deploys it in
	// the trial: a lock key, or "" for the project's own content.
	owner map[string]string
	// hashes holds the trial's hash of each handed-over file; a skill
	// directory entry has none.
	hashes map[string]string
	// before holds the hash each handed-over file had on disk before the
	// removal; a path with nothing readable there has none.
	before map[string]string
	// order lists the buckets as install prints them: the direct dependencies
	// in the order apm.yml declares them, then the others in lock order, then
	// the project's own content.
	order []string
	// parentLinks holds, before the removal, each symlink on a handed-over
	// path above it and above a skill's directory (a project's .claude linked
	// elsewhere), by slash path, with its target. The removal deletes such a
	// link with the directories it leaves empty, so restore makes it again
	// before it puts the files back through it.
	parentLinks map[string]string
}

// planUninstallHandover deploys the packages that stay into a temporary
// directory, with the apm.yml already written without the removed ones, and
// keeps each removed lock path that one of them deploys; for a skill, the
// whole directory that one of them deploys. It returns nil when
// the removed packages recorded no path that a target governs.
func planUninstallHandover(lock *lockfile.Lockfile, removalKeys map[string]bool, deployDir string) (*uninstallHandover, error) {
	removed, _ := collectUninstallDeployedProvenance(lock, removalKeys)
	var targets []string
	for name := range deploy.Adapters {
		for _, p := range removed {
			if deploy.TargetsGovernPath([]string{name}, deploy.DependencyBucket, p) {
				targets = append(targets, name)
				break
			}
		}
	}
	if len(targets) == 0 {
		return nil, nil
	}
	sort.Strings(targets)

	_, _, m, err := readUninstallManifest()
	if err != nil {
		return nil, err
	}
	// install hands deploy.Run the manifest with each local dependency under
	// its apm_modules key, the one a lock from before issue #50 names when it
	// has it; without that a local package loses its declaration order and its
	// skills: subset.
	for _, d := range allDirectDeps(m) {
		normalizeLocalDep(d)
	}
	useLegacyLocalKeys(m, lock)
	useGitPathKeys(m, lock)
	result := &resolver.ResolutionResult{}
	for i := range lock.Dependencies {
		d := &lock.Dependencies[i]
		if removalKeys[d.UniqueKey()] {
			continue
		}
		result.Deps = append(result.Deps, resolver.ResolvedDep{
			Key:               d.UniqueKey(),
			Kind:              lockedKind(d),
			RepoURL:           d.RepoURL,
			Host:              d.Host,
			Port:              d.Port,
			ArtifactoryPrefix: d.RegistryPrefix,
			VirtualPath:       d.VirtualPath,
			Constraint:        d.Constraint,
			ResolvedTag:       d.ResolvedTag,
			ResolvedRef:       d.ResolvedRef,
			Commit:            d.ResolvedCommit,
			Depth:             d.Depth,
			ResolvedBy:        d.ResolvedBy,
			TargetSubset:      d.TargetSubset,
		})
	}
	var filter *deploy.SkillFilter
	if subsets, _ := persistedSkillSubsets(m); len(subsets) > 0 {
		filter = &deploy.SkillFilter{Subsets: subsets}
	}
	trialDir, err := os.MkdirTemp("", "apm-go-uninstall-*")
	if err != nil {
		return nil, err
	}
	h := &uninstallHandover{trialDir: trialDir, owner: map[string]string{}, hashes: map[string]string{}, before: map[string]string{}, parentLinks: map[string]string{}}
	listed := map[string]bool{}
	for _, d := range allDirectDeps(m) {
		if key := deploy.DepRefKey(d); key != "" && !listed[key] {
			listed[key] = true
			h.order = append(h.order, key)
		}
	}
	for _, dep := range result.Deps {
		if !listed[dep.Key] {
			listed[dep.Key] = true
			h.order = append(h.order, dep.Key)
		}
	}
	h.order = append(h.order, "")
	trial, err := deploy.Run(targets, ".", m, result, filter, trialDir, deployDir != "")
	if err != nil {
		h.close()
		return nil, err
	}

	deployedBy := map[string]string{}
	for key, dr := range trial.PerDep {
		for _, f := range withSkillDirs(dr.Files) {
			deployedBy[f] = key
		}
	}
	take := func(key, p string) {
		h.owner[p] = key
		if hash, ok := trial.PerDep[key].Hashes[p]; ok {
			h.hashes[p] = hash
		}
	}
	for _, p := range removed {
		// A skill is a directory deployed as one unit, so it is handed over
		// whole: the survivor's own files below it come too, and the removed
		// package's own files there are not handed over.
		if dir := deploy.SkillDirOf(p); dir != "" {
			key, ok := deployedBy[dir]
			if !ok {
				continue
			}
			take(key, dir)
			for _, f := range trial.PerDep[key].Files {
				if strings.HasPrefix(f, dir+"/") {
					take(key, f)
				}
			}
			continue
		}
		if key, ok := deployedBy[p]; ok {
			take(key, p)
		}
	}
	root := "."
	if deployDir != "" {
		root = deployDir
	}
	for p := range h.owner {
		if hash, err := lockfile.HashFileBytes(filepath.Join(root, filepath.FromSlash(p))); err == nil {
			h.before[p] = hash
		}
		segs := strings.Split(p, "/")
		for i := 0; i < len(segs)-1 && (i == 0 || segs[i-1] != "skills"); i++ {
			rel := strings.Join(segs[:i+1], "/")
			if target, err := os.Readlink(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
				h.parentLinks[rel] = target
			}
		}
	}
	return h, nil
}

func (h *uninstallHandover) close() {
	if h != nil {
		os.RemoveAll(h.trialDir)
	}
}

// restore puts the trial's entry at each handed-over path that has nothing
// on it under root. A path that is still there was kept by the removal (the
// user changed it, or it is the user's symlink) and is not written. A path
// that cannot be restored is reported and no longer handed over.
func (h *uninstallHandover) restore(root string) {
	if h == nil {
		return
	}
	links := make([]string, 0, len(h.parentLinks))
	for rel := range h.parentLinks {
		links = append(links, rel)
	}
	sort.Strings(links)
	var brokenLinks []string
	for _, rel := range links {
		dst := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Lstat(dst); !os.IsNotExist(err) {
			continue
		}
		err := os.MkdirAll(filepath.Dir(dst), 0o755)
		if err == nil {
			err = os.Symlink(h.parentLinks[rel], dst)
		}
		if err != nil {
			warnReintegrationSkipped(fmt.Errorf("restore symlink %s: %w", rel, err))
			brokenLinks = append(brokenLinks, rel)
		}
	}
	paths := make([]string, 0, len(h.owner))
	for p := range h.owner {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if atOrBelowAny(brokenLinks, p) {
			delete(h.owner, p)
			delete(h.hashes, p)
			continue
		}
		if err := restoreFromTrial(h.trialDir, root, p); err != nil {
			warnReintegrationSkipped(fmt.Errorf("restore %s: %w", p, err))
			delete(h.owner, p)
			delete(h.hashes, p)
		}
	}
}

// restoreFromTrial creates, from the top down, each segment of the slash path
// p that is missing under root, as the trial has it: a directory, a file with
// its bytes and mode, or a symlink with its target (the skill directory of a
// --global deploy). A symlink at p or at a skill's directory (<root>/skills/
// <name>) is the user's, or the skill symlink just restored, and nothing is
// written through it. A symlink at any other parent (~/.claude linked into a
// dotfiles directory) is walked through, as deploy and removal walk it, but
// each segment is checked before it is made (deploy.CheckDeployWrite; for a
// symlink, deploy.CheckDeployPath), since os.Mkdir and the copy follow a
// parent that leads out of the target root, or out of the skill's directory.
func restoreFromTrial(trialDir, root, p string) error {
	segs := strings.Split(p, "/")
	for i := range segs {
		rel := filepath.Join(segs[:i+1]...)
		dst := filepath.Join(root, rel)
		info, err := os.Lstat(dst)
		if err == nil {
			isSkillDir := i > 0 && segs[i-1] == "skills"
			if info.Mode()&os.ModeSymlink != 0 && (i == len(segs)-1 || isSkillDir) {
				return nil
			}
			continue
		}
		if !os.IsNotExist(err) {
			return err
		}
		src := filepath.Join(trialDir, rel)
		sinfo, err := os.Lstat(src)
		if err != nil {
			return err
		}
		check := deploy.CheckDeployWrite
		if sinfo.Mode()&os.ModeSymlink != 0 {
			check = deploy.CheckDeployPath
		}
		if err := check(root, filepath.ToSlash(rel)); err != nil {
			return err
		}
		switch {
		case sinfo.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(src)
			if err != nil {
				return err
			}
			return os.Symlink(target, dst)
		case sinfo.IsDir():
			if err := os.Mkdir(dst, sinfo.Mode().Perm()); err != nil {
				return err
			}
		default:
			return copyNewFile(src, dst, sinfo.Mode().Perm())
		}
	}
	return nil
}

func copyNewFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// notHandedOver drops the handed-over paths from the removal's list, so they
// are not reported as cleaned.
func (h *uninstallHandover) notHandedOver(removed []string) []string {
	if h == nil || len(h.owner) == 0 {
		return removed
	}
	out := removed[:0:0]
	for _, f := range removed {
		if _, ok := h.owner[f]; !ok {
			out = append(out, f)
		}
	}
	return out
}

// record adds each handed-over path to the lock record of the bucket that
// deploys it, with the trial's hash; the record keeps what it had.
func (h *uninstallHandover) record(lock *lockfile.Lockfile) {
	if h == nil {
		return
	}
	byOwner := map[string][]string{}
	for p, key := range h.owner {
		byOwner[key] = append(byOwner[key], p)
	}
	for key, paths := range byOwner {
		files, hashes := &lock.LocalDeployedFiles, &lock.LocalDeployedHashes
		if key != "" {
			dep := lock.FindByKey(key)
			if dep == nil {
				continue
			}
			files, hashes = &dep.DeployedFiles, &dep.DeployedHashes
		}
		seen := make(map[string]bool, len(*files))
		for _, f := range *files {
			seen[f] = true
		}
		merged := append([]string(nil), *files...)
		for _, p := range paths {
			if !seen[p] {
				merged = append(merged, p)
			}
		}
		*files = withSkillDirs(merged)
		for _, p := range paths {
			if hash, ok := h.hashes[p]; ok {
				if *hashes == nil {
					*hashes = map[string]string{}
				}
				(*hashes)[p] = hash
			}
		}
	}
}

// printBlocks prints, for each package that received a path, the lines of its
// install block without the header, as the Oracle prints them after
// "Updated <apm.yml>" (commands/uninstall/engine.py:1106-1419). They count
// every file the lock now records for the package. A file is adopted when it
// had the recorded hash on disk before the removal, as the Oracle's redeploy
// adopts a file already there with the bytes it would write and integrates
// any other (integration/instruction_integrator.py:452-485,
// base_integrator.py:429); a handed-over file is judged by h.before, since
// restore has rewritten it. The Oracle prints them for every remaining
// package; apm-go prints them only for one that received a path, so an
// uninstall that hands nothing over prints what it printed before.
func (h *uninstallHandover) printBlocks(lock *lockfile.Lockfile, root string) {
	if h == nil || lock == nil {
		return
	}
	received := map[string]bool{}
	for _, key := range h.owner {
		received[key] = true
	}
	for _, key := range h.order {
		if !received[key] {
			continue
		}
		files, hashes := lock.LocalDeployedFiles, lock.LocalDeployedHashes
		if key != "" {
			dep := lock.FindByKey(key)
			if dep == nil {
				continue
			}
			files, hashes = dep.DeployedFiles, dep.DeployedHashes
		}
		before := map[string]string{}
		for _, f := range files {
			if _, handedOver := h.owner[f]; handedOver {
				if hash, ok := h.before[f]; ok {
					before[f] = hash
				}
			} else if hash, err := lockfile.HashFileBytes(filepath.Join(root, filepath.FromSlash(f))); err == nil {
				before[f] = hash
			}
		}
		printInstallBlockLines(files, hashes, before)
	}
}

// warnReintegrationSkipped reports a hand-over that did not happen. The
// second sentence is the Oracle's (commands/uninstall/engine.py:1405-1408);
// the first names the error, since the trial covers every remaining package
// at once.
func warnReintegrationSkipped(err error) {
	ux.Warn(os.Stderr, "Re-integration skipped: %v. Run 'apm-go install' to rebuild integrated files.", err)
}
