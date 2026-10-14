package main

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/apm-go/apm/internal/ux"
)

// Issue #65: a frozen install whose locked deployed files are not on disk (a
// fresh clone, CI) deploys them from the lock instead of failing. The lock and
// apm.yml are never written, and a deploy that does not reproduce the lock's
// hashes is an error.
//
// Deliberate deviation from the Oracle: its --frozen checks the lock's
// structure and then runs an ordinary install, which overwrites a changed
// deployed file and rewrites the lock when the sources changed.

const notInLockfile = "<not in lockfile>"

func frozenViolationError(path, expected, observed string) error {
	if observed == "" {
		observed = "<missing>"
	}
	return fmt.Errorf("frozen install: content-integrity violation: %s expected %s, observed %s",
		path, expected, observed)
}

// frozenDeployPlan is what a frozen install deploys to. A nil plan is a frozen
// install with every locked file in place: it verifies and deploys nothing.
type frozenDeployPlan struct {
	targets    []string
	deployDir  string
	deployRoot string
}

// planFrozenDeploy checks the deployed files against the lock. It returns nil
// when all of them verify, an error for a file that is there with other
// content or cannot be read, and a plan when the only violations are paths
// with nothing at them. It writes nothing and makes no network request.
func planFrozenDeploy(lock *lockfile.Lockfile, m *manifest.Manifest, haveManifest bool, targetFlag, deployDir string) (*frozenDeployPlan, error) {
	deployRoot := "."
	if deployDir != "" {
		deployRoot = deployDir
	}
	viol := lockfile.VerifyDeployedState(lock, deployRoot)
	if len(viol) == 0 {
		return nil, nil
	}
	for _, v := range viol {
		_, err := os.Lstat(filepath.Join(deployRoot, filepath.FromSlash(v.Path)))
		if v.Observed != "" || !errors.Is(err, fs.ErrNotExist) {
			return nil, frozenViolationError(v.Path, v.Expected, v.Observed)
		}
	}
	// Without apm.yml there are no direct dependencies and no target to
	// deploy from.
	if !haveManifest {
		return nil, frozenViolationError(viol[0].Path, viol[0].Expected, "")
	}
	targets, targetDiags := deploy.ResolveTargets(targetFlag, m.Target, deployRoot)
	for _, d := range targetDiags {
		ux.Warn(os.Stderr, "%s", d)
	}
	if len(targets) == 0 {
		return nil, errNoDeployTarget()
	}
	// A path no target of this run governs would not be written by the deploy.
	for _, v := range viol {
		if !frozenTargetsGovern(lock, targets, v.Path) {
			return nil, frozenViolationError(v.Path, v.Expected, "")
		}
	}
	return &frozenDeployPlan{targets: targets, deployDir: deployDir, deployRoot: deployRoot}, nil
}

// frozenTargetsGovern reports whether targets govern relPath in every bucket
// of the lock that records it.
func frozenTargetsGovern(lock *lockfile.Lockfile, targets []string, relPath string) bool {
	for i := range lock.Dependencies {
		if _, ok := lock.Dependencies[i].DeployedHashes[relPath]; ok &&
			!deploy.TargetsGovernPath(targets, deploy.DependencyBucket, relPath) {
			return false
		}
	}
	if _, ok := lock.LocalDeployedHashes[relPath]; ok &&
		!deploy.TargetsGovernPath(targets, deploy.LocalBucket, relPath) {
		return false
	}
	return true
}

// run deploys what the lock pins and verifies the result. It runs after the
// registry and git sources of the lock are in apm_modules/. A failure leaves
// the files already written; the next frozen install reports them.
func (p *frozenDeployPlan) run(deps *installDeps, m *manifest.Manifest, lock *lockfile.Lockfile) error {
	// The frozen source step skips local packages, so a fresh clone has no
	// copy of them. An existing copy is kept, as a frozen install with
	// nothing missing keeps it.
	for _, dep := range allDirectDeps(m) {
		if dep.LocalSourcePath == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join("apm_modules", deploy.DepRefKey(dep))); err == nil {
			continue
		}
		if _, err := deps.loader.LoadPackage(dep, ""); err != nil {
			return fmt.Errorf("frozen install: copy local package %s: %w", dep.LocalDeclaredPath, err)
		}
	}

	// deploy.Run finds the transitive dependencies in this list.
	result := &resolver.ResolutionResult{}
	for i := range lock.Dependencies {
		d := &lock.Dependencies[i]
		result.Deps = append(result.Deps, resolver.ResolvedDep{
			Key:               d.UniqueKey(),
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
		})
	}
	var filter *deploy.SkillFilter
	if subsets := effectiveSkillSubsets(m, nil, nil); len(subsets) > 0 {
		filter = &deploy.SkillFilter{Subsets: subsets}
	}
	deployed, err := deploy.Run(p.targets, ".", m, result, filter, p.deployDir, p.deployDir != "")
	if err != nil {
		return fmt.Errorf("frozen install: deploy: %w", err)
	}
	for _, d := range deployed.Diags {
		ux.Warn(os.Stderr, "%s", d)
	}

	if err := frozenDeployMatchesLock(deployed, lock); err != nil {
		return err
	}
	if viol := lockfile.VerifyDeployedState(lock, p.deployRoot); len(viol) > 0 {
		return frozenViolationError(viol[0].Path, viol[0].Expected, viol[0].Observed)
	}
	return nil
}

// frozenDeployMatchesLock reports the first file this deploy wrote that the
// lock does not record with the same hash in the same bucket: the sources are
// not what the lock was written from. Buckets are in VerifyDeployedState's
// order, then the buckets the lock does not have; paths ascend in each.
func frozenDeployMatchesLock(deployed *deploy.DeployResult, lock *lockfile.Lockfile) error {
	check := func(key string, locked map[string]string) error {
		dr := deployed.PerDep[key]
		if dr == nil {
			return nil
		}
		for _, path := range slices.Sorted(maps.Keys(dr.Hashes)) {
			written := dr.Hashes[path]
			if written == "" {
				continue
			}
			expected, ok := locked[path]
			if !ok {
				expected = notInLockfile
			}
			if expected != written {
				return frozenViolationError(path, expected, written)
			}
		}
		return nil
	}
	inLock := map[string]bool{"": true}
	for i := range lock.Dependencies {
		key := lock.Dependencies[i].UniqueKey()
		inLock[key] = true
		if err := check(key, lock.Dependencies[i].DeployedHashes); err != nil {
			return err
		}
	}
	if err := check("", lock.LocalDeployedHashes); err != nil {
		return err
	}
	for _, key := range slices.Sorted(maps.Keys(deployed.PerDep)) {
		if inLock[key] {
			continue
		}
		if err := check(key, nil); err != nil {
			return err
		}
	}
	return nil
}
