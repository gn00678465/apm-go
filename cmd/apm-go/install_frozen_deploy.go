package main

import (
	"cmp"
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
	mcp        []string
	deployDir  string
	deployRoot string
	// missing holds the locked paths with nothing at them.
	missing map[string]bool
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
	chosen := resolveInstallTargets(targetFlag, m.Target, deployDir)
	targets, targetDiags := chosen.targets, chosen.diags
	for _, d := range targetDiags {
		ux.Warn(os.Stderr, "%s", d)
	}
	if len(targets) == 0 {
		return nil, errNoDeployTarget()
	}
	// A path no target of this run governs would not be written by the deploy.
	for _, v := range viol {
		if !frozenTargetsGovern(lock, targets, v.Path, deployDir != "") {
			return nil, frozenViolationError(v.Path, v.Expected, "")
		}
	}
	missing := make(map[string]bool, len(viol))
	for _, v := range viol {
		missing[v.Path] = true
	}
	return &frozenDeployPlan{targets: targets, mcp: chosen.mcp, deployDir: deployDir, deployRoot: deployRoot, missing: missing}, nil
}

// frozenTargetsGovern reports whether targets govern relPath in every bucket
// of the lock that records it.
func frozenTargetsGovern(lock *lockfile.Lockfile, targets []string, relPath string, userScope bool) bool {
	for i := range lock.Dependencies {
		if _, ok := lock.Dependencies[i].DeployedHashes[relPath]; ok &&
			!deploy.Governs(userScope, targets, deploy.DependencyBucket, relPath) {
			return false
		}
	}
	if _, ok := lock.LocalDeployedHashes[relPath]; ok &&
		!deploy.Governs(userScope, targets, deploy.LocalBucket, relPath) {
		return false
	}
	return true
}

// run deploys what the lock pins and verifies the result. It runs after the
// registry and git sources of the lock are in apm_modules/. The deploy is
// tried in a temporary directory first, so a deploy that would not reproduce
// the lock fails with the deploy root as it was; the sources this run put in
// apm_modules/ stay, as the source step leaves them.
func (p *frozenDeployPlan) run(deps *installDeps, m *manifest.Manifest, lock *lockfile.Lockfile) error {
	// The frozen source step skips local packages, so a fresh clone has no
	// copy of them. An existing copy is kept, as a frozen install with
	// nothing missing keeps it.
	for _, dep := range allDirectDeps(m) {
		if dep.LocalSourcePath == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join("apm_modules", filepath.FromSlash(deploy.DepRefDir(dep)))); err == nil {
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
		result.Deps = append(result.Deps, lockedResolvedDep(d))
	}
	var filter *deploy.SkillFilter
	if subsets := effectiveSkillSubsets(m, nil, nil); len(subsets) > 0 {
		filter = &deploy.SkillFilter{Subsets: subsets}
	}
	trialDir, err := os.MkdirTemp("", "apm-go-frozen-*")
	if err != nil {
		return fmt.Errorf("frozen install: create trial deploy directory: %w", err)
	}
	defer os.RemoveAll(trialDir)
	// The trial takes the mode of the real deploy: a symlink deploy records
	// the symlinks inside a skill, which a copy deploy skips.
	// Whether a path is the user's is read in the real deploy root, so the
	// trial leaves out what the deploy in place leaves.
	managed := lockManaged(lock, cmp.Or(p.deployDir, "."))
	managed.NoAdopt = true
	trial, err := deploy.Run(p.targets, ".", m, result, filter, trialDir, p.deployDir != "", managed, deploy.WithMCPTargets(p.mcp))
	if err == nil {
		if err = frozenDeployMatchesLock(trial, lock); err == nil {
			err = p.missingDeployed(trial, lock)
		}
	} else {
		err = fmt.Errorf("frozen install: deploy: %w", err)
	}
	if err != nil {
		if trial != nil {
			for _, d := range trial.Diags {
				ux.Warn(os.Stderr, "%s", d)
			}
		}
		return err
	}

	deployed, err := deploy.Run(p.targets, ".", m, result, filter, p.deployDir, p.deployDir != "", managed, deploy.WithMCPTargets(p.mcp))
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

// missingDeployed reports the first locked path with nothing at it that the
// deploy did not write in the bucket that records it, in VerifyDeployedState's
// order: deploying would leave it missing.
func (p *frozenDeployPlan) missingDeployed(deployed *deploy.DeployResult, lock *lockfile.Lockfile) error {
	check := func(key string, locked map[string]string) error {
		var written map[string]string
		if dr := deployed.PerDep[key]; dr != nil {
			written = dr.Hashes
		}
		for _, path := range slices.Sorted(maps.Keys(locked)) {
			if p.missing[path] && written[path] == "" {
				return frozenViolationError(path, locked[path], "")
			}
		}
		return nil
	}
	for i := range lock.Dependencies {
		if err := check(lock.Dependencies[i].UniqueKey(), lock.Dependencies[i].DeployedHashes); err != nil {
			return err
		}
	}
	return check("", lock.LocalDeployedHashes)
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
		_, hashes := dr.Recorded()
		for _, path := range slices.Sorted(maps.Keys(hashes)) {
			written := hashes[path]
			if written == "" {
				continue
			}
			expected, ok := locked[path]
			if !ok {
				return frozenViolationError(path, notInLockfile, written)
			}
			// The lock may record a bare hex digest (req-lk-016), so the
			// parsed digests are compared, as VerifyDeployedState does.
			algo, expectedHex, err := lockfile.ParseHashEnvelope(expected)
			_, writtenHex, _ := lockfile.ParseHashEnvelope(written)
			if err != nil || algo != "sha256" || expectedHex != writtenHex {
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

// useLockedAliases gives each direct dependency the lock entry's alias, for a
// deploy that takes its packages from the lock: the alias names the directory
// below apm_modules the lock's packages are in, and a lock an apm-go before
// issue #151 wrote records none although apm.yml declares one.
func useLockedAliases(deps []*manifest.DependencyReference, lock *lockfile.Lockfile) {
	for _, d := range deps {
		if entry := lock.FindByKey(deploy.DepRefKey(d)); entry != nil {
			d.Alias = entry.Alias
		}
	}
}

// lockedResolvedDep is the dependency a lock entry records, for a deploy that
// takes its packages from the lock (a frozen install, the uninstall
// hand-over).
func lockedResolvedDep(d *lockfile.LockedDep) resolver.ResolvedDep {
	return resolver.ResolvedDep{
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
		Alias:             d.Alias,
		Insecure:          d.IsInsecure,
		AllowInsecure:     d.AllowInsecure,
	}
}
