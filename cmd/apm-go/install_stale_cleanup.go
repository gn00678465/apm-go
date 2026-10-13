package main

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
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
func cleanStaleDeployedFiles(existingLock, newLock *lockfile.Lockfile, failed map[string]bool, keptDirs, targets []string, deployRoot, linkSourceRoot string) []staleCleanup {
	claimed := make(map[string]bool)
	for _, f := range newLock.LocalDeployedFiles {
		claimed[normalizeDeployPath(f)] = true
	}
	for _, d := range newLock.Dependencies {
		for _, f := range d.DeployedFiles {
			claimed[normalizeDeployPath(f)] = true
		}
	}

	var results []staleCleanup
	clean := func(label string, bucket deploy.DeployBucket, source string, oldFiles []string, oldHashes map[string]string) {
		var stale []string
		for _, f := range oldFiles {
			if claimed[normalizeDeployPath(f)] || deploy.IsMCPConfigPath(f) || !deploy.TargetsGovernPath(targets, bucket, f) || deploy.UnderKeptDir(keptDirs, f) {
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
	out := files[:0:0]
	for _, f := range files {
		if deploy.TargetsGovernPath(targets, kind, f) && deploy.TakenOver(root, source, f) {
			continue
		}
		out = append(out, f)
	}
	return out
}
