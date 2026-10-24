package main

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
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
	// notes holds the trial's notes about the files it deployed; written,
	// the handed-over paths restore wrote.
	notes   []deploy.CommandNote
	written map[string]bool
	// refreshed holds the packages whose directory below apm_modules the
	// uninstall gave a new copy, refreshedOld the paths the lock recorded
	// for each of them, and refreshedPaths all those paths; old, the hash
	// the lock recorded for each handed-over path before the uninstall;
	// root, the deploy root; global, a --global uninstall, whose deploy
	// links its files into the global project directory, the working one.
	refreshed      map[string]bool
	refreshedOld   map[string][]string
	refreshedPaths []string
	old            map[string]string
	root           string
	global         bool
}

// planUninstallHandover deploys the packages that stay into a temporary
// directory, with the apm.yml already written without the removed ones, and
// keeps each removed lock path that one of them deploys; for a skill, the
// whole directory that one of them deploys. A package in refreshed, whose
// directory below apm_modules the uninstall gave its own copy, gets every
// path the trial deploys for it, as the Oracle deploys the remaining packages
// again (commands/uninstall/engine.py:1106-1419). It returns nil when
// the removed packages and the refreshed ones recorded no path that a target
// governs.
func planUninstallHandover(lock *lockfile.Lockfile, removalKeys, refreshed map[string]bool, deployDir string) (*uninstallHandover, error) {
	// With no lock no package recorded a path to hand over.
	if lock == nil {
		return nil, nil
	}
	removed, _ := collectUninstallDeployedProvenance(lock, removalKeys)
	governed := append([]string(nil), removed...)
	var refreshedPaths []string
	refreshedOld := map[string][]string{}
	for key := range refreshed {
		if dep := lock.FindByKey(key); dep != nil {
			refreshedOld[key] = dep.DeployedFiles
			refreshedPaths = append(refreshedPaths, dep.DeployedFiles...)
		}
	}
	governed = append(governed, refreshedPaths...)
	var targets []string
	for name := range deploy.Adapters {
		for _, p := range governed {
			if deploy.Governs(deployDir != "", []string{name}, deploy.DependencyBucket, p) {
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
	useLockedAliases(allDirectDeps(m), lock)
	result := &resolver.ResolutionResult{}
	for i := range lock.Dependencies {
		d := &lock.Dependencies[i]
		if removalKeys[d.UniqueKey()] {
			continue
		}
		result.Deps = append(result.Deps, lockedResolvedDep(d))
	}
	var filter *deploy.SkillFilter
	if subsets, _ := persistedSkillSubsets(m); len(subsets) > 0 {
		filter = &deploy.SkillFilter{Subsets: subsets}
	}
	trialDir, err := os.MkdirTemp("", "apm-go-uninstall-*")
	if err != nil {
		return nil, err
	}
	h := &uninstallHandover{trialDir: trialDir, owner: map[string]string{}, hashes: map[string]string{}, before: map[string]string{}, parentLinks: map[string]string{}, written: map[string]bool{}, refreshed: refreshed, refreshedOld: refreshedOld, refreshedPaths: refreshedPaths, old: map[string]string{}, global: deployDir != ""}
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
	trial, err := deploy.Run(targets, ".", m, result, filter, trialDir, deployDir != "", lockManaged(lock, cmp.Or(deployDir, ".")))
	if err != nil {
		h.close()
		return nil, err
	}
	h.notes = trial.CommandNotes

	deployedBy := map[string]string{}
	for key, dr := range trial.PerDep {
		files, _ := dr.Recorded()
		for _, f := range withSkillDirs(files) {
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
	for key := range refreshed {
		if dr, ok := trial.PerDep[key]; ok {
			files, _ := dr.Recorded()
			for _, f := range withSkillDirs(files) {
				take(key, f)
			}
		}
	}
	root := "."
	if deployDir != "" {
		root = deployDir
	}
	h.root = root
	for i := range lock.Dependencies {
		for p, hash := range lock.Dependencies[i].DeployedHashes {
			if _, ok := h.owner[p]; ok {
				h.old[p] = hash
			}
		}
	}
	for p, hash := range lock.LocalDeployedHashes {
		if _, ok := h.owner[p]; ok {
			h.old[p] = hash
		}
	}
	for p, key := range h.owner {
		// A refreshed package's files are written again, so the block counts
		// them integrated, as the Oracle cleans them before it redeploys
		// (commands/uninstall/cli.py:516-524,544-585).
		if hash, err := lockfile.HashDeployedFile(filepath.Join(root, filepath.FromSlash(p))); err == nil && !refreshed[key] {
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
// user changed it, or it is the user's symlink) and is not written, except
// for a path the lock records for a refreshed package: its file or skill
// directory is replaced, as the Oracle cleans and redeploys the package
// whatever the content (measured with Oracle 0.33.0, a skill and an agent the
// user edited, in a project and at user scope). A path that cannot be
// restored is reported and no longer handed over.
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
	// A path a refreshed package's old copy deployed and its new copy does
	// not is removed, as the Oracle cleans every path of the package before
	// it deploys it again (commands/uninstall/cli.py:516-524).
	for _, key := range slices.Sorted(maps.Keys(h.refreshedOld)) {
		for _, p := range h.refreshedOld[key] {
			if _, kept := h.owner[p]; kept || atOrBelowAny(brokenLinks, p) {
				continue
			}
			if err := h.clearForRefresh(root, p); err != nil {
				warnReintegrationSkipped(fmt.Errorf("remove %s: %w", p, err))
			}
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
		if h.refreshed[h.owner[p]] && (slices.Contains(h.refreshedPaths, p) || anyBelow(h.refreshedPaths, p)) {
			if err := h.clearForRefresh(root, p); err != nil {
				warnReintegrationSkipped(fmt.Errorf("restore %s: %w", p, err))
				delete(h.owner, p)
				delete(h.hashes, p)
				continue
			}
		}
		_, statErr := os.Lstat(filepath.Join(root, filepath.FromSlash(p)))
		if err := restoreFromTrial(h.trialDir, root, p); err != nil {
			warnReintegrationSkipped(fmt.Errorf("restore %s: %w", p, err))
			delete(h.owner, p)
			delete(h.hashes, p)
		} else if os.IsNotExist(statErr) {
			h.written[p] = true
		}
	}
}

// clearForRefresh removes the regular file, the real skill directory, or the
// symlink a --global deploy made, at the slash path p under root, so that
// restore writes the refreshed package's content there. A symlink on a
// parent is walked through, within the bounds of deploy.CheckDeployPath (a
// target root linked elsewhere, a directory linked to another place in it);
// a symlink of the user's at p or at a skill's directory, and what is below
// it, is left, so restore does not write there either.
func (h *uninstallHandover) clearForRefresh(root, p string) error {
	segs := strings.Split(p, "/")
	for i := range segs {
		full := filepath.Join(root, filepath.Join(segs[:i+1]...))
		info, err := os.Lstat(full)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		last := i == len(segs)-1
		if info.Mode()&os.ModeSymlink != 0 && (last || (i > 0 && segs[i-1] == "skills")) {
			if !last || !h.global || !deploy.IsDeployLink(".", full) {
				return nil
			}
		} else if !last {
			continue
		}
		if err := deploy.CheckDeployPath(root, p); err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0, info.Mode().IsRegular():
			return os.Remove(full)
		case info.IsDir() && deploy.SkillDirOf(p) == p:
			return os.RemoveAll(full)
		}
	}
	return nil
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
// deploys it, with the trial's hash. A refreshed package's record is made
// again from what its new copy deploys, as the Oracle replaces it
// (commands/uninstall/lockfile_state.py:69-82), so a path its source no
// longer has leaves the lock; a file restore did not write (the user's
// symlink) and that lacks the trial's content keeps the hash the lock had
// for it, so the lock does not record content that is not on disk.
func (h *uninstallHandover) record(lock *lockfile.Lockfile) {
	if h == nil {
		return
	}
	byOwner := map[string][]string{}
	for key := range h.refreshedOld {
		byOwner[key] = nil
	}
	for p, key := range h.owner {
		byOwner[key] = append(byOwner[key], p)
	}
	for key, paths := range byOwner {
		if h.refreshed[key] {
			paths = slices.DeleteFunc(paths, func(p string) bool {
				_, recorded := h.old[p]
				return !h.written[p] && !recorded && h.hashes[p] != "" && !h.holdsTrialContent(p)
			})
		}
		files, hashes := &lock.LocalDeployedFiles, &lock.LocalDeployedHashes
		if key != "" {
			dep := lock.FindByKey(key)
			if dep == nil {
				continue
			}
			files, hashes = &dep.DeployedFiles, &dep.DeployedHashes
		}
		if h.refreshed[key] {
			*files, *hashes = nil, nil
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
			hash, ok := h.hashes[p]
			if !ok {
				continue
			}
			if h.refreshed[key] && !h.written[p] && !h.holdsTrialContent(p) {
				hash, ok = h.old[p]
			}
			if !ok {
				continue
			}
			if *hashes == nil {
				*hashes = map[string]string{}
			}
			(*hashes)[p] = hash
		}
	}
}

// holdsTrialContent reports whether the file at the handed-over path p has
// the content the trial deployed there.
func (h *uninstallHandover) holdsTrialContent(p string) bool {
	onDisk, err := lockfile.HashDeployedFile(filepath.Join(h.root, filepath.FromSlash(p)))
	return err == nil && onDisk == h.hashes[p]
}

// anyBelow reports whether one of paths is below the slash path p.
func anyBelow(paths []string, p string) bool {
	for _, q := range paths {
		if strings.HasPrefix(q, p+"/") {
			return true
		}
	}
	return false
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
		// A refreshed package's file that restore did not write, below the
		// user's symlink, is not counted.
		if h.refreshed[key] {
			files = slices.DeleteFunc(slices.Clone(files), func(f string) bool {
				return h.owner[f] == key && !h.written[f]
			})
		}
		before := map[string]string{}
		for _, f := range files {
			if _, handedOver := h.owner[f]; handedOver {
				if hash, ok := h.before[f]; ok {
					before[f] = hash
				}
			} else if hash, err := lockfile.HashDeployedFile(filepath.Join(root, filepath.FromSlash(f))); err == nil {
				before[f] = hash
			}
		}
		printInstallBlockLines(files, hashes, before)
	}
	// The Oracle's redeploy warns about each agent it writes again, after
	// every block (commands/uninstall/engine.py:1381-1413); an agent it
	// adopts gets no warning.
	for _, key := range h.order {
		for _, n := range h.notes {
			if n.DepKey == key && n.Path != "" && h.written[n.Path] && h.owner[n.Path] == key && h.before[n.Path] != h.hashes[n.Path] {
				ux.WarnAt(os.Stdout, "  ", "%s", commandNoteText(n))
			}
		}
	}
}

// warnReintegrationSkipped reports a hand-over that did not happen. The
// second sentence is the Oracle's (commands/uninstall/engine.py:1405-1408);
// the first names the error, since the trial covers every remaining package
// at once.
func warnReintegrationSkipped(err error) {
	ux.Warn(os.Stderr, "Re-integration skipped: %v. Run 'apm-go install' to rebuild integrated files.", err)
}
