package main

import (
	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
)

// staleCleanup is the outcome for one lock bucket: the project's own .apm/
// content or one dependency.
type staleCleanup struct {
	label   string
	removed int
	diags   []string
}

// cleanStaleDeployedFiles deletes deployed files whose source is gone: paths
// the previous lock recorded for a bucket that this run's deploy over targets
// did not produce again (oracle: install/phases/cleanup.py:144-209 for
// dependencies, install/phases/post_deps_local.py:105-127 for local content).
//
// A path the run's targets do not govern is kept: it belongs to a target
// this run did not deploy, for example after a different --target (oracle:
// install/manifest_reconcile.py:184-191). A dependency with no entry in
// deployed produced nothing this run, so none of its old files can be told
// apart from stale ones and all are kept.
//
// newLock must already carry this run's deployed files. Results are ordered
// local bucket first, then newLock.Dependencies order.
func cleanStaleDeployedFiles(existingLock, newLock *lockfile.Lockfile, deployed map[string]*deploy.DepDeployResult, targets []string, projectDir string) []staleCleanup {
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
	clean := func(label string, bucket deploy.DeployBucket, oldFiles []string, oldHashes map[string]string) {
		var stale []string
		for _, f := range oldFiles {
			if claimed[normalizeDeployPath(f)] || deploy.IsMCPConfigPath(f) || !deploy.TargetsGovernPath(targets, bucket, f) {
				continue
			}
			stale = append(stale, f)
		}
		if len(stale) == 0 {
			return
		}
		removed, _, diags := deploy.RemoveDeployedFiles(projectDir, stale, oldHashes)
		if len(removed) > 0 || len(diags) > 0 {
			results = append(results, staleCleanup{label: label, removed: len(removed), diags: diags})
		}
	}

	clean("<local .apm/>", deploy.LocalBucket, existingLock.LocalDeployedFiles, existingLock.LocalDeployedHashes)
	for i := range newLock.Dependencies {
		key := newLock.Dependencies[i].UniqueKey()
		old := existingLock.FindByKey(key)
		if _, ok := deployed[key]; !ok || old == nil {
			continue
		}
		clean(key, deploy.DependencyBucket, old.DeployedFiles, old.DeployedHashes)
	}
	return results
}
