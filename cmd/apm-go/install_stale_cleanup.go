package main

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
)

// staleCleanup is the outcome for one lock bucket: the project's own .apm/
// content or one dependency.
type staleCleanup struct {
	label   string
	removed []string // lock-relative paths, sorted
	diags   []string
}

// cleanStaleDeployedFiles deletes deployed files whose source is gone: paths
// the previous lock recorded for a bucket that this run's deploy over targets
// did not produce again (oracle: install/phases/cleanup.py:144-209 for
// dependencies, install/phases/post_deps_local.py:105-127 for local content).
// A dependency that deployed nothing this run is cleaned like any other: the
// oracle records it with an empty file list (install/template.py:318).
//
// A path the run's targets do not govern is kept: it belongs to a target
// this run did not deploy, for example after a different --target (oracle:
// install/manifest_reconcile.py:184-191).
//
// A bucket in failed is skipped whole: a file that failed to re-deploy is
// missing from this run's list and would look stale (oracle:
// install/phases/cleanup.py:148-152 for dependencies,
// install/phases/post_deps_local.py:60-62,125 for local content). failed uses
// the keys of deploy.DeployResult.FailedBuckets.
//
// A path at or below one of keptDirs (deploy.DeployResult.KeptDirs) is not
// stale and gets no warning: the deploy left that directory to the user, so
// the files in it are the user's, also one that still has the recorded hash.
// So is a path in rejected (rejectedSet of deploy.DeployResult.RejectedPaths),
// whichever bucket records it: its symlink is the user's, and the Oracle
// leaves it.
//
// deployRoot is the directory lock paths are relative to. A non-empty
// linkSourceRoot says the deploy made symlinks into that project directory (a
// --global install): a stale symlink is then removed itself and nothing is
// deleted through it (deploy.RemoveStaleLinkedFiles). Only a symlink into the
// bucket's own source there, .apm or apm_modules/<key>, is the deploy's; one
// the user pointed anywhere else is kept.
//
// newLock must already carry this run's deployed files. Results are ordered
// local bucket first, then newLock.Dependencies order.
func cleanStaleDeployedFiles(existingLock, newLock *lockfile.Lockfile, failed, rejected map[string]bool, keptDirs, targets []string, deployRoot, linkSourceRoot string) []staleCleanup {
	claimed := claimedPaths(newLock)

	var results []staleCleanup
	clean := func(label string, bucket deploy.DeployBucket, source string, oldFiles []string, oldHashes map[string]string) {
		var stale []string
		for _, f := range oldFiles {
			if claimed[normalizeDeployPath(f)] || deploy.IsMCPConfigPath(f) || !deploy.TargetsGovernPath(targets, bucket, f) || deploy.UnderKeptDir(keptDirs, f) || rejected[normalizeDeployPath(f)] {
				continue
			}
			stale = append(stale, f)
		}
		if len(stale) == 0 {
			return
		}
		var removed, diags []string
		if linkSourceRoot != "" {
			removed, diags = deploy.RemoveStaleLinkedFiles(deployRoot, linkSourceRoot, filepath.Join(linkSourceRoot, source), stale, oldHashes, claimed)
		} else {
			removed, _, diags = deploy.RemoveDeployedFiles(deployRoot, stale, oldHashes)
		}
		if len(removed) > 0 || len(diags) > 0 {
			sort.Strings(removed)
			results = append(results, staleCleanup{label: label, removed: removed, diags: diags})
		}
	}

	if !failed[""] {
		clean("<local .apm/>", deploy.LocalBucket, ".apm", existingLock.LocalDeployedFiles, existingLock.LocalDeployedHashes)
	}
	for i := range newLock.Dependencies {
		key := newLock.Dependencies[i].UniqueKey()
		old := existingLock.FindByKey(key)
		if old == nil || failed[key] {
			continue
		}
		clean(key, deploy.DependencyBucket, filepath.Join("apm_modules", filepath.FromSlash(key)), old.DeployedFiles, old.DeployedHashes)
	}
	return results
}

// rejectedSet is every path of deploy.DeployResult.RejectedPaths, normalized:
// a refused path is the user's whichever package records it.
func rejectedSet(byBucket map[string][]string) map[string]bool {
	set := make(map[string]bool)
	for _, paths := range byBucket {
		for _, f := range paths {
			set[normalizeDeployPath(f)] = true
		}
	}
	return set
}

// claimedPaths is every path newLock records for this run, normalized.
func claimedPaths(newLock *lockfile.Lockfile) map[string]bool {
	claimed := make(map[string]bool)
	for _, f := range newLock.LocalDeployedFiles {
		claimed[normalizeDeployPath(f)] = true
	}
	for _, d := range newLock.Dependencies {
		for _, f := range d.DeployedFiles {
			claimed[normalizeDeployPath(f)] = true
		}
	}
	return claimed
}

// orphanDeps returns the dependencies the previous lock records and this run
// no longer resolves, transitive ones included, as the Oracle compares the
// old lock with intended_dep_keys (install/phases/resolve.py:975-980).
// existingLock is lockUnderCurrentKeys' view of the old lock.
func orphanDeps(existingLock, newLock *lockfile.Lockfile) []lockfile.LockedDep {
	var orphans []lockfile.LockedDep
	for i := range existingLock.Dependencies {
		old := &existingLock.Dependencies[i]
		if newLock.FindByKey(old.UniqueKey()) == nil {
			orphans = append(orphans, *old)
		}
	}
	return orphans
}

// lockUnderCurrentKeys returns the old lock's records with each local entry
// in the format before issue #50 that a local dependency of m still declares
// under that dependency's current key, matched as useLegacyLocalKeys matches
// it, and each git entry of a `git:` local path as useLocalKeysForGitPaths
// turns it, so the stale cleanup and the lock merge find the package's old
// record. An old-format entry no dependency declares keeps its key and is an
// orphan. existingLock is not changed: the lock write and
// removeLegacyLocalModules read the keys it was written with.
func lockUnderCurrentKeys(existingLock *lockfile.Lockfile, m *manifest.Manifest) *lockfile.Lockfile {
	prev := &lockfile.Lockfile{
		Dependencies:        slices.Clone(existingLock.Dependencies),
		LocalDeployedFiles:  existingLock.LocalDeployedFiles,
		LocalDeployedHashes: existingLock.LocalDeployedHashes,
	}
	for _, dep := range slices.Concat(m.ParsedDeps, m.ParsedDevDeps) {
		if dep.LocalSourcePath == "" || existingLock.FindByKey(dep.RepoURL) != nil {
			continue
		}
		legacy := legacyLocalModulesKey(dep.LocalSourcePath)
		for i := range prev.Dependencies {
			if old := &prev.Dependencies[i]; old.UniqueKey() == legacy && isLegacyLocalEntry(old) {
				old.RepoURL = dep.RepoURL
			}
		}
	}
	useLocalKeysForGitPaths(allDirectDeps(m), prev)
	return prev
}

// orphanCleanup is what cleanOrphanedDeployedFiles did.
type orphanCleanup struct {
	removed []string // lock-relative paths of every orphan, sorted
	diags   []string
	kept    []lockfile.LockedDep
}

// cleanOrphanedDeployedFiles deletes the files the orphans deployed (oracle:
// install/phases/cleanup.py:69-131). Their paths are judged against every
// target apm-go deploys to, not this run's, because the Oracle passes
// targets=None there: a file of a target this run no longer names goes too.
// A path this run deployed, an MCP config file, a path at or below keptDirs
// and a path no apm-go target writes are not touched and leave the record. A
// path in rejected is not touched either and keeps the record while the file
// it names exists, as the Oracle keeps it under the orphan.
//
// An orphan with a path that is still on disk after the delete (a hash
// mismatch, a failed delete) is returned in kept with only those paths and
// their old hashes, as the Oracle keeps it (orphan_cleanup_retained), so a
// later run can still clean them. deployRoot and linkSourceRoot are as for
// cleanStaleDeployedFiles; newLock must already carry this run's deployed
// files.
func cleanOrphanedDeployedFiles(orphans []lockfile.LockedDep, newLock *lockfile.Lockfile, rejected map[string]bool, keptDirs []string, deployRoot, linkSourceRoot string) orphanCleanup {
	claimed := claimedPaths(newLock)
	allTargets := deploy.AllTargets()
	takenOverRoot := ""
	if linkSourceRoot != "" {
		takenOverRoot = deployRoot
	}
	var out orphanCleanup
	for _, dep := range orphans {
		var candidates, refused []string
		for _, f := range dep.DeployedFiles {
			if rejected[normalizeDeployPath(f)] {
				refused = append(refused, f)
				continue
			}
			if claimed[normalizeDeployPath(f)] || deploy.IsMCPConfigPath(f) || !deploy.TargetsGovernPath(allTargets, deploy.DependencyBucket, f) || deploy.UnderKeptDir(keptDirs, f) {
				continue
			}
			candidates = append(candidates, f)
		}
		if len(candidates) == 0 && len(refused) == 0 {
			continue
		}
		key := dep.UniqueKey()
		var removed, diags []string
		switch {
		case len(candidates) == 0:
		case linkSourceRoot != "":
			// A removed skill link counts every path of the skill, as the
			// Oracle, which copies the files, counts them; uninstall
			// --global counts them the same way.
			var gone []string
			gone, diags = deploy.RemoveStaleLinkedFiles(deployRoot, linkSourceRoot, filepath.Join(linkSourceRoot, "apm_modules", filepath.FromSlash(key)), candidates, dep.DeployedHashes, claimed)
			for _, f := range candidates {
				if atOrBelowAny(gone, f) {
					removed = append(removed, f)
				}
			}
		default:
			removed, _, diags = deploy.RemoveDeployedFiles(deployRoot, candidates, dep.DeployedHashes)
		}
		out.removed = append(out.removed, removed...)
		out.diags = append(out.diags, diags...)
		left, hashes := mergeDeployedFiles(append(notTakenOver(key, candidates, allTargets, takenOverRoot), refused...), dep.DeployedHashes, nil, nil, deployRoot)
		if len(left) > 0 {
			dep.DeployedFiles, dep.DeployedHashes = left, hashes
			out.kept = append(out.kept, dep)
		}
	}
	sort.Strings(out.removed)
	return out
}

// withSkillDirs adds, for each deployed skill, the skill's own directory
// (<root>/skills/<name>) to the deployed files, as the Oracle's lock lists it:
// a path with no hash (measured on Oracle v0.33.0, issue #50). A --global
// deploy lists it too; there the directory is the skill symlink.
func withSkillDirs(files []string) []string {
	out := append([]string(nil), files...)
	seen := make(map[string]bool, len(files))
	for _, f := range files {
		seen[f] = true
	}
	for _, f := range files {
		segs := strings.Split(normalizeDeployPath(f), "/")
		for i := 0; i+2 < len(segs); i++ {
			if segs[i] != "skills" {
				continue
			}
			if dir := strings.Join(segs[:i+2], "/"); !seen[dir] {
				seen[dir] = true
				out = append(out, dir)
			}
			break
		}
	}
	sort.Strings(out)
	return out
}

// withoutOrphanSkillDirs drops from a merged file list each skill directory
// entry (<root>/skills/<name>, no hash) that has no file below it left in the
// list. The entry follows its files: whether the path exists says nothing,
// because a directory the user filled, or a skill symlink the user pointed
// elsewhere, is still there after the skill is gone.
func withoutOrphanSkillDirs(files []string, hashes map[string]string) []string {
	out := files[:0:0]
	for _, f := range files {
		segs := strings.Split(normalizeDeployPath(f), "/")
		_, hashed := hashes[f]
		if !hashed && len(segs) >= 2 && segs[len(segs)-2] == "skills" && !hasFileBelow(files, f) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func hasFileBelow(files []string, dir string) bool {
	prefix := normalizeDeployPath(dir) + "/"
	for _, f := range files {
		if strings.HasPrefix(normalizeDeployPath(f), prefix) {
			return true
		}
	}
	return false
}

// notTakenOver drops from a bucket's old lock paths each one at or below an
// entry the user took over (deploy.TakenOver) that this run's targets govern.
// The stale cleanup has kept it and warned once; with the record gone the
// warning is not repeated, and a frozen install and `audit` do not check a
// file that is the user's. bucket is "" for the project's own content.
// For a symlink deploy it also drops a path seen through a skill's link that
// the skill's deploy does not write (deploy.LeftOutThroughLink): a lock
// written before #113 lists a cloned package's .git there, one written
// before #126 the files the Oracle's copy leaves out. The stale cleanup
// removes nothing below the deploy's own link.
func notTakenOver(bucket string, files, targets []string, deployDir string) []string {
	kind, source := deploy.DependencyBucket, filepath.Join("apm_modules", filepath.FromSlash(bucket))
	if bucket == "" {
		kind, source = deploy.LocalBucket, ".apm"
	}
	root := "."
	if deployDir == "" {
		source = "" // a copy deploy
	} else {
		root = deployDir
	}
	leftOut := func(string) bool { return false }
	if source != "" {
		leftOut = deploy.LeftOutThroughLink(root, source, bucket)
	}
	out := files[:0:0]
	for _, f := range files {
		if deploy.TargetsGovernPath(targets, kind, f) && deploy.TakenOver(root, source, f) {
			continue
		}
		if leftOut(f) {
			continue
		}
		out = append(out, f)
	}
	return out
}
